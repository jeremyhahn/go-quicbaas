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

package gitsign

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

// The acceptance tests point git's gpg.ssh.program at this test binary. When
// git runs it, TestMain sees helperModeEnv and runs the helper instead of the
// tests, with the session key standing in for quicbaas's KMS: the seed reaches
// only this stand-in, never the Helper, which sees a signer as it would over
// MCP.
const (
	helperModeEnv = "GITSIGN_TEST_HELPER_MODE"
	helperSeedEnv = "GITSIGN_TEST_SESSION_KEY_SEED"
)

const committerEmail = "agent-session@acme.example"

func TestMain(m *testing.M) {
	if os.Getenv(helperModeEnv) == "1" {
		os.Exit(runAsHelper())
	}
	os.Exit(m.Run())
}

// runAsHelper is gpg.ssh.program as git runs it.
func runAsHelper() int {
	seed, err := hex.DecodeString(os.Getenv(helperSeedEnv))
	if err != nil || len(seed) != ed25519.SeedSize {
		return 3
	}
	signer, err := gossh.NewSignerFromKey(ed25519.NewKeyFromSeed(seed))
	if err != nil {
		return 3
	}
	key := &remoteKey{signer: signer}
	helper := &Helper{Stdin: os.Stdin, Stdout: os.Stdout, Open: key.open}
	if err := helper.Run(context.Background(), os.Args[1:]); err != nil {
		if _, writeErr := io.WriteString(os.Stderr, err.Error()+"\n"); writeErr != nil {
			return 2
		}
		return 1
	}
	return 0
}

// session is a fresh ed25519 session key and the environment that hands it to
// the stand-in.
type session struct {
	pub gossh.PublicKey
	env []string
}

func sessionKey(t *testing.T) session {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	return session{
		pub: signer.PublicKey(),
		env: []string{helperModeEnv + "=1", helperSeedEnv + "=" + hex.EncodeToString(seed)},
	}
}

// isolatedEnv is an environment in which git reads no host configuration.
func isolatedEnv(home string, extra ...string) []string {
	env := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + filepath.Join(home, "gitconfig"),
		"GIT_AUTHOR_DATE=2026-10-13T12:00:00Z",
		"GIT_COMMITTER_DATE=2026-10-13T12:00:00Z",
	}
	return append(env, extra...)
}

// run runs a real binary and returns its combined output, failing the test on
// a non-zero exit.
func run(t *testing.T, dir string, env []string, stdin []byte, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...) // #nosec G204 -- fixed binaries with test-built arguments.
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// allowedSigners writes an allowed-signers file trusting pub for the git
// namespace under committerEmail.
func allowedSigners(t *testing.T, dir string, pub gossh.PublicKey) string {
	t.Helper()
	line := committerEmail + ` namespaces="git" ` + string(gossh.MarshalAuthorizedKey(pub))
	return writeFile(t, dir, "allowed_signers", []byte(line))
}

func helperProgram(t *testing.T) string {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary: %v", err)
	}
	return program
}

// TestAcceptanceGitCommitVerifies signs a commit through the helper with stock
// git and verifies it with stock git, which runs ssh-keygen -Y verify.
func TestAcceptanceGitCommitVerifies(t *testing.T) {
	key := sessionKey(t)
	pub := key.pub
	home := t.TempDir()
	repo := t.TempDir()
	signers := allowedSigners(t, home, pub)
	env := isolatedEnv(home, key.env...)

	run(t, repo, env, nil, "git", "init", "-q", "-b", "main")
	writeFile(t, repo, "README.md", []byte("signed through quicbaas\n"))
	run(t, repo, env, nil, "git", "add", "README.md")
	run(t, repo, env, nil, "git",
		"-c", "user.name=agent-session",
		"-c", "user.email="+committerEmail,
		"-c", "gpg.format=ssh",
		"-c", "user.signingkey=key::"+strings.TrimSpace(string(gossh.MarshalAuthorizedKey(pub))),
		"-c", "commit.gpgsign=true",
		"-c", "gpg.ssh.program="+helperProgram(t),
		"commit", "-q", "-m", "signed commit")

	// Verification uses the stock ssh-keygen: gpg.ssh.program is not set here.
	out := run(t, repo, isolatedEnv(home), nil, "git",
		"-c", "gpg.ssh.allowedSignersFile="+signers,
		"verify-commit", "-v", "HEAD")
	if !strings.Contains(out, `Good "git" signature for `+committerEmail) {
		t.Fatalf("verify-commit did not report a good signature:\n%s", out)
	}
}

// TestAcceptanceSSHKeygenVerifies checks both output modes with ssh-keygen
// -Y verify directly.
func TestAcceptanceSSHKeygenVerifies(t *testing.T) {
	key := sessionKey(t)
	pub := key.pub
	dir := t.TempDir()
	message := []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n\nsigned data\n")
	keyFile := writeKeyFile(t, dir, pub)
	dataFile := writeFile(t, dir, "buffer", message)
	signers := allowedSigners(t, dir, pub)
	env := isolatedEnv(dir, key.env...)
	program := helperProgram(t)

	run(t, dir, env, nil, program, signArgs(keyFile, dataFile)...)
	stdoutSig := run(t, dir, env, message, program, signArgs(keyFile, "")...)
	stdinSigFile := writeFile(t, dir, "stdin.sig", []byte(stdoutSig))

	for name, sigFile := range map[string]string{"data file": dataFile + signatureSuffix, "stdout": stdinSigFile} {
		t.Run(name, func(t *testing.T) {
			out := run(t, dir, isolatedEnv(dir), message, "ssh-keygen",
				"-Y", "verify", "-f", signers, "-I", committerEmail, "-n", GitNamespace, "-s", sigFile)
			if !strings.Contains(out, `Good "git" signature for `+committerEmail) {
				t.Fatalf("ssh-keygen did not report a good signature:\n%s", out)
			}
		})
	}
}

// TestAcceptanceRefusedCommitIsNotCreated checks git aborts the commit when the
// helper refuses, so an unsigned commit never lands in a signing session.
func TestAcceptanceRefusedCommitIsNotCreated(t *testing.T) {
	key := sessionKey(t)
	other := newEd25519Signer(t).PublicKey()
	home := t.TempDir()
	repo := t.TempDir()
	env := isolatedEnv(home, key.env...)

	run(t, repo, env, nil, "git", "init", "-q", "-b", "main")
	writeFile(t, repo, "README.md", []byte("refused\n"))
	run(t, repo, env, nil, "git", "add", "README.md")

	// git names a key the session does not hold.
	cmd := exec.Command("git",
		"-c", "user.name=agent-session",
		"-c", "user.email="+committerEmail,
		"-c", "gpg.format=ssh",
		"-c", "user.signingkey=key::"+strings.TrimSpace(string(gossh.MarshalAuthorizedKey(other))),
		"-c", "commit.gpgsign=true",
		"-c", "gpg.ssh.program="+helperProgram(t),
		"commit", "-q", "-m", "must not land") // #nosec G204 -- fixed binary with test-built arguments.
	cmd.Dir = repo
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("git commit succeeded with a key the session does not hold:\n%s", out)
	}
	if !strings.Contains(string(out), ErrKey.Error()) {
		t.Fatalf("git did not surface the helper's refusal:\n%s", out)
	}
	head := exec.Command("git", "rev-parse", "--verify", "-q", "HEAD") // #nosec G204 -- fixed binary.
	head.Dir = repo
	head.Env = env
	if err := head.Run(); err == nil {
		t.Fatal("a commit was created")
	}
}
