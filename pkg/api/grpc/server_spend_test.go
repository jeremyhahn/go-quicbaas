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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// OrchestratorService.GetSpendReport
// =============================================================================

// spendLedgerAgentService is a test agent service that exposes a consumption
// ledger and an account store, so the spendLedgerProvider assertion resolves.
type spendLedgerAgentService struct {
	grpcTestAgentService
	ledger   orchestration.ConsumptionLedger
	accounts orchestration.AccountStore
	projects orchestration.ProjectStore
	tenant   orchestration.TenantSpendPolicyStore
	agents   *orchestration.AgentDirectory
}

func (m *spendLedgerAgentService) AgentDirectory() *orchestration.AgentDirectory { return m.agents }

func (m *spendLedgerAgentService) TenantSpendPolicies() orchestration.TenantSpendPolicyStore {
	return m.tenant
}

func (m *spendLedgerAgentService) ConsumptionLedger() orchestration.ConsumptionLedger {
	return m.ledger
}

func (m *spendLedgerAgentService) AccountStore() orchestration.AccountStore { return m.accounts }

func (m *spendLedgerAgentService) ProjectStore() orchestration.ProjectStore { return m.projects }

// grpcSpendNow returns the instant the fixture's turns are placed just before.
//
// RELATIVE TO NOW, not a fixed date, because the quota window this test reads
// is SLIDING: it is [now-5h, now), evaluated at the moment of the call. A
// fixture anchored at a fixed past date would age out of every window and the
// test would assert that a spent account has its whole allowance left.
func grpcSpendNow() time.Time { return time.Now().UTC() }

// newSpendServer seeds a ledger with two turns under one job, paid for by one
// metered account, and returns a server over it.
func newSpendServer(t *testing.T, tenantID string) *OrchestratorServer {
	t.Helper()
	ctx := context.Background()
	ledger := orchestration.NewInMemoryConsumptionLedger()
	for i, id := range []string{"e1", "e2"} {
		require.NoError(t, ledger.Record(ctx, orchestration.ConsumptionEntry{
			EntryID: id, TenantID: tenantID,
			ProjectID: "p1", TaskID: "w1", JobID: "j1", SessionID: "s1", Round: i,
			AccountID: "a1", Engine: "acp", Model: "claude-opus-5",
			Outcome:      orchestration.ConsumptionOutcomeSuccess,
			InputTokens:  10,
			OutputTokens: 5,
			OccurredAt:   grpcSpendNow().Add(-time.Duration(i+1) * time.Minute),
		}))
	}
	accounts := orchestration.NewInMemoryAccountStore()
	require.NoError(t, accounts.Create(ctx, &orchestration.Account{
		ID: "a1", TenantID: tenantID, Provider: orchestration.AccountProviderClaude,
		DisplayName: "alice-work",
		Spend: orchestration.AccountSpendPolicy{Quota: orchestration.SpendLimits{
			Windows: []orchestration.QuotaWindow{{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 1000}},
		}},
	}))
	projects := orchestration.NewInMemoryProjectStore()
	require.NoError(t, projects.Create(ctx, &orchestration.Project{ID: "p1", TenantID: tenantID, Name: "go-truststrap"}))
	return NewOrchestratorServer(
		&spendLedgerAgentService{ledger: ledger, accounts: accounts, projects: projects},
		createOrchestratorGRPCTestLogger(),
	)
}

