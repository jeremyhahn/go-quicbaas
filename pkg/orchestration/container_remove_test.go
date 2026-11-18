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
	"strings"
	"testing"
	"time"
)

const removeTestID = "quicbaas-acp-607f30739433edd7"

// TestExecContainerRuntime_Remove_AlreadyRemovedIsDone pins that a container
// the runtime no longer knows is reported as not found, and that a session
// teardown reads that as success.
func TestExecContainerRuntime_Remove_AlreadyRemovedIsDone(t *testing.T) {
	engine := &fakeImageEngine{answer: func([]string) (imageCommandResult, error) {
		return imageCommandResult{ExitCode: 1, Stderr: "Error response from daemon: No such container: " + removeTestID}, nil
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)

	err := rt.Remove(t.Context(), removeTestID)
	var notFound *ContainerNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Remove = %v, want ContainerNotFoundError", err)
	}
	if got := containerRemovalOutcome(removeTestID, err); got != nil {
		t.Fatalf("teardown outcome = %v, want success for a container already gone", got)
	}
	if len(engine.calls) != 1 || strings.Join(engine.calls[0].Args, " ") != "rm -f "+removeTestID {
		t.Fatalf("calls = %+v, want one rm -f", engine.calls)
	}
}

// TestExecContainerRuntime_Remove_InProgressRemovalWaitsThenSucceeds pins the
// go-quicbaas#1801 race: `rm -f` on a `--rm` container loses to the daemon's
// own removal, and Remove waits for the container to be gone instead of
// failing.
func TestExecContainerRuntime_Remove_InProgressRemovalWaitsThenSucceeds(t *testing.T) {
	inspects := 0
	engine := &fakeImageEngine{answer: func(args []string) (imageCommandResult, error) {
		switch args[0] {
		case "rm":
			return imageCommandResult{ExitCode: 1, Stderr: "Error response from daemon: removal of container " + removeTestID + " is already in progress"}, nil
		case "container":
			inspects++
			if inspects < 2 {
				return imageCommandResult{Stdout: "abc\n"}, nil
			}
			return imageCommandResult{ExitCode: 1, Stderr: "Error: No such container: " + removeTestID}, nil
		}
		t.Fatalf("unexpected engine call %v", args)
		return imageCommandResult{}, nil
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)

	if err := rt.Remove(t.Context(), removeTestID); err != nil {
		t.Fatalf("Remove = %v, want success once the in-progress removal finished", err)
	}
	if inspects != 2 {
		t.Fatalf("inspects = %d, want Remove to poll until the container was gone", inspects)
	}
}

// TestExecContainerRuntime_Remove_InProgressRemovalThatNeverEndsFails pins
// that the wait is bounded: a container still there is a typed failure.
func TestExecContainerRuntime_Remove_InProgressRemovalThatNeverEndsFails(t *testing.T) {
	engine := &fakeImageEngine{answer: func(args []string) (imageCommandResult, error) {
		if args[0] == "rm" {
			return imageCommandResult{ExitCode: 1, Stderr: "Error response from daemon: removal of container " + removeTestID + " is already in progress"}, nil
		}
		return imageCommandResult{Stdout: "abc\n"}, nil
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)

	ctx, cancel := context.WithTimeout(t.Context(), 600*time.Millisecond)
	defer cancel()
	err := rt.Remove(ctx, removeTestID)
	var stopErr *ContainerStopError
	if !errors.As(err, &stopErr) {
		t.Fatalf("Remove = %v, want ContainerStopError", err)
	}
}

// TestExecContainerRuntime_Remove_RealFailureIsTypedAndWrappedOnce pins that
// a refusal stays a ContainerStopError, and that the session teardown does
// not wrap it a second time ("stop failed: X: stop failed: X").
func TestExecContainerRuntime_Remove_RealFailureIsTypedAndWrappedOnce(t *testing.T) {
	engine := &fakeImageEngine{answer: func([]string) (imageCommandResult, error) {
		return imageCommandResult{ExitCode: 1, Stderr: "Error response from daemon: permission denied"}, nil
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)

	err := containerRemovalOutcome(removeTestID, rt.Remove(t.Context(), removeTestID))
	if !errors.Is(err, ErrContainerStop) {
		t.Fatalf("outcome = %v, want ErrContainerStop", err)
	}
	var cmdErr *CommandError
	if !errors.As(err, &cmdErr) || cmdErr.Op != "rm" {
		t.Fatalf("outcome = %v, want the rm CommandError as its cause", err)
	}
	if n := strings.Count(err.Error(), ErrContainerStop.Error()); n != 1 {
		t.Fatalf("Error() = %q names %q %d times, want once", err.Error(), ErrContainerStop.Error(), n)
	}
	if strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Error() = %q renders the runtime's output", err.Error())
	}
}

// TestContainerRemovalOutcome_WrapsAForeignErrorOnce pins that an error that
// is not already a ContainerStopError is wrapped, once.
func TestContainerRemovalOutcome_WrapsAForeignErrorOnce(t *testing.T) {
	cause := errors.New("transport closed")
	err := containerRemovalOutcome(removeTestID, cause)
	if !errors.Is(err, ErrContainerStop) || !errors.Is(err, cause) {
		t.Fatalf("outcome = %v, want ErrContainerStop wrapping the cause", err)
	}
}

// TestExecContainerRuntime_Stop_RunsStopThroughTheRunner pins Stop's argv and
// its not-found answer.
func TestExecContainerRuntime_Stop_RunsStopThroughTheRunner(t *testing.T) {
	engine := &fakeImageEngine{answer: func([]string) (imageCommandResult, error) {
		return imageCommandResult{ExitCode: 1, Stderr: "Error response from daemon: No such container: " + removeTestID}, nil
	}}
	rt := newImageTestRuntime(t, RuntimeDocker, engine)

	err := rt.Stop(t.Context(), removeTestID)
	var notFound *ContainerNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("Stop = %v, want ContainerNotFoundError", err)
	}
	if len(engine.calls) != 1 || strings.Join(engine.calls[0].Args, " ") != "stop "+removeTestID {
		t.Fatalf("calls = %+v, want one stop", engine.calls)
	}
}
