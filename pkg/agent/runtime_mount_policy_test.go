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
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// recordingBackend records the spawn requests it is handed so a test can
// assert that a refused spawn never reached the backend — the point of
// authorizing mounts BEFORE any side effect.
type recordingBackend struct {
	spawns []*orchestration.SpawnRequest
}

func (b *recordingBackend) Spawn(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	b.spawns = append(b.spawns, req)
	return &orchestration.Session{ID: "sess-recorded", WorkDir: req.WorkDir}, nil
}

func (b *recordingBackend) Prompt(_ context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return &orchestration.PromptResponse{SessionID: req.SessionID}, nil
}

func (b *recordingBackend) Status(_ context.Context, id string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{SessionID: id}, nil
}

func (b *recordingBackend) Close(_ context.Context, _ string) error { return nil }

func (b *recordingBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{Kind: orchestration.BackendKindAgent}
}

func (b *recordingBackend) Type() string         { return "recording" }
func (b *recordingBackend) ToolGuidance() string { return "" }

// mountPolicyRuntimeFixture builds a daemon runtime whose sandbox policy
// containerizes every spawn, plus an allowed root and a sibling directory
// outside it.
type mountPolicyRuntimeFixture struct {
	runtime *Runtime
	backend *recordingBackend

	// allowedDir is inside the daemon's configured allowlist.
	allowedDir string

	// outsideDir exists on disk but is under no configured root.
	outsideDir string
}

func newMountPolicyRuntimeFixture(t *testing.T, roots []string) mountPolicyRuntimeFixture {
	t.Helper()

	base := t.TempDir()
	allowedRoot := filepath.Join(base, "allowed")
	allowedDir := filepath.Join(allowedRoot, "workspace")
	outsideDir := filepath.Join(base, "outside")
	for _, dir := range []string{allowedDir, outsideDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	// A nil roots slice means "use the fixture's own allowed root"; an
	// explicitly empty slice means "configure no roots at all".
	if roots == nil {
		roots = []string{allowedRoot}
	}

	backend := &recordingBackend{}
	cfg := RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": backend},
		SandboxPolicy:    orchestration.SandboxPolicyRequired,
		ContainerRuntime: &fakeContainerRuntime{available: true},
		ImageMap:         map[string]string{"acp": "quicbaas/acp:test"},
		Logger:           &testLogger{t: t},
		MountPolicy:      &orchestration.MountPolicy{AllowedRoots: roots},
	}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	return mountPolicyRuntimeFixture{
		runtime:    rt,
		backend:    backend,
		allowedDir: allowedDir,
		outsideDir: outsideDir,
	}
}

// TestRuntime_Spawn_WireWorkDirOutsideAllowlistDenied is the daemon-side
// counterpart of the control plane's work_dir authorization. A principal
// holding only AgentScopeRuntimeSpawn used to be able to name ANY host path as
// work_dir and have the daemon bind-mount it read-write into the container as
// the daemon's own uid. The spawn must be refused before the backend is
// reached.
func TestRuntime_Spawn_WireWorkDirOutsideAllowlistDenied(t *testing.T) {
	t.Parallel()

	f := newMountPolicyRuntimeFixture(t, nil)

	_, err := f.runtime.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: f.outsideDir})

	var denied *orchestration.MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError, got %v", err)
	}
	if denied.Field != orchestration.MountFieldWorkDir {
		t.Errorf("field: got %q want %q", denied.Field, orchestration.MountFieldWorkDir)
	}
	if denied.TenantID != "tenant-a" {
		t.Errorf("tenant: got %q want %q", denied.TenantID, "tenant-a")
	}
	if len(f.backend.spawns) != 0 {
		t.Fatalf("backend was reached despite the denial: %d spawn(s)", len(f.backend.spawns))
	}
}

// TestRuntime_Spawn_RootWorkDirDenied is the concrete escalation the policy
// exists to stop: work_dir "/" is host root, read-write, inside the container.
func TestRuntime_Spawn_RootWorkDirDenied(t *testing.T) {
	t.Parallel()

	f := newMountPolicyRuntimeFixture(t, nil)

	_, err := f.runtime.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: "/"})

	var denied *orchestration.MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError for work_dir=/, got %v", err)
	}
	if len(f.backend.spawns) != 0 {
		t.Fatal("backend was reached despite the denial")
	}
}

