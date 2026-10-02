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
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
)

// addNodeRecorder records the AddNode request the server received.
type addNodeRecorder struct {
	pb.UnimplementedQuicBaaSServer
	got *pb.AddNodeRequest
}

func (r *addNodeRecorder) AddNode(_ context.Context, req *pb.AddNodeRequest) (*pb.AddNodeResponse, error) {
	r.got = req
	return &pb.AddNodeResponse{Success: true, ShardId: req.ShardId, NodeId: req.NodeId}, nil
}

// TestClusterHandler_AddNodeSendsShardNodeAndIdentity proves the SDK request
// reaches the server whole: it used to send shard 0 and node 0, which the
// server rejects, and had no identity to send.
func TestClusterHandler_AddNodeSendsShardNodeAndIdentity(t *testing.T) {
	rec := &addNodeRecorder{}
	srv := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterQuicBaaSServer(s, rec) })
	h := newClusterHandler(srv.dial(t))
	req := &cluster.AddNodeRequest{ShardID: 1, NodeID: 4, Address: "node-4.example.com:63001", Identity: "spki:0123abcd"}
	if _, err := h.handleRequest(context.Background(), "quicbaas.Cluster", "AddNode", req); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if rec.got == nil || rec.got.ShardId != 1 || rec.got.NodeId != 4 || rec.got.Address != req.Address || rec.got.Identity != req.Identity {
		t.Fatalf("server received %+v", rec.got)
	}
}
