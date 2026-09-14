package orchestration

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// The containerized CLI session (#741)
// =============================================================================
//
// These tests assert the property the burn-in could not get: a REQUIRED spawn
// on a CLI-mode backend runs the agent in a container and hands back that
// container's id. They drive the real spawn path through a ContainerRuntime
// double that plays the stream-json protocol over in-memory pipes, so what is
// under test is the backend's own spec, protocol and session wiring — no
// docker, no host process, no network.

// containerCLIRuntime is a ContainerRuntime whose interactive container speaks
// Claude Code's stream-json protocol, and which records every spec it was asked
// to launch so a test can assert on the sandbox that was actually built.
type containerCLIRuntime struct {
	t           *testing.T
	containerID string

	// runErr, when set, is what RunInteractive fails with instead of starting.
	runErr error

	// silent makes the container accept input but never answer, closing its
	// stdout immediately — a container whose command started and died.
	silent bool

	// deadOnArrival closes both pipes before the backend writes anything, so
	// even the handshake write fails — a container that was gone by the time
	// RunInteractive returned.
	deadOnArrival bool

	// notRunning and isRunningErr are what IsRunning answers. The zero value
	// keeps the historical answer — the container is up and the runtime can
	// say so — so every test written before liveness was probed is unchanged.
	notRunning   bool
	isRunningErr error

	mu    sync.Mutex
	specs []*ContainerSpec
	ctxs  []context.Context
}

func (r *containerCLIRuntime) RunInteractive(ctx context.Context, spec *ContainerSpec) (*InteractiveProcess, error) {
	if r.runErr != nil {
		return nil, r.runErr
	}

	r.mu.Lock()
	r.specs = append(r.specs, spec)
	r.ctxs = append(r.ctxs, ctx)
	r.mu.Unlock()

	agentStdin, backendStdin, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	backendStdout, agentStdout, err := os.Pipe()
	if err != nil {
		_ = agentStdin.Close()
		_ = backendStdin.Close()
		return nil, err
	}

	switch {
	case r.deadOnArrival:
		// Nothing is listening: the very first write breaks the pipe.
		_ = agentStdout.Close()
		_ = agentStdin.Close()
	case r.silent:
		// The container accepts the handshake and then says nothing: stdout is
		// already at EOF. Input is drained so the write itself succeeds.
		_ = agentStdout.Close()
		go func() { _, _ = io.Copy(io.Discard, agentStdin) }()
	default:
		go streamJSONMockResponder(r.t, agentStdin, agentStdout, "container-session", "claude-opus-5")
	}

	// Mirror exec.CommandContext: cancelling the context the container was
	// launched with kills `docker run` and closes its stdio.
	go func() {
		<-ctx.Done()
		_ = agentStdin.Close()
		_ = agentStdout.Close()
	}()

	return &InteractiveProcess{
		ContainerID: r.containerID,
		Stdin:       backendStdin,
		Stdout:      backendStdout,
	}, nil
}

// lastSpec returns the spec of the most recent launch, failing the test when
// nothing was launched at all — which is the failure mode these tests exist to
// catch.
func (r *containerCLIRuntime) lastSpec() *ContainerSpec {
	r.t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.specs) == 0 {
		r.t.Fatal("no container was launched: the spawn never reached RunInteractive")
	}
	return r.specs[len(r.specs)-1]
}

func (r *containerCLIRuntime) launches() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.specs)
}

func (r *containerCLIRuntime) Run(context.Context, *ContainerSpec) (string, error) {
	return r.containerID, nil
}
func (r *containerCLIRuntime) Stop(context.Context, string) error   { return nil }
func (r *containerCLIRuntime) Remove(context.Context, string) error { return nil }
func (r *containerCLIRuntime) Exec(context.Context, string, *ExecSpec) (*ExecResult, error) {
	return nil, &ContainerExecError{ContainerID: r.containerID, Cause: errNoExecInThisTest}
}
func (r *containerCLIRuntime) Logs(context.Context, string) (io.ReadCloser, error) {
	return nil, &ContainerNotFoundError{ContainerID: r.containerID}
}
func (r *containerCLIRuntime) IsRunning(context.Context, string) (bool, error) {
	if r.isRunningErr != nil {
		return false, r.isRunningErr
	}
	return !r.notRunning, nil
}
func (r *containerCLIRuntime) IsAvailable() bool                  { return true }
func (r *containerCLIRuntime) Pull(context.Context, string) error { return nil }

