# Caching Proxy

A small caching HTTP proxy written in Go. It forwards requests to an origin server and stores successful `GET` responses in Redis, so repeated requests are served from the cache instead of hitting the origin.

## Features

- Reverse proxy to a single configurable origin.
- Responses are cached in Redis and marked with an `X-Cache: HIT` / `X-Cache: MISS` header.
- `--clear-cache` command to wipe the cache.
- Fail-open behavior: if Redis is down, the proxy keeps working as a plain proxy.
- Graceful shutdown on `SIGINT` / `SIGTERM`.
- Structured logging via `log/slog`.

## Requirements

- [Go 1.26+](https://go.dev/)
- [Docker & Docker Compose](https://www.docker.com/)
- [Task](https://taskfile.dev/)

## Quick start

Start Redis:

   ```bash
   docker compose up -d
   ```
Build the proxy:

   ```bash
   go build -o caching-proxy ./cmd/caching-proxy
   ```
Run:

   ```bash
   ./caching-proxy --port 3000 --origin http://dummyjson.com
   ```

## Usage

```
caching-proxy --port <number> --origin <url>
```

| Flag            | Description                                                         | Default |
| --------------- | ------------------------------------------------------------------- | ------- |
| `--port`        | Port the proxy listens on.                                          | `3000`  |
| `--origin`      | Origin server URL (absolute `http(s)` URL). Required in proxy mode. | –       |
| `--clear-cache` | Delete all cached entries and exit. `--origin` is not required.     | `false` |

### Clearing the cache

```bash
./caching-proxy --clear-cache
```

The command connects to the same Redis instance as the running proxy, so the proxy does not need to be restarted. Only keys created by this project (the `cproxy:` prefix) are removed; other data in Redis is left untouched.

## How it works

1. A request arrives at the proxy.
2. For `GET` requests, the proxy builds a cache key and looks it up in Redis.
3. **HIT**: the stored response is returned immediately with `X-Cache: HIT`. The origin is not contacted.
4. **MISS**: the request is forwarded to the origin. If the origin responds with `200 OK`, the response is stored in Redis with a TTL. The response is returned with `X-Cache: MISS`.
5. All other methods (`POST`, `PUT`, `DELETE`, ...) are forwarded as-is, streamed back without buffering, and never cached. They carry no `X-Cache` header.

### Defaults

The project is intentionally small, so these values are constants in the code rather than flags:

| Setting                 | Value            |
| ----------------------- | ---------------- |
| Redis address           | `localhost:6379` |
| Cache entry TTL         | 1 Hour           |
| Redis key prefix        | `cproxy:`        |
| Redis operation timeout | 200 ms           |
| Origin request timeout  | 20 seconds       |

To change them, edit the constants and rebuild.

## Testing

```bash
go vet ./...
go test -race ./...
```

Unit tests use [miniredis](https://github.com/alicebob/miniredis), an in-memory Redis fake, so no running Redis is needed. They cover the HIT/MISS flow, TTL expiry, non-cacheable responses and methods, the fail-open behavior, and cache clearing.