// TestRuntime_Spawn_DenyByDefaultWithNoPolicy asserts the deny-by-default
// posture: a daemon with no allowlist configured refuses every sandboxed spawn
// that names a work directory. An unconfigured daemon must not be a permissive
// one.
func TestRuntime_Spawn_DenyByDefaultWithNoPolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		roots []string
	}{
		{name: "no policy wired at all", roots: nil},
		{name: "policy with an empty allowlist", roots: []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			backend := &recordingBackend{}
			cfg := RuntimeConfig{
				Backends:         map[string]orchestration.AgentBackend{"acp": backend},
				SandboxPolicy:    orchestration.SandboxPolicyRequired,
				ContainerRuntime: &fakeContainerRuntime{available: true},
				ImageMap:         map[string]string{"acp": "quicbaas/acp:test"},
				Logger:           &testLogger{t: t},
			}
			if tc.roots != nil {
				cfg.MountPolicy = &orchestration.MountPolicy{AllowedRoots: tc.roots}
			}
			rt, err := NewRuntime(cfg)
			if err != nil {
				t.Fatalf("NewRuntime: %v", err)
			}

			_, spawnErr := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
				&orchestration.SpawnRequest{WorkDir: dir})

			var denied *orchestration.MountNotAllowedError
			if !errors.As(spawnErr, &denied) {
				t.Fatalf("expected MountNotAllowedError, got %v", spawnErr)
			}
			if len(backend.spawns) != 0 {
				t.Fatal("backend was reached despite the denial")
			}
		})
	}
}

// TestRuntime_Spawn_AllowedWorkDirPermitted is the positive half: a work
// directory the operator explicitly allowed reaches the backend unchanged.
func TestRuntime_Spawn_AllowedWorkDirPermitted(t *testing.T) {
	t.Parallel()

	f := newMountPolicyRuntimeFixture(t, nil)

	sess, err := f.runtime.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: f.allowedDir})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if sess.WorkDir != f.allowedDir {
		t.Errorf("work dir: got %q want %q", sess.WorkDir, f.allowedDir)
	}
	if len(f.backend.spawns) != 1 {
		t.Fatalf("expected exactly one backend spawn, got %d", len(f.backend.spawns))
	}
}

// TestRuntime_Spawn_SymlinkEscapeDenied covers the resolved-path half of the
// policy. A lexically-permitted work_dir that is a symlink out of the allowed
// root must be refused: an agent with write access to its own workspace can
// plant exactly that link.
func TestRuntime_Spawn_SymlinkEscapeDenied(t *testing.T) {
	t.Parallel()

	f := newMountPolicyRuntimeFixture(t, nil)

	escape := filepath.Join(f.allowedDir, "escape")
	if err := os.Symlink(f.outsideDir, escape); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := f.runtime.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: escape})

	var denied *orchestration.MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError for symlink escape, got %v", err)
	}
	if len(f.backend.spawns) != 0 {
		t.Fatal("backend was reached despite the denial")
	}
}

// TestRuntime_Spawn_NonexistentWorkDirDenied mirrors the control plane: a
// source that does not exist is refused rather than created, because the
// runtime would create it root-owned and its identity cannot be checked.
func TestRuntime_Spawn_NonexistentWorkDirDenied(t *testing.T) {
	t.Parallel()

	f := newMountPolicyRuntimeFixture(t, nil)

	_, err := f.runtime.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: filepath.Join(f.allowedDir, "missing")})

	var denied *orchestration.MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError for a nonexistent source, got %v", err)
	}
}

// TestRuntime_Spawn_ExtraMountsAuthorizedOnEveryPath mirrors the control
// plane's asymmetry: extra mounts exist only to be bind-mounted, so they are
// authorized even under a mode that does not containerize.
func TestRuntime_Spawn_ExtraMountsAuthorizedOnEveryPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	allowedRoot := filepath.Join(base, "allowed")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{allowedRoot, outside} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	backend := &recordingBackend{}
	rt, err := NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"acp": backend},
		SandboxPolicy: orchestration.SandboxPolicyOptIn,
		Logger:        &testLogger{t: t},
		MountPolicy:   &orchestration.MountPolicy{AllowedRoots: []string{allowedRoot}},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	_, spawnErr := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{
			ExtraMounts: []orchestration.ContainerMount{{Source: outside, Target: "/data"}},
		})

	var denied *orchestration.MountNotAllowedError
	if !errors.As(spawnErr, &denied) {
		t.Fatalf("expected MountNotAllowedError, got %v", spawnErr)
	}
	if denied.Field != orchestration.MountFieldExtraMounts {
		t.Errorf("field: got %q want %q", denied.Field, orchestration.MountFieldExtraMounts)
	}
	if len(backend.spawns) != 0 {
		t.Fatal("backend was reached despite the denial")
	}
}

