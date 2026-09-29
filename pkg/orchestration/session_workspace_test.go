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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// namedWorkspace is a valid named workspace carrying a spec with a token.
func namedWorkspace() *SessionWorkspace {
	return &SessionWorkspace{
		TenantID:  "t1",
		ProjectID: "p1",
		Key:       "task-a1",
		Spec: &WorkspaceSpecFiles{Files: map[string][]byte{
			WorkspaceSpecFile:       []byte(`{"checkout_root":"/workspace"}`),
			WorkspaceForgeTokenFile: []byte("forge-secret"),
		}},
	}
}

// newTestWorkspaceManager is a manager over a fresh temporary projects root.
func newTestWorkspaceManager(t *testing.T) *WorkspaceManager {
	t.Helper()
	m, err := NewWorkspaceManager(filepath.Join(t.TempDir(), "projects"))
	if err != nil {
		t.Fatalf("NewWorkspaceManager: %v", err)
	}
	return m
}

// TestProvisionSession_MakesTheWorkspaceAndWritesTheSpecBesideIt proves the
// layout: the workspace under <root>/<tenant>/<project>/workspace/<key> at
// 0700, and the spec directory BESIDE it — never inside the read-write mount —
// holding exactly the files the spawn carried.
func TestProvisionSession_MakesTheWorkspaceAndWritesTheSpecBesideIt(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	p, err := m.ProvisionSession(namedWorkspace())
	if err != nil {
		t.Fatalf("ProvisionSession: %v", err)
	}
	t.Cleanup(p.Release)

	want := filepath.Join(m.Root(), "t1", "p1", workspaceDirName, "task-a1")
	if p.Dir != want {
		t.Fatalf("Dir = %q, want %q", p.Dir, want)
	}
	info, err := os.Stat(p.Dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != workspaceDirPerm {
		t.Fatalf("workspace = %v, %v; want a 0700 directory", info, err)
	}
	if filepath.Dir(p.SpecDir) != filepath.Dir(p.Dir) {
		t.Fatalf("the spec directory %q must sit beside the workspace %q", p.SpecDir, p.Dir)
	}
	if strings.HasPrefix(p.SpecDir, p.Dir+string(filepath.Separator)) {
		t.Fatalf("the spec directory must not be under the read-write workspace mount: %q", p.SpecDir)
	}
	token, err := os.ReadFile(filepath.Join(p.SpecDir, WorkspaceForgeTokenFile))
	if err != nil || string(token) != "forge-secret" {
		t.Fatalf("token file = %q, %v", token, err)
	}
}

// TestProvisionSession_ReleaseRemovesOnlyTheSpec: releasing drops the spec
// directory — the forge token — and leaves the checkout for the job's next
// session. Release is idempotent.
func TestProvisionSession_ReleaseRemovesOnlyTheSpec(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	p, err := m.ProvisionSession(namedWorkspace())
	if err != nil {
		t.Fatalf("ProvisionSession: %v", err)
	}
	p.Release()
	p.Release()
	if _, err := os.Stat(p.SpecDir); !os.IsNotExist(err) {
		t.Fatalf("the spec directory must be gone: %v", err)
	}
	if _, err := os.Stat(p.Dir); err != nil {
		t.Fatalf("the workspace must survive the release: %v", err)
	}
}

// TestProvisionSession_WithoutSpecWritesNoSpecDir: a spawn that carries no
// spec gets a workspace and nothing beside it.
func TestProvisionSession_WithoutSpecWritesNoSpecDir(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	ws := namedWorkspace()
	ws.Spec = nil
	p, err := m.ProvisionSession(ws)
	if err != nil {
		t.Fatalf("ProvisionSession: %v", err)
	}
	if p.SpecDir != "" {
		t.Fatalf("SpecDir = %q, want none", p.SpecDir)
	}
	p.Release()
	if _, err := os.Stat(p.Dir); err != nil {
		t.Fatalf("workspace: %v", err)
	}
}

// TestProvisionSession_UnpinnedIsIdempotent: provisioning the same workspace
// again finds the directory rather than failing on it, and keeps what it holds.
func TestProvisionSession_UnpinnedIsIdempotent(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	first, err := m.ProvisionSession(namedWorkspace())
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	first.Release()
	marker := filepath.Join(first.Dir, "clone")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := m.ProvisionSession(namedWorkspace())
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	t.Cleanup(second.Release)
	if second.Dir != first.Dir {
		t.Fatalf("Dir moved: %q -> %q", first.Dir, second.Dir)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the clone must survive a second provision: %v", err)
	}
}

// TestProvisionSession_PinnedFindsTheCheckout: a pinned workspace that is
// where the pin says is used as it is.
func TestProvisionSession_PinnedFindsTheCheckout(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	first, err := m.ProvisionSession(namedWorkspace())
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	first.Release()

	ws := namedWorkspace()
	ws.Pin = &WorkspacePlacement{Key: ws.Key, Dir: first.Dir}
	second, err := m.ProvisionSession(ws)
	if err != nil {
		t.Fatalf("pinned: %v", err)
	}
	t.Cleanup(second.Release)
	if second.Dir != first.Dir || second.SpecDir == "" {
		t.Fatalf("pinned = %+v", second)
	}
}

// TestProvisionSession_PinnedStaleIsRefused: a pinned workspace that is gone,
// was moved, or is not a directory is refused as stale — never recreated,
// because re-creating it is a fresh clone every record says did not happen.
func TestProvisionSession_PinnedStaleIsRefused(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		pinDir func(dir string) string
		setup  func(t *testing.T, dir string)
	}{
		"missing": {
			pinDir: func(dir string) string { return dir },
			setup:  func(*testing.T, string) {},
		},
		"moved": {
			pinDir: func(string) string { return "/elsewhere/t1/p1/workspace/task-a1" },
			setup:  func(*testing.T, string) {},
		},
		"not a directory": {
			pinDir: func(dir string) string { return dir },
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dir, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newTestWorkspaceManager(t)
			dir, err := m.SessionWorkspacePath("t1", "p1", "task-a1")
			if err != nil {
				t.Fatal(err)
			}
			tc.setup(t, dir)
			ws := namedWorkspace()
			ws.Pin = &WorkspacePlacement{Key: ws.Key, Dir: tc.pinDir(dir)}
			p, err := m.ProvisionSession(ws)
			var stale *WorkspaceStaleError
			if !errors.As(err, &stale) || !errors.Is(err, ErrSessionWorkspace) || p != nil {
				t.Fatalf("got %v, %v; want *WorkspaceStaleError", p, err)
			}
			if stale.Expected != ws.Pin.Dir || stale.Key != ws.Key {
				t.Fatalf("the refusal must name the pin: %+v", stale)
			}
			if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
				t.Fatal("a stale pin must never be re-created")
			}
		})
	}
}

