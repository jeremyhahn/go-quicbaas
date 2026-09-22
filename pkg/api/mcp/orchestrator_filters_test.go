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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The faceted filter contract, over MCP
//
// THE TOOL ARGUMENT NAMES ARE THE REST PARAMETER NAMES. A model that has read
// the API docs must be able to ask the same question here and meet the same
// refusal for the same mistake, which is what these assert.
// =============================================================================

// facetCounts reads one facet key out of a list tool's decoded result.
func facetCounts(t *testing.T, decoded map[string]interface{}, key string) map[string]float64 {
	t.Helper()

	facets, ok := decoded["facets"].(map[string]interface{})
	require.True(t, ok, "facets is %T, want the counts block", decoded["facets"])
	raw, ok := facets[key].(map[string]interface{})
	require.True(t, ok, "facets[%q] is %T, want a counts map", key, facets[key])

	counts := make(map[string]float64, len(raw))
	for value, count := range raw {
		number, isNumber := count.(float64)
		require.True(t, isNumber, "facets[%q][%q] is %T, want a number", key, value, count)
		counts[value] = number
	}
	return counts
}

// filterJobFixtures are four jobs across two runners, two states and two
// daemons — enough that every narrowing under test can be wrong visibly.
func filterJobFixtures() []*orchestration.JobStatus {
	return []*orchestration.JobStatus{
		{JobID: "job-0", Runner: "sdlc", State: orchestration.JobStateCompleted, AgentID: "agent-1", StartedAt: 0},
		{JobID: "job-1", Runner: "sdlc", State: orchestration.JobStateFailed, AgentID: "agent-2", StartedAt: 1},
		{JobID: "job-2", Runner: "triage", State: orchestration.JobStateCompleted, AgentID: "agent-1", StartedAt: 2},
		{JobID: "job-3", Runner: "triage", State: orchestration.JobStateFailed, StartedAt: 3},
	}
}

// TestMCPListJobs_NarrowsOnTheDispatchedAgent proves the agent_id argument
// reaches the service and narrows the answer.
func TestMCPListJobs_NarrowsOnTheDispatchedAgent(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.listJobs = filterJobFixtures()

	decoded := listJobsResult(t, svc, map[string]interface{}{"agent_id": "agent-1"})

	require.Equal(t, float64(2), decoded["total"], "want the 2 jobs agent-1 ran")
	require.NotNil(t, svc.listJobsPageFilter, "the agent_id argument never reached the service")
	require.Equal(t, "agent-1", svc.listJobsPageFilter.AgentID)
}

// TestMCPListJobs_AnswersTheFacetCounts proves the counts travel over MCP
// under the same key REST answers with, and are taken over the set the chosen
// facet was not applied to.
func TestMCPListJobs_AnswersTheFacetCounts(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.listJobs = filterJobFixtures()

	decoded := listJobsResult(t, svc, map[string]interface{}{"runner": "sdlc"})

	require.Equal(t, float64(2), decoded["total"])
	require.Equal(t, float64(2), facetCounts(t, decoded, "runner")["triage"],
		"triage must still be counted while sdlc is selected")
	states := facetCounts(t, decoded, "state")
	require.Equal(t, float64(2), states["failed"])
	require.Equal(t, float64(2), states["completed"])
	require.Equal(t, float64(2), facetCounts(t, decoded, "agent_id")["agent-1"])
}

// TestMCPListJobs_RefusesAStateOutsideTheEnum proves a model that misspells a
// state is told so rather than handed an empty listing it would report as a
// quiet factory.
func TestMCPListJobs_RefusesAStateOutsideTheEnum(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.listJobs = filterJobFixtures()
	reg := newAgentRegistry(t, svc)
	ctx := orchestration.ContextWithTenant(context.Background(), "t1")

	_, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_list_jobs",
		map[string]interface{}{"statuses": []interface{}{"failed", "fialed"}})

	require.Error(t, err, "want a refusal naming the misspelled state")
	require.True(t, strings.Contains(err.Error(), "fialed"),
		"refusal %q does not name the offending value", err)
}

