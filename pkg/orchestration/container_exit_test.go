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
	"io"
	"strings"
	"testing"
)

// =============================================================================
// A container's exit is the evidence of a worker crash — go-quicbaas#1064
// =============================================================================
//
// A container stream that closes is either the worker exiting non-zero (a
// crash, the work's) or its transport being lost (the fleet's). These pin the
// evidence that tells them apart: the exit the runtime actually reported.

func TestContainerExit_WorkerCrashed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		exit ContainerExit
		want bool
	}{
		{"unknown exit is not a crash", ContainerExit{}, false},
		{"unknown exit ignores a stale code", ContainerExit{Code: 1}, false},
		{"clean exit is not a crash", ContainerExit{Known: true, Code: 0}, false},
		{"the runtime's own failure is not the worker's", ContainerExit{Known: true, Code: 125}, false},
		{"a non-zero worker exit is a crash", ContainerExit{Known: true, Code: 1}, true},
		{"a signalled container status is the worker's", ContainerExit{Known: true, Code: 137}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.exit.WorkerCrashed(); got != tc.want {
				t.Errorf("WorkerCrashed(%+v) = %v, want %v", tc.exit, got, tc.want)
			}
			closed := &ContainerStreamClosedError{ContainerID: "c0ffee", Image: "img", Operation: "prompt", Exit: tc.exit}
			if got := closed.WorkerCrashed(); got != tc.want {
				t.Errorf("ContainerStreamClosedError.WorkerCrashed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestContainerStreamClosedError_NamesOnlyAKnownExitCode(t *testing.T) {
	t.Parallel()
	unknown := &ContainerStreamClosedError{Image: "img", Operation: "prompt"}
	if strings.Contains(unknown.Error(), "exit code") {
		t.Errorf("an unknown exit was reported as a code: %q", unknown.Error())
	}
	known := &ContainerStreamClosedError{Image: "img", Operation: "prompt", Exit: ContainerExit{Known: true, Code: 3}}
	if !strings.Contains(known.Error(), "(exit code 3)") {
		t.Errorf("error %q does not name the exit code", known.Error())
	}
}

// runInteractiveToEOF starts the fake runtime and reads its stdout to EOF, as
// the backend does before it asks for the exit.
func runInteractiveToEOF(t *testing.T, body string) *InteractiveProcess {
	t.Helper()
	// execStubRuntime waits out the ETXTBSY window a freshly written script
	// has while this package's parallel phase forks (golang/go#22315); running
	// it directly lost that race in a full-package run.
	rt := execStubRuntime(t, body+"\n")
	proc, err := rt.RunInteractive(t.Context(), &ContainerSpec{Image: "alpine:latest"})
	if err != nil {
		t.Fatalf("RunInteractive: %v", err)
	}
	t.Cleanup(func() { _ = proc.Stdin.Close() })
	if _, err := io.Copy(io.Discard, proc.Stdout); err != nil {
		t.Fatalf("read stdout to EOF: %v", err)
	}
	if proc.Wait == nil {
		t.Fatal("the exec runtime offers no exit: a crash could never be told from transport loss")
	}
	return proc
}

func TestExecContainerRuntime_RunInteractive_WaitReportsTheExitCode(t *testing.T) {
	t.Parallel()
	proc := runInteractiveToEOF(t, "exit 3")
	exit := proc.Wait(t.Context())
	if !exit.Known || exit.Code != 3 {
		t.Fatalf("exit = %+v, want the process's own status 3", exit)
	}
	// Asked twice, it answers the same: the reap happens once.
	if again := proc.Wait(t.Context()); again != exit {
		t.Errorf("second Wait = %+v, want %+v", again, exit)
	}
}

func TestExecContainerRuntime_RunInteractive_WaitOnASignalledProcessIsUnknown(t *testing.T) {
	t.Parallel()
	proc := runInteractiveToEOF(t, "kill -9 $$")
	if exit := proc.Wait(t.Context()); exit.Known {
		t.Fatalf("exit = %+v, want unknown — a killed runtime process chose no status", exit)
	}
}

func TestExecContainerRuntime_RunInteractive_WaitIsBoundedByItsContext(t *testing.T) {
	t.Parallel()
	// Stdout closes but the process lingers, as a wedged runtime would.
	proc := runInteractiveToEOF(t, "exec >&-; read _")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if exit := proc.Wait(ctx); exit.Known {
		t.Fatalf("exit = %+v, want unknown when the bound has run out", exit)
	}
}

// TestAgentGoneError_ContainerCarriesItsExit: a containerized session whose
// stream closed reports the exit its runtime gave, and reads it BEFORE the
// teardown — after procCancel the only status left would be the kill's.
func TestAgentGoneError_ContainerCarriesItsExit(t *testing.T) {
	t.Parallel()
	cancelled := false
	sess := &acpSession{
		containerID:    "ctr-1",
		containerImage: "img",
		procCancel:     func() { cancelled = true },
		containerExit: func(context.Context) ContainerExit {
			if cancelled {
				t.Error("the exit was read after the container was torn down")
			}
			return ContainerExit{Known: true, Code: 2}
		},
	}
	err := (&ACPBackend{}).agentGoneError(sess)
	var closed *ContainerStreamClosedError
	if !errors.As(err, &closed) {
		t.Fatalf("err = %T(%v), want *ContainerStreamClosedError", err, err)
	}
	if !closed.WorkerCrashed() || closed.Exit.Code != 2 {
		t.Errorf("exit = %+v, want the worker's own status 2", closed.Exit)
	}
	if !cancelled {
		t.Error("the container was not torn down")
	}
}

// TestAgentGoneError_ContainerWithoutAnExitIsTransportLoss: a runtime that
// cannot say how the container ended leaves the exit unknown, which is never a
// crash.
func TestAgentGoneError_ContainerWithoutAnExitIsTransportLoss(t *testing.T) {
	t.Parallel()
	sess := &acpSession{containerID: "ctr-1", containerImage: "img"}
	err := (&ACPBackend{}).agentGoneError(sess)
	var closed *ContainerStreamClosedError
	if !errors.As(err, &closed) {
		t.Fatalf("err = %T(%v), want *ContainerStreamClosedError", err, err)
	}
	if closed.Exit.Known || closed.WorkerCrashed() {
		t.Errorf("exit = %+v, want unknown", closed.Exit)
	}
}
