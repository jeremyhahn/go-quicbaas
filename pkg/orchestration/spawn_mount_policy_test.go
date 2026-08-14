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
	"os"
	"path/filepath"
	"testing"
)

// spawnMountHarness is a SpawnSession fixture wired the way the daemon wires
// it: a mount policy over a real projects root, an available container runtime
// so the sandbox path is live, and recorders on both side effects a rejected
// spawn must never reach — the container runtime and the backend.
type spawnMountHarness struct {
	svc      AgentService
	runtime  *mockContainerRuntime
	spawns   *[]*SpawnRequest
	projects string
	tenant   string
}

func newSpawnMountHarness(t *testing.T) spawnMountHarness {
	t.Helper()

	base := t.TempDir()
	projectsRoot := filepath.Join(base, "projects")
	tenant := "tenant-a"
	workspace := filepath.Join(projectsRoot, tenant, "proj-1", workspaceDirName)
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	spawns := new([]*SpawnRequest)
	backend := &mockAgentBackend{
		spawnFn: func(_ context.Context, req *SpawnRequest) (*Session, error) {
			*spawns = append(*spawns, req)
			return &Session{
				ID:        "session-mounts",
				Backend:   "mock",
				Status:    SessionStateActive,
				WorkDir:   req.WorkDir,
				CreatedAt: 1,
			}, nil
		},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(),
		map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService error: %v", err)
	}

	runtime := &mockContainerRuntime{available: true, runID: "container-1"}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatal("agent service is not the package implementation")
	}
	impl.SetContainerRuntime(runtime)
	impl.SetMountPolicy(&MountPolicy{ProjectsRoot: projectsRoot})

	return spawnMountHarness{
		svc:      svc,
		runtime:  runtime,
		spawns:   spawns,
		projects: projectsRoot,
		tenant:   tenant,
	}
}

// workspacePath returns the tenant's managed workspace directory.
func (h spawnMountHarness) workspacePath() string {
	return filepath.Join(h.projects, h.tenant, "proj-1", workspaceDirName)
}

// ctx returns a request context carrying the harness tenant.
func (h spawnMountHarness) ctx() context.Context {
	return ContextWithTenant(context.Background(), h.tenant)
}

// assertNoSideEffects fails when a rejected spawn reached either side effect.
func (h spawnMountHarness) assertNoSideEffects(t *testing.T) {
	t.Helper()
	if len(h.runtime.runCalls) != 0 {
		t.Errorf("container runtime was called %d time(s) despite the denial: %+v",
			len(h.runtime.runCalls), h.runtime.runCalls[0].Spec)
	}
	if len(*h.spawns) != 0 {
		t.Errorf("backend spawn was called %d time(s) despite the denial", len(*h.spawns))
	}
}

// TestSpawnSession_WorkDirOutsidePolicy_Denied is the regression test for the
// host-root bind mount: a caller with orchestrator:spawn asks for a sandboxed
// session rooted at an arbitrary host path, and both sandbox paths would
// bind-mount that path read-write into a container running as the daemon's own
// uid. Every one of these work directories must fail the spawn before any side
// effect.
func TestSpawnSession_WorkDirOutsidePolicy_Denied(t *testing.T) {
	t.Parallel()

	sensitive := t.TempDir() // stands in for the daemon's own data directory

	cases := []struct {
		name    string
		workDir string
	}{
		{"host root", "/"},
		{"absolute path outside every root", sensitive},
		{"etc", "/etc"},
		{"relative path", "../.."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := newSpawnMountHarness(t)
			_, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
				Backend:        "mock",
				Sandboxed:      true,
				ContainerImage: "ghcr.io/example/agent:v1",
				WorkDir:        tc.workDir,
			})
			var denied *MountNotAllowedError
			if !errors.As(err, &denied) {
				t.Fatalf("expected MountNotAllowedError for work_dir %q, got %T: %v",
					tc.workDir, err, err)
			}
			if denied.Field != MountFieldWorkDir {
				t.Errorf("denied.Field = %q, want %q", denied.Field, MountFieldWorkDir)
			}
			if denied.Source != tc.workDir {
				t.Errorf("denied.Source = %q, want %q", denied.Source, tc.workDir)
			}
			h.assertNoSideEffects(t)
		})
	}
}

