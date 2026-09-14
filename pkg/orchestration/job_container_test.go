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

package orchestration

import (
	"context"
	"errors"
	"testing"
)

// =============================================================================
// JobContainer
// =============================================================================

// TestNewJobContainer_RefusesAnIncompleteContainer: every argument names
// something a command needs to run at all, so a missing one is an input
// error rather than a container that fails later.
func TestNewJobContainer_RefusesAnIncompleteContainer(t *testing.T) {
	t.Parallel()

	run := func(_ context.Context, _ *ExecSpec) (*ExecResult, error) { return &ExecResult{}, nil }
	cases := map[string]struct {
		sessionID string
		workDir   string
		exec      ContainerExecFunc
	}{
		"no session":    {"", "/workspace/api", run},
		"blank session": {"   ", "/workspace/api", run},
		"no work dir":   {"session-1", "", run},
		"no exec":       {"session-1", "/workspace/api", nil},
	}
	for name, tc := range cases {
		container, err := NewJobContainer(tc.sessionID, tc.workDir, tc.exec)
		var invalid *InputValidationError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want an *InputValidationError", name, err)
		}
		if container != nil {
			t.Errorf("%s: a refused container was returned", name)
		}
	}
}

// TestJobContainer_ExecPinsTheContainersCheckout: the working directory is
// not the caller's to choose. It is the checkout the container was bound to,
// which is what keeps a caller from naming the daemon's copy of it.
func TestJobContainer_ExecPinsTheContainersCheckout(t *testing.T) {
	t.Parallel()

	var seen []*ExecSpec
	container, err := NewJobContainer("session-1", "/workspace/api",
		func(_ context.Context, spec *ExecSpec) (*ExecResult, error) {
			seen = append(seen, spec)
			return &ExecResult{ExitCode: 3, Stdout: "out", Stderr: "err"}, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	result, err := container.Exec(context.Background(), []string{"sh", "-c", "make build"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	// A non-zero exit is the command's answer, reported exactly as the
	// runtime reports it.
	if result.ExitCode != 3 || result.Stdout != "out" || result.Stderr != "err" {
		t.Errorf("result = %+v, want the runtime's own answer", result)
	}
	if len(seen) != 1 {
		t.Fatalf("execs = %d, want one", len(seen))
	}
	if seen[0].WorkDir != "/workspace/api" {
		t.Errorf("work dir = %q, want the container's checkout", seen[0].WorkDir)
	}
	if len(seen[0].Cmd) != 3 || seen[0].Cmd[2] != "make build" {
		t.Errorf("cmd = %v, want the caller's argument vector", seen[0].Cmd)
	}
}

// TestJobContainer_ExecOnNoContainerIsRefused: a nil container is what a job
// that spawned none looks like, and running the command somewhere else is
// exactly what must not happen.
func TestJobContainer_ExecOnNoContainerIsRefused(t *testing.T) {
	t.Parallel()

	for name, container := range map[string]*JobContainer{
		"nil":        nil,
		"zero value": {SessionID: "session-1", WorkDir: "/workspace/api"},
	} {
		result, err := container.Exec(context.Background(), []string{"true"})
		var invalid *InputValidationError
		if !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want an *InputValidationError", name, err)
		}
		if result != nil {
			t.Errorf("%s: a refused exec returned a result", name)
		}
	}
}

// =============================================================================
// JobRun.SetContainer
// =============================================================================

// TestJobRun_SetContainer_RoutesThroughTheSessionsExecSeam: what a runner
// records reaches the engine's exec seam, in the container the session names
// and the directory the container calls its checkout.
func TestJobRun_SetContainer_RoutesThroughTheSessionsExecSeam(t *testing.T) {
	t.Parallel()

	want := &ExecResult{Stdout: "verified"}
	rig := newExecSessionRig(t, &mockContainerRuntime{available: true, execResult: want})
	run := NewJobRun(JobRunConfig{
		JobID:  "job-1",
		Config: &JobConfig{TenantID: execSessionTenant},
		Agents: rig.service,
		Logger: &agentTestLogger{},
	})

	if err := run.SetContainer(liveContainerSession, "/workspace/api"); err != nil {
		t.Fatalf("SetContainer: %v", err)
	}
	container := run.Container()
	if container == nil || container.SessionID != liveContainerSession || container.WorkDir != "/workspace/api" {
		t.Fatalf("container = %+v, want the session and its checkout", container)
	}
	// The validators and hooks that read the job see the same container.
	if run.Context().Container != container {
		t.Fatal("the job context does not carry the container")
	}

	got, err := container.Exec(rig.ctx, []string{"sh", "-c", "make build"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if got != want {
		t.Errorf("result = %+v, want %+v", got, want)
	}
	calls := rig.execCalls(t)
	if len(calls) != 1 || calls[0].ContainerID != liveContainerID {
		t.Fatalf("runtime saw %+v, want one execution in %s", calls, liveContainerID)
	}
	if calls[0].Spec.WorkDir != "/workspace/api" {
		t.Errorf("work dir = %q, want the container's checkout", calls[0].Spec.WorkDir)
	}
}

// TestJobRun_SetContainer_RefusesAnIncompleteRecord: the run refuses what
// NewJobContainer refuses, and keeps whatever it had.
func TestJobRun_SetContainer_RefusesAnIncompleteRecord(t *testing.T) {
	t.Parallel()

	run := NewJobRun(JobRunConfig{
		JobID:  "job-1",
		Config: &JobConfig{TenantID: execSessionTenant},
		Agents: &controlLoopMockService{},
		Logger: &agentTestLogger{},
	})
	for name, tc := range map[string][2]string{
		"no session":  {"", "/workspace/api"},
		"no work dir": {"session-1", ""},
	} {
		var invalid *InputValidationError
		if err := run.SetContainer(tc[0], tc[1]); !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want an *InputValidationError", name, err)
		}
	}
	if run.Container() != nil {
		t.Error("a refused record was still installed")
	}
	if run.Context().Container != nil {
		t.Error("the job context carries a container the run refused")
	}
}

// TestJobRun_SetContainer_RefusesAServiceThatCannotExec: a job whose agent
// service owns no container runtime has nowhere to run a command, and says
// so rather than running it on the daemon.
func TestJobRun_SetContainer_RefusesAServiceThatCannotExec(t *testing.T) {
	t.Parallel()

	run := NewJobRun(JobRunConfig{
		JobID:  "job-1",
		Config: &JobConfig{TenantID: execSessionTenant},
		Agents: &controlLoopMockService{},
		Logger: &agentTestLogger{},
	})
	if err := run.SetContainer(liveContainerSession, "/workspace/api"); err != nil {
		t.Fatalf("SetContainer: %v", err)
	}
	result, err := run.Container().Exec(context.Background(), []string{"true"})
	var unsupported *SessionExecUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %T (%v), want *SessionExecUnsupportedError", err, err)
	}
	if result != nil {
		t.Error("a refused exec returned a result")
	}
}
