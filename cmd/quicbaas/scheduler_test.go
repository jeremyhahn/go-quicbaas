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

package main

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler/schedulertest"
)

// schedulerCLIService is the embedded backend the job command tests run
// against: the mock QuicBaaS with an in-memory scheduler service.
type schedulerCLIService struct {
	*testutil.MockQuicBaaS
	jobs *schedulertest.Jobs
}

// Scheduler returns the in-memory scheduler service.
func (s *schedulerCLIService) Scheduler() service.SchedulerService { return s.jobs }

// withSchedulerCLI points the CLI's SDK client at an embedded client over
// an in-memory scheduler and restores the globals the commands touch.
func withSchedulerCLI(t *testing.T, tenantID string) *schedulertest.Jobs {
	t.Helper()
	jobs := schedulertest.New()
	svc := &schedulerCLIService{MockQuicBaaS: testutil.NewMockQuicBaaS(), jobs: jobs}
	client, err := quicbaas.NewClient("embedded", quicbaas.WithEmbedded(svc))
	require.NoError(t, err)

	origFactory, origTimeout, origTenant := sdkClientFactory, globalTimeout, globalTenantID
	sdkClientFactory = func() (quicbaas.Client, error) { return client, nil }
	globalTimeout = 5 * time.Second
	globalTenantID = tenantID
	t.Cleanup(func() {
		sdkClientFactory, globalTimeout, globalTenantID = origFactory, origTimeout, origTenant
		schedulerOutputFmt = "table"
		schedulerDescription, schedulerType, schedulerSchedule, schedulerHandler = "", "one-shot", "", ""
		schedulerPayload, schedulerTimeout, schedulerDelay, schedulerExecuteAt = "", "5m", "", ""
		schedulerPriority, schedulerMaxRetries, schedulerLimit = 0, 3, 100
		schedulerStatus, schedulerCursor, schedulerReason, schedulerForce = "", "", "", false
	})
	return jobs
}

func TestSchedulerJobCreate(t *testing.T) {
	jobs := withSchedulerCLI(t, "acme")

	out, err := runScheduler(t, "job", "create", "nightly",
		"--type", "delayed", "--description", "rotate", "--handler", "kms.rotate",
		"--delay", "24h", "--execute-at", "2026-10-02T00:00:00Z", "--payload", `{"k":1}`,
		"--priority", "7", "--timeout", "2m", "--max-retries", "4", "--output", "json")
	require.NoError(t, err)

	req, ok := jobs.Received("CreateJob").(*service.CreateSchedulerJobRequest)
	require.True(t, ok)
	assert.Equal(t, "nightly", req.Name)
	assert.Equal(t, "rotate", req.Description)
	assert.Equal(t, service.SchedulerJobTypeDelayed, req.Type)
	assert.Equal(t, "kms.rotate", req.Handler)
	assert.Equal(t, 24*time.Hour, req.Delay)
	assert.Equal(t, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), req.ExecuteAt.UTC())
	assert.Equal(t, []byte(`{"k":1}`), req.Payload)
	assert.Equal(t, int32(7), req.Priority)
	assert.Equal(t, 2*time.Minute, req.Timeout)
	assert.Equal(t, int32(4), req.MaxRetries)
	assert.Equal(t, service.SchedulerOwnerTypeTenant, req.OwnerType)
	assert.Equal(t, "acme", req.OwnerID)

	var view schedulerJobCreatedView
	require.NoError(t, json.Unmarshal([]byte(out), &view))
	assert.Equal(t, uint64(42), view.ID)
	assert.Equal(t, "nightly", view.Name)
	assert.Equal(t, jobs.Job.NextRun.UTC(), view.NextRun.UTC())
}

func TestSchedulerJobCreate_SystemOwnerAndRaw(t *testing.T) {
	jobs := withSchedulerCLI(t, "")

	out, err := runScheduler(t, "job", "create", "backup", "--handler", "backup.full", "--output", "raw")
	require.NoError(t, err)
	assert.Equal(t, "Job created: backup (ID: 42)\n", out)

	req, ok := jobs.Received("CreateJob").(*service.CreateSchedulerJobRequest)
	require.True(t, ok)
	assert.Equal(t, service.SchedulerOwnerTypeSystem, req.OwnerType)
	assert.Empty(t, req.OwnerID)
}