// TestSpawnSession_WorkDirCrossTenant_Denied covers the tenant scoping: the
// projects root is shared, so one tenant naming another tenant's workspace as
// its work directory must be refused even though the path is under the root.
func TestSpawnSession_WorkDirCrossTenant_Denied(t *testing.T) {
	t.Parallel()

	h := newSpawnMountHarness(t)
	victim := filepath.Join(h.projects, "tenant-b", "proj-9", workspaceDirName)
	if err := os.MkdirAll(victim, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:        "mock",
		Sandboxed:      true,
		ContainerImage: "ghcr.io/example/agent:v1",
		WorkDir:        victim,
	})
	var denied *MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError, got %T: %v", err, err)
	}
	h.assertNoSideEffects(t)
}

// TestSpawnSession_WorkDirSymlinkEscape_Denied covers the resolved-path half at
// the spawn seam: an agent that plants a symlink inside its own workspace must
// not be able to have the symlink's target mounted on the next spawn.
func TestSpawnSession_WorkDirSymlinkEscape_Denied(t *testing.T) {
	t.Parallel()

	h := newSpawnMountHarness(t)
	escape := filepath.Join(h.workspacePath(), "escape")
	if err := os.Symlink("/", escape); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:        "mock",
		Sandboxed:      true,
		ContainerImage: "ghcr.io/example/agent:v1",
		WorkDir:        escape,
	})
	var denied *MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError, got %T: %v", err, err)
	}
	h.assertNoSideEffects(t)
}

// TestSpawnSession_ManagedWorkspaceWorkDir_Allowed is the other half of the
// contract: the legitimate flow — a work directory inside the tenant's own
// managed workspace — still spawns, and the container is mounted on it.
func TestSpawnSession_ManagedWorkspaceWorkDir_Allowed(t *testing.T) {
	t.Parallel()

	h := newSpawnMountHarness(t)
	resp, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:        "mock",
		Sandboxed:      true,
		ContainerImage: "ghcr.io/example/agent:v1",
		WorkDir:        h.workspacePath(),
	})
	if err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
	if resp.ContainerID != "container-1" {
		t.Fatalf("ContainerID = %q, want the launched container", resp.ContainerID)
	}
	if len(h.runtime.runCalls) != 1 {
		t.Fatalf("container runtime run calls = %d, want 1", len(h.runtime.runCalls))
	}
	mounts := h.runtime.runCalls[0].Spec.Mounts
	if len(mounts) != 1 || mounts[0].Source != h.workspacePath() {
		t.Fatalf("container mounts = %+v, want one mount of %q", mounts, h.workspacePath())
	}
}

// TestSpawnSession_ContainerConfigMounts_Authorized closes the second
// caller-controlled mount source: a full container spec supplied on the request
// carries its own Mounts, which the sandbox path launches verbatim.
func TestSpawnSession_ContainerConfigMounts_Authorized(t *testing.T) {
	t.Parallel()

	h := newSpawnMountHarness(t)

	// Denied: the spec names a host path outside every root.
	_, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:   "mock",
		Sandboxed: true,
		ContainerConfig: &ContainerSpec{
			Image:  "ghcr.io/example/agent:v1",
			Mounts: []ContainerMount{{Source: "/", Target: "/host"}},
		},
	})
	var denied *MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError, got %T: %v", err, err)
	}
	if denied.Field != MountFieldContainerConfig {
		t.Errorf("denied.Field = %q, want %q", denied.Field, MountFieldContainerConfig)
	}
	h.assertNoSideEffects(t)

	// Allowed: the same spec sourced from the tenant's own workspace.
	if _, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:   "mock",
		Sandboxed: true,
		ContainerConfig: &ContainerSpec{
			Image:  "ghcr.io/example/agent:v1",
			Mounts: []ContainerMount{{Source: h.workspacePath(), Target: "/workspace"}},
		},
	}); err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
}

