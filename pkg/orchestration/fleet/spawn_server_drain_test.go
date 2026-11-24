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

package fleet

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Draining before closing — go-quicbaas#1064
// =============================================================================
//
// Stopping quicbaas-agent during a deploy closed a live worker's stream
// mid-prompt, and the turn came back Internal: indistinguishable from a worker
// that crashed, and charged as one. These pin the agent's half of the fix: it
// drains first, and a turn it cuts off says so.

// streamClosed is what the ACP backend returns when a containerized worker's
// stdio ends mid-turn and the runtime reports no exit of the worker's own —
// the container was killed or its pipe cut.
func streamClosed() error {
	return &orchestration.ContainerStreamClosedError{ContainerID: "c0ffee", Image: "claude-code", Operation: "prompt"}
}

// workerExited is what the ACP backend returns when a containerized worker's
// stdio ends because the process inside it exited non-zero.
func workerExited() error {
	return &orchestration.ContainerStreamClosedError{
		ContainerID: "c0ffee", Image: "claude-code", Operation: "prompt",
		Exit: orchestration.ContainerExit{Known: true, Code: 1},
	}
}

// promptFailure runs one turn that fails with promptErr and returns the status
// code the client received.
func promptFailure(t *testing.T, promptErr error, draining bool) (codes.Code, error) {
	t.Helper()
	backend := &spawnBackend{
		promptFn: func(context.Context, *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
			return nil, promptErr
		},
	}
	srv, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	t.Cleanup(cleanup)
	srv.draining.Store(draining)

	stream, err := client.Prompt(context.Background(), drainPromptRequest())
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	_, recvErr := stream.Recv()
	return status.Code(recvErr), recvErr
}

// drainPromptRequest is a turn the test identity is allowed to send.
func drainPromptRequest() *proto.PromptSessionRequest {
	return &proto.PromptSessionRequest{Backend: "acp", TenantId: "tenant-a", SessionId: "sess-1", Prompt: "go"}
}

// TestGRPCSpawnServer_DrainWaitsForAnInFlightPrompt: Stop does not return, and
// the turn is not cut, while a prompt is still running inside its bound. When
// the turn finishes it is delivered whole.
func TestGRPCSpawnServer_DrainWaitsForAnInFlightPrompt(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	backend := &spawnBackend{
		promptFn: func(_ context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
			close(entered)
			<-release
			return &orchestration.PromptResponse{
				SessionID:  req.SessionID,
				Content:    []orchestration.ContentBlock{{Type: "text", Text: "finished"}},
				StopReason: "end_turn",
			}, nil
		},
	}
	srv, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), drainPromptRequest())
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	<-entered

	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Minute)
	defer cancelStop()
	stopped := make(chan error, 1)
	go func() { stopped <- srv.Stop(stopCtx) }()

	// THE DRAIN HAS BEGUN WHILE THE TURN IS STILL HELD. Nothing below releases
	// the prompt until Stop has provably started, so this cannot pass on a
	// prompt that finished before shutdown began.
	<-srv.DrainStarted()

	// A new spawn is refused while the drain waits on the held turn.
	_, spawnErr := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend: "acp", TenantId: "tenant-a", WorkDir: "/work",
	})
	if code := status.Code(spawnErr); code != codes.Unavailable {
		t.Fatalf("spawn during drain: code = %s (%v), want Unavailable", code, spawnErr)
	}
	if calls := backend.spawnCalls.Load(); calls != 0 {
		t.Fatalf("backend spawned %d sessions during the drain", calls)
	}

	// Stop has not returned: GracefulStop cannot complete while the handler
	// is blocked on release, which is still open.
	select {
	case err := <-stopped:
		t.Fatalf("Stop returned (%v) with a prompt still in flight inside its bound", err)
	default:
	}
	close(release)

	frame, err := stream.Recv()
	if err != nil {
		t.Fatalf("the in-flight turn was cut off by the drain: %v", err)
	}
	if got := frame.GetContent(); len(got) != 1 || got[0].GetText() != "finished" {
		t.Fatalf("frame content = %v, want the finished turn", got)
	}
	if err := <-stopped; err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !srv.Draining() {
		t.Error("a stopped server must report that it drained")
	}
}