// TestGetSpendReport_ReportsTokensAndTheAccountThatPaid is the gRPC half of
// the issue's acceptance check: the spend surface reports the tokens the turn
// recorder wrote and the account that paid. go-quicbaas#794.
func TestGetSpendReport_ReportsTokensAndTheAccountThatPaid(t *testing.T) {
	srv := newSpendServer(t, "t1")

	resp, err := srv.GetSpendReport(context.Background(), &pb.GetSpendReportRequest{TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, "tenant", resp.GetLevel())
	assert.Equal(t, "project", resp.GetChildLevel())
	assert.Equal(t, int64(30), resp.GetTotals().GetTotalTokens())
	assert.Equal(t, int32(2), resp.GetTotals().GetEntries())

	// The drill-down names its projects rather than listing bare ids.
	// go-quicbaas#1058.
	require.Len(t, resp.GetChildren(), 1)
	assert.Equal(t, "p1", resp.GetChildren()[0].GetId())
	assert.Equal(t, "go-truststrap", resp.GetChildren()[0].GetName())

	require.Len(t, resp.GetAccounts(), 1)
	account := resp.GetAccounts()[0]
	assert.Equal(t, "a1", account.GetAccountId())
	assert.Equal(t, "alice-work", account.GetDisplayName())
	assert.Equal(t, "metered", account.GetPolicyStatus())
	require.Len(t, account.GetWindows(), 1)
	assert.Equal(t, int64(5*time.Hour), account.GetWindows()[0].GetSizeNanos())
	assert.Equal(t, int64(30), account.GetWindows()[0].GetUsedTokens())
	assert.Equal(t, int64(970), account.GetWindows()[0].GetRemainingTokens())

	// The honesty rule: nothing prices a turn, so the cost crosses as
	// unmeasured and a client must render a dash. go-quicbaas#791.
	assert.Zero(t, resp.GetTotals().GetPricedEntries(), "no turn in the fixture was priced")
}

// TestGetSpendReport_DrillsDown asserts the scope narrows level by level and
// the child rows name the level below.
func TestGetSpendReport_DrillsDown(t *testing.T) {
	srv := newSpendServer(t, "t1")
	ctx := context.Background()

	project, err := srv.GetSpendReport(ctx, &pb.GetSpendReportRequest{TenantId: "t1", ProjectId: "p1"})
	require.NoError(t, err)
	assert.Equal(t, "project", project.GetLevel())
	assert.Equal(t, "go-truststrap", project.GetProjectName())
	require.Len(t, project.GetChildren(), 1)
	assert.Equal(t, "w1", project.GetChildren()[0].GetId())
	assert.Equal(t, "task", project.GetChildren()[0].GetLevel())

	round, err := srv.GetSpendReport(ctx, &pb.GetSpendReportRequest{
		TenantId: "t1", ProjectId: "p1", TaskId: "w1", JobId: "j1", SessionId: "s1",
		Round: 1, RoundSet: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "round", round.GetLevel())
	assert.Equal(t, int64(15), round.GetTotals().GetTotalTokens())
	assert.Empty(t, round.GetChildren(), "the deepest level has nothing below it")
}

// TestGetSpendReport_AbsentTimestampsStayAbsent asserts a moment the report
// does not have crosses as unset rather than as the year 1, which a client
// would render as a date.
func TestGetSpendReport_AbsentTimestampsStayAbsent(t *testing.T) {
	srv := newSpendServer(t, "t1")

	resp, err := srv.GetSpendReport(context.Background(),
		&pb.GetSpendReportRequest{TenantId: "unspent-tenant"})
	require.NoError(t, err)
	assert.Nil(t, resp.GetFirstAt())
	assert.Nil(t, resp.GetLastAt())
	assert.Zero(t, resp.GetTotals().GetEntries())
}

// tenantRecordingAccountStore records the tenant carried on the context each
// Get was made under, so a handler that forgets to scope the context is caught
// rather than merely looking different from its siblings.
type tenantRecordingAccountStore struct {
	orchestration.AccountStore
	seen []string
}

func (s *tenantRecordingAccountStore) Get(ctx context.Context, tenantID, accountID string) (*orchestration.Account, error) {
	s.seen = append(s.seen, orchestration.TenantFromContext(ctx))
	return s.AccountStore.Get(ctx, tenantID, accountID)
}

// TestGetSpendReport_ScopesTheContextToTheRequestTenant asserts the handler
// puts the request's tenant ON THE CONTEXT, not only in the scope.
//
// IT IS LOAD-BEARING, not cosmetic. The orchestration KV is tenant-routed, so
// on a barrier-isolated tenant a read carrying no tenant resolves against the
// wrong store — exactly the defect go-quicbaas#820 fixed for session
// transcripts. It also arms AccountStore's scopeCheck, which compares the
// carried tenant against the addressed one and can only refuse more strictly.
func TestGetSpendReport_ScopesTheContextToTheRequestTenant(t *testing.T) {
	ctx := context.Background()
	ledger := orchestration.NewInMemoryConsumptionLedger()
	require.NoError(t, ledger.Record(ctx, orchestration.ConsumptionEntry{
		EntryID: "e1", TenantID: "t1", ProjectID: "p1",
		AccountID: "a1", Engine: "acp", Model: "claude-opus-5",
		Outcome:      orchestration.ConsumptionOutcomeSuccess,
		InputTokens:  3,
		OutputTokens: 2,
		OccurredAt:   grpcSpendNow().Add(-time.Minute),
	}))
	accounts := orchestration.NewInMemoryAccountStore()
	require.NoError(t, accounts.Create(ctx, &orchestration.Account{
		ID: "a1", TenantID: "t1", Provider: orchestration.AccountProviderClaude,
	}))
	recorder := &tenantRecordingAccountStore{AccountStore: accounts}
	srv := NewOrchestratorServer(
		&spendLedgerAgentService{ledger: ledger, accounts: recorder},
		createOrchestratorGRPCTestLogger(),
	)

	// A bare context: whatever tenant reaches the store can only have come
	// from the handler wrapping the request's.
	resp, err := srv.GetSpendReport(ctx, &pb.GetSpendReportRequest{TenantId: "t1"})
	require.NoError(t, err)
	assert.Equal(t, int64(5), resp.GetTotals().GetTotalTokens())

	require.NotEmpty(t, recorder.seen, "the account store was never consulted")
	for _, carried := range recorder.seen {
		assert.Equal(t, "t1", carried,
			"the handler must scope the context to the request's tenant, or a tenant-routed store resolves against the wrong one")
	}
}

// TestGetSpendReport_RefusalPaths asserts each way the read fails maps onto a
// distinguishable code. "You asked badly" and "this cluster records no spend"
// must not both read as an empty report.
// TestGetSpendReport_NarrowsToARangeAndSharesFees asserts the range crosses
// the wire both ways and a subscription's fee share reaches the tenant report.
func TestGetSpendReport_NarrowsToARangeAndSharesFees(t *testing.T) {
	ctx := context.Background()
	now := grpcSpendNow()
	ledger := orchestration.NewInMemoryConsumptionLedger()
	for i, id := range []string{"old", "new"} {
		require.NoError(t, ledger.Record(ctx, orchestration.ConsumptionEntry{
			EntryID: id, TenantID: "t1", ProjectID: "p1", AccountID: "a1",
			Engine: "acp", Model: "claude-opus-5", Outcome: orchestration.ConsumptionOutcomeSuccess,
			InputTokens: 10 * int64(i+1), OccurredAt: now.Add(-time.Duration(48-47*i) * time.Hour),
		}))
	}
	accounts := orchestration.NewInMemoryAccountStore()
	require.NoError(t, accounts.Create(ctx, &orchestration.Account{
		ID: "a1", TenantID: "t1", Provider: orchestration.AccountProviderClaude,
		Billing:   orchestration.AccountBilling{Mode: orchestration.BillingSubscription, MonthlyFeeMicros: 200_000_000},
		CreatedAt: now.Add(-720 * time.Hour),
	}))
	srv := NewOrchestratorServer(&spendLedgerAgentService{ledger: ledger, accounts: accounts},
		createOrchestratorGRPCTestLogger())

	from := now.Add(-24 * time.Hour)
	resp, err := srv.GetSpendReport(ctx, &pb.GetSpendReportRequest{
		TenantId: "t1", From: timestamppb.New(from), To: timestamppb.New(now),
	})
	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.GetTotals().GetEntries(), "only the turn inside the range")
	assert.Equal(t, int64(20), resp.GetTotals().GetTotalTokens())
	assert.True(t, resp.GetFrom().AsTime().Equal(from))
	assert.True(t, resp.GetTo().AsTime().Equal(now))

	want, known := orchestration.AccountBilling{Mode: orchestration.BillingSubscription, MonthlyFeeMicros: 200_000_000}.
		FeeShare(from, now)
	require.True(t, known)
	require.NotNil(t, resp.SubscriptionFeeMicros)
	assert.Equal(t, want, resp.GetSubscriptionFeeMicros())
	require.Len(t, resp.GetAccounts(), 1)
	require.NotNil(t, resp.GetAccounts()[0].SubscriptionFeeMicros)
	assert.Equal(t, want, resp.GetAccounts()[0].GetSubscriptionFeeMicros())
}

// TestGetSpendReport_CarriesTheProjectsOwnLimits asserts a project report
// crosses the wire with the project's windows and their usage.
func TestGetSpendReport_CarriesTheProjectsOwnLimits(t *testing.T) {
	ctx := context.Background()
	ledger := orchestration.NewInMemoryConsumptionLedger()
	require.NoError(t, ledger.Record(ctx, orchestration.ConsumptionEntry{
		EntryID: "e1", TenantID: "t1", ProjectID: "p1", Engine: "openvino", Model: "qwen3-8b",
		Outcome: orchestration.ConsumptionOutcomeSuccess, InputTokens: 700,
		OccurredAt: grpcSpendNow().Add(-time.Hour),
	}))
	projects := orchestration.NewInMemoryProjectStore()
	require.NoError(t, projects.Create(ctx, &orchestration.Project{ID: "p1", TenantID: "t1", Name: "p1",
		Spend: orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{{Name: "daily", Size: 24 * time.Hour, MaxTokens: 500}}}}))
	srv := NewOrchestratorServer(&spendLedgerAgentService{ledger: ledger, projects: projects},
		createOrchestratorGRPCTestLogger())

	resp, err := srv.GetSpendReport(ctx, &pb.GetSpendReportRequest{TenantId: "t1", ProjectId: "p1"})
	require.NoError(t, err)
	assert.Equal(t, "metered", resp.GetScopePolicyStatus())
	require.Len(t, resp.GetScopeWindows(), 1)
	assert.Equal(t, int64(700), resp.GetScopeWindows()[0].GetUsedTokens())
	assert.True(t, resp.GetScopeWindows()[0].GetExhausted(), "a spent window defers admission and must say so")
}

