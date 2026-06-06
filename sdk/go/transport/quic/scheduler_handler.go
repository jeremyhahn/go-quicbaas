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

package quic

import (
	"encoding/json"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// schedulerServicePrefix is the service name prefix for Scheduler operations.
const schedulerServicePrefix = "quicbaas.Scheduler"

// isSchedulerService reports whether svc names the QuicBaaS Scheduler service.
func isSchedulerService(svc string) bool {
	return strings.HasPrefix(svc, schedulerServicePrefix) || svc == "Scheduler"
}

// =============================================================================
// Request transformation (SDK client types -> server service.*Request wire shape)
// =============================================================================
//
// The Scheduler SDK client (client/scheduler) sends its own request structs.
// Their field names and enum encodings differ from the canonical
// pkg/service.*Request types the QUIC server unmarshals (see
// pkg/api/quic/handler_scheduler.go). For example the SDK encodes JobType/
// OwnerType as integers and uses a "tags" field, whereas the server expects
// string enums ("one-shot", "system", ...) and a "labels" field. This package
// re-encodes every request into the server's wire shape before sending so the
// QUIC path returns exactly what the embedded/gRPC paths return.

// transformSchedulerRequest converts an SDK scheduler request for the given
// method into the canonical service.*Request value the server expects. Requests
// that already match the server shape (or carry no body) are passed through.
func transformSchedulerRequest(method string, req interface{}) (interface{}, error) {
	switch method {
	case "CreateJob":
		return transformCreateJobRequest(req)
	case "UpdateJob":
		return transformUpdateJobRequest(req)
	case "GetJob":
		return mapJobIDRequest(req, func(id uint64) interface{} {
			return &service.GetSchedulerJobRequest{JobID: id}
		})
	case "DeleteJob":
		return mapJobIDRequest(req, func(id uint64) interface{} {
			return &service.DeleteSchedulerJobRequest{JobID: id}
		})
	case "TriggerJob":
		return mapJobIDRequest(req, func(id uint64) interface{} {
			return &service.TriggerSchedulerJobRequest{JobID: id}
		})
	case "PauseJob":
		return mapJobIDRequest(req, func(id uint64) interface{} {
			return &service.PauseSchedulerJobRequest{JobID: id}
		})
	case "ResumeJob":
		return mapJobIDRequest(req, func(id uint64) interface{} {
			return &service.ResumeSchedulerJobRequest{JobID: id}
		})
	case "CancelJob":
		return transformCancelJobRequest(req)
	case "ListJobs":
		return transformListJobsRequest(req)
	case "GetJobHistory":
		return transformGetJobHistoryRequest(req)
	case "GetStatus":
		// GetStatus carries no request body.
		return &struct{}{}, nil
	default:
		return nil, errors.NewInvalidInput("quic.transformSchedulerRequest",
			"unknown scheduler method: "+method)
	}
}

// jobIDOnlyWire is the shared decode shape for the SDK's job-ID-only requests
// (getJobRequest, deleteJobRequest, triggerJobRequest, ...). Those structs have
// no JSON tags, so the JobID field marshals as "JobID"; we accept both spellings
// to be robust against future tag additions.
type jobIDOnlyWire struct {
	JobID    uint64 `json:"JobID"`
	JobIDAlt uint64 `json:"job_id"`
}

func (w jobIDOnlyWire) id() uint64 {
	if w.JobID != 0 {
		return w.JobID
	}
	return w.JobIDAlt
}

// mapJobIDRequest decodes a job-ID-only SDK request and builds the corresponding
// server request via the supplied constructor.
func mapJobIDRequest(req interface{}, build func(id uint64) interface{}) (interface{}, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.mapJobIDRequest", "failed to marshal request")
	}
	var w jobIDOnlyWire
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.mapJobIDRequest", "failed to decode request")
	}
	return build(w.id()), nil
}

// transformCreateJobRequest maps the SDK CreateJobRequest to the server type.
func transformCreateJobRequest(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*scheduler.CreateJobRequest)
	if !ok {
		return nil, errors.NewInvalidInput("quic.transformCreateJobRequest",
			"expected *scheduler.CreateJobRequest")
	}

	return &service.CreateSchedulerJobRequest{
		Name:         sdkReq.Name,
		Description:  sdkReq.Description,
		OwnerType:    ownerTypeToService(sdkReq.OwnerType),
		OwnerID:      sdkReq.OwnerID,
		Type:         jobTypeToService(sdkReq.JobType),
		Schedule:     sdkReq.Schedule,
		ExecuteAt:    sdkReq.ExecuteAt,
		Delay:        sdkReq.Delay,
		Handler:      sdkReq.Handler,
		Payload:      sdkReq.Payload,
		Timeout:      sdkReq.Timeout,
		MaxRetries:   int32(sdkReq.MaxRetries),
		Labels:       sdkReq.Tags,
		NodeAffinity: sdkReq.NodeAffinity,
	}, nil
}

