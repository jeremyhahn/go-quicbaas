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

package grpc

import (
	"context"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	realgrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

const inferBufSize = 1 << 20 // 1 MiB

// inferChunkCh returns a closed channel pre-loaded with the provided chunks.
func inferChunkCh(chunks ...*orchestration.InferChunk) <-chan *orchestration.InferChunk {
	ch := make(chan *orchestration.InferChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch
}

// newInferBufconn starts an in-process gRPC OrchestratorService and returns a
// connected client plus a cleanup function.
func newInferBufconn(t *testing.T, agentSvc *grpcTestAgentService) (pb.OrchestratorServiceClient, func()) {
	t.Helper()
	lis := bufconn.Listen(inferBufSize)
	srv := realgrpc.NewServer()
	pb.RegisterOrchestratorServiceServer(srv, NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger()))
	go func() { _ = srv.Serve(lis) }()

	conn, err := realgrpc.NewClient("passthrough:///bufnet",
		realgrpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		realgrpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	return pb.NewOrchestratorServiceClient(conn), func() {
		_ = conn.Close()
		srv.Stop()
	}
}

// =============================================================================
// Unary Inference Tests
// =============================================================================

func TestOrchestratorGRPC_Inference_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{
		inferFunc: func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			return inferChunkCh(&orchestration.InferChunk{
				Delta:      []orchestration.ContentBlock{{Type: "text", Text: "hello"}},
				Model:      "qwen3:1.7b",
				StopReason: "end_turn",
				Final:      true,
			}), nil
		},
	}
	srv := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := srv.Inference(context.Background(), &pb.InferenceRequest{
		Backend: "ollama",
		Prompt:  "say hello",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "qwen3:1.7b", resp.Model)
	assert.Equal(t, "end_turn", resp.StopReason)
	require.Len(t, resp.Content, 1)
	assert.Equal(t, "hello", resp.Content[0].Text)
}

func TestOrchestratorGRPC_Inference_MissingBackend(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())

	resp, err := srv.Inference(context.Background(), &pb.InferenceRequest{Prompt: "say hello"})

	assert.Nil(t, resp)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestOrchestratorGRPC_Inference_MissingPrompt(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())

	resp, err := srv.Inference(context.Background(), &pb.InferenceRequest{Backend: "ollama"})

	assert.Nil(t, resp)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestOrchestratorGRPC_Inference_UnsupportedBackend(t *testing.T) {
	agentSvc := &grpcTestAgentService{
		inferFunc: func(_ context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			return nil, &orchestration.InferUnsupportedError{Backend: req.Backend}
		},
	}
	srv := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := srv.Inference(context.Background(), &pb.InferenceRequest{
		Backend: "unsupported-backend",
		Prompt:  "say hello",
	})

	assert.Nil(t, resp)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
}

func TestOrchestratorGRPC_Inference_MultipleChunks(t *testing.T) {
	agentSvc := &grpcTestAgentService{
		inferFunc: func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			return inferChunkCh(
				&orchestration.InferChunk{Delta: []orchestration.ContentBlock{{Type: "text", Text: "foo"}}, Final: false},
				&orchestration.InferChunk{Delta: []orchestration.ContentBlock{{Type: "text", Text: "bar"}}, Model: "m", StopReason: "end_turn", Final: true},
			), nil
		},
	}
	srv := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := srv.Inference(context.Background(), &pb.InferenceRequest{Backend: "ollama", Prompt: "test"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "m", resp.Model)
	assert.Equal(t, "end_turn", resp.StopReason)
}

// TestOrchestratorGRPC_Inference_NoTenantDefaultsToSystem verifies the
// tenant-fallback migration: when the request omits tenant_id, the handler
// resolves it to qrdbsdk.SystemTenantID (NOT the retired "default" tenant)
// and threads it into both the InferRequest and the request context.
func TestOrchestratorGRPC_Inference_NoTenantDefaultsToSystem(t *testing.T) {
	var gotReqTenant, gotCtxTenant string
	agentSvc := &grpcTestAgentService{
		inferFunc: func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			gotReqTenant = req.TenantID
			gotCtxTenant = orchestration.TenantFromContext(ctx)
			return inferChunkCh(&orchestration.InferChunk{
				Delta: []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
				Final: true,
			}), nil
		},
	}
	srv := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	// No TenantId set on the request → fallback path.
	_, err := srv.Inference(context.Background(), &pb.InferenceRequest{
		Backend: "ollama",
		Prompt:  "say hello",
	})
	require.NoError(t, err)
	assert.Equal(t, qrdbsdk.SystemTenantID, gotReqTenant,
		"omitted tenant_id must resolve to the system tenant in the request")
	assert.Equal(t, qrdbsdk.SystemTenantID, gotCtxTenant,
		"omitted tenant_id must resolve to the system tenant in the context")
}

// =============================================================================
// Streaming Inference Tests (via bufconn in-process gRPC)
// =============================================================================

