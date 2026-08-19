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
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// processShutdownTimeout is the duration to wait for a subprocess to exit
	// gracefully before sending SIGKILL.
	processShutdownTimeout = 5 * time.Second
)

// StdioTransport manages JSON-RPC communication with an ACP subprocess over
// newline-delimited JSON on stdin/stdout.
// NotificationHandler processes JSON-RPC notifications (messages with no ID).
// Used to capture session/update events from ACP agents like OpenCode.
type NotificationHandler func(msg *JSONRPCMessage)

// StdioTransport manages JSON-RPC communication with an ACP subprocess over
// newline-delimited JSON on stdin/stdout.
type StdioTransport struct {
	cmd                 *exec.Cmd
	stdin               io.WriteCloser
	stdout              *bufio.Scanner
	mu                  sync.Mutex // protects writes to stdin
	nextID              atomic.Int64
	pending             sync.Map // map[int64]chan *JSONRPCMessage
	closed              atomic.Bool
	done                chan struct{}
	readDone            chan struct{} // closed when readLoop goroutine exits
	notificationHandler atomic.Value  // stores NotificationHandler
}

// SetNotificationHandler registers a handler for JSON-RPC notifications.
func (t *StdioTransport) SetNotificationHandler(handler NotificationHandler) {
	t.notificationHandler.Store(handler)
}

// NewStdioTransport creates a new StdioTransport by starting the specified
// command as a subprocess. Communication occurs via newline-delimited JSON-RPC
// over the subprocess stdin/stdout.
func NewStdioTransport(command string, args []string, workDir string, env []string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	if len(env) > 0 {
		// Build env by starting with os.Environ() then overriding with
		// custom env. Keys in the custom env take precedence.
		overrides := make(map[string]struct{})
		for _, e := range env {
			if idx := strings.IndexByte(e, '='); idx > 0 {
				overrides[e[:idx]] = struct{}{}
			}
		}
		var merged []string
		for _, e := range os.Environ() {
			if idx := strings.IndexByte(e, '='); idx > 0 {
				if _, skip := overrides[e[:idx]]; skip {
					continue
				}
			}
			merged = append(merged, e)
		}
		merged = append(merged, env...)
		cmd.Env = merged
	}
	// Note: Setpgid was removed because it interfered with pipe inheritance
	// on some systems. Process isolation is handled via HOME env isolation.

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, &SpawnError{Command: command, Cause: err}
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdinPipe.Close()
		return nil, &SpawnError{Command: command, Cause: err}
	}

	// Capture stderr for debugging subprocess failures.
	stderrFile, stderrErr := os.CreateTemp("", "acp-stderr-*.log")
	if stderrErr == nil {
		cmd.Stderr = stderrFile
	} else {
		cmd.Stderr = io.Discard
	}

	if err := cmd.Start(); err != nil {
		_ = stdinPipe.Close()
		return nil, &SpawnError{Command: command, Cause: err}
	}

	// Log process start for debugging.
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	fmt.Fprintf(os.Stderr, "[transport] started %s pid=%d workDir=%s\n", command, pid, workDir)

	t := &StdioTransport{
		cmd:      cmd,
		stdin:    stdinPipe,
		stdout:   bufio.NewScanner(stdoutPipe),
		done:     make(chan struct{}),
		readDone: make(chan struct{}),
	}

	go t.readLoop()

	return t, nil
}

// newStdioTransportFromPipes creates a StdioTransport from pre-existing pipes.
// This is used internally for testing without spawning a real subprocess.
func newStdioTransportFromPipes(stdin io.WriteCloser, stdout io.Reader) *StdioTransport {
	t := &StdioTransport{
		stdin:    stdin,
		stdout:   bufio.NewScanner(stdout),
		done:     make(chan struct{}),
		readDone: make(chan struct{}),
	}

	go t.readLoop()

	return t
}

// Send sends a JSON-RPC request and waits for the matching response.
func (t *StdioTransport) Send(ctx context.Context, method string, params any) (*JSONRPCMessage, error) {
	if t.closed.Load() {
		return nil, &TransportError{Operation: "send", Cause: ErrSessionClosed}
	}

	id := t.nextID.Add(1)

	msg := &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  params,
	}

	respCh := make(chan *JSONRPCMessage, 1)
	t.pending.Store(id, respCh)
	defer t.pending.Delete(id)

	if err := t.writeMessage(msg); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, &TransportError{Operation: "send", Cause: ctx.Err()}
	case resp, ok := <-respCh:
		if !ok {
			return nil, &TransportError{Operation: "send", Cause: ErrSessionClosed}
		}
		return resp, nil
	case <-t.done:
		// An agent that answers and immediately exits leaves both cases ready:
		// readLoop always delivers the response before it observes EOF and
		// closes done. select picks uniformly among ready cases, so the answer
		// has to be re-checked here or it is discarded at random.
		select {
		case resp, ok := <-respCh:
			if ok {
				return resp, nil
			}
		default:
		}
		return nil, &TransportError{Operation: "send", Cause: ErrSessionClosed}
	}
}