// newContainerCLIBackend builds the daemon's own claude backend shape — CLI
// mode, the stream-json flags — with a container runtime attached and a
// tripwire on the host exec seam, so any spawn that escapes to the host fails
// the test rather than passing it quietly.
func newContainerCLIBackend(t *testing.T, rt *containerCLIRuntime) *ACPBackend {
	t.Helper()
	backend := NewACPBackend(ACPBackendConfig{
		Command:      "/usr/local/bin/claude",
		Args:         []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"},
		DefaultModel: "claude-opus-5",
		CLIMode:      true,
	})
	backend.SetContainerRuntime(rt)
	tripwire := &execTripwire{t: t}
	backend.processFactory = tripwire.factory()
	return backend
}

// requiredCLISpawn is the request shape the sdlc worker sends: persistent,
// required, an image, a workspace and a pooled provider credential.
func requiredCLISpawn(workDir string) *SpawnRequest {
	return &SpawnRequest{
		ProjectID:      "proj-1",
		WorkDir:        workDir,
		Persistent:     true,
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "quicbaas-software-factory-worker:0.3.0-alpha",
		SystemPrompt:   "# SDLC worker charter",
		Env:            map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "sk-secret-value"},
	}
}

// TestSpawnContainerCLI_RequiredSpawnLaunchesTheAgentInAContainer is the
// headline test for #741: the exact spawn the sdlc worker issues must reach
// RunInteractive, run the agent CLI as the container's command with the flags
// the host path would have used, and return the container's id.
func TestSpawnContainerCLI_RequiredSpawnLaunchesTheAgentInAContainer(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "quicbaas-agent-abc123"}
	backend := newContainerCLIBackend(t, rt)
	workDir := t.TempDir()

	sess, err := backend.Spawn(t.Context(), requiredCLISpawn(workDir))
	if err != nil {
		t.Fatalf("required CLI spawn failed: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	if sess.ContainerID != rt.containerID {
		t.Errorf("Session.ContainerID = %q, want %q — the runner refuses a worker that runs no container",
			sess.ContainerID, rt.containerID)
	}
	if sess.PID != 0 {
		t.Errorf("Session.PID = %d; a containerized session owns no host process", sess.PID)
	}
	if sess.Status != SessionStateActive {
		t.Errorf("Session.Status = %q, want %q", sess.Status, SessionStateActive)
	}

	spec := rt.lastSpec()
	if spec.Image != "quicbaas-software-factory-worker:0.3.0-alpha" {
		t.Errorf("spec.Image = %q, want the requested worker image", spec.Image)
	}
	if len(spec.Entrypoint) != 0 {
		t.Errorf("spec.Entrypoint = %v; the image entrypoint must be cleared so our command runs", spec.Entrypoint)
	}

	// The command is the agent BY NAME (the image installs it on PATH, not at
	// the daemon's host path) followed by the same stream-json flags the host
	// path uses.
	want := []string{
		"claude",
		"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--no-session-persistence",
		"--system-prompt", "# SDLC worker charter",
		"--model", "claude-opus-5",
	}
	if !slices.Equal(spec.Command, want) {
		t.Errorf("spec.Command =\n  %v\nwant\n  %v", spec.Command, want)
	}
}

