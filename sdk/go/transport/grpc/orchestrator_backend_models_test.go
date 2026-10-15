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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// backendModelsServer answers ListBackendModels the way the daemon does:
// models for "ollama", NotFound for "missing", InvalidArgument for "bad/name".
// It records the last request so a test can see every field arrived.
type backendModelsServer struct {
	pb.UnimplementedOrchestratorServiceServer
	got chan *pb.ListBackendModelsRequest
}

func (s backendModelsServer) ListBackendModels(_ context.Context, req *pb.ListBackendModelsRequest) (*pb.ListBackendModelsResponse, error) {
	s.got <- req
	switch req.GetBackendName() {
	case "missing":
		return nil, status.Error(codes.NotFound, "acp: backend not found: missing")
	case "bad/name":
		return nil, status.Error(codes.InvalidArgument, "backend name is malformed")
	case "bare":
		return &pb.ListBackendModelsResponse{Backend: "bare"}, nil
	}
	return &pb.ListBackendModelsResponse{
		Backend: req.GetBackendName(),
		Models:  []string{"llama3", "qwen2"},
		Count:   2,
	}, nil
}

func newBackendModelsClient(t *testing.T) (orchestrator.Client, chan *pb.ListBackendModelsRequest) {
	t.Helper()
	got := make(chan *pb.ListBackendModelsRequest, 1)
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, backendModelsServer{got: got})
	})
	tr := &Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: srv.dial(t)}
	return orchestrator.NewClient(tr), got
}

func TestListBackendModels_RoundTripsOverGRPC(t *testing.T) {
	client, got := newBackendModelsClient(t)

	resp, err := client.ListBackendModels(context.Background(), &orchestrator.ListBackendModelsRequest{
		NodeID: "node-7", BackendName: "ollama", TenantID: "t1",
	})
	require.NoError(t, err)
	assert.Equal(t, &orchestrator.ListBackendModelsResponse{
		Backend: "ollama", Models: []string{"llama3", "qwen2"}, Count: 2,
	}, resp)

	req := <-got
	assert.Equal(t, "node-7", req.GetNodeId())
	assert.Equal(t, "ollama", req.GetBackendName())
	assert.Equal(t, "t1", req.GetTenantId())
}

// A backend with no model lister answers no models; the SDK hands back [],
// as REST renders it, never nil.
func TestListBackendModels_NoModelsDecodesToEmptySlice(t *testing.T) {
	client, got := newBackendModelsClient(t)

	resp, err := client.ListBackendModels(context.Background(), &orchestrator.ListBackendModelsRequest{BackendName: "bare"})
	require.NoError(t, err)
	<-got
	assert.Equal(t, "bare", resp.Backend)
	assert.NotNil(t, resp.Models)
	assert.Empty(t, resp.Models)
	assert.Zero(t, resp.Count)
}

func TestListBackendModels_ErrorsOverGRPC(t *testing.T) {
	client, got := newBackendModelsClient(t)

	_, err := client.ListBackendModels(context.Background(), &orchestrator.ListBackendModelsRequest{BackendName: "missing"})
	<-got
	assert.True(t, errors.IsNotFound(err), "unknown backend: got %v", err)

	_, err = client.ListBackendModels(context.Background(), &orchestrator.ListBackendModelsRequest{BackendName: "bad/name"})
	<-got
	assert.True(t, errors.IsInvalidInput(err), "malformed backend: got %v", err)
}

func TestHandleListBackendModels_WrongRequestType(t *testing.T) {
	_, err := handleListBackendModels(context.Background(), nil, &orchestrator.GetFleetNodeRequest{})
	assert.True(t, errors.IsInvalidInput(err), "got %v", err)
}
