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

package softwarefactoryworker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

// dockerfileArgPattern matches a DEFAULT-LESS ARG declaration's name: one
// whose value can only come from the outside, which is what makes it a
// build-environment input worth tracking. An ARG with its own `=default` (a
// fixed path, a version baked into the Dockerfile text) is not: its value
// never varies without an edit to the Dockerfile itself.
var dockerfileArgPattern = regexp.MustCompile(`(?m)^ARG\s+([A-Za-z_][A-Za-z0-9_]*)\s*$`)

// privateModuleVarPattern matches an ARG or ENV instruction line that names
// GOPRIVATE, GONOPROXY or GONOSUMDB: every module resolves through the proxy
// and the checksum database, so the image declares none of them.
var privateModuleVarPattern = regexp.MustCompile(`(?m)^\s*(ARG|ENV)\s.*\b(GOPRIVATE|GONOPROXY|GONOSUMDB)\b.*$`)

// dockerfileCopyPattern matches a COPY instruction's own line (not the
// heredoc body that may follow it).
var dockerfileCopyPattern = regexp.MustCompile(`(?m)^COPY\s+\S.*$`)

// dockerfileHeredocPattern captures the devcache-env heredoc's body: between
// the line that opens it and the line that is exactly the closing delimiter.
var dockerfileHeredocPattern = regexp.MustCompile(`(?s)COPY --chmod=0755 <<'DEVCACHE_ENV_SCRIPT' /usr/local/bin/devcache-env\n(.*?)\nDEVCACHE_ENV_SCRIPT\n`)

const testNpmBundledFloors = "brace-expansion@5.0.11=sha512-YWFh undici@6.28.1=sha512-YmJi"

func testPins() Pins {
	return Pins{Go: "1.27.1", Golangci: "v2.14.0", Gosec: "v2.29.0", Govulncheck: "v1.8.0", Gopls: "v0.23.0", Serena: "v1.7.0",
		Actionlint: "v1.7.12", Shellcheck: "v0.11.0", ShellcheckSHA256X86_64: "8c3b", ShellcheckSHA256Aarch64: "12b3",
		Opencode: "v1.18.35", OpencodeSHA256X86_64: "1ce4", OpencodeSHA256Aarch64: "abf3",
		Gh: "v2.103.3", GhSHA256X86_64: "b8b8", GhSHA256Aarch64: "c9c9",
		Protoc: "36.2", ProtocSHA256X86_64: "aaaa", ProtocSHA256Aarch64: "bbbb",
		ProtocGenGo: "v1.36.12", ProtocGenGoGRPC: "v1.6.2",
		ToolDependencyFloors: "golang.org/x/mod@v0.41.0", SerenaDependencyFloors: "urllib3>=2.8.0",
		Node: "v26.10.0", NodeSHA256X86_64: "cccc", NodeSHA256Aarch64: "dddd", Npm: "12.2.0",
		NpmBundledDependencyFloors: testNpmBundledFloors,
		Uv:                         "0.12.23", UvSHA256X86_64: "eeee", UvSHA256Aarch64: "ffff",
		Pyright: "1.1.414", Typescript: "6.0.3", TypescriptLanguageServer: "6.0.1",
		BashLanguageServer: "5.8.1", YamlLanguageServer: "1.24.0", RubyLsp: "0.26.11",
		Clangd: "23.1.0", ClangdSHA256X86_64: "a1a1",
		RustAnalyzer: "2026-10-05", RustAnalyzerSHA256X86_64: "b2b2", RustAnalyzerSHA256Aarch64: "c3c3",
		Marksman: "2026-02-08", MarksmanSHA256X86_64: "d4d4", MarksmanSHA256Aarch64: "e5e5",
		OpenJDK: "21", KotlinLsp: "263.6379.0", KotlinLspSHA256X86_64: "f6f6", KotlinLspSHA256Aarch64: "a7a7"}
}

func testBuild() Build {
	return Build{
		Base:   "0.11.0-alpha",
		Agents: Agents{ClaudeCode: "2.1.280", Codex: "0.156.0", Gemini: "0.64.0"},
		Pins:   testPins(),
		UID:    1000,
		GID:    1000,
	}
}

