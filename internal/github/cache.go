package github

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// cacheTTL is how long a cached value is served before the next request
// for it triggers a live refetch.
const cacheTTL = 30 * time.Minute

// cached checks redis for key; on a miss (or an undecodable cached
// value) it calls fetch, best-effort stores the JSON-encoded result with
// cacheTTL, and returns it.
func cached[T any](ctx context.Context, rdb *redis.Client, key string, fetch func(context.Context) (T, error)) (T, error) {
	var out T
	if raw, err := rdb.Get(ctx, key).Result(); err == nil {
		if json.Unmarshal([]byte(raw), &out) == nil {
			return out, nil
		}
	}

	out, err := fetch(ctx)
	if err != nil {
		return out, err
	}

	if raw, err := json.Marshal(out); err == nil {
		_ = rdb.Set(ctx, key, raw, cacheTTL).Err()
	}
	return out, nil
}
