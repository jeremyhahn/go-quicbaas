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
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

// mockResponder reads newline-delimited JSON-RPC requests from r and writes
// responses to w. It echoes back the request method in the result field.
func mockResponder(t *testing.T, r io.Reader, w io.WriteCloser, done chan struct{}) {
	t.Helper()
	defer w.Close()

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		select {
		case <-done:
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg JSONRPCMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		// Only respond to requests (messages with ID)
		if msg.ID == nil {
			continue
		}

		resp := JSONRPCMessage{
			JSONRPC: JSONRPCVersion,
			ID:      msg.ID,
			Result:  map[string]string{"method": msg.Method},
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

func TestStdioTransport_SendReceive(t *testing.T) {
	// clientWrite -> serverRead (transport writes here, mock reads from here)
	serverRead, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	// serverWrite -> clientRead (mock writes here, transport reads from here)
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	defer close(done)

	go mockResponder(t, serverRead, serverWrite, done)

	transport := newStdioTransportFromPipes(clientWrite, clientRead)
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := transport.Send(ctx, "test/method", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error)
	}

	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	var result map[string]string
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result["method"] != "test/method" {
		t.Errorf("result[method] = %q, want %q", result["method"], "test/method")
	}
}

func TestStdioTransport_Close(t *testing.T) {
	_, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	serverWrite.Close()

	transport := newStdioTransportFromPipes(clientWrite, clientRead)

	// Close should succeed
	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Second close should be a no-op
	if err := transport.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}

	// Send after close should fail
	ctx := context.Background()
	_, err = transport.Send(ctx, "test", nil)
	if err == nil {
		t.Fatal("Send after Close should return error")
	}
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Send after Close error = %v, want ErrSessionClosed", err)
	}

	// Notify after close should fail
	err = transport.Notify("test", nil)
	if err == nil {
		t.Fatal("Notify after Close should return error")
	}
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Notify after Close error = %v, want ErrSessionClosed", err)
	}
}

