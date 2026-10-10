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
// bindings (`orchestratorBinding().ListFleetNodes(req)`) and keeps the wire
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

// ListJobs returns the tenant's orchestration jobs, narrowed to the attempts
// at one task when the request names one.
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
			// The task binding, so the desktop task page can group the
			// attempts it was given without a second call.
			"task_id": job.TaskID,
			"attempt": job.Attempt,
		})
	}

	return result, nil
}

// GetJobStatus returns the status of a specific orchestration job.
// THE WHOLE STATUS, NOT A PROJECTION. This used to hand-build a ten-key map,
// which silently dropped every field added since it was written — runner,
// task_id, agent_id, stages, attempts, attempt_budget, terminal_reason,
// readmit_allowed, delivery, verification. The desktop job page reloads
// through GetJobStatus for precisely those, so an sdlc job rendered as a
// workflow job with empty panels and a Readmit with no task to readmit.
//
// The SDK type is already the wire shape — its JSON tags are what every other
// transport serializes — so returning it directly is both correct today and
// incapable of going stale tomorrow. A projection is a second wire format
// that nothing tests.
func (b *OrchestratorBinding) GetJobStatus(req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
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

	return status, nil
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

// ReadmitTask returns a parked task to the pending queue for another attempt.
//
// It is the TASK verb behind the job page's Readmit action: a concluded job
// is over, and what an operator retries is the work, on a fresh attempt. The
// server decides whether the task may come back — a parked task may, a failed
// one is terminal — so this binding carries the request and nothing else.
func (b *OrchestratorBinding) ReadmitTask(req *orchestratorSDK.ReadmitTaskRequest) error {
	if req == nil {
		return NewGUIRequestRequiredError("OrchestratorBinding.ReadmitTask")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return err
	}

	if _, err = client.Orchestrator().ReadmitTask(b.ctx, req); err != nil {
		return NewGUITransportError("OrchestratorBinding.ReadmitTask", err)
	}

	return nil
}

// ListFleetNodes returns all registered agents for the given tenant.
func (b *OrchestratorBinding) ListFleetNodes(req *orchestratorSDK.ListFleetNodesRequest) ([]map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.ListFleetNodes")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListFleetNodes(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.ListFleetNodes", err)
	}

	result := make([]map[string]interface{}, 0, len(resp.Nodes))
	for _, agent := range resp.Nodes {
		result = append(result, map[string]interface{}{
			"id":            agent.ID,
			"tenant_id":     agent.TenantID,
			"name":          agent.Name,
			"host":          agent.Host,
			"backend":       agent.Backend,
			"model":         agent.Model,
			"node_type":     agent.NodeType,
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

// GetFleetNode returns a specific agent by ID.
func (b *OrchestratorBinding) GetFleetNode(req *orchestratorSDK.GetFleetNodeRequest) (map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.GetFleetNode")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	agent, err := client.Orchestrator().GetFleetNode(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.GetFleetNode", err)
	}

	return map[string]interface{}{
		"id":            agent.ID,
		"tenant_id":     agent.TenantID,
		"name":          agent.Name,
		"host":          agent.Host,
		"backend":       agent.Backend,
		"model":         agent.Model,
		"node_type":     agent.NodeType,
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
			"model":        session.Model,
			"status":       session.Status,
			"container_id": session.ContainerID,
			"created_at":   session.CreatedAt,
			"last_active":  session.LastActive,
			// The job the session worked for and the RUNNER that drove it —
			// which is the role the session played. The projection dropped
			// both, so the desktop's session list showed no Role badge while
			// the browser's did (go-quicbaas#969).
			"job_id": session.JobID,
			"runner": session.Runner,
		})
	}

	return result, nil
}

// ListTasks returns all tasks for the given tenant.
func (b *OrchestratorBinding) ListTasks(req *orchestratorSDK.ListTasksRequest) ([]*orchestratorSDK.Task, error) {
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

	return resp.Tasks, nil
}

