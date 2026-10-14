package cache

import (
	"context"
	"errors"
	"net/http"
	"time"
)

var (
	ErrInvalidTTL = errors.New("invalid TTL value")
)

type Entry struct {
	Status int
	Header http.Header
	Body   []byte
}

type Cache interface {
	Get(ctx context.Context, key string) (e *Entry, found bool, err error)
	Set(ctx context.Context, key string, e *Entry, ttl time.Duration) error
	Clear(ctx context.Context) error
}
