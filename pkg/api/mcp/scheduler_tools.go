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
	"encoding/base64"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerSchedulerTools registers all scheduler tools with the registry.
// schedulerJobScopeProperty is the optional scope argument of every by-ID
// scheduler tool. A tenant job and a job delegated to the caller can share
// an ID, so a delegated job is reached only by naming its scope.
var schedulerJobScopeProperty = map[string]interface{}{
	"type":        "string",
	"enum":        []string{"tenant", "delegated", "system"},
	"description": "The owner_type the job was listed under: tenant (default) for the caller's own job, delegated for a job delegated to the caller",
}

func (r *ToolRegistry) registerSchedulerTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_create_job",
		Description: "Create a new scheduled job with scheduling parameters, handlers, and optional payload",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable job name",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Optional job description",
				},
				"owner_type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"system", "tenant", "delegated"},
					"description": "Job ownership type",
				},
				"owner_id": map[string]interface{}{
					"type":        "string",
					"description": "Owner identifier (optional, depends on owner_type)",
				},
				"target_tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Target tenant ID for tenant-scoped jobs (optional)",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"one-shot", "recurring", "delayed"},
					"description": "Scheduling type: one-shot (execute once), recurring (cron-like), or delayed (after duration)",
				},
				"schedule": map[string]interface{}{
					"type":        "string",
					"description": "Cron expression for recurring jobs (e.g., '0 0 * * *' for daily at midnight)",
				},
				"execute_at": map[string]interface{}{
					"type":        "string",
					"description": "RFC3339 timestamp for one-shot execution (e.g., '2025-01-20T10:00:00Z')",
				},
				"delay": map[string]interface{}{
					"type":        "string",
					"description": "Duration string for delayed jobs (e.g., '1h30m', '24h')",
				},
				"handler": map[string]interface{}{
					"type":        "string",
					"description": "Handler name that processes this job",
				},
				"payload": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded job payload data (optional)",
				},
				"priority": map[string]interface{}{
					"type":        "integer",
					"description": "Job priority (higher values = higher priority, default: 0)",
				},
				"timeout": map[string]interface{}{
					"type":        "string",
					"description": "Maximum execution time as duration string (e.g., '5m', '1h')",
				},
				"max_retries": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum retry attempts on failure (default: 0)",
				},
				"node_affinity": map[string]interface{}{
					"type":        "string",
					"description": "Pin execution to a specific node. Empty (default) = cluster-wide (leader-only). Non-empty = fires only on the node whose SchedulerEngine was constructed with a matching node ID, regardless of Raft leadership.",
				},
				"labels": map[string]interface{}{
					"type":        "object",
					"description": "Key-value labels for job categorization and filtering",
				},
			},
			"required": []string{"name", "type", "handler"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_get_job",
		Description: "Retrieve detailed information about a scheduled job by ID",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "integer",
					"description": "Unique job identifier",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control (optional)",
				},
				"scope": schedulerJobScopeProperty,
			},
			"required": []string{"job_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_update_job",
		Description: "Update an existing scheduled job's properties including name, schedule, handler, payload, priority, timeout, retries, and labels",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "integer",
					"description": "Unique job identifier",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control (optional)",
				},
				"scope": schedulerJobScopeProperty,
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Updated human-readable job name (optional)",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Updated job description (optional)",
				},
				"schedule": map[string]interface{}{
					"type":        "string",
					"description": "Updated cron expression for recurring jobs (optional)",
				},
				"execute_at": map[string]interface{}{
					"type":        "string",
					"description": "Updated RFC3339 timestamp for one-shot execution (optional)",
				},
				"handler": map[string]interface{}{
					"type":        "string",
					"description": "Updated handler name (optional)",
				},
				"payload": map[string]interface{}{
					"type":        "string",
					"description": "Updated base64-encoded job payload data (optional)",
				},
				"priority": map[string]interface{}{
					"type":        "integer",
					"description": "Updated job priority (optional)",
				},
				"timeout": map[string]interface{}{
					"type":        "string",
					"description": "Updated maximum execution time as duration string (optional)",
				},
				"max_retries": map[string]interface{}{
					"type":        "integer",
					"description": "Updated maximum retry attempts on failure (optional)",
				},
				"labels": map[string]interface{}{
					"type":        "object",
					"description": "Updated key-value labels for job categorization and filtering (optional)",
				},
			},
			"required": []string{"job_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_list_jobs",
		Description: "List scheduled jobs with optional filtering by owner, status, handler, or labels",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"owner_type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"system", "tenant", "delegated"},
					"description": "Filter by ownership type (optional)",
				},
				"owner_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by owner identifier (optional)",
				},
				"target_tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by target tenant ID (optional)",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"pending", "running", "completed", "failed", "paused", "canceled"},
					"description": "Filter by job status (optional)",
				},
				"handler": map[string]interface{}{
					"type":        "string",
					"description": "Filter by handler name (optional)",
				},
				"labels": map[string]interface{}{
					"type":        "object",
					"description": "Filter by label key-value pairs (optional)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of jobs to return (default: 100)",
				},
				"cursor": map[string]interface{}{
					"type":        "string",
					"description": "Pagination cursor from previous response",
				},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_delete_job",
		Description: "Delete a scheduled job permanently",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "integer",
					"description": "Unique job identifier",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control (optional)",
				},
				"scope": schedulerJobScopeProperty,
				"force": map[string]interface{}{
					"type":        "boolean",
					"description": "Force delete even if job is running (default: false)",
				},
			},
			"required": []string{"job_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_trigger_job",
		Description: "Immediately trigger execution of a scheduled job regardless of its schedule",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "integer",
					"description": "Unique job identifier",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control (optional)",
				},
				"scope": schedulerJobScopeProperty,
			},
			"required": []string{"job_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_pause_job",
		Description: "Pause a scheduled job to temporarily stop its execution",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "integer",
					"description": "Unique job identifier",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control (optional)",
				},
				"scope": schedulerJobScopeProperty,
			},
			"required": []string{"job_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_resume_job",
		Description: "Resume a paused job to restart its scheduled execution",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "integer",
					"description": "Unique job identifier",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control (optional)",
				},
				"scope": schedulerJobScopeProperty,
			},
			"required": []string{"job_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_cancel_job",
		Description: "Cancel a scheduled job with an optional reason",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "integer",
					"description": "Unique job identifier",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control (optional)",
				},
				"scope": schedulerJobScopeProperty,
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Reason for cancellation (optional)",
				},
			},
			"required": []string{"job_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_job_history",
		Description: "Retrieve execution history for a scheduled job including status, duration, and errors",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id": map[string]interface{}{
					"type":        "integer",
					"description": "Unique job identifier",
				},
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID for access control (optional)",
				},
				"scope": schedulerJobScopeProperty,
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of execution records to return (default: 100)",
				},
				"cursor": map[string]interface{}{
					"type":        "string",
					"description": "Pagination cursor from previous response",
				},
			},
			"required": []string{"job_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_scheduler_status",
		Description: "Get scheduler service status including running state, job counts, and statistics",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})
}

