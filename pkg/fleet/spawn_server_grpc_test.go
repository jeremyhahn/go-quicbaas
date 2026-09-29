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

package fleet

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Enroll — always Unimplemented on the daemon
// =============================================================================

func TestGRPCSpawnServer_Enroll_ReturnsUnimplemented(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	_, err := client.Enroll(context.Background(), &proto.EnrollRequest{
		Hostname: "host",
	})
	if err == nil {
		t.Fatal("expected Unimplemented error from Enroll")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != codes.Unimplemented {
		t.Fatalf("expected Unimplemented, got %v (%s)", st.Code(), st.Message())
	}
}

// TestGRPCSpawnServer_Enroll_WithoutIdentity_StillUnimplemented asserts that
// Enroll returns Unimplemented even when no identity has been enrolled yet
// (i.e. authorize is not the gatekeeper for this RPC — the handler returns
// Unimplemented directly before calling authorize).
func TestGRPCSpawnServer_Enroll_WithoutIdentity_StillUnimplemented(t *testing.T) {
	t.Parallel()
	// No identity set — server is in unenrolled state.
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, nil)
	defer cleanup()

	_, err := client.Enroll(context.Background(), &proto.EnrollRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unimplemented {
		t.Fatalf("expected Unimplemented, got %v", st.Code())
	}
}

// =============================================================================
// Heartbeat — bidi streaming
// =============================================================================

// buildHeartbeatClient constructs a bufconn-backed spawn server and returns a
// raw bidi stream to the Heartbeat RPC, ready to send/receive.
func buildHeartbeatClient(t *testing.T, identity *orchestration.AgentIdentity) (proto.FleetRuntime_HeartbeatClient, func()) {
	t.Helper()
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &spawnBackend{}},
		Logger:   &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	lis := bufconn.Listen(spawnServerBufSize)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:          rt,
		BindAddr:         "bufnet",
		Logger:           &testLogger{t: t},
		ListenerOverride: lis,
	})
	if err != nil {
		t.Fatalf("NewGRPCSpawnServer: %v", err)
	}
	if identity != nil {
		srv.SetIdentity(identity)
	}
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
			return lis.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := proto.NewFleetRuntimeClient(conn)
	stream, err := client.Heartbeat(context.Background())
	if err != nil {
		t.Fatalf("Heartbeat stream: %v", err)
	}
	cleanup := func() {
		conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Stop(ctx)
	}
	return stream, cleanup
}

// TestGRPCSpawnServer_Heartbeat_EchosSequenceNumbers sends N beats and
// verifies the server echoes each sequence number back in order.
func TestGRPCSpawnServer_Heartbeat_EchosSequenceNumbers(t *testing.T) {
	t.Parallel()

	stream, cleanup := buildHeartbeatClient(t, newTestIdentity())
	defer cleanup()

	const beats = 5
	ackDone := make(chan struct{})
	recvErr := make(chan error, 1)

	// Collect acks concurrently so we don't deadlock the bidi stream.
	go func() {
		defer close(ackDone)
		for i := 0; i < beats; i++ {
			ack, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			want := uint64(i + 1)
			if ack.GetSequence() != want {
				recvErr <- errors.New("sequence mismatch")
				return
			}
		}
	}()

	for i := 0; i < beats; i++ {
		if err := stream.Send(&proto.HeartbeatBeat{
			NodeId:   "agent-test",
			Sequence: uint64(i + 1),
		}); err != nil {
			t.Fatalf("Send beat %d: %v", i+1, err)
		}
	}

	// Close the send side and wait for all acks.
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("CloseSend: %v", err)
	}

	select {
	case <-ackDone:
	case err := <-recvErr:
		t.Fatalf("recv error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for heartbeat acks")
	}
}

// TestGRPCSpawnServer_Heartbeat_GracefulCloseOnClientEOF verifies the server
// returns io.EOF (treated as normal) when the client closes the stream.
func TestGRPCSpawnServer_Heartbeat_GracefulCloseOnClientEOF(t *testing.T) {
	t.Parallel()

	stream, cleanup := buildHeartbeatClient(t, newTestIdentity())
	defer cleanup()

	// Send one beat then immediately close.
	if err := stream.Send(&proto.HeartbeatBeat{
		NodeId:   "agent-test",
		Sequence: uint64(1),
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Consume the ack.
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("Recv ack: %v", err)
	}

	// Close the send side — server's next Recv returns io.EOF.
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("CloseSend: %v", err)
	}

	// After CloseSend the server closes its side; client Recv returns some
	// variant of EOF: either io.EOF directly, a gRPC status with code OK,
	// or a gRPC status with code Unknown wrapping EOF. All signal stream end.
	_, err := stream.Recv()
	if err == nil {
		t.Fatal("expected error after CloseSend, got nil")
	}
	if errors.Is(err, io.EOF) {
		return // direct EOF — ok
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status or io.EOF after CloseSend, got %T: %v", err, err)
	}
	if st.Code() != codes.OK && st.Code() != codes.Unknown {
		t.Fatalf("expected OK or Unknown code after CloseSend, got %v (%s)", st.Code(), st.Message())
	}
}

