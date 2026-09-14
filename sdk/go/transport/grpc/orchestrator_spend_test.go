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

package grpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// recordingSpendServer captures the proto request the SDK built and answers
// with one metered account, so both directions of the mapping are asserted
// rather than inferred.
type recordingSpendServer struct {
	pb.UnimplementedOrchestratorServiceServer

	mu   sync.Mutex
	last *pb.GetSpendReportRequest
	fail bool
}

// spendFixtureTime is the instant the fixture's windows and verdicts refer to.
var spendFixtureTime = time.Unix(1700000000, 0).UTC()

// GetSpendReport records the request and returns a fixed report.
func (s *recordingSpendServer) GetSpendReport(
	_ context.Context,
	req *pb.GetSpendReportRequest,
) (*pb.GetSpendReportResponse, error) {
	s.mu.Lock()
	s.last = req
	fail := s.fail
	s.mu.Unlock()
	if fail {
		return nil, status.Error(codes.Unavailable, "consumption ledger not available")
	}
	return &pb.GetSpendReportResponse{
		Level:        "project",
		TenantId:     req.GetTenantId(),
		ProjectId:    req.GetProjectId(),
		Entries:      3,
		InputTokens:  120,
		OutputTokens: 80,
		TotalTokens:  200,
		CostMicros:   0,
		CostMeasured: false,
		Successes:    2,
		Failures:     1,
		FirstAt:      timestamppb.New(spendFixtureTime),
		ChildLevel:   "task",
		Children: []*pb.SpendChild{
			{Id: "task-1", Level: "task", TotalTokens: 150},
			{Level: "task", Unattributed: true, TotalTokens: 50},
		},
		Models: []*pb.SpendModel{{Engine: "acp", Model: "claude-opus-5", TotalTokens: 200}},
		Accounts: []*pb.SpendAccount{{
			AccountId:    "a1",
			DisplayName:  "alice-work",
			Provider:     "claude",
			TotalTokens:  200,
			PolicyStatus: "metered",
			Windows: []*pb.SpendWindowStatus{{
				Name:            "five-hour",
				SizeNanos:       int64(5 * time.Hour),
				MaxTokens:       1000,
				UsedTokens:      200,
				RemainingTokens: 800,
				FreesAt:         timestamppb.New(spendFixtureTime.Add(5 * time.Hour)),
			}},
			Breaker: &pb.SpendBreakerVerdict{
				State:         "closed",
				Failures:      1,
				Successes:     2,
				LastFailureAt: timestamppb.New(spendFixtureTime),
			},
		}},
	}, nil
}

// request returns the last recorded proto request.
func (s *recordingSpendServer) request() *pb.GetSpendReportRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// TestOrchestratorGetSpendReport_IsDispatchedOverGRPC pins the verb into the
// dispatch map. A verb absent from it is answered "unknown method" by the SDK
// before the wire is ever touched — and the Unix transport delegates to this
// same map, so a gap here is a gap on two wires.
func TestOrchestratorGetSpendReport_IsDispatchedOverGRPC(t *testing.T) {
	if _, ok := orchestratorMethodMap["GetSpendReport"]; !ok {
		t.Fatal("GetSpendReport is not in orchestratorMethodMap: the SDK refuses the verb before dialling")
	}
}

// TestOrchestratorGetSpendReport_MapsEveryField asserts the SDK builds the
// proto request from every field of its own and reads every field back.
func TestOrchestratorGetSpendReport_MapsEveryField(t *testing.T) {
	fake := &recordingSpendServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, fake)
	})
	h := newOrchestratorHandler(srv.dial(t))

	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	resp, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "GetSpendReport",
		&orchestrator.GetSpendReportRequest{
			ProjectID: "p1", TaskID: "w1", JobID: "j1", SessionID: "s1", Round: 0, RoundSet: true,
		})
	require.NoError(t, err)

	got := fake.request()
	require.NotNil(t, got)
	assert.Equal(t, "tenant-a", got.GetTenantId(), "the context tenant must cross the wire")
	assert.Equal(t, "p1", got.GetProjectId())
	assert.Equal(t, "w1", got.GetTaskId())
	assert.Equal(t, "j1", got.GetJobId())
	assert.Equal(t, "s1", got.GetSessionId())
	assert.True(t, got.GetRoundSet(), "round 0 is a level; round_set is what says so")

	report, ok := resp.(*orchestrator.GetSpendReportResponse)
	require.Truef(t, ok, "response is %T", resp)
	assert.Equal(t, orchestrator.SpendLevelProject, report.Level)
	assert.Equal(t, int64(200), report.TotalTokens)
	assert.Equal(t, 1, report.Failures)
	assert.False(t, report.CostMeasured, "an unpriced report must not claim a measured cost")
	require.NotNil(t, report.FirstAt)
	assert.Equal(t, spendFixtureTime, *report.FirstAt)
	// NIL, not the zero instant: `omitempty` cannot omit a zero time.Time, so
	// a non-pointer field would have put "0001-01-01T00:00:00Z" on the wire
	// the moment this type is re-marshalled (the desktop binding does).
	assert.Nil(t, report.LastAt, "an absent timestamp must stay absent, not become the year 1")

	require.Len(t, report.Children, 2)
	assert.Equal(t, orchestrator.SpendLevelTask, report.Children[0].Level)
	assert.True(t, report.Children[1].Unattributed)

	require.Len(t, report.Models, 1)
	assert.Equal(t, "claude-opus-5", report.Models[0].Model)

	require.Len(t, report.Accounts, 1)
	account := report.Accounts[0]
	assert.Equal(t, orchestrator.SpendPolicyMetered, account.PolicyStatus)
	assert.Equal(t, "alice-work", account.DisplayName)
	require.Len(t, account.Windows, 1)
	assert.Equal(t, int64(5*time.Hour), account.Windows[0].Size)
	assert.Equal(t, int64(800), account.Windows[0].RemainingTokens)
	require.NotNil(t, account.Breaker)
	assert.Equal(t, "closed", account.Breaker.State)
	assert.Nil(t, account.Breaker.OpenUntil, "a closed breaker has no open-until")
}

// TestOrchestratorGetSpendReport_RefusalPaths asserts neither a server refusal
// nor a mistyped request produces a report the caller would read as no spend.
func TestOrchestratorGetSpendReport_RefusalPaths(t *testing.T) {
	fake := &recordingSpendServer{fail: true}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, fake)
	})
	h := newOrchestratorHandler(srv.dial(t))
	ctx := context.Background()

	resp, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "GetSpendReport",
		&orchestrator.GetSpendReportRequest{TenantID: "t1"})
	require.Error(t, err)
	assert.Nil(t, resp)

	resp, err = h.handleRequest(ctx, "quicbaas.Orchestrator", "GetSpendReport",
		&orchestrator.GetSessionConversationRequest{SessionID: "s1"})
	require.Error(t, err, "a mistyped request must be refused")
	assert.Nil(t, resp)
}
