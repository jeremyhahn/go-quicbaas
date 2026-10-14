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
	"testing"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// TestSDLCGRPC_ARebindKeepsTheTrainAndClustering binds, reads back and binds
// what was read, and the server must still hold both policies exactly.
func TestSDLCGRPC_ARebindKeepsTheTrainAndClustering(t *testing.T) {
	fake := &bindingStoreServer{}
	conn := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, fake) }).dial(t)
	ctx := context.Background()
	want := sdlc.ProjectBinding{
		ProjectID:  "p1",
		MergeTrain: sdlc.MergeTrainPolicy{Disabled: true, MaxSize: 7},
		Clustering: sdlc.ClusteringPolicy{Disabled: true, MaxSize: 3},
	}
	if _, err := handleSDLCBind(ctx, conn, &sdlc.BindRequest{TenantID: "t", ProjectID: "p1", Binding: want}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	read, err := handleSDLCBinding(ctx, conn, &sdlc.GetBindingRequest{TenantID: "t", ProjectID: "p1"})
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	rebound, err := handleSDLCBind(ctx, conn, &sdlc.BindRequest{TenantID: "t", ProjectID: "p1", Binding: *read.(*sdlc.ProjectBinding)})
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	got := rebound.(*sdlc.ProjectBinding)
	if got.MergeTrain != want.MergeTrain || got.Clustering != want.Clustering {
		t.Fatalf("after a rebind: %+v %+v, want %+v %+v", got.MergeTrain, got.Clustering, want.MergeTrain, want.Clustering)
	}
	fake.mu.Lock()
	held := fake.held.GetMergeTrain()
	fake.mu.Unlock()
	if held == nil || !held.GetDisabled() || held.GetMaxSize() != 7 {
		t.Fatalf("the server holds train %+v after the rebind", held)
	}
}

// TestSDLCGRPC_BindCarriesPaused: BindRequest.Paused reaches the wire as
// BindProjectRequest.paused, and an unpaused bind sends false.
func TestSDLCGRPC_BindCarriesPaused(t *testing.T) {
	fake := &bindingStoreServer{}
	conn := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, fake) }).dial(t)
	ctx := context.Background()
	for _, paused := range []bool{true, false} {
		req := &sdlc.BindRequest{TenantID: "t", ProjectID: "p1", Binding: sdlc.ProjectBinding{ProjectID: "p1"}, Paused: paused}
		if _, err := handleSDLCBind(ctx, conn, req); err != nil {
			t.Fatalf("Bind: %v", err)
		}
		fake.mu.Lock()
		got := fake.lastBind.GetPaused()
		fake.mu.Unlock()
		if got != paused {
			t.Fatalf("server saw paused=%v, want %v", got, paused)
		}
	}
}