func TestStdioTransport_SendContextCanceled(t *testing.T) {
	// Create pipes but never write a response, so Send blocks
	_, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverWrite.Close()

	transport := newStdioTransportFromPipes(clientWrite, clientRead)
	defer transport.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately
	cancel()

	_, err = transport.Send(ctx, "test/method", nil)
	if err == nil {
		t.Fatal("Send with canceled context should return error")
	}
	if !errors.Is(err, ErrTransportFailed) {
		t.Errorf("error = %v, want ErrTransportFailed", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestStdioTransport_ConcurrentSend(t *testing.T) {
	serverRead, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	defer close(done)

	go mockResponder(t, serverRead, serverWrite, done)

	transport := newStdioTransportFromPipes(clientWrite, clientRead)
	defer transport.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const numGoroutines = 10
	var wg sync.WaitGroup
	errs := make([]error, numGoroutines)

	for i := range numGoroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, errs[idx] = transport.Send(ctx, "concurrent/test", map[string]int{"id": idx})
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Send() error = %v", i, err)
		}
	}
}

func TestStdioTransport_InvalidJSON(t *testing.T) {
	// stdout of the "server": the transport reads responses from clientRead.
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	// stdin of the "server": the transport writes requests to clientWrite; we
	// read them from serverReqRead. Reading the request is the synchronization
	// point — Send stores its pending waiter BEFORE it writes the request, so a
	// response written only after we have read the request cannot be dispatched
	// before the waiter exists. Writing the response unconditionally (as this
	// test previously did) races readLoop against Send's pending.Store and the
	// ID-1 response was dropped ~intermittently, hanging Send until its deadline
	// (#128 — "context deadline exceeded").
	serverReqRead, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverReqRead.Close()

	transport := newStdioTransportFromPipes(clientWrite, clientRead)
	defer transport.Close()

	go func() {
		// Wait for the request, guaranteeing Send has registered its waiter.
		_, _ = bufio.NewReader(serverReqRead).ReadString('\n')

		// A malformed line the readLoop must skip, then the real response.
		_, _ = serverWrite.Write([]byte("this is not json\n"))
		resp := JSONRPCMessage{
			JSONRPC: JSONRPCVersion,
			ID:      float64(1),
			Result:  "ok",
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		_, _ = serverWrite.Write(data)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := transport.Send(ctx, "test", nil)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp == nil {
		t.Fatal("Send() returned nil response")
	}
}

func TestStdioTransport_Notify(t *testing.T) {
	serverRead, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverRead.Close()

	// We don't need to read responses for notifications
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	serverWrite.Close()

	transport := newStdioTransportFromPipes(clientWrite, clientRead)
	defer transport.Close()

	err = transport.Notify("test/notify", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("Notify() error = %v", err)
	}

	// Read the notification from the pipe to verify it was sent
	buf := make([]byte, 4096)
	n, err := serverRead.Read(buf)
	if err != nil {
		t.Fatalf("read notification: %v", err)
	}

	var msg JSONRPCMessage
	if err := json.Unmarshal(buf[:n], &msg); err != nil {
		t.Fatalf("unmarshal notification: %v", err)
	}

	if msg.ID != nil {
		t.Errorf("notification should not have ID, got %v", msg.ID)
	}
	if msg.Method != "test/notify" {
		t.Errorf("Method = %q, want %q", msg.Method, "test/notify")
	}
}

func TestStdioTransport_PID(t *testing.T) {
	t.Run("NilCmd", func(t *testing.T) {
		transport := &StdioTransport{}
		if got := transport.PID(); got != 0 {
			t.Errorf("PID() = %d, want 0", got)
		}
	})

	t.Run("FromPipesReturnsZero", func(t *testing.T) {
		_, clientWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		clientRead, serverWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		serverWrite.Close()

		transport := newStdioTransportFromPipes(clientWrite, clientRead)
		defer transport.Close()

		if got := transport.PID(); got != 0 {
			t.Errorf("PID() = %d, want 0 for pipe-based transport", got)
		}
	})
}

func TestStdioTransport_WriteError(t *testing.T) {
	// Create a pipe and immediately close the write end so writes fail
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	serverWrite.Close()

	_, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	// Close the pipe that transport writes to, causing write errors
	clientWrite.Close()

	transport := newStdioTransportFromPipes(clientWrite, clientRead)
	defer transport.Close()

	// Notify should fail because stdin is closed
	err = transport.Notify("test", nil)
	if err == nil {
		t.Fatal("Notify to closed pipe should return error")
	}
	if !errors.Is(err, ErrTransportFailed) {
		t.Errorf("error = %v, want ErrTransportFailed", err)
	}
}

func TestStdioTransport_SendDoneChannel(t *testing.T) {
	// Test that Send returns when the done channel is closed
	_, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverWrite.Close()

	transport := newStdioTransportFromPipes(clientWrite, clientRead)

	// Close the transport in a goroutine after a small delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		transport.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = transport.Send(ctx, "test", nil)
	if err == nil {
		t.Fatal("Send should return error when transport closes")
	}
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("error = %v, want ErrSessionClosed", err)
	}
}

func TestNewStdioTransport_InvalidCommand(t *testing.T) {
	_, err := NewStdioTransport("/nonexistent/binary/xyz123", nil, "", nil)
	if err == nil {
		t.Fatal("NewStdioTransport with invalid command should return error")
	}
	if !errors.Is(err, ErrSpawnFailed) {
		t.Errorf("error = %v, want ErrSpawnFailed", err)
	}
}

func TestNewStdioTransport_WithCat(t *testing.T) {
	// Use cat as a minimal subprocess that reads stdin and writes stdout
	transport, err := NewStdioTransport("cat", nil, "", nil)
	if err != nil {
		t.Fatalf("NewStdioTransport(cat) error = %v", err)
	}

	if transport.PID() == 0 {
		t.Error("PID() should be nonzero for real subprocess")
	}

	// Close should gracefully terminate cat
	if err := transport.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify double close is safe
	if err := transport.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestNewStdioTransport_WithWorkDirAndEnv(t *testing.T) {
	dir := t.TempDir()
	transport, err := NewStdioTransport("cat", nil, dir, []string{"TEST_VAR=test"})
	if err != nil {
		t.Fatalf("NewStdioTransport error = %v", err)
	}
	defer transport.Close()

	if transport.cmd.Dir != dir {
		t.Errorf("cmd.Dir = %q, want %q", transport.cmd.Dir, dir)
	}
}

func TestStdioTransport_CloseDrainsPending(t *testing.T) {
	_, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverWrite.Close()

	transport := newStdioTransportFromPipes(clientWrite, clientRead)

	// Store a pending channel manually
	ch := make(chan *JSONRPCMessage, 1)
	transport.pending.Store(int64(99), ch)

	transport.Close()

	// The channel should be closed by Close()
	_, ok := <-ch
	if ok {
		t.Error("pending channel should be closed after transport.Close()")
	}
}

func TestStdioTransport_ReadLoopNonNumericID(t *testing.T) {
	// Test that readLoop handles messages with non-numeric IDs gracefully
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	_, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	transport := newStdioTransportFromPipes(clientWrite, clientRead)
	defer transport.Close()

	// Write a message with a string ID (not numeric), followed by a valid one
	go func() {
		// String ID -- should be skipped by readLoop
		msg1 := `{"jsonrpc":"2.0","id":"string-id","result":"skip"}` + "\n"
		serverWrite.Write([]byte(msg1))

		// Empty line -- should be skipped
		serverWrite.Write([]byte("\n"))

		// Valid numeric ID response
		resp := JSONRPCMessage{
			JSONRPC: JSONRPCVersion,
			ID:      float64(1),
			Result:  "ok",
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		serverWrite.Write(data)
	}()

	// Generous deadline: this test proves non-numeric-ID/blank-line
	// handling, not latency, and a 2s budget flaked on a loaded CI runner
	// executing several -race jobs concurrently (runner-log-proven).
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Send should get the valid response (ID 1)
	resp, err := transport.Send(ctx, "test", nil)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestStdioTransport_ReadLoopUnmatchedID(t *testing.T) {
	// Test that readLoop handles responses for IDs nobody is waiting for
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	_, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	transport := newStdioTransportFromPipes(clientWrite, clientRead)
	defer transport.Close()

	go func() {
		// Response for ID 999 -- nobody is waiting for this
		msg := `{"jsonrpc":"2.0","id":999,"result":"orphan"}` + "\n"
		serverWrite.Write([]byte(msg))

		// Valid response for ID 1
		resp := JSONRPCMessage{
			JSONRPC: JSONRPCVersion,
			ID:      float64(1),
			Result:  "matched",
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		serverWrite.Write(data)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := transport.Send(ctx, "test", nil)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}
