// Command loadsummary reads the JSON lines written by loadgen and prints a
// Markdown table per scenario, one row per run plus the median across runs.
//
//	go run ./cmd/loadsummary results/runs.jsonl
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/ashik-ece/chatscale/internal/loadgen"
)

var order = []string{"baseline", "medium", "high", "ceiling", "failure", "failure5k", "spike"}

type column struct {
	name string
	get  func(*loadgen.Result) (float64, bool)
	fmt  string
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: loadsummary <runs.jsonl>")
	}
	runs, err := read(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	for _, scenario := range order {
		rs := runs[scenario]
		if len(rs) == 0 {
			continue
		}
		sort.Slice(rs, func(i, j int) bool { return rs[i].Config.Run < rs[j].Config.Run })
		print(scenario, rs, columnsFor(scenario))
	}
}

func read(path string) (map[string][]*loadgen.Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	runs := map[string][]*loadgen.Result{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20) // lines carry the whole timeline
	for sc.Scan() {
		var r loadgen.Result
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, err
		}
		runs[r.Config.Scenario] = append(runs[r.Config.Scenario], &r)
	}
	return runs, sc.Err()
}

func columnsFor(scenario string) []column {
	cols := []column{
		{"peak conns", func(r *loadgen.Result) (float64, bool) { return float64(r.PeakConns), true }, "%.0f"},
		{"msgs sent", func(r *loadgen.Result) (float64, bool) { return float64(r.MessagesSent), true }, "%.0f"},
		{"est. loss %", func(r *loadgen.Result) (float64, bool) {
			if r.Expected == 0 {
				return 0, false
			}
			return 100 * float64(r.EstimatedDrops) / float64(r.Expected), true
		}, "%.3f"},
		{"p50 ms", func(r *loadgen.Result) (float64, bool) { return r.P50Ms, true }, "%.1f"},
		{"p95 ms", func(r *loadgen.Result) (float64, bool) { return r.P95Ms, true }, "%.1f"},
		{"p99 ms", func(r *loadgen.Result) (float64, bool) { return r.P99Ms, true }, "%.1f"},
		{"server drops", server(func(s *loadgen.Server) float64 { return s.Dropped }), "%.0f"},
		{"fan-out p99 ms", server(func(s *loadgen.Server) float64 { return s.FanoutP99Ms }), "%.3f"},
		{"max GC pause ms", server(func(s *loadgen.Server) float64 { return s.GCPauseMaxMs }), "%.2f"},
		{"peak RSS MiB (max node)", server(func(s *loadgen.Server) float64 {
			var m float64
			for _, v := range s.PeakRSSMiB {
				m = max(m, v)
			}
			return m
		}), "%.0f"},
	}
	switch scenario {
	case "ceiling":
		cols = append([]column{
			{"stable conns", func(r *loadgen.Result) (float64, bool) {
				if r.Stable == nil {
					return 0, false
				}
				return float64(r.Stable.Connections), true
			}, "%.0f"},
			{"stable p99 ms", func(r *loadgen.Result) (float64, bool) {
				if r.Stable == nil {
					return 0, false
				}
				return r.Stable.P99Ms, true
			}, "%.1f"},
		}, cols...)
	case "failure", "failure5k":
		cols = append(cols,
			column{"reconnects", reconnect(func(c *loadgen.Reconnect) float64 { return float64(c.Count) }), "%.0f"},
			column{"reconnect p50 ms", reconnect(func(c *loadgen.Reconnect) float64 { return c.P50Ms }), "%.0f"},
			column{"reconnect p95 ms", reconnect(func(c *loadgen.Reconnect) float64 { return c.P95Ms }), "%.0f"},
			column{"reconnect max ms", reconnect(func(c *loadgen.Reconnect) float64 { return c.MaxMs }), "%.0f"},
		)
	}
	return cols
}

func server(f func(*loadgen.Server) float64) func(*loadgen.Result) (float64, bool) {
	return func(r *loadgen.Result) (float64, bool) {
		if r.Server == nil {
			return 0, false
		}
		return f(r.Server), true
	}
}

func reconnect(f func(*loadgen.Reconnect) float64) func(*loadgen.Result) (float64, bool) {
	return func(r *loadgen.Result) (float64, bool) {
		if r.Reconnect == nil {
			return 0, false
		}
		return f(r.Reconnect), true
	}
}

func print(scenario string, rs []*loadgen.Result, cols []column) {
	fmt.Printf("### %s\n\n| run |", scenario)
	sep := "| --- |"
	for _, c := range cols {
		fmt.Printf(" %s |", c.name)
		sep += " ---: |"
	}
	fmt.Printf("\n%s\n", sep)

	values := make([][]float64, len(cols))
	for _, r := range rs {
		fmt.Printf("| %d |", r.Config.Run)
		for i, c := range cols {
			v, ok := c.get(r)
			if !ok {
				fmt.Print(" n/a |")
				continue
			}
			values[i] = append(values[i], v)
			fmt.Printf(" "+c.fmt+" |", v)
		}
		fmt.Println()
	}
	fmt.Print("| **median** |")
	for i, c := range cols {
		if len(values[i]) == 0 {
			fmt.Print(" n/a |")
			continue
		}
		fmt.Printf(" **"+c.fmt+"** |", median(values[i]))
	}
	fmt.Print("\n\n")
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