func TestSchedulerJobCreate_InvalidFlags(t *testing.T) {
	cases := map[string][]string{
		"type":       {"--type", "hourly"},
		"timeout":    {"--timeout", "soon"},
		"execute-at": {"--execute-at", "tomorrow"},
		"delay":      {"--delay", "later"},
	}
	for name, flags := range cases {
		t.Run(name, func(t *testing.T) {
			jobs := withSchedulerCLI(t, "acme")
			args := append([]string{"job", "create", "x", "--handler", "h"}, flags...)
			_, err := runScheduler(t, args...)
			require.Error(t, err)
			assert.True(t, errors.IsInvalidInput(err), "want invalid input, got %v", err)
			assert.Nil(t, jobs.Received("CreateJob"), "an invalid flag must not reach the service")
		})
	}
}

func TestSchedulerJobCreate_RequiresHandler(t *testing.T) {
	jobs := withSchedulerCLI(t, "acme")
	_, err := runScheduler(t, "job", "create", "x")
	require.Error(t, err)
	assert.Nil(t, jobs.Received("CreateJob"))
}

func TestSchedulerJobList(t *testing.T) {
	jobs := withSchedulerCLI(t, "acme")

	out, err := runScheduler(t, "job", "list", "--status", "paused", "--handler", "noop",
		"--limit", "10", "--cursor", "c1", "--output", "json")
	require.NoError(t, err)

	req, ok := jobs.Received("ListJobs").(*service.ListSchedulerJobsRequest)
	require.True(t, ok)
	assert.Equal(t, service.SchedulerJobStatusPaused, req.Status)
	assert.Equal(t, "noop", req.Handler)
	assert.Equal(t, 10, req.Limit)
	assert.Equal(t, "c1", req.Cursor)
	assert.Equal(t, service.SchedulerOwnerTypeTenant, req.OwnerType)
	assert.Equal(t, "acme", req.OwnerID)

	var view schedulerJobListView
	require.NoError(t, json.Unmarshal([]byte(out), &view))
	require.Len(t, view.Jobs, 1)
	job := view.Jobs[0]
	assert.Equal(t, uint64(42), job.ID)
	assert.Equal(t, "recurring", job.Type)
	assert.Equal(t, "pending", job.Status)
	assert.Equal(t, "tenant", job.OwnerType)
	assert.Equal(t, "noop", job.Handler)
	assert.Equal(t, 5, job.Priority)
	assert.Equal(t, "handler timed out", job.LastError)
	assert.Equal(t, map[string]string{"team": "ops"}, job.Labels)
	assert.Equal(t, uint64(3), job.RunCount)
	assert.Equal(t, 1, view.TotalCount)
	assert.True(t, view.HasMore)
	assert.Equal(t, schedulertest.NextCursor, view.NextCursor)
}

func TestSchedulerJobList_Table(t *testing.T) {
	withSchedulerCLI(t, "acme")
	out, err := runScheduler(t, "job", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "ID  NAME     TYPE       HANDLER  STATUS   NEXT RUN")
	assert.Contains(t, out, "42  nightly  recurring  noop     pending  2026-10-01T13:00:00Z")
	assert.Contains(t, out, "Total: 1 jobs")
	assert.Contains(t, out, "Next cursor: "+schedulertest.NextCursor)
}

func TestSchedulerJobList_InvalidStatus(t *testing.T) {
	jobs := withSchedulerCLI(t, "acme")
	_, err := runScheduler(t, "job", "list", "--status", "sleeping")
	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err), "want invalid input, got %v", err)
	assert.Nil(t, jobs.Received("ListJobs"))
}

