# Software Factory worker
#
# The image a project binding's `worker.image` names. It is where an sdlc
# job's ONE worker session runs: the runner clones the project and its context
# repositories into it, prompts the agent with the issue contract, runs the
# binding's verification commands in it, and commits, signs and pushes from
# it. So the image has to carry every tool those four things need, and nothing
# that could stand in for a credential.
#
# WHAT IS HERE AND WHY:
#
#   claude-code    the agent the ACP backend drives, as deployments/agents/
#                  claude-code installs it.
#   opencode       the ACP agent a fleet node drives against a local model
#                  server (the review model on ollama); pinned release binary.
#   git            the runner's checkout and publish steps are git commands
#                  exec'd into this container (pkg/sdlc/checkout.go,
#                  pkg/sdlc/publish.go).
#   ssh-keygen     git signs commits with `gpg.format=ssh`, which invokes
#                  ssh-keygen to talk to the agent on $SSH_AUTH_SOCK. Without
#                  it every signed commit fails.
#   openssh-client the `ssh` binary itself. git has NO built-in ssh transport:
#                  it execs `ssh`, so an image without one turns every
#                  `git clone ssh://...` into "sh: ssh: not found" and exit
#                  128. ssh-keygen is a separate package and does not bring
#                  it, which is exactly how the first containerized worker
#                  runs failed.
#   go + make      the binding's verification commands are the project's own
#                  (`make build`, `make test`, `make lint`), and a Go project's
#                  are what this factory builds.
#   build-essential `go test -race` and any cgo dependency need a C toolchain;
#                  a project whose tests do not build cannot be verified.
#   libssl-dev     OpenSSL headers: go-tpm-tools' TPM simulator (go-xkms
#                  pkg/tpm2) includes <openssl/aes.h>, so without them `make
#                  lint` fails typechecking with "openssl/aes.h: No such file"
#                  (go-quicbaas#1164).
#   libusb-1.0-0-dev  libusb headers and pkg-config file: go-xkms `xkey` is
#                  cgo over libusb, and fails to typecheck without them.
#   sudo           the agent has passwordless sudo inside this container, so it
#                  can install a toolchain the image does not carry and run
#                  anything a task needs. The container is the isolation
#                  boundary; rationing privilege inside it only costs attempts
#                  (go-quicbaas#927). See the sudoers block below for why this
#                  is sudo rather than `USER root`.
#   golangci-lint  `make lint` IS a verification command on every Go project in
#   gosec          this fleet, and `make check` (fmt vet lint gosec vuln) and
#   govulncheck    `make security` name the two scanners. An image without them
#                  cannot verify anything: the target exits non-zero with
#                  "golangci-lint is required but not installed" on a tree that
#                  is perfectly clean, so every review round fails and every Go
#                  project escalates to a human (go-quicbaas#772). They are
#                  installed at the versions the repository pins, PASSED IN as
#                  build arguments by `make build-worker-image` from the
#                  Makefile's own GOLANGCI_VERSION / GOSEC_VERSION /
#                  GOVULNCHECK_VERSION, so the image cannot drift from the
#                  verdict CI reaches with the same pins.
#   actionlint     the fleet's `make lint` lints GitHub Actions workflows
#   shellcheck     (devops.md: `make lint` = gofmt, vet, golangci-lint,
#                  actionlint), and actionlint delegates every SC* rule to
#                  shellcheck. With shellcheck absent actionlint SILENTLY skips
#                  those rules and still exits 0, so a project's target fails
#                  closed without it — and without either tool `make lint`
#                  failed verification in this image on a tree CI passed
#                  (go-quicbaas#1039, go-blockstore). Pinned the same way, from
#                  the Makefile's ACTIONLINT_VERSION / SHELLCHECK_VERSION.
#
#                  TRIVY IS DELIBERATELY ABSENT. It is the fourth member of
#                  `make security`, but it is an image/filesystem scanner whose
#                  every run downloads a vulnerability database — in a worker
#                  sandbox whose network policy is the operator's to decide it
#                  would fail for the environment rather than for the diff. A
#                  binding that needs it must name an image that carries it.
#   serena         the semantic code-retrieval MCP server, and the language
#   language       servers it drives -- Go, Python, TypeScript/JavaScript,
#   servers        Rust, C/C++, Ruby, bash, markdown and YAML -- BAKED IN, so a
#                  session with an empty HOME and no network still reads code
#                  by symbol (see the serena block below).
#
# THE BASE IS debian:trixie-slim: the smallest standard glibc image with a
# shell. bookworm-slim is 2 MB smaller compressed but oldstable; ubuntu:24.04
# is the same size with a shorter-lived stable line; distroless has no shell,
# and every tool here runs through one. It was Alpine until the image carried
# language servers: they, opencode, protoc and clangd are all published as
# glibc builds, and on musl each needed gcompat or a patch to serena's own
# platform detection.
#
# THERE IS NO ENTRYPOINT THAT MATTERS. Every sandboxed spawn path clears the
# image's entrypoint and runs the agent binary as the container's command
# (pkg/orchestration: sandboxSpec, called by spawnContainerCLI for a CLI-mode
# backend and spawnContainerJSONRPC for an ACP one), so an entrypoint that
# cloned would never run. The clone is driven by the runner instead.
#
# WHAT THE COMMAND IS, THOUGH, MATTERS. The CLI path runs `claude` — by name,
# resolved on PATH — followed by the stream-json flags the daemon configured
# (`--print --input-format stream-json --output-format stream-json --verbose`).
# The `CMD ["claude"]` below therefore documents the image's purpose rather than
# driving it; what the image must actually guarantee is that `claude` is on the
# PATH of the uid the spawn runs as.
#
# NO SECRET IS BAKED. No API key, no forge token, no signing key, and no
# environment variable declared to hold one. The forge token arrives as a
# read-only file under /run/quicbaas/workspace and the signing key never
# arrives at all — what arrives is a socket to an agent that holds it.

# The Go toolchain the projects this factory builds pin in their go.mod. It is
# passed by `make build-worker-image` from the Makefile's own GO_VERSION, so
# the image and the repository never disagree about it. No default, for the
# reason the verification pins below give.
ARG GO_VERSION

# The verification toolchain's pins. THEY HAVE NO DEFAULT ON PURPOSE.
#
# `make build-worker-image` passes the Makefile's GOLANGCI_VERSION,
# GOSEC_VERSION and GOVULNCHECK_VERSION, which are the same values CI installs
# (.github/workflows/ci.yml reads them from the Makefile through `make
# print-<VAR>`) and the same values `make install-tools` puts on a developer's
# machine. A default here would be a fourth copy of a pin, and a copy of a pin
# is a pin that drifts — the whole defect this build argument exists to close
# (go-quicbaas#772). Building this file without them fails, by design, with the
# command to use instead.
#
# golangci-lint in particular is pinned for a documented reason — v2.13.x
# panics under Go 1.26 — so an image that floated to @latest would fail a branch
# for a reason that has nothing to do with the branch. Unpin tracker: go-qrdb#42.
ARG GOLANGCI_VERSION
ARG GOSEC_VERSION
ARG GOVULNCHECK_VERSION
ARG ACTIONLINT_VERSION

# protoc-gen-go and protoc-gen-go-grpc are the Go code-generator plugins protoc
# invokes to turn a .proto file into Go and gRPC code. Each is its own module,
# `go install`ed below exactly like the three tools above, at the tag a Go
# project's generated code is checked in against.
ARG PROTOC_GEN_GO_VERSION
ARG PROTOC_GEN_GO_GRPC_VERSION

FROM golang:${GO_VERSION}-trixie AS toolchain

# `go install`, the way the CI runner installs the same three tools, so the
# worker and CI reach their verdicts with binaries built from the same source
# at the same tag by the same toolchain. CGO_ENABLED=0 keeps them static, so
# they run in the runtime stage unchanged.
ARG GOLANGCI_VERSION
ARG GOSEC_VERSION
ARG GOVULNCHECK_VERSION
ARG ACTIONLINT_VERSION
ARG PROTOC_GEN_GO_VERSION
ARG PROTOC_GEN_GO_GRPC_VERSION

# gopls is not a verdict tool — it is SERENA'S GO LANGUAGE SERVER, and serena is
# how an agent reads this codebase without burning its context on whole files.
# It is built here for the same reason as the three above: static, from source,
# by this toolchain, at the pin CI's own gopls would be.
ARG GOPLS_VERSION

# The dependency floors gopls and gosec are built with (toolchain.mk's
# GO_TOOL_DEPENDENCY_FLOORS, which says which CVEs they close). Space-separated
# module@version pairs; no default, for the same reason as the pins above.
ARG GO_TOOL_DEPENDENCY_FLOORS

# DEVCACHE_GOPROXY and DEVCACHE_GOPRIVATE are the fleet's dev caches
# (../handbook/software/conventions/devops.md section 2a). NO DEFAULT, and
# NEVER an ENV: unset (the common case away from the fleet) must build exactly
# as it did before the caches existed, and a value here would outlive the RUN
# that needs it. They are turned into GOPROXY / GOPRIVATE / GONOPROXY /
# GONOSUMDB for the one RUN below that runs `go install` / `go get` / `go
# build`, and nowhere else.
ARG DEVCACHE_GOPROXY
ARG DEVCACHE_GOPRIVATE

ENV CGO_ENABLED=0 GOBIN=/toolchain/bin

