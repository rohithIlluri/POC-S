VERSION ?= 0.1.0
LDFLAGS := -s -w -X github.com/enterprise/aipet/internal/feed.Version=$(VERSION)
BIN := bin

.PHONY: all build install test vet fmt clean run daemon status feedsign

all: build

build: ## Build the aipet binary and the feed-signing tool
	@mkdir -p $(BIN)
	go build -ldflags "$(LDFLAGS)" -o $(BIN)/aipet ./cmd/aipet
	go build -o $(BIN)/aipet-feedsign ./cmd/aipet-feedsign
	@echo "built $(BIN)/aipet (v$(VERSION)) and $(BIN)/aipet-feedsign"

install: build ## Install aipet to $GOBIN
	go install -ldflags "$(LDFLAGS)" ./cmd/aipet

test: ## Run the test suite
	go test ./...

vet: ## Static analysis
	go vet ./...

fmt: ## Format all Go files
	gofmt -w .

run: build ## Launch the interactive pet (TUI)
	$(BIN)/aipet

daemon: build ## Run the background daemon in the foreground
	$(BIN)/aipet daemon

status: build ## Collect once and print a summary
	$(BIN)/aipet status

feedsign: build ## Generate a feed signing keypair
	$(BIN)/aipet-feedsign keygen

clean:
	rm -rf $(BIN)

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
