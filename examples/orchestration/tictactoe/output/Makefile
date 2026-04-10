.PHONY: build test coverage clean run

MODULE_ROOT := $(shell pwd)

build:
	go build ./...

run: build
	go run ./cmd/tictactoe

test:
	go test -race -count=1 ./...

test-game:
	go test -race -count=1 ./game/...

test-ai:
	go test -race -count=1 ./ai/...

test-web:
	go test -race -count=1 ./web/...

coverage:
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out

coverage-game:
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./game/...
	go tool cover -func=coverage.out

coverage-ai:
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./ai/...
	go tool cover -func=coverage.out

coverage-web:
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./web/...
	go tool cover -func=coverage.out

bench-game:
	go test -bench=. -benchmem ./game/...

bench-ai:
	go test -bench=. -benchmem ./ai/...

clean:
	rm -f coverage.out
