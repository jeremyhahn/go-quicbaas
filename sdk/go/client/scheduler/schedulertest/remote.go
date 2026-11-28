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
	"bytes"
	"context"
	"testing"
	"time"

	pkgscheduler "github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// RemoteHandler is the handler of the open run Jobs lists.
const RemoteHandler = scheduler.RemoteHandlerPrefix + "nightly"

// RemoteRun is the open run Jobs lists for RemoteHandler (go-quicbaas#1955).
func RemoteRun() *pkgscheduler.RemoteRun {
	firing := time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC)
	return &pkgscheduler.RemoteRun{
		RemoteRunRef: pkgscheduler.RemoteRunRef{JobID: 42, Firing: firing, StartedAt: firing.Add(time.Second)},
		Name:         "nightly",
		Handler:      RemoteHandler,
		Payload:      []byte(`{"node":"node-a"}`),
		Deadline:     firing.Add(31 * time.Second),
	}
}

// ListRemoteRuns lists RemoteRun to a worker serving RemoteHandler.
func (j *Jobs) ListRemoteRuns(_ context.Context, req *service.ListRemoteRunsRequest) (*service.ListRemoteRunsResponse, error) {
	if err := j.record("ListRemoteRuns", req); err != nil {
		return nil, err
	}
	resp := &service.ListRemoteRunsResponse{Runs: []*pkgscheduler.RemoteRun{}, Timestamp: j.stamp()}
	for _, h := range req.Handlers {
		if h == RemoteHandler {
			resp.Runs = append(resp.Runs, RemoteRun())
		}
	}
	return resp, nil
}

// CompleteRemoteRun records the result.
func (j *Jobs) CompleteRemoteRun(_ context.Context, req *service.CompleteRemoteRunRequest) (*service.CompleteRemoteRunResponse, error) {
	if err := j.record("CompleteRemoteRun", req); err != nil {
		return nil, err
	}
	return &service.CompleteRemoteRunResponse{JobID: req.Run.JobID, Timestamp: j.stamp()}, nil
}

// RunRemoteRunConformance drives a worker's two calls through client and
// checks what jobs received and what the client decoded.
func RunRemoteRunConformance(t *testing.T, client scheduler.RemoteRunClient, jobs *Jobs) {
	t.Helper()
	ctx := context.Background()
	want := RemoteRun()

	runs, err := client.ListRemoteRuns(ctx, []string{RemoteHandler})
	if err != nil {
		t.Fatalf("ListRemoteRuns: %v", err)
	}
	if r, _ := jobs.Received("ListRemoteRuns").(*service.ListRemoteRunsRequest); r == nil || len(r.Handlers) != 1 || r.Handlers[0] != RemoteHandler {
		t.Errorf("server received ListRemoteRuns %+v", jobs.Received("ListRemoteRuns"))
	}
	if len(runs) != 1 {
		t.Fatalf("ListRemoteRuns decoded %d runs, want 1", len(runs))
	}
	got := runs[0]
	if got.JobID != want.JobID || !got.Firing.Equal(want.Firing) || !got.StartedAt.Equal(want.StartedAt) ||
		got.Name != want.Name || got.Handler != want.Handler || !bytes.Equal(got.Payload, want.Payload) ||
		!got.Deadline.Equal(want.Deadline) {
		t.Errorf("ListRemoteRuns decoded %+v, want %+v", got, want)
	}

	if err := client.CompleteRemoteRun(ctx, &scheduler.CompleteRemoteRunRequest{
		Run: got.RemoteRunRef, Message: "renewed", Output: []byte("serial 42"),
	}); err != nil {
		t.Fatalf("CompleteRemoteRun: %v", err)
	}
	done, _ := jobs.Received("CompleteRemoteRun").(*service.CompleteRemoteRunRequest)
	if done == nil || done.Run.JobID != want.JobID || !done.Run.Firing.Equal(want.Firing) ||
		!done.Run.StartedAt.Equal(want.StartedAt) || done.Message != "renewed" ||
		string(done.Output) != "serial 42" || done.Error != "" {
		t.Errorf("server received CompleteRemoteRun %+v", done)
	}

	if err := client.CompleteRemoteRun(ctx, &scheduler.CompleteRemoteRunRequest{
		Run: got.RemoteRunRef, Error: "acme order refused",
	}); err != nil {
		t.Fatalf("CompleteRemoteRun (failed run): %v", err)
	}
	if r, _ := jobs.Received("CompleteRemoteRun").(*service.CompleteRemoteRunRequest); r == nil || r.Error != "acme order refused" {
		t.Errorf("server received failed CompleteRemoteRun %+v", r)
	}
}
