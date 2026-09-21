### Fixed

- **Verification feedback names the failure.** The bounded tail alone showed
  a worker a bare trailing `FAIL`, because `go test -v` prints tens of
  thousands of PASS lines after the failing package (three rounds on
  go-quicbaas#62). The lines that name a failure — `--- FAIL`, `FAIL`,
  `panic:`, a compiler's `# pkg` header and its `file.go:line:col:`
  diagnostics, `make: ***` — are now gathered from the whole output, up to
  200 of them, ahead of the tail.
- **Two unit tests no longer read host state.** One refused a correct
  isolated credential mount whenever `HOME` was an ancestor of the temp
  dir (the worker image sets `HOME=/tmp`); one required a docker or podman
  binary on `PATH`. The first now names the leak it means (the operator's
  own credential directory); the second supplies the engine it needs. Both
  were trunk-red inside the factory's own worker image, so no go-quicbaas
  delivery could pass `make test` there.
