package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ----------------------------------------------------------------------
// /api/analytics/events_by_day_of_week
// ----------------------------------------------------------------------

func TestEventsByDayOfWeekHandler_EmptyStore(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
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
	if items, _ := resp["by_day_of_week"].([]interface{}); len(items) != 0 {
		t.Fatalf("expected empty by_day_of_week, got %d items", len(items))
	}
	if s, _ := resp["sort"].(string); s != "day_of_week" {
		t.Fatalf("expected default sort=day_of_week, got %v", resp["sort"])
	}
}

func TestEventsByDayOfWeekHandler_AggregatesByDayOfWeek(t *testing.T) {
	resetState()
	// 2026-06-01 (Mon, ISO 1), 2026-06-03 (Wed, ISO 3), 2026-06-07 (Sun, ISO 7)
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "signup", "2026-06-01T10:00:00Z"},
		{"evt_2", "u2", "click", "2026-06-01T11:00:00Z"},
		{"evt_3", "u1", "click", "2026-06-03T09:00:00Z"},
		{"evt_4", "u3", "signup", "2026-06-07T12:00:00Z"},
		{"evt_5", "u3", "purchase", "2026-06-07T13:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp struct {
		ByDOW []EventsByDayOfWeekAggregate `json:"by_day_of_week"`
		Total int                          `json:"total"`
		Sort  string                       `json:"sort"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 3 {
		t.Fatalf("expected total=3 (Mon/Wed/Sun), got %d", resp.Total)
	}
	if resp.Sort != "day_of_week" {
		t.Fatalf("expected sort=day_of_week, got %s", resp.Sort)
	}
	// 既定 asc: "1" < "3" < "7"
	if resp.ByDOW[0].DayOfWeek != "1" || resp.ByDOW[0].EventCount != 2 {
		t.Fatalf("expected dow=1 count=2, got dow=%q count=%d", resp.ByDOW[0].DayOfWeek, resp.ByDOW[0].EventCount)
	}
	if resp.ByDOW[0].DistinctUsers != 2 || resp.ByDOW[0].DistinctEventTypes != 2 {
		t.Fatalf("dow=1 distinct mismatch: users=%d types=%d", resp.ByDOW[0].DistinctUsers, resp.ByDOW[0].DistinctEventTypes)
	}
	if resp.ByDOW[1].DayOfWeek != "3" || resp.ByDOW[1].EventCount != 1 {
		t.Fatalf("expected dow=3 count=1, got dow=%q count=%d", resp.ByDOW[1].DayOfWeek, resp.ByDOW[1].EventCount)
	}
	if resp.ByDOW[2].DayOfWeek != "7" || resp.ByDOW[2].EventCount != 2 {
		t.Fatalf("expected dow=7 count=2, got dow=%q count=%d", resp.ByDOW[2].DayOfWeek, resp.ByDOW[2].EventCount)
	}
}

func TestEventsByDayOfWeekHandler_SundayMapsToSeven(t *testing.T) {
	// 2026-06-07 は日曜 (Go time.Sunday=0)。ISO 規約で "7" に正規化されること。
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "click", "2026-06-07T10:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	var resp struct {
		ByDOW []EventsByDayOfWeekAggregate `json:"by_day_of_week"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.ByDOW) != 1 || resp.ByDOW[0].DayOfWeek != "7" {
		t.Fatalf("expected single dow=7, got %+v", resp.ByDOW)
	}
}

