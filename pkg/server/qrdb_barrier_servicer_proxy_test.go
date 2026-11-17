// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	qrdbgrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	"github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	qrdbconfig "github.com/jeremyhahn/go-qrdb/pkg/config"
	qrdbdatabase "github.com/jeremyhahn/go-qrdb/pkg/database"
	qrdbserver "github.com/jeremyhahn/go-qrdb/pkg/server"
	"github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbmemory "github.com/jeremyhahn/go-qrdb/pkg/storage/memory"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// statusOnlyBarrier is a BarrierServicer double that answers BarrierStatus;
// every other method is the embedded nil interface and is never called.
type statusOnlyBarrier struct {
	qrdbsdk.BarrierServicer
	calls int
}

func (b *statusOnlyBarrier) BarrierStatus(context.Context) (*qrdbsdk.BarrierStatusInfo, error) {
	b.calls++
	return &qrdbsdk.BarrierStatusInfo{Sealed: true, Strategy: "proxy-test", ShamirThreshold: 3, ShamirTotal: 5}, nil
}

// adminBarrier records the Shamir administrative calls the proxy forwards.
type adminBarrier struct {
	statusOnlyBarrier
	rekeys, recoveries atomic.Int32
}

func (b *adminBarrier) BarrierRekey(_ context.Context, threshold, total int) (*qrdbsdk.ShamirInitInfo, error) {
	b.rekeys.Add(1)
	return &qrdbsdk.ShamirInitInfo{Threshold: threshold, TotalShares: total, ShareCount: total}, nil
}

func (b *adminBarrier) BarrierGenerateRecoveryKeys(_ context.Context, threshold, total int) (*qrdbsdk.RecoveryKeysInfo, error) {
	b.recoveries.Add(1)
	return &qrdbsdk.RecoveryKeysInfo{Threshold: threshold, Total: total, ShareCount: total}, nil
}

// concurrentStatusBarrier answers BarrierStatus without shared mutable
// state, so the -race test sees only the proxy's own synchronization.
type concurrentStatusBarrier struct {
	qrdbsdk.BarrierServicer
}

func (concurrentStatusBarrier) BarrierStatus(context.Context) (*qrdbsdk.BarrierStatusInfo, error) {
	return &qrdbsdk.BarrierStatusInfo{Strategy: "concurrent"}, nil
}

// TestQRDBBarrierServicerProxy_ServesBarrierWiredAfterRegistration is the
// #2488 regression: the node's gRPC server registers the barrier before any
// barrier servicer is wired. The proxy built then must serve the barrier once
// it is wired, because it resolves the servicer per call.
func TestQRDBBarrierServicerProxy_ServesBarrierWiredAfterRegistration(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if s.qrdbBarrierServicer() != nil {
		t.Fatal("precondition: no barrier servicer is wired at registration time")
	}
	proxy := newQRDBBarrierServicerProxy(s)

	backing := &statusOnlyBarrier{}
	s.publishBarrierServicer(backing)

	info, err := proxy.BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("BarrierStatus after wiring: %v", err)
	}
	if backing.calls != 1 {
		t.Fatalf("BarrierStatus must delegate to the wired servicer once, got %d calls", backing.calls)
	}
	if info == nil || !info.Sealed || info.Strategy != "proxy-test" {
		t.Fatalf("BarrierStatus must return the wired servicer's answer, got %+v", info)
	}
}

