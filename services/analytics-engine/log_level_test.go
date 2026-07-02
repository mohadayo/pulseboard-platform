package main

import (
	"bytes"
	"log"
	"testing"
)

// TestParseLogLevel は LOG_LEVEL 文字列パースの境界条件を網羅する。
//
// 「DEBUG」以外は空・INFO・不正値・未知の値すべて INFO にフォールバックする
// fail-safe 設計を保証する。分岐に見落としがあると、DEBUG 指定時に既定へ
// 落ちるといった観測性劣化に繋がるためテーブルで明示する。
func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  logLevel
	}{
		{"empty defaults to info", "", logLevelInfo},
		{"whitespace only defaults to info", "   ", logLevelInfo},
		{"debug uppercase", "DEBUG", logLevelDebug},
		{"debug lowercase", "debug", logLevelDebug},
		{"debug mixed case", "DeBuG", logLevelDebug},
		{"debug with surrounding whitespace", "  DEBUG  ", logLevelDebug},
		{"info stays info", "INFO", logLevelInfo},
		{"warn falls back to info", "WARN", logLevelInfo},
		{"unknown value falls back to info", "verbose", logLevelInfo},
		{"partial match falls back to info", "DEBU", logLevelInfo},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLogLevel(tc.input)
			if got != tc.want {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestLogDebug_SuppressesOutputAtInfoLevel は INFO レベル時に logDebug が
// 何も出力しないことを検証する。K8s probe / ロードバランサヘルスチェックの
// ノイズを既定運用で確実に抑止するための回帰テスト。
func TestLogDebug_SuppressesOutputAtInfoLevel(t *testing.T) {
	prev := currentLogLevel
	t.Cleanup(func() { currentLogLevel = prev })
	currentLogLevel = logLevelInfo

	var buf bytes.Buffer
	origOut := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
	})

	logDebug("suppressed: %s", "value")

	if buf.Len() != 0 {
		t.Errorf("expected no output at INFO level, got %q", buf.String())
	}
}

// TestLogDebug_WritesOutputAtDebugLevel は DEBUG レベル時に logDebug が
// フォーマット文字列を展開して出力することを検証する。
// currentLogLevel を切り替えると挙動が変わることを担保する。
func TestLogDebug_WritesOutputAtDebugLevel(t *testing.T) {
	prev := currentLogLevel
	t.Cleanup(func() { currentLogLevel = prev })
	currentLogLevel = logLevelDebug

	var buf bytes.Buffer
	origOut := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
	})

	logDebug("visible: %s=%d", "count", 42)

	got := buf.String()
	want := "visible: count=42\n"
	if got != want {
		t.Errorf("expected %q at DEBUG level, got %q", want, got)
	}
}
