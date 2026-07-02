package main

import (
	"log"
	"os"
	"strings"
)

// init は LOG_LEVEL 環境変数を起動時に 1 回だけ読み currentLogLevel に固定する。
// main() 側での明示的な初期化を不要にすることで、main.go への変更を最小化する
// （テストからは currentLogLevel を直接書き換えて挙動を切り替えられる）。
// 実行中の切り替えは想定しない（sighup 経由の再読み込み等が必要な場合は
// 別途拡張する）。未設定 / 空 / 不正値は INFO へフォールバック。
func init() {
	currentLogLevel = parseLogLevel(os.Getenv("LOG_LEVEL"))
}

// logLevel は analytics-engine 内で使う最小限のログレベル。
// Go 標準 `log` パッケージはレベル概念を持たないため、ヘルスチェック等の
// 高頻度・低価値ログを抑制するために自前で持つ。sibling サービス
// `user-api` (Python) の `LOG_LEVEL` env、`pulseboard-app/services/metrics-worker`
// (Go) の同名抽象と運用を揃え、DEBUG 指定時のみ logDebug が出力する。
type logLevel int

const (
	logLevelDebug logLevel = iota
	logLevelInfo
)

// parseLogLevel は環境変数 LOG_LEVEL の文字列をログレベルへ変換する。
// `"DEBUG"`（大文字小文字無視、前後空白を許容）のみ logLevelDebug、それ以外
// （空・INFO・不正値含む）は logLevelInfo を返す。将来 WARN / ERROR を追加する
// 場合もこの関数だけ拡張すれば良い。
func parseLogLevel(s string) logLevel {
	if strings.EqualFold(strings.TrimSpace(s), "DEBUG") {
		return logLevelDebug
	}
	return logLevelInfo
}

// currentLogLevel は現在のログレベル。プロセス起動時に LOG_LEVEL から
// 1 回だけ読む。テストからは直接書き換えて挙動を切り替えられる。
var currentLogLevel = logLevelInfo

// logDebug は currentLogLevel が Debug 以下の場合のみログ出力する。
// K8s probe / ロードバランサヘルスチェックなど高頻度・低価値イベントを
// 既定 (INFO) では抑制し、`LOG_LEVEL=DEBUG` 時のみ可視化する。
// 既存 `log.Printf(...)` を直接呼び出している箇所は INFO 相当として
// 影響を受けない（後方互換）。
func logDebug(format string, args ...interface{}) {
	if currentLogLevel <= logLevelDebug {
		log.Printf(format, args...)
	}
}
