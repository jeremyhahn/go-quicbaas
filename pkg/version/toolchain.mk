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
GO_VERSION := 1.27.4

# Pinned per devops.md §9 and company task #122. $(GOBIN) is shared across
# checkouts on the build hosts, so an unpinned install in one tree silently
# changes every other tree's lint verdict.
#
# `make install-tools` installs them, CI installs them by reading these
# variables, and the Software Factory worker image is built with them. A
# worker that linted with a different version than CI would produce review
# verdicts a merge then contradicts (go-quicbaas#772).
GOLANGCI_VERSION := v2.14.0
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
# gosec v2.29.0 (and v2.30.0) require golang.org/x/net below v0.62.0
# (CVE-2026-78661).
GO_TOOL_DEPENDENCY_FLOORS := golang.org/x/mod@v0.41.0 golang.org/x/net@v0.62.0 golang.org/x/text@v0.42.0 golang.org/x/tools@v0.51.0 google.golang.org/grpc@v1.84.0
# trivy, the scanner `make security` ends with (devops.md section 8). From its
# release tarball, each architecture's SHA-256 as the release's own
# trivy_<version>_checksums.txt publishes it: change all three together. Pins
# the TOOL; trivy fetches its vulnerability database at run time.
TRIVY_VERSION := v0.76.0
TRIVY_SHA256_X86_64 := e24b1c51293c1b15e4ba8d45bbc7891ba053f90278ec2698f3562c993e145fe7
TRIVY_SHA256_AARCH64 := aab21e677a7b7071fc79f1711480840754c18a1b9d9cefa7ccd06af63a0e8f4c
# The devcontainer's base image (build/docker/devcontainer/Dockerfile),
# golang:$(GO_VERSION)-trixie by its multi-arch index digest, so a re-pushed
# tag cannot change the toolchain under a pin. Change it with GO_VERSION.
GO_IMAGE_DIGEST := sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9
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
# -- a single Bun binary, the glibc build for this Debian image -- and the
# SHA-256 of each architecture's tarball is pinned beside the version, taken
# from the digests the release itself publishes
# (github.com/anomalyco/opencode/releases/tag/v1.18.35): change all three
# together.
OPENCODE_VERSION := v1.18.35
OPENCODE_SHA256_X86_64 := c8f888b451f5494a18f858fffb0e0b68f4e4baa9c241761c5f206884f0fa640d
OPENCODE_SHA256_AARCH64 := f7f2ba59ee8aa94d388f9696575a32d20e71c2ee48def9f80fc693a60fec6c72
# gh, the GitHub CLI the worker image carries for the GitHub mirror work. It is
# installed from its release tarball, and the SHA-256 of each architecture's
# tarball is pinned beside the version, taken from the release's own
# gh_<version>_checksums.txt
# (github.com/cli/cli/releases/tag/v2.105.0): change all three together.
GH_VERSION := v2.105.0
GH_SHA256_X86_64 := 79e7ab5db738c48777b696e374f8b428f0647debfb424f44d2f426370407fe2f
GH_SHA256_AARCH64 := b1278c75107423d818f1937fd92cafc9e5e9b606631568f68d7fb816f5885a02
# protoc, so a Go project with .proto files regenerates code inside the worker
# image instead of host-mounting protoc (the container-only tooling rule).
# `protoc --version` prints `libprotoc <PROTOC_VERSION>`, and generated .pb.go
# files name it in their `// protoc v<version>` header -- so moving this pin
# means a project with committed .pb.go files (go-qrdb) regenerates them, or
# its next regeneration in this image is a diff nobody asked for. Upstream
# publishes no checksum file for the release zip, so the SHA-256 of each
# architecture's asset is pinned beside the version, like shellcheck's: change
# all three together.
PROTOC_VERSION := 36.2
PROTOC_SHA256_X86_64 := 121f6c7afe1d4d0e3ea6aab9432038599250134cbf4474cb1167d2c7decd4278
PROTOC_SHA256_AARCH64 := 8b8f18bd2b30346efbc698dd5a73dd7c805f3ef8380f6dfc95c768f3f1852f6a
# protoc-gen-go and protoc-gen-go-grpc are the Go code-generator plugins protoc
# invokes; each is `go install`ed in the toolchain stage like golangci-lint,
# from its own module at its own pin.
PROTOC_GEN_GO_VERSION := v1.36.12
PROTOC_GEN_GO_GRPC_VERSION := v1.6.2
# Dependency floor for Serena's own Python tool environment, the same idea as
# GO_TOOL_DEPENDENCY_FLOORS above but applied through `uv tool install
# --overrides` rather than a Go build: urllib3 < 2.8.0 is a fixable finding
# `make push-worker-image`'s trivy gate reads out of Serena's installed
# packages (go-quicbaas#1161). `--overrides`, not `--with`: serena-agent
# 1.7.0 depends on urllib3==2.7.0 exactly, and `--with` only adds a second,
# conflicting constraint uv then refuses to resolve -- `--overrides` replaces
# the declared pin instead. serena-agent 1.7.0 also pins filelock==3.25.2
# (CVE-2026-112940) and mcp==1.28.1 (CVE-2026-115563); mcp is held below 2,
# the major serena 1.7.0 was built against. Space-separated requirement
# specifiers, no spaces inside an entry. Drop an entry once serena's own
# release requires that version or newer.
SERENA_DEPENDENCY_FLOORS := urllib3>=2.8.0 filelock>=4.2.1 mcp>=1.30.0,<2

