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
	"crypto/rsa"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	xkmspb "github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// mockSymmetricEncrypter implements xkmssdk.SymmetricEncrypter for testing.
type mockSymmetricEncrypter struct {
	encryptFunc func(plaintext []byte, opts *xkmssdk.EncryptOptions) (*xkmssdk.EncryptedData, error)
	decryptFunc func(data *xkmssdk.EncryptedData, opts *xkmssdk.DecryptOptions) ([]byte, error)
}

func (m *mockSymmetricEncrypter) Encrypt(plaintext []byte, opts *xkmssdk.EncryptOptions) (*xkmssdk.EncryptedData, error) {
	if m.encryptFunc != nil {
		return m.encryptFunc(plaintext, opts)
	}
	return &xkmssdk.EncryptedData{
		Ciphertext: []byte("encrypted-" + string(plaintext)),
		Nonce:      []byte("test-nonce-12345"),
		Tag:        []byte("test-tag-1234567"),
	}, nil
}

func (m *mockSymmetricEncrypter) Decrypt(data *xkmssdk.EncryptedData, opts *xkmssdk.DecryptOptions) ([]byte, error) {
	if m.decryptFunc != nil {
		return m.decryptFunc(data, opts)
	}
	return []byte("decrypted-data"), nil
}

// mockSymmetricKeyProvider implements xkmssdk.SymmetricKeyProvider for testing.
type mockSymmetricKeyProvider struct {
	*mockKeyProvider
	generateSymmetricKeyFunc func(attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricKey, error)
	getSymmetricKeyFunc      func(attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricKey, error)
	symmetricEncrypterFunc   func(attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricEncrypter, error)
}

func newMockSymmetricKeyProvider() *mockSymmetricKeyProvider {
	return &mockSymmetricKeyProvider{
		mockKeyProvider: &mockKeyProvider{
			backendType: xkmssdk.BackendTypeSymmetric,
			capabilities: xkmssdk.Capabilities{
				Keys:                true,
				Signing:             false,
				Decryption:          false,
				KeyRotation:         true,
				SymmetricEncryption: true,
			},
			keys: make(map[string]crypto.PrivateKey),
		},
	}
}

func (m *mockSymmetricKeyProvider) GenerateSymmetricKey(attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricKey, error) {
	if m.generateSymmetricKeyFunc != nil {
		return m.generateSymmetricKeyFunc(attrs)
	}
	return nil, nil
}

func (m *mockSymmetricKeyProvider) GetSymmetricKey(attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricKey, error) {
	if m.getSymmetricKeyFunc != nil {
		return m.getSymmetricKeyFunc(attrs)
	}
	return nil, nil
}

func (m *mockSymmetricKeyProvider) SymmetricEncrypter(attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricEncrypter, error) {
	if m.symmetricEncrypterFunc != nil {
		return m.symmetricEncrypterFunc(attrs)
	}
	return &mockSymmetricEncrypter{}, nil
}

// Compile-time check
var _ xkmssdk.SymmetricKeyProvider = (*mockSymmetricKeyProvider)(nil)

// mockSymmetricXKMSBackend extends mockXKMSBackend with SymmetricKeyProvider support.
type mockSymmetricXKMSBackend struct {
	mockXKMSBackend
	symProvider *mockSymmetricKeyProvider
}

func newMockSymmetricXKMSBackend() *mockSymmetricXKMSBackend {
	symProv := newMockSymmetricKeyProvider()
	b := &mockSymmetricXKMSBackend{
		mockXKMSBackend: mockXKMSBackend{
			keyProvider: symProv.mockKeyProvider,
			certStore:   newMockCertStorage(),
		},
		symProvider: symProv,
	}
	return b
}

func (m *mockSymmetricXKMSBackend) KeyProvider() xkmssdk.KeyProvider {
	return m.symProvider
}

// mockDecrypterBackend extends mockXKMSBackend with a configurable Decrypter.
type mockDecrypterBackend struct {
	mockXKMSBackend
	decrypterFunc func(attrs *xkmssdk.KeyAttributes) (crypto.Decrypter, error)
}

