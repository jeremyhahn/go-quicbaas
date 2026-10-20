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
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/nsswrapper"
)

// =============================================================================
// Commit signing
// =============================================================================
//
// The assertion that matters is a VERIFIED SIGNATURE on a real commit, not the
// presence of the right flags. Flags prove the arguments this code chose;
// verification proves git accepted them, the agent answered, and the signature
// belongs to the key the policy named. Every layer between is real.

// fakeSignerResolver serves one prepared signer.
type fakeSignerResolver struct {
	signer ssh.Signer
	err    error
	asked  SSHKeyRef
}

func (f *fakeSignerResolver) Signer(_ context.Context, ref SSHKeyRef) (ssh.Signer, error) {
	f.asked = ref
	return f.signer, f.err
}

// ed25519TestSigner builds a real ed25519 ssh.Signer.
func ed25519TestSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatalf("wrapping: %v", err)
	}
	return signer
}

// stockSSHKeygenResolvesTheUid lets the stock ssh-keygen that git signs and
// verifies with run for a uid with no passwd entry, as the toolchain image
// runs: the commit path inherits this process's environment.
func stockSSHKeygenResolvesTheUid(t *testing.T) {
	t.Helper()
	for _, variable := range nsswrapper.Env(t, t.TempDir()) {
		name, value, _ := strings.Cut(variable, "=")
		t.Setenv(name, value)
	}
}

