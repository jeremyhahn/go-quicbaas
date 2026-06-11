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

package grpc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	xkmspb "github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// mockKeyProvider implements xkmssdk.KeyProvider for testing.
type mockKeyProvider struct {
	backendType  xkmssdk.BackendType
	capabilities xkmssdk.Capabilities
	keys         map[string]crypto.PrivateKey
	keyAttrs     []*xkmssdk.KeyAttributes
}

func newMockKeyProvider() *mockKeyProvider {
	return &mockKeyProvider{
		backendType: xkmssdk.BackendTypeSoftware,
		capabilities: xkmssdk.Capabilities{
			Keys:        true,
			Signing:     true,
			Decryption:  true,
			KeyRotation: true,
			Sealing:     true,
		},
		keys: make(map[string]crypto.PrivateKey),
	}
}

func (m *mockKeyProvider) Type() xkmssdk.BackendType          { return m.backendType }
func (m *mockKeyProvider) Capabilities() xkmssdk.Capabilities { return m.capabilities }

func (m *mockKeyProvider) GenerateKey(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	return nil, &XKMSKeyNotFoundError{KeyID: "not-implemented"}
}

func (m *mockKeyProvider) GetKey(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	if key, ok := m.keys[attrs.CN]; ok {
		return key, nil
	}
	return nil, &XKMSKeyNotFoundError{KeyID: attrs.CN}
}

func (m *mockKeyProvider) DeleteKey(_ context.Context, attrs *xkmssdk.KeyAttributes) error {
	return nil
}

func (m *mockKeyProvider) ListKeys(_ context.Context) ([]*xkmssdk.KeyAttributes, error) {
	return m.keyAttrs, nil
}

func (m *mockKeyProvider) Signer(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Signer, error) {
	key, err := m.GetKey(context.Background(), attrs)
	if err != nil {
		return nil, err
	}
	if signer, ok := key.(crypto.Signer); ok {
		return signer, nil
	}
	return nil, &XKMSKeyNotFoundError{KeyID: attrs.CN}
}

func (m *mockKeyProvider) Decrypter(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Decrypter, error) {
	return nil, &XKMSKeyNotFoundError{KeyID: "not-implemented"}
}

func (m *mockKeyProvider) RotateKey(_ context.Context, attrs *xkmssdk.KeyAttributes) error {
	return nil
}

func (m *mockKeyProvider) Close() error {
	return nil
}

// mockCertStorage implements xkmssdk.CertificateStorageAdapter for testing.
type mockCertStorage struct {
	certs map[string]*x509.Certificate
}

func newMockCertStorage() *mockCertStorage {
	return &mockCertStorage{
		certs: make(map[string]*x509.Certificate),
	}
}

func (m *mockCertStorage) SaveCert(_ context.Context, id string, cert *x509.Certificate) error {
	m.certs[id] = cert
	return nil
}

func (m *mockCertStorage) GetCert(_ context.Context, id string) (*x509.Certificate, error) {
	cert, ok := m.certs[id]
	if !ok {
		return nil, &XKMSKeyNotFoundError{KeyID: id}
	}
	return cert, nil
}

func (m *mockCertStorage) DeleteCert(_ context.Context, id string) error {
	delete(m.certs, id)
	return nil
}

func (m *mockCertStorage) SaveCertChain(_ context.Context, id string, chain []*x509.Certificate) error {
	return nil
}

func (m *mockCertStorage) GetCertChain(_ context.Context, id string) ([]*x509.Certificate, error) {
	return nil, nil
}

func (m *mockCertStorage) ListCerts(_ context.Context) ([]string, error) {
	ids := make([]string, 0, len(m.certs))
	for id := range m.certs {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockCertStorage) CertExists(_ context.Context, id string) (bool, error) {
	_, ok := m.certs[id]
	return ok, nil
}

func (m *mockCertStorage) Close() error {
	return nil
}

// mockXKMSBackend implements xkmssdk.ServiceBackend for testing.
// Function override fields allow tests to customize behavior per-algorithm.
type mockXKMSBackend struct {
	keyProvider         *mockKeyProvider
	certStore           *mockCertStorage
	generateRSAFunc     func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error)
	generateECDSAFunc   func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error)
	generateEd25519Func func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error)
	rotateKeyFunc       func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error)
}

func newMockXKMSBackend() *mockXKMSBackend {
	return &mockXKMSBackend{
		keyProvider: newMockKeyProvider(),
		certStore:   newMockCertStorage(),
	}
}

