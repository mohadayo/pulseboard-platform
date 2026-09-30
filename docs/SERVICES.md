# サービス一覧（`docs/SERVICES.md`）

PulseBoard プラットフォームを構成する 3 つのマイクロサービスの、言語・ポート・エントリファイル・テスト構成・主要環境変数を 1 ページに集約したリファレンス。「今どのサービスがどの言語・ポートで動いているか」「オンコール中にどこを叩けばよいか」を素早く確認するための入り口として利用する。

設計判断・データフローの詳細は [`ARCHITECTURE.md`](./ARCHITECTURE.md)、用語の定義は [`GLOSSARY.md`](./GLOSSARY.md) を参照。API の詳細仕様はルート [`README.md`](../README.md) の API Reference を参照。

## サービス概要表

| サービス | 責務 | 言語 / フレームワーク | ポート | エントリ | テスト |
|---------|------|-----------------------|-------|--------|-------|
| [`user-api`](../services/user-api/) | ユーザ登録・ログイン（JWT 発行）・プロフィール管理・登録件数の各種集計 | Python 3.12+ / Flask | `5001` | [`app.py`](../services/user-api/app.py) | pytest（[`test_app.py`](../services/user-api/test_app.py) ほか） |
| [`analytics-engine`](../services/analytics-engine/) | イベント収集、リアルタイム統計・時系列集計、event_type / user 別の集計 | Go 1.22+ / `net/http` | `5002` | [`main.go`](../services/analytics-engine/main.go) | `go test`（[`main_test.go`](../services/analytics-engine/main_test.go) ほか） |
| [`notification-service`](../services/notification-service/) | 複数チャネル通知（email / SMS / push）の送信・一覧・集計 | TypeScript 5+ / Express（Node.js 20+） | `5003` | [`src/index.ts`](../services/notification-service/src/index.ts) | Jest（[`src/index.test.ts`](../services/notification-service/src/index.test.ts)） |

## サービス間の連携

Client から各サービスへは HTTP で直接アクセスする。サービス間の破線矢印（`User API` → `Analytics Engine`、`Analytics Engine` → `Notification Service`）はルート `README.md` の Mermaid 図に示すとおり、将来的な連携ポイントを示すもので、現在の実装ではサービス間の RPC 呼び出しは行っていない（各サービスは in-memory ストアで独立して動作する）。

詳細なデータフロー・連携方式・拡張時の指針は [`ARCHITECTURE.md`](./ARCHITECTURE.md) を参照。

## 主要環境変数（サービス別）

各サービスの listen ポートとログレベルはすべて共通の `LOG_LEVEL` で制御する（大文字小文字は区別しない）。サービス固有の設定のみを以下に抜粋する。全変数の一覧はルート [`README.md`](../README.md) の Environment Variables 節、および [`.env.example`](../.env.example) を参照。

### `user-api`

| 変数 | 既定 | 用途 |
|------|-----|------|
| `USER_API_PORT` | `5001` | listen ポート |
| `JWT_SECRET` | `pulseboard-dev-secret` | JWT 署名鍵（本番では必ず上書き） |
| `TOKEN_EXPIRY_HOURS` | `24` | 発行した JWT の有効期限（時間） |
| `MIN_PASSWORD_LENGTH` | `6` | 登録・パスワード変更時の最小文字数 |
| `USERS_DEFAULT_LIMIT` / `USERS_MAX_LIMIT` | `50` / `200` | `GET /api/users` の既定・上限ページサイズ |

### `analytics-engine`

| 変数 | 既定 | 用途 |
|------|-----|------|
| `ANALYTICS_PORT` | `5002` | listen ポート |
| `MAX_EVENTS` | `10000` | in-memory に保持するイベント数の上限（超過時は FIFO で古い順に破棄） |
| `MAX_BODY_BYTES` | `1048576` | `POST /api/analytics/track` のリクエストボディサイズ上限 |

### `notification-service`

| 変数 | 既定 | 用途 |
|------|-----|------|
| `NOTIFICATION_PORT` | `5003` | listen ポート |
| `MAX_NOTIFICATIONS` | `10000` | in-memory に保持する通知数の上限（`0` 以下で無制限、それ以外は FIFO eviction） |
| `MAX_REQUEST_BODY` | `256kb` | `express.json` のリクエストボディサイズ上限 |

## データ保持方式

現在の実装ではいずれのサービスも **プロセスメモリ上のストアのみ** で動作し、再起動でデータは失われる。永続化・複数レプリカ間での共有は行っていない。詳細な設計理由と将来的な永続化の指針は [`ARCHITECTURE.md`](./ARCHITECTURE.md) を参照。

## ヘルスチェック

全サービスが `GET /health` を持ち、`docker-compose.yml` からもこのエンドポイントを 10 秒間隔で呼び出している（`user-api` は `python -c urllib.request`、他 2 サービスは `wget --spider`）。`LOG_LEVEL=INFO`（既定）では `/health` のアクセスログを抑止し、Kubernetes / ロードバランサの probe が生成するノイズを除去する仕様。

- `curl -sf http://localhost:5001/health`（`user-api`）
- `curl -sf http://localhost:5002/health`（`analytics-engine`）
- `curl -sf http://localhost:5003/health`（`notification-service`）

`make health` で 3 サービスをまとめて確認できる。

## Docker / コンテナ構成

- 各サービスは `services/<name>/Dockerfile` で独立してビルドされる。3 サービスすべて非 root ユーザで動作する（`#146` 対応済み）。
- ローカルでは [`docker-compose.yml`](../docker-compose.yml) から `make up` で一括起動できる。ポートは `${USER_API_PORT:-5001}` のように環境変数で差し替え可能。
- 各サービスは `.dockerignore` を持ち、テストコード・ドット設定ファイルはイメージに含めない。

## 新しいサービスを追加する時の更新箇所

サービスを追加する際は、少なくとも以下を同一 PR、または連続する PR 群で更新すること:

1. `services/<new-service>/` にコード・`Dockerfile`・`.dockerignore` を追加
2. `docker-compose.yml` にサービスブロック（ポート・環境変数・ヘルスチェック）を追加
3. `.env.example` に既定値付きの環境変数を追加
4. `Makefile` の `test-*` / `lint-*` / `health` ターゲットを更新
5. `.github/workflows/ci.yml` に対応するジョブを追加
6. ルート `README.md` の Services 表・API Reference・Project Structure を更新
7. 本ドキュメント（`docs/SERVICES.md`）の概要表・環境変数節を更新
8. 必要に応じて `docs/ARCHITECTURE.md` のデータフロー図と [`GLOSSARY.md`](./GLOSSARY.md) を更新

## 関連ドキュメント

- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — サービス構成・データフロー・拡張時の指針
- [`GLOSSARY.md`](./GLOSSARY.md) — サービス横断で使われる用語のリファレンス
- [`RUNBOOK.md`](./RUNBOOK.md) — 起動・停止・再起動などの運用手順
- [`OBSERVABILITY.md`](./OBSERVABILITY.md) — ログ・メトリクス・SLO の運用方針
- [`TROUBLESHOOTING.md`](./TROUBLESHOOTING.md) — 起動・接続時のよくあるエラーの切り分け