func TestEventsByDayOfWeekHandler_ConvertsTimezoneToUTC(t *testing.T) {
	// 2026-06-08T00:30:00+02:00 (Mon CEST) = 2026-06-07T22:30:00Z (Sun UTC) → "7"
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "click", "2026-06-08T00:30:00+02:00"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	var resp struct {
		ByDOW []EventsByDayOfWeekAggregate `json:"by_day_of_week"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.ByDOW) != 1 || resp.ByDOW[0].DayOfWeek != "7" {
		t.Fatalf("expected single dow=7 (UTC-normalized Sunday), got %+v", resp.ByDOW)
	}
}

func TestEventsByDayOfWeekHandler_FilterByEventType(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "signup", "2026-06-01T10:00:00Z"},
		{"evt_2", "u2", "click", "2026-06-02T10:00:00Z"},
		{"evt_3", "u3", "signup", "2026-06-03T10:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week?event_type=signup", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	var resp struct {
		ByDOW []EventsByDayOfWeekAggregate `json:"by_day_of_week"`
		Total int                          `json:"total"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 2 {
		t.Fatalf("expected 2 dows with signup (Mon=1, Wed=3), got total=%d", resp.Total)
	}
	for _, b := range resp.ByDOW {
		if b.DistinctEventTypes != 1 {
			t.Fatalf("expected distinct_event_types=1 (only signup), got %d", b.DistinctEventTypes)
		}
	}
}

func TestEventsByDayOfWeekHandler_FilterByUserID(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "click", "2026-06-01T10:00:00Z"},
		{"evt_2", "u2", "click", "2026-06-01T11:00:00Z"},
		{"evt_3", "u1", "click", "2026-06-03T10:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week?user_id=u1", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	var resp struct {
		Total int `json:"total"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 2 {
		t.Fatalf("expected 2 dows for u1 (Mon=1 and Wed=3), got %d", resp.Total)
	}
}

func TestEventsByDayOfWeekHandler_SinceUntilFilter(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "click", "2026-05-30T10:00:00Z"}, // Sat (6) - 範囲外
		{"evt_2", "u1", "click", "2026-06-01T10:00:00Z"}, // Mon (1) - 範囲内
		{"evt_3", "u1", "click", "2026-06-03T10:00:00Z"}, // Wed (3) - 範囲内
		{"evt_4", "u1", "click", "2026-06-10T10:00:00Z"}, // Wed (3) - 範囲外
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week?since=2026-06-01T00:00:00Z&until=2026-06-05T00:00:00Z", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	var resp struct {
		ByDOW []EventsByDayOfWeekAggregate `json:"by_day_of_week"`
		Total int                          `json:"total"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 2 {
		t.Fatalf("expected 2 dows in window (Mon=1 and Wed=3), got %d", resp.Total)
	}
}

func TestEventsByDayOfWeekHandler_SortByEventCountDesc(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		// Mon (1) x1
		{"evt_1", "u1", "click", "2026-06-01T10:00:00Z"},
		// Tue (2) x3
		{"evt_2", "u1", "click", "2026-06-02T10:00:00Z"},
		{"evt_3", "u2", "click", "2026-06-02T11:00:00Z"},
		{"evt_4", "u3", "click", "2026-06-02T12:00:00Z"},
		// Wed (3) x2
		{"evt_5", "u1", "click", "2026-06-03T10:00:00Z"},
		{"evt_6", "u2", "click", "2026-06-03T11:00:00Z"},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week?sort=event_count&order=desc", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	var resp struct {
		ByDOW []EventsByDayOfWeekAggregate `json:"by_day_of_week"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.ByDOW) != 3 {
		t.Fatalf("expected 3 dows, got %d", len(resp.ByDOW))
	}
	// desc by event_count: Tue (3) > Wed (2) > Mon (1)
	if resp.ByDOW[0].DayOfWeek != "2" || resp.ByDOW[0].EventCount != 3 {
		t.Fatalf("expected first dow=2 count=3, got dow=%q count=%d", resp.ByDOW[0].DayOfWeek, resp.ByDOW[0].EventCount)
	}
	if resp.ByDOW[1].DayOfWeek != "3" || resp.ByDOW[1].EventCount != 2 {
		t.Fatalf("expected second dow=3 count=2, got dow=%q count=%d", resp.ByDOW[1].DayOfWeek, resp.ByDOW[1].EventCount)
	}
	if resp.ByDOW[2].DayOfWeek != "1" || resp.ByDOW[2].EventCount != 1 {
		t.Fatalf("expected third dow=1 count=1, got dow=%q count=%d", resp.ByDOW[2].DayOfWeek, resp.ByDOW[2].EventCount)
	}
}

func TestEventsByDayOfWeekHandler_TieBreakerIsDayOfWeekAsc(t *testing.T) {
	// 同じ event_count を持つ複数の曜日でも、タイブレーカで day_of_week 昇順固定
	// （reverse の影響を受けない、events_by_hour_of_day と同じ規約）。
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "click", "2026-06-01T10:00:00Z"}, // Mon (1)
		{"evt_2", "u2", "click", "2026-06-03T10:00:00Z"}, // Wed (3)
		{"evt_3", "u3", "click", "2026-06-05T10:00:00Z"}, // Fri (5)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week?sort=event_count&order=desc", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	var resp struct {
		ByDOW []EventsByDayOfWeekAggregate `json:"by_day_of_week"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	// 全部 count=1。order=desc でも day_of_week 昇順タイブレーカ → 1, 3, 5
	if resp.ByDOW[0].DayOfWeek != "1" || resp.ByDOW[1].DayOfWeek != "3" || resp.ByDOW[2].DayOfWeek != "5" {
		t.Fatalf("expected tiebreaker day_of_week asc [1,3,5], got [%q,%q,%q]",
			resp.ByDOW[0].DayOfWeek, resp.ByDOW[1].DayOfWeek, resp.ByDOW[2].DayOfWeek)
	}
}

func TestEventsByDayOfWeekHandler_Pagination(t *testing.T) {
	resetState()
	seedEventsAt([]struct {
		ID        string
		UserID    string
		EventType string
		Timestamp string
	}{
		{"evt_1", "u1", "click", "2026-06-01T10:00:00Z"}, // Mon (1)
		{"evt_2", "u1", "click", "2026-06-02T10:00:00Z"}, // Tue (2)
		{"evt_3", "u1", "click", "2026-06-03T10:00:00Z"}, // Wed (3)
		{"evt_4", "u1", "click", "2026-06-04T10:00:00Z"}, // Thu (4)
		{"evt_5", "u1", "click", "2026-06-05T10:00:00Z"}, // Fri (5)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week?limit=2&offset=2", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	var resp struct {
		ByDOW  []EventsByDayOfWeekAggregate `json:"by_day_of_week"`
		Total  int                          `json:"total"`
		Limit  int                          `json:"limit"`
		Offset int                          `json:"offset"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Total != 5 || resp.Limit != 2 || resp.Offset != 2 {
		t.Fatalf("pagination metadata mismatch: total=%d limit=%d offset=%d", resp.Total, resp.Limit, resp.Offset)
	}
	if len(resp.ByDOW) != 2 || resp.ByDOW[0].DayOfWeek != "3" || resp.ByDOW[1].DayOfWeek != "4" {
		t.Fatalf("expected page=[3,4], got %+v", resp.ByDOW)
	}
}

func TestEventsByDayOfWeekHandler_MethodNotAllowed(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodPost, "/api/analytics/events_by_day_of_week", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestEventsByDayOfWeekHandler_InvalidSortField(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week?sort=bogus", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid sort, got %d", w.Code)
	}
}

func TestEventsByDayOfWeekHandler_InvalidOrder(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week?order=sideways", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid order, got %d", w.Code)
	}
}

func TestEventsByDayOfWeekHandler_SinceGreaterThanUntil(t *testing.T) {
	resetState()
	req := httptest.NewRequest(http.MethodGet, "/api/analytics/events_by_day_of_week?since=2026-06-10T00:00:00Z&until=2026-06-01T00:00:00Z", nil)
	w := httptest.NewRecorder()
	eventsByDayOfWeekHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on since>until, got %d", w.Code)
	}
}

func TestEventsByDayOfWeekHandler_RegisteredOnRouter(t *testing.T) {
	resetState()
	seedEvents([]Event{
		{UserID: "u1", EventType: "click", Timestamp: "2026-06-01T01:00:00Z"},
	})
	srv := httptest.NewServer(newRouter())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/analytics/events_by_day_of_week")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
