package trmnl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const (
	textTopic  = "trmnl/text"
	imageTopic = "trmnl/image"

	textQueueKey  = "trmnl/text/queue"
	imageQueueKey = "trmnl/image/queue"
)

// nextQueued returns the current merge_variables for a poll: it pops
// past a queued item only if another is waiting behind it, so the last
// item is served repeatedly instead of going blank between pushes. It
// returns an empty map if nothing has ever been queued.
func nextQueued(ctx context.Context, rdb *redis.Client, key string) (map[string]any, error) {
	n, err := rdb.LLen(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("check queue length: %w", err)
	}

	var raw string
	switch {
	case n > 1:
		raw, err = rdb.LPop(ctx, key).Result()
	case n == 1:
		raw, err = rdb.LIndex(ctx, key, 0).Result() // peek, don't remove
	default:
		return map[string]any{}, nil
	}
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("read queue: %w", err)
	}
	if raw == "" {
		return map[string]any{}, nil
	}

	var vars map[string]any
	if err := json.Unmarshal([]byte(raw), &vars); err != nil {
		return nil, fmt.Errorf("decode queued item: %w", err)
	}
	return vars, nil
}