func (m *mockDecrypterBackend) Decrypter(attrs *xkmssdk.KeyAttributes) (crypto.Decrypter, error) {
	if m.decrypterFunc != nil {
		return m.decrypterFunc(attrs)
	}
	return nil, errors.New("decrypter not available")
}

// =============================================================================
// Encrypt Tests (Symmetric Path)
// =============================================================================

func TestXKMSServer_Encrypt_SymmetricKey_Success(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()
	backend.symProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "sym-key-1", KeyType: xkmssdk.KeyTypeSecret, SymmetricAlgorithm: xkmssdk.SymmetricAlgorithm("aes256-gcm")},
	}

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	resp, err := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
		KeyId:          "sym-key-1",
		Backend:        "sym-be",
		Plaintext:      []byte("hello world"),
		AdditionalData: []byte("aad-data"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.Ciphertext)
	assert.NotEmpty(t, resp.Nonce)
	assert.NotEmpty(t, resp.Tag)
}

func TestXKMSServer_Encrypt_MissingKeyID(t *testing.T) {
	server := newTestXKMSServer()
	_, err := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
		Backend:   "be",
		Plaintext: []byte("data"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestXKMSServer_Encrypt_MissingBackend(t *testing.T) {
	server := newTestXKMSServer()
	_, err := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
		KeyId:     "k1",
		Plaintext: []byte("data"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestXKMSServer_Encrypt_MissingPlaintext(t *testing.T) {
	server := newTestXKMSServer()
	_, err := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
		KeyId:   "k1",
		Backend: "be",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestXKMSServer_Encrypt_BackendNotFound(t *testing.T) {
	xkmssdk.ServiceReset()
	server := newTestXKMSServer()

	_, err := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
		KeyId:     "k1",
		Backend:   "nonexistent",
		Plaintext: []byte("data"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestXKMSServer_Encrypt_KeyNotFound(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()
	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()
	_, encErr := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
		KeyId:     "nonexistent-key",
		Backend:   "sym-be",
		Plaintext: []byte("data"),
	})
	require.Error(t, encErr)
	assert.Equal(t, codes.NotFound, status.Code(encErr))
}

func TestXKMSServer_Encrypt_EncrypterError(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()
	backend.symProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "sym-key-err", KeyType: xkmssdk.KeyTypeSecret, SymmetricAlgorithm: xkmssdk.SymmetricAlgorithm("aes256-gcm")},
	}
	backend.symProvider.symmetricEncrypterFunc = func(attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricEncrypter, error) {
		return nil, errors.New("encrypter creation failed")
	}

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()
	_, encErr := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
		KeyId:     "sym-key-err",
		Backend:   "sym-be",
		Plaintext: []byte("data"),
	})
	require.Error(t, encErr)
	assert.Equal(t, codes.Internal, status.Code(encErr))
}

func TestXKMSServer_Encrypt_EncryptionFails(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()
	backend.symProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "sym-key-fail", KeyType: xkmssdk.KeyTypeSecret, SymmetricAlgorithm: xkmssdk.SymmetricAlgorithm("aes256-gcm")},
	}
	backend.symProvider.symmetricEncrypterFunc = func(attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricEncrypter, error) {
		return &mockSymmetricEncrypter{
			encryptFunc: func(plaintext []byte, opts *xkmssdk.EncryptOptions) (*xkmssdk.EncryptedData, error) {
				return nil, errors.New("encryption failed")
			},
		}, nil
	}

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()
	_, encErr := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
		KeyId:     "sym-key-fail",
		Backend:   "sym-be",
		Plaintext: []byte("data"),
	})
	require.Error(t, encErr)
	assert.Equal(t, codes.Internal, status.Code(encErr))
}

// =============================================================================
// Decrypt Tests (Symmetric Path)
// =============================================================================

