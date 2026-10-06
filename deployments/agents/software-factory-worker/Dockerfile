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
#   build-base     `go test -race` and any cgo dependency need a C toolchain;
#                  a project whose tests do not build cannot be verified.
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
# the image and the repository never disagree about it.
ARG GO_VERSION=1.27.0

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

FROM golang:${GO_VERSION}-alpine AS toolchain

# `go install`, the way the CI runner installs the same three tools, so the
# worker and CI reach their verdicts with binaries built from the same source
# at the same tag by the same toolchain. CGO_ENABLED=0 keeps them static, which
# is what lets them be copied into a runtime stage that shares only musl.
ARG GOLANGCI_VERSION
ARG GOSEC_VERSION
ARG GOVULNCHECK_VERSION
ARG ACTIONLINT_VERSION
ARG PROTOC_GEN_GO_VERSION
ARG PROTOC_GEN_GO_GRPC_VERSION

# gopls is not a verdict tool — it is SERENA'S GO LANGUAGE SERVER, and serena is
# how an agent reads this codebase without burning its context on whole files.
# It is built here for the same reason as the three above: static, from source,
# by this toolchain, so it can be copied into a runtime stage sharing only musl.
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

FROM alpine:3.21

# Fixed packages already in Alpine's apk repository reach this image without
# waiting for a new alpine:3.21 base tag to pick them up -- e.g. the openssl
# 3.3.7-r2 fix for the libssl3/libcrypto3 finding `make push-worker-image`'s
# trivy gate reported (go-quicbaas#1161). Must run before anything else is
# added, so every package below installs at the upgraded version too.
RUN apk upgrade --no-cache

RUN apk add --no-cache \
    nodejs \
    npm \
    git \
    openssh-keygen \
    openssh-client \
    make \
    build-base \
    bash \
    grep \
    rsync \
    pkgconf \
    gtk+3.0-dev \
    webkit2gtk-4.1-dev \
    cryptsetup \
    curl \
    ca-certificates \
    tzdata \
    sudo

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

# pkgconf, gtk+3.0-dev and webkit2gtk-4.1-dev are what `make build` needs to
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
# correct the change was (go-quicbaas #1003, go-luks2 #12 and #114).
#
# SHELLCHECK, actionlint's SC* backend (see the header). It is a Haskell
# program, so unlike the Go tools it cannot be `go install`ed; the upstream
# release binary is statically linked and runs on musl unchanged. The version
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
# the container-only tooling rule). Pinned at 21.12 -- `libprotoc 3.21.12`,
# the version named in the `// protoc v3.21.12` header of this fleet's
# generated .pb.go files. Alpine's own protobuf package is a different major
# and is not used.
#
# FROM THE RELEASE ZIP, GLIBC-LINKED. The upstream binary needs libpthread,
# libm, libstdc++ and libgcc_s; gcompat and libstdc++ are what let it run on
# this musl base (verified: alpine:3.21 + gcompat + libstdc++ runs `protoc
# --version` and compiles a proto importing google/protobuf/timestamp.proto).
# Upstream publishes no checksum file for the release asset, so the SHA-256 of
# each architecture's zip is pinned beside the version, like shellcheck's --
# passed in by `make build-worker-image` with no default here, for the same
# reason as every other pin: a default is a second copy that drifts.
#
# include/google/protobuf/*.proto (the well-known types) lands at
# /usr/local/include, which protoc searches by default relative to its own
# bin directory (<bin>/../include) -- no -I needed for them. unzip is a
# build-only dependency, added and removed in this one layer.
ARG PROTOC_VERSION
ARG PROTOC_SHA256_X86_64
ARG PROTOC_SHA256_AARCH64
RUN set -eux; \
    if [ -z "${PROTOC_VERSION:-}" ] || [ -z "${PROTOC_SHA256_X86_64:-}" ] || [ -z "${PROTOC_SHA256_AARCH64:-}" ]; then \
        echo "PROTOC_VERSION, PROTOC_SHA256_X86_64 and PROTOC_SHA256_AARCH64 are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the Makefile's pins." >&2; \
        exit 1; \
    fi; \
    apk add --no-cache gcompat libstdc++; \
    apk add --no-cache --virtual .protoc-build unzip; \
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
    apk del .protoc-build; \
    protoc --version

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
ARG CLAUDE_CODE_VERSION
ARG CODEX_VERSION
# DEVCACHE_NPM_REGISTRY: the fleet's npm cache (devops.md section 2a). NO
# DEFAULT and NEVER an ENV, for the same reason as the Go caches above.
# devcache-env (installed earlier in this stage) sets npm_config_registry
# only when it is set and the cache answers; unset or unreachable, npm keeps
# its own default registry.
ARG DEVCACHE_NPM_REGISTRY
RUN set -eux; \
    if [ -z "${CLAUDE_CODE_VERSION:-}" ] || [ -z "${CODEX_VERSION:-}" ]; then \
        echo "CLAUDE_CODE_VERSION and CODEX_VERSION are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the Makefile's pins." >&2; \
        exit 1; \
    fi; \
    eval "$(devcache-env)"; \
    npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" "@openai/codex@${CODEX_VERSION}"; \
    claude --version; \
    codex --version

