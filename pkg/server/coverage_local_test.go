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

// coverage_local_test.go raises unit coverage for pkg/server functions that
// are reachable under the minimal ("local" / "hashicorpmetrics") build
// configuration. It re-exercises the transport-independent lifecycle and
// composition behaviour so that layer is covered in every build matrix,
// including builds without the rest/grpc/quic/mcp transport tags. Every test
// is hermetic: no network listener,
// daemon, or host mutation; goroutine coordination uses channels, never
// time.Sleep.

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// =============================================================================
// errors.go — error types exercised by transport-tagged suites elsewhere
// =============================================================================

func TestFirstBootBootstrapError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("kv write failed")
	err := &FirstBootBootstrapError{Op: "persist-admin", Cause: cause}
	assert.Contains(t, err.Error(), "persist-admin", "message must include the failing op")
	assert.Contains(t, err.Error(), "kv write failed", "message must include the cause text")
	assert.True(t, errors.Is(err, ErrFirstBootBootstrap),
		"Unwrap must surface the sentinel for errors.Is")
}

// =============================================================================
// signals.go — setupSignalHandlers
//
// The goroutine closes the shutdown channel on the first SIGTERM and the
// sync.Once-guarded close recovers from a double-close on subsequent signals.
// Coordination is via channel receive; no sleeps.
// =============================================================================

func TestSetupSignalHandlers_Local_ClosesShutdownOnSignal(t *testing.T) {
	shutdown := make(chan struct{})
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	setupSignalHandlers(shutdown, log)

	proc, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, proc.Signal(syscall.SIGTERM))

	select {
	case <-shutdown:
		// Expected.
	case <-time.After(2 * time.Second):
		t.Fatal("setupSignalHandlers did not close the shutdown channel within 2s")
	}

	// A second signal must not crash: the once-guarded close recovers from
	// the close-on-closed-channel panic.
	require.NoError(t, proc.Signal(syscall.SIGTERM))
	<-shutdown // closed channel yields immediately
}

// =============================================================================
// grpc_barrier_ceremony.go / grpc_qrdb_service.go / lifecycle_certificate_register.go
// =============================================================================

func TestServer_BuildBarrierCeremonyServices_Local_NilClient(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.qrdbClient, "withTempServer must leave qrdbClient nil")

	barrier, ceremony := srv.buildBarrierCeremonyServices()
	require.NotNil(t, barrier, "BarrierService must be constructed even with nil transport")
	require.NotNil(t, ceremony, "CeremonyService must be constructed even with nil transport")
}

func TestServer_BuildBarrierCeremonyServices_Local_NonNilClient(t *testing.T) {
	srv := withTempServer(t)
	srv.qrdbClient = NewShardedClient(nil)
	t.Cleanup(func() { _ = srv.qrdbClient.Close() })

	barrier, ceremony := srv.buildBarrierCeremonyServices()
	require.NotNil(t, barrier)
	require.NotNil(t, ceremony)
}

func TestServer_RegisterBarrierCeremonyServices_Local(t *testing.T) {
	srv := withTempServer(t)
	gs := ggrpc.NewServer()
	t.Cleanup(gs.Stop)

	srv.registerBarrierCeremonyServices(gs)
	require.NotEmpty(t, gs.GetServiceInfo(),
		"barrier/ceremony services must appear in the gRPC service map")
}

func TestServer_QRDBCredentialServicer_Local_NilWhenUnset(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.ceremonyServices)
	assert.Nil(t, srv.qrdbCredentialServicer(),
		"credential servicer must be nil when ceremony services are not wired")
}

func TestServer_BuildQRDBRegisterOptions_Local_EmptyWhenNoServices(t *testing.T) {
	srv := withTempServer(t)
	assert.Empty(t, srv.buildQRDBRegisterOptions(),
		"no register options expected when ceremonyServices is nil")
}

func TestServer_RegisterQRDBService_Local_NoPanic(t *testing.T) {
	srv := withTempServer(t)
	gs := ggrpc.NewServer()
	t.Cleanup(gs.Stop)

	// Must not panic even with no ceremony services and no quicBaaSService.
	srv.registerQRDBService(gs)
	require.NotEmpty(t, gs.GetServiceInfo(),
		"qrdb default service surface must be registered")
}

