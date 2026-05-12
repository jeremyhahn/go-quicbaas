// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fakeContainerRuntime is a local test double for orchestration.ContainerRuntime.
// It records Pull invocations and can be seeded to return errors or simulate
// missing-runtime states.
type fakeContainerRuntime struct {
	mu        sync.Mutex
	pulled    []string
	pullErr   error
	available bool
}

func (f *fakeContainerRuntime) Run(_ context.Context, _ *orchestration.ContainerSpec) (string, error) {
	return "", nil
}

func (f *fakeContainerRuntime) RunInteractive(_ context.Context, _ *orchestration.ContainerSpec) (*orchestration.InteractiveProcess, error) {
	return nil, nil
}

func (f *fakeContainerRuntime) Stop(_ context.Context, _ string) error   { return nil }
func (f *fakeContainerRuntime) Remove(_ context.Context, _ string) error { return nil }
func (f *fakeContainerRuntime) Logs(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(nil)), nil
}
func (f *fakeContainerRuntime) IsRunning(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (f *fakeContainerRuntime) IsAvailable() bool { return f.available }

func (f *fakeContainerRuntime) Pull(_ context.Context, image string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pulled = append(f.pulled, image)
	return f.pullErr
}

func (f *fakeContainerRuntime) pulledSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.pulled))
	copy(out, f.pulled)
	sort.Strings(out)
	return out
}

// TestRuntime_ImageFor_ResolutionTable covers every interesting combination
// of registry-qualified input vs. configured registry prefix.
func TestRuntime_ImageFor_ResolutionTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		registry string
		mapping  map[string]string
		backend  string
		want     string
		found    bool
	}{
		{
			name:     "unqualified image with registry prefix",
			registry: "ghcr.io/example",
			mapping:  map[string]string{"acp": "claude-code:v1"},
			backend:  "acp",
			want:     "ghcr.io/example/claude-code:v1",
			found:    true,
		},
		{
			name:     "already qualified image left untouched",
			registry: "ghcr.io/example",
			mapping:  map[string]string{"acp": "registry.internal/claude-code:v1"},
			backend:  "acp",
			want:     "registry.internal/claude-code:v1",
			found:    true,
		},
		{
			name:     "no registry passes image through",
			registry: "",
			mapping:  map[string]string{"acp": "claude-code:v1"},
			backend:  "acp",
			want:     "claude-code:v1",
			found:    true,
		},
		{
			name:     "missing backend returns not-found",
			registry: "ghcr.io/example",
			mapping:  map[string]string{"acp": "claude-code:v1"},
			backend:  "ollama",
			want:     "",
			found:    false,
		},
		{
			name:     "trailing slash on registry is normalized",
			registry: "ghcr.io/example/",
			mapping:  map[string]string{"acp": "claude-code:v1"},
			backend:  "acp",
			want:     "ghcr.io/example/claude-code:v1",
			found:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt, err := NewRuntime(RuntimeConfig{
				Backends:      map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
				ImageMap:      tc.mapping,
				ImageRegistry: tc.registry,
				Logger:        &testLogger{t: t},
			})
			if err != nil {
				t.Fatalf("NewRuntime: %v", err)
			}
			got, ok := rt.ImageFor(tc.backend)
			if ok != tc.found {
				t.Fatalf("found: got %v want %v", ok, tc.found)
			}
			if got != tc.want {
				t.Fatalf("image: got %q want %q", got, tc.want)
			}
		})
	}
}

// TestRuntime_Spawn_MissingImageMappingErrors asserts that a sandboxed spawn
// without an ImageMap entry returns ErrRuntimeNoImage so operators can
// diagnose a misconfiguration at startup rather than mid-request.
func TestRuntime_Spawn_MissingImageMappingErrors(t *testing.T) {
	t.Parallel()

	rt, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		SandboxPolicy:    orchestration.SandboxPolicyRequired,
		ContainerRuntime: &fakeContainerRuntime{available: true},
		Logger:           &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	_, spawnErr := rt.Spawn(context.Background(), "tenant-a", nil, "acp", &orchestration.SpawnRequest{
		SandboxMode: orchestration.SandboxModeRequired,
	})
	if !errors.Is(spawnErr, ErrRuntimeNoImage) {
		t.Fatalf("expected ErrRuntimeNoImage, got %v", spawnErr)
	}
}

