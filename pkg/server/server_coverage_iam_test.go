//go:build !integration

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

// server_coverage_iam_test.go raises iam-only unit coverage for pkg/server
// without requiring the raft, rest, grpc, vector, or other build tags. Every
// test in this file uses the synthetic-harness pattern: a Server is created
// via withTempServer, fields are populated by direct assignment, and the
// behaviour-under-test is exercised in isolation. No tests spin up a network
// listener, daemon, or container runtime.

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// signals.go — setupSignalHandlers
//
// setupSignalHandlers spawns a goroutine that closes the shutdown channel on
// the first SIGTERM/SIGINT. The test sends a SIGTERM to ourselves, waits for
// the goroutine to handle it (signalled by close-on-shutdownChan), and
// confirms re-delivery of the same signal does not panic because the
// once.Do guard runs once and the recover inside it swallows any close-on-
// closed-channel panic. Hermetic: no time.Sleep, just channel receives.
// =============================================================================

func TestSetupSignalHandlers_ClosesShutdownChanOnSignal(t *testing.T) {
	shutdown := make(chan struct{})
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	setupSignalHandlers(shutdown, log)

	// Use the test process's own pid to fire SIGTERM. The goroutine inside
	// setupSignalHandlers calls signal.Notify on its own channel; sending
	// SIGTERM to our pid delivers to every Notify-registered channel in
	// the process, including the one inside setupSignalHandlers.
	proc, err := os.FindProcess(os.Getpid())
	require.NoError(t, err)
	require.NoError(t, proc.Signal(syscall.SIGTERM))

	select {
	case <-shutdown:
		// Expected: shutdown chan closed.
	case <-time.After(2 * time.Second):
		t.Fatal("setupSignalHandlers did not close the shutdown channel within 2s")
	}

	// Send the signal a second time. The goroutine's sync.Once-guarded
	// close() recovers from the double-close panic, so the second
	// delivery must not crash. After the chan is closed any number of
	// receives return immediately.
	require.NoError(t, proc.Signal(syscall.SIGTERM))

	// One more receive: the closed channel yields the zero value
	// immediately. If the second close panic-killed the goroutine, this
	// receive would still succeed because the chan was already closed,
	// but the goroutine would now be dead. We only assert no-crash.
	<-shutdown
}

// =============================================================================
// grpc_barrier_ceremony.go
//
// registerBarrierCeremonyServices builds the BarrierServicer + CeremonyServicer
// from the qrdb client and registers them on the supplied gRPC server. The
// nil-client branch in buildBarrierCeremonyServices is the canonical test
// scenario (no embedded qrdb client wired) — both helpers must succeed and
// the resulting handlers later respond with codes.Unavailable on calls.
// =============================================================================

func TestServer_BuildBarrierCeremonyServices_NilClientReturnsServices(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.qrdbClient, "withTempServer must leave qrdbClient nil")

	barrier, ceremony := srv.buildBarrierCeremonyServices()
	require.NotNil(t, barrier, "BarrierService must be constructed even with nil transport")
	require.NotNil(t, ceremony, "CeremonyService must be constructed even with nil transport")
}

func TestServer_BuildBarrierCeremonyServices_NonNilClient(t *testing.T) {
	srv := withTempServer(t)
	// Construct a ShardedClient and assign it. The platform shard lookup
	// returns a non-nil client; the Barrier and Ceremony facades use it.
	srv.qrdbClient = NewShardedClient(nil)
	t.Cleanup(func() { _ = srv.qrdbClient.Close() })

	barrier, ceremony := srv.buildBarrierCeremonyServices()
	require.NotNil(t, barrier)
	require.NotNil(t, ceremony)
}

func TestServer_RegisterBarrierCeremonyServices_RegistersOnGRPCServer(t *testing.T) {
	srv := withTempServer(t)
	gs := ggrpc.NewServer()
	t.Cleanup(gs.Stop)

	// Should not panic. After registration the gRPC server has a non-empty
	// service info map containing the Barrier and Ceremony services.
	srv.registerBarrierCeremonyServices(gs)
	info := gs.GetServiceInfo()
	require.NotEmpty(t, info)
}

