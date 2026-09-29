//go:build mcp

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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// quicbaas_orchestrator_cost
// =============================================================================

// ledgerAgentService is a mockAgentService that also exposes a consumption
// ledger and an account store, so the spendLedgerAccessor assertion resolves.
// The bare mockAgentService deliberately does not, which is what makes the
// "deployment that records no spend" branch reachable.
type ledgerAgentService struct {
	*mockAgentService
	ledger   orchestration.ConsumptionLedger
	accounts orchestration.AccountStore
	projects orchestration.ProjectStore
	tenant   orchestration.TenantSpendPolicyStore
}

func (s *ledgerAgentService) TenantSpendPolicies() orchestration.TenantSpendPolicyStore {
	return s.tenant
}

func (s *ledgerAgentService) ConsumptionLedger() orchestration.ConsumptionLedger { return s.ledger }
func (s *ledgerAgentService) AccountStore() orchestration.AccountStore           { return s.accounts }
func (s *ledgerAgentService) ProjectStore() orchestration.ProjectStore           { return s.projects }
func (s *ledgerAgentService) AgentDirectory() *orchestration.AgentDirectory      { return nil }

var _ orchestration.SpendLedgerAccessor = (*ledgerAgentService)(nil)

// newCostRegistry seeds a ledger with two turns under one job and returns a
// tool registry over it.
func newCostRegistry(t *testing.T, tenantID string) *ToolRegistry {
	t.Helper()
	ctx := context.Background()
	ledger := orchestration.NewInMemoryConsumptionLedger()
	now := time.Now().UTC()
	for i, id := range []string{"e1", "e2"} {
		require.NoError(t, ledger.Record(ctx, orchestration.ConsumptionEntry{
			EntryID: id, TenantID: tenantID,
			ProjectID: "p1", TaskID: "w1", JobID: "j1", SessionID: "s1", Round: i,
			AccountID: "a1", Engine: "acp", Model: "claude-opus-5",
			Outcome:      orchestration.ConsumptionOutcomeSuccess,
			InputTokens:  10,
			OutputTokens: 5,
			OccurredAt:   now.Add(-time.Duration(i+1) * time.Minute),
		}))
	}
	accounts := orchestration.NewInMemoryAccountStore()
	require.NoError(t, accounts.Create(ctx, &orchestration.Account{
		ID: "a1", TenantID: tenantID, Provider: orchestration.AccountProviderClaude,
		DisplayName: "alice-work",
	}))
	return newFleetRegistry(t, &ledgerAgentService{
		mockAgentService: &mockAgentService{}, ledger: ledger, accounts: accounts,
	})
}

// TestMCPCostReportsTokensAndTheAccount is the MCP half of the issue's
// acceptance check: the tool returns the tokens the turn recorder wrote and
// the account that paid. go-quicbaas#794.
func TestMCPCostReportsTokensAndTheAccount(t *testing.T) {
	reg := newCostRegistry(t, "tenant-1")

	raw, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
		map[string]interface{}{"tenant_id": "tenant-1"})
	require.NoError(t, err)

	report, ok := raw.(map[string]interface{})
	require.True(t, ok, "result type = %T", raw)
	assert.Equal(t, "tenant", report["level"])
	assert.Equal(t, float64(30), report["total_tokens"])

	// The honesty rule: nothing prices a turn, so the tool must say the cost
	// is unmeasured rather than letting a client read the zero. #791.
	assert.Equal(t, float64(0), report["priced_entries"])

	accounts, ok := report["accounts"].([]interface{})
	require.True(t, ok, "a report must name the accounts that paid")
	require.Len(t, accounts, 1)
	account, ok := accounts[0].(map[string]interface{})
	require.True(t, ok, "account type = %T", accounts[0])
	assert.Equal(t, "a1", account["account_id"])
	assert.Equal(t, string(orchestration.SpendPolicyNone), account["policy_status"],
		"an account that was read and meters nothing has NO policy, not an unknown one")
}

// TestMCPCostDrillsDownToARound asserts the chain narrows, and that round 0 —
// a real level — is selected by presence rather than by truthiness.
func TestMCPCostDrillsDownToARound(t *testing.T) {
	reg := newCostRegistry(t, "tenant-1")

	raw, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
		map[string]interface{}{
			"tenant_id": "tenant-1", "project_id": "p1", "task_id": "w1",
			"job_id": "j1", "session_id": "s1", "round": 0,
		})
	require.NoError(t, err)

	report, ok := raw.(map[string]interface{})
	require.True(t, ok, "result type = %T", raw)
	assert.Equal(t, "round", report["level"])
	assert.Equal(t, float64(15), report["total_tokens"],
		"round 0 must narrow the scope; a truthiness check would have widened it to every round")
}