// TestRuntime_PrepareImages_AutoPullInvokesRuntime verifies that every
// mapped image is pulled exactly once when autoPull is true.
func TestRuntime_PrepareImages_AutoPullInvokesRuntime(t *testing.T) {
	t.Parallel()

	cr := &fakeContainerRuntime{available: true}
	rt, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": &mockBackend{}, "ollama": &mockBackend{}},
		ImageMap:         map[string]string{"acp": "claude-code:v1", "ollama": "ollama/ollama:latest"},
		ImageRegistry:    "ghcr.io/example",
		ContainerRuntime: cr,
		Logger:           &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.PrepareImages(context.Background(), true); err != nil {
		t.Fatalf("PrepareImages: %v", err)
	}

	got := cr.pulledSnapshot()
	want := []string{"ghcr.io/example/claude-code:v1", "ollama/ollama:latest"}
	if len(got) != len(want) {
		t.Fatalf("pulls: got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("pull[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}

// TestRuntime_PrepareImages_ErrorsLoggedNotPropagated confirms that a Pull
// failure on one image does not abort the whole daemon startup.
func TestRuntime_PrepareImages_ErrorsLoggedNotPropagated(t *testing.T) {
	t.Parallel()

	cr := &fakeContainerRuntime{available: true, pullErr: errors.New("registry unreachable")}
	rt, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		ImageMap:         map[string]string{"acp": "claude-code:v1"},
		ContainerRuntime: cr,
		Logger:           &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.PrepareImages(context.Background(), true); err != nil {
		t.Fatalf("PrepareImages should swallow pull errors, got %v", err)
	}
	if n := len(cr.pulledSnapshot()); n != 1 {
		t.Fatalf("pull attempts: got %d want 1", n)
	}
}

// TestRuntime_PrepareImages_AutoPullDisabledSkips confirms the short-circuit.
func TestRuntime_PrepareImages_AutoPullDisabledSkips(t *testing.T) {
	t.Parallel()

	cr := &fakeContainerRuntime{available: true}
	rt, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		ImageMap:         map[string]string{"acp": "claude-code:v1"},
		ContainerRuntime: cr,
		Logger:           &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.PrepareImages(context.Background(), false); err != nil {
		t.Fatalf("PrepareImages(false): %v", err)
	}
	if n := len(cr.pulledSnapshot()); n != 0 {
		t.Fatalf("expected no pulls when autoPull=false, got %d", n)
	}
}

// TestRuntime_PrepareImages_NilContainerRuntimeSkips protects against the
// air-gapped configuration where no container runtime is available at all.
func TestRuntime_PrepareImages_NilContainerRuntimeSkips(t *testing.T) {
	t.Parallel()

	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &mockBackend{}},
		ImageMap: map[string]string{"acp": "claude-code:v1"},
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	if err := rt.PrepareImages(context.Background(), true); err != nil {
		t.Fatalf("PrepareImages nil-runtime: %v", err)
	}
}

// TestRuntime_Spawn_BumpsMetrics ensures SpawnsTotal increments on success.
// The metric labels are unique so parallel tests do not contend on the same
// Prometheus series.
func TestRuntime_Spawn_BumpsMetrics(t *testing.T) {
	t.Parallel()

	const tenant = "bump-tenant"
	const backend = "bump-backend"
	var spawnCalls atomic.Int32
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{
			backend: &metricsBackend{spawnCalls: &spawnCalls},
		},
		Logger: &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if _, err := rt.Spawn(context.Background(), tenant, nil, backend,
		&orchestration.SpawnRequest{WorkDir: "/tmp/x"}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if got := spawnCalls.Load(); got != 1 {
		t.Fatalf("backend spawn calls: got %d want 1", got)
	}
	if got := readCounterLabels(t, SpawnsTotal, tenant, backend, "host"); got != 1 {
		t.Fatalf("SpawnsTotal: got %v want 1", got)
	}
}

// metricsBackend is a minimal backend that records how many times its Spawn
// method was called — used by TestRuntime_Spawn_BumpsMetrics.
type metricsBackend struct {
	spawnCalls *atomic.Int32
}

func (m *metricsBackend) Spawn(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	m.spawnCalls.Add(1)
	return &orchestration.Session{ID: "sess-m", WorkDir: req.WorkDir}, nil
}
func (m *metricsBackend) Prompt(_ context.Context, _ *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return &orchestration.PromptResponse{}, nil
}
func (m *metricsBackend) Status(_ context.Context, id string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{SessionID: id}, nil
}
func (m *metricsBackend) Close(_ context.Context, _ string) error { return nil }
func (m *metricsBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{Kind: orchestration.BackendKindAgent}
}
func (m *metricsBackend) Type() string         { return "acp" }
func (m *metricsBackend) ToolGuidance() string { return "" }
