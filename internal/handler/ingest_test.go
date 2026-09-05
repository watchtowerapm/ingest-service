package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/watchtower/ingest-service/internal/rediswriter"
)

type fakeStore struct {
	projectID string
	authErr   error
	pushErr   error
	pushed    [][]byte
}

func (f *fakeStore) Authorize(ctx context.Context, token string) (string, error) {
	if f.authErr != nil {
		return "", f.authErr
	}
	return f.projectID, nil
}

func (f *fakeStore) Push(ctx context.Context, projectID string, payload []byte) error {
	if f.pushErr != nil {
		return f.pushErr
	}
	f.pushed = append(f.pushed, append([]byte(nil), payload...))
	return nil
}

func TestIngestMissingToken(t *testing.T) {
	h := Ingest(&fakeStore{projectID: "p1"}, 1024)
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", strings.NewReader(`[]`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestIngestUnauthorized(t *testing.T) {
	h := Ingest(&fakeStore{authErr: rediswriter.ErrUnauthorized}, 1024)
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", strings.NewReader(`[]`))
	req.Header.Set("Authorization", "Bearer bad")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestIngestAuthorizeInternalError(t *testing.T) {
	h := Ingest(&fakeStore{authErr: errors.New("redis down")}, 1024)
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", strings.NewReader(`[]`))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestIngestAcceptsJSONArray(t *testing.T) {
	store := &fakeStore{projectID: "env-1"}
	h := Ingest(store, 1024)
	body := `[{"t":"request","v":1}]`
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body=%s", rec.Code, rec.Body.String())
	}
	var resp ingestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Accepted {
		t.Fatal("expected accepted true")
	}
	if len(store.pushed) != 1 || string(store.pushed[0]) != body {
		t.Fatalf("pushed = %q", store.pushed)
	}
}

func TestIngestAcceptsGzip(t *testing.T) {
	store := &fakeStore{projectID: "env-1"}
	h := Ingest(store, 1024)
	raw := []byte(`[{"t":"query"}]`)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", &buf)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if len(store.pushed) != 1 || string(store.pushed[0]) != string(raw) {
		t.Fatalf("pushed = %q", store.pushed)
	}
}

func TestIngestRejectsInvalidJSON(t *testing.T) {
	h := Ingest(&fakeStore{projectID: "p"}, 1024)
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", strings.NewReader(`not-json`))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestIngestRejectsOversizedBody(t *testing.T) {
	h := Ingest(&fakeStore{projectID: "p"}, 8)
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", strings.NewReader(`{"hello":"world"}`))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestIngestPushFailure(t *testing.T) {
	h := Ingest(&fakeStore{projectID: "p", pushErr: errors.New("xadd failed")}, 1024)
	req := httptest.NewRequest(http.MethodPost, "/api/ingest", strings.NewReader(`[]`))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestBearerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer  abc ")
	if got := bearerToken(req); got != "abc" {
		t.Fatalf("bearerToken = %q", got)
	}
}

func TestReadBodyBadGzip(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("nope"))
	req.Header.Set("Content-Encoding", "gzip")
	_, err := readBody(req, 1024)
	if err == nil {
		t.Fatal("expected gzip error")
	}
}

func TestBodyPreviewQuotedTruncates(t *testing.T) {
	quoted, truncated := bodyPreviewQuoted([]byte("abcdefghij"), 4)
	if !truncated {
		t.Fatal("expected truncated")
	}
	if quoted != strconv.Quote("abcd") {
		t.Fatalf("quoted = %s", quoted)
	}
}

func TestHealthDegraded(t *testing.T) {
	h := Health(fakePinger{bufErr: errors.New("down"), cacheErr: nil})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), `"status":"degraded"`) {
		t.Fatalf("body = %s", body)
	}
}

type fakePinger struct {
	bufErr   error
	cacheErr error
}

func (f fakePinger) PingBuffer(ctx context.Context) error { return f.bufErr }
func (f fakePinger) PingCache(ctx context.Context) error  { return f.cacheErr }