// transformUpdateJobRequest maps the SDK UpdateJobRequest to the server type.
func transformUpdateJobRequest(req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*scheduler.UpdateJobRequest)
	if !ok {
		return nil, errors.NewInvalidInput("quic.transformUpdateJobRequest",
			"expected *scheduler.UpdateJobRequest")
	}

	return &service.UpdateSchedulerJobRequest{
		JobID:       sdkReq.JobID,
		Name:        sdkReq.Name,
		Description: sdkReq.Description,
		Schedule:    sdkReq.Schedule,
		Payload:     sdkReq.Payload,
		Timeout:     sdkReq.Timeout,
		MaxRetries:  int32(sdkReq.MaxRetries),
		Labels:      sdkReq.Tags,
	}, nil
}

// cancelJobWire decodes the SDK cancelJobRequest, which has no JSON tags on its
// exported fields (JobID, Reason).
type cancelJobWire struct {
	JobID  uint64 `json:"JobID"`
	Reason string `json:"Reason"`
}

// transformCancelJobRequest maps the SDK cancel request to the server type.
func transformCancelJobRequest(req interface{}) (interface{}, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.transformCancelJobRequest", "failed to marshal request")
	}
	var w cancelJobWire
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.transformCancelJobRequest", "failed to decode request")
	}
	return &service.CancelSchedulerJobRequest{JobID: w.JobID, Reason: w.Reason}, nil
}

// transformListJobsRequest maps the SDK ListJobsOptions to the server type.
func transformListJobsRequest(req interface{}) (interface{}, error) {
	opts, ok := req.(*scheduler.ListJobsOptions)
	if !ok {
		// The client always passes *ListJobsOptions (never nil; it substitutes an
		// empty struct). Anything else is a programming error.
		return nil, errors.NewInvalidInput("quic.transformListJobsRequest",
			"expected *scheduler.ListJobsOptions")
	}

	out := &service.ListSchedulerJobsRequest{
		OwnerID: opts.OwnerID,
		Handler: "",
		Labels:  opts.Tags,
		Limit:   opts.Limit,
	}
	if opts.OwnerType != nil {
		out.OwnerType = ownerTypeToService(*opts.OwnerType)
	}
	if opts.Status != nil {
		out.Status = jobStatusToService(*opts.Status)
	}
	return out, nil
}

// getJobHistoryWire decodes the SDK getJobHistoryRequest.
type getJobHistoryWire struct {
	JobID   uint64                    `json:"JobID"`
	Options *scheduler.HistoryOptions `json:"Options"`
}

// transformGetJobHistoryRequest maps the SDK history request to the server type.
func transformGetJobHistoryRequest(req interface{}) (interface{}, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.transformGetJobHistoryRequest", "failed to marshal request")
	}
	var w getJobHistoryWire
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.transformGetJobHistoryRequest", "failed to decode request")
	}

	out := &service.GetSchedulerJobHistoryRequest{JobID: w.JobID}
	if w.Options != nil {
		out.Limit = w.Options.Limit
	}
	return out, nil
}

// =============================================================================
// Response decoding (server service.*Response wire shape -> SDK client types)
// =============================================================================

// schedulerResponseDecoder decodes raw wire JSON for one scheduler method into
// the SDK client type its caller expects.
type schedulerResponseDecoder func(data []byte) (interface{}, error)

// schedulerResponseDecoders maps method names to their decoders for O(1)
// dispatch. The server marshals the canonical service.*SchedulerResponse types;
// each decoder unmarshals into that type (aliased into the SDK service package)
// and maps it to the SDK client/scheduler type the client method asserts on.
var schedulerResponseDecoders = map[string]schedulerResponseDecoder{
	"CreateJob":     decodeCreateJobResponse,
	"GetJob":        decodeGetJobResponse,
	"UpdateJob":     decodeUpdateJobResponse,
	"DeleteJob":     decodeDeleteJobResponse,
	"ListJobs":      decodeListJobsResponse,
	"TriggerJob":    decodeTriggerJobResponse,
	"PauseJob":      decodePauseJobResponse,
	"ResumeJob":     decodeResumeJobResponse,
	"CancelJob":     decodeCancelJobResponse,
	"GetJobHistory": decodeGetJobHistoryResponse,
	"GetStatus":     decodeGetStatusResponse,
}

