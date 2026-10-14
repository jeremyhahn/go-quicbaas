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
package embedded

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newTenantSpendTransport wires an in-process transport over an agent service
// holding a tenant spend-policy store and a ledger.
func newTenantSpendTransport(t *testing.T, agent *ledgerFakeAgentService) *Transport {
	t.Helper()
	tr, err := New(&orchMock{
		mockQuicBaaS: &mockQuicBaaS{},
		agent:        agent,
		proj:         &fakeProjectService{},
		admin:        &fakeFleetAdminService{},
	})
	require.NoError(t, err)
	return tr
}

// TestEmbeddedTenantSpendPolicy is go-quicbaas#1058 in process: set replaces a
// tenant's limits and get reads them back with the usage against each window.
func TestEmbeddedTenantSpendPolicy(t *testing.T) {
	tr := newTenantSpendTransport(t, &ledgerFakeAgentService{
		fakeAgentService: &fakeAgentService{},
		ledger:           orchestration.NewInMemoryConsumptionLedger(),
		tenant:           orchestration.NewInMemoryTenantSpendPolicyStore(),
	})
	ctx := transport.ContextWithTenant(context.Background(), "t1")
	limits := spend.Limits{Windows: []spend.Window{{Name: "monthly", Size: 720 * time.Hour, MaxEquivalentMicros: 9}}}

	set, err := tr.Request(ctx, "quicbaas.Orchestrator", "SetTenantSpendPolicy",
		&orchestratorSDK.SetTenantSpendPolicyRequest{TenantID: "t1", Limits: limits})
	require.NoError(t, err)
	stored, ok := set.(*orchestratorSDK.TenantSpendPolicy)
	require.Truef(t, ok, "response is %T", set)
	require.NotNil(t, stored.UpdatedAt)

	got, err := tr.Request(ctx, "quicbaas.Orchestrator", "GetTenantSpendPolicy",
		&orchestratorSDK.GetTenantSpendPolicyRequest{TenantID: "t1"})
	require.NoError(t, err)
	policy := got.(*orchestratorSDK.TenantSpendPolicy)
	require.Len(t, policy.Limits.Windows, 1)
	assert.Equal(t, limits.Windows[0], policy.Limits.Windows[0])
	assert.Len(t, policy.Windows, 1)
}

func TestEmbeddedTenantSpendPolicyRefuses(t *testing.T) {
	ctx := transport.ContextWithTenant(context.Background(), "t1")
	unwired := newTenantSpendTransport(t, &ledgerFakeAgentService{fakeAgentService: &fakeAgentService{}})
	_, err := unwired.Request(ctx, "quicbaas.Orchestrator", "GetTenantSpendPolicy",
		&orchestratorSDK.GetTenantSpendPolicyRequest{TenantID: "t1"})
	assert.Error(t, err, "a deployment with no store must not report an unmetered tenant")

	_, err = handleOrchestratorGetTenantSpendPolicy(ctx, &orchMock{mockQuicBaaS: &mockQuicBaaS{}}, &orchestratorSDK.GetSpendReportRequest{})
	assert.Error(t, err, "the wrong request type must be refused")
	_, err = handleOrchestratorSetTenantSpendPolicy(ctx, &orchMock{mockQuicBaaS: &mockQuicBaaS{}}, &orchestratorSDK.GetSpendReportRequest{})
	assert.Error(t, err, "the wrong request type must be refused")

	bad := newTenantSpendTransport(t, &ledgerFakeAgentService{
		fakeAgentService: &fakeAgentService{}, tenant: orchestration.NewInMemoryTenantSpendPolicyStore(),
	})
	_, err = bad.Request(ctx, "quicbaas.Orchestrator", "SetTenantSpendPolicy", &orchestratorSDK.SetTenantSpendPolicyRequest{
		TenantID: "t1", Limits: spend.Limits{Windows: []spend.Window{{Name: "w", Size: time.Hour}}},
	})
	assert.ErrorIs(t, err, orchestration.ErrQuotaInvalid)
}