// TestSpawnContainerCLI_SandboxIsBuiltFromThePolicy asserts the isolation the
// operator asked for is on the spec — the workspace mount, the network, the
// daemon's uid, the workdir. These are exactly the guarantees that were inert
// while the CLI path ran on the host.
func TestSpawnContainerCLI_SandboxIsBuiltFromThePolicy(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "ctr-sandbox"}
	backend := newContainerCLIBackend(t, rt)
	backend.config.SandboxNetwork = SandboxNetworkNone
	workDir := t.TempDir()

	req := requiredCLISpawn(workDir)
	req.ExtraMounts = []ContainerMount{{Source: "/run/quicbaas/workspace", Target: "/run/quicbaas/workspace", ReadOnly: true}}

	sess, err := backend.Spawn(t.Context(), req)
	if err != nil {
		t.Fatalf("required CLI spawn failed: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	spec := rt.lastSpec()
	if spec.WorkDir != containerWorkDir {
		t.Errorf("spec.WorkDir = %q, want %q", spec.WorkDir, containerWorkDir)
	}
	if spec.Network != SandboxNetworkNone.String() {
		t.Errorf("spec.Network = %q, want the backend's configured sandbox network %q",
			spec.Network, SandboxNetworkNone.String())
	}
	if spec.User == "" {
		t.Error("spec.User is empty; the container must run as the daemon's uid, not root")
	}
	if !spec.AutoRemove {
		t.Error("spec.AutoRemove is false; a session container must not outlive the session")
	}

	var sawWorkspace, sawExtra bool
	for _, m := range spec.Mounts {
		if m.Source == workDir && m.Target == containerWorkDir {
			sawWorkspace = true
		}
		if m.Source == "/run/quicbaas/workspace" && m.ReadOnly {
			sawExtra = true
		}
	}
	if !sawWorkspace {
		t.Errorf("the workspace %q is not bind-mounted at %q: mounts=%v", workDir, containerWorkDir, spec.Mounts)
	}
	if !sawExtra {
		t.Errorf("the mount-policy-authorized extra mount is missing: mounts=%v", spec.Mounts)
	}
	if spec.Labels["quicbaas.agent"] == "" {
		t.Error("spec.Labels is missing quicbaas.agent; the container is not attributable to this daemon")
	}
}

// TestSpawnContainerCLI_CredentialTravelsAsEnvNeverAsArgv is the security
// assertion. The provider credential must reach the container through the
// spec's environment — which the runtime writes to a private 0600 --env-file —
// and must appear in NO argument, where /proc/<pid>/cmdline would publish it.
func TestSpawnContainerCLI_CredentialTravelsAsEnvNeverAsArgv(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "ctr-cred"}
	backend := newContainerCLIBackend(t, rt)

	sess, err := backend.Spawn(t.Context(), requiredCLISpawn(t.TempDir()))
	if err != nil {
		t.Fatalf("required CLI spawn failed: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	spec := rt.lastSpec()
	if got := spec.Env["CLAUDE_CODE_OAUTH_TOKEN"]; got != "sk-secret-value" {
		t.Errorf("spec.Env[CLAUDE_CODE_OAUTH_TOKEN] = %q, want the resolved credential", got)
	}
	for _, arg := range slices.Concat(spec.Command, spec.Args) {
		if strings.Contains(arg, "sk-secret-value") {
			t.Fatalf("THE CREDENTIAL IS IN ARGV (%q): it would be world-readable in /proc/<pid>/cmdline", arg)
		}
	}
	// And the agent gets a writable HOME, because the container runs as an
	// arbitrary uid as far as the image is concerned.
	if spec.Env["HOME"] != containerAgentHome {
		t.Errorf("spec.Env[HOME] = %q, want %q", spec.Env["HOME"], containerAgentHome)
	}
}

// TestSpawnContainerCLI_PromptRoundTripsOverTheContainerStdio proves the
// session is not merely created but USABLE: a prompt crosses the container
// boundary and its answer comes back, over the same stream-json protocol the
// host path speaks.
func TestSpawnContainerCLI_PromptRoundTripsOverTheContainerStdio(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "ctr-prompt"}
	backend := newContainerCLIBackend(t, rt)

	sess, err := backend.Spawn(t.Context(), requiredCLISpawn(t.TempDir()))
	if err != nil {
		t.Fatalf("required CLI spawn failed: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	resp, promptErr := backend.Prompt(ctx, &PromptRequest{SessionID: sess.ID, Prompt: "ping"})
	if promptErr != nil {
		t.Fatalf("Prompt over the container stdio failed: %v", promptErr)
	}
	if len(resp.Content) == 0 || !strings.Contains(resp.Content[0].Text, "ping") {
		t.Fatalf("prompt response did not come from the containerized agent: %+v", resp.Content)
	}

	status, statusErr := backend.Status(ctx, sess.ID)
	if statusErr != nil {
		t.Fatalf("Status: %v", statusErr)
	}
	if status.State != SessionStateActive {
		t.Errorf("Status.State = %q, want %q — a container-backed CLI session must read as live",
			status.State, SessionStateActive)
	}
}

// TestSpawnContainerCLI_CloseTearsDownTheContainer pins the lifecycle: Close
// must cancel the context `docker run` is bound to, or the container outlives
// the session it belonged to.
func TestSpawnContainerCLI_CloseTearsDownTheContainer(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "ctr-close"}
	backend := newContainerCLIBackend(t, rt)

	sess, err := backend.Spawn(t.Context(), requiredCLISpawn(t.TempDir()))
	if err != nil {
		t.Fatalf("required CLI spawn failed: %v", err)
	}

	rt.mu.Lock()
	containerCtx := rt.ctxs[0]
	rt.mu.Unlock()
	if containerCtx.Err() != nil {
		t.Fatalf("the container context was already cancelled at spawn: %v", containerCtx.Err())
	}

	if closeErr := backend.Close(t.Context(), sess.ID); closeErr != nil {
		t.Fatalf("Close: %v", closeErr)
	}
	if containerCtx.Err() == nil {
		t.Fatal("Close left the container context live: `docker run` keeps running and --rm never fires")
	}
}

// TestSpawnContainerCLI_OneShotRequestIsAlsoContainerized covers the
// non-persistent request. Sandboxing is a property of the POLICY, not of the
// caller's session-persistence preference: a required one-shot must not fall
// back to the host path that spawns a fresh process per prompt.
func TestSpawnContainerCLI_OneShotRequestIsAlsoContainerized(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "ctr-oneshot"}
	backend := newContainerCLIBackend(t, rt)

	req := requiredCLISpawn(t.TempDir())
	req.Persistent = false

	sess, err := backend.Spawn(t.Context(), req)
	if err != nil {
		t.Fatalf("required one-shot CLI spawn failed: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	if sess.ContainerID != rt.containerID {
		t.Errorf("Session.ContainerID = %q, want %q", sess.ContainerID, rt.containerID)
	}
	if rt.launches() != 1 {
		t.Errorf("RunInteractive called %d times, want 1", rt.launches())
	}
}

// TestSpawnContainerCLI_NoImage_Refused is the bad-input case: a required spawn
// with no image has nothing to launch, and must say so rather than degrade.
func TestSpawnContainerCLI_NoImage_Refused(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "ctr-none"}
	backend := newContainerCLIBackend(t, rt)

	req := requiredCLISpawn(t.TempDir())
	req.ContainerImage = ""

	_, err := backend.Spawn(t.Context(), req)
	if err == nil {
		t.Fatal("a required CLI spawn with no container image succeeded")
	}
	var invalid *InputValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("error %[1]T(%[1]v) is not *InputValidationError", err)
	}
	if invalid.Field != "container_image" {
		t.Errorf("InputValidationError.Field = %q, want %q", invalid.Field, "container_image")
	}
	if rt.launches() != 0 {
		t.Error("a spawn with no image still launched a container")
	}
}

