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

package fleet

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// A CLI-mode backend under `--sandbox-policy required` (#741 / #742)
// =============================================================================
//
// The daemon builds its `claude` backend with CLIMode: true
// (cmd/quicbaas/fleet_serve.go) and the sdlc worker spawns Persistent +
// Required. On the burn-in that combination produced three `claude` processes
// as DIRECT CHILDREN of the daemon and not one container, so the runner refused
// every worker for "runs no container".
//
// This test is that exact wiring, one layer above the backend: a real
// ACPBackend in CLI mode, inside a real agent Runtime whose policy is Required,
// with the container runtime the daemon would have. What it asserts is the fact
// the runner needs — the session comes back with a container id — and the fact
// the operator was promised — nothing ran on the host.

// streamJSONContainer is a ContainerRuntime whose interactive container plays
// enough of Claude Code's stream-json protocol to complete a spawn: it emits
// the init record and answers each user message with a result.
type streamJSONContainer struct {
	containerID string

	mu    sync.Mutex
	specs []*orchestration.ContainerSpec
}

func (c *streamJSONContainer) RunInteractive(
	ctx context.Context,
	spec *orchestration.ContainerSpec,
) (*orchestration.InteractiveProcess, error) {
	c.mu.Lock()
	c.specs = append(c.specs, spec)
	c.mu.Unlock()

	agentIn, backendOut, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	backendIn, agentOut, err := os.Pipe()
	if err != nil {
		_ = agentIn.Close()
		_ = backendOut.Close()
		return nil, err
	}

	go func() {
		defer func() { _ = agentOut.Close() }()
		write := func(v any) bool {
			payload, marshalErr := json.Marshal(v)
			if marshalErr != nil {
				return false
			}
			_, writeErr := agentOut.Write(append(payload, '\n'))
			return writeErr == nil
		}
		if !write(map[string]string{
			"type": "system", "subtype": "init",
			"session_id": "container-cli-session", "model": "claude-opus-5",
		}) {
			return
		}
		scanner := bufio.NewScanner(agentIn)
		for scanner.Scan() {
			if !write(map[string]string{
				"type": "result", "subtype": "success",
				"session_id": "container-cli-session", "result": "OK",
			}) {
				return
			}
		}
	}()

	go func() {
		<-ctx.Done()
		_ = agentIn.Close()
		_ = agentOut.Close()
	}()

	return &orchestration.InteractiveProcess{
		ContainerID: c.containerID,
		Stdin:       backendOut,
		Stdout:      backendIn,
	}, nil
}

func (c *streamJSONContainer) launchedSpecs() []*orchestration.ContainerSpec {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*orchestration.ContainerSpec, len(c.specs))
	copy(out, c.specs)
	return out
}

func (c *streamJSONContainer) Run(context.Context, *orchestration.ContainerSpec) (string, error) {
	return c.containerID, nil
}
func (c *streamJSONContainer) Stop(context.Context, string) error   { return nil }
func (c *streamJSONContainer) Remove(context.Context, string) error { return nil }
func (c *streamJSONContainer) Exec(
	context.Context, string, *orchestration.ExecSpec,
) (*orchestration.ExecResult, error) {
	return nil, &orchestration.ContainerExecError{ContainerID: c.containerID, Cause: errNoExecInThisTest}
}
func (c *streamJSONContainer) Logs(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (c *streamJSONContainer) IsRunning(context.Context, string) (bool, error) { return true, nil }
func (c *streamJSONContainer) IsAvailable() bool                               { return true }
func (c *streamJSONContainer) Pull(context.Context, string) error              { return nil }

// newRequiredCLIRuntime builds the daemon's own wiring: a CLI-mode claude
// backend, a required sandbox policy, an image map and a mount policy that
// allows the workspace.
func newRequiredCLIRuntime(t *testing.T, rt *streamJSONContainer) (*Runtime, string) {
	t.Helper()

	root := t.TempDir()
	workDir := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workDir, 0o700); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	backend := orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command:      "claude",
		Args:         []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"},
		DefaultModel: "claude-opus-5",
		CLIMode:      true,
	})

	runtime, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"claude": backend},
		SandboxPolicy:    orchestration.SandboxPolicyRequired,
		ContainerRuntime: rt,
		ImageMap:         map[string]string{"claude": "quicbaas-software-factory-worker:0.3.0-alpha"},
		Logger:           &testLogger{t: t},
		MountPolicy:      &orchestration.MountPolicy{AllowedRoots: []string{root}},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return runtime, workDir
}

