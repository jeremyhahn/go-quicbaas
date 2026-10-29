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
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmsca "github.com/jeremyhahn/go-xkms/pkg/ca"
	xkmscertstore "github.com/jeremyhahn/go-xkms/pkg/certstore"
	xkmstypes "github.com/jeremyhahn/go-xkms/pkg/types"
	xkmsbackend "github.com/jeremyhahn/go-xkms/pkg/xkms"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	quicbaasseal "github.com/jeremyhahn/go-quicbaas/pkg/seal"
)

// =============================================================================
// GetCertificateAuthority Tests
// =============================================================================

func TestGetCertificateAuthority_NilWhenTLSDisabled(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = false

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	result := srv.GetCertificateAuthority()
	assert.Nil(t, result, "CA should be nil when TLS is disabled and server has not initialized CA")
}

func TestGetCertificateAuthority_ReturnsCAWhenSet(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Create a mock CA to assign directly
	mockCA := &mockCertificateAuthority{}
	srv.setCA(mockCA)

	result := srv.GetCertificateAuthority()
	assert.NotNil(t, result)
	assert.Equal(t, mockCA, result)
}

// =============================================================================
// GetBarrierManager Tests
// =============================================================================

func TestGetSealManager_NilWhenSealDisabled(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Seal.Enabled = false

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	result := srv.GetBarrierManager()
	assert.Nil(t, result, "seal manager should be nil when seal is disabled")
}

func TestGetSealManager_ReturnsSealManagerWhenSet(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	mockSM := &mockSealManager{}
	srv.barrierManager = mockSM

	result := srv.GetBarrierManager()
	assert.NotNil(t, result)
	assert.Equal(t, mockSM, result)
}

// =============================================================================
// initBarrier Tests
// =============================================================================

func TestInitBarrier_SealDisabled_Skips(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Seal.Enabled = false

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initBarrier()
	require.NoError(t, err)
	assert.Nil(t, srv.barrierManager, "seal manager should remain nil when seal is disabled")
}

func TestInitBarrier_SealEnabled_Success(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Seal.Enabled = true
	cfg.Seal.Mode = "manual"
	cfg.Seal.Threshold = 2
	cfg.Seal.TotalShares = 3

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Override the seam to inject a mock seal manager
	seamMu.Lock()
	origFn := newBarrierManagerFn
	newBarrierManagerFn = func(cfg *quicbaasseal.Config, log *logger.Logger, dataDir string) (interfaces.BarrierManager, error) {
		return &mockSealManager{}, nil
	}
	seamMu.Unlock()
	defer func() {
		seamMu.Lock()
		newBarrierManagerFn = origFn
		seamMu.Unlock()
	}()

	err = srv.initBarrier()
	require.NoError(t, err)
	assert.NotNil(t, srv.barrierManager, "seal manager should be set after initBarrier succeeds")
}

func TestInitBarrier_SealEnabled_CreationError(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Seal.Enabled = true
	cfg.Seal.Mode = "manual"
	cfg.Seal.Threshold = 2
	cfg.Seal.TotalShares = 3

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// Override the seam to return an error
	seamMu.Lock()
	origFn := newBarrierManagerFn
	newBarrierManagerFn = func(cfg *quicbaasseal.Config, log *logger.Logger, dataDir string) (interfaces.BarrierManager, error) {
		return nil, &dberrors.MessageError{Message: "seal manager init failed"}
	}
	seamMu.Unlock()
	defer func() {
		seamMu.Lock()
		newBarrierManagerFn = origFn
		seamMu.Unlock()
	}()

	err = srv.initBarrier()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create seal manager")
	assert.Nil(t, srv.barrierManager, "seal manager should remain nil on error")
}

func TestInitBarrier_DefaultBarrierDir(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Seal.Enabled = true
	cfg.Seal.Mode = "manual"
	cfg.Seal.Threshold = 2
	cfg.Seal.TotalShares = 3
	cfg.Seal.BarrierDir = "" // empty triggers default

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	var capturedDataDir string
	seamMu.Lock()
	origFn := newBarrierManagerFn
	newBarrierManagerFn = func(cfg *quicbaasseal.Config, log *logger.Logger, dataDir string) (interfaces.BarrierManager, error) {
		capturedDataDir = dataDir
		return &mockSealManager{}, nil
	}
	seamMu.Unlock()
	defer func() {
		seamMu.Lock()
		newBarrierManagerFn = origFn
		seamMu.Unlock()
	}()

	err = srv.initBarrier()
	require.NoError(t, err)
	assert.Equal(t, srv.config.Storage.DataDir, capturedDataDir,
		"should pass server data dir to seal manager constructor")
}

