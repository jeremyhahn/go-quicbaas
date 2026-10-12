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

// orchestrator_workspace_test.go is the suite for go-quicbaas#981: a spawn
// NAMES its workspace, the host that executes the session provisions it, and a
// job's later sessions are pinned to that host. It drives the public
// AgentService and JobRun against two backend instances — the daemon's own
// in-process backend and a fleet node behind the resolver — so every assertion
// is about where a session actually landed and which host's filesystem holds
// its checkout.

package orchestration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// workspaceBackend is a backend instance that records the spawn it was handed.
// A fleet instance (host != "") provisions the named workspace under its OWN
// projects root with the same WorkspaceManager a fleet node daemon runs, and
// reports the directory as the session's WorkDir — or, with agentRoot nil,
// behaves as an agent that predates named workspaces and reports nothing.
type workspaceBackend struct {
	label     string
	host      string
	agentRoot *WorkspaceManager
	spawnErr  error

	mu       sync.Mutex
	spawns   []*SpawnRequest
	closed   []string
	sessions map[string]*ProvisionedWorkspace

	// knownHosts is the pinned host key each spawn's spec carried, read at
	// spawn time: the orchestrator wipes the request's copy once the spawn is
	// over.
	knownHosts []string

	// execs is every command run in one of this host's session containers.
	execs []string
}

// ExecInSession runs a command in a session container on THIS host — the
// agent's own container runtime, as a fleet node's ExecSession RPC does.
func (b *workspaceBackend) ExecInSession(_ context.Context, sessionID string, spec *ExecSpec) (*ExecResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, held := b.sessions[sessionID]; !held {
		return nil, &SessionNotFoundError{SessionID: sessionID}
	}
	b.execs = append(b.execs, sessionID+":"+strings.Join(spec.Cmd, " "))
	return &ExecResult{Stdout: b.label}, nil
}

func newWorkspaceBackend(label, host string, agentRoot *WorkspaceManager) *workspaceBackend {
	return &workspaceBackend{label: label, host: host, agentRoot: agentRoot, sessions: map[string]*ProvisionedWorkspace{}}
}

func (b *workspaceBackend) Spawn(_ context.Context, req *SpawnRequest) (*Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spawns = append(b.spawns, req)
	if req.Workspace != nil && req.Workspace.Spec != nil {
		b.knownHosts = append(b.knownHosts, string(req.Workspace.Spec.Files[WorkspaceKnownHostsFile]))
	}
	if b.spawnErr != nil {
		return nil, b.spawnErr
	}
	id := b.label + "-" + string(rune('0'+len(b.spawns)))
	workDir := req.WorkDir
	if b.host != "" && req.Workspace != nil {
		workDir = ""
		if b.agentRoot != nil {
			p, err := b.agentRoot.ProvisionSession(req.Workspace)
			if err != nil {
				return nil, err
			}
			b.sessions[id] = p
			workDir = p.Dir
		}
	}
	return &Session{ID: id, Backend: "codex", Status: SessionStateActive, WorkDir: workDir,
		ContainerID: "ctr-" + id, CreatedAt: 1000}, nil
}

func (b *workspaceBackend) Prompt(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
	return &PromptResponse{SessionID: req.SessionID, StopReason: "end_turn"}, nil
}

func (b *workspaceBackend) Status(_ context.Context, sessionID string) (*SessionStatus, error) {
	return &SessionStatus{SessionID: sessionID, State: SessionStateActive}, nil
}

func (b *workspaceBackend) Close(_ context.Context, sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = append(b.closed, sessionID)
	if p, ok := b.sessions[sessionID]; ok {
		p.Release()
		delete(b.sessions, sessionID)
	}
	return nil
}

func (b *workspaceBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{Kind: BackendKindAgent, PersistentSessions: true, ACP: true}
}
func (b *workspaceBackend) Type() string         { return "acp" }
func (b *workspaceBackend) ToolGuidance() string { return "" }

// ResolvedHost makes a fleet instance satisfy resolvedHostBackend.
func (b *workspaceBackend) ResolvedHost() string { return b.host }

func (b *workspaceBackend) spawnCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.spawns)
}

func (b *workspaceBackend) lastSpawn() *SpawnRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.spawns) == 0 {
		return nil
	}
	return b.spawns[len(b.spawns)-1]
}

// fleet is the dispatcher's view in these tests: the agents it may route to,
// and the affinity each resolution was asked for.
type fleet struct {
	mu      sync.Mutex
	agents  []*workspaceBackend
	strict  []bool
	offline bool
}

// resolve mirrors the production resolver's filtering: the first agent that
// matches the host affinity, or any agent when none is asked for, and a fall
// through to the in-process backend when nothing matches.
func (f *fleet) resolve(ctx context.Context, _, _ string) (AgentBackend, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.strict = append(f.strict, HostAffinityStrictFromContext(ctx))
	if f.offline {
		return nil, false
	}
	affinity := HostAffinityFromContext(ctx)
	for _, a := range f.agents {
		if affinity == "" || a.host == affinity {
			return a, true
		}
	}
	return nil, false
}

