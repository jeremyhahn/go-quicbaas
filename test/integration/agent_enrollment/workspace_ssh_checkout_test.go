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

//go:build integration && rest

package agent_enrollment_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// A worker checks out over ssh against a forge it was PINNED to (#749).
// =============================================================================
//
// The burn-in's evidence was `git clone --origin origin ssh://git@…/…` exiting
// 128 inside the worker with `sh: ssh: not found`, and — once ssh was there —
// nothing anywhere that told the container which host key the forge has. The
// unit tier pins the rendering of the known_hosts and the GIT_SSH_COMMAND
// against no ssh at all; what it cannot pin is whether a REAL OpenSSH client
// accepts the file this daemon writes, and whether a real sshd offering a
// DIFFERENT key is actually refused.
//
// So this scenario runs a real sshd, a real git and a real ssh, and drives
// them with the production `orchestration.NewWorkspaceSpecFiles`,
// `WorkspaceManager.ProvisionSession` and `orchestration.ApplyWorkspaceSpec` —
// the calls pkg/sdlc's runner and the executing host make per attempt. NOTHING about the forge is written down here: the sshd
// generates its host key at container start and the test reads it back, which
// is exactly what an operator does with `ssh-keyscan` before binding.
//
// THE CLIENT AND THE SERVER SHARE ONE CONTAINER, over loopback. A second
// container would need a docker network whose addresses the test would have to
// know, and it would prove nothing more: ssh's host-key verification happens
// in the client against the file it was given, and the connection is a real
// one either way.

// sshCheckoutEntrypoint brings up the fixture forge: a fresh host key, a `git`
// account reachable with a generated client key, and one bare repository with
// a commit in it. It writes /tmp/forge-ready last, so the test waits on an
// observable condition rather than an interval.
const sshCheckoutEntrypoint = `#!/bin/sh
set -eu
ssh-keygen -A
adduser -D -h /home/git git
# adduser -D leaves "!" in the shadow entry, and sshd refuses a LOCKED account
# even for public-key auth. The fixture's git account has no password at all
# ("*"), so the client key is the only way in.
sed -i 's/^git:!:/git:*:/' /etc/shadow
mkdir -p /home/git/.ssh
ssh-keygen -t ed25519 -N '' -f /root/client_key -q
cp /root/client_key.pub /home/git/.ssh/authorized_keys
chown -R git:git /home/git/.ssh
chmod 700 /home/git/.ssh
chmod 600 /home/git/.ssh/authorized_keys
mkdir -p /root/.ssh
printf 'Host *\n  IdentityFile /root/client_key\n  IdentitiesOnly yes\n' > /root/.ssh/config
chmod 700 /root/.ssh
chmod 600 /root/.ssh/config
mkdir -p /srv
git init --bare -q /srv/repo.git
git init -q /tmp/seed
cd /tmp/seed
git -c user.email=fixture@invalid -c user.name=fixture commit -q --allow-empty -m "the forge has a commit"
git push -q /srv/repo.git HEAD:refs/heads/main
# The bare repository's default HEAD is refs/heads/master, which a clone would
# land on empty; point it at the branch that actually has the commit.
git --git-dir=/srv/repo.git symbolic-ref HEAD refs/heads/main
chown -R git:git /srv
mkdir -p /run/quicbaas/workspace
touch /tmp/forge-ready
exec /usr/sbin/sshd -D -e
`

// sshCheckoutDockerfile is the fixture forge's image: an sshd, a git and the
// ssh client, on the same base the Software Factory worker image pins.
//
// IT IS NOT THE WORKER IMAGE. The worker image's own guarantee — that `ssh`
// and `git` exist and run as the spawn's uid — is asserted by a build-time
// smoke check in its Dockerfile and by `make test-worker-image`; building it
// here would mean installing the agent from npm on every run.
const sshCheckoutDockerfile = `FROM alpine:3.21
RUN apk add --no-cache openssh openssh-client git
COPY entrypoint /usr/local/bin/entrypoint
RUN chmod 0755 /usr/local/bin/entrypoint
CMD ["/usr/local/bin/entrypoint"]
`

// sshCheckoutRepoURL is what the project's Git.RepoURL is for this scenario.
// The host is the container's own loopback: the forge and the client are the
// same container, and no deployment address appears anywhere.
const sshCheckoutRepoURL = "ssh://git@127.0.0.1/srv/repo.git"

// sshForge is one running fixture forge.
type sshForge struct {
	// container is the docker container id sshd runs in, and the client
	// commands are exec'd into.
	container string
}

