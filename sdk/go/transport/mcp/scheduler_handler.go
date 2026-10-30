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

// Package mcp provides MCP transport implementations.
package mcp

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// schedulerHandler renders SDK scheduler requests as the arguments of the
// server's quicbaas_scheduler_* tools and decodes the tool results. Both
// directions go through the pkg/service scheduler types those tools are
// written against, so every key and value format is the one the server
// reads and writes.
type schedulerHandler struct{}

// newSchedulerHandler creates a new scheduler handler.
func newSchedulerHandler() *schedulerHandler {
	return &schedulerHandler{}
}

// schedulerJobArgsFunc renders one SDK job request as tool arguments.
type schedulerJobArgsFunc func(method string, req interface{}) (map[string]interface{}, error)

// schedulerJobResultFunc decodes one job tool result into the SDK response.
// It also receives the SDK request, for the values the server does not echo.
type schedulerJobResultFunc func(req, result interface{}) (interface{}, error)

// schedulerJobArgs maps each job method to its argument renderer.
var schedulerJobArgs = map[string]schedulerJobArgsFunc{
	"CreateJob":     createJobArgs,
	"UpdateJob":     updateJobArgs,
	"ListJobs":      listJobsArgs,
	"GetJob":        jobRefArgs,
	"TriggerJob":    jobRefArgs,
	"PauseJob":      jobRefArgs,
	"ResumeJob":     jobRefArgs,
	"DeleteJob":     deleteJobArgs,
	"CancelJob":     cancelJobArgs,
	"GetJobHistory": jobHistoryArgs,
	"GetStatus":     statusArgs,
}

// schedulerJobResults maps each job method to its result decoder.
var schedulerJobResults = map[string]schedulerJobResultFunc{
	"CreateJob":     decodeJobResult(scheduler.CreateJobResponseFromService),
	"GetJob":        decodeJobInfoResult,
	"UpdateJob":     decodeJobInfoResult,
	"ListJobs":      decodeJobResult(scheduler.ListJobsResponseFromService),
	"TriggerJob":    decodeJobResult(scheduler.TriggerJobResponseFromService),
	"PauseJob":      decodeJobResult(scheduler.PauseJobResponseFromService),
	"ResumeJob":     decodeJobResult(scheduler.ResumeJobResponseFromService),
	"DeleteJob":     decodeJobResult(scheduler.DeleteJobResponseFromService),
	"CancelJob":     decodeCancelJobResult,
	"GetJobHistory": decodeJobHistoryResult,
	"GetStatus":     decodeStatusResult,
}

// transformRequest transforms an SDK scheduler request to MCP tool arguments.
func (h *schedulerHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	if _, ok := placementResults[method]; ok {
		return transformPlacementRequest(method, req)
	}
	render, ok := schedulerJobArgs[method]
	if !ok {
		return nil, errors.NewInvalidInput("mcp.scheduler."+method, "unknown scheduler method")
	}
	return render(method, req)
}

// transformResponse transforms an MCP tool result to the SDK response type.
func (h *schedulerHandler) transformResponse(method string, req, result interface{}) (interface{}, error) {
	if newResult, ok := placementResults[method]; ok {
		return decodeSchedulerResult(newResult(), result)
	}
	decode, ok := schedulerJobResults[method]
	if !ok {
		return result, nil
	}
	return decode(req, result)
}

// invalidSchedulerRequest reports a request that is nil or not the type the
// method takes.
func invalidSchedulerRequest(method string, req interface{}) error {
	return errors.NewInvalidInput("mcp.scheduler."+method, fmt.Sprintf("unexpected request type %T", req))
}

// createJobArgs renders a CreateJobRequest as quicbaas_scheduler_create_job
// arguments.
func createJobArgs(method string, req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*scheduler.CreateJobRequest)
	if !ok || r == nil {
		return nil, invalidSchedulerRequest(method, req)
	}
	svc := scheduler.CreateJobRequestToService(r)
	args := map[string]interface{}{
		"name":       svc.Name,
		"handler":    svc.Handler,
		"type":       string(svc.Type),
		"owner_type": string(svc.OwnerType),
	}
	putString(args, "description", svc.Description)
	putString(args, "owner_id", svc.OwnerID)
	putString(args, "schedule", svc.Schedule)
	putString(args, "node_affinity", svc.NodeAffinity)
	putTime(args, "execute_at", svc.ExecuteAt)
	putDuration(args, "delay", svc.Delay)
	putDuration(args, "timeout", svc.Timeout)
	putPayload(args, svc.Payload)
	putLabels(args, svc.Labels)
	putInt32(args, "priority", svc.Priority)
	putInt32(args, "max_retries", svc.MaxRetries)
	return args, nil
}

