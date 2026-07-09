// Package ratelimit provides a simple shared-interval limiter used by any
// handler that needs to gate calls to a rate-limited upstream (TRMNL,
// Home Assistant, ...).
package ratelimit

import (
	"sync"
	"time"
)

// Limiter allows at most one call per interval. It's checked after request
// validation so a malformed request never burns the quota.
type Limiter struct {
	interval time.Duration

	mu   sync.Mutex
	next time.Time
}

func NewLimiter(interval time.Duration) *Limiter {
	return &Limiter{interval: interval}
}

// Allow reports whether a call may proceed now. If not, it returns the
// duration the caller should wait before retrying.
func (l *Limiter) Allow() (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	if now.Before(l.next) {
		return false, l.next.Sub(now)
	}
	l.next = now.Add(l.interval)
	return true, 0
}