func (m *mockXKMSBackend) GenerateRSA(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	if m.generateRSAFunc != nil {
		return m.generateRSAFunc(attrs)
	}
	return nil, nil
}
func (m *mockXKMSBackend) GenerateECDSA(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	if m.generateECDSAFunc != nil {
		return m.generateECDSAFunc(attrs)
	}
	return nil, nil
}
func (m *mockXKMSBackend) GenerateEd25519(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	if m.generateEd25519Func != nil {
		return m.generateEd25519Func(attrs)
	}
	return nil, nil
}
func (m *mockXKMSBackend) GetKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	return m.keyProvider.GetKey(ctx, attrs)
}
func (m *mockXKMSBackend) DeleteKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) error {
	return nil
}
func (m *mockXKMSBackend) ListKeys(ctx context.Context) ([]*xkmssdk.KeyAttributes, error) {
	return m.keyProvider.ListKeys(ctx)
}
func (m *mockXKMSBackend) RotateKey(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	if m.rotateKeyFunc != nil {
		return m.rotateKeyFunc(attrs)
	}
	return nil, nil
}
func (m *mockXKMSBackend) Signer(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Signer, error) {
	return m.keyProvider.Signer(ctx, attrs)
}
func (m *mockXKMSBackend) Decrypter(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Decrypter, error) {
	return nil, nil
}
func (m *mockXKMSBackend) SaveCert(ctx context.Context, keyID string, cert *x509.Certificate) error {
	return m.certStore.SaveCert(ctx, keyID, cert)
}
func (m *mockXKMSBackend) GetCert(ctx context.Context, keyID string) (*x509.Certificate, error) {
	return m.certStore.GetCert(ctx, keyID)
}
func (m *mockXKMSBackend) DeleteCert(ctx context.Context, keyID string) error {
	return m.certStore.DeleteCert(ctx, keyID)
}
func (m *mockXKMSBackend) SaveCertChain(_ context.Context, keyID string, chain []*x509.Certificate) error {
	return nil
}
func (m *mockXKMSBackend) GetCertChain(_ context.Context, keyID string) ([]*x509.Certificate, error) {
	return nil, nil
}
func (m *mockXKMSBackend) ListCerts(ctx context.Context) ([]string, error) {
	return m.certStore.ListCerts(ctx)
}
func (m *mockXKMSBackend) CertExists(ctx context.Context, keyID string) (bool, error) {
	return m.certStore.CertExists(ctx, keyID)
}
func (m *mockXKMSBackend) GetTLSCertificate(_ context.Context, keyID string, attrs *xkmssdk.KeyAttributes) (tls.Certificate, error) {
	return tls.Certificate{}, nil
}
func (m *mockXKMSBackend) GetKeyByID(_ context.Context, keyID string) (crypto.PrivateKey, error) {
	return nil, nil
}
func (m *mockXKMSBackend) GetSignerByID(_ context.Context, keyID string) (crypto.Signer, error) {
	return nil, nil
}
func (m *mockXKMSBackend) GetDecrypterByID(_ context.Context, keyID string) (crypto.Decrypter, error) {
	return nil, nil
}
func (m *mockXKMSBackend) Seal(ctx context.Context, data []byte, opts *xkmssdk.SealOptions) (*xkmssdk.SealedData, error) {
	return &xkmssdk.SealedData{
		Backend:    xkmssdk.BackendTypeSoftware,
		Ciphertext: data,
		Nonce:      []byte("test-nonce"),
		Tag:        []byte("test-tag"),
	}, nil
}
func (m *mockXKMSBackend) Unseal(ctx context.Context, sealed *xkmssdk.SealedData, opts *xkmssdk.UnsealOptions) ([]byte, error) {
	return sealed.Ciphertext, nil
}
func (m *mockXKMSBackend) CanSeal() bool {
	return true
}
func (m *mockXKMSBackend) KeyProvider() xkmssdk.KeyProvider {
	return m.keyProvider
}
func (m *mockXKMSBackend) CertStorage() xkmssdk.CertificateStorageAdapter {
	return m.certStore
}
func (m *mockXKMSBackend) Close() error {
	return nil
}

// Verify mockXKMSBackend implements xkmssdk.ServiceBackend at compile time.
var _ xkmssdk.ServiceBackend = (*mockXKMSBackend)(nil)

// newTestLogger creates a logger for unit tests.
func newTestLogger() *logger.Logger {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	return log
}

// newTestXKMSServer creates an XKMSServer for unit tests.
func newTestXKMSServer() *XKMSServer {
	return NewXKMSServer(newTestLogger())
}

// generateTestCertPEM creates a self-signed test certificate in PEM format.
func generateTestCertPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}))
}

// ===== Constructor and Registration Tests =====

func TestNewXKMSServer(t *testing.T) {
	t.Run("creates_server_with_logger", func(t *testing.T) {
		log := newTestLogger()
		server := NewXKMSServer(log)
		require.NotNil(t, server)
		assert.NotNil(t, server.logger)
	})
}

func TestRegisterXKMSServer(t *testing.T) {
	t.Run("registers_without_panic", func(t *testing.T) {
		grpcServer := grpc.NewServer()
		defer grpcServer.Stop()

		log := newTestLogger()
		assert.NotPanics(t, func() {
			RegisterXKMSServer(grpcServer, log)
		})
	})

	t.Run("registers_service_on_server", func(t *testing.T) {
		grpcServer := grpc.NewServer()
		defer grpcServer.Stop()

		log := newTestLogger()
		RegisterXKMSServer(grpcServer, log)

		info := grpcServer.GetServiceInfo()
		_, exists := info["xkms.v1.KeystoreService"]
		assert.True(t, exists, "xkms.v1.KeystoreService should be registered")
	})
}

// ===== XKMSKeyNotFoundError Tests =====

func TestXKMSKeyNotFoundError(t *testing.T) {
	t.Run("error_message_includes_key_id", func(t *testing.T) {
		err := &XKMSKeyNotFoundError{KeyID: "my-test-key"}
		assert.Equal(t, "xkms: key not found: my-test-key", err.Error())
	})

	t.Run("error_message_with_empty_key_id", func(t *testing.T) {
		err := &XKMSKeyNotFoundError{KeyID: ""}
		assert.Equal(t, "xkms: key not found: ", err.Error())
	})

	t.Run("implements_error_interface", func(t *testing.T) {
		var err error = &XKMSKeyNotFoundError{KeyID: "test"}
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "test")
	})
}

// ===== Helper Function Tests =====

