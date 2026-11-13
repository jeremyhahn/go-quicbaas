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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc/workerimage"
)

func TestSDLCService_WorkerImageVerbs(t *testing.T) {
	rig := newSDLCServiceRig(t)
	ctx := context.Background()

	if _, err := rig.svc.RefreshWorkerImage(ctx); !errors.Is(err, workerimage.ErrNotRunning) {
		t.Fatalf("with no loop attached, err = %v", err)
	}
	if status, err := rig.svc.WorkerImageStatus(ctx); err != nil || status.Serving {
		t.Fatalf("with no loop attached the status is not serving: %+v, %v", status, err)
	}

	loop := &stubRefresher{}
	rig.svc.SetWorkerImageRefresher(loop)
	status, err := rig.svc.RefreshWorkerImage(ctx)
	if err != nil || !status.Queued || loop.requests != 1 {
		t.Fatalf("RefreshWorkerImage = %+v, %v (requests %d)", status, err, loop.requests)
	}
	if status, err := rig.svc.WorkerImageStatus(ctx); err != nil || !status.Serving {
		t.Fatalf("WorkerImageStatus = %+v, %v", status, err)
	}

	loop.err = &workerimage.RefreshInProgressError{}
	if _, err := rig.svc.RefreshWorkerImage(ctx); !errors.Is(err, workerimage.ErrBusy) {
		t.Fatalf("a busy loop's refusal must reach the caller, got %v", err)
	}

	rig.svc.SetWorkerImageRefresher(nil)
	if _, err := rig.svc.RefreshWorkerImage(ctx); !errors.Is(err, workerimage.ErrNotRunning) {
		t.Fatalf("a detached loop must refuse, got %v", err)
	}
}

// stubRefresher is a refresh loop that records requests.
type stubRefresher struct {
	requests int
	err      error
}

func (s *stubRefresher) RequestRefresh() error {
	s.requests++
	return s.err
}

func (s *stubRefresher) Status() workerimage.Status {
	return workerimage.Status{Serving: true, Queued: s.requests > 0 && s.err == nil}
}
