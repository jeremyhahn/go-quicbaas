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

package embedded

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// quicbaas.Orchestrator/GetSpendReport over the in-process transport
// =============================================================================

// ledgerFakeAgentService is a fakeAgentService that also exposes a
// consumption ledger and an account store. The bare fakeAgentService
// deliberately does not, which is what makes the "deployment that records no
// spend" branch reachable.
type ledgerFakeAgentService struct {
	*fakeAgentService

	ledger   orchestration.ConsumptionLedger
	accounts orchestration.AccountStore
}

// ConsumptionLedger implements orchestration.SpendLedgerAccessor.
func (f *ledgerFakeAgentService) ConsumptionLedger() orchestration.ConsumptionLedger {
	return f.ledger
}

// AccountStore implements orchestration.SpendLedgerAccessor.
func (f *ledgerFakeAgentService) AccountStore() orchestration.AccountStore { return f.accounts }

var _ orchestration.SpendLedgerAccessor = (*ledgerFakeAgentService)(nil)

// embeddedSpendBase anchors the fixture. Fixed, because the ledger's quota
// plane buckets by the hour and a fixture at time.Now() would straddle a
// boundary on some runs and not others.
var embeddedSpendBase = time.Date(2026, 8, 22, 10, 30, 0, 0, time.UTC)

// newSpendTransport wires an in-process transport over an agent service whose
// ledger already holds two turns under one job, paid for by one metered
// account.
func newSpendTransport(t *testing.T, tenantID string) *Transport {
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
			OccurredAt:   embeddedSpendBase.Add(time.Duration(i) * time.Minute),
		}))
	}
	accounts := orchestration.NewInMemoryAccountStore()
	require.NoError(t, accounts.Create(ctx, &orchestration.Account{
		ID: "a1", TenantID: tenantID, Provider: orchestration.AccountProviderClaude,
		DisplayName: "alice-work",
	}))

	tr, err := New(&orchMock{
		mockQuicBaaS: &mockQuicBaaS{},
		agent: &ledgerFakeAgentService{
			fakeAgentService: &fakeAgentService{}, ledger: ledger, accounts: accounts,
		},
		proj:  &fakeProjectService{},
		admin: &fakeAgentAdminService{},
	})
	require.NoError(t, err)
	return tr
}

// TestEmbeddedGetSpendReport_RollsUpTheLedger asserts the in-process caller
// rolls up the same ledger the remote transports read, into the same report.
func TestEmbeddedGetSpendReport_RollsUpTheLedger(t *testing.T) {
	const tenantID = "t1"
	tr := newSpendTransport(t, tenantID)

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSpendReport",
		&orchestratorSDK.GetSpendReportRequest{TenantID: tenantID, ProjectID: "p1"})
	require.NoError(t, err)

	report, ok := resp.(*orchestratorSDK.GetSpendReportResponse)
	require.Truef(t, ok, "response is %T", resp)
	assert.Equal(t, orchestratorSDK.SpendLevelProject, report.Level)
	assert.Equal(t, orchestratorSDK.SpendLevelTask, report.ChildLevel)
	assert.Equal(t, int64(30), report.TotalTokens)
	assert.Equal(t, 2, report.Entries)

	require.Len(t, report.Accounts, 1)
	assert.Equal(t, "a1", report.Accounts[0].AccountID)
	assert.Equal(t, "alice-work", report.Accounts[0].DisplayName)
	assert.Equal(t, orchestratorSDK.SpendPolicyNone, report.Accounts[0].PolicyStatus,
		"an account that was read and meters nothing has NO policy, not an unknown one")

	assert.False(t, report.CostMeasured, "no production writer prices a turn")
	require.Len(t, report.Children, 1)
	assert.Equal(t, "w1", report.Children[0].ID)
}

// TestEmbeddedGetSpendReport_DrillsDownToTheJob asserts the chain narrows in
// process exactly as it does over the wire.
func TestEmbeddedGetSpendReport_DrillsDownToTheJob(t *testing.T) {
	tr := newSpendTransport(t, "t1")

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSpendReport",
		&orchestratorSDK.GetSpendReportRequest{
			TenantID: "t1", ProjectID: "p1", TaskID: "w1", JobID: "j1",
		})
	require.NoError(t, err)

	report, ok := resp.(*orchestratorSDK.GetSpendReportResponse)
	require.Truef(t, ok, "response is %T", resp)
	assert.Equal(t, orchestratorSDK.SpendLevelJob, report.Level)
	assert.Equal(t, "j1", report.JobID)
	assert.Equal(t, int64(30), report.TotalTokens)
}

