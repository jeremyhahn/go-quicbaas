### Changed

- **One go.work script for the seal integration images.** `Dockerfile.seal`
  and `Dockerfile.seal-hardware` run `test/integration/mkworkspace.sh` instead
  of four inline copies of the workspace block, and its `go` line is the
  image toolchain's `go env GOVERSION` rather than a literal.
  `make check-workspace-script`, now part of `make test`, fails if a
  Dockerfile writes a go.work inline again and tests the script against a
  temporary directory (#399).
- **golangci-lint is pinned everywhere it is installed.** The integration-test
  image and the devcontainer read `GOLANGCI_VERSION` from
  `pkg/version/toolchain.mk` and install the v2 module at it; `sdk/go`'s
  `make tools` does the same, and its `make lint` now fails when the linter is
  absent instead of skipping (#391).
- **protoc and its Go plugins are pinned.** `make install-proto-tools` installs
  protoc from its checksum-verified release zip and protoc-gen-go /
  protoc-gen-go-grpc at the pins in `pkg/version/toolchain.mk`, and
  `make proto-gen` refuses to run, naming each mismatch, unless the installed
  versions match those pins (`make check-proto-tools`). The devcontainer
  installs the plugins at the same pins (#481).

### Fixed

- **Vector tests no longer fail in a `vector`-only build.** The
  `pkg/server` index-factory, embedding-provider and lifecycle-seam tests
  require `vector && qrdb_vector`, and the "not compiled" errors in
  `pkg/embedding` and `pkg/server` name both tags (#514).