// TestGRPCSpawnServer_DrainIsBounded: a turn that outlives the drain's bound is
// cut off, and it is cut off as the agent's interruption — never as Internal,
// which is what a crashed worker reports and what the control plane charges.
func TestGRPCSpawnServer_DrainIsBounded(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	backend := &spawnBackend{
		promptFn: func(ctx context.Context, _ *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
			close(entered)
			<-ctx.Done()
			return nil, streamClosed()
		},
	}
	srv, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), drainPromptRequest())
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	<-entered

	// The bound has already run out: Stop must force the close rather than
	// wait for a turn that will never finish on its own.
	stopCtx, cancelStop := context.WithCancel(context.Background())
	cancelStop()
	if err := srv.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	_, recvErr := stream.Recv()
	if recvErr == nil {
		t.Fatal("a turn cut off by the drain was reported as delivered")
	}
	code := status.Code(recvErr)
	if code != codes.Unavailable && code != codes.Canceled {
		t.Fatalf("code = %s (%v), want Unavailable or Canceled — the control plane charges anything else", code, recvErr)
	}
}

// TestGRPCSpawnServer_ATurnFailedDuringDrainIsUnavailable: the burn-in case
// exactly. The shutdown closes the worker's stream while the server is still
// answering, and the turn must come back as the agent's interruption — even
// when the shutdown's signal gave the worker an exit status.
func TestGRPCSpawnServer_ATurnFailedDuringDrainIsUnavailable(t *testing.T) {
	t.Parallel()

	for name, promptErr := range map[string]error{"stream closed": streamClosed(), "worker exited": workerExited()} {
		if code, err := promptFailure(t, promptErr, true); code != codes.Unavailable {
			t.Errorf("%s: code = %s (%v), want Unavailable", name, code, err)
		}
	}
}

// TestGRPCSpawnServer_AContainerStreamLostOutsideADrainIsUnavailable: a
// worker's container stream that closed with no exit of the worker's own is
// transport loss even while this agent is healthy, and must not be reported
// as the crash the control plane charges.
func TestGRPCSpawnServer_AContainerStreamLostOutsideADrainIsUnavailable(t *testing.T) {
	t.Parallel()

	lost := map[string]error{
		"no exit reported": streamClosed(),
		"runtime failure": &orchestration.ContainerStreamClosedError{
			ContainerID: "c0ffee", Image: "claude-code", Operation: "prompt",
			Exit: orchestration.ContainerExit{Known: true, Code: 125},
		},
	}
	for name, promptErr := range lost {
		if code, err := promptFailure(t, promptErr, false); code != codes.Unavailable {
			t.Errorf("%s: code = %s (%v), want Unavailable", name, code, err)
		}
	}
}

// TestGRPCSpawnServer_AWorkerCrashOutsideADrainIsInternal is the direction that
// must not regress: a worker that exited non-zero under a healthy agent is the
// worker's, and stays Internal so the control plane still charges it.
func TestGRPCSpawnServer_AWorkerCrashOutsideADrainIsInternal(t *testing.T) {
	t.Parallel()

	code, err := promptFailure(t, workerExited(), false)
	if code != codes.Internal {
		t.Fatalf("code = %s (%v), want Internal — a crashed worker is not the agent's interruption", code, err)
	}
	if !strings.Contains(err.Error(), "exit code 1") {
		t.Errorf("error %q does not carry the worker's exit", err)
	}
}

// TestGRPCSpawnServer_ADrainingAgentRefusesNewSpawns: nothing new starts on an
// agent that is going away, and the refusal is Unavailable, which the control
// plane reads as an agent that did not take the spawn and refunds.
func TestGRPCSpawnServer_ADrainingAgentRefusesNewSpawns(t *testing.T) {
	t.Parallel()

	backend := &spawnBackend{}
	srv, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()
	srv.draining.Store(true)

	_, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend: "acp", TenantId: "tenant-a", WorkDir: "/work",
	})
	if code := status.Code(err); code != codes.Unavailable {
		t.Fatalf("code = %s (%v), want Unavailable", code, err)
	}
	if calls := backend.spawnCalls.Load(); calls != 0 {
		t.Fatalf("backend spawned %d sessions on a draining agent", calls)
	}
}
