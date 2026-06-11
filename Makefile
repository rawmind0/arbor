.PHONY: fmt vet lint lint-fix test test-all test-unit test-integration test-race test-coverage test-coverage-html \
        build build-all build-linux-amd64 build-linux-arm64 build-darwin-arm64 run \
        example-generate example-plan example-build \
        release release-snapshot clean deps install help

# Variables
BINARY_NAME=arbor

# Example use cases, auto-discovered from examples/*/config.yaml.
EXAMPLES := $(patsubst examples/%/config.yaml,%,$(wildcard examples/*/config.yaml))
# Set EXAMPLE=<name> to act on a single use case; default is all of them.
EXAMPLE ?=
# The list the example-* targets iterate (one if EXAMPLE is set, else all).
example_list = $(if $(EXAMPLE),$(EXAMPLE),$(EXAMPLES))
# Set DRY=1 to simulate builds instead of running real docker.
DRY ?=
# Set GEN=1 to regenerate Dockerfiles first (plan/build --generate).
GEN ?=

# Development commands
fmt:
	go fmt ./...

vet:
	go vet ./...

# Lint with golangci-lint (config in .golangci.yml). Install:
#   https://golangci-lint.run/welcome/install/
lint:
	golangci-lint run

lint-fix:
	golangci-lint run --fix

# Test commands
test:
	go test -v ./...

test-all: test-unit test-integration

test-unit:
	@echo "Running unit tests..."
	go test -v ./internal/... ./cmd/...

test-integration:
	@echo "Running integration tests..."
	go test -v ./test/integration/...

test-race:
	go test -race ./...

test-coverage:
	go test -cover -coverprofile=coverage.out ./...

test-coverage-html: test-coverage
	go tool cover -html=coverage.out

# Build commands using goreleaser
build:
	goreleaser build --single-target --clean --snapshot

build-all:
	goreleaser build --clean --snapshot

build-linux-amd64:
	GOOS=linux GOARCH=amd64 goreleaser build --single-target --clean --snapshot

build-linux-arm64:
	GOOS=linux GOARCH=arm64 goreleaser build --single-target --clean --snapshot

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 goreleaser build --single-target --clean --snapshot

# Run commands
run:
	go run . --help

# Example workflow — generate, plan, then build the examples. By default every
# example is processed; pass EXAMPLE=<name> to scope to one. Each folder's extra
# source files are auto-loaded via --sources (a no-op for single-config examples,
# and the second stack for multisource). build runs real docker by default
# (needs Docker; multiarch also needs a registry + creds); add DRY=1 to simulate.
#   make example-generate                 # all examples
#   make example-plan EXAMPLE=multiarch   # just one
#   make example-build EXAMPLE=hierarchy DRY=1
example-generate:
	@for ex in $(example_list); do \
		echo "==> generate $$ex"; \
		go run . generate --config examples/$$ex/config.yaml --sources "examples/$$ex/*.yaml" --skip-version-check || exit 1; \
	done

example-plan:
	@for ex in $(example_list); do \
		echo "==> plan $$ex"; \
		go run . plan --config examples/$$ex/config.yaml --sources "examples/$$ex/*.yaml" $(if $(GEN),--generate,) || exit 1; \
	done

example-build:
	@for ex in $(example_list); do \
		echo "==> build $$ex"; \
		go run . build --config examples/$$ex/config.yaml --sources "examples/$$ex/*.yaml" $(if $(GEN),--generate,) $(if $(DRY),--dry-run,) || exit 1; \
	done

# Release
release:
	goreleaser release --clean

release-snapshot:
	goreleaser release --snapshot --clean

# Cleanup
clean:
	rm -rf bin/ dist/ coverage.out

# Install dependencies
deps:
	go mod download
	go mod tidy

# Install binary
install: build
	cp dist/$(BINARY_NAME)_*/$(BINARY_NAME) $(GOPATH)/bin/

# Help
help:
	@echo "Available commands:"
	@echo "  fmt                - Format Go code"
	@echo "  vet                - Run go vet"
	@echo ""
	@echo "Testing:"
	@echo "  test               - Run all tests (verbose)"
	@echo "  test-all           - Run unit and integration tests"
	@echo "  test-unit          - Run unit tests only"
	@echo "  test-integration   - Run integration tests only"
	@echo "  test-race          - Run tests with the race detector"
	@echo "  test-coverage      - Run tests with coverage (terminal)"
	@echo "  test-coverage-html - Run tests with coverage (HTML)"
	@echo ""
	@echo "Build & Run:"
	@echo "  build              - Build binary for current platform"
	@echo "  build-all          - Build binaries for all platforms"
	@echo "  build-*            - Build for a platform (linux-amd64, linux-arm64, darwin-arm64)"
	@echo "  run                - Show CLI help"
	@echo "  release            - Create a release with goreleaser"
	@echo "  release-snapshot   - Create a snapshot release"
	@echo ""
	@echo "Examples (all by default; EXAMPLE=<use-case> for one; GEN=1 regenerates first; DRY=1 simulates):"
	@echo "  example-generate   - Generate the examples' Dockerfiles"
	@echo "  example-plan       - Show the examples' build plans (GEN=1 to regenerate first)"
	@echo "  example-build      - Build the examples (real docker; DRY=1 to simulate; GEN=1 to regenerate first)"
	@echo ""
	@echo "Utilities:"
	@echo "  clean              - Clean build artifacts"
	@echo "  deps               - Download and tidy dependencies"
	@echo "  install            - Build and install binary to GOPATH"
