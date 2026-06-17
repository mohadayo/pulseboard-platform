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

// seedDeletableEvents は削除テスト用の固定タイムスタンプを持つイベント群を直接挿入する。
func seedDeletableEvents() {
	mu.Lock()
	events = []Event{
		{ID: "evt_1", UserID: "u1", EventType: "click", Timestamp: "2026-01-01T00:00:00Z"},
		{ID: "evt_2", UserID: "u1", EventType: "view", Timestamp: "2026-02-01T00:00:00Z"},
		{ID: "evt_3", UserID: "u2", EventType: "click", Timestamp: "2026-03-01T00:00:00Z"},
		{ID: "evt_4", UserID: "u2", EventType: "view", Timestamp: "2026-04-01T00:00:00Z"},
		{ID: "evt_5", UserID: "u3", EventType: "purchase", Timestamp: "2026-05-01T00:00:00Z"},
	}
	mu.Unlock()
}

func TestDeleteEvents_MissingFiltersReturns400(t *testing.T) {
	resetState()
	seedDeletableEvents()
	req := httptest.NewRequest(http.MethodDelete, "/api/analytics/events", nil)
	w := httptest.NewRecorder()
	eventsHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	// 念のため、何も削除されていないこと
	mu.RLock()
	got := len(events)
	mu.RUnlock()
	if got != 5 {
		t.Fatalf("expected 5 events still present, got %d", got)
	}
}

func TestDeleteEvents_ByUserID(t *testing.T) {
	resetState()
	seedDeletableEvents()
	req := httptest.NewRequest(http.MethodDelete,
		"/api/analytics/events?user_id=u1", nil)
	w := httptest.NewRecorder()
	eventsHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if int(resp["deleted"].(float64)) != 2 {
		t.Fatalf("expected deleted=2, got %v", resp["deleted"])
	}
	if resp["user_id"] != "u1" {
		t.Fatalf("expected echo user_id=u1, got %v", resp["user_id"])
	}
	if resp["event_type"] != nil {
		t.Fatalf("expected event_type=null, got %v", resp["event_type"])
	}
	mu.RLock()
	defer mu.RUnlock()
	if len(events) != 3 {
		t.Fatalf("expected 3 remaining, got %d", len(events))
	}
	for _, e := range events {
		if e.UserID == "u1" {
			t.Fatalf("u1 event still present: %s", e.ID)
		}
	}
}

func TestDeleteEvents_ByEventType(t *testing.T) {
	resetState()
	seedDeletableEvents()
	req := httptest.NewRequest(http.MethodDelete,
		"/api/analytics/events?event_type=click", nil)
	w := httptest.NewRecorder()
	eventsHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if int(resp["deleted"].(float64)) != 2 {
		t.Fatalf("expected deleted=2, got %v", resp["deleted"])
	}
}

func TestDeleteEvents_Before(t *testing.T) {
	resetState()
	seedDeletableEvents()
	req := httptest.NewRequest(http.MethodDelete,
		"/api/analytics/events?before=2026-03-01T00:00:00Z", nil)
	w := httptest.NewRecorder()
	eventsHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	// 2026-03-01 “未満”（厳密 <）なので 1月 / 2月 の 2 件のみ
	if int(resp["deleted"].(float64)) != 2 {
		t.Fatalf("expected deleted=2, got %v", resp["deleted"])
	}
	mu.RLock()
	defer mu.RUnlock()
	if len(events) != 3 {
		t.Fatalf("expected 3 remaining, got %d", len(events))
	}
}

func TestDeleteEvents_CombinedFilters(t *testing.T) {
	resetState()
	seedDeletableEvents()
	// user_id=u2 かつ event_type=click → evt_3 のみ
	req := httptest.NewRequest(http.MethodDelete,
		"/api/analytics/events?user_id=u2&event_type=click", nil)
	w := httptest.NewRecorder()
	eventsHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if int(resp["deleted"].(float64)) != 1 {
		t.Fatalf("expected deleted=1, got %v", resp["deleted"])
	}
	mu.RLock()
	defer mu.RUnlock()
	if len(events) != 4 {
		t.Fatalf("expected 4 remaining, got %d", len(events))
	}
	for _, e := range events {
		if e.ID == "evt_3" {
			t.Fatalf("evt_3 should have been deleted")
		}
	}
}

func TestDeleteEvents_NoMatchReturnsZero(t *testing.T) {
	resetState()
	seedDeletableEvents()
	req := httptest.NewRequest(http.MethodDelete,
		"/api/analytics/events?user_id=nonexistent", nil)
	w := httptest.NewRecorder()
	eventsHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if int(resp["deleted"].(float64)) != 0 {
		t.Fatalf("expected deleted=0, got %v", resp["deleted"])
	}
	mu.RLock()
	defer mu.RUnlock()
	if len(events) != 5 {
		t.Fatalf("expected all 5 still present, got %d", len(events))
	}
}

func TestDeleteEvents_InvalidBeforeReturns400(t *testing.T) {
	resetState()
	seedDeletableEvents()
	req := httptest.NewRequest(http.MethodDelete,
		"/api/analytics/events?before=not-a-date", nil)
	w := httptest.NewRecorder()
	eventsHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp["error"], "before") {
		t.Fatalf("expected error to mention 'before', got %q", resp["error"])
	}
}

func TestDeleteEvents_MethodNotAllowedStillWorks(t *testing.T) {
	// 非 GET/DELETE は引き続き 405
	resetState()
	req := httptest.NewRequest(http.MethodPut, "/api/analytics/events", nil)
	w := httptest.NewRecorder()
	eventsHandler(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// seedEvents は controlled timestamp で events を投入する。
// trackHandler 経由だと time.Now() で上書きされるため、時間範囲テストではこちらを使う。
func seedEvents(es []Event) {
	mu.Lock()
	for i := range es {
		counter++
		es[i].ID = fmt.Sprintf("evt_%d", counter)
		events = append(events, es[i])
	}
	mu.Unlock()
}

func callStats(t *testing.T, query string) (*httptest.ResponseRecorder, StatsResponse) {
	t.Helper()
	url := "/api/analytics/stats"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	statsHandler(w, req)
	var resp StatsResponse
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode stats: %v", err)
		}
	}
	return w, resp
}

