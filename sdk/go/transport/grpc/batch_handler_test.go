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
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newBatchClient returns the real sdk/go batch client over a gRPC transport
// dialled to the fake QuicBaaS server on a bufconn listener.
func newBatchClient(t *testing.T, fail bool) batch.Client {
	t.Helper()
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterQuicBaaSServer(s, &fakeQuicBaaSServer{fail: fail})
	})
	tr := &Transport{
		cfg:  &transport.Config{Timeout: 5 * time.Second},
		conn: srv.dial(t),
	}
	return batch.NewClient(tr)
}

func TestBatchClient_PutOverGRPC(t *testing.T) {
	c := newBatchClient(t, false)
	resp, err := c.Put(context.Background(), []*service.PutRequest{
		{EntityType: "users", Data: []byte(`{"n":1}`)},
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if len(resp.Responses) != 1 || resp.Responses[0].ID != 1 || !resp.Responses[0].Success {
		t.Fatalf("Put responses = %+v", resp.Responses)
	}
	if resp.Responses[0].EntityType != "users" || resp.Timestamp != 3 {
		t.Fatalf("Put response = %+v", resp)
	}
}

func TestBatchClient_GetOverGRPC(t *testing.T) {
	c := newBatchClient(t, false)
	resp, err := c.Get(context.Background(), []*service.GetRequest{
		{EntityType: "users", ID: 1},
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(resp.Responses) != 1 || resp.Responses[0].ID != 1 || !resp.Responses[0].Found {
		t.Fatalf("Get responses = %+v", resp.Responses)
	}
}

func TestBatchClient_DeleteOverGRPC(t *testing.T) {
	c := newBatchClient(t, false)
	resp, err := c.Delete(context.Background(), []*service.DeleteRequest{
		{EntityType: "users", ID: 1},
	})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(resp.Responses) != 1 || !resp.Responses[0].Deleted || resp.Timestamp != 3 {
		t.Fatalf("Delete response = %+v", resp)
	}
}

func TestBatchClient_ServerErrorOverGRPC(t *testing.T) {
	c := newBatchClient(t, true)
	if _, err := c.Put(context.Background(), []*service.PutRequest{{EntityType: "users"}}); err == nil {
		t.Error("Put: expected the server error")
	}
	if _, err := c.Get(context.Background(), []*service.GetRequest{{EntityType: "users", ID: 1}}); err == nil {
		t.Error("Get: expected the server error")
	}
	if _, err := c.Delete(context.Background(), []*service.DeleteRequest{{EntityType: "users", ID: 1}}); err == nil {
		t.Error("Delete: expected the server error")
	}
}

func TestKVHandler_BatchServiceRejectsWrongRequestType(t *testing.T) {
	h := newKVHandler(newQuicBaaSConn(t, false))
	for _, method := range []string{"Put", "Get", "Delete"} {
		_, err := h.handleRequest(context.Background(), batchServicePrefix, method, &service.PutRequest{})
		if !errors.IsInvalidInput(err) {
			t.Errorf("%s: err = %v, want invalid input", method, err)
		}
	}
}

func TestKVHandler_BatchServiceRejectsUnknownMethod(t *testing.T) {
	h := newKVHandler(newQuicBaaSConn(t, false))
	if _, err := h.handleRequest(context.Background(), batchServicePrefix, "Frobnicate", nil); err == nil {
		t.Fatal("expected an error for an unknown batch method")
	}
}