// workspaceSvc wires an AgentService with a daemon provisioner over its own
// projects root, an in-process backend, and the fleet behind the resolver.
func workspaceSvc(t *testing.T, local *workspaceBackend, f *fleet) (*agentServiceImpl, *WorkspaceManager) {
	t.Helper()
	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{"codex": local}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	daemonRoot := newTestWorkspaceManager(t)
	impl.SetWorkspaceProvisioner(daemonRoot)
	impl.SetMountPolicy(&MountPolicy{ProjectsRoot: daemonRoot.Root()})
	if f != nil {
		impl.SetRemoteBackendResolver(f.resolve)
	}
	return impl, daemonRoot
}

// workspaceSpawn is the request a runner's seam produces: the workspace named,
// its spec carried, and no host path anywhere.
func workspaceSpawn() *SpawnSessionRequest {
	return &SpawnSessionRequest{Backend: "codex", ProjectID: "p1", Workspace: namedWorkspace()}
}

// TestSpawnSession_FleetAgentProvisionsTheNamedWorkspace is the defect's
// shape inverted: the spawn reaches the fleet node naming the workspace and
// carrying its spec, the AGENT provisions it under its own root, and the daemon
// records and reports the agent's directory. Nothing is made on the daemon.
func TestSpawnSession_FleetAgentProvisionsTheNamedWorkspace(t *testing.T) {
	t.Parallel()
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	impl, daemonRoot := workspaceSvc(t, newWorkspaceBackend("local", "", nil), &fleet{agents: []*workspaceBackend{agent09}})

	resp, err := impl.SpawnSession(tenantCtx("t1"), workspaceSpawn())
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	got := agent09.lastSpawn()
	if got == nil || got.Workspace == nil || got.WorkDir != "" {
		t.Fatalf("the agent must be handed the workspace by name and no host path: %+v", got)
	}
	if len(agent09.knownHosts) != 1 || agent09.knownHosts[0] != testKnownHosts {
		t.Fatalf("the spec must travel to the agent that writes it: %q", agent09.knownHosts)
	}
	// AND NOTHING ON THE DAEMON HOLDS IT AFTERWARDS: the request's copy is
	// wiped once the spawn is over.
	if got.Workspace.Spec != nil && len(got.Workspace.Spec.Files) != 0 {
		t.Fatalf("the daemon still holds the spec after the spawn: %v", got.Workspace.Spec.Files)
	}
	wantDir := filepath.Join(agent09.agentRoot.Root(), "t1", "p1", workspaceDirName, "task-a1")
	if resp.Workspace == nil || resp.Workspace.AgentHost != "agent-09:9091" || resp.Workspace.Dir != wantDir ||
		resp.Workspace.Key != "task-a1" {
		t.Fatalf("placement = %+v, want agent09 at %s", resp.Workspace, wantDir)
	}
	record, err := impl.store.GetSession(tenantCtx("t1"), "t1", resp.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.WorkDir != wantDir {
		t.Fatalf("the durable record must carry the agent's directory: %q", record.WorkDir)
	}
	if _, err := os.Stat(filepath.Join(daemonRoot.Root(), "t1")); !os.IsNotExist(err) {
		t.Fatalf("nothing may be provisioned on the daemon for a fleet spawn: %v", err)
	}
}

// TestSpawnSession_PinnedSpawnLandsOnTheAgentHoldingTheCheckout: the job's
// second session resolves strictly to the pinned agent even when another agent
// would be picked first, and continues the same directory.
func TestSpawnSession_PinnedSpawnLandsOnTheAgentHoldingTheCheckout(t *testing.T) {
	t.Parallel()
	vs04 := newWorkspaceBackend("vs04", "vs04:9092", newTestWorkspaceManager(t))
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	f := &fleet{agents: []*workspaceBackend{vs04, agent09}}
	impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), f)

	first, err := agent09.agentRoot.ProvisionSession(namedWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	first.Release()

	req := workspaceSpawn()
	req.Workspace.Pin = &WorkspacePlacement{AgentHost: "agent-09:9091", Key: "task-a1", Dir: first.Dir}
	resp, err := impl.SpawnSession(tenantCtx("t1"), req)
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if agent09.spawnCount() != 1 || vs04.spawnCount() != 0 {
		t.Fatalf("the pinned spawn must land on agent09: agent09=%d vs04=%d", agent09.spawnCount(), vs04.spawnCount())
	}
	if resp.Workspace.Dir != first.Dir {
		t.Fatalf("the pinned spawn must continue the same checkout: %q", resp.Workspace.Dir)
	}
	f.mu.Lock()
	strict := f.strict[len(f.strict)-1]
	f.mu.Unlock()
	if !strict {
		t.Fatal("a pinned spawn must resolve with strict host affinity")
	}
}