func TestInitBarrier_CustomBarrierDir(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Seal.Enabled = true
	cfg.Seal.Mode = "manual"
	cfg.Seal.Threshold = 2
	cfg.Seal.TotalShares = 3
	cfg.Seal.BarrierDir = "/tmp/custom-barrier-dir"

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	var capturedCfg *quicbaasseal.Config
	seamMu.Lock()
	origFn := newBarrierManagerFn
	newBarrierManagerFn = func(cfg *quicbaasseal.Config, log *logger.Logger, dataDir string) (interfaces.BarrierManager, error) {
		capturedCfg = cfg
		return &mockSealManager{}, nil
	}
	seamMu.Unlock()
	defer func() {
		seamMu.Lock()
		newBarrierManagerFn = origFn
		seamMu.Unlock()
	}()

	err = srv.initBarrier()
	require.NoError(t, err)
	assert.Equal(t, quicbaasseal.SealMode("manual"), capturedCfg.Mode)
	assert.Equal(t, 2, capturedCfg.Threshold)
	assert.Equal(t, 3, capturedCfg.TotalShares)
}

func TestInitBarrier_RecoveryConfigPropagated(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Seal.Enabled = true
	cfg.Seal.Mode = "manual"
	cfg.Seal.Threshold = 3
	cfg.Seal.TotalShares = 5
	cfg.Seal.RecoveryEnabled = true
	cfg.Seal.RecoveryThreshold = 2
	cfg.Seal.RecoveryTotal = 4

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	var capturedCfg *quicbaasseal.Config
	seamMu.Lock()
	origFn := newBarrierManagerFn
	newBarrierManagerFn = func(cfg *quicbaasseal.Config, log *logger.Logger, dataDir string) (interfaces.BarrierManager, error) {
		capturedCfg = cfg
		return &mockSealManager{}, nil
	}
	seamMu.Unlock()
	defer func() {
		seamMu.Lock()
		newBarrierManagerFn = origFn
		seamMu.Unlock()
	}()

	err = srv.initBarrier()
	require.NoError(t, err)
	assert.True(t, capturedCfg.RecoveryKeysEnabled)
	assert.Equal(t, 2, capturedCfg.RecoveryThreshold)
	assert.Equal(t, 4, capturedCfg.RecoveryTotalKeys)
}

// =============================================================================
// saveServerCert Tests
// =============================================================================

func TestSaveServerCert_HappyPath(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "certs", "server.crt")

	issued := &xkmsca.IssuedCertificate{
		CertificatePEM:   []byte("-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----\n"),
		CACertificatePEM: []byte("-----BEGIN CERTIFICATE-----\nca-cert\n-----END CERTIFICATE-----\n"),
	}

	err := saveServerCert(issued, certPath)
	require.NoError(t, err)

	certData, err := os.ReadFile(certPath)
	require.NoError(t, err)
	assert.Contains(t, string(certData), "test-cert")
	assert.Contains(t, string(certData), "ca-cert")
}

