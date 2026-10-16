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

package workerimage

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func probeBuild() softwarefactoryworker.Build {
	return softwarefactoryworker.Build{Base: "0.11.0-alpha",
		Agents: softwarefactoryworker.Agents{ClaudeCode: "2.1.280", Codex: "0.156.0", Gemini: testGemini},
		Pins:   testPins(), UID: os.Getuid(), GID: os.Getgid()}
}

func TestRunProbes_RunsEveryProbeAsTheSpawnUIDWithALoginShell(t *testing.T) {
	engine := newFakeEngine()
	results, err := RunProbes(t.Context(), engine, "sha256:abc", probeBuild())
	if err != nil {
		t.Fatalf("RunProbes: %v", err)
	}
	if len(results) != len(softwarefactoryworker.Probes()) || len(failedNames(results)) != 0 {
		t.Fatalf("results = %+v", results)
	}
	wantUser := strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
	for _, run := range engine.runs {
		if run.User != wantUser || run.Image != "sha256:abc" || run.Command[0] != "sh" || run.Command[1] != "-lec" {
			t.Fatalf("run = %+v", run)
		}
		if run.Env[softwarefactoryworker.EnvExpectedClaudeCode] != "2.1.280" {
			t.Fatalf("the probe environment must carry the expected versions: %v", run.Env)
		}
	}
}

// TestRunProbes_SandboxedProbesRunUnderTheProductionPolicy pins that a probe
// asking for the production boundary gets the default sandbox policy (only its
// pids bound lowered when the probe says so) and its stdin, and that every
// other probe runs with neither.
func TestRunProbes_SandboxedProbesRunUnderTheProductionPolicy(t *testing.T) {
	engine := newFakeEngine()
	if _, err := RunProbes(t.Context(), engine, "sha256:abc", probeBuild()); err != nil {
		t.Fatalf("RunProbes: %v", err)
	}
	probes := softwarefactoryworker.Probes()
	if len(engine.runs) != len(probes) {
		t.Fatalf("ran %d probes, want %d", len(engine.runs), len(probes))
	}
	sandboxed := 0
	for i, probe := range probes {
		run := engine.runs[i]
		if !probe.Sandboxed {
			if run.Security != nil || run.Stdin != nil {
				t.Fatalf("probe %q is not sandboxed but ran with %+v / stdin %q", probe.Name, run.Security, run.Stdin)
			}
			if _, set := run.Env[softwarefactoryworker.EnvExpectedMemlockKiB]; set {
				t.Fatalf("probe %q is not sandboxed but was told to expect a memlock limit", probe.Name)
			}
			continue
		}
		sandboxed++
		if run.Security == nil || len(run.Security.CapAdd) != 0 || run.Security.PidsLimit != probe.PidsLimit {
			t.Fatalf("probe %q must run under the default policy with pids limit %d, got %+v", probe.Name, probe.PidsLimit, run.Security)
		}
		if string(run.Stdin) != string(probe.Stdin) {
			t.Fatalf("probe %q stdin was not fed through", probe.Name)
		}
		// The memlock expectation is the policy's own limit, in the KiB
		// `ulimit -l` reports: 4 GiB, what the CI runners give (#1038).
		if got := run.Env[softwarefactoryworker.EnvExpectedMemlockKiB]; got != "4194304" {
			t.Fatalf("probe %q must expect the policy's memlock of 4194304 KiB, got %q", probe.Name, got)
		}
	}
	if sandboxed == 0 {
		t.Fatal("at least one probe must prove the production boundary")
	}
}

func TestRunProbes_AFailingProbeIsAResultNotAnError(t *testing.T) {
	engine := newFakeEngine()
	engine.failProbe = softwarefactoryworker.Probes()[0].Name
	results, err := RunProbes(t.Context(), engine, "sha256:abc", probeBuild())
	if err != nil {
		t.Fatalf("RunProbes: %v", err)
	}
	if failed := failedNames(results); len(failed) != 1 || failed[0] != engine.failProbe {
		t.Fatalf("failed = %v", failed)
	}
	if len(results) != len(softwarefactoryworker.Probes()) {
		t.Fatal("a failing probe must not stop the rest from running")
	}
}

// ownerlessEngine runs every probe successfully but never writes the
// workspace file the ownership probe must leave.
type ownerlessEngine struct{}

func (ownerlessEngine) RunImage(context.Context, *orchestration.ImageRunSpec) (*orchestration.ExecResult, error) {
	return &orchestration.ExecResult{}, nil
}

func TestRunProbes_AMissingWorkspaceWriteIsReported(t *testing.T) {
	_, err := RunProbes(t.Context(), ownerlessEngine{}, "sha256:abc", probeBuild())
	var wsErr *ProbeWorkspaceError
	if !errors.As(err, &wsErr) {
		t.Fatalf("err = %v, want ProbeWorkspaceError", err)
	}
}

func TestCheckWorkspaceOwner_FailsAForeignOwner(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/"+softwarefactoryworker.ProbeWorkspaceFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := checkWorkspaceOwner(CheckResult{Name: "p", Passed: true}, dir, os.Getuid()+1)
	if err != nil {
		t.Fatalf("checkWorkspaceOwner: %v", err)
	}
	if result.Passed || !strings.Contains(result.Detail, "owned by uid") {
		t.Fatalf("result = %+v", result)
	}
}

// failingRunner cannot run anything.
type failingRunner struct{}

func (failingRunner) RunImage(context.Context, *orchestration.ImageRunSpec) (*orchestration.ExecResult, error) {
	return nil, errFake
}

func TestRunProbes_Errors(t *testing.T) {
	if _, err := RunProbes(t.Context(), failingRunner{}, "sha256:abc", probeBuild()); !errors.Is(err, errFake) {
		t.Fatalf("err = %v", err)
	}
	if _, err := RunProbes(t.Context(), newFakeEngine(), " ", probeBuild()); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestTail_KeepsTheEndOnARuneBoundary(t *testing.T) {
	long := strings.Repeat("é", maxDetail)
	got := tail(long)
	if !strings.HasPrefix(got, "…") || len(got) > maxDetail+len("…") {
		t.Fatalf("len(tail) = %d", len(got))
	}
	if strings.ContainsRune(got, '�') {
		t.Fatal("tail split a rune")
	}
	if tail("  short  ") != "short" {
		t.Fatal("short output is kept whole, trimmed")
	}
}

// TestSandboxedProbeEnv_FollowsThePolicyAndLeavesTheSharedEnvAlone: the
// expectation tracks the policy it is derived from — an operator's memlock
// would be expected, not the default — and deriving it never writes into the
// environment every other probe shares.
func TestSandboxedProbeEnv_FollowsThePolicyAndLeavesTheSharedEnvAlone(t *testing.T) {
	shared := map[string]string{"HOME": "/tmp"}
	policy := orchestration.SandboxSecurity{Ulimits: []orchestration.ContainerUlimit{
		{Name: orchestration.UlimitMemlock, Soft: 1 << 30, Hard: 2 << 30},
	}}
	got := sandboxedProbeEnv(shared, policy)
	if got[softwarefactoryworker.EnvExpectedMemlockKiB] != "1048576" || got["HOME"] != "/tmp" {
		t.Fatalf("sandboxedProbeEnv = %v, want the policy's 1048576 KiB soft limit and the shared env", got)
	}
	if _, leaked := shared[softwarefactoryworker.EnvExpectedMemlockKiB]; leaked {
		t.Fatal("sandboxedProbeEnv wrote into the shared environment")
	}
}
