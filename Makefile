.PHONY: test build run docker-build up smoke ready frontend-install frontend-build frontend-dev

test:
	go test ./cmd/... ./internal/...

build:
	go build ./...

run:
	ADDR=:8080 DATA_DIR=./data go run ./cmd/server

docker-build:
	docker build -t excel-ai-analysis:local .

up:
	docker compose up --build

smoke:
	bash ./scripts/smoke.sh

ready:
	curl -fsS http://127.0.0.1:8080/readyz

frontend-install:
	cd frontend && npm install

frontend-build:
	cd frontend && npm run build

frontend-dev:
	cd frontend && npm run dev -- --host 0.0.0.0 --port 4173