// TestEmbeddedGetSpendReport_RejectsBadInput asserts a mistyped request, a
// malformed scope, and a deployment whose agent service records no spend are
// all typed refusals rather than an empty report.
func TestEmbeddedGetSpendReport_RejectsBadInput(t *testing.T) {
	t.Run("wrong request type", func(t *testing.T) {
		tr := newSpendTransport(t, "t1")
		_, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSpendReport",
			&orchestratorSDK.CloseSessionRequest{SessionID: "s1"})
		require.Error(t, err)
	})

	t.Run("skipped chain level", func(t *testing.T) {
		tr := newSpendTransport(t, "t1")
		_, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSpendReport",
			&orchestratorSDK.GetSpendReportRequest{TenantID: "t1", TaskID: "w1"})
		require.Error(t, err, "the chain is a path: a task without a project is not a prefix")
	})

	t.Run("agent service records no spend", func(t *testing.T) {
		tr, err := New(&orchMock{
			mockQuicBaaS: &mockQuicBaaS{},
			agent:        &fakeAgentService{},
			proj:         &fakeProjectService{},
			admin:        &fakeAgentAdminService{},
		})
		require.NoError(t, err)
		_, reqErr := tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSpendReport",
			&orchestratorSDK.GetSpendReportRequest{TenantID: "t1"})
		require.Error(t, reqErr, "a deployment that records no spend must say so, not report zero")
	})
}

// tenantWitnessLedger records the tenant carried by the CONTEXT of each read,
// which is the only thing the tenant-routed durable KV consults when it picks a
// backend (pkg/server/tenant_kvstore.go resolves from the context, never from
// the request body).
type tenantWitnessLedger struct {
	orchestration.ConsumptionLedger

	seen []string
}

func (l *tenantWitnessLedger) Rollup(ctx context.Context, scope orchestration.ConsumptionScope) (*orchestration.ConsumptionRollup, error) {
	l.seen = append(l.seen, orchestration.TenantFromContext(ctx))
	return l.ConsumptionLedger.Rollup(ctx, scope)
}

// TestEmbeddedGetSpendReport_StampsTheTenantIntoTheContext is the regression
// test for the embedded transport reading against the wrong store.
//
// The handler used to pass the caller's ctx through unmodified, carrying the
// tenant only in the request body. On a tenant-routed deployment -- barrier
// isolation, the default since qrdb v0.10.0 -- that resolves to whatever store
// the caller's bare context names rather than the tenant's own, which is the
// go-quicbaas#820 class the gRPC handler in this same change already closed.
//
// The assertion is deliberately on the CONTEXT the ledger is called with, not
// on the numbers in the report: an in-memory ledger answers correctly whatever
// context it is handed, so a report-shaped assertion passes with the defect in
// place. That is exactly how this survived on one transport while being fixed
// on another.
func TestEmbeddedGetSpendReport_StampsTheTenantIntoTheContext(t *testing.T) {
	const tenantID = "acme"

	ctx := context.Background()
	inner := orchestration.NewInMemoryConsumptionLedger()
	require.NoError(t, inner.Record(ctx, orchestration.ConsumptionEntry{
		EntryID: "e1", TenantID: tenantID, ProjectID: "p1", JobID: "j1",
		AccountID: "a1", Engine: "acp", Model: "claude-opus-5",
		Outcome:     orchestration.ConsumptionOutcomeSuccess,
		InputTokens: 10, OutputTokens: 5, OccurredAt: embeddedSpendBase,
	}))
	witness := &tenantWitnessLedger{ConsumptionLedger: inner}

	accounts := orchestration.NewInMemoryAccountStore()
	require.NoError(t, accounts.Create(ctx, &orchestration.Account{
		ID: "a1", TenantID: tenantID, Provider: orchestration.AccountProviderClaude,
	}))

	tr, err := New(&orchMock{
		mockQuicBaaS: &mockQuicBaaS{},
		agent: &ledgerFakeAgentService{
			fakeAgentService: &fakeAgentService{}, ledger: witness, accounts: accounts,
		},
		proj:  &fakeProjectService{},
		admin: &fakeAgentAdminService{},
	})
	require.NoError(t, err)

	// A BARE context, which is what the SDK client actually hands in: it has no
	// ContextWithTenant of its own.
	_, err = tr.Request(context.Background(), "quicbaas.Orchestrator", "GetSpendReport",
		&orchestratorSDK.GetSpendReportRequest{TenantID: tenantID, ProjectID: "p1"})
	require.NoError(t, err)

	require.NotEmpty(t, witness.seen, "the ledger was never read")
	for i, got := range witness.seen {
		assert.Equal(t, tenantID, got,
			"read %d reached the ledger with tenant %q in context, want %q: a tenant-routed "+
				"store resolves the backend from the context, so this read lands on the wrong store", i, got, tenantID)
	}
}
