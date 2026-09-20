### Changed

- **Go 1.27.0 across the module, the images and CI.** Go 1.27.0 has been the
  portfolio standard since 2026-09-01 and this repository was still declaring
  1.26.7 everywhere — including the Software Factory worker image, which ships
  ONE toolchain under `GOTOOLCHAIN=local`. A factory project that had already
  moved to 1.27 (go-luks2, PR #202) could therefore not be built, tested or
  reviewed by the factory at all. Both `go.mod` files, the Makefile's
  `GO_VERSION`, every Dockerfile base, the integration `go.work` stubs and the
  four workflow pins now say 1.27.0, and golangci-lint moves to v2.13.2, the
  first release that lints a 1.27 module and the version the fleet's ci-runner
  image bakes.
