package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func newTestServer() *Server {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewServer(NewStore(), logger)
}

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", resp["status"])
	}
	if resp["service"] != "analytics" {
		t.Errorf("expected service 'analytics', got %q", resp["service"])
	}
}

func TestHealthMethodNotAllowed(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", w.Code)
	}
}

func TestPostEvent(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	body := `{"event": "click", "short_code": "abc123"}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "received" {
		t.Errorf("expected status 'received', got %q", resp["status"])
	}
}

func TestPostEventURLCreated(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	body := `{"event": "url_created", "short_code": "xyz789"}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}

func TestPostEventInvalidJSON(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestPostEventMissingFields(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	tests := []struct {
		name string
		body string
	}{
		{"missing event", `{"short_code": "abc"}`},
		{"missing short_code", `{"event": "click"}`},
		{"invalid event type", `{"event": "invalid", "short_code": "abc"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", w.Code)
			}
		})
	}
}

func TestPostEventMethodNotAllowed(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", w.Code)
	}
}

func TestGetStatsForCode(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	// Add some events
	events := []string{
		`{"event": "url_created", "short_code": "abc123"}`,
		`{"event": "click", "short_code": "abc123"}`,
		`{"event": "click", "short_code": "abc123"}`,
		`{"event": "click", "short_code": "abc123"}`,
	}
	for _, body := range events {
		req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("failed to post event: status %d", w.Code)
		}
	}

	// Get stats for abc123
	req := httptest.NewRequest(http.MethodGet, "/stats/abc123", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var stats CodeStats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if stats.ShortCode != "abc123" {
		t.Errorf("expected short_code 'abc123', got %q", stats.ShortCode)
	}
	if stats.Clicks != 3 {
		t.Errorf("expected 3 clicks, got %d", stats.Clicks)
	}
	if stats.CreatedAt == "" {
		t.Error("expected non-empty created_at")
	}
	if len(stats.Events) != 4 {
		t.Errorf("expected 4 events, got %d", len(stats.Events))
	}
}

func TestGetStatsUnknownCode(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/stats/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var stats CodeStats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if stats.ShortCode != "nonexistent" {
		t.Errorf("expected short_code 'nonexistent', got %q", stats.ShortCode)
	}
	if stats.Clicks != 0 {
		t.Errorf("expected 0 clicks, got %d", stats.Clicks)
	}
	if len(stats.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(stats.Events))
	}
}

func TestGetAllStats(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	// Add events for two codes
	postEvent := func(body string) {
		req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
	}

	postEvent(`{"event": "url_created", "short_code": "aaa"}`)
	postEvent(`{"event": "click", "short_code": "aaa"}`)
	postEvent(`{"event": "click", "short_code": "aaa"}`)
	postEvent(`{"event": "url_created", "short_code": "bbb"}`)
	postEvent(`{"event": "click", "short_code": "bbb"}`)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	totalURLs := int(result["total_urls"].(float64))
	if totalURLs != 2 {
		t.Errorf("expected total_urls 2, got %d", totalURLs)
	}

	totalClicks := int(result["total_clicks"].(float64))
	if totalClicks != 3 {
		t.Errorf("expected total_clicks 3, got %d", totalClicks)
	}

	codes := result["codes"].([]interface{})
	if len(codes) != 2 {
		t.Errorf("expected 2 codes, got %d", len(codes))
	}
}

func TestGetAllStatsEmpty(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	totalURLs := int(result["total_urls"].(float64))
	if totalURLs != 0 {
		t.Errorf("expected total_urls 0, got %d", totalURLs)
	}

	totalClicks := int(result["total_clicks"].(float64))
	if totalClicks != 0 {
		t.Errorf("expected total_clicks 0, got %d", totalClicks)
	}
}

func TestGetStatsMethodNotAllowed(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	req := httptest.NewRequest(http.MethodPost, "/stats/abc123", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", w.Code)
	}
}

func TestContentTypeIsJSON(t *testing.T) {
	srv := newTestServer()
	mux := srv.Routes()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", ct)
	}
}
