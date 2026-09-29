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
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Agent-local workspaces (go-quicbaas#981)
// =============================================================================
//
// A remote fleet node refused every sdlc and review spawn because the spawn
// bind-mounted a directory the CONTROL PLANE had provisioned on its own host.
// The spawn now names the workspace and this daemon provisions it under its own
// projects root. These tests drive the runtime and the wire with the same
// mount policy `fleet serve` builds (Config.MountPolicy) — so they also prove
// the projects root needs no --allowed-mount-root entry of its own.

// workspaceRuntimeFixture is a runtime over a real WorkspaceManager, with the
// mount policy an `fleet serve` daemon builds from its configuration.
type workspaceRuntimeFixture struct {
	runtime *Runtime
	backend *spawnBackend
	root    string

	// received is every spawn request the backend was handed.
	received chan *orchestration.SpawnRequest
}

func newWorkspaceRuntimeFixture(t *testing.T, withWorkspaces bool) *workspaceRuntimeFixture {
	t.Helper()
	cfg := &Config{WorkDir: t.TempDir()}
	f := &workspaceRuntimeFixture{root: cfg.EffectiveProjectsRoot(), received: make(chan *orchestration.SpawnRequest, 4)}
	f.backend = &spawnBackend{spawnFn: func(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
		f.received <- req
		return &orchestration.Session{
			ID: "sess-ws", Backend: "acp", Status: orchestration.SessionStateActive,
			WorkDir: "/echoed/by/backend", CreatedAt: time.Now().UnixNano(),
		}, nil
	}}
	rc := RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": f.backend},
		SandboxPolicy:    orchestration.SandboxPolicyRequired,
		ContainerRuntime: &fakeContainerRuntime{available: true},
		ImageMap:         map[string]string{"acp": "quicbaas/acp:test"},
		Logger:           &testLogger{t: t},
		MountPolicy:      cfg.MountPolicy(),
	}
	if withWorkspaces {
		manager, err := orchestration.NewWorkspaceManager(f.root)
		if err != nil {
			t.Fatalf("NewWorkspaceManager: %v", err)
		}
		rc.Workspaces = manager
	}
	rt, err := NewRuntime(rc)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	f.runtime = rt
	return f
}

// agentWorkspace is a named workspace of the fixture identity's tenant.
func agentWorkspace() *orchestration.SessionWorkspace {
	return &orchestration.SessionWorkspace{
		TenantID:  "tenant-a",
		ProjectID: "proj-1",
		Key:       "task-a1",
		Spec: &orchestration.WorkspaceSpecFiles{Files: map[string][]byte{
			orchestration.WorkspaceSpecFile:       []byte(`{}`),
			orchestration.WorkspaceForgeTokenFile: []byte("forge-secret"),
		}},
	}
}

// specMountSource is the source of the spec directory's mount, "" when none.
func specMountSource(req *orchestration.SpawnRequest) string {
	for _, m := range req.ExtraMounts {
		if m.Target == orchestration.WorkspaceSpecDir {
			return m.Source
		}
	}
	return ""
}