func TestSchedulerJobGet(t *testing.T) {
	withSchedulerCLI(t, "acme")

	out, err := runScheduler(t, "job", "get", "42")
	require.NoError(t, err)
	assert.Contains(t, out, "Name:         nightly")
	assert.Contains(t, out, "Type:         recurring")
	assert.Contains(t, out, "Schedule:     0 2 * * *")
	assert.Contains(t, out, "Next Run:     2026-10-01T13:00:00Z")
	assert.Contains(t, out, "Last Error:   handler timed out")
	assert.Contains(t, out, "Priority:     5")
}

func TestSchedulerJobGet_YAML(t *testing.T) {
	withSchedulerCLI(t, "acme")

	out, err := runScheduler(t, "job", "get", "42", "--output", "yaml")
	require.NoError(t, err)
	var doc map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(out), &doc))
	assert.Equal(t, "nightly", doc["name"])
	assert.Equal(t, "recurring", doc["type"])
	assert.Equal(t, "acme", doc["owner_id"])
}

func TestSchedulerJobDelete(t *testing.T) {
	jobs := withSchedulerCLI(t, "acme")

	out, err := runScheduler(t, "job", "delete", "42", "--force", "--output", "raw")
	require.NoError(t, err)
	assert.Equal(t, "Job 42 deleted successfully\n", out)

	req, ok := jobs.Received("DeleteJob").(*service.DeleteSchedulerJobRequest)
	require.True(t, ok)
	assert.Equal(t, uint64(42), req.JobID)
	assert.True(t, req.Force)
}

func TestSchedulerJobTrigger(t *testing.T) {
	jobs := withSchedulerCLI(t, "acme")

	out, err := runScheduler(t, "job", "trigger", "42", "--output", "json")
	require.NoError(t, err)
	var view schedulerJobTriggeredView
	require.NoError(t, json.Unmarshal([]byte(out), &view))
	assert.Equal(t, schedulerJobTriggeredView{JobID: 42, Status: "running"}, view)

	req, ok := jobs.Received("TriggerJob").(*service.TriggerSchedulerJobRequest)
	require.True(t, ok)
	assert.Equal(t, uint64(42), req.JobID)
}

func TestSchedulerJobStatusChanges(t *testing.T) {
	for _, verb := range []string{"pause", "resume", "cancel"} {
		t.Run(verb, func(t *testing.T) {
			withSchedulerCLI(t, "acme")
			out, err := runScheduler(t, "job", verb, "42", "--output", "json")
			require.NoError(t, err)
			var view schedulerJobStatusChangeView
			require.NoError(t, json.Unmarshal([]byte(out), &view))
			assert.Equal(t, uint64(42), view.JobID)
			assert.NotEmpty(t, view.OldStatus)
			assert.NotEmpty(t, view.NewStatus)
		})
	}
}

func TestSchedulerJobCancel(t *testing.T) {
	jobs := withSchedulerCLI(t, "acme")

	out, err := runScheduler(t, "job", "cancel", "42", "--reason", "retired", "--output", "raw")
	require.NoError(t, err)
	assert.Equal(t, "Job 42 status changed: pending -> canceled\n", out)

	req, ok := jobs.Received("CancelJob").(*service.CancelSchedulerJobRequest)
	require.True(t, ok)
	assert.Equal(t, "retired", req.Reason)
}

func TestSchedulerJobHistory(t *testing.T) {
	jobs := withSchedulerCLI(t, "acme")

	out, err := runScheduler(t, "job", "history", "42", "--limit", "20", "--output", "json")
	require.NoError(t, err)

	req, ok := jobs.Received("GetJobHistory").(*service.GetSchedulerJobHistoryRequest)
	require.True(t, ok)
	assert.Equal(t, uint64(42), req.JobID)
	assert.Equal(t, 20, req.Limit)

	var view schedulerJobHistoryView
	require.NoError(t, json.Unmarshal([]byte(out), &view))
	require.Len(t, view.Executions, 1)
	exec := view.Executions[0]
	assert.Equal(t, uint64(7), exec.ID)
	assert.Equal(t, "completed", exec.Status)
	assert.Equal(t, time.Second, exec.Duration)
	assert.Equal(t, "ok", exec.Output)
}

