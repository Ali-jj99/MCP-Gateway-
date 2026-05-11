// Package ratelimit implements a sliding-window rate limiter backed by in-memory counters.
package ratelimit

import (
	"sync"
	"time"
)

type Limiter struct {
	mu      sync.Mutex
	windows map[string]*window
}

type window struct {
	count     int
	limit     int
	expiresAt time.Time
	duration  time.Duration
}

func NewLimiter() *Limiter {
	return &Limiter{
		windows: make(map[string]*window),
	}
}

func (l *Limiter) Allow(key string, limit int, dur time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	w, ok := l.windows[key]
	if !ok || now.After(w.expiresAt) {
		l.windows[key] = &window{
			count:     1,
			limit:     limit,
			expiresAt: now.Add(dur),
			duration:  dur,
		}
		return true
	}

	if w.count >= w.limit {
		return false
	}
	w.count++
	return true
}

func (l *Limiter) Cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for k, w := range l.windows {
		if now.After(w.expiresAt) {
			delete(l.windows, k)
		}
	}
}
