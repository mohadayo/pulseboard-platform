package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// allowedEventSortFields は GET /api/analytics/events で sort= に指定できるフィールド。
var allowedEventSortFields = map[string]bool{
	"timestamp":  true,
	"id":         true,
	"event_type": true,
	"user_id":    true,
}

// allowedEventSortOrders は order= に指定できる順序。
var allowedEventSortOrders = map[string]bool{"asc": true, "desc": true}

// parseAnalyticsTime は ISO 8601 / RFC 3339 形式の文字列を UTC time に正規化する。
// `Z` サフィックスは `+00:00` として扱う（time.Parse(RFC3339Nano) は Z も受け付けるが、
// 一部の数値タイムゾーン記法と統一するため明示的に置換しておく）。
func parseAnalyticsTime(value string) (time.Time, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return time.Time{}, fmt.Errorf("must not be blank")
	}
	if strings.HasSuffix(v, "Z") {
		v = v[:len(v)-1] + "+00:00"
	}
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("must be an ISO 8601 / RFC 3339 datetime")
}

const defaultMaxBodyBytes = 1 << 20 // 1 MiB

// maxBodyBytes はリクエストボディの最大バイト数。main で MAX_BODY_BYTES から上書きされる。
var maxBodyBytes int64 = defaultMaxBodyBytes

const defaultMaxEvents = 10000

// maxEvents はインメモリに保持するイベント数の上限。main で MAX_EVENTS から上書きされる。
// 上限を超えた分は古いものから FIFO で破棄し、無制限なメモリ増加を防ぐ。
var maxEvents = defaultMaxEvents

const (
	defaultEventsPageLimit = 50
	defaultEventsMaxLimit  = 500
)

// /api/analytics/events のページネーション設定。main で
// EVENTS_DEFAULT_LIMIT / EVENTS_MAX_LIMIT から上書きされる。
var (
	eventsDefaultLimit = defaultEventsPageLimit
	eventsMaxLimit     = defaultEventsMaxLimit
)

type Event struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	EventType string `json:"event_type"`
	Payload   string `json:"payload"`
	Timestamp string `json:"timestamp"`
}

type StatsResponse struct {
	TotalEvents int            `json:"total_events"`
	ByType      map[string]int `json:"by_type"`
	ByUser      map[string]int `json:"by_user"`
	// DistinctUsers / DistinctEventTypes は ByType / ByUser のキー数と同じだが、
	// クライアントが ByType / ByUser を取らずに件数だけ表示したい場面（KPI バナー等）で
	// `Object.keys(by_user).length` の事前計算なしに直接参照できるよう、明示フィールドとして返す。
	DistinctUsers      int `json:"distinct_users"`
	DistinctEventTypes int `json:"distinct_event_types"`
	// FirstEventAt はフィルタ通過した最も古い Timestamp。観測ゼロのときは omitempty
	// で省略され、LastEventAt と同じセマンティクスに揃う。RFC3339 は文字列比較で
	// 順序が保たれるため、LastEventAt と同様に直接比較する。
	FirstEventAt string `json:"first_event_at,omitempty"`
	LastEventAt  string `json:"last_event_at,omitempty"`
}

var (
	events  []Event
	mu      sync.RWMutex
	counter int
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		log.Printf("Invalid %s=%q, using fallback %d", key, v, fallback)
	}
	return fallback
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Health check requested")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "healthy",
		"service":   "analytics-engine",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

func trackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// 過大なペイロードによるメモリ枯渇を防ぐためボディサイズを制限する。
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var evt Event
	if err := json.NewDecoder(r.Body).Decode(&evt); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			log.Printf("Request body too large (limit %d bytes)", maxBodyBytes)
			http.Error(w, `{"error":"request body too large"}`, http.StatusRequestEntityTooLarge)
			return
		}
		log.Printf("Invalid event payload: %v", err)
		http.Error(w, `{"error":"invalid JSON payload"}`, http.StatusBadRequest)
		return
	}

	if evt.UserID == "" || evt.EventType == "" {
		log.Println("Missing required fields in event")
		http.Error(w, `{"error":"user_id and event_type are required"}`, http.StatusBadRequest)
		return
	}

	mu.Lock()
	counter++
	evt.ID = fmt.Sprintf("evt_%d", counter)
	evt.Timestamp = time.Now().UTC().Format(time.RFC3339)
	events = append(events, evt)
	if maxEvents > 0 && len(events) > maxEvents {
		removed := len(events) - maxEvents
		events = events[removed:]
		log.Printf("Evicted %d old event(s) (store capped at %d)", removed, maxEvents)
	}
	mu.Unlock()

	log.Printf("Event tracked: type=%s user=%s", evt.EventType, evt.UserID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(evt)
}

