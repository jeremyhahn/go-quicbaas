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
	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// =============================================================================
// The worker's signing agent
// =============================================================================
//
// The delivery's commit is made inside the worker's CONTAINER and signed with
// the project's KMS-held key. The key does not travel; a socket does. These
// tests prove the socket lands where the caller said (because a bind-mount
// source has to be somewhere the operator's mount policy admits), that a real
// git commit signed against it VERIFIES, and that the agent's directory does
// not outlive the attempt.

// signingTestProject is a project that signs, with the repository reached
// over https so the access key plays no part.
func signingTestProject() *orchestration.Project {
	return &orchestration.Project{
		ID:       "project-1",
		TenantID: "tenant-1",
		Name:     "api",
		Git: orchestration.ProjectGit{
			RepoURL:       "https://forge.test/acme/api.git",
			DefaultBranch: "main",
			Author: orchestration.GitAuthor{
				Name:  "agent-quicbaas",
				Email: "agent-quicbaas@example.com",
			},
			Signing: orchestration.CommitSigning{
				Enabled: true,
				Key:     orchestration.KMSKeyRef{Name: "workspace-signing", Scope: "service"},
			},
		},
	}
}

// TestSDLCSigningAgent_SignsAContainersCommitAndTheSignatureVerifies is the
// proof the whole phase rests on.
//
// It starts the agent exactly as the runner does — socket directory named by
// the caller, under a root the mount policy would admit — and then runs the
// COMMIT THE PUBLISHER BUILDS, with SSH_AUTH_SOCK pointing at that socket and
// the signing key given by value, the way a container's git reaches it. Then
// it asks git to verify the result against the public half. A test asserting
// only that the flags were chosen would pass against an agent that never
// answered and a commit carrying no signature at all.
func TestSDLCSigningAgent_SignsAContainersCommitAndTheSignatureVerifies(t *testing.T) {
	stockSSHKeygenResolvesTheUid(t)
	signer := ed25519TestSigner(t)
	resolver := &fakeSignerResolver{signer: signer}
	agent, err := NewSDLCSigningAgent(&GitProvisioner{SSHSigner: resolver})
	if err != nil {
		t.Fatalf("NewSDLCSigningAgent: %v", err)
	}

	// The workspace root stands in for the project's own workspace, which is
	// the directory the operator's mount policy already admits. It is made
	// 0700 deliberately: the agent refuses a parent that is group- or
	// world-writable, because in one of those another user can unlink the
	// socket and bind their own agent at the same path.
	socketDir := filepath.Join(privateTestDir(t), "sa-01")
	project := signingTestProject()

	session, err := agent.StartSigningAgent(context.Background(), project, socketDir, sdlc.SigningScopeDelivery)
	if err != nil {
		t.Fatalf("StartSigningAgent: %v", err)
	}
	defer session.Stop(context.Background())

	if want := filepath.Join(socketDir, sdlc.SigningSocketFile); session.SocketPath != want {
		t.Fatalf("socket = %q, want the path a mount could name before the session existed: %q", session.SocketPath, want)
	}
	if _, statErr := os.Stat(session.SocketPath); statErr != nil {
		t.Fatalf("the socket the spawn would mount does not exist: %v", statErr)
	}
	if resolver.asked.Owner != project.TenantID {
		t.Errorf("the key was resolved for owner %q, want the project's tenant", resolver.asked.Owner)
	}

	// The commit the publisher builds, run where a container's git would run
	// it: the identity by value, the signing decision stated, and the key
	// reached only through the socket.
	dir := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}
	runTestGit(t, dir, "add", "work.txt")
	commit := exec.CommandContext(context.Background(), "git",
		"-C", dir,
		"-c", "user.name="+project.Git.Author.Name,
		"-c", "user.email="+project.Git.Author.Email,
		"-c", "gpg.format=ssh",
		"-c", "user.signingkey=key::"+session.PublicKey,
		"-c", "commit.gpgsign=true",
		"commit", "-m", "signed inside the container",
	)
	commit.Env = append(os.Environ(), "SSH_AUTH_SOCK="+session.SocketPath)
	if out, commitErr := commit.CombinedOutput(); commitErr != nil {
		t.Fatalf("the container's commit failed: %v: %s", commitErr, out)
	}

	raw := runTestGit(t, dir, "cat-file", "commit", "HEAD")
	if !strings.Contains(raw, "BEGIN SSH SIGNATURE") {
		t.Fatalf("the commit carries no SSH signature:\n%s", raw)
	}
	allowed := filepath.Join(t.TempDir(), "allowed_signers")
	entry := project.Git.Author.Email + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if err := os.WriteFile(allowed, []byte(entry+"\n"), 0o600); err != nil {
		t.Fatalf("writing allowed signers: %v", err)
	}
	out := runTestGit(t, dir, "-c", "gpg.ssh.allowedSignersFile="+allowed, "verify-commit", "HEAD")
	if !strings.Contains(out, "Good") {
		t.Fatalf("git did not accept the signature made through the mounted socket:\n%s", out)
	}
}