// =============================================================================
// grpc_qrdb_service.go
//
// registerQRDBService delegates to qrdb's own RegisterService helper with a
// slice of options assembled by buildQRDBRegisterOptions. The latter consults
// s.ceremonyServices to decide whether to register the credential sub-service.
// =============================================================================

func TestServer_QRDBCredentialServicer_NilWhenCeremonyServicesUnset(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.ceremonyServices)
	assert.Nil(t, srv.qrdbCredentialServicer())
}

func TestServer_BuildQRDBRegisterOptions_EmptyWhenNoServices(t *testing.T) {
	srv := withTempServer(t)
	opts := srv.buildQRDBRegisterOptions()
	assert.Empty(t, opts, "no register options expected when ceremonyServices is nil")
}

func TestServer_RegisterQRDBService_RegistersWithoutPanic(t *testing.T) {
	srv := withTempServer(t)
	gs := ggrpc.NewServer()
	t.Cleanup(gs.Stop)

	// With no ceremonyServices and no quicBaaSService, the registrar still
	// registers qrdb's default RegisterService surface. The call must not
	// panic.
	srv.registerQRDBService(gs)
}

// =============================================================================
// health.go — GetOverallHealth aggregation branches
// =============================================================================

func TestHealthChecker_GetOverallHealth_BothComponentsReportUnhealthy(t *testing.T) {
	srv := withTempServer(t)
	overall := srv.GetHealthChecker().GetOverallHealth()
	require.NotNil(t, overall)
	// With both transports uninitialized the aggregate is Unhealthy
	// (unhealthyCount >= 2 branch).
	assert.Equal(t, HealthStatusUnhealthy, overall.Status)
	assert.Equal(t, HealthStatusUnhealthy, overall.Components["grpc"].Status)
	assert.Equal(t, HealthStatusUnhealthy, overall.Components["rest"].Status)
	assert.NotZero(t, overall.Timestamp)
}

// =============================================================================
// file_kv_store.go — error paths and directory-filtering branches
//
// The unit-tagged tests cover the happy path. These tests cover the
// subdir-skipping, tmp-file-skipping, and invalid-hex-skipping branches in
// Scan/List, plus the NewFileKVStore empty-dataDir error.
// =============================================================================

func TestNewFileKVStore_EmptyDataDirReturnsTypedError(t *testing.T) {
	store, err := NewFileKVStore("")
	require.Error(t, err)
	assert.Nil(t, store)
}

func TestFileKVStore_Scan_SkipsSubdirsAndInvalidEntries(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileKVStore(dir)
	require.NoError(t, err)
	ctx := context.Background()

	// Seed a real entry.
	require.NoError(t, store.Put(ctx, "real-key", []byte("v")))

	// Subdir under the data dir — Scan must skip it.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o750))

	// .tmp suffix — Scan must skip it.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "leftover.tmp"), []byte("x"), 0o600))

	// Invalid hex filename — Scan must skip it via filenameToKey error.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "not-hex!"), []byte("x"), 0o600))

	var got []string
	require.NoError(t, store.Scan(ctx, "", func(key string, value []byte) error {
		got = append(got, key)
		return nil
	}))
	assert.ElementsMatch(t, []string{"real-key"}, got)
}

func TestFileKVStore_List_SkipsSubdirsAndInvalidEntries(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileKVStore(dir)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "a-key", []byte("v")))
	require.NoError(t, store.Put(ctx, "b-key", []byte("v")))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "leftover.tmp"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "not-hex!"), []byte("x"), 0o600))

	keys, err := store.List(ctx, "")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a-key", "b-key"}, keys)
}

// =============================================================================
// TODO Phase F: PebbleKVStore was removed in Phase E (the dead embedded
// transport path that used it has been deleted). Add Phase F integration tests
// that assert the qrdb node's embedded engine handles on-disk persistence
// correctly across restart cycles.

// =============================================================================
// lifecycle.go — issueTLSCertFromExternalCA pre-issued cert path
//
// When WithTLSCertificate(...) is supplied the issuer short-circuits with the
// supplied cert and a minimal TLS config. No XKMS round-trip required.
// =============================================================================

