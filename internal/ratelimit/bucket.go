// Package ratelimit implements per-user token-bucket rate limiting.
package ratelimit

import (
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	DefaultCapacity   = 5.0
	DefaultRefillRate = 2.0 // tokens per second
)

// TokenBucket allows bursts up to capacity and refills at refillRate tokens/sec.
type TokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	capacity   float64
	refillRate float64
	lastRefill time.Time
	now        func() time.Time
}

func NewTokenBucket(capacity, refillRate float64) *TokenBucket {
	return newTokenBucket(capacity, refillRate, time.Now)
}

func newTokenBucket(capacity, refillRate float64, now func() time.Time) *TokenBucket {
	return &TokenBucket{
		tokens:     capacity,
		capacity:   capacity,
		refillRate: refillRate,
		lastRefill: now(),
		now:        now,
	}
}

// Allow consumes one token and reports whether one was available.
func (b *TokenBucket) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	b.lastRefill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Config holds the bucket parameters applied to every user.
type Config struct {
	Capacity   float64
	RefillRate float64
}

// ConfigFromEnv reads RATE_LIMIT_CAPACITY and RATE_LIMIT_REFILL_RATE, falling
// back to the defaults for missing or invalid values.
func ConfigFromEnv() Config {
	return Config{
		Capacity:   envFloat("RATE_LIMIT_CAPACITY", DefaultCapacity),
		RefillRate: envFloat("RATE_LIMIT_REFILL_RATE", DefaultRefillRate),
	}
}

func envFloat(key string, def float64) float64 {
	v, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil || v <= 0 {
		return def
	}
	return v
}
