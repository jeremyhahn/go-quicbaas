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
//
// Every binding method accepts the SDK request struct directly so Wails
// can marshal the TypeScript object into the Go value without an extra
// translation layer. This matches how the Svelte frontend calls the
// bindings (`orchestratorBinding().ListAgents(req)`) and keeps the wire
// contract identical to the SDK interface.
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
func (b *OrchestratorBinding) ListJobs(req *orchestratorSDK.ListJobsRequest) ([]map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.ListJobs")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListJobs(b.ctx, req)
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
func (b *OrchestratorBinding) GetJobStatus(req *orchestratorSDK.GetJobStatusRequest) (map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.GetJobStatus")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	status, err := client.Orchestrator().GetJobStatus(b.ctx, req)
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
func (b *OrchestratorBinding) StartJob(req *orchestratorSDK.StartJobRequest) (map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.StartJob")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().StartJob(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.StartJob", err)
	}

	return map[string]interface{}{
		"job_id":    resp.JobID,
		"timestamp": resp.Timestamp,
	}, nil
}

// CancelJob cancels a running orchestration job.
func (b *OrchestratorBinding) CancelJob(req *orchestratorSDK.CancelJobRequest) error {
	if req == nil {
		return NewGUIRequestRequiredError("OrchestratorBinding.CancelJob")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return err
	}

	_, err = client.Orchestrator().CancelJob(b.ctx, req)
	if err != nil {
		return NewGUITransportError("OrchestratorBinding.CancelJob", err)
	}

	return nil
}

// ListAgents returns all registered agents for the given tenant.
func (b *OrchestratorBinding) ListAgents(req *orchestratorSDK.ListAgentsRequest) ([]map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.ListAgents")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListAgents(b.ctx, req)
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
func (b *OrchestratorBinding) GetAgent(req *orchestratorSDK.GetAgentRequest) (map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.GetAgent")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	agent, err := client.Orchestrator().GetAgent(b.ctx, req)
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

// ListSessions returns all sessions matching the filter criteria.
func (b *OrchestratorBinding) ListSessions(req *orchestratorSDK.ListSessionsRequest) ([]map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.ListSessions")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListSessions(b.ctx, req)
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
func (b *OrchestratorBinding) ListTasks(req *orchestratorSDK.ListTasksRequest) ([]map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.ListTasks")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListTasks(b.ctx, req)
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
func (b *OrchestratorBinding) GetTask(req *orchestratorSDK.GetTaskRequest) (map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.GetTask")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	task, err := client.Orchestrator().GetTask(b.ctx, req)
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

// ListBackends returns all available orchestration backends. This method
// takes no arguments.
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

// ListBackendModels returns the model names available on the named backend
// as seen from the specified agent. The frontend uses this to populate the
// model selector after a backend is chosen.
//
// Wails marshals the TypeScript object {agent_id, backend_name} into the
// SDK request struct directly. An empty agent_id (or "any") tells the
// server-side resolver to skip host affinity.
//
// The Models slice is always non-nil; callers that pass an unknown agent
// or a backend with no model lister get an empty slice and a nil error
// rather than a transport-style failure.
func (b *OrchestratorBinding) ListBackendModels(req *orchestratorSDK.ListBackendModelsRequest) ([]string, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.ListBackendModels")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListBackendModels(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.ListBackendModels", err)
	}

	if resp == nil || resp.Models == nil {
		return []string{}, nil
	}
	return resp.Models, nil
}

// HealthCheck returns the orchestrator health check report. This method
// takes no arguments.
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

// Inference runs a stateless fleet-routed inference call against a named
// backend without requiring a prior session spawn. It accepts the SDK
// InferenceRequest struct directly (Wails marshals the TypeScript object
// into the Go value) and returns the aggregated InferenceResponse as a
// map so the Wails JSON bridge can serialise it for the frontend.
func (b *OrchestratorBinding) Inference(req *orchestratorSDK.InferenceRequest) (map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.Inference")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().Inference(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.Inference", err)
	}

	// Serialise content blocks as a plain []map so the TypeScript InferenceResponse
	// type can decode them without needing a registered codec on the bridge.
	content := make([]map[string]interface{}, 0, len(resp.Content))
	for _, block := range resp.Content {
		content = append(content, map[string]interface{}{
			"type": block.Type,
			"text": block.Text,
		})
	}

	return map[string]interface{}{
		"content":     content,
		"model":       resp.Model,
		"stop_reason": resp.StopReason,
	}, nil
}

// SubscribeJobEvents starts a background goroutine that reads real-time events
// for the specified job and emits them to the Wails frontend via runtime events.
// The goroutine terminates when the event channel closes or the context is cancelled.
func (b *OrchestratorBinding) SubscribeJobEvents(req *orchestratorSDK.SubscribeJobEventsRequest) error {
	if req == nil {
		return NewGUIRequestRequiredError("OrchestratorBinding.SubscribeJobEvents")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return err
	}

	events, err := client.Orchestrator().SubscribeJobEvents(b.ctx, req)
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