// decodeSchedulerResponse decodes raw wire bytes into the SDK scheduler type for
// the given method. An empty payload returns nil. Unknown methods are rejected
// (every scheduler method has a decoder); this prevents silent mis-routing.
func decodeSchedulerResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	decoder, ok := schedulerResponseDecoders[method]
	if !ok {
		return nil, errors.NewInvalidInput("quic.decodeSchedulerResponse",
			"unknown scheduler method: "+method)
	}
	return decoder(data)
}

func decodeCreateJobResponse(data []byte) (interface{}, error) {
	var resp service.CreateSchedulerJobResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapSchedulerDecode("CreateJob", err)
	}
	out := &scheduler.CreateJobResponse{}
	if resp.Job != nil {
		out.JobID = resp.Job.ID
		out.NextRunAt = resp.Job.NextRun
	}
	return out, nil
}

func decodeGetJobResponse(data []byte) (interface{}, error) {
	var resp service.GetSchedulerJobResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapSchedulerDecode("GetJob", err)
	}
	return jobInfoFromService(resp.Job), nil
}

func decodeUpdateJobResponse(data []byte) (interface{}, error) {
	var resp service.UpdateSchedulerJobResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapSchedulerDecode("UpdateJob", err)
	}
	return jobInfoFromService(resp.Job), nil
}

func decodeDeleteJobResponse(data []byte) (interface{}, error) {
	// The SDK client treats any non-nil *deleteJobResponse-shaped value as
	// success and ignores the body. The wire body is service.DeleteSchedulerJobResponse;
	// we validate it decodes, then signal success with an empty map so the client's
	// nil-check passes without needing the unexported deleteJobResponse type.
	var resp service.DeleteSchedulerJobResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapSchedulerDecode("DeleteJob", err)
	}
	// Returning nil is valid: the client's DeleteJob accepts a nil response as
	// success. This avoids leaking a transport-internal type to the caller.
	return nil, nil
}

func decodeListJobsResponse(data []byte) (interface{}, error) {
	var resp service.ListSchedulerJobsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapSchedulerDecode("ListJobs", err)
	}
	out := &scheduler.ListJobsResponse{
		TotalCount: resp.TotalCount,
		HasMore:    resp.HasMore,
		Jobs:       make([]scheduler.JobInfo, 0, len(resp.Jobs)),
	}
	for _, j := range resp.Jobs {
		if info := jobInfoFromService(j); info != nil {
			out.Jobs = append(out.Jobs, *info)
		}
	}
	return out, nil
}

func decodeTriggerJobResponse(data []byte) (interface{}, error) {
	var resp service.TriggerSchedulerJobResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapSchedulerDecode("TriggerJob", err)
	}
	return &scheduler.TriggerJobResponse{
		JobID:     resp.JobID,
		StartedAt: resp.Timestamp,
	}, nil
}

func decodePauseJobResponse(data []byte) (interface{}, error) {
	var resp service.PauseSchedulerJobResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapSchedulerDecode("PauseJob", err)
	}
	return &scheduler.PauseJobResponse{
		JobID:    resp.JobID,
		PausedAt: resp.Timestamp,
	}, nil
}

func decodeResumeJobResponse(data []byte) (interface{}, error) {
	var resp service.ResumeSchedulerJobResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapSchedulerDecode("ResumeJob", err)
	}
	return &scheduler.ResumeJobResponse{
		JobID:     resp.JobID,
		ResumedAt: resp.Timestamp,
	}, nil
}

func decodeCancelJobResponse(data []byte) (interface{}, error) {
	var resp service.CancelSchedulerJobResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapSchedulerDecode("CancelJob", err)
	}
	return &scheduler.CancelJobResponse{
		JobID:      resp.JobID,
		CanceledAt: resp.Timestamp,
	}, nil
}

func decodeGetJobHistoryResponse(data []byte) (interface{}, error) {
	var resp service.GetSchedulerJobHistoryResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapSchedulerDecode("GetJobHistory", err)
	}
	out := &scheduler.JobHistoryResponse{
		HasMore:    resp.HasMore,
		Executions: make([]scheduler.JobExecution, 0, len(resp.Executions)),
	}
	for _, e := range resp.Executions {
		if e == nil {
			continue
		}
		if out.JobID == 0 {
			out.JobID = e.JobID
		}
		out.Executions = append(out.Executions, scheduler.JobExecution{
			ExecutionID: e.ID,
			JobID:       e.JobID,
			Status:      jobStatusFromService(e.Status),
			StartedAt:   e.StartedAt,
			CompletedAt: e.EndedAt,
			Duration:    e.Duration,
			Error:       e.Error,
			Result:      []byte(e.Output),
		})
	}
	out.TotalCount = len(out.Executions)
	return out, nil
}

