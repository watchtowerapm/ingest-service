package handler

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/watchtower/ingest-service/internal/rediswriter"
)

type ingestStore interface {
	Authorize(ctx context.Context, token string) (projectID string, err error)
	Push(ctx context.Context, projectID string, payload []byte) error
}

type ingestResponse struct {
	Accepted  bool   `json:"accepted"`
	Timestamp string `json:"timestamp"`
}

// ErrBodyTooLarge is returned when the decompressed body exceeds maxBodyBytes.
var ErrBodyTooLarge = errors.New("ingest body exceeds configured max size")

// Ingest authenticates the request via redis-cache, then pushes the payload to
// the telemetry Redis Stream tagged with the resolved project ID.
// maxBodyBytes limits the decompressed payload (after gzip); use 0 for default (10 MiB).
func Ingest(rw ingestStore, maxBodyBytes int64) http.HandlerFunc {
	if maxBodyBytes <= 0 {
		maxBodyBytes = 10 << 20
	}
	return func(w http.ResponseWriter, r *http.Request) {
		// --- Auth ---
		token := bearerToken(r)
		if token == "" {
			http.Error(w, `{"message":"missing token"}`, http.StatusUnauthorized)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		projectID, err := rw.Authorize(ctx, token)
		if errors.Is(err, rediswriter.ErrUnauthorized) {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		if err != nil {
			slog.Error("authorize error", "error", err)
			http.Error(w, `{"message":"internal error"}`, http.StatusInternalServerError)
			return
		}

		// --- Decompress ---
		body, err := readBody(r, maxBodyBytes)
		if errors.Is(err, ErrBodyTooLarge) {
			slog.Warn("ingest rejected: body larger than max",
				"project_id", projectID,
				"max_body_bytes", maxBodyBytes,
				"content_encoding", r.Header.Get("Content-Encoding"),
				"content_length", r.Header.Get("Content-Length"),
				"user_agent", r.Header.Get("User-Agent"),
			)
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil {
			slog.Warn("ingest rejected: read/decompress body failed",
				"error", err,
				"project_id", projectID,
				"content_encoding", r.Header.Get("Content-Encoding"),
				"content_length", r.Header.Get("Content-Length"),
				"user_agent", r.Header.Get("User-Agent"),
			)
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if !json.Valid(body) {
			var tmp any
			jerr := json.Unmarshal(body, &tmp)
			preview, previewTrunc := bodyPreviewQuoted(body, 512)
			atLimit := int64(len(body)) == maxBodyBytes
			slog.Warn("ingest rejected: invalid JSON",
				"project_id", projectID,
				"content_type", r.Header.Get("Content-Type"),
				"content_encoding", r.Header.Get("Content-Encoding"),
				"content_length", r.Header.Get("Content-Length"),
				"user_agent", r.Header.Get("User-Agent"),
				"body_bytes", len(body),
				"max_body_bytes", maxBodyBytes,
				"body_at_max_limit", atLimit,
				"body_preview_quoted", preview,
				"body_preview_truncated", previewTrunc,
				"json_error", jerr,
			)
			http.Error(w, "body must be valid JSON to be ingested", http.StatusUnprocessableEntity)
			return
		}

		// --- Buffer ---
		if err := rw.Push(ctx, projectID, body); err != nil {
			slog.Error("failed to push to redis stream", "project_id", projectID, "error", err)
			http.Error(w, `{"message":"failed to enqueue"}`, http.StatusServiceUnavailable)
			return
		}

		slog.Info("ingest accepted",
			"project_id", projectID,
			"bytes", len(body),
			"content_encoding", r.Header.Get("Content-Encoding"),
			"content_type", r.Header.Get("Content-Type"),
		)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(ingestResponse{
			Accepted:  true,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// bodyPreviewQuoted returns a short, log-safe quoted snippet of the body (UTF-8 cleaned).
func bodyPreviewQuoted(b []byte, max int) (quoted string, truncated bool) {
	if len(b) > max {
		b = b[:max]
		truncated = true
	}
	s := strings.ToValidUTF8(string(b), "\uFFFD")
	return strconv.Quote(s), truncated
}

// bearerToken extracts the token from the Authorization header.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// readBody reads and decompresses the request body (handles gzip automatically).
// If the decompressed size exceeds maxBytes, it returns ErrBodyTooLarge.
func readBody(r *http.Request, maxBytes int64) ([]byte, error) {
	reader := io.Reader(r.Body)
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gz.Close() }()
		reader = gz
	}
	defer func() { _ = r.Body.Close() }()
	// Read at most maxBytes+1 so we can tell oversize from exact-size payloads.
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrBodyTooLarge
	}
	return body, nil
}