// TestProvisionSession_PinForAnotherKeyProvisionsFresh: a job's pin names the
// HOST for every key, but a directory only for its own; a new key on the
// pinned host is provisioned normally.
func TestProvisionSession_PinForAnotherKeyProvisionsFresh(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	ws := namedWorkspace()
	ws.Pin = &WorkspacePlacement{Key: "review-7-abc", Dir: "/nowhere"}
	p, err := m.ProvisionSession(ws)
	if err != nil {
		t.Fatalf("ProvisionSession: %v", err)
	}
	t.Cleanup(p.Release)
	if filepath.Base(p.Dir) != "task-a1" {
		t.Fatalf("Dir = %q", p.Dir)
	}
}

// TestProvisionSession_RefusesAnInvalidWorkspace covers every name a host
// must not turn into a path, and a spec naming a file outside the closed set.
func TestProvisionSession_RefusesAnInvalidWorkspace(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	cases := map[string]func(*SessionWorkspace){
		"empty tenant":      func(w *SessionWorkspace) { w.TenantID = "" },
		"traversal project": func(w *SessionWorkspace) { w.ProjectID = ".." },
		"separator key":     func(w *SessionWorkspace) { w.Key = "a/b" },
		"foreign spec file": func(w *SessionWorkspace) { w.Spec.Files["../../etc/cron.d/x"] = []byte("x") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			ws := namedWorkspace()
			mutate(ws)
			p, err := m.ProvisionSession(ws)
			var invalid *SessionWorkspaceInvalidError
			if !errors.As(err, &invalid) || !errors.Is(err, ErrSessionWorkspace) || p != nil {
				t.Fatalf("got %v, %v; want *SessionWorkspaceInvalidError", p, err)
			}
		})
	}
	if err := (*SessionWorkspace)(nil).Validate(); err == nil {
		t.Fatal("a nil workspace validated")
	}
}

