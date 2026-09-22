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

FROM golang:${GO_VERSION}-alpine AS toolchain

# `go install`, the way the CI runner installs the same three tools, so the
# worker and CI reach their verdicts with binaries built from the same source
# at the same tag by the same toolchain. CGO_ENABLED=0 keeps them static, which
# is what lets them be copied into a runtime stage that shares only musl.
ARG GOLANGCI_VERSION
ARG GOSEC_VERSION
ARG GOVULNCHECK_VERSION

# gopls is not a verdict tool — it is SERENA'S GO LANGUAGE SERVER, and serena is
# how an agent reads this codebase without burning its context on whole files.
# It is built here for the same reason as the three above: static, from source,
# by this toolchain, so it can be copied into a runtime stage sharing only musl.
ARG GOPLS_VERSION

ENV CGO_ENABLED=0 GOBIN=/toolchain/bin

RUN set -eux; \
    if [ -z "${GOLANGCI_VERSION:-}" ] || [ -z "${GOSEC_VERSION:-}" ] || [ -z "${GOVULNCHECK_VERSION:-}" ] || [ -z "${GOPLS_VERSION:-}" ]; then \
        echo "GOLANGCI_VERSION, GOSEC_VERSION, GOVULNCHECK_VERSION and GOPLS_VERSION are required build arguments." >&2; \
        echo "Build with 'make build-worker-image', which passes the Makefile's pins." >&2; \
        exit 1; \
    fi; \
    go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_VERSION}"; \
    # gosec stamps its version only through main.Version; a bare install
    # reports "Version: dev", which makes every scan unreproducible (the
    # fleet learned this on go-luks2#21) and is what the ci-runner image
    # avoids the same way.
    go install -ldflags "-X main.Version=${GOSEC_VERSION}" "github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION}"; \
    go install "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}"; \
    go install "golang.org/x/tools/gopls@${GOPLS_VERSION}"

FROM alpine:3.21

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
    curl \
    ca-certificates \
    tzdata \
    sudo

# pkgconf, gtk+3.0-dev and webkit2gtk-4.1-dev are what `make build` needs to
# link the client's embedded desktop GUI (CGO + Wails/GTK/WebKit): the same
# packages CI's Build job installs before `make build`, and provisioned here
# for the same reason CI gives — rather than narrowing what the Build gate
# covers. Without them build-cli dies on its pkg-config guard, and a worker
# either fails verification (go-quicbaas #53, 2026-09-21) or installs them
# by hand in every round.
#
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
RUN npm install -g @anthropic-ai/claude-code@latest @openai/codex@latest \
    && claude --version \
    && codex --version

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
    if [ -z "${SERENA_VERSION:-}" ]; then \
        echo "SERENA_VERSION is a required build argument." >&2; \
        echo "Build with 'make build-worker-image', which passes the Makefile's pin." >&2; \
        exit 1; \
    fi; \
    apk add --no-cache python3 py3-pip uv; \
    apk add --no-cache --virtual .serena-build python3-dev linux-headers; \
    uv tool install --python python3 \
        --from "git+https://github.com/oraios/serena@${SERENA_VERSION}" serena-agent; \
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

LABEL org.opencontainers.image.title="QuicBaaS Software Factory Worker" \
      org.opencontainers.image.description="Worker container for the sdlc runner: claude-code, git, ssh, ssh-keygen, make, the Go toolchain and the golangci-lint/gosec/govulncheck verification tools" \
      org.opencontainers.image.vendor="QuicBaaS" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later"
