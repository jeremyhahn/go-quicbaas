//go:build rest

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

package server_test

import (
	"context"
	"crypto/tls"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
)

// TestSealedBoot_GRPCServesQuicBaaSHealth pins the sealed-window gRPC surface
// to the allowlist it is gated by: /quicbaas.v1.QuicBaaS/Health is on the
// canonical sealed-window allowlist, so a node booted sealed (a shamir node
// before its officers unseal it) must answer it on its gRPC listeners. The
// sealed gRPC server used to register only the barrier/ceremony mirrors and
// the qrdb.v1 surface, so the allowlisted verb failed with "unknown service
// quicbaas.v1.QuicBaaS" on grpc and unix alike.
func TestSealedBoot_GRPCServesQuicBaaSHealth(t *testing.T) {
	cfg := server.NewSealedBootTestConfig(t)
	// The sealed-window gRPC server always serves TLS, the way the init
	// matrix daemon does; the node issues its own server certificate.
	cfg.Security.TLSEnabled = true

	srv, err := server.NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	socket := cfg.Server.Unix.Socket
	sdkWaitFor(t, startErr, "the sealed-window unix gRPC listener", func() bool {
		_, statErr := os.Stat(socket)
		return statErr == nil
	})
	require.Nil(t, srv.GetQRDBNodeClient(), "the node must still be sealed")

	conn, err := ggrpc.NewClient("unix://"+socket,
		ggrpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion: tls.VersionTLS13,
			// The node's self-issued certificate is not what this test
			// pins; the registered service set is.
			InsecureSkipVerify: true,
		})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := pb.NewQuicBaaSClient(conn).Health(ctx, &pb.HealthRequest{})
	require.NoError(t, err, "a sealed node must serve the allowlisted QuicBaaS Health verb")
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, resp.GetStatus())

	// Off the allowlist stays sealed: the registration adds the service, not
	// its data plane.
	_, err = pb.NewQuicBaaSClient(conn).Get(ctx, &pb.GetRequest{EntityType: "sealed", Id: 1})
	require.Error(t, err, "an off-allowlist QuicBaaS verb must stay refused while sealed")
}