// TestSpawnSession_PinnedAgentGoneIsRefusedNotRehomed is the requirement's
// sharp edge: the pinned agent is deregistered, the dispatcher falls through
// to the in-process backend exactly as it does for soft affinity — and the
// spawn is REFUSED with a typed error rather than cloned afresh there.
func TestSpawnSession_PinnedAgentGoneIsRefusedNotRehomed(t *testing.T) {
	t.Parallel()
	local := newWorkspaceBackend("local", "", nil)
	impl, daemonRoot := workspaceSvc(t, local, &fleet{offline: true})

	req := workspaceSpawn()
	req.Workspace.Pin = &WorkspacePlacement{AgentHost: "agent-09:9091", Key: "task-a1", Dir: "/agent-09/projects/t1/p1/workspace/task-a1"}
	resp, err := impl.SpawnSession(tenantCtx("t1"), req)
	var gone *WorkspaceFleetNodeUnavailableError
	if !errors.As(err, &gone) || !errors.Is(err, ErrSessionWorkspace) || resp != nil {
		t.Fatalf("got %v, %v; want *WorkspaceFleetNodeUnavailableError", resp, err)
	}
	if gone.AgentHost != "agent-09:9091" || gone.Key != "task-a1" {
		t.Fatalf("the refusal must name the pinned agent: %+v", gone)
	}
	if local.spawnCount() != 0 {
		t.Fatal("a pinned spawn must never fall through to the in-process backend")
	}
	if _, err := os.Stat(filepath.Join(daemonRoot.Root(), "t1")); !os.IsNotExist(err) {
		t.Fatalf("nothing may be provisioned on the daemon: %v", err)
	}
}

// TestSpawnSession_PinnedResolvedElsewhereIsRefused: a resolver that answers
// with a different agent than the pin names is refused before any spawn.
func TestSpawnSession_PinnedResolvedElsewhereIsRefused(t *testing.T) {
	t.Parallel()
	vs04 := newWorkspaceBackend("vs04", "vs04:9092", newTestWorkspaceManager(t))
	impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), &fleet{})
	impl.SetRemoteBackendResolver(func(context.Context, string, string) (AgentBackend, bool) { return vs04, true })

	req := workspaceSpawn()
	req.Workspace.Pin = &WorkspacePlacement{AgentHost: "agent-09:9091", Key: "task-a1", Dir: "/d"}
	_, err := impl.SpawnSession(tenantCtx("t1"), req)
	var gone *WorkspaceFleetNodeUnavailableError
	if !errors.As(err, &gone) || gone.ResolvedHost != "vs04:9092" {
		t.Fatalf("got %v, want a refusal naming where it resolved", err)
	}
	if vs04.spawnCount() != 0 {
		t.Fatal("nothing may be spawned on the wrong agent")
	}
}

// TestSpawnSession_AgentThatDidNotProvisionIsRefusedAndUnwound: an agent that
// predates named workspaces spawns a container with no workspace and reports
// no directory. That session is closed again and the spawn refused, rather than
// a checkout made inside a container layer nothing can pin to.
func TestSpawnSession_AgentThatDidNotProvisionIsRefusedAndUnwound(t *testing.T) {
	t.Parallel()
	old := newWorkspaceBackend("old", "old:9091", nil)
	impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), &fleet{agents: []*workspaceBackend{old}})

	_, err := impl.SpawnSession(tenantCtx("t1"), workspaceSpawn())
	var unsupported *SessionWorkspaceUnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Host != "old:9091" {
		t.Fatalf("got %v, want *SessionWorkspaceUnsupportedError naming the agent", err)
	}
	old.mu.Lock()
	closed := len(old.closed)
	old.mu.Unlock()
	if closed != 1 {
		t.Fatalf("the spawned process must be released: closed=%d", closed)
	}
}

// TestSpawnSession_DaemonBackendProvisionsUnderTheDaemonRoot is the
// unchanged path: with no fleet node serving the backend, the daemon's own
// backend runs the session, the daemon provisions the workspace under its
// projects root, the spec directory is mounted read-only, and the session's
// close releases the spec while the checkout stays.
func TestSpawnSession_DaemonBackendProvisionsUnderTheDaemonRoot(t *testing.T) {
	t.Parallel()
	local := newWorkspaceBackend("local", "", nil)
	impl, daemonRoot := workspaceSvc(t, local, nil)

	req := workspaceSpawn()
	resp, err := impl.SpawnSession(tenantCtx("t1"), req)
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	wantDir := filepath.Join(daemonRoot.Root(), "t1", "p1", workspaceDirName, "task-a1")
	if resp.Workspace == nil || !resp.Workspace.OnDaemon() || resp.Workspace.Dir != wantDir {
		t.Fatalf("placement = %+v, want the daemon at %s", resp.Workspace, wantDir)
	}
	got := local.lastSpawn()
	if got.WorkDir != wantDir || got.Workspace != nil {
		t.Fatalf("the in-process backend is handed the provisioned path: %+v", got)
	}
	var specDir string
	for _, m := range got.ExtraMounts {
		if m.Target == WorkspaceSpecDir {
			if !m.ReadOnly {
				t.Fatal("the spec directory must be mounted read-only")
			}
			specDir = m.Source
		}
	}
	if specDir == "" {
		t.Fatalf("the spec directory must be mounted: %+v", got.ExtraMounts)
	}
	if req.ExtraMounts != nil {
		t.Fatal("the caller's request must not be mutated")
	}

	if _, err := impl.CloseSession(tenantCtx("t1"), &CloseSessionRequest{SessionID: resp.SessionID}); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}
	if _, err := os.Stat(specDir); !os.IsNotExist(err) {
		t.Fatalf("the close must release the spec directory: %v", err)
	}
	if _, err := os.Stat(wantDir); err != nil {
		t.Fatalf("the checkout must survive the close: %v", err)
	}
}

