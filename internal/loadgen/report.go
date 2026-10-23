package loadgen

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

// Sample is one point of the run's timeline.
type Sample struct {
	T          float64 `json:"t"`
	Connected  int     `json:"connected"`
	Sent       uint64  `json:"sent"`
	Received   uint64  `json:"received"`
	RecvPerSec float64 `json:"recvPerSec"`
	P50Ms      float64 `json:"p50Ms"` // trailing minute
	P95Ms      float64 `json:"p95Ms"`
	P99Ms      float64 `json:"p99Ms"`
	PromDrops  float64 `json:"promDrops1m"` // -1 when Prometheus was not queried
}

// Level is the outcome of one connection level of a ramping run, judged only
// on samples taken at that level (not the trailing minute, which still holds
// the previous, lighter level just after a ramp step).
type Level struct {
	Connections int     `json:"connections"`
	P99Ms       float64 `json:"p99Ms"`
	RecvPerSec  float64 `json:"recvPerSec"`
	PromDrops   float64 `json:"promDrops1m"`
	AtSeconds   float64 `json:"atSeconds"`
}

// Stable is the last connection level that stayed within every threshold.
type Stable struct {
	Connections int     `json:"connections"`
	P99Ms       float64 `json:"p99Ms"`
	AtSeconds   float64 `json:"atSeconds"`
}

// Reconnect summarises how long clients took to receive a message again after losing a connection.
type Reconnect struct {
	Count int     `json:"count"`
	P50Ms float64 `json:"p50Ms"`
	P95Ms float64 `json:"p95Ms"`
	MaxMs float64 `json:"maxMs"`
}

// Server holds figures read back from Prometheus after the run.
type Server struct {
	Dropped         float64            `json:"dropped"`          // slow-consumer drops reported by the gateways
	FanoutP99Ms     float64            `json:"fanoutP99Ms"`      // server-side, last minute
	GCPauseMaxMs    float64            `json:"gcPauseMaxMs"`     // worst recent GC pause on any node
	PeakRSSMiB      map[string]float64 `json:"peakRssMiBByNode"` // by scrape target
	PeakConnections map[string]float64 `json:"peakConnectionsByNode"`
}

// Result is the outcome of one run, written as one JSON line.
type Result struct {
	Config Config `json:"config"`

	DurationSec    float64 `json:"durationSec"`
	PeakConns      int64   `json:"peakConnections"`
	ConnsAlive     int64   `json:"connectionsAlive"`
	FailedDials    int64   `json:"failedDials"`
	ErrorFrames    int64   `json:"errorFrames"`
	MessagesSent   uint64  `json:"messagesSent"`
	MessagesRecv   uint64  `json:"messagesReceived"`
	Expected       uint64  `json:"expectedDeliveries"`
	EstimatedDrops int64   `json:"estimatedDrops"`
	P50Ms          float64 `json:"p50Ms"`
	P95Ms          float64 `json:"p95Ms"`
	P99Ms          float64 `json:"p99Ms"`

	StoppedBy string     `json:"stoppedBy,omitempty"` // "drops" or "p99" when a ceiling threshold ended the run
	Levels    []Level    `json:"levels,omitempty"`
	Stable    *Stable    `json:"stable,omitempty"`
	Reconnect *Reconnect `json:"reconnect,omitempty"`
	Server    *Server    `json:"server,omitempty"`
	Samples   []Sample   `json:"samples"`
}

func (r *Result) finish(g *Gen, elapsed time.Duration) {
	tot := g.totals()
	snap := g.hist.snapshot()

	r.DurationSec = elapsed.Seconds()
	r.PeakConns = g.peak.Load()
	r.ConnsAlive = g.connected.Load()
	r.FailedDials = g.failedDials.Load()
	r.ErrorFrames = g.errorFrames.Load()
	r.MessagesSent = tot.sent
	r.MessagesRecv = tot.received
	r.Expected = tot.expected
	r.EstimatedDrops = int64(tot.expected) - int64(tot.received)
	r.P50Ms, r.P95Ms, r.P99Ms = ms(snap.percentile(0.50)), ms(snap.percentile(0.95)), ms(snap.percentile(0.99))

	if len(g.rejoin) > 0 {
		d := durationsMs(g.rejoin)
		r.Reconnect = &Reconnect{
			Count: len(d),
			P50Ms: d[len(d)/2],
			P95Ms: d[min(len(d)-1, int(0.95*float64(len(d))))],
			MaxMs: d[len(d)-1],
		}
	}
	if g.cfg.PrometheusURL != "" {
		r.Server = queryServer(g.cfg.PrometheusURL, elapsed)
	}
}

