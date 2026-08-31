# オブザーバビリティ運用ガイド (Observability)

Pulseboard Platform の 3 サービス (`user-api` / `notification-service` / `analytics-engine`) に対する、メトリクス・ログ・トレースの運用方針を集約するドキュメントです。
アーキテクチャの全体像は [`docs/ARCHITECTURE.md`](./ARCHITECTURE.md)、インシデント時の対応手順は [`docs/RUNBOOK.md`](./RUNBOOK.md)、症状ベースの逆引きは [`docs/TROUBLESHOOTING.md`](./TROUBLESHOOTING.md) を参照してください。

## 1. オブザーバビリティ 3 本柱

### 1.1 Metrics (指標)

各サービスは最低限、以下のカテゴリのメトリクスを提供することを目指します。

| カテゴリ            | 例                                         | 対応サービス                    |
| ------------------- | ------------------------------------------ | ------------------------------- |
| リクエスト数        | `http_requests_total`                     | user-api / notification-service |
| リクエストレイテンシ | `http_request_duration_seconds`           | user-api / notification-service |
| エラー率            | 5xx 応答率                                | user-api / notification-service |
| 通知配信            | 送信成功数 / 失敗数 / リトライ数           | notification-service           |
| 集計ジョブ          | 実行時間 / 処理レコード数 / 失敗数         | analytics-engine               |

新規メトリクスを追加する場合は、既存の名前・単位規約 (例: `_seconds` / `_bytes`) を踏襲してください。

### 1.2 Logs (ログ)

- **フォーマット:** 各サービスで JSON 構造化ログを推奨。少なくとも `timestamp` / `level` / `service` / `message` の 4 フィールドを含める。
- **相関 ID:** リクエスト単位で `request_id` (もしくは `trace_id`) を伝播し、ログに付与する。
- **PII の扱い:** メールアドレス / 電話番号 / 認証トークンをログ本文に出さない。マスク or 参照 ID に置換する。

### 1.3 Traces (トレース)

- 対応可能な範囲で OpenTelemetry 互換の分散トレースを目指す（現状は必須ではない）。
- API → 内部呼び出し → DB のスパンを最低限捕捉できる粒度で設計する。

## 2. ログレベルとフォーマット統一方針

以下のレベル運用を推奨します:

- **DEBUG** — 開発・ローカル調査時のみ。本番では既定オフ。
- **INFO** — 正常系の主要イベント (起動 / 停止 / ジョブ開始・終了)。
- **WARNING** — リトライ発生・フォールバック動作・非致命的な設定不備。
- **ERROR** — 単一リクエスト / 単一ジョブが失敗した事象。スタックトレースを含む。
- **CRITICAL** — サービス全体が動作不能に近い事象 (依存 DB への恒常的接続失敗など)。

環境変数 `LOG_LEVEL` (該当する場合) で切り替え、既定は `INFO` としてください。

## 3. サービス別 SLI / SLO 案

以下は暫定的な指標であり、実運用データを蓄積し次第見直します。

### 3.1 user-api

- **SLI:** `POST /users/*` / `GET /users/*` の p95 レイテンシ、5xx 応答率
- **SLO 案:** 直近 30 日で p95 < 500 ms、5xx < 0.5%

### 3.2 notification-service

- **SLI:** 通知送信の成功率 (成功数 / (成功数 + 失敗数))、キュー滞留時間
- **SLO 案:** 直近 30 日で成功率 > 99%、キュー滞留 p95 < 60 秒

### 3.3 analytics-engine

- **SLI:** バッチジョブ成功率、平均実行時間
- **SLO 案:** 直近 30 日でジョブ成功率 > 99%、平均実行時間の週次悪化 < 20%

## 4. アラート → RUNBOOK 導線

アラートは可能な限り [`docs/RUNBOOK.md`](./RUNBOOK.md) の特定のセクションを指すようにしてください。テンプレート例:

```
[SEV-2] user-api 5xx > 1% (5min)
Runbook: docs/RUNBOOK.md#user-api-リカバリ
Dashboard: <URL>
```

こうすることで、オンコール担当がアラートから直接手順に到達できます。

## 5. 関連ドキュメント

- [`docs/ARCHITECTURE.md`](./ARCHITECTURE.md) — システム全体像
- [`docs/RUNBOOK.md`](./RUNBOOK.md) — インシデント初動手順
- [`docs/TROUBLESHOOTING.md`](./TROUBLESHOOTING.md) — 症状 → 原因 → 対処
- [`docs/FAQ.md`](./FAQ.md) — よくある質問
- [`SECURITY.md`](../SECURITY.md) — 脆弱性報告

## 6. 更新方針

- 新しいメトリクス / ログ / SLI を追加した際は本ドキュメントの該当節にも追記してください。
- SLO 案は実データを蓄積し、四半期毎に見直します。