// TestSpawnSession_DaemonPinStaysOnTheDaemon: a workspace the daemon holds
// keeps its job on the daemon even after a fleet node that serves the backend
// has appeared.
func TestSpawnSession_DaemonPinStaysOnTheDaemon(t *testing.T) {
	t.Parallel()
	local := newWorkspaceBackend("local", "", nil)
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	impl, daemonRoot := workspaceSvc(t, local, &fleet{agents: []*workspaceBackend{agent09}})

	first, err := daemonRoot.ProvisionSession(namedWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	first.Release()
	req := workspaceSpawn()
	req.Workspace.Pin = &WorkspacePlacement{Key: "task-a1", Dir: first.Dir}
	resp, err := impl.SpawnSession(tenantCtx("t1"), req)
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if local.spawnCount() != 1 || agent09.spawnCount() != 0 || resp.Workspace.Dir != first.Dir {
		t.Fatalf("the daemon-pinned spawn must stay local: local=%d agent09=%d", local.spawnCount(), agent09.spawnCount())
	}
}

// TestSpawnSession_FailedDaemonSpawnReleasesTheSpec: a spawn that fails after
// the daemon wrote the spec directory removes it — it holds the forge token
// and no session will ever read it.
func TestSpawnSession_FailedDaemonSpawnReleasesTheSpec(t *testing.T) {
	t.Parallel()
	local := newWorkspaceBackend("local", "", nil)
	local.spawnErr = errors.New("backend refused")
	impl, daemonRoot := workspaceSvc(t, local, nil)

	if _, err := impl.SpawnSession(tenantCtx("t1"), workspaceSpawn()); err == nil {
		t.Fatal("the spawn must fail")
	}
	entries, err := os.ReadDir(filepath.Join(daemonRoot.Root(), "t1", "p1", workspaceDirName))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "task-a1" {
			t.Fatalf("a failed spawn left %q behind", e.Name())
		}
	}
}

// TestSpawnSession_NamedWorkspaceAdmission refuses, before anything resolves,
// a workspace combined with a host path, one of another tenant, and a daemon
// with no provisioner for an in-process spawn.
func TestSpawnSession_NamedWorkspaceAdmission(t *testing.T) {
	t.Parallel()
	t.Run("work dir and workspace", func(t *testing.T) {
		t.Parallel()
		impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), nil)
		req := workspaceSpawn()
		req.WorkDir = "/srv/elsewhere"
		var invalid *SessionWorkspaceInvalidError
		if _, err := impl.SpawnSession(tenantCtx("t1"), req); !errors.As(err, &invalid) || invalid.Field != "work_dir" {
			t.Fatalf("got %v, want a work_dir refusal", err)
		}
	})
	t.Run("another tenant", func(t *testing.T) {
		t.Parallel()
		impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), nil)
		var tenant *SessionWorkspaceTenantError
		_, err := impl.SpawnSession(ContextWithTenant(context.Background(), "t2"), workspaceSpawn())
		if !errors.As(err, &tenant) || tenant.SpawnTenant != "t2" || tenant.WorkspaceTenant != "t1" {
			t.Fatalf("got %v, want *SessionWorkspaceTenantError", err)
		}
	})
	t.Run("invalid name", func(t *testing.T) {
		t.Parallel()
		impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), nil)
		req := workspaceSpawn()
		req.Workspace.Key = "../escape"
		var invalid *SessionWorkspaceInvalidError
		if _, err := impl.SpawnSession(tenantCtx("t1"), req); !errors.As(err, &invalid) {
			t.Fatalf("got %v, want *SessionWorkspaceInvalidError", err)
		}
	})
	t.Run("no daemon provisioner", func(t *testing.T) {
		t.Parallel()
		local := newWorkspaceBackend("local", "", nil)
		svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{"codex": local}, &agentTestLogger{})
		if err != nil {
			t.Fatal(err)
		}
		var unsupported *SessionWorkspaceUnsupportedError
		if _, err := svc.SpawnSession(tenantCtx("t1"), workspaceSpawn()); !errors.As(err, &unsupported) {
			t.Fatalf("got %v, want *SessionWorkspaceUnsupportedError", err)
		}
		if local.spawnCount() != 0 {
			t.Fatal("nothing may be spawned without a workspace")
		}
	})
}

