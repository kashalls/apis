// Package redisconn builds the single shared Redis client used across
// services (spotify's OAuth tokens, the github cache, spotify's
// current-track pub/sub channel).
package redisconn

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// New parses url and connects, verifying the connection with a ping
// before returning.
func New(ctx context.Context, url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	rdb := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		return nil, fmt.Errorf("connect to redis: %w", err)
	}
	return rdb, nil
}
