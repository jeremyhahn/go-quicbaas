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
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/version"
)

func testPins() Pins {
	return Pins{Go: "1.27.0", Golangci: "v2.13.2", Gosec: "v2.29.0", Govulncheck: "v1.8.0", Gopls: "v0.23.0", Serena: "v1.7.0",
		Actionlint: "v1.7.12", Shellcheck: "v0.11.0", ShellcheckSHA256X86_64: "8c3b", ShellcheckSHA256Aarch64: "12b3",
		Opencode: "v1.18.32", OpencodeSHA256X86_64: "1ce4", OpencodeSHA256Aarch64: "abf3",
		ToolDependencyFloors: "golang.org/x/mod@v0.41.0"}
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
		"ACTIONLINT_VERSION":        "v1.7.12", "SHELLCHECK_VERSION": "v0.11.0",
		"SHELLCHECK_SHA256_X86_64": "8c3b", "SHELLCHECK_SHA256_AARCH64": "12b3",
		"OPENCODE_VERSION": "v1.18.32", "OPENCODE_SHA256_X86_64": "1ce4", "OPENCODE_SHA256_AARCH64": "abf3",
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
	for arg := range testBuild().BuildArgs() {
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
	if strings.Contains(dockerfile, "\nCOPY ") && !strings.Contains(dockerfile, "COPY --from=") {
		t.Error("the Dockerfile must build from an empty context: it may COPY only between its own stages")
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
	if !errors.As(err, &missing) || len(missing.Missing) != 14 ||
		!strings.Contains(err.Error(), "GOPLS_VERSION") || !strings.Contains(err.Error(), "SHELLCHECK_SHA256_AARCH64") ||
		!strings.Contains(err.Error(), "OPENCODE_SHA256_AARCH64") || !strings.Contains(err.Error(), "GO_TOOL_DEPENDENCY_FLOORS") {
		t.Fatalf("Validate() = %v, want all fourteen pins named", err)
	}
}

func TestBuild_LabelsRecordToolVersionsNotTarballDigests(t *testing.T) {
	labels := testBuild().Labels()
	if labels[LabelActionlintVersion] != "v1.7.12" || labels[LabelShellcheckVersion] != "v0.11.0" ||
		labels[LabelOpencodeVersion] != "v1.18.32" {
		t.Fatalf("labels = %v, want actionlint, shellcheck and opencode versions recorded", labels)
	}
	for key, value := range labels {
		if key == "" || value == "8c3b" || value == "12b3" || value == "1ce4" || value == "abf3" || value == "golang.org/x/mod@v0.41.0" {
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
	if env[EnvExpectedClaudeCode] != "2.1.280" || env[EnvExpectedCodex] != "0.156.0" || env["HOME"] != "/tmp" {
		t.Fatalf("env = %v", env)
	}
	for _, p := range Probes() {
		if strings.Contains(p.Script, "2.1.280") {
			t.Fatal("expected versions must reach a probe through its environment, not its text")
		}
	}
}