// parseAnalyticsQueryTime は since / until / before 等のクエリパラメータを
// 共通で読み出し、空のときは nil を、不正フォーマットでは error を返す。
// 呼び出し側はエラーをそのまま 400 のメッセージに使える。
func parseAnalyticsQueryTime(raw, field string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, err := parseAnalyticsTime(raw)
	if err != nil {
		return nil, fmt.Errorf("query parameter %q %s", field, err.Error())
	}
	return &t, nil
}

// matchEventFilters は単一イベントが (event_type, user_id, since, until) 全フィルタに
// 合致するかを返す。listEventsHandler と statsHandler で同じ判定を共有する。
// 破損した Event.Timestamp は時刻フィルタの取りこぼし防止のため除外する。
func matchEventFilters(e Event, eventType, userID string, since, until *time.Time) bool {
	if eventType != "" && e.EventType != eventType {
		return false
	}
	if userID != "" && e.UserID != userID {
		return false
	}
	if since == nil && until == nil {
		return true
	}
	ts, terr := time.Parse(time.RFC3339, e.Timestamp)
	if terr != nil {
		return false
	}
	if since != nil && ts.Before(*since) {
		return false
	}
	if until != nil && ts.After(*until) {
		return false
	}
	return true
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	eventType := strings.TrimSpace(query.Get("event_type"))
	userID := strings.TrimSpace(query.Get("user_id"))

	since, err := parseAnalyticsQueryTime(query.Get("since"), "since")
	if err != nil {
		log.Printf("Invalid stats since: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	until, err := parseAnalyticsQueryTime(query.Get("until"), "until")
	if err != nil {
		log.Printf("Invalid stats until: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if since != nil && until != nil && until.Before(*since) {
		writeJSONError(w, http.StatusBadRequest, "query parameter 'until' must be greater than or equal to 'since'")
		return
	}

	stats := StatsResponse{
		ByType: make(map[string]int),
		ByUser: make(map[string]int),
	}
	var latestTimestamp string
	var earliestTimestamp string

	mu.RLock()
	for _, e := range events {
		if !matchEventFilters(e, eventType, userID, since, until) {
			continue
		}
		stats.TotalEvents++
		stats.ByType[e.EventType]++
		stats.ByUser[e.UserID]++
		// 最新を時系列で正しく追跡する（後段の `events[len-1]` だと挿入順依存になり、
		// フィルタ後の集合では誤った値になりやすい）。
		if e.Timestamp > latestTimestamp {
			latestTimestamp = e.Timestamp
		}
		// 最古を追跡。空文字を初期値にすると常に上書きされてしまうので、
		// "未設定" の判定を別途行う。RFC3339 は固定幅フィールドで文字列順 = 時刻順。
		if earliestTimestamp == "" || e.Timestamp < earliestTimestamp {
			earliestTimestamp = e.Timestamp
		}
	}
	mu.RUnlock()
	stats.LastEventAt = latestTimestamp
	stats.FirstEventAt = earliestTimestamp
	stats.DistinctUsers = len(stats.ByUser)
	stats.DistinctEventTypes = len(stats.ByType)

	log.Printf(
		"Stats requested: total=%d event_type=%q user_id=%q since=%v until=%v",
		stats.TotalEvents, eventType, userID, since, until,
	)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// parseEventsPageQuery は limit / offset を検証する。
// 戻り値の error が nil でなければ呼び出し側で 400 を返す。
func parseEventsPageQuery(q map[string][]string) (limit, offset int, err error) {
	limit = eventsDefaultLimit
	if vs, ok := q["limit"]; ok && len(vs) > 0 {
		n, perr := strconv.Atoi(vs[0])
		if perr != nil || n < 1 || n > eventsMaxLimit {
			return 0, 0, fmt.Errorf("limit must be an integer between 1 and %d", eventsMaxLimit)
		}
		limit = n
	}
	offset = 0
	if vs, ok := q["offset"]; ok && len(vs) > 0 {
		n, perr := strconv.Atoi(vs[0])
		if perr != nil || n < 0 {
			return 0, 0, fmt.Errorf("offset must be a non-negative integer")
		}
		offset = n
	}
	return limit, offset, nil
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func eventsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listEventsHandler(w, r)
	case http.MethodDelete:
		deleteEventsHandler(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// deleteEventsHandler は user_id / event_type / before の AND フィルタで
// 一致するイベントを削除する。誤った全件削除を防ぐためフィルタは少なくとも 1 つ必要。
func deleteEventsHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	userID := strings.TrimSpace(query.Get("user_id"))
	eventType := strings.TrimSpace(query.Get("event_type"))

	var before *time.Time
	if raw := query.Get("before"); raw != "" {
		t, err := parseAnalyticsTime(raw)
		if err != nil {
			log.Printf("Invalid before on delete: %v", err)
			writeJSONError(
				w,
				http.StatusBadRequest,
				fmt.Sprintf("query parameter 'before' %s", err.Error()),
			)
			return
		}
		before = &t
	}

	if userID == "" && eventType == "" && before == nil {
		writeJSONError(
			w,
			http.StatusBadRequest,
			"at least one of 'user_id', 'event_type', or 'before' must be provided",
		)
		return
	}

	mu.Lock()
	kept := events[:0:0] // 元の events のキャパシティを再利用しない（GC を促す）
	deleted := 0
	for _, e := range events {
		// 全フィルタに合致するなら削除対象（=保持しない）
		matchUser := userID == "" || e.UserID == userID
		matchType := eventType == "" || e.EventType == eventType
		matchBefore := true
		if before != nil {
			ts, terr := time.Parse(time.RFC3339, e.Timestamp)
			// 破損したタイムスタンプはフィルタの取りこぼし／誤削除を避けるため保持
			matchBefore = terr == nil && ts.Before(*before)
		}
		if matchUser && matchType && matchBefore {
			deleted++
			continue
		}
		kept = append(kept, e)
	}
	events = kept
	mu.Unlock()

	beforeOut := ""
	if before != nil {
		beforeOut = before.Format(time.RFC3339)
	}
	log.Printf(
		"Events deleted: count=%d user_id=%q event_type=%q before=%q",
		deleted, userID, eventType, beforeOut,
	)

	resp := map[string]interface{}{
		"deleted":    deleted,
		"user_id":    nullableString(userID),
		"event_type": nullableString(eventType),
		"before":     nullableString(beforeOut),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// nullableString は空文字列を JSON null として表現するためのヘルパ。
// レスポンスでフィルタ未指定を明示するため、`""` ではなく `null` を返す。
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func listEventsHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, offset, perr := parseEventsPageQuery(query)
	if perr != nil {
		log.Printf("Invalid events query: %v", perr)
		writeJSONError(w, http.StatusBadRequest, perr.Error())
		return
	}
	eventType := query.Get("event_type")
	userID := query.Get("user_id")

	sortField := query.Get("sort")
	if sortField == "" {
		sortField = "timestamp"
	}
	if !allowedEventSortFields[sortField] {
		log.Printf("Invalid events sort field: %q", sortField)
		writeJSONError(w, http.StatusBadRequest, "sort must be one of: event_type, id, timestamp, user_id")
		return
	}
	sortOrder := query.Get("order")
	if sortOrder == "" {
		sortOrder = "asc"
	}
	if !allowedEventSortOrders[sortOrder] {
		log.Printf("Invalid events sort order: %q", sortOrder)
		writeJSONError(w, http.StatusBadRequest, "order must be one of: asc, desc")
		return
	}

	var since, until *time.Time
	if raw := query.Get("since"); raw != "" {
		t, err := parseAnalyticsTime(raw)
		if err != nil {
			log.Printf("Invalid since: %v", err)
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("query parameter 'since' %s", err.Error()))
			return
		}
		since = &t
	}
	if raw := query.Get("until"); raw != "" {
		t, err := parseAnalyticsTime(raw)
		if err != nil {
			log.Printf("Invalid until: %v", err)
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("query parameter 'until' %s", err.Error()))
			return
		}
		until = &t
	}
	if since != nil && until != nil && until.Before(*since) {
		writeJSONError(w, http.StatusBadRequest, "query parameter 'until' must be greater than or equal to 'since'")
		return
	}

	mu.RLock()
	filtered := make([]Event, 0, len(events))
	for _, e := range events {
		if !matchEventFilters(e, eventType, userID, since, until) {
			continue
		}
		filtered = append(filtered, e)
	}
	mu.RUnlock()

	reverse := sortOrder == "desc"
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		var av, bv string
		switch sortField {
		case "timestamp":
			av, bv = a.Timestamp, b.Timestamp
		case "id":
			av, bv = a.ID, b.ID
		case "event_type":
			av, bv = a.EventType, b.EventType
		case "user_id":
			av, bv = a.UserID, b.UserID
		}
		if reverse {
			return av > bv
		}
		return av < bv
	})

	total := len(filtered)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := filtered[start:end]
	if page == nil {
		page = []Event{}
	}

	log.Printf(
		"Events list requested: total=%d returned=%d limit=%d offset=%d event_type=%q user_id=%q sort=%s order=%s",
		total, len(page), limit, offset, eventType, userID, sortField, sortOrder,
	)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"events": page,
		"count":  len(page),
		"total":  total,
		"limit":  limit,
		"offset": offset,
		"sort":   sortField,
		"order":  sortOrder,
	})
}

