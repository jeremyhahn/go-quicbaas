### Fixed

- **Tests outside `sdk/go` carry no `//nolint` directives (#559).** Each
  finding is fixed in place: ignored errors from cleanup requests, flag
  setters, gRPC `Serve`, `Close`, `Stop` and directory walks now fail the test,
  type assertions are checked, cleanup response bodies are closed, and
  nil-context guards take a typed nil `context.Context`. Two gRPC shutdown
  tests wait for a READY client connection instead of sleeping, and the audit
  compression test asserts its synchronous result instead of sleep-polling.