// TestRuntime_Spawn_ProvisionsTheNamedWorkspaceUnderItsOwnRoot: the daemon
// makes the workspace under its projects root, hands the backend that
// directory and the spec mount — both admitted by the projects-root grant
// alone — and reports the directory as the session's, whatever the backend
// echoed. Close releases the spec and keeps the checkout.
func TestRuntime_Spawn_ProvisionsTheNamedWorkspaceUnderItsOwnRoot(t *testing.T) {
	t.Parallel()
	f := newWorkspaceRuntimeFixture(t, true)

	sess, err := f.runtime.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{Workspace: agentWorkspace()})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	want := filepath.Join(f.root, "tenant-a", "proj-1", "workspace", "task-a1")
	got := <-f.received
	if got.WorkDir != want {
		t.Fatalf("backend work dir = %q, want %q", got.WorkDir, want)
	}
	specDir := specMountSource(got)
	if specDir == "" || filepath.Dir(specDir) != filepath.Dir(want) {
		t.Fatalf("spec mount = %q, want one beside the workspace", specDir)
	}
	if sess.WorkDir != want {
		t.Fatalf("session work dir = %q, want the provisioned %q", sess.WorkDir, want)
	}
	if token, err := os.ReadFile(filepath.Join(specDir, orchestration.WorkspaceForgeTokenFile)); err != nil ||
		string(token) != "forge-secret" {
		t.Fatalf("token = %q, %v", token, err)
	}

	if err := f.runtime.Close(context.Background(), "acp", sess.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(specDir); !os.IsNotExist(err) {
		t.Fatalf("Close must release the spec directory: %v", err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("the checkout must survive the close: %v", err)
	}
}

// TestRuntime_Spawn_RefusesAWorkspaceItCannotProvision covers every refusal,
// each before the backend is reached and each leaving no spec behind.
func TestRuntime_Spawn_RefusesAWorkspaceItCannotProvision(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		withWorkspaces bool
		tenant         string
		mutate         func(*orchestration.SpawnRequest)
		check          func(*testing.T, error)
	}{
		"another tenant's workspace": {
			withWorkspaces: true, tenant: "tenant-b",
			check: func(t *testing.T, err error) {
				var tenant *orchestration.SessionWorkspaceTenantError
				if !errors.As(err, &tenant) || tenant.SpawnTenant != "tenant-b" {
					t.Fatalf("got %v, want *SessionWorkspaceTenantError", err)
				}
			},
		},
		"no projects root": {
			withWorkspaces: false, tenant: "tenant-a",
			check: func(t *testing.T, err error) {
				var unsupported *orchestration.SessionWorkspaceUnsupportedError
				if !errors.As(err, &unsupported) {
					t.Fatalf("got %v, want *SessionWorkspaceUnsupportedError", err)
				}
			},
		},
		"a work dir as well": {
			withWorkspaces: true, tenant: "tenant-a",
			mutate: func(r *orchestration.SpawnRequest) { r.WorkDir = "/srv/elsewhere" },
			check: func(t *testing.T, err error) {
				var invalid *orchestration.SessionWorkspaceInvalidError
				if !errors.As(err, &invalid) || invalid.Field != "work_dir" {
					t.Fatalf("got %v, want a work_dir refusal", err)
				}
			},
		},
		"a stale pin": {
			withWorkspaces: true, tenant: "tenant-a",
			mutate: func(r *orchestration.SpawnRequest) {
				r.Workspace.Pin = &orchestration.WorkspacePlacement{Key: "task-a1", Dir: "/moved/elsewhere"}
			},
			check: func(t *testing.T, err error) {
				var stale *orchestration.WorkspaceStaleError
				if !errors.As(err, &stale) {
					t.Fatalf("got %v, want *WorkspaceStaleError", err)
				}
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newWorkspaceRuntimeFixture(t, tc.withWorkspaces)
			req := &orchestration.SpawnRequest{Workspace: agentWorkspace()}
			if tc.mutate != nil {
				tc.mutate(req)
			}
			_, err := f.runtime.Spawn(context.Background(), tc.tenant, nil, "acp", req)
			if !errors.Is(err, orchestration.ErrSessionWorkspace) {
				t.Fatalf("err = %v, want a session-workspace refusal", err)
			}
			tc.check(t, err)
			if f.backend.spawnCalls.Load() != 0 {
				t.Fatal("a refused workspace reached the backend")
			}
		})
	}
}

// TestRuntime_Spawn_AFailedSpawnReleasesTheSpec: a backend that refuses after
// the spec was written leaves no forge token on disk.
func TestRuntime_Spawn_AFailedSpawnReleasesTheSpec(t *testing.T) {
	t.Parallel()
	f := newWorkspaceRuntimeFixture(t, true)
	var specDir string
	f.backend.spawnFn = func(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
		specDir = specMountSource(req)
		return nil, errors.New("container refused")
	}
	if _, err := f.runtime.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{Workspace: agentWorkspace()}); err == nil {
		t.Fatal("the spawn must fail")
	}
	if specDir == "" {
		t.Fatal("the backend was not handed the spec")
	}
	if _, err := os.Stat(specDir); !os.IsNotExist(err) {
		t.Fatalf("a failed spawn left its spec directory: %v", err)
	}
}

