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
// through the fake JSON-RPC MCP server. Each test pins the argument keys the
// server's quicbaas_scheduler_* tools read and feeds back a result in the
// shape those tools write (the pkg/service scheduler types).

package mcp

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// schedulerClientFor returns a scheduler client wired to a fake MCP server
// that serves exactly one tool.
func schedulerClientFor(t *testing.T, exp mcpToolExpectation) scheduler.Client {
	t.Helper()
	tr, cleanup := newFakeMCPServer(t, exp)
	t.Cleanup(cleanup)
	return scheduler.NewClient(tr)
}

// assertArg fails the test when args[key] is not want.
func assertArg(t *testing.T, args map[string]interface{}, key string, want interface{}) {
	t.Helper()
	if got := args[key]; got != want {
		t.Errorf("%s = %#v, want %#v", key, got, want)
	}
}

// assertNoArg fails the test when args carries key.
func assertNoArg(t *testing.T, args map[string]interface{}, key string) {
	t.Helper()
	if got, ok := args[key]; ok {
		t.Errorf("%s = %#v, want absent", key, got)
	}
}

// assertLabels fails the test when args["labels"] is not the object want.
func assertLabels(t *testing.T, args map[string]interface{}, want map[string]string) {
	t.Helper()
	labels, ok := args["labels"].(map[string]interface{})
	if !ok || len(labels) != len(want) {
		t.Fatalf("labels = %#v, want object %v", args["labels"], want)
	}
	for k, v := range want {
		if labels[k] != v {
			t.Errorf("labels[%s] = %#v, want %q", k, labels[k], v)
		}
	}
}

// serverJob is a job in the JSON shape of service.SchedulerJob.
func serverJob(nextRun time.Time) map[string]interface{} {
	return map[string]interface{}{
		"id":          float64(42),
		"name":        "nightly",
		"owner_type":  "tenant",
		"owner_id":    "acme",
		"type":        "recurring",
		"schedule":    "0 2 * * *",
		"handler":     "noop",
		"priority":    float64(5),
		"timeout":     float64(30 * time.Second),
		"max_retries": float64(3),
		"status":      "pending",
		"next_run":    nextRun.Format(time.RFC3339Nano),
		"run_count":   float64(4),
		"fail_count":  float64(1),
		"last_error":  "handler timed out",
		"labels":      map[string]interface{}{"team": "ops"},
	}
}

