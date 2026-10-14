### Fixed

- **`make coverage-<package>` now fails when the package's tests fail.** It
  used to write the report after a failing `go test` and exit 0. The report
  step itself now fails the target when `go tool cover` cannot write the HTML,
  and `make coverage-sdk-go-<package>` accepts an absolute `COVERAGE_DIR`.
  `make check-package-targets` runs every per-package target (`test-`,
  `bench-`, `coverage-` and their `sdk-go-` forms) with failing tests and
  fails if any of them exits 0.
- **`make integration-test-agents` reports the test result, not `tee`'s.** The
  run is piped through `tee`, so the target took `tee`'s exit status and
  passed whatever the tests did.
