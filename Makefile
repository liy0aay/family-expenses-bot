.PHONY: build lint test generate eval run

BIN := bin/server

build:
	go build -o $(BIN) ./cmd/server

lint:
	go tool golangci-lint run

test:
	go test ./...

generate:
	go generate ./...

eval:
	go run -tags eval ./evals

run:
	go run ./cmd/server
