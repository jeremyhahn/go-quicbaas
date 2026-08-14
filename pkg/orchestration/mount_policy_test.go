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
