.PHONY: all build test test-race bench clean docker-up docker-down

BINARY_NAME=queueguard
MOCK_NAME=mock-origin
BIN_DIR=bin

all: build

build:
	@echo "Building QueueGuard..."
	go build -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/server
	@echo "Building Mock Origin..."
	go build -o $(BIN_DIR)/$(MOCK_NAME) ./cmd/mock-origin
	@echo "Build complete: $(BIN_DIR)/"

test:
	@echo "Running test suite..."
	go test -v ./...

test-race:
	@echo "Running tests with race detector..."
	go test -race ./...

bench:
	@echo "Running concurrency benchmark..."
	go run ./scripts/benchmark.go -url http://localhost:8000 -users 1000 -concurrency 50

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

clean:
	@rm -rf $(BIN_DIR)