func TestSchedulerCreateJobEndToEnd(t *testing.T) {
	nextRun := time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC)
	executeAt := time.Date(2026, 10, 2, 1, 0, 0, 0, time.UTC)
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_create_job",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			assertArg(t, args, "name", "nightly")
			assertArg(t, args, "handler", "noop")
			assertArg(t, args, "type", "recurring")
			assertArg(t, args, "owner_type", "tenant")
			assertArg(t, args, "owner_id", "acme")
			assertArg(t, args, "description", "nightly backup")
			assertArg(t, args, "schedule", "0 2 * * *")
			assertArg(t, args, "node_affinity", "node-a")
			assertArg(t, args, "execute_at", executeAt.Format(time.RFC3339))
			assertArg(t, args, "delay", "5s")
			assertArg(t, args, "timeout", "30s")
			assertArg(t, args, "payload", base64.StdEncoding.EncodeToString([]byte("data")))
			assertArg(t, args, "priority", float64(7))
			assertArg(t, args, "max_retries", float64(3))
			assertLabels(t, args, map[string]string{"team": "ops"})
		},
		result: map[string]interface{}{"job": serverJob(nextRun)},
	})

	resp, err := client.CreateJob(context.Background(), &scheduler.CreateJobRequest{
		Name:         "nightly",
		Description:  "nightly backup",
		Handler:      "noop",
		JobType:      scheduler.JobTypeRecurring,
		OwnerType:    scheduler.OwnerTypeTenant,
		OwnerID:      "acme",
		Schedule:     "0 2 * * *",
		ExecuteAt:    executeAt,
		Delay:        5 * time.Second,
		Payload:      []byte("data"),
		MaxRetries:   3,
		Timeout:      30 * time.Second,
		Tags:         map[string]string{"team": "ops"},
		NodeAffinity: "node-a",
		Priority:     7,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if resp.JobID != 42 || !resp.NextRunAt.Equal(nextRun) {
		t.Errorf("CreateJob decoded %+v", resp)
	}
}

func TestSchedulerCreateJobOmitsUnsetFields(t *testing.T) {
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_create_job",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			for _, key := range []string{"description", "owner_id", "schedule", "node_affinity",
				"execute_at", "delay", "timeout", "payload", "labels", "priority", "max_retries"} {
				assertNoArg(t, args, key)
			}
		},
		result: map[string]interface{}{"job": map[string]interface{}{"id": float64(1)}},
	})
	if _, err := client.CreateJob(context.Background(), &scheduler.CreateJobRequest{
		Name: "once", Handler: "noop", JobType: scheduler.JobTypeOneShot,
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
}

func TestSchedulerGetJobEndToEnd(t *testing.T) {
	nextRun := time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC)
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_get_job",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			assertArg(t, args, "job_id", float64(42))
			assertNoArg(t, args, "tenant_id")
			assertNoArg(t, args, "scope")
		},
		result: map[string]interface{}{"job": serverJob(nextRun)},
	})

	job, err := client.GetJob(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.JobID != 42 || job.Name != "nightly" || job.JobType != scheduler.JobTypeRecurring ||
		job.Status != scheduler.JobStatusPending || job.OwnerType != scheduler.OwnerTypeTenant {
		t.Errorf("GetJob identity decoded %+v", job)
	}
	if job.Handler != "noop" || job.Priority != 5 || job.LastError != "handler timed out" ||
		job.MaxRetries != 3 || job.Timeout != 30*time.Second {
		t.Errorf("GetJob settings decoded %+v", job)
	}
	if job.ExecutionCount != 4 || job.FailureCount != 1 || !job.NextRunAt.Equal(nextRun) || job.Tags["team"] != "ops" {
		t.Errorf("GetJob run state decoded %+v", job)
	}
}

func TestSchedulerGetJobWithoutJobIsNotFound(t *testing.T) {
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_get_job",
		result:   map[string]interface{}{"timestamp": time.Now().UTC().Format(time.RFC3339Nano)},
	})
	if _, err := client.GetJob(context.Background(), 42); !errors.IsNotFound(err) {
		t.Fatalf("GetJob error = %v, want not found", err)
	}
}

func TestSchedulerToolError(t *testing.T) {
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool:  "quicbaas_scheduler_get_job",
		toolError: true,
		rawText:   "job not found",
	})
	if _, err := client.GetJob(context.Background(), 404); err == nil {
		t.Fatal("expected error from tool-level failure")
	}
}

func TestSchedulerUpdateJobEndToEnd(t *testing.T) {
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_update_job",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			assertArg(t, args, "job_id", float64(42))
			assertArg(t, args, "name", "renamed")
			assertArg(t, args, "max_retries", float64(5))
			assertArg(t, args, "timeout", "1m0s")
			assertLabels(t, args, map[string]string{"team": "ops"})
			assertNoArg(t, args, "delay")
		},
		result: map[string]interface{}{"job": map[string]interface{}{"id": float64(42), "name": "renamed"}},
	})

	job, err := client.UpdateJob(context.Background(), &scheduler.UpdateJobRequest{
		JobID: 42, Name: "renamed", MaxRetries: 5, Timeout: time.Minute,
		Tags: map[string]string{"team": "ops"},
	})
	if err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if job.JobID != 42 || job.Name != "renamed" {
		t.Errorf("UpdateJob decoded %+v", job)
	}
}

func TestSchedulerListJobsEndToEnd(t *testing.T) {
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_list_jobs",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			assertArg(t, args, "status", "pending")
			assertArg(t, args, "owner_type", "tenant")
			assertArg(t, args, "owner_id", "acme")
			assertArg(t, args, "handler", "noop")
			assertArg(t, args, "cursor", "page-1")
			assertArg(t, args, "limit", float64(10))
			assertLabels(t, args, map[string]string{"team": "ops"})
		},
		result: map[string]interface{}{
			"jobs":        []interface{}{serverJob(time.Time{})},
			"total_count": float64(1),
			"has_more":    true,
			"next_cursor": "page-2",
		},
	})

	status := scheduler.JobStatusPending
	owner := scheduler.OwnerTypeTenant
	list, err := client.ListJobs(context.Background(), &scheduler.ListJobsOptions{
		Status: &status, OwnerType: &owner, OwnerID: "acme", Handler: "noop",
		Cursor: "page-1", Limit: 10, Tags: map[string]string{"team": "ops"},
	})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(list.Jobs) != 1 || list.TotalCount != 1 || !list.HasMore || list.NextCursor != "page-2" {
		t.Fatalf("ListJobs decoded %+v", list)
	}
	if job := list.Jobs[0]; job.JobID != 42 || job.Handler != "noop" || job.Tags["team"] != "ops" {
		t.Errorf("ListJobs job decoded %+v", job)
	}
}

