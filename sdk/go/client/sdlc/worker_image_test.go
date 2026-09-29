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

package sdlc

import (
	"context"
	"errors"
	"testing"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestClient_WorkerImageVerbsRouteUnderTheService(t *testing.T) {
	ctx := context.Background()
	tr := &fakeTransport{capable: true, resp: &WorkerImageStatus{Serving: true, Queued: true}}
	c := NewClient(tr)

	status, err := c.WorkerImageStatus(ctx)
	if err != nil || !status.Serving || tr.service != serviceName || tr.method != "WorkerImageStatus" {
		t.Fatalf("WorkerImageStatus: %+v %v (%s/%s)", status, err, tr.service, tr.method)
	}
	if _, ok := tr.req.(*WorkerImageStatusRequest); !ok {
		t.Fatalf("WorkerImageStatus sent %T", tr.req)
	}

	status, err = c.RefreshWorkerImage(ctx)
	if err != nil || !status.Queued || tr.method != "RefreshWorkerImage" {
		t.Fatalf("RefreshWorkerImage: %+v %v (%s)", status, err, tr.method)
	}
	if _, ok := tr.req.(*RefreshWorkerImageRequest); !ok {
		t.Fatalf("RefreshWorkerImage sent %T", tr.req)
	}
}

func TestClient_WorkerImageVerbsRefuse(t *testing.T) {
	var nilCtx context.Context
	c := NewClient(&fakeTransport{capable: true, resp: &WorkerImageStatus{}})
	if _, err := c.WorkerImageStatus(nilCtx); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("nil ctx: %v", err)
	}

	wrong := NewClient(&fakeTransport{capable: true, resp: "not a status"})
	if _, err := wrong.RefreshWorkerImage(context.Background()); err == nil {
		t.Fatal("wrong response type accepted")
	}

	failing := NewClient(&fakeTransport{capable: true, err: errors.New("boom")})
	if _, err := failing.WorkerImageStatus(context.Background()); err == nil {
		t.Fatal("transport error swallowed")
	}

	var unsupported *sdkerrors.SDLCTransportUnsupportedError
	incapable := NewClient(&fakeTransport{capable: false})
	if _, err := incapable.RefreshWorkerImage(context.Background()); !errors.As(err, &unsupported) {
		t.Fatalf("refresh on an incapable transport: %v", err)
	}
}