// AttentionList returns one page of the tenant's attention items: every
// blocked task plus environmental alerts, faceted by project and kind. The
// map carries the full paged envelope (items/total/limit/offset/facets),
// not a bare array — unlike ListTasks/ListFleetNodes/ListSessions above, so the
// frontend's badge count and filter bar read real numbers instead of a
// client-side approximation over an unpaged list.
func (b *OrchestratorBinding) AttentionList(req *orchestratorSDK.AttentionListRequest) (map[string]interface{}, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.AttentionList")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().AttentionList(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.AttentionList", err)
	}

	return map[string]interface{}{
		"items":       resp.Items,
		"count":       resp.Count,
		"total":       resp.Total,
		"limit":       resp.Limit,
		"offset":      resp.Offset,
		"next_offset": resp.NextOffset,
		"facets":      resp.Facets,
	}, nil
}

// AttentionResolve applies an operator's decision to one blocked task from
// the attention inbox.
func (b *OrchestratorBinding) AttentionResolve(req *orchestratorSDK.AttentionResolveRequest) (*orchestratorSDK.AttentionItem, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.AttentionResolve")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	item, err := client.Orchestrator().AttentionResolve(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.AttentionResolve", err)
	}
	return item, nil
}

// GetTask returns a specific task by ID.
func (b *OrchestratorBinding) GetTask(req *orchestratorSDK.GetTaskRequest) (*orchestratorSDK.Task, error) {
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

	return task, nil
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

// ListRunners returns every registered job runner and the ordered states it
// drives a job through, so the desktop Job detail view builds its stepper from
// the runner that actually drove the job rather than from a copy of the
// engine's states.
//
// The runner and state slices are always non-nil: a caller ranging over them
// must not have to nil-check.
func (b *OrchestratorBinding) ListRunners() ([]map[string]interface{}, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().ListRunners(b.ctx)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.ListRunners", err)
	}

	result := make([]map[string]interface{}, 0, len(resp.Runners))
	for _, runner := range resp.Runners {
		states := make([]map[string]interface{}, 0, len(runner.States))
		for _, state := range runner.States {
			states = append(states, map[string]interface{}{
				"state":          state.State,
				"label":          state.Label,
				"session_backed": state.SessionBacked,
			})
		}
		result = append(result, map[string]interface{}{
			"name":   runner.Name,
			"states": states,
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

// GetSpendReport returns one scope of the consumption ledger: the tokens spent
// under it, the drill-down to the level below, and the allowance status of
// every pooled account that paid.
//
// The request struct is forwarded verbatim to the SDK, matching the TypeScript
// Wails client which passes the GetSpendReportRequest object directly over
// IPC, and the response is returned as the SDK type rather than reshaped into
// a map. THE TYPE IS LOAD-BEARING HERE: `cost_measured` is what tells the
// desktop UI whether `cost_micros` is a measured figure or an unmeasured zero,
// and a hand-built map is exactly where a field like that goes missing.
func (b *OrchestratorBinding) GetSpendReport(req *orchestratorSDK.GetSpendReportRequest) (*orchestratorSDK.GetSpendReportResponse, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.GetSpendReport")
	}

	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().GetSpendReport(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.GetSpendReport", err)
	}
	return resp, nil
}

// GetTenantSpendPolicy returns the tenant's own spend limits and its usage
// against each window (go-quicbaas#1058).
func (b *OrchestratorBinding) GetTenantSpendPolicy(req *orchestratorSDK.GetTenantSpendPolicyRequest) (*orchestratorSDK.TenantSpendPolicy, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.GetTenantSpendPolicy")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.Orchestrator().GetTenantSpendPolicy(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.GetTenantSpendPolicy", err)
	}
	return resp, nil
}

// SetTenantSpendPolicy replaces the tenant's own spend limits.
func (b *OrchestratorBinding) SetTenantSpendPolicy(req *orchestratorSDK.SetTenantSpendPolicyRequest) (*orchestratorSDK.TenantSpendPolicy, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.SetTenantSpendPolicy")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.Orchestrator().SetTenantSpendPolicy(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.SetTenantSpendPolicy", err)
	}
	return resp, nil
}