// TestProvisionSession_UnwritableRootIsTyped: a projects root that cannot be
// created surfaces as the manager's own IO error, INSIDE the session-workspace
// family — an operator's condition, parked uncharged, not a charged failure.
// A file where the root should be fails for every uid, root included.
func TestProvisionSession_UnwritableRootIsTyped(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewWorkspaceManager(filepath.Join(blocker, "projects"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.ProvisionSession(namedWorkspace())
	var ioErr *WorkspaceIOError
	var provisionErr *SessionWorkspaceProvisionError
	if !errors.As(err, &ioErr) || !errors.As(err, &provisionErr) || !errors.Is(err, ErrSessionWorkspace) {
		t.Fatalf("got %v, want a WorkspaceIOError wrapped in the session-workspace family", err)
	}
	if provisionErr.Key != "task-a1" || !strings.Contains(err.Error(), "task-a1") {
		t.Fatalf("the refusal must name the workspace: %v", err)
	}
	if class := spawnFaultClassOf(err); class != spawnFaultSessionWorkspace {
		t.Fatalf("spawn fault class = %q, want it to cross the wire as a workspace fault", class)
	}
}

// TestProvisionSession_APinnedWorkspaceRejectsALinkedAncestor: a link at the
// project directory or the workspace directory — even one pointing inside the
// tenant's subtree, which the root and the mount check both admit — would give
// the pinned session another checkout than the one it reports. Both levels are
// refused, and the other checkout is untouched.
func TestProvisionSession_APinnedWorkspaceRejectsALinkedAncestor(t *testing.T) {
	t.Parallel()
	for name, level := range map[string]string{
		"project directory":   "p1",
		"workspace directory": filepath.Join("p1", workspaceDirName),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newTestWorkspaceManager(t)
			// Another project's real checkout, the link's target.
			decoy := filepath.Join(m.Root(), "t1", "p-decoy", workspaceDirName, "task-a1")
			if err := os.MkdirAll(decoy, 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(m.Root(), "t1", "p-decoy")
			if level != "p1" {
				target = filepath.Join(target, workspaceDirName)
			}
			plantLink(t, m, level, target)

			ws := namedWorkspace()
			dir, err := m.SessionWorkspacePath("t1", "p1", "task-a1")
			if err != nil {
				t.Fatal(err)
			}
			ws.Pin = &WorkspacePlacement{Key: ws.Key, Dir: dir}
			var link *WorkspaceLinkError
			if _, err := m.ProvisionSession(ws); !errors.As(err, &link) || !errors.Is(err, ErrSessionWorkspace) {
				t.Fatalf("err = %v, want *WorkspaceLinkError", err)
			}
			entries, err := os.ReadDir(filepath.Dir(decoy))
			if err != nil || len(entries) != 1 {
				t.Fatalf("the decoy's workspace directory changed: %v, %v", entries, err)
			}
		})
	}
}

// TestSessionWorkspacePath_RefusesABadKey: the key is a path segment like the
// tenant and project are.
func TestSessionWorkspacePath_RefusesABadKey(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	if _, err := m.SessionWorkspacePath("t1", "p1", ".."); err == nil {
		t.Fatal("a traversal key was accepted")
	}
	if _, err := m.SessionWorkspacePath("", "p1", "k"); err == nil {
		t.Fatal("an empty tenant was accepted")
	}
	got, err := m.SessionWorkspacePath("t1", "p1", "k")
	if err != nil || got != filepath.Join(m.Root(), "t1", "p1", workspaceDirName, "k") {
		t.Fatalf("path = %q, %v", got, err)
	}
}

// TestProvisionedWorkspace_ApplyTo puts the workspace on a backend spawn: the
// directory as the work dir, the spec directory read-only at its fixed target,
// and the caller's mounts kept.
func TestProvisionedWorkspace_ApplyTo(t *testing.T) {
	t.Parallel()
	req := &SpawnRequest{ExtraMounts: []ContainerMount{{Source: "/sock", Target: "/run/x", ReadOnly: true}}}
	(&ProvisionedWorkspace{Dir: "/root/t/p/workspace/k", SpecDir: "/root/t/p/workspace/quicbaas-workspace-1"}).ApplyTo(req)
	if req.WorkDir != "/root/t/p/workspace/k" {
		t.Fatalf("WorkDir = %q", req.WorkDir)
	}
	if len(req.ExtraMounts) != 2 || req.ExtraMounts[1].Target != WorkspaceSpecDir || !req.ExtraMounts[1].ReadOnly {
		t.Fatalf("mounts = %+v", req.ExtraMounts)
	}

	bare := &SpawnRequest{}
	(&ProvisionedWorkspace{Dir: "/d"}).ApplyTo(bare)
	if bare.WorkDir != "/d" || len(bare.ExtraMounts) != 0 {
		t.Fatalf("a workspace with no spec mounts nothing: %+v", bare)
	}
	var none *ProvisionedWorkspace
	none.ApplyTo(bare)
	none.Release()
	(&ProvisionedWorkspace{}).ApplyTo(nil)
}

// TestWorkspacePlacement_OnDaemon: an empty agent host is the daemon itself.
func TestWorkspacePlacement_OnDaemon(t *testing.T) {
	t.Parallel()
	if !(WorkspacePlacement{Dir: "/d"}).OnDaemon() {
		t.Fatal("an empty agent host is the daemon")
	}
	if (WorkspacePlacement{AgentHost: "agent-09:9091"}).OnDaemon() {
		t.Fatal("a named agent host is not the daemon")
	}
	if (*SessionWorkspace)(nil).Pinned() || (&SessionWorkspace{}).Pinned() {
		t.Fatal("no pin is not pinned")
	}
}

// TestSessionWorkspaceErrors_NameTheFaultAndUnwrap: every refusal names what
// a person has to act on and unwraps to ErrSessionWorkspace.
func TestSessionWorkspaceErrors_NameTheFaultAndUnwrap(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		err  error
		want []string
	}{
		"invalid": {&SessionWorkspaceInvalidError{Field: "key", Reason: "is empty"}, []string{"key", "is empty"}},
		"tenant": {&SessionWorkspaceTenantError{SpawnTenant: "a", WorkspaceTenant: "b"},
			[]string{`"a"`, `"b"`}},
		"unsupported": {&SessionWorkspaceUnsupportedError{Host: "agent /w"}, []string{"agent /w", "projects root"}},
		"agent pinned": {&WorkspaceFleetNodeUnavailableError{AgentHost: "agent-09:9091", Key: "k", Backend: "codex",
			ResolvedHost: "vs04:9092"}, []string{`"agent-09:9091"`, "codex", `"vs04:9092"`, "not moved"}},
		"daemon pinned": {&WorkspaceFleetNodeUnavailableError{Key: "k", Backend: "codex"},
			[]string{"in-process backend"}},
		"stale": {&WorkspaceStaleError{Key: "k", Expected: "/d", Reason: "gone"}, []string{`"k"`, `"/d"`, "gone"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if !errors.Is(tc.err, ErrSessionWorkspace) {
				t.Fatalf("%T does not unwrap to ErrSessionWorkspace", tc.err)
			}
			for _, want := range tc.want {
				if !strings.Contains(tc.err.Error(), want) {
					t.Fatalf("%q does not name %q", tc.err.Error(), want)
				}
			}
		})
	}
}

