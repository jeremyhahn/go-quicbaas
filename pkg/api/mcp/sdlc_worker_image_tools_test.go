//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/workerimage"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// workerImageLoop is a refresh loop that answers from fields.
type workerImageLoop struct {
	status workerimage.Status
	err    error
}

func (l *workerImageLoop) RequestRefresh() error      { return l.err }
func (l *workerImageLoop) Status() workerimage.Status { return l.status }

func workerImageRegistry(t *testing.T, svc *service.SDLCService) *ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	mock := testutil.NewMockQuicBaaS()
	mock.SetSDLCService(svc)
	return NewToolRegistry(mock, log)
}

func TestSDLCWorkerImageTools_AnswerFromTheLoop(t *testing.T) {
	svc := &service.SDLCService{}
	svc.SetWorkerImageRefresher(&workerImageLoop{status: workerimage.Status{Serving: true, Queued: true}})
	registry := workerImageRegistry(t, svc)
	ctx := context.Background()

	got, err := registry.executeSDLCTool(ctx, "quicbaas_sdlc_worker_image", nil)
	if err != nil {
		t.Fatalf("quicbaas_sdlc_worker_image: %v", err)
	}
	if status, ok := got.(workerimage.Status); !ok || !status.Serving {
		t.Fatalf("quicbaas_sdlc_worker_image = %#v", got)
	}
	got, err = registry.executeSDLCTool(ctx, "quicbaas_sdlc_worker_image_refresh", nil)
	if err != nil {
		t.Fatalf("quicbaas_sdlc_worker_image_refresh: %v", err)
	}
	if status, ok := got.(workerimage.Status); !ok || !status.Queued {
		t.Fatalf("quicbaas_sdlc_worker_image_refresh = %#v", got)
	}
}

func TestSDLCWorkerImageTools_Refuse(t *testing.T) {
	ctx := context.Background()
	busy := &service.SDLCService{}
	busy.SetWorkerImageRefresher(&workerImageLoop{err: &workerimage.RefreshInProgressError{}})
	if _, err := workerImageRegistry(t, busy).executeSDLCTool(ctx, "quicbaas_sdlc_worker_image_refresh", nil); !errors.Is(err, workerimage.ErrBusy) {
		t.Fatalf("a busy loop's refusal must reach the caller, got %v", err)
	}
	offline := workerImageRegistry(t, nil)
	for _, name := range []string{"quicbaas_sdlc_worker_image", "quicbaas_sdlc_worker_image_refresh"} {
		if _, err := offline.executeSDLCTool(ctx, name, nil); err == nil {
			t.Fatalf("%s answered without the module", name)
		}
	}
}