// TestSpawnContainerCLI_RuntimeFailure_ReportsSpawnError covers the runtime
// refusing the launch (no daemon, image absent, pin mismatch): the spawn fails
// with the cause attached, and never falls back to a host process.
func TestSpawnContainerCLI_RuntimeFailure_ReportsSpawnError(t *testing.T) {
	runFailure := errors.New("cannot connect to the docker daemon")
	rt := &containerCLIRuntime{t: t, containerID: "ctr-fail", runErr: runFailure}
	backend := newContainerCLIBackend(t, rt)

	_, err := backend.Spawn(t.Context(), requiredCLISpawn(t.TempDir()))
	if err == nil {
		t.Fatal("a required CLI spawn succeeded despite the runtime refusing to launch")
	}
	var spawnErr *SpawnError
	if !errors.As(err, &spawnErr) {
		t.Fatalf("error %[1]T(%[1]v) is not *SpawnError", err)
	}
	if !errors.Is(err, runFailure) {
		t.Errorf("the runtime's own failure is not carried on the error: %v", err)
	}
}

// TestSpawnContainerCLI_SilentContainer_ReportsTheContainerNotAnExitCode
// covers the container that comes up and dies without speaking. There is no
// host process to read an exit code from, so the report names the container —
// a fabricated "exit code 0" would read as success.
func TestSpawnContainerCLI_SilentContainer_ReportsTheContainerNotAnExitCode(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "ctr-silent", silent: true}
	backend := newContainerCLIBackend(t, rt)

	_, err := backend.Spawn(t.Context(), requiredCLISpawn(t.TempDir()))
	if err == nil {
		t.Fatal("a spawn whose container never spoke returned a session")
	}
	var closed *ContainerStreamClosedError
	if !errors.As(err, &closed) {
		t.Fatalf("error %[1]T(%[1]v) is not *ContainerStreamClosedError", err)
	}
	if closed.ContainerID != rt.containerID {
		t.Errorf("ContainerStreamClosedError.ContainerID = %q, want %q", closed.ContainerID, rt.containerID)
	}
	if !errors.Is(err, ErrContainerStreamClosed) {
		t.Error("the error does not wrap ErrContainerStreamClosed")
	}
	var exited *StreamJSONProcessExitedError
	if errors.As(err, &exited) {
		t.Errorf("a containerized failure reported a host process exit code %d it cannot know", exited.ExitCode)
	}
}