func TestSchedulerJobTransitionsEndToEnd(t *testing.T) {
	ctx := context.Background()

	trigger := schedulerClientFor(t, mcpToolExpectation{
		wantTool:   "quicbaas_scheduler_trigger_job",
		assertArgs: func(t *testing.T, args map[string]interface{}) { assertArg(t, args, "job_id", float64(42)) },
		result:     map[string]interface{}{"job_id": float64(42), "status": "running"},
	})
	triggered, err := trigger.TriggerJob(ctx, 42)
	if err != nil || triggered.JobID != 42 || triggered.Status != scheduler.JobStatusRunning {
		t.Errorf("TriggerJob = %+v, %v", triggered, err)
	}

	pause := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_pause_job",
		result:   map[string]interface{}{"job_id": float64(42), "old_status": "pending", "new_status": "paused"},
	})
	paused, err := pause.PauseJob(ctx, 42)
	if err != nil || paused.OldStatus != scheduler.JobStatusPending || paused.NewStatus != scheduler.JobStatusPaused {
		t.Errorf("PauseJob = %+v, %v", paused, err)
	}

	resume := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_resume_job",
		result:   map[string]interface{}{"job_id": float64(42), "old_status": "paused", "new_status": "pending"},
	})
	resumed, err := resume.ResumeJob(ctx, 42)
	if err != nil || resumed.OldStatus != scheduler.JobStatusPaused || resumed.NewStatus != scheduler.JobStatusPending {
		t.Errorf("ResumeJob = %+v, %v", resumed, err)
	}
}

func TestSchedulerCancelJobEndToEnd(t *testing.T) {
	canceledAt := time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_cancel_job",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			assertArg(t, args, "job_id", float64(42))
			assertArg(t, args, "reason", "maintenance")
		},
		result: map[string]interface{}{
			"job_id": float64(42), "old_status": "pending", "new_status": "canceled",
			"timestamp": canceledAt.Format(time.RFC3339Nano),
		},
	})

	canceled, err := client.CancelJob(context.Background(), 42, "maintenance")
	if err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	if canceled.JobID != 42 || canceled.Reason != "maintenance" || !canceled.CanceledAt.Equal(canceledAt) ||
		canceled.OldStatus != scheduler.JobStatusPending || canceled.NewStatus != scheduler.JobStatusCanceled {
		t.Errorf("CancelJob decoded %+v", canceled)
	}
}

