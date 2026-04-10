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
	"encoding/json"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// schedulerHandler transforms MCP scheduler requests and responses to SDK types.
type schedulerHandler struct{}

// newSchedulerHandler creates a new scheduler handler.
func newSchedulerHandler() *schedulerHandler {
	return &schedulerHandler{}
}

// transformRequest transforms SDK scheduler request to MCP tool arguments.
func (h *schedulerHandler) transformRequest(method string, req interface{}) (map[string]interface{}, error) {
	switch method {
	case "CreateJob":
		return h.transformCreateJobRequest(req)
	case "UpdateJob":
		return h.transformUpdateJobRequest(req)
	default:
		// For other methods, use generic JSON conversion
		return h.genericTransform(req)
	}
}

// genericTransform converts request via JSON for simple request types.
func (h *schedulerHandler) genericTransform(req interface{}) (map[string]interface{}, error) {
	if req == nil {
		return map[string]interface{}{}, nil
	}
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var args map[string]interface{}
	if err := json.Unmarshal(data, &args); err != nil {
		return nil, err
	}
	// Convert Go struct field names to MCP tool argument names
	if jobID, ok := args["JobID"]; ok {
		args["job_id"] = jobID
		delete(args, "JobID")
	}
	if tenantID, ok := args["TenantID"]; ok {
		args["tenant_id"] = tenantID
		delete(args, "TenantID")
	}
	if reason, ok := args["Reason"]; ok {
		args["reason"] = reason
		delete(args, "Reason")
	}
	return args, nil
}

// transformCreateJobRequest transforms CreateJobRequest to MCP arguments.
func (h *schedulerHandler) transformCreateJobRequest(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*scheduler.CreateJobRequest)
	if !ok {
		// Fall back to generic conversion
		return h.genericTransform(req)
	}

	args := map[string]interface{}{
		"name":       sdkReq.Name,
		"handler":    sdkReq.Handler,
		"type":       jobTypeToString(sdkReq.JobType),
		"owner_type": ownerTypeToString(sdkReq.OwnerType),
	}

	if sdkReq.Description != "" {
		args["description"] = sdkReq.Description
	}
	if sdkReq.Schedule != "" {
		args["schedule"] = sdkReq.Schedule
	}
	if !sdkReq.ExecuteAt.IsZero() {
		args["execute_at"] = sdkReq.ExecuteAt.Format(time.RFC3339)
	}
	if sdkReq.Delay > 0 {
		args["delay"] = sdkReq.Delay.String()
	}
	if len(sdkReq.Payload) > 0 {
		args["payload"] = base64.StdEncoding.EncodeToString(sdkReq.Payload)
	}
	if sdkReq.OwnerID != "" {
		args["owner_id"] = sdkReq.OwnerID
	}
	if sdkReq.MaxRetries > 0 {
		args["max_retries"] = sdkReq.MaxRetries
	}
	if sdkReq.Timeout > 0 {
		args["timeout"] = sdkReq.Timeout.String()
	}
	if len(sdkReq.Tags) > 0 {
		tagsJSON, _ := json.Marshal(sdkReq.Tags)
		args["labels"] = string(tagsJSON)
	}

	return args, nil
}

// transformUpdateJobRequest transforms UpdateJobRequest to MCP arguments.
func (h *schedulerHandler) transformUpdateJobRequest(req interface{}) (map[string]interface{}, error) {
	sdkReq, ok := req.(*scheduler.UpdateJobRequest)
	if !ok {
		// Fall back to generic conversion
		return h.genericTransform(req)
	}

	args := map[string]interface{}{
		"job_id": sdkReq.JobID,
	}

	if sdkReq.Name != "" {
		args["name"] = sdkReq.Name
	}
	if sdkReq.Description != "" {
		args["description"] = sdkReq.Description
	}
	if sdkReq.Schedule != "" {
		args["schedule"] = sdkReq.Schedule
	}
	if sdkReq.Delay > 0 {
		args["delay"] = sdkReq.Delay.String()
	}
	if len(sdkReq.Payload) > 0 {
		args["payload"] = base64.StdEncoding.EncodeToString(sdkReq.Payload)
	}
	if sdkReq.MaxRetries > 0 {
		args["max_retries"] = sdkReq.MaxRetries
	}
	if sdkReq.Timeout > 0 {
		args["timeout"] = sdkReq.Timeout.String()
	}

	return args, nil
}

// jobTypeToString converts JobType enum to string.
func jobTypeToString(jt scheduler.JobType) string {
	switch jt {
	case scheduler.JobTypeOneShot:
		return "one-shot"
	case scheduler.JobTypeRecurring:
		return "recurring"
	case scheduler.JobTypeDelayed:
		return "delayed"
	default:
		return "one-shot"
	}
}

