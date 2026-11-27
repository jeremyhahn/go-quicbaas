# The repository's toolchain pins, the only copy; the Makefile, the devcontainer and the worker image read them.

# The Go toolchain the projects this factory builds pin in their go.mod.
GO_VERSION := 1.27.4

# golangci-lint pin.
GOLANGCI_VERSION := v2.14.0
# gosec pin; an unpinned gosec changes findings without a commit.
GOSEC_VERSION := v2.29.0
# Pins the tool; govulncheck fetches its database at run time.
GOVULNCHECK_VERSION := v1.8.0
# Serena, the semantic code-retrieval MCP server, and gopls, the Go language server it drives.
SERENA_VERSION := v1.7.0
GOPLS_VERSION := v0.23.0
# Dependency floors for the Go tools the worker image ships, so `trivy image` finds no fixable CVE in them.
GO_TOOL_DEPENDENCY_FLOORS := golang.org/x/mod@v0.41.0 golang.org/x/net@v0.62.0 golang.org/x/text@v0.43.0 golang.org/x/tools@v0.51.0 google.golang.org/grpc@v1.84.0
# trivy, which `make security` ends with.
TRIVY_VERSION := v0.76.2
TRIVY_SHA256_X86_64 := bc0fc726e017b8927a62b2d3531d7a84ee01769daf5c99ed866dced54089fdbf
TRIVY_SHA256_AARCH64 := ab9d51f86e6a5fb936f605faff6b593ac51bc378fff4d3e8b9d58358b9c367c3
# The devcontainer base image by multi-arch index digest; change it with GO_VERSION.
GO_IMAGE_DIGEST := sha256:b54b2639de8c1bfe538ec5ffc45f29b1932235ac5f6d63c059853646677112d9
# Workflow linting: actionlint skips every SC rule without shellcheck; the shellcheck SHA-256s change with its version.
ACTIONLINT_VERSION := v1.7.12
SHELLCHECK_VERSION := v0.11.0
SHELLCHECK_SHA256_X86_64 := 8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198
SHELLCHECK_SHA256_AARCH64 := 12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588
# opencode, the ACP agent CLI the worker image carries; the SHA-256s of its release tarballs change with its version.
OPENCODE_VERSION := v1.18.35
OPENCODE_SHA256_X86_64 := c8f888b451f5494a18f858fffb0e0b68f4e4baa9c241761c5f206884f0fa640d
OPENCODE_SHA256_AARCH64 := f7f2ba59ee8aa94d388f9696575a32d20e71c2ee48def9f80fc693a60fec6c72
# gh, the GitHub CLI the worker image carries.
GH_VERSION := v2.105.0
GH_SHA256_X86_64 := 79e7ab5db738c48777b696e374f8b428f0647debfb424f44d2f426370407fe2f
GH_SHA256_AARCH64 := b1278c75107423d818f1937fd92cafc9e5e9b606631568f68d7fb816f5885a02
# protoc, so a project with .proto files regenerates code inside the worker image.
PROTOC_VERSION := 36.2
PROTOC_SHA256_X86_64 := 121f6c7afe1d4d0e3ea6aab9432038599250134cbf4474cb1167d2c7decd4278
PROTOC_SHA256_AARCH64 := 8b8f18bd2b30346efbc698dd5a73dd7c805f3ef8380f6dfc95c768f3f1852f6a
# protoc-gen-go and protoc-gen-go-grpc, each `go install`ed at its own pin.
PROTOC_GEN_GO_VERSION := v1.36.12
PROTOC_GEN_GO_GRPC_VERSION := v1.6.2
# Dependency floors for Serena's Python tool environment, applied through `uv tool install --overrides`.
SERENA_DEPENDENCY_FLOORS := urllib3>=2.8.0 filelock>=4.2.1 mcp>=1.30.0,<2

# Node.js, for the claude and codex CLIs, the npm-built language servers and frontend builds.
NODE_VERSION := v26.10.0
NODE_SHA256_X86_64 := ca70e9e349de048b9522abb3adc05b3bd6f43c5ffd3ec57916c7da292f59f022
NODE_SHA256_AARCH64 := 7a6353f63eb3d04765004b4adf172616243e4522434635cb1d26288658b04ab5
NPM_VERSION := 12.2.3
# Optional floors for packages npm bundles; empty when absent.
UV_VERSION := 0.12.23
UV_SHA256_X86_64 := 9167d72b3319674b6303c4cbe071854bba13ebdf3d76b1a7cbdc175471fb66d6
UV_SHA256_AARCH64 := 6524bd338177ed50d035d39354e12545e993bbeba2ecbddf0480c5b3a81d313f

# The language servers Serena drives, baked into the image.
PYRIGHT_VERSION := 1.1.414
TYPESCRIPT_VERSION := 6.0.3
TYPESCRIPT_LANGUAGE_SERVER_VERSION := 6.0.1
BASH_LANGUAGE_SERVER_VERSION := 5.8.1
YAML_LANGUAGE_SERVER_VERSION := 1.24.0
# Ruby's server, a gem installed against Debian's ruby.
RUBY_LSP_VERSION := 0.26.11
# Release binaries with each architecture's SHA-256; clangd has no aarch64 Linux build, so aarch64 uses Debian's clangd.
CLANGD_VERSION := 23.1.0
CLANGD_SHA256_X86_64 := e53b1a96196095faedb7642cf64964f7fb9ad4a0c1f00dd2c172a3d9dcbafdfd
RUST_ANALYZER_VERSION := 2026-10-05
RUST_ANALYZER_SHA256_X86_64 := 28070188df63b6f217768040781decc8db43bc9d29b126847acb365575b09bc9
RUST_ANALYZER_SHA256_AARCH64 := 3974c863acbbd96ef2cc02a7aaa79123935b05f27ffe4fd393fc229522f75cb9
MARKSMAN_VERSION := 2026-02-08
MARKSMAN_SHA256_X86_64 := be5098e8213219269c47fc0d916a66fa31ce0602ec967475c722260aabf26087
MARKSMAN_SHA256_AARCH64 := db8e124527f7f8048e3e6c91821b9c52ef173d92c01e47d221bf1337afd962fb
# The JDK, Debian's headless package at this major.
OPENJDK_MAJOR := 21
