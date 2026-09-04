# ingest-service

The telemetry ingest service for [Watchtower APM](https://github.com/watchtowerapm). Accepts JSON payloads over HTTP, authenticates them against a Redis token cache, and enqueues them onto a Redis Stream for downstream processing by the worker service.

[![CI](https://github.com/watchtowerapm/ingest-service/actions/workflows/ci.yml/badge.svg?branch=1.x)](https://github.com/watchtowerapm/ingest-service/actions/workflows/ci.yml)
[![Release](https://github.com/watchtowerapm/ingest-service/actions/workflows/release.yml/badge.svg)](https://github.com/watchtowerapm/ingest-service/actions/workflows/release.yml)
[![Go Version](https://img.shields.io/badge/go-1.27-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

---

## How it works

```
SDK / Agent
    │
    │  POST /api/ingest
    │  Authorization: Bearer <token>
    ▼
ingest-service
    ├── auth   → redis-cache (GET agent_token:<token> → project_id)
    ├── decompress → gzip or raw body, max 10 MiB
    ├── validate → must be valid JSON
    └── enqueue → redis-buffer (XADD telemetry:events)
                        │
                        ▼
                  worker-service
```

## API

### `POST /api/ingest`

Accepts a telemetry payload for an authenticated project.

**Request**

| Header | Value |
|---|---|
| `Authorization` | `Bearer <token>` |
| `Content-Type` | `application/json` |
| `Content-Encoding` | `gzip` _(optional)_ |

**Body** — any valid JSON object or array, up to 10 MiB decompressed.

**Responses**

| Status | Meaning |
|---|---|
| `202 Accepted` | Payload enqueued successfully |
| `401 Unauthorized` | Missing or invalid token |
| `413 Request Entity Too Large` | Decompressed body exceeds limit |
| `422 Unprocessable Entity` | Body is not valid JSON |
| `503 Service Unavailable` | Redis buffer unreachable |

**Example**

```bash
curl -X POST https://ingest.example.com/api/ingest \
  -H "Authorization: Bearer <your-token>" \
  -H "Content-Type: application/json" \
  -d '{"event":"page_view","url":"/dashboard"}'
```

**Success response**

```json
{
  "accepted": true,
  "timestamp": "2026-04-01T12:00:00Z"
}
```

---

### `GET /health`

Reports liveness of both Redis dependencies.

```bash
curl http://localhost:3000/health
```

```json
{
  "status": "ok",
  "checks": {
    "redis-buffer": "ok",
    "redis-cache": "ok"
  },
  "timestamp": "2026-04-01T12:00:00Z"
}
```

Returns `200 OK` when all checks pass, `503 Service Unavailable` when any dependency is unreachable.

---

## Configuration

All configuration is via environment variables.

| Variable | Default | Description |
|---|---|---|
| `PORT` | `3000` | HTTP listen port |
| `REDIS_BUFFER_ADDR` | `localhost:6379` | Redis Stream host (buffer) |
| `REDIS_CACHE_ADDR` | `localhost:6380` | Redis token cache host |
| `REDIS_BUFFER_PASSWORD` | _(empty)_ | Redis Stream password |
| `REDIS_CACHE_PASSWORD` | _(empty)_ | Redis token cache password |
| `INGEST_MAX_BODY_BYTES` | `10485760` (10 MiB) | Max decompressed payload size |

---

## Docker

Images are published to GitHub Container Registry for `linux/amd64` and `linux/arm64`. Docker pulls the correct arch automatically.

```bash
# Latest stable release
docker pull ghcr.io/watchtowerapm/ingest-service:v1.0.0

# Always latest 1.x
docker pull ghcr.io/watchtowerapm/ingest-service:1
```

---

## Development

**Prerequisites:** Go 1.27+, Docker, Make.

```bash
# Start all dependencies (redis-buffer, redis-cache) + ingest with hot reload
make watchtower-up        # from the repo root

# Or run the ingest service standalone
make run                  # builds and runs locally
make test                 # run tests with race detector
make lint                 # run golangci-lint
make cover                # open HTML coverage report
```

The dev Docker target uses [Air](https://github.com/air-verse/air) for hot reload — any `.go` file change rebuilds and restarts the binary automatically.

---

## Project structure

```
ingest-service/
├── cmd/server/        # main entrypoint
├── internal/
│   ├── config/        # environment helpers
│   ├── handler/       # HTTP handlers (ingest, health)
│   └── rediswriter/   # Redis auth + stream writer
└── docker/
    └── Dockerfile     # multi-stage: dev (Air) · builder · prod (distroless)
```

---

## Releasing

```bash
git tag v1.2.3
git push origin v1.2.3
```

The [CI workflow](.github/workflows/ci.yml) runs `go vet`, golangci-lint, and `go test -race` on `1.x` and `develop`. Handler tests do not need Redis.

Images for production are built from `1.x` tags, not `develop`.


---

## License

MIT — see [LICENSE](LICENSE).
