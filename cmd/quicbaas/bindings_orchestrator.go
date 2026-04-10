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

//go:build desktop

package main

import (
	"context"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// OrchestratorBinding wraps the SDK orchestrator client for Wails IPC,
// exposing orchestration operations to the frontend.
type OrchestratorBinding struct {
	logger *logger.Logger
	conn   *ConnectionBinding
	ctx    context.Context
}

// NewOrchestratorBinding creates a new OrchestratorBinding that uses the
// given ConnectionBinding for SDK client access.
func NewOrchestratorBinding(log *logger.Logger, conn *ConnectionBinding) *OrchestratorBinding {
	return &OrchestratorBinding{
		logger: log,
		conn:   conn,
	}
}

// startup is called by Wails when the application starts.
func (b *OrchestratorBinding) startup(ctx context.Context) {
	b.ctx = ctx
}

// shutdown is called by Wails when the application is closing.
func (b *OrchestratorBinding) shutdown(ctx context.Context) {
	// No resources to release; the connection is managed by ConnectionBinding.
}

// ListJobs returns all orchestration jobs for the given tenant.
func (b *OrchestratorBinding) ListJobs(tenantID string) ([]map[string]interface{}, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListJobs(b.ctx, &orchestratorSDK.ListJobsRequest{
		TenantID: tenantID,
	})
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.ListJobs", err)
	}

	result := make([]map[string]interface{}, 0, len(resp.Jobs))
	for _, job := range resp.Jobs {
		result = append(result, map[string]interface{}{
			"job_id":      job.JobID,
			"state":       job.State,
			"iteration":   job.Iteration,
			"task_count":  job.TaskCount,
			"completed":   job.Completed,
			"failed":      job.Failed,
			"description": job.Description,
			"started_at":  job.StartedAt,
			"updated_at":  job.UpdatedAt,
			"error":       job.Error,
			"event_count": job.EventCount,
		})
	}

	return result, nil
}

// GetJobStatus returns the status of a specific orchestration job.
func (b *OrchestratorBinding) GetJobStatus(tenantID, jobID string) (map[string]interface{}, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	status, err := client.Orchestrator().GetJobStatus(b.ctx, &orchestratorSDK.GetJobStatusRequest{
		TenantID: tenantID,
		JobID:    jobID,
	})
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.GetJobStatus", err)
	}

	return map[string]interface{}{
		"job_id":      status.JobID,
		"state":       status.State,
		"iteration":   status.Iteration,
		"task_count":  status.TaskCount,
		"completed":   status.Completed,
		"failed":      status.Failed,
		"description": status.Description,
		"started_at":  status.StartedAt,
		"updated_at":  status.UpdatedAt,
		"error":       status.Error,
	}, nil
}

// StartJob starts a new autonomous orchestration job.
func (b *OrchestratorBinding) StartJob(tenantID, description, workflowName, leadBackend, leadModel string) (map[string]interface{}, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().StartJob(b.ctx, &orchestratorSDK.StartJobRequest{
		TenantID:     tenantID,
		Description:  description,
		WorkflowName: workflowName,
		LeadBackend:  leadBackend,
		LeadModel:    leadModel,
	})
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.StartJob", err)
	}

	return map[string]interface{}{
		"job_id":    resp.JobID,
		"timestamp": resp.Timestamp,
	}, nil
}

// CancelJob cancels a running orchestration job.
func (b *OrchestratorBinding) CancelJob(tenantID, jobID string) error {
	client, err := b.conn.loadClient()
	if err != nil {
		return err
	}

	_, err = client.Orchestrator().CancelJob(b.ctx, &orchestratorSDK.CancelJobRequest{
		TenantID: tenantID,
		JobID:    jobID,
	})
	if err != nil {
		return NewGUITransportError("OrchestratorBinding.CancelJob", err)
	}

	return nil
}

// ListAgents returns all registered agents for the given tenant.
func (b *OrchestratorBinding) ListAgents(tenantID string) ([]map[string]interface{}, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListAgents(b.ctx, &orchestratorSDK.ListAgentsRequest{
		TenantID: tenantID,
	})
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.ListAgents", err)
	}

	result := make([]map[string]interface{}, 0, len(resp.Agents))
	for _, agent := range resp.Agents {
		result = append(result, map[string]interface{}{
			"id":            agent.ID,
			"tenant_id":     agent.TenantID,
			"name":          agent.Name,
			"host":          agent.Host,
			"backend":       agent.Backend,
			"model":         agent.Model,
			"agent_type":    agent.AgentType,
			"status":        agent.Status,
			"work_dir":      agent.WorkDir,
			"last_seen":     agent.LastSeen,
			"registered_at": agent.RegisteredAt,
			"version":       agent.Version,
			"metadata":      agent.Metadata,
		})
	}

	return result, nil
}

