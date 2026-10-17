package cache

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestCache(t *testing.T) (*Redis, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedis(client), mr
}

func TestSetGet(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	want := &Entry{
		Status: http.StatusOK,
		Header: http.Header{
			"Content-Type": {"application/json"},
			"Set-Cookie":   {"a=1", "b=2"},
		},
		Body: []byte("test body"),
	}

	if err := c.Set(ctx, "k1", want, time.Minute); err != nil {
		t.Fatal(err)
	}

	got, found, err := c.Get(ctx, "k1")
	if err != nil || !found {
		t.Fatalf("got: found=%v, err=%v", found, err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestGetNotExisting(t *testing.T) {
	c, _ := newTestCache(t)

	e, found, err := c.Get(context.Background(), "key")
	if e != nil || found || err != nil {
		t.Errorf("got e=%+v, found=%v, err=%v; want nil, false, nil", e, found, err)
	}
}

func TestTTL(t *testing.T) {
	c, mr := newTestCache(t)
	ctx := context.Background()

	e := &Entry{Status: 200, Header: http.Header{}, Body: []byte("ttl")}
	if err := c.Set(ctx, "k", e, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if mr.TTL(cachePrefix+"k") <= 0 {
		t.Fatal("TTL wasn't set or set incorrectly on the key")
	}

	mr.FastForward(11 * time.Second)

	if _, found, err := c.Get(ctx, "k"); err != nil || found {
		t.Errorf("expired entry: found=%v, err=%v; want found=false, err=nil", found, err)
	}
}

func TestInvalidTTL(t *testing.T) {
	c, _ := newTestCache(t)
	ctx := context.Background()

	for _, ttl := range []time.Duration{0, -time.Second} {
		err := c.Set(ctx, "k", &Entry{Status: 200}, ttl)
		if !errors.Is(err, ErrInvalidTTL) {
			t.Errorf("ttl=%v: err=%v, want ErrInvalidTTL", ttl, err)
		}
	}
}

func TestClearRemovesOnlyOwnKeys(t *testing.T) {
	c, mr := newTestCache(t)
	ctx := context.Background()
	testKeys := []string{"a", "b", "c"}

	if err := mr.Set("other:key", "value"); err != nil {
		t.Fatal(err)
	}

	for _, k := range testKeys {
		e := &Entry{Status: 200, Header: http.Header{}, Body: []byte(k)}
		if err := c.Set(ctx, k, e, time.Minute); err != nil {
			t.Fatal(err)
		}
	}

	if err := c.Clear(ctx); err != nil {
		t.Fatal(err)
	}

	for _, k := range testKeys {
		if _, found, _ := c.Get(ctx, k); found {
			t.Errorf("key %q present after .Clear() call", k)
		}
	}
	if !mr.Exists("other:key") {
		t.Error(".Clear() removed a key outside the caching proxy prefix")
	}
}

func TestKey(t *testing.T) {
	testUrl := "https://example.com/about?page=1"
	a := Key("GET", testUrl)

	if len(a) != 64 {
		t.Errorf("len=%d, want 64", len(a))
	}
	if a != Key("GET", testUrl) {
		t.Error("key is not determenistic")
	}
	if a == Key("HEAD", testUrl) {
		t.Error("method must affect the key")
	}
	if a == Key("GET", "https://example.com/about?page=2") {
		t.Error("full URL must affect the key")
	}
}
