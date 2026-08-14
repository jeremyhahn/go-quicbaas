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
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestIsSchedulerService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.Scheduler", true},
		{"Scheduler", true},
		{"quicbaas.SchedulerExtra", true}, // HasPrefix matches the canonical name
		{"quicbaas.Backup", false},
		{"quicbaas.Crypto", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isSchedulerService(tt.svc), tt.svc)
	}
}

// TestSchedulerOpcodeAndSubOp proves every scheduler method maps to the single
// dispatch opcode OpScheduler (0x00) with a unique sub-op in the 0x01-0x1F range,
// matching the server's handleSchedulerDispatch.
func TestSchedulerOpcodeAndSubOp(t *testing.T) {
	methods := map[string]byte{
		"CreateJob":     SubOpSchedulerCreateJob,
		"GetJob":        SubOpSchedulerGetJob,
		"UpdateJob":     SubOpSchedulerUpdateJob,
		"DeleteJob":     SubOpSchedulerDeleteJob,
		"ListJobs":      SubOpSchedulerListJobs,
		"TriggerJob":    SubOpSchedulerTriggerJob,
		"PauseJob":      SubOpSchedulerPauseJob,
		"ResumeJob":     SubOpSchedulerResumeJob,
		"CancelJob":     SubOpSchedulerCancelJob,
		"GetJobHistory": SubOpSchedulerGetJobHistory,
		"GetStatus":     SubOpSchedulerGetStatus,
	}
	for method, wantSub := range methods {
		op, ok := MethodToOpcode("quicbaas.Scheduler", method)
		require.True(t, ok, method)
		assert.Equal(t, OpScheduler, op, method)
		assert.Equal(t, byte(0x00), op, method)

		gotSub, ok := SchedulerMethodToSubOp(method)
		require.True(t, ok, method)
		assert.Equal(t, wantSub, gotSub, method)
		assert.GreaterOrEqual(t, gotSub, byte(0x01), method)
		assert.LessOrEqual(t, gotSub, byte(0x1F), method)
	}

	_, ok := SchedulerMethodToSubOp("Nope")
	assert.False(t, ok)
}

// TestDecodeSchedulerResponse_CreateJob proves the server wire shape
// (EncodePayload of service.CreateSchedulerJobResponse, which nests the job
// under "job" and uses string enums) round-trips into the SDK CreateJobResponse.
func TestDecodeSchedulerResponse_CreateJob(t *testing.T) {
	next := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	payload, err := EncodePayload(&service.CreateSchedulerJobResponse{
		Job: &service.SchedulerJob{
			ID:      42,
			Name:    "nightly",
			Type:    service.SchedulerJobTypeRecurring,
			Status:  service.SchedulerJobStatusPending,
			NextRun: next,
		},
		Timestamp: time.Now(),
	})
	require.NoError(t, err)

	got, err := decodeSchedulerResponse("CreateJob", payload)
	require.NoError(t, err)
	resp, ok := got.(*scheduler.CreateJobResponse)
	require.True(t, ok, "expected *scheduler.CreateJobResponse, got %T", got)
	assert.Equal(t, uint64(42), resp.JobID)
	assert.True(t, next.Equal(resp.NextRunAt))
}

