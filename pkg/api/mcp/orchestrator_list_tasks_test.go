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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const listTasksToolName = "quicbaas_orchestrator_list_tasks"

// taskListMockAgentService answers the task listing from a fixture and
// records the filter and page the tool handed the service.
type taskListMockAgentService struct {
	fleetMockAgentService
	tasks      []*orchestration.Task
	pageCalls  int
	pageFilter *orchestration.TaskFilter
	page       *orchestration.Page
}

func (m *taskListMockAgentService) ListTasksPage(_ context.Context, _ string, filter *orchestration.TaskFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.Task], error) {
	m.pageCalls++
	m.pageFilter = filter
	m.page = page
	return orchestration.ApplyFacetedPage(m.tasks, page, orchestration.TaskPageSpec(),
		orchestration.TaskFacetSpec(), filter.MatchesFacets)
}

// taskListFixtures are two completed and two failed tasks, so a status
// narrowing and the counts it leaves are both visible.
func taskListFixtures() []*orchestration.Task {
	return []*orchestration.Task{
		{ID: "tk1", Subject: "one", Status: orchestration.TaskStatusCompleted, CreatedAt: 1},
		{ID: "tk2", Subject: "two", Status: orchestration.TaskStatusFailed, CreatedAt: 2},
		{ID: "tk3", Subject: "three", Status: orchestration.TaskStatusCompleted, CreatedAt: 3},
		{ID: "tk4", Subject: "four", Status: orchestration.TaskStatusFailed, CreatedAt: 4},
	}
}

func listTasksCall(t *testing.T, svc *taskListMockAgentService, args map[string]interface{}) (interface{}, error) {
	t.Helper()

	reg := newFleetRegistry(t, svc)
	ctx := orchestration.ContextWithTenant(tenantctx.WithSystem(context.Background()), "t1")
	return reg.executeAgentTool(ctx, listTasksToolName, args)
}

// TestMCPListTasks_CarriesEveryFilterToTheService proves each argument the
// REST query string carries reaches ListTasksPage under the same field.
func TestMCPListTasks_CarriesEveryFilterToTheService(t *testing.T) {
	svc := &taskListMockAgentService{tasks: taskListFixtures()}

	_, err := listTasksCall(t, svc, map[string]interface{}{
		"status":      "pending",
		"team_name":   "core",
		"assigned_to": "agent-a",
		"created_by":  "alice",
		"project_id":  "p1",
		"statuses":    []interface{}{"failed", "blocked"},
		"auto_spawn":  true,
		"agent_id":    "node-1",
		"job_id":      "job-9",
		"source_kind": "issue",
		"source_ref":  "repo#7",
		"sort":        "subject",
		"q":           "fo",
	})
	require.NoError(t, err)

	got := svc.pageFilter
	require.NotNil(t, got, "the filter never reached the service")
	require.Equal(t, orchestration.TaskStatusPending, got.Status)
	require.Equal(t, "core", got.TeamName)
	require.Equal(t, "agent-a", got.AssignedTo)
	require.Equal(t, "alice", got.CreatedBy)
	require.Equal(t, "p1", got.ProjectID)
	require.Equal(t, []orchestration.TaskStatus{orchestration.TaskStatusFailed, orchestration.TaskStatusBlocked}, got.Statuses)
	require.NotNil(t, got.AutoSpawn)
	require.True(t, *got.AutoSpawn)
	require.Equal(t, "node-1", got.AgentID)
	require.Equal(t, "job-9", got.JobID)
	require.Equal(t, orchestration.TaskSourceKind("issue"), got.SourceKind)
	require.Equal(t, "repo#7", got.SourceRef)
	require.Equal(t, "subject", svc.page.Sort)
	require.Equal(t, "fo", svc.page.Query)
}

// TestMCPListTasks_PagesAndCountsFacets proves the tool answers with the
// envelope REST does: one page, the matching total, the next offset, and the
// status counts taken before the status narrowing.
func TestMCPListTasks_PagesAndCountsFacets(t *testing.T) {
	svc := &taskListMockAgentService{tasks: taskListFixtures()}

	result, err := listTasksCall(t, svc, map[string]interface{}{
		"statuses": []interface{}{"failed"},
		"limit":    1,
	})
	require.NoError(t, err)
	decoded, ok := result.(map[string]interface{})
	require.True(t, ok, "tool result is %T, want a JSON object", result)

	tasks, ok := decoded["tasks"].([]interface{})
	require.True(t, ok, "tasks is %T, want a list", decoded["tasks"])
	require.Len(t, tasks, 1)
	require.Equal(t, float64(1), decoded["count"])
	require.Equal(t, float64(2), decoded["total"], "want the 2 failed tasks")
	require.Equal(t, float64(1), decoded["next_offset"])
	statuses := facetCounts(t, decoded, "status")
	require.Equal(t, float64(2), statuses["completed"], "completed must still be counted while failed is selected")
	require.Equal(t, float64(2), statuses["failed"])
}

// TestMCPListTasks_RefusesAStatusOutsideTheEnum proves a misspelt status is
// refused before the service is asked, as REST refuses it.
func TestMCPListTasks_RefusesAStatusOutsideTheEnum(t *testing.T) {
	svc := &taskListMockAgentService{tasks: taskListFixtures()}

	_, err := listTasksCall(t, svc, map[string]interface{}{"statuses": []interface{}{"failed", "fialed"}})

	var invalid *orchestration.InvalidFilterParamError
	require.True(t, errors.As(err, &invalid), "error = %v, want *InvalidFilterParamError", err)
	require.Equal(t, "statuses", invalid.Param)
	require.Zero(t, svc.pageCalls, "a refused filter must not reach the service")
}

// TestMCPListTasks_RefusesAnUnknownSortField proves the paging allow-list
// refusal travels over MCP.
func TestMCPListTasks_RefusesAnUnknownSortField(t *testing.T) {
	svc := &taskListMockAgentService{tasks: taskListFixtures()}

	_, err := listTasksCall(t, svc, map[string]interface{}{"sort": "runner"})

	var unknown *orchestration.UnknownSortFieldError
	require.True(t, errors.As(err, &unknown), "error = %v, want *UnknownSortFieldError", err)
	require.Equal(t, "runner", unknown.Field)
}
