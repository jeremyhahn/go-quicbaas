// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package orchestration

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// =============================================================================
// inferDaemonServer — minimal gRPC AgentRuntime implementing only Infer
// =============================================================================

// inferDaemonServer is a minimal in-memory server for exercising Infer.
type inferDaemonServer struct {
	proto.UnimplementedAgentRuntimeServer

	frames          []*proto.InferResponse
	inferErr        error
	blockAfterFirst bool
}

func (s *inferDaemonServer) Infer(_ *proto.InferRequest, stream grpc.ServerStreamingServer[proto.InferResponse]) error {
	if s.inferErr != nil {
		return s.inferErr
	}
	for i, f := range s.frames {
		if err := stream.Send(f); err != nil {
			return err
		}
		if s.blockAfterFirst && i == 0 {
			<-stream.Context().Done()
			return stream.Context().Err()
		}
	}
	return nil
}

// startInferBufconnDaemon starts a gRPC bufconn server backed by an
// inferDaemonServer. Returns a dialer closure and a cleanup function.
func startInferBufconnDaemon(t *testing.T, svc *inferDaemonServer) func(ctx context.Context, target string) (*grpc.ClientConn, error) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	proto.RegisterAgentRuntimeServer(srv, svc)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop() })
	return func(ctx context.Context, target string) (*grpc.ClientConn, error) {
		return grpc.NewClient(
			"passthrough:///"+target,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
				return lis.Dial()
			}),
		)
	}
}

// newInferBackend constructs a RemoteACPBackend wired to the given dialer.
func newInferBackend(t *testing.T, dialer func(ctx context.Context, target string) (*grpc.ClientConn, error)) *RemoteACPBackend {
	t.Helper()
	reg := &AgentRegistration{
		ID:       "infer-agent",
		TenantID: "tenant-infer",
		Name:     "infer-daemon",
		Host:     "daemon.infer:9091",
		Backend:  "ollama",
		Kind:     AgentKindDaemon,
		Status:   AgentStatusRunning,
	}
	b, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: reg,
		Backend:      "ollama",
		ConnDialer:   dialer,
		Logger:       &agentTestLogger{},
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	return b
}

// =============================================================================
// RemoteACPBackend.Infer tests
// =============================================================================

// TestRemoteACPBackend_Infer_HappyPath verifies that Infer streams chunks and
// the final frame carries model and stop_reason.
func TestRemoteACPBackend_Infer_HappyPath(t *testing.T) {
	t.Parallel()

	svc := &inferDaemonServer{
		frames: []*proto.InferResponse{
			{
				Delta: []*proto.ContentBlock{{Type: "text", Text: "hello "}},
				Final: false,
			},
			{
				Delta:      []*proto.ContentBlock{{Type: "text", Text: "world"}},
				Model:      "qwen3:1.7b",
				StopReason: "end_turn",
				Final:      true,
			},
		},
	}
	dialer := startInferBufconnDaemon(t, svc)
	backend := newInferBackend(t, dialer)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := backend.Infer(ctx, &InferRequest{Backend: "ollama", Prompt: "hello"})
	if err != nil {
		t.Fatalf("Infer returned unexpected error: %v", err)
	}

	var chunks []*InferChunk
	for c := range ch {
		chunks = append(chunks, c)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if !chunks[1].Final {
		t.Error("expected final=true on last chunk")
	}
	if chunks[1].Model != "qwen3:1.7b" {
		t.Errorf("expected model 'qwen3:1.7b', got %q", chunks[1].Model)
	}
	if chunks[1].StopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %q", chunks[1].StopReason)
	}
	if len(chunks[0].Delta) == 0 || chunks[0].Delta[0].Text != "hello " {
		t.Errorf("unexpected first chunk delta: %+v", chunks[0].Delta)
	}
}

// TestRemoteACPBackend_Infer_NilRequest verifies that a nil request is rejected
// with InputValidationError before any dial is attempted.
func TestRemoteACPBackend_Infer_NilRequest(t *testing.T) {
	t.Parallel()

	svc := &inferDaemonServer{}
	dialer := startInferBufconnDaemon(t, svc)
	backend := newInferBackend(t, dialer)

	_, err := backend.Infer(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
	var valErr *InputValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("expected InputValidationError, got %T: %v", err, err)
	}
}

// TestRemoteACPBackend_Infer_EmptyPrompt verifies that an empty prompt returns
// an InputValidationError.
func TestRemoteACPBackend_Infer_EmptyPrompt(t *testing.T) {
	t.Parallel()

	svc := &inferDaemonServer{}
	dialer := startInferBufconnDaemon(t, svc)
	backend := newInferBackend(t, dialer)

	_, err := backend.Infer(context.Background(), &InferRequest{Backend: "ollama", Prompt: ""})
	if err == nil {
		t.Fatal("expected error for empty prompt, got nil")
	}
	var valErr *InputValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("expected InputValidationError, got %T: %v", err, err)
	}
}