// TestSpawnContainerCLI_DeadContainer_ReportsTheContainerNotABrokenPipe covers
// the other way a container can be absent: it is gone before the handshake is
// even written. "write |1: broken pipe" names nothing an operator can act on;
// the container and image do.
func TestSpawnContainerCLI_DeadContainer_ReportsTheContainerNotABrokenPipe(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "ctr-dead", deadOnArrival: true}
	backend := newContainerCLIBackend(t, rt)

	_, err := backend.Spawn(t.Context(), requiredCLISpawn(t.TempDir()))
	if err == nil {
		t.Fatal("a spawn into a dead container returned a session")
	}
	var closed *ContainerStreamClosedError
	if !errors.As(err, &closed) {
		t.Fatalf("error %[1]T(%[1]v) is not *ContainerStreamClosedError", err)
	}
	if closed.Image != "quicbaas-software-factory-worker:0.3.0-alpha" {
		t.Errorf("ContainerStreamClosedError.Image = %q, want the launched image", closed.Image)
	}
}

// TestResumeStarter_RequiredCLIWithRuntime_ReconstitutesInAContainer pins the
// resume half: a sandboxed CLI session comes back as a container, not as the
// host process the old ordering would have given it.
func TestResumeStarter_RequiredCLIWithRuntime_ReconstitutesInAContainer(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "ctr-resume"}
	backend := newContainerCLIBackend(t, rt)

	starter, err := backend.resumeStarter(t.Context(), &ResumeRequest{
		SessionID: "original-session-id",
		Spawn:     requiredCLISpawn(t.TempDir()),
	})
	if err != nil {
		t.Fatalf("resumeStarter refused a sandboxed CLI resume: %v", err)
	}

	sess, startErr := starter(spawnPlan{registerID: "original-session-id"})
	if startErr != nil {
		t.Fatalf("resume starter failed: %v", startErr)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	if sess.ContainerID != rt.containerID {
		t.Errorf("resumed Session.ContainerID = %q, want %q", sess.ContainerID, rt.containerID)
	}
	if sess.ID != "original-session-id" {
		t.Errorf("resumed session ID = %q, want the operator's original id", sess.ID)
	}
	if rt.launches() != 1 {
		t.Errorf("RunInteractive called %d times, want 1", rt.launches())
	}
}

// =============================================================================
// Container liveness (#774)
// =============================================================================
//
// A containerized session has no supervisor: `docker run -i --rm` belongs to
// the container runtime, so nothing Waits on it and the container's death
// arrives only as EOF on a stdio nobody reads between prompts. Status is the
// one place that can still find out, and until it asked, a worker container
// that died mid-execute went on reporting ACTIVE — holding its job's lease and
// its project's only slot indefinitely.

