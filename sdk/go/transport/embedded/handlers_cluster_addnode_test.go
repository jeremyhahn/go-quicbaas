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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
)

// TestAddNode_SDKRequestReachesTheService proves Cluster().AddNode works over
// the embedded transport: the SDK's request, identity included, arrives at
// the service as its own request.
func TestAddNode_SDKRequestReachesTheService(t *testing.T) {
	var got *service.AddNodeRequest
	tr, err := New(&mockQuicBaaS{addNodeFunc: func(_ context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
		got = req
		return &service.AddNodeResponse{Success: true}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	client := cluster.NewClient(tr)
	err = client.AddNode(context.Background(), &cluster.AddNodeRequest{ShardID: 1, NodeID: 4, Address: "node-4.example.com:63001", Identity: "spki:0123abcd"})
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if got == nil || got.ShardID != 1 || got.NodeID != 4 || got.Address != "node-4.example.com:63001" || got.Identity != "spki:0123abcd" {
		t.Fatalf("service received %+v", got)
	}
}

func TestAddNode_WrongRequestTypeIsRefused(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Request(context.Background(), "quicbaas.Cluster", "AddNode", "node-4"); err == nil {
		t.Fatal("a string request must be refused")
	}
}

func TestAddNode_UnsuccessfulAddIsAnError(t *testing.T) {
	tr, err := New(&mockQuicBaaS{addNodeFunc: func(context.Context, *service.AddNodeRequest) (*service.AddNodeResponse, error) {
		return &service.AddNodeResponse{Success: false, Message: "replica already a member"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = cluster.NewClient(tr).AddNode(context.Background(), &cluster.AddNodeRequest{ShardID: 1, NodeID: 4, Address: "a:1"})
	if err == nil {
		t.Fatal("an unsuccessful add must not read as success")
	}
}