// TestJobRun_PinsLaterSessionsToTheWorkspaceHost drives the whole contract
// through the envelope every runner spawns through: the job's first session
// lands wherever the dispatcher picks, and every later one — a round, a
// rolled-over session — carries the pin and lands on the same agent, in the
// same directory, even though the dispatcher would now pick another.
func TestJobRun_PinsLaterSessionsToTheWorkspaceHost(t *testing.T) {
	t.Parallel()
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	vs04 := newWorkspaceBackend("vs04", "vs04:9092", newTestWorkspaceManager(t))
	f := &fleet{agents: []*workspaceBackend{agent09, vs04}}
	impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), f)
	run := NewJobRun(JobRunConfig{
		JobID:  "job-981",
		Config: &JobConfig{TenantID: "t1", ProjectID: "p1"},
		Agents: impl,
		Logger: &agentTestLogger{},
	})

	first, err := run.Spawn(tenantCtx("t1"), workspaceSpawn())
	if err != nil {
		t.Fatalf("first spawn: %v", err)
	}
	pin := run.WorkspacePin()
	if pin == nil || pin.AgentHost != "agent-09:9091" || pin.Dir != first.Workspace.Dir {
		t.Fatalf("pin = %+v, want agent09 at %s", pin, first.Workspace.Dir)
	}

	// The dispatcher's preference moves; the job does not.
	f.mu.Lock()
	f.agents = []*workspaceBackend{vs04, agent09}
	f.mu.Unlock()
	second, err := run.Spawn(tenantCtx("t1"), workspaceSpawn())
	if err != nil {
		t.Fatalf("second spawn: %v", err)
	}
	if agent09.spawnCount() != 2 || vs04.spawnCount() != 0 || second.Workspace.Dir != first.Workspace.Dir {
		t.Fatalf("the second session must continue on agent09: agent09=%d vs04=%d", agent09.spawnCount(), vs04.spawnCount())
	}

	if err := run.SetState(context.Background(), JobStateExecuting); err != nil {
		t.Fatal(err)
	}
	if got := run.Status().WorkspacePin; got == nil || *got != *pin {
		t.Fatalf("the published status must carry the pin: %+v", got)
	}
}

// TestJobRun_ResumedRunKeepsItsPin: a job resumed after a restart respawns on
// the host its persisted status names — and is refused, not re-homed, when that
// host is gone.
func TestJobRun_ResumedRunKeepsItsPin(t *testing.T) {
	t.Parallel()
	agent09Root := newTestWorkspaceManager(t)
	checkout, err := agent09Root.ProvisionSession(namedWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	checkout.Release()
	pin := &WorkspacePlacement{AgentHost: "agent-09:9091", Key: "task-a1", Dir: checkout.Dir}

	resumed := func(f *fleet) (*JobRun, *workspaceBackend) {
		local := newWorkspaceBackend("local", "", nil)
		impl, _ := workspaceSvc(t, local, f)
		return NewJobRun(JobRunConfig{
			JobID:   "job-981",
			Config:  &JobConfig{TenantID: "t1", ProjectID: "p1"},
			Agents:  impl,
			Logger:  &agentTestLogger{},
			Resumed: &JobStatus{JobID: "job-981", State: JobStateExecuting, WorkspacePin: pin},
		}), local
	}

	t.Run("agent present", func(t *testing.T) {
		t.Parallel()
		agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", agent09Root)
		vs04 := newWorkspaceBackend("vs04", "vs04:9092", newTestWorkspaceManager(t))
		run, _ := resumed(&fleet{agents: []*workspaceBackend{vs04, agent09}})
		resp, err := run.Spawn(tenantCtx("t1"), workspaceSpawn())
		if err != nil {
			t.Fatalf("resumed spawn: %v", err)
		}
		if agent09.spawnCount() != 1 || resp.Workspace.Dir != checkout.Dir {
			t.Fatalf("the resumed job must respawn where its checkout is: %+v", resp.Workspace)
		}
	})
	t.Run("agent gone", func(t *testing.T) {
		t.Parallel()
		run, local := resumed(&fleet{offline: true})
		_, err := run.Spawn(tenantCtx("t1"), workspaceSpawn())
		var gone *WorkspaceFleetNodeUnavailableError
		if !errors.As(err, &gone) || local.spawnCount() != 0 {
			t.Fatalf("got %v (local spawns %d), want a refusal and no fresh clone", err, local.spawnCount())
		}
	})
}

// TestSpawnSession_APinnedAgentThatDiesAfterResolvingIsTheWorkspaceRefusal:
// the pinned agent's heartbeat is still fresh, so it resolves — and then the
// spawn RPC never reaches it. For a pinned spawn that is the pinned host being
// gone: WorkspaceFleetNodeUnavailableError, which parks the job, and NOT the
// transient unreachability that would requeue it onto a fresh clone elsewhere.
// An unpinned spawn keeps the ordinary refusal.
func TestSpawnSession_APinnedAgentThatDiesAfterResolvingIsTheWorkspaceRefusal(t *testing.T) {
	t.Parallel()
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	agent09.spawnErr = &RemoteSpawnFaultError{Op: "Spawn", Sentinel: ErrFleetNodeUnreachable, Err: errors.New("connection refused")}
	impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), &fleet{agents: []*workspaceBackend{agent09}})

	pinned := workspaceSpawn()
	pinned.Workspace.Pin = &WorkspacePlacement{AgentHost: "agent-09:9091", Key: "task-a1", Dir: "/agent-09/p"}
	_, err := impl.SpawnSession(tenantCtx("t1"), pinned)
	var gone *WorkspaceFleetNodeUnavailableError
	if !errors.As(err, &gone) || gone.AgentHost != "agent-09:9091" || gone.ResolvedHost != "agent-09:9091" {
		t.Fatalf("err = %v, want *WorkspaceFleetNodeUnavailableError for agent09", err)
	}
	if errors.Is(err, ErrFleetNodeUnreachable) {
		t.Fatal("the refusal must not classify as the transient unreachability it was observed as")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("the refusal must carry what showed the agent gone: %v", err)
	}

	_, err = impl.SpawnSession(tenantCtx("t1"), workspaceSpawn())
	if !errors.Is(err, ErrFleetNodeUnreachable) || errors.Is(err, ErrSessionWorkspace) {
		t.Fatalf("unpinned: %v, want the ordinary unreachable refusal", err)
	}
}

