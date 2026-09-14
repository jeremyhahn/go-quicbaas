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

package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// runGitCmd runs one git command inside dir and fails the test on error.
func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Isolate the fixture repo from host-level git configuration (commit
	// signing, templates, hooks) so the test is deterministic on any host.
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// initLocalGitRepo creates a local repository with one commit in a tmpdir and
// returns its file:// URL, HEAD SHA, and default branch name. No network.
func initLocalGitRepo(t *testing.T) (repoURL, headSHA, branch string) {
	t.Helper()
	dir := t.TempDir()
	runGitCmd(t, dir, "init", "-q")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "commit", "-q", "-m", "init")
	headSHA = runGitCmd(t, dir, "rev-parse", "HEAD")
	branch = runGitCmd(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	return "file://" + dir, headSHA, branch
}

// fakeGitSecrets satisfies SecretsService for the one method the provisioner
// uses; every other method is inherited from the embedded nil interface and
// must never be called.
type fakeGitSecrets struct {
	SecretsService
	getFn func(ctx context.Context, req *GetSecretRequest) (*GetSecretResponse, error)
}

func (f *fakeGitSecrets) GetSecret(ctx context.Context, req *GetSecretRequest) (*GetSecretResponse, error) {
	return f.getFn(ctx, req)
}

func TestGitProvisioner_Clone_Anonymous(t *testing.T) {
	t.Parallel()

	repoURL, wantSHA, _ := initLocalGitRepo(t)
	workspace := t.TempDir()

	p := &GitProvisioner{}
	sha, err := p.Clone(context.Background(), &GitCloneRequest{
		RepoURL: repoURL,
		Dir:     workspace,
	})
	if err != nil {
		t.Fatalf("Clone error: %v", err)
	}
	if sha != wantSHA {
		t.Errorf("Clone SHA = %q, want %q", sha, wantSHA)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, "README.md")); statErr != nil {
		t.Errorf("cloned file missing: %v", statErr)
	}
}

func TestGitProvisioner_Clone_Branch(t *testing.T) {
	t.Parallel()

	repoURL, wantSHA, branch := initLocalGitRepo(t)
	workspace := t.TempDir()

	p := &GitProvisioner{}
	sha, err := p.Clone(context.Background(), &GitCloneRequest{
		RepoURL: repoURL,
		Branch:  branch,
		Dir:     workspace,
	})
	if err != nil {
		t.Fatalf("Clone error: %v", err)
	}
	if sha != wantSHA {
		t.Errorf("Clone SHA = %q, want %q", sha, wantSHA)
	}
}

func TestGitProvisioner_Clone_BadURL(t *testing.T) {
	t.Parallel()

	p := &GitProvisioner{}
	_, err := p.Clone(context.Background(), &GitCloneRequest{
		RepoURL: "file://" + filepath.Join(t.TempDir(), "does-not-exist"),
		Dir:     t.TempDir(),
	})
	var cloneErr *GitCloneError
	if !errors.As(err, &cloneErr) {
		t.Fatalf("expected GitCloneError, got %T: %v", err, err)
	}
	if cloneErr.Stage != "clone" {
		t.Errorf("Stage = %q, want %q", cloneErr.Stage, "clone")
	}
}

func TestGitProvisioner_Clone_InvalidRequests(t *testing.T) {
	t.Parallel()

	p := &GitProvisioner{}
	tests := []struct {
		name string
		req  *GitCloneRequest
	}{
		{"nil request", nil},
		{"missing url", &GitCloneRequest{Dir: "/abs"}},
		{"missing dir", &GitCloneRequest{RepoURL: "https://example.com/r.git"}},
		{"relative dir", &GitCloneRequest{RepoURL: "https://example.com/r.git", Dir: "rel"}},
		{"conflicting credentials", &GitCloneRequest{
			RepoURL: "https://example.com/r.git",
			Dir:     "/abs",
			GitCredentials: GitCredentials{
				TokenSecretRef: "sec-1",
				SSHKey:         SSHKeyRef{Name: "key-1"},
			},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := p.Clone(context.Background(), tt.req)
			var invalid *GitCloneInvalidError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected GitCloneInvalidError, got %T: %v", err, err)
			}
		})
	}
}