func TestXKMSServer_Decrypt_SymmetricKey_Success(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()
	backend.symProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "sym-decrypt-key", KeyType: xkmssdk.KeyTypeSecret, SymmetricAlgorithm: xkmssdk.SymmetricAlgorithm("aes256-gcm")},
	}

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	resp, err := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
		KeyId:          "sym-decrypt-key",
		Backend:        "sym-be",
		Ciphertext:     []byte("encrypted-data"),
		Nonce:          []byte("nonce-12345"),
		Tag:            []byte("tag-12345"),
		AdditionalData: []byte("aad"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.Plaintext)
}

func TestXKMSServer_Decrypt_SymmetricKey_EncrypterError(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()
	backend.symProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "sym-dec-err", KeyType: xkmssdk.KeyTypeSecret, SymmetricAlgorithm: xkmssdk.SymmetricAlgorithm("aes256-gcm")},
	}
	backend.symProvider.symmetricEncrypterFunc = func(attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricEncrypter, error) {
		return nil, errors.New("encrypter error")
	}

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()
	_, decErr := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
		KeyId:      "sym-dec-err",
		Backend:    "sym-be",
		Ciphertext: []byte("data"),
	})
	require.Error(t, decErr)
	assert.Equal(t, codes.Internal, status.Code(decErr))
}

func TestXKMSServer_Decrypt_SymmetricKey_DecryptionFails(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()
	backend.symProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "sym-dec-fail", KeyType: xkmssdk.KeyTypeSecret, SymmetricAlgorithm: xkmssdk.SymmetricAlgorithm("aes256-gcm")},
	}
	backend.symProvider.symmetricEncrypterFunc = func(attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricEncrypter, error) {
		return &mockSymmetricEncrypter{
			decryptFunc: func(data *xkmssdk.EncryptedData, opts *xkmssdk.DecryptOptions) ([]byte, error) {
				return nil, errors.New("decryption failed")
			},
		}, nil
	}

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()
	_, decErr := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
		KeyId:      "sym-dec-fail",
		Backend:    "sym-be",
		Ciphertext: []byte("data"),
	})
	require.Error(t, decErr)
	assert.Equal(t, codes.Internal, status.Code(decErr))
}

func TestXKMSServer_Decrypt_SymmetricKey_NonSymmetricBackend(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "fake-sym", KeyType: xkmssdk.KeyTypeSecret, SymmetricAlgorithm: xkmssdk.SymmetricAlgorithm("aes256-gcm")},
	}

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()
	_, decErr := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
		KeyId:      "fake-sym",
		Backend:    "test-be",
		Ciphertext: []byte("data"),
	})
	require.Error(t, decErr)
	assert.Equal(t, codes.Unimplemented, status.Code(decErr))
}

// =============================================================================
// Decrypt Tests (Asymmetric Path via Decrypter)
// =============================================================================

func TestXKMSServer_Decrypt_AsymmetricKey_DecrypterError(t *testing.T) {
	// Use mockDecrypterBackend that returns an error from Decrypter(),
	// triggering the error path at server_xkms.go line 625-626.
	backend := &mockDecrypterBackend{
		mockXKMSBackend: *newMockXKMSBackend(),
		decrypterFunc: func(attrs *xkmssdk.KeyAttributes) (crypto.Decrypter, error) {
			return nil, errors.New("decrypter not available")
		},
	}

	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "rsa-decrypt-key", KeyType: xkmssdk.KeyTypeEncryption},
	}

	xkmssdk.ServiceReset()
	xkmsErr := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, xkmsErr)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	_, decErr := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
		KeyId:      "rsa-decrypt-key",
		Backend:    "test-be",
		Ciphertext: []byte("encrypted-data"),
	})
	require.Error(t, decErr)
	assert.Equal(t, codes.Internal, status.Code(decErr))
}

