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
	"google.golang.org/protobuf/proto"
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
		Level:     "project",
		TenantId:  req.GetTenantId(),
		ProjectId: req.GetProjectId(),
		Totals: &pb.SpendTotals{
			Entries: 3, InputTokens: 120, OutputTokens: 80, TotalTokens: 200,
			Successes: 2, Failures: 1,
			EquivalentMicros: 1_250_000, PricedEntries: 3,
			MeasuredEnergyEntries: 2,
		},
		Host:   req.GetHost(),
		Hosts:  []*pb.SpendHost{{Host: "gpu-host:9091", Totals: &pb.SpendTotals{TotalTokens: 200}}, {Unattributed: true}},
		Agent:  req.GetAgent(),
		Agents: []*pb.SpendAgent{{AgentId: "claude-coder", Name: "Claude coder", Totals: &pb.SpendTotals{TotalTokens: 200}}, {Unattributed: true}},

		AgentPolicyStatus: "metered",
		AgentWindows:      []*pb.SpendWindowStatus{{Name: "daily", MaxTokens: 100, UsedTokens: 200, Exhausted: true}},
		Currency:          "USD",
		FirstAt:           timestamppb.New(spendFixtureTime),
		From:              req.GetFrom(),
		To:                req.GetTo(),
		ChildLevel:        "task",

		SubscriptionFeeMicros: proto.Int64(46_000_000),
		ScopePolicyStatus:     "metered",
		ScopeWindows:          []*pb.SpendWindowStatus{{Name: "weekly", SizeNanos: int64(7 * 24 * time.Hour), MaxTokens: 10, UsedTokens: 12, Exhausted: true}},
		Children: []*pb.SpendChild{
			{Id: "task-1", Level: "task", Totals: &pb.SpendTotals{TotalTokens: 150}},
			{Level: "task", Unattributed: true, Totals: &pb.SpendTotals{TotalTokens: 50}},
		},
		Models: []*pb.SpendModel{{Engine: "acp", Model: "claude-opus-5", Totals: &pb.SpendTotals{TotalTokens: 200}}},
		Accounts: []*pb.SpendAccount{{
			AccountId:        "a1",
			DisplayName:      "alice-work",
			Provider:         "claude",
			Totals:           &pb.SpendTotals{TotalTokens: 200, BilledEntries: 3},
			BillingMode:      "subscription",
			MonthlyFeeMicros: 200_000_000,
			PolicyStatus:     "metered",

			SubscriptionFeeMicros: proto.Int64(46_000_000),
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
	from := spendFixtureTime.Add(-24 * time.Hour)
	resp, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "GetSpendReport",
		&orchestrator.GetSpendReportRequest{
			ProjectID: "p1", TaskID: "w1", JobID: "j1", SessionID: "s1", Round: 0, RoundSet: true,
			From: &from, Host: "gpu-host:9091", Agent: "claude-coder",
		})
	require.NoError(t, err)

	got := fake.request()
	assert.Equal(t, "gpu-host:9091", got.GetHost(), "the host drill-down must cross the wire")
	assert.Equal(t, "claude-coder", got.GetAgent(), "the Agent drill-down must cross the wire")
	require.NotNil(t, got)
	assert.Equal(t, "tenant-a", got.GetTenantId(), "the context tenant must cross the wire")
	assert.Equal(t, "p1", got.GetProjectId())
	assert.Equal(t, "w1", got.GetTaskId())
	assert.Equal(t, "j1", got.GetJobId())
	assert.Equal(t, "s1", got.GetSessionId())
	assert.True(t, got.GetRoundSet(), "round 0 is a level; round_set is what says so")
	assert.True(t, got.GetFrom().AsTime().Equal(from), "the range start must cross the wire")
	assert.Nil(t, got.GetTo(), "an unset end stays unset")

	report, ok := resp.(*orchestrator.GetSpendReportResponse)
	require.Truef(t, ok, "response is %T", resp)
	assert.Equal(t, orchestrator.SpendLevelProject, report.Level)
	assert.Equal(t, int64(200), report.TotalTokens)
	assert.Equal(t, 1, report.Failures)
	// The money crosses in its totals message, and the billing with the
	// account that pays. go-quicbaas#1058.
	assert.Equal(t, int64(1_250_000), report.EquivalentMicros)
	assert.Equal(t, 2, report.MeasuredEnergyEntries)
	assert.Equal(t, "gpu-host:9091", report.Host)
	require.Len(t, report.Hosts, 2)
	assert.Equal(t, int64(200), report.Hosts[0].TotalTokens)
	assert.True(t, report.Hosts[1].Unattributed)
	assert.Equal(t, "claude-coder", report.Agent)
	require.Len(t, report.Agents, 2)
	assert.Equal(t, "Claude coder", report.Agents[0].Name)
	assert.True(t, report.Agents[1].Unattributed)
	assert.Equal(t, orchestrator.SpendPolicyStatus("metered"), report.AgentPolicyStatus)
	require.Len(t, report.AgentWindows, 1)
	assert.True(t, report.AgentWindows[0].Exhausted)
	assert.Equal(t, 3, report.PricedEntries)
	assert.Equal(t, "USD", report.Currency)
	assert.Equal(t, int64(150), report.Children[0].TotalTokens)
	assert.Equal(t, "subscription", report.Accounts[0].Billing.Mode)
	assert.Equal(t, int64(200_000_000), report.Accounts[0].Billing.MonthlyFeeMicros)
	require.NotNil(t, report.FirstAt)
	assert.Equal(t, spendFixtureTime, *report.FirstAt)
	// NIL, not the zero instant: `omitempty` cannot omit a zero time.Time, so
	// a non-pointer field would have put "0001-01-01T00:00:00Z" on the wire
	// the moment this type is re-marshalled (the desktop binding does).
	assert.Nil(t, report.LastAt, "an absent timestamp must stay absent, not become the year 1")
	require.NotNil(t, report.From)
	assert.True(t, report.From.Equal(from))
	require.NotNil(t, report.SubscriptionFeeMicros)
	assert.Equal(t, int64(46_000_000), *report.SubscriptionFeeMicros)
	require.NotNil(t, report.Accounts[0].SubscriptionFeeMicros)
	assert.Equal(t, orchestrator.SpendPolicyMetered, report.ScopePolicyStatus)
	require.Len(t, report.ScopeWindows, 1)
	assert.True(t, report.ScopeWindows[0].Exhausted)

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