func TestOrchestratorGRPC_InferenceStream_Success(t *testing.T) {
	agentSvc := &grpcTestAgentService{
		inferFunc: func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			return inferChunkCh(
				&orchestration.InferChunk{Delta: []orchestration.ContentBlock{{Type: "text", Text: "hello "}}, Final: false},
				&orchestration.InferChunk{Delta: []orchestration.ContentBlock{{Type: "text", Text: "world"}}, Model: "qwen", StopReason: "end_turn", Final: true},
			), nil
		},
	}
	client, cleanup := newInferBufconn(t, agentSvc)
	defer cleanup()

	stream, err := client.InferenceStream(context.Background(), &pb.InferenceRequest{
		Backend: "ollama",
		Prompt:  "say hello world",
	})
	require.NoError(t, err)

	var chunks []*pb.InferenceChunk
	for {
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		require.NoError(t, recvErr)
		chunks = append(chunks, chunk)
	}

	require.Len(t, chunks, 2)
	assert.Equal(t, "hello ", chunks[0].Delta[0].Text)
	assert.False(t, chunks[0].Final)
	assert.Equal(t, "world", chunks[1].Delta[0].Text)
	assert.True(t, chunks[1].Final)
	assert.Equal(t, "qwen", chunks[1].Model)
}

func TestOrchestratorGRPC_InferenceStream_MissingBackend(t *testing.T) {
	client, cleanup := newInferBufconn(t, &grpcTestAgentService{})
	defer cleanup()

	stream, err := client.InferenceStream(context.Background(), &pb.InferenceRequest{
		Prompt: "hello",
	})
	require.NoError(t, err)

	_, recvErr := stream.Recv()
	require.Error(t, recvErr)
	st, ok := status.FromError(recvErr)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestOrchestratorGRPC_InferenceStream_MissingPrompt(t *testing.T) {
	client, cleanup := newInferBufconn(t, &grpcTestAgentService{})
	defer cleanup()

	stream, err := client.InferenceStream(context.Background(), &pb.InferenceRequest{
		Backend: "ollama",
	})
	require.NoError(t, err)

	_, recvErr := stream.Recv()
	require.Error(t, recvErr)
	st, ok := status.FromError(recvErr)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// =============================================================================
// GpuRequired propagation tests
// =============================================================================

// TestGRPCInference_PassesGpuRequiredToOrchestrator verifies that the gRPC
// Inference handler propagates GpuRequired from the proto request into
// InferRequest.GpuRequired and decorates the context via WithGpuRequired.
func TestGRPCInference_PassesGpuRequiredToOrchestrator(t *testing.T) {
	var capturedReq *orchestration.InferRequest
	var capturedCtxGpu bool

	agentSvc := &grpcTestAgentService{
		inferFunc: func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			capturedReq = req
			capturedCtxGpu = orchestration.GpuRequiredFromContext(ctx)
			return inferChunkCh(&orchestration.InferChunk{
				Delta:      []orchestration.ContentBlock{{Type: "text", Text: "pong"}},
				Model:      "qwen3:1.7b",
				StopReason: "end_turn",
				Final:      true,
			}), nil
		},
	}
	srv := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := srv.Inference(context.Background(), &pb.InferenceRequest{
		Backend:     "ollama",
		Prompt:      "ping",
		GpuRequired: true,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, capturedReq)
	assert.True(t, capturedReq.GpuRequired, "InferRequest.GpuRequired must be true")
	assert.True(t, capturedCtxGpu, "context must carry GPU requirement flag")
}

// TestGRPCInference_GpuRequiredFalseByDefault verifies that omitting
// GpuRequired from the proto request leaves InferRequest.GpuRequired false
// and does not decorate the context.
func TestGRPCInference_GpuRequiredFalseByDefault(t *testing.T) {
	var capturedReq *orchestration.InferRequest
	var capturedCtxGpu bool

	agentSvc := &grpcTestAgentService{
		inferFunc: func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			capturedReq = req
			capturedCtxGpu = orchestration.GpuRequiredFromContext(ctx)
			return inferChunkCh(&orchestration.InferChunk{
				Delta: []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
				Final: true,
			}), nil
		},
	}
	srv := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	resp, err := srv.Inference(context.Background(), &pb.InferenceRequest{
		Backend: "ollama",
		Prompt:  "hello",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, capturedReq)
	assert.False(t, capturedReq.GpuRequired, "InferRequest.GpuRequired must be false by default")
	assert.False(t, capturedCtxGpu, "context must not carry GPU flag when not requested")
}

// =============================================================================
// convertInferError Tests
// =============================================================================

func TestConvertInferError_Nil(t *testing.T) {
	assert.Nil(t, convertInferError(nil))
}

func TestConvertInferError_Unsupported(t *testing.T) {
	err := convertInferError(&orchestration.InferUnsupportedError{Backend: "bogus"})
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
}

func TestConvertInferError_BackendError(t *testing.T) {
	err := convertInferError(&orchestration.InferBackendError{Op: "ollama.generate", Err: assert.AnError})
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}