// TestSpawnContainerCLI_StatusReportsADeadContainerAsClosed is the regression
// for #774: the session record is intact and unclosed, and the only thing that
// changed is the container. Both ways a container can be gone are covered —
// stopped, and removed by --rm — because the runtime reports them differently.
func TestSpawnContainerCLI_StatusReportsADeadContainerAsClosed(t *testing.T) {
	for name, kill := range map[string]func(rt *containerCLIRuntime){
		"the container stopped": func(rt *containerCLIRuntime) { rt.notRunning = true },
		"the container was removed by --rm": func(rt *containerCLIRuntime) {
			rt.isRunningErr = &ContainerNotFoundError{ContainerID: rt.containerID}
		},
	} {
		t.Run(name, func(t *testing.T) {
			rt := &containerCLIRuntime{t: t, containerID: "quicbaas-acp-158dbc6f"}
			backend := newContainerCLIBackend(t, rt)

			sess, err := backend.Spawn(t.Context(), requiredCLISpawn(t.TempDir()))
			if err != nil {
				t.Fatalf("required CLI spawn failed: %v", err)
			}
			t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

			live, err := backend.Status(t.Context(), sess.ID)
			if err != nil {
				t.Fatalf("Status on a live container: %v", err)
			}
			if live.State != SessionStateActive {
				t.Fatalf("a running container is active, got %q", live.State)
			}

			kill(rt)

			dead, err := backend.Status(t.Context(), sess.ID)
			if err != nil {
				t.Fatalf("Status on a dead container: %v", err)
			}
			if dead.State != SessionStateClosed {
				t.Fatalf("a dead container must not report %q — that is the slot held forever", dead.State)
			}
		})
	}
}

// TestSpawnContainerCLI_StatusRefusesToGuessWhenTheRuntimeCannotAnswer is the
// bad-input half: a runtime that failed for any reason other than "no such
// container" has told us nothing, and Status must say so rather than report a
// state. The stall detector turns a state into a conclusion, and a guess in
// the not-live direction ends work that is still running.
func TestSpawnContainerCLI_StatusRefusesToGuessWhenTheRuntimeCannotAnswer(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "quicbaas-acp-158dbc6f"}
	backend := newContainerCLIBackend(t, rt)

	sess, err := backend.Spawn(t.Context(), requiredCLISpawn(t.TempDir()))
	if err != nil {
		t.Fatalf("required CLI spawn failed: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	rt.isRunningErr = &TransportError{
		Operation: "inspect",
		Cause:     &CommandError{Command: "docker", Op: "inspect", Output: "daemon socket unavailable"},
	}

	got, err := backend.Status(t.Context(), sess.ID)
	if got != nil {
		t.Fatalf("an unanswerable probe must yield no status, got %+v", got)
	}
	var transport *TransportError
	if !errors.As(err, &transport) || transport.Operation != "inspect" {
		t.Fatalf("Status must surface the runtime's own failure, got %v", err)
	}
}

// TestSpawnContainerCLI_StatusPrefersTheSessionsOwnConclusion proves the
// probe is subordinate: a session already closed reports closed without the
// runtime being asked at all, so a teardown that has already happened is never
// re-litigated against a container id that may have been reused.
func TestSpawnContainerCLI_StatusPrefersTheSessionsOwnConclusion(t *testing.T) {
	rt := &containerCLIRuntime{t: t, containerID: "quicbaas-acp-158dbc6f"}
	backend := newContainerCLIBackend(t, rt)

	sess, err := backend.Spawn(t.Context(), requiredCLISpawn(t.TempDir()))
	if err != nil {
		t.Fatalf("required CLI spawn failed: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	val, ok := backend.sessions.Load(sess.ID)
	if !ok {
		t.Fatal("the session record must be live after a spawn")
	}
	val.(*acpSession).closed.Store(true)

	// A runtime that would fail the probe proves it was never reached.
	rt.isRunningErr = errors.New("the runtime must not be asked about a concluded session")

	got, err := backend.Status(t.Context(), sess.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != SessionStateClosed {
		t.Fatalf("a concluded session is closed, got %q", got.State)
	}
}
