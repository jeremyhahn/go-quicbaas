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
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	xkmspb "github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// errorXKMSBackend is a mock backend that returns errors for various operations.
type errorXKMSBackend struct {
	keyProvider   *mockKeyProvider
	certStore     *mockCertStorage
	listKeysErr   error
	deleteKeyErr  error
	rotateKeyErr  error
	signerErr     error
	getKeyErr     error
	saveCertErr   error
	deleteCertErr error
	listCertsErr  error
	sealErr       error
	unsealErr     error
}

func newErrorXKMSBackend() *errorXKMSBackend {
	return &errorXKMSBackend{
		keyProvider: newMockKeyProvider(),
		certStore:   newMockCertStorage(),
	}
}

func (m *errorXKMSBackend) GenerateRSA(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	return nil, nil
}
func (m *errorXKMSBackend) GenerateECDSA(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	return nil, nil
}
func (m *errorXKMSBackend) GenerateEd25519(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	return nil, nil
}
func (m *errorXKMSBackend) GetKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	if m.getKeyErr != nil {
		return nil, m.getKeyErr
	}
	return m.keyProvider.GetKey(ctx, attrs)
}
func (m *errorXKMSBackend) DeleteKey(_ context.Context, attrs *xkmssdk.KeyAttributes) error {
	return m.deleteKeyErr
}
func (m *errorXKMSBackend) ListKeys(ctx context.Context) ([]*xkmssdk.KeyAttributes, error) {
	if m.listKeysErr != nil {
		return nil, m.listKeysErr
	}
	return m.keyProvider.ListKeys(ctx)
}
func (m *errorXKMSBackend) RotateKey(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
	if m.rotateKeyErr != nil {
		return nil, m.rotateKeyErr
	}
	return nil, nil
}
func (m *errorXKMSBackend) Signer(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Signer, error) {
	if m.signerErr != nil {
		return nil, m.signerErr
	}
	return m.keyProvider.Signer(ctx, attrs)
}
func (m *errorXKMSBackend) Decrypter(_ context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Decrypter, error) {
	return nil, nil
}
func (m *errorXKMSBackend) SaveCert(ctx context.Context, keyID string, cert *x509.Certificate) error {
	if m.saveCertErr != nil {
		return m.saveCertErr
	}
	return m.certStore.SaveCert(ctx, keyID, cert)
}
func (m *errorXKMSBackend) GetCert(ctx context.Context, keyID string) (*x509.Certificate, error) {
	return m.certStore.GetCert(ctx, keyID)
}
func (m *errorXKMSBackend) DeleteCert(ctx context.Context, keyID string) error {
	if m.deleteCertErr != nil {
		return m.deleteCertErr
	}
	return m.certStore.DeleteCert(ctx, keyID)
}
func (m *errorXKMSBackend) SaveCertChain(_ context.Context, keyID string, chain []*x509.Certificate) error {
	return nil
}
func (m *errorXKMSBackend) GetCertChain(_ context.Context, keyID string) ([]*x509.Certificate, error) {
	return nil, nil
}
func (m *errorXKMSBackend) ListCerts(ctx context.Context) ([]string, error) {
	if m.listCertsErr != nil {
		return nil, m.listCertsErr
	}
	return m.certStore.ListCerts(ctx)
}
func (m *errorXKMSBackend) CertExists(_ context.Context, keyID string) (bool, error) {
	return false, nil
}
func (m *errorXKMSBackend) GetTLSCertificate(_ context.Context, keyID string, attrs *xkmssdk.KeyAttributes) (tls.Certificate, error) {
	return tls.Certificate{}, nil
}
func (m *errorXKMSBackend) GetKeyByID(_ context.Context, keyID string) (crypto.PrivateKey, error) {
	return nil, nil
}
func (m *errorXKMSBackend) GetSignerByID(_ context.Context, keyID string) (crypto.Signer, error) {
	return nil, nil
}
func (m *errorXKMSBackend) GetDecrypterByID(_ context.Context, keyID string) (crypto.Decrypter, error) {
	return nil, nil
}
func (m *errorXKMSBackend) Seal(ctx context.Context, data []byte, opts *xkmssdk.SealOptions) (*xkmssdk.SealedData, error) {
	if m.sealErr != nil {
		return nil, m.sealErr
	}
	return &xkmssdk.SealedData{Backend: xkmssdk.BackendTypeSoftware, Ciphertext: data}, nil
}
func (m *errorXKMSBackend) Unseal(ctx context.Context, sealed *xkmssdk.SealedData, opts *xkmssdk.UnsealOptions) ([]byte, error) {
	if m.unsealErr != nil {
		return nil, m.unsealErr
	}
	return sealed.Ciphertext, nil
}
func (m *errorXKMSBackend) CanSeal() bool                                  { return true }
func (m *errorXKMSBackend) KeyProvider() xkmssdk.KeyProvider               { return m.keyProvider }
func (m *errorXKMSBackend) CertStorage() xkmssdk.CertificateStorageAdapter { return m.certStore }
func (m *errorXKMSBackend) Close() error                                   { return nil }

var _ xkmssdk.ServiceBackend = (*errorXKMSBackend)(nil)

// =============================================================================
// ListKeys Error
// =============================================================================

func TestXKMSServer_ListKeys_ListKeysError(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newErrorXKMSBackend()
	backend.listKeysErr = errors.New("storage failure")
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	_, listErr := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{Backend: "test-be"})
	require.Error(t, listErr)
	st, ok := status.FromError(listErr)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// DeleteKey Error
