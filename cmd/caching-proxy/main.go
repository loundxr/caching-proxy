package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/loundxr/caching-proxy/internal/config"
	"github.com/loundxr/caching-proxy/internal/transport"
)

func main() {
	cfg, err := config.ParseFlags(os.Args[1:])
	if err != nil {
		fmt.Println("flags: %w", err)
		return
	}

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt, syscall.SIGINT, syscall.SIGTERM,
	)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	srv := transport.NewHTTPServer(cfg, logger)
	if err := srv.Run(ctx); err != nil {
		logger.Error("HTTP server run", slog.String("error", err.Error()))
	}
}
