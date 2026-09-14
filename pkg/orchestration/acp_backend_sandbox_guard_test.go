package orchestration

import (
	"context"
	"errors"
	"os/exec"
	"sync/atomic"
	"testing"
)

// =============================================================================
// The fail-closed guard (#741 / #742)
// =============================================================================
//
// A CLI-mode backend under `--sandbox-policy required` used to run the agent
// as a plain host process, because dispatchSpawn asked about CLI mode before
// it asked about the sandbox. These tests pin the property that closes that
// hole and must keep holding however the container CLI path evolves:
//
//	A REQUIRED SPAWN THAT CANNOT BE CONTAINERIZED NEVER STARTS A HOST PROCESS.
//
// "Never starts a host process" is asserted at the only seam a host process
// can come through — ACPBackend.processFactory, which every host spawn path
// (spawnCLI, spawnOneShotCLI's per-prompt exec, spawnJSONRPC's transport) must
// pass to reach exec.Cmd.Start. A factory that is never invoked is a Start
// that was never reached.

// execTripwire is a ProcessFactory that fails the test the moment anything
// asks it for a host process, and records the attempt so a caller can also
// assert on it after the fact.
type execTripwire struct {
	t       *testing.T
	tripped atomic.Bool
}

func (w *execTripwire) factory() ProcessFactory {
	return func(_ context.Context, command string, args []string, _ string, _ []string) (*exec.Cmd, error) {
		w.tripped.Store(true)
		w.t.Errorf("SANDBOX BYPASS: a host process was requested under sandbox mode required: %s %v", command, args)
		return nil, errExecTripwireTripped
	}
}

// errExecTripwireTripped is what the tripwire returns so a spawn that ignored
// the guard fails instead of proceeding with a nil *exec.Cmd.
var errExecTripwireTripped = errors.New("orchestration: exec tripwire refused to start a host process")

// newGuardCLIBackend builds a claude-shaped CLI-mode backend whose only route
// to a host process is the tripwire. It is deliberately given NO container
// runtime: that is the configuration in which `required` has nowhere to go and
// must therefore refuse.
func newGuardCLIBackend(t *testing.T) (*ACPBackend, *execTripwire) {
	t.Helper()
	backend := NewACPBackend(ACPBackendConfig{
		Command:      "claude",
		Args:         []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"},
		DefaultModel: "claude-opus-5",
		CLIMode:      true,
	})
	tripwire := &execTripwire{t: t}
	backend.processFactory = tripwire.factory()
	return backend, tripwire
}

// assertSandboxRefusal checks that err is the typed refusal naming CLI mode as
// the reason, which is what distinguishes "the sandbox was enforced" from any
// other spawn failure that happens to be non-nil.
func assertSandboxRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("required spawn on a CLI-mode backend succeeded; it must fail closed")
	}
	var violation *SandboxModeViolationError
	if !errors.As(err, &violation) {
		t.Fatalf("error %[1]T(%[1]v) is not *SandboxModeViolationError", err)
	}
	if violation.Requested != SandboxModeRequired {
		t.Errorf("Requested = %v, want %v", violation.Requested, SandboxModeRequired)
	}
	if violation.Reason != sandboxReasonCLIMode && violation.Reason != sandboxReasonNoRuntime {
		t.Errorf("Reason = %q, want one of %q / %q", violation.Reason, sandboxReasonCLIMode, sandboxReasonNoRuntime)
	}
}

// TestDispatchSpawn_RequiredCLIPersistent_RefusesWithoutHostExec is the
// headline guard test: the exact shape the SDLC worker spawns (CLI mode,
// Persistent, SandboxModeRequired) on a daemon with no container runtime.
func TestDispatchSpawn_RequiredCLIPersistent_RefusesWithoutHostExec(t *testing.T) {
	backend, tripwire := newGuardCLIBackend(t)

	_, err := backend.Spawn(t.Context(), &SpawnRequest{
		ProjectID:   "p",
		WorkDir:     t.TempDir(),
		Persistent:  true,
		SandboxMode: SandboxModeRequired,
	})

	assertSandboxRefusal(t, err)
	if tripwire.tripped.Load() {
		t.Fatal("the spawn reached the host process factory: cmd.Start() was on the table")
	}
}

