package proxy

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Handler struct {
	origin *url.URL
	client *http.Client
	logger *slog.Logger
}

func New(origin *url.URL, logger *slog.Logger) *Handler {
	return &Handler{
		origin: origin,
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
	// почему создаем именно новый
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, h.targetURL(r), r.Body)
	if err != nil {
		h.logger.Error("build request", slog.String("error", err.Error()))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	outReq.ContentLength = r.ContentLength
	// clone() потому что указатель?
	outReq.Header = r.Header.Clone()
	removeHopByHop(outReq.Header)

	// что здесь делаем и с какой целью
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if prior := outReq.Header.Get("X-Forwarded-For"); prior != "" {
			ip = prior + ", " + ip
		}
		outReq.Header.Set("X-Forwarded-For", ip)
	}

	resp, err := h.client.Do(outReq)
	if err != nil {
		h.logger.Error(
			"origin request fail",
			slog.String("url", outReq.URL.String()),
			"error", err.Error(),
		)
		// почему BadGateway
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	removeHopByHop(resp.Header)
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