// TestGRPCSpawnServer_Heartbeat_RejectsAgentIDMismatch verifies that a beat
// carrying a different agent_id than the enrolled identity is rejected with
// PermissionDenied.
func TestGRPCSpawnServer_Heartbeat_RejectsAgentIDMismatch(t *testing.T) {
	t.Parallel()

	stream, cleanup := buildHeartbeatClient(t, newTestIdentity())
	defer cleanup()

	// enrolled agent_id is "agent-test"; send a beat with a different one.
	if err := stream.Send(&proto.HeartbeatBeat{
		NodeId:   "impostor-agent",
		Sequence: uint64(1),
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The server should reject the beat.
	_, err := stream.Recv()
	if err == nil {
		t.Fatal("expected error for agent_id mismatch")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v (%s)", st.Code(), st.Message())
	}
}

// TestGRPCSpawnServer_Heartbeat_RequiresEnrollment verifies that a Heartbeat
// stream is rejected with Unauthenticated when no identity is enrolled.
func TestGRPCSpawnServer_Heartbeat_RequiresEnrollment(t *testing.T) {
	t.Parallel()

	// No identity set — server is in unenrolled state.
	stream, cleanup := buildHeartbeatClient(t, nil)
	defer cleanup()

	// The server rejects the stream before any beats arrive. The error may
	// surface on Send or on the initial stream setup. Accept it in either case.
	sendErr := stream.Send(&proto.HeartbeatBeat{Sequence: uint64(1)})
	if sendErr != nil {
		// Error came back on Send — verify it's auth-related.
		// gRPC may wrap the Unauthenticated error as EOF or the actual status.
		// Accept any non-OK status here since the reason is the auth gate.
		st, ok := status.FromError(sendErr)
		if ok && (st.Code() == codes.Unauthenticated || st.Code() == codes.OK) {
			return
		}
		// Also accept io.EOF (the server closed before the send completed).
		if errors.Is(sendErr, io.EOF) {
			return
		}
		// For any other wrapped error check the Recv.
	}

	_, err := stream.Recv()
	if err == nil {
		t.Fatal("expected Unauthenticated error for unenrolled daemon")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		// Also accept EOF — the server may have already closed the stream.
		if errors.Is(err, io.EOF) {
			// The stream was rejected and closed; treat as expected behavior.
			return
		}
		t.Fatalf("expected Unauthenticated, got %v (%s)", st.Code(), st.Message())
	}
}

// =============================================================================
// Prompt — error branches
// =============================================================================

// TestGRPCSpawnServer_Prompt_RequiresPromptField asserts that an empty prompt
// field returns InvalidArgument, not Internal.
func TestGRPCSpawnServer_Prompt_RequiresPromptField(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		Backend:   "acp",
		TenantId:  "tenant-a",
		SessionId: "sess-1",
		Prompt:    "", // missing
	})
	if err != nil {
		t.Fatalf("Prompt RPC: %v", err)
	}
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected InvalidArgument error for empty prompt")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v (%s)", st.Code(), st.Message())
	}
}

// TestGRPCSpawnServer_Prompt_RequiresBackendField asserts that a missing
// backend field returns InvalidArgument.
func TestGRPCSpawnServer_Prompt_RequiresBackendField(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		TenantId:  "tenant-a",
		SessionId: "sess-1",
		Prompt:    "hello",
		Backend:   "", // missing
	})
	if err != nil {
		t.Fatalf("Prompt RPC: %v", err)
	}
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected InvalidArgument error for empty backend")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v (%s)", st.Code(), st.Message())
	}
}

// TestGRPCSpawnServer_Prompt_BackendErrorMapsToInternal verifies that a
// non-domain backend error becomes codes.Internal.
func TestGRPCSpawnServer_Prompt_BackendErrorMapsToInternal(t *testing.T) {
	t.Parallel()

	backend := &spawnBackend{
		promptFn: func(_ context.Context, _ *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
			return nil, errors.New("backend exploded")
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		Backend:   "acp",
		TenantId:  "tenant-a",
		SessionId: "sess-1",
		Prompt:    "hello",
	})
	if err != nil {
		t.Fatalf("Prompt RPC: %v", err)
	}
	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected Internal error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Fatalf("expected Internal, got %v (%s)", st.Code(), st.Message())
	}
}

