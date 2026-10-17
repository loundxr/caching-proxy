package transport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	cache "github.com/loundxr/caching-proxy/internal/cache/redis"
	"github.com/loundxr/caching-proxy/internal/config"
	"github.com/loundxr/caching-proxy/internal/proxy"
	"github.com/redis/go-redis/v9"
)

type HTTPServer struct {
	cfg config.Config
	log *slog.Logger
	rdb *redis.Client
	ttl time.Duration
}

type CacheParameters struct {
	rdb    *redis.Client
	ttl    time.Duration
	prefix string
}

func NewHTTPServer(cfg config.Config, rdb *redis.Client, ttl time.Duration, log *slog.Logger) *HTTPServer {
	return &HTTPServer{
		cfg: cfg,
		rdb: rdb,
		ttl: ttl,
		log: log,
	}
}

func (s *HTTPServer) Run(ctx context.Context) error {
	srv := http.Server{
		Addr:    fmt.Sprintf(":%d", s.cfg.Port),
		Handler: proxy.New(s.cfg.Origin, cache.NewRedis(s.rdb), s.ttl, s.log),
	}

	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)

		err := srv.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	s.log.Info(
		"HTTP server started",
		slog.String("addr", srv.Addr),
		slog.String("origin", s.cfg.Origin.String()),
	)

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("listen and server HTTP: %w", err)
		}
	case <-ctx.Done():
		s.log.Warn("shutting down http server...", slog.Int("port", s.cfg.Port))
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			_ = srv.Close()
			return fmt.Errorf("shutdown http server: %w", err)
		}
	}
	return nil
}
