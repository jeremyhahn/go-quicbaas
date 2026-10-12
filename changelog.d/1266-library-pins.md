### Changed

- **QuicBaaS builds and deploys only on released go-qrdb, go-xkms and
  go-quicraft versions.** `make check-library-pins`, now part of `make test`,
  fails when `go.mod` or `sdk/go/go.mod` takes any of the three at a
  pseudo-version or through a `replace` directive. `make proto-gen` reads
  go-qrdb's `authz.proto` from the pinned module in the module cache instead
  of a sibling `../go-qrdb` checkout, so generated code matches the deployed
  qrdb. `docs/go-sdk/engineering.md` documents the deployment pins.
