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

GOLANGCI_LINT_VERSION := v2.11.4
GOLANGCI_LINT := bin/golangci-lint

$(GOLANGCI_LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b bin $(GOLANGCI_LINT_VERSION)

## lint: Run golangci-lint v2
lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run ./...

## clean: Remove build artifacts
clean:
	rm -rf bin/