func TestStatsHandler_FilterByEventType(t *testing.T) {
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-01T00:00:00Z"},
		{UserID: "u2", EventType: "click", Timestamp: "2026-05-02T00:00:00Z"},
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-03T00:00:00Z"},
	})
	w, stats := callStats(t, "event_type=page_view")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stats.TotalEvents != 2 {
		t.Fatalf("expected 2 page_view events, got %d", stats.TotalEvents)
	}
	if stats.ByType["click"] != 0 {
		t.Fatalf("click should be filtered out, got %d", stats.ByType["click"])
	}
	if stats.LastEventAt != "2026-05-03T00:00:00Z" {
		t.Fatalf("expected last_event_at 2026-05-03T00:00:00Z, got %q", stats.LastEventAt)
	}
}

func TestStatsHandler_FilterByUserID(t *testing.T) {
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-01T00:00:00Z"},
		{UserID: "u2", EventType: "page_view", Timestamp: "2026-05-02T00:00:00Z"},
		{UserID: "u1", EventType: "click", Timestamp: "2026-05-03T00:00:00Z"},
	})
	w, stats := callStats(t, "user_id=u1")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stats.TotalEvents != 2 {
		t.Fatalf("expected 2 events for u1, got %d", stats.TotalEvents)
	}
	if stats.ByUser["u2"] != 0 {
		t.Fatalf("u2 should be filtered out, got %d", stats.ByUser["u2"])
	}
	if stats.ByType["page_view"] != 1 || stats.ByType["click"] != 1 {
		t.Fatalf("unexpected by_type: %+v", stats.ByType)
	}
}

func TestStatsHandler_FilterBySince(t *testing.T) {
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-01-01T00:00:00Z"},
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-01T00:00:00Z"},
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-06-01T00:00:00Z"},
	})
	w, stats := callStats(t, "since=2026-04-01T00:00:00Z")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stats.TotalEvents != 2 {
		t.Fatalf("expected 2 events on/after 2026-04-01, got %d", stats.TotalEvents)
	}
}

func TestStatsHandler_FilterByUntil(t *testing.T) {
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-01-01T00:00:00Z"},
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-01T00:00:00Z"},
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-06-01T00:00:00Z"},
	})
	w, stats := callStats(t, "until=2026-05-01T00:00:00Z")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stats.TotalEvents != 2 {
		t.Fatalf("expected 2 events on/before 2026-05-01, got %d", stats.TotalEvents)
	}
}

func TestStatsHandler_FilterCombined(t *testing.T) {
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-01T00:00:00Z"},
		{UserID: "u1", EventType: "click", Timestamp: "2026-05-02T00:00:00Z"},
		{UserID: "u2", EventType: "page_view", Timestamp: "2026-05-03T00:00:00Z"},
	})
	w, stats := callStats(t, "user_id=u1&event_type=page_view")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stats.TotalEvents != 1 {
		t.Fatalf("expected 1 event matching both filters, got %d", stats.TotalEvents)
	}
	if stats.ByType["click"] != 0 || stats.ByUser["u2"] != 0 {
		t.Fatalf("unexpected aggregates: %+v / %+v", stats.ByType, stats.ByUser)
	}
}

func TestStatsHandler_InvalidSinceReturns400(t *testing.T) {
	resetState()
	w, _ := callStats(t, "since=not-a-date")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bogus since, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if !strings.Contains(resp["error"], "since") {
		t.Fatalf("expected error to mention 'since', got %q", resp["error"])
	}
}

func TestStatsHandler_SinceGreaterThanUntilReturns400(t *testing.T) {
	resetState()
	w, _ := callStats(t, "since=2026-06-01T00:00:00Z&until=2026-05-01T00:00:00Z")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for since > until, got %d", w.Code)
	}
}

func TestStatsHandler_NoFilterReturnsAll(t *testing.T) {
	// 後方互換性: フィルタ未指定時は従来通り全件集計
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-01T00:00:00Z"},
		{UserID: "u2", EventType: "click", Timestamp: "2026-05-02T00:00:00Z"},
	})
	w, stats := callStats(t, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stats.TotalEvents != 2 {
		t.Fatalf("expected 2 total, got %d", stats.TotalEvents)
	}
}

func TestStatsHandler_FirstEventAtTracksEarliest(t *testing.T) {
	// 最古の Timestamp が first_event_at として返り、最新（last_event_at）と一致しないこと。
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-03T00:00:00Z"},
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-01T00:00:00Z"},
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-02T00:00:00Z"},
	})
	w, stats := callStats(t, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stats.FirstEventAt != "2026-05-01T00:00:00Z" {
		t.Fatalf("expected first_event_at 2026-05-01T00:00:00Z, got %q", stats.FirstEventAt)
	}
	if stats.LastEventAt != "2026-05-03T00:00:00Z" {
		t.Fatalf("expected last_event_at 2026-05-03T00:00:00Z, got %q", stats.LastEventAt)
	}
}

func TestStatsHandler_SingleEventFirstEqualsLast(t *testing.T) {
	// 1 件のみのときは first と last が一致する。
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-01T12:34:56Z"},
	})
	w, stats := callStats(t, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stats.FirstEventAt != stats.LastEventAt {
		t.Fatalf("expected first == last for single event, got first=%q last=%q",
			stats.FirstEventAt, stats.LastEventAt)
	}
	if stats.FirstEventAt != "2026-05-01T12:34:56Z" {
		t.Fatalf("unexpected first_event_at %q", stats.FirstEventAt)
	}
}

