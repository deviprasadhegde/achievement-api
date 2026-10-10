# Makefile — short names for the commands you'll run all the time.
# Run `make` or `make help` to list them. Run from the project root.

# Load local settings from .env (if it exists) and export them to commands.
# .env format: KEY=value, one per line, no quotes, no spaces around "=".
-include .env
export

APP     := achievement-api
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := help
.PHONY: help run build test test-race cover fmt vet lint vuln tidy check clean

help: ## Show this list
	@grep -hE '^[a-zA-Z_-]+:.*?## ' Makefile | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

run: ## Run the API locally (reads .env)
	go run -ldflags "$(LDFLAGS)" ./cmd/api

build: ## Build a static binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(APP) ./cmd/api

test: ## Run unit tests
	go test ./...

test-race: ## Run tests with the race detector
	go test -race ./...

cover: ## Run tests and print total coverage
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1

fmt: ## Format all Go code
	gofmt -s -w .

vet: ## Go's built-in static checks
	go vet ./...

lint: ## Run golangci-lint (config: .golangci.yml)
	golangci-lint run ./...

vuln: ## Check dependencies + stdlib for known vulnerabilities
	govulncheck ./...

tidy: ## Sync go.mod/go.sum with imports and verify checksums
	go mod tidy
	go mod verify

check: vet lint test-race vuln ## Everything CI will run

clean: ## Remove build outputs
	rm -rf bin coverage.out
