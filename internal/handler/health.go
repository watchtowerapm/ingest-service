package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type pinger interface {
	PingBuffer(ctx context.Context) error
	PingCache(ctx context.Context) error
}

type healthResponse struct {
	Status    string            `json:"status"`
	Checks    map[string]string `json:"checks"`
	Timestamp string            `json:"timestamp"`
}

// Health returns an HTTP handler that reports liveness of downstream dependencies.
func Health(rw pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		checks := make(map[string]string, 2)
		overall := "ok"

		if err := rw.PingBuffer(ctx); err != nil {
			checks["redis-buffer"] = "unreachable: " + err.Error()
			overall = "degraded"
		} else {
			checks["redis-buffer"] = "ok"
		}

		if err := rw.PingCache(ctx); err != nil {
			checks["redis-cache"] = "unreachable: " + err.Error()
			overall = "degraded"
		} else {
			checks["redis-cache"] = "ok"
		}

		resp := healthResponse{
			Status:    overall,
			Checks:    checks,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		status := http.StatusOK
		if overall != "ok" {
			status = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}
}