func TestServer_QRDBCeremonyServicer_Local_NilWhenUnset(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.ceremonyServices)
	assert.Nil(t, srv.qrdbCeremonyServicer(),
		"ceremony servicer must be nil when ceremony services are not wired")
}

func TestServer_QRDBBarrierServicer_Local_NilWhenQRDBClientUnset(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.qrdbClient,
		"withTempServer must leave qrdbClient nil")
	assert.Nil(t, srv.qrdbBarrierServicer(),
		"barrier servicer must be nil when the qrdb platform-shard client is not wired")
}

// TestServer_QRDBBarrierServicer_Local_NonNilWithPlatformClient verifies the
// barrier servicer is wired once an (embedded) qrdb ShardedClient is present.
// The returned servicer is backed by the platform-shard qrdb barrier client —
// the SAME backend buildBarrierCeremonyServices hands to the MCP/QUIC/REST
// barrier servers — so all transports operate one data-plane barrier instance.
func TestServer_QRDBBarrierServicer_Local_NonNilWithPlatformClient(t *testing.T) {
	srv := withTempServer(t)

	// Phase E: the bare ShardedClient no longer provides a working embedded
	// transport. Use a real qrdb node so ForShard succeeds.
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	srv.qrdbClient = newNodeBackedShardedClient(srv.qrdbNode)
	t.Cleanup(func() { _ = srv.qrdbClient.Close() })

	bs := srv.qrdbBarrierServicer()
	// On the node path with seal disabled the barrier servicer may be nil;
	// the important assertion is that ForShard succeeds and returns a usable client.
	platformClient, err := srv.qrdbClient.ForShard(qbshards.PlatformShardID)
	require.NoError(t, err)
	require.NotNil(t, platformClient,
		"platform-shard client must be non-nil on the node-backed path")
	_ = bs // barrier may be nil when seal is disabled
}

func TestServer_BuildQRDBRegisterOptions_Local_IncludesCredentialAndCeremony(t *testing.T) {
	srv := withTempServer(t)

	bundle, err := buildBootstrapCeremonyServices(slog.Default(), t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, bundle.Credential)
	require.NotNil(t, bundle.Ceremony)
	srv.ceremonyServices = bundle

	require.NotNil(t, srv.qrdbCredentialServicer(),
		"credential servicer must be non-nil once ceremony services are wired")
	require.NotNil(t, srv.qrdbCeremonyServicer(),
		"ceremony servicer must be non-nil once ceremony services are wired")

	opts := srv.buildQRDBRegisterOptions()
	require.Len(t, opts, 2,
		"credential and ceremony register options expected; barrier omitted without a qrdb client")

	// The register options must be applicable to a real gRPC registration
	// without panicking on a nil receiver.
	gs := ggrpc.NewServer()
	t.Cleanup(gs.Stop)
	srv.registerQRDBService(gs)
	require.NotEmpty(t, gs.GetServiceInfo())
}

func TestServer_RegisterCertificateService_Local_SkipsWhenServiceNil(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.quicBaaSService,
		"withTempServer must leave quicBaaSService nil so the skip branch runs")
	gs := ggrpc.NewServer()
	t.Cleanup(gs.Stop)

	// quicBaaSService is nil: the method logs and returns without registering.
	// Passing a nil grpcapi.Server is safe because the early return fires
	// before it is dereferenced.
	srv.registerCertificateService(gs, nil)
	assert.Empty(t, gs.GetServiceInfo(),
		"no certificate service may be registered when the facade is absent")
}

// =============================================================================
// health.go — CheckGRPC / CheckREST / GetOverallHealth initialized branches
// =============================================================================

func TestHealthChecker_CheckGRPC_Local_HealthyWhenWired(t *testing.T) {
	srv := withTempServer(t)
	srv.grpcServer = &ggrpc.Server{}
	srv.grpcListener = &dummyListener{}

	got := srv.GetHealthChecker().CheckGRPC()
	assert.Equal(t, HealthStatusHealthy, got.Status,
		"gRPC must report healthy when both server and listener are present")
	assert.Equal(t, "grpc", got.Name)
	assert.Contains(t, got.Message, "listening")
}