// TestDecodeSchedulerResponse_GetUpdateJobInfo proves the GetJob and UpdateJob
// responses (both nest under "job") decode into *scheduler.JobInfo with the SDK
// int enums correctly translated from the server string enums.
func TestDecodeSchedulerResponse_GetUpdateJobInfo(t *testing.T) {
	job := &service.SchedulerJob{
		ID:          7,
		Name:        "backup",
		Description: "daily backup",
		Type:        service.SchedulerJobTypeDelayed,
		Status:      service.SchedulerJobStatusRunning,
		Schedule:    "@daily",
		Delay:       5 * time.Minute,
		Payload:     []byte("body"),
		OwnerType:   service.SchedulerOwnerTypeTenant,
		OwnerID:     "tenant-1",
		MaxRetries:  3,
		Timeout:     30 * time.Second,
		RunCount:    9,
		FailCount:   1,
		Labels:      map[string]string{"env": "prod"},
	}

	for _, method := range []string{"GetJob", "UpdateJob"} {
		var payload []byte
		var err error
		if method == "GetJob" {
			payload, err = EncodePayload(&service.GetSchedulerJobResponse{Job: job})
		} else {
			payload, err = EncodePayload(&service.UpdateSchedulerJobResponse{Job: job})
		}
		require.NoError(t, err)

		got, err := decodeSchedulerResponse(method, payload)
		require.NoError(t, err)
		info, ok := got.(*scheduler.JobInfo)
		require.True(t, ok, "%s: expected *scheduler.JobInfo, got %T", method, got)
		assert.Equal(t, uint64(7), info.JobID)
		assert.Equal(t, "backup", info.Name)
		assert.Equal(t, scheduler.JobTypeDelayed, info.JobType)
		assert.Equal(t, scheduler.JobStatusRunning, info.Status)
		assert.Equal(t, scheduler.OwnerTypeTenant, info.OwnerType)
		assert.Equal(t, "tenant-1", info.OwnerID)
		assert.Equal(t, 3, info.MaxRetries)
		assert.Equal(t, 5*time.Minute, info.Delay)
		assert.Equal(t, uint64(9), info.ExecutionCount)
		assert.Equal(t, uint64(1), info.FailureCount)
		assert.Equal(t, map[string]string{"env": "prod"}, info.Tags)
	}
}

func TestDecodeSchedulerResponse_DeleteJob(t *testing.T) {
	payload, err := EncodePayload(&service.DeleteSchedulerJobResponse{JobID: 1, Deleted: true})
	require.NoError(t, err)

	got, err := decodeSchedulerResponse("DeleteJob", payload)
	require.NoError(t, err)
	// DeleteJob signals success with a nil value; the SDK client treats nil as success.
	assert.Nil(t, got)
}

func TestDecodeSchedulerResponse_ListJobs(t *testing.T) {
	payload, err := EncodePayload(&service.ListSchedulerJobsResponse{
		Jobs: []*service.SchedulerJob{
			{ID: 1, Name: "a", Type: service.SchedulerJobTypeOneShot, Status: service.SchedulerJobStatusCompleted},
			{ID: 2, Name: "b", Type: service.SchedulerJobTypeRecurring, Status: service.SchedulerJobStatusPaused},
		},
		TotalCount: 2,
		HasMore:    true,
	})
	require.NoError(t, err)

	got, err := decodeSchedulerResponse("ListJobs", payload)
	require.NoError(t, err)
	resp, ok := got.(*scheduler.ListJobsResponse)
	require.True(t, ok, "expected *scheduler.ListJobsResponse, got %T", got)
	assert.Equal(t, 2, resp.TotalCount)
	assert.True(t, resp.HasMore)
	require.Len(t, resp.Jobs, 2)
	assert.Equal(t, uint64(1), resp.Jobs[0].JobID)
	assert.Equal(t, scheduler.JobStatusCompleted, resp.Jobs[0].Status)
	assert.Equal(t, scheduler.JobStatusPaused, resp.Jobs[1].Status)
}

func TestDecodeSchedulerResponse_Control(t *testing.T) {
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	trigger, err := EncodePayload(&service.TriggerSchedulerJobResponse{JobID: 3, Timestamp: ts})
	require.NoError(t, err)
	pause, err := EncodePayload(&service.PauseSchedulerJobResponse{JobID: 4, Timestamp: ts})
	require.NoError(t, err)
	resume, err := EncodePayload(&service.ResumeSchedulerJobResponse{JobID: 5, Timestamp: ts})
	require.NoError(t, err)
	cancel, err := EncodePayload(&service.CancelSchedulerJobResponse{JobID: 6, Timestamp: ts})
	require.NoError(t, err)

	gotTrigger, err := decodeSchedulerResponse("TriggerJob", trigger)
	require.NoError(t, err)
	tr := gotTrigger.(*scheduler.TriggerJobResponse)
	assert.Equal(t, uint64(3), tr.JobID)
	assert.True(t, ts.Equal(tr.StartedAt))

	gotPause, err := decodeSchedulerResponse("PauseJob", pause)
	require.NoError(t, err)
	pr := gotPause.(*scheduler.PauseJobResponse)
	assert.Equal(t, uint64(4), pr.JobID)
	assert.True(t, ts.Equal(pr.PausedAt))

	gotResume, err := decodeSchedulerResponse("ResumeJob", resume)
	require.NoError(t, err)
	rr := gotResume.(*scheduler.ResumeJobResponse)
	assert.Equal(t, uint64(5), rr.JobID)
	assert.True(t, ts.Equal(rr.ResumedAt))

	gotCancel, err := decodeSchedulerResponse("CancelJob", cancel)
	require.NoError(t, err)
	cr := gotCancel.(*scheduler.CancelJobResponse)
	assert.Equal(t, uint64(6), cr.JobID)
	assert.True(t, ts.Equal(cr.CanceledAt))
}