func TestBuild_TagIsTheContentTag(t *testing.T) {
	if got, want := testBuild().Tag(), "0.11.0-alpha-cc2.1.280-cx0.156.0"; got != want {
		t.Fatalf("Tag() = %q, want %q", got, want)
	}
}

func TestBuild_ValidateAcceptsARealBuild(t *testing.T) {
	if err := testBuild().Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestBuild_ValidateRefuses(t *testing.T) {
	cases := map[string]func(*Build){
		"empty claude-code":      func(b *Build) { b.Agents.ClaudeCode = "" },
		"empty gemini":           func(b *Build) { b.Agents.Gemini = "" },
		"shell in gemini":        func(b *Build) { b.Agents.Gemini = "0.64.0;id" },
		"build metadata":         func(b *Build) { b.Agents.Codex = "1.0.0+sha" },
		"shell in a version":     func(b *Build) { b.Agents.Codex = "1.0;rm -rf /" },
		"leading dash":           func(b *Build) { b.Base = "-x" },
		"negative uid":           func(b *Build) { b.UID = -1 },
		"missing pin":            func(b *Build) { b.Pins.Gopls = "" },
		"missing floors":         func(b *Build) { b.Pins.ToolDependencyFloors = " " },
		"tag past the OCI limit": func(b *Build) { b.Base = strings.Repeat("9", 120) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			b := testBuild()
			mutate(&b)
			if err := b.Validate(); !errors.Is(err, ErrInvalidBuild) {
				t.Fatalf("Validate() = %v, want ErrInvalidBuild", err)
			}
		})
	}
}

func TestBuild_BuildArgsCarryEveryInput(t *testing.T) {
	args := testBuild().BuildArgs()
	want := map[string]string{
		"CLAUDE_CODE_VERSION": "2.1.280", "CODEX_VERSION": "0.156.0", "GEMINI_CLI_VERSION": "0.64.0", "QUICBAAS_VERSION": "0.11.0-alpha",
		"AGENT_UID": "1000", "AGENT_GID": "1000", "GO_VERSION": "1.27.1", "GOLANGCI_VERSION": "v2.14.0",
		"GOSEC_VERSION": "v2.29.0", "GOVULNCHECK_VERSION": "v1.8.0", "GOPLS_VERSION": "v0.23.0", "SERENA_VERSION": "v1.7.0",
		"GO_TOOL_DEPENDENCY_FLOORS": "golang.org/x/mod@v0.41.0",
		"SERENA_DEPENDENCY_FLOORS":  "urllib3>=2.8.0",
		"ACTIONLINT_VERSION":        "v1.7.12", "SHELLCHECK_VERSION": "v0.11.0",
		"SHELLCHECK_SHA256_X86_64": "8c3b", "SHELLCHECK_SHA256_AARCH64": "12b3",
		"OPENCODE_VERSION": "v1.18.35", "OPENCODE_SHA256_X86_64": "1ce4", "OPENCODE_SHA256_AARCH64": "abf3",
		"GH_VERSION": "v2.103.3", "GH_SHA256_X86_64": "b8b8", "GH_SHA256_AARCH64": "c9c9",
		"PROTOC_VERSION": "36.2", "PROTOC_SHA256_X86_64": "aaaa", "PROTOC_SHA256_AARCH64": "bbbb",
		"PROTOC_GEN_GO_VERSION": "v1.36.12", "PROTOC_GEN_GO_GRPC_VERSION": "v1.6.2",
		"NODE_VERSION": "v26.10.0", "NODE_SHA256_X86_64": "cccc", "NODE_SHA256_AARCH64": "dddd", "NPM_VERSION": "12.2.0",
		"NPM_BUNDLED_DEPENDENCY_FLOORS": testNpmBundledFloors,
		"UV_VERSION":                    "0.12.23", "UV_SHA256_X86_64": "eeee", "UV_SHA256_AARCH64": "ffff",
		"PYRIGHT_VERSION": "1.1.414", "TYPESCRIPT_VERSION": "6.0.3", "TYPESCRIPT_LANGUAGE_SERVER_VERSION": "6.0.1",
		"BASH_LANGUAGE_SERVER_VERSION": "5.8.1", "YAML_LANGUAGE_SERVER_VERSION": "1.24.0", "RUBY_LSP_VERSION": "0.26.11",
		"CLANGD_VERSION": "23.1.0", "CLANGD_SHA256_X86_64": "a1a1",
		"RUST_ANALYZER_VERSION": "2026-10-05", "RUST_ANALYZER_SHA256_X86_64": "b2b2", "RUST_ANALYZER_SHA256_AARCH64": "c3c3",
		"MARKSMAN_VERSION": "2026-02-08", "MARKSMAN_SHA256_X86_64": "d4d4", "MARKSMAN_SHA256_AARCH64": "e5e5",
		"OPENJDK_MAJOR": "21", "KOTLIN_LSP_VERSION": "263.6379.0",
		"KOTLIN_LSP_SHA256_X86_64": "f6f6", "KOTLIN_LSP_SHA256_AARCH64": "a7a7",
	}
	for k, v := range want {
		if args[k] != v {
			t.Errorf("BuildArgs()[%s] = %q, want %q", k, args[k], v)
		}
	}
	if len(args) != len(want) {
		t.Errorf("BuildArgs() has %d entries, want %d", len(args), len(want))
	}
}

