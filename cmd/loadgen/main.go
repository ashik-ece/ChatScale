// Command loadgen opens many WebSocket connections to the gateways, publishes
// at a fixed rate, and reports delivery latency, loss and reconnect times.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/ashik-ece/chatscale/internal/loadgen"
)

func main() {
	var (
		target       = flag.String("target", "ws://localhost:8081,ws://localhost:8082,ws://localhost:8083", "comma-separated gateway base URLs")
		connections  = flag.Int("connections", 1000, "final number of connections")
		start        = flag.Int("start", 0, "connections opened immediately (default: all)")
		rampStep     = flag.Int("ramp-step", 0, "connections added per ramp interval")
		rampInterval = flag.Duration("ramp-interval", 0, "time between ramp steps")
		senders      = flag.Int("senders", 5, "connections that also publish")
		msgRate      = flag.Float64("msg-rate", 2, "messages per second per sender")
		duration     = flag.Duration("duration", 60*time.Second, "maximum test duration")
		room         = flag.String("room", "loadtest", "room to join")
		prom         = flag.String("prometheus", "http://localhost:9090", "Prometheus URL for server-side figures (empty to disable)")
		stopDrops    = flag.Float64("stop-drops-per-min", 0, "stop when the gateways drop more than this many messages per minute (0 = never)")
		stopP99      = flag.Float64("stop-p99-ms", 0, "also stop a ramping run when a connection level's p99 exceeds this many ms (0 = never)")
		dialConc     = flag.Int("dial-concurrency", 256, "simultaneous connection attempts")
		scenario     = flag.String("scenario", "adhoc", "scenario label for the results file")
		run          = flag.Int("run", 1, "run number for the results file")
		out          = flag.String("out", "", "append the result as a JSON line to this file")
	)
	flag.Parse()

	cfg := loadgen.Config{
		Targets:         strings.Split(*target, ","),
		Room:            *room,
		Connections:     *connections,
		Start:           *start,
		RampStep:        *rampStep,
		RampInterval:    *rampInterval,
		Senders:         *senders,
		MsgRate:         *msgRate,
		Duration:        *duration,
		PrometheusURL:   *prom,
		StopDropsPerMin: *stopDrops,
		StopP99Ms:       *stopP99,
		DialConcurrency: *dialConc,
		Scenario:        *scenario,
		Run:             *run,
		Log:             os.Stderr,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	res, err := loadgen.Run(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	res.Print(os.Stdout)

	if *out != "" {
		f, err := os.OpenFile(*out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		if err := res.WriteJSON(f); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(os.Stderr, "result appended to %s\n", *out)
	}
}
