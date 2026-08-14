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
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Resume tests for the persistent stream-json CLI backend.
//
// These exercise the real subprocess path: the helper below stands in for the
// claude CLI, records the command line it was launched with and the first
// message written to its stdin, and then behaves like a persistent agent. That
// makes both halves of a resume observable — the flags that select the
// mechanism, and the conversation actually handed to the replacement process.
// =============================================================================

// resumeHelperCaptureEnv names the file the helper writes its capture to, and
// resumeHelperArgsEnv carries the agent command line the backend built. The
// command line travels through the environment rather than argv because the
// helper is the test binary itself, whose own flag parser would reject the
// agent's flags.
const (
	resumeHelperCaptureEnv = "STREAM_JSON_RESUME_CAPTURE"
	resumeHelperArgsEnv    = "STREAM_JSON_RESUME_ARGS"
)

// resumeCapture is what the fake agent observed about its own launch.
type resumeCapture struct {
	Args      []string `json:"args"`
	FirstLine string   `json:"first_line"`
}

// TestStreamJSONResumeCaptureHelper is a fake claude that records its argv and
// its first stdin message, then serves prompts like the real persistent CLI.
func TestStreamJSONResumeCaptureHelper(t *testing.T) {
	if os.Getenv("STREAM_JSON_RESUME_CAPTURE_HELPER") != "1" {
		return
	}
	var capture resumeCapture
	if raw := os.Getenv(resumeHelperArgsEnv); raw != "" {
		_ = json.Unmarshal([]byte(raw), &capture.Args)
	}

	writeMsg := func(m streamJSONMessage) {
		b, _ := json.Marshal(m)
		os.Stdout.Write(append(b, '\n'))
	}
	writeMsg(streamJSONMessage{
		Type:      "system",
		Subtype:   "init",
		SessionID: os.Getenv("STREAM_JSON_SESSION_ID"),
		Model:     os.Getenv("STREAM_JSON_MODEL"),
	})

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	first := true
	for scanner.Scan() {
		var in streamJSONUserMessage
		if json.Unmarshal(scanner.Bytes(), &in) != nil {
			continue
		}
		if first {
			capture.FirstLine = in.Message.Content
			if path := os.Getenv(resumeHelperCaptureEnv); path != "" {
				raw, _ := json.Marshal(capture)
				_ = os.WriteFile(path, raw, 0o600)
			}
			first = false
		}
		writeMsg(streamJSONMessage{
			Type: "assistant",
			Message: &streamJSONAssistant{
				Role:    "assistant",
				Content: []streamJSONContent{{Type: "text", Text: "echo:" + in.Message.Content}},
			},
		})
		writeMsg(streamJSONMessage{Type: "result", Subtype: "success", Result: "echo:" + in.Message.Content})
	}
	os.Exit(0)
}

// newResumeCaptureBackend builds a persistent CLI backend whose subprocess is
// the capture helper, and returns the path the capture lands in.
func newResumeCaptureBackend(t *testing.T, nativeResume bool) (*ACPBackend, string) {
	t.Helper()

	capturePath := filepath.Join(t.TempDir(), "capture.json")
	backend := NewACPBackend(ACPBackendConfig{
		Command: "claude",
		// The persistence flag is intentionally absent: the backend applies it
		// from NativeSessionResume, which is the behaviour under test.
		Args:                []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"},
		DefaultModel:        "claude-opus-4-5",
		CLIMode:             true,
		NativeSessionResume: nativeResume,
	})
	backend.processFactory = func(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error) {
		encodedArgs, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStreamJSONResumeCaptureHelper$")
		cmd.Env = append(os.Environ(),
			"STREAM_JSON_RESUME_CAPTURE_HELPER=1",
			"STREAM_JSON_SESSION_ID=agent-assigned-id",
			"STREAM_JSON_MODEL=claude-opus-4-5",
			resumeHelperCaptureEnv+"="+capturePath,
			resumeHelperArgsEnv+"="+string(encodedArgs),
		)
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
		cmd.WaitDelay = 5 * time.Second
		if workDir != "" {
			cmd.Dir = workDir
		}
		return cmd, nil
	}
	return backend, capturePath
}

// readResumeCapture reads what the fake agent recorded about its launch.
func readResumeCapture(t *testing.T, path string) resumeCapture {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	var capture resumeCapture
	if err := json.Unmarshal(raw, &capture); err != nil {
		t.Fatalf("decode capture: %v", err)
	}
	return capture
}

// =============================================================================
// Replay resume
// =============================================================================

