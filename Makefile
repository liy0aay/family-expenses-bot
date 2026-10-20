.PHONY: build lint test generate eval run

BIN := bin/server

EVALS_POSTGRES_DSN ?= postgres://postgres:postgres@localhost:55450/expenses?sslmode=disable

build:
	go build -o $(BIN) ./cmd/server

lint:
	go tool golangci-lint run

test:
	go test -p 1 -count=1 ./...

generate:
	go generate ./...

eval:
	EVALS_POSTGRES_DSN=$(EVALS_POSTGRES_DSN) go run -tags eval ./evals

run:
	set -a && . ./.env && set +a && go run ./cmd/server