func TestDecodeSchedulerResponse_GetJobHistory(t *testing.T) {
	started := time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC)
	ended := started.Add(2 * time.Second)
	payload, err := EncodePayload(&service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{
			{ID: 100, JobID: 9, Status: service.SchedulerJobStatusCompleted, StartedAt: started, EndedAt: ended, Duration: 2 * time.Second, Output: "ok"},
			{ID: 101, JobID: 9, Status: service.SchedulerJobStatusFailed, StartedAt: started, Error: "boom"},
		},
		HasMore: false,
	})
	require.NoError(t, err)

	got, err := decodeSchedulerResponse("GetJobHistory", payload)
	require.NoError(t, err)
	resp, ok := got.(*scheduler.JobHistoryResponse)
	require.True(t, ok, "expected *scheduler.JobHistoryResponse, got %T", got)
	assert.Equal(t, uint64(9), resp.JobID)
	assert.Equal(t, 2, resp.TotalCount)
	require.Len(t, resp.Executions, 2)
	assert.Equal(t, uint64(100), resp.Executions[0].ExecutionID)
	assert.Equal(t, scheduler.JobStatusCompleted, resp.Executions[0].Status)
	assert.Equal(t, 2*time.Second, resp.Executions[0].Duration)
	assert.Equal(t, []byte("ok"), resp.Executions[0].Result)
	assert.Equal(t, "boom", resp.Executions[1].Error)
}

func TestDecodeSchedulerResponse_GetStatus(t *testing.T) {
	payload, err := EncodePayload(&service.GetSchedulerStatusResponse{
		Running: true,
		Stats: &service.SchedulerStats{
			JobCount:     10,
			RunningJobs:  2,
			SuccessCount: 7,
			FailureCount: 1,
		},
	})
	require.NoError(t, err)

	got, err := decodeSchedulerResponse("GetStatus", payload)
	require.NoError(t, err)
	resp, ok := got.(*scheduler.SchedulerStatus)
	require.True(t, ok, "expected *scheduler.SchedulerStatus, got %T", got)
	assert.True(t, resp.Healthy)
	assert.Equal(t, uint64(10), resp.TotalJobs)
	assert.Equal(t, uint64(2), resp.RunningJobs)
	assert.Equal(t, uint64(7), resp.CompletedJobs)
	assert.Equal(t, uint64(1), resp.FailedJobs)
}

// TestDecodeSchedulerResponse_Errors covers the empty-payload short circuit, the
// unknown-method rejection, and an invalid-JSON payload.
func TestDecodeSchedulerResponse_Errors(t *testing.T) {
	got, err := decodeSchedulerResponse("GetStatus", nil)
	require.NoError(t, err)
	assert.Nil(t, got)

	_, err = decodeSchedulerResponse("NopeMethod", []byte(`{}`))
	require.Error(t, err)

	_, err = decodeSchedulerResponse("CreateJob", []byte(`{not json`))
	require.Error(t, err)
}

// TestTransformSchedulerRequest_CreateJob proves the SDK request is re-encoded
// into the server wire shape: int enums become string enums, "tags" becomes
// "labels", and the canonical field names/tags are emitted.
func TestTransformSchedulerRequest_CreateJob(t *testing.T) {
	out, err := transformSchedulerRequest("CreateJob", &scheduler.CreateJobRequest{
		Name:       "job",
		Handler:    "noop",
		JobType:    scheduler.JobTypeRecurring,
		OwnerType:  scheduler.OwnerTypeTenant,
		OwnerID:    "t1",
		Schedule:   "@daily",
		MaxRetries: 4,
		Tags:       map[string]string{"k": "v"},
	})
	require.NoError(t, err)

	svcReq, ok := out.(*service.CreateSchedulerJobRequest)
	require.True(t, ok)
	assert.Equal(t, service.SchedulerJobTypeRecurring, svcReq.Type)
	assert.Equal(t, service.SchedulerOwnerTypeTenant, svcReq.OwnerType)
	assert.Equal(t, int32(4), svcReq.MaxRetries)
	assert.Equal(t, map[string]string{"k": "v"}, svcReq.Labels)

	// Confirm the wire JSON uses the server's string-enum + labels shape.
	data, err := json.Marshal(svcReq)
	require.NoError(t, err)
	var wire map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &wire))
	assert.Equal(t, "recurring", wire["type"])
	assert.Equal(t, "tenant", wire["owner_type"])
	assert.Contains(t, wire, "labels")
	assert.NotContains(t, wire, "tags")
}

