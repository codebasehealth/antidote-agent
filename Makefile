.PHONY: build build-all clean test run fmt lint

# Build variables
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

# Default target
all: build

# Build for current platform
build:
	go build $(LDFLAGS) -o bin/antidote-agent ./cmd/antidote-agent

# Build for all platforms
build-all:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/antidote-agent-linux-amd64 ./cmd/antidote-agent
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/antidote-agent-linux-arm64 ./cmd/antidote-agent
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/antidote-agent-darwin-amd64 ./cmd/antidote-agent
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/antidote-agent-darwin-arm64 ./cmd/antidote-agent
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/antidote-agent-windows-amd64.exe ./cmd/antidote-agent

# Clean build artifacts
clean:
	rm -rf bin/

# Run tests
test:
	go test -v ./...

# Run the agent
run: build
	./bin/antidote-agent --config=./antidote.example.yml

# Format code
fmt:
	go fmt ./...

# Lint code
lint:
	golangci-lint run

# Install dependencies
deps:
	go mod download
	go mod tidy

# Development run with hot reload (requires air)
dev:
	air -c .air.toml