// TestSpawnSession_UnsandboxedWorkDir_NotMounted documents the boundary: a raw
// exec session bind-mounts nothing, so the work directory is not a mount source
// and is not run through the mount policy. The agent process has the daemon's
// own filesystem access either way — that is what the sandbox axis governs —
// and constraining the cwd here would only break unmanaged project paths.
func TestSpawnSession_UnsandboxedWorkDir_NotMounted(t *testing.T) {
	t.Parallel()

	h := newSpawnMountHarness(t)
	outside := t.TempDir()
	if _, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend: "mock",
		WorkDir: outside,
	}); err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
	if len(h.runtime.runCalls) != 0 {
		t.Fatalf("container runtime run calls = %d, want 0 for a raw exec session",
			len(h.runtime.runCalls))
	}
	if len(*h.spawns) != 1 || (*h.spawns)[0].WorkDir != outside {
		t.Fatalf("backend spawn work dir = %+v, want %q", *h.spawns, outside)
	}
}

// TestSpawnSession_SandboxedWithoutPolicy_Denied is the deny-by-default case:
// an orchestrator with no mount policy wired must refuse a containerized spawn
// that names a work directory rather than mounting it unchecked.
func TestSpawnSession_SandboxedWithoutPolicy_Denied(t *testing.T) {
	t.Parallel()

	h := newSpawnMountHarness(t)
	impl, ok := h.svc.(*agentServiceImpl)
	if !ok {
		t.Fatal("agent service is not the package implementation")
	}
	impl.SetMountPolicy(nil)

	_, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:        "mock",
		Sandboxed:      true,
		ContainerImage: "ghcr.io/example/agent:v1",
		WorkDir:        h.workspacePath(),
	})
	var denied *MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError, got %T: %v", err, err)
	}
	h.assertNoSideEffects(t)
}

// TestSpawnSession_ExtraMountsScopedToTenant checks that ExtraMounts are
// authorized against the requesting tenant's subtree, not the shared root.
func TestSpawnSession_ExtraMountsScopedToTenant(t *testing.T) {
	t.Parallel()

	h := newSpawnMountHarness(t)
	victim := filepath.Join(h.projects, "tenant-b", "proj-9", workspaceDirName)
	if err := os.MkdirAll(victim, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:     "mock",
		ExtraMounts: []ContainerMount{{Source: victim, Target: "/peek"}},
	})
	var denied *MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError, got %T: %v", err, err)
	}
	if denied.Field != MountFieldExtraMounts {
		t.Errorf("denied.Field = %q, want %q", denied.Field, MountFieldExtraMounts)
	}
	if denied.TenantID != h.tenant {
		t.Errorf("denied.TenantID = %q, want %q", denied.TenantID, h.tenant)
	}
	h.assertNoSideEffects(t)

	// The tenant's own workspace passes and reaches the backend verbatim.
	if _, spawnErr := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:     "mock",
		ExtraMounts: []ContainerMount{{Source: h.workspacePath(), Target: "/extra", ReadOnly: true}},
	}); spawnErr != nil {
		t.Fatalf("SpawnSession error: %v", spawnErr)
	}
	if len(*h.spawns) != 1 || len((*h.spawns)[0].ExtraMounts) != 1 {
		t.Fatalf("backend ExtraMounts = %+v, want one mount", *h.spawns)
	}
	if !(*h.spawns)[0].ExtraMounts[0].ReadOnly {
		t.Error("ExtraMounts[0].ReadOnly = false, want the request's read-only flag preserved")
	}
}

// =============================================================================
// Sandbox network clamp
// =============================================================================