func TestStatsHandler_FirstEventAtRespectsFilter(t *testing.T) {
	// since/until フィルタ後の集合に対して first_event_at が再計算されること。
	// 範囲外の最古のレコードが first_event_at として漏れないことを確認する。
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-01-01T00:00:00Z"},
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-02T00:00:00Z"},
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-03T00:00:00Z"},
	})
	w, stats := callStats(t, "since=2026-04-01T00:00:00Z")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stats.FirstEventAt != "2026-05-02T00:00:00Z" {
		t.Fatalf("expected first_event_at after since filter, got %q", stats.FirstEventAt)
	}
}

func TestStatsHandler_EmptyResultOmitsTimestamps(t *testing.T) {
	// 該当 0 件のときは first_event_at / last_event_at が omitempty で消える。
	// distinct_users / distinct_event_types は 0 で出る（数値型なので omitempty 対象外）。
	// callStats だと body を消費してしまうため、ここでは直接 statsHandler を呼んで生 body を検査する。
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/stats", nil)
	w := httptest.NewRecorder()
	statsHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "first_event_at") {
		t.Fatalf("expected first_event_at to be omitted on empty result, body=%s", body)
	}
	if strings.Contains(body, "last_event_at") {
		t.Fatalf("expected last_event_at to be omitted on empty result, body=%s", body)
	}
	if !strings.Contains(body, `"distinct_users":0`) {
		t.Fatalf("expected distinct_users=0 in body, got %s", body)
	}
	if !strings.Contains(body, `"distinct_event_types":0`) {
		t.Fatalf("expected distinct_event_types=0 in body, got %s", body)
	}
}

func TestStatsHandler_DistinctCountsMatchByMapKeys(t *testing.T) {
	// distinct_users / distinct_event_types が by_user / by_type のキー数と一致する。
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-01T00:00:00Z"},
		{UserID: "u1", EventType: "click", Timestamp: "2026-05-02T00:00:00Z"},
		{UserID: "u2", EventType: "page_view", Timestamp: "2026-05-03T00:00:00Z"},
		{UserID: "u3", EventType: "signup", Timestamp: "2026-05-04T00:00:00Z"},
	})
	w, stats := callStats(t, "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stats.DistinctUsers != 3 {
		t.Fatalf("expected distinct_users=3, got %d", stats.DistinctUsers)
	}
	if stats.DistinctEventTypes != 3 {
		t.Fatalf("expected distinct_event_types=3, got %d", stats.DistinctEventTypes)
	}
	if stats.DistinctUsers != len(stats.ByUser) {
		t.Fatalf("distinct_users (%d) should equal len(by_user) (%d)",
			stats.DistinctUsers, len(stats.ByUser))
	}
	if stats.DistinctEventTypes != len(stats.ByType) {
		t.Fatalf("distinct_event_types (%d) should equal len(by_type) (%d)",
			stats.DistinctEventTypes, len(stats.ByType))
	}
}

func TestStatsHandler_DistinctCountsRespectFilter(t *testing.T) {
	// フィルタで除外されたユーザーは distinct_users に含めない。
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "page_view", Timestamp: "2026-05-01T00:00:00Z"},
		{UserID: "u2", EventType: "click", Timestamp: "2026-05-02T00:00:00Z"},
		{UserID: "u3", EventType: "page_view", Timestamp: "2026-05-03T00:00:00Z"},
	})
	w, stats := callStats(t, "event_type=page_view")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// u1, u3 だけが page_view を発火している
	if stats.DistinctUsers != 2 {
		t.Fatalf("expected distinct_users=2 after filter, got %d", stats.DistinctUsers)
	}
	if stats.DistinctEventTypes != 1 {
		t.Fatalf("expected distinct_event_types=1 after filter, got %d", stats.DistinctEventTypes)
	}
}

// ---------------------------------------------------------------------------
// GET /api/analytics/events/{id} — 単一イベント取得
// ---------------------------------------------------------------------------

