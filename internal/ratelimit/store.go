package ratelimit

import (
	"sync"
	"time"
)

// Store holds one TokenBucket per user and the set of timed-out users.
type Store struct {
	cfg      Config
	now      func() time.Time
	buckets  sync.Map // userID -> *TokenBucket
	timeouts sync.Map // userID -> time.Time (expiry)
}

func NewStore(cfg Config) *Store {
	return &Store{cfg: cfg, now: time.Now}
}

// Check reports whether userID may send a message now. It creates the user's
// bucket on first use and always returns false while the user is timed out.
func (s *Store) Check(userID string) bool {
	if s.IsTimedOut(userID) {
		return false
	}
	b, ok := s.buckets.Load(userID)
	if !ok {
		b, _ = s.buckets.LoadOrStore(userID, newTokenBucket(s.cfg.Capacity, s.cfg.RefillRate, s.now))
	}
	return b.(*TokenBucket).Allow()
}

// Timeout blocks userID until d has elapsed.
func (s *Store) Timeout(userID string, d time.Duration) {
	s.timeouts.Store(userID, s.now().Add(d))
}

// IsTimedOut reports whether userID is currently timed out, clearing expired entries.
func (s *Store) IsTimedOut(userID string) bool {
	v, ok := s.timeouts.Load(userID)
	if !ok {
		return false
	}
	if s.now().Before(v.(time.Time)) {
		return true
	}
	s.timeouts.Delete(userID)
	return false
}
