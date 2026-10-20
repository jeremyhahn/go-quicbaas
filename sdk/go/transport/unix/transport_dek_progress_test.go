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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// dekProgressServer answers the DEK progress verbs the way the daemon's
// DEKServer does: estimated_completion in Unix seconds, and the progress
// status as the re-encryption response's top-level status.
type dekProgressServer struct {
	pb.UnimplementedDEKManagementServer
}

func (dekProgressServer) GetDEKStatus(_ context.Context, req *pb.GetDEKStatusRequest) (*pb.GetDEKStatusResponse, error) {
	return &pb.GetDEKStatusResponse{
		Dek: &pb.DEKMetadata{Path: req.Path, Version: 3},
		ReencryptionProgress: &pb.ReencryptionProgress{
			InProgress: true, TotalKeys: 4, ProcessedKeys: 2, EstimatedCompletion: 1700003600,
		},
	}, nil
}

func (dekProgressServer) GetReencryptionProgress(_ context.Context, req *pb.GetReencryptionProgressRequest) (*pb.GetReencryptionProgressResponse, error) {
	return &pb.GetReencryptionProgressResponse{
		OperationId: req.OperationId,
		Path:        req.Path,
		Status:      "running",
		Progress: &pb.ReencryptionProgress{
			InProgress: true, TotalKeys: 4, ProcessedKeys: 2, EstimatedCompletion: 1700003600,
		},
	}, nil
}

// TestTransport_DEKProgressOverUnixSocket drives the DEK client over a real
// Unix socket and checks the progress Status and EstimatedCompletion arrive.
func TestTransport_DEKProgressOverUnixSocket(t *testing.T) {
	serverTLS, clientTLS := unixTestTLS(t)
	socket := filepath.Join(t.TempDir(), "quicbaas.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	pb.RegisterDEKManagementServer(server, dekProgressServer{})
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
	client := dekclient.NewClient(tr)
	ctx := context.Background()
	eta := time.Unix(1700003600, 0)

	progress, err := client.GetReencryptionProgress(ctx, &dekclient.GetReencryptionProgressRequest{Path: "app/key", OperationID: "op-1"})
	if err != nil {
		t.Fatalf("GetReencryptionProgress: %v", err)
	}
	if progress.Progress == nil {
		t.Fatal("progress is nil")
	}
	if progress.Progress.Status != "running" {
		t.Errorf("Status = %q, want running", progress.Progress.Status)
	}
	if !progress.Progress.EstimatedCompletion.Equal(eta) {
		t.Errorf("EstimatedCompletion = %v, want %v", progress.Progress.EstimatedCompletion, eta)
	}

	status, err := client.GetDEKStatus(ctx, "app/key")
	if err != nil {
		t.Fatalf("GetDEKStatus: %v", err)
	}
	if status.ReencryptionProgress == nil {
		t.Fatal("status progress is nil")
	}
	if !status.ReencryptionProgress.EstimatedCompletion.Equal(eta) {
		t.Errorf("status EstimatedCompletion = %v, want %v", status.ReencryptionProgress.EstimatedCompletion, eta)
	}
}
