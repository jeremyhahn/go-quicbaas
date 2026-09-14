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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/agent"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// A CLI-mode worker under `--sandbox-policy required` runs in a REAL container
// (go-quicbaas#741 / #742).
// =============================================================================
//
// The burn-in's evidence was `ps --ppid <daemon>` showing three `claude`
// processes as children of a daemon started with `--sandbox-policy required`,
// and `docker ps -a --filter label=quicbaas.agent` showing nothing. The unit
// tier pins the routing and the container spec against a ContainerRuntime
// double; what it cannot pin is that the spec the backend builds is one the
// real docker CLI accepts, that the id it hands back names a container that
// exists, or that stdio actually crosses a container boundary.
//
// So this scenario uses the PRODUCTION ExecContainerRuntime and the real docker
// on the host. Every layer is production: a real ACPBackend in CLI mode, a real
// agent.Runtime resolving the required policy and the image map, real
// `docker run -i --rm`, real pipes.
//
// THE ONE SUBSTITUTION IS THE AGENT ITSELF, and it is not a QuicBaaS
// component: the real `claude` needs a paid provider credential and network
// egress, which a test may not have and must never spend. The image built below
// therefore carries a `claude` that speaks the stream-json protocol and nothing
// else. It is installed at the same path and invoked with the same argv the
// production spawn builds, so what is under test is the invocation, not the
// agent's answer.

// sandboxCLIAgentScript is the /usr/local/bin/claude of the test image: a
// faithful stream-json peer. It emits the init record the spawn reads its
// session id from, then answers each newline-delimited user message with a
// result record, and exits when its stdin closes — exactly the lifecycle
// spawnCLI and spawnContainerCLI both depend on.
const sandboxCLIAgentScript = `#!/bin/sh
printf '{"type":"system","subtype":"init","session_id":"container-cli-session","model":"claude-opus-5"}\n'
while IFS= read -r line; do
  printf '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn"}}\n'
  printf '{"type":"result","subtype":"success","session_id":"container-cli-session","result":"OK"}\n'
done
`

// sandboxCLIDockerfile installs that agent into a stock alpine. The base is the
// one the real Software Factory worker image pins, so a host that can build the
// worker can build this.
const sandboxCLIDockerfile = `FROM alpine:3.21
COPY claude /usr/local/bin/claude
RUN chmod 0755 /usr/local/bin/claude
CMD ["claude"]
`

// buildSandboxCLIImage builds the throwaway agent image and returns its tag.
// The build context is a directory this process owns and the docker CLI streams
// it to the daemon, so this works identically when the test runs on a host and
// when it runs in the integration harness over the mounted docker socket —
// unlike a bind mount, whose source is resolved by the DAEMON.
func buildSandboxCLIImage(t *testing.T) string {
	t.Helper()

	ctxDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ctxDir, "claude"), []byte(sandboxCLIAgentScript), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ctxDir, "Dockerfile"), []byte(sandboxCLIDockerfile), 0o644))

	tag := "quicbaas-sandbox-cli-test:" + strings.ToLower(t.Name())
	tag = strings.NewReplacer("/", "-", "_", "-").Replace(tag)

	buildCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	build := exec.CommandContext(buildCtx, "docker", "build", "-t", tag, ctxDir)
	out, err := build.CombinedOutput()
	require.NoError(t, err, "docker build failed: %s", out)

	t.Cleanup(func() {
		rmCtx, rmCancel := context.WithTimeout(context.Background(), time.Minute)
		defer rmCancel()
		_ = exec.CommandContext(rmCtx, "docker", "rmi", "-f", tag).Run()
	})
	return tag
}

// newSandboxCLIDaemon wires the daemon exactly as `quicbaas agent serve
// --backends acp --container-runtime docker --sandbox-policy required` does for
// its `claude` backend, over the production container runtime.
func newSandboxCLIDaemon(t *testing.T, image string) *agent.Runtime {
	t.Helper()

	log := spawnLogger(t)
	containerRuntime, err := orchestration.NewExecContainerRuntimeWithCommand(orchestration.RuntimeDocker, log)
	require.NoError(t, err, "docker is required for this scenario: it is what #741 could not produce")

	backend := orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command:      "claude",
		Args:         []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"},
		DefaultModel: "claude-opus-5",
		CLIMode:      true,
	})

	runtime, err := agent.NewRuntime(agent.RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"claude": backend},
		SandboxPolicy:    orchestration.SandboxPolicyRequired,
		ContainerRuntime: containerRuntime,
		ImageMap:         map[string]string{"claude": image},
		Logger:           log,
		MountPolicy:      &orchestration.MountPolicy{AllowedRoots: []string{t.TempDir()}},
	})
	require.NoError(t, err)
	return runtime
}

