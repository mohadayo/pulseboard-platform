# Troubleshooting Guide

PulseBoard Platform を動かしていて詰まったときに最初に参照するドキュメント。
`README.md` は「正常系の使い方」を、この `TROUBLESHOOTING.md` は「異常系の切り分け」を担当する。

新しい症状に当たった場合は、対応する再現手順・原因・回避策をこのファイルに追記して次の人が同じ穴に落ちないようにすること。

---

## 目次

1. [まず確認する 4 点](#まず確認する-4-点)
2. [Docker & docker compose](#docker--docker-compose)
3. [環境変数と `.env`](#環境変数と-env)
4. [User API (`:5001`)](#user-api-5001)
5. [Analytics Engine (`:5002`)](#analytics-engine-5002)
6. [Notification Service (`:5003`)](#notification-service-5003)
7. [テストと Lint](#テストと-lint)
8. [手動セットアップ (Docker を使わない場合)](#手動セットアップ-docker-を使わない場合)
9. [それでも解決しないとき](#それでも解決しないとき)

---

## まず確認する 4 点

障害切り分けに入る前に、以下 4 点を先に確認するとほとんどの「動かない」が数分で判別できる。

1. **`.env` を `.env.example` からコピーしたか**
   ```bash
   cp .env.example .env
   ```
   `docker-compose.yml` は `${VAR:-default}` フォールバックを持つので `.env` 無しでも一応起動するが、`JWT_SECRET` などが既定値になり後段のトラブルの温床になる。
2. **3 サービスすべてのヘルスチェックが通っているか**
   ```bash
   make health
   ```
   `make health` は `/health` に順番に `curl` して JSON を整形表示する。1 つでも接続拒否なら該当サービスのログを最優先で確認する。
3. **ポート 5001 / 5002 / 5003 が空いているか**
   ```bash
   lsof -iTCP:5001 -sTCP:LISTEN
   lsof -iTCP:5002 -sTCP:LISTEN
   lsof -iTCP:5003 -sTCP:LISTEN
   ```
   ホスト側で別プロセスが掴んでいる場合は後述の「ポート衝突」を参照。
4. **`docker compose ps` の `STATUS` 列**
   `unhealthy` / `Restarting` / `Exited (1)` などで一次判別する。`Exit 137` は OOM Killed の可能性がある。

---

## Docker & docker compose

### 症状: `make up` 後にすぐ `unhealthy` になる

`docker-compose.yml` の 3 サービスすべてに以下の `healthcheck` が付いている:

```yaml
healthcheck:
  interval: 10s
  timeout: 5s
  retries: 3
  start_period: 5s
```

`start_period: 5s` は**猶予期間**であって「起動保証」ではない。Python (Flask) や TypeScript (tsx) の初回起動、`npm install` が走る初回ビルドでは 5 秒では立ち上がらないことがある。**まず 30 〜 60 秒待ってから再度 `docker compose ps` を確認する**。それでも `unhealthy` なら:

```bash
docker compose logs --tail=100 user-api
docker compose logs --tail=100 analytics-engine
docker compose logs --tail=100 notification-service
```

でエラーメッセージを確認する。

### 症状: `Bind for 0.0.0.0:5001 failed: port is already allocated`

ホスト側のポートが埋まっている。`.env` で衝突しない値に上書きする:

```bash
# .env
USER_API_PORT=15001
ANALYTICS_PORT=15002
NOTIFICATION_PORT=15003
```

`docker-compose.yml` は `"${USER_API_PORT:-5001}:5001"` のように**ホスト側のみ上書き**する形式なので、コンテナ内部のポートは 5001 / 5002 / 5003 のまま。API を叩く URL のポート番号だけを差し替えればよい。

### 症状: コードを直したのに古い挙動のままになる

`docker compose up` はイメージを自動で作り直さない。`make up` は `docker compose up --build -d` を呼ぶので通常は問題ないが、キャッシュが強く効いているときは:

```bash
make down
docker compose build --no-cache
make up
```

### 症状: `docker: Cannot connect to the Docker daemon`

Docker Desktop / Docker Engine が起動していない。`docker info` を実行し `Cannot connect...` が返るなら Docker 側の問題で、リポジトリのコード変更では直らない。

### 症状: ボリューム・イメージを完全に掃除したい

```bash
make clean
```

`docker compose down -v --rmi local` に加えて Python の `__pycache__` と Node の `node_modules` / `dist` も消えるので、初期状態から再現テストしたいときに使う。

---

## 環境変数と `.env`

### `JWT_SECRET` を必ず変更する

既定値は `.env.example` にあるとおり `change-me-in-production` (compose では `pulseboard-dev-secret`)。ローカル開発では動作するが、共有環境・本番相当環境では**必ず**十分に長いランダム値に変更する。

```bash
# 例: openssl でランダムに生成
openssl rand -hex 32
```

### `LOG_LEVEL` は 3 サービスで意味が微妙に違う

`README.md` の環境変数表にあるとおり、`LOG_LEVEL` は `DEBUG` / `INFO` / `WARN` / `ERROR` を受け付けるが、**Analytics Engine は `DEBUG` かそれ以外の 2 値しか区別しない**。詳細ログが Go 側で出ないときはこれが原因。

いずれのサービスも、`INFO` (既定) では `/health` のアクセスログが抑止される。K8s の readinessProbe / liveness probe を有効にすると `INFO` のままでもログはノイズにならない設計。

### `MAX_EVENTS` / `MAX_NOTIFICATIONS` の FIFO 破棄は仕様

- Analytics Engine: `MAX_EVENTS=10000` (既定) を超えたイベントは**古いものから破棄**される。
- Notification Service: `MAX_NOTIFICATIONS=10000` (既定) を超えた通知は**古いものから FIFO 破棄**、`0` 以下で無制限になる。

「登録したはずのイベント / 通知が消える」場合、まずこの閾値を疑う。永続化層を持たないインメモリ実装なので、コンテナ再起動でも同様にゼロクリアされる。

---

## User API (`:5001`)

### 症状: `POST /api/users/register` が `400`

以下のいずれかを満たしていない可能性が高い:

- `email` が正規表現による形式チェックを通っていない
- `password` の長さが `MIN_PASSWORD_LENGTH` (既定 6) 未満
- `email` の大文字小文字違いで既に登録済み (`Foo@x.com` と `foo@x.com` は同一アカウント)

### 症状: `GET /api/users/me` が `401`

`Authorization: Bearer <token>` ヘッダが付いていない、または `JWT_SECRET` を変更したのに古いトークンを使っている。ログイン直後に返る `token` を必ず再取得すること。`TOKEN_EXPIRY_HOURS` (既定 24) を超過していないかも合わせて確認する。

### 症状: `DELETE /api/users/me` が `401`

このエンドポイントは JWT だけでは削除を許可せず、リクエストボディに `current_password` の再入力を要求する仕様。`POST /api/users/me/password` と同じ二段階確認パターンで、トークン漏洩時の誤削除を防ぐためのもの。詳細は `README.md` の該当セクションを参照。

### 症状: `GET /api/users?limit=...` が 400 になる

`USERS_MAX_LIMIT` (既定 200) を超えた `limit` は拒否される。バッチ処理で全件舐めたい場合は `offset` を加算しつつ複数回に分けて取得する。件数だけ知りたい場合は `GET /api/users/count` を使うと `limit` / `offset` を消費せずに済む。

---

## Analytics Engine (`:5002`)

### 症状: `POST /api/analytics/track` が `413 Request Entity Too Large`

リクエストボディが `MAX_BODY_BYTES` (既定 1 MiB = `1048576`) を超えている。ペイロードを分割するか `.env` で上限を引き上げる:

```bash
MAX_BODY_BYTES=5242880  # 5 MiB
```

### 症状: 直前に track したイベントが `GET /api/analytics/events` に出てこない

- `MAX_EVENTS` を超えて FIFO 破棄された可能性 (前述)
- コンテナが再起動されている (インメモリ実装なので永続化されない)
- そもそも 202 / 200 以外のレスポンスが返っていた可能性 — `curl -v` でステータスコードを確認する

### 症状: 集計 API (`events_by_*`) のレスポンスが空

`min_event_count` / `since` / `until` のフィルタが強すぎて 0 件になっているケースが多い。まずは絞り込みなしで `GET /api/analytics/events` の全件を確認し、想定通りイベントが蓄積されているかから切り分ける。

---

## Notification Service (`:5003`)

### 症状: `POST /api/notifications/send` が `413 PayloadTooLargeError`

`MAX_REQUEST_BODY` (既定 `256kb`) を超過している。`express.json` の `limit` パラメータにそのまま渡される値で、[`bytes`](https://www.npmjs.com/package/bytes) パッケージの記法 (`256kb`, `1mb`, `2mb` など) が使える。

```bash
MAX_REQUEST_BODY=2mb
```

### 症状: `GET /api/notifications` が想定より少ない件数を返す

`MAX_NOTIFICATIONS` (既定 10000) を超えた分は FIFO で破棄される。`0` 以下を設定すると上限なしで動作するが、メモリ消費に注意。

### 症状: `channel` のバリデーションで弾かれる

`channel` は `email` / `sms` / `push` のいずれか。それ以外の値は 400 で拒否される。将来のチャネル追加はサービス本体の変更が必要で、環境変数では制御できない。

---

## テストと Lint

### 全部まとめて実行

```bash
make test   # Python + Go + TypeScript の順で実行、途中で失敗すると停止
make lint   # flake8 + go vet + eslint
```

### 言語ごとに個別に切り出す

CI ログの再現や、直したいサービスだけを回したい場合:

```bash
make test-python   # cd services/user-api && pytest -v
make test-go       # cd services/analytics-engine && go test -v ./...
make test-ts       # cd services/notification-service && npm test
```

Lint も同様:

```bash
make lint-python   # flake8 --max-line-length=120
make lint-go       # go vet ./...
make lint-ts       # npx eslint src/
```

### 症状: `test-python` が `ModuleNotFoundError`

`make test-python` は毎回 `pip install -r requirements.txt -q` を実行するが、システムの Python 環境を汚したくない場合は先に venv を作ってから呼ぶ:

```bash
python3 -m venv .venv
source .venv/bin/activate
make test-python
```

### 症状: `test-ts` が `npm ERR!` で失敗する

`services/notification-service/node_modules` が壊れていることが多い。丸ごと消してからリトライ:

```bash
rm -rf services/notification-service/node_modules
make test-ts
```

### 症状: `lint-go` が古い vet 結果を返す

Go の `vet` はキャッシュが強い。`go clean -cache` を実行してから再度 `make lint-go` する。

---

## 手動セットアップ (Docker を使わない場合)

対応ランタイムは `.tool-versions` で管理している。バージョンを揃えるのがトラブルを避ける近道:

- Python 3.12+
- Go 1.22+
- Node.js 20+

`asdf` / `mise` を使っていれば `asdf install` / `mise install` で一発で揃う。バージョン不一致のときは `python3 --version` / `go version` / `node --version` を疑う。

### ポート占有の避け方

`docker compose` を使わないなら `.env` の `USER_API_PORT` / `ANALYTICS_PORT` / `NOTIFICATION_PORT` は環境変数として直接 export しても効く:

```bash
export USER_API_PORT=15001
cd services/user-api && python app.py
```

---

## それでも解決しないとき

1. `make down && make up` で状態をリセットしてから再現するか確認する (インメモリ実装なので前回の副作用が残らない)
2. `docker compose logs --tail=200 <service>` で該当サービスの直近ログを取得する
3. 上記でも原因が絞れない場合は Issue を立てる:
   - 再現手順
   - 期待動作 / 実際の動作
   - `docker compose ps` の出力
   - 関連する `docker compose logs` の抜粋
   - `.env` の差分 (シークレットはマスク)
4. セキュリティ関連の疑いがある場合は Issue ではなく [`SECURITY.md`](../SECURITY.md) の窓口を使うこと。
