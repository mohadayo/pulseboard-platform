package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func resetState() {
	mu.Lock()
	events = nil
	counter = 0
	mu.Unlock()
}

func TestHealthHandler(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "healthy" {
		t.Fatalf("expected healthy, got %s", resp["status"])
	}
	if resp["service"] != "analytics-engine" {
		t.Fatalf("expected analytics-engine, got %s", resp["service"])
	}
}

func TestTrackHandler_Success(t *testing.T) {
	resetState()
	body, _ := json.Marshal(map[string]string{"user_id": "u1", "event_type": "page_view", "payload": "home"})
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/track", bytes.NewReader(body))
	w := httptest.NewRecorder()
	trackHandler(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	var evt Event
	json.NewDecoder(w.Body).Decode(&evt)
	if evt.UserID != "u1" {
		t.Fatalf("expected u1, got %s", evt.UserID)
	}
	if evt.ID == "" {
		t.Fatal("expected non-empty ID")
	}
}

func TestTrackHandler_MissingFields(t *testing.T) {
	resetState()
	body, _ := json.Marshal(map[string]string{"user_id": "u1"})
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/track", bytes.NewReader(body))
	w := httptest.NewRecorder()
	trackHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTrackHandler_InvalidJSON(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/track", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	trackHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestTrackHandler_WrongMethod(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/track", nil)
	w := httptest.NewRecorder()
	trackHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestStatsHandler(t *testing.T) {
	resetState()
	body1, _ := json.Marshal(map[string]string{"user_id": "u1", "event_type": "page_view"})
	body2, _ := json.Marshal(map[string]string{"user_id": "u2", "event_type": "click"})
	body3, _ := json.Marshal(map[string]string{"user_id": "u1", "event_type": "page_view"})

	for _, b := range [][]byte{body1, body2, body3} {
		req := httptest.NewRequest(http.MethodPost, "/api/analytics/track", bytes.NewReader(b))
		w := httptest.NewRecorder()
		trackHandler(w, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/analytics/stats", nil)
	w := httptest.NewRecorder()
	statsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var stats StatsResponse
	json.NewDecoder(w.Body).Decode(&stats)
	if stats.TotalEvents != 3 {
		t.Fatalf("expected 3 events, got %d", stats.TotalEvents)
	}
	if stats.ByType["page_view"] != 2 {
		t.Fatalf("expected 2 page_views, got %d", stats.ByType["page_view"])
	}
	if stats.ByUser["u1"] != 2 {
		t.Fatalf("expected 2 events for u1, got %d", stats.ByUser["u1"])
	}
}

func TestTrackHandler_BodyTooLarge(t *testing.T) {
	resetState()
	original := maxBodyBytes
	maxBodyBytes = 16
	defer func() { maxBodyBytes = original }()

	body, _ := json.Marshal(map[string]string{"user_id": "u1", "event_type": "page_view", "payload": strings.Repeat("x", 1024)})
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/track", bytes.NewReader(body))
	w := httptest.NewRecorder()
	trackHandler(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}

	mu.RLock()
	n := len(events)
	mu.RUnlock()
	if n != 0 {
		t.Fatalf("expected no event stored on oversized body, got %d", n)
	}
}

func TestTrackHandler_EvictsOldEvents(t *testing.T) {
	resetState()
	original := maxEvents
	maxEvents = 3
	defer func() { maxEvents = original }()

	for i := 0; i < 5; i++ {
		body, _ := json.Marshal(map[string]string{"user_id": fmt.Sprintf("u%d", i), "event_type": "page_view"})
		req := httptest.NewRequest(http.MethodPost, "/api/analytics/track", bytes.NewReader(body))
		w := httptest.NewRecorder()
		trackHandler(w, req)
	}

	mu.RLock()
	n := len(events)
	oldest := ""
	if n > 0 {
		oldest = events[0].UserID
	}
	mu.RUnlock()

	if n != 3 {
		t.Fatalf("expected store capped at 3, got %d", n)
	}
	// 古い u0/u1 が破棄され、先頭は u2 になっているはず。
	if oldest != "u2" {
		t.Fatalf("expected oldest retained event to be u2, got %s", oldest)
	}
}

func TestNewServerTimeouts(t *testing.T) {
	srv := newServer(":5002", newRouter())
	if srv.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("expected ReadHeaderTimeout 5s, got %v", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout != 15*time.Second {
		t.Fatalf("expected ReadTimeout 15s, got %v", srv.ReadTimeout)
	}
	if srv.WriteTimeout != 15*time.Second {
		t.Fatalf("expected WriteTimeout 15s, got %v", srv.WriteTimeout)
	}
	if srv.IdleTimeout != 60*time.Second {
		t.Fatalf("expected IdleTimeout 60s, got %v", srv.IdleTimeout)
	}
}

func TestNewRouter(t *testing.T) {
	resetState()
	srv := httptest.NewServer(newRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /health via router, got %d", resp.StatusCode)
	}
}

func TestEventsHandler(t *testing.T) {
	resetState()
	body, _ := json.Marshal(map[string]string{"user_id": "u1", "event_type": "signup"})
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/track", bytes.NewReader(body))
	w := httptest.NewRecorder()
	trackHandler(w, req)

	req = httptest.NewRequest(http.MethodGet, "/api/analytics/events", nil)
	w = httptest.NewRecorder()
	eventsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var evts []Event
	json.NewDecoder(w.Body).Decode(&evts)
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
}