// TestSchedulerByIDToolsSendTheJobScope checks every by-ID tool call carries
// the scope WithJobScope names, so the server looks the job up in the
// delegated namespace rather than the caller's own.
func TestSchedulerByIDToolsSendTheJobScope(t *testing.T) {
	nextRun := time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC)
	transition := map[string]interface{}{"job_id": float64(42), "status": "running", "old_status": "pending", "new_status": "paused"}
	scope := scheduler.WithJobScope(scheduler.OwnerTypeDelegated)
	delegated := scheduler.OwnerTypeDelegated

	tests := []struct {
		tool   string
		result map[string]interface{}
		call   func(c scheduler.Client) error
	}{
		{"quicbaas_scheduler_get_job", map[string]interface{}{"job": serverJob(nextRun)}, func(c scheduler.Client) error {
			_, err := c.GetJob(context.Background(), 42, scope)
			return err
		}},
		{"quicbaas_scheduler_update_job", map[string]interface{}{"job": serverJob(nextRun)}, func(c scheduler.Client) error {
			_, err := c.UpdateJob(context.Background(), &scheduler.UpdateJobRequest{JobID: 42, Name: "renamed", Scope: &delegated})
			return err
		}},
		{"quicbaas_scheduler_delete_job", map[string]interface{}{"job_id": float64(42), "deleted": true}, func(c scheduler.Client) error {
			return c.DeleteJob(context.Background(), 42, scope)
		}},
		{"quicbaas_scheduler_trigger_job", transition, func(c scheduler.Client) error {
			_, err := c.TriggerJob(context.Background(), 42, scope)
			return err
		}},
		{"quicbaas_scheduler_pause_job", transition, func(c scheduler.Client) error {
			_, err := c.PauseJob(context.Background(), 42, scope)
			return err
		}},
		{"quicbaas_scheduler_resume_job", transition, func(c scheduler.Client) error {
			_, err := c.ResumeJob(context.Background(), 42, scope)
			return err
		}},
		{"quicbaas_scheduler_cancel_job", transition, func(c scheduler.Client) error {
			_, err := c.CancelJob(context.Background(), 42, "maintenance", scope)
			return err
		}},
		{"quicbaas_scheduler_job_history", map[string]interface{}{"executions": []interface{}{}}, func(c scheduler.Client) error {
			_, err := c.GetJobHistory(context.Background(), 42, nil, scope)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			client := schedulerClientFor(t, mcpToolExpectation{
				wantTool: tt.tool,
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					assertArg(t, args, "job_id", float64(42))
					assertArg(t, args, "scope", "delegated")
				},
				result: tt.result,
			})
			if err := tt.call(client); err != nil {
				t.Fatalf("%s: %v", tt.tool, err)
			}
		})
	}
}

func TestSchedulerDeleteJobEndToEnd(t *testing.T) {
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_delete_job",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			assertArg(t, args, "job_id", float64(42))
			assertArg(t, args, "force", true)
		},
		result: map[string]interface{}{"job_id": float64(42), "deleted": true},
	})

	deleted, err := client.DeleteJobWithOptions(context.Background(), &scheduler.DeleteJobRequest{JobID: 42, Force: true})
	if err != nil {
		t.Fatalf("DeleteJobWithOptions: %v", err)
	}
	if deleted.JobID != 42 || !deleted.Deleted {
		t.Errorf("DeleteJobWithOptions decoded %+v", deleted)
	}
}

func TestSchedulerDeleteJobOmitsForceByDefault(t *testing.T) {
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool:   "quicbaas_scheduler_delete_job",
		assertArgs: func(t *testing.T, args map[string]interface{}) { assertNoArg(t, args, "force") },
		result:     map[string]interface{}{"job_id": float64(42), "deleted": true},
	})
	if err := client.DeleteJob(context.Background(), 42); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
}

func TestSchedulerGetJobHistoryEndToEnd(t *testing.T) {
	started := time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC)
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_job_history",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			assertArg(t, args, "job_id", float64(42))
			assertArg(t, args, "limit", float64(5))
		},
		result: map[string]interface{}{
			"executions": []interface{}{map[string]interface{}{
				"id": float64(7), "job_id": float64(42), "status": "completed",
				"started_at": started.Format(time.RFC3339Nano),
				"ended_at":   started.Add(time.Second).Format(time.RFC3339Nano),
				"duration":   float64(time.Second), "output": "ok",
			}},
			"has_more": true,
		},
	})

	history, err := client.GetJobHistory(context.Background(), 42, &scheduler.HistoryOptions{Limit: 5})
	if err != nil {
		t.Fatalf("GetJobHistory: %v", err)
	}
	if history.JobID != 42 || len(history.Executions) != 1 || !history.HasMore {
		t.Fatalf("GetJobHistory decoded %+v", history)
	}
	exec := history.Executions[0]
	if exec.ExecutionID != 7 || exec.Status != scheduler.JobStatusCompleted || exec.Duration != time.Second ||
		!exec.CompletedAt.Equal(started.Add(time.Second)) || string(exec.Result) != "ok" {
		t.Errorf("GetJobHistory execution decoded %+v", exec)
	}
}

