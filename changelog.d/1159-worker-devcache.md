### Added

- **The Software Factory worker image builds through the fleet's dev caches
  (go-quicbaas#1159).** `make build-worker-image` passes `DEVCACHE_GOPROXY`,
  `DEVCACHE_GOPRIVATE` and `DEVCACHE_NPM_REGISTRY` through by name, from the
  caller's environment or command line, to three new `ARG`s the Dockerfile
  declares but never defaults or `ENV`s; unset, or with a cache unreachable,
  the build is byte-for-byte what it always was. The toolchain stage's `go
  install` / `go get` / `go build` feed `DEVCACHE_GOPROXY` through Go's `|`
  fallback chain and set `GOPRIVATE`/`GONOPROXY`/`GONOSUMDB` from
  `DEVCACHE_GOPRIVATE`; the agent-CLI `npm install -g` evaluates
  `devcache-env` first. The image now carries `devcache-env` itself at
  `/usr/local/bin/devcache-env`, embedded byte-for-byte from dev-cache
  v0.1.0 via a BuildKit heredoc and checked against its pinned SHA-256 at
  build time, so the Dockerfile keeps building from the empty context the
  daemon's worker-image refresh depends on.