// TestACPBackendResume_ReplayPrimesAgentWithPriorTurns asserts the replacement
// process is actually handed the prior conversation, and that the session keeps
// the identifier the operator already holds rather than the one the agent
// reports.
func TestACPBackendResume_ReplayPrimesAgentWithPriorTurns(t *testing.T) {
	backend, capturePath := newResumeCaptureBackend(t, false)

	result, err := backend.Resume(context.Background(), &ResumeRequest{
		SessionID: "operator-held-id",
		Spawn:     &SpawnRequest{WorkDir: t.TempDir(), Persistent: true},
		Transcript: &ReplayTranscript{
			SessionID:    "operator-held-id",
			SystemPrompt: "you are a careful engineer",
			Turns: []ReplayTurn{
				{Role: ReplayRoleUser, Text: "what is the capital of France?"},
				{Role: ReplayRoleAssistant, Text: "Paris."},
			},
			Omitted: 3,
		},
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), "operator-held-id") })

	if result.Mode != ResumeModeReplay {
		t.Errorf("mode = %q, want replay", result.Mode)
	}
	if result.ReplayedTurns != 2 || result.OmittedTurns != 3 {
		t.Errorf("replayed=%d omitted=%d, want 2/3", result.ReplayedTurns, result.OmittedTurns)
	}
	if result.Session.ID != "operator-held-id" {
		t.Errorf("session ID = %q, want the original operator-held ID", result.Session.ID)
	}

	capture := readResumeCapture(t, capturePath)
	for _, want := range []string{"what is the capital of France?", "Paris.", "you are a careful engineer", "3 earlier turn(s) omitted"} {
		if !strings.Contains(capture.FirstLine, want) {
			t.Errorf("agent's first message missing %q:\n%s", want, capture.FirstLine)
		}
	}

	// Replay mode must keep the CLI's own session store switched off, and must
	// not ask it to resume a session it never wrote.
	if !containsArg(capture.Args, sessionPersistenceFlag) {
		t.Errorf("args = %v, want %s in replay mode", capture.Args, sessionPersistenceFlag)
	}
	if containsArg(capture.Args, "--resume") {
		t.Errorf("args = %v, want no --resume in replay mode", capture.Args)
	}

	// The resumed session answers under the original ID.
	resp, err := backend.Prompt(context.Background(), &PromptRequest{
		SessionID: "operator-held-id",
		Prompt:    "and its population?",
	})
	if err != nil {
		t.Fatalf("Prompt after resume: %v", err)
	}
	if len(resp.Content) == 0 || !strings.Contains(resp.Content[0].Text, "and its population?") {
		t.Fatalf("resumed session response = %+v", resp.Content)
	}
}

// TestACPBackendResume_EmptyTranscriptStartsCleanly asserts a session that was
// spawned but never prompted resumes without a priming message describing a
// conversation that never happened.
func TestACPBackendResume_EmptyTranscriptStartsCleanly(t *testing.T) {
	backend, capturePath := newResumeCaptureBackend(t, false)

	result, err := backend.Resume(context.Background(), &ResumeRequest{
		SessionID:  "never-prompted",
		Spawn:      &SpawnRequest{WorkDir: t.TempDir(), Persistent: true},
		Transcript: &ReplayTranscript{SessionID: "never-prompted"},
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), "never-prompted") })

	if result.ReplayedTurns != 0 {
		t.Errorf("replayed turns = %d, want 0", result.ReplayedTurns)
	}
	capture := readResumeCapture(t, capturePath)
	if strings.Contains(capture.FirstLine, "transcript of session") {
		t.Errorf("agent primed with an empty transcript:\n%s", capture.FirstLine)
	}
}

// =============================================================================
// Native resume
// =============================================================================

// TestACPBackendResume_NativeUsesAgentSessionStore asserts native mode hands the
// conversation back to the CLI's own store: it withholds the
// no-session-persistence flag, passes --resume, and re-sends nothing.
func TestACPBackendResume_NativeUsesAgentSessionStore(t *testing.T) {
	backend, capturePath := newResumeCaptureBackend(t, true)

	result, err := backend.Resume(context.Background(), &ResumeRequest{
		SessionID: "native-id",
		Spawn:     &SpawnRequest{WorkDir: t.TempDir(), Persistent: true},
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), "native-id") })

	if result.Mode != ResumeModeNative {
		t.Fatalf("mode = %q, want native", result.Mode)
	}
	if result.ReplayedTurns != 0 {
		t.Errorf("replayed turns = %d, want 0 — native resume re-sends nothing", result.ReplayedTurns)
	}

	capture := readResumeCapture(t, capturePath)
	if containsArg(capture.Args, sessionPersistenceFlag) {
		t.Errorf("args = %v, want %s withheld in native mode", capture.Args, sessionPersistenceFlag)
	}
	if !containsArgPair(capture.Args, "--resume", "native-id") {
		t.Errorf("args = %v, want --resume native-id", capture.Args)
	}
	if strings.Contains(capture.FirstLine, "transcript of session") {
		t.Errorf("native resume primed the agent with a transcript:\n%s", capture.FirstLine)
	}
}

