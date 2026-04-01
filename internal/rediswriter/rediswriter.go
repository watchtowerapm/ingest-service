package rediswriter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	telemetryStream = "telemetry:events"
	maxStreamLen    = 1_000_000
	tokenKeyPrefix  = "agent_token:"
)

var ErrUnauthorized = errors.New("unauthorized: token not found or expired")

// Writer holds connections to both Redis instances.
type Writer struct {
	buffer *redis.Client
	cache  *redis.Client
}

// New dials both Redis addresses and verifies connectivity with a short ping.
func New(bufferAddr, bufferPass, cacheAddr, cachePass string) (*Writer, error) {
	buffer := redis.NewClient(&redis.Options{
		Addr:         bufferAddr,
		Password:     bufferPass,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	cache := redis.NewClient(&redis.Options{
		Addr:         cacheAddr,
		Password:     cachePass,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := buffer.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	if err := cache.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &Writer{buffer: buffer, cache: cache}, nil
}

// Authorize looks up the ingest token in redis-cache and returns the project ID.
// Returns ErrUnauthorized if the token is missing or expired.
func (w *Writer) Authorize(ctx context.Context, token string) (string, error) {
	projectID, err := w.cache.Get(ctx, tokenKeyPrefix+token).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrUnauthorized
	}
	if err != nil {
		return "", fmt.Errorf("redis cache get: %w", err)
	}
	return projectID, nil
}

// Push appends a telemetry payload to the Redis Stream on the buffer instance,
// tagged with the resolved project ID.
func (w *Writer) Push(ctx context.Context, projectID string, payload []byte) error {
	return w.buffer.XAdd(ctx, &redis.XAddArgs{
		Stream: telemetryStream,
		MaxLen: maxStreamLen,
		Approx: true,
		Values: map[string]any{
			"project_id": projectID,
			"data":       payload,
		},
	}).Err()
}

// PingBuffer returns nil if the buffer Redis is reachable.
func (w *Writer) PingBuffer(ctx context.Context) error {
	return w.buffer.Ping(ctx).Err()
}

// PingCache returns nil if the cache Redis is reachable.
func (w *Writer) PingCache(ctx context.Context) error {
	return w.cache.Ping(ctx).Err()
}

// Close closes both Redis connections.
func (w *Writer) Close() {
	_ = w.buffer.Close()
	_ = w.cache.Close()
}
