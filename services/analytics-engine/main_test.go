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