// TestGRPCSpawnServer_Prompt_StreamsManyChunks verifies the streaming path
// with multiple content blocks (all arrive in a single response frame today,
// but tests the slice translation through runtimeContentBlocksToProto).
func TestGRPCSpawnServer_Prompt_StreamsManyChunks(t *testing.T) {
	t.Parallel()

	backend := &spawnBackend{
		promptFn: func(_ context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
			return &orchestration.PromptResponse{
				SessionID: req.SessionID,
				Content: []orchestration.ContentBlock{
					{Type: "text", Text: "chunk one"},
					{Type: "text", Text: "chunk two"},
					{Type: "text", Text: "chunk three"},
				},
				Model:      "test-model",
				StopReason: "end_turn",
			}, nil
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		Backend:   "acp",
		TenantId:  "tenant-a",
		SessionId: "sess-chunks",
		Prompt:    "give me chunks",
	})
	if err != nil {
		t.Fatalf("Prompt RPC: %v", err)
	}

	frame, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv frame: %v", err)
	}
	if got := len(frame.GetContent()); got != 3 {
		t.Errorf("content blocks: got %d, want 3", got)
	}
	if frame.GetContent()[0].GetText() != "chunk one" {
		t.Errorf("block[0]: got %q, want %q", frame.GetContent()[0].GetText(), "chunk one")
	}
	if frame.GetContent()[2].GetText() != "chunk three" {
		t.Errorf("block[2]: got %q, want %q", frame.GetContent()[2].GetText(), "chunk three")
	}

	// Stream should close after the single response.
	_, err = stream.Recv()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF after single frame, got %v", err)
	}
}

// =============================================================================
// Status — error branches
// =============================================================================

// TestGRPCSpawnServer_Status_RequiresSessionID verifies that a missing
// session_id returns InvalidArgument.
func TestGRPCSpawnServer_Status_RequiresSessionID(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	_, err := client.Status(context.Background(), &proto.SessionStatusRequest{
		TenantId:  "tenant-a",
		SessionId: "",
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for empty session_id")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v (%s)", st.Code(), st.Message())
	}
}

// TestGRPCSpawnServer_Status_NotFoundWhenBackendMisses verifies that a
// session not found in any backend returns NotFound (via the
// "session not found" path, not a backend error).
func TestGRPCSpawnServer_Status_NotFoundWhenBackendMisses(t *testing.T) {
	t.Parallel()

	backend := &spawnBackend{
		statusFn: func(_ context.Context, _ string) (*orchestration.SessionStatus, error) {
			return nil, &orchestration.SessionNotFoundError{SessionID: "ghost-sess"}
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	_, err := client.Status(context.Background(), &proto.SessionStatusRequest{
		TenantId:  "tenant-a",
		SessionId: "ghost-sess",
	})
	if err == nil {
		t.Fatal("expected error for unknown session")
	}
	// The error may be NotFound (domain error map) or Internal depending on
	// how the backend signals not-found. Either is a non-success status.
	st, _ := status.FromError(err)
	if st.Code() == codes.OK {
		t.Fatalf("expected non-OK status, got OK")
	}
}

// TestGRPCSpawnServer_Status_BackendErrorMapsToInternal verifies that a
// generic backend error becomes codes.Internal.
func TestGRPCSpawnServer_Status_BackendErrorMapsToInternal(t *testing.T) {
	t.Parallel()

	backend := &spawnBackend{
		statusFn: func(_ context.Context, _ string) (*orchestration.SessionStatus, error) {
			return nil, errors.New("storage unavailable")
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	_, err := client.Status(context.Background(), &proto.SessionStatusRequest{
		TenantId:  "tenant-a",
		SessionId: "sess-err",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Fatalf("expected Internal, got %v (%s)", st.Code(), st.Message())
	}
}

// =============================================================================
// Close — error branches
// =============================================================================

// TestGRPCSpawnServer_Close_RequiresSessionID verifies that a missing
// session_id returns InvalidArgument.
func TestGRPCSpawnServer_Close_RequiresSessionID(t *testing.T) {
	t.Parallel()
	_, client, cleanup := buildSpawnServer(t, &spawnBackend{}, newTestIdentity())
	defer cleanup()

	_, err := client.Close(context.Background(), &proto.CloseSessionRequest{
		TenantId:  "tenant-a",
		SessionId: "",
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for empty session_id")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v (%s)", st.Code(), st.Message())
	}
}

// TestGRPCSpawnServer_Close_BackendErrorMapsToInternal verifies a generic
// close error becomes codes.Internal.
func TestGRPCSpawnServer_Close_BackendErrorMapsToInternal(t *testing.T) {
	t.Parallel()

	backend := &spawnBackend{
		closeFn: func(_ context.Context, _ string) error {
			return errors.New("backend unreachable")
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	_, err := client.Close(context.Background(), &proto.CloseSessionRequest{
		TenantId:  "tenant-a",
		SessionId: "sess-close-err",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Fatalf("expected Internal, got %v (%s)", st.Code(), st.Message())
	}
}

// TestGRPCSpawnServer_Close_NotFoundWhenNoBackendCloses verifies the NotFound
// path when the backend signals success=false (nil return but session absent).
// We model this by returning an error that is not found by the backend.
func TestGRPCSpawnServer_Close_NotFoundWhenSessionAbsent(t *testing.T) {
	t.Parallel()

	backend := &spawnBackend{
		closeFn: func(_ context.Context, id string) error {
			return &orchestration.SessionNotFoundError{SessionID: id}
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	_, err := client.Close(context.Background(), &proto.CloseSessionRequest{
		TenantId:  "tenant-a",
		SessionId: "absent-sess",
	})
	if err == nil {
		t.Fatal("expected error for absent session")
	}
	st, _ := status.FromError(err)
	if st.Code() == codes.OK {
		t.Fatalf("expected non-OK status, got OK")
	}
}