// getEventByIDHandler は GET /api/analytics/events/{id} を処理し、id 完全一致の
// 1 件を返す。該当なしは 404 を JSON で返す。Go 1.22 の拡張ルーティングで
// `{id}` セグメントを取り出すため、http.ServeMux の `GET /...` パターンで登録する。
// メソッドは GET のみ。誤って他メソッドで叩かれた場合に備え、明示的に 405 を返す
// （拡張ルーティングは "GET ..." パターンで他メソッドを 405 にしてくれるが、
// 直接ハンドラを呼ぶテストに対しても挙動を揃えるため冗長に検査する）。
func getEventByIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "id must not be blank")
		return
	}
	mu.RLock()
	var found *Event
	for i := range events {
		if events[i].ID == id {
			// コピーを保持してロック外で安全に Encode する。
			e := events[i]
			found = &e
			break
		}
	}
	mu.RUnlock()
	if found == nil {
		log.Printf("Event not found: id=%s", id)
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("event %q not found", id))
		return
	}
	log.Printf("Event retrieved: id=%s type=%s user=%s", found.ID, found.EventType, found.UserID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(found)
}

// deleteEventByIDHandler は DELETE /api/analytics/events/{id} を処理し、
// id 完全一致の 1 件を削除する。削除前のイベントをレスポンスに含めるので、
// クライアントは別 GET をしなくても監査ログに残せる。
func deleteEventByIDHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "id must not be blank")
		return
	}
	mu.Lock()
	var removed *Event
	for i := range events {
		if events[i].ID == id {
			// 削除前の値を保持してロック外で安全に Encode する。
			e := events[i]
			removed = &e
			// in-place で 1 件を取り除く（順序を保つ）。
			events = append(events[:i], events[i+1:]...)
			break
		}
	}
	mu.Unlock()
	if removed == nil {
		log.Printf("Event not found on delete: id=%s", id)
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("event %q not found", id))
		return
	}
	log.Printf("Event deleted: id=%s type=%s user=%s", removed.ID, removed.EventType, removed.UserID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deleted": 1,
		"id":      removed.ID,
		"event":   removed,
	})
}

