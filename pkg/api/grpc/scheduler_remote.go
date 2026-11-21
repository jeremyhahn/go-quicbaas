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

package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Remote handlers (go-quicbaas#1955) over gRPC: a worker lists the open runs
// of its remote: handlers and reports each run's result.

// remoteRunService is the job service's remote-run half.
func (s *SchedulerServer) remoteRunService() (service.RemoteRunService, error) {
	jobs, err := s.jobService()
	if err != nil {
		return nil, err
	}
	remote, ok := jobs.(service.RemoteRunService)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "this scheduler does not serve remote handlers")
	}
	return remote, nil
}

// ListRemoteRuns lists the caller's open runs of the named remote handlers.
func (s *SchedulerServer) ListRemoteRuns(ctx context.Context, req *pb.ListRemoteRunsRequest) (*pb.ListRemoteRunsResponse, error) {
	if len(req.GetHandlers()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one handler is required")
	}
	remote, err := s.remoteRunService()
	if err != nil {
		return nil, err
	}
	resp, err := remote.ListRemoteRuns(ctx, &service.ListRemoteRunsRequest{TenantID: req.GetTenantId(), Handlers: req.GetHandlers()})
	if err != nil {
		return nil, convertSchedulerError(err)
	}
	out := &pb.ListRemoteRunsResponse{Runs: make([]*pb.RemoteRun, 0, len(resp.Runs)), Timestamp: resp.Timestamp.Unix()}
	for _, run := range resp.Runs {
		out.Runs = append(out.Runs, &pb.RemoteRun{
			Run:              remoteRunRefToProto(run.RemoteRunRef),
			Name:             run.Name,
			Handler:          run.Handler,
			Payload:          run.Payload,
			DeadlineUnixNano: unixNanoFromTime(run.Deadline),
		})
	}
	return out, nil
}

// CompleteRemoteRun records a worker's result for one of the caller's runs.
func (s *SchedulerServer) CompleteRemoteRun(ctx context.Context, req *pb.CompleteRemoteRunRequest) (*pb.CompleteRemoteRunResponse, error) {
	if req.GetRun().GetJobId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "run.job_id is required")
	}
	remote, err := s.remoteRunService()
	if err != nil {
		return nil, err
	}
	resp, err := remote.CompleteRemoteRun(ctx, &service.CompleteRemoteRunRequest{
		TenantID: req.GetTenantId(),
		Run:      remoteRunRefFromProto(req.GetRun()),
		Message:  req.GetMessage(),
		Output:   req.GetOutput(),
		Error:    req.GetError(),
	})
	if err != nil {
		return nil, convertSchedulerError(err)
	}
	return &pb.CompleteRemoteRunResponse{JobId: resp.JobID, Timestamp: resp.Timestamp.Unix()}, nil
}

// ListRemoteRuns presents the remote-run half to scheduler.Dispatch.
func (j *SchedulerJobsClient) ListRemoteRuns(ctx context.Context, req *service.ListRemoteRunsRequest) (*service.ListRemoteRunsResponse, error) {
	if req == nil {
		return nil, &errors.QBError{Code: errors.ErrInvalidInput, Op: "Client.ListRemoteRuns", Err: ErrRequestNil}
	}
	resp, err := pb.NewSchedulerServiceClient(j.c.conn).ListRemoteRuns(ctx,
		&pb.ListRemoteRunsRequest{Handlers: req.Handlers, TenantId: req.TenantID})
	if err != nil {
		return nil, convertError(err)
	}
	out := &service.ListRemoteRunsResponse{Runs: make([]*scheduler.RemoteRun, 0, len(resp.GetRuns())), Timestamp: time.Unix(resp.GetTimestamp(), 0)}
	for _, run := range resp.GetRuns() {
		out.Runs = append(out.Runs, &scheduler.RemoteRun{
			RemoteRunRef: remoteRunRefFromProto(run.GetRun()),
			Name:         run.GetName(),
			Handler:      run.GetHandler(),
			Payload:      run.GetPayload(),
			Deadline:     timeFromUnixNano(run.GetDeadlineUnixNano()),
		})
	}
	return out, nil
}

// CompleteRemoteRun presents the remote-run half to scheduler.Dispatch.
func (j *SchedulerJobsClient) CompleteRemoteRun(ctx context.Context, req *service.CompleteRemoteRunRequest) (*service.CompleteRemoteRunResponse, error) {
	if req == nil {
		return nil, &errors.QBError{Code: errors.ErrInvalidInput, Op: "Client.CompleteRemoteRun", Err: ErrRequestNil}
	}
	resp, err := pb.NewSchedulerServiceClient(j.c.conn).CompleteRemoteRun(ctx, &pb.CompleteRemoteRunRequest{
		Run:      remoteRunRefToProto(req.Run),
		Message:  req.Message,
		Output:   req.Output,
		Error:    req.Error,
		TenantId: req.TenantID,
	})
	if err != nil {
		return nil, convertError(err)
	}
	return &service.CompleteRemoteRunResponse{JobID: resp.GetJobId(), Timestamp: time.Unix(resp.GetTimestamp(), 0)}, nil
}

func remoteRunRefToProto(ref scheduler.RemoteRunRef) *pb.RemoteRunRef {
	return &pb.RemoteRunRef{
		JobId:             ref.JobID,
		FiringUnixNano:    unixNanoFromTime(ref.Firing),
		StartedAtUnixNano: unixNanoFromTime(ref.StartedAt),
	}
}

// remoteRunRefFromProto leaves the tenant unset: the service scopes the run to
// the caller's tenant.
func remoteRunRefFromProto(ref *pb.RemoteRunRef) scheduler.RemoteRunRef {
	return scheduler.RemoteRunRef{
		JobID:     ref.GetJobId(),
		Firing:    timeFromUnixNano(ref.GetFiringUnixNano()),
		StartedAt: timeFromUnixNano(ref.GetStartedAtUnixNano()),
	}
}

// The run's times travel as Unix nanoseconds so a reported run matches the
// run the scheduler started exactly; zero is the zero time.
func unixNanoFromTime(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func timeFromUnixNano(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}