// =============================================================================

func TestXKMSServer_DeleteKey_DeleteError(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newErrorXKMSBackend()
	backend.deleteKeyErr = errors.New("delete failed")
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "key-1", KeyAlgorithm: x509.ECDSA, KeyType: xkmssdk.KeyTypeSigning},
	}
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	_, delErr := server.DeleteKey(context.Background(), &xkmspb.DeleteKeyRequest{KeyId: "key-1", Backend: "test-be"})
	require.Error(t, delErr)
	st, ok := status.FromError(delErr)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// RotateKey Error
// =============================================================================

func TestXKMSServer_RotateKey_RotateError(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newErrorXKMSBackend()
	backend.rotateKeyErr = errors.New("rotate failed")
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "key-1", KeyAlgorithm: x509.ECDSA, KeyType: xkmssdk.KeyTypeSigning},
	}
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	_, rotErr := server.RotateKey(context.Background(), &xkmspb.RotateKeyRequest{KeyId: "key-1", Backend: "test-be"})
	require.Error(t, rotErr)
	st, ok := status.FromError(rotErr)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// Sign - Signer Error
// =============================================================================

func TestXKMSServer_Sign_SignerError(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newErrorXKMSBackend()
	backend.signerErr = errors.New("signer unavailable")
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "key-1", KeyAlgorithm: x509.ECDSA, KeyType: xkmssdk.KeyTypeSigning},
	}
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	_, signErr := server.Sign(context.Background(), &xkmspb.SignRequest{
		KeyId: "key-1", Backend: "test-be", Data: []byte("data"),
	})
	require.Error(t, signErr)
	st, ok := status.FromError(signErr)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "failed to get signer")
}

// =============================================================================
// Verify - GetKey Error
// =============================================================================

func TestXKMSServer_Verify_GetKeyError(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newErrorXKMSBackend()
	backend.getKeyErr = errors.New("key unavailable")
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "key-1", KeyAlgorithm: x509.ECDSA, KeyType: xkmssdk.KeyTypeSigning},
	}
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	_, verifyErr := server.Verify(context.Background(), &xkmspb.VerifyRequest{
		KeyId: "key-1", Backend: "test-be", Data: []byte("data"), Signature: []byte("sig"),
	})
	require.Error(t, verifyErr)
	st, ok := status.FromError(verifyErr)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// SaveCert - SaveCert Error
// =============================================================================

func TestXKMSServer_SaveCert_SaveError(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newErrorXKMSBackend()
	backend.saveCertErr = errors.New("save failed")
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()
	certPEM := generateTestCertPEM(t)

	_, saveErr := server.SaveCert(context.Background(), &xkmspb.SaveCertRequest{
		KeyId: "test-key", CertPem: certPEM,
	})
	require.Error(t, saveErr)
	st, ok := status.FromError(saveErr)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// DeleteCert - DeleteCert Error
// =============================================================================

func TestXKMSServer_DeleteCert_DeleteError(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newErrorXKMSBackend()
	backend.deleteCertErr = errors.New("delete failed")
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	_, delErr := server.DeleteCert(context.Background(), &xkmspb.DeleteCertRequest{KeyId: "cert-key"})
	require.Error(t, delErr)
	st, ok := status.FromError(delErr)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// ListCerts - ListCerts Error
// =============================================================================

func TestXKMSServer_ListCerts_ListError(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newErrorXKMSBackend()
	backend.listCertsErr = errors.New("list failed")
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	_, listErr := server.ListCerts(context.Background(), &xkmspb.ListCertsRequest{})
	require.Error(t, listErr)
	st, ok := status.FromError(listErr)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// Sign/Verify with ECDSA - full happy path
// =============================================================================

func TestXKMSServer_Sign_Verify_ECDSA(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	backend.keyProvider.keys["ec-key"] = ecKey
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "ec-key", KeyAlgorithm: x509.ECDSA, KeyType: xkmssdk.KeyTypeSigning},
	}

	err = xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("sign_and_verify_ecdsa", func(t *testing.T) {
		signResp, err := server.Sign(context.Background(), &xkmspb.SignRequest{
			KeyId: "ec-key", Backend: "test-be", Data: []byte("test data"), Hash: "SHA256",
		})
		require.NoError(t, err)
		require.NotNil(t, signResp)
		assert.NotEmpty(t, signResp.Signature)

		verifyResp, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId: "ec-key", Backend: "test-be", Data: []byte("test data"),
			Signature: signResp.Signature, Hash: "SHA256",
		})
		require.NoError(t, err)
		assert.True(t, verifyResp.Valid)
	})

	t.Run("invalid_ecdsa_signature_returns_false", func(t *testing.T) {
		verifyResp, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId: "ec-key", Backend: "test-be", Data: []byte("test data"),
			Signature: []byte("bad-sig"), Hash: "SHA256",
		})
		require.NoError(t, err)
		assert.False(t, verifyResp.Valid)
		assert.Contains(t, verifyResp.Message, "invalid")
	})
}

// =============================================================================
// xkmsFindKeyAttributes - ListKeys Error
// =============================================================================

func TestXkmsFindKeyAttributes_ListKeysError(t *testing.T) {
	backend := newErrorXKMSBackend()
	backend.listKeysErr = errors.New("storage failure")

	_, err := xkmsFindKeyAttributes(backend, "any-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage failure")
}