// ownerTypeToString converts OwnerType enum to string.
func ownerTypeToString(ot scheduler.OwnerType) string {
	switch ot {
	case scheduler.OwnerTypeSystem:
		return "system"
	case scheduler.OwnerTypeTenant:
		return "tenant"
	case scheduler.OwnerTypeDelegated:
		return "delegated"
	default:
		return "system"
	}
}

// transformResponse transforms MCP tool response to SDK scheduler types.
func (h *schedulerHandler) transformResponse(method string, resp interface{}) (interface{}, error) {
	switch method {
	case "CreateJob":
		return h.transformCreateJobResponse(resp)
	case "GetJob":
		return h.transformGetJobResponse(resp)
	case "UpdateJob":
		return h.transformUpdateJobResponse(resp)
	case "DeleteJob":
		// DeleteJob returns nil on success
		return nil, nil
	case "ListJobs":
		return h.transformListJobsResponse(resp)
	case "TriggerJob":
		return h.transformTriggerJobResponse(resp)
	case "PauseJob":
		return h.transformPauseJobResponse(resp)
	case "ResumeJob":
		return h.transformResumeJobResponse(resp)
	case "CancelJob":
		return h.transformCancelJobResponse(resp)
	case "GetJobHistory":
		return h.transformGetJobHistoryResponse(resp)
	case "GetStatus":
		return h.transformGetStatusResponse(resp)
	default:
		return resp, nil
	}
}

