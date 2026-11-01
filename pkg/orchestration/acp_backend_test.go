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
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// =============================================================================
// JSON-RPC Mode Test Helpers
// =============================================================================

// acpMockResponder reads JSON-RPC requests and responds with appropriate ACP
// protocol responses for initialize, session/new, session/prompt, and session/close.
func acpMockResponder(t *testing.T, r io.Reader, w io.WriteCloser, done chan struct{}) {
	t.Helper()
	defer w.Close()

	buf := make([]byte, 8192)
	for {
		select {
		case <-done:
			return
		default:
		}

		n, err := r.Read(buf)
		if err != nil {
			return
		}

		var msg JSONRPCMessage
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			continue
		}

		if msg.ID == nil {
			// Notification, no response needed
			continue
		}

		var resp JSONRPCMessage
		resp.JSONRPC = JSONRPCVersion
		resp.ID = msg.ID

		switch msg.Method {
		case "initialize":
			resp.Result = InitializeResult{
				AgentInfo: ServerInfo{
					Name:    "mock-agent",
					Version: "1.0.0",
				},
			}
		case "session/new":
			resp.Result = SessionNewResult{
				SessionID: "test-session-001",
			}
		case "session/prompt":
			resp.Result = SessionPromptResult{
				SessionID: "test-session-001",
				Content: []ContentBlock{
					{Type: "text", Text: "Hello from mock agent"},
				},
				Model:      "test-model",
				StopReason: "end_turn",
			}
		case "session/close":
			resp.Result = map[string]string{"status": "closed"}
		default:
			resp.Error = &JSONRPCError{
				Code:    -32601,
				Message: "method not found: " + msg.Method,
			}
		}

		data, err := json.Marshal(resp)
		if err != nil {
			continue
		}
		data = append(data, '\n')

		if _, err := w.Write(data); err != nil {
			return
		}
	}
}

// mockTransportFactory returns a TransportFactory that creates pipe-based
// transports backed by a mock ACP responder.
func mockTransportFactory(t *testing.T) (TransportFactory, func()) {
	t.Helper()

	var cleanups []func()
	var mu sync.Mutex

	factory := func(command string, args []string, workDir string, env []string) (*StdioTransport, error) {
		serverRead, clientWrite, err := os.Pipe()
		if err != nil {
			return nil, err
		}

		clientRead, serverWrite, err := os.Pipe()
		if err != nil {
			clientWrite.Close()
			return nil, err
		}

		done := make(chan struct{})
		go acpMockResponder(t, serverRead, serverWrite, done)

		transport := newStdioTransportFromPipes(clientWrite, clientRead)

		mu.Lock()
		cleanups = append(cleanups, func() {
			close(done)
			serverRead.Close()
		})
		mu.Unlock()

		return transport, nil
	}

	cleanup := func() {
		mu.Lock()
		defer mu.Unlock()
		for _, fn := range cleanups {
			fn()
		}
	}

	return factory, cleanup
}

// newTestACPBackendWithMock creates an ACPBackend wired to a mock ACP responder
// via pipe-based transport factory (JSON-RPC mode).
func newTestACPBackendWithMock(t *testing.T) (*ACPBackend, func()) {
	t.Helper()

	factory, cleanup := mockTransportFactory(t)

	backend := NewACPBackend(ACPBackendConfig{
		Command:      "mock",
		Args:         nil,
		DefaultModel: "default-model",
	})
	backend.transportFactory = factory

	return backend, cleanup
}

// spawnMockSession uses backend.Spawn() to create a real session through the
// mock transport factory, exercising the full Spawn code path.
func spawnMockSession(t *testing.T, backend *ACPBackend) (string, func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{
		ProjectID:    "test-project",
		WorkDir:      "/tmp",
		Model:        "test-model",
		SystemPrompt: "You are a test agent",
		Env:          map[string]string{"TEST": "1"},
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	cleanup := func() {
		backend.Close(context.Background(), sess.ID)
	}

	return sess.ID, cleanup
}

// =============================================================================
// CLI Stream-JSON Mode Test Helpers
// =============================================================================

// streamJSONMockResponder simulates a Claude Code stream-json subprocess. It
// writes an init message on startup, then reads user messages from stdin and
// responds with assistant + result messages on stdout.
func streamJSONMockResponder(t *testing.T, stdinR io.Reader, stdoutW io.WriteCloser, initSessionID string, initModel string) {
	t.Helper()
	defer stdoutW.Close()

	// Write the init message.
	initMsg := streamJSONMessage{
		Type:      "system",
		Subtype:   "init",
		SessionID: initSessionID,
		Model:     initModel,
	}
	initBytes, marshalErr := json.Marshal(initMsg)
	if marshalErr != nil {
		return
	}
	initBytes = append(initBytes, '\n')
	if _, writeErr := stdoutW.Write(initBytes); writeErr != nil {
		return
	}

	// Read user messages and respond.
	scanner := bufio.NewScanner(stdinR)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var userMsg streamJSONUserMessage
		if jsonErr := json.Unmarshal(scanner.Bytes(), &userMsg); jsonErr != nil {
			continue
		}

		// Write assistant message.
		assistantMsg := streamJSONMessage{
			Type: "assistant",
			Message: &streamJSONAssistant{
				Role: "assistant",
				Content: []streamJSONContent{
					{Type: "text", Text: "Response to: " + userMsg.Message.Content},
				},
				StopReason: "end_turn",
				Model:      initModel,
			},
		}
		assistantBytes, aErr := json.Marshal(assistantMsg)
		if aErr != nil {
			continue
		}
		assistantBytes = append(assistantBytes, '\n')
		if _, wErr := stdoutW.Write(assistantBytes); wErr != nil {
			return
		}

		// Write result message.
		resultMsg := streamJSONMessage{
			Type:      "result",
			Subtype:   "success",
			SessionID: initSessionID,
			Result:    "Response to: " + userMsg.Message.Content,
		}
		resultBytes, rErr := json.Marshal(resultMsg)
		if rErr != nil {
			continue
		}
		resultBytes = append(resultBytes, '\n')
		if _, wErr := stdoutW.Write(resultBytes); wErr != nil {
			return
		}
	}
}