// TestDockerfile_DeclaresEveryBuildArgAndLabel is the drift guard between the
// Dockerfile and this package: every build argument the refresh passes must be
// declared, and every label the refresh reads must be written, or a daemon-built
// image would be labelled differently from a make-built one.
func TestDockerfile_DeclaresEveryBuildArgAndLabel(t *testing.T) {
	dockerfile := string(Dockerfile)
	if dockerfile == "" {
		t.Fatal("the Dockerfile was not embedded")
	}
	buildArgs := testBuild().BuildArgs()
	for arg := range buildArgs {
		if !strings.Contains(dockerfile, "ARG "+arg) {
			t.Errorf("the Dockerfile does not declare ARG %s", arg)
		}
	}
	for label := range testBuild().Labels() {
		if !strings.Contains(dockerfile, label+"=") {
			t.Errorf("the Dockerfile does not write label %s", label)
		}
	}
	if strings.Contains(dockerfile, "claude-code@latest") || strings.Contains(dockerfile, "codex@latest") ||
		strings.Contains(dockerfile, "gemini-cli@latest") {
		t.Error("the agent CLIs must be installed at their pinned versions, never @latest")
	}

	// Every ARG the Dockerfile declares is either one of the content-tag
	// inputs in Build.BuildArgs(), or named on this allow-list: a build-host
	// property (the dev caches, go-quicbaas#1159) that never changes the
	// image's content tag. An ARG that is neither is a hole: nobody reviewed
	// whether it belongs on the tag.
	optionalBuildEnvArgs := map[string]bool{
		"DEVCACHE_GOPROXY":      true,
		"DEVCACHE_NPM_REGISTRY": true,
	}
	for _, arg := range dockerfileArgNames(dockerfile) {
		if _, ok := buildArgs[arg]; ok {
			continue
		}
		if !optionalBuildEnvArgs[arg] {
			t.Errorf("ARG %s is declared but is neither a Build.BuildArgs() input nor on the optional build-environment allow-list", arg)
		}
	}
	for arg := range optionalBuildEnvArgs {
		if !strings.Contains(dockerfile, "ARG "+arg) {
			t.Errorf("the Dockerfile does not declare ARG %s", arg)
		}
		if strings.Contains(dockerfile, "ENV "+arg) {
			t.Errorf("ARG %s must never become an ENV: it would outlive the RUN that needs it", arg)
		}
	}
	for _, name := range []string{"GOPROXY", "npm_config_registry"} {
		if strings.Contains(dockerfile, "ENV "+name) {
			t.Errorf("the Dockerfile must never ENV %s: an empty/unset cache must build exactly as it did before the caches existed", name)
		}
	}
	for _, line := range privateModuleVarPattern.FindAllString(dockerfile, -1) {
		t.Errorf("the image must never ARG or ENV a private-module variable: %q", strings.TrimSpace(line))
	}

	// The Dockerfile builds from an EMPTY context (pkg/orchestration/
	// container_image.go buildImageArgs builds it with `docker build --file -`
	// and no context directory): every COPY instruction may only move bytes
	// between its own stages (--from=) or embed a heredoc (<<), never pull
	// from a build directory it does not have.
	for _, line := range copyLines(dockerfile) {
		if !strings.Contains(line, "--from=") && !strings.Contains(line, "<<") {
			t.Errorf("the Dockerfile must build from an empty context: COPY must use --from= or a heredoc, got %q", line)
		}
	}
}