// TestCommit_SignsAndTheSignatureVerifies is the slice's proof.
//
// It commits through the provisioner with signing enabled and then asks GIT to
// verify the result against an allowed-signers file naming the public half. A
// test asserting only that "-c commit.gpgsign=true" was passed would pass
// against an agent that never answered and a commit that carries no signature
// at all.
func TestCommit_SignsAndTheSignatureVerifies(t *testing.T) {
	stockSSHKeygenResolvesTheUid(t)
	signer := ed25519TestSigner(t)
	dir := initGitRepo(t)

	provisioner := &GitProvisioner{SSHSigner: &fakeSignerResolver{signer: signer}}

	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	const author = "agent-go-quicbaas@example.internal"
	_, err := provisioner.Commit(context.Background(), &GitCommitRequest{
		Dir:        dir,
		Message:    "signed by the module",
		Author:     orchestration.GitAuthor{Name: "agent-go-quicbaas", Email: author},
		AllChanges: true,
		Signing: orchestration.CommitSigning{
			Enabled: true,
			Key:     orchestration.KMSKeyRef{Name: "workspace-signing", Owner: "agent"},
		},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// The commit object must actually carry a signature.
	raw := runTestGit(t, dir, "cat-file", "commit", "HEAD")
	if !strings.Contains(raw, "BEGIN SSH SIGNATURE") {
		t.Fatalf("the commit carries no SSH signature:\n%s", raw)
	}

	// And git must accept it against the public half of the policy's key.
	allowed := filepath.Join(t.TempDir(), "allowed_signers")
	entry := author + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if err := os.WriteFile(allowed, []byte(entry+"\n"), 0o600); err != nil {
		t.Fatalf("writing allowed signers: %v", err)
	}

	out := runTestGit(t, dir, "-c", "gpg.ssh.allowedSignersFile="+allowed, "verify-commit", "HEAD")
	if !strings.Contains(out, "Good") {
		t.Fatalf("git did not accept the signature:\n%s", out)
	}
}

// TestCommit_DisabledPolicySaysSoExplicitly guards the decision the host must
// never make.
//
// A developer machine commonly sets commit.gpgsign globally. Leaving it
// inherited means a module commit carries a human's signature — a provenance
// claim that is wrong and permanent.
func TestCommit_DisabledPolicySaysSoExplicitly(t *testing.T) {
	provisioner := &GitProvisioner{}

	env := []string{}
	args, stop, err := provisioner.prepareSigning(context.Background(), &env, orchestration.CommitSigning{})
	if err != nil {
		t.Fatalf("prepareSigning: %v", err)
	}
	defer stop()

	if strings.Join(args, " ") != "-c commit.gpgsign=false" {
		t.Errorf("args = %v, want signing turned off EXPLICITLY", args)
	}
	if len(env) != 0 {
		t.Errorf("an unsigned commit started an agent: %v", env)
	}
}

// TestCommit_EnabledWithoutAResolverRefuses proves the policy never degrades.
//
// A project that ASKED for signed commits and silently got unsigned ones has a
// gap nobody sees until someone audits history.
func TestCommit_EnabledWithoutAResolverRefuses(t *testing.T) {
	provisioner := &GitProvisioner{} // no SSHSigner

	env := []string{}
	_, stop, err := provisioner.prepareSigning(context.Background(), &env,
		orchestration.CommitSigning{Enabled: true, Key: orchestration.KMSKeyRef{Name: "workspace-signing"}})
	if stop != nil {
		defer stop()
	}
	if err == nil {
		t.Fatal("an enabled policy with no resolver must refuse, never fall back to unsigned")
	}
}

// TestCommit_RefusesAKeyThatIsNotTheAlgorithmThePolicyChose is the FIPS guard.
//
// A deployment selecting ECDSA-P256 because Ed25519 is not FIPS-approved, served
// an ed25519 key, would sign every commit with an algorithm that deployment
// forbids — and the signatures would VERIFY, so nothing downstream would catch
// it. The mismatch has to be refused here or not at all.
func TestCommit_RefusesAKeyThatIsNotTheAlgorithmThePolicyChose(t *testing.T) {
	provisioner := &GitProvisioner{SSHSigner: &fakeSignerResolver{signer: ed25519TestSigner(t)}}

	env := []string{}
	_, stop, err := provisioner.prepareSigning(context.Background(), &env, orchestration.CommitSigning{
		Enabled:   true,
		Algorithm: kms.AlgECDSAP256,
		Key:       orchestration.KMSKeyRef{Name: "workspace-signing"},
	})
	if stop != nil {
		defer stop()
	}
	if err == nil {
		t.Fatal("an ed25519 key was accepted under a P-256 policy; the signature " +
			"would verify and the FIPS selection would be silently ignored")
	}
	if !strings.Contains(err.Error(), "ECDSA-P256") {
		t.Errorf("the error must name the algorithm that was chosen: %v", err)
	}
}

// TestCommit_AcceptsTheMatchingFIPSAlgorithm keeps the guard from refusing
// everything.
func TestCommit_AcceptsTheMatchingFIPSAlgorithm(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatalf("wrapping: %v", err)
	}

	provisioner := &GitProvisioner{SSHSigner: &fakeSignerResolver{signer: signer}}

	env := []string{}
	args, stop, err := provisioner.prepareSigning(context.Background(), &env, orchestration.CommitSigning{
		Enabled:   true,
		Algorithm: kms.AlgECDSAP256,
		Key:       orchestration.KMSKeyRef{Name: "workspace-signing"},
	})
	if err != nil {
		t.Fatalf("a P-256 key under a P-256 policy must be accepted: %v", err)
	}
	defer stop()

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "gpg.format=ssh") || !strings.Contains(joined, "commit.gpgsign=true") {
		t.Errorf("args = %v", args)
	}
	// The public key travels BY VALUE, so no key artefact is written to disk.
	if !strings.Contains(joined, "user.signingkey=key::") {
		t.Errorf("the public key must be passed by value, not as a path: %v", args)
	}
	if len(env) == 0 || !strings.HasPrefix(env[0], "SSH_AUTH_SOCK=") {
		t.Errorf("the agent socket was not handed to the child process: %v", env)
	}
}

// =============================================================================
// Project-driven commits
// =============================================================================

