# QuicBaaS Go SDK Makefile
# Build, test, and development targets

.PHONY: all build test lint fmt vet clean \
        integration-test integration-test-up integration-test-down integration-test-logs \
        coverage coverage-html unit-test help

# Default target
all: lint test

# Build the SDK (compile check)
build:
	@echo "Building SDK..."
	@go build ./...

# Run unit tests
test: unit-test

unit-test:
	@echo "Running unit tests..."
	@go test -v -race -count=1 $$(go list ./... | grep -v /test/integration) -run "^Test"

# Run integration tests
integration-test: integration-test-up
	@echo "Running integration tests..."
	@cd test/integration && go test -v -race -count=1 -timeout 5m ./...
	@$(MAKE) integration-test-down

# Start integration test containers
integration-test-up:
	@echo "Starting test containers..."
	@cd test/integration && docker compose up -d --build --wait

# Stop integration test containers
integration-test-down:
	@echo "Stopping test containers..."
	@cd test/integration && docker compose down -v --remove-orphans

# View integration test container logs
integration-test-logs:
	@cd test/integration && docker compose logs -f

# Run integration tests without managing containers (assumes containers are running)
integration-test-only:
	@echo "Running integration tests (containers must be running)..."
	@cd test/integration && go test -v -race -count=1 -timeout 5m ./...

# Run linter
lint:
	@echo "Running linter..."
	@go vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

# Format code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -w .; \
	fi

# Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

# Generate code coverage report (excludes integration tests)
coverage:
	@echo "Generating coverage report..."
	@go test -coverprofile=coverage.out -covermode=atomic $$(go list ./... | grep -v /test/integration)
	@go tool cover -func=coverage.out | tail -1

# Generate HTML coverage report
coverage-html: coverage
	@echo "Generating HTML coverage report..."
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f coverage.out coverage.html
	@go clean -cache -testcache

# Run benchmarks
bench:
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./...

# Update dependencies
deps:
	@echo "Updating dependencies..."
	@go mod tidy
	@go mod download

# Verify dependencies
verify:
	@echo "Verifying dependencies..."
	@go mod verify

# Install development tools
tools:
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install golang.org/x/tools/cmd/goimports@latest

# Help target
help:
	@echo "QuicBaaS Go SDK Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all                    - Run lint and tests (default)"
	@echo "  build                  - Build/compile the SDK"
	@echo "  test                   - Run unit tests"
	@echo "  unit-test              - Run unit tests"
	@echo "  integration-test       - Run integration tests (manages containers)"
	@echo "  integration-test-up    - Start test containers"
	@echo "  integration-test-down  - Stop test containers"
	@echo "  integration-test-logs  - View container logs"
	@echo "  integration-test-only  - Run integration tests (containers must be running)"
	@echo "  lint                   - Run linters"
	@echo "  fmt                    - Format code"
	@echo "  vet                    - Run go vet"
	@echo "  coverage               - Generate coverage report"
	@echo "  coverage-html          - Generate HTML coverage report"
	@echo "  bench                  - Run benchmarks"
	@echo "  deps                   - Update dependencies"
	@echo "  verify                 - Verify dependencies"
	@echo "  tools                  - Install development tools"
	@echo "  clean                  - Clean build artifacts"
	@echo "  help                   - Show this help message"
