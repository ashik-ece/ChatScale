package loadgen

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	sampleEvery   = 5 * time.Second
	trailingSamps = 12 // 12 x 5s: the trailing minute used for "stable" figures
)

// Gen is one running load test.
type Gen struct {
	cfg Config
	ctx context.Context

	connected atomic.Int64
	peak      atomic.Int64
	hist      *histogram

	dialSem     chan struct{}
	failedDials atomic.Int64
	errorFrames atomic.Int64

	rampSteps atomic.Int64 // completed ramp steps

	wg      sync.WaitGroup
	mu      sync.Mutex
	clients []*client
	rejoin  []time.Duration // reconnect-time samples
}

// Run executes the test described by cfg and returns its measurements.
func Run(ctx context.Context, cfg Config) (*Result, error) {
	if len(cfg.Targets) == 0 || cfg.Connections <= 0 || cfg.Duration <= 0 {
		return nil, fmt.Errorf("need at least one target, connections > 0 and a duration")
	}
	cfg.applyDefaults()

	g := &Gen{
		cfg:     cfg,
		hist:    newHistogram(),
		dialSem: make(chan struct{}, cfg.DialConcurrency),
	}

	// Clients outlive the measured period: the run context ends the test, but
	// the final figures are read while everyone is still connected.
	clientCtx, stopClients := context.WithCancel(ctx)
	defer stopClients()
	runCtx, endRun := context.WithTimeout(clientCtx, cfg.Duration)
	defer endRun()

	g.ctx = clientCtx
	started := time.Now()
	g.addClients(cfg.Start)
	go g.ramp(runCtx)

	res := g.sample(runCtx, endRun, started)
	res.finish(g, time.Since(started))

	stopClients()
	g.wg.Wait()
	return res, nil
}

func (g *Gen) logf(format string, a ...any) {
	if g.cfg.Log != nil {
		fmt.Fprintf(g.cfg.Log, format+"\n", a...)
	}
}

func (g *Gen) addClients(n int) { g.addClientsCtx(g.ctx, n) }

func (g *Gen) addClientsCtx(ctx context.Context, n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := 0; i < n && len(g.clients) < g.cfg.Connections; i++ {
		id := len(g.clients)
		c := &client{id: id, user: fmt.Sprintf("lg-%d", id), sender: id < g.cfg.Senders}
		g.clients = append(g.clients, c)
		g.wg.Add(1)
		go c.run(ctx, g)
	}
}

func (g *Gen) ramp(ctx context.Context) {
	if g.cfg.RampInterval <= 0 || g.cfg.RampStep <= 0 {
		return
	}
	t := time.NewTicker(g.cfg.RampInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.addClientsCtx(ctx, g.cfg.RampStep)
			g.rampSteps.Add(1)
		}
	}
}

func (g *Gen) dial(ctx context.Context, target, user string) (*websocket.Conn, error) {
	select {
	case g.dialSem <- struct{}{}:
		defer func() { <-g.dialSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second, ReadBufferSize: 1024, WriteBufferSize: 1024}
	conn, _, err := d.DialContext(ctx, fmt.Sprintf("%s/ws/%s?user=%s", target, g.cfg.Room, user), nil)
	return conn, err
}

func (g *Gen) noteConnected(delta int64) {
	n := g.connected.Add(delta)
	for {
		p := g.peak.Load()
		if n <= p || g.peak.CompareAndSwap(p, n) {
			return
		}
	}
}

func (g *Gen) noteReconnect(d time.Duration) {
	g.mu.Lock()
	g.rejoin = append(g.rejoin, d)
	g.mu.Unlock()
}

type totals struct{ sent, received, expected uint64 }

func (g *Gen) totals() totals {
	g.mu.Lock()
	defer g.mu.Unlock()
	var t totals
	for _, c := range g.clients {
		t.sent += c.sent.Load()
		t.received += c.received.Load()
		t.expected += c.expected.Load()
	}
	return t
}

// sample records a point every sampleEvery and returns when ctx ends or a
// ceiling-search threshold trips.
func (g *Gen) sample(ctx context.Context, stop context.CancelFunc, started time.Time) *Result {
	res := &Result{Config: g.cfg}
	snaps := []*snapshot{g.hist.snapshot()}
	var prev totals
	prevAt := started
	ceiling := newCeiling(g.cfg, res, snaps[0])

	tick := time.NewTicker(sampleEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			ceiling.finish()
			return res
		case <-tick.C:
		}
		now := time.Now()
		tot := g.totals()
		snap := g.hist.snapshot()
		snaps = append(snaps, snap)
		old := snaps[max(0, len(snaps)-1-trailingSamps)]
		win := snap.minus(old)

		s := Sample{
			T:          now.Sub(started).Seconds(),
			Connected:  int(g.connected.Load()),
			Sent:       tot.sent,
			Received:   tot.received,
			RecvPerSec: float64(tot.received-prev.received) / now.Sub(prevAt).Seconds(),
			P50Ms:      ms(win.percentile(0.50)),
			P95Ms:      ms(win.percentile(0.95)),
			P99Ms:      ms(win.percentile(0.99)),
			PromDrops:  -1,
		}
		prev, prevAt = tot, now

		if g.cfg.PrometheusURL != "" {
			if v, err := promScalar(ctx, g.cfg.PrometheusURL, `sum(increase(chatscale_messages_dropped_total[1m]))`); err == nil {
				s.PromDrops = v
			}
		}
		res.Samples = append(res.Samples, s)
		g.logf("t=%4.0fs conns=%-6d sent=%-8d recv/s=%-9.0f p99(1m)=%7.2fms drops(1m)=%.0f errframes=%d",
			s.T, s.Connected, s.Sent, s.RecvPerSec, s.P99Ms, s.PromDrops, g.errorFrames.Load())

		if ceiling.observe(s, snap, g.rampSteps.Load()) {
			g.logf("level of %d connections exceeded a threshold (%s): stopping", s.Connected, res.StoppedBy)
			stop()
			return res
		}
	}
}

func ms(ns uint64) float64 { return float64(ns) / 1e6 }

func durationsMs(ds []time.Duration) []float64 {
	out := make([]float64, len(ds))
	for i, d := range ds {
		out[i] = float64(d) / float64(time.Millisecond)
	}
	sort.Float64s(out)
	return out
}