// mockProcessFactory returns a ProcessFactory that creates a pipe-backed mock
// Claude Code stream-json subprocess. The returned cleanup function closes any
// resources created by the factory.
func mockProcessFactory(t *testing.T, initSessionID string, initModel string) (ProcessFactory, func()) {
	t.Helper()

	var cleanups []func()
	var mu sync.Mutex

	factory := func(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error) {
		// Create pipes: the "subprocess" reads from stdinR and writes to stdoutW.
		stdinR, stdinW, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		stdoutR, stdoutW, err := os.Pipe()
		if err != nil {
			_ = stdinR.Close()
			_ = stdinW.Close()
			return nil, err
		}

		go streamJSONMockResponder(t, stdinR, stdoutW, initSessionID, initModel)

		// Create a real cmd that we will never actually Start() -- instead we
		// override its pipes. We use "true" as a no-op command to avoid needing
		// a real binary. But since spawnCLI calls cmd.StdinPipe() and
		// cmd.StdoutPipe(), we cannot use a pre-configured cmd.
		//
		// Instead, we use a different approach: we create a real subprocess
		// that copies stdin to /dev/null and has stdout piped, but the test
		// responder goroutine handles the actual protocol. This is too complex.
		//
		// Simpler approach: return a cmd that runs "cat" (which echoes stdin
		// to stdout), but intercept via the pipes directly.
		//
		// Actually, the cleanest approach for testing is to set up the cmd
		// so that StdinPipe and StdoutPipe work, then have the responder
		// connected. Since exec.Cmd.StdinPipe creates its own pipe, we need
		// a different strategy.
		//
		// We will create a real exec.Cmd that runs "cat" as a dummy process.
		// The processFactory returns the cmd, and spawnCLI calls StdinPipe /
		// StdoutPipe / Start. The mock responder is connected to the pipe
		// ends that spawnCLI reads/writes.
		//
		// But that means we can't control what the mock responder writes to
		// stdoutPipe, because stdoutPipe is created by cmd.StdoutPipe().
		//
		// The right approach: make the mock responder the actual subprocess.
		// We run a tiny helper binary. But for unit tests, we should avoid
		// real subprocesses.
		//
		// Best approach: override the cmd's Stdin and Stdout before Start()
		// is called, then use our pipes. But StdinPipe() fails if Stdin is
		// already set. So we need a cmd where StdinPipe and StdoutPipe
		// succeed, but the process uses our mock pipes.
		//
		// The simplest working approach: create a cmd that runs "cat",
		// which reads stdin and writes to stdout. The mock responder goroutine
		// will read from stdinR (which is the read end of what spawnCLI writes
		// to) and write to stdoutW (which is what spawnCLI reads from).
		//
		// Problem: cmd.StdinPipe creates a NEW pipe, different from our stdinR/W.
		// We need the responder to read from that new pipe.
		//
		// Solution: Don't use cmd.StdinPipe/StdoutPipe. Instead, set cmd.Stdin
		// and cmd.Stdout to our pipe ends, and have spawnCLI write to stdinW
		// and read from stdoutR directly.
		//
		// But spawnCLI calls cmd.StdinPipe() and cmd.StdoutPipe() which fail
		// if Stdin/Stdout are already set.
		//
		// We need to restructure spawnCLI to accept the pipes, or better yet,
		// have the processFactory return both the cmd and the pipes.

		// Actually - let's just make the mock process factory return a cmd
		// that uses the Go test helper subprocess pattern. The cmd runs the
		// test binary itself with a special env var. But this is getting
		// overcomplicated for a unit test.

		// Cleanest solution: close the mock responder pipes and let the
		// factory return an error to indicate "use pipe override". But that
		// breaks the interface.

		// FINAL APPROACH: We bypass exec.Cmd entirely by wrapping the pipes
		// in a pipeCmd that satisfies what spawnCLI needs. Since spawnCLI
		// calls cmd.StdinPipe(), cmd.StdoutPipe(), cmd.Start(), we create
		// a real cmd pointing to /bin/cat. When Start() runs, cat reads
		// from its stdin pipe and writes to its stdout pipe. Our mock
		// responder is connected through the cat process: we pipe stdinW ->
		// cat -> stdoutR. But cat just echoes, it doesn't implement the
		// stream-json protocol.
		//
		// OK, truly simplest: run the test binary itself as a subprocess
		// using the standard Go test helper pattern.

		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()

		// Use Go test helper subprocess pattern.
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestStreamJSONHelper")
		cmd.Env = append(os.Environ(),
			"STREAM_JSON_HELPER=1",
			"STREAM_JSON_SESSION_ID="+initSessionID,
			"STREAM_JSON_MODEL="+initModel,
		)
		if workDir != "" {
			cmd.Dir = workDir
		}

		mu.Lock()
		cleanups = append(cleanups, func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
		mu.Unlock()

		return cmd, nil
	}

	cleanup := func() {
		mu.Lock()
		defer mu.Unlock()
		for _, fn := range cleanups {
			fn()
		}
	}

	return factory, cleanup
}

// TestStreamJSONHelper is a helper subprocess that implements the stream-json
// protocol for testing. It is invoked by the test binary itself via the
// STREAM_JSON_HELPER environment variable.
func TestStreamJSONHelper(t *testing.T) {
	if os.Getenv("STREAM_JSON_HELPER") != "1" {
		return
	}

	sessionID := os.Getenv("STREAM_JSON_SESSION_ID")
	model := os.Getenv("STREAM_JSON_MODEL")

	// Write init message.
	initMsg := streamJSONMessage{
		Type:      "system",
		Subtype:   "init",
		SessionID: sessionID,
		Model:     model,
	}
	initBytes, err := json.Marshal(initMsg)
	if err != nil {
		os.Exit(1)
	}
	os.Stdout.Write(append(initBytes, '\n'))

	// Read user messages from stdin and respond.
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var userMsg streamJSONUserMessage
		if jsonErr := json.Unmarshal(scanner.Bytes(), &userMsg); jsonErr != nil {
			continue
		}

		// Write assistant message.
		assistantMsg := streamJSONMessage{
			Type: "assistant",
			Message: &streamJSONAssistant{
				Role: "assistant",
				Content: []streamJSONContent{
					{Type: "text", Text: "Response to: " + userMsg.Message.Content},
				},
				StopReason: "end_turn",
				Model:      model,
			},
		}
		aBytes, aErr := json.Marshal(assistantMsg)
		if aErr != nil {
			continue
		}
		os.Stdout.Write(append(aBytes, '\n'))

		// Write result message.
		resultMsg := streamJSONMessage{
			Type:      "result",
			Subtype:   "success",
			SessionID: sessionID,
			Result:    "Response to: " + userMsg.Message.Content,
		}
		rBytes, rErr := json.Marshal(resultMsg)
		if rErr != nil {
			continue
		}
		os.Stdout.Write(append(rBytes, '\n'))
	}

	os.Exit(0)
}

