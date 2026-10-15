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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	messagingclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// dedupStatsServer answers GetDedupStats for the orders channel only, and
// refuses any other channel as the real server does.
type dedupStatsServer struct {
	pb.UnimplementedMessagingServiceServer
}

func (dedupStatsServer) GetDedupStats(_ context.Context, req *pb.MessagingGetDedupStatsRequest) (*pb.MessagingGetDedupStatsResponse, error) {
	if req.GetChannel() != "orders" {
		return nil, status.Error(codes.NotFound, "channel not found")
	}
	return &pb.MessagingGetDedupStatsResponse{Stats: &pb.DedupStats{Channel: req.GetChannel(), TotalMessages: 10, TotalDuplicates: 4}}, nil
}

// TestTransport_MessagingGetDedupStats pins that the SDK client's
// GetDedupStats carries its channel over the Unix socket and returns that
// channel's figures, and that a missing or unknown channel is refused with a
// typed error.
func TestTransport_MessagingGetDedupStats(t *testing.T) {
	serverTLS, clientTLS := unixTestTLS(t)
	socket := filepath.Join(t.TempDir(), "quicbaas.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)))
	pb.RegisterMessagingServiceServer(server, dedupStatsServer{})
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

	client := messagingclient.NewClient(tr)
	ctx := context.Background()

	got, err := client.GetDedupStats(ctx, "orders")
	if err != nil {
		t.Fatalf("GetDedupStats: %v", err)
	}
	// The server reports 10 unique messages and 4 duplicates.
	want := messagingclient.DedupStats{TotalChecks: 14, DuplicatesDetected: 4, UniqueMessages: 10}
	if *got != want {
		t.Errorf("dedup stats = %+v, want %+v", *got, want)
	}

	if _, err := client.GetDedupStats(ctx, ""); !errors.IsInvalidInput(err) {
		t.Errorf("GetDedupStats without a channel: err = %v, want an InvalidInput error", err)
	}
	if _, err := client.GetDedupStats(ctx, "missing"); !errors.IsNotFound(err) {
		t.Errorf("GetDedupStats for an unknown channel: err = %v, want a NotFound error", err)
	}
}
