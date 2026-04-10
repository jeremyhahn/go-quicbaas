# Testing Conventions

## Unit Tests
- Execute: `make test` or `make test-<package>`
- **NEVER** modify the host system
- **NEVER** perform blocking operations
- **ALWAYS** run in memory, execute quickly
- Target: 90%+ code coverage per package
- Naming: `$SOURCE_FILE_NAME_test.go`
- **NEVER** use skip guards or `-short` flags

## Integration Tests
- Execute: `make integration-test` or `make integration-test-<package>`
- **ALWAYS** run in Docker containers (never on host)
- Full E2E testing with real server resources
- Mock external 3rd party API calls
- Target: 90%+ integration coverage
- Location: `test/integration/<package>/`
- Use multi-protocol table-driven tests for CLI

## Benchmarks
- Execute: `make bench-<package>`
- Use convention `bench-$PACKAGE_X`

## Build Tags
- Unit tests: `local quic raft rest rag timeseries vector iam mcp` (and codec/storage tags)
- Integration tests: `integration` + all feature tags
- Always use `-tags` flag with appropriate tags

## Key Rules
- ALL code must be thoroughly tested including edge cases
- Every function needs at least 2 tests (success + error handling)
- Don't write tests just for coverage — all tests must provide real value
- Prefer running single tests for performance: `go test -run TestName`
- Consolidate related tests into a single test file per source file
- Use TDD (Test Driven Development) approach
