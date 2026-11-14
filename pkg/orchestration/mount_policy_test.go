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

package orchestration

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// mountPolicyFixture builds a projects root with two tenant subtrees and one
// operator allowlist root on a temporary filesystem. Every path the policy is
// asked about has to exist, so the fixture is the filesystem the tests assert
// against rather than a set of string literals.
type mountPolicyFixture struct {
	policy       *MountPolicy
	projectsRoot string
	allowedRoot  string

	// tenantA and tenantB are provisioned workspace directories, one per
	// tenant, in the layout WorkspaceManager creates.
	tenantA string
	tenantB string
}

func newMountPolicyFixture(t *testing.T) mountPolicyFixture {
	t.Helper()

	base := t.TempDir()
	projectsRoot := filepath.Join(base, "projects")
	allowedRoot := filepath.Join(base, "shared")

	tenantA := filepath.Join(projectsRoot, "tenant-a", "proj-1", workspaceDirName)
	tenantB := filepath.Join(projectsRoot, "tenant-b", "proj-9", workspaceDirName)
	for _, dir := range []string{tenantA, tenantB, filepath.Join(allowedRoot, "corpus")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}

	return mountPolicyFixture{
		policy: &MountPolicy{
			ProjectsRoot: projectsRoot,
			AllowedRoots: []string{allowedRoot},
		},
		projectsRoot: projectsRoot,
		allowedRoot:  allowedRoot,
		tenantA:      tenantA,
		tenantB:      tenantB,
	}
}

