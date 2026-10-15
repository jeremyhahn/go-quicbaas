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
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// backendModelsRecorder answers ListBackendModels the way the daemon does:
// models for "ollama", NotFound for "missing", InvalidArgument for "bad/name".
type backendModelsRecorder struct {
	pb.UnimplementedOrchestratorServiceServer
	got chan *pb.ListBackendModelsRequest
}

func (s *backendModelsRecorder) ListBackendModels(_ context.Context, req *pb.ListBackendModelsRequest) (*pb.ListBackendModelsResponse, error) {
	s.got <- req
	switch req.GetBackendName() {
	case "missing":
		return nil, status.Error(codes.NotFound, "acp: backend not found: missing")
	case "bad/name":
		return nil, status.Error(codes.InvalidArgument, "backend name is malformed")
	}
	return &pb.ListBackendModelsResponse{Backend: req.GetBackendName(), Models: []string{"llama3", "qwen2"}, Count: 2}, nil
}

// TestTransport_ListBackendModelsOverUnixSocket drives the SDK client over a
// real Unix socket: the request fields reach the server, every response field
// comes back, and the server's NotFound and InvalidArgument arrive coded.
func TestTransport_ListBackendModelsOverUnixSocket(t *testing.T) {
	serverTLS, clientTLS := unixTestTLS(t)
	socket := filepath.Join(t.TempDir(), "quicbaas.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	recorder := &backendModelsRecorder{got: make(chan *pb.ListBackendModelsRequest, 1)}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	pb.RegisterOrchestratorServiceServer(server, recorder)
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
	client := orchestratorclient.NewClient(tr)
	ctx := context.Background()

	resp, err := client.ListBackendModels(ctx, &orchestratorclient.ListBackendModelsRequest{
		NodeID: "node-7", BackendName: "ollama", TenantID: "t1",
	})
	if err != nil {
		t.Fatalf("ListBackendModels: %v", err)
	}
	want := &orchestratorclient.ListBackendModelsResponse{Backend: "ollama", Models: []string{"llama3", "qwen2"}, Count: 2}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
	req := <-recorder.got
	if req.GetNodeId() != "node-7" || req.GetBackendName() != "ollama" || req.GetTenantId() != "t1" {
		t.Errorf("server saw %+v, want node-7/ollama/t1", req)
	}

	_, err = client.ListBackendModels(ctx, &orchestratorclient.ListBackendModelsRequest{BackendName: "missing"})
	<-recorder.got
	if !errors.IsNotFound(err) {
		t.Errorf("unknown backend: got %v, want not found", err)
	}

	_, err = client.ListBackendModels(ctx, &orchestratorclient.ListBackendModelsRequest{BackendName: "bad/name"})
	<-recorder.got
	if !errors.IsInvalidInput(err) {
		t.Errorf("malformed backend: got %v, want invalid input", err)
	}
}