// startSSHForge builds the fixture image, starts it and waits for sshd.
func startSSHForge(t *testing.T) *sshForge {
	t.Helper()

	ctxDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ctxDir, "entrypoint"), []byte(sshCheckoutEntrypoint), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ctxDir, "Dockerfile"), []byte(sshCheckoutDockerfile), 0o644))

	tag := "quicbaas-ssh-forge-test:" + strings.ToLower(strings.NewReplacer("/", "-", "_", "-").Replace(t.Name()))
	buildCtx, cancelBuild := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelBuild()
	out, err := exec.CommandContext(buildCtx, "docker", "build", "-t", tag, ctxDir).CombinedOutput()
	require.NoError(t, err, "docker build failed: %s", out)
	t.Cleanup(func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = exec.CommandContext(rmCtx, "docker", "rmi", "-f", tag).Run()
	})

	runCtx, cancelRun := context.WithTimeout(context.Background(), time.Minute)
	defer cancelRun()
	idOut, err := exec.CommandContext(runCtx, "docker", "run", "-d", "--rm", tag).CombinedOutput()
	require.NoError(t, err, "docker run failed: %s", idOut)
	forge := &sshForge{container: strings.TrimSpace(string(idOut))}
	require.NotEmpty(t, forge.container)
	t.Cleanup(func() {
		killCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = exec.CommandContext(killCtx, "docker", "kill", forge.container).Run()
	})

	require.Eventually(t, func() bool {
		_, execErr := forge.exec(t, nil, "test", "-f", "/tmp/forge-ready")
		return execErr == nil
	}, 2*time.Minute, 500*time.Millisecond, "the fixture forge's sshd never came up")
	return forge
}

// exec runs one command inside the fixture container with env applied, and
// returns its combined output. It is how the test plays the part the sdlc
// runner's container exec plays in production.
func (f *sshForge) exec(t *testing.T, env map[string]string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	argv := []string{"exec"}
	for name, value := range env {
		argv = append(argv, "-e", name+"="+value)
	}
	argv = append(argv, f.container)
	argv = append(argv, args...)
	out, err := exec.CommandContext(ctx, "docker", argv...).CombinedOutput()
	return string(out), err
}

// hostKey reads the ed25519 host public key the fixture's sshd generated. It
// is the line an operator pastes into the binding's forge.ssh_host_key.
func (f *sshForge) hostKey(t *testing.T) string {
	t.Helper()
	out, err := f.exec(t, nil, "cat", "/etc/ssh/ssh_host_ed25519_key.pub")
	require.NoError(t, err, "reading the fixture forge's host key: %s", out)
	return strings.TrimSpace(out)
}

// sshWorkspace is one attempt's ssh workspace as the production path leaves
// it: the spec the daemon decided, and the spec directory the executing host
// wrote beside the workspace it provisioned.
type sshWorkspace struct {
	files   *orchestration.WorkspaceSpecFiles
	specDir string
}

// install copies a written workspace's known_hosts into the container at the
// path the spec mount would put it, and returns the container path.
//
// `docker cp` rather than a bind mount: a bind SOURCE is resolved by the
// docker DAEMON, and this suite may be running against a mounted socket where
// a path this process created does not exist daemon-side.
func (f *sshForge) install(t *testing.T, ws sshWorkspace) string {
	t.Helper()
	target := filepath.Join(orchestration.WorkspaceSpecDir, ws.files.Spec.KnownHostsFile)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	source := filepath.Join(ws.specDir, orchestration.WorkspaceKnownHostsFile)
	out, err := exec.CommandContext(ctx, "docker", "cp", source, f.container+":"+target).CombinedOutput()
	require.NoError(t, err, "docker cp known_hosts failed: %s", out)
	return target
}

// materializeSSHWorkspace runs the production path for a project cloned over
// ssh with hostKey pinned on its binding: the daemon decides the spec, and the
// executing host provisions the named workspace and writes the spec beside it.
func materializeSSHWorkspace(t *testing.T, hostKey string) sshWorkspace {
	t.Helper()
	spec, err := orchestration.NewWorkspaceSpec(&orchestration.Project{
		ID: "p-749", TenantID: "t-749", Name: "repo",
		Git: orchestration.ProjectGit{RepoURL: sshCheckoutRepoURL, DefaultBranch: "main"},
	}, orchestration.WorkspaceBranchPlan{Name: "main"})
	require.NoError(t, err)
	spec.SSHHostKey = hostKey

	files, err := orchestration.NewWorkspaceSpecFiles(spec, nil)
	require.NoError(t, err, "deciding an ssh workspace with a pinned key failed")
	require.Equal(t, orchestration.WorkspaceKnownHostsFile, files.Spec.KnownHostsFile)

	host, err := orchestration.NewWorkspaceManager(t.TempDir())
	require.NoError(t, err)
	provisioned, err := host.ProvisionSession(&orchestration.SessionWorkspace{
		TenantID: "t-749", ProjectID: "p-749", Key: "attempt-1", Spec: files,
	})
	require.NoError(t, err, "provisioning the ssh workspace failed")
	t.Cleanup(provisioned.Release)
	return sshWorkspace{files: files, specDir: provisioned.SpecDir}
}