// updateJobArgs renders an UpdateJobRequest as quicbaas_scheduler_update_job
// arguments.
func updateJobArgs(method string, req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*scheduler.UpdateJobRequest)
	if !ok || r == nil {
		return nil, invalidSchedulerRequest(method, req)
	}
	svc := scheduler.UpdateJobRequestToService(r)
	args := jobRefArgsFor(svc.JobID, svc.TenantID, svc.Scope)
	putString(args, "name", svc.Name)
	putString(args, "description", svc.Description)
	putString(args, "schedule", svc.Schedule)
	putDuration(args, "timeout", svc.Timeout)
	putPayload(args, svc.Payload)
	putLabels(args, svc.Labels)
	putInt32(args, "max_retries", svc.MaxRetries)
	return args, nil
}

// listJobsArgs renders ListJobsOptions as quicbaas_scheduler_list_jobs
// arguments.
func listJobsArgs(method string, req interface{}) (map[string]interface{}, error) {
	opts, ok := req.(*scheduler.ListJobsOptions)
	if !ok {
		return nil, invalidSchedulerRequest(method, req)
	}
	svc := scheduler.ListJobsOptionsToService(opts)
	args := map[string]interface{}{}
	putString(args, "owner_type", string(svc.OwnerType))
	putString(args, "owner_id", svc.OwnerID)
	putString(args, "status", string(svc.Status))
	putString(args, "handler", svc.Handler)
	putString(args, "cursor", svc.Cursor)
	putLabels(args, svc.Labels)
	if svc.Limit > 0 {
		args["limit"] = svc.Limit
	}
	return args, nil
}

// jobRefArgs renders the requests that name only a job: GetJob, TriggerJob,
// PauseJob and ResumeJob.
func jobRefArgs(method string, req interface{}) (map[string]interface{}, error) {
	switch r := req.(type) {
	case *scheduler.GetJobRequest:
		if r != nil {
			return jobRefArgsFor(r.JobID, r.TenantID, scheduler.JobScopeToService(r.Scope)), nil
		}
	case *scheduler.TriggerJobRequest:
		if r != nil {
			return jobRefArgsFor(r.JobID, r.TenantID, scheduler.JobScopeToService(r.Scope)), nil
		}
	case *scheduler.PauseJobRequest:
		if r != nil {
			return jobRefArgsFor(r.JobID, r.TenantID, scheduler.JobScopeToService(r.Scope)), nil
		}
	case *scheduler.ResumeJobRequest:
		if r != nil {
			return jobRefArgsFor(r.JobID, r.TenantID, scheduler.JobScopeToService(r.Scope)), nil
		}
	}
	return nil, invalidSchedulerRequest(method, req)
}

// deleteJobArgs renders a DeleteJobRequest as quicbaas_scheduler_delete_job
// arguments.
func deleteJobArgs(method string, req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*scheduler.DeleteJobRequest)
	if !ok || r == nil {
		return nil, invalidSchedulerRequest(method, req)
	}
	args := jobRefArgsFor(r.JobID, r.TenantID, scheduler.JobScopeToService(r.Scope))
	if r.Force {
		args["force"] = true
	}
	return args, nil
}

// cancelJobArgs renders a CancelJobRequest as quicbaas_scheduler_cancel_job
// arguments.
func cancelJobArgs(method string, req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*scheduler.CancelJobRequest)
	if !ok || r == nil {
		return nil, invalidSchedulerRequest(method, req)
	}
	args := jobRefArgsFor(r.JobID, r.TenantID, scheduler.JobScopeToService(r.Scope))
	putString(args, "reason", r.Reason)
	return args, nil
}

// jobHistoryArgs renders a GetJobHistoryRequest as
// quicbaas_scheduler_job_history arguments.
func jobHistoryArgs(method string, req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*scheduler.GetJobHistoryRequest)
	if !ok || r == nil {
		return nil, invalidSchedulerRequest(method, req)
	}
	args := jobRefArgsFor(r.JobID, r.TenantID, scheduler.JobScopeToService(r.Scope))
	if limit := scheduler.HistoryLimit(r.Options); limit > 0 {
		args["limit"] = limit
	}
	return args, nil
}

// statusArgs renders GetStatus, which takes no arguments.
func statusArgs(method string, req interface{}) (map[string]interface{}, error) {
	if _, ok := req.(*scheduler.GetStatusRequest); !ok {
		return nil, invalidSchedulerRequest(method, req)
	}
	return map[string]interface{}{}, nil
}

// jobRefArgsFor returns the arguments that address one job: its ID, the
// tenant, and the scope that names the namespace holding it.
func jobRefArgsFor(jobID uint64, tenantID string, scope service.SchedulerOwnerType) map[string]interface{} {
	args := map[string]interface{}{"job_id": jobID}
	putString(args, "tenant_id", tenantID)
	putString(args, "scope", string(scope))
	return args
}

