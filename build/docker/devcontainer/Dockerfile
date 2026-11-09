# syntax=docker/dockerfile:1
#
# The project's devcontainer: the one image every CI stage runs in (handbook
# software/conventions/devcontainer.md, ci-standard.md). Built with the
# repository root as context:
#
#   make docker-build                 builds the toolchain and integration images
#   make docker-run CMD='make test'   runs CMD in the toolchain image
#
# Stages (devcontainer.md section 1; both final stages are FROM base):
#   base         Go pinned by digest, a non-root user, the module cache
#   tools        build-only: fetches and builds the pinned tools
#   toolchain    base + the tools: build, unit tests, lint, security, deps-check
#   integration-build  build-only: compiles the node binaries from the source
#   integration  base + the node binaries, entrypoint and config: the node
#                image of test/integration/docker-compose.integration.yml
#
# Every tool version comes from pkg/version/toolchain.mk, the repository's one
# pin file, which the Makefile includes too (devcontainer.md section 2). The
# base image's two pins arrive as build arguments because a FROM line cannot
# read a file; the Makefile passes them from the same file.
#
# Debian, not alpine (devops.md section 9a): `make test` runs -race over cgo
# packages (the libcontainer runtime among them), and those gates are verified
# today on the glibc toolchain of the worker image
# (deployments/agents/software-factory-worker), whose golang trixie base this
# image shares.

ARG GO_VERSION
ARG GO_IMAGE_DIGEST

FROM golang:${GO_VERSION}-trixie@${GO_IMAGE_DIGEST} AS base

# GO_BUILD_PROXY: the caller's module cache (devops.md section 2a). No
# default and never an ENV: unset builds straight from the public proxy.
ARG GO_BUILD_PROXY

ENV GOTOOLCHAIN=local \
    GOPATH=/opt/go \
    GOMODCACHE=/opt/go/mod \
    GOFLAGS=-mod=readonly \
    GOWORK=off \
    PATH=/opt/go/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

