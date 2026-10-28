package main

import (
	"os"
	"testing"
)

// envTestKey は本テスト用に使う一意性の高い環境変数名を払い出す。
// 親プロセス環境に同名が既存の場合はテスト前提が崩れるため早期に失敗させる。
// サブテストごとに別キーを使うことで、t.Setenv の Cleanup 順序に依存せず
// 「未設定」状態と「空文字セット済み」状態を独立に検証できる。
func envTestKey(t *testing.T, suffix string) string {
	t.Helper()
	key := "PULSEBOARD_ANALYTICS_ENV_TEST_" + suffix
	if v, ok := os.LookupEnv(key); ok {
		t.Fatalf("test precondition violated: %s already set to %q", key, v)
	}
	return key
}

// TestGetEnv は未設定 / 空文字 / 値あり の 3 パターンで getEnv の
// フォールバック挙動が崩れていないことを担保する。
//
// getEnv は PORT / 外部エンドポイント URL など analytics-engine 起動時の
// 文字列設定を読み出す共通経路で使われるため、空文字が「設定済み」ではなく
// 「fallback に落ちる」側に分類される点をテストで固定する。
func TestGetEnv(t *testing.T) {
	t.Run("unset returns fallback", func(t *testing.T) {
		key := envTestKey(t, "GETENV_UNSET")
		if got := getEnv(key, "fb"); got != "fb" {
			t.Errorf("getEnv(unset) = %q, want %q", got, "fb")
		}
	})
	t.Run("empty returns fallback", func(t *testing.T) {
		key := envTestKey(t, "GETENV_EMPTY")
		t.Setenv(key, "")
		if got := getEnv(key, "fb"); got != "fb" {
			t.Errorf("getEnv(empty) = %q, want %q", got, "fb")
		}
	})
	t.Run("value returned when set", func(t *testing.T) {
		key := envTestKey(t, "GETENV_VALUE")
		t.Setenv(key, "override")
		if got := getEnv(key, "fb"); got != "override" {
			t.Errorf("getEnv(set) = %q, want %q", got, "override")
		}
	})
}

// TestGetEnvInt は未設定 / 空文字 / 非整数 / 0 / 負数 / 正の値 の
// 全パターンで getEnvInt のフォールバック挙動と「0 以下は不正値として
// fallback に落とす」ポリシーが崩れていないことを担保する。
//
// MAX_BODY_BYTES / MAX_EVENTS / EVENTS_DEFAULT_LIMIT / EVENTS_MAX_LIMIT 等、
// サイズ・件数系の設定は 0 や負値を受理するとその後の処理（スライス切り詰め・
// 比較）で誤動作しかねないため、本テストで正の値のみ採用する挙動を回帰保護する。
func TestGetEnvInt(t *testing.T) {
	t.Run("unset returns fallback", func(t *testing.T) {
		key := envTestKey(t, "GETENVINT_UNSET")
		if got := getEnvInt(key, 42); got != 42 {
			t.Errorf("getEnvInt(unset) = %d, want 42", got)
		}
	})
	t.Run("empty returns fallback", func(t *testing.T) {
		key := envTestKey(t, "GETENVINT_EMPTY")
		t.Setenv(key, "")
		if got := getEnvInt(key, 42); got != 42 {
			t.Errorf("getEnvInt(empty) = %d, want 42", got)
		}
	})
	t.Run("non integer falls back", func(t *testing.T) {
		key := envTestKey(t, "GETENVINT_NAN")
		t.Setenv(key, "not-an-int")
		if got := getEnvInt(key, 42); got != 42 {
			t.Errorf("getEnvInt(non-int) = %d, want 42", got)
		}
	})
	t.Run("zero falls back", func(t *testing.T) {
		key := envTestKey(t, "GETENVINT_ZERO")
		t.Setenv(key, "0")
		if got := getEnvInt(key, 42); got != 42 {
			t.Errorf("getEnvInt(0) = %d, want 42", got)
		}
	})
	t.Run("negative falls back", func(t *testing.T) {
		key := envTestKey(t, "GETENVINT_NEG")
		t.Setenv(key, "-3")
		if got := getEnvInt(key, 42); got != 42 {
			t.Errorf("getEnvInt(-3) = %d, want 42", got)
		}
	})
	t.Run("positive value is returned", func(t *testing.T) {
		key := envTestKey(t, "GETENVINT_POS")
		t.Setenv(key, "7")
		if got := getEnvInt(key, 42); got != 7 {
			t.Errorf("getEnvInt(7) = %d, want 7", got)
		}
	})
}