// TestCloseSession_ARemoteSessionWhoseHostCannotBeReachedIsNotConcluded is the
// round-5 regression: an agent that misses heartbeats stops resolving, and the
// close used to fall back to the local backend, whose session-not-found was
// read as a completed teardown — the record went CLOSED while the container
// and its forge token lived on. Now the close fails typed, the local backend
// is never asked, the record stays ACTIVE, and once the agent answers again
// the close reaches it and succeeds.
func TestCloseSession_ARemoteSessionWhoseHostCannotBeReachedIsNotConcluded(t *testing.T) {
	t.Parallel()
	local := newWorkspaceBackend("local", "", nil)
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	f := &fleet{agents: []*workspaceBackend{agent09}}
	impl, _ := workspaceSvc(t, local, f)

	resp, err := impl.SpawnSession(tenantCtx("t1"), workspaceSpawn())
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	f.mu.Lock()
	f.offline = true
	f.mu.Unlock()
	_, err = impl.CloseSession(tenantCtx("t1"), &CloseSessionRequest{SessionID: resp.SessionID})
	var unavailable *SessionHostUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Host != "agent-09:9091" || unavailable.SessionID != resp.SessionID {
		t.Fatalf("err = %v, want *SessionHostUnavailableError naming agent09", err)
	}
	local.mu.Lock()
	localCloses := len(local.closed)
	local.mu.Unlock()
	if localCloses != 0 {
		t.Fatal("the close was routed to the local backend, which never held the session")
	}
	record, err := impl.store.GetSession(tenantCtx("t1"), "t1", resp.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status == SessionStateClosed {
		t.Fatal("the session was marked closed while its agent could not be reached")
	}

	f.mu.Lock()
	f.offline = false
	f.mu.Unlock()
	if _, err := impl.CloseSession(tenantCtx("t1"), &CloseSessionRequest{SessionID: resp.SessionID}); err != nil {
		t.Fatalf("close once the agent answers: %v", err)
	}
	agent09.mu.Lock()
	closed := len(agent09.closed)
	agent09.mu.Unlock()
	if closed != 1 {
		t.Fatalf("agent-09 closes = %d, want the one that reached it", closed)
	}
}

// TestSpawnSession_PlacementIsMarkedOnTheResolverContext: a spawn resolves
// with the placement mark (so the fleet may pass over a quiet agent), and an
// existing session's operation resolves without it (so it still reaches the
// agent that holds it).
func TestSpawnSession_PlacementIsMarkedOnTheResolverContext(t *testing.T) {
	t.Parallel()
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	var marks []bool
	impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), nil)
	impl.SetRemoteBackendResolver(func(ctx context.Context, _, _ string) (AgentBackend, bool) {
		marks = append(marks, SpawnPlacementFromContext(ctx))
		return agent09, true
	})
	resp, err := impl.SpawnSession(tenantCtx("t1"), workspaceSpawn())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := impl.CloseSession(tenantCtx("t1"), &CloseSessionRequest{SessionID: resp.SessionID}); err != nil {
		t.Fatal(err)
	}
	if len(marks) < 2 || !marks[0] || marks[len(marks)-1] {
		t.Fatalf("placement marks = %v, want the spawn marked and the close not", marks)
	}
	if SpawnPlacementFromContext(nil) { //nolint:staticcheck // nil is the case under test
		t.Fatal("a nil context places nothing")
	}
}