// putString sets key when value is not empty.
func putString(args map[string]interface{}, key, value string) {
	if value != "" {
		args[key] = value
	}
}

// putInt32 sets key when value is not zero.
func putInt32(args map[string]interface{}, key string, value int32) {
	if value != 0 {
		args[key] = value
	}
}

// putTime sets key to value in RFC 3339, the format the tools parse, when
// value is set.
func putTime(args map[string]interface{}, key string, value time.Time) {
	if !value.IsZero() {
		args[key] = value.Format(time.RFC3339)
	}
}

// putDuration sets key to value as a Go duration string, the format the
// tools parse, when value is positive.
func putDuration(args map[string]interface{}, key string, value time.Duration) {
	if value > 0 {
		args[key] = value.String()
	}
}

// putPayload sets "payload" to the base64 encoding the tools decode.
func putPayload(args map[string]interface{}, payload []byte) {
	if len(payload) > 0 {
		args["payload"] = base64.StdEncoding.EncodeToString(payload)
	}
}

// putLabels sets "labels" to the label object the tools read.
func putLabels(args map[string]interface{}, labels map[string]string) {
	if len(labels) > 0 {
		args["labels"] = labels
	}
}

// decodeJobResult returns a decoder that reads a tool result as the service
// response T and converts it with convert.
func decodeJobResult[T, R any](convert func(*T) R) schedulerJobResultFunc {
	return func(_, result interface{}) (interface{}, error) {
		resp := new(T)
		if _, err := decodeSchedulerResult(resp, result); err != nil {
			return nil, err
		}
		return convert(resp), nil
	}
}

// decodeJobInfoResult decodes the {job, timestamp} result of GetJob and
// UpdateJob. A result without a job decodes to nil, which the client reports
// as not found.
func decodeJobInfoResult(_, result interface{}) (interface{}, error) {
	var resp service.GetSchedulerJobResponse
	if _, err := decodeSchedulerResult(&resp, result); err != nil {
		return nil, err
	}
	if resp.Job == nil {
		return nil, nil
	}
	return scheduler.JobInfoFromService(resp.Job), nil
}

// decodeCancelJobResult decodes a CancelJob result. The server does not echo
// the reason, so it is taken from the request.
func decodeCancelJobResult(req, result interface{}) (interface{}, error) {
	var resp service.CancelSchedulerJobResponse
	if _, err := decodeSchedulerResult(&resp, result); err != nil {
		return nil, err
	}
	var reason string
	if r, ok := req.(*scheduler.CancelJobRequest); ok && r != nil {
		reason = r.Reason
	}
	return scheduler.CancelJobResponseFromService(&resp, reason), nil
}

// decodeJobHistoryResult decodes a GetJobHistory result. The server does not
// echo the job ID, so it is taken from the request.
func decodeJobHistoryResult(req, result interface{}) (interface{}, error) {
	var resp service.GetSchedulerJobHistoryResponse
	if _, err := decodeSchedulerResult(&resp, result); err != nil {
		return nil, err
	}
	var jobID uint64
	if r, ok := req.(*scheduler.GetJobHistoryRequest); ok && r != nil {
		jobID = r.JobID
	}
	return scheduler.JobHistoryResponseFromService(jobID, &resp), nil
}

// decodeStatusResult decodes a GetStatus result.
func decodeStatusResult(_, result interface{}) (interface{}, error) {
	var resp service.GetSchedulerStatusResponse
	if _, err := decodeSchedulerResult(&resp, result); err != nil {
		return nil, err
	}
	return scheduler.SchedulerStatusFromService(&resp, time.Now()), nil
}

// Helper functions for extracting values from maps.

func getStringFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt64FromMap(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case int:
			return int64(n)
		}
	}
	return 0
}

func getUint64FromMap(m map[string]interface{}, key string) uint64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return uint64(n)
		case uint64:
			return n
		case int64:
			return uint64(n)
		case int:
			return uint64(n)
		}
	}
	return 0
}

func getBoolFromMap(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func parseTimeFromMap(m map[string]interface{}, key string) time.Time {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case string:
			if parsed, err := time.Parse(time.RFC3339, t); err == nil {
				return parsed
			}
		case float64:
			return time.Unix(int64(t), 0)
		case int64:
			return time.Unix(t, 0)
		}
	}
	return time.Time{}
}

// isMCPSchedulerService returns true if the service is a Scheduler service.
func isMCPSchedulerService(service string) bool {
	serviceName := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		serviceName = service[idx+1:]
	}
	return serviceName == "Scheduler"
}
