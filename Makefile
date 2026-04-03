.PHONY: help build test codegen run docker clean

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

## test: Run all tests
test:
	go test ./...

## codegen: Run the code generator
codegen:
	go run ./cmd/codegen

## run: Start the MCP server
run: build
	./bin/spotify-mcp-go

## docker: Build container image with ko
docker:
	ko build ./cmd/server

## clean: Remove build artifacts
clean:
	rm -rf bin/
