# PulseBoard Platform

Real-time microservices dashboard platform with user management, analytics tracking, and multi-channel notification delivery.

## Architecture

```mermaid
graph TB
    Client[Client Application]
    
    subgraph PulseBoard Platform
        UA[User API<br/>Python/Flask<br/>:5001]
        AE[Analytics Engine<br/>Go<br/>:5002]
        NS[Notification Service<br/>TypeScript/Express<br/>:5003]
    end
    
    Client -->|Register/Login/Profile| UA
    Client -->|Track Events/Stats| AE
    Client -->|Send/List Notifications| NS
    
    UA -.->|User events| AE
    AE -.->|Alert triggers| NS
```

## Services

| Service | Language | Port | Description |
|---------|----------|------|-------------|
| User API | Python (Flask) | 5001 | User registration, authentication (JWT), profile management |
| Analytics Engine | Go (net/http) | 5002 | Event tracking, real-time statistics, event aggregation |
| Notification Service | TypeScript (Express) | 5003 | Multi-channel notifications (email, SMS, push) |

## Quick Start

### Prerequisites

- Docker & Docker Compose
- Or: Python 3.12+, Go 1.22+, Node.js 20+

### Using Docker Compose

```bash
cp .env.example .env
make up
```

### Manual Setup

```bash
# User API
cd services/user-api
pip install -r requirements.txt
python app.py

# Analytics Engine
cd services/analytics-engine
go run main.go

# Notification Service
cd services/notification-service
npm install
npm run dev
```

## API Reference

### User API (`:5001`)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| POST | `/api/users/register` | Register a new user |
| POST | `/api/users/login` | Login and receive JWT |
| GET | `/api/users/me` | Get current user profile (requires JWT) |
| DELETE | `/api/users/me` | アカウント自己退会（要 JWT + `current_password` 再入力。削除後は同 email で再登録可能、二重 DELETE は 404、`users_db` から該当エントリのみ削除し他ユーザに影響しない） |
| GET | `/api/users` | List users（`?limit=` / `?offset=` ページネーション、`?q=` 部分一致、`?sort=` `?order=`、`?since=` / `?until=` で `created_at` の ISO 8601 範囲フィルタ） |

> Email addresses are normalized (trimmed + lowercased) on register/login, so
> `Foo@x.com` and `foo@x.com` map to the same account. Registration validates
> the email format and enforces a minimum password length (`MIN_PASSWORD_LENGTH`).

**Register:**
```bash
curl -X POST http://localhost:5001/api/users/register \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"secret","name":"Alice"}'
```

**Login:**
```bash
curl -X POST http://localhost:5001/api/users/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"secret"}'
```

**Delete account (self-service):**
```bash
# JWT を Authorization ヘッダで、再入力したパスワードを JSON ボディで送る。
# 成功時 `{"deleted": true}` (200)。誤入力は 401、二重 DELETE は 404 を返す。
curl -X DELETE http://localhost:5001/api/users/me \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"current_password":"secret"}'
```

JWT のみでは削除を許可せず、`current_password` の再入力を要求する。これはトークン漏洩時の誤削除・第三者による削除を防ぐためで、`POST /api/users/me/password` と同じ二段階確認パターン。削除後は既存 JWT を明示的に失効させる機構は無いが、`/me` 系のエンドポイントは `users_db.get` で 404 を返すため実害は無い（同 email での再登録は新しい `id` が割り当てられる）。

### Analytics Engine (`:5002`)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| POST | `/api/analytics/track` | Track an event |
| GET | `/api/analytics/stats` | Get aggregated statistics |
| GET | `/api/analytics/event_types` | event_type 別の集計 (event_count / distinct_users / first_event_at / last_event_at) を `sort` / `order` / `limit` / `offset` で取得 |
| GET | `/api/analytics/users` | user_id 別の集計 (event_count / distinct_event_types / first_event_at / last_event_at) を `sort` / `order` / `limit` / `offset` で取得 |
| GET | `/api/analytics/events` | List all events |
| GET | `/api/analytics/events/{id}` | Get a single event by id (404 if not found) |
| DELETE | `/api/analytics/events` | Delete events by `user_id` / `event_type` / `before` filters |
| DELETE | `/api/analytics/events/{id}` | Delete a single event by id. レスポンスに削除前のイベント内容を含め、別 GET なしで監査ログに残せる。存在しない id は 404 |

