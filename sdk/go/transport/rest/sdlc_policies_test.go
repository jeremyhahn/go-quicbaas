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

package rest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// TestSDLCREST_ARebindKeepsTheTrainAndClustering drives bind, read, rebind
// through the SDK's REST transport and proves both policies survive.
func TestSDLCREST_ARebindKeepsTheTrainAndClustering(t *testing.T) {
	var mu sync.Mutex
	var held []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			held = body
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(held)
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	ctx := context.Background()
	want := sdlc.ProjectBinding{
		ProjectID:  "p1",
		MergeTrain: sdlc.MergeTrainPolicy{Disabled: true, MaxSize: 7},
		Clustering: sdlc.ClusteringPolicy{MaxSize: 3},
	}
	if _, err := tr.Request(ctx, "quicbaas.SDLC", "Bind", &sdlc.BindRequest{ProjectID: "p1", Binding: want}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	read, err := tr.Request(ctx, "quicbaas.SDLC", "Binding", &sdlc.GetBindingRequest{ProjectID: "p1"})
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	rebound, err := tr.Request(ctx, "quicbaas.SDLC", "Bind", &sdlc.BindRequest{ProjectID: "p1", Binding: *read.(*sdlc.ProjectBinding)})
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	got := rebound.(*sdlc.ProjectBinding)
	if got.MergeTrain != want.MergeTrain || got.Clustering != want.Clustering {
		t.Fatalf("after a rebind: %+v %+v, want %+v %+v", got.MergeTrain, got.Clustering, want.MergeTrain, want.Clustering)
	}
}