func TestGetEventByID_Success(t *testing.T) {
	resetState()
	// track 経由で投入すると ID/Timestamp が割り当てられる
	body, _ := json.Marshal(map[string]string{
		"user_id":    "u1",
		"event_type": "signup",
		"payload":    "free_trial",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/track", bytes.NewReader(body))
	w := httptest.NewRecorder()
	trackHandler(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed failed: %d", w.Code)
	}
	var created Event
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}

	srv := httptest.NewServer(newRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/analytics/events/" + created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var got Event
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("expected id=%s, got %s", created.ID, got.ID)
	}
	if got.EventType != "signup" || got.UserID != "u1" || got.Payload != "free_trial" {
		t.Fatalf("event fields mismatch: %+v", got)
	}
	if got.Timestamp == "" {
		t.Fatalf("timestamp must be preserved")
	}
}

func TestGetEventByID_NotFound(t *testing.T) {
	resetState()
	srv := httptest.NewServer(newRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/analytics/events/evt_does_not_exist")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(body["error"], "evt_does_not_exist") {
		t.Fatalf("error should mention id, got: %v", body)
	}
}

func TestGetEventByID_MethodNotAllowed(t *testing.T) {
	resetState()
	seedEvents([]Event{{ID: "evt_1", UserID: "u1", EventType: "click", Timestamp: "2026-05-01T00:00:00Z"}})
	srv := httptest.NewServer(newRouter())
	defer srv.Close()

	// router 経由で POST すると、Go 1.22 の "GET ..." パターン非マッチで 405 になる
	resp, err := http.Post(
		srv.URL+"/api/analytics/events/evt_1",
		"application/json",
		strings.NewReader(`{}`),
	)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestGetEventByID_DoesNotCollideWithListRoute(t *testing.T) {
	// `/api/analytics/events`（一覧）と `/api/analytics/events/{id}`（単発）は
	// ルータレベルで別パターンとして登録されており、互いに干渉しないこと。
	resetState()
	body, _ := json.Marshal(map[string]string{"user_id": "u1", "event_type": "click"})
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/track", bytes.NewReader(body))
	w := httptest.NewRecorder()
	trackHandler(w, req)
	var created Event
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	srv := httptest.NewServer(newRouter())
	defer srv.Close()

	// 一覧側は配列 (events) を含む JSON を返す
	listResp, err := http.Get(srv.URL + "/api/analytics/events")
	if err != nil {
		t.Fatalf("get list: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from list, got %d", listResp.StatusCode)
	}
	var listBody map[string]interface{}
	if err := json.NewDecoder(listResp.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if _, ok := listBody["events"]; !ok {
		t.Fatalf("list response must contain 'events' field, got: %v", listBody)
	}

	// 単発側は Event 形状（events 配列を持たない）
	detailResp, err := http.Get(srv.URL + "/api/analytics/events/" + created.ID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	defer detailResp.Body.Close()
	if detailResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from detail, got %d", detailResp.StatusCode)
	}
	var detailBody map[string]interface{}
	if err := json.NewDecoder(detailResp.Body).Decode(&detailBody); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if _, ok := detailBody["events"]; ok {
		t.Fatalf("detail response must NOT contain 'events' field, got: %v", detailBody)
	}
	if detailBody["id"] != created.ID {
		t.Fatalf("detail id mismatch: %v", detailBody)
	}
}

func TestGetEventByID_DirectHandlerWrongMethod(t *testing.T) {
	// ルータを通さず直接 getEventByIDHandler を非 GET で叩いた場合の 405 挙動を確認する。
	// 拡張ルーティングのメソッドゲートに頼らない明示的な防御を回帰する。
	resetState()
	req := httptest.NewRequest(http.MethodDelete, "/api/analytics/events/evt_1", nil)
	req.SetPathValue("id", "evt_1")
	w := httptest.NewRecorder()
	getEventByIDHandler(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestGetEventByID_BlankIDReturns404(t *testing.T) {
	// 通常のルータ経由では `{id}` セグメントが空にはならないが、
	// 直接ハンドラを呼ぶテストでブランク id の 400 ガードを回帰する。
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events/", nil)
	req.SetPathValue("id", "   ")
	w := httptest.NewRecorder()
	getEventByIDHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for blank id, got %d", w.Code)
	}
}

// === DELETE /api/analytics/events/{id} ===

func TestDeleteEventByID_Success(t *testing.T) {
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "click", Timestamp: "2026-05-01T00:00:00Z"},
		{UserID: "u2", EventType: "click", Timestamp: "2026-05-02T00:00:00Z"},
		{UserID: "u3", EventType: "click", Timestamp: "2026-05-03T00:00:00Z"},
	})

	srv := httptest.NewServer(newRouter())
	defer srv.Close()

	// 真ん中の evt_2 を id 指定で削除
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/analytics/events/evt_2", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["deleted"].(float64) != 1 {
		t.Fatalf("expected deleted=1, got %v", body["deleted"])
	}
	if body["id"].(string) != "evt_2" {
		t.Fatalf("expected id=evt_2, got %v", body["id"])
	}
	if body["event"] == nil {
		t.Fatalf("expected event in response, got nil")
	}
	eventMap := body["event"].(map[string]interface{})
	if eventMap["user_id"].(string) != "u2" {
		t.Fatalf("expected user_id=u2 in returned event, got %v", eventMap["user_id"])
	}

	// 残った 2 件と順序が保たれていること
	mu.RLock()
	remaining := make([]string, len(events))
	for i, e := range events {
		remaining[i] = e.ID
	}
	mu.RUnlock()
	if len(remaining) != 2 || remaining[0] != "evt_1" || remaining[1] != "evt_3" {
		t.Fatalf("expected [evt_1 evt_3], got %v", remaining)
	}
}

func TestDeleteEventByID_NotFound(t *testing.T) {
	resetState()
	seedEvents([]Event{{UserID: "u1", EventType: "click", Timestamp: "2026-05-01T00:00:00Z"}})

	srv := httptest.NewServer(newRouter())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/analytics/events/evt_does_not_exist", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(body["error"], "evt_does_not_exist") {
		t.Fatalf("error should mention id, got: %v", body)
	}

	// 既存イベントは削除されていないこと
	mu.RLock()
	count := len(events)
	mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected 1 remaining, got %d", count)
	}
}

func TestDeleteEventByID_BlankIDReturns400(t *testing.T) {
	// 通常のルータ経由では `{id}` 空にはならないが、直接ハンドラを呼ぶテストで
	// ブランク id の 400 ガードを回帰する。
	resetState()
	req := httptest.NewRequest(http.MethodDelete, "/api/analytics/events/", nil)
	req.SetPathValue("id", "   ")
	w := httptest.NewRecorder()
	deleteEventByIDHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for blank id, got %d", w.Code)
	}
}

func TestDeleteEventByID_DoesNotAffectGetSameID(t *testing.T) {
	// 同じ path だが GET と DELETE は別ハンドラ。順序: DELETE 前は GET 成功、
	// DELETE 後は GET が 404 になる。
	resetState()
	seedEvents([]Event{{UserID: "u1", EventType: "click", Timestamp: "2026-05-01T00:00:00Z"}})

	srv := httptest.NewServer(newRouter())
	defer srv.Close()

	// 削除前: GET 成功
	getResp, err := http.Get(srv.URL + "/api/analytics/events/evt_1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("pre-delete GET expected 200, got %d", getResp.StatusCode)
	}

	// 削除
	delReq, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/analytics/events/evt_1", nil)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete expected 200, got %d", delResp.StatusCode)
	}

	// 削除後: GET 404
	getResp2, err := http.Get(srv.URL + "/api/analytics/events/evt_1")
	if err != nil {
		t.Fatalf("get post-delete: %v", err)
	}
	getResp2.Body.Close()
	if getResp2.StatusCode != http.StatusNotFound {
		t.Fatalf("post-delete GET expected 404, got %d", getResp2.StatusCode)
	}
}