// TestQRDBBarrierServicerProxy_NotConfiguredWhenNothingWired pins the answer
// of a node with no barrier servicer: the typed
// BarrierManagerNotConfiguredError, which go-qrdb's transports classify as
// Unimplemented (a raw gRPC status would be reclassified as Internal).
func TestQRDBBarrierServicerProxy_NotConfiguredWhenNothingWired(t *testing.T) {
	proxy := newQRDBBarrierServicerProxy(&Server{logger: buildTestLogger(t)})
	ctx := context.Background()

	_, statusErr := proxy.BarrierStatus(ctx)
	_, hasErr := proxy.BarrierHasRecoveryKeys(ctx)
	errs := map[string]error{
		"BarrierStatus":          statusErr,
		"BarrierSeal":            proxy.BarrierSeal(ctx),
		"BarrierUnseal":          proxy.BarrierUnseal(ctx, []byte("x")),
		"BarrierHasRecoveryKeys": hasErr,
	}
	for name, err := range errs {
		if !errors.As(err, new(*service.BarrierManagerNotConfiguredError)) {
			t.Fatalf("%s: expected BarrierManagerNotConfiguredError, got %v", name, err)
		}
	}

	if _, err := newQRDBBarrierServicerProxy(nil).BarrierStatus(ctx); !errors.As(err, new(*service.BarrierManagerNotConfiguredError)) {
		t.Fatalf("a proxy without a server must answer BarrierManagerNotConfiguredError, got %v", err)
	}
}

// serveQRDBBarrierOverTCP serves the qrdb.v1 service on a real loopback TCP
// listener with the proxy as its barrier service -- the registration
// qrdbserver.WithBarrierService gives the node's TCP gRPC server -- and
// returns a qrdb.v1 client dialled over TCP.
func serveQRDBBarrierOverTCP(t *testing.T, s *Server) qrdbv1.QRDBServiceClient {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	node := ggrpc.NewServer()
	qrdbgrpc.RegisterService(node, nil, nil, qrdbgrpc.WithRegisterBarrierService(newQRDBBarrierServicerProxy(s)))
	served := make(chan error, 1)
	go func() { served <- node.Serve(listener) }()
	t.Cleanup(func() {
		node.Stop()
		if serveErr := <-served; serveErr != nil {
			t.Errorf("serve: %v", serveErr)
		}
	})
	conn, err := ggrpc.NewClient(listener.Addr().String(),
		ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return qrdbv1.NewQRDBServiceClient(conn)
}

// TestQRDBBarrierStatusOverTCP_ServesWiredBarrier: qrdb.v1 BarrierStatus over
// TCP reaches a barrier servicer wired after registration, instead of the
// "unknown service"/"method not found" Unimplemented of an unregistered one.
func TestQRDBBarrierStatusOverTCP_ServesWiredBarrier(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	client := serveQRDBBarrierOverTCP(t, s)

	backing := &statusOnlyBarrier{}
	s.publishBarrierServicer(backing)

	resp, err := client.BarrierStatus(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("BarrierStatus over TCP with a wired barrier: %v (code %s)", err, status.Code(err))
	}
	if backing.calls != 1 || !resp.GetSealed() || resp.GetStrategy() != "proxy-test" {
		t.Fatalf("BarrierStatus over TCP must return the wired servicer's answer, got %+v after %d calls", resp, backing.calls)
	}
}

// TestQRDBBarrierStatusOverTCP_UnimplementedWhenNothingWired: with no barrier
// servicer wired, qrdb.v1 BarrierStatus over TCP answers Unimplemented, not
// Internal.
func TestQRDBBarrierStatusOverTCP_UnimplementedWhenNothingWired(t *testing.T) {
	client := serveQRDBBarrierOverTCP(t, &Server{logger: buildTestLogger(t)})

	_, err := client.BarrierStatus(context.Background(), &emptypb.Empty{})
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("BarrierStatus over TCP with nothing wired: expected codes.Unimplemented, got %s (%v)", got, err)
	}
}

// The client adapter dropped the Shamir quorum; the published direct
// servicer's counts reach the qrdb.v1 caller.
func TestQRDBBarrierStatusOverTCP_CarriesShamirCounts(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	client := serveQRDBBarrierOverTCP(t, s)
	s.publishBarrierServicer(&statusOnlyBarrier{})

	resp, err := client.BarrierStatus(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("BarrierStatus over TCP: %v", err)
	}
	if resp.GetShamirThreshold() != 3 || resp.GetShamirTotal() != 5 {
		t.Fatalf("BarrierStatus must carry the Shamir quorum 3/5, got %d/%d", resp.GetShamirThreshold(), resp.GetShamirTotal())
	}
}

