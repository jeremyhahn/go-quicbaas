# Code Style and Conventions

## Go Style Guide
Follows the official Google Go style guide. Priority order: Clarity > Simplicity > Concision > Maintainability > Consistency.

## Naming Conventions
- **NEVER** use "impl" for file names or struct names
- Use standard Go naming: exported = PascalCase, unexported = camelCase
- Test files: `$SOURCE_FILE_NAME_test.go`
- **NEVER** use generic test names like `_coverage_boost_test.go`

## Error Handling
- **ALWAYS** declare errors as types, never use `fmt.Errorf` or inline errors
- **NEVER** ignore errors with `_` in Go
- Use typed errors from `pkg/api/` and `pkg/errors/`

## Concurrency
- Prefer lock-free algorithms to mutexes
- Prefer native `sync/atomic` package (no pointer magic or unsafe operations)
- **NEVER** use `time.Sleep` — use Go channels with select or wait groups
- Use map-based dispatch instead of large switch statements (O(1) lookup)

## Architecture Patterns
- Clean abstraction layers: NEVER directly call business logic from public interface
- Service layer provides encapsulation between public API and business logic
- DRY (Don't Repeat Yourself) and YAGNI (You Ain't Gonna Need It)
- KISS — Keep It Simple

## Code Quality
- Write simple, concise code — never terse, dense, or hard to understand
- All functions should have at least 2 tests: one positive, one for error handling
- Never use simplified versions, mocks, or stubs without explicit approval
- Never use skip guards or -short flags in tests

## Build Tags
Feature-gated builds using build tags: `raft`, `rest`, `quic`, `mcp`, `iam`, `vector`, `timeseries`, `rag`, etc. Always include `local` tag for local builds.

## File Organization
- Never create superfluous .md files
- Documentation goes in `docs/` folder
- Integration tests in `test/integration/`
- Use multi-level CLAUDE.md/AGENTS.md for fine-grained context