// TestJobRun_TheWorkspacePinIsDurableBeforeTheSessionIsUsed is the round-5
// recovery regression: the daemon dies after the first spawn provisioned the
// workspace and before the job's next status write. The status the store holds
// at that instant — no SetState in between — already names the pin, and a job
// recovered from exactly that record respawns on the pinned agent, or is
// refused when it is gone, never placed afresh elsewhere.
func TestJobRun_TheWorkspacePinIsDurableBeforeTheSessionIsUsed(t *testing.T) {
	t.Parallel()
	agent09Root := newTestWorkspaceManager(t)
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", agent09Root)
	vs04 := newWorkspaceBackend("vs04", "vs04:9092", newTestWorkspaceManager(t))
	impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), &fleet{agents: []*workspaceBackend{agent09, vs04}})

	var mu sync.Mutex
	var stored *JobStatus
	run := NewJobRun(JobRunConfig{
		JobID:  "job-981",
		Config: &JobConfig{TenantID: "t1", ProjectID: "p1"},
		Agents: impl,
		Logger: &agentTestLogger{},
		PersistFn: func(status *JobStatus) {
			mu.Lock()
			defer mu.Unlock()
			cp := *status
			stored = &cp
		},
	})
	run.snapshot(JobStateInitializing) // what the envelope publishes before the runner
	resp, err := run.Spawn(tenantCtx("t1"), workspaceSpawn())
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// THE CRASH: nothing else is written. The store holds what it holds.
	mu.Lock()
	crashed := stored
	mu.Unlock()
	if crashed == nil || crashed.WorkspacePin == nil || *crashed.WorkspacePin != *resp.Workspace {
		t.Fatalf("persisted pin = %+v, want %+v before any further status write", crashed, resp.Workspace)
	}
	if crashed.State != JobStateInitializing {
		t.Fatalf("persisted state = %q; the pin must not have advanced the job", crashed.State)
	}

	recoverJob := func(f *fleet) (*JobRun, *workspaceBackend) {
		local := newWorkspaceBackend("local", "", nil)
		svc, _ := workspaceSvc(t, local, f)
		return NewJobRun(JobRunConfig{
			JobID: "job-981", Config: &JobConfig{TenantID: "t1", ProjectID: "p1"},
			Agents: svc, Logger: &agentTestLogger{}, Resumed: crashed,
		}), local
	}

	t.Run("pinned agent present", func(t *testing.T) {
		recovered, _ := recoverJob(&fleet{agents: []*workspaceBackend{vs04, agent09}})
		again, err := recovered.Spawn(tenantCtx("t1"), workspaceSpawn())
		if err != nil {
			t.Fatalf("recovered spawn: %v", err)
		}
		if again.Workspace.AgentHost != "agent-09:9091" || again.Workspace.Dir != resp.Workspace.Dir {
			t.Fatalf("recovered on %+v, want agent09 at %s", again.Workspace, resp.Workspace.Dir)
		}
	})
	t.Run("pinned agent gone", func(t *testing.T) {
		recovered, local := recoverJob(&fleet{agents: []*workspaceBackend{vs04}})
		_, err := recovered.Spawn(tenantCtx("t1"), workspaceSpawn())
		var gone *WorkspaceFleetNodeUnavailableError
		if !errors.As(err, &gone) || local.spawnCount() != 0 {
			t.Fatalf("err = %v, want a refusal and no placement elsewhere", err)
		}
	})
}

// TestJobRun_ALaterPlacementIsNotPersistedAgain: the first placement wins, so
// a later session of the job writes no second pin.
func TestJobRun_ALaterPlacementIsNotPersistedAgain(t *testing.T) {
	t.Parallel()
	writes := 0
	run := NewJobRun(JobRunConfig{
		JobID: "job-pin", Config: &JobConfig{TenantID: "t1"}, Logger: &agentTestLogger{},
		PersistFn: func(*JobStatus) { writes++ },
	})
	run.recordWorkspace(WorkspacePlacement{AgentHost: "a", Key: "k", Dir: "/d"}) // nothing published yet
	if writes != 0 || run.WorkspacePin() == nil {
		t.Fatalf("writes = %d pin = %v; with no published status the pin is held for the next snapshot", writes, run.WorkspacePin())
	}
	run.snapshot(JobStateInitializing)
	writes = 0
	run.recordWorkspace(WorkspacePlacement{AgentHost: "b", Key: "k", Dir: "/e"})
	if writes != 0 || run.WorkspacePin().AgentHost != "a" {
		t.Fatalf("a later placement rewrote the pin: writes=%d pin=%+v", writes, run.WorkspacePin())
	}
}

