// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.
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
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// recordingTenantSpendServer captures the tenant spend-policy requests and
// answers with one monthly window.
type recordingTenantSpendServer struct {
	pb.UnimplementedOrchestratorServiceServer

	mu      sync.Mutex
	lastGet *pb.GetTenantSpendPolicyRequest
	lastSet *pb.SetTenantSpendPolicyRequest
}

func (s *recordingTenantSpendServer) response(tenantID string) *pb.TenantSpendPolicyResponse {
	return &pb.TenantSpendPolicyResponse{
		TenantId: tenantID,
		Limits: &pb.SpendLimits{Windows: []*pb.SpendLimitWindow{
			{Name: "monthly", SizeNanos: int64(720 * time.Hour), MaxEquivalentMicros: 2_000_000_000},
		}},
		UpdatedAt: timestamppb.New(time.Unix(1700000000, 0)),
		Windows:   []*pb.SpendWindowStatus{{Name: "monthly", UsedEquivalentMicros: 1_500_000}},
	}
}

func (s *recordingTenantSpendServer) GetTenantSpendPolicy(_ context.Context, req *pb.GetTenantSpendPolicyRequest) (*pb.TenantSpendPolicyResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastGet = req
	return s.response(req.GetTenantId()), nil
}

func (s *recordingTenantSpendServer) SetTenantSpendPolicy(_ context.Context, req *pb.SetTenantSpendPolicyRequest) (*pb.TenantSpendPolicyResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSet = req
	return s.response(req.GetTenantId()), nil
}

// TestOrchestratorTenantSpendPolicy_OverGRPC is go-quicbaas#1058 on the SDK's
// gRPC (and so Unix) transport: both verbs are dispatched, the context tenant
// crosses the wire, and the limits and window usage come back whole.
func TestOrchestratorTenantSpendPolicy_OverGRPC(t *testing.T) {
	fake := &recordingTenantSpendServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterOrchestratorServiceServer(s, fake) })
	h := newOrchestratorHandler(srv.dial(t))
	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")

	resp, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "GetTenantSpendPolicy", &orchestrator.GetTenantSpendPolicyRequest{})
	require.NoError(t, err)
	assert.Equal(t, "tenant-a", fake.lastGet.GetTenantId())
	policy, ok := resp.(*orchestrator.TenantSpendPolicy)
	require.Truef(t, ok, "response is %T", resp)
	require.Len(t, policy.Limits.Windows, 1)
	assert.Equal(t, 720*time.Hour, policy.Limits.Windows[0].Size)
	require.NotNil(t, policy.UpdatedAt)
	require.Len(t, policy.Windows, 1)
	assert.Equal(t, int64(1_500_000), policy.Windows[0].UsedEquivalentMicros)

	limits := spend.Limits{Windows: []spend.Window{{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: 5}}}
	_, err = h.handleRequest(ctx, "quicbaas.Orchestrator", "SetTenantSpendPolicy",
		&orchestrator.SetTenantSpendPolicyRequest{TenantID: "tenant-b", Limits: limits})
	require.NoError(t, err)
	assert.Equal(t, "tenant-b", fake.lastSet.GetTenantId(), "an explicit tenant wins over the context")
	assert.Equal(t, int64(5), fake.lastSet.GetLimits().GetWindows()[0].GetMaxEquivalentMicros())

	// Clearing sends a present, empty message rather than nothing.
	_, err = h.handleRequest(ctx, "quicbaas.Orchestrator", "SetTenantSpendPolicy", &orchestrator.SetTenantSpendPolicyRequest{})
	require.NoError(t, err)
	require.NotNil(t, fake.lastSet.GetLimits())
	assert.Empty(t, fake.lastSet.GetLimits().GetWindows())
}

func TestOrchestratorTenantSpendPolicy_RefusesAWrongRequestType(t *testing.T) {
	for _, method := range []string{"GetTenantSpendPolicy", "SetTenantSpendPolicy"} {
		handler, ok := orchestratorMethodMap[method]
		require.Truef(t, ok, "%s is not dispatched over gRPC", method)
		_, err := handler(context.Background(), nil, &orchestrator.GetSpendReportRequest{})
		require.Errorf(t, err, "%s accepted the wrong request type", method)
	}
}
