package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// remoteEngine serves the engine's remote-run half and records what the
// adapter asked of it; every other engine method is unused here.
type remoteEngine struct {
	interfaces.SchedulerEngine

	listTenant   string
	listHandlers []string
	runs         []*scheduler.RemoteRun
	listErr      error

	completed  scheduler.RemoteRunRef
	result     *scheduler.JobResult
	runErr     error
	completeTo error
}

func (e *remoteEngine) ListRemoteRuns(_ context.Context, tenantID string, handlers []string) ([]*scheduler.RemoteRun, error) {
	e.listTenant, e.listHandlers = tenantID, handlers
	return e.runs, e.listErr
}

func (e *remoteEngine) CompleteRemoteRun(_ context.Context, ref scheduler.RemoteRunRef, result *scheduler.JobResult, runErr error) error {
	e.completed, e.result, e.runErr = ref, result, runErr
	return e.completeTo
}

func TestSchedulerAdapter_ListRemoteRuns_ListsInTheCallersTenant(t *testing.T) {
	engine := &remoteEngine{runs: []*scheduler.RemoteRun{{RemoteRunRef: scheduler.RemoteRunRef{JobID: 7}}}}
	adapter := NewSchedulerAdapter(engine)

	resp, err := adapter.ListRemoteRuns(tenantctx.WithTenant(context.Background(), "tenant-a"),
		&ListRemoteRunsRequest{Handlers: []string{"remote:renew"}})
	require.NoError(t, err)
	require.Len(t, resp.Runs, 1)
	require.Equal(t, "tenant-a", engine.listTenant)
	require.Equal(t, []string{"remote:renew"}, engine.listHandlers)

	_, err = adapter.ListRemoteRuns(tenantctx.WithTenant(context.Background(), "tenant-a"),
		&ListRemoteRunsRequest{TenantID: "tenant-b", Handlers: []string{"remote:renew"}})
	require.Error(t, err, "a worker named another tenant")

	_, err = adapter.ListRemoteRuns(context.Background(), &ListRemoteRunsRequest{Handlers: []string{"remote:renew"}})
	require.Error(t, err, "a context with no tenant")

	_, err = adapter.ListRemoteRuns(tenantctx.WithTenant(context.Background(), "tenant-a"), &ListRemoteRunsRequest{})
	require.Equal(t, errors.ErrInvalidInput, errors.Code(err), "no handler named: %v", err)

	engine.listErr = scheduler.ErrNotRemoteHandler
	_, err = adapter.ListRemoteRuns(tenantctx.WithTenant(context.Background(), "tenant-a"),
		&ListRemoteRunsRequest{Handlers: []string{"sdlc.reconcile"}})
	require.Equal(t, errors.ErrInvalidInput, errors.Code(err), "a local handler name: %v", err)
}

func TestSchedulerAdapter_CompleteRemoteRun_ScopesTheRunToTheCallersTenant(t *testing.T) {
	engine := &remoteEngine{}
	adapter := NewSchedulerAdapter(engine)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	_, err := adapter.CompleteRemoteRun(ctx, &CompleteRemoteRunRequest{
		Run:     scheduler.RemoteRunRef{JobID: 7, TenantID: "tenant-b"},
		Message: "renewed",
	})
	require.NoError(t, err)
	require.Equal(t, "tenant-a", engine.completed.TenantID, "the run named another tenant's; the caller's is looked up")
	require.NotNil(t, engine.result)
	require.True(t, engine.result.Success)
	require.Equal(t, "renewed", engine.result.Message)
	require.NoError(t, engine.runErr)

	_, err = adapter.CompleteRemoteRun(ctx, &CompleteRemoteRunRequest{Run: scheduler.RemoteRunRef{JobID: 7}, Error: "acme order refused"})
	require.NoError(t, err)
	require.Nil(t, engine.result)
	require.EqualError(t, engine.runErr, "acme order refused")

	engine.completeTo = scheduler.RemoteRunNotOpenError{Reason: "the run already ended"}
	_, err = adapter.CompleteRemoteRun(ctx, &CompleteRemoteRunRequest{Run: scheduler.RemoteRunRef{JobID: 7}})
	require.Equal(t, errors.ErrPreconditionFailed, errors.Code(err), "a run that is not open: %v", err)
	require.True(t, stderrors.Is(err, scheduler.ErrRemoteRunNotOpen), "the engine's refusal stays in the chain: %v", err)

	_, err = adapter.CompleteRemoteRun(ctx, &CompleteRemoteRunRequest{})
	require.Equal(t, errors.ErrInvalidInput, errors.Code(err), "no run named: %v", err)
}

func TestSchedulerAdapter_RemoteRuns_UnimplementedWithoutTheEngineHalf(t *testing.T) {
	adapter := NewSchedulerAdapter(struct{ interfaces.SchedulerEngine }{})
	_, err := adapter.ListRemoteRuns(tenantctx.WithTenant(context.Background(), "tenant-a"),
		&ListRemoteRunsRequest{Handlers: []string{"remote:renew"}})
	require.Equal(t, errors.ErrUnimplemented, errors.Code(err), "%v", err)
}