func TestXKMSServer_Decrypt_AsymmetricKey_DecryptionFails(t *testing.T) {
	// Use a real RSA key so Decrypter succeeds but decryption itself fails
	// due to invalid ciphertext, testing server_xkms.go line 629-631.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	backend := &mockDecrypterBackend{
		mockXKMSBackend: *newMockXKMSBackend(),
		decrypterFunc: func(attrs *xkmssdk.KeyAttributes) (crypto.Decrypter, error) {
			return rsaKey, nil
		},
	}

	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "rsa-decrypt-key2", KeyType: xkmssdk.KeyTypeEncryption},
	}

	xkmssdk.ServiceReset()
	xkmsErr := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, xkmsErr)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	// Pass invalid ciphertext to trigger a decryption error
	_, decErr := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
		KeyId:      "rsa-decrypt-key2",
		Backend:    "test-be",
		Ciphertext: []byte("invalid-ciphertext-that-will-fail"),
	})
	require.Error(t, decErr)
	assert.Equal(t, codes.Internal, status.Code(decErr))
}

func TestXKMSServer_Decrypt_MissingKeyID(t *testing.T) {
	server := newTestXKMSServer()
	_, err := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
		Backend:    "be",
		Ciphertext: []byte("data"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestXKMSServer_Decrypt_MissingBackend(t *testing.T) {
	server := newTestXKMSServer()
	_, err := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
		KeyId:      "k1",
		Ciphertext: []byte("data"),
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestXKMSServer_Decrypt_MissingCiphertext(t *testing.T) {
	server := newTestXKMSServer()
	_, err := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
		KeyId:   "k1",
		Backend: "be",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// =============================================================================
// GenerateKey Symmetric Tests
// =============================================================================

func TestXKMSServer_GenerateKey_Symmetric_DefaultSize(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	resp, genErr := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId:     "sym-gen-key",
		Backend:   "sym-be",
		KeyType:   "secret",
		Algorithm: "symmetric",
	})
	require.NoError(t, genErr)
	require.NotNil(t, resp)
	assert.Equal(t, "sym-gen-key", resp.KeyId)
	assert.Equal(t, "sym-be", resp.Backend)
	assert.NotNil(t, resp.CreatedAt)
}

func TestXKMSServer_GenerateKey_Symmetric_128(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	resp, genErr := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId:     "aes128-key",
		Backend:   "sym-be",
		KeyType:   "secret",
		Algorithm: "symmetric",
		KeySize:   128,
	})
	require.NoError(t, genErr)
	require.NotNil(t, resp)
	assert.Equal(t, "aes128-key", resp.KeyId)
}

func TestXKMSServer_GenerateKey_Symmetric_192(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	resp, genErr := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId:     "aes192-key",
		Backend:   "sym-be",
		KeyType:   "secret",
		Algorithm: "symmetric",
		KeySize:   192,
	})
	require.NoError(t, genErr)
	require.NotNil(t, resp)
}

func TestXKMSServer_GenerateKey_Symmetric_256(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	resp, genErr := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId:     "aes256-key",
		Backend:   "sym-be",
		KeyType:   "secret",
		Algorithm: "symmetric",
		KeySize:   256,
	})
	require.NoError(t, genErr)
	require.NotNil(t, resp)
}

func TestXKMSServer_GenerateKey_Symmetric_InvalidSize(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	_, genErr := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId:     "bad-size-key",
		Backend:   "sym-be",
		KeyType:   "secret",
		Algorithm: "symmetric",
		KeySize:   64,
	})
	require.Error(t, genErr)
	assert.Equal(t, codes.InvalidArgument, status.Code(genErr))
}

func TestXKMSServer_GenerateKey_Symmetric_NonSymmetricBackend(t *testing.T) {
	backend := newMockXKMSBackend()

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	_, genErr := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId:     "sym-no-support",
		Backend:   "test-be",
		KeyType:   "secret",
		Algorithm: "symmetric",
	})
	require.Error(t, genErr)
	assert.Equal(t, codes.Unimplemented, status.Code(genErr))
}