func TestDeleteEventByID_FilterDeleteRouteUnchanged(t *testing.T) {
	// DELETE /api/analytics/events?user_id=... (フィルタベース) と
	// DELETE /api/analytics/events/{id} (単発) は別 path なので衝突しない。
	// フィルタベース側が依然として動作することを回帰する。
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "click", Timestamp: "2026-05-01T00:00:00Z"},
		{UserID: "u2", EventType: "click", Timestamp: "2026-05-02T00:00:00Z"},
	})

	srv := httptest.NewServer(newRouter())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/analytics/events?user_id=u1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete by filter: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["deleted"].(float64) != 1 {
		t.Fatalf("expected deleted=1 by filter, got %v", body["deleted"])
	}
}

func TestDeleteEventByID_MethodNotAllowed(t *testing.T) {
	// PUT などは GET / DELETE のどちらにもマッチせず 405 が返る。
	resetState()
	seedEvents([]Event{{UserID: "u1", EventType: "click", Timestamp: "2026-05-01T00:00:00Z"}})

	srv := httptest.NewServer(newRouter())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/analytics/events/evt_1", strings.NewReader(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestEventTypesHandler_EmptyStore(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/event_types", nil)
	w := httptest.NewRecorder()
	eventTypesHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if total, ok := resp["total"].(float64); !ok || total != 0 {
		t.Fatalf("expected total=0, got %v", resp["total"])
	}
	types, _ := resp["event_types"].([]interface{})
	if len(types) != 0 {
		t.Fatalf("expected empty event_types, got %d items", len(types))
	}
}

func TestEventTypesHandler_AggregatesByType(t *testing.T) {
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "signup", "2026-06-01T00:00:00Z"},
		{"evt_2", "u2", "signup", "2026-06-02T00:00:00Z"},
		{"evt_3", "u2", "signup", "2026-06-03T00:00:00Z"},
		{"evt_4", "u1", "click", "2026-06-04T00:00:00Z"},
		{"evt_5", "u3", "click", "2026-06-05T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/event_types", nil)
	w := httptest.NewRecorder()
	eventTypesHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		EventTypes []EventTypeAggregate `json:"event_types"`
		Total      int                  `json:"total"`
		Count      int                  `json:"count"`
		Sort       string               `json:"sort"`
		Order      string               `json:"order"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 || resp.Count != 2 {
		t.Fatalf("expected total=2 count=2, got total=%d count=%d", resp.Total, resp.Count)
	}
	if resp.Sort != "event_type" || resp.Order != "asc" {
		t.Fatalf("expected default sort/order, got %s/%s", resp.Sort, resp.Order)
	}
	// 既定では event_type 昇順 — "click", "signup"
	if resp.EventTypes[0].EventType != "click" {
		t.Fatalf("expected first=click, got %s", resp.EventTypes[0].EventType)
	}
	clickAgg := resp.EventTypes[0]
	if clickAgg.EventCount != 2 || clickAgg.DistinctUsers != 2 {
		t.Fatalf("click: expected count=2 distinct=2, got count=%d distinct=%d", clickAgg.EventCount, clickAgg.DistinctUsers)
	}
	if clickAgg.FirstEventAt != "2026-06-04T00:00:00Z" || clickAgg.LastEventAt != "2026-06-05T00:00:00Z" {
		t.Fatalf("click: first/last mismatch: %s / %s", clickAgg.FirstEventAt, clickAgg.LastEventAt)
	}
	signupAgg := resp.EventTypes[1]
	if signupAgg.EventCount != 3 || signupAgg.DistinctUsers != 2 {
		t.Fatalf("signup: expected count=3 distinct=2, got count=%d distinct=%d", signupAgg.EventCount, signupAgg.DistinctUsers)
	}
	if signupAgg.FirstEventAt != "2026-06-01T00:00:00Z" || signupAgg.LastEventAt != "2026-06-03T00:00:00Z" {
		t.Fatalf("signup: first/last mismatch: %s / %s", signupAgg.FirstEventAt, signupAgg.LastEventAt)
	}
}

func TestEventTypesHandler_SortByEventCountDesc(t *testing.T) {
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "click", "2026-06-01T00:00:00Z"},
		{"evt_2", "u2", "click", "2026-06-02T00:00:00Z"},
		{"evt_3", "u3", "click", "2026-06-03T00:00:00Z"},
		{"evt_4", "u1", "signup", "2026-06-04T00:00:00Z"},
		{"evt_5", "u2", "purchase", "2026-06-05T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/event_types?sort=event_count&order=desc", nil)
	w := httptest.NewRecorder()
	eventTypesHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		EventTypes []EventTypeAggregate `json:"event_types"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.EventTypes[0].EventType != "click" || resp.EventTypes[0].EventCount != 3 {
		t.Fatalf("expected click first with count=3, got %+v", resp.EventTypes[0])
	}
	// purchase / signup は count=1 で同点 → 同点時は event_type 昇順タイブレーカー
	if resp.EventTypes[1].EventType != "purchase" || resp.EventTypes[2].EventType != "signup" {
		t.Fatalf("tiebreaker should keep event_type ascending: got %s, %s",
			resp.EventTypes[1].EventType, resp.EventTypes[2].EventType)
	}
}

