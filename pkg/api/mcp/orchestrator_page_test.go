//go:build mcp

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// pagedJobFixtures builds n jobs, oldest first, alternating runners.
func pagedJobFixtures(n int) []*orchestration.JobStatus {
	jobs := make([]*orchestration.JobStatus, 0, n)
	for i := range n {
		runner := "sdlc"
		if i%2 == 1 {
			runner = "triage"
		}
		jobs = append(jobs, &orchestration.JobStatus{
			JobID:       "job-" + strconv.Itoa(i),
			Description: "attempt " + strconv.Itoa(i),
			Runner:      runner,
			State:       orchestration.JobStateCompleted,
			StartedAt:   int64(i),
		})
	}
	return jobs
}

// listJobsResult runs the tool and returns its decoded result object.
func listJobsResult(t *testing.T, svc orchestration.AgentService, args map[string]interface{}) map[string]interface{} {
	t.Helper()

	reg := newFleetRegistry(t, svc)
	ctx := orchestration.ContextWithTenant(tenantctx.WithSystem(context.Background()), "t1")
	result, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_list_jobs", args)
	require.NoError(t, err)

	decoded, ok := result.(map[string]interface{})
	require.True(t, ok, "tool result is %T, want a JSON object", result)
	return decoded
}

// TestMCPListJobs_PagesAndCounts proves the MCP tool answers with the same
// envelope REST does: one page of records, the matching total, and where the
// next page starts.
func TestMCPListJobs_PagesAndCounts(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.listJobs = pagedJobFixtures(5)

	decoded := listJobsResult(t, svc, map[string]interface{}{"limit": 2})

	jobs, ok := decoded["jobs"].([]interface{})
	require.True(t, ok, "jobs is %T, want a list", decoded["jobs"])
	require.Len(t, jobs, 2)
	require.Equal(t, float64(5), decoded["total"])
	require.Equal(t, float64(2), decoded["limit"])
	require.Equal(t, float64(0), decoded["offset"])
	require.Equal(t, float64(2), decoded["next_offset"])

	last := listJobsResult(t, svc, map[string]interface{}{"limit": 2, "offset": 4})
	require.NotContains(t, last, "next_offset", "the last page must carry no next offset")
}

// TestMCPListJobs_RefusesAnUnknownSortField proves the allow-list refusal
// reaches this transport as an error rather than an unordered answer.
func TestMCPListJobs_RefusesAnUnknownSortField(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.listJobs = pagedJobFixtures(3)
	reg := newFleetRegistry(t, svc)
	ctx := orchestration.ContextWithTenant(tenantctx.WithSystem(context.Background()), "t1")

	_, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_list_jobs",
		map[string]interface{}{"sort": "subject"})

	require.Error(t, err)
	var unknown *orchestration.UnknownSortFieldError
	require.True(t, errors.As(err, &unknown), "error = %v, want *UnknownSortFieldError", err)
	require.Equal(t, "subject", unknown.Field)
}

// TestMCPListJobs_SearchComposesWithTheRunnerFilter proves the tool's new
// narrowings reach the service and compose with the search.
func TestMCPListJobs_SearchComposesWithTheRunnerFilter(t *testing.T) {
	svc := &fleetMockAgentService{}
	svc.listJobs = pagedJobFixtures(6)

	decoded := listJobsResult(t, svc, map[string]interface{}{
		"runner": "sdlc",
		"q":      "attempt 4",
	})

	require.Equal(t, float64(1), decoded["total"])
	// The runner is a COUNTED narrowing now, so it runs in the page rather
	// than in the store read — listJobsFilter sees only what survives into
	// the listing. What the transport must be held to is the filter it built.
	require.NotNil(t, svc.listJobsPageFilter, "the runner filter never reached the service")
	require.Equal(t, "sdlc", svc.listJobsPageFilter.Runner)
}

// TestMCPListJobs_DeclaresThePagingArguments proves the tool's schema offers
// the shared parameters, so a model can page without guessing.
func TestMCPListJobs_DeclaresThePagingArguments(t *testing.T) {
	reg := newFleetRegistry(t, &fleetMockAgentService{})

	var schema map[string]interface{}
	for _, tool := range reg.ListTools() {
		if tool.Name == "quicbaas_orchestrator_list_jobs" {
			schema = tool.InputSchema
		}
	}
	require.NotNil(t, schema, "the list_jobs tool is not registered")

	properties, ok := schema["properties"].(map[string]interface{})
	require.True(t, ok, "properties is %T, want a map", schema["properties"])
	for _, name := range []string{"limit", "offset", "sort", "q", "runner", "project_id", "statuses"} {
		require.Contains(t, properties, name)
	}
}