// Rekey and recovery-generate with an SO PIN reach the published servicer:
// the PIN is the outer handler's to check, and the proxy must not refuse
// the call the way the client adapter did.
func TestQRDBBarrierAdminOverTCP_ReachesPublishedServicer(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	client := serveQRDBBarrierOverTCP(t, s)
	backing := &adminBarrier{}
	s.publishBarrierServicer(backing)
	ctx := context.Background()

	if _, err := client.BarrierRekey(ctx, &qrdbv1.BarrierRekeyRequest{Threshold: 3, Total: 5, SoPin: "123456"}); err != nil {
		t.Fatalf("BarrierRekey with an SO PIN: %v (code %s)", err, status.Code(err))
	}
	if _, err := client.BarrierRecoveryGenerate(ctx, &qrdbv1.BarrierRecoveryGenerateRequest{Threshold: 3, Total: 5, SoPin: "123456"}); err != nil {
		t.Fatalf("BarrierRecoveryGenerate with an SO PIN: %v (code %s)", err, status.Code(err))
	}
	if backing.rekeys.Load() != 1 || backing.recoveries.Load() != 1 {
		t.Fatalf("the published servicer must see one rekey and one recovery-generate, got %d and %d",
			backing.rekeys.Load(), backing.recoveries.Load())
	}
}

// Readers call BarrierStatus while another goroutine publishes and clears
// the servicer; run under -race.
func TestQRDBBarrierServicerProxy_ConcurrentPublish(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	proxy := newQRDBBarrierServicerProxy(s)
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				info, err := proxy.BarrierStatus(ctx)
				if err == nil && info.Strategy != "concurrent" {
					t.Errorf("unexpected status %+v", info)
					return
				}
				if err != nil && !errors.As(err, new(*service.BarrierManagerNotConfiguredError)) {
					t.Errorf("unexpected error %v", err)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 200 {
			if i%2 == 0 {
				s.publishBarrierServicer(concurrentStatusBarrier{})
			} else {
				s.publishBarrierServicer(nil)
			}
		}
	}()
	wg.Wait()
}

// With the seal enabled go-qrdb registers the node's own barrier servicer,
// which the proxy must not override; with it disabled the proxy is the
// node's only barrier service.
func TestUnifiedModuleServerOptions_BarrierProxyOnlyWhenSealDisabled(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if got := s.nodeBarrierServerOptions(true); len(got) != 0 {
		t.Fatalf("seal enabled: expected no barrier option, got %d", len(got))
	}
	if got := s.nodeBarrierServerOptions(false); len(got) != 2 {
		t.Fatalf("seal disabled: expected the barrier proxy and sealed-window probe options, got %d", len(got))
	}

	srv := newModuleHostTestServer(t)
	sealed, err := srv.unifiedModuleServerOptions(nil, true)
	if err != nil {
		t.Fatalf("unifiedModuleServerOptions(sealEnabled=true): %v", err)
	}
	unsealed, err := srv.unifiedModuleServerOptions(nil, false)
	if err != nil {
		t.Fatalf("unifiedModuleServerOptions(sealEnabled=false): %v", err)
	}
	if len(unsealed) != len(sealed)+2 {
		t.Fatalf("seal disabled must add exactly the barrier proxy and sealed-window probe options: %d options sealed, %d unsealed", len(sealed), len(unsealed))
	}
}