func TestServer_IssueTLSCertFromExternalCA_PreIssuedCertShortCircuits(t *testing.T) {
	// Build a self-signed cert + key.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(42),
		Subject:               pkix.Name{CommonName: "issue-tls-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)

	cert := &tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
	}

	// Construct a server with WithTLSCertificate. The functional option
	// stores the cert on serverOptions; issueTLSCertFromExternalCA reads
	// from there and skips the issuance round-trip.
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	srv, err := NewServer(cfg, WithTLSCertificate(cert))
	require.NoError(t, err)

	require.NoError(t, srv.issueTLSCertFromExternalCA())
	require.NotNil(t, srv.tlsConfig)
	assert.Equal(t, uint16(tls.VersionTLS13), srv.tlsConfig.MinVersion)
	assert.Equal(t, []string{"h2", "http/1.1"}, srv.tlsConfig.NextProtos)
	require.Len(t, srv.tlsConfig.Certificates, 1)
}

// =============================================================================
// lifecycle.go — initBarrierIsolation guard branches
//
// Two early-return guards: (1) sealManager == nil → fall back; (2) the seal
// manager doesn't implement systemBarrierProvider → fall back. Both must
// leave the QuicBaaS facade untouched.
// =============================================================================

// TestServer_InitBarrierIsolation is removed: barrier isolation is now owned by the
// qrdb node's TenantServicer. The server no longer constructs a separate BarrierRegistry.

// =============================================================================
// lifecycle.go — stop* nil-manager guards
//
// Each stop helper returns nil when its field is unset. The Shutdown path
// already exercises this implicitly, but a direct test locks in the contract.
// =============================================================================

func TestServer_StopHelpers_NilFieldReturnsNil(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.workerManager)
	require.Nil(t, srv.backendManager)
	require.Nil(t, srv.catalogManager)
	require.Nil(t, srv.backupManager)

	assert.NoError(t, srv.stopWorkerManager())
	assert.NoError(t, srv.stopBackendManager())
	assert.NoError(t, srv.stopCatalogManager())
	assert.NoError(t, srv.stopBackupManager())
}

// =============================================================================
// server.go — Shutdown is idempotent
//
// shutdownOnce guarantees the body runs at most once. A second Shutdown call
// must return the same nil result without re-entering the cleanup.
// =============================================================================

func TestServer_Shutdown_IdempotentIAMVariant(t *testing.T) {
	srv := withTempServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, srv.Shutdown(ctx))
	require.True(t, srv.IsClosed())

	// Second call must return cleanly. The shutdownOnce.Do body short-
	// circuits on the s.closed flag.
	require.NoError(t, srv.Shutdown(ctx))
}

func TestServer_TriggerShutdown_ClosesShutdownChanIAMVariant(t *testing.T) {
	srv := withTempServer(t)
	srv.TriggerShutdown()
	select {
	case <-srv.shutdownChan:
		// Expected: chan was closed.
	default:
		t.Fatal("TriggerShutdown must close shutdownChan")
	}

	// A second TriggerShutdown call must NOT panic on the already-closed chan.
	assert.NotPanics(t, func() { srv.TriggerShutdown() })
}

func TestServer_NewServer_NilConfigReturnsTypedErrorIAMVariant(t *testing.T) {
	srv, err := NewServer(nil)
	require.Error(t, err)
	assert.Nil(t, srv)
}

// =============================================================================
// server.go — Uptime and IsStarted / IsClosed flags
// =============================================================================

func TestServer_StateFlagsIAMVariant(t *testing.T) {
	srv := withTempServer(t)
	assert.False(t, srv.IsStarted())
	assert.False(t, srv.IsClosed())
	assert.GreaterOrEqual(t, srv.Uptime(), time.Duration(0))
}

// =============================================================================
// orchestration_workflow_persistor.go — LoadAll
// =============================================================================

