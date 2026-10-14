### Added

- **Per-package benchmark and coverage targets for the Go SDK module.**
  `make bench-sdk-go-<package>` and `make coverage-sdk-go-<package>` join
  `make test-sdk-go-<package>`: each resolves the package the same way
  (dashes name nested directories, so `transport-grpc` is
  `sdk/go/transport/grpc`) and runs inside the `sdk/go` module. Benchmarks use
  `bench-sdk-go`'s flags; coverage writes
  `coverage/coverage-sdk-go-<package>.{out,html}` and prints the total, as
  `coverage-<package>` does. An unknown package name fails before Go runs, and
  `make check-package-targets` asserts all three.