// dockerfileArgNames returns the name of every ARG the Dockerfile declares,
// deduplicated.
func dockerfileArgNames(dockerfile string) []string {
	seen := map[string]bool{}
	var names []string
	for _, match := range dockerfileArgPattern.FindAllStringSubmatch(dockerfile, -1) {
		name := match[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// copyLines returns every COPY instruction line in the Dockerfile.
func copyLines(dockerfile string) []string {
	return dockerfileCopyPattern.FindAllString(dockerfile, -1)
}

// TestDockerfile_EmbedsDevcacheEnvAtThePinnedDigest proves the devcache-env
// bytes the Dockerfile's heredoc embeds are exactly the ones go-quicbaas#1159
// pinned (dev-cache v0.1.0, client/devcache-env) -- not a copy that drifted
// from the vendored script -- and that the Dockerfile's own `sha256sum -c`
// line checks the image's copy against that same digest.
func TestDockerfile_EmbedsDevcacheEnvAtThePinnedDigest(t *testing.T) {
	dockerfile := string(Dockerfile)
	match := dockerfileHeredocPattern.FindStringSubmatch(dockerfile)
	if match == nil {
		t.Fatal("the Dockerfile does not embed a devcache-env heredoc at /usr/local/bin/devcache-env")
	}
	body := match[1] + "\n"
	sum := sha256.Sum256([]byte(body))
	if got := hex.EncodeToString(sum[:]); got != DevcacheEnvSHA256 {
		t.Fatalf("the embedded devcache-env script's SHA-256 is %s, want the pinned %s", got, DevcacheEnvSHA256)
	}
	if !strings.Contains(dockerfile, "echo \""+DevcacheEnvSHA256+"  /usr/local/bin/devcache-env\" | sha256sum -c -") {
		t.Errorf("the Dockerfile's sha256sum -c line does not check the pinned digest %s", DevcacheEnvSHA256)
	}
}

// TestDockerfile_GoInstallUsesDevcacheProxyChain proves the toolchain stage's
// `go install` / `go get` / `go build` RUN feeds DEVCACHE_GOPROXY through
// Go's `|` fallback chain -- never `,`, which fails a build outright on a
// cache outage -- and sets none of GOPRIVATE, GONOPROXY or GONOSUMDB, so
// every module, first-party included, resolves through the proxy and is
// checked against the checksum database
// (../handbook/software/conventions/devops.md section 2a).
func TestDockerfile_GoInstallUsesDevcacheProxyChain(t *testing.T) {
	dockerfile := string(Dockerfile)
	if !strings.Contains(dockerfile, `GOPROXY="${DEVCACHE_GOPROXY:+${DEVCACHE_GOPROXY%/}|https://proxy.golang.org|direct}"`) {
		t.Error("the Go install RUN does not build GOPROXY from DEVCACHE_GOPROXY with the | fallback chain")
	}
	for _, name := range []string{"GOPRIVATE=", "GONOPROXY=", "GONOSUMDB="} {
		if strings.Contains(dockerfile, name) {
			t.Errorf("the Dockerfile sets %s: modules must resolve through the proxy and the checksum database", name)
		}
	}
}

// TestDockerfile_NpmInstallEvalsDevcacheEnvFirst proves the agent-CLI npm
// install reads the dev caches before it installs, and that devcache-env
// lands in the image before that RUN needs it.
func TestDockerfile_NpmInstallEvalsDevcacheEnvFirst(t *testing.T) {
	dockerfile := string(Dockerfile)
	heredocAt := strings.Index(dockerfile, "COPY --chmod=0755 <<'DEVCACHE_ENV_SCRIPT'")
	evalAt := strings.Index(dockerfile, `eval "$(devcache-env)"`)
	installAt := strings.Index(dockerfile, `npm install -g --allow-scripts="@anthropic-ai/claude-code@`)
	if heredocAt == -1 || evalAt == -1 || installAt == -1 {
		t.Fatalf("heredoc at %d, eval at %d, npm install at %d: expected all three present", heredocAt, evalAt, installAt)
	}
	if !(heredocAt < evalAt && evalAt < installAt) {
		t.Errorf("expected devcache-env embedded, then eval'd, then npm install -g; got offsets %d, %d, %d", heredocAt, evalAt, installAt)
	}
}

// TestDockerfile_OnlyClaudeCodeRunsAnInstallScript proves npm 12's script
// policy is opened for exactly one package at its pin: Claude Code, whose
// postinstall places its native binary. A wider allowance would let any
// package in the image's npm trees run code at build time.
func TestDockerfile_OnlyClaudeCodeRunsAnInstallScript(t *testing.T) {
	dockerfile := string(Dockerfile)
	if got := strings.Count(dockerfile, "--allow-scripts="); got != 1 {
		t.Fatalf("--allow-scripts appears %d times, want once (the Claude Code install)", got)
	}
	if !strings.Contains(dockerfile, `--allow-scripts="@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}"`) {
		t.Error("the allowance must name Claude Code at its pinned version, not the bare package or a list")
	}
	for _, wide := range []string{"--dangerously-allow-all-scripts", "--ignore-scripts=false", "allow-scripts=true"} {
		if strings.Contains(dockerfile, wide) {
			t.Errorf("the Dockerfile opens install scripts to every package with %q", wide)
		}
	}
}

func TestToolchainPins_AreTheRepositorysPins(t *testing.T) {
	pins, err := ToolchainPins()
	if err != nil {
		t.Fatalf("ToolchainPins: %v", err)
	}
	toolchain, err := version.Toolchain()
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range pins.named() {
		if pin.value != toolchain[pin.buildArg] {
			t.Errorf("pin %s = %q, toolchain.mk says %q", pin.buildArg, pin.value, toolchain[pin.buildArg])
		}
	}
}

func TestPins_ValidateNamesWhatIsMissing(t *testing.T) {
	var missing *PinsMissingError
	err := Pins{}.Validate()
	if !errors.As(err, &missing) || len(missing.Missing) != 49 ||
		!strings.Contains(err.Error(), "GOPLS_VERSION") || !strings.Contains(err.Error(), "SHELLCHECK_SHA256_AARCH64") ||
		!strings.Contains(err.Error(), "OPENCODE_SHA256_AARCH64") || !strings.Contains(err.Error(), "GO_TOOL_DEPENDENCY_FLOORS") ||
		!strings.Contains(err.Error(), "SERENA_DEPENDENCY_FLOORS") || !strings.Contains(err.Error(), "PROTOC_VERSION") ||
		!strings.Contains(err.Error(), "PROTOC_SHA256_AARCH64") || !strings.Contains(err.Error(), "PROTOC_GEN_GO_GRPC_VERSION") ||
		!strings.Contains(err.Error(), "NODE_SHA256_AARCH64") || !strings.Contains(err.Error(), "UV_VERSION") ||
		!strings.Contains(err.Error(), "TYPESCRIPT_LANGUAGE_SERVER_VERSION") || !strings.Contains(err.Error(), "CLANGD_SHA256_X86_64") ||
		!strings.Contains(err.Error(), "MARKSMAN_SHA256_AARCH64") || !strings.Contains(err.Error(), "NPM_BUNDLED_DEPENDENCY_FLOORS") ||
		!strings.Contains(err.Error(), "OPENJDK_MAJOR") || !strings.Contains(err.Error(), "KOTLIN_LSP_SHA256_AARCH64") ||
		!strings.Contains(err.Error(), "GH_VERSION") || !strings.Contains(err.Error(), "GH_SHA256_AARCH64") {
		t.Fatalf("Validate() = %v, want all forty-nine pins named", err)
	}
}

func TestBuild_LabelsRecordToolVersionsNotTarballDigests(t *testing.T) {
	labels := testBuild().Labels()
	if labels[LabelActionlintVersion] != "v1.7.12" || labels[LabelShellcheckVersion] != "v0.11.0" ||
		labels[LabelOpencodeVersion] != "v1.18.35" || labels[LabelGhVersion] != "v2.103.3" {
		t.Fatalf("labels = %v, want actionlint, shellcheck, opencode and gh versions recorded", labels)
	}
	if labels[LabelProtocVersion] != "36.2" || labels[LabelProtocGenGoVersion] != "v1.36.12" ||
		labels[LabelProtocGenGoGRPCVersion] != "v1.6.2" {
		t.Fatalf("labels = %v, want protoc, protoc-gen-go and protoc-gen-go-grpc versions recorded", labels)
	}
	if labels[LabelNodeVersion] != "v26.10.0" || labels[LabelUvVersion] != "0.12.23" ||
		labels[LabelTypescriptVersion] != "6.0.3" || labels[LabelClangdVersion] != "23.1.0" ||
		labels[LabelRustAnalyzerVersion] != "2026-10-05" || labels[LabelMarksmanVersion] != "2026-02-08" {
		t.Fatalf("labels = %v, want node, uv and language-server versions recorded", labels)
	}
	if labels[LabelOpenJDKVersion] != "21" || labels[LabelKotlinLspVersion] != "263.6379.0" {
		t.Fatalf("labels = %v, want the OpenJDK major and the Kotlin language server version recorded", labels)
	}
	for key, value := range labels {
		switch value {
		case "8c3b", "12b3", "1ce4", "abf3", "aaaa", "bbbb", "cccc", "dddd", "eeee", "ffff",
			"a1a1", "b2b2", "c3c3", "d4d4", "e5e5", "f6f6", "a7a7", "b8b8", "c9c9", "golang.org/x/mod@v0.41.0", "urllib3>=2.8.0":
			t.Fatalf("label %q=%q: a tarball digest or dependency floor is a build input, not a label", key, value)
		}
	}
}

func TestAgentsFromLabels(t *testing.T) {
	got := AgentsFromLabels(testBuild().Labels())
	if got != (Agents{ClaudeCode: "2.1.280", Codex: "0.156.0", Gemini: "0.64.0"}) {
		t.Fatalf("AgentsFromLabels = %+v", got)
	}
	if got := AgentsFromLabels(nil); got != (Agents{}) {
		t.Fatalf("an unlabelled image declares no versions, got %+v", got)
	}
	// An image built before the gemini label existed reports Gemini unknown,
	// without losing the versions it does declare.
	labels := testBuild().Labels()
	delete(labels, LabelGeminiVersion)
	if got := AgentsFromLabels(labels); got != (Agents{ClaudeCode: "2.1.280", Codex: "0.156.0"}) {
		t.Fatalf("an image without the gemini label must report Gemini empty, got %+v", got)
	}
}

func TestLanguagesFromLabels_SplitsTheList(t *testing.T) {
	got := LanguagesFromLabels(map[string]string{LabelLanguages: " go  typescript\tyaml "})
	if !slices.Equal(got, []string{"go", "typescript", "yaml"}) {
		t.Fatalf("LanguagesFromLabels = %q, want [go typescript yaml]", got)
	}
}

func TestLanguagesFromLabels_AnUnlabelledImageDeclaresNone(t *testing.T) {
	if got := LanguagesFromLabels(map[string]string{LabelGoVersion: "1.26.2"}); len(got) != 0 {
		t.Fatalf("LanguagesFromLabels = %q, want none for an image without the label", got)
	}
}

// The Dockerfile's WORKER_LANGUAGES default is the image's one list, and its
// build check maps each language to a server. Both must stay inside the Go
// vocabulary the refresher's gate checks the label against.
func TestDockerfile_LanguagesLabelMatchesTheServerVocabulary(t *testing.T) {
	dockerfile := string(Dockerfile)
	match := regexp.MustCompile(`(?m)^ARG WORKER_LANGUAGES="([^"]*)"$`).FindStringSubmatch(dockerfile)
	if match == nil {
		t.Fatal("the Dockerfile does not declare ARG WORKER_LANGUAGES with a quoted default")
	}
	languages := strings.Fields(match[1])
	if len(languages) == 0 {
		t.Fatal("WORKER_LANGUAGES is empty")
	}
	for _, language := range languages {
		if _, ok := LanguageServers[language]; !ok {
			t.Errorf("WORKER_LANGUAGES names %q, which LanguageServers does not know", language)
		}
	}
	for language, server := range LanguageServers {
		if !strings.Contains(dockerfile, language+") server="+server+" ;;") {
			t.Errorf("the Dockerfile's language check does not map %s to %s", language, server)
		}
	}
	if !strings.Contains(dockerfile, LabelLanguages+`="${WORKER_LANGUAGES}"`) {
		t.Errorf("the Dockerfile does not write label %s from WORKER_LANGUAGES", LabelLanguages)
	}
}

func TestProbes_AreNamedUniquelyAndRunAsALoginShellContract(t *testing.T) {
	seen := map[string]bool{}
	var mounts int
	for _, p := range Probes() {
		if p.Name == "" || strings.TrimSpace(p.Script) == "" {
			t.Fatalf("probe %+v is incomplete", p)
		}
		if seen[p.Name] {
			t.Fatalf("probe name %q is duplicated", p.Name)
		}
		seen[p.Name] = true
		if p.WorkspaceMount {
			mounts++
			if !strings.Contains(p.Script, ProbeWorkspaceFile) {
				t.Fatalf("the workspace probe must write %s", ProbeWorkspaceFile)
			}
		}
	}
	if mounts != 1 {
		t.Fatalf("want exactly one bind-mount ownership probe, got %d", mounts)
	}
}

// TestProbes_ProveTheProductionSandboxBoundary pins the two probes main's
// #1040 added: full control must hold UNDER the production capability set, and
// the pids bound must refuse a fork past it (with a bound lowered for the probe
// alone, so it need not start thousands of processes). And locked memory must
// have the CI runners' limit, exercised by a real mlock (#1038).
func TestProbes_ProveTheProductionSandboxBoundary(t *testing.T) {
	var fullControl, pids, workflow, memlock *Probe
	for _, p := range Probes() {
		switch {
		case strings.Contains(p.Script, "CapEff"):
			fullControl = &p
		case strings.Contains(string(p.Stdin), "EAGAIN"):
			pids = &p
		case strings.Contains(p.Script, "actionlint"):
			workflow = &p
		case strings.Contains(p.Script, "libc.mlock"):
			memlock = &p
		}
	}
	if memlock == nil || !memlock.Sandboxed || memlock.PidsLimit != 0 ||
		!strings.Contains(memlock.Script, "$"+EnvExpectedMemlockKiB) {
		t.Fatalf("the memlock probe must run under the production policy and read its expectation from %s, got %+v",
			EnvExpectedMemlockKiB, memlock)
	}
	if _, set := ProbeEnv(testBuild())[EnvExpectedMemlockKiB]; set {
		t.Fatal("ProbeEnv must not restate the memlock limit; the runner derives it from the policy")
	}
	if fullControl == nil || !fullControl.Sandboxed || fullControl.PidsLimit != 0 {
		t.Fatalf("the full-control probe must run under the production sandbox policy, got %+v", fullControl)
	}
	if pids == nil || !pids.Sandboxed || pids.PidsLimit != ProbePidsLimit || !strings.Contains(pids.Script, "python3 -") {
		t.Fatalf("the pids probe must feed pids-limit-probe.py to python3 under a lowered bound, got %+v", pids)
	}
	if workflow == nil || !workflow.NetworkNone || !strings.Contains(workflow.Script, "SC2046") {
		t.Fatalf("the workflow-lint probe must prove actionlint reaches shellcheck, offline, got %+v", workflow)
	}
}

func TestProbeEnv_CarriesExpectationsNotShell(t *testing.T) {
	env := ProbeEnv(testBuild())
	if env[EnvExpectedGolangci] != "2.14.0" {
		t.Fatalf("golangci-lint reports its version without the v: got %q", env[EnvExpectedGolangci])
	}
	if env[EnvExpectedActionlint] != "v1.7.12" || env[EnvExpectedShellcheck] != "0.11.0" {
		t.Fatalf("actionlint reports its tag, shellcheck its version without the v: got %v", env)
	}
	if env[EnvExpectedOpencode] != "1.18.35" {
		t.Fatalf("opencode reports its version without the v: got %q", env[EnvExpectedOpencode])
	}
	if env[EnvExpectedProtoc] != "36.2" {
		t.Fatalf("protoc reports libprotoc <version>: got %q", env[EnvExpectedProtoc])
	}
	if env[EnvExpectedProtocGenGo] != "v1.36.12" || env[EnvExpectedProtocGenGoGRPC] != "1.6.2" {
		t.Fatalf("protoc-gen-go reports its tag, protoc-gen-go-grpc its version without the v: got %v", env)
	}
	if env[EnvExpectedOpenJDK] != "21" {
		t.Fatalf("java -version is checked against the JDK major: got %q", env[EnvExpectedOpenJDK])
	}
	if env[EnvExpectedClaudeCode] != "2.1.280" || env[EnvExpectedCodex] != "0.156.0" ||
		env[EnvExpectedGemini] != "0.64.0" || env["HOME"] != "/tmp" {
		t.Fatalf("env = %v", env)
	}
	if env[EnvExpectedNpmBundledFloors] != "brace-expansion@5.0.11 undici@6.28.1" {
		t.Fatalf("the npm floors reach the probe as name@version, without integrities: got %q", env[EnvExpectedNpmBundledFloors])
	}
	for _, p := range Probes() {
		if strings.Contains(p.Script, "2.1.280") || strings.Contains(p.Script, "5.0.11") {
			t.Fatal("expected versions must reach a probe through its environment, not its text")
		}
	}
}

func TestNpmBundledFloorSpecs_DropsIntegrities(t *testing.T) {
	got := npmBundledFloorSpecs("  brace-expansion@5.0.11=sha512-YW== \n undici@6.28.1=sha512-Ym== ")
	if want := "brace-expansion@5.0.11 undici@6.28.1"; got != want {
		t.Fatalf("npmBundledFloorSpecs = %q, want %q", got, want)
	}
}

func TestNpmBundledFloorSpecs_EmptyOrUnpinnedInput(t *testing.T) {
	if got := npmBundledFloorSpecs(" \t"); got != "" {
		t.Fatalf("blank floors must yield no specs, got %q", got)
	}
	// An entry with no integrity passes through for the probe to compare;
	// the build, not this helper, refuses it for having nothing to verify.
	if got := npmBundledFloorSpecs("undici@6.28.1"); got != "undici@6.28.1" {
		t.Fatalf("an entry without an integrity = %q, want it unchanged", got)
	}
}

// TestProbes_NpmBundledFloorsAreCheckedOfflineFromTheEnvironment pins the
// probe that holds npm's own brace-expansion and undici at their floors: it
// reads the floors from its environment, checks every bundled copy, and runs
// with no network, as a sandboxed lane would.
func TestProbes_NpmBundledFloorsAreCheckedOfflineFromTheEnvironment(t *testing.T) {
	var floors *Probe
	for _, p := range Probes() {
		if strings.Contains(p.Script, "$"+EnvExpectedNpmBundledFloors) {
			floors = &p
		}
	}
	if floors == nil || !floors.NetworkNone {
		t.Fatalf("want an offline probe reading $%s, got %+v", EnvExpectedNpmBundledFloors, floors)
	}
	for _, want := range []string{`npm root -g`, `find "$modules"`, `sort -V`, `npm pack --dry-run`} {
		if !strings.Contains(floors.Script, want) {
			t.Errorf("the npm floors probe does not run %q", want)
		}
	}
}

// TestDockerfile_RaisesNpmBundledDependenciesBetweenUpgradeAndUse proves the
// floors are applied after the npm upgrade (which would otherwise restore the
// vulnerable copies) and before the language-server install that proves npm
// still works, and that the step verifies what it fetched and refuses a floor
// npm already meets.
func TestDockerfile_RaisesNpmBundledDependenciesBetweenUpgradeAndUse(t *testing.T) {
	dockerfile := string(Dockerfile)
	upgradeAt := strings.Index(dockerfile, `npm install -g "npm@${NPM_VERSION}"`)
	packAt := strings.Index(dockerfile, `npm pack --silent --pack-destination "$floors_dir" "$spec"`)
	serversAt := strings.Index(dockerfile, `npm install -g --prefix /opt/serena/language-servers`)
	if upgradeAt == -1 || packAt == -1 || serversAt == -1 || !(upgradeAt < packAt && packAt < serversAt) {
		t.Fatalf("want npm upgrade, then the floors, then the language servers; got offsets %d, %d, %d", upgradeAt, packAt, serversAt)
	}
	for _, want := range []string{
		`for floor in ${NPM_BUNDLED_DEPENDENCY_FLOORS}; do`,
		`if [ "$actual" != "$integrity" ]; then`,
		`at or above the floor $want: drop it from NPM_BUNDLED_DEPENDENCY_FLOORS.`,
		`no longer bundles $name: drop it from NPM_BUNDLED_DEPENDENCY_FLOORS.`,
		`is not a patch release of npm's bundled $have.`,
	} {
		if !strings.Contains(dockerfile, want) {
			t.Errorf("the npm floors step is missing %q", want)
		}
	}
}