func TestGitProvisioner_Clone_TokenErrors(t *testing.T) {
	t.Parallel()

	t.Run("token requires https", func(t *testing.T) {
		t.Parallel()
		p := &GitProvisioner{}
		_, err := p.Clone(context.Background(), &GitCloneRequest{
			RepoURL: "http://example.com/r.git",
			Dir:     t.TempDir(),
			GitCredentials: GitCredentials{
				TokenSecretRef: "sec-1",
			},
		})
		var urlErr *GitRepoURLError
		if !errors.As(err, &urlErr) {
			t.Fatalf("expected GitRepoURLError, got %T: %v", err, err)
		}
	})

	t.Run("no secrets service", func(t *testing.T) {
		t.Parallel()
		p := &GitProvisioner{}
		_, err := p.Clone(context.Background(), &GitCloneRequest{
			RepoURL: "https://example.com/r.git",
			Dir:     t.TempDir(),
			GitCredentials: GitCredentials{
				TokenSecretRef: "sec-1",
			},
		})
		var credErr *GitCredentialError
		if !errors.As(err, &credErr) {
			t.Fatalf("expected GitCredentialError, got %T: %v", err, err)
		}
	})

	t.Run("secret resolution fails", func(t *testing.T) {
		t.Parallel()
		resolveErr := &ProjectInvalidError{Message: "boom"}
		p := &GitProvisioner{Secrets: &fakeGitSecrets{
			getFn: func(ctx context.Context, req *GetSecretRequest) (*GetSecretResponse, error) {
				return nil, resolveErr
			},
		}}
		_, err := p.Clone(context.Background(), &GitCloneRequest{
			RepoURL: "https://example.com/r.git",
			Dir:     t.TempDir(),
			GitCredentials: GitCredentials{
				TokenSecretRef: "sec-1",
			},
		})
		var credErr *GitCredentialError
		if !errors.As(err, &credErr) {
			t.Fatalf("expected GitCredentialError, got %T: %v", err, err)
		}
		if !errors.Is(err, resolveErr) {
			t.Error("expected the underlying resolution error to be wrapped")
		}
	})

	t.Run("empty token", func(t *testing.T) {
		t.Parallel()
		p := &GitProvisioner{Secrets: &fakeGitSecrets{
			getFn: func(ctx context.Context, req *GetSecretRequest) (*GetSecretResponse, error) {
				return &GetSecretResponse{Data: []byte("  ")}, nil
			},
		}}
		_, err := p.Clone(context.Background(), &GitCloneRequest{
			RepoURL: "https://example.com/r.git",
			Dir:     t.TempDir(),
			GitCredentials: GitCredentials{
				TokenSecretRef: "sec-1",
			},
		})
		var credErr *GitCredentialError
		if !errors.As(err, &credErr) {
			t.Fatalf("expected GitCredentialError, got %T: %v", err, err)
		}
	})
}

func TestGitProvisioner_TokenAuth_TokenNeverInArgs(t *testing.T) {
	t.Parallel()

	const token = "sup3r-secret-token"
	p := &GitProvisioner{Secrets: &fakeGitSecrets{
		getFn: func(ctx context.Context, req *GetSecretRequest) (*GetSecretResponse, error) {
			return &GetSecretResponse{Data: []byte(token)}, nil
		},
	}}
	env, args, err := p.tokenAuth(context.Background(), "https://example.com/r.git",
		GitCredentials{TokenSecretRef: "sec-1"})
	if err != nil {
		t.Fatalf("tokenAuth error: %v", err)
	}
	for _, arg := range args {
		if strings.Contains(arg, token) {
			t.Fatalf("token leaked into git argv: %q", arg)
		}
	}
	foundToken, foundUser := false, false
	for _, e := range env {
		if e == gitTokenEnvVar+"="+token {
			foundToken = true
		}
		if e == gitUsernameEnvVar+"="+defaultGitTokenUsername {
			foundUser = true
		}
	}
	if !foundToken || !foundUser {
		t.Errorf("token/username env not prepared: token=%v user=%v", foundToken, foundUser)
	}
}

