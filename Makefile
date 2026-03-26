BINARY   := skwad-cli
MODULE   := github.com/lsinghkochava/skwad-cli
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -ldflags "-X main.version=$(VERSION)"

.PHONY: build install test test-race lint clean tidy fmt vet help

build: ## Build the CLI binary
	go build $(LDFLAGS) -o $(BINARY) ./cmd/skwad-cli/

install: ## Install to $GOPATH/bin
	go install $(LDFLAGS) ./cmd/skwad-cli/

test: ## Run tests
	go test ./...

test-race: ## Run tests with race detector
	go test -race ./...

lint: ## Run go vet (and golangci-lint if available)
	go vet ./...
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run || true

tidy: ## Tidy Go modules
	go mod tidy

fmt: ## Format Go source
	gofmt -w -s .

vet: ## Run go vet
	go vet ./...

clean: ## Remove built binary
	rm -f $(BINARY)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
