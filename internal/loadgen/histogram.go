package loadgen

import (
	"math/bits"
	"sync/atomic"
)

// A log-linear histogram of nanosecond latencies: 32 linear sub-buckets per
// power of two, so any reported percentile is within about 3% of the true
// value. Recording is lock-free, which matters at hundreds of thousands of
// deliveries per second, where keeping every sample is not an option.
const (
	subBits    = 5
	subCount   = 1 << subBits
	numBuckets = 64 * subCount
	numShards  = 16
)

type snapshot [numBuckets]uint64

type histogram struct {
	shards [numShards]*[numBuckets]atomic.Uint64
}

func newHistogram() *histogram {
	h := &histogram{}
	for i := range h.shards {
		h.shards[i] = new([numBuckets]atomic.Uint64)
	}
	return h
}

func bucketIndex(v uint64) int {
	if v < subCount {
		return int(v)
	}
	shift := bits.Len64(v) - 1 - subBits
	return (shift+1)<<subBits + int((v>>shift)&(subCount-1))
}

// bucketUpper returns the largest value that falls in bucket idx.
func bucketUpper(idx int) uint64 {
	group, low := idx>>subBits, idx&(subCount-1)
	if group == 0 {
		return uint64(low)
	}
	shift := group - 1
	return (uint64(subCount+low)<<shift + 1<<shift) - 1
}

// record adds one latency sample. shard spreads writers across counters.
func (h *histogram) record(shard int, ns int64) {
	if ns < 0 {
		ns = 0
	}
	h.shards[shard%numShards][bucketIndex(uint64(ns))].Add(1)
}

// snapshot returns the cumulative counts across all shards.
func (h *histogram) snapshot() *snapshot {
	s := new(snapshot)
	for _, sh := range h.shards {
		for i := range sh {
			s[i] += sh[i].Load()
		}
	}
	return s
}

func (s *snapshot) total() uint64 {
	var n uint64
	for _, c := range s {
		n += c
	}
	return n
}

// minus returns the samples recorded between older and s.
func (s *snapshot) minus(older *snapshot) *snapshot {
	d := new(snapshot)
	for i := range s {
		d[i] = s[i] - older[i]
	}
	return d
}

// percentile returns the q-quantile (0..1) in nanoseconds, or 0 if empty.
func (s *snapshot) percentile(q float64) uint64 {
	n := s.total()
	if n == 0 {
		return 0
	}
	rank := uint64(q * float64(n))
	if rank >= n {
		rank = n - 1
	}
	var seen uint64
	for i, c := range s {
		seen += c
		if seen > rank {
			return bucketUpper(i)
		}
	}
	return 0
}