// TestRemoteACPBackend_Infer_RPCError verifies that a daemon RPC error is
// surfaced as an InferBackendError (start failure) or causes the channel to
// close without a final chunk.
func TestRemoteACPBackend_Infer_RPCError(t *testing.T) {
	t.Parallel()

	svc := &inferDaemonServer{
		inferErr: status.Error(codes.Internal, "backend exploded"),
	}
	dialer := startInferBufconnDaemon(t, svc)
	backend := newInferBackend(t, dialer)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, startErr := backend.Infer(ctx, &InferRequest{Backend: "ollama", Prompt: "hello"})
	if startErr != nil {
		// RPC start itself failed — acceptable as InferBackendError.
		var ibErr *InferBackendError
		if !errors.As(startErr, &ibErr) {
			t.Errorf("expected InferBackendError, got %T: %v", startErr, startErr)
		}
		return
	}

	// Stream error path: channel closes with no final chunk.
	var got []*InferChunk
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 chunks after RPC error, got %d", len(got))
	}
}

// TestRemoteACPBackend_Infer_ContextCancel verifies that context cancellation
// terminates the goroutine cleanly without a panic or goroutine leak.
// inferHangTimeout is a deadlock detector, not a budget. It separates "this
// channel never closes" from "this machine is slow", which is the only
// distinction the assertions below still need, so it is set far above any
// plausible scheduling delay rather than tuned.
const inferHangTimeout = 30 * time.Second

// TestRemoteACPBackend_Infer_ContextCancel asserts that cancelling the caller's
// context closes the chunk channel instead of leaving the caller blocked.
//
// The version this replaces gave that context a 150ms timeout and let it do two
// jobs at once: it was both the deadline for gRPC to get the connection ready
// and the cancellation being tested. Under load the first job lost, and Infer
// returned before the stream ever started:
//
//	unexpected error starting Infer: op=Infer.Start: rpc error:
//	code = DeadlineExceeded desc = context deadline exceeded while
//	waiting for connections to become ready
//
// so the test failed without once exercising the behaviour it names. Seen in CI
// on PR #412, whose diff is 22 lines in an unrelated IAM test (go-quicbaas#423).
//
// The two jobs are now separate. Connection setup has no deadline at all, and
// cancellation is triggered explicitly after the first frame proves the stream
// is live -- the fake sends one frame and then blocks, so nothing but the
// cancel can end it.
func TestRemoteACPBackend_Infer_ContextCancel(t *testing.T) {
	t.Parallel()

	svc := &inferDaemonServer{
		frames: []*proto.InferResponse{
			{Delta: []*proto.ContentBlock{{Type: "text", Text: "partial"}}, Final: false},
		},
		blockAfterFirst: true,
	}
	dialer := startInferBufconnDaemon(t, svc)
	backend := newInferBackend(t, dialer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := backend.Infer(ctx, &InferRequest{Backend: "ollama", Prompt: "slow"})
	if err != nil {
		t.Fatalf("unexpected error starting Infer: %v", err)
	}

	select {
	case _, ok := <-ch:
		if !ok {
			t.Fatal("stream closed before delivering the first frame")
		}
	case <-time.After(inferHangTimeout):
		t.Fatal("the first frame never arrived, so the cancel below would not have tested anything")
	}

	cancel()

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range ch {
		}
	}()

	select {
	case <-drained:
	case <-time.After(inferHangTimeout):
		t.Fatal("the chunk channel did not close after the context was cancelled")
	}
}

// TestRemoteACPBackend_Infer_EmptyStream verifies that an empty server-side
// stream closes the channel without hanging.
func TestRemoteACPBackend_Infer_EmptyStream(t *testing.T) {
	t.Parallel()

	svc := &inferDaemonServer{frames: nil}
	dialer := startInferBufconnDaemon(t, svc)
	backend := newInferBackend(t, dialer)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := backend.Infer(ctx, &InferRequest{Backend: "ollama", Prompt: "empty"})
	if err != nil {
		t.Fatalf("unexpected error starting Infer: %v", err)
	}

	var got []*InferChunk
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 chunks from empty stream, got %d", len(got))
	}
}

// TestAccumulateChunks verifies that AccumulateChunks correctly aggregates a
// multi-chunk stream and returns an error for an empty stream.
func TestAccumulateChunks_HappyPath(t *testing.T) {
	t.Parallel()

	ch := make(chan *InferChunk, 2)
	ch <- &InferChunk{Delta: []ContentBlock{{Type: "text", Text: "hello "}}, Final: false}
	ch <- &InferChunk{Delta: []ContentBlock{{Type: "text", Text: "world"}}, Model: "m1", StopReason: "end_turn", Final: true}
	close(ch)

	resp, err := AccumulateChunks(ch)
	if err != nil {
		t.Fatalf("AccumulateChunks returned error: %v", err)
	}
	if len(resp.Content) != 2 {
		t.Errorf("expected 2 content blocks, got %d", len(resp.Content))
	}
	if resp.Model != "m1" {
		t.Errorf("expected model 'm1', got %q", resp.Model)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("expected stop_reason 'end_turn', got %q", resp.StopReason)
	}
}

func TestAccumulateChunks_EmptyChannel(t *testing.T) {
	t.Parallel()

	ch := make(chan *InferChunk)
	close(ch)

	_, err := AccumulateChunks(ch)
	if err == nil {
		t.Fatal("expected error for empty channel, got nil")
	}
	if !errors.Is(err, ErrInferNoChunks) {
		t.Errorf("expected ErrInferNoChunks, got %v", err)
	}
}