// allowedEventTypeSortFields は GET /api/analytics/event_types の sort= 候補。
var allowedEventTypeSortFields = map[string]bool{
	"event_type":     true,
	"event_count":    true,
	"distinct_users": true,
	"first_event_at": true,
	"last_event_at":  true,
}

// allowedEventsByDaySortFields は GET /api/analytics/events_by_day の sort= 候補。
// `event_types` の sort 候補と対称な形（grouping キーが `day` に置き換わる）。
var allowedEventsByDaySortFields = map[string]bool{
	"day":                  true,
	"event_count":          true,
	"distinct_users":       true,
	"distinct_event_types": true,
	"first_event_at":       true,
	"last_event_at":        true,
}

// allowedUserSortFields は GET /api/analytics/users の sort= 候補。
// event_types エンドポイントと並行な命名で揃える（タイブレーカは常に user_id 昇順）。
var allowedUserSortFields = map[string]bool{
	"user_id":              true,
	"event_count":          true,
	"distinct_event_types": true,
	"first_event_at":       true,
	"last_event_at":        true,
}

// UserAggregate は users エンドポイントの 1 要素。
// EventTypeAggregate と意図的に対称な構造（grouping キーと distinct カウントの軸が入れ替わる）。
type UserAggregate struct {
	UserID             string `json:"user_id"`
	EventCount         int    `json:"event_count"`
	DistinctEventTypes int    `json:"distinct_event_types"`
	FirstEventAt       string `json:"first_event_at"`
	LastEventAt        string `json:"last_event_at"`
}