// =============================================================================
// Scheduler Tool Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeSchedulerCreateJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedulerSvc, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}

	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}

	handler := getString(args, "handler")
	if handler == "" {
		return nil, api.NewRequiredFieldError("handler")
	}

	ownerTypeStr := getString(args, "owner_type")
	if ownerTypeStr == "" {
		ownerTypeStr = "system" // Default to system-owned job
	}

	jobTypeStr := getString(args, "type")
	if jobTypeStr == "" {
		return nil, api.NewRequiredFieldError("type")
	}

	req := &service.CreateSchedulerJobRequest{
		Name:           name,
		Description:    getString(args, "description"),
		OwnerType:      service.SchedulerOwnerType(ownerTypeStr),
		OwnerID:        getString(args, "owner_id"),
		TargetTenantID: getString(args, "target_tenant_id"),
		Type:           service.SchedulerJobType(jobTypeStr),
		Schedule:       getString(args, "schedule"),
		Handler:        handler,
		Priority:       int32(getInt(args, "priority")),
		MaxRetries:     int32(getInt(args, "max_retries")),
		NodeAffinity:   getString(args, "node_affinity"),
	}

	// Parse execute_at timestamp
	if executeAtStr := getString(args, "execute_at"); executeAtStr != "" {
		executeAt, parseErr := time.Parse(time.RFC3339, executeAtStr)
		if parseErr != nil {
			return nil, api.NewInvalidFormatError("execute_at", parseErr)
		}
		req.ExecuteAt = executeAt
	}

	// Parse delay duration
	if delayStr := getString(args, "delay"); delayStr != "" {
		delay, parseErr := time.ParseDuration(delayStr)
		if parseErr != nil {
			return nil, api.NewInvalidFormatError("delay", parseErr)
		}
		req.Delay = delay
	}

	// Parse timeout duration
	if timeoutStr := getString(args, "timeout"); timeoutStr != "" {
		timeout, parseErr := time.ParseDuration(timeoutStr)
		if parseErr != nil {
			return nil, api.NewInvalidFormatError("timeout", parseErr)
		}
		req.Timeout = timeout
	}

	// Parse base64-encoded payload
	if payloadB64 := getString(args, "payload"); payloadB64 != "" {
		payload, decodeErr := base64.StdEncoding.DecodeString(payloadB64)
		if decodeErr != nil {
			return nil, api.NewInvalidFormatError("payload encoding", decodeErr)
		}
		req.Payload = payload
	}

	// Parse labels
	if labelsRaw, ok := args["labels"].(map[string]interface{}); ok {
		labels := make(map[string]string, len(labelsRaw))
		for k, v := range labelsRaw {
			if str, isStr := v.(string); isStr {
				labels[k] = str
			}
		}
		req.Labels = labels
	}

	resp, err := schedulerSvc.CreateJob(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"job":       resp.Job,
		"timestamp": resp.Timestamp.Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeSchedulerGetJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedulerSvc, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}

	jobID := getUint64(args, "job_id")
	if jobID == 0 {
		return nil, api.NewRequiredFieldError("job_id")
	}

	req := &service.GetSchedulerJobRequest{
		JobID:    jobID,
		TenantID: getString(args, "tenant_id"),
		Scope:    service.SchedulerOwnerType(getString(args, "scope")),
	}

	resp, err := schedulerSvc.GetJob(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"job":       resp.Job,
		"timestamp": resp.Timestamp.Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeSchedulerUpdateJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedulerSvc, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}

	jobID := getUint64(args, "job_id")
	if jobID == 0 {
		return nil, api.NewRequiredFieldError("job_id")
	}

	req := &service.UpdateSchedulerJobRequest{
		JobID:       jobID,
		TenantID:    getString(args, "tenant_id"),
		Scope:       service.SchedulerOwnerType(getString(args, "scope")),
		Name:        getString(args, "name"),
		Description: getString(args, "description"),
		Schedule:    getString(args, "schedule"),
		Handler:     getString(args, "handler"),
		Priority:    int32(getInt(args, "priority")),
		MaxRetries:  int32(getInt(args, "max_retries")),
	}

	// Parse execute_at timestamp
	if executeAtStr := getString(args, "execute_at"); executeAtStr != "" {
		executeAt, parseErr := time.Parse(time.RFC3339, executeAtStr)
		if parseErr != nil {
			return nil, api.NewInvalidFormatError("execute_at", parseErr)
		}
		req.ExecuteAt = executeAt
	}

	// Parse timeout duration
	if timeoutStr := getString(args, "timeout"); timeoutStr != "" {
		timeout, parseErr := time.ParseDuration(timeoutStr)
		if parseErr != nil {
			return nil, api.NewInvalidFormatError("timeout", parseErr)
		}
		req.Timeout = timeout
	}

	// Parse base64-encoded payload
	if payloadB64 := getString(args, "payload"); payloadB64 != "" {
		payload, decodeErr := base64.StdEncoding.DecodeString(payloadB64)
		if decodeErr != nil {
			return nil, api.NewInvalidFormatError("payload encoding", decodeErr)
		}
		req.Payload = payload
	}

	// Parse labels
	if labelsRaw, ok := args["labels"].(map[string]interface{}); ok {
		labels := make(map[string]string, len(labelsRaw))
		for k, v := range labelsRaw {
			if str, isStr := v.(string); isStr {
				labels[k] = str
			}
		}
		req.Labels = labels
	}

	resp, err := schedulerSvc.UpdateJob(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"job":       resp.Job,
		"timestamp": resp.Timestamp.Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeSchedulerListJobs(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedulerSvc, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}

	limit := getInt(args, "limit")
	if limit == 0 {
		limit = 100
	}

	req := &service.ListSchedulerJobsRequest{
		OwnerID:        getString(args, "owner_id"),
		TargetTenantID: getString(args, "target_tenant_id"),
		Handler:        getString(args, "handler"),
		Limit:          limit,
		Cursor:         getString(args, "cursor"),
	}

	// Parse owner_type if provided
	if ownerTypeStr := getString(args, "owner_type"); ownerTypeStr != "" {
		req.OwnerType = service.SchedulerOwnerType(ownerTypeStr)
	}

	// Parse status if provided
	if statusStr := getString(args, "status"); statusStr != "" {
		req.Status = service.SchedulerJobStatus(statusStr)
	}

	// Parse labels
	if labelsRaw, ok := args["labels"].(map[string]interface{}); ok {
		labels := make(map[string]string, len(labelsRaw))
		for k, v := range labelsRaw {
			if str, isStr := v.(string); isStr {
				labels[k] = str
			}
		}
		req.Labels = labels
	}

	resp, err := schedulerSvc.ListJobs(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"jobs":        resp.Jobs,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
		"total_count": resp.TotalCount,
		"timestamp":   resp.Timestamp.Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeSchedulerDeleteJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedulerSvc, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}

	jobID := getUint64(args, "job_id")
	if jobID == 0 {
		return nil, api.NewRequiredFieldError("job_id")
	}

	req := &service.DeleteSchedulerJobRequest{
		JobID:    jobID,
		TenantID: getString(args, "tenant_id"),
		Scope:    service.SchedulerOwnerType(getString(args, "scope")),
		Force:    getBool(args, "force"),
	}

	resp, err := schedulerSvc.DeleteJob(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"job_id":    resp.JobID,
		"deleted":   resp.Deleted,
		"timestamp": resp.Timestamp.Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeSchedulerTriggerJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedulerSvc, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}

	jobID := getUint64(args, "job_id")
	if jobID == 0 {
		return nil, api.NewRequiredFieldError("job_id")
	}

	req := &service.TriggerSchedulerJobRequest{
		JobID:    jobID,
		TenantID: getString(args, "tenant_id"),
		Scope:    service.SchedulerOwnerType(getString(args, "scope")),
	}

	resp, err := schedulerSvc.TriggerJob(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"job_id":    resp.JobID,
		"status":    string(resp.Status),
		"timestamp": resp.Timestamp.Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeSchedulerPauseJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedulerSvc, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}

	jobID := getUint64(args, "job_id")
	if jobID == 0 {
		return nil, api.NewRequiredFieldError("job_id")
	}

	req := &service.PauseSchedulerJobRequest{
		JobID:    jobID,
		TenantID: getString(args, "tenant_id"),
		Scope:    service.SchedulerOwnerType(getString(args, "scope")),
	}

	resp, err := schedulerSvc.PauseJob(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"job_id":     resp.JobID,
		"old_status": string(resp.OldStatus),
		"new_status": string(resp.NewStatus),
		"timestamp":  resp.Timestamp.Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeSchedulerResumeJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedulerSvc, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}

	jobID := getUint64(args, "job_id")
	if jobID == 0 {
		return nil, api.NewRequiredFieldError("job_id")
	}

	req := &service.ResumeSchedulerJobRequest{
		JobID:    jobID,
		TenantID: getString(args, "tenant_id"),
		Scope:    service.SchedulerOwnerType(getString(args, "scope")),
	}

	resp, err := schedulerSvc.ResumeJob(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"job_id":     resp.JobID,
		"old_status": string(resp.OldStatus),
		"new_status": string(resp.NewStatus),
		"timestamp":  resp.Timestamp.Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeSchedulerCancelJob(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedulerSvc, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}

	jobID := getUint64(args, "job_id")
	if jobID == 0 {
		return nil, api.NewRequiredFieldError("job_id")
	}

	req := &service.CancelSchedulerJobRequest{
		JobID:    jobID,
		TenantID: getString(args, "tenant_id"),
		Scope:    service.SchedulerOwnerType(getString(args, "scope")),
		Reason:   getString(args, "reason"),
	}

	resp, err := schedulerSvc.CancelJob(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"job_id":     resp.JobID,
		"old_status": string(resp.OldStatus),
		"new_status": string(resp.NewStatus),
		"timestamp":  resp.Timestamp.Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeSchedulerJobHistory(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedulerSvc, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}

	jobID := getUint64(args, "job_id")
	if jobID == 0 {
		return nil, api.NewRequiredFieldError("job_id")
	}

	limit := getInt(args, "limit")
	if limit == 0 {
		limit = 100
	}

	req := &service.GetSchedulerJobHistoryRequest{
		JobID:    jobID,
		TenantID: getString(args, "tenant_id"),
		Scope:    service.SchedulerOwnerType(getString(args, "scope")),
		Limit:    limit,
		Cursor:   getString(args, "cursor"),
	}

	resp, err := schedulerSvc.GetJobHistory(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"executions":  resp.Executions,
		"next_cursor": resp.NextCursor,
		"has_more":    resp.HasMore,
		"timestamp":   resp.Timestamp.Format(time.RFC3339Nano),
	})
}

func (r *ToolRegistry) executeSchedulerStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	schedulerSvc, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}

	resp, err := schedulerSvc.GetStatus(ctx)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"running":   resp.Running,
		"stats":     resp.Stats,
		"timestamp": resp.Timestamp.Format(time.RFC3339Nano),
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// getSchedulerService retrieves the SchedulerService from the QuicBaaS service.
func (r *ToolRegistry) getSchedulerService() (service.SchedulerService, error) {
	schedulerSvc := r.service.Scheduler()
	if schedulerSvc == nil {
		return nil, api.NewServiceUnavailableError("scheduler")
	}
	return schedulerSvc, nil
}

