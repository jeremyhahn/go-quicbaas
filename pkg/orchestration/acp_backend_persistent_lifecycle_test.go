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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

// =============================================================================
// Persistent CLI subprocess lifecycle tests.
//
// These tests pin the root-cause fix for the production failure
// "acp: transport failed: write: broken pipe" on a persistent claude ACP
// session: the persistent subprocess must outlive the Spawn request context
// (which gRPC cancels the moment the Spawn RPC returns) and must surface an
// actionable typed error — carrying the exit code and stderr tail — when it
// exits unexpectedly, instead of a bare broken-pipe.
// =============================================================================

// TestStreamJSONStaysAliveHelper is a fake claude that, like the real CLI in
// --print --input-format stream-json mode, stays alive across multiple turns:
// it emits an init message and then echoes a result for every user message read
// from stdin, exiting only when stdin is closed.
func TestStreamJSONStaysAliveHelper(t *testing.T) {
	if os.Getenv("STREAM_JSON_STAYS_ALIVE_HELPER") != "1" {
		return
	}
	sessionID := os.Getenv("STREAM_JSON_SESSION_ID")
	model := os.Getenv("STREAM_JSON_MODEL")

	writeMsg := func(m streamJSONMessage) {
		b, _ := json.Marshal(m)
		os.Stdout.Write(append(b, '\n'))
	}

	writeMsg(streamJSONMessage{Type: "system", Subtype: "init", SessionID: sessionID, Model: model})

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var in streamJSONUserMessage
		if json.Unmarshal(scanner.Bytes(), &in) != nil {
			continue
		}
		writeMsg(streamJSONMessage{
			Type: "assistant",
			Message: &streamJSONAssistant{
				Role:       "assistant",
				Content:    []streamJSONContent{{Type: "text", Text: "echo:" + in.Message.Content}},
				StopReason: "end_turn",
				Model:      model,
			},
		})
		writeMsg(streamJSONMessage{Type: "result", Subtype: "success", SessionID: sessionID, Result: "echo:" + in.Message.Content})
	}
	os.Exit(0)
}

// TestStreamJSONHandshakeThenDieHelper emulates a claude that completes the
// spawn handshake (init + one result) but then exits with a non-zero code and a
// stderr message before any real prompt arrives — the exact production scenario
// where the follow-up prompt would hit a broken pipe.
func TestStreamJSONHandshakeThenDieHelper(t *testing.T) {
	if os.Getenv("STREAM_JSON_HANDSHAKE_THEN_DIE_HELPER") != "1" {
		return
	}
	sessionID := os.Getenv("STREAM_JSON_SESSION_ID")
	model := os.Getenv("STREAM_JSON_MODEL")

	writeMsg := func(m streamJSONMessage) {
		b, _ := json.Marshal(m)
		os.Stdout.Write(append(b, '\n'))
	}

	writeMsg(streamJSONMessage{Type: "system", Subtype: "init", SessionID: sessionID, Model: model})

	// Consume the spawn handshake message and answer with a result so Spawn
	// completes successfully.
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	if scanner.Scan() {
		writeMsg(streamJSONMessage{Type: "result", Subtype: "success", SessionID: sessionID, Result: "OK"})
	}

	// Now die with a diagnostic on stderr and a non-zero exit code, simulating
	// e.g. an auth/credit failure that kills the agent after startup.
	os.Stderr.WriteString("fatal: credit balance too low\n")
	os.Exit(7)
}

// newLifecycleCLIBackend builds an ACPBackend in CLI stream-json mode whose
// processFactory re-execs the test binary as the named helper. Crucially it
// honours the (decoupled) context the backend passes in — proving that after
// the fix the backend no longer hands the spawn request ctx to the subprocess.
func newLifecycleCLIBackend(t *testing.T, helperTest, helperEnvVar string) *ACPBackend {
	t.Helper()
	backend := NewACPBackend(ACPBackendConfig{
		Command:      "claude",
		Args:         []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"},
		DefaultModel: "claude-opus-4-5",
		CLIMode:      true,
	})
	backend.processFactory = func(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+helperTest+"$")
		cmd.Env = append(os.Environ(),
			helperEnvVar+"=1",
			"STREAM_JSON_SESSION_ID=lifecycle-session",
			"STREAM_JSON_MODEL=claude-opus-4-5",
		)
		// Mirror defaultProcessFactory: a Cancel hook so cancelling ctx
		// terminates the process, plus a bounded reap delay.
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
		cmd.WaitDelay = 5 * time.Second
		if workDir != "" {
			cmd.Dir = workDir
		}
		return cmd, nil
	}
	return backend
}

