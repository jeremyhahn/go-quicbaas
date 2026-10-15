### Added

- **A cgo fleet node agent build and its FIPS twin (go-quicbaas#1349).**
  `make build-agent` builds `bin/quicbaas-agent`, the linux `quicbaas` CLI
  with `CGO_ENABLED=1`, statically linked (`-linkmode external -extldflags
  -static`, tags `netgo osusergo`), for runc `libcontainer`'s `nsenter` init.
  `make build-agent-fips` builds the `GOFIPS140=latest` twin,
  `bin/quicbaas-agent-fips`. `make check-agent-build` builds both and fails
  unless their build info shows `CGO_ENABLED=1` (and, for the twin,
  `GOFIPS140` with `fips140=on` by default) and `quicbaasd` still shows
  `CGO_ENABLED=0`.

### Changed

- **`quicbaas fleet deploy --binary` defaults to `bin/quicbaas-agent`**, the
  cgo agent build, instead of `bin/quicbaas-static`. `quicbaasd` is never
  built with cgo.
