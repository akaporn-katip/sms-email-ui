package store

import (
	"sync"
	"time"
)

// RateLimiter is an in-memory sliding-window rate limiter. A disabled limiter
// allows everything, which is the default for local development.
type RateLimiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time
	enabled bool
}

// NewRateLimiter creates a limiter. When enabled is false every call to Allow
// returns true.
func NewRateLimiter(enabled bool) *RateLimiter {
	return &RateLimiter{windows: make(map[string][]time.Time), enabled: enabled}
}

// Allow reports whether another event for key is permitted under the given
// limit and window. The event is recorded when it is allowed.
func (r *RateLimiter) Allow(key string, limit int, window time.Duration, now time.Time) bool {
	if !r.enabled {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := now.Add(-window)
	times := r.windows[key]
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		r.windows[key] = kept
		return false
	}
	r.windows[key] = append(kept, now)
	return true
}

// Enabled reports whether limiting is active.
func (r *RateLimiter) Enabled() bool { return r.enabled }

// SetEnabled toggles limiting at runtime.
func (r *RateLimiter) SetEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = enabled
}

// Reset clears all recorded events.
func (r *RateLimiter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.windows = make(map[string][]time.Time)
}