// EventTypeAggregate は event_types エンドポイントの 1 要素。
// JSON タグは StatsResponse と意味的に揃える（first_event_at / last_event_at）。
type EventTypeAggregate struct {
	EventType     string `json:"event_type"`
	EventCount    int    `json:"event_count"`
	DistinctUsers int    `json:"distinct_users"`
	FirstEventAt  string `json:"first_event_at"`
	LastEventAt   string `json:"last_event_at"`
}

// eventTypesHandler は GET /api/analytics/event_types を処理する。
// 保持中イベントを 1 回スキャンして event_type ごとに件数 / distinct_users /
// first_event_at / last_event_at を集計し、sort + pagination をかけて返す。
//
// 既存 stats エンドポイントは「全体集計の数値」のみで、event_type 別の
// timeline (first/last) や per-type の distinct ユーザー数を返さない。
// UI 側で /events?event_type=X を type の数だけ叩く必要があったため、
// 単発エンドポイントで集約して返す。
func eventTypesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := r.URL.Query()

	since, err := parseAnalyticsQueryTime(query.Get("since"), "since")
	if err != nil {
		log.Printf("Invalid event_types since: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	until, err := parseAnalyticsQueryTime(query.Get("until"), "until")
	if err != nil {
		log.Printf("Invalid event_types until: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if since != nil && until != nil && until.Before(*since) {
		writeJSONError(w, http.StatusBadRequest, "query parameter 'until' must be greater than or equal to 'since'")
		return
	}

	sortField := query.Get("sort")
	if sortField == "" {
		sortField = "event_type"
	}
	if !allowedEventTypeSortFields[sortField] {
		log.Printf("Invalid event_types sort field: %q", sortField)
		writeJSONError(w, http.StatusBadRequest, "sort must be one of: distinct_users, event_count, event_type, first_event_at, last_event_at")
		return
	}
	sortOrder := query.Get("order")
	if sortOrder == "" {
		sortOrder = "asc"
	}
	if !allowedEventSortOrders[sortOrder] {
		log.Printf("Invalid event_types sort order: %q", sortOrder)
		writeJSONError(w, http.StatusBadRequest, "order must be one of: asc, desc")
		return
	}

	limit, offset, perr := parseEventsPageQuery(query)
	if perr != nil {
		log.Printf("Invalid event_types pagination: %v", perr)
		writeJSONError(w, http.StatusBadRequest, perr.Error())
		return
	}

	type bucket struct {
		count    int
		users    map[string]struct{}
		first    string
		last     string
	}
	buckets := make(map[string]*bucket)

	mu.RLock()
	for _, e := range events {
		if !matchEventFilters(e, "", "", since, until) {
			continue
		}
		b, ok := buckets[e.EventType]
		if !ok {
			b = &bucket{users: make(map[string]struct{})}
			buckets[e.EventType] = b
		}
		b.count++
		b.users[e.UserID] = struct{}{}
		// RFC3339 は固定幅フィールドで文字列比較が時刻順と一致する。
		if b.first == "" || e.Timestamp < b.first {
			b.first = e.Timestamp
		}
		if e.Timestamp > b.last {
			b.last = e.Timestamp
		}
	}
	mu.RUnlock()

	result := make([]EventTypeAggregate, 0, len(buckets))
	for et, b := range buckets {
		result = append(result, EventTypeAggregate{
			EventType:     et,
			EventCount:    b.count,
			DistinctUsers: len(b.users),
			FirstEventAt:  b.first,
			LastEventAt:   b.last,
		})
	}

	// primary field の値を抽出し、reverse の方向に応じた比較を返す。
	// 同値時は event_type 昇順をタイブレーカーとして使い、reverse モードでも
	// 同一にすることで「primary 同値の表示順」が予測可能になる。
	reverse := sortOrder == "desc"
	primaryLess := func(a, c EventTypeAggregate) (less, equal bool) {
		switch sortField {
		case "event_type":
			return a.EventType < c.EventType, a.EventType == c.EventType
		case "event_count":
			return a.EventCount < c.EventCount, a.EventCount == c.EventCount
		case "distinct_users":
			return a.DistinctUsers < c.DistinctUsers, a.DistinctUsers == c.DistinctUsers
		case "first_event_at":
			return a.FirstEventAt < c.FirstEventAt, a.FirstEventAt == c.FirstEventAt
		case "last_event_at":
			return a.LastEventAt < c.LastEventAt, a.LastEventAt == c.LastEventAt
		}
		return false, true
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, c := result[i], result[j]
		less, equal := primaryLess(a, c)
		if equal {
			// タイブレーカーは reverse の影響を受けず、常に event_type 昇順。
			return a.EventType < c.EventType
		}
		if reverse {
			return !less
		}
		return less
	})

	total := len(result)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := result[start:end]
	if page == nil {
		page = []EventTypeAggregate{}
	}

	log.Printf(
		"EventTypes requested: total=%d returned=%d limit=%d offset=%d sort=%s order=%s",
		total, len(page), limit, offset, sortField, sortOrder,
	)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"event_types": page,
		"count":       len(page),
		"total":       total,
		"limit":       limit,
		"offset":      offset,
		"sort":        sortField,
		"order":       sortOrder,
	})
}