// TestTransformSchedulerRequest_JobIDOnly proves the job-ID-only requests map to
// the matching server request type with job_id preserved.
func TestTransformSchedulerRequest_JobIDOnly(t *testing.T) {
	get, err := transformSchedulerRequest("GetJob", &struct {
		JobID uint64
	}{JobID: 11})
	require.NoError(t, err)
	assert.Equal(t, uint64(11), get.(*service.GetSchedulerJobRequest).JobID)

	cancel, err := transformSchedulerRequest("CancelJob", &struct {
		JobID  uint64
		Reason string
	}{JobID: 12, Reason: "stop"})
	require.NoError(t, err)
	cr := cancel.(*service.CancelSchedulerJobRequest)
	assert.Equal(t, uint64(12), cr.JobID)
	assert.Equal(t, "stop", cr.Reason)
}

func TestTransformSchedulerRequest_Errors(t *testing.T) {
	_, err := transformSchedulerRequest("Nope", nil)
	require.Error(t, err)

	// CreateJob with a wrong concrete type must be rejected, not silently encoded.
	_, err = transformSchedulerRequest("CreateJob", map[string]string{"name": "x"})
	require.Error(t, err)

	_, err = transformSchedulerRequest("ListJobs", "not-options")
	require.Error(t, err)
}

