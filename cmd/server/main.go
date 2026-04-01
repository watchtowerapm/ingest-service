package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/watchtower/ingest-service/internal/handler"
	"github.com/watchtower/ingest-service/internal/rediswriter"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	port := envOr("PORT", "3000")
	bufferAddr := envOr("REDIS_BUFFER_ADDR", "localhost:6379")
	bufferPass := envOr("REDIS_BUFFER_PASSWORD", "")
	cacheAddr := envOr("REDIS_CACHE_ADDR", "localhost:6380")
	cachePass := envOr("REDIS_CACHE_PASSWORD", "")
	maxIngestBody := envInt64("INGEST_MAX_BODY_BYTES", 10<<20)

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
		slog.Info("ingest-service starting", "port", port, "max_ingest_body_bytes", maxIngestBody)
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

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
