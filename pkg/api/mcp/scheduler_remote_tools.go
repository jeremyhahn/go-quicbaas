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
	"encoding/base64"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Remote handlers (go-quicbaas#1955) over MCP: a worker lists the open runs
// of its remote: handlers and reports each run's result.

const (
	toolSchedulerListRemoteRuns    = "quicbaas_scheduler_list_remote_runs"
	toolSchedulerCompleteRemoteRun = "quicbaas_scheduler_complete_remote_run"
)

// registerSchedulerRemoteTools registers the two remote-run tools.
func (r *ToolRegistry) registerSchedulerRemoteTools() {
	tenant := map[string]interface{}{
		"type":        "string",
		"description": "Tenant ID for access control (optional)",
	}
	r.addTool(ToolDefinition{
		Name:        toolSchedulerListRemoteRuns,
		Description: "List the caller's open runs of the named remote: handlers, for a worker to execute",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"handlers": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "The remote: handlers the worker serves",
				},
				"tenant_id": tenant,
			},
			"required": []string{"handlers"},
		},
	})
	r.addTool(ToolDefinition{
		Name:        toolSchedulerCompleteRemoteRun,
		Description: "Report a worker's result for one open run of a remote: handler",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"job_id":     map[string]interface{}{"type": "integer", "description": "The run's job"},
				"firing":     map[string]interface{}{"type": "string", "description": "The run's firing, RFC 3339 with nanoseconds"},
				"started_at": map[string]interface{}{"type": "string", "description": "When the run started, RFC 3339 with nanoseconds"},
				"message":    map[string]interface{}{"type": "string", "description": "The result message (optional)"},
				"output":     map[string]interface{}{"type": "string", "description": "Base64-encoded result output (optional)"},
				"error":      map[string]interface{}{"type": "string", "description": "Set when the handler failed (optional)"},
				"tenant_id":  tenant,
			},
			"required": []string{"job_id", "firing", "started_at"},
		},
	})
}

// isSchedulerRemoteTool reports whether name is a remote-run tool.
func isSchedulerRemoteTool(name string) bool {
	return name == toolSchedulerListRemoteRuns || name == toolSchedulerCompleteRemoteRun
}

// schedulerRemoteRuns is the scheduler's remote-run half.
func (r *ToolRegistry) schedulerRemoteRuns() (service.RemoteRunService, error) {
	jobs, err := r.getSchedulerService()
	if err != nil {
		return nil, err
	}
	remote, ok := jobs.(service.RemoteRunService)
	if !ok {
		return nil, api.NewServiceUnavailableError("scheduler remote handlers")
	}
	return remote, nil
}

func (r *ToolRegistry) executeSchedulerListRemoteRuns(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	var handlers []string
	if raw, ok := args["handlers"].([]interface{}); ok {
		for _, h := range raw {
			if s, isStr := h.(string); isStr && s != "" {
				handlers = append(handlers, s)
			}
		}
	}
	if len(handlers) == 0 {
		return nil, api.NewRequiredFieldError("handlers")
	}
	remote, err := r.schedulerRemoteRuns()
	if err != nil {
		return nil, err
	}
	resp, err := remote.ListRemoteRuns(ctx, &service.ListRemoteRunsRequest{TenantID: getString(args, "tenant_id"), Handlers: handlers})
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSchedulerCompleteRemoteRun(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	req := &service.CompleteRemoteRunRequest{
		TenantID: getString(args, "tenant_id"),
		Message:  getString(args, "message"),
		Error:    getString(args, "error"),
	}
	// The run is the caller's: the service scopes it to the caller's tenant.
	req.Run.JobID = getUint64(args, "job_id")
	if req.Run.JobID == 0 {
		return nil, api.NewRequiredFieldError("job_id")
	}
	for key, into := range map[string]*time.Time{"firing": &req.Run.Firing, "started_at": &req.Run.StartedAt} {
		s := getString(args, key)
		if s == "" {
			return nil, api.NewRequiredFieldError(key)
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, api.NewInvalidFormatError(key, err)
		}
		*into = t
	}
	if out := getString(args, "output"); out != "" {
		decoded, err := base64.StdEncoding.DecodeString(out)
		if err != nil {
			return nil, api.NewInvalidFormatError("output encoding", err)
		}
		req.Output = decoded
	}
	remote, err := r.schedulerRemoteRuns()
	if err != nil {
		return nil, err
	}
	resp, err := remote.CompleteRemoteRun(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}