func queryServer(base string, elapsed time.Duration) *Server {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	window := fmt.Sprintf("%ds", int(elapsed.Seconds())+5)
	s := &Server{PeakRSSMiB: map[string]float64{}, PeakConnections: map[string]float64{}}

	s.Dropped, _ = promScalar(ctx, base, fmt.Sprintf(`sum(increase(chatscale_messages_dropped_total[%s]))`, window))
	s.FanoutP99Ms, _ = promScalar(ctx, base, `histogram_quantile(0.99, sum by (le) (rate(chatscale_fanout_latency_seconds_bucket[1m]))) * 1000`)
	if v, err := promScalar(ctx, base, `max(go_gc_duration_seconds{job="chatscale",quantile="1.0"}) * 1000`); err == nil {
		s.GCPauseMaxMs = v
	}
	if m, err := promQuery(ctx, base, fmt.Sprintf(`max_over_time(process_resident_memory_bytes{job="chatscale"}[%s])`, window), "instance"); err == nil {
		for k, v := range m {
			s.PeakRSSMiB[k] = v / (1 << 20)
		}
	}
	if m, err := promQuery(ctx, base, fmt.Sprintf(`max_over_time(chatscale_active_connections[%s])`, window), "node"); err == nil {
		s.PeakConnections = m
	}
	return s
}

// WriteJSON writes the result as a single line of JSON.
func (r *Result) WriteJSON(w io.Writer) error {
	return json.NewEncoder(w).Encode(r)
}

// Print writes a human-readable summary.
func (r *Result) Print(w io.Writer) {
	fmt.Fprintf(w, "\n=== %s run %d: %.0fs ===\n", r.Config.Scenario, r.Config.Run, r.DurationSec)
	fmt.Fprintf(w, "connections    peak=%d alive_at_end=%d failed_dials=%d\n", r.PeakConns, r.ConnsAlive, r.FailedDials)
	fmt.Fprintf(w, "messages       sent=%d received=%d expected=%d estimatedDrops=%d (%.3f%%)\n",
		r.MessagesSent, r.MessagesRecv, r.Expected, r.EstimatedDrops, pct(r.EstimatedDrops, r.Expected))
	fmt.Fprintf(w, "e2e latency    p50=%.2fms p95=%.2fms p99=%.2fms\n", r.P50Ms, r.P95Ms, r.P99Ms)
	if r.Stable != nil {
		fmt.Fprintf(w, "last stable    %d connections, trailing-minute p99=%.2fms (t=%.0fs)\n", r.Stable.Connections, r.Stable.P99Ms, r.Stable.AtSeconds)
	}
	switch r.StoppedBy {
	case "drops":
		fmt.Fprintf(w, "stopped        server drops exceeded %.0f/min\n", r.Config.StopDropsPerMin)
	case "p99":
		fmt.Fprintf(w, "stopped        level p99 exceeded %.0fms\n", r.Config.StopP99Ms)
	}
	for _, l := range r.Levels {
		fmt.Fprintf(w, "  level %-6d p99=%8.1fms recv/s=%-9.0f drops(1m)=%.0f\n", l.Connections, l.P99Ms, l.RecvPerSec, l.PromDrops)
	}
	if r.Reconnect != nil {
		fmt.Fprintf(w, "reconnect      n=%d p50=%.0fms p95=%.0fms max=%.0fms\n", r.Reconnect.Count, r.Reconnect.P50Ms, r.Reconnect.P95Ms, r.Reconnect.MaxMs)
	}
	if s := r.Server; s != nil {
		fmt.Fprintf(w, "server         dropped=%.0f fanout_p99(1m)=%.3fms max_gc_pause=%.2fms\n", s.Dropped, s.FanoutP99Ms, s.GCPauseMaxMs)
		for _, k := range sortedKeys(s.PeakRSSMiB) {
			fmt.Fprintf(w, "               %s peak RSS %.0f MiB\n", k, s.PeakRSSMiB[k])
		}
	}
	if r.ErrorFrames > 0 {
		fmt.Fprintf(w, "WARNING: %d non-chat frames received (rate-limit errors?); raise RATE_LIMIT_* on the gateways\n", r.ErrorFrames)
	}
}

func pct(n int64, of uint64) float64 {
	if of == 0 {
		return 0
	}
	return 100 * float64(n) / float64(of)
}

func sortedKeys(m map[string]float64) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
