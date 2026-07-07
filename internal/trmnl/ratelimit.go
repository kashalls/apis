package trmnl

import (
	"sync"
	"time"
)

// RateLimiter gates actual TRMNL webhook pushes behind a single shared
// interval, since text and image writes share the same TRMNL account
// webhook quota. It's checked after request validation so a malformed
// request never burns the quota.
type RateLimiter struct {
	interval time.Duration

	mu   sync.Mutex
	next time.Time
}

func NewRateLimiter(interval time.Duration) *RateLimiter {
	return &RateLimiter{interval: interval}
}

// Allow reports whether a push may proceed now. If not, it returns the
// duration the caller should wait before retrying.
func (rl *RateLimiter) Allow() (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	if now.Before(rl.next) {
		return false, rl.next.Sub(now)
	}
	rl.next = now.Add(rl.interval)
	return true, 0
}