// isSchedulerTool checks if a tool name is a scheduler tool.
func isSchedulerTool(name string) bool {
	switch name {
	case "quicbaas_scheduler_create_job",
		"quicbaas_scheduler_get_job",
		"quicbaas_scheduler_update_job",
		"quicbaas_scheduler_list_jobs",
		"quicbaas_scheduler_delete_job",
		"quicbaas_scheduler_trigger_job",
		"quicbaas_scheduler_pause_job",
		"quicbaas_scheduler_resume_job",
		"quicbaas_scheduler_cancel_job",
		"quicbaas_scheduler_job_history",
		"quicbaas_scheduler_status":
		return true
	default:
		return false
	}
}

// executeSchedulerTool dispatches scheduler tool execution to the appropriate handler.
func (r *ToolRegistry) executeSchedulerTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "quicbaas_scheduler_create_job":
		return r.executeSchedulerCreateJob(ctx, args)
	case "quicbaas_scheduler_get_job":
		return r.executeSchedulerGetJob(ctx, args)
	case "quicbaas_scheduler_update_job":
		return r.executeSchedulerUpdateJob(ctx, args)
	case "quicbaas_scheduler_list_jobs":
		return r.executeSchedulerListJobs(ctx, args)
	case "quicbaas_scheduler_delete_job":
		return r.executeSchedulerDeleteJob(ctx, args)
	case "quicbaas_scheduler_trigger_job":
		return r.executeSchedulerTriggerJob(ctx, args)
	case "quicbaas_scheduler_pause_job":
		return r.executeSchedulerPauseJob(ctx, args)
	case "quicbaas_scheduler_resume_job":
		return r.executeSchedulerResumeJob(ctx, args)
	case "quicbaas_scheduler_cancel_job":
		return r.executeSchedulerCancelJob(ctx, args)
	case "quicbaas_scheduler_job_history":
		return r.executeSchedulerJobHistory(ctx, args)
	case "quicbaas_scheduler_status":
		return r.executeSchedulerStatus(ctx, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}
