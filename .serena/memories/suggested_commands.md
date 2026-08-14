# Suggested Commands

## Build
```bash
make build                    # Build both daemon + CLI
make build-daemon             # Build server only
make build-cli                # Build CLI only
make build-static             # Static binaries for containers
make build-fips               # FIPS 140-3 build
make build-embedded           # Single-node embedded (no Raft)
```

## Test
```bash
make test                     # Run ALL unit tests
make test-<package>           # Run unit tests for specific package (e.g., make test-kms)
make integration-test         # Run ALL integration tests in Docker
make integration-test-<pkg>   # Run integration tests for specific package
make test-verbose             # Unit tests with verbose output
```

## Code Quality
```bash
make lint                     # Run golangci-lint
make fmt                      # Format code (gofmt)
make fmt-check                # Check formatting without modifying
make vet                      # Run go vet
```

## Coverage
```bash
make coverage                 # Generate overall coverage report
make coverage-<package>       # Coverage for specific package (e.g., make coverage-kms)
make coverage-report          # Open coverage in browser
```

## Benchmarks
```bash
make bench-<package>          # Run benchmarks for a package (e.g., make bench-raft)
make benchmark                # Run all benchmarks
make benchmark-quick          # Quick benchmark (1 iteration)
```

## Docker
```bash
make docker-build             # Build Docker image
make docker-build-dev         # Build dev Docker image
make docker-run               # Run Docker container
make docker-compose-up        # Start docker-compose environment
make docker-compose-down      # Stop docker-compose
make docker-compose-logs      # Show logs
```

## Protobuf
```bash
make proto-gen                # Generate protobuf code
make proto-lint               # Lint proto files
make proto-format             # Format proto files
```

## Cleanup
```bash
make clean                    # Clean build artifacts
make clean-docker             # Clean Docker resources
make clean-all                # Clean everything
```

## Direct Go Commands (when needed)
```bash
# Build with specific tags
go build -tags "local quic raft rest rag timeseries vector iam mcp" ./pkg/...

# Run single test
go test -tags "local quic raft rest rag timeseries vector iam mcp" -run TestName ./pkg/path/...

# Build tags reference:
#   local quic raft rest rag timeseries vector iam mcp
#   hashicorpmetrics codec_json codec_yaml codec_toml codec_msgpack
#   codec_bson codec_cbor codec_protobuf
#   awss3 gcpstorage azureblob minio
```

## System Utilities
```bash
git status / git diff / git log      # Version control (Linux)
ls, cd, grep, find                   # Standard Linux utilities
go env                               # Go environment info
```