func TestKVWorkflowPersistor_LoadAllRoundTrip(t *testing.T) {
	srv := withTempServer(t)
	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init must succeed")
	t.Cleanup(func() { _ = srv.stopQRDBNode() })
	client := srv.qrdbNode.Client()
	require.NotNil(t, client)

	p := newKVWorkflowPersistor(client, 1)
	ctx := context.Background()

	require.NoError(t, p.Save(ctx, &orchestration.WorkflowDefinition{Name: "wf-load-A"}))

	got, err := p.LoadAll(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(got), 1)
}

// =============================================================================
// lifecycle_remote_resolver.go — newRemoteBackendPool
// =============================================================================

func TestNewRemoteBackendPool_StoresFields(t *testing.T) {
	pool := newRemoteBackendPool(nil, nil, "https://central:8443/oauth2/token", "/path/to/ca.crt", config.SchedulerHostAffinityConfig{}, logger.NewAdapter(mustCreateLogger()))
	require.NotNil(t, pool)
	assert.Equal(t, "https://central:8443/oauth2/token", pool.tokenURL)
	assert.Equal(t, "/path/to/ca.crt", pool.caCertPath)
	assert.NotNil(t, pool.entries)
}

// =============================================================================
// centralOAuth2TokenSource — token caching on second call
//
// The token source caches an unexpired token. A second Token() returns the
// cached value without re-hitting the endpoint.
// =============================================================================

func TestCentralOAuth2TokenSource_CachedTokenSkipsRefresh(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"cached.token","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)

	ts := newCentralOAuth2TokenSource(&OrchestratorDaemonClient{
		ClientID: "cid", ClientSecret: "csec",
	}, srv.URL, "")
	ts.loopback = false

	first, err := ts.Token()
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := ts.Token()
	require.NoError(t, err)
	assert.Same(t, first, second, "cached unexpired token must be returned")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, hits, "endpoint must be hit exactly once")
}

// =============================================================================
// iam_init.go — registerIAMService early-return guards
// =============================================================================

func TestServer_RegisterIAMService_NilServiceReturnsCleanly(t *testing.T) {
	srv := withTempServer(t)
	require.Nil(t, srv.quicBaaSService)
	gs := ggrpc.NewServer()
	t.Cleanup(gs.Stop)
	assert.NotPanics(t, func() { srv.registerIAMService(gs) })
}

// =============================================================================
// iam_init.go — iamComponents getter passthroughs (non-nil branches)
// =============================================================================

func TestIAMComponents_GetSessionStorage_NonNilIAMVariant(t *testing.T) {
	c := &iamComponents{sessionStorage: iam.NewMemorySessionStorage()}
	got := c.GetSessionStorage()
	assert.NotNil(t, got)
}

func TestIAMComponents_GetAuditLogger_NonNilIAMVariant(t *testing.T) {
	al, err := iam.NewAuditLogger()
	require.NoError(t, err)
	c := &iamComponents{auditLogger: al}
	assert.NotNil(t, c.GetAuditLogger())
}

func TestIAMComponents_GetWebAuthnManager_NilWhenUnset(t *testing.T) {
	c := &iamComponents{}
	got := c.GetWebAuthnManager()
	assert.Nil(t, got, "GetWebAuthnManager must return nil when no manager is attached")
}

// =============================================================================
// iam_init.go — ensureOrchestratorDaemonClient cached path
//
// When the on-disk cache exists with valid credentials, the function returns
// the cached client without contacting AppManager.
// =============================================================================

func TestServer_EnsureOrchestratorDaemonClient_ReturnsCachedClient(t *testing.T) {
	srv := withTempServer(t)
	cacheDir := srv.config.Storage.DataDir
	require.NoError(t, os.MkdirAll(cacheDir, 0o700))
	cachePath := filepath.Join(cacheDir, "orchestrator_daemon_client.json")

	cached := &OrchestratorDaemonClient{
		ClientID:     "cached-id",
		ClientSecret: "cached-secret",
		Scopes:       []string{"scope.a"},
		TenantID:     "default",
	}
	require.NoError(t, writeOrchestratorDaemonClient(cachePath, cached))

	got, err := srv.ensureOrchestratorDaemonClient(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, cached.ClientID, got.ClientID)
	assert.Equal(t, cached.ClientSecret, got.ClientSecret)
	assert.Equal(t, cached.TenantID, got.TenantID)
}