// TestDoRequest_Scheduler drives the full doRequest path through the mock server
// for every scheduler method, asserting the wire opcode is OpScheduler (0x00),
// the sub-op byte is prepended correctly, and each response decodes into the
// canonical SDK type the scheduler client expects.
func TestDoRequest_Scheduler(t *testing.T) {
	server, err := newMockQUICServer(t)
	require.NoError(t, err)

	// Capture the sub-op byte the transport prepends for each call so we can
	// assert correct dispatch. The mock keys handlers by opcode; all scheduler
	// calls share OpScheduler, so one handler inspects payload[0].
	type captured struct {
		subOp   byte
		hasBody bool
	}
	calls := make(chan captured, 1)

	respFor := func(subOp byte) []byte {
		switch subOp {
		case SubOpSchedulerCreateJob:
			p, _ := EncodePayload(&service.CreateSchedulerJobResponse{Job: &service.SchedulerJob{ID: 1}})
			return p
		case SubOpSchedulerGetJob:
			p, _ := EncodePayload(&service.GetSchedulerJobResponse{Job: &service.SchedulerJob{ID: 2}})
			return p
		case SubOpSchedulerUpdateJob:
			p, _ := EncodePayload(&service.UpdateSchedulerJobResponse{Job: &service.SchedulerJob{ID: 3}})
			return p
		case SubOpSchedulerDeleteJob:
			p, _ := EncodePayload(&service.DeleteSchedulerJobResponse{JobID: 4, Deleted: true})
			return p
		case SubOpSchedulerListJobs:
			p, _ := EncodePayload(&service.ListSchedulerJobsResponse{
				Jobs: []*service.SchedulerJob{{ID: 5}}, TotalCount: 1})
			return p
		case SubOpSchedulerTriggerJob:
			p, _ := EncodePayload(&service.TriggerSchedulerJobResponse{JobID: 6})
			return p
		case SubOpSchedulerPauseJob:
			p, _ := EncodePayload(&service.PauseSchedulerJobResponse{JobID: 7})
			return p
		case SubOpSchedulerResumeJob:
			p, _ := EncodePayload(&service.ResumeSchedulerJobResponse{JobID: 8})
			return p
		case SubOpSchedulerCancelJob:
			p, _ := EncodePayload(&service.CancelSchedulerJobResponse{JobID: 9})
			return p
		case SubOpSchedulerGetJobHistory:
			p, _ := EncodePayload(&service.GetSchedulerJobHistoryResponse{
				Executions: []*service.SchedulerJobExecution{{ID: 1, JobID: 10}}})
			return p
		case SubOpSchedulerGetStatus:
			p, _ := EncodePayload(&service.GetSchedulerStatusResponse{
				Running: true, Stats: &service.SchedulerStats{JobCount: 11}})
			return p
		default:
			return nil
		}
	}

	server.RegisterHandler(OpScheduler, func(req *Request) *Response {
		require.NotEmpty(t, req.Payload, "scheduler request must carry a sub-op byte")
		subOp := req.Payload[0]
		select {
		case calls <- captured{subOp: subOp, hasBody: len(req.Payload) > 1}:
		default:
		}
		return &Response{Status: StatusOK, Payload: respFor(subOp)}
	})

	trans := newDispatchTestTransport(t, server)
	ctx := context.Background()
	sc := scheduler.NewClient(trans)

	assertSubOp := func(t *testing.T, want byte) {
		t.Helper()
		select {
		case c := <-calls:
			assert.Equal(t, want, c.subOp)
			assert.True(t, c.hasBody, "scheduler payload must carry a JSON body after the sub-op byte")
		default:
			t.Fatal("server did not record the scheduler call")
		}
	}

	t.Run("CreateJob", func(t *testing.T) {
		resp, err := sc.CreateJob(ctx, &scheduler.CreateJobRequest{
			Name: "j", Handler: "noop", JobType: scheduler.JobTypeOneShot, OwnerType: scheduler.OwnerTypeSystem,
		})
		require.NoError(t, err)
		assert.Equal(t, uint64(1), resp.JobID)
		assertSubOp(t, SubOpSchedulerCreateJob)
	})

	t.Run("GetJob", func(t *testing.T) {
		info, err := sc.GetJob(ctx, 2)
		require.NoError(t, err)
		assert.Equal(t, uint64(2), info.JobID)
		assertSubOp(t, SubOpSchedulerGetJob)
	})

	t.Run("UpdateJob", func(t *testing.T) {
		info, err := sc.UpdateJob(ctx, &scheduler.UpdateJobRequest{JobID: 3, Name: "x"})
		require.NoError(t, err)
		assert.Equal(t, uint64(3), info.JobID)
		assertSubOp(t, SubOpSchedulerUpdateJob)
	})

	t.Run("DeleteJob", func(t *testing.T) {
		require.NoError(t, sc.DeleteJob(ctx, 4))
		assertSubOp(t, SubOpSchedulerDeleteJob)
	})

	t.Run("ListJobs", func(t *testing.T) {
		resp, err := sc.ListJobs(ctx, &scheduler.ListJobsOptions{Limit: 10})
		require.NoError(t, err)
		assert.Equal(t, 1, resp.TotalCount)
		require.Len(t, resp.Jobs, 1)
		assert.Equal(t, uint64(5), resp.Jobs[0].JobID)
		assertSubOp(t, SubOpSchedulerListJobs)
	})

	t.Run("TriggerJob", func(t *testing.T) {
		resp, err := sc.TriggerJob(ctx, 6)
		require.NoError(t, err)
		assert.Equal(t, uint64(6), resp.JobID)
		assertSubOp(t, SubOpSchedulerTriggerJob)
	})

	t.Run("PauseJob", func(t *testing.T) {
		resp, err := sc.PauseJob(ctx, 7)
		require.NoError(t, err)
		assert.Equal(t, uint64(7), resp.JobID)
		assertSubOp(t, SubOpSchedulerPauseJob)
	})

	t.Run("ResumeJob", func(t *testing.T) {
		resp, err := sc.ResumeJob(ctx, 8)
		require.NoError(t, err)
		assert.Equal(t, uint64(8), resp.JobID)
		assertSubOp(t, SubOpSchedulerResumeJob)
	})

	t.Run("CancelJob", func(t *testing.T) {
		resp, err := sc.CancelJob(ctx, 9, "reason")
		require.NoError(t, err)
		assert.Equal(t, uint64(9), resp.JobID)
		assertSubOp(t, SubOpSchedulerCancelJob)
	})

	t.Run("GetJobHistory", func(t *testing.T) {
		resp, err := sc.GetJobHistory(ctx, 10, nil)
		require.NoError(t, err)
		assert.Equal(t, uint64(10), resp.JobID)
		require.Len(t, resp.Executions, 1)
		assertSubOp(t, SubOpSchedulerGetJobHistory)
	})

	t.Run("GetStatus", func(t *testing.T) {
		resp, err := sc.GetStatus(ctx)
		require.NoError(t, err)
		assert.True(t, resp.Healthy)
		assert.Equal(t, uint64(11), resp.TotalJobs)
		assertSubOp(t, SubOpSchedulerGetStatus)
	})
}

