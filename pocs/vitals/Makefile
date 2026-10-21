VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)
BIN := bin

.PHONY: all build install test vet fmt clean help

all: build

build: ## Build the vitals binary
	@mkdir -p $(BIN)
	go build -ldflags "$(LDFLAGS)" -o $(BIN)/vitals ./cmd/vitals
	@echo "built $(BIN)/vitals (v$(VERSION))"

install: ## Install vitals to $GOBIN
	go install -ldflags "$(LDFLAGS)" ./cmd/vitals

test: ## Run the test suite
	go test ./...

vet: ## Static analysis
	go vet ./...

fmt: ## Format all Go files
	gofmt -w .

clean:
	rm -rf $(BIN)

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
