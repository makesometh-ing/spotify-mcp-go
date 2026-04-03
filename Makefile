.PHONY: help build test codegen run docker clean lint

## help: Print this help message
help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'

## build: Compile server and codegen binaries
build:
	go build -o bin/spotify-mcp-go ./cmd/server
	go build -o bin/codegen ./cmd/codegen

## test: Run all tests with race detector
test:
	go test -race ./...

## codegen: Run the code generator
codegen:
	go run ./cmd/codegen/...

## run: Build and start the MCP server
run: build
	./bin/spotify-mcp-go

## docker: Build container image with ko
docker:
	ko build ./cmd/server

## lint: Run golangci-lint (prints install instructions if missing)
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not found. Install: https://golangci-lint.run/welcome/install/"; exit 1; }
	golangci-lint run ./...

## clean: Remove build artifacts
clean:
	rm -rf bin/