// TestProvisionSession_WipesTheInMemorySpec: once the spec is written, the
// token bytes the spawn carried are zeroed and dropped — the file is the one
// copy on this host.
func TestProvisionSession_WipesTheInMemorySpec(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	ws := namedWorkspace()
	token := ws.Spec.Files[WorkspaceForgeTokenFile]
	p, err := m.ProvisionSession(ws)
	if err != nil {
		t.Fatalf("ProvisionSession: %v", err)
	}
	t.Cleanup(p.Release)
	if len(ws.Spec.Files) != 0 {
		t.Fatalf("the spec is still held in memory: %v", ws.Spec.Files)
	}
	for _, b := range token {
		if b != 0 {
			t.Fatal("the token's bytes were dropped but not zeroed")
		}
	}
	if raw, err := os.ReadFile(filepath.Join(p.SpecDir, WorkspaceForgeTokenFile)); err != nil || string(raw) != "forge-secret" {
		t.Fatalf("the written copy = %q, %v", raw, err)
	}
	var none *WorkspaceSpecFiles
	none.Wipe()
}

// TestSweepSessionSpecs_RemovesEveryTokenAndKeepsEveryCheckout is the restart
// half: the spec directories a previous run left — each a forge token in
// plaintext on a persistent disk — are removed, across tenants and projects,
// and the checkouts beside them are untouched.
func TestSweepSessionSpecs_RemovesEveryTokenAndKeepsEveryCheckout(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	var specs []string
	var checkouts []string
	for _, ws := range []*SessionWorkspace{namedWorkspace(), {TenantID: "t2", ProjectID: "p9", Key: "review-4-abc",
		Spec: &WorkspaceSpecFiles{Files: map[string][]byte{WorkspaceForgeTokenFile: []byte("other-secret")}}}} {
		p, err := m.ProvisionSession(ws)
		if err != nil {
			t.Fatal(err)
		}
		specs = append(specs, p.SpecDir)
		checkouts = append(checkouts, p.Dir)
		if err := os.WriteFile(filepath.Join(p.Dir, "clone"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := m.SweepSessionSpecs()
	if err != nil {
		t.Fatalf("SweepSessionSpecs: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("removed %v, want both spec directories", removed)
	}
	for _, dir := range specs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("spec directory %s survived the sweep: %v", dir, err)
		}
	}
	for _, dir := range checkouts {
		if _, err := os.Stat(filepath.Join(dir, "clone")); err != nil {
			t.Fatalf("checkout %s was touched by the sweep: %v", dir, err)
		}
	}
	if again, err := m.SweepSessionSpecs(); err != nil || len(again) != 0 {
		t.Fatalf("a second sweep = %v, %v; want nothing", again, err)
	}
	empty, err := NewWorkspaceManager(filepath.Join(t.TempDir(), "never-made"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := empty.SweepSessionSpecs(); err != nil || len(got) != 0 {
		t.Fatalf("a root that does not exist yet = %v, %v; want nothing", got, err)
	}
}

// TestSessionWorkspace_RefusesASpecShapedKey: a key the startup sweep would
// mistake for a spec directory is refused, so no checkout can be swept.
func TestSessionWorkspace_RefusesASpecShapedKey(t *testing.T) {
	t.Parallel()
	ws := namedWorkspace()
	ws.Key = workspaceSpecDirName + "123"
	var invalid *SessionWorkspaceInvalidError
	if err := ws.Validate(); !errors.As(err, &invalid) || invalid.Field != "key" {
		t.Fatalf("err = %v, want the key refused", err)
	}
}

// =============================================================================
// Confinement: a tenant's links never aim provisioning outside its subtree
// =============================================================================

// outsideTarget is a directory outside every projects root, 0755, holding one
// file, with a check that nothing about it changed.
func outsideTarget(t *testing.T) (string, func(*testing.T)) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, func(t *testing.T) {
		t.Helper()
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("the outside directory was changed: %v, %v", info, err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 || entries[0].Name() != "keep" {
			t.Fatalf("the outside directory gained or lost entries: %v, %v", entries, err)
		}
	}
}

// plantLink creates the workspace's parents under the tenant's root and puts
// a link at rel pointing at target.
func plantLink(t *testing.T, m *WorkspaceManager, rel, target string) {
	t.Helper()
	path := filepath.Join(m.Root(), "t1", rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// TestProvisionSession_NeverFollowsALinkOutOfTheTenantSubtree is the review
// finding's three shapes: a workspace key, a project directory and a
// workspace directory each replaced by a link to a directory outside the
// tenant's subtree. Provisioning refuses every one, and the outside directory
// keeps its mode and its contents — no chmod, no mkdir, no spec written there.
func TestProvisionSession_NeverFollowsALinkOutOfTheTenantSubtree(t *testing.T) {
	t.Parallel()
	for name, rel := range map[string]string{
		"a linked workspace key":       filepath.Join("p1", workspaceDirName, "task-a1"),
		"a linked workspace directory": filepath.Join("p1", workspaceDirName),
		"a linked project directory":   "p1",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newTestWorkspaceManager(t)
			outside, unchanged := outsideTarget(t)
			plantLink(t, m, rel, outside)
			p, err := m.ProvisionSession(namedWorkspace())
			if err == nil || p != nil {
				t.Fatalf("provisioning through a link succeeded: %+v", p)
			}
			unchanged(t)
		})
	}
}

// TestProvisionSession_RefusesALinkInsideTheSubtreeToo: a link that stays
// inside the tenant's subtree is still not a workspace — the workspace is made
// of directories — and is refused typed, pinned or not.
func TestProvisionSession_RefusesALinkInsideTheSubtreeToo(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	other := filepath.Join(m.Root(), "t1", "p1", workspaceDirName, "other")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	plantLink(t, m, filepath.Join("p1", workspaceDirName, "task-a1"), "other")

	var link *WorkspaceLinkError
	if _, err := m.ProvisionSession(namedWorkspace()); !errors.As(err, &link) || !errors.Is(err, ErrSessionWorkspace) {
		t.Fatalf("unpinned: %v, want *WorkspaceLinkError", err)
	}
	ws := namedWorkspace()
	dir, _ := m.SessionWorkspacePath("t1", "p1", "task-a1")
	ws.Pin = &WorkspacePlacement{Key: ws.Key, Dir: dir}
	if _, err := m.ProvisionSession(ws); !errors.As(err, &link) {
		t.Fatalf("pinned: %v, want *WorkspaceLinkError", err)
	}
}

// TestProvisionSession_ALinkSwappedInDuringProvisioningEscapesNothing races a
// tenant flipping the workspace key between a real directory and a link to an
// outside directory against provisioning. Whatever interleaving happens —
// refusals are expected — nothing outside the tenant's subtree changes.
func TestProvisionSession_ALinkSwappedInDuringProvisioningEscapesNothing(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	outside, unchanged := outsideTarget(t)
	key := filepath.Join(m.Root(), "t1", "p1", workspaceDirName, "task-a1")
	if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	swapped := make(chan struct{})
	go func() {
		defer close(swapped)
		for {
			select {
			case <-done:
				return
			default:
			}
			_ = os.RemoveAll(key)
			_ = os.Mkdir(key, 0o755)
			_ = os.RemoveAll(key)
			_ = os.Symlink(outside, key)
		}
	}()
	for i := 0; i < 300; i++ {
		if p, err := m.ProvisionSession(namedWorkspace()); err == nil {
			p.Release()
		}
	}
	close(done)
	<-swapped
	unchanged(t)
}

// TestSweepSessionSpecs_NeverFollowsALink: a tenant that links a project or
// its workspace directory at a tree outside its subtree holding spec-shaped
// directories cannot make the startup sweep remove them.
func TestSweepSessionSpecs_NeverFollowsALink(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	outside := filepath.Join(t.TempDir(), "outside")
	victim := filepath.Join(outside, workspaceDirName, workspaceSpecDirName+"victim")
	if err := os.MkdirAll(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	plantLink(t, m, "p-linked", outside)
	plantLink(t, m, filepath.Join("p2", workspaceDirName), filepath.Join(outside, workspaceDirName))

	if _, err := m.SweepSessionSpecs(); err != nil {
		t.Fatalf("SweepSessionSpecs: %v", err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("the sweep followed a link out of the tenant's subtree: %v", err)
	}
}

// TestTenantRoot_RefusesALinkedTenantDirectory: the tenant directory is the
// level the confinement rests on, so a link there refuses provisioning.
func TestTenantRoot_RefusesALinkedTenantDirectory(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	outside, unchanged := outsideTarget(t)
	if err := os.MkdirAll(m.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(m.Root(), "t1")); err != nil {
		t.Fatal(err)
	}
	var link *WorkspaceLinkError
	if _, err := m.ProvisionSession(namedWorkspace()); !errors.As(err, &link) {
		t.Fatalf("err = %v, want *WorkspaceLinkError", err)
	}
	unchanged(t)
}
