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

package unix

import (
	"context"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	batchclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// batchExecuteServer records each BatchExecute call. It refuses a batch
// whose first key is "refuse" the way service.BatchExecute refuses a bad
// request, with InvalidArgument.
type batchExecuteServer struct {
	pb.UnimplementedQuicBaaSServer

	mu    sync.Mutex
	calls [][]*pb.BatchExecuteOperation
}

func (s *batchExecuteServer) BatchExecute(_ context.Context, req *pb.BatchExecuteRequest) (*pb.BatchExecuteResponse, error) {
	s.mu.Lock()
	s.calls = append(s.calls, req.GetOperations())
	s.mu.Unlock()
	if len(req.GetOperations()) > 0 && req.GetOperations()[0].GetKey() == "refuse" {
		return nil, status.Error(codes.InvalidArgument, "type must be PUT or DELETE for all operations")
	}
	results := make([]*pb.BatchExecuteResult, len(req.GetOperations()))
	for i := range results {
		results[i] = &pb.BatchExecuteResult{Succeeded: true}
	}
	return &pb.BatchExecuteResponse{Results: results}, nil
}

// TestTransport_BatchExecute sends a put and delete batch through the SDK
// batch client over the Unix transport: it reaches the BatchExecute RPC as
// one call, and a request the server refuses comes back as InvalidInput.
func TestTransport_BatchExecute(t *testing.T) {
	serverTLS, clientTLS := unixTestTLS(t)
	socket := filepath.Join(t.TempDir(), "quicbaas.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	fake := &batchExecuteServer{}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	pb.RegisterQuicBaaSServer(server, fake)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	defer func() {
		server.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	}()

	tr, err := New(&transport.Config{Address: socket, Timeout: 5 * time.Second, TLSConfig: clientTLS})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() {
		if err := tr.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()

	client := batchclient.NewClient(tr)
	ctx := context.Background()

	resp, err := client.Execute(ctx, &batchclient.BatchRequest{
		Operations: []batchclient.BatchOperation{
			{Type: batchclient.OpPut, EntityType: "users", Key: "u1", Value: []byte("v1")},
			{Type: batchclient.OpDelete, EntityType: "users", Key: "u2"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(resp.Results) != 2 || !resp.Results[0].Success || !resp.Results[1].Success {
		t.Fatalf("Execute response = %+v, want two succeeded results", resp)
	}

	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("BatchExecute calls = %d, want one atomic call", len(calls))
	}
	ops := calls[0]
	if len(ops) != 2 ||
		ops[0].GetType() != pb.BatchExecuteOpType_BATCH_EXECUTE_OP_TYPE_PUT || ops[0].GetKey() != "u1" || string(ops[0].GetValue()) != "v1" ||
		ops[1].GetType() != pb.BatchExecuteOpType_BATCH_EXECUTE_OP_TYPE_DELETE || ops[1].GetKey() != "u2" {
		t.Fatalf("BatchExecute operations = %v, want the put of u1 then the delete of u2", ops)
	}

	_, err = client.Execute(ctx, &batchclient.BatchRequest{
		Operations: []batchclient.BatchOperation{
			{Type: batchclient.OpPut, EntityType: "users", Key: "refuse", Value: []byte("v")},
		},
	})
	if !errors.IsInvalidInput(err) {
		t.Errorf("Execute of a refused batch: err = %v, want an InvalidInput error", err)
	}
}
