package scheduler

import (
	"context"
	stderrors "errors"
	"sync"
	"testing"
	"time"

	pkgscheduler "github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// dispatchTransport serves a client's requests from in-process backends, as
// the embedded transport does.
type dispatchTransport struct {
	transport.Transport
	backends Backends
}

func (d *dispatchTransport) Request(ctx context.Context, _, method string, req interface{}) (interface{}, error) {
	return Dispatch(ctx, d.backends, method, req)
}

// remoteService is a scheduler whose open remote runs are a fixed list; a
// completion is recorded and closes the run.
type remoteService struct {
	service.SchedulerService

	mu        sync.Mutex
	open      []*pkgscheduler.RemoteRun
	completed []*service.CompleteRemoteRunRequest
}

func (s *remoteService) ListRemoteRuns(_ context.Context, req *service.ListRemoteRunsRequest) (*service.ListRemoteRunsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var runs []*pkgscheduler.RemoteRun
	for _, run := range s.open {
		for _, h := range req.Handlers {
			if run.Handler == h {
				runs = append(runs, run)
			}
		}
	}
	return &service.ListRemoteRunsResponse{Runs: runs}, nil
}

func (s *remoteService) CompleteRemoteRun(_ context.Context, req *service.CompleteRemoteRunRequest) (*service.CompleteRemoteRunResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed = append(s.completed, req)
	return &service.CompleteRemoteRunResponse{JobID: req.Run.JobID}, nil
}

func (s *remoteService) results() []*service.CompleteRemoteRunRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*service.CompleteRemoteRunRequest(nil), s.completed...)
}

const renewHandler = RemoteHandlerPrefix + "renew-cert/node-a"

func openRemoteRun(jobID uint64, deadline time.Time) *pkgscheduler.RemoteRun {
	firing := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	return &pkgscheduler.RemoteRun{
		RemoteRunRef: pkgscheduler.RemoteRunRef{JobID: jobID, Firing: firing, StartedAt: firing.Add(time.Second)},
		Name:         "renew-cert",
		Handler:      renewHandler,
		Payload:      []byte(`{"node":"node-a"}`),
		Deadline:     deadline,
	}
}

func remoteWorker(t *testing.T, svc *remoteService, fn RemoteHandlerFunc) *Worker {
	t.Helper()
	client := NewRemoteRunClient(&dispatchTransport{backends: Backends{Jobs: svc}})
	w, err := NewWorker(client, map[string]RemoteHandlerFunc{renewHandler: fn}, time.Millisecond)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return w
}

// TestWorker_RunsAnOpenRunOnceAndReportsItsResult is go-quicbaas#1955: a
// client runs its job's firings, which it could not before — no handler it
// could register would ever be called.
func TestWorker_RunsAnOpenRunOnceAndReportsItsResult(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	svc := &remoteService{open: []*pkgscheduler.RemoteRun{openRemoteRun(7, deadline)}}
	var (
		calls       int
		gotPayload  string
		gotDeadline time.Time
	)
	w := remoteWorker(t, svc, func(ctx context.Context, run *RemoteRun) (*RemoteResult, error) {
		calls++
		gotPayload = string(run.Payload)
		gotDeadline, _ = ctx.Deadline()
		return &RemoteResult{Message: "renewed", Output: []byte("serial 42")}, nil
	})

	for i := 0; i < 3; i++ { // the run stays listed until the scheduler records it
		if err := w.Poll(context.Background()); err != nil {
			t.Fatalf("Poll: %v", err)
		}
		w.Wait()
	}

	if calls != 1 {
		t.Fatalf("the handler ran %d times for one run, want 1", calls)
	}
	if gotPayload != `{"node":"node-a"}` || !gotDeadline.Equal(deadline) {
		t.Fatalf("the handler got payload %q and deadline %s", gotPayload, gotDeadline)
	}
	results := svc.results()
	if len(results) != 1 {
		t.Fatalf("%d results reported, want 1", len(results))
	}
	want := svc.open[0].RemoteRunRef
	got := results[0]
	if got.Run.JobID != want.JobID || !got.Run.Firing.Equal(want.Firing) || !got.Run.StartedAt.Equal(want.StartedAt) {
		t.Fatalf("reported run %+v, want %+v", got.Run, want)
	}
	if got.Message != "renewed" || string(got.Output) != "serial 42" || got.Error != "" {
		t.Fatalf("reported %+v", got)
	}
}

func TestWorker_AFailedOrPanickingHandlerFailsTheRun(t *testing.T) {
	for name, fn := range map[string]RemoteHandlerFunc{
		"error": func(context.Context, *RemoteRun) (*RemoteResult, error) {
			return nil, stderrors.New("acme order refused")
		},
		"panic": func(context.Context, *RemoteRun) (*RemoteResult, error) { panic("boom") },
	} {
		t.Run(name, func(t *testing.T) {
			svc := &remoteService{open: []*pkgscheduler.RemoteRun{openRemoteRun(7, time.Now().Add(time.Minute))}}
			w := remoteWorker(t, svc, fn)
			if err := w.Poll(context.Background()); err != nil {
				t.Fatalf("Poll: %v", err)
			}
			w.Wait()
			results := svc.results()
			if len(results) != 1 || results[0].Error == "" {
				t.Fatalf("reported %+v, want one failed run", results)
			}
		})
	}
}

func TestWorker_RunReturnsWhenItsContextEnds(t *testing.T) {
	svc := &remoteService{}
	w := remoteWorker(t, svc, func(context.Context, *RemoteRun) (*RemoteResult, error) { return nil, nil })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := w.Run(ctx); !stderrors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run returned %v", err)
	}
}

func TestNewWorker_RefusesALocalHandlerName(t *testing.T) {
	client := NewRemoteRunClient(&dispatchTransport{backends: Backends{Jobs: &remoteService{}}})
	if _, err := NewWorker(client, map[string]RemoteHandlerFunc{"renew-cert": func(context.Context, *RemoteRun) (*RemoteResult, error) {
		return nil, nil
	}}, 0); err == nil {
		t.Fatal("a handler without the remote: prefix was accepted")
	}
}

func TestDispatch_RemoteRunsNeedAServiceThatServesThem(t *testing.T) {
	type jobsOnly struct{ service.SchedulerService }
	_, err := Dispatch(context.Background(), Backends{Jobs: jobsOnly{}}, "ListRemoteRuns",
		&ListRemoteRunsRequest{Handlers: []string{renewHandler}})
	if err == nil {
		t.Fatal("a scheduler without remote runs served ListRemoteRuns")
	}
}
