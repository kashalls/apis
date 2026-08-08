package trmnl

import (
	"context"
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/kashalls/apis/internal/mqtt"
)

// StartQueueConsumer subscribes to the mqtt topics PushText/PushImage
// publish to and appends each message onto its redis-backed queue for
// Handlers.PollText/PollImage to serve.
func StartQueueConsumer(mqttClient *mqtt.Client, rdb *redis.Client) error {
	if err := mqttClient.Subscribe(textTopic, 1, enqueueHandler(rdb, textQueueKey)); err != nil {
		return err
	}
	return mqttClient.Subscribe(imageTopic, 1, enqueueHandler(rdb, imageQueueKey))
}

// enqueueHandler appends the already-JSON-encoded mqtt payload straight
// onto the redis list - nextQueued decodes it later, so there's no need
// to decode/re-encode here.
func enqueueHandler(rdb *redis.Client, key string) func([]byte) {
	return func(payload []byte) {
		if err := rdb.RPush(context.Background(), key, payload).Err(); err != nil {
			slog.Warn("enqueue trmnl message", "key", key, "err", err)
		}
	}
}
