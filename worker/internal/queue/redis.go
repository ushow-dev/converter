package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	DownloadQueue       = "download_queue"
	ConvertQueue        = "convert_queue"
	RemoteDownloadQueue = "remote_download_queue"
	TransferQueue       = "transfer_queue"

	lockTTL = time.Hour
)

// ErrEmpty is returned by Pop when the BLPOP timeout elapses with no message.
var ErrEmpty = errors.New("queue empty")

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

// Ping checks connectivity.
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Pop blocks for up to timeout waiting for a message on the queue.
// Returns ErrEmpty on timeout.
func (c *Client) Pop(ctx context.Context, queueName string, timeout time.Duration) ([]byte, error) {
	result, err := c.rdb.BLPop(ctx, timeout, queueName).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrEmpty
		}
		return nil, fmt.Errorf("blpop %s: %w", queueName, err)
	}
	// result[0] = key name, result[1] = value
	return []byte(result[1]), nil
}

// Push serialises v as JSON and appends it to the queue (RPUSH).
func (c *Client) Push(ctx context.Context, queueName string, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	return c.rdb.RPush(ctx, queueName, payload).Err()
}

// AcquireLock attempts a distributed lock for jobID (NX, TTL=1h).
// Returns true if the lock was acquired.
func (c *Client) AcquireLock(ctx context.Context, jobID string) (bool, error) {
	key := "job_lock:" + jobID
	ok, err := c.rdb.SetNX(ctx, key, "1", lockTTL).Result()
	return ok, err
}

// ReleaseLock removes the distributed lock for jobID.
func (c *Client) ReleaseLock(ctx context.Context, jobID string) {
	c.rdb.Del(ctx, "job_lock:"+jobID)
}
