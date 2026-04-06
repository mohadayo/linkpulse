package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Event represents a tracked analytics event.
type Event struct {
	EventType string    `json:"event"`
	ShortCode string    `json:"short_code"`
	Timestamp time.Time `json:"timestamp"`
	Extra     map[string]interface{}
}

// eventJSON is used for unmarshalling incoming event payloads.
type eventJSON struct {
	EventType string `json:"event"`
	ShortCode string `json:"short_code"`
}

// CodeStats holds aggregated statistics for a single short code.
type CodeStats struct {
	ShortCode string  `json:"short_code"`
	Clicks    int     `json:"clicks"`
	CreatedAt string  `json:"created_at"`
	Events    []Event `json:"events"`
}

// Store is a thread-safe in-memory event store.
type Store struct {
	mu     sync.RWMutex
	events map[string][]Event // keyed by short_code
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{
		events: make(map[string][]Event),
	}
}

// AddEvent stores an event for the given short code.
func (s *Store) AddEvent(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events[e.ShortCode] = append(s.events[e.ShortCode], e)
}

// GetCodeStats returns statistics for a single short code.
func (s *Store) GetCodeStats(code string) CodeStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.events[code]
	clicks := 0
	createdAt := ""
	for _, e := range events {
		if e.EventType == "click" {
			clicks++
		}
		if e.EventType == "url_created" && createdAt == "" {
			createdAt = e.Timestamp.UTC().Format(time.RFC3339)
		}
	}

	evCopy := make([]Event, len(events))
	copy(evCopy, events)

	return CodeStats{
		ShortCode: code,
		Clicks:    clicks,
		CreatedAt: createdAt,
		Events:    evCopy,
	}
}

// GetAllStats returns aggregate statistics across all short codes.
func (s *Store) GetAllStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalClicks := 0
	codes := make([]CodeStats, 0, len(s.events))

	for code, events := range s.events {
		clicks := 0
		createdAt := ""
		for _, e := range events {
			if e.EventType == "click" {
				clicks++
			}
			if e.EventType == "url_created" && createdAt == "" {
				createdAt = e.Timestamp.UTC().Format(time.RFC3339)
			}
		}
		totalClicks += clicks

		evCopy := make([]Event, len(events))
		copy(evCopy, events)

		codes = append(codes, CodeStats{
			ShortCode: code,
			Clicks:    clicks,
			CreatedAt: createdAt,
			Events:    evCopy,
		})
	}

	return map[string]interface{}{
		"total_urls":   len(s.events),
		"total_clicks": totalClicks,
		"codes":        codes,
	}
}

// Server holds the HTTP handler dependencies.
type Server struct {
	store  *Store
	logger *slog.Logger
}

// NewServer creates a Server with the given store and logger.
func NewServer(store *Store, logger *slog.Logger) *Server {
	return &Server{store: store, logger: logger}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// HandlePostEvent handles POST /events.
func (s *Server) HandlePostEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var payload eventJSON
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.logger.Error("failed to decode event payload", "error", err)
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if payload.EventType == "" {
		writeError(w, http.StatusBadRequest, "missing required field: event")
		return
	}
	if payload.ShortCode == "" {
		writeError(w, http.StatusBadRequest, "missing required field: short_code")
		return
	}
	if payload.EventType != "click" && payload.EventType != "url_created" {
		writeError(w, http.StatusBadRequest, "event must be 'click' or 'url_created'")
		return
	}

	event := Event{
		EventType: payload.EventType,
		ShortCode: payload.ShortCode,
		Timestamp: time.Now().UTC(),
	}

	s.store.AddEvent(event)
	s.logger.Info("event recorded", "event", event.EventType, "short_code", event.ShortCode)

	writeJSON(w, http.StatusOK, map[string]string{"status": "received"})
}

// HandleGetStats handles GET /stats and GET /stats/{short_code}.
func (s *Server) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract short_code from path: /stats/{short_code}
	path := strings.TrimPrefix(r.URL.Path, "/stats")
	path = strings.TrimPrefix(path, "/")

	if path == "" {
		// Return all stats
		stats := s.store.GetAllStats()
		s.logger.Info("returning all stats")
		writeJSON(w, http.StatusOK, stats)
		return
	}

	// Return stats for a specific short code
	code := path
	stats := s.store.GetCodeStats(code)
	s.logger.Info("returning stats for code", "short_code", code)
	writeJSON(w, http.StatusOK, stats)
}

// HandleHealth handles GET /health.
func (s *Server) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "analytics",
	})
}

// Routes returns an http.ServeMux with all routes registered.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/events", s.HandlePostEvent)
	mux.HandleFunc("/stats/", s.HandleGetStats)
	mux.HandleFunc("/stats", s.HandleGetStats)
	mux.HandleFunc("/health", s.HandleHealth)
	return mux
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	store := NewStore()
	srv := NewServer(store, logger)
	mux := srv.Routes()

	host := getEnv("ANALYTICS_HOST", "0.0.0.0")
	port := getEnv("ANALYTICS_PORT", "8002")
	addr := fmt.Sprintf("%s:%s", host, port)

	logger.Info("starting analytics service", "addr", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
