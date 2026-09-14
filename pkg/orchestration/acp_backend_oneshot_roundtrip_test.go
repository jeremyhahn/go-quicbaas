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
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// captureLogger records Debugf/Warnf lines so diagnostic-log assertions can
// verify the create-key vs lookup-key evidence the daemon emits.
type captureLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *captureLogger) record(format string, args ...interface{}) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *captureLogger) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func (l *captureLogger) Debug(string, ...any)                                {}
func (l *captureLogger) Debugf(f string, a ...interface{})                   { l.record(f, a...) }
func (l *captureLogger) Info(string, ...any)                                 {}
func (l *captureLogger) Infof(string, ...interface{})                        {}
func (l *captureLogger) Warn(string, ...any)                                 {}
func (l *captureLogger) Warnf(f string, a ...interface{})                    { l.record(f, a...) }
func (l *captureLogger) Error(string, ...any)                                {}
func (l *captureLogger) Errorf(string, ...interface{})                       {}
func (l *captureLogger) Fatal(string)                                        {}
func (l *captureLogger) Fatalf(string, ...interface{})                       {}
func (l *captureLogger) WithField(string, interface{}) interfaces.Logger     { return l }
func (l *captureLogger) WithFields(map[string]interface{}) interfaces.Logger { return l }
func (l *captureLogger) WithError(error) interfaces.Logger                   { return l }

// TestOneShotJSONHelper is a helper subprocess that mimics
// "claude --print --output-format json" for the one-shot CLI prompt path. It
// reads the prompt from stdin and emits a single JSON result. Invoked by the
// test binary itself via ONESHOT_JSON_HELPER.
func TestOneShotJSONHelper(t *testing.T) {
	if os.Getenv("ONESHOT_JSON_HELPER") != "1" {
		return
	}
	in, _ := os.ReadFile("/dev/stdin")
	resp := `{"type":"result","result":"oneshot:` + strings.TrimSpace(string(in)) + `","is_error":false}`
	os.Stdout.Write([]byte(resp))
	os.Exit(0)
}

// newOneShotBackend builds an ACPBackend configured exactly like the embedded
// "claude" agent (CLIMode=true) whose prompt exec is redirected to the
// TestOneShotJSONHelper subprocess so no real claude binary is required.
func newOneShotBackend(t *testing.T) *ACPBackend {
	t.Helper()
	b := NewACPBackend(ACPBackendConfig{
		Command: "claude",
		Args:    []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--no-session-persistence"},
		CLIMode: true,
	})
	b.processFactory = func(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestOneShotJSONHelper")
		cmd.Env = append(os.Environ(), "ONESHOT_JSON_HELPER=1")
		if workDir != "" {
			cmd.Dir = workDir
		}
		return cmd, nil
	}
	return b
}