func TestEventTypesHandler_SinceUntilFilter(t *testing.T) {
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "signup", "2026-06-01T00:00:00Z"},
		{"evt_2", "u2", "signup", "2026-06-05T00:00:00Z"},
		{"evt_3", "u3", "signup", "2026-06-10T00:00:00Z"},
		{"evt_4", "u1", "click", "2026-06-15T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet,
		"/api/analytics/event_types?since=2026-06-03T00:00:00Z&until=2026-06-12T00:00:00Z", nil)
	w := httptest.NewRecorder()
	eventTypesHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		EventTypes []EventTypeAggregate `json:"event_types"`
		Total      int                  `json:"total"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	// ウィンドウ内 signup の 2 件のみ
	if resp.Total != 1 {
		t.Fatalf("expected total=1 (only signup in window), got %d", resp.Total)
	}
	if resp.EventTypes[0].EventType != "signup" || resp.EventTypes[0].EventCount != 2 {
		t.Fatalf("expected signup count=2, got %+v", resp.EventTypes[0])
	}
}

func TestEventTypesHandler_Pagination(t *testing.T) {
	seed := []struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{}
	for i, et := range []string{"a", "b", "c", "d", "e"} {
		seed = append(seed, struct {
			ID        string
			UserID    string
			EventType string
			Timestamp string
		}{
			ID:        fmt.Sprintf("evt_%d", i+1),
			UserID:    "u1",
			EventType: et,
			Timestamp: fmt.Sprintf("2026-06-%02dT00:00:00Z", i+1),
		})
	}
	seedEventsAt(seed)
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/event_types?limit=2&offset=1", nil)
	w := httptest.NewRecorder()
	eventTypesHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		EventTypes []EventTypeAggregate `json:"event_types"`
		Total      int                  `json:"total"`
		Count      int                  `json:"count"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 5 || resp.Count != 2 {
		t.Fatalf("expected total=5 count=2, got total=%d count=%d", resp.Total, resp.Count)
	}
	// event_type 昇順 → a, b, c, d, e → offset=1 limit=2 で b, c
	if resp.EventTypes[0].EventType != "b" || resp.EventTypes[1].EventType != "c" {
		t.Fatalf("expected b,c got %s,%s", resp.EventTypes[0].EventType, resp.EventTypes[1].EventType)
	}
}

func TestEventTypesHandler_MethodNotAllowed(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/event_types", nil)
	w := httptest.NewRecorder()
	eventTypesHandler(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestEventTypesHandler_InvalidSortField(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/event_types?sort=garbage", nil)
	w := httptest.NewRecorder()
	eventTypesHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestEventTypesHandler_InvalidSinceFormat(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/event_types?since=not-a-date", nil)
	w := httptest.NewRecorder()
	eventTypesHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestEventTypesHandler_SinceGreaterThanUntil(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet,
		"/api/analytics/event_types?since=2026-06-10T00:00:00Z&until=2026-06-01T00:00:00Z", nil)
	w := httptest.NewRecorder()
	eventTypesHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestEventTypesHandler_RegisteredOnRouter(t *testing.T) {
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "click", Timestamp: "2026-06-01T00:00:00Z"},
	})
	srv := httptest.NewServer(newRouter())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/analytics/event_types")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---- /api/analytics/users ----

func TestUsersHandler_EmptyStore(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/users", nil)
	w := httptest.NewRecorder()
	usersHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if total, ok := resp["total"].(float64); !ok || total != 0 {
		t.Fatalf("expected total=0, got %v", resp["total"])
	}
	users, _ := resp["users"].([]interface{})
	if len(users) != 0 {
		t.Fatalf("expected empty users, got %d items", len(users))
	}
	if sort, _ := resp["sort"].(string); sort != "user_id" {
		t.Fatalf("expected default sort=user_id, got %v", resp["sort"])
	}
}

func TestUsersHandler_AggregatesByUserID(t *testing.T) {
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "signup", "2026-06-01T00:00:00Z"},
		{"evt_2", "u1", "click", "2026-06-02T00:00:00Z"},
		{"evt_3", "u2", "signup", "2026-06-03T00:00:00Z"},
		{"evt_4", "u2", "signup", "2026-06-04T00:00:00Z"},
		{"evt_5", "u1", "click", "2026-06-05T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/users", nil)
	w := httptest.NewRecorder()
	usersHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Users []UserAggregate `json:"users"`
		Total int             `json:"total"`
		Count int             `json:"count"`
		Sort  string          `json:"sort"`
		Order string          `json:"order"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 || resp.Count != 2 {
		t.Fatalf("expected total=2 count=2, got total=%d count=%d", resp.Total, resp.Count)
	}
	if resp.Sort != "user_id" || resp.Order != "asc" {
		t.Fatalf("expected default sort=user_id order=asc, got %s/%s", resp.Sort, resp.Order)
	}
	u1 := resp.Users[0]
	if u1.UserID != "u1" || u1.EventCount != 3 || u1.DistinctEventTypes != 2 {
		t.Fatalf("u1 mismatch: %+v", u1)
	}
	if u1.FirstEventAt != "2026-06-01T00:00:00Z" || u1.LastEventAt != "2026-06-05T00:00:00Z" {
		t.Fatalf("u1 first/last mismatch: %s / %s", u1.FirstEventAt, u1.LastEventAt)
	}
	u2 := resp.Users[1]
	if u2.UserID != "u2" || u2.EventCount != 2 || u2.DistinctEventTypes != 1 {
		t.Fatalf("u2 mismatch: %+v", u2)
	}
}

func TestUsersHandler_SortByEventCountDesc(t *testing.T) {
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "click", "2026-06-01T00:00:00Z"},
		{"evt_2", "u2", "click", "2026-06-02T00:00:00Z"},
		{"evt_3", "u3", "click", "2026-06-03T00:00:00Z"},
		{"evt_4", "u3", "signup", "2026-06-04T00:00:00Z"},
		{"evt_5", "u3", "purchase", "2026-06-05T00:00:00Z"},
		{"evt_6", "u2", "signup", "2026-06-06T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/users?sort=event_count&order=desc", nil)
	w := httptest.NewRecorder()
	usersHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Users []UserAggregate `json:"users"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Users[0].UserID != "u3" {
		t.Fatalf("expected u3 first (3 events), got %s", resp.Users[0].UserID)
	}
	// 同 event_count (u2=2 / u1=1) では event_count 同値タイブレーカで user_id 昇順だが、
	// u2=2 / u1=1 は同値ではないので u2 → u1 の順になる。
	if resp.Users[1].UserID != "u2" || resp.Users[2].UserID != "u1" {
		t.Fatalf("unexpected order: %s, %s", resp.Users[1].UserID, resp.Users[2].UserID)
	}
}

func TestUsersHandler_TieBreakerIsUserIDAsc(t *testing.T) {
	// 同 event_count なら user_id 昇順がタイブレーカー（reverse 指定でも変わらない）。
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "uB", "click", "2026-06-01T00:00:00Z"},
		{"evt_2", "uA", "click", "2026-06-02T00:00:00Z"},
		{"evt_3", "uC", "click", "2026-06-03T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/users?sort=event_count&order=desc", nil)
	w := httptest.NewRecorder()
	usersHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Users []UserAggregate `json:"users"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := []string{resp.Users[0].UserID, resp.Users[1].UserID, resp.Users[2].UserID}
	want := []string{"uA", "uB", "uC"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tie-break order mismatch: got %v want %v", got, want)
		}
	}
}

func TestUsersHandler_SinceUntilFilter(t *testing.T) {
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "click", "2026-06-01T00:00:00Z"},
		{"evt_2", "u1", "click", "2026-06-05T00:00:00Z"},
		{"evt_3", "u2", "click", "2026-06-10T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/users?since=2026-06-04T00:00:00Z&until=2026-06-09T00:00:00Z", nil)
	w := httptest.NewRecorder()
	usersHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Users []UserAggregate `json:"users"`
		Total int             `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected total=1 in window, got %d", resp.Total)
	}
	if resp.Users[0].UserID != "u1" || resp.Users[0].EventCount != 1 {
		t.Fatalf("expected u1 with 1 event in window, got %+v", resp.Users[0])
	}
}