func TestSchedulerJobHistory_NoCursorFlag(t *testing.T) {
	withSchedulerCLI(t, "acme")
	_, err := runScheduler(t, "job", "history", "42", "--cursor", "c1")
	require.Error(t, err, "history has no cursor: the SDK cannot page it")
}

func TestFormatSchedulerJobHistory_TruncatesErrors(t *testing.T) {
	schedulerOutputFmt = "table"
	t.Cleanup(func() { schedulerOutputFmt = "table" })
	resp := &scheduler.JobHistoryResponse{
		JobID:   42,
		HasMore: true,
		Executions: []scheduler.JobExecution{{
			ExecutionID: 1, Status: scheduler.JobStatusFailed,
			Error: "0123456789012345678901234567890123456789012345678901234567890",
		}},
	}
	var out bytes.Buffer
	require.NoError(t, formatSchedulerJobHistory(&out, resp))
	assert.Contains(t, out.String(), "01234567890123456789012345678901234567890123456...")
	assert.NotContains(t, out.String(), "4567890\n")
	assert.Contains(t, out.String(), "Raise --limit")
}

func TestSchedulerStatus(t *testing.T) {
	jobs := withSchedulerCLI(t, "acme")

	out, err := runScheduler(t, "status", "--output", "json")
	require.NoError(t, err)
	assert.NotNil(t, jobs.Received("GetStatus"))

	var view schedulerStatusView
	require.NoError(t, json.Unmarshal([]byte(out), &view))
	assert.True(t, view.Running)
	assert.Equal(t, uint64(5), view.TotalJobs)
	assert.Equal(t, uint64(1), view.RunningJobs)
	assert.Equal(t, uint64(3), view.SuccessCount)
	assert.Equal(t, uint64(1), view.FailureCount)
}

func TestSchedulerStatus_Raw(t *testing.T) {
	withSchedulerCLI(t, "acme")
	out, err := runScheduler(t, "status", "--output", "raw")
	require.NoError(t, err)
	assert.Equal(t, "Scheduler: running\n", out)
}

func TestSchedulerJob_InvalidJobID(t *testing.T) {
	for _, verb := range []string{"get", "delete", "trigger", "pause", "resume", "cancel", "history"} {
		t.Run(verb, func(t *testing.T) {
			withSchedulerCLI(t, "acme")
			_, err := runScheduler(t, "job", verb, "not-a-number")
			require.Error(t, err)
			assert.True(t, errors.IsInvalidInput(err), "want invalid input, got %v", err)
		})
	}
}

func TestSchedulerJob_ServiceError(t *testing.T) {
	cases := map[string][]string{
		"create":  {"job", "create", "x", "--handler", "h"},
		"list":    {"job", "list"},
		"get":     {"job", "get", "42"},
		"delete":  {"job", "delete", "42"},
		"trigger": {"job", "trigger", "42"},
		"pause":   {"job", "pause", "42"},
		"resume":  {"job", "resume", "42"},
		"cancel":  {"job", "cancel", "42"},
		"history": {"job", "history", "42"},
		"status":  {"status"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			jobs := withSchedulerCLI(t, "acme")
			jobs.Err = errors.Newf(errors.ErrNotFound, "schedulertest", "job missing")
			_, err := runScheduler(t, args...)
			require.Error(t, err)
		})
	}
}

func TestSchedulerJob_FactoryError(t *testing.T) {
	withSchedulerCLI(t, "acme")
	boom := stderrors.New("boom")
	sdkClientFactory = func() (quicbaas.Client, error) { return nil, boom }

	_, err := runScheduler(t, "job", "get", "42")
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}

func TestSchedulerJob_InvalidOutputFormat(t *testing.T) {
	withSchedulerCLI(t, "acme")
	_, err := runScheduler(t, "job", "get", "42", "--output", "xml")
	require.Error(t, err)
}

func TestSchedulerName(t *testing.T) {
	assert.Equal(t, "one-shot", schedulerName(scheduler.JobTypeOneShot))
	assert.Equal(t, "canceled", schedulerName(scheduler.JobStatusCanceled))
	assert.Equal(t, "unknown", schedulerName(scheduler.JobStatus(99)))
}