// EventsByDayAggregate は events_by_day エンドポイントの 1 要素。
// `event_types` / `users` ハンドラとの対称性を保つため、JSON タグは
// 既存集計型と意味的に揃える（first_event_at / last_event_at）。
type EventsByDayAggregate struct {
	Day                string `json:"day"`
	EventCount         int    `json:"event_count"`
	DistinctUsers      int    `json:"distinct_users"`
	DistinctEventTypes int    `json:"distinct_event_types"`
	FirstEventAt       string `json:"first_event_at"`
	LastEventAt        string `json:"last_event_at"`
}

// eventsByDayHandler は GET /api/analytics/events_by_day を処理する。
// 保持中イベントを 1 回スキャンして UTC の `YYYY-MM-DD` ごとに件数 /
// distinct_users / distinct_event_types / first_event_at / last_event_at
// を集計し、sort + pagination をかけて返す。`event_types` / `users` の
// 「対象軸を入れ替えた」対称構造で、時間軸のグルーピングを補完する。
//
// クエリ:
//
//   - event_type, user_id, since, until: `statsHandler` と同じ filter セマンティクス
//   - sort: `day` (default), `event_count`, `distinct_users`,
//     `distinct_event_types`, `first_event_at`, `last_event_at`
//   - order: `asc` (default), `desc`
//   - limit, offset: `parseEventsPageQuery` 共通実装
//
// タイブレーカーは `day` 昇順（reverse モードでも変えない）。
func eventsByDayHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := r.URL.Query()

	eventType := strings.TrimSpace(query.Get("event_type"))
	userID := strings.TrimSpace(query.Get("user_id"))

	since, err := parseAnalyticsQueryTime(query.Get("since"), "since")
	if err != nil {
		log.Printf("Invalid events_by_day since: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	until, err := parseAnalyticsQueryTime(query.Get("until"), "until")
	if err != nil {
		log.Printf("Invalid events_by_day until: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if since != nil && until != nil && until.Before(*since) {
		writeJSONError(w, http.StatusBadRequest, "query parameter 'until' must be greater than or equal to 'since'")
		return
	}

	sortField := query.Get("sort")
	if sortField == "" {
		sortField = "day"
	}
	if !allowedEventsByDaySortFields[sortField] {
		log.Printf("Invalid events_by_day sort field: %q", sortField)
		writeJSONError(w, http.StatusBadRequest, "sort must be one of: day, distinct_event_types, distinct_users, event_count, first_event_at, last_event_at")
		return
	}
	sortOrder := query.Get("order")
	if sortOrder == "" {
		sortOrder = "asc"
	}
	if !allowedEventSortOrders[sortOrder] {
		log.Printf("Invalid events_by_day sort order: %q", sortOrder)
		writeJSONError(w, http.StatusBadRequest, "order must be one of: asc, desc")
		return
	}

	limit, offset, perr := parseEventsPageQuery(query)
	if perr != nil {
		log.Printf("Invalid events_by_day pagination: %v", perr)
		writeJSONError(w, http.StatusBadRequest, perr.Error())
		return
	}

	type bucket struct {
		count      int
		users      map[string]struct{}
		eventTypes map[string]struct{}
		first      string
		last       string
	}
	buckets := make(map[string]*bucket)

	mu.RLock()
	for _, e := range events {
		if !matchEventFilters(e, eventType, userID, since, until) {
			continue
		}
		// `Timestamp` は POST 時に RFC3339 に正規化されている前提だが、
		// 何らかの理由でパースに失敗した場合は当該イベントを集計対象から除外する
		// （壊れた行で集計全体が崩れないように deny-by-default）。
		t, perr := parseAnalyticsTime(e.Timestamp)
		if perr != nil {
			continue
		}
		day := t.UTC().Format("2006-01-02")
		b, ok := buckets[day]
		if !ok {
			b = &bucket{
				users:      make(map[string]struct{}),
				eventTypes: make(map[string]struct{}),
			}
			buckets[day] = b
		}
		b.count++
		b.users[e.UserID] = struct{}{}
		b.eventTypes[e.EventType] = struct{}{}
		if b.first == "" || e.Timestamp < b.first {
			b.first = e.Timestamp
		}
		if e.Timestamp > b.last {
			b.last = e.Timestamp
		}
	}
	mu.RUnlock()

	result := make([]EventsByDayAggregate, 0, len(buckets))
	for day, b := range buckets {
		result = append(result, EventsByDayAggregate{
			Day:                day,
			EventCount:         b.count,
			DistinctUsers:      len(b.users),
			DistinctEventTypes: len(b.eventTypes),
			FirstEventAt:       b.first,
			LastEventAt:        b.last,
		})
	}

	reverse := sortOrder == "desc"
	primaryLess := func(a, c EventsByDayAggregate) (less, equal bool) {
		switch sortField {
		case "day":
			return a.Day < c.Day, a.Day == c.Day
		case "event_count":
			return a.EventCount < c.EventCount, a.EventCount == c.EventCount
		case "distinct_users":
			return a.DistinctUsers < c.DistinctUsers, a.DistinctUsers == c.DistinctUsers
		case "distinct_event_types":
			return a.DistinctEventTypes < c.DistinctEventTypes, a.DistinctEventTypes == c.DistinctEventTypes
		case "first_event_at":
			return a.FirstEventAt < c.FirstEventAt, a.FirstEventAt == c.FirstEventAt
		case "last_event_at":
			return a.LastEventAt < c.LastEventAt, a.LastEventAt == c.LastEventAt
		}
		return false, true
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, c := result[i], result[j]
		less, equal := primaryLess(a, c)
		if equal {
			// タイブレーカーは reverse の影響を受けず、常に day 昇順。
			return a.Day < c.Day
		}
		if reverse {
			return !less
		}
		return less
	})

	total := len(result)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := result[start:end]
	if page == nil {
		page = []EventsByDayAggregate{}
	}

	log.Printf(
		"EventsByDay requested: total=%d returned=%d limit=%d offset=%d sort=%s order=%s",
		total, len(page), limit, offset, sortField, sortOrder,
	)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"by_day": page,
		"count":  len(page),
		"total":  total,
		"limit":  limit,
		"offset": offset,
		"sort":   sortField,
		"order":  sortOrder,
	})
}