func TestXKMSBackendDescription(t *testing.T) {
	tests := []struct {
		name        string
		backendType xkmssdk.BackendType
		expected    string
	}{
		{
			name:        "software_backend",
			backendType: xkmssdk.BackendTypeSoftware,
			expected:    "Software-based key storage",
		},
		{
			name:        "pkcs11_backend",
			backendType: xkmssdk.BackendTypePKCS11,
			expected:    "Hardware Security Module (PKCS#11)",
		},
		{
			name:        "tpm2_backend",
			backendType: xkmssdk.BackendTypeTPM2,
			expected:    "Trusted Platform Module 2.0",
		},
		{
			name:        "aws_kms_backend",
			backendType: xkmssdk.BackendTypeAWSKMS,
			expected:    "AWS Key Management Service",
		},
		{
			name:        "gcp_kms_backend",
			backendType: xkmssdk.BackendTypeGCPKMS,
			expected:    "Google Cloud Key Management Service",
		},
		{
			name:        "azure_kv_backend",
			backendType: xkmssdk.BackendTypeAzureKV,
			expected:    "Azure Key Vault",
		},
		{
			name:        "vault_backend",
			backendType: xkmssdk.BackendTypeVault,
			expected:    "HashiCorp Vault",
		},
		{
			name:        "symmetric_backend",
			backendType: xkmssdk.BackendTypeSymmetric,
			expected:    "Symmetric key storage",
		},
		{
			name:        "unknown_backend_returns_type_string",
			backendType: xkmssdk.BackendType("custom-provider"),
			expected:    "custom-provider",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := xkmsBackendDescription(tt.backendType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestXKMSParseHash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected crypto.Hash
	}{
		{name: "sha256_lowercase", input: "sha256", expected: crypto.SHA256},
		{name: "sha256_dash", input: "SHA-256", expected: crypto.SHA256},
		{name: "sha256_uppercase", input: "SHA256", expected: crypto.SHA256},
		{name: "sha384_lowercase", input: "sha384", expected: crypto.SHA384},
		{name: "sha384_dash", input: "SHA-384", expected: crypto.SHA384},
		{name: "sha384_uppercase", input: "SHA384", expected: crypto.SHA384},
		{name: "sha512_lowercase", input: "sha512", expected: crypto.SHA512},
		{name: "sha512_dash", input: "SHA-512", expected: crypto.SHA512},
		{name: "sha512_uppercase", input: "SHA512", expected: crypto.SHA512},
		{name: "empty_defaults_to_sha256", input: "", expected: crypto.SHA256},
		{name: "unknown_defaults_to_sha256", input: "md5", expected: crypto.SHA256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := xkmsParseHash(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestXKMSAlgorithmString(t *testing.T) {
	tests := []struct {
		name     string
		attrs    *xkmssdk.KeyAttributes
		expected string
	}{
		{
			name:     "rsa_algorithm",
			attrs:    &xkmssdk.KeyAttributes{KeyAlgorithm: x509.RSA},
			expected: "RSA",
		},
		{
			name:     "ecdsa_algorithm",
			attrs:    &xkmssdk.KeyAttributes{KeyAlgorithm: x509.ECDSA},
			expected: "ECDSA",
		},
		{
			name:     "ed25519_algorithm",
			attrs:    &xkmssdk.KeyAttributes{KeyAlgorithm: x509.Ed25519},
			expected: "Ed25519",
		},
		{
			name: "symmetric_algorithm",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:       0,
				SymmetricAlgorithm: xkmssdk.SymmetricAlgorithm("aes256-gcm"),
			},
			expected: "aes256-gcm",
		},
		{
			name: "unknown_algorithm_returns_key_type",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: 0,
				KeyType:      xkmssdk.KeyTypeSigning,
			},
			expected: string(xkmssdk.KeyTypeSigning),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := xkmsAlgorithmString(tt.attrs)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestXKMSExtractPublicKeyPEM(t *testing.T) {
	t.Run("rsa_key_returns_pem", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		pemStr := xkmsExtractPublicKeyPEM(key)
		assert.NotEmpty(t, pemStr)
		assert.Contains(t, pemStr, "PUBLIC KEY")

		block, _ := pem.Decode([]byte(pemStr))
		require.NotNil(t, block)
		pubKey, parseErr := x509.ParsePKIXPublicKey(block.Bytes)
		require.NoError(t, parseErr)
		_, ok := pubKey.(*rsa.PublicKey)
		assert.True(t, ok)
	})

	t.Run("ecdsa_key_returns_pem", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		pemStr := xkmsExtractPublicKeyPEM(key)
		assert.NotEmpty(t, pemStr)
		assert.Contains(t, pemStr, "PUBLIC KEY")

		block, _ := pem.Decode([]byte(pemStr))
		require.NotNil(t, block)
		pubKey, parseErr := x509.ParsePKIXPublicKey(block.Bytes)
		require.NoError(t, parseErr)
		_, ok := pubKey.(*ecdsa.PublicKey)
		assert.True(t, ok)
	})

	t.Run("ed25519_key_returns_pem", func(t *testing.T) {
		_, privKey, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		pemStr := xkmsExtractPublicKeyPEM(privKey)
		assert.NotEmpty(t, pemStr)
		assert.Contains(t, pemStr, "PUBLIC KEY")
	})

	t.Run("signer_interface_returns_pem", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		require.NoError(t, err)

		var signer crypto.Signer = key
		pemStr := xkmsExtractPublicKeyPEM(signer)
		assert.NotEmpty(t, pemStr)
		assert.Contains(t, pemStr, "PUBLIC KEY")
	})

	t.Run("unsupported_type_returns_empty", func(t *testing.T) {
		pemStr := xkmsExtractPublicKeyPEM("not-a-key")
		assert.Empty(t, pemStr)
	})

	t.Run("nil_returns_empty", func(t *testing.T) {
		pemStr := xkmsExtractPublicKeyPEM(nil)
		assert.Empty(t, pemStr)
	})
}

func TestXKMSFindKeyAttributes(t *testing.T) {
	backend := newMockXKMSBackend()

	t.Run("finds_existing_key", func(t *testing.T) {
		backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
			{CN: "key-1", KeyAlgorithm: x509.RSA},
			{CN: "key-2", KeyAlgorithm: x509.ECDSA},
			{CN: "key-3", KeyAlgorithm: x509.Ed25519},
		}

		attrs, err := xkmsFindKeyAttributes(backend, "key-2")
		require.NoError(t, err)
		assert.Equal(t, "key-2", attrs.CN)
		assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
	})

	t.Run("returns_error_for_missing_key", func(t *testing.T) {
		backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
			{CN: "key-1", KeyAlgorithm: x509.RSA},
		}

		_, err := xkmsFindKeyAttributes(backend, "nonexistent-key")
		require.Error(t, err)
		var keyErr *XKMSKeyNotFoundError
		assert.ErrorAs(t, err, &keyErr)
		assert.Equal(t, "nonexistent-key", keyErr.KeyID)
	})

	t.Run("returns_error_for_empty_list", func(t *testing.T) {
		backend.keyProvider.keyAttrs = nil

		_, err := xkmsFindKeyAttributes(backend, "any-key")
		require.Error(t, err)
	})
}

// ===== Health Method Tests =====

func TestXKMSServer_Health(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_healthy_status", func(t *testing.T) {
		resp, err := server.Health(context.Background(), &xkmspb.HealthRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "healthy", resp.Status)
		assert.NotEmpty(t, resp.Version)
	})

	t.Run("returns_xkms_version", func(t *testing.T) {
		resp, err := server.Health(context.Background(), &xkmspb.HealthRequest{})
		require.NoError(t, err)
		assert.Equal(t, xkmssdk.ServiceVersion(), resp.Version)
	})
}