func TestUsersHandler_Pagination(t *testing.T) {
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"e1", "uA", "click", "2026-06-01T00:00:00Z"},
		{"e2", "uB", "click", "2026-06-01T00:00:00Z"},
		{"e3", "uC", "click", "2026-06-01T00:00:00Z"},
		{"e4", "uD", "click", "2026-06-01T00:00:00Z"},
		{"e5", "uE", "click", "2026-06-01T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/users?limit=2&offset=2", nil)
	w := httptest.NewRecorder()
	usersHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		Users []UserAggregate `json:"users"`
		Total int             `json:"total"`
		Count int             `json:"count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 5 || resp.Count != 2 {
		t.Fatalf("expected total=5 count=2, got total=%d count=%d", resp.Total, resp.Count)
	}
	got := []string{resp.Users[0].UserID, resp.Users[1].UserID}
	if got[0] != "uC" || got[1] != "uD" {
		t.Fatalf("expected page=[uC,uD], got %v", got)
	}
}

func TestUsersHandler_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/users", nil)
	w := httptest.NewRecorder()
	usersHandler(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestUsersHandler_InvalidSortField(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/users?sort=bogus", nil)
	w := httptest.NewRecorder()
	usersHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUsersHandler_InvalidOrder(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/users?order=bogus", nil)
	w := httptest.NewRecorder()
	usersHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUsersHandler_SinceGreaterThanUntil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/users?since=2026-06-10T00:00:00Z&until=2026-06-01T00:00:00Z", nil)
	w := httptest.NewRecorder()
	usersHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUsersHandler_RegisteredOnRouter(t *testing.T) {
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "click", Timestamp: "2026-06-01T00:00:00Z"},
	})
	srv := httptest.NewServer(newRouter())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/analytics/users")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---- /api/analytics/events_by_day ----

func TestEventsByDayHandler_EmptyStore(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if total, ok := resp["total"].(float64); !ok || total != 0 {
		t.Fatalf("expected total=0, got %v", resp["total"])
	}
	if items, _ := resp["by_day"].([]interface{}); len(items) != 0 {
		t.Fatalf("expected empty by_day, got %d items", len(items))
	}
	if s, _ := resp["sort"].(string); s != "day" {
		t.Fatalf("expected default sort=day, got %v", resp["sort"])
	}
}

func TestEventsByDayHandler_AggregatesByDay(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "signup", "2026-06-01T01:00:00Z"},
		{"evt_2", "u2", "click", "2026-06-01T22:00:00Z"},
		{"evt_3", "u1", "click", "2026-06-02T03:00:00Z"},
		{"evt_4", "u3", "signup", "2026-06-02T12:00:00Z"},
		{"evt_5", "u3", "purchase", "2026-06-02T13:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		ByDay []EventsByDayAggregate `json:"by_day"`
		Total int                    `json:"total"`
		Count int                    `json:"count"`
		Sort  string                 `json:"sort"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("expected total=2 (two days), got %d", resp.Total)
	}
	if resp.Sort != "day" {
		t.Fatalf("expected sort=day, got %s", resp.Sort)
	}
	// 既定 sort=day asc → 2026-06-01 が先頭
	if resp.ByDay[0].Day != "2026-06-01" || resp.ByDay[0].EventCount != 2 {
		t.Fatalf("expected first day=2026-06-01 count=2, got day=%q count=%d", resp.ByDay[0].Day, resp.ByDay[0].EventCount)
	}
	if resp.ByDay[0].DistinctUsers != 2 || resp.ByDay[0].DistinctEventTypes != 2 {
		t.Fatalf("first day distinct mismatch: users=%d types=%d", resp.ByDay[0].DistinctUsers, resp.ByDay[0].DistinctEventTypes)
	}
	if resp.ByDay[0].FirstEventAt != "2026-06-01T01:00:00Z" || resp.ByDay[0].LastEventAt != "2026-06-01T22:00:00Z" {
		t.Fatalf("first day first/last mismatch: first=%q last=%q", resp.ByDay[0].FirstEventAt, resp.ByDay[0].LastEventAt)
	}
	if resp.ByDay[1].Day != "2026-06-02" || resp.ByDay[1].EventCount != 3 {
		t.Fatalf("expected second day=2026-06-02 count=3, got day=%q count=%d", resp.ByDay[1].Day, resp.ByDay[1].EventCount)
	}
	if resp.ByDay[1].DistinctUsers != 2 || resp.ByDay[1].DistinctEventTypes != 3 {
		t.Fatalf("second day distinct mismatch: users=%d types=%d", resp.ByDay[1].DistinctUsers, resp.ByDay[1].DistinctEventTypes)
	}
}