// dockerContainerExists asks docker whether a container with this id or name is
// known to it. It is the test's `docker ps -a`, and it is the assertion the
// burn-in could not satisfy.
func dockerContainerExists(t *testing.T, id string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Id}}", id).CombinedOutput()
	if err != nil {
		t.Logf("docker inspect %s: %v: %s", id, err, out)
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// TestE2E_RequiredCLISpawn_RunsInARealContainer is the scenario the burn-in
// blocked on: the daemon's own claude backend, its own required policy, and a
// real docker. The session must name a container docker actually created, and
// the agent must answer through it.
func TestE2E_RequiredCLISpawn_RunsInARealContainer(t *testing.T) {
	image := buildSandboxCLIImage(t)
	runtime := newSandboxCLIDaemon(t, image)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	session, err := runtime.Spawn(ctx, "tenant-sandbox-cli", nil, "claude", &orchestration.SpawnRequest{
		ProjectID:  "proj-741",
		Persistent: true,

		// WorkDirCurrent asks for no workspace bind mount. A bind SOURCE is
		// resolved by the docker daemon, not by this process, so a path this
		// test created would not exist daemon-side when the suite runs in the
		// integration harness over the mounted socket. The mount itself is
		// pinned in the unit tier (TestSpawnContainerCLI_SandboxIsBuiltFromThePolicy);
		// what is under test HERE is that a container exists at all.
		WorkDir: orchestration.WorkDirCurrent,
	})
	require.NoError(t, err, "the required spawn of a CLI-mode backend failed")
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Minute)
		defer closeCancel()
		_ = runtime.Close(closeCtx, "claude", session.ID)
	})

	require.NotEmpty(t, session.ContainerID,
		"the required spawn produced no container id: the agent ran on the daemon host with the "+
			"tenant's credential in its environment, and pkg/sdlc refuses the worker as 'runs no container'")
	require.Zero(t, session.PID,
		"the session reports a host pid: the agent is a child of the daemon, not a container process")

	require.True(t, dockerContainerExists(t, session.ContainerID),
		"docker does not know container %q: the id is a fabrication, not a running sandbox", session.ContainerID)

	// And the containerized agent answers over the container's stdio, on the
	// same stream-json protocol the host path speaks.
	promptCtx, promptCancel := context.WithTimeout(context.Background(), time.Minute)
	defer promptCancel()
	resp, promptErr := runtime.Prompt(promptCtx, "claude", &orchestration.PromptRequest{
		SessionID: session.ID,
		Prompt:    "ping",
	})
	require.NoError(t, promptErr, "the prompt did not cross the container boundary")
	require.NotEmpty(t, resp.Content, "the containerized agent returned no content")
}

// TestE2E_RequiredCLISpawn_CloseRemovesTheContainer is the lifecycle half. The
// container runs with --rm and is bound to a context Close cancels, so a closed
// session must not leave a container behind on the daemon host.
func TestE2E_RequiredCLISpawn_CloseRemovesTheContainer(t *testing.T) {
	image := buildSandboxCLIImage(t)
	runtime := newSandboxCLIDaemon(t, image)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	session, err := runtime.Spawn(ctx, "tenant-sandbox-cli", nil, "claude", &orchestration.SpawnRequest{
		ProjectID:  "proj-741-close",
		Persistent: true,
		WorkDir:    orchestration.WorkDirCurrent,
	})
	require.NoError(t, err)
	require.NotEmpty(t, session.ContainerID)

	closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Minute)
	defer closeCancel()
	require.NoError(t, runtime.Close(closeCtx, "claude", session.ID))

	// --rm cleanup is performed by the docker daemon after `docker run` exits,
	// so the assertion waits for an observable condition on a bounded ticker
	// rather than assuming an instant.
	require.Eventually(t, func() bool {
		return !dockerContainerExists(t, session.ContainerID)
	}, 30*time.Second, 500*time.Millisecond,
		"container %s still exists after Close: a closed session leaked a sandbox and --rm never fired",
		session.ContainerID)
}
