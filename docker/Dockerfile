# =============================================================================
# Stage 1: Go module cache (shared by dev and prod)
# =============================================================================
FROM golang:1.23.8-alpine3.21 AS base

RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# =============================================================================
# Stage 2: Development — Air hot-reload
# =============================================================================
FROM base AS dev

RUN go install github.com/air-verse/air@v1.52.3

COPY . .

EXPOSE 3000

CMD ["air", "-c", ".air.toml"]

# =============================================================================
# Stage 3: Build production binary
# =============================================================================
FROM base AS builder

ARG VERSION=dev
ARG BUILD_DATE
ARG VCS_REF

COPY . .

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
      -ldflags="-s -w -X main.version=${VERSION} -X main.buildDate=${BUILD_DATE} -X main.vcsRef=${VCS_REF}" \
      -trimpath \
      -o /ingest \
      ./cmd/server

# =============================================================================
# Stage 4: Production runtime (distroless, no shell)
# =============================================================================
FROM gcr.io/distroless/static-debian12:nonroot AS prod

ARG VERSION=dev
ARG BUILD_DATE
ARG VCS_REF

LABEL org.opencontainers.image.title="ingest-service" \
      org.opencontainers.image.description="Watchtower telemetry ingest service" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.source="https://github.com/watchtowerapm/ingest-service"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /ingest /ingest

EXPOSE 3000

# distroless:nonroot runs as uid 65532 (nonroot) by default — no USER directive needed.
# The binary is owned by root and executed as nonroot (read+execute is sufficient).
# Healthcheck is performed by the container orchestrator (docker-compose / Kubernetes)
# via an HTTP probe to GET /health — distroless has no shell to run a CMD healthcheck.

ENTRYPOINT ["/ingest"]