// GetAgent returns a specific agent by ID.
func (b *OrchestratorBinding) GetAgent(tenantID, agentID string) (map[string]interface{}, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	agent, err := client.Orchestrator().GetAgent(b.ctx, &orchestratorSDK.GetAgentRequest{
		TenantID: tenantID,
		AgentID:  agentID,
	})
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.GetAgent", err)
	}

	return map[string]interface{}{
		"id":            agent.ID,
		"tenant_id":     agent.TenantID,
		"name":          agent.Name,
		"host":          agent.Host,
		"backend":       agent.Backend,
		"model":         agent.Model,
		"agent_type":    agent.AgentType,
		"status":        agent.Status,
		"work_dir":      agent.WorkDir,
		"last_seen":     agent.LastSeen,
		"registered_at": agent.RegisteredAt,
		"version":       agent.Version,
		"metadata":      agent.Metadata,
	}, nil
}

// ListSessions returns all active sessions, optionally filtered by backend.
func (b *OrchestratorBinding) ListSessions(backend string) ([]map[string]interface{}, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListSessions(b.ctx, &orchestratorSDK.ListSessionsRequest{
		Backend: backend,
	})
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.ListSessions", err)
	}

	result := make([]map[string]interface{}, 0, len(resp.Sessions))
	for _, session := range resp.Sessions {
		result = append(result, map[string]interface{}{
			"session_id":   session.SessionID,
			"backend":      session.Backend,
			"status":       session.Status,
			"container_id": session.ContainerID,
			"created_at":   session.CreatedAt,
			"last_active":  session.LastActive,
		})
	}

	return result, nil
}

// ListTasks returns all tasks for the given tenant.
func (b *OrchestratorBinding) ListTasks(tenantID string) ([]map[string]interface{}, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListTasks(b.ctx, &orchestratorSDK.ListTasksRequest{
		TenantID: tenantID,
	})
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.ListTasks", err)
	}

	result := make([]map[string]interface{}, 0, len(resp.Tasks))
	for _, task := range resp.Tasks {
		result = append(result, map[string]interface{}{
			"id":           task.ID,
			"tenant_id":    task.TenantID,
			"team_name":    task.TeamName,
			"subject":      task.Subject,
			"description":  task.Description,
			"status":       task.Status,
			"assigned_to":  task.AssignedTo,
			"dependencies": task.Dependencies,
			"created_at":   task.CreatedAt,
			"claimed_at":   task.ClaimedAt,
			"completed_at": task.CompletedAt,
			"version":      task.Version,
			"metadata":     task.Metadata,
		})
	}

	return result, nil
}

// GetTask returns a specific task by ID.
func (b *OrchestratorBinding) GetTask(tenantID, taskID string) (map[string]interface{}, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	task, err := client.Orchestrator().GetTask(b.ctx, &orchestratorSDK.GetTaskRequest{
		TenantID: tenantID,
		TaskID:   taskID,
	})
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.GetTask", err)
	}

	return map[string]interface{}{
		"id":           task.ID,
		"tenant_id":    task.TenantID,
		"team_name":    task.TeamName,
		"subject":      task.Subject,
		"description":  task.Description,
		"status":       task.Status,
		"assigned_to":  task.AssignedTo,
		"dependencies": task.Dependencies,
		"created_at":   task.CreatedAt,
		"claimed_at":   task.ClaimedAt,
		"completed_at": task.CompletedAt,
		"version":      task.Version,
		"metadata":     task.Metadata,
	}, nil
}

// ListBackends returns all available orchestration backends.
func (b *OrchestratorBinding) ListBackends() ([]map[string]interface{}, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListBackends(b.ctx)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.ListBackends", err)
	}

	result := make([]map[string]interface{}, 0, len(resp.Backends))
	for _, backend := range resp.Backends {
		result = append(result, map[string]interface{}{
			"name":      backend.Name,
			"type":      backend.Type,
			"status":    backend.Status,
			"available": backend.Available,
		})
	}

	return result, nil
}

// HealthCheck returns the orchestrator health check report.
func (b *OrchestratorBinding) HealthCheck() (map[string]interface{}, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	report, err := client.Orchestrator().Health(b.ctx)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.HealthCheck", err)
	}

	return map[string]interface{}{
		"healthy":    report.Healthy,
		"status":     report.Status,
		"components": report.Components,
		"timestamp":  report.Timestamp,
	}, nil
}

// SubscribeJobEvents starts a background goroutine that reads real-time events
// for the specified job and emits them to the Wails frontend via runtime events.
// The goroutine terminates when the event channel closes or the context is cancelled.
func (b *OrchestratorBinding) SubscribeJobEvents(tenantID, jobID string) error {
	client, err := b.conn.loadClient()
	if err != nil {
		return err
	}

	events, err := client.Orchestrator().SubscribeJobEvents(b.ctx, &orchestratorSDK.SubscribeJobEventsRequest{
		TenantID: tenantID,
		JobID:    jobID,
	})
	if err != nil {
		return NewGUITransportError("OrchestratorBinding.SubscribeJobEvents", err)
	}

	go func() {
		for {
			select {
			case <-b.ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				wailsRuntime.EventsEmit(b.ctx, "job-event", map[string]interface{}{
					"job_id":    event.JobID,
					"type":      event.Type,
					"state":     event.State,
					"message":   event.Message,
					"iteration": event.Iteration,
					"metadata":  event.Metadata,
					"timestamp": event.Timestamp,
				})
			}
		}
	}()

	return nil
}
