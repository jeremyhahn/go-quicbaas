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
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// The subprocess has an owner
// =============================================================================
//
// Measured 2026-08-26, go-truststrap burn-in (#187): a persistent agent process
// was SIGKILLed and stayed <defunct> for twenty-one minutes, because nothing in
// the daemon was waiting on it — cmd.Wait ran only on a spawn failure, a
// mid-flight write failure, or Close, and the session was doing none of those.
// Status went on reporting the session ACTIVE the whole time, because it read
// cmd.ProcessState, which only Wait populates.
//
// The contract these tests hold the supervisor to:
//
//  1. the process is reaped, always, whoever ends it and whether or not
//     anything was waiting — no zombies;
//  2. the exit is REPORTED, so the layer that owns the session record can mark
//     it closed rather than discovering the death on the next prompt;
//  3. Status tells a dead process from a live one;
//  4. Close still works with the supervisor already parked in that single Wait,
//     which is the concurrent cmd.Wait os/exec forbids.

// spawnSupervisedSession starts a persistent CLI session over the stays-alive
// helper and returns it with the exits channel its observer feeds.
func spawnSupervisedSession(t *testing.T) (*ACPBackend, *Session, chan ProcessExit) {
	t.Helper()
	backend := newLifecycleCLIBackend(t, "TestStreamJSONStaysAliveHelper", "STREAM_JSON_STAYS_ALIVE_HELPER")
	exits := make(chan ProcessExit, 1)
	backend.config.ProcessExitObserver = func(exit ProcessExit) { exits <- exit }

	sess, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: t.TempDir(), Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	if sess.PID <= 0 {
		t.Fatalf("Spawn() returned pid %d; the test cannot kill a process it cannot name", sess.PID)
	}
	return backend, sess, exits
}

func TestSupervisedProcess_AKilledAgentIsReapedAndReported(t *testing.T) {
	backend, sess, exits := spawnSupervisedSession(t)
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	// A live session reports live. This is the control: without it, a Status
	// that answered "closed" for everything would pass the assertion below.
	status, err := backend.Status(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.State != SessionStateActive {
		t.Fatalf("Status() before the kill = %q, want active", status.State)
	}

	proc, err := os.FindProcess(sess.PID)
	if err != nil {
		t.Fatalf("FindProcess(%d): %v", sess.PID, err)
	}
	// SIGKILL, not the process context: this is the burn-in's own kill test —
	// an agent ended by something outside the daemon entirely, with nobody in
	// the daemon waiting for it.
	if err := proc.Kill(); err != nil {
		t.Fatalf("Kill(%d): %v", sess.PID, err)
	}

	var exit ProcessExit
	select {
	case exit = <-exits:
	case <-time.After(15 * time.Second):
		t.Fatal("no exit was reported; nothing in the daemon observed the agent die")
	}

	if exit.SessionID != sess.ID {
		t.Errorf("exit session = %q, want %q", exit.SessionID, sess.ID)
	}
	if exit.PID != sess.PID {
		t.Errorf("exit pid = %d, want %d", exit.PID, sess.PID)
	}
	if exit.At.IsZero() {
		t.Error("exit carries no timestamp; when a worker died is half of whether it mattered")
	}

	// NO ZOMBIE. ProcessState is populated by exactly one thing — a completed
	// Wait — so a non-nil ProcessState IS the proof the process was reaped.
	val, ok := backend.sessions.Load(sess.ID)
	if !ok {
		t.Fatal("session vanished from the backend registry")
	}
	if state := val.(*acpSession).cmd.ProcessState; state == nil {
		t.Fatal("cmd.ProcessState is nil: nothing ever waited on the process, so it is a zombie")
	}

	// And on Linux the kernel's own view agrees: a reaped pid has no /proc
	// entry, while an unreaped one sits there in state Z.
	if runtime.GOOS == "linux" {
		if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(sess.PID))); err == nil {
			t.Errorf("/proc/%d still exists; the process was never reaped", sess.PID)
		}
	}

	// The session must now read as closed. Before the supervisor it read ACTIVE
	// for as long as the zombie survived, which is how a dead worker went on
	// looking like a live one.
	status, err = backend.Status(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("Status() after the kill error = %v", err)
	}
	if status.State != SessionStateClosed {
		t.Errorf("Status() after the kill = %q, want closed", status.State)
	}

	// A prompt to a dead session is refused rather than written to a dead pipe.
	if _, err := backend.Prompt(context.Background(), &PromptRequest{
		SessionID: sess.ID, Prompt: "are you there",
	}); err == nil {
		t.Error("Prompt() to a session whose process is gone = nil error")
	}
}

func TestSupervisedProcess_CloseReapsThroughTheSameWait(t *testing.T) {
	// Close used to call cmd.Wait directly. With a supervisor already parked in
	// that Wait, a second one is the concurrent call os/exec forbids — so Close
	// goes through the shared reaper, blocks until that single Wait returns, and
	// reports success exactly once.
	backend, sess, exits := spawnSupervisedSession(t)

	done := make(chan error, 1)
	go func() { done <- backend.Close(context.Background(), sess.ID) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Close() did not return; the reap deadlocked against the supervisor")
	}

	select {
	case exit := <-exits:
		if exit.SessionID != sess.ID {
			t.Errorf("exit session = %q, want %q", exit.SessionID, sess.ID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close() returned without the exit ever being observed")
	}

	// Closing twice must not fail differently: the session is gone from the
	// registry, which is a not-found, not a second teardown.
	if err := backend.Close(context.Background(), sess.ID); err == nil {
		t.Error("second Close() = nil error; the session was already removed")
	}
}

func TestProcessExit_CarriesMetadataAndRetainsNoStderrAnywhere(t *testing.T) {
	// This string is logged on EVERY supervised exit, ordinary shutdowns
	// included, so it must carry metadata and nothing an agent printed.
	exit := ProcessExit{SessionID: "ses-9", PID: 4242, ExitCode: 7}
	got := exit.String()
	for _, want := range []string{"ses-9", "4242", "7"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, missing %q", got, want)
		}
	}

	// AND THE RECORD ITSELF HOLDS NO TAIL TO RENDER. Not logging a secret was
	// never the same as not RETAINING one: this record is stored for the life
	// of the session and handed to an observer, so a stderr field on it would
	// be plaintext credential material sitting in ordinary memory for that
	// whole span. Nothing consumed it, so it bought exposure and no capability.
	//
	// Asserted structurally rather than by reading a field, because the point
	// is that the field does not exist — a future "just for diagnostics" tail
	// put back on this struct should fail here.
	for _, f := range reflect.VisibleFields(reflect.TypeOf(exit)) {
		if strings.Contains(strings.ToLower(f.Name), "stderr") {
			t.Errorf("ProcessExit has field %q: agent stderr must not be retained on the exit record; "+
				"the tail belongs in the typed exit errors, or behind the protected-secret abstraction", f.Name)
		}
	}
}
