VERSION ?= 1.0.0
LDFLAGS := -s -w -X github.com/enterprise/aipet/internal/version.Version=$(VERSION)
BIN := bin

.PHONY: all build install test vet fmt clean run daemon status release

all: build

build: ## Build the aipet binary
	@mkdir -p $(BIN)
	go build -ldflags "$(LDFLAGS)" -o $(BIN)/aipet ./cmd/aipet
	@echo "built $(BIN)/aipet (v$(VERSION))"

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

release: ## Cross-compile release binaries into bin/release
	@mkdir -p $(BIN)/release
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/release/aipet-darwin-arm64 ./cmd/aipet
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/release/aipet-darwin-amd64 ./cmd/aipet
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/release/aipet-linux-arm64 ./cmd/aipet
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/release/aipet-linux-amd64 ./cmd/aipet
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/release/aipet-windows-amd64.exe ./cmd/aipet
	cd $(BIN)/release && shasum -a 256 aipet-* > checksums.txt
	@echo "release artifacts in $(BIN)/release (v$(VERSION))"

clean:
	rm -rf $(BIN)

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
	  awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