// =============================================================================
// iam_init.go — reconcileAdminRole guard (verify-only against qrdb's canonical
// admin role; see iam_init_test.go for the primary suite)
// =============================================================================

func TestReconcileAdminRole_NilStore(t *testing.T) {
	assert.NoError(t, reconcileAdminRole(context.Background(), nil))
}

func TestReconcileAdminRole_PresentAfterSeed(t *testing.T) {
	c := buildTestIAMComponents(t)
	ctx := context.Background()

	// Seed the canonical admin role (as qrdb's first-boot would) and confirm
	// reconcile is a clean no-op when it is present.
	_, err := c.identityStore.CreateRole(ctx, AdminTenantID, iam.CreateRoleOptions{
		ID:          AdminRoleID,
		Name:        "admin",
		Permissions: []string{"*:*"},
	})
	require.NoError(t, err)

	require.NoError(t, reconcileAdminRole(ctx, c.identityStore))

	role, err := c.identityStore.GetRole(ctx, AdminTenantID, AdminRoleID)
	require.NoError(t, err)
	assert.Equal(t, AdminRoleID, role.ID)
}

// =============================================================================
// ceremony_wiring.go — SOCertSPKIThumbprint error branches
// =============================================================================

func TestSOCertSPKIThumbprint_EmptyInputReturnsSentinel(t *testing.T) {
	_, err := SOCertSPKIThumbprint(nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSOCertEmpty)
}

func TestSOCertSPKIThumbprint_NoPEMBlockReturnsSentinel(t *testing.T) {
	_, err := SOCertSPKIThumbprint([]byte("not a pem block"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSOCertNoPEMBlock)
}

func TestSOCertSPKIThumbprint_ValidCertReturnsThumbprint(t *testing.T) {
	cert := newTestSelfSignedCertPEM(t)
	thumb, err := SOCertSPKIThumbprint(cert)
	require.NoError(t, err)
	assert.NotEmpty(t, thumb)
}

// =============================================================================
// helpers
// =============================================================================

// newTestSelfSignedCertPEM produces a real PEM-encoded self-signed X.509
// certificate suitable for SOCertSPKIThumbprint testing. Generated fresh
// per test to keep the test hermetic.
func newTestSelfSignedCertPEM(t *testing.T) []byte {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "spki-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// TestServer_EnsureOrchestratorDaemonClient_WriteCacheError covers the
// persist-cache error branch of ensureOrchestratorDaemonClient: when the data
// directory is a regular file, writeOrchestratorDaemonClient's MkdirAll fails
// and ensureOrchestratorDaemonClient surfaces a "persist cache" error after
// successfully registering the app.
func TestServer_EnsureOrchestratorDaemonClient_WriteCacheError(t *testing.T) {
	srv := withTempServer(t)

	// Make the data directory a regular FILE so both the cache read (non-ENOENT
	// error → re-bootstrap) and the subsequent cache write (MkdirAll over a file)
	// fail. The read failure routes through RegisterApp; the write failure is the
	// branch under test.
	base := t.TempDir()
	dataFile := filepath.Join(base, "datadir-as-file")
	require.NoError(t, os.WriteFile(dataFile, []byte("x"), 0o600))
	srv.config.Storage.DataDir = dataFile

	// Build a real AppManager so RegisterApp succeeds before the cache write.
	memStorage := iam.NewMemoryStorage()
	tenantManager := iam.NewDefaultTenantManager(memStorage)
	_, err := tenantManager.CreateTenant(context.Background(), iam.CreateTenantOptions{
		ID: "default", Name: "Default",
	})
	require.NoError(t, err)
	appManager := iam.NewAppManager(memStorage, tenantManager)

	_, err = srv.ensureOrchestratorDaemonClient(context.Background(), appManager)
	require.Error(t, err, "an unwritable cache directory must fail ensureOrchestratorDaemonClient")
	require.Contains(t, err.Error(), "persist cache")
}