// TestRuntime_RequiredSpawnOfACLIBackend_YieldsAContainerID is the regression
// test for the burn-in failure. Before the fix this returned a session with an
// empty ContainerID and a live `claude` process on the host.
func TestRuntime_RequiredSpawnOfACLIBackend_YieldsAContainerID(t *testing.T) {
	t.Parallel()

	fake := &streamJSONContainer{containerID: "quicbaas-agent-741"}
	runtime, workDir := newRequiredCLIRuntime(t, fake)

	sess, err := runtime.Spawn(t.Context(), "tenant-1", nil, "claude", &orchestration.SpawnRequest{
		ProjectID:  "proj-1",
		WorkDir:    workDir,
		Persistent: true,
	})
	if err != nil {
		t.Fatalf("required spawn of a CLI-mode backend failed: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background(), "claude", sess.ID) })

	if sess.ContainerID != fake.containerID {
		t.Fatalf("Session.ContainerID = %q, want %q — the sdlc runner refuses a worker that runs no container",
			sess.ContainerID, fake.containerID)
	}
	if sess.PID != 0 {
		t.Errorf("Session.PID = %d: the agent ran on the daemon host under sandbox mode required", sess.PID)
	}

	specs := fake.launchedSpecs()
	if len(specs) != 1 {
		t.Fatalf("containers launched = %d, want 1", len(specs))
	}
	// The daemon's image map, not the caller, decides the image.
	if specs[0].Image != "quicbaas-software-factory-worker:0.3.0-alpha" {
		t.Errorf("launched image = %q, want the daemon's mapped worker image", specs[0].Image)
	}
	if len(specs[0].Command) == 0 || specs[0].Command[0] != "claude" {
		t.Errorf("container command = %v, want the agent CLI as the container's command", specs[0].Command)
	}
}

// TestRuntime_RequiredSpawnOfACLIBackend_WithoutRuntimeIsRefusedAtConstruction
// is the fail-closed half, and #732 moved WHERE it fails closed. A daemon whose
// policy demands a sandbox and which holds no container runtime can satisfy no
// spawn at all — every request resolves to SandboxModeRequired — so NewRuntime
// now refuses at CONSTRUCTION rather than letting the first dispatched task
// carry the error (ErrRuntimeSandboxRequiredNoRuntime). For a CLI-mode backend
// that is the guard for the no-runtime case: the runtime that would host-exec
// the agent is never built.
func TestRuntime_RequiredSpawnOfACLIBackend_WithoutRuntimeIsRefusedAtConstruction(t *testing.T) {
	t.Parallel()

	backend := orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command:      "claude",
		Args:         []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json"},
		DefaultModel: "claude-opus-5",
		CLIMode:      true,
	})
	_, err := NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"claude": backend},
		SandboxPolicy: orchestration.SandboxPolicyRequired,
		Logger:        &testLogger{t: t},
		MountPolicy:   &orchestration.MountPolicy{AllowedRoots: []string{t.TempDir()}},
	})
	if !errors.Is(err, ErrRuntimeSandboxRequiredNoRuntime) {
		t.Fatalf("NewRuntime error = %v, want ErrRuntimeSandboxRequiredNoRuntime — "+
			"a required-policy daemon with no runtime must refuse at construction, "+
			"never build a runtime that would host-exec a CLI-mode agent", err)
	}
}