func TestGetSpendReport_RefusalPaths(t *testing.T) {
	t.Run("to without from", func(t *testing.T) {
		srv := newSpendServer(t, "t1")
		_, err := srv.GetSpendReport(context.Background(),
			&pb.GetSpendReportRequest{TenantId: "t1", To: timestamppb.Now()})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("nil request", func(t *testing.T) {
		srv := newSpendServer(t, "t1")
		_, err := srv.GetSpendReport(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("skipped chain level", func(t *testing.T) {
		srv := newSpendServer(t, "t1")
		_, err := srv.GetSpendReport(context.Background(),
			&pb.GetSpendReportRequest{TenantId: "t1", TaskId: "w1"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err),
			"the chain is a path: a task without a project is not a prefix of anything")
	})

	t.Run("no ledger wired", func(t *testing.T) {
		srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())
		_, err := srv.GetSpendReport(context.Background(), &pb.GetSpendReportRequest{TenantId: "t1"})
		require.Error(t, err)
		assert.Equal(t, codes.Unavailable, status.Code(err),
			"a deployment that records no spend must say so, not report zero")
	})

	t.Run("ledger accessor with nothing behind it", func(t *testing.T) {
		srv := NewOrchestratorServer(&spendLedgerAgentService{}, createOrchestratorGRPCTestLogger())
		_, err := srv.GetSpendReport(context.Background(), &pb.GetSpendReportRequest{TenantId: "t1"})
		require.Error(t, err)
		assert.Equal(t, codes.Unavailable, status.Code(err))
	})
}

// TestGetSpendReport_BreaksDownAndNarrowsByHost asserts the host drill-down
// crosses the wire: every report carries its per-host rows, and a request
// naming a host is narrowed to it.
func TestGetSpendReport_BreaksDownAndNarrowsByHost(t *testing.T) {
	ctx := context.Background()
	ledger := orchestration.NewInMemoryConsumptionLedger()
	for i, host := range []string{"gpu-host:9091", "vm-host:9091"} {
		require.NoError(t, ledger.Record(ctx, orchestration.ConsumptionEntry{
			EntryID: host, TenantID: "t1", ProjectID: "p1", Host: host,
			Engine: "ollama", Model: "review-q8", Outcome: orchestration.ConsumptionOutcomeSuccess,
			InputTokens: int64(10 * (i + 1)), OccurredAt: grpcSpendNow().Add(-time.Minute),
			PriceBasis: orchestration.PriceBasisElectricity, PriceRef: "r", EquivalentMicros: 1,
			EnergyMilliWh: 5, EnergyProvenance: orchestration.ProvenanceMeasured,
		}))
	}
	srv := NewOrchestratorServer(&spendLedgerAgentService{ledger: ledger}, createOrchestratorGRPCTestLogger())

	all, err := srv.GetSpendReport(ctx, &pb.GetSpendReportRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.Len(t, all.GetHosts(), 2)
	assert.Equal(t, "gpu-host:9091", all.GetHosts()[0].GetHost())
	assert.Equal(t, int64(10), all.GetHosts()[0].GetTotals().GetTotalTokens())
	assert.Equal(t, int32(2), all.GetTotals().GetMeasuredEnergyEntries())

	one, err := srv.GetSpendReport(ctx, &pb.GetSpendReportRequest{TenantId: "t1", Host: "vm-host:9091"})
	require.NoError(t, err)
	assert.Equal(t, "vm-host:9091", one.GetHost())
	assert.Equal(t, int64(20), one.GetTotals().GetTotalTokens())
	require.Len(t, one.GetHosts(), 1)

	_, err = srv.GetSpendReport(ctx, &pb.GetSpendReportRequest{TenantId: "t1", Host: " "})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGetSpendReport_BreaksDownAndNarrowsByAgent(t *testing.T) {
	ctx := context.Background()
	ledger := orchestration.NewInMemoryConsumptionLedger()
	for i, agent := range []string{"claude-coder", ""} {
		require.NoError(t, ledger.Record(ctx, orchestration.ConsumptionEntry{
			EntryID: "e" + agent, TenantID: "t1", ProjectID: "p1", ResolvedAgentID: agent,
			Engine: "acp", Model: "claude-opus-5", Outcome: orchestration.ConsumptionOutcomeSuccess,
			InputTokens: int64(10 * (i + 1)), OccurredAt: grpcSpendNow().Add(-time.Minute),
		}))
	}
	dir, err := orchestration.NewAgentDirectory(orchestration.AgentDirectoryConfig{
		Store: orchestration.NewInMemoryAgentCatalogStore(), Models: anyModels{},
	})
	require.NoError(t, err)
	_, err = dir.CreateAgent(ctx, "t1", &orchestration.Agent{
		ID: "claude-coder", Name: "Claude coder", Roles: []orchestration.AgentRole{orchestration.AgentRoleCoding},
		Backend: "claude", Model: "claude-opus-5",
		Spend: orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{{Name: "daily", Size: 24 * time.Hour, MaxTokens: 5}}},
	})
	require.NoError(t, err)
	srv := NewOrchestratorServer(&spendLedgerAgentService{ledger: ledger, agents: dir}, createOrchestratorGRPCTestLogger())

	all, err := srv.GetSpendReport(ctx, &pb.GetSpendReportRequest{TenantId: "t1"})
	require.NoError(t, err)
	require.Len(t, all.GetAgents(), 2)
	assert.Equal(t, "claude-coder", all.GetAgents()[0].GetAgentId())
	assert.Equal(t, "Claude coder", all.GetAgents()[0].GetName())
	assert.True(t, all.GetAgents()[1].GetUnattributed())
	assert.Empty(t, all.GetAgentPolicyStatus())

	one, err := srv.GetSpendReport(ctx, &pb.GetSpendReportRequest{TenantId: "t1", Agent: "claude-coder"})
	require.NoError(t, err)
	assert.Equal(t, "claude-coder", one.GetAgent())
	assert.Equal(t, int64(10), one.GetTotals().GetTotalTokens())
	assert.Equal(t, "metered", one.GetAgentPolicyStatus())
	require.Len(t, one.GetAgentWindows(), 1)
	assert.True(t, one.GetAgentWindows()[0].GetExhausted())

	_, err = srv.GetSpendReport(ctx, &pb.GetSpendReportRequest{TenantId: "t1", Agent: "Not An ID"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
