# Task Completion Checklist

When a task/feature is completed, ensure ALL of the following:

## Code Quality
1. Run `make fmt` to format code
2. Run `make vet` to check for issues
3. Run `make lint` to run golangci-lint

## Testing
1. Run `make test` to ensure ALL unit tests pass
2. Run specific package tests: `make test-<package>` for modified packages
3. Ensure 90%+ code coverage: `make coverage-<package>`
4. Every function has at least 2 tests (happy path + error handling)
5. Unit tests MUST run fast, in-memory, never modify the host

## Integration Tests
1. If touching API/transport/service layers, verify with `make integration-test`
2. Integration tests run in Docker containers with real services
3. Use `make integration-test-<package>` for specific packages

## Documentation
1. Update documentation in `docs/` folder upon feature completion
2. Update examples if API changed

## Cleanup
1. Clean up any temporary scripts used for testing/debugging
2. Remove unused code completely (no commented-out code or `_` renamed vars)
3. Ensure no superfluous .md files were created

## Build Verification
1. Verify builds: `make build`
2. If touching protobuf: `make proto-gen && make proto-lint`