// TestRuntime_Spawn_UnsandboxedWorkDirNotMountChecked keeps the daemon's
// semantics identical to the control plane's: work_dir becomes a host bind
// mount only when the spawn is containerized, so a raw-exec spawn is not
// gated on the mount allowlist.
func TestRuntime_Spawn_UnsandboxedWorkDirNotMountChecked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	backend := &recordingBackend{}
	rt, err := NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"acp": backend},
		SandboxPolicy: orchestration.SandboxPolicyOptIn,
		Logger:        &testLogger{t: t},
		MountPolicy:   &orchestration.MountPolicy{},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if _, spawnErr := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: dir}); spawnErr != nil {
		t.Fatalf("raw-exec spawn must not be mount-gated: %v", spawnErr)
	}
	if len(backend.spawns) != 1 {
		t.Fatalf("expected one backend spawn, got %d", len(backend.spawns))
	}
}

// TestRuntime_Spawn_DaemonPolicyClosesControlPlaneGap covers the reason the
// daemon check is the last line rather than a duplicate of the control
// plane's. A remote spawn whose control-plane-resolved mode is not Required
// skips the control plane's work_dir authorization entirely, yet the daemon's
// own SandboxPolicy still containerizes it. The daemon resolves the mode
// itself and authorizes against THAT.
func TestRuntime_Spawn_DaemonPolicyClosesControlPlaneGap(t *testing.T) {
	t.Parallel()

	f := newMountPolicyRuntimeFixture(t, nil)

	// SandboxModeNone is what a control plane running an opt-in policy would
	// have resolved and forwarded; the daemon's Required policy overrides it.
	_, err := f.runtime.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{
			WorkDir:     f.outsideDir,
			SandboxMode: orchestration.SandboxModeDefault,
		})

	var denied *orchestration.MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError, got %v", err)
	}
	if len(f.backend.spawns) != 0 {
		t.Fatal("backend was reached despite the denial")
	}
}

// TestRuntime_Spawn_TenantScopingIsReal asserts the tenant reaching the policy
// is the caller's, not a placeholder: a daemon whose policy also names a
// projects root grants tenant-a its own subtree and refuses tenant-b's.
func TestRuntime_Spawn_TenantScopingIsReal(t *testing.T) {
	t.Parallel()

	projectsRoot := t.TempDir()
	tenantA := filepath.Join(projectsRoot, "tenant-a", "proj", "workspace")
	tenantB := filepath.Join(projectsRoot, "tenant-b", "proj", "workspace")
	for _, dir := range []string{tenantA, tenantB} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	backend := &recordingBackend{}
	rt, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": backend},
		SandboxPolicy:    orchestration.SandboxPolicyRequired,
		ContainerRuntime: &fakeContainerRuntime{available: true},
		ImageMap:         map[string]string{"acp": "quicbaas/acp:test"},
		Logger:           &testLogger{t: t},
		MountPolicy:      &orchestration.MountPolicy{ProjectsRoot: projectsRoot},
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	if _, spawnErr := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: tenantA}); spawnErr != nil {
		t.Fatalf("tenant-a's own workspace must be allowed: %v", spawnErr)
	}

	_, crossErr := rt.Spawn(context.Background(), "tenant-a", nil, "acp",
		&orchestration.SpawnRequest{WorkDir: tenantB})
	var denied *orchestration.MountNotAllowedError
	if !errors.As(crossErr, &denied) {
		t.Fatalf("expected MountNotAllowedError for a cross-tenant workspace, got %v", crossErr)
	}
}

// TestRuntime_Spawn_NilRequestStillRejected guards the error path ordering:
// mount authorization must not run before the nil-request check.
func TestRuntime_Spawn_NilRequestStillRejected(t *testing.T) {
	t.Parallel()

	f := newMountPolicyRuntimeFixture(t, nil)

	_, err := f.runtime.Spawn(context.Background(), "tenant-a", nil, "acp", nil)
	var invalid *orchestration.InputValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected InputValidationError, got %v", err)
	}
}