# Node.js, which runs the claude and codex CLIs and the npm-built language
# servers below, and a project's own frontend build. Installed from the
# nodejs.org release tarball (Debian's nodejs is a major behind), with the
# SHA-256 of each architecture's tarball -- as published in the release's
# SHASUMS256.txt -- pinned beside the version: change all three together.
# NPM_VERSION is the npm the image upgrades the bundled one to.
NODE_VERSION := v26.10.0
NODE_SHA256_X86_64 := ca70e9e349de048b9522abb3adc05b3bd6f43c5ffd3ec57916c7da292f59f022
NODE_SHA256_AARCH64 := 7a6353f63eb3d04765004b4adf172616243e4522434635cb1d26288658b04ab5
NPM_VERSION := 12.2.3
# Dependency floors for the packages npm BUNDLES in its own node_modules, the
# same idea as SERENA_DEPENDENCY_FLOORS: when an npm release bundles a copy the
# image's trivy gate reads as a fixable finding, the image replaces exactly
# that package after its npm upgrade. npm 12.2.3 bundles brace-expansion
# 5.0.12 and undici 6.29.1, so no floor is needed and there is no line here:
# NPM_BUNDLED_DEPENDENCY_FLOORS is optional, and empty when absent. When a
# future npm bundles a vulnerable copy, add the floor back as a
# `NPM_BUNDLED_DEPENDENCY_FLOORS := name@version=integrity` line (space-
# separated entries), the integrity being the tarball's sha512 as the npm
# registry publishes it (dist.integrity), so the build verifies what it
# fetched: change version and integrity together. A floor is a patch release
# of what npm bundles and nothing more; the build refuses one npm already
# meets, so an entry is dropped as soon as an npm release carries the fix.
# uv, which installs Serena (and is on the PATH for a Python project). From its
# release tarball -- Debian packages no uv -- with each architecture's SHA-256
# pinned beside the version, as the release's own .sha256 files publish it.
UV_VERSION := 0.12.23
UV_SHA256_X86_64 := 9167d72b3319674b6303c4cbe071854bba13ebdf3d76b1a7cbdc175471fb66d6
UV_SHA256_AARCH64 := 6524bd338177ed50d035d39354e12545e993bbeba2ecbddf0480c5b3a81d313f

# The language servers Serena drives, baked into the image so a session with an
# empty HOME and no network still reads code by symbol. Left to itself Serena
# downloads each one into ~/.serena on first use; the image's global Serena
# settings point it at these instead (docs/agent-fleet/worker-image.md).
# gopls is pinned above.
#
# The npm-published servers. TYPESCRIPT_VERSION stays on the 6.x line on
# purpose: typescript-language-server drives tsserver.js, which TypeScript 7
# (the native rewrite) no longer ships -- the currency check tracks 6.x for it.
PYRIGHT_VERSION := 1.1.414
TYPESCRIPT_VERSION := 6.0.3
TYPESCRIPT_LANGUAGE_SERVER_VERSION := 6.0.1
BASH_LANGUAGE_SERVER_VERSION := 5.8.1
YAML_LANGUAGE_SERVER_VERSION := 1.24.0
# Ruby's server, a gem installed against Debian's ruby.
RUBY_LSP_VERSION := 0.26.11
# The release binaries, each with its architecture's SHA-256 as the release's
# own asset digest publishes it: change each group together. clangd publishes
# no aarch64 Linux build, so an aarch64 image uses Debian's clangd instead and
# only the x86_64 digest is pinned.
CLANGD_VERSION := 23.1.0
CLANGD_SHA256_X86_64 := e53b1a96196095faedb7642cf64964f7fb9ad4a0c1f00dd2c172a3d9dcbafdfd
RUST_ANALYZER_VERSION := 2026-10-05
RUST_ANALYZER_SHA256_X86_64 := 28070188df63b6f217768040781decc8db43bc9d29b126847acb365575b09bc9
RUST_ANALYZER_SHA256_AARCH64 := 3974c863acbbd96ef2cc02a7aaa79123935b05f27ffe4fd393fc229522f75cb9
MARKSMAN_VERSION := 2026-02-08
MARKSMAN_SHA256_X86_64 := be5098e8213219269c47fc0d916a66fa31ce0602ec967475c722260aabf26087
MARKSMAN_SHA256_AARCH64 := db8e124527f7f8048e3e6c91821b9c52ef173d92c01e47d221bf1337afd962fb
# Kotlin's server, JetBrains' kotlin-server release tarball, with each
# architecture's SHA-256 as the release's own .sha256 files publish it. It runs
# on its bundled JetBrains runtime; the JDK below is for the projects a worker
# builds, Debian's headless package at this major (go-quicbaas#1746). A major
# rather than a *_VERSION: Debian's archive moves its patch release, and the
# currency check tracks no upstream for it.
KOTLIN_LSP_VERSION := 271.2085.0
KOTLIN_LSP_SHA256_X86_64 := e398e799027f41749ea33faa57470307d13d40f910961858c7e30adbd778ae08
KOTLIN_LSP_SHA256_AARCH64 := 877aee612a17dab48144466282a6414ec58ab428a10b97c7e13edd9b6b3dc96e
OPENJDK_MAJOR := 21