// ===== Barrier Operation Tests =====

func TestXKMSServer_BarrierStatus(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.BarrierStatus(context.Background(), &emptypb.Empty{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
		assert.Contains(t, st.Message(), "barrier operations not available")
	})
}

func TestXKMSServer_BarrierInitialize(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.BarrierInitialize(context.Background(), &xkmspb.BarrierInitializeRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}

func TestXKMSServer_BarrierUnseal(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.BarrierUnseal(context.Background(), &xkmspb.BarrierUnsealRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}

func TestXKMSServer_BarrierSeal(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.BarrierSeal(context.Background(), &emptypb.Empty{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}

// ===== Input Validation Tests =====

func TestXKMSServer_GenerateKey_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_key_id_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			Backend: "software",
			KeyType: "signing",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "key_id")
	})

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			KeyId:   "test-key",
			KeyType: "signing",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "backend")
	})

	t.Run("missing_key_type_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			KeyId:   "test-key",
			Backend: "software",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "key_type")
	})
}

func TestXKMSServer_GetKey_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_key_id_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GetKey(context.Background(), &xkmspb.GetKeyRequest{
			Backend: "software",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "key_id")
	})

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GetKey(context.Background(), &xkmspb.GetKeyRequest{
			KeyId: "test-key",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "backend")
	})
}

