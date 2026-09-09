# 用語集（Glossary）

PulseBoard Platform のドキュメントおよびソースコードで登場する専門用語を、サービス横断で参照できる単一のリファレンスとして集約する。README・[`ARCHITECTURE.md`](./ARCHITECTURE.md)・[`RUNBOOK.md`](./RUNBOOK.md)・[`FAQ.md`](./FAQ.md)・[`TROUBLESHOOTING.md`](./TROUBLESHOOTING.md)・[`OBSERVABILITY.md`](./OBSERVABILITY.md) のいずれかで初出となる語彙を、意味・使用文脈・関連する API や環境変数と共に記載する。

用語の定義に矛盾が見つかった場合は、本ドキュメントを唯一の正（source of truth）とし、他ドキュメントを本ドキュメントに揃える。

## 1. プラットフォーム共通概念

| 用語 | 定義 |
|------|------|
| **PulseBoard Platform** | `user-api` / `analytics-engine` / `notification-service` の 3 サービスから構成されるマイクロサービス型ダッシュボード基盤。 |
| **サービス（Service）** | 単一責務を持つ独立したプロセス／コンテナ。本プロジェクトでは 3 つ（Python / Go / TypeScript）。 |
| **in-memory store** | 永続化を持たず、プロセスメモリ上のマップ・スライスにデータを保持する方式。プロセス再起動でデータは消える。プロトタイプ／評価用途を想定した設計判断。詳細は [ARCHITECTURE.md](./ARCHITECTURE.md) を参照。 |
| **FIFO eviction** | 保持件数上限（`MAX_EVENTS` / `MAX_NOTIFICATIONS`）に達した際、最も古いレコードから順に削除する退避戦略。First-In-First-Out。 |
| **ヘルスチェック（`/health`）** | 各サービスが提供する軽量エンドポイント。K8s / ロードバランサからの liveness probe を想定。`LOG_LEVEL=INFO`（既定）ではこのエンドポイントのアクセスログは抑止される。 |
| **グレースフルシャットダウン** | `SIGTERM` / `SIGINT` を受け、進行中リクエストを完了させてから終了する挙動。全 3 サービスで実装済み。 |
| **ページネーション** | `?limit=` / `?offset=` によるオフセット式ページ送り。上限は各サービスの `*_MAX_LIMIT` 相当の環境変数で制御。 |
| **フィルタパラメータ** | `?user_id=` / `?channel=` / `?status=` / `?since=` / `?until=` / `?q=` など、リスト・集計エンドポイントに共通する絞り込みクエリ。 |

## 2. User API 用語

| 用語 | 定義 |
|------|------|
| **user** | `email` をキーとする一意の主体。`id` はサーバ側で払い出す不透明識別子。 |
| **正規化された email** | 前後の空白を除去し小文字化した email 文字列。登録時・ログイン時に正規化して照合するため `Foo@x.com` と `foo@x.com` は同一アカウントに解決される。 |
| **JWT** | ログイン時に払い出す JSON Web Token。`Authorization: Bearer <token>` ヘッダで認証。有効期限は `TOKEN_EXPIRY_HOURS`。 |
| **`current_password` 再入力** | パスワード変更・アカウント削除で JWT に加えて要求される二段階確認。JWT 漏洩時の誤操作・第三者操作を防ぐ設計。 |
| **`created_at`** | ユーザ登録時刻。ISO 8601 UTC。集計エンドポイントの粒度別バケット（日／週／月／年／曜日／時）はすべてこの値を基準とする。 |
| **domain バケット（`by_domain`）** | email の `@` 以降を集計軸とするカウント。`@` を含まない値は `unknown` バケットにフォールバック。 |
| **signups_by_\* 系エンドポイント** | 登録件数を粒度別（day / week / month / year / day_of_week / hour_of_day）に返す軽量集計。ダッシュボード用。 |

## 3. Analytics Engine 用語

| 用語 | 定義 |
|------|------|
| **event** | Analytics Engine が受け付ける最小単位のログ。`user_id` / `event_type` / `payload` / `created_at` を持つ。 |
| **event_type** | event の分類ラベル（例: `page_view` / `click` / `signup`）。集計軸として頻用。 |
| **payload** | event に付随する任意の文字列。構造化スキーマは強制しない。 |
| **event_count** | 集計エンドポイントで返す、そのバケットに属する event の総数。 |
| **distinct_users** | そのバケット内で一意な `user_id` の数。 |
| **distinct_event_types** | そのバケット内で一意な `event_type` の数。 |
| **first_event_at / last_event_at** | そのバケット内で最古／最新の event の `created_at`。ISO 8601 UTC。 |
| **`min_event_count`** | 一部の集計エンドポイントで、`event_count` がしきい値未満のバケットを除外するフィルタ。 |
| **粒度別集計** | UTC 日付 / UTC 時刻 / ISO 曜日 / event_type / user_id をキーとする集計エンドポイント群。粒度をまたぐ集計は行わない（例: `events_by_hour_of_day` は日付を跨いで統合）。 |