// TestDoRequest_SchedulerServerError proves a non-OK server status surfaces as a
// typed error and is not silently swallowed.
func TestDoRequest_SchedulerServerError(t *testing.T) {
	server, err := newMockQUICServer(t)
	require.NoError(t, err)
	server.RegisterHandler(OpScheduler, func(*Request) *Response {
		return &Response{Status: StatusNotFound, Payload: []byte("job not found")}
	})
	trans := newDispatchTestTransport(t, server)

	sc := scheduler.NewClient(trans)
	_, err = sc.GetJob(context.Background(), 99)
	require.Error(t, err)
}

// TestSchedulerEnumConversions exercises every arm of the enum mappers in both
// directions, including the SDK->service status mapping used by ListJobs filters.
func TestSchedulerEnumConversions(t *testing.T) {
	jobTypes := []scheduler.JobType{
		scheduler.JobTypeOneShot, scheduler.JobTypeRecurring, scheduler.JobTypeDelayed,
	}
	for _, jt := range jobTypes {
		assert.Equal(t, jt, jobTypeFromService(jobTypeToService(jt)))
	}

	owners := []scheduler.OwnerType{
		scheduler.OwnerTypeSystem, scheduler.OwnerTypeTenant, scheduler.OwnerTypeDelegated,
	}
	for _, ot := range owners {
		assert.Equal(t, ot, ownerTypeFromService(ownerTypeToService(ot)))
	}

	statuses := []scheduler.JobStatus{
		scheduler.JobStatusPending, scheduler.JobStatusRunning, scheduler.JobStatusCompleted,
		scheduler.JobStatusFailed, scheduler.JobStatusPaused, scheduler.JobStatusCanceled,
	}
	for _, st := range statuses {
		assert.Equal(t, st, jobStatusFromService(jobStatusToService(st)))
	}

	// ListJobs with OwnerType+Status set drives jobStatusToService/ownerTypeToService.
	ot := scheduler.OwnerTypeTenant
	st := scheduler.JobStatusRunning
	out, err := transformListJobsRequest(&scheduler.ListJobsOptions{
		OwnerType: &ot, Status: &st, OwnerID: "o1", Limit: 5,
		Tags: map[string]string{"k": "v"},
	})
	require.NoError(t, err)
	req := out.(*service.ListSchedulerJobsRequest)
	assert.Equal(t, service.SchedulerOwnerTypeTenant, req.OwnerType)
	assert.Equal(t, service.SchedulerJobStatusRunning, req.Status)
	assert.Equal(t, 5, req.Limit)
}

// TestSchedulerNilJobAndAltID covers the nil-job decode paths and the job_id
// (snake_case) fallback in the job-ID-only request decoder.
func TestSchedulerNilJobAndAltID(t *testing.T) {
	// GetJob with a nil "job" must yield a nil *JobInfo (client treats as not found).
	payload, err := EncodePayload(&service.GetSchedulerJobResponse{Job: nil})
	require.NoError(t, err)
	got, err := decodeSchedulerResponse("GetJob", payload)
	require.NoError(t, err)
	info, ok := got.(*scheduler.JobInfo)
	require.True(t, ok)
	assert.Nil(t, info)

	// CreateJob with a nil "job" yields an empty CreateJobResponse (no panic).
	cp, err := EncodePayload(&service.CreateSchedulerJobResponse{Job: nil})
	require.NoError(t, err)
	cgot, err := decodeSchedulerResponse("CreateJob", cp)
	require.NoError(t, err)
	assert.Equal(t, uint64(0), cgot.(*scheduler.CreateJobResponse).JobID)

	// A request carrying job_id (snake_case) must resolve via the alt field.
	out, err := transformSchedulerRequest("GetJob", &struct {
		JobID uint64 `json:"job_id"`
	}{JobID: 77})
	require.NoError(t, err)
	assert.Equal(t, uint64(77), out.(*service.GetSchedulerJobRequest).JobID)
}

