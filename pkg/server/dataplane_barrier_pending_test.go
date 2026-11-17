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

package server

import (
	"context"
	"net"
	"testing"
	"time"

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	qrdbconfig "github.com/jeremyhahn/go-qrdb/pkg/config"
	qrdbdatabase "github.com/jeremyhahn/go-qrdb/pkg/database"
	qrdbserver "github.com/jeremyhahn/go-qrdb/pkg/server"
	"github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbmemory "github.com/jeremyhahn/go-qrdb/pkg/storage/memory"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestNativeGRPC_RestartRefusedUntilBarrierPublished is the restart
// regression for #2512. Seal is disabled and the data dir already holds an
// initialized data-plane barrier from a previous boot. The node's native
// listener starts before startService builds and publishes that barrier; in
// that window a native Get must be refused as sealed, not served because
// "no barrier is published". Once the barrier is published the way
// startService publishes it, and unsealed, the Get is admitted.
func TestNativeGRPC_RestartRefusedUntilBarrierPublished(t *testing.T) {
	cfg := config.Default()
	cfg.Seal.Enabled = false
	cfg.Seal.Strategy = "passphrase"
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()

	creds, err := qrdbsdk.CredentialsFor([]byte("restart-pending-barrier-unit-test-passphrase"))
	if err != nil {
		t.Fatalf("protect the passphrase: %v", err)
	}
	defer creds.Secret.Destroy()

	// Previous boot: initialize the persistent barrier, then shut down.
	_, first, _, firstCloser, err := buildDataPlaneBarrierService(cfg, nil)
	if err != nil || first == nil || firstCloser == nil {
		t.Fatalf("build the first-boot barrier: barrier=%v closer=%v err=%v", first, firstCloser, err)
	}
	if err := first.Initialize(t.Context(), creds); err != nil {
		t.Fatalf("initialize the data-plane barrier: %v", err)
	}
	if err := firstCloser.Close(); err != nil {
		t.Fatalf("close the first-boot barrier: %v", err)
	}

	// Restart: the native listener comes up with the production
	// seal-disabled options; startService has not run yet.
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer(seal disabled): %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	nodeCfg := qrdbconfig.DefaultConfig()
	nodeCfg.Mode = "standalone"
	nodeCfg.Server.Host = "127.0.0.1"
	nodeCfg.Protocols = qrdbconfig.ProtocolsConfig{GRPC: true}
	nodeCfg.TLS.Enabled = false
	authOff := false
	nodeCfg.Auth.Enabled = &authOff
	serverTLS, clientTLS := loopbackTLSConfigs(t)
	kv := service.NewBackendKVService(qrdbdatabase.NewStandaloneBackend(qrdbmemory.NewStorage()))
	opts := append(srv.nodeBarrierServerOptions(false),
		qrdbserver.WithTLS(serverTLS), qrdbserver.WithGRPCListener(listener))
	node := qrdbserver.New(nodeCfg, kv, nil, opts...)
	if err := node.Start(); err != nil {
		t.Fatalf("start the qrdb gRPC server: %v", err)
	}
	t.Cleanup(node.Stop)

	conn, err := ggrpc.NewClient(listener.Addr().String(),
		ggrpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := qrdbv1.NewQRDBServiceClient(conn)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	_, err = client.Get(ctx, &qrdbv1.GetRequest{Key: "k"})
	if got := status.Code(err); got != codes.Unavailable {
		t.Fatalf("before startService publishes the barrier: native Get must return codes.Unavailable, got %v (err=%v)", got, err)
	}

	// startService: rebuild the persistent barrier and publish it.
	_, barrier, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	if err != nil || barrier == nil || closer == nil {
		t.Fatalf("rebuild the data-plane barrier: barrier=%v closer=%v err=%v", barrier, closer, err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	srv.publishDataPlaneBarrier(barrier)
	if srv.dataPlaneBarrierPending.Load() {
		t.Fatal("publishing the barrier must clear the pending flag")
	}

	if err := barrier.Unseal(t.Context(), creds); err != nil {
		t.Fatalf("unseal the data-plane barrier: %v", err)
	}
	_, err = client.Get(ctx, &qrdbv1.GetRequest{Key: "k"})
	if got := status.Code(err); got == codes.Unavailable {
		t.Fatalf("published and unsealed barrier: native Get must be admitted, got %v", err)
	}
}

// TestSealedWindowProbe_PendingBarrierReportsSealed: on a seal-disabled boot
// with a persistent DataDir the probe reports sealed from the moment the
// native barrier options are built until the barrier is published.
func TestSealedWindowProbe_PendingBarrierReportsSealed(t *testing.T) {
	cfg := config.Default()
	cfg.Seal.Enabled = false
	cfg.Storage.DataDir = t.TempDir()
	s := &Server{config: cfg, logger: buildTestLogger(t)}
	probe := s.SealedWindowProbe()
	if probe.IsSealed() {
		t.Fatal("before the native options are built nothing is pending")
	}
	_ = s.nodeBarrierServerOptions(false)
	if !probe.IsSealed() {
		t.Fatal("an unpublished persistent barrier must report sealed")
	}
	s.publishDataPlaneBarrier(newProbeTestBarrier(t))
	if probe.IsSealed() {
		t.Fatal("a published, uninitialized barrier must report unsealed")
	}
}