**Track Event:**
```bash
curl -X POST http://localhost:5002/api/analytics/track \
  -H "Content-Type: application/json" \
  -d '{"user_id":"u1","event_type":"page_view","payload":"homepage"}'
```

### Notification Service (`:5003`)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| POST | `/api/notifications/send` | Send a notification |
| GET | `/api/notifications` | List notifications (optional `?user_id=`) |
| GET | `/api/notifications/:id` | Get notification by ID |

**Send Notification:**
```bash
curl -X POST http://localhost:5003/api/notifications/send \
  -H "Content-Type: application/json" \
  -d '{"user_id":"u1","channel":"email","title":"Welcome","message":"Hello!"}'
```

## Development

### Running Tests

```bash
make test          # Run all tests
make test-python   # Python tests only
make test-go       # Go tests only
make test-ts       # TypeScript tests only
```

### Linting

```bash
make lint          # Run all linters
make lint-python   # flake8
make lint-go       # go vet
make lint-ts       # eslint
```

### Health Check

```bash
make health        # Check all services
```

## Environment Variables

See [`.env.example`](.env.example) for all available configuration options.

| Variable | Default | Description |
|----------|---------|-------------|
| `USER_API_PORT` | `5001` | User API listen port |
| `JWT_SECRET` | `pulseboard-dev-secret` | JWT signing secret |
| `TOKEN_EXPIRY_HOURS` | `24` | JWT token expiry in hours |
| `MIN_PASSWORD_LENGTH` | `6` | Minimum password length on registration |
| `USERS_DEFAULT_LIMIT` | `50` | Default page size for `GET /api/users` |
| `USERS_MAX_LIMIT` | `200` | Maximum page size for `GET /api/users` |
| `ANALYTICS_PORT` | `5002` | Analytics Engine listen port |
| `MAX_EVENTS` | `10000` | Max in-memory events retained (FIFO eviction) |
| `MAX_BODY_BYTES` | `1048576` | Max request body size for event tracking |
| `NOTIFICATION_PORT` | `5003` | Notification Service listen port |
| `MAX_NOTIFICATIONS` | `10000` | Notification Service: max in-memory notifications (FIFO eviction、`0` 以下で無制限) |
| `MAX_REQUEST_BODY` | `256kb` | Notification Service: `express.json` のリクエストボディサイズ上限 |
| `LOG_LEVEL` | `INFO` | Log verbosity level (`DEBUG` / `INFO` / `WARN` / `ERROR`). `user-api` と `notification-service` の双方で参照される。`INFO`（既定）では `/health` のアクセスログが抑止され、K8s probe / ロードバランサ probe のノイズログを除去する。`DEBUG` で詳細ログを有効化。大文字小文字を無視し、不正値・空・未指定は `INFO` にフォールバック |

## CI/CD

GitHub Actions workflow runs on push/PR to `main`:
1. Python: install deps, flake8 lint, pytest
2. Go: go vet, go test
3. TypeScript: npm ci, eslint, jest
4. Docker: compose build (after all tests pass)

> **Note:** The `.github/workflows/ci.yml` file may need to be added manually after initial setup due to GitHub API restrictions on the `.github/` directory.

## Project Structure

```
pulseboard-platform/
├── docker-compose.yml
├── Makefile
├── .env.example
├── .gitignore
├── README.md
└── services/
    ├── user-api/           # Python/Flask
    │   ├── app.py
    │   ├── test_app.py
    │   ├── requirements.txt
    │   └── Dockerfile
    ├── analytics-engine/   # Go
    │   ├── main.go
    │   ├── main_test.go
    │   ├── go.mod
    │   └── Dockerfile
    └── notification-service/  # TypeScript/Express
        ├── src/
        │   ├── index.ts
        │   └── index.test.ts
        ├── package.json
        ├── tsconfig.json
        ├── jest.config.js
        └── Dockerfile
```

## License

MIT