func TestXKMSServer_GenerateKey_Symmetric_GenerationError(t *testing.T) {
	backend := newMockSymmetricXKMSBackend()
	backend.symProvider.generateSymmetricKeyFunc = func(attrs *xkmssdk.KeyAttributes) (xkmssdk.SymmetricKey, error) {
		return nil, errors.New("symmetric key generation failed")
	}

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	_, genErr := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId:     "fail-gen-key",
		Backend:   "sym-be",
		KeyType:   "secret",
		Algorithm: "symmetric",
	})
	require.Error(t, genErr)
	assert.Equal(t, codes.Internal, status.Code(genErr))
}

func TestXKMSServer_GenerateKey_Symmetric_CaseSensitiveAlgorithm(t *testing.T) {
	// When Algorithm is "Symmetric" (capital S), it matches AlgorithmSymmetric.Equals()
	// but does NOT match the lowercase check at line 193 (symAlgorithm == "symmetric"),
	// so the algorithm is used as-is without key-size-based mapping.
	backend := newMockSymmetricXKMSBackend()

	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"sym-be": backend},
		DefaultBackend: "sym-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	resp, genErr := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId:     "capital-s-key",
		Backend:   "sym-be",
		KeyType:   "secret",
		Algorithm: "Symmetric",
	})
	require.NoError(t, genErr)
	require.NotNil(t, resp)
	assert.Equal(t, "capital-s-key", resp.KeyId)
}

// =============================================================================
// GenerateKey Error Path Tests
// =============================================================================

func TestXKMSServer_GenerateKey_RSA_GenerationError(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.generateRSAFunc = func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
		return nil, errors.New("RSA generation failed")
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	_, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId:     "rsa-fail-key",
		Backend:   "test-be",
		KeyType:   "signing",
		Algorithm: "RSA",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestXKMSServer_GenerateKey_ECDSA_GenerationError(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.generateECDSAFunc = func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
		return nil, errors.New("ECDSA generation failed")
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	_, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId:     "ecdsa-fail-key",
		Backend:   "test-be",
		KeyType:   "signing",
		Algorithm: "ECDSA",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestXKMSServer_GenerateKey_Ed25519_GenerationError(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.generateEd25519Func = func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
		return nil, errors.New("Ed25519 generation failed")
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	_, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
		KeyId:     "ed25519-fail-key",
		Backend:   "test-be",
		KeyType:   "signing",
		Algorithm: "Ed25519",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// =============================================================================
// xkmsExtractPublicKeyPEM Tests
// =============================================================================

func TestXKMSExtractPublicKeyPEM_RSA(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pemStr := xkmsExtractPublicKeyPEM(rsaKey)
	assert.NotEmpty(t, pemStr)
	assert.Contains(t, pemStr, "PUBLIC KEY")
}

func TestXKMSExtractPublicKeyPEM_ECDSA(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	pemStr := xkmsExtractPublicKeyPEM(ecKey)
	assert.NotEmpty(t, pemStr)
	assert.Contains(t, pemStr, "PUBLIC KEY")
}

func TestXKMSExtractPublicKeyPEM_UnsupportedType(t *testing.T) {
	pemStr := xkmsExtractPublicKeyPEM("not-a-key")
	assert.Empty(t, pemStr)
}

// =============================================================================
// xkmsFindKeyAttributes Tests
// =============================================================================

func TestXKMSFindKeyAttributes_Found(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "key-1"},
		{CN: "key-2"},
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	ks, err := xkmssdk.ServiceGetBackend("test-be")
	require.NoError(t, err)

	attrs, findErr := xkmsFindKeyAttributes(ks, "key-2")
	require.NoError(t, findErr)
	assert.Equal(t, "key-2", attrs.CN)
}

func TestXKMSFindKeyAttributes_NotFound(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "key-1"},
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	ks, err := xkmssdk.ServiceGetBackend("test-be")
	require.NoError(t, err)

	_, findErr := xkmsFindKeyAttributes(ks, "nonexistent")
	require.Error(t, findErr)
}
