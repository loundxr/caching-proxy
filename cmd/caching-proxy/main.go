package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	cache "github.com/loundxr/caching-proxy/internal/cache/redis"
	"github.com/loundxr/caching-proxy/internal/config"
	"github.com/loundxr/caching-proxy/internal/transport"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg, err := config.ParseFlags(os.Args[1:])
	if err != nil {
		fmt.Printf("flags: %v", err)
		return
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt, syscall.SIGINT, syscall.SIGTERM,
	)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	rdb := redis.NewClient(&redis.Options{Addr: cache.RedisAddr})
	defer rdb.Close()

	if cfg.ClearCache {
		if err := cache.Clear(ctx, cache.NewRedis(rdb)); err != nil {
			fmt.Printf("clear cache: %v", err)
			os.Exit(1)
		}
		fmt.Println("cache cleared")
		os.Exit(0)
	}

	pingCtx, cancelPing := context.WithTimeout(ctx, 3*time.Second)
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		logger.Warn(
			"redis is unavailable, running without caching...",
			slog.String("addr", cache.RedisAddr),
			slog.String("error", err.Error()),
		)
	}
	cancelPing()

	srv := transport.NewHTTPServer(cfg, rdb, cache.DefaultTTL, logger)
	if err := srv.Run(ctx); err != nil {
		logger.Error("HTTP server run", slog.String("error", err.Error()))
	}
}