RUN set -eux; \
    if [ -z "${GOLANGCI_VERSION:-}" ] || [ -z "${GOSEC_VERSION:-}" ] || [ -z "${GOVULNCHECK_VERSION:-}" ] || [ -z "${ACTIONLINT_VERSION:-}" ] || [ -z "${GOPLS_VERSION:-}" ] || [ -z "${GO_TOOL_DEPENDENCY_FLOORS:-}" ] || [ -z "${PROTOC_GEN_GO_VERSION:-}" ] || [ -z "${PROTOC_GEN_GO_GRPC_VERSION:-}" ]; then \
        echo "GOLANGCI_VERSION, GOSEC_VERSION, GOVULNCHECK_VERSION, ACTIONLINT_VERSION, GOPLS_VERSION, GO_TOOL_DEPENDENCY_FLOORS, PROTOC_GEN_GO_VERSION and PROTOC_GEN_GO_GRPC_VERSION are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the Makefile's pins." >&2; \
        exit 1; \
    fi; \
    GOPROXY="${DEVCACHE_GOPROXY:+${DEVCACHE_GOPROXY%/}|https://proxy.golang.org|direct}"; \
    [ -z "${GOPROXY}" ] || export GOPROXY; \
    if [ -n "${DEVCACHE_GOPRIVATE:-}" ]; then \
        export GOPRIVATE="${DEVCACHE_GOPRIVATE}" GONOPROXY="${DEVCACHE_GOPRIVATE}" GONOSUMDB="${DEVCACHE_GOPRIVATE}"; \
    fi; \
    go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"; \
    go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"; \
    go install "github.com/rhysd/actionlint/cmd/actionlint@${ACTIONLINT_VERSION}"; \
    go install "google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION}"; \
    go install "google.golang.org/grpc/cmd/protoc-gen-go-grpc@${PROTOC_GEN_GO_GRPC_VERSION}"; \
    # gopls and gosec are the two tools this image ships whose own newest
    # releases carry vulnerable dependencies, and `go install tool@version`
    # cannot change a dependency. So each is built from a throwaway module:
    # require the tool at its pin, raise every floor the tool already depends
    # on (a floor it does not use would only drag unrelated modules along),
    # and `go build` the tool's main package. Every module is resolved through
    # the proxy and verified against the checksum database into that module's
    # go.sum, and every version is fixed, so the build is reproducible.
    #
    #   $1  binary name        $2  main package        $3  its pin
    #   $4  -ldflags           $5  a module the tool is released in lockstep
    #                              with, held at the version the tool requires
    build_with_floors() { \
        dir="/tmp/build-$1"; \
        mkdir -p "$dir"; \
        cd "$dir"; \
        go mod init "build.local/$1"; \
        go get "$2@$3"; \
        if [ -n "$5" ]; then held="$(go list -m -f '{{.Version}}' "$5")"; fi; \
        for floor in ${GO_TOOL_DEPENDENCY_FLOORS}; do \
            if go list -m "${floor%@*}" >/dev/null 2>&1; then go get "$floor"; fi; \
        done; \
        if [ -n "$5" ]; then go mod edit -replace "$5=$5@$held"; fi; \
        go build -ldflags "$4" -o "${GOBIN}/$1" "$2"; \
        cd /; \
        rm -rf "$dir"; \
    }; \
    # gosec stamps its version only through main.Version; a bare build
    # reports "Version: dev", which makes every scan unreproducible (the
    # fleet learned this on go-luks2#21) and is what the ci-runner image
    # avoids the same way.
    build_with_floors gosec "github.com/securego/gosec/v2/cmd/gosec" "${GOSEC_VERSION}" "-X main.Version=${GOSEC_VERSION}" ""; \
    # gopls compiles only against the exact golang.org/x/tools commit it was
    # released with (it imports x/tools' internal packages), and every fixed
    # golang.org/x/mod requires a newer x/tools -- for x/mod's own tests, not
    # its code. So x/tools is held at gopls's version while x/mod is raised.
    build_with_floors gopls "golang.org/x/tools/gopls" "${GOPLS_VERSION}" "" "golang.org/x/tools"; \
    "${GOBIN}/gopls" version; \
    "${GOBIN}/gosec" -version

FROM debian:trixie-slim

