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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fleetMockAgentService overrides the artifact accessors so handlers that
// dereference the returned artifact don't panic on the nil default.
type fleetMockAgentService struct {
	mockAgentService
	getArtifactFn  func(ctx context.Context, tenantID, taskID, name string) (*orchestration.Artifact, error)
	listArtifactFn func(ctx context.Context, tenantID, taskID string) ([]orchestration.Artifact, error)
	registerFn     func(ctx context.Context, req *orchestration.RegisterAgentRequest) (*orchestration.RegisterAgentResponse, error)
	removeFn       func(ctx context.Context, tenantID, agentID string) error
	heartbeatFn    func(ctx context.Context, tenantID, agentID string) error
	startJobFn     func(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error)
	cancelJobFn    func(ctx context.Context, tenantID, jobID string) error
}

func (m *fleetMockAgentService) GetTaskArtifact(ctx context.Context, tenantID, taskID, name string) (*orchestration.Artifact, error) {
	if m.getArtifactFn != nil {
		return m.getArtifactFn(ctx, tenantID, taskID, name)
	}
	return &orchestration.Artifact{Name: name}, nil
}

func (m *fleetMockAgentService) ListTaskArtifacts(ctx context.Context, tenantID, taskID string) ([]orchestration.Artifact, error) {
	if m.listArtifactFn != nil {
		return m.listArtifactFn(ctx, tenantID, taskID)
	}
	return []orchestration.Artifact{{Name: "a1"}}, nil
}

func (m *fleetMockAgentService) RegisterAgent(ctx context.Context, req *orchestration.RegisterAgentRequest) (*orchestration.RegisterAgentResponse, error) {
	if m.registerFn != nil {
		return m.registerFn(ctx, req)
	}
	return &orchestration.RegisterAgentResponse{Agent: &orchestration.AgentRegistration{ID: "a1"}}, nil
}

func (m *fleetMockAgentService) RemoveAgent(ctx context.Context, tenantID, agentID string) error {
	if m.removeFn != nil {
		return m.removeFn(ctx, tenantID, agentID)
	}
	return nil
}

func (m *fleetMockAgentService) AgentHeartbeat(ctx context.Context, tenantID, agentID string) error {
	if m.heartbeatFn != nil {
		return m.heartbeatFn(ctx, tenantID, agentID)
	}
	return nil
}

func (m *fleetMockAgentService) StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	if m.startJobFn != nil {
		return m.startJobFn(ctx, req)
	}
	return &orchestration.StartJobResponse{JobID: "job-1"}, nil
}

func (m *fleetMockAgentService) CancelJob(ctx context.Context, tenantID, jobID string) error {
	if m.cancelJobFn != nil {
		return m.cancelJobFn(ctx, tenantID, jobID)
	}
	return nil
}

// =============================================================================
// Fleet + Job + Task tool happy paths
// =============================================================================

func TestExecuteAgentTool_FleetAndJobs_HappyPaths(t *testing.T) {
	reg := newAgentRegistry(t, &fleetMockAgentService{})
	ctx := orchestration.ContextWithTenant(context.Background(), "t1")

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"quicbaas_agent_fleet_register", map[string]interface{}{
			"tenant_id": "t1", "name": "agent-a", "host": "h1", "agent_type": "claude",
		}},
		{"quicbaas_agent_fleet_list", map[string]interface{}{
			"tenant_id": "t1", "status": "online", "host": "h1", "agent_type": "claude",
		}},
		{"quicbaas_agent_fleet_status", map[string]interface{}{"tenant_id": "t1", "agent_id": "a1"}},
		{"quicbaas_agent_fleet_remove", map[string]interface{}{"tenant_id": "t1", "agent_id": "a1"}},
		{"quicbaas_agent_fleet_heartbeat", map[string]interface{}{"tenant_id": "t1", "agent_id": "a1"}},
		{"quicbaas_orchestrator_start_job", map[string]interface{}{"description": "do work", "max_iterations": 5}},
		{"quicbaas_orchestrator_get_job_status", map[string]interface{}{"job_id": "job-1"}},
		{"quicbaas_orchestrator_cancel_job", map[string]interface{}{"job_id": "job-1"}},
		{"quicbaas_orchestrator_list_jobs", map[string]interface{}{}},
		{"quicbaas_orchestrator_task_create", map[string]interface{}{"subject": "fix bug", "auto_spawn": true}},
		{"quicbaas_task_get_artifact", map[string]interface{}{"task_id": "tk1", "name": "result.txt"}},
		{"quicbaas_task_list_artifacts", map[string]interface{}{"task_id": "tk1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := reg.executeAgentTool(ctx, tc.name, tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

// =============================================================================
// Required field validation
// =============================================================================

func TestExecuteAgentTool_FleetAndJobs_MissingRequired(t *testing.T) {
	reg := newAgentRegistry(t, &fleetMockAgentService{})
	ctx := context.Background()

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"quicbaas_agent_fleet_register", map[string]interface{}{}},                                            // tenant_id
		{"quicbaas_agent_fleet_register", map[string]interface{}{"tenant_id": "t1"}},                           // name
		{"quicbaas_agent_fleet_register", map[string]interface{}{"tenant_id": "t1", "name": "n"}},              // host
		{"quicbaas_agent_fleet_register", map[string]interface{}{"tenant_id": "t1", "name": "n", "host": "h"}}, // agent_type
		{"quicbaas_agent_fleet_list", map[string]interface{}{}},                                                // tenant_id
		{"quicbaas_agent_fleet_status", map[string]interface{}{"tenant_id": "t1"}},                             // agent_id
		{"quicbaas_agent_fleet_remove", map[string]interface{}{"tenant_id": "t1"}},                             // agent_id
		{"quicbaas_agent_fleet_heartbeat", map[string]interface{}{"tenant_id": "t1"}},                          // agent_id
		{"quicbaas_orchestrator_start_job", map[string]interface{}{}},                                          // description
		{"quicbaas_orchestrator_get_job_status", map[string]interface{}{}},                                     // job_id
		{"quicbaas_orchestrator_cancel_job", map[string]interface{}{}},                                         // job_id
		{"quicbaas_orchestrator_task_create", map[string]interface{}{}},                                        // subject
		{"quicbaas_task_get_artifact", map[string]interface{}{}},                                               // task_id
		{"quicbaas_task_get_artifact", map[string]interface{}{"task_id": "tk1"}},                               // name
		{"quicbaas_task_list_artifacts", map[string]interface{}{}},                                             // task_id
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reg.executeAgentTool(ctx, tc.name, tc.args)
			require.Error(t, err)
		})
	}
}