// usersHandler は GET /api/analytics/users を処理する。
// 保持中イベントを 1 回スキャンして user_id ごとに件数 / distinct_event_types /
// first_event_at / last_event_at を集計し、sort + pagination をかけて返す。
//
// /api/analytics/event_types とは grouping キーと distinct カウントの軸が入れ替わった
// 対称な構造で、`/api/analytics/stats` の `by_user` カウント以上の情報（時刻範囲・
// distinct event_type 数）を 1 リクエストで取得できる。
func usersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	query := r.URL.Query()

	since, err := parseAnalyticsQueryTime(query.Get("since"), "since")
	if err != nil {
		log.Printf("Invalid users since: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	until, err := parseAnalyticsQueryTime(query.Get("until"), "until")
	if err != nil {
		log.Printf("Invalid users until: %v", err)
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if since != nil && until != nil && until.Before(*since) {
		writeJSONError(w, http.StatusBadRequest, "query parameter 'until' must be greater than or equal to 'since'")
		return
	}

	sortField := query.Get("sort")
	if sortField == "" {
		sortField = "user_id"
	}
	if !allowedUserSortFields[sortField] {
		log.Printf("Invalid users sort field: %q", sortField)
		writeJSONError(w, http.StatusBadRequest, "sort must be one of: distinct_event_types, event_count, first_event_at, last_event_at, user_id")
		return
	}
	sortOrder := query.Get("order")
	if sortOrder == "" {
		sortOrder = "asc"
	}
	if !allowedEventSortOrders[sortOrder] {
		log.Printf("Invalid users sort order: %q", sortOrder)
		writeJSONError(w, http.StatusBadRequest, "order must be one of: asc, desc")
		return
	}

	limit, offset, perr := parseEventsPageQuery(query)
	if perr != nil {
		log.Printf("Invalid users pagination: %v", perr)
		writeJSONError(w, http.StatusBadRequest, perr.Error())
		return
	}

	type bucket struct {
		count      int
		eventTypes map[string]struct{}
		first      string
		last       string
	}
	buckets := make(map[string]*bucket)

	mu.RLock()
	for _, e := range events {
		if !matchEventFilters(e, "", "", since, until) {
			continue
		}
		b, ok := buckets[e.UserID]
		if !ok {
			b = &bucket{eventTypes: make(map[string]struct{})}
			buckets[e.UserID] = b
		}
		b.count++
		b.eventTypes[e.EventType] = struct{}{}
		// RFC3339 は固定幅フィールドで文字列比較が時刻順と一致する。
		if b.first == "" || e.Timestamp < b.first {
			b.first = e.Timestamp
		}
		if e.Timestamp > b.last {
			b.last = e.Timestamp
		}
	}
	mu.RUnlock()

	result := make([]UserAggregate, 0, len(buckets))
	for uid, b := range buckets {
		result = append(result, UserAggregate{
			UserID:             uid,
			EventCount:         b.count,
			DistinctEventTypes: len(b.eventTypes),
			FirstEventAt:       b.first,
			LastEventAt:        b.last,
		})
	}

	reverse := sortOrder == "desc"
	primaryLess := func(a, c UserAggregate) (less, equal bool) {
		switch sortField {
		case "user_id":
			return a.UserID < c.UserID, a.UserID == c.UserID
		case "event_count":
			return a.EventCount < c.EventCount, a.EventCount == c.EventCount
		case "distinct_event_types":
			return a.DistinctEventTypes < c.DistinctEventTypes, a.DistinctEventTypes == c.DistinctEventTypes
		case "first_event_at":
			return a.FirstEventAt < c.FirstEventAt, a.FirstEventAt == c.FirstEventAt
		case "last_event_at":
			return a.LastEventAt < c.LastEventAt, a.LastEventAt == c.LastEventAt
		}
		return false, true
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, c := result[i], result[j]
		less, equal := primaryLess(a, c)
		if equal {
			// タイブレーカーは reverse の影響を受けず、常に user_id 昇順。
			return a.UserID < c.UserID
		}
		if reverse {
			return !less
		}
		return less
	})

	total := len(result)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := result[start:end]
	if page == nil {
		page = []UserAggregate{}
	}

	log.Printf(
		"Users requested: total=%d returned=%d limit=%d offset=%d sort=%s order=%s",
		total, len(page), limit, offset, sortField, sortOrder,
	)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"users":  page,
		"count":  len(page),
		"total":  total,
		"limit":  limit,
		"offset": offset,
		"sort":   sortField,
		"order":  sortOrder,
	})
}

