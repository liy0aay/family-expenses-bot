.PHONY: build lint test generate eval run

BIN := bin/server

build:
	go build -o $(BIN) ./cmd/server

lint:
	go tool golangci-lint run

test:
	go test -p 1 -count=1 ./...

generate:
	go generate ./...

eval:
	go run -tags eval ./evals

run:
	set -a && . ./.env && set +a && go run ./cmd/server