// TestACPBackend_OneShot_SpawnThenPromptFindsSession is the authoritative
// regression for the embedded-daemon failure "session created at spawn is not
// found by the immediate prompt" for the live claude config path.
//
// The embedded claude backend is CLIMode=true, and the v1 wire (proto
// SpawnSessionRequest) carries no "persistent" flag, so the daemon always
// spawns claude in ONE-SHOT CLI mode (req.Persistent=false ->
// spawnOneShotCLI). This test exercises that exact path: Spawn stores the
// session synchronously under the returned id; the immediate Prompt must
// resolve the SAME id. It asserts the store-key == return-id contract that the
// orchestrator relies on for Prompt/Status/Close.
func TestACPBackend_OneShot_SpawnThenPromptFindsSession(t *testing.T) {
	b := newOneShotBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// req.Persistent is deliberately false — this is what the daemon always
	// passes because the v1 wire has no persistent field.
	sess, err := b.Spawn(ctx, &SpawnRequest{
		WorkDir:    "/tmp",
		Persistent: false,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if !strings.HasPrefix(sess.ID, "ses-") {
		t.Fatalf("one-shot session id = %q, want ses- prefix (generateSessionID)", sess.ID)
	}

	// The session MUST be present in the registry under the returned id
	// immediately after Spawn returns — i.e. registration is synchronous.
	if _, ok := b.sessions.Load(sess.ID); !ok {
		t.Fatalf("session %q not registered synchronously after Spawn", sess.ID)
	}

	// Immediate prompt using the returned id resolves the live session.
	resp, promptErr := b.Prompt(ctx, &PromptRequest{
		SessionID: sess.ID,
		Prompt:    "hello",
	})
	if promptErr != nil {
		t.Fatalf("immediate Prompt after Spawn failed: %v", promptErr)
	}
	if resp.SessionID != sess.ID {
		t.Errorf("Prompt SessionID = %q, want %q", resp.SessionID, sess.ID)
	}
	if len(resp.Content) == 0 || !strings.Contains(resp.Content[0].Text, "oneshot:") {
		t.Errorf("unexpected prompt content: %+v", resp.Content)
	}
}

// TestACPBackend_OneShot_RegistrationIsSynchronous proves Spawn does not defer
// registration: the returned id is queryable via Status the instant Spawn
// returns, with no intervening synchronization.
func TestACPBackend_OneShot_RegistrationIsSynchronous(t *testing.T) {
	b := newOneShotBackend(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := b.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	st, statusErr := b.Status(ctx, sess.ID)
	if statusErr != nil {
		t.Fatalf("Status immediately after Spawn failed: %v", statusErr)
	}
	if st.SessionID != sess.ID {
		t.Errorf("Status SessionID = %q, want %q", st.SessionID, sess.ID)
	}
}

// TestACPBackend_Diagnostics_CreateKeyAndLookupMiss verifies the session
// registry diagnostics that make a real "session not found" debuggable: Spawn
// emits a create-key line, and a Prompt/Status/Close for an absent id emits a
// lookup-miss line listing the keys actually present. These are the operational
// logs an operator inspects when a session goes missing.
func TestACPBackend_Diagnostics_CreateKeyAndLookupMiss(t *testing.T) {
	b := newOneShotBackend(t)
	cl := &captureLogger{}
	b.SetLogger(cl)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := b.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Create-key diagnostic must record the stored key and one-shot mode.
	if got := cl.joined(); !strings.Contains(got, "session stored") || !strings.Contains(got, "cli-oneshot") {
		t.Errorf("missing create-key diagnostic; got:\n%s", got)
	}

	// A lookup for a different id must miss and log present_keys.
	if _, perr := b.Prompt(ctx, &PromptRequest{SessionID: "ses-absent", Prompt: "x"}); perr == nil {
		t.Fatal("expected SessionNotFoundError for absent id")
	}
	if _, serr := b.Status(ctx, "ses-absent"); serr == nil {
		t.Fatal("expected SessionNotFoundError on Status for absent id")
	}
	if cerr := b.Close(ctx, "ses-absent"); cerr == nil {
		t.Fatal("expected SessionNotFoundError on Close for absent id")
	}
	got := cl.joined()
	if !strings.Contains(got, "session miss") {
		t.Errorf("missing lookup-miss diagnostic; got:\n%s", got)
	}
	// The real session id should still be queryable (sanity: it wasn't evicted).
	if _, ok := b.sessions.Load(sess.ID); !ok {
		t.Errorf("real session %q evicted by failed lookups", sess.ID)
	}
}

// TestACPBackend_SessionMode_Labels covers the sessionMode label helper for all
// four storage shapes so the diagnostic logs name the mode correctly.
//
// The persistent shapes carry PIPES, not merely a cmd: the stdio pair is what
// makes a session speak stream-json, and a containerized one has no *exec.Cmd
// at all (acpSession.streamJSON).
func TestACPBackend_SessionMode_Labels(t *testing.T) {
	b := NewACPBackend(ACPBackendConfig{Command: "claude"})
	pipes := func(s *acpSession) *acpSession {
		s.stdin = discardWriteCloser{}
		s.stdout = bufio.NewScanner(strings.NewReader(""))
		return s
	}
	cases := []struct {
		name string
		sess *acpSession
		want string
	}{
		{"oneshot", &acpSession{id: "a"}, "cli-oneshot"},
		{"persistent", pipes(&acpSession{id: "b", cmd: &exec.Cmd{}}), "cli-persistent"},
		{"jsonrpc", &acpSession{id: "c", transport: &StdioTransport{}}, "jsonrpc"},
		{"container", pipes(&acpSession{id: "d", containerID: "ctr-1"}), "cli-container"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.sessionMode(tc.sess); got != tc.want {
				t.Errorf("sessionMode = %q, want %q", got, tc.want)
			}
		})
	}
}

// discardWriteCloser is a stand-in for an agent's stdin pipe in tests that only
// need the session to LOOK like it has one.
type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }
