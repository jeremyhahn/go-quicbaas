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

// scheduler_handler_test.go exercises the MCP scheduler handler end-to-end
// through the fake JSON-RPC MCP server, plus direct unit tests for the
// request/response transforms and the enum/value parsing helpers.

package mcp

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// =============================================================================
// End-to-end through the transport
// =============================================================================

func TestSchedulerCreateJobEndToEnd(t *testing.T) {
	nextRun := time.Now().UTC().Truncate(time.Second).Add(time.Hour)
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_create_job",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["name"] != "nightly" {
				t.Errorf("name = %v", args["name"])
			}
			if args["handler"] != "backup" {
				t.Errorf("handler = %v", args["handler"])
			}
			if args["type"] != "recurring" {
				t.Errorf("type = %v, want recurring", args["type"])
			}
			if args["owner_type"] != "tenant" {
				t.Errorf("owner_type = %v, want tenant", args["owner_type"])
			}
			if args["schedule"] != "0 0 * * *" {
				t.Errorf("schedule = %v", args["schedule"])
			}
			if args["payload"] != base64.StdEncoding.EncodeToString([]byte("data")) {
				t.Errorf("payload = %v", args["payload"])
			}
			if args["labels"] != `{"env":"prod"}` {
				t.Errorf("labels = %v", args["labels"])
			}
			if args["priority"] != float64(7) {
				t.Errorf("priority = %v, want 7", args["priority"])
			}
		},
		result: map[string]interface{}{
			"job": map[string]interface{}{
				"id":          float64(7),
				"next_run_at": nextRun.Format(time.RFC3339),
			},
		},
	})
	defer cleanup()

	resp, err := scheduler.NewClient(tr).CreateJob(context.Background(), &scheduler.CreateJobRequest{
		Name:       "nightly",
		Handler:    "backup",
		JobType:    scheduler.JobTypeRecurring,
		OwnerType:  scheduler.OwnerTypeTenant,
		Schedule:   "0 0 * * *",
		Payload:    []byte("data"),
		MaxRetries: 3,
		Timeout:    30 * time.Second,
		Delay:      5 * time.Second,
		OwnerID:    "tenant-1",
		Tags:       map[string]string{"env": "prod"},
		Priority:   7,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if resp.JobID != 7 {
		t.Errorf("JobID = %d, want 7", resp.JobID)
	}
	if !resp.NextRunAt.Equal(nextRun) {
		t.Errorf("NextRunAt = %v, want %v", resp.NextRunAt, nextRun)
	}
}

func TestSchedulerGetJobEndToEnd(t *testing.T) {
	created := time.Now().UTC().Truncate(time.Second)
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_get_job",
		result: map[string]interface{}{
			"job": map[string]interface{}{
				"id":          float64(11),
				"name":        "report",
				"description": "weekly report",
				"type":        "recurring",
				"status":      "running",
				"schedule":    "@weekly",
				"owner_type":  "system",
				"owner_id":    "sys",
				"created_at":  created.Format(time.RFC3339),
				"tags":        map[string]interface{}{"team": "ops"},
			},
		},
	})
	defer cleanup()

	info, err := scheduler.NewClient(tr).GetJob(context.Background(), 11)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if info.JobID != 11 || info.Name != "report" {
		t.Errorf("decoded job mismatch: %+v", info)
	}
	if info.JobType != scheduler.JobTypeRecurring || info.Status != scheduler.JobStatusRunning {
		t.Errorf("enum decode mismatch: type=%v status=%v", info.JobType, info.Status)
	}
	if info.Tags["team"] != "ops" {
		t.Errorf("tags = %v", info.Tags)
	}
}

func TestSchedulerUpdateJobEndToEnd(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_update_job",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["job_id"] != float64(5) {
				t.Errorf("job_id = %v", args["job_id"])
			}
			if args["name"] != "renamed" {
				t.Errorf("name = %v", args["name"])
			}
		},
		result: map[string]interface{}{
			"job": map[string]interface{}{"id": float64(5), "name": "renamed", "status": "paused"},
		},
	})
	defer cleanup()

	info, err := scheduler.NewClient(tr).UpdateJob(context.Background(), &scheduler.UpdateJobRequest{
		JobID:      5,
		Name:       "renamed",
		Schedule:   "@daily",
		Payload:    []byte("p"),
		MaxRetries: 2,
		Timeout:    time.Minute,
		Delay:      time.Second,
	})
	if err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if info.JobID != 5 || info.Status != scheduler.JobStatusPaused {
		t.Errorf("decoded job mismatch: %+v", info)
	}
}

func TestSchedulerDeleteJobEndToEnd(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Second)

	t.Run("delete", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_scheduler_delete_job",
			result:   map[string]interface{}{"deleted": true},
		})
		defer cleanup()

		if err := scheduler.NewClient(tr).DeleteJob(context.Background(), 9); err != nil {
			t.Fatalf("DeleteJob: %v", err)
		}
	})

	t.Run("force", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_scheduler_delete_job",
			assertArgs: func(t *testing.T, args map[string]interface{}) {
				if args["job_id"] != float64(9) {
					t.Errorf("job_id = %v, want 9", args["job_id"])
				}
				if args["force"] != true {
					t.Errorf("force = %v, want true", args["force"])
				}
			},
			result: map[string]interface{}{
				"job_id": float64(9), "deleted": true, "timestamp": ts.Format(time.RFC3339Nano),
			},
		})
		defer cleanup()

		resp, err := scheduler.NewClient(tr).DeleteJobWithOptions(context.Background(),
			&scheduler.DeleteJobRequest{JobID: 9, Force: true})
		if err != nil {
			t.Fatalf("DeleteJobWithOptions: %v", err)
		}
		if resp.JobID != 9 || !resp.Deleted || !resp.DeletedAt.Equal(ts) {
			t.Errorf("delete mismatch: %+v", resp)
		}
	})
}

func TestSchedulerListJobsEndToEnd(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_list_jobs",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["handler"] != "backup" {
				t.Errorf("handler = %v, want backup", args["handler"])
			}
			if args["cursor"] != "page-1" {
				t.Errorf("cursor = %v, want page-1", args["cursor"])
			}
		},
		result: map[string]interface{}{
			"jobs": []interface{}{
				map[string]interface{}{"id": float64(1), "name": "a", "status": "pending"},
				map[string]interface{}{
					"id": float64(2), "name": "b", "status": "completed",
					"handler": "backup", "priority": float64(5), "last_error": "disk full",
				},
			},
			"total_count": float64(2),
			"has_more":    true,
			"next_cursor": "page-2",
		},
	})
	defer cleanup()

	resp, err := scheduler.NewClient(tr).ListJobs(context.Background(),
		&scheduler.ListJobsOptions{Limit: 10, Handler: "backup", Cursor: "page-1"})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(resp.Jobs) != 2 || resp.TotalCount != 2 || !resp.HasMore || resp.NextCursor != "page-2" {
		t.Errorf("decoded list mismatch: %+v", resp)
	}
	job := resp.Jobs[1]
	if job.Name != "b" || job.Status != scheduler.JobStatusCompleted {
		t.Errorf("job summary mismatch: %+v", job)
	}
	if job.Handler != "backup" || job.Priority != 5 || job.LastError != "disk full" {
		t.Errorf("job fields mismatch: %+v", job)
	}
}

func TestSchedulerTriggerPauseResumeCancel(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Second)

	t.Run("trigger", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_scheduler_trigger_job",
			result: map[string]interface{}{
				"job_id": float64(3), "execution_id": float64(99), "started_at": ts.Format(time.RFC3339),
				"status": "running",
			},
		})
		defer cleanup()
		resp, err := scheduler.NewClient(tr).TriggerJob(context.Background(), 3)
		if err != nil {
			t.Fatalf("TriggerJob: %v", err)
		}
		if resp.JobID != 3 || resp.ExecutionID != 99 || resp.Status != scheduler.JobStatusRunning {
			t.Errorf("trigger mismatch: %+v", resp)
		}
	})

	t.Run("pause", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_scheduler_pause_job",
			result: map[string]interface{}{
				"job_id": float64(3), "paused_at": ts.Format(time.RFC3339),
				"old_status": "pending", "new_status": "paused",
			},
		})
		defer cleanup()
		resp, err := scheduler.NewClient(tr).PauseJob(context.Background(), 3)
		if err != nil {
			t.Fatalf("PauseJob: %v", err)
		}
		if resp.JobID != 3 || !resp.PausedAt.Equal(ts) {
			t.Errorf("pause mismatch: %+v", resp)
		}
		if resp.OldStatus != scheduler.JobStatusPending || resp.NewStatus != scheduler.JobStatusPaused {
			t.Errorf("pause mismatch: %+v", resp)
		}
	})

	t.Run("resume", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_scheduler_resume_job",
			result: map[string]interface{}{
				"job_id": float64(3), "resumed_at": ts.Format(time.RFC3339), "next_run_at": ts.Format(time.RFC3339),
				"old_status": "paused", "new_status": "pending",
			},
		})
		defer cleanup()
		resp, err := scheduler.NewClient(tr).ResumeJob(context.Background(), 3)
		if err != nil {
			t.Fatalf("ResumeJob: %v", err)
		}
		if resp.JobID != 3 || resp.OldStatus != scheduler.JobStatusPaused || resp.NewStatus != scheduler.JobStatusPending {
			t.Errorf("resume mismatch: %+v", resp)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_scheduler_cancel_job",
			assertArgs: func(t *testing.T, args map[string]interface{}) {
				if args["reason"] != "obsolete" {
					t.Errorf("reason = %v", args["reason"])
				}
			},
			result: map[string]interface{}{
				"job_id": float64(3), "canceled_at": ts.Format(time.RFC3339), "reason": "obsolete",
				"old_status": "running", "new_status": "canceled",
			},
		})
		defer cleanup()
		resp, err := scheduler.NewClient(tr).CancelJob(context.Background(), 3, "obsolete")
		if err != nil {
			t.Fatalf("CancelJob: %v", err)
		}
		if resp.JobID != 3 || resp.Reason != "obsolete" {
			t.Errorf("cancel mismatch: %+v", resp)
		}
		if resp.OldStatus != scheduler.JobStatusRunning || resp.NewStatus != scheduler.JobStatusCanceled {
			t.Errorf("cancel mismatch: %+v", resp)
		}
	})
}

func TestSchedulerGetJobHistoryEndToEnd(t *testing.T) {
	started := time.Now().UTC().Truncate(time.Second)
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_job_history",
		result: map[string]interface{}{
			"job_id": float64(4),
			"executions": []interface{}{
				map[string]interface{}{
					"id":         float64(1),
					"job_id":     float64(4),
					"status":     "completed",
					"started_at": started.Format(time.RFC3339),
				},
			},
			"total_count": float64(1),
			"has_more":    false,
		},
	})
	defer cleanup()

	resp, err := scheduler.NewClient(tr).GetJobHistory(context.Background(), 4, &scheduler.HistoryOptions{Limit: 5})
	if err != nil {
		t.Fatalf("GetJobHistory: %v", err)
	}
	if resp.JobID != 4 || len(resp.Executions) != 1 {
		t.Errorf("history mismatch: %+v", resp)
	}
	if resp.Executions[0].ExecutionID != 1 || resp.Executions[0].Status != scheduler.JobStatusCompleted {
		t.Errorf("execution mismatch: %+v", resp.Executions[0])
	}
}

func TestSchedulerGetStatusEndToEnd(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_status",
		result: map[string]interface{}{
			"healthy":      true,
			"total_jobs":   float64(10),
			"running_jobs": float64(2),
			"worker_count": float64(4),
			"queue_depth":  float64(1),
		},
	})
	defer cleanup()

	status, err := scheduler.NewClient(tr).GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !status.Healthy || status.TotalJobs != 10 || status.WorkerCount != 4 {
		t.Errorf("status mismatch: %+v", status)
	}
}

func TestSchedulerToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:  "quicbaas_scheduler_get_job",
		toolError: true,
		rawText:   "job not found",
	})
	defer cleanup()
	_, err := scheduler.NewClient(tr).GetJob(context.Background(), 404)
	if err == nil {
		t.Fatal("expected error from tool-level failure")
	}
}

// =============================================================================
// Direct transform / helper unit tests
// =============================================================================

func TestSchedulerTransformRequestGenericFallback(t *testing.T) {
	h := newSchedulerHandler()

	// nil request yields an empty argument map.
	args, err := h.genericTransform(nil)
	if err != nil {
		t.Fatalf("genericTransform(nil): %v", err)
	}
	if len(args) != 0 {
		t.Errorf("expected empty args, got %v", args)
	}

	// CreateJob with a non-matching type falls back to generic conversion.
	type bogus struct {
		JobID    uint64 `json:"JobID"`
		TenantID string `json:"TenantID"`
		Reason   string `json:"Reason"`
	}
	args, err = h.transformRequest("CreateJob", &bogus{JobID: 1, TenantID: "t", Reason: "r"})
	if err != nil {
		t.Fatalf("transformRequest fallback: %v", err)
	}
	if args["job_id"] != float64(1) || args["tenant_id"] != "t" || args["reason"] != "r" {
		t.Errorf("generic key remap failed: %v", args)
	}
}

func TestSchedulerTransformRequestUpdateFallback(t *testing.T) {
	h := newSchedulerHandler()
	// UpdateJob with wrong concrete type falls back to generic transform.
	args, err := h.transformRequest("UpdateJob", map[string]interface{}{"x": 1})
	if err != nil {
		t.Fatalf("transformRequest: %v", err)
	}
	if args["x"] != float64(1) {
		t.Errorf("args = %v", args)
	}
}

func TestSchedulerEnumToString(t *testing.T) {
	jobTypeCases := map[scheduler.JobType]string{
		scheduler.JobTypeOneShot:   "one-shot",
		scheduler.JobTypeRecurring: "recurring",
		scheduler.JobTypeDelayed:   "delayed",
		scheduler.JobType(99):      "one-shot",
	}
	for jt, want := range jobTypeCases {
		if got := jobTypeToString(jt); got != want {
			t.Errorf("jobTypeToString(%v) = %q, want %q", jt, got, want)
		}
	}

	ownerCases := map[scheduler.OwnerType]string{
		scheduler.OwnerTypeSystem:    "system",
		scheduler.OwnerTypeTenant:    "tenant",
		scheduler.OwnerTypeDelegated: "delegated",
		scheduler.OwnerType(99):      "system",
	}
	for ot, want := range ownerCases {
		if got := ownerTypeToString(ot); got != want {
			t.Errorf("ownerTypeToString(%v) = %q, want %q", ot, got, want)
		}
	}
}

func TestSchedulerParseEnums(t *testing.T) {
	if parseJobType("recurring") != scheduler.JobTypeRecurring ||
		parseJobType("delayed") != scheduler.JobTypeDelayed ||
		parseJobType("unknown") != scheduler.JobTypeOneShot ||
		parseJobType("2") != scheduler.JobTypeDelayed {
		t.Error("parseJobType mismatch")
	}
	if parseJobStatus("running") != scheduler.JobStatusRunning ||
		parseJobStatus("completed") != scheduler.JobStatusCompleted ||
		parseJobStatus("failed") != scheduler.JobStatusFailed ||
		parseJobStatus("paused") != scheduler.JobStatusPaused ||
		parseJobStatus("cancelled") != scheduler.JobStatusCanceled ||
		parseJobStatus("bogus") != scheduler.JobStatusPending {
		t.Error("parseJobStatus mismatch")
	}
	if parseOwnerType("tenant") != scheduler.OwnerTypeTenant ||
		parseOwnerType("delegated") != scheduler.OwnerTypeDelegated ||
		parseOwnerType("bogus") != scheduler.OwnerTypeSystem {
		t.Error("parseOwnerType mismatch")
	}
}

func TestSchedulerGetBytesFromMap(t *testing.T) {
	if got := getBytesFromMap(map[string]interface{}{"k": []byte("ab")}, "k"); string(got) != "ab" {
		t.Errorf("bytes case = %q", got)
	}
	if got := getBytesFromMap(map[string]interface{}{"k": "cd"}, "k"); string(got) != "cd" {
		t.Errorf("string case = %q", got)
	}
	if got := getBytesFromMap(map[string]interface{}{"k": 5}, "k"); got != nil {
		t.Errorf("unexpected type should yield nil, got %v", got)
	}
	if got := getBytesFromMap(map[string]interface{}{}, "missing"); got != nil {
		t.Errorf("missing key should yield nil, got %v", got)
	}
}

func TestSchedulerNilResponses(t *testing.T) {
	h := newSchedulerHandler()
	if r, err := h.transformCreateJobResponse(nil); err != nil || r == nil {
		t.Errorf("transformCreateJobResponse(nil) = %v, %v", r, err)
	}
	if r, err := h.transformGetJobResponse(nil); err != nil || r != nil {
		t.Errorf("transformGetJobResponse(nil) = %v, %v", r, err)
	}
	if r, err := h.transformListJobsResponse(nil); err != nil || r == nil {
		t.Errorf("transformListJobsResponse(nil) = %v, %v", r, err)
	}
	if r, err := h.transformDeleteJobResponse(nil); err != nil || r == nil {
		t.Errorf("transformDeleteJobResponse(nil) = %v, %v", r, err)
	}
	if r, err := h.transformGetStatusResponse(nil); err != nil || r == nil {
		t.Errorf("transformGetStatusResponse(nil) = %v, %v", r, err)
	}
	if r, err := h.parseJobInfo(nil); err != nil || r != nil {
		t.Errorf("parseJobInfo(nil) = %v, %v", r, err)
	}
	if r, err := h.parseJobExecution(nil); err != nil || r != nil {
		t.Errorf("parseJobExecution(nil) = %v, %v", r, err)
	}
}

// TestSchedulerResponseTransformsFull drives each scheduler response transform
// with a fully-populated map so the time-parse and numeric branches are
// exercised, including the JSON round-trip fallback for non-map inputs.
func TestSchedulerResponseTransformsFull(t *testing.T) {
	h := newSchedulerHandler()
	ts := "2026-05-29T00:00:00Z"

	create, err := h.transformCreateJobResponse(map[string]interface{}{
		"job_id": float64(1), "next_run_at": ts})
	if err != nil || create.JobID != 1 || create.NextRunAt.IsZero() {
		t.Errorf("create = %+v, %v", create, err)
	}

	trig, err := h.transformTriggerJobResponse(map[string]interface{}{
		"job_id": float64(2), "execution_id": float64(9), "started_at": ts})
	if err != nil || trig.StartedAt.IsZero() {
		t.Errorf("trigger = %+v, %v", trig, err)
	}

	pause, err := h.transformPauseJobResponse(map[string]interface{}{
		"job_id": float64(3), "paused_at": ts})
	if err != nil || pause.PausedAt.IsZero() {
		t.Errorf("pause = %+v, %v", pause, err)
	}

	resume, err := h.transformResumeJobResponse(map[string]interface{}{
		"job_id": float64(3), "resumed_at": ts, "next_run_at": ts})
	if err != nil || resume.ResumedAt.IsZero() || resume.NextRunAt.IsZero() {
		t.Errorf("resume = %+v, %v", resume, err)
	}

	cancel, err := h.transformCancelJobResponse(map[string]interface{}{
		"job_id": float64(3), "canceled_at": ts, "reason": "r"})
	if err != nil || cancel.CanceledAt.IsZero() || cancel.Reason != "r" {
		t.Errorf("cancel = %+v, %v", cancel, err)
	}

	status, err := h.transformGetStatusResponse(map[string]interface{}{
		"healthy": true, "total_jobs": float64(10), "running_jobs": float64(2),
		"pending_jobs": float64(1), "completed_jobs": float64(5), "failed_jobs": float64(1),
		"paused_jobs": float64(0), "canceled_jobs": float64(1), "executions_today": float64(20),
		"failures_today": float64(2), "average_latency": float64(1000),
		"worker_count": float64(4), "queue_depth": float64(3),
		"last_heartbeat": ts, "uptime_duration": float64(5000)})
	if err != nil || status.TotalJobs != 10 || status.LastHeartbeat.IsZero() {
		t.Errorf("status = %+v, %v", status, err)
	}

	// Full job info exercises parseJobInfo's tag/time branches.
	info, err := h.transformGetJobResponse(map[string]interface{}{
		"job": map[string]interface{}{
			"id": float64(11), "name": "j", "description": "d", "type": "recurring",
			"status": "running", "schedule": "@daily", "payload": "cGF5",
			"owner_type": "tenant", "owner_id": "o", "max_retries": float64(3),
			"timeout": float64(1000), "execution_count": float64(7), "failure_count": float64(1),
			"next_run_at": ts, "last_run_at": ts, "created_at": ts, "updated_at": ts,
			"tags": map[string]interface{}{"k": "v"},
		},
	})
	if err != nil || info.JobID != 11 || info.Tags["k"] != "v" || info.CreatedAt.IsZero() {
		t.Errorf("job info = %+v, %v", info, err)
	}

	// History with a full execution exercises parseJobExecution.
	hist, err := h.transformGetJobHistoryResponse(map[string]interface{}{
		"job_id": float64(4), "total_count": float64(1), "has_more": false,
		"executions": []interface{}{
			map[string]interface{}{
				"id": float64(1), "job_id": float64(4), "status": "completed",
				"started_at": ts, "completed_at": ts, "duration": float64(500),
				"retry_count": float64(0), "error": "", "result": "cmVz",
			},
		},
	})
	if err != nil || len(hist.Executions) != 1 || hist.Executions[0].CompletedAt.IsZero() {
		t.Errorf("history = %+v, %v", hist, err)
	}

	// UpdateJob response (extracts nested job).
	upd, err := h.transformUpdateJobResponse(map[string]interface{}{
		"job": map[string]interface{}{"id": float64(5), "status": "paused"}})
	if err != nil || upd.JobID != 5 {
		t.Errorf("update = %+v, %v", upd, err)
	}
}

// TestSchedulerGetIntUintFromMapVariants covers the numeric extraction helpers'
// non-float branches.
func TestSchedulerGetIntUintFromMapVariants(t *testing.T) {
	if getInt64FromMap(map[string]interface{}{"k": int64(5)}, "k") != 5 {
		t.Error("int64 case")
	}
	if getInt64FromMap(map[string]interface{}{"k": int(6)}, "k") != 6 {
		t.Error("int case")
	}
	if getInt64FromMap(map[string]interface{}{"k": "x"}, "k") != 0 {
		t.Error("string should yield 0")
	}
	if getUint64FromMap(map[string]interface{}{"k": uint64(7)}, "k") != 7 {
		t.Error("uint64 case")
	}
	if getUint64FromMap(map[string]interface{}{"k": int64(8)}, "k") != 8 {
		t.Error("int64 case")
	}
	if getUint64FromMap(map[string]interface{}{"k": int(9)}, "k") != 9 {
		t.Error("int case")
	}
	if getUint64FromMap(map[string]interface{}{"k": "x"}, "k") != 0 {
		t.Error("string should yield 0")
	}
}

// TestSchedulerParseTimeFromMapVariants covers parseTimeFromMap's numeric paths.
func TestSchedulerParseTimeFromMapVariants(t *testing.T) {
	if parseTimeFromMap(map[string]interface{}{"k": float64(1_700_000_000)}, "k").IsZero() {
		t.Error("float64 unix should parse")
	}
	if parseTimeFromMap(map[string]interface{}{"k": int64(1_700_000_000)}, "k").IsZero() {
		t.Error("int64 unix should parse")
	}
	if !parseTimeFromMap(map[string]interface{}{"k": "garbage"}, "k").IsZero() {
		t.Error("garbage string should yield zero")
	}
	if !parseTimeFromMap(map[string]interface{}{}, "missing").IsZero() {
		t.Error("missing key should yield zero")
	}
}