// TestMCPCostNarrowsToARange asserts from/to reach the ledger and are echoed.
// The fixture's turns are one and two minutes old; a range starting 90
// seconds ago holds only the newer.
func TestMCPCostNarrowsToARange(t *testing.T) {
	reg := newCostRegistry(t, "tenant-1")
	from := time.Now().UTC().Add(-90 * time.Second)

	raw, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
		map[string]interface{}{"tenant_id": "tenant-1", "from": from.Format(time.RFC3339Nano)})
	require.NoError(t, err)

	report, ok := raw.(map[string]interface{})
	require.True(t, ok, "result type = %T", raw)
	assert.Equal(t, float64(1), report["entries"])
	assert.Equal(t, float64(15), report["total_tokens"])
	assert.NotEmpty(t, report["from"])
	assert.NotEmpty(t, report["to"], "a range given only its start closes at now")
}

// TestMCPCostRejectsBadInput asserts an unaddressed request, a malformed
// scope, and a deployment that records no spend are typed refusals rather
// than an empty report an MCP client would read as no spend.
func TestMCPCostRejectsBadInput(t *testing.T) {
	t.Run("no tenant id", func(t *testing.T) {
		reg := newCostRegistry(t, "tenant-1")
		_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
			map[string]interface{}{"project_id": "p1"})
		require.Error(t, err)
	})

	t.Run("malformed from", func(t *testing.T) {
		reg := newCostRegistry(t, "tenant-1")
		_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
			map[string]interface{}{"tenant_id": "tenant-1", "from": "last week"})
		require.Error(t, err)
	})

	t.Run("to without from", func(t *testing.T) {
		reg := newCostRegistry(t, "tenant-1")
		_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
			map[string]interface{}{"tenant_id": "tenant-1", "to": time.Now().UTC().Format(time.RFC3339)})
		require.Error(t, err, "an open start is the whole history, not a range")
	})

	t.Run("skipped chain level", func(t *testing.T) {
		reg := newCostRegistry(t, "tenant-1")
		_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
			map[string]interface{}{"tenant_id": "tenant-1", "task_id": "w1"})
		require.Error(t, err, "the chain is a path: a task without a project is not a prefix")
	})

	t.Run("no ledger configured", func(t *testing.T) {
		reg := newFleetRegistry(t, &mockAgentService{})
		_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
			map[string]interface{}{"tenant_id": "tenant-1"})
		require.Error(t, err, "a deployment that records no spend must say so, not report zero")
	})
}

// TestMCPCostToolIsRegistered pins the tool's name and its required
// arguments. A drift here makes the verb unreachable over MCP with "unknown
// tool" as the only symptom.
func TestMCPCostToolIsRegistered(t *testing.T) {
	reg := newCostRegistry(t, "tenant-1")
	tool := toolByName(t, reg, "quicbaas_orchestrator_cost")
	assert.ElementsMatch(t, []string{"tenant_id"}, requiredArgs(t, tool))
}

// TestMCPCostNarrowsToAHost asserts the host argument reaches the ledger. The
// fixture's turns name no host, so "-" selects both and a named host neither.
func TestMCPCostNarrowsToAHost(t *testing.T) {
	reg := newCostRegistry(t, "tenant-1")
	tests := map[string]struct {
		host    string
		entries float64
	}{
		"the unattributed host": {"-", 2},
		"a host that ran none":  {"gpu-host:9091", 0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			raw, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
				map[string]interface{}{"tenant_id": "tenant-1", "host": tc.host})
			require.NoError(t, err)
			report, ok := raw.(map[string]interface{})
			require.True(t, ok, "result type = %T", raw)
			assert.Equal(t, tc.entries, report["entries"])
			assert.Equal(t, tc.host, report["host"])
		})
	}
	_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
		map[string]interface{}{"tenant_id": "tenant-1", "host": "  "})
	assert.Error(t, err, "a blank host is refused, not read as every host")
}

// TestMCPCostNarrowsToAnAgent asserts the agent argument reaches the ledger.
// The fixture's turns ran on raw backends, so "-" selects both.
func TestMCPCostNarrowsToAnAgent(t *testing.T) {
	reg := newCostRegistry(t, "tenant-1")
	for agent, entries := range map[string]float64{"-": 2, "claude-coder": 0} {
		raw, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
			map[string]interface{}{"tenant_id": "tenant-1", "agent": agent})
		require.NoError(t, err)
		report, ok := raw.(map[string]interface{})
		require.True(t, ok, "result type = %T", raw)
		assert.Equal(t, entries, report["entries"], agent)
		assert.Equal(t, agent, report["agent"])
	}
	_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_cost",
		map[string]interface{}{"tenant_id": "tenant-1", "agent": "Bad ID"})
	assert.Error(t, err, "a malformed Agent is refused, not read as every Agent")
}
