.PHONY: up down test test-python test-go test-ts lint lint-python lint-go lint-ts build health clean

# ---------------------------------------------------------------------------
# `make health` の設定。既定値は docker-compose.yml / .env.example と一致させ、
# 非既定ポートで動かしている環境でも `PORT=xxxx make health` で参照できるように
# `?=` で環境変数からの上書きを許可する。
# ---------------------------------------------------------------------------
USER_API_PORT ?= 5001
ANALYTICS_PORT ?= 5002
NOTIFICATION_PORT ?= 5003
# 1 サービスあたりの HTTP タイムアウト (秒)。オンコール初動の一次診断として
# 使う想定なので、既定値は敢えて短くしてハングを避ける。長時間のヘルス確認は
# 監視系側で行う。
HEALTH_TIMEOUT ?= 3

up:
	docker compose up --build -d

down:
	docker compose down

build:
	docker compose build

test: test-python test-go test-ts

test-python:
	cd services/user-api && pip install -r requirements.txt -q && pytest -v

test-go:
	cd services/analytics-engine && go test -v ./...

test-ts:
	cd services/notification-service && npm install && npm test

lint: lint-python lint-go lint-ts

lint-python:
	cd services/user-api && flake8 .

lint-go:
	cd services/analytics-engine && go vet ./...

lint-ts:
	cd services/notification-service && npm install && npx eslint src/

# ---------------------------------------------------------------------------
# `make health` 内部ヘルパー。
#   $(1) = サービス表示名 / $(2) = ポート番号
# curl の挙動:
#   -s        進捗バーを抑止
#   -S        -s と併用して失敗時のみエラーメッセージを stderr に出す
#   --max-time 全体タイムアウト。dead network でハングしない保険
# python3 -m json.tool が入っていないホストでは raw 出力にフォールバックする。
# 1 サービスが unreachable でも `exit 0` で握って他サービスの探査を続ける。
# ---------------------------------------------------------------------------
define health_check
	@echo "$(1) (:$(2))"
	@out=$$(curl -sS --max-time $(HEALTH_TIMEOUT) http://localhost:$(2)/health 2>&1); rc=$$?; \
	if [ $$rc -ne 0 ] || [ -z "$$out" ]; then \
	    echo "  (unreachable: curl exit $$rc)"; \
	else \
	    printf '%s' "$$out" | python3 -m json.tool 2>/dev/null || printf '  %s\n' "$$out"; \
	fi
endef

health:
	$(call health_check,User API,$(USER_API_PORT))
	$(call health_check,Analytics Engine,$(ANALYTICS_PORT))
	$(call health_check,Notification Service,$(NOTIFICATION_PORT))

clean:
	docker compose down -v --rmi local
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	rm -rf services/notification-service/node_modules services/notification-service/dist
