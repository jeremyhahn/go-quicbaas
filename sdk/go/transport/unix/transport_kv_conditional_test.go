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
	"bytes"
	"context"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kv"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// conditionalPutServer decides the KV conditional writes against its own
// store, the way the daemon's KV service does: a swap needs the expected
// value, a claim needs the entity absent, and a miss is applied=false.
type conditionalPutServer struct {
	pb.UnimplementedQuicBaaSServer
	mu     sync.Mutex
	stored map[uint64][]byte
}

func (s *conditionalPutServer) CompareAndSwap(_ context.Context, req *pb.CompareAndSwapRequest) (*pb.ConditionalPutResponse, error) {
	if strings.TrimSpace(req.GetEntityType()) == "" {
		return nil, status.Error(codes.InvalidArgument, "entity type is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.stored[req.GetId()]
	if !ok || !bytes.Equal(current, req.GetExpect()) {
		return &pb.ConditionalPutResponse{Applied: false}, nil
	}
	s.stored[req.GetId()] = req.GetSet()
	return &pb.ConditionalPutResponse{Applied: true}, nil
}

func (s *conditionalPutServer) PutIfAbsent(_ context.Context, req *pb.PutIfAbsentRequest) (*pb.ConditionalPutResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.stored[req.GetId()]; ok {
		return &pb.ConditionalPutResponse{Applied: false}, nil
	}
	s.stored[req.GetId()] = req.GetData()
	return &pb.ConditionalPutResponse{Applied: true}, nil
}

// newConditionalPutClient serves conditionalPutServer on a Unix socket and
// returns a KV client over it.
func newConditionalPutClient(t *testing.T) kv.Client {
	t.Helper()
	serverTLS, clientTLS := unixTestTLS(t)
	socket := filepath.Join(t.TempDir(), "quicbaas.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	pb.RegisterQuicBaaSServer(server, &conditionalPutServer{stored: map[uint64][]byte{}})
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := <-served; err != nil {
			t.Errorf("serve: %v", err)
		}
	})

	tr, err := New(&transport.Config{Address: socket, Timeout: 5 * time.Second, TLSConfig: clientTLS})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := tr.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return kv.NewClient(tr)
}

func TestTransport_PutIfAbsentOverUnixSocket(t *testing.T) {
	client := newConditionalPutClient(t)
	ctx := context.Background()
	req := &service.PutIfAbsentRequest{EntityType: "locks", ID: 7, Data: []byte("owner")}

	stored, err := client.PutIfAbsent(ctx, req)
	if err != nil {
		t.Fatalf("PutIfAbsent: %v", err)
	}
	if !stored {
		t.Fatal("first claim was not stored")
	}

	stored, err = client.PutIfAbsent(ctx, req)
	if err != nil {
		t.Fatalf("a present entity must be applied=false, not an error: %v", err)
	}
	if stored {
		t.Error("second claim was stored over a present entity")
	}
}

func TestTransport_CompareAndSwapOverUnixSocket(t *testing.T) {
	client := newConditionalPutClient(t)
	ctx := context.Background()
	if _, err := client.PutIfAbsent(ctx, &service.PutIfAbsentRequest{EntityType: "users", ID: 1, Data: []byte("v1")}); err != nil {
		t.Fatalf("PutIfAbsent: %v", err)
	}

	swapped, err := client.CompareAndSwap(ctx, &service.CompareAndSwapRequest{EntityType: "users", ID: 1, Expect: []byte("v1"), Set: []byte("v2")})
	if err != nil {
		t.Fatalf("CompareAndSwap: %v", err)
	}
	if !swapped {
		t.Fatal("swap with the current value did not apply")
	}

	swapped, err = client.CompareAndSwap(ctx, &service.CompareAndSwapRequest{EntityType: "users", ID: 1, Expect: []byte("v1"), Set: []byte("v3")})
	if err != nil {
		t.Fatalf("a stale expect must be applied=false, not an error: %v", err)
	}
	if swapped {
		t.Error("swap with a stale expected value applied")
	}
}

func TestTransport_CompareAndSwapOverUnixSocket_InvalidArgument(t *testing.T) {
	client := newConditionalPutClient(t)
	// The SDK client refuses an empty entity type before any I/O, so the
	// server-side refusal is reached with a blank one the client accepts.
	_, err := client.CompareAndSwap(context.Background(), &service.CompareAndSwapRequest{EntityType: " "})
	if !errors.IsInvalidInput(err) {
		t.Fatalf("err = %v, want an invalid-input error", err)
	}
}