func TestEventsByDayHandler_FilterByEventType(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "signup", "2026-06-01T00:00:00Z"},
		{"evt_2", "u2", "click", "2026-06-01T01:00:00Z"},
		{"evt_3", "u3", "signup", "2026-06-02T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day?event_type=signup", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		ByDay []EventsByDayAggregate `json:"by_day"`
		Total int                    `json:"total"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 2 {
		t.Fatalf("expected 2 days (one signup each), got total=%d", resp.Total)
	}
	for _, b := range resp.ByDay {
		if b.EventCount != 1 {
			t.Fatalf("expected 1 signup per day, got %d on %s", b.EventCount, b.Day)
		}
		if b.DistinctEventTypes != 1 {
			t.Fatalf("expected distinct_event_types=1, got %d", b.DistinctEventTypes)
		}
	}
}

func TestEventsByDayHandler_FilterByUserID(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "click", "2026-06-01T00:00:00Z"},
		{"evt_2", "u2", "click", "2026-06-01T01:00:00Z"},
		{"evt_3", "u1", "click", "2026-06-02T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day?user_id=u1", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		ByDay []EventsByDayAggregate `json:"by_day"`
		Total int                    `json:"total"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 2 {
		t.Fatalf("expected 2 days for u1, got %d", resp.Total)
	}
}

func TestEventsByDayHandler_SortByEventCountDesc(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "x", "2026-06-01T00:00:00Z"},
		{"evt_2", "u1", "x", "2026-06-02T00:00:00Z"},
		{"evt_3", "u2", "x", "2026-06-02T01:00:00Z"},
		{"evt_4", "u3", "x", "2026-06-02T02:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day?sort=event_count&order=desc", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		ByDay []EventsByDayAggregate `json:"by_day"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.ByDay[0].Day != "2026-06-02" || resp.ByDay[0].EventCount != 3 {
		t.Fatalf("expected first item to be 2026-06-02 with count=3, got %q (count=%d)", resp.ByDay[0].Day, resp.ByDay[0].EventCount)
	}
}

func TestEventsByDayHandler_TieBreakerIsDayAsc(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "x", "2026-06-03T00:00:00Z"},
		{"evt_2", "u1", "x", "2026-06-01T00:00:00Z"},
		{"evt_3", "u1", "x", "2026-06-02T00:00:00Z"},
	})
	// 同 event_count=1 で並ぶときは day 昇順がタイブレーカー（reverse モードでも day 昇順を保つ）。
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day?sort=event_count&order=desc", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	var resp struct {
		ByDay []EventsByDayAggregate `json:"by_day"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.ByDay) != 3 {
		t.Fatalf("expected 3 days, got %d", len(resp.ByDay))
	}
	got := []string{resp.ByDay[0].Day, resp.ByDay[1].Day, resp.ByDay[2].Day}
	want := []string{"2026-06-01", "2026-06-02", "2026-06-03"}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("tiebreaker order mismatch at %d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestEventsByDayHandler_SinceUntilFilter(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "x", "2026-06-01T00:00:00Z"},
		{"evt_2", "u1", "x", "2026-06-02T00:00:00Z"},
		{"evt_3", "u1", "x", "2026-06-03T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day?since=2026-06-02T00:00:00Z&until=2026-06-02T23:59:59Z", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	var resp struct {
		ByDay []EventsByDayAggregate `json:"by_day"`
		Total int                    `json:"total"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 1 || resp.ByDay[0].Day != "2026-06-02" {
		t.Fatalf("expected only 2026-06-02, got total=%d (%v)", resp.Total, resp.ByDay)
	}
}

func TestEventsByDayHandler_Pagination(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "x", "2026-06-01T00:00:00Z"},
		{"evt_2", "u1", "x", "2026-06-02T00:00:00Z"},
		{"evt_3", "u1", "x", "2026-06-03T00:00:00Z"},
		{"evt_4", "u1", "x", "2026-06-04T00:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day?limit=2&offset=1", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	var resp struct {
		ByDay  []EventsByDayAggregate `json:"by_day"`
		Total  int                    `json:"total"`
		Count  int                    `json:"count"`
		Limit  int                    `json:"limit"`
		Offset int                    `json:"offset"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 4 {
		t.Fatalf("expected total=4 days, got %d", resp.Total)
	}
	if resp.Count != 2 || resp.Limit != 2 || resp.Offset != 1 {
		t.Fatalf("page metadata mismatch: count=%d limit=%d offset=%d", resp.Count, resp.Limit, resp.Offset)
	}
	if resp.ByDay[0].Day != "2026-06-02" || resp.ByDay[1].Day != "2026-06-03" {
		t.Fatalf("expected page=[2026-06-02, 2026-06-03], got %v", resp.ByDay)
	}
}

func TestEventsByDayHandler_MethodNotAllowed(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/events_by_day", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestEventsByDayHandler_InvalidSortField(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day?sort=bogus", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid sort, got %d", w.Code)
	}
}

func TestEventsByDayHandler_InvalidOrder(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day?order=bogus", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid order, got %d", w.Code)
	}
}

func TestEventsByDayHandler_SinceGreaterThanUntil(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day?since=2026-06-10T00:00:00Z&until=2026-06-01T00:00:00Z", nil)
	w := httptest.NewRecorder()
	eventsByDayHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when since > until, got %d", w.Code)
	}
}

func TestEventsByDayHandler_RegisteredOnRouter(t *testing.T) {
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "click", Timestamp: "2026-06-01T00:00:00Z"},
	})
	srv := httptest.NewServer(newRouter())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/analytics/events_by_day")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
