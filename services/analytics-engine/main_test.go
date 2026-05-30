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

// EventsListResponse は eventsHandler のページネーション付きレスポンス形状。
type EventsListResponse struct {
	Events []Event `json:"events"`
	Count  int     `json:"count"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
	Sort   string  `json:"sort"`
	Order  string  `json:"order"`
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

	var resp EventsListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if resp.Total != 1 || resp.Count != 1 || len(resp.Events) != 1 {
		t.Fatalf("expected total=1 count=1 len=1, got total=%d count=%d len=%d",
			resp.Total, resp.Count, len(resp.Events))
	}
	if resp.Limit != eventsDefaultLimit {
		t.Fatalf("expected default limit=%d, got %d", eventsDefaultLimit, resp.Limit)
	}
	if resp.Offset != 0 {
		t.Fatalf("expected offset=0, got %d", resp.Offset)
	}
}

func TestEventsHandler_PaginationAndFilters(t *testing.T) {
	resetState()
	seed := []map[string]string{
		{"user_id": "u1", "event_type": "page_view"},
		{"user_id": "u2", "event_type": "click"},
		{"user_id": "u1", "event_type": "page_view"},
		{"user_id": "u3", "event_type": "signup"},
		{"user_id": "u1", "event_type": "click"},
	}
	for _, s := range seed {
		body, _ := json.Marshal(s)
		req := httptest.NewRequest(http.MethodPost, "/api/analytics/track", bytes.NewReader(body))
		w := httptest.NewRecorder()
		trackHandler(w, req)
	}

	t.Run("limit and offset", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/analytics/events?limit=2&offset=1", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp EventsListResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode error: %v", err)
		}
		if resp.Total != 5 || resp.Count != 2 || resp.Limit != 2 || resp.Offset != 1 {
			t.Fatalf("unexpected page: %+v", resp)
		}
		if resp.Events[0].UserID != "u2" || resp.Events[1].UserID != "u1" {
			t.Fatalf("unexpected ordering: %+v", resp.Events)
		}
	})

	t.Run("filter by event_type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/analytics/events?event_type=page_view", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp EventsListResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Total != 2 || resp.Count != 2 {
			t.Fatalf("expected 2 page_view events, got total=%d count=%d", resp.Total, resp.Count)
		}
		for _, e := range resp.Events {
			if e.EventType != "page_view" {
				t.Fatalf("expected only page_view, got %s", e.EventType)
			}
		}
	})

	t.Run("filter by user_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/analytics/events?user_id=u1", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		var resp EventsListResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Total != 3 {
			t.Fatalf("expected total=3 for u1, got %d", resp.Total)
		}
		for _, e := range resp.Events {
			if e.UserID != "u1" {
				t.Fatalf("expected only u1, got %s", e.UserID)
			}
		}
	})

	t.Run("combined filters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/analytics/events?user_id=u1&event_type=click", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		var resp EventsListResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Total != 1 || resp.Events[0].UserID != "u1" || resp.Events[0].EventType != "click" {
			t.Fatalf("unexpected: %+v", resp)
		}
	})

	t.Run("offset past total returns empty page", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/analytics/events?offset=100", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		var resp EventsListResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Total != 5 || resp.Count != 0 || len(resp.Events) != 0 {
			t.Fatalf("expected empty page, got %+v", resp)
		}
	})
}

func TestEventsHandler_InvalidPagination(t *testing.T) {
	resetState()
	cases := []struct {
		name string
		url  string
	}{
		{"non-numeric limit", "/api/analytics/events?limit=abc"},
		{"zero limit", "/api/analytics/events?limit=0"},
		{"negative limit", "/api/analytics/events?limit=-1"},
		{"limit over max", fmt.Sprintf("/api/analytics/events?limit=%d", eventsMaxLimit+1)},
		{"non-numeric offset", "/api/analytics/events?offset=abc"},
		{"negative offset", "/api/analytics/events?offset=-1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, c.url, nil)
			w := httptest.NewRecorder()
			eventsHandler(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
			}
		})
	}
}

func TestParseAnalyticsTime(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"rfc3339 z", "2024-01-02T03:04:05Z", false},
		{"rfc3339 offset", "2024-01-02T03:04:05+09:00", false},
		{"rfc3339 nano", "2024-01-02T03:04:05.123456Z", false},
		{"blank", "", true},
		{"whitespace", "   ", true},
		{"not a date", "yesterday", true},
		{"date only", "2024-01-02", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseAnalyticsTime(c.in)
			if c.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", c.in)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", c.in, err)
			}
		})
	}
}

// 内部の events ストアに直接時刻指定でイベントを並べるユーティリティ。
// trackHandler 経由だと Timestamp が time.Now() に固定されるため、since/until の
// 範囲テストを書くには時刻の異なるイベントが必要。
func seedEventsAt(seed []struct {
	ID        string
	UserID    string
	EventType string
	Timestamp string
}) {
	mu.Lock()
	defer mu.Unlock()
	events = nil
	counter = 0
	for _, s := range seed {
		events = append(events, Event{
			ID:        s.ID,
			UserID:    s.UserID,
			EventType: s.EventType,
			Timestamp: s.Timestamp,
		})
		counter++
	}
}

func TestEventsHandler_SinceUntilFilters(t *testing.T) {
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "page_view", "2024-01-01T00:00:00Z"},
		{"evt_2", "u1", "page_view", "2024-01-02T00:00:00Z"},
		{"evt_3", "u1", "page_view", "2024-01-03T00:00:00Z"},
		{"evt_4", "u1", "page_view", "2024-01-04T00:00:00Z"},
	})

	t.Run("since only includes equal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/analytics/events?since=2024-01-02T00:00:00Z", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
		}
		var resp EventsListResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Total != 3 {
			t.Fatalf("expected 3 (Jan 2,3,4), got %d", resp.Total)
		}
	})

	t.Run("until includes equal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/analytics/events?until=2024-01-02T00:00:00Z", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		var resp EventsListResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Total != 2 {
			t.Fatalf("expected 2 (Jan 1,2), got %d", resp.Total)
		}
	})

	t.Run("range", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/analytics/events?since=2024-01-02T00:00:00Z&until=2024-01-03T00:00:00Z", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		var resp EventsListResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Total != 2 {
			t.Fatalf("expected 2 (Jan 2,3), got %d", resp.Total)
		}
	})

	t.Run("invalid since", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/analytics/events?since=not-a-date", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("invalid until", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/analytics/events?until=2024-13-99", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("until before since rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/analytics/events?since=2024-02-01T00:00:00Z&until=2024-01-01T00:00:00Z", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

func TestEventsHandler_SortAndOrder(t *testing.T) {
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_a", "u3", "click", "2024-01-03T00:00:00Z"},
		{"evt_b", "u1", "signup", "2024-01-01T00:00:00Z"},
		{"evt_c", "u2", "page_view", "2024-01-02T00:00:00Z"},
	})

	t.Run("default sort is timestamp asc", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/analytics/events", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		var resp EventsListResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Sort != "timestamp" || resp.Order != "asc" {
			t.Fatalf("expected sort=timestamp order=asc, got sort=%s order=%s",
				resp.Sort, resp.Order)
		}
		if len(resp.Events) != 3 ||
			resp.Events[0].ID != "evt_b" ||
			resp.Events[1].ID != "evt_c" ||
			resp.Events[2].ID != "evt_a" {
			t.Fatalf("unexpected ordering: %+v", resp.Events)
		}
	})

	t.Run("timestamp desc", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/analytics/events?sort=timestamp&order=desc", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		var resp EventsListResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Events[0].ID != "evt_a" ||
			resp.Events[2].ID != "evt_b" {
			t.Fatalf("unexpected desc ordering: %+v", resp.Events)
		}
	})

	t.Run("sort by event_type asc", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/analytics/events?sort=event_type", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		var resp EventsListResponse
		json.NewDecoder(w.Body).Decode(&resp)
		// click < page_view < signup (lexically)
		if resp.Events[0].EventType != "click" ||
			resp.Events[1].EventType != "page_view" ||
			resp.Events[2].EventType != "signup" {
			t.Fatalf("unexpected event_type ordering: %+v", resp.Events)
		}
	})

	t.Run("sort by user_id desc", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/analytics/events?sort=user_id&order=desc", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		var resp EventsListResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.Events[0].UserID != "u3" ||
			resp.Events[2].UserID != "u1" {
			t.Fatalf("unexpected user_id desc: %+v", resp.Events)
		}
	})

	t.Run("invalid sort", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/analytics/events?sort=password", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("invalid order", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/analytics/events?order=random", nil)
		w := httptest.NewRecorder()
		eventsHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}
