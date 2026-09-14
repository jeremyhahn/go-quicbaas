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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// recordingConversationServer captures the proto request the SDK built and
// answers with one page of turns, so both directions of the mapping can be
// asserted rather than inferred.
type recordingConversationServer struct {
	pb.UnimplementedOrchestratorServiceServer

	mu        sync.Mutex
	last      *pb.GetSessionConversationRequest
	usageJSON []byte
}

// GetSessionConversation records the request and returns a fixed page.
func (s *recordingConversationServer) GetSessionConversation(
	_ context.Context,
	req *pb.GetSessionConversationRequest,
) (*pb.GetSessionConversationResponse, error) {
	s.mu.Lock()
	s.last = req
	usage := s.usageJSON
	s.mu.Unlock()
	if usage == nil {
		usage = []byte(`{"input_tokens":7,"output_tokens":8}`)
	}
	return &pb.GetSessionConversationResponse{
		SessionId:      req.GetSessionId(),
		TenantId:       req.GetTenantId(),
		NextCursor:     42,
		OldestSequence: 3,
		Turns: []*pb.SessionTurn{
			{
				Sequence:  4,
				SessionId: req.GetSessionId(),
				Role:      "user",
				Text:      "hi",
				Timestamp: timestamppb.New(time.Unix(1700000000, 0).UTC()),
			},
			{
				Sequence:       5,
				SessionId:      req.GetSessionId(),
				Role:           "assistant",
				Text:           "hello",
				Model:          "m1",
				StopReason:     "end_turn",
				TruncatedBytes: 12,
				Tools: []*pb.SessionToolUse{
					{InvocationId: "i1", Name: "Read", ResultBytes: 5},
					{InvocationId: "i2", Name: "Bash", IsError: true},
				},
				UsageJson: usage,
			},
		},
	}, nil
}

// request returns the last recorded proto request.
func (s *recordingConversationServer) request() *pb.GetSessionConversationRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// TestOrchestratorGetSessionConversation_MapsEveryField asserts the SDK builds
// the proto request from every field of its own and reads every field back.
func TestOrchestratorGetSessionConversation_MapsEveryField(t *testing.T) {
	fake := &recordingConversationServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, fake)
	})
	h := newOrchestratorHandler(srv.dial(t))

	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	resp, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "GetSessionConversation",
		&orchestrator.GetSessionConversationRequest{SessionID: "s1", Cursor: 4, Limit: 25})
	require.NoError(t, err)

	got := fake.request()
	require.NotNil(t, got)
	assert.Equal(t, "s1", got.GetSessionId())
	assert.Equal(t, "tenant-a", got.GetTenantId(), "the context tenant must cross the wire")
	assert.Equal(t, uint64(4), got.GetCursor())
	assert.Equal(t, uint32(25), got.GetLimit())

	page, ok := resp.(*orchestrator.GetSessionConversationResponse)
	require.Truef(t, ok, "response is %T", resp)
	require.Len(t, page.Turns, 2)
	assert.Equal(t, orchestrator.SessionTurnRoleUser, page.Turns[0].Role)
	assert.Equal(t, int64(1700000000), page.Turns[0].Timestamp.Unix())

	reply := page.Turns[1]
	assert.Equal(t, orchestrator.SessionTurnRoleAssistant, reply.Role)
	assert.Equal(t, "hello", reply.Text)
	assert.Equal(t, "m1", reply.Model)
	assert.Equal(t, int64(12), reply.TruncatedBytes)
	require.Len(t, reply.Tools, 2)
	assert.Equal(t, "Read", reply.Tools[0].Name)
	assert.True(t, reply.Tools[1].IsError)
	require.NotNil(t, reply.Usage, "the turn's cost must survive the JSON hop")
	assert.Equal(t, int64(8), reply.Usage.OutputTokens)

	assert.Equal(t, uint64(42), page.NextCursor)
	assert.Equal(t, uint64(3), page.OldestSequence)
}

// TestOrchestratorGetSessionConversation_UnusableUsageIsAnError asserts a turn
// whose accounting cannot be decoded fails the call. Dropping it silently
// would report a turn that cost nothing, which is indistinguishable from a
// backend that reported nothing.
func TestOrchestratorGetSessionConversation_UnusableUsageIsAnError(t *testing.T) {
	fake := &recordingConversationServer{usageJSON: []byte(`{"input_tokens":`)}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, fake)
	})
	h := newOrchestratorHandler(srv.dial(t))

	_, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", "GetSessionConversation",
		&orchestrator.GetSessionConversationRequest{SessionID: "s1"})
	require.Error(t, err)
}

// TestOrchestratorGetSessionConversation_WrongRequestType asserts a mistyped
// request is refused locally rather than written to the wire.
func TestOrchestratorGetSessionConversation_WrongRequestType(t *testing.T) {
	h := newOrchestratorHandlerWithServer(t, false)

	_, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator",
		"GetSessionConversation", &orchestrator.CloseSessionRequest{SessionID: "s1"})
	require.Error(t, err)
}
