package loadgen

// ceiling decides, sample by sample, which connection level of a ramping run
// is the last one to stay within the thresholds.
//
// Each ramp step starts a new level and is judged only on samples taken inside
// it: the trailing-minute figures still hold the previous, lighter level just
// after a step and would flatter it. A level is judged from half way through
// its ramp interval, which guarantees a sample lands inside it however the
// ramp and sample tickers drift, and each later sample in the level replaces
// the verdict. A level becomes the stable one only once it finishes without
// failing, so a level that passes early and then fails is never reported.
type ceiling struct {
	cfg     Config
	res     *Result
	ramping bool
	active  bool // any threshold is configured

	levelSnap  *snapshot
	levelStart float64
	lastSteps  int64
	levelIdx   int     // index into res.Levels for the current level, -1 before its first verdict
	passing    *Stable // the current level's verdict while it is passing
}

func newCeiling(cfg Config, res *Result, start *snapshot) *ceiling {
	return &ceiling{
		cfg:       cfg,
		res:       res,
		ramping:   cfg.RampInterval > 0 && cfg.RampStep > 0,
		active:    cfg.StopDropsPerMin > 0 || cfg.StopP99Ms > 0,
		levelSnap: start,
		levelIdx:  -1,
	}
}

// observe feeds one sample, the cumulative latency snapshot at that moment and
// the number of ramp steps taken so far. It reports whether the run should stop.
func (c *ceiling) observe(s Sample, snap *snapshot, steps int64) (stop bool) {
	if !c.active {
		return false
	}

	levelP99, ready := s.P99Ms, true
	if c.ramping {
		if steps != c.lastSteps {
			// The previous level ran to its end without failing.
			if c.passing != nil {
				c.res.Stable, c.passing = c.passing, nil
			}
			c.lastSteps, c.levelSnap, c.levelStart, c.levelIdx = steps, snap, s.T, -1
		}
		levelP99 = ms(snap.minus(c.levelSnap).percentile(0.99))
		ready = s.T-c.levelStart >= c.cfg.RampInterval.Seconds()/2
	}
	if !ready {
		return false
	}

	lvl := Level{Connections: s.Connected, P99Ms: levelP99, RecvPerSec: s.RecvPerSec, PromDrops: s.PromDrops, AtSeconds: s.T}
	if c.levelIdx < 0 {
		c.res.Levels = append(c.res.Levels, lvl)
		c.levelIdx = len(c.res.Levels) - 1
	} else {
		c.res.Levels[c.levelIdx] = lvl
	}

	switch {
	case c.cfg.StopDropsPerMin > 0 && s.PromDrops > c.cfg.StopDropsPerMin:
		c.res.StoppedBy = "drops"
	case c.cfg.StopP99Ms > 0 && levelP99 > c.cfg.StopP99Ms:
		c.res.StoppedBy = "p99"
	}
	if c.res.StoppedBy != "" {
		c.passing = nil // a level that failed, even after passing earlier, is never stable
		return true
	}

	stable := &Stable{Connections: s.Connected, P99Ms: levelP99, AtSeconds: s.T}
	if c.ramping {
		c.passing = stable
	} else {
		c.res.Stable = stable
	}
	return false
}

// finish commits a level that was still passing when the run ended.
func (c *ceiling) finish() {
	if c.passing != nil {
		c.res.Stable = c.passing
	}
}
