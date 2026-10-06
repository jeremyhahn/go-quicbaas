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

// dockerfileCopyPattern matches a COPY instruction's own line (not the
// heredoc body that may follow it).
var dockerfileCopyPattern = regexp.MustCompile(`(?m)^COPY\s+\S.*$`)

// dockerfileHeredocPattern captures the devcache-env heredoc's body: between
// the line that opens it and the line that is exactly the closing delimiter.
var dockerfileHeredocPattern = regexp.MustCompile(`(?s)COPY --chmod=0755 <<'DEVCACHE_ENV_SCRIPT' /usr/local/bin/devcache-env\n(.*?)\nDEVCACHE_ENV_SCRIPT\n`)

func testPins() Pins {
	return Pins{Go: "1.27.0", Golangci: "v2.13.2", Gosec: "v2.29.0", Govulncheck: "v1.8.0", Gopls: "v0.23.0", Serena: "v1.7.0",
		Actionlint: "v1.7.12", Shellcheck: "v0.11.0", ShellcheckSHA256X86_64: "8c3b", ShellcheckSHA256Aarch64: "12b3",
		Opencode: "v1.18.32", OpencodeSHA256X86_64: "1ce4", OpencodeSHA256Aarch64: "abf3",
		Protoc: "21.12", ProtocSHA256X86_64: "aaaa", ProtocSHA256Aarch64: "bbbb",
		ProtocGenGo: "v1.36.12", ProtocGenGoGRPC: "v1.6.1",
		ToolDependencyFloors: "golang.org/x/mod@v0.41.0", SerenaDependencyFloors: "urllib3>=2.8.0"}
}

