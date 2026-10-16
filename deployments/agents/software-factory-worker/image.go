// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Package softwarefactoryworker is the Software Factory worker image as the
// binary knows it: the Dockerfile beside this file, the toolchain pins it is
// built with, the labels it carries, the tag it is published under and the
// probes it must pass.
//
// It exists so the daemon can rebuild the image by itself (pkg/sdlc/workerimage)
// from exactly what `make build-worker-image` builds, without a checkout of
// this repository on the node: the Dockerfile is embedded, and the toolchain
// pins are the repository's own (pkg/version/toolchain.mk, which the Makefile
// includes and the binary embeds). There is no second copy of a pin anywhere.
//
// The package holds DATA and pure functions only. Driving an engine is
// pkg/orchestration's; deciding when to rebuild is pkg/sdlc/workerimage's.
package softwarefactoryworker

import (
	_ "embed"
	"regexp"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

// Dockerfile is the worker image's build definition, embedded from the file
// beside this one. It builds from an EMPTY context: it COPYs nothing from its
// build directory, which is what lets a daemon build it from these bytes alone.
//
//go:embed Dockerfile
var Dockerfile []byte

// Pins are the toolchain versions the image is built with — every build
// argument except the two agent CLIs, whose versions are what a refresh moves.
type Pins struct {
	Go          string
	Golangci    string
	Gosec       string
	Govulncheck string
	Gopls       string
	Serena      string

	// ToolDependencyFloors are the space-separated module@version pairs gopls
	// and gosec are built with in place of the vulnerable versions their own
	// releases require (toolchain.mk's GO_TOOL_DEPENDENCY_FLOORS). A build
	// INPUT rather than a tool version, so it carries no label.
	ToolDependencyFloors string

	// SerenaDependencyFloors raises a dependency in Serena's own `uv tool
	// install` environment past a fixed CVE (toolchain.mk's
	// SERENA_DEPENDENCY_FLOORS, go-quicbaas#1161), the same idea as
	// ToolDependencyFloors but applied through uv's `--overrides` rather than
	// a Go build: serena-agent pins its dependencies exactly, so `--with`
	// (an addition) cannot raise one -- only `--overrides` (a replacement)
	// can. A build INPUT, so it carries no label.
	SerenaDependencyFloors string

	// Actionlint and Shellcheck lint GitHub Actions workflows, which the
	// fleet's `make lint` does (go-quicbaas#1039). shellcheck is installed from
	// its release tarball, whose SHA-256 per architecture is pinned beside the
	// version because upstream publishes no checksum file.
	Actionlint              string
	Shellcheck              string
	ShellcheckSHA256X86_64  string
	ShellcheckSHA256Aarch64 string

	// Opencode is the third agent CLI: the one a fleet node drives against a
	// local model server. Unlike claude-code and codex it is not refreshed from
	// npm but installed from its release tarball, so it is a toolchain pin,
	// with the release's published SHA-256 per architecture beside it.
	Opencode              string
	OpencodeSHA256X86_64  string
	OpencodeSHA256Aarch64 string

	// Protoc is the protobuf compiler a Go project's .proto files regenerate
	// code with inside this image, rather than a host-mounted protoc (the
	// container-only tooling rule). Installed from its release zip, whose
	// SHA-256 per architecture is pinned beside the version because upstream
	// publishes no checksum file.
	Protoc              string
	ProtocSHA256X86_64  string
	ProtocSHA256Aarch64 string

	// ProtocGenGo and ProtocGenGoGRPC are the Go code-generator plugins protoc
	// invokes, `go install`ed in the toolchain stage like golangci-lint.
	ProtocGenGo     string
	ProtocGenGoGRPC string

	// Node runs the npm-installed agent CLIs and language servers; Npm is the
	// npm the bundled one is upgraded to. Uv installs Serena. Each comes from
	// its release tarball, with the SHA-256 per architecture pinned beside it.
	Node              string
	NodeSHA256X86_64  string
	NodeSHA256Aarch64 string
	Npm               string
	Uv                string
	UvSHA256X86_64    string
	UvSHA256Aarch64   string

	// NpmBundledDependencyFloors raises packages npm bundles in its own
	// node_modules past a fixed CVE (toolchain.mk's
	// NPM_BUNDLED_DEPENDENCY_FLOORS): space-separated name@version=integrity
	// entries, each a patch release of the bundled package, verified against
	// its registry integrity. A build INPUT, so it carries no label.
	NpmBundledDependencyFloors string

	// The language servers Serena drives, baked in so a session with an empty
	// HOME and no network reads code by symbol. The npm and gem servers are
	// pinned by version; the release binaries carry their SHA-256 per
	// architecture too. clangd publishes no aarch64 Linux build, so an aarch64
	// image uses Debian's and only the x86_64 digest is pinned.
	Pyright                   string
	Typescript                string
	TypescriptLanguageServer  string
	BashLanguageServer        string
	YamlLanguageServer        string
	RubyLsp                   string
	Clangd                    string
	ClangdSHA256X86_64        string
	RustAnalyzer              string
	RustAnalyzerSHA256X86_64  string
	RustAnalyzerSHA256Aarch64 string
	Marksman                  string
	MarksmanSHA256X86_64      string
	MarksmanSHA256Aarch64     string
}

// ToolchainPins returns the repository's toolchain pins, as this binary
// embedded them from pkg/version/toolchain.mk.
func ToolchainPins() (Pins, error) {
	toolchain, err := version.Toolchain()
	if err != nil {
		return Pins{}, err
	}
	pins := Pins{
		Go:          toolchain["GO_VERSION"],
		Golangci:    toolchain["GOLANGCI_VERSION"],
		Gosec:       toolchain["GOSEC_VERSION"],
		Govulncheck: toolchain["GOVULNCHECK_VERSION"],
		Gopls:       toolchain["GOPLS_VERSION"],
		Serena:      toolchain["SERENA_VERSION"],

		ToolDependencyFloors:   toolchain["GO_TOOL_DEPENDENCY_FLOORS"],
		SerenaDependencyFloors: toolchain["SERENA_DEPENDENCY_FLOORS"],

		Actionlint:              toolchain["ACTIONLINT_VERSION"],
		Shellcheck:              toolchain["SHELLCHECK_VERSION"],
		ShellcheckSHA256X86_64:  toolchain["SHELLCHECK_SHA256_X86_64"],
		ShellcheckSHA256Aarch64: toolchain["SHELLCHECK_SHA256_AARCH64"],

		Opencode:              toolchain["OPENCODE_VERSION"],
		OpencodeSHA256X86_64:  toolchain["OPENCODE_SHA256_X86_64"],
		OpencodeSHA256Aarch64: toolchain["OPENCODE_SHA256_AARCH64"],

		Protoc:              toolchain["PROTOC_VERSION"],
		ProtocSHA256X86_64:  toolchain["PROTOC_SHA256_X86_64"],
		ProtocSHA256Aarch64: toolchain["PROTOC_SHA256_AARCH64"],

		ProtocGenGo:     toolchain["PROTOC_GEN_GO_VERSION"],
		ProtocGenGoGRPC: toolchain["PROTOC_GEN_GO_GRPC_VERSION"],

		Node:              toolchain["NODE_VERSION"],
		NodeSHA256X86_64:  toolchain["NODE_SHA256_X86_64"],
		NodeSHA256Aarch64: toolchain["NODE_SHA256_AARCH64"],
		Npm:               toolchain["NPM_VERSION"],
		Uv:                toolchain["UV_VERSION"],
		UvSHA256X86_64:    toolchain["UV_SHA256_X86_64"],
		UvSHA256Aarch64:   toolchain["UV_SHA256_AARCH64"],

		NpmBundledDependencyFloors: toolchain["NPM_BUNDLED_DEPENDENCY_FLOORS"],

		Pyright:                   toolchain["PYRIGHT_VERSION"],
		Typescript:                toolchain["TYPESCRIPT_VERSION"],
		TypescriptLanguageServer:  toolchain["TYPESCRIPT_LANGUAGE_SERVER_VERSION"],
		BashLanguageServer:        toolchain["BASH_LANGUAGE_SERVER_VERSION"],
		YamlLanguageServer:        toolchain["YAML_LANGUAGE_SERVER_VERSION"],
		RubyLsp:                   toolchain["RUBY_LSP_VERSION"],
		Clangd:                    toolchain["CLANGD_VERSION"],
		ClangdSHA256X86_64:        toolchain["CLANGD_SHA256_X86_64"],
		RustAnalyzer:              toolchain["RUST_ANALYZER_VERSION"],
		RustAnalyzerSHA256X86_64:  toolchain["RUST_ANALYZER_SHA256_X86_64"],
		RustAnalyzerSHA256Aarch64: toolchain["RUST_ANALYZER_SHA256_AARCH64"],
		Marksman:                  toolchain["MARKSMAN_VERSION"],
		MarksmanSHA256X86_64:      toolchain["MARKSMAN_SHA256_X86_64"],
		MarksmanSHA256Aarch64:     toolchain["MARKSMAN_SHA256_AARCH64"],
	}
	return pins, pins.Validate()
}

// Validate reports the pins that are empty.
func (p Pins) Validate() error {
	var missing []string
	for _, pin := range p.named() {
		if strings.TrimSpace(pin.value) == "" {
			missing = append(missing, pin.buildArg)
		}
	}
	if len(missing) > 0 {
		return &PinsMissingError{Missing: missing}
	}
	return nil
}

// namedPin pairs a pin with the build argument that carries it and the label
// that records it. A pin with no label is a build INPUT rather than a tool
// version — a tarball digest — and is not part of the bill of materials.
type namedPin struct {
	buildArg string
	label    string
	value    string
}

// named lists the pins in build-argument order.
func (p Pins) named() []namedPin {
	return []namedPin{
		{buildArg: "GO_VERSION", label: LabelGoVersion, value: p.Go},
		{buildArg: "GOLANGCI_VERSION", label: LabelGolangciVersion, value: p.Golangci},
		{buildArg: "GOSEC_VERSION", label: LabelGosecVersion, value: p.Gosec},
		{buildArg: "GOVULNCHECK_VERSION", label: LabelGovulncheckVersion, value: p.Govulncheck},
		{buildArg: "GOPLS_VERSION", label: LabelGoplsVersion, value: p.Gopls},
		{buildArg: "SERENA_VERSION", label: LabelSerenaVersion, value: p.Serena},
		{buildArg: "GO_TOOL_DEPENDENCY_FLOORS", value: p.ToolDependencyFloors},
		{buildArg: "SERENA_DEPENDENCY_FLOORS", value: p.SerenaDependencyFloors},
		{buildArg: "ACTIONLINT_VERSION", label: LabelActionlintVersion, value: p.Actionlint},
		{buildArg: "SHELLCHECK_VERSION", label: LabelShellcheckVersion, value: p.Shellcheck},
		{buildArg: "SHELLCHECK_SHA256_X86_64", value: p.ShellcheckSHA256X86_64},
		{buildArg: "SHELLCHECK_SHA256_AARCH64", value: p.ShellcheckSHA256Aarch64},
		{buildArg: "OPENCODE_VERSION", label: LabelOpencodeVersion, value: p.Opencode},
		{buildArg: "OPENCODE_SHA256_X86_64", value: p.OpencodeSHA256X86_64},
		{buildArg: "OPENCODE_SHA256_AARCH64", value: p.OpencodeSHA256Aarch64},
		{buildArg: "PROTOC_VERSION", label: LabelProtocVersion, value: p.Protoc},
		{buildArg: "PROTOC_SHA256_X86_64", value: p.ProtocSHA256X86_64},
		{buildArg: "PROTOC_SHA256_AARCH64", value: p.ProtocSHA256Aarch64},
		{buildArg: "PROTOC_GEN_GO_VERSION", label: LabelProtocGenGoVersion, value: p.ProtocGenGo},
		{buildArg: "PROTOC_GEN_GO_GRPC_VERSION", label: LabelProtocGenGoGRPCVersion, value: p.ProtocGenGoGRPC},
		{buildArg: "NODE_VERSION", label: LabelNodeVersion, value: p.Node},
		{buildArg: "NODE_SHA256_X86_64", value: p.NodeSHA256X86_64},
		{buildArg: "NODE_SHA256_AARCH64", value: p.NodeSHA256Aarch64},
		{buildArg: "NPM_VERSION", label: LabelNpmVersion, value: p.Npm},
		{buildArg: "NPM_BUNDLED_DEPENDENCY_FLOORS", value: p.NpmBundledDependencyFloors},
		{buildArg: "UV_VERSION", label: LabelUvVersion, value: p.Uv},
		{buildArg: "UV_SHA256_X86_64", value: p.UvSHA256X86_64},
		{buildArg: "UV_SHA256_AARCH64", value: p.UvSHA256Aarch64},
		{buildArg: "PYRIGHT_VERSION", label: LabelPyrightVersion, value: p.Pyright},
		{buildArg: "TYPESCRIPT_VERSION", label: LabelTypescriptVersion, value: p.Typescript},
		{buildArg: "TYPESCRIPT_LANGUAGE_SERVER_VERSION", label: LabelTypescriptLanguageServerVersion, value: p.TypescriptLanguageServer},
		{buildArg: "BASH_LANGUAGE_SERVER_VERSION", label: LabelBashLanguageServerVersion, value: p.BashLanguageServer},
		{buildArg: "YAML_LANGUAGE_SERVER_VERSION", label: LabelYamlLanguageServerVersion, value: p.YamlLanguageServer},
		{buildArg: "RUBY_LSP_VERSION", label: LabelRubyLspVersion, value: p.RubyLsp},
		{buildArg: "CLANGD_VERSION", label: LabelClangdVersion, value: p.Clangd},
		{buildArg: "CLANGD_SHA256_X86_64", value: p.ClangdSHA256X86_64},
		{buildArg: "RUST_ANALYZER_VERSION", label: LabelRustAnalyzerVersion, value: p.RustAnalyzer},
		{buildArg: "RUST_ANALYZER_SHA256_X86_64", value: p.RustAnalyzerSHA256X86_64},
		{buildArg: "RUST_ANALYZER_SHA256_AARCH64", value: p.RustAnalyzerSHA256Aarch64},
		{buildArg: "MARKSMAN_VERSION", label: LabelMarksmanVersion, value: p.Marksman},
		{buildArg: "MARKSMAN_SHA256_X86_64", value: p.MarksmanSHA256X86_64},
		{buildArg: "MARKSMAN_SHA256_AARCH64", value: p.MarksmanSHA256Aarch64},
	}
}

// The image's labels. The Dockerfile writes every one of them from its build
// arguments, so an image built by `make build-worker-image` and one built by
// the daemon's refresh are labelled identically.
//
// The three agent-CLI labels are the ones a refresh READS: they are how the
// daemon knows what a bound image carries without running it.
const (
	LabelClaudeCodeVersion      = "io.quicbaas.worker.claude-code.version"
	LabelCodexVersion           = "io.quicbaas.worker.codex.version"
	LabelGeminiVersion          = "io.quicbaas.worker.gemini.version"
	LabelBaseVersion            = "io.quicbaas.worker.base.version"
	LabelGoVersion              = "io.quicbaas.worker.go.version"
	LabelGolangciVersion        = "io.quicbaas.worker.golangci-lint.version"
	LabelGosecVersion           = "io.quicbaas.worker.gosec.version"
	LabelGovulncheckVersion     = "io.quicbaas.worker.govulncheck.version"
	LabelGoplsVersion           = "io.quicbaas.worker.gopls.version"
	LabelSerenaVersion          = "io.quicbaas.worker.serena.version"
	LabelActionlintVersion      = "io.quicbaas.worker.actionlint.version"
	LabelShellcheckVersion      = "io.quicbaas.worker.shellcheck.version"
	LabelOpencodeVersion        = "io.quicbaas.worker.opencode.version"
	LabelProtocVersion          = "io.quicbaas.worker.protoc.version"
	LabelProtocGenGoVersion     = "io.quicbaas.worker.protoc-gen-go.version"
	LabelProtocGenGoGRPCVersion = "io.quicbaas.worker.protoc-gen-go-grpc.version"

	LabelNodeVersion                     = "io.quicbaas.worker.node.version"
	LabelNpmVersion                      = "io.quicbaas.worker.npm.version"
	LabelUvVersion                       = "io.quicbaas.worker.uv.version"
	LabelPyrightVersion                  = "io.quicbaas.worker.pyright.version"
	LabelTypescriptVersion               = "io.quicbaas.worker.typescript.version"
	LabelTypescriptLanguageServerVersion = "io.quicbaas.worker.typescript-language-server.version"
	LabelBashLanguageServerVersion       = "io.quicbaas.worker.bash-language-server.version"
	LabelYamlLanguageServerVersion       = "io.quicbaas.worker.yaml-language-server.version"
	LabelRubyLspVersion                  = "io.quicbaas.worker.ruby-lsp.version"
	LabelClangdVersion                   = "io.quicbaas.worker.clangd.version"
	LabelRustAnalyzerVersion             = "io.quicbaas.worker.rust-analyzer.version"
	LabelMarksmanVersion                 = "io.quicbaas.worker.marksman.version"
)

// The npm packages the image installs the agents from.
const (
	ClaudeCodePackage = "@anthropic-ai/claude-code"
	CodexPackage      = "@openai/codex"
	GeminiPackage     = "@google/gemini-cli"
)

// DevcacheEnvSHA256 is the SHA-256 of the devcache-env client script the
// Dockerfile embeds byte-for-byte via a heredoc (dev-cache v0.1.0,
// client/devcache-env). The Dockerfile's own `sha256sum -c` line checks the
// copy that lands in the image against this same digest; it is recorded here
// too so the test suite can check the Dockerfile's embedded bytes against it
// independently of the build.
const DevcacheEnvSHA256 = "54ebcd1955daa328f31ebe8c93b50b71fcb9489a4a45a0a2986c530d4517cd29"

// Agents are the npm-installed agent-CLI versions an image carries. Gemini is
// installed for operators to use by hand; it is not in the content tag (Tag).
type Agents struct {
	ClaudeCode string `json:"claude_code"`
	Codex      string `json:"codex"`
	Gemini     string `json:"gemini"`
}

// AgentsFromLabels reads the agent versions an image's labels declare. An
// image built before a label existed does not declare it, and reports that
// version empty: it is unknown, which is not the same as current.
func AgentsFromLabels(labels map[string]string) Agents {
	return Agents{
		ClaudeCode: labels[LabelClaudeCodeVersion],
		Codex:      labels[LabelCodexVersion],
		Gemini:     labels[LabelGeminiVersion],
	}
}

// LabelLanguages names, space-separated, the serena languages whose language
// servers the image carries. The factory reads it to decide which languages a
// lane's serena may start, and refuses an image that does not declare it. The
// list itself is written once, as the Dockerfile's ARG WORKER_LANGUAGES
// default, and the build fails when a listed language's server is missing.
const LabelLanguages = "io.quicbaas.worker.languages"

// LanguageServers maps each serena language a worker image may declare to the
// server binary serena runs for it. It is the vocabulary LabelLanguages is
// checked against, not the image's list: a name outside it is a typo or a
// language no image of this Dockerfile supplies.
var LanguageServers = map[string]string{
	"bash":       "bash-language-server",
	"cpp":        "clangd",
	"go":         "gopls",
	"markdown":   "marksman",
	"python":     "pyright-langserver",
	"ruby":       "ruby-lsp",
	"rust":       "rust-analyzer",
	"typescript": "typescript-language-server",
	"yaml":       "yaml-language-server",
}

// LanguagesFromLabels reads the serena languages an image's labels declare. An
// image built before the label existed declares none.
func LanguagesFromLabels(labels map[string]string) []string {
	return strings.Fields(labels[LabelLanguages])
}

// Build is everything one build of the image is a function of.
type Build struct {
	// Base is the QuicBaaS release the Dockerfile came from.
	Base string

	// Agents are the agent-CLI versions installed.
	Agents Agents

	// Pins are the toolchain versions.
	Pins Pins

	// UID and GID are the ids the image's agent user is created with: the ids
	// the sandboxed spawn runs as (see the Dockerfile's AGENT_UID).
	UID int
	GID int
}

// Validate checks the build can be named and run.
func (b Build) Validate() error {
	if err := ValidateVersion("base", b.Base); err != nil {
		return err
	}
	if err := ValidateVersion(ClaudeCodePackage, b.Agents.ClaudeCode); err != nil {
		return err
	}
	if err := ValidateVersion(CodexPackage, b.Agents.Codex); err != nil {
		return err
	}
	if err := ValidateVersion(GeminiPackage, b.Agents.Gemini); err != nil {
		return err
	}
	if b.UID < 0 || b.GID < 0 {
		return &InvalidBuildError{Field: "uid/gid", Message: "must not be negative"}
	}
	if len(b.Tag()) > maxTagLength {
		return &InvalidBuildError{Field: "tag", Message: "longer than the " + strconv.Itoa(maxTagLength) + " characters a registry accepts"}
	}
	return b.Pins.Validate()
}

// maxTagLength is the OCI distribution limit on a tag.
const maxTagLength = 128

// Tag is the image's CONTENT TAG: "<base>-cc<claude-code>-cx<codex>", e.g.
// "0.11.0-alpha-cc2.1.280-cx0.156.0".
//
// It names what is inside rather than when it was built, so the same inputs
// always produce the same name, a newer agent CLI always produces a new one,
// and a previous tag is never overwritten — which is what keeps it available
// for a rollback.
func (b Build) Tag() string {
	return b.Base + "-cc" + b.Agents.ClaudeCode + "-cx" + b.Agents.Codex
}

// BuildArgs are the build's --build-arg values.
func (b Build) BuildArgs() map[string]string {
	args := map[string]string{
		"CLAUDE_CODE_VERSION": b.Agents.ClaudeCode,
		"CODEX_VERSION":       b.Agents.Codex,
		"GEMINI_CLI_VERSION":  b.Agents.Gemini,
		"QUICBAAS_VERSION":    b.Base,
		"AGENT_UID":           strconv.Itoa(b.UID),
		"AGENT_GID":           strconv.Itoa(b.GID),
	}
	for _, pin := range b.Pins.named() {
		args[pin.buildArg] = pin.value
	}
	return args
}

// Labels are the labels the build's image carries.
func (b Build) Labels() map[string]string {
	labels := map[string]string{
		LabelClaudeCodeVersion: b.Agents.ClaudeCode,
		LabelCodexVersion:      b.Agents.Codex,
		LabelGeminiVersion:     b.Agents.Gemini,
		LabelBaseVersion:       b.Base,
	}
	for _, pin := range b.Pins.named() {
		if pin.label != "" {
			labels[pin.label] = pin.value
		}
	}
	return labels
}

// versionPattern is what a version may look like to be part of a tag and a
// build argument: semver's characters without build metadata ("+" is not a
// tag character), starting alphanumeric.
var versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.\-]*$`)

// ValidateVersion refuses a version that cannot be part of a tag. It is also
// the guard between a registry's answer and a build argument: a version is
// only ever these characters.
func ValidateVersion(what, version string) error {
	if !versionPattern.MatchString(version) {
		return &InvalidVersionError{What: what, Version: version}
	}
	return nil
}
