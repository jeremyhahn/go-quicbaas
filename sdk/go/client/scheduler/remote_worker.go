package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// RemoteResult is a remote handler's successful result.
type RemoteResult struct {
	Message string
	Output  []byte
}

// RemoteHandlerFunc runs one remote run. Its context ends at the run's
// deadline. A non-nil error fails the run.
type RemoteHandlerFunc func(ctx context.Context, run *RemoteRun) (*RemoteResult, error)

// DefaultRemotePollInterval is how often a Worker lists its open runs.
const DefaultRemotePollInterval = 5 * time.Second

// Worker runs the firings of a client's remote handlers: each poll lists the
// handlers' open runs, runs every run it has not started, and reports each
// result. Give each worker its own handler names (one per node, say): two
// workers polling one name may both run a firing, and only one result counts.
type Worker struct {
	client   RemoteRunClient
	handlers map[string]RemoteHandlerFunc
	names    []string
	interval time.Duration

	// OnReportError, when set, is told of a result the scheduler refused or
	// could not be reached for.
	OnReportError func(run *RemoteRun, err error)

	mu      sync.Mutex
	started map[string]struct{}
	wg      sync.WaitGroup
}

// NewWorker returns a worker for the named remote handlers. Every name must
// carry RemoteHandlerPrefix. A zero interval is DefaultRemotePollInterval.
func NewWorker(c RemoteRunClient, handlers map[string]RemoteHandlerFunc, interval time.Duration) (*Worker, error) {
	if c == nil || len(handlers) == 0 {
		return nil, errors.NewInvalidInput("scheduler.NewWorker", "a client and at least one handler are required")
	}
	w := &Worker{client: c, handlers: handlers, interval: interval, started: map[string]struct{}{}}
	for name, fn := range handlers {
		if !strings.HasPrefix(name, RemoteHandlerPrefix) || len(name) == len(RemoteHandlerPrefix) || fn == nil {
			return nil, errors.NewInvalidInput("scheduler.NewWorker",
				fmt.Sprintf("handler %q needs the %s prefix and a function", name, RemoteHandlerPrefix))
		}
		w.names = append(w.names, name)
	}
	if w.interval <= 0 {
		w.interval = DefaultRemotePollInterval
	}
	return w, nil
}

func runKey(r RemoteRunRef) string {
	// Firing is left out: a schedule update moves it while the run still runs.
	return strconv.FormatUint(r.JobID, 10) + "/" + strconv.FormatInt(r.StartedAt.UnixNano(), 10)
}

// Poll lists the open runs once and starts each run not yet started. It does
// not wait for them; Wait does.
func (w *Worker) Poll(ctx context.Context) error {
	runs, err := w.client.ListRemoteRuns(ctx, w.names)
	if err != nil {
		return err
	}
	listed := make(map[string]struct{}, len(runs))
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, run := range runs {
		key := runKey(run.RemoteRunRef)
		listed[key] = struct{}{}
		if _, ok := w.started[key]; ok {
			continue
		}
		fn, ok := w.handlers[run.Handler]
		if !ok {
			continue
		}
		w.started[key] = struct{}{}
		w.wg.Add(1)
		go w.execute(ctx, fn, run)
	}
	// A run no longer listed is concluded; forget it. One still listed after
	// its result stays started, so it is never run twice.
	for key := range w.started {
		if _, ok := listed[key]; !ok {
			delete(w.started, key)
		}
	}
	return nil
}

func (w *Worker) execute(ctx context.Context, fn RemoteHandlerFunc, run *RemoteRun) {
	defer w.wg.Done()
	runCtx, cancel := ctx, context.CancelFunc(func() {})
	if !run.Deadline.IsZero() {
		runCtx, cancel = context.WithDeadline(ctx, run.Deadline)
	}
	result, err := callRemoteHandler(runCtx, fn, run)
	cancel()

	report := &CompleteRemoteRunRequest{Run: run.RemoteRunRef}
	switch {
	case err != nil:
		report.Error = err.Error()
	case result != nil:
		report.Message, report.Output = result.Message, result.Output
	}
	if err := w.client.CompleteRemoteRun(ctx, report); err != nil && w.OnReportError != nil {
		w.OnReportError(run, err)
	}
}

func callRemoteHandler(ctx context.Context, fn RemoteHandlerFunc, run *RemoteRun) (result *RemoteResult, err error) {
	defer func() {
		if p := recover(); p != nil {
			result, err = nil, fmt.Errorf("remote handler %s panicked: %v", run.Handler, p)
		}
	}()
	return fn(ctx, run)
}

// Run polls every interval until ctx ends, then waits for the runs it
// started and returns ctx's error. A failed poll is retried on the next tick.
func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		_ = w.Poll(ctx)
		select {
		case <-ctx.Done():
			w.Wait()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Wait blocks until every run the worker started has reported.
func (w *Worker) Wait() { w.wg.Wait() }