// newRouter はエンドポイントを登録した mux を返す（テスト容易性のため分離）。
func newRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/analytics/track", trackHandler)
	mux.HandleFunc("/api/analytics/stats", statsHandler)
	mux.HandleFunc("/api/analytics/event_types", eventTypesHandler)
	mux.HandleFunc("/api/analytics/users", usersHandler)
	mux.HandleFunc("/api/analytics/events_by_day", eventsByDayHandler)
	mux.HandleFunc("/api/analytics/events", eventsHandler)
	// 単一イベント取得 / 削除。Go 1.22 の拡張ルーティングで {id} を取り出す。
	// `/api/analytics/events`（一覧/フィルタ削除）と `/api/analytics/events/{id}`（単発）は
	// パターンとして異なるため衝突しない。メソッド指定で GET と DELETE を別ハンドラに振り分ける。
	mux.HandleFunc("GET /api/analytics/events/{id}", getEventByIDHandler)
	mux.HandleFunc("DELETE /api/analytics/events/{id}", deleteEventByIDHandler)
	return mux
}

// newServer はスロークライアント攻撃やコネクションリーク対策として
// 各種タイムアウトを設定した http.Server を返す。
func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func main() {
	port := getEnv("ANALYTICS_PORT", "5002")
	maxBodyBytes = int64(getEnvInt("MAX_BODY_BYTES", defaultMaxBodyBytes))
	maxEvents = getEnvInt("MAX_EVENTS", defaultMaxEvents)
	eventsDefaultLimit = getEnvInt("EVENTS_DEFAULT_LIMIT", defaultEventsPageLimit)
	eventsMaxLimit = getEnvInt("EVENTS_MAX_LIMIT", defaultEventsMaxLimit)
	if eventsDefaultLimit > eventsMaxLimit {
		eventsDefaultLimit = eventsMaxLimit
	}

	srv := newServer(":"+port, newRouter())
	log.Printf("Starting analytics-engine on port %s (max body %d bytes, max events %d)", port, maxBodyBytes, maxEvents)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