# OPENCODE, the third agent CLI: the one a fleet node drives against a LOCAL
# model server, where claude and codex drive hosted ones. A node that serves
# the local review model advertises the "opencode" backend, and a reviewer
# placed there runs this binary in this image -- so an image without it is a
# node that advertises a backend it cannot run.
#
# FROM THE RELEASE TARBALL, NOT FROM npm. opencode ships as one static Bun
# binary per platform, and the musl build is the one this Alpine image runs.
# The version and each architecture's SHA-256 are the repository's pins
# (pkg/version/toolchain.mk: OPENCODE_VERSION, OPENCODE_SHA256_*), taken from
# the digests the release itself publishes and passed in by `make
# build-worker-image` and the daemon's refresh with no default here, for the
# same reason as every other pin: a default is a second copy that drifts. The
# digest is CHECKED, so a tarball that is not the release's fails the build.
#
# libstdc++ and libgcc are the Bun runtime's own shared dependencies on musl.
ARG OPENCODE_VERSION
ARG OPENCODE_SHA256_X86_64
ARG OPENCODE_SHA256_AARCH64
RUN set -eux; \
    if [ -z "${OPENCODE_VERSION:-}" ] || [ -z "${OPENCODE_SHA256_X86_64:-}" ] || [ -z "${OPENCODE_SHA256_AARCH64:-}" ]; then \
        echo "OPENCODE_VERSION, OPENCODE_SHA256_X86_64 and OPENCODE_SHA256_AARCH64 are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the repository's pins." >&2; \
        exit 1; \
    fi; \
    apk add --no-cache libstdc++ libgcc; \
    arch="$(uname -m)"; \
    case "$arch" in \
        x86_64) platform="linux-x64-musl"; sum="$OPENCODE_SHA256_X86_64" ;; \
        aarch64) platform="linux-arm64-musl"; sum="$OPENCODE_SHA256_AARCH64" ;; \
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

