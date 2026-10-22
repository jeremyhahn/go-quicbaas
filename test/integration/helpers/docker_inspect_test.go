//go:build integration

package helpers

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

// killedProcessError stands in for the error exec returns when a command's
// context ends and the process is killed.
type killedProcessError struct{}

func (killedProcessError) Error() string { return "signal: killed" }

// exitStatusError stands in for docker inspect exiting non-zero.
type exitStatusError struct{}

func (exitStatusError) Error() string { return "exit status 1" }

// blockingRunner never answers until its context ends, like a docker inspect
// hung on an unresponsive daemon.
func blockingRunner(ctx context.Context, _ string, _ ...string) ([]byte, []byte, error) {
	<-ctx.Done()
	return nil, nil, killedProcessError{}
}

func failingRunner(stderr string) commandRunner {
	return func(context.Context, string, ...string) ([]byte, []byte, error) {
		return nil, []byte(stderr), exitStatusError{}
	}
}

func statusRunner(status string) commandRunner {
	return func(context.Context, string, ...string) ([]byte, []byte, error) {
		return []byte(status + "\n"), nil, nil
	}
}

// An inspect cancelled by the polling deadline says nothing about the
// container, so the wait must fail rather than report it stopped.
func TestWaitForContainerStopped_InspectCancelledByDeadlineFails(t *testing.T) {
	dm := &DockerManager{projectName: "p", run: blockingRunner}

	err := dm.waitForContainerStopped(context.Background(), t, "node", 50*time.Millisecond)

	if err == nil {
		t.Fatal("waitForContainerStopped succeeded although every inspect was cancelled")
	}
	var timeoutErr *PollTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("error = %v, want a *PollTimeoutError", err)
	}
	var inspectErr *ContainerInspectError
	if !errors.As(err, &inspectErr) {
		t.Fatalf("error = %v, want it to carry the *ContainerInspectError", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want it to carry context.DeadlineExceeded", err)
	}
}

func TestWaitForContainerStopped_NoSuchContainerIsStopped(t *testing.T) {
	for _, stderr := range []string{
		"Error: No such container: p-node-1",
		"Error: No such object: p-node-1",
	} {
		dm := &DockerManager{projectName: "p", run: failingRunner(stderr)}
		if err := dm.waitForContainerStopped(context.Background(), t, "node", time.Second); err != nil {
			t.Fatalf("stderr %q: waitForContainerStopped = %v, want nil", stderr, err)
		}
	}
}

func TestWaitForContainerStopped_OtherInspectFailureFails(t *testing.T) {
	dm := &DockerManager{
		projectName: "p",
		run:         failingRunner("Cannot connect to the Docker daemon at unix:///var/run/docker.sock"),
	}

	err := dm.waitForContainerStopped(context.Background(), t, "node", 50*time.Millisecond)

	var inspectErr *ContainerInspectError
	if !errors.As(err, &inspectErr) {
		t.Fatalf("error = %v, want a *ContainerInspectError", err)
	}
}

func TestWaitForContainerStopped_ExitedStatusIsStopped(t *testing.T) {
	dm := &DockerManager{projectName: "p", run: statusRunner("exited")}
	if err := dm.waitForContainerStopped(context.Background(), t, "node", time.Second); err != nil {
		t.Fatalf("waitForContainerStopped = %v, want nil", err)
	}
}

func TestExecCommand_ReturnsStderrAndExitError(t *testing.T) {
	_, stderr, err := execCommand(context.Background(), "sh", "-c", "echo boom >&2; exit 3")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %v, want an *exec.ExitError", err)
	}
	if string(stderr) != "boom\n" {
		t.Fatalf("stderr = %q, want %q", stderr, "boom\n")
	}
}
