.PHONY: build lint test generate eval run

BIN := bin/server

build:
	go build -o $(BIN) ./cmd/server

lint:
	go tool golangci-lint run

# -p 1: service and rest tests share one TEST_POSTGRES_DSN database, parallel packages race on TRUNCATE; -count=1 bans the cache
test:
	go test -p 1 -count=1 ./...

generate:
	go generate ./...

eval:
	go run -tags eval ./evals

run:
	go run ./cmd/server
