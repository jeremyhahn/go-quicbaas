package scheduler

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// Remote handlers (go-quicbaas#1955). A job handler registers in the server's
// process, so a client cannot register one. Instead it creates the job with a
// handler named RemoteHandlerPrefix+<name>, and a Worker in the client runs
// the job's firings: it lists the open runs of its handlers and reports each
// run's result. See docs/scheduler/architecture.md, "Remote handlers".

// RemoteHandlerPrefix marks a job handler that a remote worker executes.
const RemoteHandlerPrefix = scheduler.RemoteHandlerPrefix

// RemoteRunRef names one run of a remotely handled job.
type RemoteRunRef struct {
	JobID     uint64    `json:"job_id"`
	Firing    time.Time `json:"firing"`
	StartedAt time.Time `json:"started_at"`
}

// RemoteRun is an open run for a worker to execute.
type RemoteRun struct {
	RemoteRunRef
	Name    string `json:"name"`
	Handler string `json:"handler"`
	Payload []byte `json:"payload,omitempty"`
	// Deadline is when the scheduler stops waiting and records the run as
	// timed out; a result reported after it is refused.
	Deadline time.Time `json:"deadline"`
}

// ListRemoteRunsRequest names the remote handlers a worker serves.
type ListRemoteRunsRequest struct {
	Handlers []string `json:"handlers"`
	TenantID string   `json:"tenant_id,omitempty"`
}

// ListRemoteRunsResponse carries the open runs.
type ListRemoteRunsResponse struct {
	Runs []*RemoteRun `json:"runs"`
}

// CompleteRemoteRunRequest reports one run: Error set means the handler
// failed; otherwise Message and Output are its result.
type CompleteRemoteRunRequest struct {
	Run      RemoteRunRef `json:"run"`
	Message  string       `json:"message,omitempty"`
	Output   []byte       `json:"output,omitempty"`
	Error    string       `json:"error,omitempty"`
	TenantID string       `json:"tenant_id,omitempty"`
}

// CompleteRemoteRunResponse acknowledges a recorded result.
type CompleteRemoteRunResponse struct {
	JobID uint64 `json:"job_id"`
}

// RemoteRunClient lists and completes remote runs.
type RemoteRunClient interface {
	ListRemoteRuns(ctx context.Context, handlers []string) ([]*RemoteRun, error)
	CompleteRemoteRun(ctx context.Context, req *CompleteRemoteRunRequest) error
}

// NewRemoteRunClient returns the remote-run client over a transport.
func NewRemoteRunClient(t transport.Transport) RemoteRunClient {
	if t == nil {
		return nil
	}
	return &client{transport: t}
}

func (c *client) ListRemoteRuns(ctx context.Context, handlers []string) ([]*RemoteRun, error) {
	if ctx == nil || len(handlers) == 0 {
		return nil, errors.NewInvalidInput("scheduler.ListRemoteRuns", "a context and at least one handler are required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "ListRemoteRuns", &ListRemoteRunsRequest{Handlers: handlers})
	if err != nil {
		return nil, err
	}
	listed, ok := resp.(*ListRemoteRunsResponse)
	if !ok || listed == nil {
		return nil, errors.NewInternal("scheduler.ListRemoteRuns", "invalid response type")
	}
	return listed.Runs, nil
}

func (c *client) CompleteRemoteRun(ctx context.Context, req *CompleteRemoteRunRequest) error {
	if ctx == nil || req == nil || req.Run.JobID == 0 {
		return errors.NewInvalidInput("scheduler.CompleteRemoteRun", "a context and the run are required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "CompleteRemoteRun", req)
	if err != nil {
		return err
	}
	if _, ok := resp.(*CompleteRemoteRunResponse); !ok {
		return errors.NewInternal("scheduler.CompleteRemoteRun", "invalid response type")
	}
	return nil
}

func (b Backends) remoteRuns(method string) (service.RemoteRunService, error) {
	jobs, err := b.jobs(method)
	if err != nil {
		return nil, err
	}
	remote, ok := jobs.(service.RemoteRunService)
	if !ok {
		return nil, errors.NewUnimplemented("scheduler."+method, "this scheduler does not serve remote handlers")
	}
	return remote, nil
}

func dispatchListRemoteRuns(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[ListRemoteRunsRequest]("ListRemoteRuns", req)
	if err != nil {
		return nil, err
	}
	remote, err := b.remoteRuns("ListRemoteRuns")
	if err != nil {
		return nil, err
	}
	resp, err := remote.ListRemoteRuns(ctx, &service.ListRemoteRunsRequest{Handlers: r.Handlers, TenantID: r.TenantID})
	if err != nil {
		return nil, err
	}
	out := &ListRemoteRunsResponse{Runs: make([]*RemoteRun, 0, len(resp.Runs))}
	for _, run := range resp.Runs {
		out.Runs = append(out.Runs, &RemoteRun{
			RemoteRunRef: RemoteRunRef{JobID: run.JobID, Firing: run.Firing, StartedAt: run.StartedAt},
			Name:         run.Name,
			Handler:      run.Handler,
			Payload:      run.Payload,
			Deadline:     run.Deadline,
		})
	}
	return out, nil
}

func dispatchCompleteRemoteRun(ctx context.Context, b Backends, req any) (any, error) {
	r, err := requestAs[CompleteRemoteRunRequest]("CompleteRemoteRun", req)
	if err != nil {
		return nil, err
	}
	remote, err := b.remoteRuns("CompleteRemoteRun")
	if err != nil {
		return nil, err
	}
	resp, err := remote.CompleteRemoteRun(ctx, &service.CompleteRemoteRunRequest{
		TenantID: r.TenantID,
		Run:      scheduler.RemoteRunRef{JobID: r.Run.JobID, Firing: r.Run.Firing, StartedAt: r.Run.StartedAt},
		Message:  r.Message,
		Output:   r.Output,
		Error:    r.Error,
	})
	if err != nil {
		return nil, err
	}
	return &CompleteRemoteRunResponse{JobID: resp.JobID}, nil
}
