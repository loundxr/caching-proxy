package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newProxyFor(t *testing.T, origin http.Handler) *httptest.Server {
	t.Helper()

	o := httptest.NewServer(origin)
	t.Cleanup(o.Close)

	u, err := url.Parse(o.URL)
	if err != nil {
		t.Fatal(err)
	}

	p := httptest.NewServer(New(u, newTestLogger()))
	t.Cleanup(p.Close)
	return p
}

func TestPassthrough(t *testing.T) {
	var gotURI, gotXFF string
	p := newProxyFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		gotXFF = r.Header.Get("X-Forwarded-For")
		w.Header().Set("X-Origin", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("test"))
	}))
	wantURI := "/products/1?limit=5"

	resp, err := http.Get(p.URL + wantURI)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status=%d, want %d", resp.StatusCode, http.StatusCreated)
	}

	if string(body) != "test" {
		t.Errorf("body=%q, want %q", string(body), "test")
	}

	if resp.Header.Get("X-Origin") != "yes" {
		t.Error("X-Origin header wasn't forwarded")
	}

	if gotURI != wantURI {
		t.Errorf("origin got URI %q, want %q", gotURI, wantURI)
	}

	if gotXFF == "" {
		t.Error("X-Forwarded-For wasn't set")
	}
}

func TestPostBodyForwarded(t *testing.T) {
	p := newProxyFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write(b)
	}))

	resp, err := http.Post(p.URL+"/ping", "text/plain", strings.NewReader("ping"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ping" {
		t.Errorf("body=%q, want %q", string(body), "ping")
	}
}

func TestOriginDown(t *testing.T) {
	o := httptest.NewServer(http.NotFoundHandler())
	u, _ := url.Parse(o.URL)
	o.Close()

	p := httptest.NewServer(New(u, newTestLogger()))
	defer p.Close()

	resp, err := http.Get(p.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status=%d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}