func TestGitProvisioner_Clone_SSHErrors(t *testing.T) {
	t.Parallel()

	t.Run("no ssh signer resolver", func(t *testing.T) {
		t.Parallel()
		p := &GitProvisioner{}
		_, err := p.Clone(context.Background(), &GitCloneRequest{
			RepoURL: "git@github.com:org/repo.git",
			Dir:     t.TempDir(),
			GitCredentials: GitCredentials{
				TenantID: "tenant-a",
				SSHKey:   SSHKeyRef{Name: "deploy-key"},
			},
		})
		var credErr *GitCredentialError
		if !errors.As(err, &credErr) {
			t.Fatalf("expected GitCredentialError, got %T: %v", err, err)
		}
	})

	t.Run("ssh key with non-ssh url", func(t *testing.T) {
		t.Parallel()
		p := &GitProvisioner{}
		_, err := p.Clone(context.Background(), &GitCloneRequest{
			RepoURL: "https://example.com/r.git",
			Dir:     t.TempDir(),
			GitCredentials: GitCredentials{
				SSHKey: SSHKeyRef{Name: "deploy-key"},
			},
		})
		var urlErr *GitRepoURLError
		if !errors.As(err, &urlErr) {
			t.Fatalf("expected GitRepoURLError, got %T: %v", err, err)
		}
	})
}

func TestParseGitSSHEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		url      string
		wantUser string
		wantHost string
		wantPort int
		wantErr  bool
	}{
		{"scp-like", "git@github.com:org/repo.git", "git", "github.com", 0, false},
		{"scp-like no user", "github.com:org/repo.git", "", "github.com", 0, false},
		{"ssh scheme", "ssh://git@git.example.com/org/repo.git", "git", "git.example.com", 0, false},
		{"ssh scheme with port", "ssh://git@git.example.com:2222/org/repo.git", "git", "git.example.com", 2222, false},
		{"https rejected", "https://example.com/r.git", "", "", 0, true},
		{"local path rejected", "/srv/git/repo.git", "", "", 0, true},
		{"relative path rejected", "repo/path", "", "", 0, true},
		{"empty host scp", "git@:org/repo.git", "", "", 0, true},
		{"ssh scheme no host", "ssh:///org/repo.git", "", "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseGitSSHEndpoint(tt.url)
			if tt.wantErr {
				var urlErr *GitRepoURLError
				if !errors.As(err, &urlErr) {
					t.Fatalf("expected GitRepoURLError, got %T: %v", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGitSSHEndpoint(%q) error: %v", tt.url, err)
			}
			if got.user != tt.wantUser || got.host != tt.wantHost || got.port != tt.wantPort {
				t.Errorf("parseGitSSHEndpoint(%q) = %+v, want user=%q host=%q port=%d",
					tt.url, got, tt.wantUser, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestBuildGitSSHCommand(t *testing.T) {
	t.Parallel()

	cmd := buildGitSSHCommand("/tmp/xkey-ssh-1/known_hosts")
	for _, want := range []string{
		"BatchMode=yes",
		"IdentitiesOnly=yes",
		"StrictHostKeyChecking=accept-new",
		"UserKnownHostsFile='/tmp/xkey-ssh-1/known_hosts'",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("GIT_SSH_COMMAND %q missing %q", cmd, want)
		}
	}
}

func TestShellSingleQuote(t *testing.T) {
	t.Parallel()

	if got := shellSingleQuote("/a b/c"); got != "'/a b/c'" {
		t.Errorf("shellSingleQuote = %q", got)
	}
	if got := shellSingleQuote("a'b"); got != `'a'\''b'` {
		t.Errorf("shellSingleQuote with quote = %q", got)
	}
}

func TestGitCloneArgs(t *testing.T) {
	t.Parallel()

	got := gitCloneArgs("https://example.com/r.git", "", "/ws")
	want := []string{"clone", "--", "https://example.com/r.git", "/ws"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("gitCloneArgs = %v, want %v", got, want)
	}

	got = gitCloneArgs("https://example.com/r.git", "dev", "/ws")
	want = []string{"clone", "--branch", "dev", "--single-branch", "--", "https://example.com/r.git", "/ws"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("gitCloneArgs with branch = %v, want %v", got, want)
	}
}

// =============================================================================
// The write path
// =============================================================================
//
// Against a REAL git repository in a temp dir, not a double. The behaviour
// under test is what the git binary does with the arguments this code builds,
// and a fake would only confirm the arguments we chose to build — which is the
// fixture blindness that let two wrong Gitea field names ship earlier in this
// initiative. Nothing here touches a network: the "remote" is a bare repo on
// disk, so push is exercised without credentials.

// initGitRepo creates a repository with one commit and returns its directory.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	runTestGit(t, dir, "init", "--initial-branch=main")
	runTestGit(t, dir, "config", "user.name", "fixture")
	runTestGit(t, dir, "config", "user.email", "fixture@example.com")
	// The host's GLOBAL config sets commit.gpgsign on a developer machine, and
	// this fixture inherits it: the seed commit then tries to sign with
	// whoever's key that config names and fails. The code under test decides
	// signing explicitly for exactly this reason; the fixture has to do the
	// same to stand in for a clean workspace.
	runTestGit(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o600); err != nil {
		t.Fatalf("seeding the repo: %v", err)
	}
	runTestGit(t, dir, "add", "seed.txt")
	runTestGit(t, dir, "commit", "-m", "seed")
	return dir
}

// runTestGit runs a git command in dir, failing the test on error.
func runTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir}, args...)
	out, err := exec.CommandContext(context.Background(), "git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestBranch_CutsAndChecksOutLocally covers the branch step.
func TestBranch_CutsAndChecksOutLocally(t *testing.T) {
	dir := initGitRepo(t)
	p := &GitProvisioner{}

	if err := p.Branch(context.Background(), &GitBranchRequest{Dir: dir, Name: "issue/32-slug"}); err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if got := runTestGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "issue/32-slug" {
		t.Errorf("checked out %q, want issue/32-slug", got)
	}
}

// TestCommit_StagesOnlyWhatItWasToldTo is the property that keeps build output
// out of history.
//
// A worker's workspace accumulates caches, binaries and editor droppings while
// tools run in it. Defaulting to "stage everything" means those land in a
// commit and are found at review time, so naming nothing stages nothing and a
// caller that means everything says so.
func TestCommit_StagesOnlyWhatItWasToldTo(t *testing.T) {
	dir := initGitRepo(t)
	p := &GitProvisioner{}
	ctx := context.Background()

	for _, name := range []string{"wanted.txt", "build-output.bin"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	// Staging nothing is refused rather than silently meaning everything.
	_, err := p.Commit(ctx, &GitCommitRequest{
		Dir: dir, Message: "m", Author: orchestration.GitAuthor{Name: "a", Email: "a@b.c"},
	})
	if err == nil {
		t.Fatal("a commit naming no paths was accepted; it would have swept up build output")
	}

	sha, err := p.Commit(ctx, &GitCommitRequest{
		Dir:     dir,
		Message: "only the wanted file",
		Author:  orchestration.GitAuthor{Name: "agent", Email: "agent@example.com"},
		Paths:   []string{"wanted.txt"},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha == "" {
		t.Fatal("Commit returned no SHA")
	}

	files := runTestGit(t, dir, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(files, "wanted.txt") {
		t.Errorf("commit does not contain wanted.txt: %q", files)
	}
	if strings.Contains(files, "build-output.bin") {
		t.Error("the commit swept up a file nobody asked to stage")
	}
}

// TestCommit_RecordsTheIdentityItWasGiven proves attribution does not come
// from the host's global git config.
//
// A worker runs on a shared host where global config belongs to whoever set it
// up last, and a commit attributed to the wrong identity is a provenance
// failure that survives in history.
func TestCommit_RecordsTheIdentityItWasGiven(t *testing.T) {
	dir := initGitRepo(t)
	p := &GitProvisioner{}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if _, err := p.Commit(context.Background(), &GitCommitRequest{
		Dir:     dir,
		Message: "attributed",
		Author:  orchestration.GitAuthor{Name: "agent-go-quicbaas", Email: "agent@example.internal"},
		Paths:   []string{"f.txt"},
	}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if got := runTestGit(t, dir, "log", "-1", "--format=%an <%ae>"); got != "agent-go-quicbaas <agent@example.internal>" {
		t.Errorf("author = %q, want the identity passed to Commit", got)
	}
	// The workspace's own config must be untouched, so concurrent workers in
	// sibling worktrees cannot read each other's identity.
	if got := runTestGit(t, dir, "config", "user.name"); got != "fixture" {
		t.Errorf("workspace config user.name = %q; Commit wrote into the workspace", got)
	}
}

// TestCommit_RefusesAnEmptyCommitByDefault keeps a worker that produced
// nothing from recording that it did.
func TestCommit_RefusesAnEmptyCommitByDefault(t *testing.T) {
	dir := initGitRepo(t)
	p := &GitProvisioner{}

	_, err := p.Commit(context.Background(), &GitCommitRequest{
		Dir: dir, Message: "nothing changed",
		Author:     orchestration.GitAuthor{Name: "a", Email: "a@b.c"},
		AllChanges: true,
	})
	if err == nil {
		t.Fatal("an empty commit was recorded, hiding that the work produced nothing")
	}
}

// TestCommit_RefusesAnUnattributedCommit covers the identity validation.
func TestCommit_RefusesAnUnattributedCommit(t *testing.T) {
	dir := initGitRepo(t)
	p := &GitProvisioner{}
	ctx := context.Background()

	cases := map[string]orchestration.GitAuthor{
		"no name":  {Email: "a@b.c"},
		"no email": {Name: "a"},
	}
	for name, identity := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := p.Commit(ctx, &GitCommitRequest{
				Dir: dir, Message: "m", Author: identity, AllChanges: true,
			}); err == nil {
				t.Error("an unattributed commit was accepted")
			}
		})
	}
}

// TestPush_PublishesToTheRemote covers the push against a bare repository on
// disk, which needs no credentials and so isolates the git mechanics from the
// credential path.
func TestPush_PublishesToTheRemote(t *testing.T) {
	remote := t.TempDir()
	runTestGit(t, remote, "init", "--bare", "--initial-branch=main")

	work := initGitRepo(t)
	runTestGit(t, work, "remote", "add", "origin", remote)
	runTestGit(t, work, "push", "origin", "main")

	p := &GitProvisioner{}
	ctx := context.Background()
	if err := p.Branch(ctx, &GitBranchRequest{Dir: work, Name: "issue/32-slug"}); err != nil {
		t.Fatalf("Branch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "new.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	sha, err := p.Commit(ctx, &GitCommitRequest{
		Dir: work, Message: "work", Author: orchestration.GitAuthor{Name: "a", Email: "a@b.c"},
		Paths: []string{"new.txt"},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := p.Push(ctx, &GitPushRequest{
		Dir: work, RepoURL: remote, Branch: "issue/32-slug", SetUpstream: true,
	}); err != nil {
		t.Fatalf("Push: %v", err)
	}

	// The remote's own view is the read-back: an exit code says the command
	// ran, not that the branch arrived.
	if got := runTestGit(t, remote, "rev-parse", "issue/32-slug"); got != sha {
		t.Errorf("remote branch is at %q, want the commit that was pushed (%q)", got, sha)
	}
}

// TestPush_RefusesConflictingCredentials proves the write path validates the
// same credential combination the clone path does, through the same check.
func TestPush_RefusesConflictingCredentials(t *testing.T) {
	err := (&GitProvisioner{}).Push(context.Background(), &GitPushRequest{
		Dir: "/abs", RepoURL: "https://example.com/r.git", Branch: "b",
		GitCredentials: GitCredentials{
			TokenSecretRef: "sec-1",
			SSHKey:         SSHKeyRef{Name: "key-1"},
		},
	})
	if err == nil {
		t.Fatal("a push carrying both credential kinds was accepted")
	}
}

// =============================================================================
// Signing algorithm drift
// =============================================================================

// TestSigningAlgorithmsMatchKMS pins the orchestration signing-algorithm
// literals to the KMS algorithm names. The orchestration package spells them
// as literals because the standalone agent binary must not import pkg/kms; a
// drift here would let a project select an algorithm the KMS does not serve.
func TestSigningAlgorithmsMatchKMS(t *testing.T) {
	if orchestration.SigningAlgorithmEd25519 != kms.AlgEd25519 {
		t.Errorf("orchestration ed25519 literal %q != kms %q", orchestration.SigningAlgorithmEd25519, kms.AlgEd25519)
	}
	if orchestration.SigningAlgorithmECDSAP256 != kms.AlgECDSAP256 {
		t.Errorf("orchestration ECDSA-P256 literal %q != kms %q", orchestration.SigningAlgorithmECDSAP256, kms.AlgECDSAP256)
	}
}
