# flora-agent Makefile

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -X github.com/flora-suite/flora-agent/pkg/version.Version=$(VERSION) \
           -X github.com/flora-suite/flora-agent/pkg/version.Commit=$(COMMIT) \
           -X github.com/flora-suite/flora-agent/pkg/version.BuildDate=$(BUILD_DATE)

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOVET := $(GOCMD) vet
GOMOD := $(GOCMD) mod
GOFMT := gofmt

# Binary name
BINARY_NAME := flora-agent
BINARY_DIR := bin

# Main package
MAIN_PKG := ./cmd/flora-agent

.PHONY: all build clean test test-integration lint fmt vet tidy help
.PHONY: build-linux build-darwin build-windows build-all
.PHONY: install docker

all: build

## Build

build: ## Build for current platform
	$(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BINARY_DIR)/$(BINARY_NAME) $(MAIN_PKG)

build-linux-amd64: ## Build for Linux amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) -ldflags "$(LDFLAGS) -s -w" -o $(BINARY_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PKG)

build-linux-arm64: ## Build for Linux arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) -ldflags "$(LDFLAGS) -s -w" -o $(BINARY_DIR)/$(BINARY_NAME)-linux-arm64 $(MAIN_PKG)

build-linux-armv7: ## Build for Linux armv7
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 $(GOBUILD) -ldflags "$(LDFLAGS) -s -w" -o $(BINARY_DIR)/$(BINARY_NAME)-linux-armv7 $(MAIN_PKG)

build-darwin-amd64: ## Build for macOS amd64
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GOBUILD) -ldflags "$(LDFLAGS) -s -w" -o $(BINARY_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PKG)

build-darwin-arm64: ## Build for macOS amd64/Apple Silicon
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) -ldflags "$(LDFLAGS) -s -w" -o $(BINARY_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PKG)

build-windows-amd64: ## Build for Windows amd64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) -ldflags "$(LDFLAGS) -s -w" -o $(BINARY_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PKG)

build-windows-arm64: ## Build for Windows arm64
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 $(GOBUILD) -ldflags "$(LDFLAGS) -s -w" -o $(BINARY_DIR)/$(BINARY_NAME)-windows-arm64.exe $(MAIN_PKG)

build-all: build-linux-amd64 build-linux-arm64 build-linux-armv7 build-darwin-amd64 build-darwin-arm64 build-windows-amd64 build-windows-arm64 ## Build for all platforms
	@echo "Built binaries:"
	@ls -la $(BINARY_DIR)/

## Development

run: ## Run the agent (development)
	$(GOCMD) run $(MAIN_PKG) run --config configs/agent.example.yaml --log-format text --log-level debug

dev: ## Run with hot reload (requires air)
	air

## Testing

test: ## Run tests
	$(GOTEST) -v -race ./...

test-integration: ## Run tagged integration tests
	$(GOTEST) -v -race -tags=integration ./tests/integration

test-cover: ## Run tests with coverage
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

bench: ## Run benchmarks
	$(GOTEST) -bench=. -benchmem ./...

## Code Quality

lint: ## Run linter (requires golangci-lint)
	golangci-lint run

fmt: ## Format code
	$(GOFMT) -s -w .

vet: ## Run go vet
	$(GOVET) ./...

check: fmt vet lint test ## Run all checks

## Dependencies

tidy: ## Tidy go modules
	$(GOMOD) tidy

deps: ## Download dependencies
	$(GOMOD) download

## Installation

install: build ## Install to /usr/local/bin
	sudo install -m 755 $(BINARY_DIR)/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
	@echo "Installed $(BINARY_NAME) to /usr/local/bin/"

install-service: ## Install systemd service (Linux)
	sudo cp deploy/systemd/flora-agent.service /etc/systemd/system/
	sudo mkdir -p /etc/flora-agent
	sudo cp configs/agent.example.yaml /etc/flora-agent/agent.yaml
	sudo mkdir -p /var/lib/flora-agent
	sudo systemctl daemon-reload
	@echo "Service installed. Edit /etc/flora-agent/agent.yaml and run:"
	@echo "  sudo systemctl enable --now flora-agent"

## Docker

docker-build: ## Build Docker image
	docker build -t flora-agent:$(VERSION) -f deploy/docker/Dockerfile .

docker-push: ## Push Docker image
	docker tag flora-agent:$(VERSION) ghcr.io/flora-suite/flora-agent:$(VERSION)
	docker push ghcr.io/flora-suite/flora-agent:$(VERSION)

## Cleanup

clean: ## Remove build artifacts
	rm -rf $(BINARY_DIR)
	rm -f coverage.out coverage.html

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# Default target
.DEFAULT_GOAL := help