func testBuild() Build {
	return Build{
		Base:   "0.11.0-alpha",
		Agents: Agents{ClaudeCode: "2.1.280", Codex: "0.156.0"},
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
		"CLAUDE_CODE_VERSION": "2.1.280", "CODEX_VERSION": "0.156.0", "QUICBAAS_VERSION": "0.11.0-alpha",
		"AGENT_UID": "1000", "AGENT_GID": "1000", "GO_VERSION": "1.27.0", "GOLANGCI_VERSION": "v2.13.2",
		"GOSEC_VERSION": "v2.29.0", "GOVULNCHECK_VERSION": "v1.8.0", "GOPLS_VERSION": "v0.23.0", "SERENA_VERSION": "v1.7.0",
		"GO_TOOL_DEPENDENCY_FLOORS": "golang.org/x/mod@v0.41.0",
		"SERENA_DEPENDENCY_FLOORS":  "urllib3>=2.8.0",
		"ACTIONLINT_VERSION":        "v1.7.12", "SHELLCHECK_VERSION": "v0.11.0",
		"SHELLCHECK_SHA256_X86_64": "8c3b", "SHELLCHECK_SHA256_AARCH64": "12b3",
		"OPENCODE_VERSION": "v1.18.32", "OPENCODE_SHA256_X86_64": "1ce4", "OPENCODE_SHA256_AARCH64": "abf3",
		"PROTOC_VERSION": "21.12", "PROTOC_SHA256_X86_64": "aaaa", "PROTOC_SHA256_AARCH64": "bbbb",
		"PROTOC_GEN_GO_VERSION": "v1.36.12", "PROTOC_GEN_GO_GRPC_VERSION": "v1.6.1",
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
	if strings.Contains(dockerfile, "claude-code@latest") || strings.Contains(dockerfile, "codex@latest") {
		t.Error("the agent CLIs must be installed at their pinned versions, never @latest")
	}

	// Every ARG the Dockerfile declares is either one of the content-tag
	// inputs in Build.BuildArgs(), or named on this allow-list: a build-host
	// property (the dev caches, go-quicbaas#1159) that never changes the
	// image's content tag. An ARG that is neither is a hole: nobody reviewed
	// whether it belongs on the tag.
	optionalBuildEnvArgs := map[string]bool{
		"DEVCACHE_GOPROXY":      true,
		"DEVCACHE_GOPRIVATE":    true,
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
	for _, name := range []string{"GOPROXY", "GOPRIVATE", "GONOPROXY", "GONOSUMDB", "npm_config_registry"} {
		if strings.Contains(dockerfile, "ENV "+name) {
			t.Errorf("the Dockerfile must never ENV %s: an empty/unset cache must build exactly as it did before the caches existed", name)
		}
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
// cache outage -- and sets GOPRIVATE/GONOPROXY/GONOSUMDB from
// DEVCACHE_GOPRIVATE, so a private module path never reaches a public proxy
// on a cache miss (../handbook/software/conventions/devops.md section 2a).
func TestDockerfile_GoInstallUsesDevcacheProxyChain(t *testing.T) {
	dockerfile := string(Dockerfile)
	if !strings.Contains(dockerfile, `GOPROXY="${DEVCACHE_GOPROXY:+${DEVCACHE_GOPROXY%/}|https://proxy.golang.org|direct}"`) {
		t.Error("the Go install RUN does not build GOPROXY from DEVCACHE_GOPROXY with the | fallback chain")
	}
	if !strings.Contains(dockerfile, `export GOPRIVATE="${DEVCACHE_GOPRIVATE}" GONOPROXY="${DEVCACHE_GOPRIVATE}" GONOSUMDB="${DEVCACHE_GOPRIVATE}"`) {
		t.Error("the Go install RUN does not set GOPRIVATE, GONOPROXY and GONOSUMDB from DEVCACHE_GOPRIVATE")
	}
}

// TestDockerfile_NpmInstallEvalsDevcacheEnvFirst proves the agent-CLI npm
// install reads the dev caches before it installs, and that devcache-env
// lands in the image before that RUN needs it.
func TestDockerfile_NpmInstallEvalsDevcacheEnvFirst(t *testing.T) {
	dockerfile := string(Dockerfile)
	heredocAt := strings.Index(dockerfile, "COPY --chmod=0755 <<'DEVCACHE_ENV_SCRIPT'")
	evalAt := strings.Index(dockerfile, `eval "$(devcache-env)"`)
	installAt := strings.Index(dockerfile, `npm install -g "@anthropic-ai/claude-code@`)
	if heredocAt == -1 || evalAt == -1 || installAt == -1 {
		t.Fatalf("heredoc at %d, eval at %d, npm install at %d: expected all three present", heredocAt, evalAt, installAt)
	}
	if !(heredocAt < evalAt && evalAt < installAt) {
		t.Errorf("expected devcache-env embedded, then eval'd, then npm install -g; got offsets %d, %d, %d", heredocAt, evalAt, installAt)
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
	if !errors.As(err, &missing) || len(missing.Missing) != 20 ||
		!strings.Contains(err.Error(), "GOPLS_VERSION") || !strings.Contains(err.Error(), "SHELLCHECK_SHA256_AARCH64") ||
		!strings.Contains(err.Error(), "OPENCODE_SHA256_AARCH64") || !strings.Contains(err.Error(), "GO_TOOL_DEPENDENCY_FLOORS") ||
		!strings.Contains(err.Error(), "SERENA_DEPENDENCY_FLOORS") || !strings.Contains(err.Error(), "PROTOC_VERSION") ||
		!strings.Contains(err.Error(), "PROTOC_SHA256_AARCH64") || !strings.Contains(err.Error(), "PROTOC_GEN_GO_GRPC_VERSION") {
		t.Fatalf("Validate() = %v, want all twenty pins named", err)
	}
}

func TestBuild_LabelsRecordToolVersionsNotTarballDigests(t *testing.T) {
	labels := testBuild().Labels()
	if labels[LabelActionlintVersion] != "v1.7.12" || labels[LabelShellcheckVersion] != "v0.11.0" ||
		labels[LabelOpencodeVersion] != "v1.18.32" {
		t.Fatalf("labels = %v, want actionlint, shellcheck and opencode versions recorded", labels)
	}
	if labels[LabelProtocVersion] != "21.12" || labels[LabelProtocGenGoVersion] != "v1.36.12" ||
		labels[LabelProtocGenGoGRPCVersion] != "v1.6.1" {
		t.Fatalf("labels = %v, want protoc, protoc-gen-go and protoc-gen-go-grpc versions recorded", labels)
	}
	for key, value := range labels {
		switch value {
		case "8c3b", "12b3", "1ce4", "abf3", "aaaa", "bbbb", "golang.org/x/mod@v0.41.0", "urllib3>=2.8.0":
			t.Fatalf("label %q=%q: a tarball digest or dependency floor is a build input, not a label", key, value)
		}
	}
}

func TestAgentsFromLabels(t *testing.T) {
	got := AgentsFromLabels(testBuild().Labels())
	if got != (Agents{ClaudeCode: "2.1.280", Codex: "0.156.0"}) {
		t.Fatalf("AgentsFromLabels = %+v", got)
	}
	if got := AgentsFromLabels(nil); got != (Agents{}) {
		t.Fatalf("an unlabelled image declares no versions, got %+v", got)
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
		case len(p.Stdin) > 0:
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
	if !strings.Contains(string(pids.Stdin), "EAGAIN") {
		t.Fatal("the pids probe's stdin must be pids-limit-probe.py")
	}
	if workflow == nil || !workflow.NetworkNone || !strings.Contains(workflow.Script, "SC2046") {
		t.Fatalf("the workflow-lint probe must prove actionlint reaches shellcheck, offline, got %+v", workflow)
	}
}

func TestProbeEnv_CarriesExpectationsNotShell(t *testing.T) {
	env := ProbeEnv(testBuild())
	if env[EnvExpectedGolangci] != "2.13.2" {
		t.Fatalf("golangci-lint reports its version without the v: got %q", env[EnvExpectedGolangci])
	}
	if env[EnvExpectedActionlint] != "v1.7.12" || env[EnvExpectedShellcheck] != "0.11.0" {
		t.Fatalf("actionlint reports its tag, shellcheck its version without the v: got %v", env)
	}
	if env[EnvExpectedOpencode] != "1.18.32" {
		t.Fatalf("opencode reports its version without the v: got %q", env[EnvExpectedOpencode])
	}
	if env[EnvExpectedProtoc] != "3.21.12" {
		t.Fatalf("protoc reports libprotoc 3.<version>: got %q", env[EnvExpectedProtoc])
	}
	if env[EnvExpectedProtocGenGo] != "v1.36.12" || env[EnvExpectedProtocGenGoGRPC] != "1.6.1" {
		t.Fatalf("protoc-gen-go reports its tag, protoc-gen-go-grpc its version without the v: got %v", env)
	}
	if env[EnvExpectedClaudeCode] != "2.1.280" || env[EnvExpectedCodex] != "0.156.0" || env["HOME"] != "/tmp" {
		t.Fatalf("env = %v", env)
	}
	for _, p := range Probes() {
		if strings.Contains(p.Script, "2.1.280") {
			t.Fatal("expected versions must reach a probe through its environment, not its text")
		}
	}
}
