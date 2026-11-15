.PHONY: help test bench fmt lint mcp run-mcp wasm playground clean

BINARY_DIR := bin
MCP_BINARY := $(BINARY_DIR)/ruleengine-mcp
PLAYGROUND_PORT ?= 8080

help: ## Show available Make targets
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

test: ## Run all unit tests with the race detector
	go test -v -race ./...

bench: ## Run benchmarks across all packages
	go test -bench=. -benchmem ./...

fmt: ## Format all Go source files with gofmt -s
	gofmt -s -w .

lint: fmt ## Verify formatting and run go vet across all packages
	go vet ./...

mcp: ## Build the ruleengine-mcp CLI binary into bin/ruleengine-mcp
	@mkdir -p $(BINARY_DIR)
	go build -trimpath -ldflags="-s -w" -o $(MCP_BINARY) ./cmd/ruleengine-mcp
	@echo "Built $(MCP_BINARY)"

run-mcp: ## Run the MCP server over stdio using testdata/rules.yml
	go run ./cmd/ruleengine-mcp -config ./testdata/rules.yml -env development

wasm: ## Build the WebAssembly playground bundle into playground/dist/
	./playground/build.sh

playground: wasm ## Build the Wasm bundle and serve it locally on PLAYGROUND_PORT (default 8080)
	@echo "Serving playground at http://localhost:$(PLAYGROUND_PORT)"
	python3 -m http.server $(PLAYGROUND_PORT) --directory playground/dist

clean: ## Remove build artifacts (bin/, playground/dist/, *.wasm)
	rm -rf $(BINARY_DIR) playground/dist *.wasm
