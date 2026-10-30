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

package scheduler_test

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement/placementtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler/schedulertest"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// dispatchTransport is a transport.Transport that serves every request with
// scheduler.Dispatch over in-process backends.
type dispatchTransport struct {
	b scheduler.Backends
}

func (d *dispatchTransport) Request(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	return scheduler.Dispatch(ctx, d.b, method, req)
}

func (d *dispatchTransport) RequestStream(context.Context, string, string, interface{}) (transport.Stream, error) {
	return nil, nil
}
func (d *dispatchTransport) Conn() interface{} { return nil }
func (d *dispatchTransport) Close() error      { return nil }
func (d *dispatchTransport) Healthy() bool     { return true }

func TestDispatch_JobConformance(t *testing.T) {
	jobs := schedulertest.New()
	schedulertest.RunJobConformance(t, scheduler.NewClient(&dispatchTransport{b: scheduler.Backends{Jobs: jobs}}), jobs)
}

func TestDispatch_PlacementConformance(t *testing.T) {
	places := service.NewPlacementService(placement.New(placementtest.New()))
	schedulertest.RunPlacementConformance(t, tenantctx.WithTenant(context.Background(), "acme"), scheduler.NewClient(&dispatchTransport{b: scheduler.Backends{Placement: places}}), "")
}

func TestDispatch_JobServiceErrorsPassThrough(t *testing.T) {
	jobs := schedulertest.New()
	jobs.Err = context.DeadlineExceeded
	c := scheduler.NewClient(&dispatchTransport{b: scheduler.Backends{Jobs: jobs}})
	ctx := context.Background()
	calls := map[string]func() error{
		"CreateJob": func() error {
			_, err := c.CreateJob(ctx, &scheduler.CreateJobRequest{Name: "n", Handler: "h", Schedule: "* * * * *", JobType: scheduler.JobTypeRecurring})
			return err
		},
		"GetJob":        func() error { _, err := c.GetJob(ctx, 1); return err },
		"UpdateJob":     func() error { _, err := c.UpdateJob(ctx, &scheduler.UpdateJobRequest{JobID: 1}); return err },
		"DeleteJob":     func() error { return c.DeleteJob(ctx, 1) },
		"ListJobs":      func() error { _, err := c.ListJobs(ctx, nil); return err },
		"TriggerJob":    func() error { _, err := c.TriggerJob(ctx, 1); return err },
		"PauseJob":      func() error { _, err := c.PauseJob(ctx, 1); return err },
		"ResumeJob":     func() error { _, err := c.ResumeJob(ctx, 1); return err },
		"CancelJob":     func() error { _, err := c.CancelJob(ctx, 1, "r"); return err },
		"GetJobHistory": func() error { _, err := c.GetJobHistory(ctx, 1, nil); return err },
		"GetStatus":     func() error { _, err := c.GetStatus(ctx); return err },
	}
	for name, call := range calls {
		if err := call(); err != context.DeadlineExceeded {
			t.Errorf("%s: err = %v, want the service's error", name, err)
		}
	}
}

// A request's scope reaches the service by its service name; no scope is
// the caller's own jobs, and a value with no name is passed on for the
// service to refuse, never read as the caller's own namespace.
func TestJobScopeToService(t *testing.T) {
	delegated := scheduler.OwnerTypeDelegated
	unknown := scheduler.OwnerType(9)
	cases := []struct {
		name  string
		scope *scheduler.OwnerType
		want  service.SchedulerOwnerType
	}{
		{"nil is the caller's own", nil, ""},
		{"delegated", &delegated, service.SchedulerOwnerTypeDelegated},
		{"unknown is passed on", &unknown, "9"},
	}
	for _, tc := range cases {
		if got := scheduler.JobScopeToService(tc.scope); got != tc.want {
			t.Errorf("%s: JobScopeToService = %q, want %q", tc.name, got, tc.want)
		}
	}
}
