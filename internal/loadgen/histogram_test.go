package loadgen

import (
	"math"
	"math/rand"
	"sort"
	"testing"
)

func TestBucketRoundTrip(t *testing.T) {
	for _, v := range []uint64{0, 1, 31, 32, 33, 1000, 12345, 1e6, 123456789, 1e12} {
		idx := bucketIndex(v)
		upper := bucketUpper(idx)
		if upper < v {
			t.Errorf("value %d maps to bucket %d whose upper bound %d is smaller", v, idx, upper)
		}
		if v >= subCount && float64(upper-v)/float64(v) > 1.0/subCount {
			t.Errorf("value %d: bucket upper bound %d is off by more than 1/%d", v, upper, subCount)
		}
	}
}

func TestPercentilesMatchSortedSamples(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	h := newHistogram()
	samples := make([]int64, 100_000)
	for i := range samples {
		// log-normal-ish spread from microseconds to tens of milliseconds
		samples[i] = int64(math.Exp(rng.NormFloat64()*1.2 + 12))
		h.record(i, samples[i])
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	snap := h.snapshot()

	for _, q := range []float64{0.5, 0.95, 0.99} {
		want := float64(samples[int(q*float64(len(samples)))])
		got := float64(snap.percentile(q))
		if math.Abs(got-want)/want > 0.05 {
			t.Errorf("p%.0f = %.0f, want within 5%% of %.0f", q*100, got, want)
		}
	}
}

func TestWindowedPercentile(t *testing.T) {
	h := newHistogram()
	for i := 0; i < 1000; i++ {
		h.record(i, 1_000_000) // 1ms
	}
	before := h.snapshot()
	for i := 0; i < 1000; i++ {
		h.record(i, 100_000_000) // 100ms
	}
	window := h.snapshot().minus(before)
	if got := window.percentile(0.5); got < 95_000_000 {
		t.Errorf("windowed p50 = %d, want about 100ms", got)
	}
	if got := h.snapshot().percentile(0.25); got > 2_000_000 {
		t.Errorf("cumulative p25 = %d, want about 1ms", got)
	}
}

func TestEmptyAndNegative(t *testing.T) {
	h := newHistogram()
	if h.snapshot().percentile(0.99) != 0 {
		t.Error("empty histogram should report 0")
	}
	h.record(0, -5)
	if h.snapshot().total() != 1 {
		t.Error("negative sample should be clamped, not dropped")
	}
}
