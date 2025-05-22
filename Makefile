.PHONY: test build run docker-build up smoke

test:
	go test ./...

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
