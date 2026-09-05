package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/watchtower/ingest-service/internal/config"
	"github.com/watchtower/ingest-service/internal/handler"
	"github.com/watchtower/ingest-service/internal/rediswriter"
)

// Set by Go ldflags at build time.
var (
	version   = "dev"
	buildDate = ""
	vcsRef    = ""
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	port := config.EnvOr("PORT", "3000")
	bufferAddr := config.EnvOr("REDIS_BUFFER_ADDR", "localhost:6379")
	bufferPass := config.EnvOr("REDIS_BUFFER_PASSWORD", "")
	cacheAddr := config.EnvOr("REDIS_CACHE_ADDR", "localhost:6380")
	cachePass := config.EnvOr("REDIS_CACHE_PASSWORD", "")
	maxIngestBody := config.EnvInt64("INGEST_MAX_BODY_BYTES", 10<<20)

	rw, err := rediswriter.New(bufferAddr, bufferPass, cacheAddr, cachePass)
	if err != nil {
		slog.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer rw.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handler.Health(rw))
	mux.HandleFunc("POST /api/ingest", handler.Ingest(rw, maxIngestBody))

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("ingest-service starting",
			"port", port,
			"max_ingest_body_bytes", maxIngestBody,
			"version", version,
			"build_date", buildDate,
			"vcs_ref", vcsRef,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
}
