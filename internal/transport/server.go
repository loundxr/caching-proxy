package transport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/loundxr/caching-proxy/internal/config"
)

type HTTPServer struct {
	cfg config.Config
	log *slog.Logger
}

func NewHTTPServer(cfg config.Config, log *slog.Logger) *HTTPServer {
	return &HTTPServer{
		cfg: cfg,
		log: log,
	}
}

func (s *HTTPServer) Run(ctx context.Context) error {
	srv := http.Server{
		Addr: fmt.Sprintf(":%d", s.cfg.Port),
		// Handler: proxy.New(),
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