func TestSchedulerGetJobHistoryOmitsLimitWithoutOptions(t *testing.T) {
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool:   "quicbaas_scheduler_job_history",
		assertArgs: func(t *testing.T, args map[string]interface{}) { assertNoArg(t, args, "limit") },
		result:     map[string]interface{}{"executions": []interface{}{}},
	})
	if _, err := client.GetJobHistory(context.Background(), 42, nil); err != nil {
		t.Fatalf("GetJobHistory: %v", err)
	}
}

func TestSchedulerGetStatusEndToEnd(t *testing.T) {
	tick := time.Date(2026, 10, 2, 2, 0, 0, 0, time.UTC)
	client := schedulerClientFor(t, mcpToolExpectation{
		wantTool: "quicbaas_scheduler_status",
		result: map[string]interface{}{
			"running": true,
			"stats": map[string]interface{}{
				"job_count": float64(5), "running_jobs": float64(1), "success_count": float64(3),
				"failure_count": float64(1), "last_tick_time": tick.Format(time.RFC3339Nano),
			},
		},
	})

	st, err := client.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !st.Healthy || st.TotalJobs != 5 || st.RunningJobs != 1 || st.CompletedJobs != 3 ||
		st.FailedJobs != 1 || !st.LastHeartbeat.Equal(tick) {
		t.Errorf("GetStatus decoded %+v", st)
	}
}

func TestSchedulerTransformRequestRejectsBadRequests(t *testing.T) {
	h := newSchedulerHandler()
	if _, err := h.transformRequest("NoSuchMethod", nil); !errors.IsInvalidInput(err) {
		t.Errorf("unknown method error = %v, want invalid input", err)
	}
	for method := range schedulerJobArgs {
		if _, err := h.transformRequest(method, struct{}{}); !errors.IsInvalidInput(err) {
			t.Errorf("%s with a foreign request type: error = %v, want invalid input", method, err)
		}
	}
	nilRequests := map[string]interface{}{
		"CreateJob":     (*scheduler.CreateJobRequest)(nil),
		"UpdateJob":     (*scheduler.UpdateJobRequest)(nil),
		"GetJob":        (*scheduler.GetJobRequest)(nil),
		"TriggerJob":    (*scheduler.TriggerJobRequest)(nil),
		"PauseJob":      (*scheduler.PauseJobRequest)(nil),
		"ResumeJob":     (*scheduler.ResumeJobRequest)(nil),
		"DeleteJob":     (*scheduler.DeleteJobRequest)(nil),
		"CancelJob":     (*scheduler.CancelJobRequest)(nil),
		"GetJobHistory": (*scheduler.GetJobHistoryRequest)(nil),
	}
	for method, req := range nilRequests {
		if _, err := h.transformRequest(method, req); !errors.IsInvalidInput(err) {
			t.Errorf("%s with a nil request: error = %v, want invalid input", method, err)
		}
	}
}

func TestSchedulerTransformResponseRejectsMismatchedResults(t *testing.T) {
	h := newSchedulerHandler()
	bad := map[string]interface{}{
		"job": "not-an-object", "jobs": "x", "job_id": "x", "executions": "x", "stats": "x",
	}
	for method := range schedulerJobResults {
		if _, err := h.transformResponse(method, nil, bad); err == nil {
			t.Errorf("%s decoded a mismatched result", method)
		}
		if _, err := h.transformResponse(method, nil, make(chan int)); err == nil {
			t.Errorf("%s decoded an unencodable result", method)
		}
	}
}

func TestSchedulerTransformResponseWithoutRequest(t *testing.T) {
	h := newSchedulerHandler()
	got, err := h.transformResponse("CancelJob", nil, map[string]interface{}{"job_id": float64(42)})
	if err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	if c, ok := got.(*scheduler.CancelJobResponse); !ok || c.JobID != 42 || c.Reason != "" {
		t.Errorf("CancelJob without a request decoded %+v", got)
	}
	got, err = h.transformResponse("GetJobHistory", nil, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetJobHistory: %v", err)
	}
	if hist, ok := got.(*scheduler.JobHistoryResponse); !ok || hist.JobID != 0 || len(hist.Executions) != 0 {
		t.Errorf("GetJobHistory without a request decoded %+v", got)
	}
	if got, err := h.transformResponse("NoSuchMethod", nil, "raw"); err != nil || got != "raw" {
		t.Errorf("unknown method result = %v, %v, want it passed through", got, err)
	}
}

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
