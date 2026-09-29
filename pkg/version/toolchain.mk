# The repository's toolchain pins — THE ONLY COPY.
#
# This file is read by two things, and that is why it exists rather than these
# lines living in the Makefile:
#
#   make   The top-level Makefile `include`s it, so every target, `make
#          install-tools`, and CI (which reads the pins through `make
#          print-<VAR>`, .github/workflows/ci.yml) see exactly these values.
#   Go     pkg/version embeds it (version.Toolchain), so a running daemon knows
#          the pins it was released with — which is what lets the Software
#          Factory's worker image refresh rebuild the worker image with the
#          same toolchain `make build-worker-image` uses, from a binary built
#          by any path (make, the release workflow, a Dockerfile).
#
# Keep it to plain `NAME := value` lines and comments: the Go side reads that
# shape and nothing else, and a test refuses the file if a line is anything
# more (pkg/version, TestToolchain_EveryLineIsAnAssignment).

# The Go toolchain the projects this factory builds pin in their go.mod.
GO_VERSION := 1.27.0

# Pinned per devops.md §9 and company task #122. $(GOBIN) is shared across
# checkouts on the build hosts, so an unpinned install in one tree silently
# changes every other tree's lint verdict.
#
# `make install-tools` installs them, CI installs them by reading these
# variables, and the Software Factory worker image is built with them. A
# worker that linted with a different version than CI would produce review
# verdicts a merge then contradicts (go-quicbaas#772).
GOLANGCI_VERSION := v2.13.2
# Pinned by the fleet ruling of 2026-08-26: an unpinned gosec self-updated
# mid-day and turned unrelated branches red, so nobody could say which commit
# owned the new findings.
GOSEC_VERSION := v2.29.0
# Pins the TOOL, not the vulnerability DATABASE -- govulncheck fetches that at
# run time, which is the point of running it.
GOVULNCHECK_VERSION := v1.8.0
# Serena is the semantic code-retrieval MCP server the Software Factory's agents
# search this codebase with, and gopls is the Go language server it drives. Both
# are pinned for the same reason as the tools above: an agent whose search
# behaviour changes under an image rebuild is a change nobody authored.
SERENA_VERSION := v1.7.0
GOPLS_VERSION := v0.23.0
# Dependency floors for the Go tools the worker image SHIPS (gopls, gosec).
# devops.md section 8 gates every image on `trivy image`, and trivy reads the
# module list compiled into each binary: gopls v0.23.0 was released against
# golang.org/x/mod v0.37.0 (CVE-2026-56864) and golang.org/x/text v0.38.0
# (CVE-2026-56852), gosec v2.29.0 against google.golang.org/grpc v1.83.1
# (CVE-2026-84445), and both are their project's newest release. So the tools
# are built from a throwaway module that requires the tool at its pin and
# raises each of these modules the tool ALREADY depends on to the version
# below -- the newest release of each, checksum-verified through the module's
# go.sum -- rather than being `go install`ed with what upstream shipped.
# `make install-gosec` and CI's gosec build the same way, so the gosec that
# gates CI is the gosec in the image. Space-separated module@version pairs.
# Drop an entry once the tools' own releases require that version or newer.
GO_TOOL_DEPENDENCY_FLOORS := golang.org/x/mod@v0.41.0 golang.org/x/text@v0.42.0 google.golang.org/grpc@v1.84.0
# Workflow linting. The fleet's `make lint` includes actionlint (devops.md
# lint row), and actionlint delegates every SC* rule to shellcheck -- with
# shellcheck absent it SILENTLY skips them and still exits 0, so a project's
# target fails closed without it. The worker image carries both at these pins
# (go-quicbaas#1039). shellcheck publishes no checksum file, so the SHA-256 of
# each release tarball is pinned beside the version: change all three together.
ACTIONLINT_VERSION := v1.7.12
SHELLCHECK_VERSION := v0.11.0
SHELLCHECK_SHA256_X86_64 := 8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198
SHELLCHECK_SHA256_AARCH64 := 12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588
# opencode, the third agent CLI the worker image carries: the ACP agent a fleet
# node drives against a LOCAL model server (the review model on ollama), where
# claude and codex drive hosted ones. It is installed from its release tarball
# -- a single static Bun binary, the musl build for this Alpine image -- and the
# SHA-256 of each architecture's tarball is pinned beside the version, taken
# from the digests the release itself publishes
# (github.com/anomalyco/opencode/releases/tag/v1.18.32): change all three
# together.
OPENCODE_VERSION := v1.18.32
OPENCODE_SHA256_X86_64 := 1ce4699c8a9470ad806fa824112cd30b5d51b4038c29231189486302ef394d94
OPENCODE_SHA256_AARCH64 := abf302edbc996548e28417dcdf7f67109fd95c7344f813a312633a818986204d