# `apt-get upgrade` FIRST: fixed packages already in Debian's archive reach this
# image without waiting for a new debian:trixie-slim tag to pick them up -- the
# trivy gate `make push-worker-image` runs reads the installed versions, not the
# base tag (go-quicbaas#1161). --no-install-recommends keeps what is installed
# to what is named here; the package lists are removed in the same layer, so
# anything that installs later (sudo's proof below, a worker's own install)
# runs `apt-get update` first.
#
# What the names below are for, beyond the header: xz-utils and unzip unpack
# the release tarballs and zips below; python3 runs serena (uv installs it);
# ruby, ruby-dev and ruby-bundler are what ruby-lsp installs against and
# re-execs through; libicu76 is the ICU marksman's .NET runtime loads (it is
# not built invariant-globalization), named rather than left to arrive with
# WebKit.
RUN set -eux; \
    export DEBIAN_FRONTEND=noninteractive; \
    apt-get update; \
    apt-get upgrade -y; \
    apt-get install -y --no-install-recommends \
        git \
        openssh-client \
        make \
        build-essential \
        libssl-dev \
        libusb-1.0-0-dev \
        bash \
        grep \
        rsync \
        pkgconf \
        libgtk-3-dev \
        libwebkit2gtk-4.1-dev \
        cryptsetup-bin \
        curl \
        ca-certificates \
        tzdata \
        sudo \
        xz-utils \
        unzip \
        python3 \
        python3-pip \
        ruby \
        ruby-dev \
        ruby-bundler \
        libicu76; \
    rm -rf /var/lib/apt/lists/*

# DEVCACHE-ENV, the dev caches' client (../handbook/software/conventions/
# devops.md section 2a). Embedded byte-for-byte via a BuildKit heredoc so it
# lands in the image with NO build context -- this Dockerfile is built with
# `docker build --file -` and an EMPTY context directory (see
# pkg/orchestration/container_image.go buildImageArgs), which rules out a
# plain COPY from a build directory. Unconfigured -- no DEVCACHE_* build arg
# set, or a cache that does not answer -- it changes nothing: every tool it
# feeds keeps its default behaviour.
#
# PROVENANCE: dev-cache v0.1.0, client/devcache-env, SHA-256
# 54ebcd1955daa328f31ebe8c93b50b71fcb9489a4a45a0a2986c530d4517cd29. The RUN
# below checks the copy that landed in this layer against that exact digest,
# so a byte this was not vendored from fails the build rather than the image.
COPY --chmod=0755 <<'DEVCACHE_ENV_SCRIPT' /usr/local/bin/devcache-env
#!/bin/sh
# devcache-env: turn the DEVCACHE_* variables into tool settings.
#
#   devcache-env [FORMAT]      FORMAT: sh (default) | env | apt-conf | apt-detect | daemon-json | report
#   devcache-apt-proxy URI     (a symlink to this script) apt's Proxy-Auto-Detect hook
#
# Each cache has its own variable. A variable that is unset or empty, or
# whose server does not answer, produces NO setting for its tool, so the tool
# keeps its default behaviour (no proxy). Nothing is ever forced.
#
#   DEVCACHE_GOPROXY          Go module proxy URL          -> GOPROXY="<url>|https://proxy.golang.org|direct"
#   DEVCACHE_GOPRIVATE        private module patterns      -> GOPRIVATE, GONOPROXY, GONOSUMDB (always, merged
#                                                             with any already set; never sent to a proxy)
#   DEVCACHE_NPM_REGISTRY     npm registry cache URL       -> npm_config_registry
#   DEVCACHE_APT_PROXY        apt cache URL                -> apt-conf / apt-detect output
#   DEVCACHE_DOCKER_MIRROR    Docker Hub pull-through URL  -> DEVCACHE_DOCKERHUB_REGISTRY, daemon-json registry-mirrors
#   DEVCACHE_MCR_MIRROR       mcr.microsoft.com cache URL  -> DEVCACHE_MCR_REGISTRY
#   DEVCACHE_GHCR_MIRROR      ghcr.io cache URL            -> DEVCACHE_GHCR_REGISTRY, TRIVY_DB_REPOSITORY,
#                                                             TRIVY_JAVA_DB_REPOSITORY (+ TRIVY_INSECURE for http)
#   DEVCACHE_PLAYWRIGHT_HOST  Playwright browser cache URL -> PLAYWRIGHT_DOWNLOAD_HOST
#
#   DEVCACHE_PROBE_TIMEOUT    seconds per reachability probe (default 2)
#   DEVCACHE_CONFIG           file of DEVCACHE_*=value lines (default /etc/devcache/client.env). A
#                             variable present in the environment, even empty, wins over the file.
#
# Formats:
#   sh           `export K='v'` lines:   eval "$(devcache-env)"
#   env          `K=v` lines, for docker --env-file and $GITHUB_ENV:  devcache-env env >> "$GITHUB_ENV"
#   apt-conf     an apt.conf line with the proxy, or nothing:  devcache-env apt-conf > /etc/apt/apt.conf.d/01devcache
#   apt-detect   the apt proxy URL, or DIRECT (what apt's Proxy-Auto-Detect expects)
#   daemon-json  a dockerd daemon.json fragment (registry-mirrors, insecure-registries); not probed,
#                because dockerd falls back to the upstream itself when a mirror fails
#   report       one line per cache: configured value and whether it answers
set -u

VARS="DEVCACHE_GOPROXY DEVCACHE_GOPRIVATE DEVCACHE_NPM_REGISTRY DEVCACHE_APT_PROXY DEVCACHE_DOCKER_MIRROR DEVCACHE_MCR_MIRROR DEVCACHE_GHCR_MIRROR DEVCACHE_PLAYWRIGHT_HOST DEVCACHE_PROBE_TIMEOUT"

die() { echo "devcache-env: $*" >&2; exit 2; }

# load_config: fill DEVCACHE_* variables that are absent from the
# environment from DEVCACHE_CONFIG. Only KEY=value lines for known keys are
# read; the file is never sourced.
load_config() {
	f="${DEVCACHE_CONFIG:-/etc/devcache/client.env}"
	[ -r "$f" ] || return 0
	for v in $VARS; do
		eval "isset=\${$v+x}"
		[ -z "$isset" ] || continue
		line="$(grep -E "^[[:space:]]*(export[[:space:]]+)?$v=" "$f" | tail -n 1)" || continue
		[ -n "$line" ] || continue
		val="${line#*=}"
		val="$(printf '%s' "$val" | sed -e 's/[[:space:]]*$//' -e "s/^'\(.*\)'\$/\1/" -e 's/^"\(.*\)"$/\1/')"
		case "$val" in *[\'\"\`\$\\]*) die "$f: $v: quotes, backticks, \$ and \\ are not allowed in values" ;; esac
		eval "$v=\$val"
	done
}

# url_hostport URL: host:port of an http(s) URL, with the scheme's default port.
url_hostport() {
	rest="${1#*://}"
	rest="${rest%%/*}"
	case "$rest" in
	*\]:*) printf '%s\n' "$rest" ;;                       # [v6]:port
	\[*\]) printf '%s:%s\n' "$rest" "$(default_port "$1")" ;; # [v6]
	*:*) printf '%s\n' "$rest" ;;
	*) printf '%s:%s\n' "$rest" "$(default_port "$1")" ;;
	esac
}
default_port() { case "$1" in https://*) echo 443 ;; *) echo 80 ;; esac; }

# registry_host URL: what an image reference uses: host[:port], no scheme.
registry_host() { r="${1#*://}"; printf '%s\n' "${r%%/*}"; }

valid_url() { case "$1" in http://?* | https://?*) return 0 ;; *) return 1 ;; esac; }

# reachable URL: any HTTP answer within the timeout. curl, then wget, then a
# bare TCP connect through bash; with none of them, unreachable (no proxy).
reachable() {
	t="${DEVCACHE_PROBE_TIMEOUT:-2}"
	if command -v curl >/dev/null 2>&1; then
		curl -s -o /dev/null --connect-timeout "$t" --max-time "$((t + 1))" "$1" 2>/dev/null
		return $?
	fi
	if command -v wget >/dev/null 2>&1; then
		wget -S --spider -q -T "$t" -t 1 "$1" 2>&1 | grep -q 'HTTP/'
		return $?
	fi
	if command -v bash >/dev/null 2>&1 && command -v timeout >/dev/null 2>&1; then
		hp="$(url_hostport "$1")"
		h="${hp%:*}"; p="${hp##*:}"; h="${h#[}"; h="${h%]}"
		# shellcheck disable=SC2016 # expanded by the inner bash, from its arguments
		timeout "$t" bash -c ': < "/dev/tcp/$0/$1"' "$h" "$p" 2>/dev/null
		return $?
	fi
	return 1
}

# use VAR: succeed when VAR is a set, valid, reachable URL.
use() {
	eval "u=\${$1:-}"
	[ -n "$u" ] || return 1
	valid_url "$u" || { echo "devcache-env: $1=$u is not an http(s) URL; ignored" >&2; return 1; }
	reachable "$u"
}

# merge_patterns A B: comma-separated union, order kept, duplicates dropped.
merge_patterns() {
	printf '%s,%s\n' "$1" "$2" | tr ',' '\n' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' |
		awk 'length($0) && !seen[$0]++' | paste -sd, -
}

OUT=""
add() { OUT="$OUT$1=$2
"; }

# settings: the KEY=value lines every env-style format prints.
settings() {
	for v in $VARS; do
		eval "val=\${$v:-}"
		[ -z "$val" ] || add "$v" "$val"
	done
	priv="$(merge_patterns "${GOPRIVATE:-}" "${DEVCACHE_GOPRIVATE:-}")"
	if [ -n "$priv" ]; then
		# GOPRIVATE implies GONOPROXY and GONOSUMDB only while those are
		# unset, so all three are set: private module paths never reach a
		# proxy or the checksum database, even on a cache miss.
		add GOPRIVATE "$priv"
		add GONOPROXY "$(merge_patterns "${GONOPROXY:-}" "$priv")"
		add GONOSUMDB "$(merge_patterns "${GONOSUMDB:-}" "$priv")"
	fi
	if use DEVCACHE_GOPROXY; then
		add GOPROXY "${DEVCACHE_GOPROXY%/}|https://proxy.golang.org|direct"
	fi
	if use DEVCACHE_NPM_REGISTRY; then
		add npm_config_registry "${DEVCACHE_NPM_REGISTRY%/}/"
	fi
	if use DEVCACHE_PLAYWRIGHT_HOST; then
		add PLAYWRIGHT_DOWNLOAD_HOST "${DEVCACHE_PLAYWRIGHT_HOST%/}"
	fi
	hub=docker.io mcr=mcr.microsoft.com ghcr=ghcr.io
	if use DEVCACHE_DOCKER_MIRROR; then hub="$(registry_host "$DEVCACHE_DOCKER_MIRROR")"; fi
	if use DEVCACHE_MCR_MIRROR; then mcr="$(registry_host "$DEVCACHE_MCR_MIRROR")"; fi
	add DEVCACHE_DOCKERHUB_REGISTRY "$hub"
	add DEVCACHE_MCR_REGISTRY "$mcr"
	if use DEVCACHE_GHCR_MIRROR; then
		ghcr="$(registry_host "$DEVCACHE_GHCR_MIRROR")"
		add TRIVY_DB_REPOSITORY "$ghcr/aquasecurity/trivy-db:2"
		add TRIVY_JAVA_DB_REPOSITORY "$ghcr/aquasecurity/trivy-java-db:1"
		case "$DEVCACHE_GHCR_MIRROR" in http://*) add TRIVY_INSECURE true ;; esac
	fi
	add DEVCACHE_GHCR_REGISTRY "$ghcr"
}

# apt_proxy: the apt proxy URL when usable, else nothing.
apt_proxy() { if use DEVCACHE_APT_PROXY; then printf '%s\n' "${DEVCACHE_APT_PROXY%/}"; fi; }

json_list() {
	first=1
	printf '['
	for x in "$@"; do
		[ "$first" = 1 ] || printf ', '
		first=0
		printf '"%s"' "$x"
	done
	printf ']'
}

daemon_json() {
	mirrors="" insecure=""
	for v in DEVCACHE_DOCKER_MIRROR DEVCACHE_MCR_MIRROR DEVCACHE_GHCR_MIRROR; do
		eval "u=\${$v:-}"
		[ -n "$u" ] || continue
		valid_url "$u" || { echo "devcache-env: $v=$u is not an http(s) URL; ignored" >&2; continue; }
		[ "$v" != DEVCACHE_DOCKER_MIRROR ] || mirrors="${u%/}"
		case "$u" in http://*) insecure="$insecure $(registry_host "$u")" ;; esac
	done
	# shellcheck disable=SC2086
	printf '{"registry-mirrors": %s, "insecure-registries": %s}\n' \
		"$(if [ -n "$mirrors" ]; then json_list "$mirrors"; else printf '[]'; fi)" \
		"$(set -f; json_list $insecure)"
}

report() {
	for v in DEVCACHE_GOPROXY DEVCACHE_NPM_REGISTRY DEVCACHE_APT_PROXY DEVCACHE_DOCKER_MIRROR DEVCACHE_MCR_MIRROR DEVCACHE_GHCR_MIRROR DEVCACHE_PLAYWRIGHT_HOST; do
		eval "u=\${$v:-}"
		if [ -z "$u" ]; then s="unset (no proxy)"
		elif ! valid_url "$u"; then s="INVALID (no proxy)"
		elif reachable "$u"; then s="up"
		else s="UNREACHABLE (no proxy)"; fi
		printf '%-26s %-40s %s\n' "$v" "${u:--}" "$s"
	done
	printf '%-26s %s\n' DEVCACHE_GOPRIVATE "${DEVCACHE_GOPRIVATE:--}"
}

load_config
fmt="${1:-sh}"
case "${0##*/}" in devcache-apt-proxy) fmt=apt-detect ;; esac

case "$fmt" in
sh)
	settings
	printf '%s' "$OUT" | while IFS= read -r line; do
		[ -n "$line" ] || continue
		k="${line%%=*}" val="${line#*=}"
		printf "export %s='%s'\n" "$k" "$(printf '%s' "$val" | sed "s/'/'\\\\''/g")"
	done
	;;
env) settings; printf '%s' "$OUT" ;;
apt-conf)
	p="$(apt_proxy)"
	if [ -n "$p" ]; then printf 'Acquire::http::Proxy "%s";\n' "$p"; fi
	;;
apt-detect) p="$(apt_proxy)"; printf '%s\n' "${p:-DIRECT}" ;;
daemon-json) daemon_json ;;
report) report ;;
-h | --help | help) sed -n '2,36p' "$0" | sed 's/^# \{0,1\}//' ;;
*) die "unknown format '$fmt' (sh, env, apt-conf, apt-detect, daemon-json, report)" ;;
esac
DEVCACHE_ENV_SCRIPT
RUN echo "54ebcd1955daa328f31ebe8c93b50b71fcb9489a4a45a0a2986c530d4517cd29  /usr/local/bin/devcache-env" | sha256sum -c -

