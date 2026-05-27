package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

const defaultMaxBodyBytes = 1 << 20 // 1 MiB

// maxBodyBytes はリクエストボディの最大バイト数。main で MAX_BODY_BYTES から上書きされる。
var maxBodyBytes int64 = defaultMaxBodyBytes

const defaultMaxEvents = 10000

// maxEvents はインメモリに保持するイベント数の上限。main で MAX_EVENTS から上書きされる。
// 上限を超えた分は古いものから FIFO で破棄し、無制限なメモリ増加を防ぐ。
var maxEvents = defaultMaxEvents

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
	LastEventAt string         `json:"last_event_at,omitempty"`
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

func statsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	mu.RLock()
	defer mu.RUnlock()

	stats := StatsResponse{
		TotalEvents: len(events),
		ByType:      make(map[string]int),
		ByUser:      make(map[string]int),
	}

	for _, e := range events {
		stats.ByType[e.EventType]++
		stats.ByUser[e.UserID]++
	}

	if len(events) > 0 {
		stats.LastEventAt = events[len(events)-1].Timestamp
	}

	log.Printf("Stats requested: %d total events", stats.TotalEvents)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func eventsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	mu.RLock()
	defer mu.RUnlock()

	log.Printf("Events list requested: %d events", len(events))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// newRouter はエンドポイントを登録した mux を返す（テスト容易性のため分離）。
func newRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/analytics/track", trackHandler)
	mux.HandleFunc("/api/analytics/stats", statsHandler)
	mux.HandleFunc("/api/analytics/events", eventsHandler)
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

	srv := newServer(":"+port, newRouter())
	log.Printf("Starting analytics-engine on port %s (max body %d bytes, max events %d)", port, maxBodyBytes, maxEvents)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