func decodeGetStatusResponse(data []byte) (interface{}, error) {
	var resp service.GetSchedulerStatusResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, wrapSchedulerDecode("GetStatus", err)
	}
	out := &scheduler.SchedulerStatus{Healthy: resp.Running}
	if resp.Stats != nil {
		out.TotalJobs = resp.Stats.JobCount
		out.RunningJobs = resp.Stats.RunningJobs
		out.CompletedJobs = resp.Stats.SuccessCount
		out.FailedJobs = resp.Stats.FailureCount
	}
	return out, nil
}

func wrapSchedulerDecode(method string, err error) error {
	return errors.Wrap(err, errors.CodeInternal,
		"quic.decodeSchedulerResponse", "failed to unmarshal "+method+" response")
}

// jobInfoFromService maps a server SchedulerJob to the SDK JobInfo type.
func jobInfoFromService(j *service.SchedulerJob) *scheduler.JobInfo {
	if j == nil {
		return nil
	}
	return &scheduler.JobInfo{
		JobID:          j.ID,
		Name:           j.Name,
		Description:    j.Description,
		JobType:        jobTypeFromService(j.Type),
		Status:         jobStatusFromService(j.Status),
		Schedule:       j.Schedule,
		Delay:          j.Delay,
		Payload:        j.Payload,
		OwnerType:      ownerTypeFromService(j.OwnerType),
		OwnerID:        j.OwnerID,
		MaxRetries:     int(j.MaxRetries),
		Timeout:        j.Timeout,
		NextRunAt:      j.NextRun,
		LastRunAt:      j.LastRun,
		CreatedAt:      j.CreatedAt,
		UpdatedAt:      j.UpdatedAt,
		ExecutionCount: j.RunCount,
		FailureCount:   j.FailCount,
		Tags:           j.Labels,
	}
}

// =============================================================================
// Enum conversions (SDK int enums <-> server string enums)
// =============================================================================

func jobTypeToService(t scheduler.JobType) service.SchedulerJobType {
	switch t {
	case scheduler.JobTypeRecurring:
		return service.SchedulerJobTypeRecurring
	case scheduler.JobTypeDelayed:
		return service.SchedulerJobTypeDelayed
	default:
		return service.SchedulerJobTypeOneShot
	}
}

func jobTypeFromService(t service.SchedulerJobType) scheduler.JobType {
	switch t {
	case service.SchedulerJobTypeRecurring:
		return scheduler.JobTypeRecurring
	case service.SchedulerJobTypeDelayed:
		return scheduler.JobTypeDelayed
	default:
		return scheduler.JobTypeOneShot
	}
}

func ownerTypeToService(t scheduler.OwnerType) service.SchedulerOwnerType {
	switch t {
	case scheduler.OwnerTypeTenant:
		return service.SchedulerOwnerTypeTenant
	case scheduler.OwnerTypeDelegated:
		return service.SchedulerOwnerTypeDelegated
	default:
		return service.SchedulerOwnerTypeSystem
	}
}

func ownerTypeFromService(t service.SchedulerOwnerType) scheduler.OwnerType {
	switch t {
	case service.SchedulerOwnerTypeTenant:
		return scheduler.OwnerTypeTenant
	case service.SchedulerOwnerTypeDelegated:
		return scheduler.OwnerTypeDelegated
	default:
		return scheduler.OwnerTypeSystem
	}
}

func jobStatusToService(s scheduler.JobStatus) service.SchedulerJobStatus {
	switch s {
	case scheduler.JobStatusRunning:
		return service.SchedulerJobStatusRunning
	case scheduler.JobStatusCompleted:
		return service.SchedulerJobStatusCompleted
	case scheduler.JobStatusFailed:
		return service.SchedulerJobStatusFailed
	case scheduler.JobStatusPaused:
		return service.SchedulerJobStatusPaused
	case scheduler.JobStatusCanceled:
		return service.SchedulerJobStatusCanceled
	default:
		return service.SchedulerJobStatusPending
	}
}

func jobStatusFromService(s service.SchedulerJobStatus) scheduler.JobStatus {
	switch s {
	case service.SchedulerJobStatusRunning:
		return scheduler.JobStatusRunning
	case service.SchedulerJobStatusCompleted:
		return scheduler.JobStatusCompleted
	case service.SchedulerJobStatusFailed:
		return scheduler.JobStatusFailed
	case service.SchedulerJobStatusPaused:
		return scheduler.JobStatusPaused
	case service.SchedulerJobStatusCanceled:
		return scheduler.JobStatusCanceled
	default:
		return scheduler.JobStatusPending
	}
}
