package proxy

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	cache "github.com/loundxr/caching-proxy/internal/cache/redis"
)

const (
	headerXCache = "X-Cache"
	xCacheHit    = "HIT"
	xCacheMiss   = "MISS"
)

type Handler struct {
	origin *url.URL
	client *http.Client
	cache  cache.Cache
	ttl    time.Duration
	logger *slog.Logger
}

func New(origin *url.URL, c cache.Cache, ttl time.Duration, logger *slog.Logger) *Handler {
	return &Handler{
		origin: origin,
		cache:  c,
		ttl:    ttl,
		logger: logger,
		client: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target := h.targetURL(r)
	cacheble := r.Method == http.MethodGet

	var key string
	if cacheble {
		key = cache.Key(r.Method, target)

		e, found, err := h.cache.Get(r.Context(), key)
		if err != nil {
			h.logger.Warn("cache get failed", slog.String("error", err.Error()))
		} else if found {
			h.WriteEntry(w, e, xCacheHit)
			h.logger.Info(
				"proxied",
				slog.String("method", r.Method),
				slog.String("path", r.URL.RequestURI()),
				slog.Int("status", e.Status),
				slog.String("cache", xCacheHit),
			)
		}
	}

	resp, err := h.fetch(r, target)
	if err != nil {
		h.logger.Error(
			"origin request fail",
			slog.String("url", target),
			slog.String("error", err.Error()),
		)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	removeHopByHop(resp.Header)

	if !cacheble {
		copyHeader(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		if _, err := io.Copy(w, resp.Body); err != nil {
			h.logger.Warn("copy response body", slog.String("error", err.Error()))
		}
		h.logger.Info(
			"proxied",
			slog.String("method", r.Method),
			slog.String("path", r.URL.RequestURI()),
			slog.Int("status", resp.StatusCode),
		)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.Error("read origin body", slog.String("url", target), slog.String("error", err.Error()))
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}

	e := &cache.Entry{Status: resp.StatusCode, Header: resp.Header, Body: body}

	if resp.StatusCode == http.StatusOK {
		if err := h.cache.Set(r.Context(), key, e, h.ttl); err != nil {
			h.logger.Warn("cache set failed", slog.String("error", err.Error()))
		}
	}

	h.WriteEntry(w, e, xCacheMiss)
	h.logger.Info(
		"proxied",
		slog.String("method", r.Method),
		slog.String("path", r.URL.RequestURI()),
		slog.Int("status", e.Status),
		slog.String("cache", xCacheMiss),
	)
}

func (h *Handler) fetch(r *http.Request, target string) (*http.Response, error) {
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		return nil, err
	}

	outReq.ContentLength = r.ContentLength
	outReq.Header = r.Header.Clone()
	removeHopByHop(outReq.Header)

	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if prior := outReq.Header.Get("X-Forwarded-For"); prior != "" {
			ip = prior + ", " + ip
		}
		outReq.Header.Set("X-Forwarded-For", ip)
	}

	return h.client.Do(outReq)
}

func (h *Handler) WriteEntry(w http.ResponseWriter, e *cache.Entry, xCacheStatus string) {
	copyHeader(w.Header(), e.Header)
	w.Header().Set(headerXCache, xCacheStatus)
	w.WriteHeader(e.Status)
	if _, err := w.Write(e.Body); err != nil {
		h.logger.Warn("write response body", slog.String("error", err.Error()))
	}
}

func (h *Handler) targetURL(r *http.Request) string {
	u := h.origin.Scheme + "://" +
		h.origin.Host +
		strings.TrimSuffix(h.origin.EscapedPath(), "/") + r.URL.EscapedPath()

	if r.URL.RawQuery != "" {
		u += "?" + r.URL.RawQuery
	}
	return u
}
