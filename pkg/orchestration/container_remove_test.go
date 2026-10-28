package orchestration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// failingRuntime returns a runtime whose every invocation prints stderr and
// exits 1, the way docker answers an `rm` it refuses.
func failingRuntime(t *testing.T, stderr string) *ExecContainerRuntime {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-runtime")
	script := "#!/bin/sh\nprintf '%s\\n' '" + stderr + "' >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake runtime: %v", err)
	}
	// WriteFile's mode is masked by the umask; exec needs the execute bit.
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod fake runtime: %v", err)
	}
	return &ExecContainerRuntime{command: path, logger: newContainerTestLogger(t)}
}

// An interactive container runs with --rm, so closing its stdin can start the
// daemon's own removal before Close asks for one. Docker then refuses `rm -f`
// with "removal ... is already in progress" — the removal asked for, already
// underway — and Close reported it as a failed stop (go-quicbaas#2038).
func TestRemove_RemovalAlreadyInProgressIsSuccess(t *testing.T) {
	rt := failingRuntime(t, "Error response from daemon: removal of container quicbaas-acp-0123456789abcdef is already in progress")
	if err := rt.Remove(context.Background(), "quicbaas-acp-0123456789abcdef"); err != nil {
		t.Fatalf("Remove() = %v, want nil: the container is already being removed", err)
	}
}

// Any other refusal is still a failed removal.
func TestRemove_OtherRefusalIsStopError(t *testing.T) {
	rt := failingRuntime(t, "Error response from daemon: permission denied")
	err := rt.Remove(context.Background(), "quicbaas-acp-0123456789abcdef")
	var stopErr *ContainerStopError
	if !errors.As(err, &stopErr) {
		t.Fatalf("Remove() = %v, want *ContainerStopError", err)
	}
}
