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

package scheduler

import (
	"context"
	"sort"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// ServiceName is the service identifier every transport routes scheduler
// requests by.
const ServiceName = serviceName

// Backends are the two server-side halves a scheduler request reaches. A
// transport supplies them in service-layer shape — the embedded transport
// passes the server's own services, the gRPC transport adapts the generated
// client, the REST transport adapts its HTTP routes — and Dispatch does the
// SDK mapping once for all of them. Either half may be nil when the server
// does not run it; a method of an absent half fails with an unavailable
// error naming it.
type Backends struct {
	Jobs      service.SchedulerService
	Placement service.PlacementService
}

// dispatchFunc serves one SDK scheduler method.
type dispatchFunc func(ctx context.Context, b Backends, req any) (any, error)

// dispatchers maps each SDK method to its server call.
var dispatchers = map[string]dispatchFunc{
	"CreateJob":     dispatchCreateJob,
	"GetJob":        dispatchGetJob,
	"UpdateJob":     dispatchUpdateJob,
	"DeleteJob":     dispatchDeleteJob,
	"ListJobs":      dispatchListJobs,
	"TriggerJob":    dispatchTriggerJob,
	"PauseJob":      dispatchPauseJob,
	"ResumeJob":     dispatchResumeJob,
	"CancelJob":     dispatchCancelJob,
	"GetJobHistory": dispatchGetJobHistory,
	"GetStatus":     dispatchGetStatus,

	"RegisterNode":   dispatchRegisterNode,
	"RenewNode":      dispatchRenewNode,
	"DeregisterNode": dispatchDeregisterNode,
	"GetNode":        nodeDispatch("GetNode", service.PlacementService.GetNode),
	"CordonNode":     nodeDispatch("CordonNode", service.PlacementService.CordonNode),
	"UncordonNode":   nodeDispatch("UncordonNode", service.PlacementService.UncordonNode),
	"ListNodes":      dispatchListNodes,
	"DrainNode":      dispatchDrainNode,
	"Place":          dispatchPlace,
	"Release":        dispatchRelease,
	"GetPlacement":   dispatchGetPlacement,
	"ListPlacements": dispatchListPlacements,
}

// Dispatch serves one SDK scheduler method against b and returns the SDK
// response type the client expects.
func Dispatch(ctx context.Context, b Backends, method string, req any) (any, error) {
	fn, ok := dispatchers[method]
	if !ok {
		return nil, errors.NewUnimplemented("scheduler."+method, "unknown scheduler method")
	}
	return fn(ctx, b, req)
}

// requestAs asserts the request type a method expects.
func requestAs[T any](method string, req any) (*T, error) {
	typed, ok := req.(*T)
	if !ok || typed == nil {
		return nil, errors.NewInvalidInput("scheduler."+method, "invalid request type")
	}
	return typed, nil
}

// jobs returns the job half or the unavailable error.
func (b Backends) jobs(method string) (service.SchedulerService, error) {
	if b.Jobs == nil {
		return nil, errors.NewUnavailable("scheduler."+method, "scheduler job service not available")
	}
	return b.Jobs, nil
}

// placement returns the placement half or the unavailable error.
func (b Backends) placement(method string) (service.PlacementService, error) {
	if b.Placement == nil {
		return nil, errors.NewUnavailable("scheduler."+method, "scheduler placement service not available")
	}
	return b.Placement, nil
}

func dispatchCreateJob(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[CreateJobRequest]("CreateJob", req)
	if err != nil {
		return nil, err
	}
	jobs, err := b.jobs("CreateJob")
	if err != nil {
		return nil, err
	}
	resp, err := jobs.CreateJob(ctx, CreateJobRequestToService(r))
	if err != nil {
		return nil, err
	}
	return CreateJobResponseFromService(resp), nil
}

func dispatchGetJob(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[GetJobRequest]("GetJob", req)
	if err != nil {
		return nil, err
	}
	jobs, err := b.jobs("GetJob")
	if err != nil {
		return nil, err
	}
	resp, err := jobs.GetJob(ctx, &service.GetSchedulerJobRequest{JobID: r.JobID, TenantID: r.TenantID, Scope: JobScopeToService(r.Scope)})
	if err != nil {
		return nil, err
	}
	return JobInfoFromService(resp.Job), nil
}

func dispatchUpdateJob(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[UpdateJobRequest]("UpdateJob", req)
	if err != nil {
		return nil, err
	}
	jobs, err := b.jobs("UpdateJob")
	if err != nil {
		return nil, err
	}
	resp, err := jobs.UpdateJob(ctx, UpdateJobRequestToService(r))
	if err != nil {
		return nil, err
	}
	return JobInfoFromService(resp.Job), nil
}

func dispatchDeleteJob(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[DeleteJobRequest]("DeleteJob", req)
	if err != nil {
		return nil, err
	}
	jobs, err := b.jobs("DeleteJob")
	if err != nil {
		return nil, err
	}
	resp, err := jobs.DeleteJob(ctx, &service.DeleteSchedulerJobRequest{JobID: r.JobID, TenantID: r.TenantID, Scope: JobScopeToService(r.Scope), Force: r.Force})
	if err != nil {
		return nil, err
	}
	return DeleteJobResponseFromService(resp), nil
}

func dispatchListJobs(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[ListJobsOptions]("ListJobs", req)
	if err != nil {
		return nil, err
	}
	jobs, err := b.jobs("ListJobs")
	if err != nil {
		return nil, err
	}
	resp, err := jobs.ListJobs(ctx, ListJobsOptionsToService(r))
	if err != nil {
		return nil, err
	}
	return ListJobsResponseFromService(resp), nil
}

func dispatchTriggerJob(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[TriggerJobRequest]("TriggerJob", req)
	if err != nil {
		return nil, err
	}
	jobs, err := b.jobs("TriggerJob")
	if err != nil {
		return nil, err
	}
	resp, err := jobs.TriggerJob(ctx, &service.TriggerSchedulerJobRequest{JobID: r.JobID, TenantID: r.TenantID, Scope: JobScopeToService(r.Scope)})
	if err != nil {
		return nil, err
	}
	return TriggerJobResponseFromService(resp), nil
}

func dispatchPauseJob(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[PauseJobRequest]("PauseJob", req)
	if err != nil {
		return nil, err
	}
	jobs, err := b.jobs("PauseJob")
	if err != nil {
		return nil, err
	}
	resp, err := jobs.PauseJob(ctx, &service.PauseSchedulerJobRequest{JobID: r.JobID, TenantID: r.TenantID, Scope: JobScopeToService(r.Scope)})
	if err != nil {
		return nil, err
	}
	return PauseJobResponseFromService(resp), nil
}

func dispatchResumeJob(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[ResumeJobRequest]("ResumeJob", req)
	if err != nil {
		return nil, err
	}
	jobs, err := b.jobs("ResumeJob")
	if err != nil {
		return nil, err
	}
	resp, err := jobs.ResumeJob(ctx, &service.ResumeSchedulerJobRequest{JobID: r.JobID, TenantID: r.TenantID, Scope: JobScopeToService(r.Scope)})
	if err != nil {
		return nil, err
	}
	return ResumeJobResponseFromService(resp), nil
}

func dispatchCancelJob(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[CancelJobRequest]("CancelJob", req)
	if err != nil {
		return nil, err
	}
	jobs, err := b.jobs("CancelJob")
	if err != nil {
		return nil, err
	}
	resp, err := jobs.CancelJob(ctx, &service.CancelSchedulerJobRequest{JobID: r.JobID, TenantID: r.TenantID, Scope: JobScopeToService(r.Scope), Reason: r.Reason})
	if err != nil {
		return nil, err
	}
	return CancelJobResponseFromService(resp, r.Reason), nil
}

func dispatchGetJobHistory(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[GetJobHistoryRequest]("GetJobHistory", req)
	if err != nil {
		return nil, err
	}
	jobs, err := b.jobs("GetJobHistory")
	if err != nil {
		return nil, err
	}
	resp, err := jobs.GetJobHistory(ctx, &service.GetSchedulerJobHistoryRequest{
		JobID:    r.JobID,
		TenantID: r.TenantID,
		Scope:    JobScopeToService(r.Scope),
		Limit:    HistoryLimit(r.Options),
	})
	if err != nil {
		return nil, err
	}
	return JobHistoryResponseFromService(r.JobID, resp), nil
}

func dispatchGetStatus(ctx context.Context, b Backends, req any) (any, error) {
	if _, err := requestAs[GetStatusRequest]("GetStatus", req); err != nil {
		return nil, err
	}
	jobs, err := b.jobs("GetStatus")
	if err != nil {
		return nil, err
	}
	resp, err := jobs.GetStatus(ctx)
	if err != nil {
		return nil, err
	}
	return SchedulerStatusFromService(resp, time.Now()), nil
}

func dispatchRegisterNode(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[RegisterNodeRequest]("RegisterNode", req)
	if err != nil {
		return nil, err
	}
	svc, err := b.placement("RegisterNode")
	if err != nil {
		return nil, err
	}
	return svc.RegisterNode(ctx, &service.RegisterPlacementNodeRequest{Facts: r.Facts, TTLSeconds: r.TTLSeconds})
}

func dispatchRenewNode(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[NodeRequest]("RenewNode", req)
	if err != nil {
		return nil, err
	}
	svc, err := b.placement("RenewNode")
	if err != nil {
		return nil, err
	}
	if err := svc.RenewNode(ctx, r.NodeID); err != nil {
		return nil, err
	}
	return &RenewNodeResponse{NodeID: r.NodeID, Renewed: true}, nil
}

func dispatchDeregisterNode(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[NodeRequest]("DeregisterNode", req)
	if err != nil {
		return nil, err
	}
	svc, err := b.placement("DeregisterNode")
	if err != nil {
		return nil, err
	}
	if err := svc.DeregisterNode(ctx, r.NodeID); err != nil {
		return nil, err
	}
	return &DeregisterNodeResponse{NodeID: r.NodeID, Deregistered: true}, nil
}

// nodeDispatch serves a node-keyed placement call that returns the node.
func nodeDispatch(method string, call func(service.PlacementService, context.Context, string) (*NodeInfo, error)) dispatchFunc {
	return func(ctx context.Context, b Backends, req any) (any, error) {
		r, err := requestAs[NodeRequest](method, req)
		if err != nil {
			return nil, err
		}
		svc, err := b.placement(method)
		if err != nil {
			return nil, err
		}
		return call(svc, ctx, r.NodeID)
	}
}

func dispatchListNodes(ctx context.Context, b Backends, req any) (any, error) {
	if _, err := requestAs[ListNodesRequest]("ListNodes", req); err != nil {
		return nil, err
	}
	svc, err := b.placement("ListNodes")
	if err != nil {
		return nil, err
	}
	nodes, err := svc.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	return &ListNodesResponse{Nodes: nodes}, nil
}

func dispatchDrainNode(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[NodeRequest]("DrainNode", req)
	if err != nil {
		return nil, err
	}
	svc, err := b.placement("DrainNode")
	if err != nil {
		return nil, err
	}
	moved, err := svc.DrainNode(ctx, r.NodeID)
	if err != nil {
		return nil, err
	}
	return &DrainNodeResponse{NodeID: r.NodeID, Placements: moved}, nil
}

func dispatchPlace(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[PlacementRequest]("Place", req)
	if err != nil {
		return nil, err
	}
	svc, err := b.placement("Place")
	if err != nil {
		return nil, err
	}
	return svc.Place(ctx, r)
}

func dispatchRelease(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[WorkloadRequest]("Release", req)
	if err != nil {
		return nil, err
	}
	svc, err := b.placement("Release")
	if err != nil {
		return nil, err
	}
	if err := svc.Release(ctx, r.WorkloadID); err != nil {
		return nil, err
	}
	return &ReleaseResponse{WorkloadID: r.WorkloadID, Released: true}, nil
}

func dispatchGetPlacement(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[WorkloadRequest]("GetPlacement", req)
	if err != nil {
		return nil, err
	}
	svc, err := b.placement("GetPlacement")
	if err != nil {
		return nil, err
	}
	return svc.GetPlacement(ctx, r.WorkloadID)
}

func dispatchListPlacements(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[PlacementFilter]("ListPlacements", req)
	if err != nil {
		return nil, err
	}
	svc, err := b.placement("ListPlacements")
	if err != nil {
		return nil, err
	}
	out, err := svc.ListPlacements(ctx, r)
	if err != nil {
		return nil, err
	}
	return &ListPlacementsResponse{Placements: out}, nil
}

// Methods returns every SDK scheduler method Dispatch serves, in ascending
// order, so a transport registers exactly the set the client can send.
func Methods() []string {
	methods := make([]string, 0, len(dispatchers))
	for m := range dispatchers {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	return methods
}
