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
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/workerimage"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// workerImageLoop is a refresh loop that answers from fields.
type workerImageLoop struct {
	status workerimage.Status
	err    error
}

func (l *workerImageLoop) RequestRefresh() error      { return l.err }
func (l *workerImageLoop) Status() workerimage.Status { return l.status }

// workerImageProvider serves one SDLC service with the loop attached.
type workerImageProvider struct{ svc *service.SDLCService }

func (p workerImageProvider) SDLC() *service.SDLCService { return p.svc }

func newWorkerImageServer(t *testing.T, loop *workerImageLoop) *SDLCServer {
	t.Helper()
	svc := &service.SDLCService{}
	if loop != nil {
		svc.SetWorkerImageRefresher(loop)
	}
	return NewSDLCServer(workerImageProvider{svc: svc}, sdlcTestLogger(t))
}

func TestSDLCServer_GetWorkerImageStatusCarriesTheWholeReport(t *testing.T) {
	finished := time.Unix(1_800_000_000, 0)
	srv := newWorkerImageServer(t, &workerImageLoop{status: workerimage.Status{
		Serving: true,
		Last: &workerimage.Report{
			Trigger: workerimage.TriggerSchedule, Outcome: workerimage.OutcomePartial, FinishedAt: finished,
			Candidate: "w:1-cc2-cx3", Reference: "reg/o/w:1-cc2-cx3@sha256:abc", Digest: "sha256:abc",
			Probes:     []workerimage.CheckResult{{Name: "toolchain", Passed: true}},
			Smokes:     []workerimage.CheckResult{{Name: "claude/claude-opus-5-5", Passed: true}},
			Promotions: []workerimage.Promotion{{TenantID: "acme", AgentID: "claude-coder", From: []string{"w:0"}, To: "reg/o/w", Error: "sealed"}},
			Skipped:    []string{"t2: sealed"},
			Bound:      []workerimage.BoundImage{{Image: "w:0", Uses: 2, Stale: true}},
		},
	}})

	resp, err := srv.GetWorkerImageStatus(context.Background(), &pb.GetWorkerImageStatusRequest{})
	if err != nil {
		t.Fatalf("GetWorkerImageStatus: %v", err)
	}
	st := resp.GetStatus()
	last := st.GetLast()
	if !st.GetServing() || last.GetOutcome() != "partial" || last.GetTrigger() != "schedule" ||
		last.GetFinishedAtUnix() != finished.Unix() || last.GetDigest() != "sha256:abc" {
		t.Fatalf("status = %+v", st)
	}
	if len(last.GetProbes()) != 1 || len(last.GetSmokes()) != 1 || last.GetPromotions()[0].GetError() != "sealed" ||
		last.GetSkipped()[0] != "t2: sealed" || last.GetBound()[0].GetUses() != 2 || !last.GetBound()[0].GetStale() || last.GetStartedAtUnix() != 0 {
		t.Fatalf("report = %+v", last)
	}
}

func TestSDLCServer_RefreshWorkerImageMapsRefusals(t *testing.T) {
	cases := map[string]struct {
		loop *workerImageLoop
		want codes.Code
	}{
		"queued":  {loop: &workerImageLoop{status: workerimage.Status{Serving: true, Queued: true}}, want: codes.OK},
		"busy":    {loop: &workerImageLoop{err: &workerimage.RefreshInProgressError{}}, want: codes.Aborted},
		"no loop": {loop: nil, want: codes.Unavailable},
		"broken":  {loop: &workerImageLoop{err: errors.New("unexpected")}, want: codes.Internal},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newWorkerImageServer(t, tc.loop).RefreshWorkerImage(context.Background(), &pb.RefreshWorkerImageRequest{})
			if status.Code(err) != tc.want {
				t.Fatalf("code = %v (%v), want %v", status.Code(err), err, tc.want)
			}
		})
	}
}

func TestSDLCServer_WorkerImageWithoutTheModule(t *testing.T) {
	srv := NewSDLCServer(nilSDLCProvider{}, sdlcTestLogger(t))
	if _, err := srv.GetWorkerImageStatus(context.Background(), &pb.GetWorkerImageStatusRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("status without the module: %v", err)
	}
	if _, err := srv.RefreshWorkerImage(context.Background(), &pb.RefreshWorkerImageRequest{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("refresh without the module: %v", err)
	}
}