# SERENA, the semantic code-retrieval MCP server, so an agent reads this
# codebase by symbol instead of by whole file.
#
# INSTALLED AT BUILD TIME, NOT RUN THROUGH `uvx --from git+...`. That is how a
# developer runs serena on a workstation, and it is wrong here twice over: a
# sandboxed spawn is not promised egress to GitHub, and a clone-per-spawn makes
# every session pay for it and fail whenever the network does. `uv tool install`
# resolves it once, into the image, at a pinned tag.
#
# PINNED, because an image that installs @latest cannot be rebuilt to the same
# bytes, and an agent's search behaviour changing under a rebuild is a change
# nobody authored.
#
# The language servers serena drives are NOT vendored here. Go's is gopls,
# copied from the toolchain stage below; a project in another language gets
# serena's own on-demand download, which is why python3 and the cache directory
# are present rather than only the binary.
#
# THE DEV HEADERS ARE BUILD-ONLY AND REMOVED IN THE SAME LAYER. serena depends
# on psutil, which publishes no musl wheel, so uv compiles it from source and
# needs Python.h and the kernel headers:
#
#   hint: This error likely indicates that you need to install a library
#         that provides "Python.h" for `psutil@7.0.0`
#
# A virtual package installed and deleted inside one RUN keeps the compiler
# inputs out of the shipped layers; the compiled extension stays.
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
    apk add --no-cache python3 py3-pip uv; \
    apk add --no-cache --virtual .serena-build python3-dev linux-headers; \
    printf '%s\n' ${SERENA_DEPENDENCY_FLOORS} > /tmp/serena-overrides.txt; \
    uv tool install --python python3 \
        --from "git+https://github.com/oraios/serena@${SERENA_VERSION}" serena-agent \
        --overrides /tmp/serena-overrides.txt; \
    rm -f /tmp/serena-overrides.txt; \
    apk del .serena-build; \
    chmod -R a+rX /opt/uv; \
    serena --version

# SERENA CANNOT DETECT MUSL, AND ON THIS IMAGE THAT BREAKS EVERY LANGUAGE.
#
# solidlsp builds its platform id as "<os>-<libc>-<arch>" whenever the libc is
# not glibc, reading the libc from Python's platform.libc_ver(). That function
# only ever detects glibc -- it scans the executable for a glibc version string
# -- so on musl it returns "" and the id comes out "linux--x64", which is not a
# member of the PlatformId enum and raises ValueError. "linux-musl-x64" IS a
# member; nothing is missing but the detection.
#
# IT BREAKS GO EVEN THOUGH NOTHING ABOUT GO ASKS FOR A PLATFORM. Resolving any
# language server calls LanguageServerId.from_ls_class, which walks every enum
# member and IMPORTS every language-server module to compare classes, and
# typescript_language_server.py evaluates get_platform_id() at import time. So
# starting the GO server imports the TypeScript module and dies there:
#   go: 'linux--x64' is not a valid PlatformId
# The reviewer then loses symbol and reference lookup and says it cannot
# complete the review -- a verdict that decides nothing.
#
# The patch fills in the libc only when Python found none, so a glibc base
# image is unaffected, and the build asserts the result rather than trusting
# the substitution: if a serena upgrade rewrites this line the replacement
# silently matches nothing, and the assertion is what turns that into a failed
# build instead of a reviewer that cannot read code.
ARG SOLIDLSP_UTILS=/opt/uv/tools/serena-agent/lib/python3.12/site-packages/solidlsp/ls_utils.py
RUN set -eux; \
    sed -i 's|libc = platform\.libc_ver()\[0\]$|libc = platform.libc_ver()[0] or ("musl" if __import__("glob").glob("/lib/ld-musl-*.so.1") else "")|' "$SOLIDLSP_UTILS"; \
    grep -q 'ld-musl' "$SOLIDLSP_UTILS" \
      || { echo "solidlsp libc detection line not found; serena changed it -- re-check the musl patch" >&2; exit 1; }; \
    find /opt/uv -name '__pycache__' -type d -exec rm -rf {} +; \
    chmod -R a+rX /opt/uv; \
    python3 -c "import sys; sys.path.insert(0, '/opt/uv/tools/serena-agent/lib/python3.12/site-packages'); from solidlsp.ls_utils import PlatformUtils; got = PlatformUtils.get_platform_id().value; print('serena platform id:', got); sys.exit(0 if got == 'linux-musl-x64' else 'expected linux-musl-x64, got ' + got)"

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
# that builds a frontend (go-quicbaas #966, 2026-09-21). GNU grep is installed
# above for the same reason: the Makefile's checks use --include/--exclude-dir
# and -E, which busybox grep rejects, and a rejected grep in a `|| true`
# pipeline passes a security check that never ran.
ENV PATH="/usr/local/go/bin:${GOPATH}/bin:${PATH}" \
    GOTOOLCHAIN=local

