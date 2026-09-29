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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// capturingTaskCreator records the CreateTaskRequest the tool built.
type capturingTaskCreator struct {
	*mockAgentService
	got *orchestration.CreateTaskRequest
}

func (c *capturingTaskCreator) CreateTask(_ context.Context, req *orchestration.CreateTaskRequest) (*orchestration.Task, error) {
	c.got = req
	return &orchestration.Task{ID: "task-1", Status: orchestration.TaskStatusPending}, nil
}

// TestExecuteOrchestratorTaskCreate_CarriesWhatRESTCarries is X6's parity
// gap: the MCP tool now carries the project, the source, the metadata, the
// dependencies, the priority, the attempt budget and a park, as the REST
// body does — the adoption workaround used to be REST-only.
func TestExecuteOrchestratorTaskCreate_CarriesWhatRESTCarries(t *testing.T) {
	svc := &capturingTaskCreator{mockAgentService: &mockAgentService{}}
	_, err := newFleetRegistry(t, svc).executeAgentTool(context.Background(), "quicbaas_orchestrator_task_create", map[string]interface{}{
		"tenant_id": "t1", "subject": "Fix it", "runner": "sdlc", "auto_spawn": true,
		"project_id":     "proj-1",
		"source":         map[string]interface{}{"kind": "forge_issue", "ref": "acme/api#141", "url": "http://forge/141"},
		"metadata":       map[string]interface{}{"issue_number": "141"},
		"dependencies":   []interface{}{"task-0"},
		"priority":       float64(7),
		"attempt_budget": float64(4),
		"park":           map[string]interface{}{"kind": "needs_info", "message": "which curve?"},
	})
	require.NoError(t, err)
	req := svc.got
	require.NotNil(t, req)
	assert.Equal(t, "proj-1", req.ProjectID)
	require.NotNil(t, req.Source)
	assert.Equal(t, orchestration.TaskSourceRef{Kind: "forge_issue", Ref: "acme/api#141", URL: "http://forge/141"}, *req.Source)
	assert.Equal(t, map[string]string{"issue_number": "141"}, req.Metadata)
	assert.Equal(t, []string{"task-0"}, req.Dependencies)
	assert.Equal(t, 7, req.Priority)
	assert.Equal(t, 4, req.AttemptBudget)
	require.NotNil(t, req.Park)
	assert.Equal(t, orchestration.TaskBlockKindNeedsInfo, req.Park.Kind)
	assert.Equal(t, "which curve?", req.Park.Message)
}

// TestExecuteOrchestratorTaskCreate_RefusesAMalformedObject: a source or a
// park that is not an object is refused, never silently dropped.
func TestExecuteOrchestratorTaskCreate_RefusesAMalformedObject(t *testing.T) {
	for _, field := range []string{"source", "park"} {
		svc := &capturingTaskCreator{mockAgentService: &mockAgentService{}}
		_, err := newFleetRegistry(t, svc).executeAgentTool(context.Background(), "quicbaas_orchestrator_task_create", map[string]interface{}{
			"tenant_id": "t1", "subject": "Fix it", field: "acme/api#141",
		})
		require.Error(t, err, field)
		assert.Nil(t, svc.got, "%s: a malformed request reached the engine", field)
	}
}