# pkgconf, libgtk-3-dev and libwebkit2gtk-4.1-dev are what `make build` needs to
# link the client's embedded desktop GUI (CGO + Wails/GTK/WebKit): the same
# packages CI's Build job installs before `make build`, and provisioned here
# for the same reason CI gives — rather than narrowing what the Build gate
# covers. Without them build-cli dies on its pkg-config guard, and a worker
# either fails verification (go-quicbaas #53, 2026-09-21) or installs them
# by hand in every round.
#
# cryptsetup is a TEST dependency of a factory project, not of this repo.
# go-luks2's unit tier runs LUKS2 conformance tests against the reference
# implementation and fails them on purpose when the binary is absent (they
# read and write image files; no loop device, no root). Without it every
# go-luks2 delivery failed `make test` on six TestConformance_* cases however
# correct the change was (go-quicbaas #1003, go-luks2 #12 and #114). Debian
# ships the binary in cryptsetup-bin, at /usr/sbin/cryptsetup -- a directory a
# non-root login shell's PATH lacks until the profile.d file below adds it.
#
# SHELLCHECK, actionlint's SC* backend (see the header). It is a Haskell
# program, so unlike the Go tools it cannot be `go install`ed; the upstream
# release binary is statically linked. The version
# and the SHA-256 of each architecture's tarball are the Makefile's
# (SHELLCHECK_VERSION, SHELLCHECK_SHA256_*), passed in by `make
# build-worker-image` with no default here for the same reason as the Go pins:
# a default is a second copy that drifts. The digest is checked because the
# project publishes no checksum file to check it against. It lands in
# /usr/local/bin, which survives the login shell's PATH reset below.
ARG SHELLCHECK_VERSION
ARG SHELLCHECK_SHA256_X86_64
ARG SHELLCHECK_SHA256_AARCH64
RUN set -eux; \
    if [ -z "${SHELLCHECK_VERSION:-}" ] || [ -z "${SHELLCHECK_SHA256_X86_64:-}" ] || [ -z "${SHELLCHECK_SHA256_AARCH64:-}" ]; then \
        echo "SHELLCHECK_VERSION, SHELLCHECK_SHA256_X86_64 and SHELLCHECK_SHA256_AARCH64 are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the Makefile's pins." >&2; \
        exit 1; \
    fi; \
    arch="$(uname -m)"; \
    case "$arch" in \
        x86_64) sum="$SHELLCHECK_SHA256_X86_64" ;; \
        aarch64) sum="$SHELLCHECK_SHA256_AARCH64" ;; \
        *) echo "no pinned shellcheck release for $arch" >&2; exit 1 ;; \
    esac; \
    tarball="shellcheck-${SHELLCHECK_VERSION}.linux.${arch}.tar.xz"; \
    curl -fsSL -o "/tmp/$tarball" "https://github.com/koalaman/shellcheck/releases/download/${SHELLCHECK_VERSION}/$tarball"; \
    echo "$sum  /tmp/$tarball" | sha256sum -c -; \
    tar -xJf "/tmp/$tarball" -C /tmp; \
    install -m 0755 "/tmp/shellcheck-${SHELLCHECK_VERSION}/shellcheck" /usr/local/bin/shellcheck; \
    rm -rf "/tmp/$tarball" "/tmp/shellcheck-${SHELLCHECK_VERSION}"; \
    shellcheck --version

# PROTOC, the protobuf compiler a Go project with .proto files regenerates
# code with inside this image, instead of host-mounting protoc (which breaks
# the container-only tooling rule). `protoc --version` prints `libprotoc
# <PROTOC_VERSION>`, the version a generated .pb.go names in its `// protoc`
# header -- so moving the pin means a project with committed .pb.go files
# regenerates them (pkg/version/toolchain.mk).
#
# FROM THE RELEASE ZIP. Upstream publishes no checksum file for the release
# asset, so the SHA-256 of each architecture's zip is pinned beside the
# version, like shellcheck's -- passed in by `make build-worker-image` with no
# default here, for the same reason as every other pin: a default is a second
# copy that drifts.
#
# include/google/protobuf/*.proto (the well-known types) lands at
# /usr/local/include, which protoc searches by default relative to its own
# bin directory (<bin>/../include) -- no -I needed for them.
ARG PROTOC_VERSION
ARG PROTOC_SHA256_X86_64
ARG PROTOC_SHA256_AARCH64
RUN set -eux; \
    if [ -z "${PROTOC_VERSION:-}" ] || [ -z "${PROTOC_SHA256_X86_64:-}" ] || [ -z "${PROTOC_SHA256_AARCH64:-}" ]; then \
        echo "PROTOC_VERSION, PROTOC_SHA256_X86_64 and PROTOC_SHA256_AARCH64 are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the Makefile's pins." >&2; \
        exit 1; \
    fi; \
    arch="$(uname -m)"; \
    case "$arch" in \
        x86_64) zipname="protoc-${PROTOC_VERSION}-linux-x86_64.zip"; sum="$PROTOC_SHA256_X86_64" ;; \
        aarch64) zipname="protoc-${PROTOC_VERSION}-linux-aarch_64.zip"; sum="$PROTOC_SHA256_AARCH64" ;; \
        *) echo "no pinned protoc release for $arch" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o "/tmp/$zipname" "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/$zipname"; \
    echo "$sum  /tmp/$zipname" | sha256sum -c -; \
    mkdir -p /tmp/protoc; \
    unzip -q "/tmp/$zipname" -d /tmp/protoc; \
    install -m 0755 /tmp/protoc/bin/protoc /usr/local/bin/protoc; \
    mkdir -p /usr/local/include; \
    cp -r /tmp/protoc/include/. /usr/local/include/; \
    rm -rf "/tmp/$zipname" /tmp/protoc; \
    test "$(protoc --version)" = "libprotoc ${PROTOC_VERSION}"

# NODE.JS runs the claude and codex CLIs, the npm-built language servers
# below and a project's own frontend build. FROM THE nodejs.org RELEASE
# TARBALL, because Debian's nodejs is a major behind the line the agent CLIs
# track. Each architecture's SHA-256 is pinned beside the version, as the
# release's SHASUMS256.txt publishes it (toolchain.mk NODE_*), and the bundled
# npm is upgraded to NPM_VERSION in the npm layer below.
ARG NODE_VERSION
ARG NODE_SHA256_X86_64
ARG NODE_SHA256_AARCH64
RUN set -eux; \
    if [ -z "${NODE_VERSION:-}" ] || [ -z "${NODE_SHA256_X86_64:-}" ] || [ -z "${NODE_SHA256_AARCH64:-}" ]; then \
        echo "NODE_VERSION, NODE_SHA256_X86_64 and NODE_SHA256_AARCH64 are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the repository's pins." >&2; \
        exit 1; \
    fi; \
    case "$(uname -m)" in \
        x86_64) platform="linux-x64"; sum="$NODE_SHA256_X86_64" ;; \
        aarch64) platform="linux-arm64"; sum="$NODE_SHA256_AARCH64" ;; \
        *) echo "no pinned node release for $(uname -m)" >&2; exit 1 ;; \
    esac; \
    tarball="node-${NODE_VERSION}-${platform}.tar.xz"; \
    curl -fsSL -o "/tmp/$tarball" "https://nodejs.org/dist/${NODE_VERSION}/$tarball"; \
    echo "$sum  /tmp/$tarball" | sha256sum -c -; \
    tar -xJf "/tmp/$tarball" -C /usr/local --strip-components=1 --no-same-owner; \
    rm -f "/tmp/$tarball" /usr/local/CHANGELOG.md /usr/local/README.md /usr/local/LICENSE; \
    test "$(node --version)" = "${NODE_VERSION}"