// TestSpawnSession_ContainerConfigHostNetwork_Refused is the regression test for
// the second sandbox dimension: a caller-supplied container spec must not be
// able to select the host network namespace, which would let the "sandboxed"
// agent dial every loopback-bound service on the daemon's host.
func TestSpawnSession_ContainerConfigHostNetwork_Refused(t *testing.T) {
	t.Parallel()

	// Every spelling a runtime would accept as host networking is refused, not
	// just the canonical one.
	for _, requested := range []string{"host", "HOST", " host ", "Host"} {
		t.Run(requested, func(t *testing.T) {
			t.Parallel()

			h := newSpawnMountHarness(t)
			_, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
				Backend:   "mock",
				Sandboxed: true,
				ContainerConfig: &ContainerSpec{
					Image:   "ghcr.io/example/agent:v1",
					Network: requested,
				},
			})
			var denied *SandboxNetworkNotAllowedError
			if !errors.As(err, &denied) {
				t.Fatalf("expected SandboxNetworkNotAllowedError, got %T: %v", err, err)
			}
			if denied.Effective != SandboxNetworkDefault {
				t.Errorf("denied.Effective = %q, want the isolated default %q",
					denied.Effective, SandboxNetworkDefault)
			}
			h.assertNoSideEffects(t)
		})
	}
}

// TestSpawnSession_ContainerConfigNetwork_ReplacedByOperatorValue proves the
// caller's network never reaches the runtime even when it is an innocuous name:
// the network is operator configuration, so the request has no say in it.
func TestSpawnSession_ContainerConfigNetwork_ReplacedByOperatorValue(t *testing.T) {
	t.Parallel()

	h := newSpawnMountHarness(t)
	if _, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:   "mock",
		Sandboxed: true,
		ContainerConfig: &ContainerSpec{
			Image:   "ghcr.io/example/agent:v1",
			Network: "caller-chosen-net",
		},
	}); err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
	if len(h.runtime.runCalls) != 1 {
		t.Fatalf("container runtime run calls = %d, want 1", len(h.runtime.runCalls))
	}
	if got := h.runtime.runCalls[0].Spec.Network; got != SandboxNetworkDefault.String() {
		t.Fatalf("container network = %q, want the operator default %q",
			got, SandboxNetworkDefault)
	}
}

// TestSpawnSession_SandboxedNetworkIsIsolatedByDefault covers the plain path:
// a spawn that supplies no container spec still gets an isolated namespace
// rather than whatever the runtime defaults to.
func TestSpawnSession_SandboxedNetworkIsIsolatedByDefault(t *testing.T) {
	t.Parallel()

	h := newSpawnMountHarness(t)
	if _, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:        "mock",
		Sandboxed:      true,
		ContainerImage: "ghcr.io/example/agent:v1",
		WorkDir:        h.workspacePath(),
	}); err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
	spec := h.runtime.runCalls[0].Spec
	if !SandboxNetwork(spec.Network).IsolatesHost() {
		t.Fatalf("container network = %q, want a host-isolating network", spec.Network)
	}
}

// TestResolveSandboxNetwork_HonorsBackendConfiguration proves the clamp reuses
// the backend's operator-configured value rather than a second mechanism: an
// operator who chose host networking on the backend still gets it, and a caller
// asking for it is still refused.
func TestResolveSandboxNetwork_HonorsBackendConfiguration(t *testing.T) {
	t.Parallel()

	h := newSpawnMountHarness(t)
	impl, ok := h.svc.(*agentServiceImpl)
	if !ok {
		t.Fatal("agent service is not the package implementation")
	}

	operatorHost := NewACPBackend(ACPBackendConfig{
		Command:        "true",
		SandboxNetwork: SandboxNetworkHost,
	})
	network, err := impl.resolveSandboxNetwork(operatorHost, "")
	if err != nil {
		t.Fatalf("resolveSandboxNetwork error: %v", err)
	}
	if network != SandboxNetworkHost {
		t.Fatalf("network = %q, want the operator-configured %q", network, SandboxNetworkHost)
	}

	// The same backend still refuses a caller that names it.
	if _, err := impl.resolveSandboxNetwork(operatorHost, "host"); err == nil {
		t.Fatal("expected a caller-supplied host network to be refused")
	}

	// A backend with no configured network resolves to the isolated default.
	plain := NewACPBackend(ACPBackendConfig{Command: "true"})
	network, err = impl.resolveSandboxNetwork(plain, "")
	if err != nil {
		t.Fatalf("resolveSandboxNetwork error: %v", err)
	}
	if network != SandboxNetworkDefault {
		t.Fatalf("network = %q, want %q", network, SandboxNetworkDefault)
	}
}