func TestXKMSServer_DeleteKey_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_key_id_returns_invalid_argument", func(t *testing.T) {
		_, err := server.DeleteKey(context.Background(), &xkmspb.DeleteKeyRequest{
			Backend: "software",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.DeleteKey(context.Background(), &xkmspb.DeleteKeyRequest{
			KeyId: "test-key",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestXKMSServer_RotateKey_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_key_id_returns_invalid_argument", func(t *testing.T) {
		_, err := server.RotateKey(context.Background(), &xkmspb.RotateKeyRequest{
			Backend: "software",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.RotateKey(context.Background(), &xkmspb.RotateKeyRequest{
			KeyId: "test-key",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestXKMSServer_Sign_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_key_id_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Sign(context.Background(), &xkmspb.SignRequest{
			Backend: "software",
			Data:    []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Sign(context.Background(), &xkmspb.SignRequest{
			KeyId: "test-key",
			Data:  []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestXKMSServer_Verify_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_key_id_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			Backend:   "software",
			Signature: []byte("sig"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:     "test-key",
			Signature: []byte("sig"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_signature_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:   "test-key",
			Backend: "software",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestXKMSServer_Encrypt_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_key_id_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
			Backend:   "software",
			Plaintext: []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
			KeyId:     "test-key",
			Plaintext: []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_plaintext_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
			KeyId:   "test-key",
			Backend: "software",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestXKMSServer_Decrypt_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_key_id_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
			Backend:    "software",
			Ciphertext: []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
			KeyId:      "test-key",
			Ciphertext: []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_ciphertext_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
			KeyId:   "test-key",
			Backend: "software",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestXKMSServer_SaveCert_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_key_id_returns_invalid_argument", func(t *testing.T) {
		_, err := server.SaveCert(context.Background(), &xkmspb.SaveCertRequest{
			CertPem: "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_cert_pem_returns_invalid_argument", func(t *testing.T) {
		_, err := server.SaveCert(context.Background(), &xkmspb.SaveCertRequest{
			KeyId: "test-key",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("invalid_pem_returns_invalid_argument", func(t *testing.T) {
		// First ensure we have a backend registered for SaveCert to reach PEM parsing
		xkmssdk.ServiceReset()
		backend := newMockXKMSBackend()
		err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
			Backends:       map[string]xkmssdk.ServiceBackend{"test": backend},
			DefaultBackend: "test",
		})
		require.NoError(t, err)
		defer xkmssdk.ServiceReset()

		_, saveErr := server.SaveCert(context.Background(), &xkmspb.SaveCertRequest{
			KeyId:   "test-key",
			CertPem: "not-valid-pem",
		})
		require.Error(t, saveErr)
		st, ok := status.FromError(saveErr)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "invalid PEM")
	})
}

func TestXKMSServer_GetCert_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_key_id_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GetCert(context.Background(), &xkmspb.GetCertRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestXKMSServer_DeleteCert_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_key_id_returns_invalid_argument", func(t *testing.T) {
		_, err := server.DeleteCert(context.Background(), &xkmspb.DeleteCertRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestXKMSServer_Seal_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Seal(context.Background(), &xkmspb.SealRequest{
			Data: []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_data_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Seal(context.Background(), &xkmspb.SealRequest{
			Backend: "software",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestXKMSServer_Unseal_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Unseal(context.Background(), &xkmspb.UnsealRequest{
			Ciphertext: []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_ciphertext_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Unseal(context.Background(), &xkmspb.UnsealRequest{
			Backend: "software",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

// ===== Backend Not Found Tests =====

func TestXKMSServer_ListBackends_NoBackends(t *testing.T) {
	xkmssdk.ServiceReset()
	server := newTestXKMSServer()

	t.Run("returns_empty_list_when_uninitialized", func(t *testing.T) {
		resp, err := server.ListBackends(context.Background(), &xkmspb.ListBackendsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Empty(t, resp.Backends)
		assert.Equal(t, int32(0), resp.Count)
	})
}

func TestXKMSServer_GetBackendInfo_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_name_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GetBackendInfo(context.Background(), &xkmspb.GetBackendInfoRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("unknown_backend_returns_not_found", func(t *testing.T) {
		xkmssdk.ServiceReset()
		_, err := server.GetBackendInfo(context.Background(), &xkmspb.GetBackendInfoRequest{
			Name: "nonexistent",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

func TestXKMSServer_ListKeys_BackendNotFound(t *testing.T) {
	xkmssdk.ServiceReset()
	server := newTestXKMSServer()

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("unknown_backend_returns_not_found", func(t *testing.T) {
		_, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{
			Backend: "nonexistent",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

func TestXKMSServer_CanSeal_NoBackends(t *testing.T) {
	xkmssdk.ServiceReset()
	server := newTestXKMSServer()

	t.Run("returns_false_when_uninitialized", func(t *testing.T) {
		resp, err := server.CanSeal(context.Background(), &xkmspb.CanSealRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.False(t, resp.CanSeal)
	})

	t.Run("returns_false_for_unknown_backend", func(t *testing.T) {
		resp, err := server.CanSeal(context.Background(), &xkmspb.CanSealRequest{
			Backend: "nonexistent",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.False(t, resp.CanSeal)
	})
}

// ===== Tests With Mock Backend =====

func TestXKMSServer_WithMockBackend_ListBackends(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-software": backend},
		DefaultBackend: "test-software",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("returns_registered_backends", func(t *testing.T) {
		resp, err := server.ListBackends(context.Background(), &xkmspb.ListBackendsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, int32(1), resp.Count)
		require.Len(t, resp.Backends, 1)
		assert.Equal(t, "test-software", resp.Backends[0].Name)
		assert.Equal(t, string(xkmssdk.BackendTypeSoftware), resp.Backends[0].Type)
		assert.True(t, resp.Backends[0].SupportsSigning)
		assert.True(t, resp.Backends[0].SupportsDecryption)
		assert.True(t, resp.Backends[0].SupportsRotation)
	})
}

func TestXKMSServer_WithMockBackend_GetBackendInfo(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-software": backend},
		DefaultBackend: "test-software",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("returns_backend_info", func(t *testing.T) {
		resp, err := server.GetBackendInfo(context.Background(), &xkmspb.GetBackendInfoRequest{
			Name: "test-software",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Backend)
		assert.Equal(t, "test-software", resp.Backend.Name)
		assert.Equal(t, string(xkmssdk.BackendTypeSoftware), resp.Backend.Type)
		assert.Equal(t, "Software-based key storage", resp.Backend.Description)
	})
}

func TestXKMSServer_WithMockBackend_ListKeys(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "key-1", KeyAlgorithm: x509.RSA, RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 2048}},
		{CN: "key-2", KeyAlgorithm: x509.ECDSA, ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()}},
		{CN: "key-3", KeyAlgorithm: x509.Ed25519},
	}

	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-software": backend},
		DefaultBackend: "test-software",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("returns_all_keys", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{
			Backend: "test-software",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Keys, 3)
		assert.Equal(t, int32(3), resp.Total)
	})

	t.Run("pagination_with_limit", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{
			Backend: "test-software",
			Limit:   2,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Keys, 2)
		assert.Equal(t, int32(3), resp.Total)
	})

	t.Run("pagination_with_offset", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{
			Backend: "test-software",
			Offset:  2,
			Limit:   10,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Keys, 1)
	})

	t.Run("offset_beyond_range_returns_empty", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{
			Backend: "test-software",
			Offset:  100,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Empty(t, resp.Keys)
	})
}

func TestXKMSServer_WithMockBackend_GetKey(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "rsa-key", KeyAlgorithm: x509.RSA, KeyType: xkmssdk.KeyTypeSigning, RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 2048}},
	}

	// Add an actual RSA key so Signer can return a public key
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	backend.keyProvider.keys["rsa-key"] = rsaKey

	err = xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-software": backend},
		DefaultBackend: "test-software",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("returns_key_info_with_public_key", func(t *testing.T) {
		resp, err := server.GetKey(context.Background(), &xkmspb.GetKeyRequest{
			KeyId:   "rsa-key",
			Backend: "test-software",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Key)
		assert.Equal(t, "rsa-key", resp.Key.KeyId)
		assert.Equal(t, "RSA", resp.Key.Algorithm)
		assert.Equal(t, int32(2048), resp.Key.KeySize)
		assert.NotEmpty(t, resp.PublicKeyPem)
		assert.Contains(t, resp.PublicKeyPem, "PUBLIC KEY")
	})

	t.Run("returns_not_found_for_missing_key", func(t *testing.T) {
		_, err := server.GetKey(context.Background(), &xkmspb.GetKeyRequest{
			KeyId:   "nonexistent",
			Backend: "test-software",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

func TestXKMSServer_WithMockBackend_CanSeal(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-software": backend},
		DefaultBackend: "test-software",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("returns_true_for_capable_backend", func(t *testing.T) {
		resp, err := server.CanSeal(context.Background(), &xkmspb.CanSealRequest{
			Backend: "test-software",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.CanSeal)
	})

	t.Run("returns_true_for_default_backend", func(t *testing.T) {
		resp, err := server.CanSeal(context.Background(), &xkmspb.CanSealRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.CanSeal)
	})
}

func TestXKMSServer_WithMockBackend_CertOperations(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-software": backend},
		DefaultBackend: "test-software",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()
	certPEM := generateTestCertPEM(t)

	t.Run("save_and_get_cert", func(t *testing.T) {
		saveResp, saveErr := server.SaveCert(context.Background(), &xkmspb.SaveCertRequest{
			KeyId:   "cert-key",
			CertPem: certPEM,
		})
		require.NoError(t, saveErr)
		require.NotNil(t, saveResp)
		assert.True(t, saveResp.Success)

		getResp, getErr := server.GetCert(context.Background(), &xkmspb.GetCertRequest{
			KeyId: "cert-key",
		})
		require.NoError(t, getErr)
		require.NotNil(t, getResp)
		assert.Contains(t, getResp.CertPem, "CERTIFICATE")
	})

	t.Run("delete_cert", func(t *testing.T) {
		// Save first
		_, saveErr := server.SaveCert(context.Background(), &xkmspb.SaveCertRequest{
			KeyId:   "delete-cert-key",
			CertPem: certPEM,
		})
		require.NoError(t, saveErr)

		delResp, delErr := server.DeleteCert(context.Background(), &xkmspb.DeleteCertRequest{
			KeyId: "delete-cert-key",
		})
		require.NoError(t, delErr)
		require.NotNil(t, delResp)
		assert.True(t, delResp.Success)

		// Verify it's gone
		_, getErr := server.GetCert(context.Background(), &xkmspb.GetCertRequest{
			KeyId: "delete-cert-key",
		})
		require.Error(t, getErr)
		st, ok := status.FromError(getErr)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("list_certs", func(t *testing.T) {
		// Save a cert first
		_, saveErr := server.SaveCert(context.Background(), &xkmspb.SaveCertRequest{
			KeyId:   "list-cert-key",
			CertPem: certPEM,
		})
		require.NoError(t, saveErr)

		listResp, listErr := server.ListCerts(context.Background(), &xkmspb.ListCertsRequest{})
		require.NoError(t, listErr)
		require.NotNil(t, listResp)
		assert.Greater(t, int(listResp.Total), 0)
		assert.NotEmpty(t, listResp.KeyIds)
	})

	t.Run("get_cert_not_found", func(t *testing.T) {
		_, err := server.GetCert(context.Background(), &xkmspb.GetCertRequest{
			KeyId: "nonexistent-cert",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

func TestXKMSServer_WithMockBackend_ListCerts_NoBackends(t *testing.T) {
	xkmssdk.ServiceReset()
	server := newTestXKMSServer()

	t.Run("returns_unavailable_when_no_backends", func(t *testing.T) {
		_, err := server.ListCerts(context.Background(), &xkmspb.ListCertsRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

// --- Merged from server_xkms_extended_test.go ---

// ===== PIV Validation Tests =====

func TestXKMSServer_ListPIVSlots_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.ListPIVSlots(context.Background(), &xkmspb.ListPIVSlotsRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "backend")
	})

	t.Run("piv_not_initialized_returns_unavailable", func(t *testing.T) {
		xkmssdk.ServiceResetPIV()
		_, err := server.ListPIVSlots(context.Background(), &xkmspb.ListPIVSlotsRequest{
			Backend: "nonexistent",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

func TestXKMSServer_GetPIVCertificate_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GetPIVCertificate(context.Background(), &xkmspb.GetPIVCertificateRequest{
			Slot:   "9a",
			Format: "pem",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "backend")
	})

	t.Run("missing_slot_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GetPIVCertificate(context.Background(), &xkmspb.GetPIVCertificateRequest{
			Backend: "software",
			Format:  "pem",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "slot")
	})

	t.Run("missing_format_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GetPIVCertificate(context.Background(), &xkmspb.GetPIVCertificateRequest{
			Backend: "software",
			Slot:    "9a",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "format")
	})

	t.Run("piv_not_initialized_returns_unavailable", func(t *testing.T) {
		xkmssdk.ServiceResetPIV()
		_, err := server.GetPIVCertificate(context.Background(), &xkmspb.GetPIVCertificateRequest{
			Backend: "software",
			Slot:    "9a",
			Format:  "pem",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

func TestXKMSServer_StorePIVCertificate_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.StorePIVCertificate(context.Background(), &xkmspb.StorePIVCertificateRequest{
			Slot:        "9a",
			Format:      "pem",
			Certificate: []byte("cert-data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "backend")
	})

	t.Run("missing_slot_returns_invalid_argument", func(t *testing.T) {
		_, err := server.StorePIVCertificate(context.Background(), &xkmspb.StorePIVCertificateRequest{
			Backend:     "software",
			Format:      "pem",
			Certificate: []byte("cert-data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "slot")
	})

	t.Run("missing_format_returns_invalid_argument", func(t *testing.T) {
		_, err := server.StorePIVCertificate(context.Background(), &xkmspb.StorePIVCertificateRequest{
			Backend:     "software",
			Slot:        "9a",
			Certificate: []byte("cert-data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "format")
	})

	t.Run("missing_certificate_returns_invalid_argument", func(t *testing.T) {
		_, err := server.StorePIVCertificate(context.Background(), &xkmspb.StorePIVCertificateRequest{
			Backend: "software",
			Slot:    "9a",
			Format:  "pem",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "certificate")
	})

	t.Run("piv_not_initialized_returns_unavailable", func(t *testing.T) {
		xkmssdk.ServiceResetPIV()
		_, err := server.StorePIVCertificate(context.Background(), &xkmspb.StorePIVCertificateRequest{
			Backend:     "software",
			Slot:        "9a",
			Format:      "pem",
			Certificate: []byte("cert-data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

func TestXKMSServer_DeletePIVCertificate_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.DeletePIVCertificate(context.Background(), &xkmspb.DeletePIVCertificateRequest{
			Slot: "9a",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "backend")
	})

	t.Run("missing_slot_returns_invalid_argument", func(t *testing.T) {
		_, err := server.DeletePIVCertificate(context.Background(), &xkmspb.DeletePIVCertificateRequest{
			Backend: "software",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "slot")
	})

	t.Run("piv_not_initialized_returns_unavailable", func(t *testing.T) {
		xkmssdk.ServiceResetPIV()
		_, err := server.DeletePIVCertificate(context.Background(), &xkmspb.DeletePIVCertificateRequest{
			Backend: "software",
			Slot:    "9a",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

func TestXKMSServer_GeneratePIVKey_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GeneratePIVKey(context.Background(), &xkmspb.GeneratePIVKeyRequest{
			Slot:      "9a",
			Algorithm: "ecdsap256",
			Subject:   "test",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "backend")
	})

	t.Run("missing_slot_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GeneratePIVKey(context.Background(), &xkmspb.GeneratePIVKeyRequest{
			Backend:   "software",
			Algorithm: "ecdsap256",
			Subject:   "test",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "slot")
	})

	t.Run("piv_not_initialized_returns_unavailable", func(t *testing.T) {
		xkmssdk.ServiceResetPIV()
		_, err := server.GeneratePIVKey(context.Background(), &xkmspb.GeneratePIVKeyRequest{
			Backend:   "software",
			Slot:      "9a",
			Algorithm: "ecdsap256",
			Subject:   "test",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

func TestXKMSServer_ImportPIVCertificate_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.ImportPIVCertificate(context.Background(), &xkmspb.StorePIVCertificateRequest{
			Slot:        "9a",
			Format:      "pem",
			Certificate: []byte("cert-data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_slot_returns_invalid_argument", func(t *testing.T) {
		_, err := server.ImportPIVCertificate(context.Background(), &xkmspb.StorePIVCertificateRequest{
			Backend:     "software",
			Format:      "pem",
			Certificate: []byte("cert-data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_format_returns_invalid_argument", func(t *testing.T) {
		_, err := server.ImportPIVCertificate(context.Background(), &xkmspb.StorePIVCertificateRequest{
			Backend:     "software",
			Slot:        "9a",
			Certificate: []byte("cert-data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_certificate_returns_invalid_argument", func(t *testing.T) {
		_, err := server.ImportPIVCertificate(context.Background(), &xkmspb.StorePIVCertificateRequest{
			Backend: "software",
			Slot:    "9a",
			Format:  "pem",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("piv_not_initialized_returns_unavailable", func(t *testing.T) {
		xkmssdk.ServiceResetPIV()
		_, err := server.ImportPIVCertificate(context.Background(), &xkmspb.StorePIVCertificateRequest{
			Backend:     "software",
			Slot:        "9a",
			Format:      "pem",
			Certificate: []byte("cert-data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

func TestXKMSServer_ExportPIVCertificate_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.ExportPIVCertificate(context.Background(), &xkmspb.GetPIVCertificateRequest{
			Slot:   "9a",
			Format: "pem",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_slot_returns_invalid_argument", func(t *testing.T) {
		_, err := server.ExportPIVCertificate(context.Background(), &xkmspb.GetPIVCertificateRequest{
			Backend: "software",
			Format:  "pem",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_format_returns_invalid_argument", func(t *testing.T) {
		_, err := server.ExportPIVCertificate(context.Background(), &xkmspb.GetPIVCertificateRequest{
			Backend: "software",
			Slot:    "9a",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("piv_not_initialized_returns_unavailable", func(t *testing.T) {
		xkmssdk.ServiceResetPIV()
		_, err := server.ExportPIVCertificate(context.Background(), &xkmspb.GetPIVCertificateRequest{
			Backend: "software",
			Slot:    "9a",
			Format:  "pem",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

func TestXKMSServer_GeneratePIVCSR_Validation(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("missing_backend_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GeneratePIVCSR(context.Background(), &xkmspb.GeneratePIVCSRRequest{
			Slot:    "9a",
			Subject: "test",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "backend")
	})

	t.Run("missing_slot_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GeneratePIVCSR(context.Background(), &xkmspb.GeneratePIVCSRRequest{
			Backend: "software",
			Subject: "test",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "slot")
	})

	t.Run("piv_not_initialized_returns_unavailable", func(t *testing.T) {
		xkmssdk.ServiceResetPIV()
		_, err := server.GeneratePIVCSR(context.Background(), &xkmspb.GeneratePIVCSRRequest{
			Backend: "software",
			Slot:    "9a",
			Subject: "test",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

// ===== PIV Error Mapping Tests =====

func TestXKMSMapPIVError(t *testing.T) {
	t.Run("nil_error_returns_nil", func(t *testing.T) {
		result := xkmsMapPIVError(nil, "test")
		assert.Nil(t, result)
	})

	t.Run("not_initialized_maps_to_unavailable", func(t *testing.T) {
		result := xkmsMapPIVError(xkmssdk.ServiceErrPIVNotInitialized, "test")
		require.Error(t, result)
		st, ok := status.FromError(result)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})

	t.Run("backend_not_found_maps_to_not_found", func(t *testing.T) {
		result := xkmsMapPIVError(xkmssdk.ServiceErrPIVBackendNotFound, "test")
		require.Error(t, result)
		st, ok := status.FromError(result)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("invalid_algorithm_maps_to_invalid_argument", func(t *testing.T) {
		result := xkmsMapPIVError(xkmssdk.ServiceErrPIVInvalidAlgorithm, "test")
		require.Error(t, result)
		st, ok := status.FromError(result)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("invalid_slot_maps_to_invalid_argument", func(t *testing.T) {
		result := xkmsMapPIVError(xkmssdk.ServiceErrPIVInvalidSlot, "test")
		require.Error(t, result)
		st, ok := status.FromError(result)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("invalid_format_maps_to_invalid_argument", func(t *testing.T) {
		result := xkmsMapPIVError(xkmssdk.ServiceErrPIVInvalidFormat, "test")
		require.Error(t, result)
		st, ok := status.FromError(result)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("key_not_found_maps_to_not_found", func(t *testing.T) {
		result := xkmsMapPIVError(xkmssdk.ServiceErrPIVKeyNotFound, "test")
		require.Error(t, result)
		st, ok := status.FromError(result)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("unknown_error_maps_to_internal", func(t *testing.T) {
		result := xkmsMapPIVError(&XKMSKeyNotFoundError{KeyID: "test"}, "test")
		require.Error(t, result)
		st, ok := status.FromError(result)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
	})
}

// ===== PIV Typed Error Tests =====

func TestXKMSPIVErrors(t *testing.T) {
	t.Run("backend_required_error_message", func(t *testing.T) {
		assert.Contains(t, ErrXKMSPIVBackendRequired.Error(), "backend")
	})

	t.Run("slot_required_error_message", func(t *testing.T) {
		assert.Contains(t, ErrXKMSPIVSlotRequired.Error(), "slot")
	})

	t.Run("format_required_error_message", func(t *testing.T) {
		assert.Contains(t, ErrXKMSPIVFormatRequired.Error(), "format")
	})

	t.Run("certificate_required_error_message", func(t *testing.T) {
		assert.Contains(t, ErrXKMSPIVCertificateRequired.Error(), "certificate")
	})
}

// ===== PIN Unimplemented Tests =====

func TestXKMSServer_SetSOPIN(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.SetSOPIN(context.Background(), &xkmspb.SetSOPINRequest{
			CurrentSoPin: "old-pin",
			NewSoPin:     "new-pin",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
		assert.Contains(t, st.Message(), "PIN operations not available")
	})

	t.Run("returns_unimplemented_with_empty_request", func(t *testing.T) {
		_, err := server.SetSOPIN(context.Background(), &xkmspb.SetSOPINRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}

func TestXKMSServer_SetUserPIN(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.SetUserPIN(context.Background(), &xkmspb.SetUserPINRequest{
			SoPin:      "so-pin",
			NewUserPin: "user-pin",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
		assert.Contains(t, st.Message(), "PIN operations not available")
	})

	t.Run("returns_unimplemented_with_empty_request", func(t *testing.T) {
		_, err := server.SetUserPIN(context.Background(), &xkmspb.SetUserPINRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}

func TestXKMSServer_ChangeSOPIN(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.ChangeSOPIN(context.Background(), &xkmspb.ChangeSOPINRequest{
			CurrentSoPin: "old-pin",
			NewSoPin:     "new-pin",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
		assert.Contains(t, st.Message(), "PIN operations not available")
	})

	t.Run("returns_unimplemented_with_empty_request", func(t *testing.T) {
		_, err := server.ChangeSOPIN(context.Background(), &xkmspb.ChangeSOPINRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}

func TestXKMSServer_ChangeUserPIN(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.ChangeUserPIN(context.Background(), &xkmspb.ChangeUserPINRequest{
			CurrentUserPin: "old-pin",
			NewUserPin:     "new-pin",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
		assert.Contains(t, st.Message(), "PIN operations not available")
	})

	t.Run("returns_unimplemented_with_empty_request", func(t *testing.T) {
		_, err := server.ChangeUserPIN(context.Background(), &xkmspb.ChangeUserPINRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}

func TestXKMSServer_VerifySOPIN(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.VerifySOPIN(context.Background(), &xkmspb.VerifySOPINRequest{
			SoPin: "so-pin",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
		assert.Contains(t, st.Message(), "PIN operations not available")
	})

	t.Run("returns_unimplemented_with_empty_request", func(t *testing.T) {
		_, err := server.VerifySOPIN(context.Background(), &xkmspb.VerifySOPINRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}

func TestXKMSServer_VerifyUserPIN(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.VerifyUserPIN(context.Background(), &xkmspb.VerifyUserPINRequest{
			UserPin: "user-pin",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
		assert.Contains(t, st.Message(), "PIN operations not available")
	})

	t.Run("returns_unimplemented_with_empty_request", func(t *testing.T) {
		_, err := server.VerifyUserPIN(context.Background(), &xkmspb.VerifyUserPINRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}

func TestXKMSServer_GetLockoutStatus(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.GetLockoutStatus(context.Background(), &emptypb.Empty{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
		assert.Contains(t, st.Message(), "PIN operations not available")
	})
}

func TestXKMSServer_ResetLockout(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.ResetLockout(context.Background(), &xkmspb.ResetLockoutRequest{
			SoPin: "so-pin",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
		assert.Contains(t, st.Message(), "PIN operations not available")
	})

	t.Run("returns_unimplemented_with_empty_request", func(t *testing.T) {
		_, err := server.ResetLockout(context.Background(), &xkmspb.ResetLockoutRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}

// ===== CA Bundle Unimplemented Tests =====

func TestXKMSServer_GetCABundle(t *testing.T) {
	server := newTestXKMSServer()

	t.Run("returns_unimplemented", func(t *testing.T) {
		_, err := server.GetCABundle(context.Background(), &xkmspb.GetCABundleRequest{
			StoreType: "root",
			Algorithm: "ECDSA",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
		assert.Contains(t, st.Message(), "CA bundle operations not available")
	})

	t.Run("returns_unimplemented_with_empty_request", func(t *testing.T) {
		_, err := server.GetCABundle(context.Background(), &xkmspb.GetCABundleRequest{})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}