// =============================================================================
// Service unavailable (nil agent service)
// =============================================================================

func TestExecuteAgentTool_FleetAndJobs_ServiceUnavailable(t *testing.T) {
	reg := newAgentRegistry(t, nil) // Agent() returns nil
	ctx := context.Background()

	names := []string{
		"quicbaas_agent_fleet_register",
		"quicbaas_agent_fleet_list",
		"quicbaas_agent_fleet_status",
		"quicbaas_agent_fleet_remove",
		"quicbaas_agent_fleet_heartbeat",
		"quicbaas_orchestrator_start_job",
		"quicbaas_orchestrator_get_job_status",
		"quicbaas_orchestrator_cancel_job",
		"quicbaas_orchestrator_list_jobs",
		"quicbaas_orchestrator_task_create",
		"quicbaas_task_get_artifact",
		"quicbaas_task_list_artifacts",
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			_, err := reg.executeAgentTool(ctx, name, map[string]interface{}{})
			require.Error(t, err)
		})
	}
}

// =============================================================================
// Service error propagation
// =============================================================================

func TestExecuteAgentTool_FleetAndJobs_ServiceError(t *testing.T) {
	sentinel := errors.New("orch boom")
	svc := &fleetMockAgentService{
		registerFn: func(context.Context, *orchestration.RegisterAgentRequest) (*orchestration.RegisterAgentResponse, error) {
			return nil, sentinel
		},
		removeFn:    func(context.Context, string, string) error { return sentinel },
		heartbeatFn: func(context.Context, string, string) error { return sentinel },
		startJobFn: func(context.Context, *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
			return nil, sentinel
		},
		cancelJobFn: func(context.Context, string, string) error { return sentinel },
		getArtifactFn: func(context.Context, string, string, string) (*orchestration.Artifact, error) {
			return nil, sentinel
		},
		listArtifactFn: func(context.Context, string, string) ([]orchestration.Artifact, error) {
			return nil, sentinel
		},
	}
	reg := newAgentRegistry(t, svc)
	ctx := orchestration.ContextWithTenant(context.Background(), "t1")

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"quicbaas_agent_fleet_register", map[string]interface{}{"tenant_id": "t1", "name": "n", "host": "h", "agent_type": "claude"}},
		{"quicbaas_agent_fleet_remove", map[string]interface{}{"tenant_id": "t1", "agent_id": "a1"}},
		{"quicbaas_agent_fleet_heartbeat", map[string]interface{}{"tenant_id": "t1", "agent_id": "a1"}},
		{"quicbaas_orchestrator_start_job", map[string]interface{}{"description": "x"}},
		{"quicbaas_orchestrator_cancel_job", map[string]interface{}{"job_id": "j1"}},
		{"quicbaas_task_get_artifact", map[string]interface{}{"task_id": "tk1", "name": "n"}},
		{"quicbaas_task_list_artifacts", map[string]interface{}{"task_id": "tk1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reg.executeAgentTool(ctx, tc.name, tc.args)
			require.ErrorIs(t, err, sentinel)
		})
	}
}