// wrongHostKeyLine is a key no host holds: a freshly generated ed25519 public
// key, rendered as the line a binding pins. It stands for the operator who
// pinned the wrong forge, and for the machine that answered in the real
// forge's place.
func wrongHostKeyLine(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := ssh.NewPublicKey(pub)
	require.NoError(t, err)
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}

// checkoutEnv is the environment the spawn would carry for this workspace:
// the production ApplyWorkspaceSpec's GIT_SSH_COMMAND, which names the
// known_hosts by its path INSIDE the container.
func checkoutEnv(t *testing.T, ws sshWorkspace) map[string]string {
	t.Helper()
	req := &orchestration.SpawnSessionRequest{
		Backend:   "claude",
		Workspace: &orchestration.SessionWorkspace{TenantID: "t-749", ProjectID: "p-749", Key: "attempt-1"},
	}
	orchestration.ApplyWorkspaceSpec(req, ws.files, "")
	command := req.Env[orchestration.WorkspaceSSHCommandEnv]
	require.NotEmpty(t, command, "an ssh workspace produced no GIT_SSH_COMMAND")
	return map[string]string{orchestration.WorkspaceSSHCommandEnv: command}
}

// TestE2E_SSHCheckout_ClonesWithThePinnedHostKeyAndRefusesAWrongOne is the
// scenario the burn-in blocked on, both halves in one run against one forge.
//
// With the forge's OWN key pinned, `git clone ssh://…` succeeds under
// StrictHostKeyChecking=yes with no ~/.ssh/known_hosts anywhere — the pinning
// is doing the work, not a learned key. With a key the forge does not hold,
// the identical clone FAILS on host-key verification. A test that only
// asserted the first half would pass just as well against accept-new.
func TestE2E_SSHCheckout_ClonesWithThePinnedHostKeyAndRefusesAWrongOne(t *testing.T) {
	forge := startSSHForge(t)

	// Nothing may have learned this host already: the refusal half is only
	// meaningful if the client has no other way to recognise the forge.
	out, err := forge.exec(t, nil, "test", "-e", "/root/.ssh/known_hosts")
	require.Error(t, err, "the fixture already holds a known_hosts, which would mask the pinning: %s", out)

	t.Run("the pinned key clones", func(t *testing.T) {
		ws := materializeSSHWorkspace(t, forge.hostKey(t))
		forge.install(t, ws)

		out, cloneErr := forge.exec(t, checkoutEnv(t, ws),
			"git", "clone", "--origin", "origin", sshCheckoutRepoURL, "/tmp/clone-pinned")
		require.NoError(t, cloneErr, "the clone against the pinned forge failed: %s", out)

		log, logErr := forge.exec(t, nil, "git", "-C", "/tmp/clone-pinned", "log", "-1", "--format=%s")
		require.NoError(t, logErr, "the clone produced no repository: %s", log)
		require.Contains(t, log, "the forge has a commit")
	})

	t.Run("a wrong key is refused", func(t *testing.T) {
		ws := materializeSSHWorkspace(t, wrongHostKeyLine(t))
		forge.install(t, ws)

		out, cloneErr := forge.exec(t, checkoutEnv(t, ws),
			"git", "clone", "--origin", "origin", sshCheckoutRepoURL, "/tmp/clone-wrong")
		require.Error(t, cloneErr,
			"a clone against a forge whose key does not match the pin SUCCEEDED; the pinning is not being enforced: %s", out)
		require.Contains(t, strings.ToLower(out), "host key verification failed",
			"the clone failed for some other reason than the host key: %s", out)

		_, gitErr := forge.exec(t, nil, "test", "-d", "/tmp/clone-wrong/.git")
		require.Error(t, gitErr, "the refused clone left a repository behind")
	})
}

// TestE2E_SSHCheckout_AnUnpinnedForgeIsRefusedBeforeAnythingRuns is the other
// half of the contract: the daemon does not reach a container at all when the
// binding pinned no key for an ssh remote. No sshd is needed — the refusal is
// the point, and it happens while the workspace is being written.
func TestE2E_SSHCheckout_AnUnpinnedForgeIsRefusedBeforeAnythingRuns(t *testing.T) {
	spec, err := orchestration.NewWorkspaceSpec(&orchestration.Project{
		ID: "p-749", TenantID: "t-749", Name: "repo",
		Git: orchestration.ProjectGit{RepoURL: sshCheckoutRepoURL, DefaultBranch: "main"},
	}, orchestration.WorkspaceBranchPlan{Name: "main"})
	require.NoError(t, err)

	files, err := orchestration.NewWorkspaceSpecFiles(spec, nil)
	require.Error(t, err, "an ssh checkout with no pinned host key was decided")
	require.Nil(t, files, "a refused spec has nothing to write")
	require.ErrorIs(t, err, orchestration.ErrSSHHostKeyInvalid)
	require.Contains(t, err.Error(), "forge.ssh_host_key")
}