// TestMCPListJobs_AcceptsAStateTheEngineHas proves the refusal above is about
// the value and not about the parameter: the correct spelling is served.
func TestMCPListJobs_AcceptsAStateTheEngineHas(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.listJobs = filterJobFixtures()

	decoded := listJobsResult(t, svc, map[string]interface{}{
		"statuses": []interface{}{string(orchestration.JobStateFailed)},
	})

	require.Equal(t, float64(2), decoded["total"], "want the 2 failed jobs")
}

// TestMCPFleetList_NarrowsOnServedBackend proves the fleet tool gained the
// backend argument REST has had, and that it is membership rather than
// equality — a daemon serving claude as a capability must match.
func TestMCPFleetList_NarrowsOnServedBackend(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.listAgents = []*orchestration.AgentRegistration{
		{ID: "a1", Name: "one", Backend: "acp", Capabilities: []string{"claude", "codex"}, Status: orchestration.AgentStatusRunning},
		{ID: "a2", Name: "two", Backend: "codex", Status: orchestration.AgentStatusOffline},
	}
	reg := newAgentRegistry(t, svc)

	result, err := reg.executeAgentTool(context.Background(), "quicbaas_agent_fleet_list",
		map[string]interface{}{"tenant_id": "t1", "backend": "claude"})
	require.NoError(t, err)

	decoded, ok := result.(map[string]interface{})
	require.True(t, ok, "tool result is %T, want a JSON object", result)
	require.Equal(t, float64(1), decoded["total"], "want the one daemon that serves claude")
	require.NotNil(t, svc.listAgentsPageFilter, "the backend argument never reached the service")
	require.Equal(t, "claude", svc.listAgentsPageFilter.Backend)
	require.Equal(t, float64(2), facetCounts(t, decoded, "backend")["codex"],
		"both codex-serving daemons must be counted")
}

// TestMCPListSessions_CarriesTheNewNarrowings proves the four arguments the
// sessions tool gained are read and passed to the service, and that the
// service's counts come back on the wire.
func TestMCPListSessions_CarriesTheNewNarrowings(t *testing.T) {
	svc := &fleetMockAgentService{}
	var got *orchestration.ListSessionsRequest
	svc.listSessionsFn = func(_ context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
		got = req
		return &orchestration.ListSessionsResponse{Facets: orchestration.Facets{"runner": {"review": 1}}}, nil
	}
	reg := newAgentRegistry(t, svc)

	result, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_list_sessions", map[string]interface{}{
		"tenant_id":  "t1",
		"project_id": "proj-a",
		"job_id":     "job-1",
		"runner":     "review",
		"agent_id":   "agent-2",
	})
	require.NoError(t, err)
	require.NotNil(t, got, "the sessions listing was never called")
	require.Equal(t, "proj-a", got.ProjectID)
	require.Equal(t, "job-1", got.JobID)
	require.Equal(t, "review", got.Runner)
	require.Equal(t, "agent-2", got.AgentID)

	decoded, ok := result.(map[string]interface{})
	require.True(t, ok, "tool result is %T, want a JSON object", result)
	require.Equal(t, float64(1), facetCounts(t, decoded, "runner")["review"],
		"the service's counts must reach the model")
}

// TestMCPListTools_DeclareTheNewArguments proves the model is TOLD about the
// narrowings: an argument the schema omits is one no model will ever send.
func TestMCPListTools_DeclareTheNewArguments(t *testing.T) {
	reg := newAgentRegistry(t, &fleetMockAgentService{})

	want := map[string][]string{
		"quicbaas_orchestrator_list_jobs":     {"agent_id", "runner", "project_id", "statuses", "job_ids"},
		"quicbaas_orchestrator_list_sessions": {"project_id", "job_id", "runner", "agent_id", "backend", "status"},
		"quicbaas_agent_fleet_list":           {"backend", "status", "host"},
	}
	schemas := make(map[string]map[string]interface{}, len(want))
	for _, tool := range reg.ListTools() {
		if _, wanted := want[tool.Name]; !wanted {
			continue
		}
		properties, ok := tool.InputSchema["properties"].(map[string]interface{})
		require.True(t, ok, "%s: input schema has no properties object", tool.Name)
		schemas[tool.Name] = properties
	}
	for name, arguments := range want {
		properties := schemas[name]
		require.NotNil(t, properties, "tool %s is not registered", name)
		for _, argument := range arguments {
			require.Contains(t, properties, argument,
				"%s: schema omits %q, so no model will send it", name, argument)
		}
	}
}
