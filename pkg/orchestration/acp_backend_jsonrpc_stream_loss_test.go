// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"errors"
	"io"
	"testing"
)

// =============================================================================
// A containerized JSON-RPC session's stream closing mid-prompt — go-quicbaas#1064
// =============================================================================
//
// The JSON-RPC transport reports a closed pipe as TransportError{ErrSessionClosed},
// which carries no evidence of WHY it closed. These pin that such a close is
// classified by the container's exit, exactly as the stream-json path is: a
// lost transport is refunded and requeued, a worker's non-zero exit is charged.

// jsonRPCContainerSession returns a containerized JSON-RPC session whose agent
// reads one request and then closes its stdout, the way a container does when
// its stream is cut mid-prompt. exit is what the runtime reports afterwards.
func jsonRPCContainerSession(t *testing.T, exit ContainerExit) (*acpSession, *bool) {
	t.Helper()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	go func() {
		scanner := bufio.NewScanner(stdinR)
		scanner.Scan() // the session/prompt request is in flight
		_ = stdoutW.Close()
		_, _ = io.Copy(io.Discard, stdinR)
	}()
	transport := newStdioTransportFromPipes(stdinW, stdoutR)
	t.Cleanup(func() {
		_ = transport.Close()
		_ = stdinR.Close()
	})
	cancelled := new(bool)
	sess := &acpSession{
		id:             "sess-1",
		agentID:        "agent-sess-1",
		transport:      transport,
		containerID:    "c0ffee",
		containerImage: "opencode",
		procCancel:     func() { *cancelled = true },
		containerExit: func(context.Context) ContainerExit {
			if *cancelled {
				t.Error("the exit was read after the container was torn down")
			}
			return exit
		},
	}
	return sess, cancelled
}

func promptJSONRPCStreamLoss(t *testing.T, sess *acpSession) error {
	t.Helper()
	_, err := (&ACPBackend{}).promptJSONRPC(t.Context(), sess, &PromptRequest{SessionID: sess.id, Prompt: "work"})
	if err == nil {
		t.Fatal("prompt succeeded on a stream that closed")
	}
	return err
}

// TestPromptJSONRPC_ContainerStreamLossIsRefundedAndRequeued: the stream closed
// and the runtime could not say the worker exited non-zero — transport loss.
func TestPromptJSONRPC_ContainerStreamLossIsRefundedAndRequeued(t *testing.T) {
	t.Parallel()
	sess, cancelled := jsonRPCContainerSession(t, ContainerExit{})

	err := promptJSONRPCStreamLoss(t, sess)
	var closed *ContainerStreamClosedError
	if !errors.As(err, &closed) {
		t.Fatalf("err = %T(%v), want *ContainerStreamClosedError", err, err)
	}
	if closed.ContainerID != "c0ffee" || closed.Image != "opencode" || closed.Operation != "prompt" {
		t.Errorf("closed = %+v, want the session's container, image and the prompt", closed)
	}
	if !*cancelled || !sess.closed.Load() {
		t.Error("the lost session was not torn down and marked closed")
	}

	task, class := concludeFailedJob(t, err)
	if class != FaultAgentInterrupted || task.ChargedAttempts() != 0 {
		t.Errorf("class %q charged %d, want %q and 0 — a lost JSON-RPC container stream was charged to the work",
			class, task.ChargedAttempts(), FaultAgentInterrupted)
	}
	if task.Status != TaskStatusPending {
		t.Errorf("status = %s, want requeued", task.Status)
	}
}

// TestPromptJSONRPC_ContainerWorkerCrashIsCharged: the stream closed because
// the worker itself exited non-zero. That is the work's failure, as today.
func TestPromptJSONRPC_ContainerWorkerCrashIsCharged(t *testing.T) {
	t.Parallel()
	sess, _ := jsonRPCContainerSession(t, ContainerExit{Known: true, Code: 1})

	err := promptJSONRPCStreamLoss(t, sess)
	var closed *ContainerStreamClosedError
	if !errors.As(err, &closed) || !closed.WorkerCrashed() {
		t.Fatalf("err = %T(%v), want a crashed *ContainerStreamClosedError", err, err)
	}

	task, class := concludeFailedJob(t, err)
	if class != "" || task.ChargedAttempts() != 1 {
		t.Errorf("class %q charged %d, want none and 1 — a worker crash was refunded", class, task.ChargedAttempts())
	}
}

// brokenPipe is a stdin whose reader has gone.
type brokenPipe struct{}

func (brokenPipe) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (brokenPipe) Close() error              { return nil }

// TestPromptJSONRPC_ContainerBrokenPipeIsStreamLoss: a write that finds the
// container's stdin already gone is the same closed stream.
func TestPromptJSONRPC_ContainerBrokenPipeIsStreamLoss(t *testing.T) {
	t.Parallel()
	stdoutR, stdoutW := io.Pipe()
	t.Cleanup(func() { _ = stdoutW.Close() })
	transport := newStdioTransportFromPipes(brokenPipe{}, stdoutR)
	t.Cleanup(func() { _ = transport.Close() })
	sess := &acpSession{id: "sess-1", transport: transport, containerID: "c0ffee", containerImage: "opencode"}

	err := promptJSONRPCStreamLoss(t, sess)
	if !isContainerTransportLoss(err) {
		t.Errorf("err = %T(%v), want container transport loss", err, err)
	}
}

// TestPromptJSONRPC_ClosedByBackendKeepsItsError: a session this backend
// closed itself is not a container whose stream was cut, and a host session
// has no container to read an exit from.
func TestPromptJSONRPC_ClosedByBackendKeepsItsError(t *testing.T) {
	t.Parallel()
	for name, containerID := range map[string]string{"closed container": "c0ffee", "host process": ""} {
		stdoutR, stdoutW := io.Pipe()
		t.Cleanup(func() { _ = stdoutW.Close() })
		transport := newStdioTransportFromPipes(brokenPipe{}, stdoutR)
		t.Cleanup(func() { _ = transport.Close() })
		sess := &acpSession{id: "sess-1", transport: transport, containerID: containerID}
		if containerID != "" {
			sess.closed.Store(true)
		}

		err := promptJSONRPCStreamLoss(t, sess)
		if errors.Is(err, ErrContainerStreamClosed) {
			t.Errorf("%s: err = %v, want the transport's own error", name, err)
		}
	}
}