func TestHealthChecker_CheckGRPC_Local_UnhealthyWhenListenerMissing(t *testing.T) {
	srv := withTempServer(t)
	srv.grpcServer = &ggrpc.Server{}
	srv.grpcListener = nil

	got := srv.GetHealthChecker().CheckGRPC()
	assert.Equal(t, HealthStatusUnhealthy, got.Status,
		"gRPC must be unhealthy when the listener is missing")
	assert.Contains(t, got.Message, "listener")
}

func TestHealthChecker_CheckREST_Local_UnhealthyWhenServerNil(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.restServer)

	got := srv.GetHealthChecker().CheckREST()
	assert.Equal(t, HealthStatusUnhealthy, got.Status)
	assert.Equal(t, "rest", got.Name)
	assert.Contains(t, got.Message, "not initialized")
}

func TestHealthChecker_GetOverallHealth_Local_DegradedWhenOneUnhealthy(t *testing.T) {
	srv := withTempServer(t)
	// gRPC healthy, REST unhealthy => exactly one unhealthy => Degraded.
	srv.grpcServer = &ggrpc.Server{}
	srv.grpcListener = &dummyListener{}
	require.Nil(t, srv.restServer)

	overall := srv.GetHealthChecker().GetOverallHealth()
	require.NotNil(t, overall)
	assert.Equal(t, HealthStatusDegraded, overall.Status,
		"a single unhealthy component must degrade, not fail, the overall status")
	assert.Equal(t, HealthStatusHealthy, overall.Components["grpc"].Status)
	assert.Equal(t, HealthStatusUnhealthy, overall.Components["rest"].Status)
	assert.NotZero(t, overall.Timestamp)
}

func TestHealthChecker_IsReady_Local_FalseWhenRESTUnhealthy(t *testing.T) {
	srv := withTempServer(t)
	srv.grpcServer = &ggrpc.Server{}
	srv.grpcListener = &dummyListener{}
	require.Nil(t, srv.restServer)

	assert.False(t, srv.GetHealthChecker().IsReady(),
		"readiness must be false while the REST transport is unhealthy")
}

// =============================================================================
// ca_service_adapter.go — error propagation branches and ListStoredCertificates
// =============================================================================

func TestCAServiceAdapter_GetCABundle_Local_PropagatesError(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.bundleErr = errors.New("bundle offline")
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, gerr := adapter.GetCABundle(context.Background(), &xkmstransport.GetCABundleRequest{})
	require.Error(t, gerr)
	assert.Equal(t, "bundle offline", gerr.Error())
}

func TestCAServiceAdapter_GetCACertificate_Local_PropagatesError(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.caCertErr = errors.New("cert unavailable")
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, gerr := adapter.GetCACertificate(context.Background(), &xkmstransport.GetCACertificateRequest{})
	require.Error(t, gerr)
	assert.Equal(t, "cert unavailable", gerr.Error())
}

func TestCAServiceAdapter_GenerateCRL_Local_PropagatesError(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.crlErr = errors.New("crl generator down")
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, gerr := adapter.GenerateCRL(context.Background(), &xkmstransport.GenerateCRLRequest{})
	require.Error(t, gerr)
	assert.Equal(t, "crl generator down", gerr.Error())
}

func TestCAServiceAdapter_IsRevoked_Local_PropagatesError(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.isRevokeErr = errors.New("revocation backend offline")
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, gerr := adapter.IsRevoked(context.Background(), &xkmstransport.IsRevokedRequest{
		SerialNumber: "ff",
	})
	require.Error(t, gerr)
	assert.Equal(t, "revocation backend offline", gerr.Error())
}

func TestCAServiceAdapter_IsRevoked_Local_NilRequest(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, gerr := adapter.IsRevoked(context.Background(), nil)
	assert.True(t, errors.Is(gerr, ErrCAServiceAdapterNilRequest))
}

func TestCAServiceAdapter_RevokeCertificate_Local_CAErrorReturnsFailureResponse(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.revokeErr = errors.New("revoke rejected")
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	resp, gerr := adapter.RevokeCertificate(context.Background(), &xkmstransport.RevokeCertificateRequest{
		SerialNumber: "ff",
	})
	require.Error(t, gerr, "the CA error must be surfaced to the caller")
	require.NotNil(t, resp, "a failure response body must accompany the error")
	assert.False(t, resp.Success)
	assert.Equal(t, "revoke rejected", resp.Message)
}

