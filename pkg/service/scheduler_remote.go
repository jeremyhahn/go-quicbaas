package service

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// RemoteRunService is the scheduler's remote-handler surface
// (go-quicbaas#1955): a worker that reaches the scheduler over the wire lists
// the open runs of its "remote:" handlers and reports each run's result. See
// docs/scheduler/architecture.md, "Remote handlers".
//
// It is separate from SchedulerService so a SchedulerService that does not
// serve remote handlers stays one; a transport asserts it.
type RemoteRunService interface {
	// ListRemoteRuns returns the open runs of the named remote handlers in
	// the caller's tenant.
	ListRemoteRuns(ctx context.Context, req *ListRemoteRunsRequest) (*ListRemoteRunsResponse, error)

	// CompleteRemoteRun reports one run's result. A run that is not open (a
	// result after its deadline, for another run, or one already concluded)
	// is refused with ErrPreconditionFailed and records nothing.
	CompleteRemoteRun(ctx context.Context, req *CompleteRemoteRunRequest) (*CompleteRemoteRunResponse, error)
}

// ListRemoteRunsRequest names the remote handlers a worker serves.
type ListRemoteRunsRequest struct {
	TenantID string   `json:"tenant_id,omitempty"`
	Handlers []string `json:"handlers"`
}

// ListRemoteRunsResponse carries the open runs.
type ListRemoteRunsResponse struct {
	Runs      []*scheduler.RemoteRun `json:"runs"`
	Timestamp time.Time              `json:"timestamp"`
}

// CompleteRemoteRunRequest reports one run: Error set means the worker's
// handler failed; otherwise Message and Output are the run's result.
type CompleteRemoteRunRequest struct {
	TenantID string                 `json:"tenant_id,omitempty"`
	Run      scheduler.RemoteRunRef `json:"run"`
	Message  string                 `json:"message,omitempty"`
	Output   []byte                 `json:"output,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

// CompleteRemoteRunResponse acknowledges a recorded result.
type CompleteRemoteRunResponse struct {
	JobID     uint64    `json:"job_id"`
	Timestamp time.Time `json:"timestamp"`
}

// remoteRunEngine is the engine half of RemoteRunService
// (scheduler.SchedulerEngine implements it).
type remoteRunEngine interface {
	ListRemoteRuns(ctx context.Context, tenantID string, handlers []string) ([]*scheduler.RemoteRun, error)
	CompleteRemoteRun(ctx context.Context, ref scheduler.RemoteRunRef, result *scheduler.JobResult, runErr error) error
}

var _ RemoteRunService = (*SchedulerAdapter)(nil)

func (a *SchedulerAdapter) remoteRunEngine(op string) (remoteRunEngine, error) {
	engine, ok := a.scheduler.(remoteRunEngine)
	if !ok {
		return nil, NewError(errors.ErrUnimplemented, "scheduler."+op, "this scheduler does not serve remote handlers")
	}
	return engine, nil
}

// ListRemoteRuns lists the open runs of the named remote handlers in the
// caller's tenant.
func (a *SchedulerAdapter) ListRemoteRuns(ctx context.Context, req *ListRemoteRunsRequest) (*ListRemoteRunsResponse, error) {
	if req == nil || len(req.Handlers) == 0 {
		return nil, NewError(errors.ErrInvalidInput, "scheduler.ListRemoteRuns", "name at least one remote handler")
	}
	tenantID, err := schedulerTenantResolveID(ctx, "ListRemoteRuns", req.TenantID)
	if err != nil {
		return nil, err
	}
	engine, err := a.remoteRunEngine("ListRemoteRuns")
	if err != nil {
		return nil, err
	}
	runs, err := engine.ListRemoteRuns(ctx, tenantID, req.Handlers)
	if err != nil {
		if stderrors.Is(err, scheduler.ErrNotRemoteHandler) {
			return nil, WrapError(err, errors.ErrInvalidInput, "scheduler.ListRemoteRuns",
				"a handler name lacks the "+scheduler.RemoteHandlerPrefix+" prefix")
		}
		return nil, err
	}
	if runs == nil {
		runs = []*scheduler.RemoteRun{}
	}
	return &ListRemoteRunsResponse{Runs: runs, Timestamp: time.Now()}, nil
}

// CompleteRemoteRun reports one run's result. The run is looked up in the
// caller's tenant only: the tenant the request's run names is replaced by the
// caller's, so a worker cannot complete another tenant's run.
func (a *SchedulerAdapter) CompleteRemoteRun(ctx context.Context, req *CompleteRemoteRunRequest) (*CompleteRemoteRunResponse, error) {
	if req == nil || req.Run.JobID == 0 {
		return nil, NewError(errors.ErrInvalidInput, "scheduler.CompleteRemoteRun", "name the run")
	}
	tenantID, err := schedulerTenantResolveID(ctx, "CompleteRemoteRun", req.TenantID)
	if err != nil {
		return nil, err
	}
	engine, err := a.remoteRunEngine("CompleteRemoteRun")
	if err != nil {
		return nil, err
	}
	ref := req.Run
	ref.TenantID = tenantID
	var (
		result *scheduler.JobResult
		runErr error
	)
	if req.Error != "" {
		runErr = stderrors.New(req.Error)
	} else {
		result = &scheduler.JobResult{Success: true, Message: req.Message, Output: req.Output}
	}
	if err := engine.CompleteRemoteRun(ctx, ref, result, runErr); err != nil {
		if stderrors.Is(err, scheduler.ErrRemoteRunNotOpen) {
			return nil, WrapError(err, errors.ErrPreconditionFailed, "scheduler.CompleteRemoteRun", err.Error())
		}
		return nil, err
	}
	return &CompleteRemoteRunResponse{JobID: ref.JobID, Timestamp: time.Now()}, nil
}
