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
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// =============================================================================
// The storage tier reaches BOTH sandbox paths
// =============================================================================
//
// THERE ARE TWO, AND ONLY ONE OF THEM RUNS A SOFTWARE-FACTORY WORKER. The
// orchestrator's own spawnSandboxedSession launches a detached container for a
// backend that does not containerize itself. An ACP backend holding a container
// runtime does its own launching, and SpawnSession SKIPS the orchestrator's path
// entirely for it (backendHandlesContainers) — so the spec a worker actually
// runs in is built by ACPBackend.sandboxSpec.
//
// A tier wired into one path is a tier that silently does not apply to the
// fleet. These tests hold both.

// storageProject is the durable project record a spawn's tier is resolved from.
func storageProject(tenantID, projectID, workspace string, storage ProjectStorage) *Project {
	return &Project{
		ID:        projectID,
		TenantID:  tenantID,
		Name:      "api",
		Workspace: ProjectWorkspace{Path: workspace},
		Storage:   storage,
	}
}

// withStorageProject wires a durable project store holding one record onto the
// harness's service, so the spawn has a tier to resolve.
func (h spawnMountHarness) withStorageProject(t *testing.T, projectID string, storage ProjectStorage) {
	t.Helper()
	impl, ok := h.svc.(*agentServiceImpl)
	if !ok {
		t.Fatal("agent service is not the package implementation")
	}
	store := NewInMemoryProjectStore()
	project := storageProject(h.tenant, projectID, h.workspacePath(), storage)
	if err := store.Create(context.Background(), project); err != nil {
		t.Fatalf("project store Create: %v", err)
	}
	impl.SetProjectStore(store)
}

// ephemeralStorage is the configuration an operator writes to move a project's
// checkout off the disk: 2 GiB of tmpfs inside a 4 GiB container, which leaves
// more than the build headroom free.
func ephemeralStorage() ProjectStorage {
	return ProjectStorage{
		WorkspaceTier:     WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  2048,
		ContainerMemoryMB: 4096,
	}
}

// TestSandboxSpec_TheEphemeralTierReachesTheACPWorkerContainer is the one that
// matters most: this is the spec a software-factory worker runs in, built by the
// path SpawnSession dispatches to when the backend owns its own containers.
func TestSandboxSpec_TheEphemeralTierReachesTheACPWorkerContainer(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "claude", CLIMode: true})

	spec, err := backend.sandboxSpec(&SpawnRequest{
		WorkDir: "/srv/projects/tenant-a/proj-1/workspace",
		Storage: ephemeralStorage().Plan(),
	}, testContainerIdentity, "worker:1", []string{"claude"})
	if err != nil {
		t.Fatalf("sandboxSpec: %v", err)
	}

	if len(spec.Mounts) != 1 {
		t.Fatalf("mounts = %+v, want exactly the tmpfs checkout", spec.Mounts)
	}
	got := spec.Mounts[0]
	if got.Type != MountTypeTmpfs {
		t.Errorf("Type = %q, want %q — the worker's checkout is still on the host disk", got.Type, MountTypeTmpfs)
	}
	if got.Source != "" {
		t.Errorf("Source = %q, want no host source at all", got.Source)
	}
	if got.Target != containerWorkDir {
		t.Errorf("Target = %q, want %q", got.Target, containerWorkDir)
	}
	if got.TmpfsSizeBytes != 2048*testMB {
		t.Errorf("TmpfsSizeBytes = %d, want the project's configured size", got.TmpfsSizeBytes)
	}
	if spec.MemoryBytes != 4096*testMB {
		t.Errorf("MemoryBytes = %d, want the project's configured limit", spec.MemoryBytes)
	}
	if err := spec.Validate(); err != nil {
		t.Fatalf("the worker's spec does not validate: %v", err)
	}
}

