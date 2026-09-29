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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// runOrchCostWith runs the cost command with args against mock and returns
// the request it sent.
func runOrchCostWith(t *testing.T, args ...string) (*orchestratorSDK.GetSpendReportRequest, error) {
	t.Helper()
	mock := &mockOrchestratorClient{}
	var sent *orchestratorSDK.GetSpendReportRequest
	setSpendReportFn(mock, func(_ context.Context, req *orchestratorSDK.GetSpendReportRequest) (*orchestratorSDK.GetSpendReportResponse, error) {
		sent = req
		return &orchestratorSDK.GetSpendReportResponse{Level: orchestratorSDK.SpendLevelTenant}, nil
	})
	setMockOrchestratorFactory(mock)
	t.Cleanup(func() { delete(mockSessionOverrideRegistry, mock) })
	orchOutputFmt = "json"
	cmd := newOrchCostCommand()
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return sent, cmd.Execute()
}

func TestOrchCostSendsTheScopeAndHost(t *testing.T) {
	sent, err := runOrchCostWith(t, "--project", "p1", "--task", "w1", "--job", "j1", "--session", "s1",
		"--round", "0", "--from", "2026-09-01T00:00:00Z", "--host", "gpu-host:9091")
	require.NoError(t, err)
	require.NotNil(t, sent)
	assert.Equal(t, "p1", sent.ProjectID)
	assert.Equal(t, "s1", sent.SessionID)
	assert.True(t, sent.RoundSet, "round 0 is a level; the flag's presence selects it")
	assert.Equal(t, "gpu-host:9091", sent.Host)
	require.NotNil(t, sent.From)
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), *sent.From)
	assert.Nil(t, sent.To)

	bare, err := runOrchCostWith(t)
	require.NoError(t, err)
	assert.False(t, bare.RoundSet)
	assert.Empty(t, bare.Host)
}

func TestOrchCostRefusesAMalformedInstant(t *testing.T) {
	sent, err := runOrchCostWith(t, "--from", "yesterday")
	require.Error(t, err)
	assert.Nil(t, sent, "nothing is sent for a request the command cannot read")
}

func TestFormatSpendReport(t *testing.T) {
	report := &orchestratorSDK.GetSpendReportResponse{
		Level: orchestratorSDK.SpendLevelTenant, TenantID: "acme", Host: "gpu-host:9091",
		SpendTotals: orchestratorSDK.SpendTotals{
			Entries: 3, TotalTokens: 42, EquivalentMicros: 1_500_000, PricedEntries: 2, UnpricedEntries: 1,
			EnergyMilliWh: 2500, ElectricityEntries: 2, MeasuredEnergyEntries: 1,
		},
		Hosts: []orchestratorSDK.SpendHost{
			{Host: "gpu-host:9091", SpendTotals: orchestratorSDK.SpendTotals{Entries: 2, TotalTokens: 40}},
			{Unattributed: true, SpendTotals: orchestratorSDK.SpendTotals{Entries: 1, TotalTokens: 2}},
		},
	}

	var table bytes.Buffer
	require.NoError(t, formatSpendReport(report, "table", &table))
	out := table.String()
	assert.Contains(t, out, "$1.5000+", "a figure beside unpriced turns is a floor")
	assert.Contains(t, out, "2.50 Wh (1 of 2 measured)")
	assert.Contains(t, out, "(no host)")
	assert.Contains(t, out, "gpu-host:9091")

	var js bytes.Buffer
	require.NoError(t, formatSpendReport(report, "json", &js))
	var decoded orchestratorSDK.GetSpendReportResponse
	require.NoError(t, json.Unmarshal(js.Bytes(), &decoded))
	assert.Len(t, decoded.Hosts, 2)

	assert.Error(t, formatSpendReport(report, "xml", &bytes.Buffer{}))
}

func TestCostMoneyAndEnergyAreHonestAboutAbsence(t *testing.T) {
	assert.Equal(t, "-", costMoney(0, 0, 3), "nothing priced is a dash, never $0")
	assert.Equal(t, "$0.0000", costMoney(0, 1, 0))
	assert.Equal(t, "-", costEnergy(orchestratorSDK.SpendTotals{}))
}

func TestOrchCostSendsTheAgent(t *testing.T) {
	sent, err := runOrchCostWith(t, "--agent", "claude-coder")
	require.NoError(t, err)
	require.NotNil(t, sent)
	assert.Equal(t, "claude-coder", sent.Agent)

	bare, err := runOrchCostWith(t)
	require.NoError(t, err)
	assert.Empty(t, bare.Agent)
}

func TestFormatSpendReport_ShowsAgentsAndTheAgentsLimits(t *testing.T) {
	freesAt := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	report := &orchestratorSDK.GetSpendReportResponse{
		Level: orchestratorSDK.SpendLevelTenant, TenantID: "acme", Agent: "claude-coder",
		Agents: []orchestratorSDK.SpendAgent{
			{AgentID: "claude-coder", Name: "Claude coder", SpendTotals: orchestratorSDK.SpendTotals{Entries: 2, TotalTokens: 40}},
			{Unattributed: true, SpendTotals: orchestratorSDK.SpendTotals{Entries: 1, TotalTokens: 2}},
		},
		AgentPolicyStatus: "metered",
		AgentWindows: []orchestratorSDK.SpendWindowStatus{
			{Name: "daily", MaxTokens: 30, UsedTokens: 40, MaxEquivalentMicros: 2_000_000, UsedEquivalentMicros: 500_000, Exhausted: true, FreesAt: &freesAt},
		},
	}
	var table bytes.Buffer
	require.NoError(t, formatSpendReport(report, "table", &table))
	out := table.String()
	assert.Contains(t, out, "Agent:")
	assert.Contains(t, out, "Claude coder (claude-coder)")
	assert.Contains(t, out, "(no agent: raw backend)")
	assert.Contains(t, out, "40 of 30 tokens, $0.50 of $2.00 — spent, frees up at 2026-09-28T12:00:00Z")
}

func TestCostWindowLabel_AWindowWithHeadroomIsNotSpent(t *testing.T) {
	label := costWindowLabel(orchestratorSDK.SpendWindowStatus{Name: "daily", MaxTokens: 100, UsedTokens: 10})
	assert.Equal(t, "10 of 100 tokens", label)
	assert.Equal(t, "claude-coder", costAgentLabel(orchestratorSDK.SpendAgent{AgentID: "claude-coder"}))
}
