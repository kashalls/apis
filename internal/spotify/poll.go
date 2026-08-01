package spotify

import (
	"context"
	"log/slog"
	"time"
)

// currentPollInterval is how often the background poller refreshes the
// cached currently-playing state.
const currentPollInterval = 1 * time.Second

// StartCurrentPoller periodically refreshes the client's cached playback
// state (see CachedCurrent), so Handlers.Current can serve it without
// calling the Spotify API on every request. It runs until ctx is canceled.
func (c *Client) StartCurrentPoller(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(currentPollInterval)
		defer ticker.Stop()
		c.pollCurrent(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.pollCurrent(ctx)
			}
		}
	}()
}

// CachedCurrent returns the last state observed by StartCurrentPoller, or
// nil if it hasn't successfully polled yet (e.g. not authorized).
func (c *Client) CachedCurrent() *Current {
	c.currentMu.RLock()
	defer c.currentMu.RUnlock()
	return c.current
}

func (c *Client) pollCurrent(ctx context.Context) {
	authorized, err := c.Authorized(ctx)
	if err != nil || !authorized {
		return
	}

	current, err := c.CurrentPlayback(ctx)
	if err != nil {
		slog.Warn("poll spotify current", "err", err)
		return
	}

	c.currentMu.Lock()
	c.current = current
	c.currentMu.Unlock()
}