// transformCreateJobResponse transforms CreateJob response.
func (h *schedulerHandler) transformCreateJobResponse(resp interface{}) (*scheduler.CreateJobResponse, error) {
	if resp == nil {
		return &scheduler.CreateJobResponse{}, nil
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		// Try JSON round-trip
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	// MCP server returns: {job: {id: ..., ...}, timestamp: ...}
	// Try to extract job_id from nested "job" object first
	var jobID uint64
	var nextRunAt time.Time

	if jobData, ok := data["job"].(map[string]interface{}); ok {
		// Get ID from nested job object (MCP format uses "id" not "job_id")
		jobID = getUint64FromMap(jobData, "id")
		if jobID == 0 {
			jobID = getUint64FromMap(jobData, "job_id")
		}
		nextRunAt = parseTimeFromMap(jobData, "next_run_at")
	}

	// Fallback to top-level fields if job object not found
	if jobID == 0 {
		jobID = getUint64FromMap(data, "job_id")
	}
	if nextRunAt.IsZero() {
		nextRunAt = parseTimeFromMap(data, "next_run_at")
	}

	return &scheduler.CreateJobResponse{
		JobID:     jobID,
		NextRunAt: nextRunAt,
	}, nil
}

// transformGetJobResponse transforms GetJob response.
func (h *schedulerHandler) transformGetJobResponse(resp interface{}) (*scheduler.JobInfo, error) {
	if resp == nil {
		return nil, nil
	}

	// MCP server returns {job: {...}, timestamp: ...} - extract nested job
	data, ok := resp.(map[string]interface{})
	if !ok {
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	// Try to get nested job object
	if jobData, ok := data["job"]; ok {
		return h.parseJobInfo(jobData)
	}

	// Fallback to parsing response directly
	return h.parseJobInfo(resp)
}

// transformUpdateJobResponse transforms UpdateJob response.
func (h *schedulerHandler) transformUpdateJobResponse(resp interface{}) (*scheduler.JobInfo, error) {
	if resp == nil {
		return nil, nil
	}

	// MCP server returns {job: {...}, timestamp: ...} - extract nested job
	data, ok := resp.(map[string]interface{})
	if !ok {
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	// Try to get nested job object
	if jobData, ok := data["job"]; ok {
		return h.parseJobInfo(jobData)
	}

	// Fallback to parsing response directly
	return h.parseJobInfo(resp)
}

// transformListJobsResponse transforms ListJobs response.
func (h *schedulerHandler) transformListJobsResponse(resp interface{}) (*scheduler.ListJobsResponse, error) {
	if resp == nil {
		return &scheduler.ListJobsResponse{}, nil
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	result := &scheduler.ListJobsResponse{
		TotalCount: int(getInt64FromMap(data, "total_count")),
		HasMore:    getBoolFromMap(data, "has_more"),
	}

	// Parse jobs array
	if jobsRaw, ok := data["jobs"].([]interface{}); ok {
		result.Jobs = make([]scheduler.JobInfo, 0, len(jobsRaw))
		for _, jobRaw := range jobsRaw {
			if jobInfo, err := h.parseJobInfo(jobRaw); err == nil && jobInfo != nil {
				result.Jobs = append(result.Jobs, *jobInfo)
			}
		}
	}

	return result, nil
}

// transformTriggerJobResponse transforms TriggerJob response.
func (h *schedulerHandler) transformTriggerJobResponse(resp interface{}) (*scheduler.TriggerJobResponse, error) {
	if resp == nil {
		return &scheduler.TriggerJobResponse{}, nil
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	return &scheduler.TriggerJobResponse{
		JobID:       getUint64FromMap(data, "job_id"),
		ExecutionID: getUint64FromMap(data, "execution_id"),
		StartedAt:   parseTimeFromMap(data, "started_at"),
	}, nil
}

// transformPauseJobResponse transforms PauseJob response.
func (h *schedulerHandler) transformPauseJobResponse(resp interface{}) (*scheduler.PauseJobResponse, error) {
	if resp == nil {
		return &scheduler.PauseJobResponse{}, nil
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	return &scheduler.PauseJobResponse{
		JobID:    getUint64FromMap(data, "job_id"),
		PausedAt: parseTimeFromMap(data, "paused_at"),
	}, nil
}

// transformResumeJobResponse transforms ResumeJob response.
func (h *schedulerHandler) transformResumeJobResponse(resp interface{}) (*scheduler.ResumeJobResponse, error) {
	if resp == nil {
		return &scheduler.ResumeJobResponse{}, nil
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	return &scheduler.ResumeJobResponse{
		JobID:     getUint64FromMap(data, "job_id"),
		ResumedAt: parseTimeFromMap(data, "resumed_at"),
		NextRunAt: parseTimeFromMap(data, "next_run_at"),
	}, nil
}

// transformCancelJobResponse transforms CancelJob response.
func (h *schedulerHandler) transformCancelJobResponse(resp interface{}) (*scheduler.CancelJobResponse, error) {
	if resp == nil {
		return &scheduler.CancelJobResponse{}, nil
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	return &scheduler.CancelJobResponse{
		JobID:      getUint64FromMap(data, "job_id"),
		CanceledAt: parseTimeFromMap(data, "canceled_at"),
		Reason:     getStringFromMap(data, "reason"),
	}, nil
}

// transformGetJobHistoryResponse transforms GetJobHistory response.
func (h *schedulerHandler) transformGetJobHistoryResponse(resp interface{}) (*scheduler.JobHistoryResponse, error) {
	if resp == nil {
		return &scheduler.JobHistoryResponse{}, nil
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	result := &scheduler.JobHistoryResponse{
		JobID:      getUint64FromMap(data, "job_id"),
		TotalCount: int(getInt64FromMap(data, "total_count")),
		HasMore:    getBoolFromMap(data, "has_more"),
	}

	// Parse executions array
	if execsRaw, ok := data["executions"].([]interface{}); ok {
		result.Executions = make([]scheduler.JobExecution, 0, len(execsRaw))
		for _, execRaw := range execsRaw {
			if exec, err := h.parseJobExecution(execRaw); err == nil && exec != nil {
				result.Executions = append(result.Executions, *exec)
			}
		}
	}

	return result, nil
}

// transformGetStatusResponse transforms GetStatus response.
func (h *schedulerHandler) transformGetStatusResponse(resp interface{}) (*scheduler.SchedulerStatus, error) {
	if resp == nil {
		return &scheduler.SchedulerStatus{}, nil
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	return &scheduler.SchedulerStatus{
		Healthy:         getBoolFromMap(data, "healthy"),
		TotalJobs:       getUint64FromMap(data, "total_jobs"),
		RunningJobs:     getUint64FromMap(data, "running_jobs"),
		PendingJobs:     getUint64FromMap(data, "pending_jobs"),
		CompletedJobs:   getUint64FromMap(data, "completed_jobs"),
		FailedJobs:      getUint64FromMap(data, "failed_jobs"),
		PausedJobs:      getUint64FromMap(data, "paused_jobs"),
		CanceledJobs:    getUint64FromMap(data, "canceled_jobs"),
		ExecutionsToday: getUint64FromMap(data, "executions_today"),
		FailuresToday:   getUint64FromMap(data, "failures_today"),
		AverageLatency:  time.Duration(getInt64FromMap(data, "average_latency")),
		WorkerCount:     int(getInt64FromMap(data, "worker_count")),
		QueueDepth:      int(getInt64FromMap(data, "queue_depth")),
		LastHeartbeat:   parseTimeFromMap(data, "last_heartbeat"),
		UptimeDuration:  time.Duration(getInt64FromMap(data, "uptime_duration")),
	}, nil
}

// parseJobInfo parses a raw response into a JobInfo struct.
func (h *schedulerHandler) parseJobInfo(resp interface{}) (*scheduler.JobInfo, error) {
	if resp == nil {
		return nil, nil
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	// MCP server uses "id" but SDK uses "job_id" - try both
	jobID := getUint64FromMap(data, "id")
	if jobID == 0 {
		jobID = getUint64FromMap(data, "job_id")
	}

	// MCP server uses "type" but SDK expects "job_type" - try both
	jobTypeStr := getStringFromMap(data, "type")
	if jobTypeStr == "" {
		jobTypeStr = getStringFromMap(data, "job_type")
	}

	jobInfo := &scheduler.JobInfo{
		JobID:          jobID,
		Name:           getStringFromMap(data, "name"),
		Description:    getStringFromMap(data, "description"),
		JobType:        parseJobType(jobTypeStr),
		Status:         parseJobStatus(getStringFromMap(data, "status")),
		Schedule:       getStringFromMap(data, "schedule"),
		Payload:        getBytesFromMap(data, "payload"),
		OwnerType:      parseOwnerType(getStringFromMap(data, "owner_type")),
		OwnerID:        getStringFromMap(data, "owner_id"),
		MaxRetries:     int(getInt64FromMap(data, "max_retries")),
		Timeout:        time.Duration(getInt64FromMap(data, "timeout")),
		ExecutionCount: getUint64FromMap(data, "execution_count"),
		FailureCount:   getUint64FromMap(data, "failure_count"),
		NextRunAt:      parseTimeFromMap(data, "next_run_at"),
		LastRunAt:      parseTimeFromMap(data, "last_run_at"),
		CreatedAt:      parseTimeFromMap(data, "created_at"),
		UpdatedAt:      parseTimeFromMap(data, "updated_at"),
	}

	// Parse tags
	if tagsRaw, ok := data["tags"].(map[string]interface{}); ok {
		jobInfo.Tags = make(map[string]string)
		for k, v := range tagsRaw {
			if s, ok := v.(string); ok {
				jobInfo.Tags[k] = s
			}
		}
	}

	return jobInfo, nil
}

// parseJobExecution parses a raw response into a JobExecution struct.
func (h *schedulerHandler) parseJobExecution(resp interface{}) (*scheduler.JobExecution, error) {
	if resp == nil {
		return nil, nil
	}

	data, ok := resp.(map[string]interface{})
	if !ok {
		jsonBytes, err := json.Marshal(resp)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			return nil, err
		}
	}

	// MCP server uses "id" but SDK expects "execution_id" - try both
	execID := getUint64FromMap(data, "id")
	if execID == 0 {
		execID = getUint64FromMap(data, "execution_id")
	}

	return &scheduler.JobExecution{
		ExecutionID: execID,
		JobID:       getUint64FromMap(data, "job_id"),
		Status:      parseJobStatus(getStringFromMap(data, "status")),
		StartedAt:   parseTimeFromMap(data, "started_at"),
		CompletedAt: parseTimeFromMap(data, "completed_at"),
		Duration:    time.Duration(getInt64FromMap(data, "duration")),
		RetryCount:  int(getInt64FromMap(data, "retry_count")),
		Error:       getStringFromMap(data, "error"),
		Result:      getBytesFromMap(data, "result"),
	}, nil
}

// parseJobType converts a string to JobType.
func parseJobType(s string) scheduler.JobType {
	switch strings.ToLower(s) {
	case "one-shot", "oneshot", "one_shot", "0":
		return scheduler.JobTypeOneShot
	case "recurring", "1":
		return scheduler.JobTypeRecurring
	case "delayed", "2":
		return scheduler.JobTypeDelayed
	default:
		return scheduler.JobTypeOneShot
	}
}

// parseJobStatus converts a string to JobStatus.
func parseJobStatus(s string) scheduler.JobStatus {
	switch strings.ToLower(s) {
	case "pending", "0":
		return scheduler.JobStatusPending
	case "running", "1":
		return scheduler.JobStatusRunning
	case "completed", "2":
		return scheduler.JobStatusCompleted
	case "failed", "3":
		return scheduler.JobStatusFailed
	case "paused", "4":
		return scheduler.JobStatusPaused
	case "canceled", "cancelled", "5":
		return scheduler.JobStatusCanceled
	default:
		return scheduler.JobStatusPending
	}
}

// parseOwnerType converts a string to OwnerType.
func parseOwnerType(s string) scheduler.OwnerType {
	switch strings.ToLower(s) {
	case "system", "0":
		return scheduler.OwnerTypeSystem
	case "tenant", "1":
		return scheduler.OwnerTypeTenant
	case "delegated", "2":
		return scheduler.OwnerTypeDelegated
	default:
		return scheduler.OwnerTypeSystem
	}
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

func getBytesFromMap(m map[string]interface{}, key string) []byte {
	if v, ok := m[key]; ok {
		switch b := v.(type) {
		case []byte:
			return b
		case string:
			return []byte(b)
		}
	}
	return nil
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