// TestSpawnCLI_SubprocessOutlivesSpawnRequestContext is the headline regression
// test. It reproduces the production setup where the Spawn RPC context is
// cancelled the instant Spawn returns (as gRPC does for a unary handler) and
// asserts the persistent subprocess survives so a subsequent Prompt succeeds.
//
// Before the fix, spawnCLI passed the request ctx to exec.CommandContext, so
// cancelling it after Spawn killed the subprocess and the Prompt wrote to a
// dead stdin -> "acp: transport failed: write: broken pipe".
func TestSpawnCLI_SubprocessOutlivesSpawnRequestContext(t *testing.T) {
	backend := newLifecycleCLIBackend(t, "TestStreamJSONStaysAliveHelper", "STREAM_JSON_STAYS_ALIVE_HELPER")

	spawnCtx, cancelSpawn := context.WithCancel(context.Background())
	sess, err := backend.Spawn(spawnCtx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	// Cancel the spawn request context, exactly as gRPC does when the unary
	// Spawn handler returns. The subprocess must NOT die with it.
	cancelSpawn()
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	resp, err := backend.Prompt(context.Background(), &PromptRequest{
		SessionID: sess.ID,
		Prompt:    "hello world",
	})
	if err != nil {
		t.Fatalf("Prompt() after spawn-ctx cancel error = %v (subprocess died with the request context)", err)
	}
	if len(resp.Content) == 0 || resp.Content[0].Text != "echo:hello world" {
		t.Fatalf("Prompt() content = %+v, want echo of the prompt", resp.Content)
	}

	// A second prompt must also succeed: the process is genuinely persistent.
	resp2, err := backend.Prompt(context.Background(), &PromptRequest{SessionID: sess.ID, Prompt: "again"})
	if err != nil {
		t.Fatalf("second Prompt() error = %v", err)
	}
	if resp2.Content[0].Text != "echo:again" {
		t.Fatalf("second Prompt() content = %+v", resp2.Content)
	}
}

// TestPromptStreamJSON_SubprocessExitSurfacesTypedError asserts that when the
// persistent subprocess has exited (non-zero code with a stderr message), a
// Prompt returns an actionable StreamJSONProcessExitedError carrying the exit
// code and stderr tail — not a bare broken pipe — and marks the session closed
// so it is not left dangling.
func TestPromptStreamJSON_SubprocessExitSurfacesTypedError(t *testing.T) {
	backend := newLifecycleCLIBackend(t, "TestStreamJSONHandshakeThenDieHelper", "STREAM_JSON_HANDSHAKE_THEN_DIE_HELPER")

	sess, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	_, err = backend.Prompt(context.Background(), &PromptRequest{SessionID: sess.ID, Prompt: "do work"})
	if err == nil {
		t.Fatal("Prompt() against an exited subprocess should error")
	}

	// The error must be the actionable typed exit error, not a raw pipe error.
	if !errors.Is(err, ErrStreamJSONProcessExited) {
		t.Fatalf("error %v should wrap ErrStreamJSONProcessExited", err)
	}
	var exited *StreamJSONProcessExitedError
	if !errors.As(err, &exited) {
		t.Fatalf("error %v should be a *StreamJSONProcessExitedError", err)
	}
	if exited.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", exited.ExitCode)
	}
	if exited.Stderr != "fatal: credit balance too low" {
		t.Errorf("Stderr = %q, want the captured stderr tail", exited.Stderr)
	}

	// The session must be marked closed (not left dangling): a status lookup
	// reports closed and a follow-up prompt fails fast as closed.
	status, statusErr := backend.Status(context.Background(), sess.ID)
	if statusErr != nil {
		t.Fatalf("Status() error = %v", statusErr)
	}
	if status.State != SessionStateClosed {
		t.Errorf("Status().State = %q, want %q after subprocess exit", status.State, SessionStateClosed)
	}

	_, againErr := backend.Prompt(context.Background(), &PromptRequest{SessionID: sess.ID, Prompt: "retry"})
	if !errors.Is(againErr, ErrSessionClosed) {
		t.Errorf("follow-up Prompt error = %v, want ErrSessionClosed", againErr)
	}
}

// TestStderrTail_RetainsBoundedTail verifies the stderr capture keeps only the
// last stderrTailLimit bytes and trims surrounding whitespace, so an exited
// subprocess yields a bounded, clean diagnostic.
func TestStderrTail_RetainsBoundedTail(t *testing.T) {
	tail := &stderrTail{}

	// Write more than the limit; only the trailing window must survive.
	big := make([]byte, stderrTailLimit+128)
	for i := range big {
		big[i] = 'a'
	}
	if _, err := tail.Write(big); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := len(tail.buf); got != stderrTailLimit {
		t.Fatalf("retained %d bytes, want bounded to %d", got, stderrTailLimit)
	}

	// A subsequent short write with whitespace must appear at the tail, trimmed.
	if _, err := tail.Write([]byte("\n  last line  \n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := tail.String(); got[len(got)-len("last line"):] != "last line" {
		t.Fatalf("String() = %q, want it to end with the latest trimmed line", got)
	}
}

// TestStderrTail_NilReceiverString ensures String tolerates a nil tail, which
// can occur if a session predates stderr capture (defensive path).
func TestStderrTail_NilReceiverString(t *testing.T) {
	var tail *stderrTail
	if got := tail.String(); got != "" {
		t.Fatalf("nil stderrTail String() = %q, want empty", got)
	}
}

// TestSpawnCLI_SubprocessStillRunningAfterSpawnCtxCancel verifies the process
// state directly: after the spawn ctx is cancelled the subprocess is still
// alive (ProcessState nil / not exited), and Close cleanly terminates it.
func TestSpawnCLI_SubprocessStillRunningAfterSpawnCtxCancel(t *testing.T) {
	backend := newLifecycleCLIBackend(t, "TestStreamJSONStaysAliveHelper", "STREAM_JSON_STAYS_ALIVE_HELPER")

	spawnCtx, cancelSpawn := context.WithCancel(context.Background())
	sess, err := backend.Spawn(spawnCtx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	cancelSpawn()

	val, ok := backend.sessions.Load(sess.ID)
	if !ok {
		t.Fatal("session not stored")
	}
	acp := val.(*acpSession)
	if acp.cmd.ProcessState != nil && acp.cmd.ProcessState.Exited() {
		t.Fatal("subprocess exited after spawn-ctx cancel; it must outlive the request context")
	}

	if err := backend.Close(context.Background(), sess.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}
