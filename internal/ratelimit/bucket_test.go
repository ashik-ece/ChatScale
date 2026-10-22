package ratelimit

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *fakeClock { return &fakeClock{t: time.Unix(1_000_000, 0)} }

func TestTokenBucket(t *testing.T) {
	tests := []struct {
		name  string
		steps []struct {
			wait time.Duration
			want bool
		}
	}{
		{
			name: "fresh bucket allows capacity then blocks",
			steps: []struct {
				wait time.Duration
				want bool
			}{{0, true}, {0, true}, {0, true}, {0, true}, {0, true}, {0, false}},
		},
		{
			name: "500ms refills one token",
			steps: []struct {
				wait time.Duration
				want bool
			}{{0, true}, {0, true}, {0, true}, {0, true}, {0, true}, {0, false}, {500 * time.Millisecond, true}, {0, false}},
		},
		{
			name: "refill is capped at capacity",
			steps: []struct {
				wait time.Duration
				want bool
			}{{0, true}, {time.Hour, true}, {0, true}, {0, true}, {0, true}, {0, true}, {0, false}},
		},
		{
			name: "partial refill is not enough",
			steps: []struct {
				wait time.Duration
				want bool
			}{{0, true}, {0, true}, {0, true}, {0, true}, {0, true}, {200 * time.Millisecond, false}, {300 * time.Millisecond, true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clk := newClock()
			b := newTokenBucket(5, 2, clk.now)
			for i, s := range tt.steps {
				clk.advance(s.wait)
				if got := b.Allow(); got != s.want {
					t.Fatalf("step %d: Allow() = %v, want %v", i, got, s.want)
				}
			}
		})
	}
}

func TestStoreBucketsAreIndependent(t *testing.T) {
	clk := newClock()
	s := NewStore(Config{Capacity: 1, RefillRate: 1})
	s.now = clk.now

	if !s.Check("a") || s.Check("a") {
		t.Fatal("user a should get exactly one message")
	}
	if !s.Check("b") {
		t.Fatal("user b should have their own bucket")
	}
}

func TestStoreTimeout(t *testing.T) {
	clk := newClock()
	s := NewStore(Config{Capacity: 5, RefillRate: 2})
	s.now = clk.now

	s.Timeout("a", 10*time.Second)
	if s.Check("a") || !s.IsTimedOut("a") {
		t.Fatal("timed-out user must be rejected")
	}
	if !s.Check("b") {
		t.Fatal("other users must be unaffected")
	}
	clk.advance(11 * time.Second)
	if s.IsTimedOut("a") || !s.Check("a") {
		t.Fatal("timeout should have expired")
	}
}
