package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	cache "github.com/loundxr/caching-proxy/internal/cache/redis"
	"github.com/redis/go-redis/v9"
)

const testTTL = time.Minute

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newRedisCache(t *testing.T) (*cache.Redis, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return cache.NewRedis(client), mr
}

func newProxyFor(t *testing.T, origin http.Handler, c cache.Cache) *httptest.Server {
	t.Helper()

	o := httptest.NewServer(origin)
	t.Cleanup(o.Close)

	u, err := url.Parse(o.URL)
	if err != nil {
		t.Fatal(err)
	}

	p := httptest.NewServer(New(u, c, testTTL, newTestLogger()))
	t.Cleanup(p.Close)
	return p
}

func get(t *testing.T, rawURL string) (*http.Response, string) {
	t.Helper()

	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
	}
	return resp, string(body)
}

func TestPassthrough(t *testing.T) {
	var gotURI, gotXFF string
	c, _ := newRedisCache(t)

	p := newProxyFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		gotXFF = r.Header.Get("X-Forwarded-For")
		w.Header().Set("X-Origin", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("test"))
	}), c)
	wantURI := "/products/1?limit=5"

	resp, body := get(t, p.URL+wantURI)

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
	c, _ := newRedisCache(t)

	p := newProxyFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = w.Write(b)
	}), c)

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

func TestMissThenHit(t *testing.T) {
	var calls atomic.Int32

	const xCache = "X-Cache"

	c, _ := newRedisCache(t)
	p := newProxyFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-Origin", "yes")
		_, _ = w.Write([]byte("payload"))
	}), c)

	r1, b1 := get(t, p.URL+"/item?id=1")
	r2, b2 := get(t, p.URL+"/item?id=1")

	if r1.Header.Get(xCache) != "MISS" {
		t.Errorf("response 1 header X-Cache = %q, want MISS", r1.Header.Get(xCache))
	}
	if r2.Header.Get(xCache) != "HIT" {
		t.Errorf("response 2 header header X-Cache = %q, want HIT", r2.Header.Get(xCache))
	}

	if b1 != "payload" || b2 != "payload" {
		t.Errorf("bodies = %q, %q", b1, b2)
	}
	if r2.Header.Get("X-Origin") != "yes" {
		t.Error("X-Origin header lost in cached response")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("origin calls = %d, want 1", n)
	}
}

func TestExpired(t *testing.T) {
	var calls atomic.Int32
	c, mr := newRedisCache(t)
	p := newProxyFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte("x"))
	}), c)

	get(t, p.URL+"/e")
	mr.FastForward(testTTL + time.Second)
	resp, _ := get(t, p.URL+"/e")

	if resp.Header.Get("X-Cache") != "MISS" {
		t.Errorf("X-Cache=%q, want MISS after expiration", resp.Header.Get("X-Cache"))
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("origin calls = %d, want 2", n)
	}
}

func TestNon200NotCached(t *testing.T) {
	var calls atomic.Int32
	c, _ := newRedisCache(t)
	p := newProxyFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.NotFound(w, r)
	}), c)

	r1, _ := get(t, p.URL+"/notfound")
	r2, _ := get(t, p.URL+"/notfound")

	if r1.StatusCode != http.StatusNotFound || r2.StatusCode != http.StatusNotFound {
		t.Errorf("status codes = %d, %d, want 404", r1.StatusCode, r2.StatusCode)
	}
	if r2.Header.Get("X-Cache") != "MISS" {
		t.Errorf("X-Cache=%q, want MISS", r2.Header.Get("X-Cache"))
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("origin calls = %d, want 2", n)
	}
}

func TestOriginDown(t *testing.T) {
	o := httptest.NewServer(http.NotFoundHandler())
	u, _ := url.Parse(o.URL)
	o.Close()

	c, _ := newRedisCache(t)
	p := httptest.NewServer(New(u, c, testTTL, newTestLogger()))
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

type failingCache struct{}

var ErrRedisDown = errors.New("redis down")

func (failingCache) Get(context.Context, string) (*cache.Entry, bool, error) {
	return nil, false, ErrRedisDown
}

func (failingCache) Set(context.Context, string, *cache.Entry, time.Duration) error {
	return ErrRedisDown
}

func (failingCache) Clear(context.Context) error {
	return ErrRedisDown
}

func TestCacheFailureFailsOpen(t *testing.T) {
	p := newProxyFor(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}), failingCache{})

	resp, body := get(t, p.URL+"/failcache")

	if resp.StatusCode != http.StatusOK || body != "ok" {
		t.Errorf("got %d %q, want 200 %q", resp.StatusCode, body, "ok")
	}
	if resp.Header.Get("X-Cache") != "MISS" {
		t.Errorf("X-Cache = %q, want MISS", resp.Header.Get("X-Cache"))
	}
}