// schedJobJSON is a struct whose JSON fields mirror the scheduler wire format;
// passing it (instead of a map) forces the response transforms through their
// json.Marshal/Unmarshal fallback branch.
type schedJobJSON struct {
	JobID       uint64 `json:"job_id"`
	ExecutionID uint64 `json:"execution_id,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	PausedAt    string `json:"paused_at,omitempty"`
	ResumedAt   string `json:"resumed_at,omitempty"`
	NextRunAt   string `json:"next_run_at,omitempty"`
	CanceledAt  string `json:"canceled_at,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Healthy     bool   `json:"healthy,omitempty"`
	TotalJobs   uint64 `json:"total_jobs,omitempty"`
}

func TestSchedulerResponseStructFallback(t *testing.T) {
	h := newSchedulerHandler()
	ts := "2026-05-29T00:00:00Z"

	if r, err := h.transformCreateJobResponse(schedJobJSON{JobID: 1, NextRunAt: ts}); err != nil || r.JobID != 1 {
		t.Errorf("create fallback = %+v, %v", r, err)
	}
	if r, err := h.transformTriggerJobResponse(schedJobJSON{JobID: 2, ExecutionID: 9, StartedAt: ts}); err != nil || r.ExecutionID != 9 {
		t.Errorf("trigger fallback = %+v, %v", r, err)
	}
	if r, err := h.transformDeleteJobResponse(schedJobJSON{JobID: 5}); err != nil || r.JobID != 5 {
		t.Errorf("delete fallback = %+v, %v", r, err)
	}
	if r, err := h.transformPauseJobResponse(schedJobJSON{JobID: 3, PausedAt: ts}); err != nil || r.JobID != 3 {
		t.Errorf("pause fallback = %+v, %v", r, err)
	}
	if r, err := h.transformResumeJobResponse(schedJobJSON{JobID: 3, ResumedAt: ts}); err != nil || r.JobID != 3 {
		t.Errorf("resume fallback = %+v, %v", r, err)
	}
	if r, err := h.transformCancelJobResponse(schedJobJSON{JobID: 3, CanceledAt: ts, Reason: "x"}); err != nil || r.Reason != "x" {
		t.Errorf("cancel fallback = %+v, %v", r, err)
	}
	if r, err := h.transformGetStatusResponse(schedJobJSON{Healthy: true, TotalJobs: 5}); err != nil || r.TotalJobs != 5 {
		t.Errorf("status fallback = %+v, %v", r, err)
	}
	if r, err := h.transformGetJobResponse(schedJobJSON{JobID: 7}); err != nil || r.JobID != 7 {
		t.Errorf("getjob fallback = %+v, %v", r, err)
	}
	if r, err := h.transformUpdateJobResponse(schedJobJSON{JobID: 8}); err != nil || r.JobID != 8 {
		t.Errorf("update fallback = %+v, %v", r, err)
	}
	if r, err := h.transformListJobsResponse(schedJobJSON{JobID: 1}); err != nil || r == nil {
		t.Errorf("list fallback = %+v, %v", r, err)
	}
	if r, err := h.transformGetJobHistoryResponse(schedJobJSON{JobID: 4}); err != nil || r.JobID != 4 {
		t.Errorf("history fallback = %+v, %v", r, err)
	}
	if r, err := h.parseJobExecution(schedJobJSON{ExecutionID: 1}); err != nil || r == nil {
		t.Errorf("parseJobExecution fallback = %+v, %v", r, err)
	}
}

// TestSchedulerResponseMarshalError exercises the error path when the response
// is a value that cannot be marshaled to JSON.
func TestSchedulerResponseMarshalError(t *testing.T) {
	h := newSchedulerHandler()
	bad := make(chan int) // channels are not JSON-marshalable
	if _, err := h.transformCreateJobResponse(bad); err == nil {
		t.Error("create should fail to marshal channel")
	}
	if _, err := h.transformGetJobResponse(bad); err == nil {
		t.Error("getjob should fail to marshal channel")
	}
	if _, err := h.parseJobInfo(bad); err == nil {
		t.Error("parseJobInfo should fail to marshal channel")
	}
	if _, err := h.transformDeleteJobResponse(bad); err == nil {
		t.Error("delete should fail to marshal channel")
	}
}