// TestDecodeSchedulerResponse_InvalidPerMethod proves every per-method decoder
// surfaces a typed error on malformed JSON rather than returning a zero value.
func TestDecodeSchedulerResponse_InvalidPerMethod(t *testing.T) {
	methods := []string{
		"CreateJob", "GetJob", "UpdateJob", "DeleteJob", "ListJobs", "TriggerJob",
		"PauseJob", "ResumeJob", "CancelJob", "GetJobHistory", "GetStatus",
	}
	for _, m := range methods {
		_, err := decodeSchedulerResponse(m, []byte(`{"job":`))
		require.Error(t, err, m)
	}
}

// TestTransformSchedulerRequest_BadJobIDPayload covers the marshal/unmarshal
// error branches in the job-ID-only and cancel/history transformers.
func TestTransformSchedulerRequest_BadJobIDPayload(t *testing.T) {
	// json.Marshal fails on a channel value.
	badMethods := []string{"GetJob", "CancelJob", "GetJobHistory"}
	for _, m := range badMethods {
		_, err := transformSchedulerRequest(m, make(chan int))
		require.Error(t, err, m)
	}
}

// TestTransformSchedulerRequest_UpdateAndHistory covers UpdateJob conversion and
// GetJobHistory option mapping (Limit propagation).
func TestTransformSchedulerRequest_UpdateAndHistory(t *testing.T) {
	upd, err := transformSchedulerRequest("UpdateJob", &scheduler.UpdateJobRequest{
		JobID: 5, Name: "n", Description: "d", MaxRetries: 2,
		Tags: map[string]string{"a": "b"},
	})
	require.NoError(t, err)
	ur := upd.(*service.UpdateSchedulerJobRequest)
	assert.Equal(t, uint64(5), ur.JobID)
	assert.Equal(t, int32(2), ur.MaxRetries)
	assert.Equal(t, map[string]string{"a": "b"}, ur.Labels)

	hist, err := transformSchedulerRequest("GetJobHistory", &struct {
		JobID   uint64
		Options *scheduler.HistoryOptions
	}{JobID: 8, Options: &scheduler.HistoryOptions{Limit: 25}})
	require.NoError(t, err)
	hr := hist.(*service.GetSchedulerJobHistoryRequest)
	assert.Equal(t, uint64(8), hr.JobID)
	assert.Equal(t, 25, hr.Limit)

	// GetStatus carries an empty body.
	st, err := transformSchedulerRequest("GetStatus", &struct{}{})
	require.NoError(t, err)
	assert.NotNil(t, st)
}

// TestTransformSchedulerRequest_WrongTypes covers the type-assertion rejections
// for the methods that require a specific concrete SDK request type.
func TestTransformSchedulerRequest_WrongTypes(t *testing.T) {
	_, err := transformSchedulerRequest("UpdateJob", map[string]string{"x": "y"})
	require.Error(t, err)

	// mapJobIDRequest inner-unmarshal failure: a JSON object whose JobID field is
	// a string cannot decode into uint64.
	_, err = transformSchedulerRequest("GetJob", map[string]interface{}{"JobID": "not-a-number"})
	require.Error(t, err)
}

// TestDecodeSchedulerResponse_HistoryNilExecution covers the nil-execution skip
// branch in the history decoder.
func TestDecodeSchedulerResponse_HistoryNilExecution(t *testing.T) {
	payload, err := EncodePayload(&service.GetSchedulerJobHistoryResponse{
		Executions: []*service.SchedulerJobExecution{
			nil,
			{ID: 1, JobID: 3, Status: service.SchedulerJobStatusCompleted},
		},
	})
	require.NoError(t, err)

	got, err := decodeSchedulerResponse("GetJobHistory", payload)
	require.NoError(t, err)
	resp := got.(*scheduler.JobHistoryResponse)
	require.Len(t, resp.Executions, 1)
	assert.Equal(t, uint64(3), resp.JobID)
}
