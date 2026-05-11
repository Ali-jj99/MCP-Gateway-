.PHONY: build run test lint migrate docker-up docker-down clean

BIN := bin/gateway
DATABASE_URL ?= postgres://mcp:mcp_secret@localhost:5432/mcp_gateway?sslmode=disable

build:
	go build -o $(BIN) ./cmd/gateway

run: build
	DATABASE_URL=$(DATABASE_URL) ./$(BIN)

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

migrate:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/gateway

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down

clean:
	rm -rf bin/
