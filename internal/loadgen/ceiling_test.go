package loadgen

import (
	"testing"
	"time"
)

// snapAt builds a cumulative snapshot with n samples at the given latency.
func snapWith(h *histogram, n int, latency time.Duration) *snapshot {
	for i := 0; i < n; i++ {
		h.record(i, int64(latency))
	}
	return h.snapshot()
}

func rampCfg() Config {
	return Config{RampStep: 1000, RampInterval: 30 * time.Second, StopP99Ms: 100, StopDropsPerMin: 5}
}

func TestCeilingReportsLastLevelThatFinishedPassing(t *testing.T) {
	res := &Result{}
	h := newHistogram()
	c := newCeiling(rampCfg(), res, h.snapshot())

	// Level 1 (1000 conns) at 10ms, level 2 (2000) at 40ms: both fine.
	for _, tt := range []struct {
		t       float64
		conns   int
		steps   int64
		latency time.Duration
	}{
		{15, 1000, 0, 10 * time.Millisecond}, {20, 1000, 0, 10 * time.Millisecond}, {25, 1000, 0, 10 * time.Millisecond},
		{35, 2000, 1, 40 * time.Millisecond}, {50, 2000, 1, 40 * time.Millisecond}, {55, 2000, 1, 40 * time.Millisecond},
	} {
		snap := snapWith(h, 1000, tt.latency)
		if c.observe(Sample{T: tt.t, Connected: tt.conns, PromDrops: 0}, snap, tt.steps) {
			t.Fatalf("stopped too early at t=%v", tt.t)
		}
	}
	c.finish()

	if res.Stable == nil || res.Stable.Connections != 2000 {
		t.Errorf("stable = %+v, want the 2000-connection level that ran to the end", res.Stable)
	}
	if len(res.Levels) != 2 {
		t.Errorf("levels = %+v, want 2", res.Levels)
	}
}

func TestCeilingNeverReportsALevelThatPassedEarlyThenFailed(t *testing.T) {
	res := &Result{}
	h := newHistogram()
	c := newCeiling(rampCfg(), res, h.snapshot())

	// Level 1 is healthy all the way through.
	for _, at := range []float64{15, 20, 25} {
		c.observe(Sample{T: at, Connected: 1000}, snapWith(h, 1000, 10*time.Millisecond), 0)
	}
	// Level 2 starts at t=35, looks fine at its first judged sample (t=50), then degrades.
	c.observe(Sample{T: 35, Connected: 2000}, h.snapshot(), 1)
	if c.observe(Sample{T: 50, Connected: 2000}, snapWith(h, 1000, 20*time.Millisecond), 1) {
		t.Fatal("level 2's first judged sample is healthy and must not stop the run")
	}
	if !c.observe(Sample{T: 55, Connected: 2000}, snapWith(h, 5000, 900*time.Millisecond), 1) {
		t.Fatal("level 2 degraded past the p99 bound and must stop the run")
	}
	c.finish()

	if res.StoppedBy != "p99" {
		t.Errorf("StoppedBy = %q, want p99", res.StoppedBy)
	}
	if res.Stable == nil || res.Stable.Connections != 1000 {
		t.Errorf("stable = %+v, want the 1000-connection level, not the one that failed", res.Stable)
	}
}

func TestCeilingJudgesALevelOnItsOwnSamplesOnly(t *testing.T) {
	res := &Result{}
	h := newHistogram()
	c := newCeiling(rampCfg(), res, h.snapshot())

	// A long history of slow deliveries at level 1...
	c.observe(Sample{T: 25, Connected: 1000}, snapWith(h, 100000, 10*time.Millisecond), 0)
	// ...must not leak into level 2's verdict, which has only fast samples.
	c.observe(Sample{T: 35, Connected: 2000}, snapWith(h, 10, 500*time.Millisecond), 1) // level start; slow stragglers land before the level baseline
	snap := snapWith(h, 1000, 5*time.Millisecond)
	if c.observe(Sample{T: 55, Connected: 2000}, snap, 1) {
		t.Errorf("level 2's own p99 is small (levels: %+v)", res.Levels)
	}
}

func TestCeilingStopsOnServerDrops(t *testing.T) {
	res := &Result{}
	h := newHistogram()
	c := newCeiling(rampCfg(), res, h.snapshot())

	if !c.observe(Sample{T: 20, Connected: 1000, PromDrops: 42}, snapWith(h, 100, time.Millisecond), 0) {
		t.Error("drops over the threshold must stop the run")
	}
	if res.StoppedBy != "drops" || res.Stable != nil {
		t.Errorf("StoppedBy=%q stable=%+v", res.StoppedBy, res.Stable)
	}
}

func TestCeilingIsInertWithoutThresholds(t *testing.T) {
	res := &Result{}
	h := newHistogram()
	c := newCeiling(Config{RampStep: 10, RampInterval: time.Second}, res, h.snapshot())
	if c.observe(Sample{T: 10, PromDrops: 1000}, snapWith(h, 10, time.Hour), 3) || len(res.Levels) != 0 {
		t.Error("with no thresholds configured nothing should be judged")
	}
}

func TestCeilingWithoutRampUsesTheTrailingFigures(t *testing.T) {
	res := &Result{}
	h := newHistogram()
	c := newCeiling(Config{StopP99Ms: 100}, res, h.snapshot())

	if c.observe(Sample{T: 5, Connected: 50, P99Ms: 20}, h.snapshot(), 0) || res.Stable == nil || res.Stable.Connections != 50 {
		t.Errorf("a passing sample should be stable at once without a ramp: %+v", res.Stable)
	}
	if !c.observe(Sample{T: 10, Connected: 50, P99Ms: 300}, h.snapshot(), 0) {
		t.Error("a trailing p99 over the bound should stop the run")
	}
}