RUN set -eux; \
    export DEBIAN_FRONTEND=noninteractive; \
    apt-get update; \
    apt-get upgrade -y; \
    rm -rf /var/lib/apt/lists/*; \
    useradd --uid 1000 --user-group --create-home --home-dir /home/dev dev

# CVE-2026-111497: take the fixed libbrotli1 even when the base digest lags it.
RUN apt-get update && apt-get install -y --only-upgrade libbrotli1 && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
COPY sdk/go/go.mod sdk/go/go.sum sdk/go/

# Private first-party modules resolve through the gomodauth secret, staged by
# `make stage-gomodauth` (docs/testing/private-modules.md); the token never
# reaches a layer.
RUN --mount=type=secret,id=gomodauth \
    set -eu; \
    if [ -f /run/secrets/gomodauth ]; then set -a; . /run/secrets/gomodauth; set +a; fi; \
    GOPROXY="${GO_BUILD_PROXY:+${GO_BUILD_PROXY%/}|}https://proxy.golang.org|direct"; export GOPROXY; \
    go mod download; \
    (cd sdk/go && go mod download); \
    chmod -R a+rX /opt/go

FROM base AS tools

ARG GO_BUILD_PROXY

# The tools build in their own module cache, so none of their dependencies
# lands in the cache the project's own build reads.
ENV GOMODCACHE=/tmp/tools-mod GOBIN=/opt/tools/bin GOFLAGS= CGO_ENABLED=0

# xz-utils unpacks the shellcheck .tar.xz below; the golang base has no xz.
RUN set -eux; \
    export DEBIAN_FRONTEND=noninteractive; \
    apt-get update; \
    apt-get install -y --no-install-recommends xz-utils; \
    rm -rf /var/lib/apt/lists/*

COPY pkg/version/toolchain.mk /tmp/toolchain.mk

RUN set -eu; \
    pin() { v="$(sed -n "s/^$1 := //p" /tmp/toolchain.mk)"; \
        [ -n "$v" ] || { echo "$1 missing from pkg/version/toolchain.mk" >&2; exit 1; }; \
        printf '%s' "$v"; }; \
    GOPROXY="${GO_BUILD_PROXY:+${GO_BUILD_PROXY%/}|}https://proxy.golang.org|direct"; export GOPROXY; \
    go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(pin GOLANGCI_VERSION)"; \
    go install "golang.org/x/vuln/cmd/govulncheck@$(pin GOVULNCHECK_VERSION)"; \
    go install "github.com/rhysd/actionlint/cmd/actionlint@$(pin ACTIONLINT_VERSION)"; \
    # gosec the way `make install-gosec` builds it: a throwaway module that
    # requires the pin and raises each GO_TOOL_DEPENDENCY_FLOORS module gosec
    # already depends on.
    gosec_version="$(pin GOSEC_VERSION)"; \
    mkdir /tmp/gosec; cd /tmp/gosec; \
    go mod init build.local/gosec; \
    go get "github.com/securego/gosec/v2/cmd/gosec@${gosec_version}"; \
    for floor in $(pin GO_TOOL_DEPENDENCY_FLOORS); do \
        if go list -m "${floor%@*}" >/dev/null 2>&1; then go get "$floor"; fi; \
    done; \
    go build -ldflags "-X main.Version=${gosec_version}" -o "${GOBIN}/gosec" github.com/securego/gosec/v2/cmd/gosec; \
    cd /; rm -rf /tmp/gosec /tmp/tools-mod; \
    "${GOBIN}/gosec" -version

# The release binaries, each checked against the SHA-256 pinned beside its
# version.
RUN set -eu; \
    pin() { v="$(sed -n "s/^$1 := //p" /tmp/toolchain.mk)"; \
        [ -n "$v" ] || { echo "$1 missing from pkg/version/toolchain.mk" >&2; exit 1; }; \
        printf '%s' "$v"; }; \
    case "$(uname -m)" in \
        x86_64) arch=X86_64; trivy_asset=Linux-64bit; shellcheck_arch=x86_64 ;; \
        aarch64) arch=AARCH64; trivy_asset=Linux-ARM64; shellcheck_arch=aarch64 ;; \
        *) echo "no tool pins for $(uname -m)" >&2; exit 1 ;; \
    esac; \
    trivy_version="$(pin TRIVY_VERSION)"; \
    trivy_tarball="trivy_${trivy_version#v}_${trivy_asset}.tar.gz"; \
    curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors -o "/tmp/${trivy_tarball}" "https://github.com/aquasecurity/trivy/releases/download/${trivy_version}/${trivy_tarball}"; \
    echo "$(pin TRIVY_SHA256_${arch})  /tmp/${trivy_tarball}" | sha256sum -c -; \
    tar -xzf "/tmp/${trivy_tarball}" -C "${GOBIN}" trivy; \
    shellcheck_version="$(pin SHELLCHECK_VERSION)"; \
    shellcheck_tarball="shellcheck-${shellcheck_version}.linux.${shellcheck_arch}.tar.xz"; \
    curl -fsSL --retry 5 --retry-delay 5 --retry-all-errors -o "/tmp/${shellcheck_tarball}" "https://github.com/koalaman/shellcheck/releases/download/${shellcheck_version}/${shellcheck_tarball}"; \
    echo "$(pin SHELLCHECK_SHA256_${arch})  /tmp/${shellcheck_tarball}" | sha256sum -c -; \
    tar -xJf "/tmp/${shellcheck_tarball}" -C /tmp; \
    install -m 0755 "/tmp/shellcheck-${shellcheck_version}/shellcheck" "${GOBIN}/shellcheck"; \
    rm -rf /tmp/*.tar.* "/tmp/shellcheck-${shellcheck_version}"; \
    "${GOBIN}/trivy" --version; \
    "${GOBIN}/shellcheck" --version

FROM base AS toolchain

# jq, make, bash and git are what the Makefile's recipes call besides Go and
# the pinned tools; the golang image already carries make, bash, git and gcc.
# `make build` also builds the client with its embedded desktop GUI: build-ui
# needs Node.js, npm and rsync, and build-cli links Wails against GTK and
# WebKitGTK through pkg-config with cgo. libnss-wrapper gives the stock
# ssh-keygen that the signing interop tests run a passwd entry: the image runs
# as any non-root uid, which has none, and ssh-keygen exits without one
# (pkg/testutil/nsswrapper).
RUN set -eux; \
    export DEBIAN_FRONTEND=noninteractive; \
    apt-get update; \
    apt-get install -y --no-install-recommends jq xz-utils libnss-wrapper \
        nodejs npm rsync pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev; \
    rm -rf /var/lib/apt/lists/*

COPY --from=tools /opt/tools/bin/ /opt/go/bin/

# The npm cache, filled here from the tracked lock files of the TypeScript SDK,
# the frontend and the JavaScript SDK so build-ui's, test-sdk-ts's,
# test-frontend's and test-sdk-js's `npm ci` run offline in CI (devcontainer.md
# section 4). The file: links (the TypeScript SDK and svelte-security) need
# only their package.json. The cache stays writable because npm records its
# index and logs there.
COPY sdk/typescript/package.json sdk/typescript/package-lock.json /tmp/npm-seed/sdk/typescript/
COPY sdk/typescript/svelte-security/package.json /tmp/npm-seed/sdk/typescript/svelte-security/
COPY cmd/quicbaas/frontend/package.json cmd/quicbaas/frontend/package-lock.json /tmp/npm-seed/cmd/quicbaas/frontend/
COPY sdk/javascript/package.json sdk/javascript/package-lock.json /tmp/npm-seed/sdk/javascript/
RUN set -eu; \
    export npm_config_cache=/opt/npm-cache; \
    (cd /tmp/npm-seed/sdk/typescript && npm ci --ignore-scripts --no-audit --no-fund); \
    (cd /tmp/npm-seed/cmd/quicbaas/frontend && npm ci --ignore-scripts --no-audit --no-fund); \
    (cd /tmp/npm-seed/sdk/javascript && npm ci --ignore-scripts --no-audit --no-fund); \
    rm -rf /tmp/npm-seed /opt/npm-cache/_logs; \
    chmod -R a+rwX /opt/npm-cache

# The stages copy the checkout into /src and run as a non-root uid the image
# does not know (scripts/in-toolchain-image.sh, IN_IMAGE_UID). The copy-in
# leaves /src itself as the image has it, so /src is empty and writable by any
# uid here, and git trusts it whoever owns it. Without this, base's root-owned
# /src (its module download) refuses `mkdir bin` and git's ownership check.
# The RUN leaves /src, base's WORKDIR, before it removes it, or the commands
# after the rm run in a deleted directory.
RUN set -eu; \
    cd /; \
    rm -rf /src; \
    install -d -m 1777 /src; \
    git config --system --add safe.directory /src

# Offline after the last fetch (devcontainer.md section 4), and usable as any
# non-root uid with HOME=/tmp and the caches mounted (section 5). The source
# is bind-mounted at /src. `make deps-check` sets its own GOPROXY.
ENV GOPROXY=off HOME=/tmp GOCACHE=/tmp/go-build \
    npm_config_cache=/opt/npm-cache \
    npm_config_offline=true \
    npm_config_update_notifier=false
USER dev
WORKDIR /src

FROM base AS integration-build

# The node binaries, built from the source at the commit with the tags and
# flags test/integration/Dockerfile.node uses. COVER=1 builds the daemon with
# coverage instrumentation scoped to pkg/server; -ldflags "-w -s" is dropped
# then, because the coverage runtime writes a meta-data file at exit.
ARG BUILD_TAGS="local quic raft rest rag timeseries vector qrdb_vector iam mcp hashicorpmetrics codec_json codec_yaml codec_toml codec_msgpack codec_bson codec_cbor codec_protobuf gms_pure_go awss3 gcpstorage azureblob minio"
ARG COVER=""

ENV GOPROXY=off CGO_ENABLED=0 GOOS=linux

COPY . /src
RUN set -eu; \
    if [ "${COVER}" = "1" ]; then \
        go build -cover -covermode=atomic \
            -coverpkg=github.com/jeremyhahn/go-quicbaas/pkg/server,github.com/jeremyhahn/go-quicbaas/cmd/quicbaasd \
            -tags "coverage ${BUILD_TAGS}" -o /out/quicbaasd ./cmd/quicbaasd; \
    else \
        go build -tags "${BUILD_TAGS}" -ldflags "-w -s" -o /out/quicbaasd ./cmd/quicbaasd; \
    fi; \
    go build -tags "${BUILD_TAGS}" -ldflags "-w -s" -o /out/quicbaas ./cmd/quicbaas

# The integration image is the node image the integration compose stack runs
# (go-quicbaas#1383, option B): base plus the node binaries, the node
# entrypoint and the node config. No source and no dev tools.
FROM base AS integration

# The paths the node writes. Compose mounts a named volume (or, for /covdata, a
# bind) on each; the image creates them so a fresh volume starts with them.
# netcat: the integration-test recipe probes each node's gRPC and MCP ports
# with `nc -z` inside the node.
RUN set -eux; \
    export DEBIAN_FRONTEND=noninteractive; \
    apt-get update; \
    apt-get install -y --no-install-recommends netcat-openbsd; \
    rm -rf /var/lib/apt/lists/*; \
    mkdir -p /data /var/log/quicbaas /var/run/quicbaas /etc/quicbaas/certs /covdata; \
    chmod 0777 /covdata

COPY --from=integration-build /out/quicbaasd /out/quicbaas /usr/local/bin/
COPY --chmod=0755 test/integration/node-entrypoint.sh /usr/local/bin/node-entrypoint.sh
# Baked in, not bind-mounted: the stack is driven from a job container whose
# workspace the host daemon cannot see (see Dockerfile.node).
COPY test/integration/quicbaas.yaml /etc/quicbaas/quicbaas.yaml

ENV GOPROXY=off HOME=/tmp \
    QUICBAAS_LOG_LEVEL=info \
    QUICBAAS_LOG_FORMAT=json \
    QUICBAAS_DATA_DIR=/data

# 8080 REST, 9090 metrics, 50051 gRPC, 4433 QUIC, 63001-63010 Raft.
EXPOSE 8080 9090 50051 4433 63001-63010

USER dev
WORKDIR /data
# The entrypoint derives the Raft member identities go-quicraft requires from
# the peers' certificates, then execs the command.
ENTRYPOINT ["/usr/local/bin/node-entrypoint.sh"]
CMD ["/usr/local/bin/quicbaasd", "node"]