func TestMountPolicy_AuthorizeSource(t *testing.T) {
	t.Parallel()

	f := newMountPolicyFixture(t)

	tests := []struct {
		name    string
		tenant  string
		source  string
		allowed bool
	}{
		{"own workspace", "tenant-a", f.tenantA, true},
		{"own tenant root", "tenant-a", filepath.Dir(filepath.Dir(f.tenantA)), true},
		{"allowlist root itself", "tenant-a", f.allowedRoot, true},
		{"under allowlist root", "tenant-a", filepath.Join(f.allowedRoot, "corpus"), true},

		// The projects root is shared by every tenant, so naming it — or
		// another tenant's subtree under it — is a cross-tenant read.
		{"another tenant's workspace", "tenant-a", f.tenantB, false},
		{"projects root itself", "tenant-a", f.projectsRoot, false},
		{"no tenant", "", f.tenantA, false},
		{"tenant with a separator", "../tenant-a", f.tenantA, false},

		{"host root", "tenant-a", "/", false},
		{"outside every root", "tenant-a", "/etc", false},
		{"sibling with root prefix", "tenant-a", f.allowedRoot + "-evil", false},
		{"traversal escaping the tenant root", "tenant-a",
			filepath.Join(f.tenantA, "..", "..", "tenant-b"), false},
		{"relative source", "tenant-a", "relative/path", false},
		{"empty source", "tenant-a", "", false},
		{"absent source under own root", "tenant-a", filepath.Join(f.tenantA, "missing"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := f.policy.AuthorizeSource(tt.tenant, MountFieldExtraMounts, tt.source)
			if tt.allowed {
				if err != nil {
					t.Fatalf("AuthorizeSource(%q, %q): expected allowed, got %v",
						tt.tenant, tt.source, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("AuthorizeSource(%q, %q): expected denial, got nil", tt.tenant, tt.source)
			}
			var denied *MountNotAllowedError
			if !errors.As(err, &denied) {
				t.Fatalf("expected MountNotAllowedError, got %T: %v", err, err)
			}
			if denied.Source != tt.source {
				t.Errorf("denied.Source = %q, want %q", denied.Source, tt.source)
			}
			if denied.Field != MountFieldExtraMounts {
				t.Errorf("denied.Field = %q, want %q", denied.Field, MountFieldExtraMounts)
			}
		})
	}
}

// TestMountPolicy_SymlinkEscapeDenied is the resolved-path half of the policy:
// a source that passes the textual prefix test but resolves outside every root
// is refused. An agent has write access to its own workspace, so it can plant
// exactly this symlink and then ask for it to be mounted.
func TestMountPolicy_SymlinkEscapeDenied(t *testing.T) {
	t.Parallel()

	f := newMountPolicyFixture(t)

	escape := filepath.Join(f.tenantA, "escape")
	if err := os.Symlink("/", escape); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	crossTenant := filepath.Join(f.tenantA, "peek")
	if err := os.Symlink(f.tenantB, crossTenant); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	for name, source := range map[string]string{
		"symlink to host root":             escape,
		"symlink to another tenant":        crossTenant,
		"path traversing a root symlink":   filepath.Join(escape, "etc"),
		"path traversing a peek symlink":   filepath.Join(crossTenant, "src"),
		"symlink named through the target": filepath.Join(f.tenantA, "escape") + string(filepath.Separator),
	} {
		t.Run(name, func(t *testing.T) {
			err := f.policy.AuthorizeSource("tenant-a", MountFieldWorkDir, source)
			var denied *MountNotAllowedError
			if !errors.As(err, &denied) {
				t.Fatalf("expected MountNotAllowedError for %q, got %T: %v", source, err, err)
			}
		})
	}

	// A symlink that stays inside the tenant's own subtree is fine: the policy
	// rejects escapes, not symlinks.
	inside := filepath.Join(f.tenantA, "inside")
	if err := os.Mkdir(filepath.Join(f.tenantA, "src"), 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := os.Symlink(filepath.Join(f.tenantA, "src"), inside); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if err := f.policy.AuthorizeSource("tenant-a", MountFieldWorkDir, inside); err != nil {
		t.Fatalf("in-subtree symlink: expected allowed, got %v", err)
	}
}

// TestMountPolicy_SymlinkedRootStillMatches covers the operator-side half of
// symlink resolution: a deployment whose projects root is reached through a
// symlinked parent must still authorize its own workspaces.
func TestMountPolicy_SymlinkedRootStillMatches(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	real := filepath.Join(base, "real")
	workspace := filepath.Join(real, "tenant-a", "proj-1", workspaceDirName)
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// Root configured as the symlink, source named through the symlink.
	policy := &MountPolicy{ProjectsRoot: link}
	viaLink := filepath.Join(link, "tenant-a", "proj-1", workspaceDirName)
	if err := policy.AuthorizeSource("tenant-a", MountFieldWorkDir, viaLink); err != nil {
		t.Fatalf("symlinked root, symlinked source: expected allowed, got %v", err)
	}

	// Root configured as the symlink, source named by its real path.
	if err := policy.AuthorizeSource("tenant-a", MountFieldWorkDir, workspace); err != nil {
		t.Fatalf("symlinked root, real source: expected allowed, got %v", err)
	}

	// Another tenant's workspace is still denied through the symlink.
	other := filepath.Join(real, "tenant-b", "proj-2", workspaceDirName)
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := policy.AuthorizeSource("tenant-a", MountFieldWorkDir,
		filepath.Join(link, "tenant-b", "proj-2", workspaceDirName)); err == nil {
		t.Fatal("cross-tenant path through a symlinked root: expected denial, got nil")
	}
}

func TestMountPolicy_Authorize_DenyByDefault(t *testing.T) {
	t.Parallel()

	f := newMountPolicyFixture(t)
	mounts := []ContainerMount{{Source: f.tenantA, Target: "/mnt"}}

	// A nil policy denies everything.
	var nilPolicy *MountPolicy
	if err := nilPolicy.Authorize("tenant-a", MountFieldExtraMounts, mounts); err == nil {
		t.Fatal("nil policy: expected denial, got nil")
	}

	// An empty policy denies everything.
	if err := (&MountPolicy{}).Authorize("tenant-a", MountFieldExtraMounts, mounts); err == nil {
		t.Fatal("empty policy: expected denial, got nil")
	}

	// Relative roots can never widen the policy.
	relPolicy := &MountPolicy{ProjectsRoot: "projects", AllowedRoots: []string{"data"}}
	if err := relPolicy.Authorize("tenant-a", MountFieldExtraMounts, mounts); err == nil {
		t.Fatal("relative-roots policy: expected denial, got nil")
	}

	// No mounts is always fine, even with no policy.
	if err := nilPolicy.Authorize("tenant-a", MountFieldExtraMounts, nil); err != nil {
		t.Fatalf("nil policy with no mounts: expected nil, got %v", err)
	}
}

// TestMountPolicy_AdmitsASourcelessTmpfs: a tmpfs reaches nothing on the
// host, so it needs no grant — it is how the forge credential reaches a
// container without a disk (go-quicbaas#899). A tmpfs that names a source is
// a bind in disguise and is judged like one, and a sourceless bind is still
// refused.
func TestMountPolicy_AdmitsASourcelessTmpfs(t *testing.T) {
	t.Parallel()

	f := newMountPolicyFixture(t)
	if err := f.policy.Authorize("tenant-a", MountFieldExtraMounts, []ContainerMount{WorkspaceCredentialMount()}); err != nil {
		t.Fatalf("the credential tier was refused: %v", err)
	}

	refused := map[string]ContainerMount{
		"tmpfs with an ungranted source": {Type: MountTypeTmpfs, Source: "/etc", Target: "/mnt"},
		"bind with no source":            {Target: "/mnt"},
	}
	for name, mount := range refused {
		var denied *MountNotAllowedError
		if err := f.policy.Authorize("tenant-a", MountFieldExtraMounts, []ContainerMount{mount}); !errors.As(err, &denied) {
			t.Errorf("%s: err = %v, want *MountNotAllowedError", name, err)
		}
	}
}

func TestMountPolicy_Authorize_FirstViolationWins(t *testing.T) {
	t.Parallel()

	f := newMountPolicyFixture(t)
	err := f.policy.Authorize("tenant-a", MountFieldExtraMounts, []ContainerMount{
		{Source: f.tenantA, Target: "/a"},
		{Source: "/etc", Target: "/b"},
	})
	var denied *MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError, got %T: %v", err, err)
	}
	if denied.Source != "/etc" {
		t.Errorf("denied.Source = %q, want %q", denied.Source, "/etc")
	}
}

func TestMountNotAllowedError_Message(t *testing.T) {
	t.Parallel()

	err := &MountNotAllowedError{
		Field:    MountFieldWorkDir,
		TenantID: "tenant-a",
		Source:   "/",
		Reason:   "nope",
	}
	want := `mount not allowed (work_dir) for tenant "tenant-a": "/": nope`
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}

	// A denial that never got as far as the tenant check names no tenant.
	bare := &MountNotAllowedError{Source: "rel", Reason: "nope"}
	if got, want := bare.Error(), `mount not allowed: "rel": nope`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// =============================================================================
// The daemon's sockets root
// =============================================================================

// socketsRootFixture builds a sockets root holding one live session
// directory with a real unix socket in it, which is the only shape the policy
// admits under that root.
type socketsRootFixture struct {
	policy  *MountPolicy
	root    string
	session string
	socket  string
}

func newSocketsRootFixture(t *testing.T) socketsRootFixture {
	t.Helper()
	base := newMountPolicyFixture(t)
	// A short /tmp parent keeps the socket path under the 108-byte
	// sockaddr_un limit when the runner's TMPDIR is long.
	parent, err := os.MkdirTemp("/tmp", "msr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent) })
	root := filepath.Join(parent, "s")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(root, "0123456789ab")
	if err := os.Mkdir(session, 0o700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(session, WorkspaceSigningAgentSocketFile)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("bind the session socket: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	base.policy.SocketsRoot = root
	return socketsRootFixture{policy: base.policy, root: root, session: session, socket: socket}
}

// TestMountPolicy_AdmitsTheSigningSocketUnderTheSocketsRoot: the daemon binds
// each attempt's signing socket under its own sockets root because a unix
// socket address does not fit under a managed workspace, so the policy admits
// that socket WITHOUT the operator having allowlisted anything — and it
// admits it for a tenant that has no projects-root grant at all, because the
// socket is the daemon's, not the tenant's.
func TestMountPolicy_AdmitsTheSigningSocketUnderTheSocketsRoot(t *testing.T) {
	f := newSocketsRootFixture(t)
	mount := ContainerMount{Source: f.socket, Target: WorkspaceSigningAgentSocket, ReadOnly: true}
	if err := f.policy.Authorize("tenant-a", MountFieldExtraMounts, []ContainerMount{mount}); err != nil {
		t.Fatalf("the signing socket was denied: %v", err)
	}
	if err := f.policy.Authorize("", MountFieldExtraMounts, []ContainerMount{mount}); err != nil {
		t.Errorf("the socket is the daemon's, not a tenant's, yet an untenanted spawn was denied: %v", err)
	}
	// The mount the runner actually builds is the one that has to pass.
	req := &SpawnSessionRequest{Workspace: &SessionWorkspace{TenantID: "tenant-a", ProjectID: "p1", Key: "k"}}
	ApplyWorkspaceSpec(req, &WorkspaceSpecFiles{}, f.socket)
	if err := f.policy.Authorize("tenant-a", MountFieldExtraMounts, req.ExtraMounts); err != nil {
		t.Errorf("the mount ApplyWorkspaceSpec builds was denied: %v", err)
	}
}

// TestMountPolicy_TheSocketsRootGrantIsOnlyTheSocket: the root is shared by
// every tenant, so what is admitted is the socket file itself, one level
// down, read-only, and actually a socket. Everything beside it — the root,
// the session directory, a sibling file, a deeper path, a regular file
// wearing the socket's name — stays denied, which is also what stops a caller
// mounting the root to read the session names inside it.
func TestMountPolicy_TheSocketsRootGrantIsOnlyTheSocket(t *testing.T) {
	f := newSocketsRootFixture(t)

	sibling := filepath.Join(f.session, "other")
	if err := os.WriteFile(sibling, []byte("beside the socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	impostorDir := filepath.Join(f.root, "ffffffffffff")
	if err := os.Mkdir(impostorDir, 0o700); err != nil {
		t.Fatal(err)
	}
	impostor := filepath.Join(impostorDir, WorkspaceSigningAgentSocketFile)
	if err := os.WriteFile(impostor, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	deepDir := filepath.Join(f.session, "deeper")
	if err := os.Mkdir(deepDir, 0o700); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(deepDir, WorkspaceSigningAgentSocketFile)
	if err := os.WriteFile(deep, []byte("two levels down"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, mount := range map[string]ContainerMount{
		"the root itself":                    {Source: f.root, ReadOnly: true},
		"the session directory":              {Source: f.session, ReadOnly: true},
		"a file beside the socket":           {Source: sibling, ReadOnly: true},
		"a regular file named like a socket": {Source: impostor, ReadOnly: true},
		"a socket name two levels down":      {Source: deep, ReadOnly: true},
		"the socket, writable":               {Source: f.socket, ReadOnly: false},
	} {
		err := f.policy.Authorize("tenant-a", MountFieldExtraMounts, []ContainerMount{mount})
		var denied *MountNotAllowedError
		if !errors.As(err, &denied) {
			t.Errorf("%s: err = %v, want *MountNotAllowedError", name, err)
		}
	}

	// AuthorizeSource is the work-directory door and cannot express
	// read-only, so it never opens the sockets root either.
	if err := f.policy.AuthorizeSource("tenant-a", MountFieldWorkDir, f.socket); err == nil {
		t.Error("the signing socket was admitted as a work directory")
	}

	// An unset sockets root grants nothing, which is what an unwired daemon
	// must do.
	f.policy.SocketsRoot = ""
	if err := f.policy.Authorize("tenant-a", MountFieldExtraMounts,
		[]ContainerMount{{Source: f.socket, ReadOnly: true}}); err == nil {
		t.Error("a policy with no sockets root admitted a socket anyway")
	}
	f.policy.SocketsRoot = "relative/sockets"
	if err := f.policy.Authorize("tenant-a", MountFieldExtraMounts,
		[]ContainerMount{{Source: f.socket, ReadOnly: true}}); err == nil {
		t.Error("a relative sockets root admitted a socket anyway")
	}
}

// TestMountPolicy_SymlinkedSocketsRootStillMatches: a deployment whose
// runtime or data directory is reached through a symlinked parent resolves
// its own socket to a path under the resolved root, and denying that would
// deny every signing agent it starts.
func TestMountPolicy_SymlinkedSocketsRootStillMatches(t *testing.T) {
	f := newSocketsRootFixture(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(f.root, link); err != nil {
		t.Fatal(err)
	}
	f.policy.SocketsRoot = link
	linked := filepath.Join(link, filepath.Base(f.session), WorkspaceSigningAgentSocketFile)
	if err := f.policy.Authorize("tenant-a", MountFieldExtraMounts,
		[]ContainerMount{{Source: linked, ReadOnly: true}}); err != nil {
		t.Fatalf("a socket under a symlinked sockets root was denied: %v", err)
	}
}
