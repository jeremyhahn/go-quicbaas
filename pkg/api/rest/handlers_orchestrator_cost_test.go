//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// GET /api/v1/orchestrator/cost
// =============================================================================

// agentWithLedger adds a consumption ledger and an account store to the mock
// agent service so the cost handler resolves orchestration.SpendLedgerAccessor.
type agentWithLedger struct {
	*mockAgentService
	ledger   orchestration.ConsumptionLedger
	accounts orchestration.AccountStore
	projects orchestration.ProjectStore
}

func (a *agentWithLedger) ConsumptionLedger() orchestration.ConsumptionLedger { return a.ledger }
func (a *agentWithLedger) AccountStore() orchestration.AccountStore           { return a.accounts }
func (a *agentWithLedger) ProjectStore() orchestration.ProjectStore           { return a.projects }

// costBase anchors the fixture. Fixed, for the reason every ledger fixture is:
// the quota plane buckets by the hour.
var costBase = time.Date(2026, 8, 22, 10, 30, 0, 0, time.UTC)

// setupCostServer builds a server whose agent service exposes a seeded ledger.
// withLedger=false yields a service that cannot answer at all, which must be a
// 503 and never an empty report.
func setupCostServer(t *testing.T, withLedger bool) (*Server, orchestration.ConsumptionLedger, orchestration.AccountStore, func()) {
	t.Helper()
	server, _, cleanup := setupMockServer(t)

	var agent orchestration.AgentService = &mockAgentService{}
	var ledger orchestration.ConsumptionLedger
	var accounts orchestration.AccountStore
	if withLedger {
		ledger = orchestration.NewInMemoryConsumptionLedger()
		accounts = orchestration.NewInMemoryAccountStore()
		agent = &agentWithLedger{mockAgentService: &mockAgentService{}, ledger: ledger, accounts: accounts}
	}
	server.service = &MockQuicBaaSWithAgent{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agentService: agent,
	}
	return server, ledger, accounts, cleanup
}

// seedCostFixture records two turns under one job of one task of one project.
func seedCostFixture(t *testing.T, ledger orchestration.ConsumptionLedger, tenantID string) {
	t.Helper()
	ctx := context.Background()
	for i, id := range []string{"e1", "e2"} {
		require.NoError(t, ledger.Record(ctx, orchestration.ConsumptionEntry{
			EntryID: id, TenantID: tenantID,
			ProjectID: "proj-1", TaskID: "task-1", JobID: "job-1", SessionID: "sess-1", Round: i,
			AccountID: "acct-1", Engine: "acp", Model: "claude-opus-5",
			Outcome:      orchestration.ConsumptionOutcomeSuccess,
			InputTokens:  int64(10 * (i + 1)),
			OutputTokens: int64(5 * (i + 1)),
			OccurredAt:   costBase.Add(time.Duration(i) * time.Minute),
		}))
	}
}