## 4. Notification Service 用語

| 用語 | 定義 |
|------|------|
| **notification** | 送信済み通知の 1 レコード。`user_id` / `channel` / `status` / `title` / `message` / `created_at` を持つ。 |
| **channel** | 送信経路。`ALLOWED_CHANNELS`（例: `email` / `sms` / `push`）に含まれる値のみ許容。集計サマリはこの一覧を全キーで 0 埋めして返すため、クライアント側は存在チェック不要。 |
| **status** | 送信結果ステータス。`ALLOWED_STATUSES`（例: `sent` / `failed` / `pending`）で制約。サマリの `by_status` も全キー 0 埋め。 |
| **summary エンドポイント（`/api/notifications/summary`）** | フィルタ後の通知件数を `by_channel` / `by_status` で軽量に返す集計 API。 |
| **by_day / by_week / by_hour_of_day / by_day_of_week** | 送信時刻の粒度別バケット集計。バケット定義は Analytics Engine と揃えている。 |
| **一括削除（`DELETE /api/notifications`）** | フィルタ条件に一致する通知をまとめて削除する。誤った全件削除防止のため、`user_id` / `channel` / `status` / `since` / `until` のうち少なくとも 1 つを必須とする。 |

## 5. 時刻・粒度の表記規約

3 サービス共通の時刻仕様。集計エンドポイントは常にこの規約に従う。

| 表記 | 意味 |
|------|------|
| **UTC** | 全ての時刻はサーバ側で UTC に統一。タイムゾーン付き ISO 8601 で入出力。 |
| **ISO 8601** | 日時の入出力フォーマット（例: `2026-09-09T12:34:56Z`）。`?since=` / `?until=` フィルタも ISO 8601 で受け付ける。 |
| **`YYYY-MM-DD`** | UTC の暦日バケットキー。`signups_by_day` / `events_by_day` / `notifications/by_day` で使用。 |
| **`YYYY-Www`** | ISO 週バケットキー（例: `2026-W37`）。週は月曜始まりの ISO 8601 定義に従う。 |
| **`YYYY-MM`** | UTC の月バケットキー。 |
| **`YYYY`** | UTC の年バケットキー。年次 KPI 用。 |
| **`00`〜`23`** | UTC 時刻の hour_of_day バケットキー。曜日・日付を跨いで統合する（例: 「毎日の 09 時台の合算」）。 |
| **`1`〜`7`（ISO 曜日）** | `1`=Mon, `2`=Tue, ..., `7`=Sun。米国式の日曜始まり（0=Sun）は用いない。 |
| **`unknown` バケット** | 集計対象キーがパース不能または欠落した場合のフォールバックバケット。 |

## 6. 環境変数の分類

環境変数は「サービス固有」「サイズ・件数上限」「共通運用」の 3 系統に分けて扱う。全一覧はルート [`README.md`](../README.md) と [`.env.example`](../.env.example) を参照。

| 分類 | 例 | 特徴 |
|------|-----|------|
| **サービス固有** | `USER_API_PORT` / `ANALYTICS_PORT` / `NOTIFICATION_PORT` / `JWT_SECRET` / `TOKEN_EXPIRY_HOURS` / `MIN_PASSWORD_LENGTH` / `MAX_NAME_LENGTH` | 特定サービスの起動・機能挙動に直接紐づく。値の変更はそのサービスの再起動のみで反映。 |
| **サイズ・件数上限** | `MAX_EVENTS` / `MAX_NOTIFICATIONS` / `MAX_BODY_BYTES` / `MAX_REQUEST_BODY` / `USERS_DEFAULT_LIMIT` / `USERS_MAX_LIMIT` | in-memory store 前提のプロトタイプ運用における保護境界。上限を超えると FIFO 退避またはリクエスト拒否。 |
| **共通運用** | `LOG_LEVEL` | 3 サービス共通で参照。大文字小文字を無視し、不正値・空・未指定は `INFO` にフォールバック。`INFO` では `/health` アクセスログを抑止して probe ノイズを除去。`analytics-engine` は `DEBUG` とそれ以外の 2 値のみ解釈する点に注意。 |

---

## 用語追加のガイドライン

- 新しい概念・エンドポイント・環境変数を導入した際は、本ドキュメントの該当節に 1〜3 行で追記する。定義の詳細は各ドキュメントに書き、本ファイルは索引と 1 行定義に留める。
- 既存語彙の意味を変更した場合は、本ドキュメントを先に更新し、他ドキュメント側の記述を揃える PR を続けて出す。
- サービス固有の内部用語は、まずそのサービスの README に置いてから、横断で使われるようになった段階でここへ昇格させる。