// TestExecSession_ARemoteSessionRunsOnTheAgentThatOwnsItsContainer is round
// 6's P1: a review's checkout runs through JobRun.Exec, and for a session on a
// fleet node it must run in that agent's container runtime — the daemon's
// own runtime does not know the container and must never be asked.
func TestExecSession_ARemoteSessionRunsOnTheAgentThatOwnsItsContainer(t *testing.T) {
	t.Parallel()
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	f := &fleet{agents: []*workspaceBackend{agent09}}
	impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), f)
	daemonRuntime := &mockContainerRuntime{available: true}
	impl.SetContainerRuntime(daemonRuntime)
	run := NewJobRun(JobRunConfig{
		JobID: "job-exec", Config: &JobConfig{TenantID: "t1", ProjectID: "p1"},
		Agents: impl, Logger: &agentTestLogger{},
	})

	resp, err := run.Spawn(tenantCtx("t1"), workspaceSpawn())
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	result, err := run.Exec(tenantCtx("t1"), &ExecSessionRequest{
		SessionID: resp.SessionID, Spec: &ExecSpec{Cmd: []string{"git", "clone", "repo"}},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.Stdout != "agent-09" || len(agent09.execs) != 1 || agent09.execs[0] != resp.SessionID+":git clone repo" {
		t.Fatalf("the checkout did not run on agent09: result=%+v execs=%v", result, agent09.execs)
	}
	daemonRuntime.mu.Lock()
	daemonExecs := len(daemonRuntime.execCalls)
	daemonRuntime.mu.Unlock()
	if daemonExecs != 0 {
		t.Fatal("the daemon's own container runtime was asked for a remote session's container")
	}

	// ANOTHER TENANT'S SESSION IS NOT FOUND, never run.
	var notFound *SessionNotFoundError
	if _, err := impl.ExecSession(tenantCtx("t2"), &ExecSessionRequest{
		SessionID: resp.SessionID, Spec: &ExecSpec{Cmd: []string{"id"}},
	}); !errors.As(err, &notFound) {
		t.Fatalf("another tenant's exec: %v, want SessionNotFoundError", err)
	}

	// AN AGENT THAT CANNOT BE REACHED IS A REFUSAL, never the daemon's runtime.
	f.mu.Lock()
	f.offline = true
	f.mu.Unlock()
	var unavailable *SessionHostUnavailableError
	if _, err := impl.ExecSession(tenantCtx("t1"), &ExecSessionRequest{
		SessionID: resp.SessionID, Spec: &ExecSpec{Cmd: []string{"git", "status"}},
	}); !errors.As(err, &unavailable) {
		t.Fatalf("exec with the agent unreachable: %v, want SessionHostUnavailableError", err)
	}
	daemonRuntime.mu.Lock()
	defer daemonRuntime.mu.Unlock()
	if len(daemonRuntime.execCalls) != 0 {
		t.Fatal("an unreachable agent's exec fell back to the daemon's runtime")
	}
}

// TestJobRun_APinThatCannotBeStoredHandsOutNoSession is round 6's P2: the
// first pin is written strictly. A store that refuses it gets the session
// closed on the agent and a workspace refusal returned — which parks the job —
// and the run holds no pin a later status could publish, so no recovery can
// be told of a placement no durable record made.
func TestJobRun_APinThatCannotBeStoredHandsOutNoSession(t *testing.T) {
	t.Parallel()
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), &fleet{agents: []*workspaceBackend{agent09}})
	storeFailure := errors.New("kv: quorum lost")
	var stored []*JobStatus
	run := NewJobRun(JobRunConfig{
		JobID: "job-strict", Config: &JobConfig{TenantID: "t1", ProjectID: "p1"},
		Agents: impl, Logger: &agentTestLogger{},
		StoreFn: func(status *JobStatus) error {
			if status.WorkspacePin != nil {
				return storeFailure
			}
			cp := *status
			stored = append(stored, &cp)
			return nil
		},
		PersistFn: func(status *JobStatus) {
			cp := *status
			stored = append(stored, &cp)
		},
	})
	run.snapshot(JobStateInitializing)

	resp, err := run.Spawn(tenantCtx("t1"), workspaceSpawn())
	var persist *WorkspacePinPersistError
	if !errors.As(err, &persist) || resp != nil || !errors.Is(err, ErrSessionWorkspace) || !errors.Is(err, storeFailure) {
		t.Fatalf("Spawn = %+v, %v; want *WorkspacePinPersistError and no session", resp, err)
	}
	agent09.mu.Lock()
	closed := len(agent09.closed)
	agent09.mu.Unlock()
	if closed != 1 {
		t.Fatalf("the spawned session was not closed: closes=%d", closed)
	}
	if run.WorkspacePin() != nil || run.Status().WorkspacePin != nil {
		t.Fatal("the run still claims a placement the store refused")
	}
	run.snapshot(JobStateEscalating)
	for _, status := range stored {
		if status.WorkspacePin != nil {
			t.Fatalf("a status naming the refused pin was written: %+v", status.WorkspacePin)
		}
	}
}
