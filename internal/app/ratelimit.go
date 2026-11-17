package app

import (
	"sync"
	"time"
)

// rateLimiter allows n events per window per key (e.g. per IP on sign-in).
type rateLimiter struct {
	mu     sync.Mutex
	n      int
	window time.Duration
	hits   map[string][]time.Time
}

func newRateLimiter(n int, window time.Duration) *rateLimiter {
	return &rateLimiter{n: n, window: window, hits: map[string][]time.Time{}}
}

func (r *rateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	cut := now.Add(-r.window)
	h := r.hits[key][:0]
	for _, t := range r.hits[key] {
		if t.After(cut) {
			h = append(h, t)
		}
	}
	if len(h) >= r.n {
		r.hits[key] = h
		return false
	}
	r.hits[key] = append(h, now)
	if len(r.hits) > 10000 {
		r.hits = map[string][]time.Time{}
	}
	return true
}

// Blocked reports whether key has used up its tries, without counting one.
func (r *rateLimiter) Blocked(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cut := time.Now().Add(-r.window)
	n := 0
	for _, t := range r.hits[key] {
		if t.After(cut) {
			n++
		}
	}
	return n >= r.n
}