// TestSandboxSpec_AnUnconfiguredProjectBuildsTheSpecItAlwaysDid is the
// inertness half on the same path: no storage configuration, no change.
func TestSandboxSpec_AnUnconfiguredProjectBuildsTheSpecItAlwaysDid(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "claude", CLIMode: true})
	const workDir = "/srv/projects/tenant-a/proj-1/workspace"

	spec, err := backend.sandboxSpec(&SpawnRequest{WorkDir: workDir}, testContainerIdentity, "worker:1", []string{"claude"})
	if err != nil {
		t.Fatalf("sandboxSpec: %v", err)
	}

	if len(spec.Mounts) != 1 {
		t.Fatalf("mounts = %+v, want exactly the host bind mount", spec.Mounts)
	}
	if spec.Mounts[0].Source != workDir || spec.Mounts[0].Type != "" {
		t.Fatalf("mounts[0] = %+v, want the untyped host bind mount", spec.Mounts[0])
	}
	if spec.MemoryBytes != 0 {
		t.Errorf("MemoryBytes = %d, want the runtime's own default", spec.MemoryBytes)
	}
}

// TestSandboxSpec_RefusesAnUnderProvisionedBudget proves the refusal reaches
// the path a worker takes, before the container is launched. The same
// misconfiguration observed at runtime is an OOM kill mid-attempt.
func TestSandboxSpec_RefusesAnUnderProvisionedBudget(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "claude", CLIMode: true})

	_, err := backend.sandboxSpec(&SpawnRequest{
		WorkDir: "/srv/projects/tenant-a/proj-1/workspace",
		Storage: ProjectStorage{
			WorkspaceTier:     WorkspaceTierEphemeral,
			WorkspaceTmpfsMB:  4096,
			ContainerMemoryMB: 4096,
		}.Plan(),
	}, testContainerIdentity, "worker:1", []string{"claude"})

	var budget *MemoryBudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("sandboxSpec() = %T %v, want a MemoryBudgetError", err, err)
	}
	if budget.ShortfallBytes() != WorkspaceBuildHeadroomBytes {
		t.Errorf("ShortfallBytes() = %d, want %d", budget.ShortfallBytes(), WorkspaceBuildHeadroomBytes)
	}
}

// TestSpawnSession_TheEphemeralTierReachesTheOrchestratorSandboxPath covers the
// other path: a backend that does not containerize itself, whose container the
// orchestrator launches. The assertion is on the spec the RUNTIME was handed.
func TestSpawnSession_TheEphemeralTierReachesTheOrchestratorSandboxPath(t *testing.T) {
	h := newSpawnMountHarness(t)
	h.withStorageProject(t, "proj-1", ephemeralStorage())

	if _, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:        "mock",
		ProjectID:      "proj-1",
		Sandboxed:      true,
		ContainerImage: "ghcr.io/example/agent:v1",
		WorkDir:        h.workspacePath(),
	}); err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	if len(h.runtime.runCalls) != 1 {
		t.Fatalf("container runtime run calls = %d, want 1", len(h.runtime.runCalls))
	}
	spec := h.runtime.runCalls[0].Spec
	if len(spec.Mounts) != 1 {
		t.Fatalf("mounts = %+v, want exactly the tmpfs checkout", spec.Mounts)
	}
	if spec.Mounts[0].Type != MountTypeTmpfs || spec.Mounts[0].Source != "" {
		t.Fatalf("mounts[0] = %+v, want an unsourced tmpfs", spec.Mounts[0])
	}
	if spec.Mounts[0].TmpfsSizeBytes != 2048*testMB {
		t.Errorf("TmpfsSizeBytes = %d, want the project's configured size", spec.Mounts[0].TmpfsSizeBytes)
	}
	if spec.MemoryBytes != 4096*testMB {
		t.Errorf("MemoryBytes = %d, want the project's configured limit", spec.MemoryBytes)
	}
}