# ENV PATH IS NOT ENOUGH, BECAUSE THE AGENT EXECS THROUGH A LOGIN SHELL.
#
# Every tool the factory runs in this container arrives as `sh -lc <command>`.
# The -l makes it a LOGIN shell, which sources /etc/profile, whose first line
# on this base image is an unconditional
#     export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
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
# sourced by /etc/profile AFTER that assignment.
RUN printf '%s\n' \
      '# Restores the toolchain PATH that /etc/profile unconditionally clears.' \
      '# The agent runs every command as `sh -lc`, so this file is what makes' \
      '# go, gofmt and the GOPATH/bin tools resolvable to it.' \
      'export PATH="/usr/local/go/bin:/home/agent/go/bin:$PATH"' \
      > /etc/profile.d/10-go-toolchain.sh \
    && chmod 0644 /etc/profile.d/10-go-toolchain.sh \
    && sh -lc 'command -v go >/dev/null' \
    && sh -lc 'command -v gofmt >/dev/null'

# Into GOPATH/bin, which is both what `go install` would have written and what
# the projects' Makefiles probe. The runtime stage shares musl with the builder
# and the binaries are static, so they run here unchanged.
COPY --from=toolchain /toolchain/bin/ ${GOPATH}/bin/

# THE USER IS THE ONE THE SPAWN WILL RUN AS, AND IT MUST EXIST IN /etc/passwd.
#
# The ACP spawn runs a sandboxed session as the DAEMON's own uid:gid
# (acp_backend.go: `User: fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())`), so
# the container writes through the bind mount as its owner. An image whose
# /etc/passwd has no entry for that uid is not merely untidy: ssh-keygen —
# which git invokes for every `gpg.format=ssh` signature — calls getpwuid at
# start and exits "No user exists for uid" without one, so EVERY signed commit
# would fail. musl resolves users from /etc/passwd and nothing else, and no
# entrypoint runs to add one (the spawn clears it), so the entry has to be in
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
    if ! getent group "${AGENT_GID}" >/dev/null; then addgroup -g "${AGENT_GID}" agent; fi; \
    if ! getent passwd "${AGENT_UID}" >/dev/null; then \
        adduser -D -u "${AGENT_UID}" -G "$(getent group "${AGENT_GID}" | cut -d: -f1)" -h /home/agent agent; \
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
# one a worker needs. jq is added and removed again in the same layer, so the
# proof leaves the image exactly as it found it.
RUN set -eux; \
    sudo -n true; \
    test "$(sudo -n id -u)" = "0"; \
    sudo -n apk add --no-cache jq; \
    jq --version; \
    sudo -n apk del jq

# A SIGNED COMMIT SUCCEEDS AS THIS UID. This is the regression that would break
# publishing silently, so the build proves it.
#
# git signs with `gpg.format=ssh`, which means it execs ssh-keygen — and
# ssh-keygen calls getpwuid before it does anything else, exiting "No user
# exists for uid" when /etc/passwd has no entry for the uid it is running as.
# musl resolves users from /etc/passwd and nothing else, and the spawn clears
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

CMD ["claude"]

# THE LABELS ARE THE IMAGE'S BILL OF MATERIALS, and two of them are read by
# software. io.quicbaas.worker.claude-code.version and ...codex.version are how
# the daemon's worker image refresh decides whether a bound image is behind the
# npm registry; every other one records what the image was built from, so an
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
      org.opencontainers.image.description="Worker container for the sdlc runner: claude-code, codex, opencode, git, ssh, ssh-keygen, make, the Go toolchain, protoc and the golangci-lint/gosec/govulncheck/actionlint/shellcheck verification tools" \
      org.opencontainers.image.vendor="QuicBaaS" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later" \
      org.opencontainers.image.version="${QUICBAAS_VERSION}" \
      io.quicbaas.worker.base.version="${QUICBAAS_VERSION}" \
      io.quicbaas.worker.claude-code.version="${CLAUDE_CODE_VERSION}" \
      io.quicbaas.worker.codex.version="${CODEX_VERSION}" \
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
      io.quicbaas.worker.protoc-gen-go-grpc.version="${PROTOC_GEN_GO_GRPC_VERSION}"
