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
	"encoding/base64"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// Remote handlers (go-quicbaas#1955) over MCP: the arguments of
// quicbaas_scheduler_list_remote_runs and quicbaas_scheduler_complete_remote_run
// and their results.

// listRemoteRunsArgs renders a ListRemoteRunsRequest.
func listRemoteRunsArgs(method string, req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*scheduler.ListRemoteRunsRequest)
	if !ok || r == nil {
		return nil, invalidSchedulerRequest(method, req)
	}
	args := map[string]interface{}{"handlers": r.Handlers}
	putString(args, "tenant_id", r.TenantID)
	return args, nil
}

// completeRemoteRunArgs renders a CompleteRemoteRunRequest. The run's times
// carry nanoseconds so the run matches the one the scheduler started exactly.
func completeRemoteRunArgs(method string, req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*scheduler.CompleteRemoteRunRequest)
	if !ok || r == nil {
		return nil, invalidSchedulerRequest(method, req)
	}
	args := jobRefArgsFor(r.Run.JobID, r.TenantID)
	args["firing"] = r.Run.Firing.Format(time.RFC3339Nano)
	args["started_at"] = r.Run.StartedAt.Format(time.RFC3339Nano)
	putString(args, "message", r.Message)
	putString(args, "error", r.Error)
	if len(r.Output) > 0 {
		args["output"] = base64.StdEncoding.EncodeToString(r.Output)
	}
	return args, nil
}

// decodeRemoteRunsResult decodes a ListRemoteRuns result.
func decodeRemoteRunsResult(_, result interface{}) (interface{}, error) {
	var resp service.ListRemoteRunsResponse
	if _, err := decodeSchedulerResult(&resp, result); err != nil {
		return nil, err
	}
	out := &scheduler.ListRemoteRunsResponse{Runs: make([]*scheduler.RemoteRun, 0, len(resp.Runs))}
	for _, run := range resp.Runs {
		out.Runs = append(out.Runs, &scheduler.RemoteRun{
			RemoteRunRef: scheduler.RemoteRunRef{JobID: run.JobID, Firing: run.Firing, StartedAt: run.StartedAt},
			Name:         run.Name,
			Handler:      run.Handler,
			Payload:      run.Payload,
			Deadline:     run.Deadline,
		})
	}
	return out, nil
}

// decodeCompleteRemoteRunResult decodes a CompleteRemoteRun result.
func decodeCompleteRemoteRunResult(_, result interface{}) (interface{}, error) {
	var resp service.CompleteRemoteRunResponse
	if _, err := decodeSchedulerResult(&resp, result); err != nil {
		return nil, err
	}
	return &scheduler.CompleteRemoteRunResponse{JobID: resp.JobID}, nil
}
