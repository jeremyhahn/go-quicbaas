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

package schedulertest

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// RunJobConformance drives every job method of client, which must reach
// jobs through the transport under test, and checks both what the server
// received and what the client decoded.
func RunJobConformance(t *testing.T, client scheduler.Client, jobs *Jobs) {
	t.Helper()
	ctx := context.Background()

	created, err := client.CreateJob(ctx, &scheduler.CreateJobRequest{
		Name:         "nightly",
		JobType:      scheduler.JobTypeRecurring,
		Handler:      "noop",
		Schedule:     "0 2 * * *",
		OwnerType:    scheduler.OwnerTypeTenant,
		OwnerID:      "acme",
		MaxRetries:   3,
		Timeout:      30 * time.Second,
		Tags:         map[string]string{"team": "ops"},
		NodeAffinity: "node-a",
		Priority:     7,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if created.JobID != jobs.Job.ID {
		t.Errorf("CreateJob decoded job ID %d, want %d", created.JobID, jobs.Job.ID)
	}
	got, ok := jobs.Received("CreateJob").(*service.CreateSchedulerJobRequest)
	if !ok {
		t.Fatalf("server received %T for CreateJob", jobs.Received("CreateJob"))
	}
	if got.Name != "nightly" || got.Type != service.SchedulerJobTypeRecurring || got.Handler != "noop" ||
		got.Schedule != "0 2 * * *" || got.OwnerType != service.SchedulerOwnerTypeTenant || got.MaxRetries != 3 ||
		got.Timeout != 30*time.Second || got.Labels["team"] != "ops" || got.NodeAffinity != "node-a" || got.Priority != 7 {
		t.Errorf("server received CreateJob %+v", got)
	}

	info, err := client.GetJob(ctx, 42)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if r, _ := jobs.Received("GetJob").(*service.GetSchedulerJobRequest); r == nil || r.JobID != 42 {
		t.Errorf("server received GetJob %+v", jobs.Received("GetJob"))
	}
	if info.JobID != 42 || info.Name != "nightly" || info.JobType != scheduler.JobTypeRecurring ||
		info.Status != scheduler.JobStatusPending || info.Tags["team"] != "ops" || info.ExecutionCount != 3 ||
		info.Handler != "noop" || info.Priority != 5 || info.LastError != "handler timed out" {
		t.Errorf("GetJob decoded %+v", info)
	}

	if _, err := client.UpdateJob(ctx, &scheduler.UpdateJobRequest{JobID: 42, Name: "renamed", MaxRetries: 5}); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if r, _ := jobs.Received("UpdateJob").(*service.UpdateSchedulerJobRequest); r == nil || r.JobID != 42 || r.Name != "renamed" || r.MaxRetries != 5 {
		t.Errorf("server received UpdateJob %+v", jobs.Received("UpdateJob"))
	}

	status := scheduler.JobStatusPending
	list, err := client.ListJobs(ctx, &scheduler.ListJobsOptions{Status: &status, Limit: 10, Handler: "noop", Cursor: "page-1"})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(list.Jobs) != 1 || list.Jobs[0].JobID != 42 || list.Jobs[0].Handler != "noop" ||
		list.NextCursor != NextCursor || !list.HasMore {
		t.Errorf("ListJobs decoded %+v", list)
	}
	if r, _ := jobs.Received("ListJobs").(*service.ListSchedulerJobsRequest); r == nil || r.Status != service.SchedulerJobStatusPending || r.Limit != 10 ||
		r.Handler != "noop" || r.Cursor != "page-1" {
		t.Errorf("server received ListJobs %+v", jobs.Received("ListJobs"))
	}

	trigger, err := client.TriggerJob(ctx, 42)
	if err != nil {
		t.Fatalf("TriggerJob: %v", err)
	}
	if r, _ := jobs.Received("TriggerJob").(*service.TriggerSchedulerJobRequest); r == nil || r.JobID != 42 {
		t.Errorf("server received TriggerJob %+v", jobs.Received("TriggerJob"))
	}
	if trigger.JobID != 42 || trigger.Status != scheduler.JobStatusRunning {
		t.Errorf("TriggerJob decoded %+v", trigger)
	}

	pause, err := client.PauseJob(ctx, 42)
	if err != nil {
		t.Fatalf("PauseJob: %v", err)
	}
	if r, _ := jobs.Received("PauseJob").(*service.PauseSchedulerJobRequest); r == nil || r.JobID != 42 {
		t.Errorf("server received PauseJob %+v", jobs.Received("PauseJob"))
	}
	if pause.JobID != 42 || pause.OldStatus != scheduler.JobStatusPending || pause.NewStatus != scheduler.JobStatusPaused {
		t.Errorf("PauseJob decoded %+v", pause)
	}

	resume, err := client.ResumeJob(ctx, 42)
	if err != nil {
		t.Fatalf("ResumeJob: %v", err)
	}
	if r, _ := jobs.Received("ResumeJob").(*service.ResumeSchedulerJobRequest); r == nil || r.JobID != 42 {
		t.Errorf("server received ResumeJob %+v", jobs.Received("ResumeJob"))
	}
	if resume.JobID != 42 || resume.OldStatus != scheduler.JobStatusPaused || resume.NewStatus != scheduler.JobStatusPending {
		t.Errorf("ResumeJob decoded %+v", resume)
	}

	if err := client.DeleteJob(ctx, 42); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	if r, _ := jobs.Received("DeleteJob").(*service.DeleteSchedulerJobRequest); r == nil || r.JobID != 42 || r.Force {
		t.Errorf("server received DeleteJob %+v", jobs.Received("DeleteJob"))
	}

	deleted, err := client.DeleteJobWithOptions(ctx, &scheduler.DeleteJobRequest{JobID: 42, Force: true})
	if err != nil {
		t.Fatalf("DeleteJobWithOptions: %v", err)
	}
	if r, _ := jobs.Received("DeleteJob").(*service.DeleteSchedulerJobRequest); r == nil || r.JobID != 42 || !r.Force {
		t.Errorf("server received forced DeleteJob %+v", jobs.Received("DeleteJob"))
	}
	if deleted.JobID != 42 || !deleted.Deleted {
		t.Errorf("DeleteJobWithOptions decoded %+v", deleted)
	}

	cancel, err := client.CancelJob(ctx, 42, "maintenance")
	if err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	if r, _ := jobs.Received("CancelJob").(*service.CancelSchedulerJobRequest); r == nil || r.JobID != 42 || r.Reason != "maintenance" {
		t.Errorf("server received CancelJob %+v", jobs.Received("CancelJob"))
	}
	if cancel.JobID != 42 || cancel.Reason != "maintenance" ||
		cancel.OldStatus != scheduler.JobStatusPending || cancel.NewStatus != scheduler.JobStatusCanceled {
		t.Errorf("CancelJob decoded %+v", cancel)
	}

	history, err := client.GetJobHistory(ctx, 42, &scheduler.HistoryOptions{Limit: 5})
	if err != nil {
		t.Fatalf("GetJobHistory: %v", err)
	}
	if r, _ := jobs.Received("GetJobHistory").(*service.GetSchedulerJobHistoryRequest); r == nil || r.JobID != 42 || r.Limit != 5 {
		t.Errorf("server received GetJobHistory %+v", jobs.Received("GetJobHistory"))
	}
	if len(history.Executions) != 1 || history.Executions[0].ExecutionID != 7 || history.Executions[0].Status != scheduler.JobStatusCompleted {
		t.Errorf("GetJobHistory decoded %+v", history)
	}

	st, err := client.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !st.Healthy || st.TotalJobs != 5 || st.FailedJobs != 1 {
		t.Errorf("GetStatus decoded %+v", st)
	}
}

// RunJobScopeConformance checks that a transport carries the scope a by-ID
// request names to the scheduler service. A tenant job and a job delegated
// to the caller can share an ID, so every by-ID request naming the
// delegated scope must arrive with it.
func RunJobScopeConformance(t *testing.T, client scheduler.Client, jobs *Jobs) {
	t.Helper()
	ctx := context.Background()
	delegated := scheduler.OwnerTypeDelegated
	scope := scheduler.WithJobScope(delegated)

	calls := []struct {
		method string
		call   func() error
	}{
		{"GetJob", func() error { _, err := client.GetJob(ctx, 42, scope); return err }},
		{"UpdateJob", func() error {
			_, err := client.UpdateJob(ctx, &scheduler.UpdateJobRequest{JobID: 42, Name: "renamed", Scope: &delegated})
			return err
		}},
		{"TriggerJob", func() error { _, err := client.TriggerJob(ctx, 42, scope); return err }},
		{"PauseJob", func() error { _, err := client.PauseJob(ctx, 42, scope); return err }},
		{"ResumeJob", func() error { _, err := client.ResumeJob(ctx, 42, scope); return err }},
		{"CancelJob", func() error { _, err := client.CancelJob(ctx, 42, "maintenance", scope); return err }},
		{"GetJobHistory", func() error { _, err := client.GetJobHistory(ctx, 42, nil, scope); return err }},
		{"DeleteJob", func() error { return client.DeleteJob(ctx, 42, scope) }},
	}

	for _, c := range calls {
		if err := c.call(); err != nil {
			t.Fatalf("%s with the delegated scope: %v", c.method, err)
		}
		got, ok := receivedScope(jobs.Received(c.method))
		if !ok {
			t.Fatalf("server received %T for %s", jobs.Received(c.method), c.method)
		}
		if got != service.SchedulerOwnerTypeDelegated {
			t.Errorf("server received %s with scope %q, want %q", c.method, got, service.SchedulerOwnerTypeDelegated)
		}
	}
}

// receivedScope returns the scope of a by-ID request the server received,
// or false when req is not one.
func receivedScope(req any) (service.SchedulerOwnerType, bool) {
	switch r := req.(type) {
	case *service.GetSchedulerJobRequest:
		return r.Scope, true
	case *service.UpdateSchedulerJobRequest:
		return r.Scope, true
	case *service.DeleteSchedulerJobRequest:
		return r.Scope, true
	case *service.TriggerSchedulerJobRequest:
		return r.Scope, true
	case *service.PauseSchedulerJobRequest:
		return r.Scope, true
	case *service.ResumeSchedulerJobRequest:
		return r.Scope, true
	case *service.CancelSchedulerJobRequest:
		return r.Scope, true
	case *service.GetSchedulerJobHistoryRequest:
		return r.Scope, true
	}
	return "", false
}

// RunPlacementConformance drives every placement method of client, which
// must reach a real placement engine through the transport under test, and
// checks the engine's state after each write by reading it back. Every node
// and workload ID is prefixed with prefix, and list results are checked for
// membership, so the routine can run against a server other tests share.
//
// ctx is the caller's request context. Placement is tenant-scoped and refuses
// a request with no tenant bound, so ctx must carry whatever the transport
// under test binds the tenant from: an authenticated client on the wire, or
// transport.ContextWithTenant for an in-process caller.
func RunPlacementConformance(t *testing.T, ctx context.Context, client scheduler.Client, prefix string) {
	t.Helper()
	n1, n2, w1, group := prefix+"n1", prefix+"n2", prefix+"w1", prefix+"web"
	arch := prefix + "arch"

	node, err := client.RegisterNode(ctx, &scheduler.RegisterNodeRequest{
		Facts: scheduler.NodeFacts{
			NodeID:   n1,
			Capacity: map[string]int64{"cpu": 8, "mem": 1024},
			Labels:   map[string]string{"zone": "a", arch: "arm64"},
		},
		TTLSeconds: 30,
	})
	if err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}
	if node.Facts.NodeID != n1 || node.TTLSeconds != 30 || node.Facts.Labels[arch] != "arm64" || node.LeaseID == 0 {
		t.Errorf("RegisterNode decoded %+v", node)
	}
	if _, err := client.RegisterNode(ctx, &scheduler.RegisterNodeRequest{
		Facts:      scheduler.NodeFacts{NodeID: n2, Capacity: map[string]int64{"cpu": 8}, Labels: map[string]string{"zone": "b", arch: "amd64"}},
		TTLSeconds: 30,
	}); err != nil {
		t.Fatalf("RegisterNode n2: %v", err)
	}
	if err := client.RenewNode(ctx, n1); err != nil {
		t.Fatalf("RenewNode: %v", err)
	}

	res, err := client.Place(ctx, &scheduler.PlacementRequest{
		WorkloadID:   w1,
		Group:        group,
		Resources:    map[string]int64{"cpu": 3},
		Constraints:  []scheduler.Constraint{{Key: arch, Operator: scheduler.OperatorIn, Values: []string{"arm64"}}},
		AntiAffinity: []scheduler.AffinityTerm{{Group: group, Required: true}},
		Spread:       &scheduler.SpreadConstraint{TopologyKey: "zone", MaxSkew: 1},
		Strategy:     scheduler.StrategySpread,
	})
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if res.Existing || res.Placement.NodeID != n1 {
		t.Errorf("Place decoded %+v; the label constraint must pick %s", res, n1)
	}

	again, err := client.Place(ctx, &scheduler.PlacementRequest{WorkloadID: w1, Group: group})
	if err != nil || !again.Existing {
		t.Errorf("idempotent Place = %+v, %v", again, err)
	}

	read, err := client.GetNode(ctx, n1)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if read.Allocated["cpu"] != 3 || read.Placements != 1 {
		t.Errorf("the server did not record the resources: %+v", read)
	}

	nodes, err := client.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	listed := map[string]bool{}
	for _, n := range nodes {
		listed[n.Facts.NodeID] = true
	}
	if !listed[n1] || !listed[n2] {
		t.Errorf("ListNodes is missing %s or %s", n1, n2)
	}

	pl, err := client.GetPlacement(ctx, w1)
	if err != nil || pl.Group != group || pl.Resources["cpu"] != 3 || pl.Orphaned {
		t.Errorf("GetPlacement = %+v, %v", pl, err)
	}
	inGroup, err := client.ListPlacements(ctx, &scheduler.PlacementFilter{Group: group})
	if err != nil || len(inGroup) != 1 {
		t.Errorf("ListPlacements(group) = %+v, %v", inGroup, err)
	}
	onN2, err := client.ListPlacements(ctx, &scheduler.PlacementFilter{NodeID: n2})
	if err != nil || len(onN2) != 0 {
		t.Errorf("ListPlacements(%s) = %+v, %v", n2, onN2, err)
	}

	cordoned, err := client.CordonNode(ctx, n2)
	if err != nil || !cordoned.Status.Unschedulable {
		t.Errorf("CordonNode = %+v, %v", cordoned, err)
	}
	uncordoned, err := client.UncordonNode(ctx, n2)
	if err != nil || uncordoned.Status.Unschedulable {
		t.Errorf("UncordonNode = %+v, %v", uncordoned, err)
	}

	moved, err := client.DrainNode(ctx, n1)
	if err != nil || len(moved) != 1 || moved[0].WorkloadID != w1 {
		t.Fatalf("DrainNode = %+v, %v", moved, err)
	}
	drained, err := client.GetNode(ctx, n1)
	if err != nil || !drained.Status.Draining {
		t.Errorf("drained node = %+v, %v", drained, err)
	}

	if err := client.Release(ctx, w1); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := client.GetPlacement(ctx, w1); err == nil {
		t.Error("released placement still readable")
	}
	for _, id := range []string{n1, n2} {
		if err := client.DeregisterNode(ctx, id); err != nil {
			t.Fatalf("DeregisterNode %s: %v", id, err)
		}
	}
	if _, err := client.GetNode(ctx, n1); err == nil {
		t.Error("deregistered node still readable")
	}

	// A failure crosses the transport as an error, not a decoded zero value.
	if _, err := client.Place(ctx, &scheduler.PlacementRequest{WorkloadID: prefix + "huge", Resources: map[string]int64{prefix + "unobtainium": 1}}); err == nil {
		t.Error("an unplaceable workload was placed")
	}
}