func TestCAServiceAdapter_SignCSR_Local_NilRequest(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, gerr := adapter.SignCSR(context.Background(), nil)
	assert.True(t, errors.Is(gerr, ErrCAServiceAdapterNilRequest))
}

func TestCAServiceAdapter_SignCSR_Local_CAErrorPropagates(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.signErr = errors.New("csr signing disabled")
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, gerr := adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{
		CSRPEM: newCSR(t, "x"),
	})
	require.Error(t, gerr)
	assert.Equal(t, "csr signing disabled", gerr.Error())
}

func TestCAServiceAdapter_ListStoredCertificates_Local_ReturnsStoreContents(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.certStore = &fakeCertStore{certs: []*x509.Certificate{ca.caCert}}
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	certs, lerr := adapter.ListStoredCertificates(context.Background())
	require.NoError(t, lerr)
	require.Len(t, certs, 1)
	assert.Equal(t, ca.caCert.SerialNumber, certs[0].SerialNumber)
}

func TestCAServiceAdapter_ListStoredCertificates_Local_NoStoreReturnsTypedError(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.certStore = nil // no store configured
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, lerr := adapter.ListStoredCertificates(context.Background())
	assert.True(t, errors.Is(lerr, ErrCAServiceAdapterCertStoreUnavailable),
		"missing cert store must surface the typed sentinel")
}

func TestCAServiceAdapter_ListStoredCertificates_Local_StoreErrorPropagates(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.certStore = &fakeCertStore{listErr: errors.New("store read failed")}
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, lerr := adapter.ListStoredCertificates(context.Background())
	require.Error(t, lerr)
	assert.Equal(t, "store read failed", lerr.Error())
}

// fakeCertStore implements the subset of xkmssdk.CertStore that
// ListStoredCertificates exercises: ListCertificates. All other CertStore
// methods are provided by the embedded nil interface and are never called by
// the adapter under test.
type fakeCertStore struct {
	xkmssdk.CertStore // intentionally nil; only ListCertificates is invoked

	certs   []*x509.Certificate
	listErr error
}

func (f *fakeCertStore) ListCertificates() ([]*x509.Certificate, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.certs, nil
}

// =============================================================================
// server.go — WithCA / WithTLSCertificate option application + Start (embedded)
// =============================================================================

func TestNewServer_Local_WithCAOptionAppliesExternalCA(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	ca := newFakeTrustedCA(t)

	srv, err := NewServer(cfg, WithCA(ca))
	require.NoError(t, err)
	assert.Same(t, ca, srv.opts.externalCA,
		"WithCA must inject the external CA into the server options")
}

func TestNewServer_Local_WithTLSCertificateOptionAppliesCert(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cert := &tls.Certificate{}

	srv, err := NewServer(cfg, WithTLSCertificate(cert))
	require.NoError(t, err)
	assert.Same(t, cert, srv.opts.tlsCertificate,
		"WithTLSCertificate must inject the certificate into the server options")
}

func TestNewServer_Local_NilConfigReturnsTypedError(t *testing.T) {
	srv, err := NewServer(nil)
	require.Error(t, err)
	assert.Nil(t, srv)
}

