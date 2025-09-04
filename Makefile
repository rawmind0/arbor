.PHONY: fmt vet test build run clean install help

# Variables
BINARY_NAME=container-builder
VERSION=$(shell git describe --tags --always --dirty)
BUILD_FLAGS=-ldflags="-s -w -X main.Version=$(VERSION)" -a -installsuffix cgo
CGO_ENABLED=0

# Development commands
fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test -v ./...

test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

# Build commands
build:
	CGO_ENABLED=$(CGO_ENABLED) go build $(BUILD_FLAGS) -o bin/$(BINARY_NAME) .

build-linux-amd64:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -o bin/$(BINARY_NAME)-linux-amd64 .

build-linux-arm64:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=linux GOARCH=arm64 go build $(BUILD_FLAGS) -o bin/$(BINARY_NAME)-linux-arm64 .

build-darwin-arm64:
	CGO_ENABLED=$(CGO_ENABLED) GOOS=darwin GOARCH=arm64 go build $(BUILD_FLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 .

# Run commands
run:
	go run . --help

dev:
	go run . generate --config examples/config.yaml

# Release
release:
	goreleaser release --clean

release-snapshot:
	goreleaser release --snapshot --clean

# Cleanup
clean:
	rm -rf bin/ dist/

# Install dependencies
deps:
	go mod download
	go mod tidy

# Install binary
install:
	go install $(BUILD_FLAGS) .

# Help
help:
	@echo "Available commands:"
	@echo "  fmt              - Format Go code"
	@echo "  vet              - Run go vet"
	@echo "  test             - Run tests"
	@echo "  test-coverage    - Run tests with coverage"
	@echo "  build            - Build binary for current platform"
	@echo "  build-*          - Cross-compile for specific platforms"
	@echo "  run              - Run application with help"
	@echo "  dev              - Run with example config"
	@echo "  release          - Create release with goreleaser"
	@echo "  release-snapshot - Create snapshot release"
	@echo "  clean            - Clean build artifacts"
	@echo "  deps             - Download and tidy dependencies"
	@echo "  install          - Install binary to GOPATH"