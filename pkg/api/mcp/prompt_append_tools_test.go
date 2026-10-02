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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestOrchestratorTools_CarryPromptAppend(t *testing.T) {
	var spawn *orchestration.SpawnSessionRequest
	var start *orchestration.StartJobRequest
	svc := &fleetMockAgentService{
		mockAgentService: mockAgentService{
			spawnFn: func(_ context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
				spawn = req
				return &orchestration.SpawnSessionResponse{SessionID: "sess-1"}, nil
			},
		},
		startJobFn: func(_ context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
			start = req
			return &orchestration.StartJobResponse{JobID: "job-1"}, nil
		},
	}
	reg := newFleetRegistry(t, svc)
	ctx := orchestration.ContextWithTenant(tenantctx.WithTenant(context.Background(), "t1"), "t1")

	_, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_spawn", map[string]interface{}{
		"tenant_id": "t1", "agent_id": "claude-coder", "project_id": "proj-1", "prompt_append": "spawn material",
	})
	require.NoError(t, err)
	require.NotNil(t, spawn)
	assert.Equal(t, "spawn material", spawn.PromptAppend)

	_, err = reg.executeAgentTool(ctx, "quicbaas_orchestrator_start_job", map[string]interface{}{
		"description": "d", "prompt_append": "job material",
	})
	require.NoError(t, err)
	require.NotNil(t, start)
	assert.Equal(t, "job material", start.PromptAppend)

	creator := &capturingTaskCreator{mockAgentService: &mockAgentService{}}
	_, err = newFleetRegistry(t, creator).executeAgentTool(ctx, "quicbaas_orchestrator_task_create", map[string]interface{}{
		"tenant_id": "t1", "subject": "Fix it", "prompt_append": "task material",
	})
	require.NoError(t, err)
	require.NotNil(t, creator.got)
	assert.Equal(t, "task material", creator.got.PromptAppend)
}

func TestOrchestratorTools_WithoutPromptAppendCarryNone(t *testing.T) {
	creator := &capturingTaskCreator{mockAgentService: &mockAgentService{}}
	_, err := newFleetRegistry(t, creator).executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_task_create", map[string]interface{}{
		"tenant_id": "t1", "subject": "Fix it",
	})
	require.NoError(t, err)
	assert.Empty(t, creator.got.PromptAppend)
}

// TestSDLCBindingSchema_DescribesEverySlotsAppendAndItsAgent keeps the bind
// tool's schema on the Agent model: slots name Agents, and each carries its
// own run-time prompt material.
func TestSDLCBindingSchema_DescribesEverySlotsAppendAndItsAgent(t *testing.T) {
	properties := sdlcBindingSchema["properties"].(map[string]interface{})
	for _, key := range []string{"worker_prompt_append", "triage_prompt_append", "worker", "triage"} {
		if _, ok := properties[key]; !ok {
			t.Fatalf("the binding schema lacks %q", key)
		}
	}
	worker := properties["worker"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := worker["agent_id"]; !ok {
		t.Fatal("the worker slot must be described as an Agent reference")
	}
	seat := sdlcReviewerSchema["properties"].(map[string]interface{})
	for _, key := range []string{"agent_id", "prompt_append"} {
		if _, ok := seat[key]; !ok {
			t.Fatalf("the reviewer seat schema lacks %q", key)
		}
	}
	if _, ok := seat["backend"]; ok {
		t.Fatal("a reviewer seat names an Agent, not a backend")
	}
}

func TestSDLCReviewerSetTool_SaysItNeverCancels(t *testing.T) {
	for _, tool := range newSDLCToolRegistry(t, true).ListTools() {
		if tool.Name == "sdlc_reviewer_set" {
			assert.True(t, strings.Contains(tool.Description, "never cancels"), tool.Description)
			return
		}
	}
	t.Fatal("sdlc_reviewer_set is not registered")
}
