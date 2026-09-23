### Fixed

- **SDLC: the worker image carries actionlint and shellcheck** (#1039). A
  project whose `make lint` lints its GitHub Actions workflows (go-blockstore)
  failed verification in the worker on a tree CI passed. The target fails
  closed without shellcheck, because actionlint would otherwise skip every SC*
  rule and still exit 0, and the image carried neither tool. The image now
  builds actionlint with `go install` at the Makefile's `ACTIONLINT_VERSION`
  and installs the upstream static shellcheck release at `SHELLCHECK_VERSION`,
  checked against a pinned SHA-256 for each architecture. `make
  test-worker-image` checks both pins, and checks that actionlint reports a
  shellcheck finding (SC2046), so the image cannot pass with shellcheck
  unreachable.