// Notify sends a JSON-RPC notification (no ID, no response expected).
func (t *StdioTransport) Notify(method string, params any) error {
	if t.closed.Load() {
		return &TransportError{Operation: "notify", Cause: ErrSessionClosed}
	}

	msg := &JSONRPCMessage{
		JSONRPC: JSONRPCVersion,
		Method:  method,
		Params:  params,
	}

	return t.writeMessage(msg)
}

// Close shuts down the transport. It closes stdin to signal EOF to the
// subprocess, sends SIGTERM, and waits for the readLoop goroutine to finish
// (which calls cmd.Wait exactly once). If the process does not exit within
// the shutdown timeout, SIGKILL is sent.
func (t *StdioTransport) Close() error {
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}

	// Close stdin to signal EOF to the subprocess
	_ = t.stdin.Close()

	// Close the done channel to unblock pending requests
	close(t.done)

	// Drain all pending channels
	t.pending.Range(func(key, value any) bool {
		if ch, ok := value.(chan *JSONRPCMessage); ok {
			close(ch)
		}
		t.pending.Delete(key)
		return true
	})

	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}

	// Try graceful shutdown with SIGTERM
	_ = t.cmd.Process.Signal(syscall.SIGTERM)

	// Wait for readLoop to finish (it calls cmd.Wait exactly once).
	// If the process doesn't exit in time, escalate to SIGKILL.
	select {
	case <-t.readDone:
		return nil
	case <-time.After(processShutdownTimeout):
		_ = t.cmd.Process.Kill()
		<-t.readDone
		return nil
	}
}

// PID returns the subprocess PID, or 0 if no subprocess is running.
func (t *StdioTransport) PID() int {
	if t.cmd != nil && t.cmd.Process != nil {
		return t.cmd.Process.Pid
	}
	return 0
}

// writeMessage serializes a JSON-RPC message and writes it to stdin followed
// by a newline. The mutex protects concurrent writes to the stdin pipe.
func (t *StdioTransport) writeMessage(msg *JSONRPCMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return &TransportError{Operation: "marshal", Cause: err}
	}
	data = append(data, '\n')

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, err := t.stdin.Write(data); err != nil {
		return &TransportError{Operation: "write", Cause: err}
	}

	return nil
}

// readLoop reads newline-delimited JSON-RPC messages from stdout and dispatches
// responses to the matching pending channel. Messages without an ID (notifications)
// are currently discarded.
func (t *StdioTransport) readLoop() {
	defer close(t.readDone)

	for t.stdout.Scan() {
		line := t.stdout.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg JSONRPCMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		// Dispatch responses to pending callers
		if msg.ID != nil {
			// JSON numbers unmarshal as float64
			var id int64
			switch v := msg.ID.(type) {
			case float64:
				id = int64(v)
			case int64:
				id = v
			default:
				continue
			}

			if ch, ok := t.pending.LoadAndDelete(id); ok {
				if respCh, ok := ch.(chan *JSONRPCMessage); ok {
					select {
					case respCh <- &msg:
					default:
					}
				}
			}
		}
		// Dispatch notifications (no ID) to the handler if registered.
		if msg.ID == nil && msg.Method != "" {
			if handler, ok := t.notificationHandler.Load().(NotificationHandler); ok && handler != nil {
				handler(&msg)
			}
		}
	}

	// Process exited or pipe closed. Signal done via CAS to avoid double-close.
	scanErr := t.stdout.Err()
	pid := 0
	if t.cmd != nil && t.cmd.Process != nil {
		pid = t.cmd.Process.Pid
	}
	fmt.Fprintf(os.Stderr, "[transport] readLoop exited pid=%d scanErr=%v\n", pid, scanErr)
	if t.closed.CompareAndSwap(false, true) {
		close(t.done)
	}
	if t.cmd != nil && t.cmd.Process != nil {
		waitErr := t.cmd.Wait()
		fmt.Fprintf(os.Stderr, "[transport] process exited pid=%d waitErr=%v\n", pid, waitErr)
	}
}
