package spotify

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// currentPollInterval is how often the background poller refreshes the
// cached currently-playing state.
const currentPollInterval = 1 * time.Second

// redisCurrentChannel is the pub/sub channel a changed playback state is
// published to (see publishIfChanged).
const redisCurrentChannel = "spotify/current"

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

	c.publishIfChanged(ctx, current)
}

// publishIfChanged publishes current to the redis channel and (if
// configured) the mqtt topic, but only when it differs from the last
// published state (see sameTrack) - progress_ms alone changing every
// tick shouldn't trigger a publish.
func (c *Client) publishIfChanged(ctx context.Context, current *Current) {
	c.lastPublishedMu.Lock()
	changed := !sameTrack(c.lastPublished, current)
	if changed {
		c.lastPublished = current
	}
	c.lastPublishedMu.Unlock()
	if !changed {
		return
	}

	payload, err := json.Marshal(current)
	if err != nil {
		slog.Warn("marshal current for publish", "err", err)
		return
	}

	if err := c.redis.Publish(ctx, redisCurrentChannel, payload).Err(); err != nil {
		slog.Warn("publish current to redis", "err", err)
	}

	if c.mqtt != nil {
		if err := c.mqtt.Publish(c.mqttTopic, payload); err != nil {
			slog.Warn("publish current to mqtt", "err", err)
		}
	}
}

// sameTrack reports whether a and b represent the same playback state
// for change-detection purposes, ignoring progress_ms.
func sameTrack(a, b *Current) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Playing == b.Playing && a.ID == b.ID && a.Type == b.Type && devicesEqual(a.Device, b.Device)
}

func devicesEqual(a, b *Device) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
