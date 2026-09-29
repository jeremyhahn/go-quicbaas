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

package orchestration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSpendAgents is a tenant agent catalog for spend reports.
type stubSpendAgents struct {
	agents map[string]Agent
	err    error
}

func (s stubSpendAgents) Catalogue(context.Context, string) (AgentCatalogue, error) {
	return AgentCatalogue{Agents: s.agents}, s.err
}

// seedAgentFixture records turns of two Agents and one raw-backend session.
func seedAgentFixture(t *testing.T) ConsumptionLedger {
	t.Helper()
	ledger := NewInMemoryConsumptionLedger()
	agents := map[string]string{"a1": "claude-coder", "a2": "claude-coder", "a3": "codex-reviewer", "a4": ""}
	tokens := map[string]int64{"a1": 10, "a2": 20, "a3": 5, "a4": 1}
	for i, id := range []string{"a1", "a2", "a3", "a4"} {
		entry := spendEntry(id, "p1", "task-a", "job-1", "s1", i, "acct-1", tokens[id], ConsumptionOutcomeSuccess, spendBase.Add(time.Duration(i)*time.Minute))
		entry.ResolvedAgentID = agents[id]
		require.NoError(t, ledger.Record(context.Background(), entry))
	}
	return ledger
}

func coderWithLimit(maxTokens int64) stubSpendAgents {
	coder := testAgent("claude-coder", "claude", "claude-opus-5-5")
	coder.Name = "Claude coder"
	coder.Spend = SpendLimits{Windows: []QuotaWindow{{Name: "daily", Size: 24 * time.Hour, MaxTokens: maxTokens}}}
	reviewer := testAgent("codex-reviewer", "codex", "gpt-5.5", AgentRoleReview)
	return stubSpendAgents{agents: map[string]Agent{coder.ID: coder, reviewer.ID: reviewer}}
}

func TestReadSpendReport_BreaksSpendDownByAgent(t *testing.T) {
	t.Parallel()
	report, err := ReadSpendReport(context.Background(),
		SpendSources{Ledger: seedAgentFixture(t), Agents: coderWithLimit(100)}, &SpendReportRequest{TenantID: "t1"})
	require.NoError(t, err)
	require.Len(t, report.Agents, 3)
	assert.Equal(t, "claude-coder", report.Agents[0].AgentID)
	assert.Equal(t, "Claude coder", report.Agents[0].Name)
	assert.Equal(t, int64(60), report.Agents[0].TotalTokens)
	assert.Equal(t, "codex-reviewer", report.Agents[1].AgentID)
	assert.True(t, report.Agents[2].Unattributed, "a raw-backend session's turns are labelled, and sort last")
	assert.Empty(t, report.AgentPolicyStatus, "an un-narrowed report carries no Agent's limits")
}

func TestReadSpendReport_NarrowedToAnAgentReportsItsOwnLimits(t *testing.T) {
	t.Parallel()
	now := spendBase.Add(time.Hour)
	report, err := ReadSpendReport(context.Background(),
		SpendSources{Ledger: seedAgentFixture(t), Agents: coderWithLimit(50)},
		&SpendReportRequest{TenantID: "t1", Agent: "claude-coder", Now: now})
	require.NoError(t, err)
	assert.Equal(t, "claude-coder", report.Agent)
	assert.Equal(t, 2, report.Entries)
	assert.Equal(t, int64(60), report.TotalTokens)
	assert.Equal(t, SpendPolicyMetered, report.AgentPolicyStatus)
	require.Len(t, report.AgentWindows, 1)
	assert.Equal(t, int64(60), report.AgentWindows[0].UsedTokens)
	assert.True(t, report.AgentWindows[0].Exhausted, "60 of 50 tokens is a spent window")
}

func TestReadSpendReport_AgentLimitsUnknownOrNone(t *testing.T) {
	t.Parallel()
	ledger := seedAgentFixture(t)
	ctx := context.Background()
	tests := map[string]struct {
		sources SpendSources
		agent   string
		want    SpendPolicyStatus
	}{
		"no catalog wired":         {SpendSources{Ledger: ledger}, "claude-coder", SpendPolicyUnknown},
		"an Agent that is gone":    {SpendSources{Ledger: ledger, Agents: coderWithLimit(1)}, "retired", SpendPolicyUnknown},
		"an Agent with no limits":  {SpendSources{Ledger: ledger, Agents: coderWithLimit(1)}, "codex-reviewer", SpendPolicyNone},
		"the raw-backend sessions": {SpendSources{Ledger: ledger, Agents: coderWithLimit(1)}, SpendUnattributed, ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			report, err := ReadSpendReport(ctx, tc.sources, &SpendReportRequest{TenantID: "t1", Agent: tc.agent})
			require.NoError(t, err)
			assert.Equal(t, tc.want, report.AgentPolicyStatus)
		})
	}
}

func TestReadSpendReport_RefusesAMalformedAgentAndAFailingCatalog(t *testing.T) {
	t.Parallel()
	ledger := seedAgentFixture(t)
	_, err := ReadSpendReport(context.Background(), SpendSources{Ledger: ledger}, &SpendReportRequest{TenantID: "t1", Agent: "Not An ID"})
	assert.ErrorIs(t, err, ErrConsumptionInvalid)

	boom := errors.New("catalog down")
	_, err = ReadSpendReport(context.Background(), SpendSources{Ledger: ledger, Agents: stubSpendAgents{err: boom}}, &SpendReportRequest{TenantID: "t1"})
	assert.ErrorIs(t, err, boom, "a catalog that cannot answer fails the report rather than reading as nameless Agents")
}