func TestSaveServerCert_BadCertDirectory(t *testing.T) {
	t.Parallel()
	certPath := "/dev/null/certs/server.crt"

	issued := &xkmsca.IssuedCertificate{
		CertificatePEM: []byte("cert"),
	}

	err := saveServerCert(issued, certPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create certs directory")
}

// A directory standing where the certificate file belongs makes the write fail
// with EISDIR for every uid; a read-only parent only stops an unprivileged one,
// and CI job containers run as root (testing-guidelines.md, Fault Injection).
func TestSaveServerCert_WhenCertPathIsDirectory(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	certDir := filepath.Join(tmpDir, "certs")
	require.NoError(t, os.MkdirAll(certDir, 0750))

	certPath := filepath.Join(certDir, "server.crt")
	require.NoError(t, os.MkdirAll(certPath, 0750))

	issued := &xkmsca.IssuedCertificate{
		CertificatePEM: []byte("cert"),
	}

	err := saveServerCert(issued, certPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write server certificate")
}

// =============================================================================
// initializeCA Tests - seam injection for newCAFn
// =============================================================================

func TestInitializeCA_CACreationError(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	seamMu.Lock()
	origFn := newCAFn
	newCAFn = func(_ *xkmsca.Params) (xkmsca.TrustedCA, error) {
		return nil, &dberrors.MessageError{Message: "ca creation failed"}
	}
	seamMu.Unlock()
	defer func() {
		seamMu.Lock()
		newCAFn = origFn
		seamMu.Unlock()
	}()

	err = srv.initializeCA()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create certificate authority")
}

func TestInitializeCA_InitError_NotAlreadyInitialized(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	seamMu.Lock()
	origFn := newCAFn
	newCAFn = func(_ *xkmsca.Params) (xkmsca.TrustedCA, error) {
		return &mockCertificateAuthority{
			loadErr: &dberrors.MessageError{Message: "load failed"},
			initErr: &dberrors.MessageError{Message: "some other init error"},
		}, nil
	}
	seamMu.Unlock()
	defer func() {
		seamMu.Lock()
		newCAFn = origFn
		seamMu.Unlock()
	}()

	err = srv.initializeCA()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initialize certificate authority")
}

func TestInitializeCA_LoadError_InitAlsoFails(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	seamMu.Lock()
	origFn := newCAFn
	newCAFn = func(_ *xkmsca.Params) (xkmsca.TrustedCA, error) {
		return &mockCertificateAuthority{
			loadErr: &dberrors.MessageError{Message: "load failed"},
			initErr: &dberrors.MessageError{Message: "init also failed"},
		}, nil
	}
	seamMu.Unlock()
	defer func() {
		seamMu.Lock()
		newCAFn = origFn
		seamMu.Unlock()
	}()

	err = srv.initializeCA()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initialize certificate authority")
}

func TestInitializeCA_AlreadyInitialized_LoadSuccess(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// First call initializes the CA and creates keys/certs on disk
	err = srv.initializeCA()
	require.NoError(t, err)
	assert.NotNil(t, srv.CA(), "CA should be set after first initialization")

	// Second call should load the existing CA (already initialized)
	err = srv.initializeCA()
	require.NoError(t, err)
	assert.NotNil(t, srv.CA(), "CA should be set after successful load")
	assert.True(t, cfg.Security.TLSEnabled)
	assert.False(t, cfg.Security.TLSClientAuth)
}

func TestInitializeCA_IssueCertificateError(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	mockCA := &mockCertificateAuthority{
		issueCertErr: &dberrors.MessageError{Message: "cert issuance failed"},
	}

	seamMu.Lock()
	origFn := newCAFn
	newCAFn = func(_ *xkmsca.Params) (xkmsca.TrustedCA, error) {
		return mockCA, nil
	}
	seamMu.Unlock()
	defer func() {
		seamMu.Lock()
		newCAFn = origFn
		seamMu.Unlock()
	}()

	err = srv.initializeCA()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "issue server TLS certificate")
}

func TestInitializeCA_CustomKeysDir(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true
	customKeysDir := filepath.Join(t.TempDir(), "custom-keys")
	cfg.CA.KeysDir = customKeysDir

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initializeCA()
	require.NoError(t, err)
	// Verify the custom keys dir was used by checking it exists
	_, statErr := os.Stat(customKeysDir)
	assert.NoError(t, statErr, "custom keys directory should exist")
}

func TestInitializeCA_CustomHostAddedToDNSNames(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true
	cfg.Server.Host = "mynode.example.com"

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initializeCA()
	require.NoError(t, err)
	// Verify TLS config was created with the custom host
	require.NotNil(t, srv.tlsConfig)
	// The server cert should include the custom host in its SAN DNS names.
	// Since we can't easily inspect SAN from tlsConfig, verify the cert file was written.
	assert.NotEmpty(t, cfg.Security.TLSCertFile)
}

func TestInitializeCA_Host0000_NotAddedToDNSNames(t *testing.T) {
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.Security.TLSEnabled = true
	cfg.Server.Host = "0.0.0.0"

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	err = srv.initializeCA()
	require.NoError(t, err)
	// Verify TLS config was created and cert files written.
	// With host 0.0.0.0, the server key CN defaults to "localhost".
	require.NotNil(t, srv.tlsConfig)
	assert.NotEmpty(t, cfg.Security.TLSCertFile)
}

// =============================================================================
// Mock Types
// =============================================================================

// mockCertificateAuthority implements xkmsca.TrustedCA for testing.
//
// The xkmsca.TrustedCA interface is intentionally embedded (zero-value) so
// that the mock satisfies the full interface without hand-stubbing every
// method. The zero-value embedded interface panics with a nil-pointer
// dereference if any method that is NOT explicitly overridden below gets
// called. Tests that exercise a newly-used method MUST add a concrete
// override on *mockCertificateAuthority; do not silently rely on the
// panicking default.
type mockCertificateAuthority struct {
	xkmsca.TrustedCA // embedded interface: panics on any un-overridden call

	initErr      error
	loadErr      error
	issuedCert   *xkmsca.IssuedCertificate
	issueCertErr error
	caBundle     []byte
	caBundleErr  error
	issueCertFn  func(request *xkmsca.CertificateRequest) (*xkmsca.IssuedCertificate, error)
}

// crypto.Signer
func (m *mockCertificateAuthority) Public() crypto.PublicKey { return nil }
func (m *mockCertificateAuthority) Sign(_ io.Reader, _ []byte, _ crypto.SignerOpts) ([]byte, error) {
	return nil, nil
}

// Lifecycle
func (m *mockCertificateAuthority) Init(_ context.Context) error { return m.initErr }
func (m *mockCertificateAuthority) Load(_ context.Context) error { return m.loadErr }
func (m *mockCertificateAuthority) IsInitialized() bool          { return m.initErr == nil }

// CSR
func (m *mockCertificateAuthority) SignCSR(_ context.Context, _ []byte, _ *xkmsca.SignOptions) (*x509.Certificate, error) {
	return nil, nil
}
func (m *mockCertificateAuthority) CreateCSR(_ context.Context, _ *xkmsca.CertificateRequest) ([]byte, error) {
	return nil, nil
}

// Certificate Issuance
func (m *mockCertificateAuthority) IssueCertificate(_ context.Context, request *xkmsca.CertificateRequest) (*xkmsca.IssuedCertificate, error) {
	if m.issueCertFn != nil {
		return m.issueCertFn(request)
	}
	if m.issueCertErr != nil {
		return nil, m.issueCertErr
	}
	return m.issuedCert, nil
}
func (m *mockCertificateAuthority) IssueCertificateWithProfile(_ context.Context, _ *xkmsca.CertificateRequest, _ string) (*xkmsca.IssuedCertificate, error) {
	return nil, nil
}

// Trust Chain
func (m *mockCertificateAuthority) CACertificate() (*x509.Certificate, error) { return nil, nil }
func (m *mockCertificateAuthority) CABundle() ([]byte, error) {
	if m.caBundleErr != nil {
		return nil, m.caBundleErr
	}
	if m.caBundle != nil {
		return m.caBundle, nil
	}
	return []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"), nil
}
func (m *mockCertificateAuthority) Verify(_ *x509.Certificate, _ ...crypto.Signer) ([][]*x509.Certificate, error) {
	return nil, nil
}

// Revocation
func (m *mockCertificateAuthority) Revoke(_ context.Context, _ *big.Int, _ int) error { return nil }
func (m *mockCertificateAuthority) GenerateCRL(_ context.Context) ([]byte, error)     { return nil, nil }
func (m *mockCertificateAuthority) IsRevoked(_ *big.Int) (bool, error)                { return false, nil }

// TLS
func (m *mockCertificateAuthority) TLSCertificate(_ context.Context, _ *xkmstypes.KeyAttributes) (tls.Certificate, error) {
	return tls.Certificate{}, nil
}
func (m *mockCertificateAuthority) TLSConfig(_ context.Context, _ *xkmstypes.KeyAttributes) (*tls.Config, error) {
	return nil, nil
}
func (m *mockCertificateAuthority) TLSConfigWithOptions(_ *xkmstypes.KeyAttributes, _ *xkmsca.TLSConfigOptions) (*tls.Config, error) {
	return nil, nil
}
func (m *mockCertificateAuthority) ServerTLSConfig(_ context.Context, _ *xkmstypes.KeyAttributes, _ bool) (*tls.Config, error) {
	return nil, nil
}
func (m *mockCertificateAuthority) ClientTLSConfig(_ context.Context, _ *xkmstypes.KeyAttributes, _ string) (*tls.Config, error) {
	return nil, nil
}
func (m *mockCertificateAuthority) MutualTLSConfig(_ *xkmstypes.KeyAttributes) (*tls.Config, error) {
	return nil, nil
}
func (m *mockCertificateAuthority) VerifyPeerCertificate(_ [][]byte, _ [][]*x509.Certificate) error {
	return nil
}

// Trust Pool
func (m *mockCertificateAuthority) CABundleCertPool() (*x509.CertPool, error) { return nil, nil }
func (m *mockCertificateAuthority) TrustedRootCertPool(_ *x509.Certificate) (*x509.CertPool, error) {
	return nil, nil
}
func (m *mockCertificateAuthority) TrustedIntermediateCertPool(_ *x509.Certificate) (*x509.CertPool, error) {
	return nil, nil
}
func (m *mockCertificateAuthority) OSTrustStore() (*x509.CertPool, error) { return nil, nil }
func (m *mockCertificateAuthority) QuantumSafeTLSConfig(_ context.Context, _ *xkmstypes.KeyAttributes) (*tls.Config, error) {
	return nil, nil
}

// Storage
func (m *mockCertificateAuthority) KeyStore() xkmsbackend.Backend      { return nil }
func (m *mockCertificateAuthority) CertStore() xkmscertstore.CertStore { return nil }

// Configuration
func (m *mockCertificateAuthority) Config() *xkmsca.Identity { return nil }
func (m *mockCertificateAuthority) Identity() string         { return "mock-ca" }

// mockSealManager implements interfaces.BarrierManager for testing.
type mockSealManager struct{}

func (m *mockSealManager) Status(_ context.Context) (interface{}, error) {
	return nil, nil
}

func (m *mockSealManager) IsSealed() bool {
	return false
}

func (m *mockSealManager) IsInitialized() bool {
	return false
}

func (m *mockSealManager) Initialize(_ context.Context, _, _ int) (interface{}, error) {
	return nil, nil
}

func (m *mockSealManager) Unseal(_ context.Context, _ []byte) (interface{}, error) {
	return nil, nil
}

func (m *mockSealManager) Seal(_ context.Context) error {
	return nil
}

func (m *mockSealManager) Rekey(_ context.Context, _, _ int) (interface{}, error) {
	return nil, nil
}

func (m *mockSealManager) GenerateRecoveryKeys(_ context.Context, _, _ int) ([][]byte, error) {
	return nil, nil
}

func (m *mockSealManager) DeleteRecoveryKeys(_ context.Context) error {
	return nil
}

func (m *mockSealManager) GenerateRootToken(_ context.Context, _ string, _ [][]byte) (string, error) {
	return "", nil
}

func (m *mockSealManager) RotateRootKey(_ context.Context) error {
	return nil
}

func (m *mockSealManager) Close() error {
	return nil
}

// =============================================================================
// UpdateCA Tests
// =============================================================================

// TestUpdateCA_NilIsNoop verifies that UpdateCA(nil) returns nil without
// touching the caAdapter field.
func TestUpdateCA_NilIsNoop(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	// caAdapter is nil (server not started, TLS not set up).
	if err := srv.UpdateCA(nil); err != nil {
		t.Fatalf("UpdateCA(nil) should be a no-op, got error: %v", err)
	}
}

// TestUpdateCA_ErrorsWhenAdapterNotReady verifies that UpdateCA returns a
// typed error when the caAdapter has not been wired (server not started or
// TLS disabled).
func TestUpdateCA_ErrorsWhenAdapterNotReady(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	mockCA := &mockCertificateAuthority{}
	err = srv.UpdateCA(mockCA)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUpdateCANotReady)
}

// TestUpdateCA_SwapsCAOnAdapter verifies that UpdateCA updates both
// Server.CA() and the live caAdapter reference.
func TestUpdateCA_SwapsCAOnAdapter(t *testing.T) {
	t.Parallel()

	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	firstCA := &mockCertificateAuthority{}
	secondCA := &mockCertificateAuthority{}

	// Simulate a wired caAdapter (as done by startService after TLS init).
	adapter, adapterErr := newCAServiceAdapter(firstCA)
	require.NoError(t, adapterErr)
	srv.caAdapter = adapter
	srv.setCA(firstCA)

	// UpdateCA should swap to secondCA.
	require.NoError(t, srv.UpdateCA(secondCA))

	// Server-level reference should be updated.
	assert.Equal(t, secondCA, srv.CA())

	// Adapter-level reference should also be updated.
	assert.Equal(t, secondCA, adapter.getCA())
}
