package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	DownloadQueue = "download_queue"
	ConvertQueue  = "convert_queue"
)

// Client wraps redis.Client with domain-level queue operations.
type Client struct {
	rdb *redis.Client
}

// New creates a Client from the given Redis URL.
func New(redisURL string) (*Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}
	return &Client{rdb: redis.NewClient(opts)}, nil
}

// Ping checks whether Redis is reachable.
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Enqueue serialises v as JSON and appends it to the named queue (RPUSH).
func (c *Client) Enqueue(ctx context.Context, queue string, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	if err := c.rdb.RPush(ctx, queue, payload).Err(); err != nil {
		return fmt.Errorf("rpush %s: %w", queue, err)
	}
	return nil
}

// SetJobLock acquires a distributed lock for the given jobID (NX, TTL = 1 h).
// Returns true if the lock was acquired, false if it already exists.
func (c *Client) SetJobLock(ctx context.Context, jobID string) (bool, error) {
	key := "job_lock:" + jobID
	ok, err := c.rdb.SetNX(ctx, key, "1", time.Hour).Result()
	return ok, err
}