# THE RELEASE-BINARY LANGUAGE SERVERS serena drives: clangd (C/C++),
# rust-analyzer (Rust) and marksman (markdown). Each from its project's own
# release, its SHA-256 per architecture pinned beside the version as the
# release's asset digest publishes it (toolchain.mk), checked here.
#
# clangd ships only bin/clangd and its builtin headers (lib/clang/<major>/
# include) from the release's 220 MB: the rest is compiler-rt, which a
# language server never links. clangd publishes no aarch64 Linux build, so an
# aarch64 image takes Debian's clangd instead, at the same /usr/local/bin path
# serena's settings name.
#
# rust-analyzer runs WITHOUT cargo: there is no Rust toolchain here, so it
# reads a crate's syntax -- symbols, definitions, references within the
# workspace -- and logs that it cannot load the sysroot. That is what serena
# needs; a project that builds Rust installs rustup with sudo.
ARG CLANGD_VERSION
ARG CLANGD_SHA256_X86_64
ARG RUST_ANALYZER_VERSION
ARG RUST_ANALYZER_SHA256_X86_64
ARG RUST_ANALYZER_SHA256_AARCH64
ARG MARKSMAN_VERSION
ARG MARKSMAN_SHA256_X86_64
ARG MARKSMAN_SHA256_AARCH64
RUN set -eux; \
    for pin in CLANGD_VERSION CLANGD_SHA256_X86_64 RUST_ANALYZER_VERSION RUST_ANALYZER_SHA256_X86_64 RUST_ANALYZER_SHA256_AARCH64 \
               MARKSMAN_VERSION MARKSMAN_SHA256_X86_64 MARKSMAN_SHA256_AARCH64; do \
        if [ -z "$(printenv "$pin")" ]; then \
            echo "$pin is a required build argument. Build with 'make build-worker-image', which passes the repository's pins." >&2; \
            exit 1; \
        fi; \
    done; \
    case "$(uname -m)" in \
        x86_64) ra="x86_64-unknown-linux-gnu"; ra_sum="$RUST_ANALYZER_SHA256_X86_64"; mk="linux-x64"; mk_sum="$MARKSMAN_SHA256_X86_64" ;; \
        aarch64) ra="aarch64-unknown-linux-gnu"; ra_sum="$RUST_ANALYZER_SHA256_AARCH64"; mk="linux-arm64"; mk_sum="$MARKSMAN_SHA256_AARCH64" ;; \
        *) echo "no pinned language-server releases for $(uname -m)" >&2; exit 1 ;; \
    esac; \
    if [ "$(uname -m)" = x86_64 ]; then \
        zipname="clangd-linux-${CLANGD_VERSION}.zip"; \
        curl -fsSL -o "/tmp/$zipname" "https://github.com/clangd/clangd/releases/download/${CLANGD_VERSION}/$zipname"; \
        echo "$CLANGD_SHA256_X86_64  /tmp/$zipname" | sha256sum -c -; \
        unzip -q "/tmp/$zipname" "clangd_${CLANGD_VERSION}/bin/clangd" "clangd_${CLANGD_VERSION}/lib/clang/*/include/*" -d /tmp/clangd; \
        mkdir -p /opt/clangd; \
        cp -r "/tmp/clangd/clangd_${CLANGD_VERSION}/bin" "/tmp/clangd/clangd_${CLANGD_VERSION}/lib" /opt/clangd/; \
        ln -s /opt/clangd/bin/clangd /usr/local/bin/clangd; \
        rm -rf "/tmp/$zipname" /tmp/clangd; \
        clangd --version | grep -qF "clangd version ${CLANGD_VERSION}"; \
    else \
        export DEBIAN_FRONTEND=noninteractive; \
        apt-get update; \
        apt-get install -y --no-install-recommends clangd; \
        rm -rf /var/lib/apt/lists/*; \
        ln -s /usr/bin/clangd /usr/local/bin/clangd; \
        clangd --version; \
    fi; \
    curl -fsSL -o /tmp/rust-analyzer.gz "https://github.com/rust-lang/rust-analyzer/releases/download/${RUST_ANALYZER_VERSION}/rust-analyzer-${ra}.gz"; \
    echo "$ra_sum  /tmp/rust-analyzer.gz" | sha256sum -c -; \
    gunzip -c /tmp/rust-analyzer.gz > /usr/local/bin/rust-analyzer; \
    chmod 0755 /usr/local/bin/rust-analyzer; \
    rm -f /tmp/rust-analyzer.gz; \
    rust-analyzer --version; \
    curl -fsSL -o /tmp/marksman "https://github.com/artempyanykh/marksman/releases/download/${MARKSMAN_VERSION}/marksman-${mk}"; \
    echo "$mk_sum  /tmp/marksman" | sha256sum -c -; \
    install -m 0755 /tmp/marksman /usr/local/bin/marksman; \
    rm -f /tmp/marksman; \
    test "$(marksman --version)" = "${MARKSMAN_VERSION}"

# NPM, AND THE npm-PUBLISHED LANGUAGE SERVERS serena drives: pyright (Python),
# typescript-language-server with the TypeScript it drives (TypeScript and
# JavaScript), bash-language-server and yaml-language-server. Each at its pin
# (toolchain.mk), into a prefix of their own rather than the global one, so
# they are serena's servers and not tools a project's build picks up: serena's
# settings name their bin/ paths (the serena block below).
#
# TYPESCRIPT STAYS ON 6.x: typescript-language-server drives tsserver.js, which
# TypeScript 7 (the native rewrite) no longer ships. The server resolves
# `typescript` from its own prefix, where this install puts it.
#
# DEVCACHE_NPM_REGISTRY: the fleet's npm cache (devops.md section 2a). NO
# DEFAULT and NEVER an ENV, for the same reason as the Go caches above.
# devcache-env (installed earlier in this stage) sets npm_config_registry
# only when it is set and the cache answers; unset or unreachable, npm keeps
# its own default registry. It is eval'd in every RUN that installs from npm.
#
# NPM_BUNDLED_DEPENDENCY_FLOORS replaces packages npm bundles in its own
# node_modules with the patch release that fixes a CVE the trivy gate reads
# there (toolchain.mk). Each name@version=integrity entry is fetched with
# `npm pack` -- through the same registry as every other npm fetch here -- its
# tarball checked against the pinned registry integrity, and unpacked over
# npm's copy. The build fails when npm already bundles the floor or newer, or
# no longer bundles the package, so a stale floor cannot outlive the npm
# release that fixed it; and when a floor is more than a patch release of
# npm's copy, so npm keeps the major and minor its own dependents declared.
# The language-server install that follows is the proof npm still works.
ARG NPM_VERSION
ARG NPM_BUNDLED_DEPENDENCY_FLOORS
ARG PYRIGHT_VERSION
ARG TYPESCRIPT_VERSION
ARG TYPESCRIPT_LANGUAGE_SERVER_VERSION
ARG BASH_LANGUAGE_SERVER_VERSION
ARG YAML_LANGUAGE_SERVER_VERSION
ARG DEVCACHE_NPM_REGISTRY
RUN set -eux; \
    for pin in NPM_VERSION NPM_BUNDLED_DEPENDENCY_FLOORS PYRIGHT_VERSION TYPESCRIPT_VERSION TYPESCRIPT_LANGUAGE_SERVER_VERSION BASH_LANGUAGE_SERVER_VERSION YAML_LANGUAGE_SERVER_VERSION; do \
        if [ -z "$(printenv "$pin")" ]; then \
            echo "$pin is a required build argument. Build with 'make build-worker-image', which passes the repository's pins." >&2; \
            exit 1; \
        fi; \
    done; \
    eval "$(devcache-env)"; \
    npm install -g "npm@${NPM_VERSION}"; \
    test "$(npm --version)" = "${NPM_VERSION}"; \
    npm_modules="$(npm root -g)/npm/node_modules"; \
    floors_dir="$(mktemp -d)"; \
    for floor in ${NPM_BUNDLED_DEPENDENCY_FLOORS}; do \
        spec="${floor%%=*}"; \
        integrity="${floor#*=}"; \
        name="${spec%@*}"; \
        want="${spec##*@}"; \
        if [ ! -f "$npm_modules/$name/package.json" ]; then \
            echo "npm ${NPM_VERSION} no longer bundles $name: drop it from NPM_BUNDLED_DEPENDENCY_FLOORS." >&2; \
            exit 1; \
        fi; \
        have="$(node -p 'require(process.argv[1]).version' "$npm_modules/$name/package.json")"; \
        if [ "$(printf '%s\n%s\n' "$want" "$have" | sort -V | tail -n 1)" = "$have" ]; then \
            echo "npm ${NPM_VERSION} bundles $name $have, at or above the floor $want: drop it from NPM_BUNDLED_DEPENDENCY_FLOORS." >&2; \
            exit 1; \
        fi; \
        if [ "${have%.*}" != "${want%.*}" ]; then \
            echo "The $name floor $want is not a patch release of npm's bundled $have." >&2; \
            exit 1; \
        fi; \
        tarball="$floors_dir/$(npm pack --silent --pack-destination "$floors_dir" "$spec")"; \
        actual="$(node -p '"sha512-" + require("crypto").createHash("sha512").update(require("fs").readFileSync(process.argv[1])).digest("base64")' "$tarball")"; \
        if [ "$actual" != "$integrity" ]; then \
            echo "$spec: tarball integrity $actual, pinned $integrity." >&2; \
            exit 1; \
        fi; \
        rm -rf "${npm_modules:?}/$name"; \
        mkdir "$npm_modules/$name"; \
        tar -xzf "$tarball" -C "$npm_modules/$name" --strip-components=1 --no-same-owner; \
        test "$(node -p 'require(process.argv[1]).version' "$npm_modules/$name/package.json")" = "$want"; \
    done; \
    rm -rf "$floors_dir"; \
    test "$(npm --version)" = "${NPM_VERSION}"; \
    npm install -g --prefix /opt/serena/language-servers \
        "pyright@${PYRIGHT_VERSION}" \
        "typescript@${TYPESCRIPT_VERSION}" \
        "typescript-language-server@${TYPESCRIPT_LANGUAGE_SERVER_VERSION}" \
        "bash-language-server@${BASH_LANGUAGE_SERVER_VERSION}" \
        "yaml-language-server@${YAML_LANGUAGE_SERVER_VERSION}"; \
    npm cache clean --force; \
    for server in pyright-langserver typescript-language-server bash-language-server yaml-language-server; do \
        test -x "/opt/serena/language-servers/bin/$server"; \
    done; \
    /opt/serena/language-servers/bin/typescript-language-server --version

# The agents themselves, exactly as deployments/agents/claude-code and
# deployments/agents/codex install them.
#
# BOTH CLIs, ONE IMAGE, DELIBERATELY. This image hosts two roles — the worker
# that writes a change and the reviewer that judges it — and
# ReviewPolicy.RequireIndependentReviewer refuses an approval from the model
# family that wrote the code, so on any one project the two roles are never the
# same CLI. What they need FROM THE IMAGE is identical: git, a real checkout,
# the non-root agent user, the sudo grant and the workspace contract. A second
# Dockerfile would duplicate every one of those and then drift from this one.
# ReviewerConfig.Image stays a separate field so a project MAY differ; the
# default is that it does not.
#
# PINNED, AND THE PIN IS WHAT MOVES. The two CLIs are installed at the exact
# versions passed in, never at @latest: an image that installs @latest cannot
# be rebuilt to the same bytes, and nobody could say which CLI a bound image
# carries. Keeping them current is the daemon's job, not the build's — its
# worker image refresh (pkg/sdlc/workerimage, docs/software-factory/
# worker-image.md) reads the npm registry, rebuilds this file with the newer
# versions, gates the result and only then rebinds projects to it. The
# versions are recorded as labels below, which is how that refresh knows what
# a bound image carries without running it.
#
# ONE PACKAGE MAY RUN ITS INSTALL SCRIPT. npm 12 runs no install-time script
# a package does not name on --allow-scripts. Claude Code needs its own: its
# postinstall (install.cjs, offline) puts the native binary from its platform
# package (@anthropic-ai/claude-code-linux-*) over the bin/claude placeholder,
# and without it `claude` only reports "native binary not installed". The
# allowance names that package at its pin, so no other package, nor another
# version of this one, runs a script. Codex needs none: its native binaries
# arrive as plain platform packages. Gemini needs none either:
# @google/gemini-cli declares no scripts, and the optional dependencies that
# do (node-pty, @github/keytar) are skipped without them -- the pty arrives
# prebuilt as the @lydell/node-pty-linux-* platform package. The npm and language-server installs
# above need none either; the only script in their tree is fsevents, which is
# macOS-only and never installed here.
ARG CLAUDE_CODE_VERSION
ARG CODEX_VERSION
ARG GEMINI_CLI_VERSION
RUN set -eux; \
    if [ -z "${CLAUDE_CODE_VERSION:-}" ] || [ -z "${CODEX_VERSION:-}" ] || [ -z "${GEMINI_CLI_VERSION:-}" ]; then \
        echo "CLAUDE_CODE_VERSION, CODEX_VERSION and GEMINI_CLI_VERSION are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the Makefile's pins." >&2; \
        exit 1; \
    fi; \
    eval "$(devcache-env)"; \
    npm install -g --allow-scripts="@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" \
        "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" "@openai/codex@${CODEX_VERSION}" \
        "@google/gemini-cli@${GEMINI_CLI_VERSION}"; \
    npm cache clean --force; \
    claude --version; \
    codex --version; \
    gemini --version

# OPENCODE, the third agent CLI: the one a fleet node drives against a LOCAL
# model server, where claude and codex drive hosted ones. A node that serves
# the local review model advertises the "opencode" backend, and a reviewer
# placed there runs this binary in this image -- so an image without it is a
# node that advertises a backend it cannot run.
#
# FROM THE RELEASE TARBALL, NOT FROM npm. opencode ships as one Bun binary per
# platform, and the glibc build is the one this Debian image runs.
# The version and each architecture's SHA-256 are the repository's pins
# (pkg/version/toolchain.mk: OPENCODE_VERSION, OPENCODE_SHA256_*), taken from
# the digests the release itself publishes and passed in by `make
# build-worker-image` and the daemon's refresh with no default here, for the
# same reason as every other pin: a default is a second copy that drifts. The
# digest is CHECKED, so a tarball that is not the release's fails the build.
ARG OPENCODE_VERSION
ARG OPENCODE_SHA256_X86_64
ARG OPENCODE_SHA256_AARCH64
RUN set -eux; \
    if [ -z "${OPENCODE_VERSION:-}" ] || [ -z "${OPENCODE_SHA256_X86_64:-}" ] || [ -z "${OPENCODE_SHA256_AARCH64:-}" ]; then \
        echo "OPENCODE_VERSION, OPENCODE_SHA256_X86_64 and OPENCODE_SHA256_AARCH64 are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the repository's pins." >&2; \
        exit 1; \
    fi; \
    arch="$(uname -m)"; \
    case "$arch" in \
        x86_64) platform="linux-x64"; sum="$OPENCODE_SHA256_X86_64" ;; \
        aarch64) platform="linux-arm64"; sum="$OPENCODE_SHA256_AARCH64" ;; \
        *) echo "no pinned opencode release for $arch" >&2; exit 1 ;; \
    esac; \
    tarball="opencode-${platform}.tar.gz"; \
    curl -fsSL -o "/tmp/$tarball" "https://github.com/anomalyco/opencode/releases/download/${OPENCODE_VERSION}/$tarball"; \
    echo "$sum  /tmp/$tarball" | sha256sum -c -; \
    mkdir -p /tmp/opencode; \
    tar -xzf "/tmp/$tarball" -C /tmp/opencode; \
    install -m 0755 /tmp/opencode/opencode /usr/local/bin/opencode; \
    rm -rf "/tmp/$tarball" /tmp/opencode; \
    test "$(opencode --version)" = "${OPENCODE_VERSION#v}"

# RUBY-LSP, Ruby's language server: a gem, installed against Debian's ruby at
# its pin (toolchain.mk RUBY_LSP_VERSION), into /usr/local/bin where serena
# finds it on the PATH. Offline and with an empty HOME it cannot compose a
# bundle for a project and falls back to the gems installed here -- which is
# all reading by symbol needs. It re-execs through `bundle`, which is Debian's
# ruby-bundler, installed above.
ARG RUBY_LSP_VERSION
RUN set -eux; \
    if [ -z "${RUBY_LSP_VERSION:-}" ]; then \
        echo "RUBY_LSP_VERSION is a required build argument." >&2; \
        echo "Build with 'make build-worker-image', which passes the repository's pins." >&2; \
        exit 1; \
    fi; \
    gem install --no-document ruby-lsp -v "${RUBY_LSP_VERSION}"; \
    rm -rf "$(gem env gemdir)/cache"; \
    test "$(ruby-lsp --version)" = "${RUBY_LSP_VERSION}"

# UV, which installs serena below and is on the PATH for a Python project.
# From its release tarball -- Debian packages no uv -- with each
# architecture's SHA-256 pinned beside the version, as the release's own
# .sha256 files publish it (toolchain.mk UV_*).
ARG UV_VERSION
ARG UV_SHA256_X86_64
ARG UV_SHA256_AARCH64
RUN set -eux; \
    if [ -z "${UV_VERSION:-}" ] || [ -z "${UV_SHA256_X86_64:-}" ] || [ -z "${UV_SHA256_AARCH64:-}" ]; then \
        echo "UV_VERSION, UV_SHA256_X86_64 and UV_SHA256_AARCH64 are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the repository's pins." >&2; \
        exit 1; \
    fi; \
    case "$(uname -m)" in \
        x86_64) target="x86_64-unknown-linux-gnu"; sum="$UV_SHA256_X86_64" ;; \
        aarch64) target="aarch64-unknown-linux-gnu"; sum="$UV_SHA256_AARCH64" ;; \
        *) echo "no pinned uv release for $(uname -m)" >&2; exit 1 ;; \
    esac; \
    tarball="uv-${target}.tar.gz"; \
    curl -fsSL -o "/tmp/$tarball" "https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/$tarball"; \
    echo "$sum  /tmp/$tarball" | sha256sum -c -; \
    tar -xzf "/tmp/$tarball" -C /tmp; \
    install -m 0755 "/tmp/uv-${target}/uv" "/tmp/uv-${target}/uvx" /usr/local/bin/; \
    rm -rf "/tmp/$tarball" "/tmp/uv-${target}"; \
    test "$(uv --version | cut -d' ' -f2)" = "${UV_VERSION}"

# SERENA, the semantic code-retrieval MCP server, so an agent reads this
# codebase by symbol instead of by whole file.
#
# INSTALLED AT BUILD TIME, NOT RUN THROUGH `uvx --from git+...`. That is how a
# developer runs serena on a workstation, and it is wrong here twice over: a
# sandboxed spawn is not promised egress to GitHub, and a clone-per-spawn makes
# every session pay for it and fail whenever the network does. `uv tool install`
# resolves it once, into the image, at a pinned tag, against Debian's python3.
#
# PINNED, because an image that installs @latest cannot be rebuilt to the same
# bytes, and an agent's search behaviour changing under a rebuild is a change
# nobody authored.
#
# THE LANGUAGE SERVERS IT DRIVES ARE BAKED IN, not left to serena's on-demand
# download: gopls (copied from the toolchain stage below), the npm servers,
# clangd, rust-analyzer, marksman and ruby-lsp, all installed above. The
# settings block after this one is what makes serena use them.
ARG SERENA_VERSION
# SERENA_DEPENDENCY_FLOORS raises a dependency in serena's own `uv tool
# install` environment past a fixed CVE that trivy reads out of the installed
# package list the same way it reads a Go binary's module list -- urllib3 <
# 2.8.0 (go-quicbaas#1161). Space-separated requirement specifiers (toolchain.mk
# SERENA_DEPENDENCY_FLOORS), no default for the same reason as every other
# pin: a default is a second copy that drifts.
#
# `--overrides`, NOT `--with`. serena-agent 1.7.0 depends on urllib3==2.7.0
# EXACTLY, and `--with` only ADDS a constraint alongside the package's own
# declared dependencies -- uv then has two conflicting requirements on
# urllib3 and refuses to resolve at all ("No solution found ... your
# requirements are unsatisfiable"). `--overrides <file>` REPLACES the
# declared pin instead, which is the uv analogue of what the throwaway
# module above does for gopls/gosec's go.mod requirements.
ARG SERENA_DEPENDENCY_FLOORS
# UV_TOOL_DIR IS THE ONE THAT MATTERS, AND ITS DEFAULT IS UNREADABLE HERE.
# `uv tool install` runs as root during the build, and by default puts the
# tool's virtualenv under /root/.local/share/uv/tools with the shim in
# UV_TOOL_BIN_DIR symlinked into it. /root is mode 0700, so the shim resolves to
# a path the spawn's non-root uid cannot traverse:
#
#   sh: serena: Permission denied
#
# Naming a tool directory outside /root, and making the tree world-readable
# below, is what lets the user the spawn actually runs as execute it.
ENV UV_TOOL_DIR=/opt/uv/tools \
    UV_TOOL_BIN_DIR=/usr/local/bin \
    UV_NO_PROGRESS=1
RUN set -eux; \
    if [ -z "${SERENA_VERSION:-}" ] || [ -z "${SERENA_DEPENDENCY_FLOORS:-}" ]; then \
        echo "SERENA_VERSION and SERENA_DEPENDENCY_FLOORS are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the Makefile's pins." >&2; \
        exit 1; \
    fi; \
    printf '%s\n' ${SERENA_DEPENDENCY_FLOORS} > /tmp/serena-overrides.txt; \
    uv tool install --python /usr/bin/python3 \
        --from "git+https://github.com/oraios/serena@${SERENA_VERSION}" serena-agent \
        --overrides /tmp/serena-overrides.txt; \
    rm -f /tmp/serena-overrides.txt; \
    uv cache clean; \
    chmod -R a+rX /opt/uv; \
    serena --version

# SERENA'S GLOBAL LANGUAGE-SERVER SETTINGS, so a FRESH HOME USES THE BAKED-IN
# SERVERS.
#
# Serena reads ~/.serena/serena_config.yml and, when there is none -- every
# lane, whose HOME starts empty -- writes one from the template it ships. Left
# alone, that template names no server, and serena downloads each one into
# ~/.serena/language_servers on first use: a fetch per session, and a failure
# in a sandbox without egress. So the template's one `ls_specific_settings: {}`
# line is replaced with the block below, and every config serena generates
# points at the servers this image carries. Go (gopls, in GOPATH/bin), Rust
# (rust-analyzer) and Ruby (ruby-lsp) need no entry: serena runs them from the
# PATH. A project's own ls_specific_settings (.serena/project.yml) replace
# these language by language.
#
# THE COPY AT /etc/serena IS THE DOCUMENTED DEFAULT for a HOME that already
# holds a serena_config.yml, which serena never regenerates: splice the block
# into it (docs/agent-fleet/worker-image.md).
#
# THE BUILD ASSERTS THE RESULT rather than trusting the substitution: a serena
# release that reshapes its template fails the build here, and a config loaded
# under an empty HOME must carry exactly these settings.
COPY <<'SERENA_LS_SETTINGS' /etc/serena/ls_specific_settings.yml
ls_specific_settings:
  python:
    ls_path: /opt/serena/language-servers/bin/pyright-langserver
  typescript:
    ls_path: /opt/serena/language-servers/bin/typescript-language-server
  bash:
    ls_path: /opt/serena/language-servers/bin/bash-language-server
  yaml:
    ls_path: /opt/serena/language-servers/bin/yaml-language-server
  markdown:
    ls_path: /usr/local/bin/marksman
  cpp:
    ls_path: /usr/local/bin/clangd
SERENA_LS_SETTINGS
RUN set -eux; \
    serena_python=/opt/uv/tools/serena-agent/bin/python; \
    "$serena_python" -c 'import os, sys, serena; template = os.path.join(os.path.dirname(serena.__file__), "resources", "serena_config.template.yml"); text = open(template).read(); line = "\nls_specific_settings: {}\n"; text.count(line) == 1 or sys.exit("serena config template " + template + " no longer has exactly one ls_specific_settings: {} line; re-check the language-server settings"); open(template, "w").write(text.replace(line, "\n" + open("/etc/serena/ls_specific_settings.yml").read()))'; \
    chmod -R a+rX /opt/uv /etc/serena; \
    fresh_home="$(mktemp -d)"; \
    HOME="$fresh_home" "$serena_python" -c 'import json, sys; from ruamel.yaml import YAML; from serena.config.serena_config import SerenaConfig; want = YAML(typ="safe").load(open("/etc/serena/ls_specific_settings.yml"))["ls_specific_settings"]; got = json.loads(json.dumps(SerenaConfig.from_config_file().ls_specific_settings)); got == want or sys.exit("a fresh HOME does not inherit the language-server settings: " + json.dumps(got))'; \
    rm -rf "$fresh_home"

# THE MCP REGISTRATION IS NOT IN THIS IMAGE, and that is deliberate. Claude Code
# takes `--mcp-config` as "JSON files or strings" and codex takes
# `-c mcp_servers.serena.*`, so both are built in Go from the one constant that
# names the checkout root (pkg/orchestration, WorkerMCPArgs and
# SerenaCodexConfigArgs). A copy here would be a second place to change and
# nothing could check the two agreed.

COPY --from=toolchain /usr/local/go /usr/local/go

# GOPATH IS DECLARED, NOT LEFT TO $HOME.
#
# Go derives an unset GOPATH from the home directory, and the spawn does not
# promise what $HOME is inside the container. That matters because a Go
# project's `make lint` does not look for the linter on the PATH: the fleet's
# Makefiles resolve it as `$(shell go env GOPATH)/bin/golangci-lint` and `test
# -x` it (this repository's Makefile does exactly that). A binary installed on
# the PATH alone would therefore leave `make lint` still reporting the tool as
# missing. Declaring GOPATH here makes `go env GOPATH` a fixed, writable path
# that the tools live in and that the module cache can be written to, whatever
# $HOME turns out to be.
ENV GOPATH=/home/agent/go

# GOTOOLCHAIN=local: this image ships ONE toolchain, and a project whose go.mod
# asks for another fails loudly here rather than silently downloading one into
# a sandboxed container whose network policy is the operator's to decide.
#
# NODE_ENV is deliberately NOT set. This image runs the project's own `make
# build` and `make test`, whose TypeScript SDK and frontend install their
# compilers as devDependencies; under NODE_ENV=production `npm ci` omits
# devDependencies, tsc never lands, and `make build` fails on every project
# that builds a frontend (go-quicbaas #966, 2026-09-21). The grep is GNU grep,
# Debian's essential one, for the same reason: the Makefile's checks use
# --include/--exclude-dir and -E, which a minimal grep rejects, and a rejected
# grep in a `|| true` pipeline passes a security check that never ran.
ENV PATH="/usr/local/go/bin:${GOPATH}/bin:${PATH}" \
    GOTOOLCHAIN=local

# ENV PATH IS NOT ENOUGH, BECAUSE THE AGENT EXECS THROUGH A LOGIN SHELL.
#
# Every tool the factory runs in this container arrives as `sh -lc <command>`.
# The -l makes it a LOGIN shell, which sources /etc/profile, which on this base
# image unconditionally assigns PATH -- for a non-root user
#     PATH="/usr/local/bin:/usr/bin:/bin:/usr/local/games:/usr/games"
# — so the PATH declared above is discarded before the command runs. Measured
# on this image: `sh -c "command -v go"` finds /usr/local/go/bin/go and
# `sh -lc "command -v go"` finds nothing.
#
# THE FAILURE IS SILENT AND IT IS NOT THE ONE YOU WOULD PREDICT. Nothing
# reports a missing toolchain: Serena's Go language server simply fails to
# start, the reviewer loses symbol and reference lookup, and it returns a
# "comment" verdict saying it could not complete the review. A comment is
# neither approve nor request-changes, so the review gate stays at
# insufficient_approvals, the delivery job waits in `reviewing` for a verdict
# that will never change, and the stall detector ends it 30 minutes later.
# Five jobs died that way before the cause was found; the symptom logged is
# "job stalled", which points at the agent rather than at PATH.
#
# Restoring it from /etc/profile.d is what survives the reset: the directory is
# sourced by /etc/profile AFTER that assignment. The sbin directories are
# appended because Debian's non-root PATH omits them, and cryptsetup -- which
# a sealed-volume task runs -- lives in /usr/sbin.
RUN printf '%s\n' \
      '# Restores the toolchain PATH that /etc/profile unconditionally clears.' \
      '# The agent runs every command as `sh -lc`, so this file is what makes' \
      '# go, gofmt, the GOPATH/bin tools and the sbin tools resolvable to it.' \
      'export PATH="/usr/local/go/bin:/home/agent/go/bin:$PATH:/usr/local/sbin:/usr/sbin:/sbin"' \
      > /etc/profile.d/10-go-toolchain.sh \
    && chmod 0644 /etc/profile.d/10-go-toolchain.sh \
    && sh -lc 'command -v go >/dev/null' \
    && sh -lc 'command -v gofmt >/dev/null' \
    && sh -lc 'command -v cryptsetup >/dev/null'

# Into GOPATH/bin, which is both what `go install` would have written and what
# the projects' Makefiles probe. The builder is the same Debian release as the
# runtime stage and the binaries are static, so they run here unchanged.
COPY --from=toolchain /toolchain/bin/ ${GOPATH}/bin/

# THE USER IS THE ONE THE SPAWN WILL RUN AS, AND IT MUST EXIST IN /etc/passwd.
#
# The ACP spawn runs a sandboxed session as the DAEMON's own uid:gid
# (acp_backend.go: `User: fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())`), so
# the container writes through the bind mount as its owner. An image whose
# /etc/passwd has no entry for that uid is not merely untidy: ssh-keygen —
# which git invokes for every `gpg.format=ssh` signature — calls getpwuid at
# start and exits "No user exists for uid" without one, so EVERY signed commit
# would fail. No NSS module in the image resolves users anywhere but
# /etc/passwd, and no entrypoint runs to add one (the spawn clears it), so the entry has to be in
# the image.
#
# Hence the build argument: `make build-worker-image` passes the building
# user's own uid:gid, which on a node that builds and runs the image is the
# daemon's. An operator building it elsewhere passes the uid the daemon runs
# as. A uid the base image already has — root, when the daemon runs as root —
# is left alone rather than duplicated.
ARG AGENT_UID=1001
ARG AGENT_GID=1001

RUN set -eu; \
    if ! getent group "${AGENT_GID}" >/dev/null; then groupadd -g "${AGENT_GID}" agent; fi; \
    if ! getent passwd "${AGENT_UID}" >/dev/null; then \
        useradd -l -m -u "${AGENT_UID}" -g "${AGENT_GID}" -d /home/agent -s /bin/bash agent; \
    fi; \
    mkdir -p /workspace "${GOPATH}/bin"; \
    chown "${AGENT_UID}:${AGENT_GID}" /workspace; \
    chown -R "${AGENT_UID}:${AGENT_GID}" "${GOPATH}"

# PASSWORDLESS SUDO, BECAUSE THE CONTAINER IS THE BOUNDARY AND NOTHING INSIDE
# IT IS.
#
# A worker that cannot install a package cannot finish a task whose toolchain
# this image does not already carry, and it does not fail usefully: it reasons
# about the work, narrates a plan, and spends an attempt (go-quicbaas#927). The
# isolation an operator asked for is the container — its own root filesystem,
# its own network policy, its own memory cgroup, and no credential the engine
# did not put there — so rationing privilege a second time inside it buys
# nothing and costs attempts.
#
# SUDO RATHER THAN `USER root`, and both consequences are the reason:
#
#   - FILE OWNERSHIP ON BIND MOUNTS. The spawn runs as the DAEMON's uid
#     (acp_backend.go, sandboxSpec) precisely so the files an agent writes are
#     owned by the daemon. A container running as root would leave root-owned
#     trees on any mount that is still a host bind — the persistent-tier
#     workspace — which the daemon can then neither clean up nor re-clone into.
#   - THE TOOL GRANT ITSELF. Claude Code refuses
#     --dangerously-skip-permissions when its uid is 0, so a root container
#     would have taken back the very grant that lets the agent run commands.
#
# So privilege is reachable, not ambient: the agent runs as an unprivileged uid
# whose /etc/passwd entry keeps ssh-keygen and therefore commit signing working,
# and escalates with `sudo` when it needs root.
#
# THE RULE NAMES ALL RATHER THAN ONE USER on purpose. AGENT_UID is a build
# argument — an operator building for a daemon on another uid passes another
# value — and a sudoers rule naming a user the image was not built for is a
# rule that silently does not apply. Inside a single-purpose sandbox whose only
# human-facing account IS the agent, "every user may become root" and "the
# agent may become root" are the same statement.
RUN set -eu; \
    printf '%s\n' 'ALL ALL=(ALL:ALL) NOPASSWD: ALL' > /etc/sudoers.d/agent-full-control; \
    chmod 0440 /etc/sudoers.d/agent-full-control; \
    visudo -cf /etc/sudoers.d/agent-full-control

USER ${AGENT_UID}:${AGENT_GID}
WORKDIR /workspace

# THE SMOKE CHECK RUNS AS THE UID THE SPAWN WILL, and it fails the BUILD.
#
# The two binaries the checkout is made of are `git` and the `ssh` it execs,
# and a missing one of them is not visible until a job dies in a container an
# operator cannot see into. Running them here, after USER, is what makes "the
# image can check out over ssh" a property the build proves rather than a
# property the burn-in discovers. `ssh -V` prints to stderr and exits zero;
# what is asserted is that it resolves on PATH and runs under this uid at all.
#
# The verification toolchain is asserted the same way and for the same reason:
# a worker that cannot run `make lint` fails every review round on a clean
# tree. The last line is the one that matters most — it asserts the linter at
# the path the projects' Makefiles probe, `$(go env GOPATH)/bin`, not merely
# somewhere on the PATH, which is the difference between `make lint` running
# and `make lint` reporting the tool as missing.
#
# NO FORGE AND NO KEY IS TOUCHED. Nothing here reaches a forge, and the image
# carries no known_hosts: the forge's host key is pinned on the project's
# binding and mounted per attempt, never baked in. `govulncheck -version` is
# the one line that will consult the public Go vulnerability database if it can
# reach it; it prints its scanner version and exits 0 either way, so it neither
# leaks anything nor breaks an air-gapped build.
RUN set -eux; \
    git --version; \
    ssh -V; \
    golangci-lint --version; \
    gosec -version; \
    govulncheck -version; \
    test -x "$(go env GOPATH)/bin/golangci-lint"

# PRIVILEGE IS ASSERTED, NOT ASSUMED, and it is asserted as the uid the spawn
# runs as.
#
# Three separate things can break it and none of them is visible from a Docker
# build that merely installs the package: a missing sudoers drop-in, a sudo
# binary whose setuid bit did not survive the layer, and a uid the image has no
# /etc/passwd entry for — sudo resolves the invoking user through getpwuid and
# refuses outright ("you do not exist in the passwd database") without one.
#
# The install is proved END TO END rather than by `command -v sudo`, because
# "can escalate" and "can install" are different claims and the second is the
# one a worker needs. jq is added and purged again in the same layer, with the
# package lists it fetched, so the proof leaves the image exactly as it found it.
RUN set -eux; \
    sudo -n true; \
    test "$(sudo -n id -u)" = "0"; \
    sudo -n apt-get update -q; \
    sudo -n env DEBIAN_FRONTEND=noninteractive apt-get install -y -q --no-install-recommends jq; \
    jq --version; \
    sudo -n apt-get purge -y -q --autoremove jq; \
    sudo -n rm -rf /var/lib/apt/lists/*

# A SIGNED COMMIT SUCCEEDS AS THIS UID. This is the regression that would break
# publishing silently, so the build proves it.
#
# git signs with `gpg.format=ssh`, which means it execs ssh-keygen — and
# ssh-keygen calls getpwuid before it does anything else, exiting "No user
# exists for uid" when /etc/passwd has no entry for the uid it is running as.
# Nothing in the image resolves users but /etc/passwd, and the spawn clears
# the image's entrypoint, so no start-up script can add an entry later. The
# failure is not a warning: EVERY commit the publish step makes is signed, so an
# image that cannot sign is an image whose every task ends after the work is
# done and before it is pushed.
#
# THE KEY IS EPHEMERAL AND LOCAL. It is generated here, used here, verified
# here, and deleted in the same RUN, so no key material reaches a layer. No
# forge is contacted and no agent socket is involved — the real signing key
# lives in the KMS and arrives as a socket per attempt, never as a file — but
# the ssh-keygen path git takes is identical either way, which is what makes
# this a proof of the trap rather than of the key.
RUN set -eux; \
    export HOME=/tmp/signing-smoke; \
    mkdir -p "${HOME}"; \
    ssh-keygen -q -t ed25519 -N '' -C signing-smoke -f "${HOME}/key"; \
    printf '%s %s\n' 'signing-smoke@invalid' "$(cat "${HOME}/key.pub")" > "${HOME}/allowed_signers"; \
    git init -q "${HOME}/repo"; \
    git -C "${HOME}/repo" \
        -c user.name=signing-smoke -c user.email=signing-smoke@invalid \
        -c gpg.format=ssh -c user.signingkey="${HOME}/key" \
        commit -q --gpg-sign --allow-empty -m 'signing smoke'; \
    git -C "${HOME}/repo" cat-file commit HEAD | grep -q '^gpgsig'; \
    git -C "${HOME}/repo" \
        -c gpg.format=ssh -c gpg.ssh.allowedSignersFile="${HOME}/allowed_signers" \
        verify-commit HEAD; \
    rm -rf "${HOME}"

# THE SERENA LANGUAGES THIS IMAGE SERVES, written once: the languages label
# below reads this default, and the factory refuses an image without it. Each
# name is serena's language key, mapped here to the server serena runs for it
# (softwarefactoryworker.LanguageServers is the same vocabulary, and a test
# holds the two together). The build fails when a listed language's server is
# not on the PATH -- the npm servers' own prefix included -- so the label
# cannot claim a server the image does not carry.
ARG WORKER_LANGUAGES="go python typescript bash yaml markdown cpp rust ruby"
RUN set -eu; \
    PATH="${PATH}:/opt/serena/language-servers/bin"; \
    for language in ${WORKER_LANGUAGES}; do \
        case "$language" in \
            bash) server=bash-language-server ;; \
            cpp) server=clangd ;; \
            go) server=gopls ;; \
            markdown) server=marksman ;; \
            python) server=pyright-langserver ;; \
            ruby) server=ruby-lsp ;; \
            rust) server=rust-analyzer ;; \
            typescript) server=typescript-language-server ;; \
            yaml) server=yaml-language-server ;; \
            *) echo "WORKER_LANGUAGES names $language, which has no known language server" >&2; exit 1 ;; \
        esac; \
        command -v "$server" >/dev/null || \
            { echo "WORKER_LANGUAGES names $language, but its server $server is not on the PATH" >&2; exit 1; }; \
    done

CMD ["claude"]

# THE LABELS ARE THE IMAGE'S BILL OF MATERIALS, and four of them are read by
# software: io.quicbaas.worker.languages is how the factory knows which serena
# languages it may start (see WORKER_LANGUAGES above). io.quicbaas.worker.claude-code.version and ...codex.version are how
# the daemon's worker image refresh decides whether a bound image is behind the
# npm registry; ...gemini.version is how it refuses a rebuild that would move a
# bound image's Gemini CLI backwards (Gemini is not in the content tag, so it
# never makes an image stale by itself); every other one records what the image was built from, so an
# operator rolling back can see what they are rolling back to. The keys are
# declared once, in Go (softwarefactoryworker.Label*), and the refresh passes
# the same values as --label, so an image built here and one built by the
# daemon are labelled identically.
ARG GO_VERSION
ARG GOLANGCI_VERSION
ARG GOSEC_VERSION
ARG GOVULNCHECK_VERSION
ARG GOPLS_VERSION
ARG ACTIONLINT_VERSION
ARG OPENCODE_VERSION
ARG PROTOC_VERSION
ARG PROTOC_GEN_GO_VERSION
ARG PROTOC_GEN_GO_GRPC_VERSION
ARG QUICBAAS_VERSION
LABEL org.opencontainers.image.title="QuicBaaS Software Factory Worker" \
      org.opencontainers.image.description="Worker container for the sdlc runner: claude-code, codex, gemini, opencode, serena and its language servers, git, ssh, ssh-keygen, make, the Go toolchain, node, python, protoc and the golangci-lint/gosec/govulncheck/actionlint/shellcheck verification tools" \
      org.opencontainers.image.vendor="QuicBaaS" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.version="${QUICBAAS_VERSION}" \
      io.quicbaas.worker.base.version="${QUICBAAS_VERSION}" \
      io.quicbaas.worker.claude-code.version="${CLAUDE_CODE_VERSION}" \
      io.quicbaas.worker.codex.version="${CODEX_VERSION}" \
      io.quicbaas.worker.gemini.version="${GEMINI_CLI_VERSION}" \
      io.quicbaas.worker.languages="${WORKER_LANGUAGES}" \
      io.quicbaas.worker.go.version="${GO_VERSION}" \
      io.quicbaas.worker.golangci-lint.version="${GOLANGCI_VERSION}" \
      io.quicbaas.worker.gosec.version="${GOSEC_VERSION}" \
      io.quicbaas.worker.govulncheck.version="${GOVULNCHECK_VERSION}" \
      io.quicbaas.worker.gopls.version="${GOPLS_VERSION}" \
      io.quicbaas.worker.serena.version="${SERENA_VERSION}" \
      io.quicbaas.worker.actionlint.version="${ACTIONLINT_VERSION}" \
      io.quicbaas.worker.shellcheck.version="${SHELLCHECK_VERSION}" \
      io.quicbaas.worker.opencode.version="${OPENCODE_VERSION}" \
      io.quicbaas.worker.protoc.version="${PROTOC_VERSION}" \
      io.quicbaas.worker.protoc-gen-go.version="${PROTOC_GEN_GO_VERSION}" \
      io.quicbaas.worker.protoc-gen-go-grpc.version="${PROTOC_GEN_GO_GRPC_VERSION}" \
      io.quicbaas.worker.node.version="${NODE_VERSION}" \
      io.quicbaas.worker.npm.version="${NPM_VERSION}" \
      io.quicbaas.worker.uv.version="${UV_VERSION}" \
      io.quicbaas.worker.pyright.version="${PYRIGHT_VERSION}" \
      io.quicbaas.worker.typescript.version="${TYPESCRIPT_VERSION}" \
      io.quicbaas.worker.typescript-language-server.version="${TYPESCRIPT_LANGUAGE_SERVER_VERSION}" \
      io.quicbaas.worker.bash-language-server.version="${BASH_LANGUAGE_SERVER_VERSION}" \
      io.quicbaas.worker.yaml-language-server.version="${YAML_LANGUAGE_SERVER_VERSION}" \
      io.quicbaas.worker.ruby-lsp.version="${RUBY_LSP_VERSION}" \
      io.quicbaas.worker.clangd.version="${CLANGD_VERSION}" \
      io.quicbaas.worker.rust-analyzer.version="${RUST_ANALYZER_VERSION}" \
      io.quicbaas.worker.marksman.version="${MARKSMAN_VERSION}"