// TestSpawnWireContract_ANamedWorkspaceIsProvisionedOnTheDaemon is the round
// trip the field-equality contract cannot express: a named workspace crosses a
// real RemoteACPBackend and GRPCSpawnServer, the DAEMON provisions it under its
// own root, and the control plane's session carries the daemon's directory. A
// refusal crosses back typed.
func TestSpawnWireContract_ANamedWorkspaceIsProvisionedOnTheDaemon(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "projects")
	manager, err := orchestration.NewWorkspaceManager(root)
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan *orchestration.SpawnRequest, 2)
	backend := &spawnBackend{spawnFn: func(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
		received <- req
		return &orchestration.Session{ID: "sess-wire", Backend: "acp", WorkDir: req.WorkDir, CreatedAt: time.Now().UnixNano()}, nil
	}}
	remote, cleanup := newContractBoundary(t, backend, &orchestration.MountPolicy{ProjectsRoot: root},
		&fakeContainerRuntime{available: true}, manager)
	defer cleanup()

	sess, err := remote.Spawn(context.Background(), &orchestration.SpawnRequest{
		ProjectID: "proj-1", SandboxMode: orchestration.SandboxModeRequired,
		ContainerImage: "quicbaas/acp:test", Workspace: agentWorkspace(),
	})
	if err != nil {
		t.Fatalf("remote spawn: %v", err)
	}
	want := filepath.Join(root, "tenant-a", "proj-1", "workspace", "task-a1")
	got := <-received
	if got.WorkDir != want || specMountSource(got) == "" {
		t.Fatalf("daemon backend got work_dir %q spec %q", got.WorkDir, specMountSource(got))
	}
	if sess.WorkDir != want {
		t.Fatalf("the control plane's session names %q, want the daemon's %q", sess.WorkDir, want)
	}

	// A projects root the daemon cannot provision under — a file where the
	// root should be, which fails for every uid — comes back as a workspace
	// fault too, not an internal error a runner would charge.
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	blockedManager, err := orchestration.NewWorkspaceManager(filepath.Join(blocked, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	blockedRemote, blockedCleanup := newContractBoundary(t, backend, &orchestration.MountPolicy{ProjectsRoot: root},
		&fakeContainerRuntime{available: true}, blockedManager)
	defer blockedCleanup()
	_, err = blockedRemote.Spawn(context.Background(), &orchestration.SpawnRequest{
		ProjectID: "proj-1", SandboxMode: orchestration.SandboxModeRequired,
		ContainerImage: "quicbaas/acp:test", Workspace: agentWorkspace(),
	})
	if !errors.Is(err, orchestration.ErrSessionWorkspace) {
		t.Fatalf("a filesystem failure across the wire: %v, want ErrSessionWorkspace", err)
	}

	// A pin the daemon cannot honour comes back as the typed refusal it is.
	stale := agentWorkspace()
	stale.Pin = &orchestration.WorkspacePlacement{AgentHost: "bufnet", Key: "task-a1", Dir: "/not/here"}
	_, err = remote.Spawn(context.Background(), &orchestration.SpawnRequest{
		ProjectID: "proj-1", SandboxMode: orchestration.SandboxModeRequired,
		ContainerImage: "quicbaas/acp:test", Workspace: stale,
	})
	if !errors.Is(err, orchestration.ErrSessionWorkspace) {
		t.Fatalf("err = %v, want ErrSessionWorkspace across the wire", err)
	}
}

// TestConfig_ProjectsRoot defaults under the work directory, honours an
// explicit root, refuses a relative one, and is granted tenant-scoped by the
// mount policy.
func TestConfig_ProjectsRoot(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	if got, want := cfg.EffectiveProjectsRoot(), filepath.Join(cfg.WorkDir, "projects"); got != want {
		t.Fatalf("default = %q, want %q", got, want)
	}
	cfg.ProjectsRoot = "/srv/quicbaas/projects/"
	if got := cfg.EffectiveProjectsRoot(); got != "/srv/quicbaas/projects" {
		t.Fatalf("explicit = %q", got)
	}
	policy := cfg.MountPolicy()
	if policy.ProjectsRoot != "/srv/quicbaas/projects" {
		t.Fatalf("the mount policy must grant the projects root: %+v", policy)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	cfg.ProjectsRoot = "relative/projects"
	if err := cfg.Validate(); !errors.Is(err, ErrConfigProjectsRootNotAbsolute) {
		t.Fatalf("a relative projects root: %v", err)
	}
	cfg.ProjectsRoot = ""
	cfg.WorkDir = "relative/work"
	if err := cfg.Validate(); !errors.Is(err, ErrConfigProjectsRootNotAbsolute) {
		t.Fatalf("a projects root derived from a relative work dir: %v", err)
	}
}

// TestRuntime_ARestartLeavesNoForgeCredentialOnDisk is the regression for the
// review of go-quicbaas#981: a daemon that dies while a session holds a spec
// directory must not leave that session's forge token on disk. The session
// state it persists names the workspace and nothing more; the startup sweep
// removes every spec directory; the checkout — what the job's next session is
// pinned to — survives. The token is also gone from the spawn request the
// moment it was written.
func TestRuntime_ARestartLeavesNoForgeCredentialOnDisk(t *testing.T) {
	t.Parallel()
	const token = "forge-secret-7f3c9a"
	workDir := t.TempDir()
	projects := filepath.Join(workDir, "projects")
	newRuntime := func() *Runtime {
		manager, err := orchestration.NewWorkspaceManager(projects)
		if err != nil {
			t.Fatal(err)
		}
		rt, err := NewRuntime(RuntimeConfig{
			Backends:         map[string]orchestration.AgentBackend{"acp": &spawnBackend{}},
			SandboxPolicy:    orchestration.SandboxPolicyRequired,
			ContainerRuntime: &fakeContainerRuntime{available: true},
			ImageMap:         map[string]string{"acp": "quicbaas/acp:test"},
			Logger:           &testLogger{t: t},
			WorkDir:          workDir,
			MountPolicy:      &orchestration.MountPolicy{ProjectsRoot: projects},
			Workspaces:       manager,
		})
		if err != nil {
			t.Fatal(err)
		}
		return rt
	}

	before := newRuntime()
	ws := agentWorkspace()
	ws.Spec.Files[orchestration.WorkspaceForgeTokenFile] = []byte(token)
	req := &orchestration.SpawnRequest{Workspace: ws}
	sess, err := before.Spawn(context.Background(), "tenant-a", nil, "acp", req)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(ws.Spec.Files) != 0 {
		t.Fatal("the spawn request still holds the spec after it was written")
	}
	specDir := specMountSource(req)
	if raw, err := os.ReadFile(filepath.Join(specDir, orchestration.WorkspaceForgeTokenFile)); err != nil || string(raw) != token {
		t.Fatalf("the live session's token = %q, %v", raw, err)
	}

	// The daemon dies: no Close. A fresh runtime over the same state starts.
	after := newRuntime()
	removed, err := after.SweepOrphanWorkspaceSpecs()
	if err != nil {
		t.Fatalf("SweepOrphanWorkspaceSpecs: %v", err)
	}
	if len(removed) != 1 || removed[0] != specDir {
		t.Fatalf("swept %v, want the dead session's spec %s", removed, specDir)
	}
	if _, err := os.Stat(sess.WorkDir); err != nil {
		t.Fatalf("the checkout the job is pinned to did not survive: %v", err)
	}
	persisted, err := after.LoadPersistedSessions(context.Background())
	if err != nil || len(persisted) != 1 || persisted[0].WorkspacePath != sess.WorkDir {
		t.Fatalf("persisted sessions = %+v, %v; want the workspace, re-attachable", persisted, err)
	}

	// NO CREDENTIAL MATERIAL ANYWHERE THIS DAEMON KEEPS STATE.
	walkErr := filepath.WalkDir(workDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return walkErr
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(raw), token) {
			t.Errorf("%s holds the forge token after the restart", path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}

	// A daemon that provisions no workspaces has nothing to sweep.
	if got, err := newWorkspaceRuntimeFixture(t, false).runtime.SweepOrphanWorkspaceSpecs(); err != nil || got != nil {
		t.Fatalf("sweep without workspaces = %v, %v", got, err)
	}
}
