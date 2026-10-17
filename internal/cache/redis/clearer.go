package cache

import (
	"context"
	"time"
)

type Clearer interface {
	Clear(ctx context.Context) error
}

func Clear(ctx context.Context, c Clearer) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return c.Clear(ctx)
}