// TestDispatchSpawn_RequiredCLIOneShot_RefusesWithoutHostExec covers the other
// CLI branch. A one-shot session starts no process at spawn, so a guard that
// only watched the persistent path would let it register a session that execs
// unsandboxed on its FIRST PROMPT instead.
func TestDispatchSpawn_RequiredCLIOneShot_RefusesWithoutHostExec(t *testing.T) {
	backend, tripwire := newGuardCLIBackend(t)

	sess, err := backend.Spawn(t.Context(), &SpawnRequest{
		ProjectID:   "p",
		WorkDir:     t.TempDir(),
		Persistent:  false,
		SandboxMode: SandboxModeRequired,
	})

	assertSandboxRefusal(t, err)
	if sess != nil {
		t.Fatalf("a refused spawn returned session %q; it must return none", sess.ID)
	}
	if tripwire.tripped.Load() {
		t.Fatal("the spawn reached the host process factory: cmd.Start() was on the table")
	}
}

// TestDispatchSpawn_RequiredCLILegacySandboxedFlag_Refuses pins the legacy
// spelling. Callers predating SandboxMode ask for isolation with
// Sandboxed=true, which EffectiveSandboxMode maps to Required — a guard that
// only read SandboxMode would leave every one of them running on the host.
func TestDispatchSpawn_RequiredCLILegacySandboxedFlag_Refuses(t *testing.T) {
	backend, tripwire := newGuardCLIBackend(t)

	_, err := backend.Spawn(t.Context(), &SpawnRequest{
		ProjectID:  "p",
		WorkDir:    t.TempDir(),
		Persistent: true,
		Sandboxed:  true,
	})

	assertSandboxRefusal(t, err)
	if tripwire.tripped.Load() {
		t.Fatal("the spawn reached the host process factory: cmd.Start() was on the table")
	}
}

// TestDispatchSpawn_UnsandboxedCLI_StillRunsOnHost is the other half of the
// contract: the guard must not turn every CLI spawn into a refusal. A backend
// whose request asks for no sandbox still takes the host path it always did,
// which is what an operator running `--sandbox-policy opt-in` relies on.
func TestDispatchSpawn_UnsandboxedCLI_StillRunsOnHost(t *testing.T) {
	backend := newLifecycleCLIBackend(t, "TestStreamJSONStaysAliveHelper", "STREAM_JSON_STAYS_ALIVE_HELPER")

	sess, err := backend.Spawn(t.Context(), &SpawnRequest{
		ProjectID:   "p",
		WorkDir:     t.TempDir(),
		Persistent:  true,
		SandboxMode: SandboxModeNone,
	})
	if err != nil {
		t.Fatalf("unsandboxed CLI spawn failed: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	if sess.PID == 0 {
		t.Error("unsandboxed CLI session reports no PID; it should be a host process")
	}
	if sess.ContainerID != "" {
		t.Errorf("unsandboxed CLI session reports container %q; it should have none", sess.ContainerID)
	}
}

// TestResumeStarter_RequiredCLIWithoutRuntime_Refuses pins the same guard on
// the resume path, which selected its spawn path with the identical
// CLI-mode-before-sandbox ordering. A resume that silently reconstituted a
// sandboxed session as a host process would reopen the hole one restart later.
func TestResumeStarter_RequiredCLIWithoutRuntime_Refuses(t *testing.T) {
	backend, tripwire := newGuardCLIBackend(t)

	_, err := backend.resumeStarter(t.Context(), &ResumeRequest{
		SessionID: "s-1",
		Spawn: &SpawnRequest{
			ProjectID:   "p",
			WorkDir:     t.TempDir(),
			Persistent:  true,
			SandboxMode: SandboxModeRequired,
		},
	})
	if err == nil {
		t.Fatal("resumeStarter returned a starter for a sandboxed CLI session with no runtime")
	}
	var unsupported *SessionResumeUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error %[1]T(%[1]v) is not *SessionResumeUnsupportedError", err)
	}
	if tripwire.tripped.Load() {
		t.Fatal("resumeStarter reached the host process factory")
	}
}

// TestResumeStarter_UnsandboxedCLI_ReturnsHostStarter is the counterpart: an
// unsandboxed CLI session is still resumable exactly as before.
func TestResumeStarter_UnsandboxedCLI_ReturnsHostStarter(t *testing.T) {
	backend, _ := newGuardCLIBackend(t)

	starter, err := backend.resumeStarter(t.Context(), &ResumeRequest{
		SessionID: "s-1",
		Spawn: &SpawnRequest{
			ProjectID:   "p",
			WorkDir:     t.TempDir(),
			Persistent:  true,
			SandboxMode: SandboxModeNone,
		},
	})
	if err != nil {
		t.Fatalf("resumeStarter refused an unsandboxed CLI resume: %v", err)
	}
	if starter == nil {
		t.Fatal("resumeStarter returned no starter and no error")
	}
}