// TestSpawnSession_AnUnderProvisionedProjectIsRefusedBeforeAnyContainer is the
// fail-closed property at the spawn seam: the refusal happens instead of a
// launch, not after one.
func TestSpawnSession_AnUnderProvisionedProjectIsRefusedBeforeAnyContainer(t *testing.T) {
	h := newSpawnMountHarness(t)
	h.withStorageProject(t, "proj-1", ProjectStorage{
		WorkspaceTier:     WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 4096,
	})

	_, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:        "mock",
		ProjectID:      "proj-1",
		Sandboxed:      true,
		ContainerImage: "ghcr.io/example/agent:v1",
		WorkDir:        h.workspacePath(),
	})
	var budget *MemoryBudgetError
	if !errors.As(err, &budget) {
		t.Fatalf("SpawnSession() = %T %v, want a MemoryBudgetError", err, err)
	}
	if len(h.runtime.runCalls) != 0 {
		t.Fatalf("a container was launched despite the refusal: %+v", h.runtime.runCalls[0].Spec)
	}
}

// TestSpawnSession_APersistentProjectStillMountsItsHostWorkspace is the
// regression guard for every project that exists today: with the tier unset the
// spawn is byte-for-byte the one it has always been.
func TestSpawnSession_APersistentProjectStillMountsItsHostWorkspace(t *testing.T) {
	h := newSpawnMountHarness(t)
	h.withStorageProject(t, "proj-1", ProjectStorage{})

	if _, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:        "mock",
		ProjectID:      "proj-1",
		Sandboxed:      true,
		ContainerImage: "ghcr.io/example/agent:v1",
		WorkDir:        h.workspacePath(),
	}); err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	spec := h.runtime.runCalls[0].Spec
	if len(spec.Mounts) != 1 || spec.Mounts[0].Source != h.workspacePath() {
		t.Fatalf("mounts = %+v, want the host bind mount of %q", spec.Mounts, h.workspacePath())
	}
	if spec.MemoryBytes != 0 {
		t.Errorf("MemoryBytes = %d, want the runtime's own default", spec.MemoryBytes)
	}
}

// specCapturingRuntime is a working ACP container runtime that keeps every spec
// it was asked to launch. It delegates the stdio handshake to the fixture that
// already models one, so the spawn under test is a real ACP spawn.
type specCapturingRuntime struct {
	inner *ctxBoundContainerRuntime

	mu    sync.Mutex
	specs []*ContainerSpec
}

func (r *specCapturingRuntime) RunInteractive(ctx context.Context, spec *ContainerSpec) (*InteractiveProcess, error) {
	r.mu.Lock()
	r.specs = append(r.specs, spec)
	r.mu.Unlock()
	return r.inner.RunInteractive(ctx, spec)
}

func (r *specCapturingRuntime) Run(ctx context.Context, spec *ContainerSpec) (string, error) {
	r.mu.Lock()
	r.specs = append(r.specs, spec)
	r.mu.Unlock()
	return r.inner.Run(ctx, spec)
}

func (r *specCapturingRuntime) Stop(ctx context.Context, id string) error {
	return r.inner.Stop(ctx, id)
}
func (r *specCapturingRuntime) Exec(ctx context.Context, id string, spec *ExecSpec) (*ExecResult, error) {
	return r.inner.Exec(ctx, id, spec)
}
func (r *specCapturingRuntime) Remove(ctx context.Context, id string) error {
	return r.inner.Remove(ctx, id)
}
func (r *specCapturingRuntime) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	return r.inner.Logs(ctx, id)
}
func (r *specCapturingRuntime) IsRunning(ctx context.Context, id string) (bool, error) {
	return r.inner.IsRunning(ctx, id)
}
func (r *specCapturingRuntime) IsAvailable() bool                        { return true }
func (r *specCapturingRuntime) Pull(ctx context.Context, i string) error { return r.inner.Pull(ctx, i) }

// captured returns the specs launched so far.
func (r *specCapturingRuntime) captured() []*ContainerSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*ContainerSpec(nil), r.specs...)
}