// TestCommitForProject_ReadsAuthorAndSigningFromTheProject is the proof that
// the workspace record drives the commit: the request names a different
// author and no signing, and the commit still carries the project's identity
// and a signature that verifies against the project's key.
func TestCommitForProject_ReadsAuthorAndSigningFromTheProject(t *testing.T) {
	stockSSHKeygenResolvesTheUid(t)
	signer := ed25519TestSigner(t)
	dir := initGitRepo(t)
	resolver := &fakeSignerResolver{signer: signer}
	provisioner := &GitProvisioner{SSHSigner: resolver}

	if err := os.WriteFile(filepath.Join(dir, "work.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	project := &orchestration.Project{
		ID:       "proj-1",
		TenantID: "tenant-1",
		Name:     "workspace",
		Git: orchestration.ProjectGit{
			Author: orchestration.GitAuthor{Name: "agent-go-quicbaas", Email: "agent-go-quicbaas@example.internal"},
			Signing: orchestration.CommitSigning{
				Enabled: true,
				Key:     orchestration.KMSKeyRef{Name: "workspace-signing", Scope: "tenant", Owner: "tenant-1"},
			},
		},
	}

	req := &GitCommitRequest{
		Dir:        dir,
		Message:    "committed under the project's identity",
		Author:     orchestration.GitAuthor{Name: "impostor", Email: "impostor@example.test"},
		AllChanges: true,
	}
	if _, err := provisioner.CommitForProject(context.Background(), project, req); err != nil {
		t.Fatalf("CommitForProject: %v", err)
	}

	if resolver.asked != (SSHKeyRef{Name: "workspace-signing", Scope: "tenant", Owner: "tenant-1"}) {
		t.Fatalf("the signer was asked for %+v, want the project's signing key", resolver.asked)
	}

	raw := runTestGit(t, dir, "cat-file", "commit", "HEAD")
	if !strings.Contains(raw, "author agent-go-quicbaas <agent-go-quicbaas@example.internal>") {
		t.Fatalf("the commit is not attributed to the project's author:\n%s", raw)
	}
	if strings.Contains(raw, "impostor") {
		t.Fatalf("the request's author overrode the project's:\n%s", raw)
	}
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
		t.Fatalf("git did not accept the signature:\n%s", out)
	}
}

// TestCommitForProject_RefusesWhatTheProjectCannotCommitWith covers the
// refusals: no project, no request, a project with no author, and a project
// whose signing policy is enabled without a key. None may reach git.
func TestCommitForProject_RefusesWhatTheProjectCannotCommitWith(t *testing.T) {
	dir := initGitRepo(t)
	provisioner := &GitProvisioner{}
	ctx := context.Background()
	valid := &GitCommitRequest{Dir: dir, Message: "m", AllChanges: true}

	if _, err := provisioner.CommitForProject(ctx, nil, valid); err == nil {
		t.Error("a nil project was accepted")
	}
	if _, err := provisioner.CommitForProject(ctx, &orchestration.Project{}, nil); err == nil {
		t.Error("a nil request was accepted")
	}

	unattributed := &orchestration.Project{}
	if _, err := provisioner.CommitForProject(ctx, unattributed, &GitCommitRequest{Dir: dir, Message: "m", AllChanges: true}); err == nil {
		t.Error("a project with no author committed")
	}

	unsignable := &orchestration.Project{Git: orchestration.ProjectGit{
		Author:  orchestration.GitAuthor{Name: "a", Email: "a@b.c"},
		Signing: orchestration.CommitSigning{Enabled: true},
	}}
	if _, err := provisioner.CommitForProject(ctx, unsignable, &GitCommitRequest{Dir: dir, Message: "m", AllChanges: true}); err == nil {
		t.Error("an enabled policy with no key committed; it would have degraded to unsigned")
	}

	// Nothing above may have produced a commit.
	if out := runTestGit(t, dir, "rev-list", "--count", "HEAD"); strings.TrimSpace(out) != "1" {
		t.Fatalf("a refused commit reached the repository: %s commits", strings.TrimSpace(out))
	}
}