// TestSDLCSigningAgent_TheDirectoryIsOwnedAndRemoved: the session CREATES the
// directory — which is what makes a 0600 socket inside it safe — refuses one
// that already exists rather than adopting its contents, and removes both on
// stop so nothing is left in the tenant's tree.
func TestSDLCSigningAgent_TheDirectoryIsOwnedAndRemoved(t *testing.T) {
	agent, err := NewSDLCSigningAgent(&GitProvisioner{SSHSigner: &fakeSignerResolver{signer: ed25519TestSigner(t)}})
	if err != nil {
		t.Fatalf("NewSDLCSigningAgent: %v", err)
	}
	socketDir := filepath.Join(privateTestDir(t), "sa-01")

	session, err := agent.StartSigningAgent(context.Background(), signingTestProject(), socketDir, sdlc.SigningScopeDelivery)
	if err != nil {
		t.Fatalf("StartSigningAgent: %v", err)
	}
	info, err := os.Stat(socketDir)
	if err != nil {
		t.Fatalf("the session created no directory: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("socket directory mode = %o, want 0700; a 0600 socket is only safe inside one", perm)
	}

	// A second session on the same directory is refused, never adopted.
	_, err = agent.StartSigningAgent(context.Background(), signingTestProject(), socketDir, sdlc.SigningScopeDelivery)
	var exists *xkmsssh.AgentSocketDirExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("a second session on an existing directory: %v, want AgentSocketDirExistsError", err)
	}

	session.Stop(context.Background())
	if _, err := os.Stat(socketDir); !os.IsNotExist(err) {
		t.Errorf("the agent's directory outlived the session: %v", err)
	}
	// Stopping twice is what a teardown that also ran on the error path does.
	session.Stop(context.Background())
}

// TestSDLCSigningAgent_RefusesWhatItCannotServe: every refusal is typed and
// none of them degrades to an unsigned commit, which is the one outcome that
// would be invisible until someone audited history.
func TestSDLCSigningAgent_RefusesWhatItCannotServe(t *testing.T) {
	resolverErr := errors.New("the KMS refused")
	for name, tc := range map[string]struct {
		provisioner *GitProvisioner
		project     *orchestration.Project
		socketDir   string
	}{
		"no project": {
			provisioner: &GitProvisioner{SSHSigner: &fakeSignerResolver{}},
			socketDir:   "unused",
		},
		"no socket directory": {
			provisioner: &GitProvisioner{SSHSigner: &fakeSignerResolver{}},
			project:     signingTestProject(),
		},
		"signing disabled": {
			provisioner: &GitProvisioner{SSHSigner: &fakeSignerResolver{}},
			project: func() *orchestration.Project {
				p := signingTestProject()
				p.Git.Signing.Enabled = false
				return p
			}(),
			socketDir: "unused",
		},
		"no signer resolver": {
			provisioner: &GitProvisioner{},
			project:     signingTestProject(),
			socketDir:   "unused",
		},
		"the resolver refuses": {
			provisioner: &GitProvisioner{SSHSigner: &fakeSignerResolver{err: resolverErr}},
			project:     signingTestProject(),
			socketDir:   "unused",
		},
		"the resolver answers nothing": {
			provisioner: &GitProvisioner{SSHSigner: &fakeSignerResolver{}},
			project:     signingTestProject(),
			socketDir:   "unused",
		},
	} {
		t.Run(name, func(t *testing.T) {
			agent, err := NewSDLCSigningAgent(tc.provisioner)
			if err != nil {
				t.Fatalf("NewSDLCSigningAgent: %v", err)
			}
			dir := tc.socketDir
			if dir == "unused" {
				dir = filepath.Join(privateTestDir(t), "sa-01")
			}
			if _, err := agent.StartSigningAgent(context.Background(), tc.project, dir, sdlc.SigningScopeDelivery); err == nil {
				t.Fatal("the agent started for a configuration it cannot serve")
			}
		})
	}
	if _, err := NewSDLCSigningAgent(nil); !errors.Is(err, sdlc.ErrInvalidInput) {
		t.Fatalf("a nil provisioner: %v, want ErrInvalidInput", err)
	}
}

// TestSDLCSigningAgent_RefusesAKeyOfTheWrongAlgorithm: a policy selecting
// ECDSA-P256 for a FIPS deployment, served by an ed25519 key, would sign every
// commit with an algorithm that deployment forbids — and the signatures would
// verify, so nothing downstream would notice.
func TestSDLCSigningAgent_RefusesAKeyOfTheWrongAlgorithm(t *testing.T) {
	agent, err := NewSDLCSigningAgent(&GitProvisioner{SSHSigner: &fakeSignerResolver{signer: ed25519TestSigner(t)}})
	if err != nil {
		t.Fatalf("NewSDLCSigningAgent: %v", err)
	}
	project := signingTestProject()
	project.Git.Signing.Algorithm = orchestration.SigningAlgorithmECDSAP256

	socketDir := filepath.Join(privateTestDir(t), "sa-01")
	if _, err := agent.StartSigningAgent(context.Background(), project, socketDir, sdlc.SigningScopeDelivery); err == nil {
		t.Fatal("a key of the wrong algorithm was accepted")
	}
	if _, err := os.Stat(socketDir); !os.IsNotExist(err) {
		t.Errorf("a refused start left a directory behind: %v", err)
	}
}

// privateTestDir is a directory private to this user, standing in for a
// project workspace root an operator provisioned.
//
// It is NOT t.TempDir, for two reasons the agent enforces and a test would
// otherwise trip over: t.TempDir's numbered subdirectory is created 0777 and
// lands group-writable under a 002 umask, which the agent refuses; and its
// name carries the whole test function name, which pushes the socket past the
// 108-byte sun_path limit — the same failure MaxSigningSocketPath exists to
// name.
func privateTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "qb-sa-*")
	if err != nil {
		t.Fatalf("creating the workspace root: %v", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("sealing the workspace root: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
