# Architecture / アーキテクチャ

PulseBoard Platform のシステム構成・データフロー・設計判断を一枚にまとめたドキュメント。
`README.md` は「使い方」、`docs/TROUBLESHOOTING.md` は「異常系の切り分け」、
本ドキュメントは「なぜ現在の構成になっているか」を担当する。

新しく機能を追加する前、または運用構成を判断する前にまず参照する。

---

## 目次

1. [システム全体像](#システム全体像)
2. [サービス境界とデータフロー](#サービス境界とデータフロー)
3. [データ保持と揮発性](#データ保持と揮発性)
4. [設定と環境変数の分類](#設定と環境変数の分類)
5. [デプロイ形態](#デプロイ形態)
6. [ロギングと可観測性](#ロギングと可観測性)
7. [拡張時の指針](#拡張時の指針)
8. [スコープ外](#スコープ外)

---

## システム全体像

3 サービス構成のマイクロサービス風モノレポで、各サービスは独立したプロセス
（ローカルではコンテナ）として起動する。共通の DB / キャッシュ / メッセージバスは持たない。

| サービス | ディレクトリ | 言語 / フレームワーク | ポート | 主な役割 |
|---------|--------------|----------------------|-------|---------|
| User API | `services/user-api` | Python 3.12 / Flask | `5001` | ユーザ登録・JWT 発行・プロフィール管理・登録系集計 |
| Analytics Engine | `services/analytics-engine` | Go 1.22 / net/http | `5002` | イベントトラッキング・時系列 / 周期集計 |
| Notification Service | `services/notification-service` | Node.js 20 / TypeScript / Express | `5003` | 通知送信（email / sms / push）・通知系集計 |

3 サービスとも:

- 単一の実行ファイル（`app.py` / `main.go` / `src/index.ts`）とテストのみで完結
- 外部依存は言語標準ライブラリ + 最小限のフレームワークのみ（DB クライアント無し）
- `/health` エンドポイントを持ち、`make health` から順次疎通確認できる
- `LOG_LEVEL` を尊重する（詳細は [Logging](#ロギングと可観測性) 参照）

---

## サービス境界とデータフロー

### 現状: サービス間 HTTP 呼び出しは無い

`README.md` の mermaid 図には次のような点線が引かれている。

```
UA -.->|User events| AE
AE -.->|Alert triggers| NS
```

この点線は **将来のサービス連携** を示すもので、**現時点の実装では 3 サービス間の
HTTP 呼び出しは一切存在しない**。すべての呼び出しはクライアントから直接、各サービスへ
実線で行われる。

```
Client ──► User API           (:5001)   register / login / me / users / signups_*
Client ──► Analytics Engine   (:5002)   track / stats / events / events_by_*
Client ──► Notification Svc   (:5003)   send / list / by_day / by_week / summary
```

このため、以下のいずれかを検討する場合は本ドキュメントを更新すること。

- User API 上の登録イベントを Analytics Engine に自動転送する
- Analytics Engine の閾値超過を Notification Service にフォワードする
- サービス間の共有シークレット・mTLS を導入する

### なぜ結合していないか

- **単独動作の担保**: 1 サービスだけを再起動しても他 2 サービスの API は稼働し続ける。
- **テストの独立性**: 各サービスのテスト（`pytest` / `go test` / `jest`）はネットワークを
  張らずに完結する。CI ジョブ (`test-python` / `test-go` / `test-typescript`) は
  完全並列で実行できる。
- **クライアント側での orchestration が現実的**: 現在のユースケース（デモ・スケルトン）
  では、クライアント（フロントや別ゲートウェイ）が 3 サービスを直接叩けば十分。

---

## データ保持と揮発性

**3 サービスすべてがインメモリストアで動作する。プロセス再起動でデータは揮発する。**

| サービス | ストア | 上限 | 超過時の挙動 |
|---------|--------|------|-------------|
| User API | `users_db` (`dict[str, User]`) | 無制限 | — |
| Analytics Engine | `events` (`[]Event`) | `MAX_EVENTS` (既定 10000) | 古いイベントから FIFO 破棄 |
| Notification Service | `notifications` (`Notification[]`) | `MAX_NOTIFICATIONS` (既定 10000, `0` 以下で無制限) | 古い通知から FIFO 破棄 |

### 結果として起きること

- `docker compose down` / `make down` でストア全体が失われる。
- `restart: unless-stopped` によりコンテナ単体クラッシュ時も再起動されるが、
  再起動された瞬間から `users_db` / events / notifications はすべて空になる。
- **本番相当の永続化は提供しない**。DB 接続やスナップショットは意図的にスコープ外。

### なぜ永続化していないか

- 本リポジトリの目的は **マルチ言語マイクロサービスの薄いリファレンス実装** であり、
  DB スキーマ設計や migration まで含めると学習・レビュー時のノイズが増える。
- CI と本番の環境差を最小化するため、外部依存 (DB / MQ / Redis) を持ち込まない。
- 永続化が必要になった時点で、サービス単位に別ドキュメント (ADR) を追加し、
  スキーマ・マイグレーションポリシー・バックアップ戦略を明記する。

---

## 設定と環境変数の分類

`.env.example` および `docker-compose.yml` に登場する環境変数は、役割で 4 系統に分類できる。

### 1. ネットワーク（ポート）

| 変数 | 既定 | 対象サービス |
|-----|------|-------------|
| `USER_API_PORT` | `5001` | user-api |
| `ANALYTICS_PORT` | `5002` | analytics-engine |
| `NOTIFICATION_PORT` | `5003` | notification-service |

コンテナ内のリッスンポートは固定（5001 / 5002 / 5003）で、ホスト側の
公開ポートのみが `${VAR:-DEFAULT}` フォールバックで上書き可能。

### 2. 制限（レート・サイズ・保持件数）

| 変数 | 既定 | 対象サービス |
|-----|------|-------------|
| `MIN_PASSWORD_LENGTH` | `6` | user-api |
| `USERS_DEFAULT_LIMIT` | `50` | user-api |
| `USERS_MAX_LIMIT` | `200` | user-api |
| `MAX_EVENTS` | `10000` | analytics-engine |
| `MAX_BODY_BYTES` | `1048576` | analytics-engine |
| `MAX_NOTIFICATIONS` | `10000` | notification-service |
| `MAX_REQUEST_BODY` | `256kb` | notification-service |

### 3. 認可・シークレット

| 変数 | 既定 | 備考 |
|-----|------|------|
| `JWT_SECRET` | `pulseboard-dev-secret` | 本番相当環境では必ず上書きする。デフォルト値は `docs/TROUBLESHOOTING.md` でも警告している |
| `TOKEN_EXPIRY_HOURS` | `24` | JWT の有効期限 |

### 4. ロギング

| 変数 | 既定 | 備考 |
|-----|------|------|
| `LOG_LEVEL` | `INFO` | 3 サービス共通。詳細は [Logging](#ロギングと可観測性) を参照 |

新しい環境変数を追加する場合、次の 4 箇所を必ず同時に更新すること。

- `.env.example`（コメント付きでデフォルト値を記載）
- `docker-compose.yml`（`environment:` セクションに `${VAR:-DEFAULT}` で列挙）
- `README.md` の "Environment Variables" 表
- 本ドキュメントの該当分類表

---

## デプロイ形態

**公式サポートするデプロイ手段は Docker Compose によるローカル起動のみ。**

- エントリポイント: リポジトリルートの `docker-compose.yml` および `Makefile`
- 起動: `make up`（`docker compose up --build -d`）
- 停止: `make down`（`docker compose down`）
- 破棄: `make clean`（`down -v --rmi local` + `__pycache__` / `node_modules` / `dist` 削除）

`docker-compose.yml` は 3 サービスに次を共通で設定している。

- `healthcheck` （interval 10s / timeout 5s / retries 3 / start_period 5s）
- `restart: unless-stopped`
- `environment:` に上記 4 系統の変数を `${VAR:-DEFAULT}` で渡す

### 未提供のもの

- Kubernetes マニフェスト / Helm チャート
- Terraform / IaC
- 本番 CDN・LB・TLS 終端の構成例
- マルチノード / 複数レプリカでの整合性保証（インメモリ store のためレプリカ間で状態が食い違う）

これらは意図的にスコープ外。追加する場合は本セクションに要件と設計を追記する。

---

## ロギングと可観測性

`LOG_LEVEL` は 3 サービスすべてで参照される共通変数だが、解釈する値の範囲は異なる。

| サービス | 解釈する値 | フォールバック |
|---------|-----------|---------------|
| user-api (Python) | `CRITICAL` / `FATAL` / `ERROR` / `WARN` / `WARNING` / `INFO` / `DEBUG` / `NOTSET` | 前後空白 strip + 大文字化後、`_nameToLevel` に無ければ `INFO` |
| analytics-engine (Go) | `DEBUG` / それ以外 2 値 | 大文字小文字無視・未知値は `INFO` 相当 |
| notification-service (TypeScript) | `DEBUG` / `INFO` / `WARN` / `ERROR` | 大文字小文字無視・不正値は `INFO` |

3 サービスに共通する挙動:

- 既定 (`INFO`) では `/health` のアクセスログを抑止する。
  K8s probe / ロードバランサ probe のノイズを除去するため。
- `DEBUG` で `/health` のアクセスログを含めた詳細ログが出る。
- 未指定・空文字・不正値・大文字小文字揺れはすべて `INFO` にフォールバックする
  （fail-safe。プロセス起動を落とさない）。

現時点で **メトリクス出力（Prometheus 形式など）・分散トレーシング・構造化ログ
（JSON ロガー）は未提供**。追加する場合は本セクションを更新する。

---

## 拡張時の指針

### 新しいエンドポイントを追加するとき

1. **命名規約に揃える。** 特に時系列 / 周期集計は 3 サービスで揃えている。
   - 時系列: `by_day` / `by_week` / `by_month`
   - 周期: `by_day_of_week` / `by_hour_of_day`
   - 例: 曜日周期は必ず ISO 8601 曜日 (`1`=Mon 〜 `7`=Sun) を使う。
2. **populated-only 集計方針を守る。** 母集団 0 のバケットは配列に含めない。
   既存のクライアントがこの前提でループしている。
3. **`/:id` 系ルートより前に登録する。** Express / Flask / net/http いずれでも
   `/api/foo/by_day` と `/api/foo/:id` の登録順が誤ると `:id = "by_day"` として
   マッチしてしまう。
4. **README の API 表を更新する。** 3 サービス分の表フォーマットが揃っているので、
   同じ粒度・同じ書式で追記する。
5. **必要に応じ `docs/TROUBLESHOOTING.md` にも項目追加。** 新エンドポイントで
   起きやすい 4xx / 5xx の切り分け手順を残す。

### 新しい環境変数を追加するとき

[設定と環境変数の分類](#設定と環境変数の分類) の末尾に列挙した「4 箇所同時更新」
のチェックリストに従う。

### 新しいサービスを追加するとき

本ドキュメントで想定していない領域なので、追加時に次を必ず本ドキュメントへ反映する。

- [システム全体像](#システム全体像) の表に新サービス行を追加
- [サービス境界とデータフロー](#サービス境界とデータフロー) の図を更新
- [データ保持と揮発性](#データ保持と揮発性) にストア方式を追加
- `docker-compose.yml` / `Makefile` / `.github/workflows/ci.yml` にジョブ追加

---

## スコープ外

以下は現在の実装では扱わない。要件が発生した時点で ADR (Architecture Decision Record)
として本ドキュメントの隣に追加し、本ドキュメントから参照する。

- 永続ストレージ（RDB / KVS / Object Storage）との接続
- サービス間の同期 / 非同期通信（HTTP / gRPC / メッセージキュー）
- マルチテナント・組織単位の isolation
- 監視スタック（Prometheus / OpenTelemetry / Grafana）
- 本番相当の認可（RBAC / ABAC / OAuth 2.0 リソースサーバ）
- レートリミット・WAF・DDoS 対策
- Kubernetes デプロイ・オートスケーリング

---

## 関連ドキュメント

- [README.md](../README.md) — サービス概要・API 表・環境変数一覧
- [CONTRIBUTING.md](../CONTRIBUTING.md) — ブランチ運用・PR フロー
- [docs/TROUBLESHOOTING.md](TROUBLESHOOTING.md) — 症状別の切り分け手順
- [SECURITY.md](../SECURITY.md) — 脆弱性報告経路・脅威モデル
- [CHANGELOG.md](../CHANGELOG.md) — 変更履歴 (Keep a Changelog 形式)
