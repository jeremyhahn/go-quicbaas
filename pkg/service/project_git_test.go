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

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
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
			RepoURL:        "https://example.com/r.git",
			Dir:            "/abs",
			TokenSecretRef: "sec-1",
			SSHKey:         SSHKeyRef{Name: "key-1"},
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
			RepoURL:        "http://example.com/r.git",
			Dir:            t.TempDir(),
			TokenSecretRef: "sec-1",
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
			RepoURL:        "https://example.com/r.git",
			Dir:            t.TempDir(),
			TokenSecretRef: "sec-1",
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
			RepoURL:        "https://example.com/r.git",
			Dir:            t.TempDir(),
			TokenSecretRef: "sec-1",
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
			RepoURL:        "https://example.com/r.git",
			Dir:            t.TempDir(),
			TokenSecretRef: "sec-1",
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
	env, args, err := p.tokenAuth(context.Background(), &GitCloneRequest{
		RepoURL:        "https://example.com/r.git",
		Dir:            "/abs",
		TokenSecretRef: "sec-1",
	})
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
			RepoURL:  "git@github.com:org/repo.git",
			Dir:      t.TempDir(),
			TenantID: "tenant-a",
			SSHKey:   SSHKeyRef{Name: "deploy-key"},
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
			SSHKey:  SSHKeyRef{Name: "deploy-key"},
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

// fakeSSHSession stands in for the xkms ConnectSession's teardown half: it
// records whether the context it was handed is bounded and returns whatever
// the test scripted.
type fakeSSHSession struct {
	added    int
	report   xkmsssh.AgentShutdownReport
	err      error
	stopped  bool
	deadline bool
}

func (f *fakeSSHSession) SyncAndStop(ctx context.Context) (int, xkmsssh.AgentShutdownReport, error) {
	f.stopped = true
	_, f.deadline = ctx.Deadline()
	return f.added, f.report, f.err
}

func TestGitProvisioner_StopSSHSession(t *testing.T) {
	t.Parallel()

	t.Run("clean shutdown logs nothing at warn", func(t *testing.T) {
		t.Parallel()
		logger := newAEADTestLogger()
		sess := &fakeSSHSession{added: 2, report: xkmsssh.AgentShutdownReport{Joined: true}}
		(&GitProvisioner{Logger: logger}).stopSSHSession(sess)
		if !sess.stopped {
			t.Fatal("SyncAndStop was not called")
		}
		if !sess.deadline {
			t.Error("teardown context carries no deadline; a wedged signer would hang the clone forever")
		}
		if len(logger.warnings) != 0 {
			t.Errorf("warnings = %q, want none", logger.warnings)
		}
	})

	t.Run("sync failure is logged and does not panic", func(t *testing.T) {
		t.Parallel()
		logger := newAEADTestLogger()
		sess := &fakeSSHSession{err: errors.New("store unavailable"), report: xkmsssh.AgentShutdownReport{Joined: true}}
		(&GitProvisioner{Logger: logger}).stopSSHSession(sess)
		if len(logger.warnings) != 1 || !strings.Contains(logger.warnings[0], "host-key pin sync failed") {
			t.Errorf("warnings = %q, want one host-key sync warning", logger.warnings)
		}
	})

	t.Run("abandoned signing operations are logged", func(t *testing.T) {
		t.Parallel()
		logger := newAEADTestLogger()
		sess := &fakeSSHSession{report: xkmsssh.AgentShutdownReport{Joined: false, Abandoned: 1}}
		(&GitProvisioner{Logger: logger}).stopSSHSession(sess)
		if len(logger.warnings) != 1 || !strings.Contains(logger.warnings[0], "abandoned") {
			t.Errorf("warnings = %q, want one abandoned-signing warning", logger.warnings)
		}
	})

	t.Run("nil logger still stops the session", func(t *testing.T) {
		t.Parallel()
		sess := &fakeSSHSession{err: errors.New("store unavailable"), report: xkmsssh.AgentShutdownReport{Abandoned: 3}}
		(&GitProvisioner{}).stopSSHSession(sess)
		if !sess.stopped {
			t.Fatal("SyncAndStop was not called")
		}
	})
}