// TestStreamJSONErrorHelper is a helper subprocess that returns an error result
// from the stream-json protocol for testing error handling.
func TestStreamJSONErrorHelper(t *testing.T) {
	if os.Getenv("STREAM_JSON_ERROR_HELPER") != "1" {
		return
	}

	sessionID := os.Getenv("STREAM_JSON_SESSION_ID")
	model := os.Getenv("STREAM_JSON_MODEL")

	// Write init message.
	initMsg := streamJSONMessage{
		Type:      "system",
		Subtype:   "init",
		SessionID: sessionID,
		Model:     model,
	}
	initBytes, err := json.Marshal(initMsg)
	if err != nil {
		os.Exit(1)
	}
	os.Stdout.Write(append(initBytes, '\n'))

	// Read one user message, then respond with an error result.
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		resultMsg := streamJSONMessage{
			Type:    "result",
			Subtype: "error",
			IsError: true,
			Result:  "rate limit exceeded",
		}
		rBytes, rErr := json.Marshal(resultMsg)
		if rErr != nil {
			os.Exit(1)
		}
		os.Stdout.Write(append(rBytes, '\n'))
	}

	os.Exit(0)
}

// TestStreamJSONExitMidTurnHelper answers the handshake, then answers the
// first prompt with more assistant records than the stream reader buffers and
// exits without a result: an agent that dies mid-turn with output still in
// flight. With STREAM_JSON_EXIT_ON_STDIN_EOF=1 it waits for its stdin to close
// before it exits.
func TestStreamJSONExitMidTurnHelper(t *testing.T) {
	if os.Getenv("STREAM_JSON_EXIT_MID_TURN_HELPER") != "1" {
		return
	}

	out := bufio.NewWriter(os.Stdout)
	fmt.Fprintf(out, `{"type":"system","subtype":"init","session_id":%q,"model":%q}`+"\n",
		os.Getenv("STREAM_JSON_SESSION_ID"), os.Getenv("STREAM_JSON_MODEL"))
	out.Flush()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, streamJSONScannerBufferSize), streamJSONScannerBufferSize)
	if !scanner.Scan() {
		os.Exit(1)
	}
	out.WriteString(`{"type":"result","subtype":"success","result":"ready"}` + "\n")
	out.Flush()

	if !scanner.Scan() {
		os.Exit(1)
	}
	count, err := strconv.Atoi(os.Getenv("STREAM_JSON_RECORD_COUNT"))
	if err != nil {
		os.Exit(1)
	}
	for range count {
		out.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}` + "\n")
	}
	out.Flush()
	// The caller decides when the agent dies: it exits once its stdin closes.
	if os.Getenv("STREAM_JSON_EXIT_ON_STDIN_EOF") == "1" {
		for scanner.Scan() {
		}
	}
	os.Exit(3)
}

// TestStreamJSONExitHelper is a helper subprocess that writes the init message
// and then immediately exits, simulating an unexpected process termination.
func TestStreamJSONExitHelper(t *testing.T) {
	if os.Getenv("STREAM_JSON_EXIT_HELPER") != "1" {
		return
	}

	sessionID := os.Getenv("STREAM_JSON_SESSION_ID")
	model := os.Getenv("STREAM_JSON_MODEL")

	// Write init message.
	initMsg := streamJSONMessage{
		Type:      "system",
		Subtype:   "init",
		SessionID: sessionID,
		Model:     model,
	}
	initBytes, err := json.Marshal(initMsg)
	if err != nil {
		os.Exit(1)
	}
	os.Stdout.Write(append(initBytes, '\n'))

	// Read one user message then exit.
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()

	os.Exit(0)
}

// TestStreamJSONNoInitHelper is a helper subprocess that exits without writing
// an init message, simulating a startup failure.
func TestStreamJSONNoInitHelper(t *testing.T) {
	if os.Getenv("STREAM_JSON_NO_INIT_HELPER") != "1" {
		return
	}
	os.Exit(1)
}

// newTestCLIBackend creates an ACPBackend configured for CLI stream-json mode
// backed by a test helper subprocess.
func newTestCLIBackend(t *testing.T, helperTest string, helperEnvVar string) *ACPBackend {
	t.Helper()

	backend := NewACPBackend(ACPBackendConfig{
		Command:      "claude",
		Args:         []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"},
		DefaultModel: "claude-sonnet-4-5-20250514",
		CLIMode:      true,
	})

	backend.processFactory = func(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+helperTest)
		cmd.Env = append(os.Environ(),
			helperEnvVar+"=1",
			"STREAM_JSON_SESSION_ID=cli-test-session",
			"STREAM_JSON_MODEL=claude-sonnet-4-5-20250514",
		)
		if workDir != "" {
			cmd.Dir = workDir
		}
		return cmd, nil
	}

	return backend
}

// spawnCLISession creates a CLI stream-json session for testing.
func spawnCLISession(t *testing.T, backend *ACPBackend) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{
		ProjectID:    "test-project",
		WorkDir:      "/tmp",
		Model:        "test-model",
		SystemPrompt: "You are a test agent",
		Env:          map[string]string{"TEST": "1"},
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	return sess.ID
}

// =============================================================================
// Common Tests (both modes)
// =============================================================================

func TestACPBackend_Capabilities(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{})
	caps := backend.Capabilities()

	if !caps.PersistentSessions {
		t.Error("PersistentSessions should be true")
	}
	if !caps.ToolUse {
		t.Error("ToolUse should be true")
	}
	if !caps.FileAccess {
		t.Error("FileAccess should be true")
	}
	if caps.Streaming {
		t.Error("Streaming should be false")
	}
	if !caps.ACP {
		t.Error("ACP should be true")
	}
}

func TestACPBackend_Type(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{})
	if got := backend.Type(); got != "acp" {
		t.Errorf("Type() = %q, want %q", got, "acp")
	}
}

func TestACPBackendCapabilitiesKind(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{})
	caps := backend.Capabilities()

	if caps.Kind != BackendKindAgent {
		t.Errorf("Capabilities().Kind = %q, want %q", caps.Kind, BackendKindAgent)
	}
}

func TestACPBackendCapabilitiesKind_InvalidKind(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{})
	caps := backend.Capabilities()

	if caps.Kind == BackendKindModelServer {
		t.Error("ACP backend Kind should not be BackendKindModelServer")
	}
	if caps.Kind == "" {
		t.Error("ACP backend Kind should not be empty")
	}
}

func TestACPBackend_InvalidInputAndNotFound(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "echo"})
	ctx := context.Background()

	tests := []struct {
		name    string
		call    func() error
		wantErr error
	}{
		{
			name: "Spawn nil request",
			call: func() error {
				_, err := backend.Spawn(ctx, nil)
				return err
			},
			wantErr: ErrInvalidInput,
		},
		{
			name: "Spawn empty WorkDir",
			call: func() error {
				_, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: ""})
				return err
			},
			wantErr: ErrInvalidInput,
		},
		{
			name: "Prompt nil request",
			call: func() error {
				_, err := backend.Prompt(ctx, nil)
				return err
			},
			wantErr: ErrInvalidInput,
		},
		{
			name: "Prompt empty prompt",
			call: func() error {
				_, err := backend.Prompt(ctx, &PromptRequest{Prompt: ""})
				return err
			},
			wantErr: ErrInvalidInput,
		},
		{
			name: "Prompt unknown session",
			call: func() error {
				_, err := backend.Prompt(ctx, &PromptRequest{SessionID: "nonexistent", Prompt: "Hello"})
				return err
			},
			wantErr: ErrSessionNotFound,
		},
		{
			name: "Status unknown session",
			call: func() error {
				_, err := backend.Status(ctx, "nonexistent")
				return err
			},
			wantErr: ErrSessionNotFound,
		},
		{
			name: "Close unknown session",
			call: func() error {
				return backend.Close(ctx, "nonexistent")
			},
			wantErr: ErrSessionNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == ErrSessionNotFound {
				var target *SessionNotFoundError
				if !errors.As(err, &target) {
					t.Errorf("error = %T, want *SessionNotFoundError", err)
				}
			}
		})
	}
}

// =============================================================================
// JSON-RPC Mode Tests
// =============================================================================

func TestACPBackend_JSONRPC_SpawnAndClose(t *testing.T) {
	backend, cleanup := newTestACPBackendWithMock(t)
	defer cleanup()

	sessionID, mockCleanup := spawnMockSession(t, backend)
	defer mockCleanup()

	// Verify session is stored
	_, ok := backend.sessions.Load(sessionID)
	if !ok {
		t.Fatal("session not found in backend after spawn")
	}

	// Close the session
	ctx := context.Background()
	if err := backend.Close(ctx, sessionID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Session should be removed
	_, ok = backend.sessions.Load(sessionID)
	if ok {
		t.Error("session should be removed after close")
	}
}

func TestACPBackend_JSONRPC_Status(t *testing.T) {
	backend, cleanup := newTestACPBackendWithMock(t)
	defer cleanup()

	sessionID, mockCleanup := spawnMockSession(t, backend)
	defer mockCleanup()

	ctx := context.Background()
	status, err := backend.Status(ctx, sessionID)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", status.SessionID, sessionID)
	}
	if status.State != SessionStateIdle {
		t.Errorf("State = %q, want %q", status.State, SessionStateIdle)
	}
	if status.Uptime < 0 {
		t.Errorf("Uptime = %d, want >= 0", status.Uptime)
	}
}

func TestACPBackend_JSONRPC_Prompt_WithSession(t *testing.T) {
	backend, cleanup := newTestACPBackendWithMock(t)
	defer cleanup()

	sessionID, mockCleanup := spawnMockSession(t, backend)
	defer mockCleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := backend.Prompt(ctx, &PromptRequest{
		SessionID: sessionID,
		Prompt:    "Hello",
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	if resp.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", resp.SessionID, sessionID)
	}
	if len(resp.Content) == 0 {
		t.Fatal("Content should not be empty")
	}
	if resp.Content[0].Text != "Hello from mock agent" {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "Hello from mock agent")
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "end_turn")
	}
}

func TestACPBackend_JSONRPC_Prompt_Ephemeral(t *testing.T) {
	backend, cleanup := newTestACPBackendWithMock(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Prompt without session ID triggers ephemeral session
	resp, err := backend.Prompt(ctx, &PromptRequest{
		Prompt:    "Hello ephemeral",
		ProjectID: "test-project",
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	if len(resp.Content) == 0 {
		t.Fatal("Content should not be empty")
	}
	if resp.Content[0].Text != "Hello from mock agent" {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "Hello from mock agent")
	}
}

func TestACPBackend_JSONRPC_Status_TransportClosed(t *testing.T) {
	backend, cleanup := newTestACPBackendWithMock(t)
	defer cleanup()

	sessionID, mockCleanup := spawnMockSession(t, backend)
	defer mockCleanup()

	// Get the session and close its transport directly
	val, _ := backend.sessions.Load(sessionID)
	sess := val.(*acpSession)
	sess.transport.Close()

	ctx := context.Background()
	status, err := backend.Status(ctx, sessionID)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.State != SessionStateClosed {
		t.Errorf("State = %q, want %q", status.State, SessionStateClosed)
	}
}

func TestACPBackend_JSONRPC_Spawn_InvalidCommand(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{
		Command: "/nonexistent/binary/xyz",
	})

	ctx := context.Background()
	_, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp"})
	if err == nil {
		t.Fatal("Spawn with nonexistent command should return error")
	}
	if !errors.Is(err, ErrSpawnFailed) {
		t.Errorf("error = %v, want ErrSpawnFailed", err)
	}
}

func TestACPBackend_JSONRPC_Spawn_InitializeError(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{DefaultModel: "test"})

	backend.transportFactory = func(command string, args []string, workDir string, env []string) (*StdioTransport, error) {
		serverRead, clientWrite, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		clientRead, serverWrite, err := os.Pipe()
		if err != nil {
			clientWrite.Close()
			return nil, err
		}

		// Responder that returns error for initialize
		go func() {
			defer serverWrite.Close()
			scanner := json.NewDecoder(serverRead)
			var msg JSONRPCMessage
			if err := scanner.Decode(&msg); err != nil {
				return
			}
			resp := JSONRPCMessage{
				JSONRPC: JSONRPCVersion,
				ID:      msg.ID,
				Error: &JSONRPCError{
					Code:    -32600,
					Message: "initialization failed",
				},
			}
			data, _ := json.Marshal(resp)
			data = append(data, '\n')
			serverWrite.Write(data)
		}()

		return newStdioTransportFromPipes(clientWrite, clientRead), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp"})
	if err == nil {
		t.Fatal("Spawn should return error when initialize fails")
	}
	if !errors.Is(err, ErrProtocolViolation) {
		t.Errorf("error = %v, want ErrProtocolViolation", err)
	}
}

func TestACPBackend_JSONRPC_Spawn_SessionNewError(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{DefaultModel: "test"})

	backend.transportFactory = func(command string, args []string, workDir string, env []string) (*StdioTransport, error) {
		serverRead, clientWrite, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		clientRead, serverWrite, err := os.Pipe()
		if err != nil {
			return nil, err
		}

		go func() {
			defer serverWrite.Close()
			scanner := json.NewDecoder(serverRead)
			for {
				var msg JSONRPCMessage
				if err := scanner.Decode(&msg); err != nil {
					return
				}

				var resp JSONRPCMessage
				resp.JSONRPC = JSONRPCVersion
				resp.ID = msg.ID

				if msg.Method == "initialize" {
					resp.Result = InitializeResult{
						AgentInfo: ServerInfo{Name: "mock", Version: "1.0"},
					}
				} else {
					// session/new returns error
					resp.Error = &JSONRPCError{
						Code:    -32600,
						Message: "session creation failed",
					}
				}

				data, _ := json.Marshal(resp)
				data = append(data, '\n')
				serverWrite.Write(data)
			}
		}()

		return newStdioTransportFromPipes(clientWrite, clientRead), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp"})
	if err == nil {
		t.Fatal("Spawn should return error when session/new fails")
	}
	if !errors.Is(err, ErrProtocolViolation) {
		t.Errorf("error = %v, want ErrProtocolViolation", err)
	}
}

func TestACPBackend_JSONRPC_Spawn_TransportFactoryError(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{})
	backend.transportFactory = func(command string, args []string, workDir string, env []string) (*StdioTransport, error) {
		return nil, &SpawnError{Command: command, Cause: errors.New("factory error")}
	}

	ctx := context.Background()
	_, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp"})
	if err == nil {
		t.Fatal("Spawn should return error from transport factory")
	}
	if !errors.Is(err, ErrSpawnFailed) {
		t.Errorf("error = %v, want ErrSpawnFailed", err)
	}
}

// =============================================================================
// CLI Stream-JSON Mode Tests
// =============================================================================

func TestACPBackend_CLI_SpawnAndPrompt(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONHelper", "STREAM_JSON_HELPER")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{
		ProjectID:    "proj-1",
		WorkDir:      "/tmp",
		Model:        "test-model",
		SystemPrompt: "You are a test agent",
		Env:          map[string]string{"KEY": "value"},
		Persistent:   true,
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	if sess.ID == "" {
		t.Error("session ID should not be empty")
	}
	if sess.Backend != "acp" {
		t.Errorf("Backend = %q, want %q", sess.Backend, "acp")
	}
	if sess.Status != SessionStateActive {
		t.Errorf("Status = %q, want %q", sess.Status, SessionStateActive)
	}
	if sess.PID == 0 {
		t.Error("PID should not be 0 for CLI session")
	}

	// Verify internal session state.
	val, ok := backend.sessions.Load(sess.ID)
	if !ok {
		t.Fatal("session not found in backend after spawn")
	}
	internal := val.(*acpSession)
	if internal.cmd == nil {
		t.Error("cmd should not be nil for CLI session")
	}
	if internal.stdin == nil {
		t.Error("stdin should not be nil for CLI session")
	}
	if internal.reader == nil {
		t.Error("reader should not be nil for CLI session")
	}
	if internal.transport != nil {
		t.Error("transport should be nil for CLI session")
	}

	// Send a prompt.
	resp, err := backend.Prompt(ctx, &PromptRequest{
		SessionID: sess.ID,
		Prompt:    "Hello",
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	if resp.SessionID != sess.ID {
		t.Errorf("SessionID = %q, want %q", resp.SessionID, sess.ID)
	}
	if len(resp.Content) == 0 {
		t.Fatal("Content should not be empty")
	}
	if resp.Content[0].Text != "Response to: Hello" {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "Response to: Hello")
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "end_turn")
	}
}

func TestACPBackend_CLI_SpawnAndPrompt_InvalidCommand(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{
		Command: "/nonexistent/binary/xyz",
		CLIMode: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err == nil {
		t.Fatal("Spawn with nonexistent command should return error")
	}
	if !errors.Is(err, ErrSpawnFailed) {
		t.Errorf("error = %v, want ErrSpawnFailed", err)
	}
}

func TestACPBackend_CLI_SpawnNoInit(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONNoInitHelper", "STREAM_JSON_NO_INIT_HELPER")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err == nil {
		t.Fatal("Spawn should return error when init message is not received")
	}
	if !errors.Is(err, ErrSpawnFailed) {
		t.Errorf("error = %v, want ErrSpawnFailed", err)
	}
}

func TestACPBackend_CLI_MultiplePrompts(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONHelper", "STREAM_JSON_HELPER")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{
		WorkDir:    "/tmp",
		Persistent: true,
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	// Send multiple prompts on the same session.
	prompts := []string{"First prompt", "Second prompt", "Third prompt"}
	for _, p := range prompts {
		resp, promptErr := backend.Prompt(ctx, &PromptRequest{
			SessionID: sess.ID,
			Prompt:    p,
		})
		if promptErr != nil {
			t.Fatalf("Prompt(%q) error = %v", p, promptErr)
		}
		expected := "Response to: " + p
		if len(resp.Content) == 0 || resp.Content[0].Text != expected {
			got := ""
			if len(resp.Content) > 0 {
				got = resp.Content[0].Text
			}
			t.Errorf("Prompt(%q) Content = %q, want %q", p, got, expected)
		}
	}
}

func TestACPBackend_CLI_PromptError(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONErrorHelper", "STREAM_JSON_ERROR_HELPER")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	_, err = backend.Prompt(ctx, &PromptRequest{
		SessionID: sess.ID,
		Prompt:    "Hello",
	})
	if err == nil {
		t.Fatal("Prompt should return error when result has is_error=true")
	}
	if !errors.Is(err, ErrPromptFailed) {
		t.Errorf("error = %v, want ErrPromptFailed", err)
	}
	// The error may be ErrStreamJSON (if the result was read) or
	// ErrStreamJSONProcessExited (if the process exited before reading).
	if !errors.Is(err, ErrStreamJSON) && !errors.Is(err, ErrStreamJSONProcessExited) {
		t.Errorf("error = %v, want ErrStreamJSON or ErrStreamJSONProcessExited", err)
	}
}

// TestACPBackend_CLI_PromptExitDrainsStdout pins the order in which a
// persistent CLI agent that dies mid-turn is reaped: stdout is drained to EOF
// first, so the prompt reports the process exit, never the raw "file already
// closed" a read gets once exec.Cmd.Wait has closed the pipe under the reader
// (#1329).
//
// The prompt's consumer is held on the first record, so the reader fills its
// buffer and parks with output still unread. synctest.Wait then returns only
// once every goroutine is durably blocked, which is after the agent has exited
// and after any reap that would close stdout has finished; only then is the
// consumer released.
func TestACPBackend_CLI_PromptExitDrainsStdout(t *testing.T) {
	// One record held by the consumer, a full buffer, and one more the reader
	// holds while it waits for room: the reader cannot reach EOF on its own.
	t.Setenv("STREAM_JSON_RECORD_COUNT", strconv.Itoa(streamJSONReaderBuffer+2))

	synctest.Test(t, func(t *testing.T) {
		backend := newTestCLIBackend(t, "TestStreamJSONExitMidTurnHelper", "STREAM_JSON_EXIT_MID_TURN_HELPER")

		sess, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: "/tmp", Persistent: true})
		if err != nil {
			t.Fatalf("Spawn() error = %v", err)
		}
		defer backend.Close(context.Background(), sess.ID)

		held := make(chan struct{})
		release := make(chan struct{})
		var holdOnce sync.Once
		promptErr := make(chan error, 1)
		go func() {
			_, promptErrValue := backend.Prompt(context.Background(), &PromptRequest{
				SessionID: sess.ID,
				Prompt:    "Hello",
				EventCallback: func(*SessionEvent) {
					holdOnce.Do(func() {
						close(held)
						<-release
					})
				},
			})
			promptErr <- promptErrValue
		}()

		<-held
		synctest.Wait()
		close(release)

		err = <-promptErr
		if errors.Is(err, os.ErrClosed) {
			t.Fatalf("Prompt() error = %v, read stdout after it was closed", err)
		}
		var exited *StreamJSONProcessExitedError
		if !errors.As(err, &exited) {
			t.Fatalf("Prompt() error = %v, want ErrStreamJSONProcessExited", err)
		}
		if exited.ExitCode != 3 {
			t.Errorf("ExitCode = %d, want 3", exited.ExitCode)
		}
	})
}

func TestACPBackend_CLI_PromptProcessExited(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONExitHelper", "STREAM_JSON_EXIT_HELPER")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	_, err = backend.Prompt(ctx, &PromptRequest{
		SessionID: sess.ID,
		Prompt:    "Hello",
	})
	if err == nil {
		t.Fatal("Prompt should return error when process exits unexpectedly")
	}
	if !errors.Is(err, ErrPromptFailed) {
		t.Errorf("error = %v, want ErrPromptFailed", err)
	}
}

func TestACPBackend_CLI_Status(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONHelper", "STREAM_JSON_HELPER")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	status, err := backend.Status(ctx, sess.ID)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.SessionID != sess.ID {
		t.Errorf("SessionID = %q, want %q", status.SessionID, sess.ID)
	}
	if status.State != SessionStateActive {
		t.Errorf("State = %q, want %q", status.State, SessionStateActive)
	}
	if status.Uptime < 0 {
		t.Errorf("Uptime = %d, want >= 0", status.Uptime)
	}
}

func TestACPBackend_CLI_StatusAfterClose(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONHelper", "STREAM_JSON_HELPER")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	if closeErr := backend.Close(ctx, sess.ID); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}

	// Session should be removed after close.
	_, err = backend.Status(ctx, sess.ID)
	if err == nil {
		t.Fatal("Status after Close should return error")
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("error = %v, want ErrSessionNotFound", err)
	}
}

func TestACPBackend_CLI_Close(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONHelper", "STREAM_JSON_HELPER")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	if closeErr := backend.Close(ctx, sess.ID); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}

	// Session should be removed.
	_, ok := backend.sessions.Load(sess.ID)
	if ok {
		t.Error("session should be removed after close")
	}
}

func TestACPBackend_CLI_Ephemeral(t *testing.T) {
	// Ephemeral prompts (no session ID) use one-shot mode: spawn a fresh
	// process, send prompt on stdin, read JSON result from stdout.
	backend := &ACPBackend{
		config: ACPBackendConfig{
			Command: "test",
			Args:    []string{"--print", "--output-format", "json"},
			CLIMode: true,
		},
		transportFactory: NewStdioTransport,
		processFactory: func(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error) {
			// Return a process that outputs Claude --output-format json response
			cmd := exec.CommandContext(ctx, "echo", `{"type":"result","result":"ephemeral response","is_error":false}`)
			if workDir != "" {
				cmd.Dir = workDir
			}
			return cmd, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := backend.Prompt(ctx, &PromptRequest{
		Prompt:    "Hello ephemeral",
		ProjectID: "test-project",
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}

	if len(resp.Content) == 0 {
		t.Fatal("Content should not be empty")
	}
	if resp.Content[0].Text != "ephemeral response" {
		t.Errorf("Content[0].Text = %q, want %q", resp.Content[0].Text, "ephemeral response")
	}
}

func TestACPBackend_CLI_SessionIDFromInit(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONHelper", "STREAM_JSON_HELPER")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	// The mock helper writes session_id="cli-test-session" in the init message.
	if sess.ID != "cli-test-session" {
		t.Errorf("SessionID = %q, want %q (from init message)", sess.ID, "cli-test-session")
	}
}

func TestACPBackend_CLI_ModelFromInit(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONHelper", "STREAM_JSON_HELPER")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	// The mock helper writes model="claude-sonnet-4-5-20250514" in the init message.
	if sess.Model != "claude-sonnet-4-5-20250514" {
		t.Errorf("Model = %q, want %q (from init message)", sess.Model, "claude-sonnet-4-5-20250514")
	}
}

func TestACPBackend_CLI_ProcessFactoryError(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{
		Command: "claude",
		CLIMode: true,
	})
	backend.processFactory = func(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error) {
		return nil, &SpawnError{Command: command, Cause: errors.New("factory error")}
	}

	ctx := context.Background()
	_, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err == nil {
		t.Fatal("Spawn should return error from process factory")
	}
	if !errors.Is(err, ErrSpawnFailed) {
		t.Errorf("error = %v, want ErrSpawnFailed", err)
	}
}

func TestACPBackend_CLI_PromptClosedSession(t *testing.T) {
	backend := newTestCLIBackend(t, "TestStreamJSONHelper", "STREAM_JSON_HELPER")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	// Manually mark the session as closed without removing it from the map.
	val, _ := backend.sessions.Load(sess.ID)
	internal := val.(*acpSession)
	internal.closed.Store(true)

	_, err = backend.Prompt(ctx, &PromptRequest{
		SessionID: sess.ID,
		Prompt:    "Hello",
	})
	if err == nil {
		t.Fatal("Prompt on closed session should return error")
	}
	if !errors.Is(err, ErrPromptFailed) {
		t.Errorf("error = %v, want ErrPromptFailed", err)
	}
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("error = %v, want ErrSessionClosed", err)
	}

	// Clean up.
	backend.sessions.Delete(sess.ID)
	_ = internal.stdin.Close()
	_ = internal.cmd.Process.Kill()
	// Through the session's own reaper, not cmd.Wait: the session's supervisor
	// holds the single Wait the process is allowed, and a second one here is
	// the concurrent call os/exec forbids.
	_ = internal.reapProcess()
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestOneShotExecutionError_UnwrapNoCause(t *testing.T) {
	err := &OneShotExecutionError{
		Command: "claude",
		Stderr:  "some error",
	}

	if !errors.Is(err, ErrOneShotExecution) {
		t.Error("should unwrap to ErrOneShotExecution")
	}

	unwrapped := err.Unwrap()
	if len(unwrapped) != 1 {
		t.Errorf("Unwrap() should return 1 error when no cause, got %d", len(unwrapped))
	}
}

func TestACPBackendErrors_ErrorAndUnwrap(t *testing.T) {
	oneShotCause := errors.New("exit status 1")

	tests := []struct {
		name        string
		err         error
		wantContain []string
		wantAbsent  []string
		wantIs      []error
	}{
		{
			// THE STDERR TAIL IS WITHHELD FROM THE MESSAGE, on purpose: an error string
			// reaches every %v in the process, and an agent's diagnostics can carry the
			// credential material its environment holds. The count says evidence exists;
			// the field carries it for a caller that asks by name.
			name: "OneShotExecutionError",
			err: &OneShotExecutionError{
				Command: "claude",
				Stderr:  "permission denied",
				Cause:   oneShotCause,
			},
			wantContain: []string{"claude", "withheld"},
			wantAbsent:  []string{"permission denied"},
			wantIs:      []error{ErrOneShotExecution, oneShotCause},
		},
		{
			// INVERTED (review 1330): the agent's own result text is the most likely
			// place in this package for a credential to appear — it is whatever the
			// model wrote about what went wrong, and what went wrong is often a command
			// it ran with a token in it. Error() reports its SIZE, never its content.
			name: "StreamJSONError",
			err: &StreamJSONError{
				Command: "claude",
				Result:  "rate limit exceeded",
			},
			wantContain: []string{"claude", "19 bytes of result withheld"},
			wantAbsent:  []string{"rate limit exceeded"},
			wantIs:      []error{ErrStreamJSON},
		},
		{
			name:        "StreamJSONProcessExitedError",
			err:         &StreamJSONProcessExitedError{Command: "claude"},
			wantContain: []string{"claude", "process exited"},
			wantIs:      []error{ErrStreamJSONProcessExited},
		},
		{
			name:        "SessionClosedError",
			err:         &SessionClosedError{SessionID: "test-session"},
			wantContain: []string{"test-session", "session closed"},
			wantIs:      []error{ErrSessionClosed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			for _, want := range tt.wantContain {
				if !strings.Contains(msg, want) {
					t.Errorf("Error() = %q, want it to contain %q", msg, want)
				}
			}
			for _, secret := range tt.wantAbsent {
				if strings.Contains(msg, secret) {
					t.Errorf("Error() = %q rendered %q, which is a secret-to-log path", msg, secret)
				}
			}
			for _, target := range tt.wantIs {
				if !errors.Is(tt.err, target) {
					t.Errorf("errors.Is(%T, %v) = false, want true", tt.err, target)
				}
			}
		})
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1, err := generateSessionID()
	if err != nil {
		t.Fatalf("generateSessionID() error = %v", err)
	}

	id2, err := generateSessionID()
	if err != nil {
		t.Fatalf("generateSessionID() error = %v", err)
	}

	if !strings.HasPrefix(id1, "ses-") {
		t.Errorf("session ID should have prefix 'ses-', got: %s", id1)
	}
	if id1 == id2 {
		t.Error("session IDs should be unique")
	}
	// 16 bytes = 32 hex chars + "ses-" prefix = 36 chars
	if len(id1) != 36 {
		t.Errorf("session ID length = %d, want 36", len(id1))
	}
}

// =============================================================================
// ToolGuidance Tests
// =============================================================================

func TestACPBackend_ToolGuidance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		command     string
		model       string
		wantContain []string // nil means the guidance must be empty
	}{
		// opencode's Bash tool needs a 'description' field and its file tools a 'filePath'.
		{command: "opencode", model: "some-model", wantContain: []string{"description", "filePath"}},
		{command: "claude", model: "claude-sonnet-4-5-20250514"},
		{command: "codex", model: "o3-mini"},
		{command: "some-unknown-agent", model: "model-x"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			t.Parallel()

			backend := NewACPBackend(ACPBackendConfig{
				Command:      tt.command,
				DefaultModel: tt.model,
			})

			guidance := backend.ToolGuidance()
			if tt.wantContain == nil {
				if guidance != "" {
					t.Errorf("ToolGuidance() = %q, want empty", guidance)
				}
				return
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(guidance, want) {
					t.Errorf("ToolGuidance() = %q, want it to contain %q", guidance, want)
				}
			}
		})
	}
}
