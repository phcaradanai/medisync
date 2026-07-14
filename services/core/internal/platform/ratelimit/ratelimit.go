// Package ratelimit provides an in-memory, per-key rate limiter using a
// fixed-window counter. It is safe for concurrent use.
package ratelimit

import (
	"errors"
	"sync"
	"time"
)

// ErrLimitExceeded is returned when a key has exceeded the configured limit.
var ErrLimitExceeded = errors.New("rate limit exceeded")

// Limiter is a fixed-window, in-memory rate limiter. Keys are arbitrary
// strings; the caller decides what constitutes a key (identifier, IP, etc.).
type Limiter struct {
	mu      sync.Mutex
	max     int
	window  time.Duration
	entries map[string]*entry
}

type entry struct {
	count int
	start time.Time
}

// New creates a Limiter. max is the maximum number of events per window.
// window must be positive. If either is zero/negative, Allow always returns
// true (no limiting).
func New(max int, window time.Duration) *Limiter {
	if max <= 0 || window <= 0 {
		return &Limiter{} // noop limiter
	}
	return &Limiter{
		max:     max,
		window:  window,
		entries: make(map[string]*entry),
	}
}

// Allow reports whether the given key has not yet exceeded the configured
// rate. When it returns false, ErrLimitExceeded can be used as a sentinel.
func (l *Limiter) Allow(key string) bool {
	if l.max <= 0 || l.window <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e, ok := l.entries[key]
	if !ok {
		l.entries[key] = &entry{count: 1, start: now}
		return true
	}

	if now.Sub(e.start) >= l.window {
		// Window expired; reset.
		e.count = 1
		e.start = now
		return true
	}

	if e.count >= l.max {
		return false
	}

	e.count++
	return true
}

// Reset clears all state. Exported for tests.
func (l *Limiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = make(map[string]*entry)
}