// With the seal disabled the proxy serves the data-plane barrier on the
// node's native transports, and the sealed-window probe it brings along
// must arm once that barrier is initialized and sealed, so the native data
// RPCs answer Unavailable (go-quicbaas #2488). pkg/server has no untagged
// seal-disabled helper that serves a real node listener, so this checks the
// probe the option carries.
func TestNodeBarrierServerOptions_SealDisabledProbeArmsOnSealedBarrier(t *testing.T) {
	cfg := config.Default()
	cfg.Seal.Enabled = false
	cfg.Seal.Strategy = "passphrase"
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer(seal disabled): %v", err)
	}
	if got := srv.nodeBarrierServerOptions(false); len(got) != 2 {
		t.Fatalf("seal disabled: expected the barrier proxy and sealed-window probe options, got %d", len(got))
	}

	_, barrier, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	if err != nil || barrier == nil || closer == nil {
		t.Fatalf("build the data-plane barrier: barrier=%v closer=%v err=%v", barrier, closer, err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	srv.publishDataPlaneBarrier(barrier)

	probe := srv.SealedWindowProbe()
	creds, err := qrdbsdk.CredentialsFor([]byte("seal-disabled-probe-unit-test-passphrase"))
	if err != nil {
		t.Fatalf("protect the passphrase: %v", err)
	}
	defer creds.Secret.Destroy()
	if err := barrier.Initialize(t.Context(), creds); err != nil {
		t.Fatalf("initialize the data-plane barrier: %v", err)
	}
	if probe.IsSealed() {
		t.Fatal("an initialized, unsealed barrier must not arm the sealed window")
	}
	if err := barrier.Seal(); err != nil {
		t.Fatalf("seal the data-plane barrier: %v", err)
	}
	if !probe.IsSealed() {
		t.Fatal("seal disabled: an initialized, sealed data-plane barrier must arm the sealed window")
	}
}

// TestNodeBarrierServerOptions_NativeGRPCGatedBySealedBarrier is the
// regression test for #2512: a qrdb gRPC server built with the production
// seal-disabled options (nodeBarrierServerOptions(false)) serves native data
// RPCs while the published data-plane barrier is unsealed, and refuses them
// with codes.Unavailable once that barrier is sealed. The refusal comes only
// from the WithSealedWindowProbe option: with it replaced by a no-op the
// sealed-barrier Put succeeds and this test fails.
func TestNodeBarrierServerOptions_NativeGRPCGatedBySealedBarrier(t *testing.T) {
	cfg := config.Default()
	cfg.Seal.Enabled = false
	cfg.Seal.Strategy = "passphrase"
	cfg.Storage.Backend = config.StorageBackendMemory
	cfg.Storage.DataDir = t.TempDir()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer(seal disabled): %v", err)
	}

	_, barrier, _, closer, err := buildDataPlaneBarrierService(cfg, nil)
	if err != nil || barrier == nil || closer == nil {
		t.Fatalf("build the data-plane barrier: barrier=%v closer=%v err=%v", barrier, closer, err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	creds, err := qrdbsdk.CredentialsFor([]byte("native-grpc-sealed-gate-unit-test-passphrase"))
	if err != nil {
		t.Fatalf("protect the passphrase: %v", err)
	}
	defer creds.Secret.Destroy()
	if err := barrier.Initialize(t.Context(), creds); err != nil {
		t.Fatalf("initialize the data-plane barrier: %v", err)
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
	// Publish the barrier exactly as the lifecycle does.
	srv.publishDataPlaneBarrier(barrier)

	conn, err := ggrpc.NewClient(listener.Addr().String(),
		ggrpc.WithTransportCredentials(credentials.NewTLS(clientTLS)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := qrdbv1.NewQRDBServiceClient(conn)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := client.Put(ctx, &qrdbv1.PutRequest{Key: "unsealed", Value: []byte("v")}); err != nil {
		t.Fatalf("unsealed barrier: native Put must succeed, got %v", err)
	}

	if err := barrier.Seal(); err != nil {
		t.Fatalf("seal the data-plane barrier: %v", err)
	}
	_, err = client.Put(ctx, &qrdbv1.PutRequest{Key: "sealed", Value: []byte("v")})
	if got := status.Code(err); got != codes.Unavailable {
		t.Fatalf("sealed barrier: native Put must return codes.Unavailable, got %v (err=%v)", got, err)
	}
}

// loopbackTLSConfigs returns a server TLS config with an in-memory
// self-signed certificate for 127.0.0.1 and a client config that trusts
// exactly that certificate.
func loopbackTLSConfigs(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "qrdb-barrier-gate-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: priv, Leaf: leaf}},
		NextProtos:   []string{"h2"},
		MinVersion:   tls.VersionTLS13,
	}
	clientTLS := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}
	return serverTLS, clientTLS
}
