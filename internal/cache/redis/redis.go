package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultOpTimeout = 200 * time.Millisecond
	RedisAddr        = "localhost:6379"
	DefaultTTL       = time.Hour
	cachePrefix      = "cproxy:"
)

type Redis struct {
	client    redis.Cmdable
	prefix    string
	opTimeout time.Duration
}

func NewRedis(client redis.Cmdable) *Redis {
	return &Redis{
		client:    client,
		prefix:    cachePrefix,
		opTimeout: defaultOpTimeout,
	}
}

func (r *Redis) Get(ctx context.Context, key string) (*Entry, bool, error) {
	const op = "cache.redis.Get"

	ctx, cancel := context.WithTimeout(ctx, r.opTimeout)
	defer cancel()

	m, err := r.client.HGetAll(ctx, r.prefix+key).Result()
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", op, err)
	}
	if len(m) == 0 {
		return nil, false, nil
	}

	status, err := strconv.Atoi(m["status"])
	if err != nil {
		return nil, false, fmt.Errorf("%s: corrupt status: %w", op, err)
	}

	e := &Entry{
		Status: status,
		Body:   []byte(m["body"]),
	}
	if err := json.Unmarshal([]byte(m["header"]), &e.Header); err != nil {
		return nil, false, fmt.Errorf("%s: corrupt header: %w", op, err)
	}
	return e, true, nil
}

func (r *Redis) Set(ctx context.Context, key string, e *Entry, ttl time.Duration) error {
	const op = "cache.redis.Set"

	if ttl <= 0 {
		return ErrInvalidTTL
	}

	hdr, err := json.Marshal(e.Header)
	if err != nil {
		return fmt.Errorf("%s: encode header: %w", op, err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.opTimeout)
	defer cancel()

	k := r.prefix + key
	_, err = r.client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.HSet(ctx, k, "status", e.Status, "header", hdr, "body", e.Body)
		p.Expire(ctx, k, ttl)
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s: transaction: %w", op, err)
	}

	return nil
}

func (r *Redis) Clear(ctx context.Context) error {
	const op = "cache.redis.Clear"

	var cursor uint64
	for {
		keys, next, err := r.client.Scan(ctx, cursor, r.prefix+"*", 1_000).Result()
		if err != nil {
			return fmt.Errorf("%s: scan: %w", op, err)
		}

		if len(keys) > 0 {
			if err := r.client.Unlink(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("%s: unlink: %w", op, err)
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}