// TestSpawnSession_AnACPBackendOwnsItsContainerAndStillGetsTheTier is the
// end-to-end proof of the dispatch. A real ACPBackend holding a container
// runtime makes backendHandlesContainers true, so SpawnSession never calls the
// ORCHESTRATOR's runtime — and the tier must still reach the container the
// BACKEND launched, via SpawnRequest.Storage.
func TestSpawnSession_AnACPBackendOwnsItsContainerAndStillGetsTheTier(t *testing.T) {
	base := t.TempDir()
	projectsRoot := filepath.Join(base, "projects")
	tenant := "tenant-a"
	workspace := filepath.Join(projectsRoot, tenant, "proj-1", workspaceDirName)
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	backendRuntime := &specCapturingRuntime{inner: &ctxBoundContainerRuntime{t: t}}
	backend := NewACPBackend(ACPBackendConfig{
		Command:      "opencode",
		Args:         []string{"acp"},
		DefaultModel: "test-model",
	})
	backend.SetContainerRuntime(backendRuntime)

	svc, err := NewAgentService(NewInMemoryAgentStore(),
		map[string]AgentBackend{"acp": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatal("agent service is not the package implementation")
	}
	// The ORCHESTRATOR's runtime is a separate recorder: the point of the test
	// is that it is never asked to launch anything.
	orchestratorRuntime := &mockContainerRuntime{available: true, runID: "orchestrator-container"}
	impl.SetContainerRuntime(orchestratorRuntime)
	impl.SetMountPolicy(&MountPolicy{ProjectsRoot: projectsRoot})

	store := NewInMemoryProjectStore()
	if err := store.Create(context.Background(),
		storageProject(tenant, "proj-1", workspace, ephemeralStorage())); err != nil {
		t.Fatalf("project store Create: %v", err)
	}
	impl.SetProjectStore(store)

	ctx := ContextWithTenant(context.Background(), tenant)
	resp, err := svc.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:        "acp",
		ProjectID:      "proj-1",
		Model:          "test-model",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "worker:1",
		WorkDir:        workspace,
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	t.Cleanup(func() { _, _ = svc.CloseSession(ctx, &CloseSessionRequest{SessionID: resp.SessionID}) })

	if len(orchestratorRuntime.runCalls) != 0 {
		t.Fatalf("the orchestrator launched its own container for a backend that owns one: %+v",
			orchestratorRuntime.runCalls[0].Spec)
	}
	specs := backendRuntime.captured()
	if len(specs) != 1 {
		t.Fatalf("the backend launched %d containers, want 1", len(specs))
	}
	// The checkout is found by its container-side path rather than by
	// position: the JSON-RPC path appends its own credential-configuration
	// mount, and this test is about the checkout, not about that.
	var checkout *ContainerMount
	for i := range specs[0].Mounts {
		if specs[0].Mounts[i].Target == containerWorkDir {
			checkout = &specs[0].Mounts[i]
		}
	}
	if checkout == nil {
		t.Fatalf("mounts = %+v, want one at %q", specs[0].Mounts, containerWorkDir)
	}
	if checkout.Type != MountTypeTmpfs || checkout.Source != "" {
		t.Fatalf("the checkout mount is %+v: the worker's work is still on the host disk", *checkout)
	}
	if checkout.TmpfsSizeBytes != 2048*testMB {
		t.Errorf("TmpfsSizeBytes = %d, want the project's configured size", checkout.TmpfsSizeBytes)
	}
	// No OTHER mount became a tmpfs: an ephemeral checkout must not make the
	// shared-persistent mounts a spawn carries ephemeral too.
	for i := range specs[0].Mounts {
		m := specs[0].Mounts[i]
		if m.Target != containerWorkDir && m.Type == MountTypeTmpfs {
			t.Errorf("mount %+v became ephemeral with the checkout", m)
		}
	}
	if specs[0].MemoryBytes != 4096*testMB {
		t.Errorf("MemoryBytes = %d, want the project's configured limit", specs[0].MemoryBytes)
	}
}

// TestResolveWorkspaceStorage_AnUnresolvableProjectPlansNothing covers every
// way the daemon can have no record to read, all of which must leave the spawn
// launching what it always did.
func TestResolveWorkspaceStorage_AnUnresolvableProjectPlansNothing(t *testing.T) {
	h := newSpawnMountHarness(t)
	impl, ok := h.svc.(*agentServiceImpl)
	if !ok {
		t.Fatal("agent service is not the package implementation")
	}

	t.Run("NoProjectStoreWired", func(t *testing.T) {
		plan, err := impl.resolveWorkspaceStorage(h.ctx(), "proj-1")
		if err != nil {
			t.Fatalf("resolveWorkspaceStorage: %v", err)
		}
		if !plan.Inert() {
			t.Fatalf("plan = %+v, want the inert plan", plan)
		}
	})

	h.withStorageProject(t, "proj-1", ephemeralStorage())

	t.Run("NoProjectIDOnTheRequest", func(t *testing.T) {
		plan, err := impl.resolveWorkspaceStorage(h.ctx(), "")
		if err != nil {
			t.Fatalf("resolveWorkspaceStorage: %v", err)
		}
		if !plan.Inert() {
			t.Fatalf("plan = %+v, want the inert plan", plan)
		}
	})

	t.Run("TheStoreHoldsNoSuchRecord", func(t *testing.T) {
		plan, err := impl.resolveWorkspaceStorage(h.ctx(), "proj-missing")
		if err != nil {
			t.Fatalf("resolveWorkspaceStorage: %v", err)
		}
		if !plan.Inert() {
			t.Fatalf("plan = %+v, want the inert plan", plan)
		}
	})

	t.Run("TheRecordIsRead", func(t *testing.T) {
		plan, err := impl.resolveWorkspaceStorage(h.ctx(), "proj-1")
		if err != nil {
			t.Fatalf("resolveWorkspaceStorage: %v", err)
		}
		if !plan.Ephemeral() || plan.TmpfsSizeBytes != 2048*testMB {
			t.Fatalf("plan = %+v, want the project's ephemeral tier", plan)
		}
	})
}

// TestResolveWorkspaceStorage_RefusesARecordItCannotHonour is the bad-input
// twin, and the reason it is a refusal rather than a fallback: the zero value
// is the weakest reading, so a project whose tier was misspelled would silently
// keep writing to the disk it was configured off.
func TestResolveWorkspaceStorage_RefusesARecordItCannotHonour(t *testing.T) {
	h := newSpawnMountHarness(t)
	h.withStorageProject(t, "proj-1", ProjectStorage{WorkspaceTier: "ephemerial"})
	impl, ok := h.svc.(*agentServiceImpl)
	if !ok {
		t.Fatal("agent service is not the package implementation")
	}

	_, err := impl.resolveWorkspaceStorage(h.ctx(), "proj-1")
	var invalid *InputValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("resolveWorkspaceStorage() = %T %v, want an InputValidationError", err, err)
	}
	if invalid.Field != "storage.workspace_tier" {
		t.Errorf("Field = %q, want storage.workspace_tier", invalid.Field)
	}

	// And the spawn it would have served is refused with it, before any
	// container exists.
	if _, err := h.svc.SpawnSession(h.ctx(), &SpawnSessionRequest{
		Backend:        "mock",
		ProjectID:      "proj-1",
		Sandboxed:      true,
		ContainerImage: "ghcr.io/example/agent:v1",
		WorkDir:        h.workspacePath(),
	}); !errors.As(err, &invalid) {
		t.Fatalf("SpawnSession() = %T %v, want the same refusal", err, err)
	}
	h.assertNoSideEffects(t)
}
