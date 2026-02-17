package utils

import (
	"math/rand"
	"time"
)

// RateLimiter enforces a minimum delay between operations with optional random jitter.
// Since chromedp has no built-in rate limiting, this is implemented manually
// using a ticker and a random jitter on top of the base delay.
type RateLimiter struct {
	base    time.Duration
	jitter  time.Duration
	last    time.Time
	rng     *rand.Rand
}

// NewRateLimiter creates a RateLimiter with a base delay and a maximum random jitter.
// Each Wait() call will block for at least base duration plus a random amount up to jitter.
func NewRateLimiter(base, jitter time.Duration) *RateLimiter {
	return &RateLimiter{
		base:   base,
		jitter: jitter,
		last:   time.Now().Add(-base), // allow immediate first request
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Wait blocks until the rate limit interval has elapsed since the last call,
// then records the current time as the new last-request timestamp.
func (r *RateLimiter) Wait() {
	jitterMs := time.Duration(0)
	if r.jitter > 0 {
		jitterMs = time.Duration(r.rng.Int63n(int64(r.jitter)))
	}

	total := r.base + jitterMs
	elapsed := time.Since(r.last)

	if elapsed < total {
		time.Sleep(total - elapsed)
	}

	r.last = time.Now()
}

// RandomSleep sleeps for a random duration between minMs and maxMs milliseconds.
// Used between page navigations to simulate human-like behaviour.
func RandomSleep(minMs, maxMs int) {
	if minMs >= maxMs {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}
	ms := minMs + rand.Intn(maxMs-minMs)
	time.Sleep(time.Duration(ms) * time.Millisecond)
}