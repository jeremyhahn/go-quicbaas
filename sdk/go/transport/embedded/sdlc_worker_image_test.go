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

package embedded

import (
	"context"
	"errors"
	"testing"
	"time"

	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc/workerimage"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// workerImageLoop is a refresh loop that answers from fields.
type workerImageLoop struct {
	status workerimage.Status
	err    error
}

func (l *workerImageLoop) RequestRefresh() error      { return l.err }
func (l *workerImageLoop) Status() workerimage.Status { return l.status }

// workerImageFacade serves one SDLC service with a loop attached.
type workerImageFacade struct {
	service.QuicBaaS
	svc *service.SDLCService
}

func (f workerImageFacade) SDLC() *service.SDLCService { return f.svc }

func workerImageTransport(t *testing.T, loop *workerImageLoop) *Transport {
	t.Helper()
	svc := &service.SDLCService{}
	svc.SetWorkerImageRefresher(loop)
	tr, err := New(workerImageFacade{svc: svc})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tr
}

func TestWorkerImageVerbsAreDispatchedEmbedded(t *testing.T) {
	for _, key := range []string{"quicbaas.SDLC/WorkerImageStatus", "quicbaas.SDLC/RefreshWorkerImage"} {
		if _, ok := unaryHandlers[key]; !ok {
			t.Errorf("%s is not in the embedded dispatch table", key)
		}
	}
}

func TestWorkerImageVerbsEmbedded(t *testing.T) {
	finished := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	tr := workerImageTransport(t, &workerImageLoop{status: workerimage.Status{
		Serving: true, Queued: true,
		Last: &workerimage.Report{
			Trigger: workerimage.TriggerManual, Outcome: workerimage.OutcomePromoted, FinishedAt: finished,
			Probes:     []workerimage.CheckResult{{Name: "p", Passed: true}},
			Smokes:     []workerimage.CheckResult{{Name: "claude/claude-opus-5-5", Passed: true}},
			Promotions: []workerimage.Promotion{{TenantID: "acme", AgentID: "claude-coder", From: []string{"w:old"}, To: "w:new"}},
			Bound:      []workerimage.BoundImage{{Image: "w:old", Uses: 1, Stale: true}},
		},
	}})
	for _, method := range []string{"WorkerImageStatus", "RefreshWorkerImage"} {
		resp, err := tr.Request(context.Background(), "quicbaas.SDLC", method, nil)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		status, ok := resp.(*sdlc.WorkerImageStatus)
		if !ok || !status.Serving || status.Last == nil || !status.Last.FinishedAt.Equal(finished) ||
			status.Last.Promotions[0].To != "w:new" || len(status.Last.Smokes) != 1 || len(status.Last.Probes) != 1 ||
			len(status.Last.Bound) != 1 || !status.Last.Bound[0].Stale {
			t.Fatalf("%s decoded %#v", method, resp)
		}
	}
}

func TestWorkerImageVerbsEmbeddedRefuse(t *testing.T) {
	busy := workerImageTransport(t, &workerImageLoop{err: &workerimage.RefreshInProgressError{}})
	if _, err := busy.Request(context.Background(), "quicbaas.SDLC", "RefreshWorkerImage", nil); !errors.Is(err, workerimage.ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	offline, err := New(sdlcNilFacade{})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"WorkerImageStatus", "RefreshWorkerImage"} {
		if _, err := offline.Request(context.Background(), "quicbaas.SDLC", method, nil); !errors.Is(err, pkgsdlc.ErrUnavailable) {
			t.Fatalf("%s without the module: %v", method, err)
		}
	}
}
