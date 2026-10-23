package loadgen_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ashik-ece/chatscale/internal/filter"
	"github.com/ashik-ece/chatscale/internal/hub"
	"github.com/ashik-ece/chatscale/internal/loadgen"
	"github.com/ashik-ece/chatscale/internal/metrics"
	"github.com/ashik-ece/chatscale/internal/pubsub"
	"github.com/ashik-ece/chatscale/internal/ratelimit"
	"github.com/ashik-ece/chatscale/internal/ws"
)

type node struct {
	srv *httptest.Server
	hub *hub.Hub
}

func (n *node) url() string { return "ws" + strings.TrimPrefix(n.srv.URL, "http") }

func startNode(t *testing.T, bus *pubsub.MemoryBus, id string) *node {
	t.Helper()
	limiter := ratelimit.NewStore(ratelimit.Config{Capacity: 1000, RefillRate: 1000})
	h := hub.New(limiter, filter.NewTrie(), bus, bus.NewSubscriber(), id, metrics.New(id, prometheus.NewRegistry()))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/{roomID}", ws.ServeWS(h, ws.NewUpgrader(nil)))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &node{srv: srv, hub: h}
}

func TestRunMeasuresDeliveryAndLatency(t *testing.T) {
	bus := pubsub.NewMemoryBus()
	a, b := startNode(t, bus, "a"), startNode(t, bus, "b")

	res, err := loadgen.Run(context.Background(), loadgen.Config{
		Targets:     []string{a.url(), b.url()},
		Connections: 20,
		Senders:     2,
		MsgRate:     20,
		Duration:    3 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	if res.PeakConns != 20 {
		t.Errorf("peak connections = %d, want 20", res.PeakConns)
	}
	if res.MessagesSent < 60 {
		t.Errorf("sent = %d, want about 120 (2 senders x 20/s x 3s)", res.MessagesSent)
	}
	// Each message goes to all 20 connections. The last few sent as the run
	// ends may not arrive, so allow a small shortfall.
	if res.MessagesRecv < res.Expected*95/100 {
		t.Errorf("received %d of %d expected deliveries", res.MessagesRecv, res.Expected)
	}
	if res.EstimatedDrops != int64(res.Expected)-int64(res.MessagesRecv) {
		t.Errorf("estimatedDrops = %d is inconsistent with expected-received", res.EstimatedDrops)
	}
	if res.P99Ms <= 0 || res.P99Ms > 200 {
		t.Errorf("p99 = %.2fms, want a small positive number", res.P99Ms)
	}
	if res.ErrorFrames != 0 {
		t.Errorf("error frames = %d, want 0", res.ErrorFrames)
	}
}

func TestRunMeasuresReconnectAfterNodeFailure(t *testing.T) {
	bus := pubsub.NewMemoryBus()
	a, b := startNode(t, bus, "a"), startNode(t, bus, "b")

	go func() {
		time.Sleep(1500 * time.Millisecond)
		b.hub.Shutdown()
		b.srv.CloseClientConnections()
		b.srv.Close() // further dials to b are refused, like a stopped container
	}()

	res, err := loadgen.Run(context.Background(), loadgen.Config{
		Targets:     []string{a.url(), b.url()},
		Connections: 20,
		Senders:     4,
		MsgRate:     20,
		Duration:    7 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	if res.Reconnect == nil || res.Reconnect.Count < 5 {
		t.Fatalf("expected about 10 clients to reconnect, got %+v", res.Reconnect)
	}
	if res.Reconnect.MaxMs <= 0 || res.Reconnect.MaxMs > 6000 {
		t.Errorf("max reconnect = %.0fms, want a few seconds at most", res.Reconnect.MaxMs)
	}
	if res.PeakConns != 20 {
		t.Errorf("peak connections = %d, want 20", res.PeakConns)
	}
	// Everyone ends up on the surviving node; a client caught between
	// dial retries when the run ends may still be missing.
	if res.ConnsAlive < 18 {
		t.Errorf("alive at end = %d, want about 20", res.ConnsAlive)
	}
}

func TestRunStopsWhenServerDropsExceedThreshold(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"metric":{},"value":[0,"42"]}]}}`))
	}))
	defer prom.Close()

	bus := pubsub.NewMemoryBus()
	a := startNode(t, bus, "a")

	start := time.Now()
	res, err := loadgen.Run(context.Background(), loadgen.Config{
		Targets:         []string{a.url()},
		Connections:     10,
		Senders:         1,
		MsgRate:         5,
		Duration:        time.Minute,
		PrometheusURL:   prom.URL,
		StopDropsPerMin: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.StoppedBy != "drops" {
		t.Errorf("StoppedBy = %q, want drops", res.StoppedBy)
	}
	if time.Since(start) > 15*time.Second {
		t.Errorf("run took %v, should stop at the first sample", time.Since(start))
	}
}

func TestRampStopsWhenALevelExceedsP99(t *testing.T) {
	t.Parallel()
	a := startNode(t, pubsub.NewMemoryBus(), "a")

	res, err := loadgen.Run(context.Background(), loadgen.Config{
		Targets:      []string{a.url()},
		Connections:  15,
		Start:        5,
		RampStep:     5,
		RampInterval: 8 * time.Second,
		Senders:      1,
		MsgRate:      10,
		Duration:     time.Minute,
		StopP99Ms:    0.0001, // no real delivery is this fast
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.StoppedBy != "p99" {
		t.Errorf("StoppedBy = %q, want p99", res.StoppedBy)
	}
	if len(res.Levels) != 1 || res.Stable != nil {
		t.Errorf("want one failing level and no stable level, got levels=%+v stable=%+v", res.Levels, res.Stable)
	}
}

func TestRampRecordsEachLevelAndTheLastStableOne(t *testing.T) {
	t.Parallel()
	a := startNode(t, pubsub.NewMemoryBus(), "a")

	res, err := loadgen.Run(context.Background(), loadgen.Config{
		Targets:      []string{a.url()},
		Connections:  15,
		Start:        5,
		RampStep:     5,
		RampInterval: 8 * time.Second,
		Senders:      1,
		MsgRate:      10,
		Duration:     22 * time.Second,
		StopP99Ms:    10_000, // never trips
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.StoppedBy != "" {
		t.Errorf("StoppedBy = %q, want the run to complete", res.StoppedBy)
	}
	if len(res.Levels) < 2 {
		t.Fatalf("expected at least two levels, got %+v", res.Levels)
	}
	for i := 1; i < len(res.Levels); i++ {
		if res.Levels[i].Connections <= res.Levels[i-1].Connections {
			t.Errorf("levels should grow: %+v", res.Levels)
		}
	}
	if res.Stable == nil || res.Stable.Connections != res.Levels[len(res.Levels)-1].Connections {
		t.Errorf("stable = %+v, want the last level %+v", res.Stable, res.Levels[len(res.Levels)-1])
	}
}
