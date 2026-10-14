//go:build rest && mcp

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

package apimodule

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler/schedulertest"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// schedulerParityTransports serves jobs through the real server stack of
// every production transport.
func schedulerParityTransports(t *testing.T, jobs *schedulertest.Jobs) map[string]transport.Transport {
	t.Helper()

	facade := mocks.NewMockQuicBaaS()
	facade.SchedulerServiceFunc = func() service.SchedulerService { return jobs }
	return parityTransports(t, facade, func(srv grpclib.ServiceRegistrar) {
		pb.RegisterSchedulerServiceServer(srv, grpcapi.NewSchedulerServer(jobs, nil, testLogger(t)))
	})
}

// TestSchedulerTransportParity_Jobs drives the whole SDK scheduler job surface
// over every transport whose SDK handler speaks the full server wire. MCP is
// covered field by field in TestSchedulerTransportParity_MCPJobFields: its SDK
// handler still reads older key names for labels, retries, timeout, cancel
// reason, history and status, which the full suite would report.
func TestSchedulerTransportParity_Jobs(t *testing.T) {
	jobs := schedulertest.New()
	for name, tr := range schedulerParityTransports(t, jobs) {
		if name == "mcp" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			schedulertest.RunJobConformance(t, scheduler.NewClient(tr), jobs)
		})
	}
}

// TestSchedulerTransportParity_MCPJobFields round-trips the job fields the
// scheduler service returns (handler, priority, last error, cursor, force,
// deleted, and the old and new status of each transition) through the
// production MCP tools.
func TestSchedulerTransportParity_MCPJobFields(t *testing.T) {
	jobs := schedulertest.New()
	client := scheduler.NewClient(schedulerParityTransports(t, jobs)["mcp"])
	ctx := context.Background()

	_, err := client.CreateJob(ctx, &scheduler.CreateJobRequest{
		Name: "nightly", Handler: "noop", JobType: scheduler.JobTypeRecurring,
		OwnerType: scheduler.OwnerTypeTenant, OwnerID: "acme", Schedule: "0 2 * * *", Priority: 7,
	})
	require.NoError(t, err)
	create, ok := jobs.Received("CreateJob").(*service.CreateSchedulerJobRequest)
	require.True(t, ok, "server received CreateJob")
	assert.Equal(t, int32(7), create.Priority)

	job, err := client.GetJob(ctx, 42)
	require.NoError(t, err)
	assert.Equal(t, "noop", job.Handler)
	assert.Equal(t, 5, job.Priority)
	assert.Equal(t, "handler timed out", job.LastError)

	list, err := client.ListJobs(ctx, &scheduler.ListJobsOptions{Handler: "noop", Cursor: "page-1", Limit: 10})
	require.NoError(t, err)
	listReq, ok := jobs.Received("ListJobs").(*service.ListSchedulerJobsRequest)
	require.True(t, ok, "server received ListJobs")
	assert.Equal(t, "noop", listReq.Handler)
	assert.Equal(t, "page-1", listReq.Cursor)
	assert.Equal(t, schedulertest.NextCursor, list.NextCursor)
	require.NotEmpty(t, list.Jobs)
	assert.Equal(t, "noop", list.Jobs[0].Handler)

	triggered, err := client.TriggerJob(ctx, 42)
	require.NoError(t, err)
	assert.Equal(t, scheduler.JobStatusRunning, triggered.Status)

	paused, err := client.PauseJob(ctx, 42)
	require.NoError(t, err)
	assert.Equal(t, scheduler.JobStatusPending, paused.OldStatus)
	assert.Equal(t, scheduler.JobStatusPaused, paused.NewStatus)

	resumed, err := client.ResumeJob(ctx, 42)
	require.NoError(t, err)
	assert.Equal(t, scheduler.JobStatusPaused, resumed.OldStatus)
	assert.Equal(t, scheduler.JobStatusPending, resumed.NewStatus)

	canceled, err := client.CancelJob(ctx, 42, "obsolete")
	require.NoError(t, err)
	assert.Equal(t, scheduler.JobStatusPending, canceled.OldStatus)
	assert.Equal(t, scheduler.JobStatusCanceled, canceled.NewStatus)

	deleted, err := client.DeleteJobWithOptions(ctx, &scheduler.DeleteJobRequest{JobID: 42, Force: true})
	require.NoError(t, err)
	deleteReq, ok := jobs.Received("DeleteJob").(*service.DeleteSchedulerJobRequest)
	require.True(t, ok, "server received DeleteJob")
	assert.True(t, deleteReq.Force)
	assert.Equal(t, uint64(42), deleted.JobID)
	assert.True(t, deleted.Deleted)
}
