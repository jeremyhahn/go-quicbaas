### Fixed

- **`make test-sdk-go`, `make test-sdk-go-coverage` and `make coverage` fail
  when their package list cannot be built.** They passed
  `$(go list ... | grep -v ...)` straight to `go test`, so a failing `go list`
  (a broken `go.mod`, a package that does not load) left an empty argument
  list, and `go test` with no packages tests only the current directory and
  can pass. Each now builds the list first and exits non-zero, naming the
  target, when `go list` fails or the filter leaves nothing.
  `make check-package-targets` drives all three with a failing and an empty
  `go list`.
