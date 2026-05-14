.PHONY: up down test test-python test-go test-ts lint build clean

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
	cd services/user-api && flake8 --max-line-length=120 --exclude=__pycache__ app.py

lint-go:
	cd services/analytics-engine && go vet ./...

lint-ts:
	cd services/notification-service && npm install && npx eslint src/

health:
	@echo "User API:"; curl -s http://localhost:5001/health | python3 -m json.tool
	@echo "Analytics Engine:"; curl -s http://localhost:5002/health | python3 -m json.tool
	@echo "Notification Service:"; curl -s http://localhost:5003/health | python3 -m json.tool

clean:
	docker compose down -v --rmi local
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	rm -rf services/notification-service/node_modules services/notification-service/dist
