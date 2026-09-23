// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestOrchestratorHandler_JobsCarryTaskSource pins the forge issue through
// this transport's decoders. The server fills the source on every job row and
// on the detail read; a converter that stops at the fields it already knew
// hands the caller a job that cannot name its issue, which is exactly the
// gap a paged jobs view has no second request to close (go-quicbaas#976).
func TestOrchestratorHandler_JobsCarryTaskSource(t *testing.T) {
	h := newOrchestratorHandlerWithServer(t, false)
	ctx := context.Background()

	listed, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "ListJobs", &orchestrator.ListJobsRequest{})
	if err != nil {
		t.Fatalf("ListJobs: unexpected error: %v", err)
	}
	listResp, ok := listed.(*orchestrator.ListJobsResponse)
	if !ok {
		t.Fatalf("ListJobs answered %T, want *orchestrator.ListJobsResponse", listed)
	}
	if len(listResp.Jobs) != 1 {
		t.Fatalf("ListJobs returned %d jobs, want 1", len(listResp.Jobs))
	}
	assertFakeJobTaskSource(t, "ListJobs", listResp.Jobs[0].Source)

	read, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "GetJobStatus",
		&orchestrator.GetJobStatusRequest{JobID: "j1"})
	if err != nil {
		t.Fatalf("GetJobStatus: unexpected error: %v", err)
	}
	statusResp, ok := read.(*orchestrator.JobStatus)
	if !ok {
		t.Fatalf("GetJobStatus answered %T, want *orchestrator.JobStatus", read)
	}
	assertFakeJobTaskSource(t, "GetJobStatus", statusResp.Source)
}

// TestOrchestratorHandler_JobWithoutTaskSourceDecodesNone is the other half:
// a server that reports no origin must decode to no source, so a consumer can
// tell "this job has no issue" from "this issue has an empty ref".
func TestOrchestratorHandler_JobWithoutTaskSourceDecodesNone(t *testing.T) {
	if got := taskSourceFromProto(nil); got != nil {
		t.Fatalf("an absent source decoded to %+v, want nil", got)
	}
}

// assertFakeJobTaskSource checks one decoded source against what the fake
// server reported.
func assertFakeJobTaskSource(t *testing.T, method string, got *orchestrator.TaskSource) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s dropped the forge issue behind the job's task", method)
	}
	if got.Kind != fakeJobTaskSource.GetKind() {
		t.Errorf("%s source kind = %q, want %q", method, got.Kind, fakeJobTaskSource.GetKind())
	}
	if got.Ref != fakeJobTaskSource.GetRef() {
		t.Errorf("%s source ref = %q, want %q", method, got.Ref, fakeJobTaskSource.GetRef())
	}
	if got.URL != fakeJobTaskSource.GetUrl() {
		t.Errorf("%s source url = %q, want %q", method, got.URL, fakeJobTaskSource.GetUrl())
	}
}
