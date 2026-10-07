// Package ratelimit is a single-process window used in front of the figure API.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter allows limit events per minute.
type Limiter struct {
	mu     sync.Mutex
	limit  int
	window []time.Time
}

// New returns a limiter. A non-positive limit denies every call.
func New(limit int) *Limiter {
	return &Limiter{limit: limit}
}

// Allow records one event and reports whether it fits in the current minute.
func (l *Limiter) Allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.limit < 1 {
		return false
	}
	cutoff := now.Add(-time.Minute)
	kept := l.window[:0]
	for _, ts := range l.window {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	l.window = kept
	if len(l.window) >= l.limit {
		return false
	}
	l.window = append(l.window, now)
	return true
}