// TestACPBackendSpawn_AppliesSessionPersistencePolicy asserts the backend owns
// the persistence flag: a caller-supplied copy is normalised away and the flag
// is applied from configuration, so the two never disagree.
func TestACPBackendSpawn_AppliesSessionPersistencePolicy(t *testing.T) {
	t.Parallel()

	withFlag := applySessionPersistenceFlag([]string{"--verbose", sessionPersistenceFlag}, false)
	if countArg(withFlag, sessionPersistenceFlag) != 1 {
		t.Fatalf("args = %v, want exactly one %s", withFlag, sessionPersistenceFlag)
	}

	native := applySessionPersistenceFlag([]string{"--verbose", sessionPersistenceFlag}, true)
	if containsArg(native, sessionPersistenceFlag) {
		t.Fatalf("args = %v, want the flag stripped for native resume", native)
	}
	if len(native) != 1 || native[0] != "--verbose" {
		t.Fatalf("args = %v, want the caller's other flags preserved", native)
	}
}

// =============================================================================
// Refusals
// =============================================================================

// TestACPBackendResume_RejectsInvalidRequests asserts the backend refuses a
// resume it cannot honour instead of starting an agent with the wrong identity
// or no context.
func TestACPBackendResume_RejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	backend, _ := newResumeCaptureBackend(t, false)
	ctx := context.Background()

	if _, err := backend.Resume(ctx, nil); err == nil {
		t.Error("Resume(nil) succeeded, want an error")
	}
	if _, err := backend.Resume(ctx, &ResumeRequest{Spawn: &SpawnRequest{WorkDir: "/tmp", Persistent: true}}); err == nil {
		t.Error("Resume without a session ID succeeded, want an error")
	}
	if _, err := backend.Resume(ctx, &ResumeRequest{SessionID: "s"}); err == nil {
		t.Error("Resume without a spawn config succeeded, want an error")
	}
	if _, err := backend.Resume(ctx, &ResumeRequest{SessionID: "s", Spawn: &SpawnRequest{Persistent: true}}); err == nil {
		t.Error("Resume without a work dir succeeded, want an error")
	}

	// A non-persistent session has no long-running process and no context.
	_, err := backend.Resume(ctx, &ResumeRequest{
		SessionID: "s",
		Spawn:     &SpawnRequest{WorkDir: "/tmp"},
	})
	if !errors.Is(err, ErrSessionResumeUnsupported) {
		t.Errorf("non-persistent resume error = %v, want ErrSessionResumeUnsupported", err)
	}

	// Replay mode with no transcript must not silently start a fresh agent.
	_, err = backend.Resume(ctx, &ResumeRequest{
		SessionID: "s",
		Spawn:     &SpawnRequest{WorkDir: "/tmp", Persistent: true},
	})
	if !errors.Is(err, ErrSessionTranscriptUnavailable) {
		t.Errorf("replay resume without a transcript error = %v, want ErrSessionTranscriptUnavailable", err)
	}
}

// TestACPBackendResume_RefusesLiveSession asserts a session whose process is
// still running is not replaced — doing so would discard the context it holds.
func TestACPBackendResume_RefusesLiveSession(t *testing.T) {
	backend, _ := newResumeCaptureBackend(t, false)
	ctx := context.Background()

	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: t.TempDir(), Persistent: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(ctx, sess.ID) })

	_, err = backend.Resume(ctx, &ResumeRequest{
		SessionID:  sess.ID,
		Spawn:      &SpawnRequest{WorkDir: t.TempDir(), Persistent: true},
		Transcript: &ReplayTranscript{SessionID: sess.ID},
	})
	if !errors.Is(err, ErrSessionResumeFailed) {
		t.Fatalf("resume of a live session error = %v, want ErrSessionResumeFailed", err)
	}
	var live *SessionAlreadyLiveError
	if !errors.As(err, &live) {
		t.Fatalf("error = %v, want it to identify the session as already live", err)
	}
}

// TestACPBackendResume_UnsupportedOnNonCLIBackend asserts a JSON-RPC agent,
// which exposes no way to restore a prior conversation, refuses rather than
// pretending.
func TestACPBackendResume_UnsupportedOnNonCLIBackend(t *testing.T) {
	t.Parallel()

	backend := NewACPBackend(ACPBackendConfig{Command: "opencode", Args: []string{"acp"}})
	_, err := backend.Resume(context.Background(), &ResumeRequest{
		SessionID: "s",
		Spawn:     &SpawnRequest{WorkDir: "/tmp", Persistent: true},
	})
	if !errors.Is(err, ErrSessionResumeUnsupported) {
		t.Fatalf("error = %v, want ErrSessionResumeUnsupported", err)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func containsArg(args []string, want string) bool {
	return countArg(args, want) > 0
}

func countArg(args []string, want string) int {
	n := 0
	for _, arg := range args {
		if arg == want {
			n++
		}
	}
	return n
}

func containsArgPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
