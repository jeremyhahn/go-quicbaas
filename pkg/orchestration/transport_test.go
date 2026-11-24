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

// stdioTestTimeout bounds a Send whose answer is already ordered after the
// request that registers its waiter. It is a liveness backstop, not a race
// budget: every stdio test using it synchronizes on the request first, so a
// value this generous should never be approached. When one of these tests
// times out, the ordering has broken and the number is not the thing to
// change -- raising it converts a fast failure into a slow one and leaves the
// defect (#140; #128 was diagnosed after exactly that 2s -> 30s raise).
const stdioTestTimeout = 10 * time.Second

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

	// Kept open for the life of the transport: on EOF readLoop closes the
	// session itself, and Close() would then return early through its
	// compare-and-swap without ever running the body under test.
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverWrite.Close()

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

	// No responses are needed for a notification, but this end must stay open
	// for the life of the transport: readLoop treats EOF on it as the session
	// ending and sets closed, which would race Notify's own closed check.
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverWrite.Close()

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

// TestStdioTransport_NotifyAfterPeerEOF is the control for
// TestStdioTransport_Notify: the same call, the opposite precondition, the
// opposite required outcome.
//
// TestStdioTransport_Notify keeps the peer's stdout open and requires success.
// Here the peer's stdout is closed and the test waits for readLoop to observe
// the EOF, so the session is provably over before Notify is called, and
// ErrSessionClosed is then the only correct answer.
//
// Pairing the two is what makes either meaningful. The flake this replaces
// (#207, and #79 before it) closed the peer's stdout and then asserted
// success: it stood in this test's precondition while demanding the other
// test's outcome, so which one it got was a coin toss between readLoop's
// compare-and-swap and Notify's closed check. If a future change makes
// readLoop stop ending the session on EOF, this test fails while
// TestStdioTransport_Notify still passes -- the asymmetry is the point.
func TestStdioTransport_NotifyAfterPeerEOF(t *testing.T) {
	serverRead, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverRead.Close()
	// Closed here rather than left to transport.Close(): readLoop wins the
	// closed compare-and-swap in this test by construction, so Close() returns
	// through its early exit and never closes stdin. Without this the test
	// leaks both descriptors on every iteration, which a -count=200 loop
	// notices. LIFO puts these after transport.Close(), so nothing the
	// transport still uses is closed out from under it.
	defer clientWrite.Close()

	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer clientRead.Close()

	transport := newStdioTransportFromPipes(clientWrite, clientRead)
	defer transport.Close()

	// End the peer's stdout, then wait for readLoop to exit. readLoop closes
	// readDone in a defer that runs after its closed compare-and-swap, so this
	// wait -- not a sleep or a retry -- is what establishes "session closed".
	serverWrite.Close()
	<-transport.readDone

	if !transport.closed.Load() {
		t.Fatal("readLoop exited without marking the session closed")
	}

	err = transport.Notify("test/notify", map[string]string{"key": "value"})
	if err == nil {
		t.Fatal("Notify() on a session ended by peer EOF should return an error")
	}
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Notify() error = %v, want ErrSessionClosed", err)
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
		defer serverWrite.Close()

		transport := newStdioTransportFromPipes(clientWrite, clientRead)
		defer transport.Close()

		if got := transport.PID(); got != 0 {
			t.Errorf("PID() = %d, want 0 for pipe-based transport", got)
		}
	})
}

func TestStdioTransport_WriteError(t *testing.T) {
	// Left open: an EOF here would close the session and Notify would report
	// ErrSessionClosed instead of the write failure this test is about.
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverWrite.Close()

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

	// stdin of the "server": the transport writes its request to clientWrite and
	// we read it from serverReqRead. Reading the request is the synchronization
	// point -- Send stores its pending waiter BEFORE writing the request
	// (transport.go, pending.Store then writeMessage), so a response written
	// only after the request has been read cannot be dispatched before the
	// waiter exists. Answering unconditionally raced readLoop against that
	// Store and dropped the ID-1 response, hanging Send until its deadline
	// (#140; the same defect #129 fixed in TestStdioTransport_InvalidJSON).
	serverReqRead, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverReqRead.Close()

	transport := newStdioTransportFromPipes(clientWrite, clientRead)
	defer transport.Close()

	// Write a message with a string ID (not numeric), followed by a valid one
	go func() {
		// Wait for the request, guaranteeing Send has registered its waiter.
		_, _ = bufio.NewReader(serverReqRead).ReadString('\n')

		// String ID -- should be skipped by readLoop
		msg1 := `{"jsonrpc":"2.0","id":"string-id","result":"skip"}` + "\n"
		_, _ = serverWrite.Write([]byte(msg1))

		// Empty line -- should be skipped
		_, _ = serverWrite.Write([]byte("\n"))

		// Valid numeric ID response
		resp := JSONRPCMessage{
			JSONRPC: JSONRPCVersion,
			ID:      float64(1),
			Result:  "ok",
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		_, _ = serverWrite.Write(data)
	}()

	// With the answer ordered after the request, the response cannot be missed,
	// so this bounds a handful of pipe writes rather than hiding a lost race.
	ctx, cancel := context.WithTimeout(context.Background(), stdioTestTimeout)
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

	// Same synchronization point as TestStdioTransport_ReadLoopNonNumericID:
	// the ID-1 response must not be written until the request proves Send has
	// registered its waiter, or readLoop discards it and Send hangs (#140).
	serverReqRead, clientWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer serverReqRead.Close()

	transport := newStdioTransportFromPipes(clientWrite, clientRead)
	defer transport.Close()

	go func() {
		_, _ = bufio.NewReader(serverReqRead).ReadString('\n')

		// Response for ID 999 -- nobody is waiting for this
		msg := `{"jsonrpc":"2.0","id":999,"result":"orphan"}` + "\n"
		_, _ = serverWrite.Write([]byte(msg))

		// Valid response for ID 1
		resp := JSONRPCMessage{
			JSONRPC: JSONRPCVersion,
			ID:      float64(1),
			Result:  "matched",
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		_, _ = serverWrite.Write(data)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), stdioTestTimeout)
	defer cancel()

	resp, err := transport.Send(ctx, "test", nil)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}