// TestHandleOrchestratorCost_ReportsTenantSpend is the issue's acceptance
// check: the cost surface reports the tokens the turn recorder wrote, and the
// account that paid. go-quicbaas#794.
func TestHandleOrchestratorCost_ReportsTenantSpend(t *testing.T) {
	server, ledger, _, cleanup := setupCostServer(t, true)
	defer cleanup()
	seedCostFixture(t, ledger, qrdbsdk.SystemTenantID)

	w := httptest.NewRecorder()
	server.HandleOrchestratorCost(w, httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/cost", nil))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var report orchestration.SpendReport
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
	assert.Equal(t, orchestration.SpendLevelTenant, report.Level)
	assert.Equal(t, int64(45), report.TotalTokens)
	assert.Equal(t, int64(30), report.InputTokens)
	assert.Equal(t, int64(15), report.OutputTokens)
	assert.Equal(t, 2, report.Entries)
	require.Len(t, report.Accounts, 1)
	assert.Equal(t, "acct-1", report.Accounts[0].AccountID)

	// The honesty rule: nothing prices a turn, so the cost is unmeasured and
	// a renderer must show a dash rather than this zero. go-quicbaas#791.
	assert.False(t, report.CostMeasured, "no production writer prices a turn")
}

// TestHandleOrchestratorCost_NamesProjects is the go-quicbaas#1058 regression
// over REST: the By Project card listed bare project ids.
func TestHandleOrchestratorCost_NamesProjects(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()
	ctx := context.Background()
	ledger := orchestration.NewInMemoryConsumptionLedger()
	seedCostFixture(t, ledger, qrdbsdk.SystemTenantID)
	projects := orchestration.NewInMemoryProjectStore()
	require.NoError(t, projects.Create(ctx, &orchestration.Project{
		ID: "proj-1", TenantID: qrdbsdk.SystemTenantID, Name: "go-truststrap",
	}))
	server.service = &MockQuicBaaSWithAgent{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agentService: &agentWithLedger{mockAgentService: &mockAgentService{}, ledger: ledger, projects: projects},
	}

	w := httptest.NewRecorder()
	server.HandleOrchestratorCost(w, httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/cost", nil))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var report orchestration.SpendReport
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
	require.Len(t, report.Children, 1)
	assert.Equal(t, "proj-1", report.Children[0].ID)
	assert.Equal(t, "go-truststrap", report.Children[0].Name)
}

// TestHandleOrchestratorCost_DrillsDown asserts the scope narrows level by
// level and the child breakdown names the level below.
func TestHandleOrchestratorCost_DrillsDown(t *testing.T) {
	server, ledger, _, cleanup := setupCostServer(t, true)
	defer cleanup()
	seedCostFixture(t, ledger, qrdbsdk.SystemTenantID)

	cases := []struct {
		name       string
		query      string
		wantLevel  orchestration.SpendLevel
		wantChild  orchestration.SpendLevel
		wantTokens int64
	}{
		{"project", "?project_id=proj-1", orchestration.SpendLevelProject, orchestration.SpendLevelTask, 45},
		{"task", "?project_id=proj-1&task_id=task-1", orchestration.SpendLevelTask, orchestration.SpendLevelJob, 45},
		{
			"job", "?project_id=proj-1&task_id=task-1&job_id=job-1",
			orchestration.SpendLevelJob, orchestration.SpendLevelSession, 45,
		},
		{
			"round", "?project_id=proj-1&task_id=task-1&job_id=job-1&session_id=sess-1&round=1",
			orchestration.SpendLevelRound, "", 30,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			server.HandleOrchestratorCost(w, httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/cost"+tc.query, nil))
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

			var report orchestration.SpendReport
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
			assert.Equal(t, tc.wantLevel, report.Level)
			assert.Equal(t, tc.wantChild, report.ChildLevel)
			assert.Equal(t, tc.wantTokens, report.TotalTokens)
		})
	}
}

// TestHandleOrchestratorCost_ReportsTheSpendPolicy asserts the window an
// operator set is reported with live usage against it, so the page shows a cap
// rather than an iteration budget.
func TestHandleOrchestratorCost_ReportsTheSpendPolicy(t *testing.T) {
	server, ledger, accounts, cleanup := setupCostServer(t, true)
	defer cleanup()
	seedCostFixture(t, ledger, qrdbsdk.SystemTenantID)
	require.NoError(t, accounts.Create(context.Background(), &orchestration.Account{
		ID: "acct-1", TenantID: qrdbsdk.SystemTenantID, Provider: orchestration.AccountProviderClaude,
		DisplayName: "alice-work",
		Spend: orchestration.AccountSpendPolicy{Quota: orchestration.AccountQuotaPolicy{
			Windows: []orchestration.QuotaWindow{{Name: "five-hour", Size: 5 * time.Hour, MaxTokens: 1000}},
		}},
	}))

	w := httptest.NewRecorder()
	server.HandleOrchestratorCost(w, httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/cost", nil))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var report orchestration.SpendReport
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
	require.Len(t, report.Accounts, 1)
	account := report.Accounts[0]
	assert.Equal(t, orchestration.SpendPolicyMetered, account.PolicyStatus)
	assert.Equal(t, "alice-work", account.DisplayName)
	require.Len(t, account.Windows, 1)
	assert.Equal(t, "five-hour", account.Windows[0].Name)
	assert.Equal(t, int64(1000), account.Windows[0].MaxTokens)
}

// TestHandleOrchestratorCost_TenantIsolation asserts a caller naming another
// tenant reads nothing. The chain keyspace is tenant-prefixed, so this is
// structural rather than a filter someone can forget.
func TestHandleOrchestratorCost_TenantIsolation(t *testing.T) {
	server, ledger, _, cleanup := setupCostServer(t, true)
	defer cleanup()
	seedCostFixture(t, ledger, qrdbsdk.SystemTenantID)

	w := httptest.NewRecorder()
	server.HandleOrchestratorCost(w,
		httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/cost?tenant_id=someone-else", nil))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var report orchestration.SpendReport
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &report))
	assert.Zero(t, report.Entries, "another tenant's spend leaked")
	assert.Zero(t, report.TotalTokens)
}

// TestHandleOrchestratorCost_RefusalPaths asserts each way the read fails is
// distinguishable. "You asked badly" and "this cluster records no spend" must
// not both render as an empty report.
func TestHandleOrchestratorCost_RefusalPaths(t *testing.T) {
	t.Run("malformed round", func(t *testing.T) {
		server, _, _, cleanup := setupCostServer(t, true)
		defer cleanup()

		w := httptest.NewRecorder()
		server.HandleOrchestratorCost(w,
			httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/cost?round=later", nil))
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("skipped chain level", func(t *testing.T) {
		server, _, _, cleanup := setupCostServer(t, true)
		defer cleanup()

		w := httptest.NewRecorder()
		server.HandleOrchestratorCost(w,
			httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/cost?task_id=task-1", nil))
		assert.Equal(t, http.StatusBadRequest, w.Code,
			"the chain is a path: a task without a project is not a prefix of anything")
	})

	t.Run("no ledger wired", func(t *testing.T) {
		server, _, _, cleanup := setupCostServer(t, false)
		defer cleanup()

		w := httptest.NewRecorder()
		server.HandleOrchestratorCost(w, httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/cost", nil))
		assert.Equal(t, http.StatusServiceUnavailable, w.Code,
			"a deployment that records no spend must say so, not report zero")
	})
}

// TestHandleOrchestratorCost_LedgerAccessorWithNoLedger asserts a service that
// implements the accessor but has nothing wired behind it is still a 503. The
// type assertion succeeding is not the same as the capability existing.
func TestHandleOrchestratorCost_LedgerAccessorWithNoLedger(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()
	server.service = &MockQuicBaaSWithAgent{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agentService: &agentWithLedger{mockAgentService: &mockAgentService{}},
	}

	w := httptest.NewRecorder()
	server.HandleOrchestratorCost(w, httptest.NewRequest(http.MethodGet, "/api/v1/orchestrator/cost", nil))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