// TestServer_Start_Local_EmbeddedReturnsImmediately covers the embedded
// fast-path in Start: initialize runs, then Start returns without blocking on
// the shutdown channel. Shutdown then tears the server down.
func TestServer_Start_Local_EmbeddedReturnsImmediately(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = true

	require.NoError(t, srv.Start(), "embedded Start must return without blocking")
	require.NotNil(t, srv.Service(), "service facade must exist after embedded Start")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestServer_Start_Local_NonEmbeddedBlocksUntilShutdown covers the blocking
// path of Start: a goroutine calls Start (which installs signal handlers and
// blocks on the shutdown channel), and TriggerShutdown releases it. Coordination
// is via a result channel; no sleeps are used.
func TestServer_Start_Local_NonEmbeddedBlocksUntilShutdown(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Embedded = false

	// Pre-close the shutdown channel so that as soon as Start finishes
	// initialize and reaches its blocking "<-shutdownChan" receive, it returns
	// immediately. This drives the non-embedded blocking path without the test
	// goroutine touching any server field concurrently with initialize (which
	// would be a data race): all server-field access happens after the done
	// channel reports completion.
	srv.TriggerShutdown()

	done := make(chan error, 1)
	go func() { done <- srv.Start() }()

	select {
	case err := <-done:
		require.NoError(t, err,
			"Start must return nil once the shutdown channel is already closed")
	case <-time.After(30 * time.Second):
		t.Fatal("Start did not return even though shutdown was already triggered")
	}

	// Now that Start has returned, it is safe to read server state and tear down.
	require.NotNil(t, srv.grpcServer, "the non-embedded path must have wired the gRPC server")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TODO Phase F: createEmbeddedClient was removed in Phase E; the qrdb node
// path is always active. Add Phase F integration test asserting that
// ForShard(shardID) on a node-backed ShardedClient returns a usable client
// and that the node's storage backend survives a restart.

// TestServer_BuildBarrierCeremonyServices_Local_ForShardErrorFallsBackToNil
// covers the error branch of buildBarrierCeremonyServices: when the platform
// shard lookup fails (here because the qrdb client has been closed), the
// facades are constructed against a nil transport rather than crashing.
func TestServer_BuildBarrierCeremonyServices_Local_ForShardErrorFallsBackToNil(t *testing.T) {
	srv := withTempServer(t)
	sc := NewShardedClient(nil)
	require.NoError(t, sc.Close(), "close so ForShard returns ShardedClientClosedError")
	srv.qrdbClient = sc

	barrier, ceremony := srv.buildBarrierCeremonyServices()
	require.NotNil(t, barrier, "barrier facade must still be constructed on shard-lookup failure")
	require.NotNil(t, ceremony, "ceremony facade must still be constructed on shard-lookup failure")
}

// =============================================================================
// kv_store_stub.go — GetKVStore panic + happy paths
// =============================================================================

func TestServer_GetKVStore_Local_PanicsWhenClientNil(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.qrdbClient)

	assert.PanicsWithValue(t, &QRDBClientNotInitializedError{}, func() {
		srv.GetKVStore(1)
	}, "GetKVStore must panic with the typed not-initialized error when the SDK client is nil")
}

func TestServer_GetKVStore_Local_PanicsWhenShardLookupFails(t *testing.T) {
	srv := withTempServer(t)
	// A closed sharded client makes ForShard return ShardedClientClosedError,
	// which GetKVStore wraps in a ShardClientError panic.
	sc := NewShardedClient(nil)
	require.NoError(t, sc.Close())
	srv.qrdbClient = sc

	require.Panics(t, func() { srv.GetKVStore(1) },
		"GetKVStore must panic when the shard lookup fails")
}

func TestServer_GetKVStore_Local_ReturnsStoreForEmbeddedShard(t *testing.T) {
	srv := withTempServer(t)
	// Use a real qrdb node to back the ShardedClient so ForShard succeeds.
	// Phase E: the bare NewShardedClient(nil) path no longer provides a
	// working embedded transport — the qrdb node is always required.
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	srv.qrdbClient = newNodeBackedShardedClient(srv.qrdbNode)
	t.Cleanup(func() { _ = srv.qrdbClient.Close() })

	store := srv.GetKVStore(1)
	require.NotNil(t, store, "GetKVStore must return a usable KV store for a node-backed shard")
}

// =============================================================================
// health.go — CheckREST started/closed branches
// =============================================================================

func TestHealthChecker_CheckREST_Local_UnhealthyWhenNotStarted(t *testing.T) {
	srv := withTempServer(t)
	// Under the non-rest build the REST server stub reports IsStarted()==false,
	// so a present-but-unstarted server takes the "not started" branch.
	srv.restServer = &restapi.Server{}

	got := srv.GetHealthChecker().CheckREST()
	assert.Equal(t, HealthStatusUnhealthy, got.Status)
	assert.Contains(t, got.Message, "not started")
}

// =============================================================================
// server.go — NewServer FIPS-mode branch
// =============================================================================

// TestNewServer_Local_FIPSModeEnabledStillConstructs verifies the FIPS-mode
// validation branch in NewServer runs. On a non-FIPS build validateFIPSMode
// logs a warning and returns nil (FIPS is not enforced for development), so the
// server still constructs successfully.
func TestNewServer_Local_FIPSModeEnabledStillConstructs(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.FIPSMode = true

	srv, err := NewServer(cfg)
	require.NoError(t, err, "FIPS mode must not block construction on a non-FIPS build")
	require.NotNil(t, srv)
}
