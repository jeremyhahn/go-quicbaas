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
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	xkmspb "github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// setupXKMSBackend registers a mock backend and returns cleanup function.
func setupXKMSBackend(t *testing.T, name string, backend xkmssdk.ServiceBackend) func() {
	t.Helper()
	xkmssdk.ServiceReset()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{name: backend},
		DefaultBackend: name,
	})
	require.NoError(t, err)
	return func() { xkmssdk.ServiceReset() }
}

// =============================================================================
// GenerateKey with Backend Tests
// =============================================================================

func TestXKMSServer_GenerateKey_NonexistentBackend(t *testing.T) {
	xkmssdk.ServiceReset()
	server := newTestXKMSServer()

	t.Run("backend_not_found", func(t *testing.T) {
		_, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			KeyId:   "test-key",
			Backend: "nonexistent",
			KeyType: "signing",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

func TestXKMSServer_GenerateKey_RSA(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.generateRSAFunc = func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, err
		}
		backend.keyProvider.keys[attrs.CN] = key
		backend.keyProvider.keyAttrs = append(backend.keyProvider.keyAttrs, attrs)
		return key, nil
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("generates_rsa_key_with_defaults", func(t *testing.T) {
		resp, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			KeyId:     "rsa-test-key",
			Backend:   "test-be",
			KeyType:   "signing",
			Algorithm: "RSA",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "rsa-test-key", resp.KeyId)
		assert.Equal(t, "test-be", resp.Backend)
		assert.NotEmpty(t, resp.PublicKeyPem)
		assert.Contains(t, resp.PublicKeyPem, "PUBLIC KEY")
		assert.NotNil(t, resp.CreatedAt)
	})

	t.Run("generates_rsa_key_with_explicit_size", func(t *testing.T) {
		resp, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			KeyId:     "rsa-4096-key",
			Backend:   "test-be",
			KeyType:   "signing",
			Algorithm: "RSA",
			KeySize:   4096,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "rsa-4096-key", resp.KeyId)
	})
}

func TestXKMSServer_GenerateKey_ECDSA(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.generateECDSAFunc = func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		backend.keyProvider.keys[attrs.CN] = key
		backend.keyProvider.keyAttrs = append(backend.keyProvider.keyAttrs, attrs)
		return key, nil
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("generates_ecdsa_key_with_default_curve", func(t *testing.T) {
		resp, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			KeyId:     "ecdsa-test-key",
			Backend:   "test-be",
			KeyType:   "signing",
			Algorithm: "ECDSA",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "ecdsa-test-key", resp.KeyId)
		assert.NotEmpty(t, resp.PublicKeyPem)
	})

	t.Run("generates_ecdsa_key_with_explicit_curve", func(t *testing.T) {
		resp, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			KeyId:     "ecdsa-p384-key",
			Backend:   "test-be",
			KeyType:   "signing",
			Algorithm: "ECDSA",
			Curve:     "P-384",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "ecdsa-p384-key", resp.KeyId)
	})

	t.Run("invalid_curve_returns_invalid_argument", func(t *testing.T) {
		_, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			KeyId:     "bad-curve-key",
			Backend:   "test-be",
			KeyType:   "signing",
			Algorithm: "ECDSA",
			Curve:     "invalid-curve",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestXKMSServer_GenerateKey_Ed25519(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.generateEd25519Func = func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
		_, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		backend.keyProvider.keys[attrs.CN] = privKey
		backend.keyProvider.keyAttrs = append(backend.keyProvider.keyAttrs, attrs)
		return privKey, nil
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("generates_ed25519_key", func(t *testing.T) {
		resp, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			KeyId:     "ed25519-test-key",
			Backend:   "test-be",
			KeyType:   "signing",
			Algorithm: "Ed25519",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "ed25519-test-key", resp.KeyId)
		assert.NotEmpty(t, resp.PublicKeyPem)
	})
}

func TestXKMSServer_GenerateKey_UnsupportedAlgorithm(t *testing.T) {
	backend := newMockXKMSBackend()
	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("unsupported_algorithm_returns_error", func(t *testing.T) {
		_, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			KeyId:     "bad-alg-key",
			Backend:   "test-be",
			KeyType:   "signing",
			Algorithm: "UNSUPPORTED_ALG",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestXKMSServer_GenerateKey_DefaultKeyType(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.generateEd25519Func = func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
		_, privKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		return privKey, nil
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("empty_algorithm_uses_key_type_as_algorithm", func(t *testing.T) {
		// When algorithm is empty, it falls back to keyType
		_, err := server.GenerateKey(context.Background(), &xkmspb.GenerateKeyRequest{
			KeyId:   "fallback-key",
			Backend: "test-be",
			KeyType: "Ed25519",
		})
		require.NoError(t, err)
	})
}

// =============================================================================
// DeleteKey with Backend Tests
// =============================================================================

func TestXKMSServer_DeleteKey_WithBackend(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "delete-me", KeyAlgorithm: x509.ECDSA},
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("deletes_existing_key", func(t *testing.T) {
		resp, err := server.DeleteKey(context.Background(), &xkmspb.DeleteKeyRequest{
			KeyId:   "delete-me",
			Backend: "test-be",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Success)
		assert.Contains(t, resp.Message, "deleted")
	})

	t.Run("key_not_found_returns_error", func(t *testing.T) {
		_, err := server.DeleteKey(context.Background(), &xkmspb.DeleteKeyRequest{
			KeyId:   "nonexistent-key",
			Backend: "test-be",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("backend_not_found_returns_error", func(t *testing.T) {
		_, err := server.DeleteKey(context.Background(), &xkmspb.DeleteKeyRequest{
			KeyId:   "some-key",
			Backend: "nonexistent-backend",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

// =============================================================================
// RotateKey with Backend Tests
// =============================================================================

func TestXKMSServer_RotateKey_WithBackend(t *testing.T) {
	backend := newMockXKMSBackend()
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "rotate-me", KeyAlgorithm: x509.ECDSA, KeyType: xkmssdk.KeyTypeSigning},
	}
	backend.rotateKeyFunc = func(attrs *xkmssdk.KeyAttributes) (crypto.PrivateKey, error) {
		return ecKey, nil
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("rotates_existing_key", func(t *testing.T) {
		resp, err := server.RotateKey(context.Background(), &xkmspb.RotateKeyRequest{
			KeyId:   "rotate-me",
			Backend: "test-be",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "rotate-me", resp.KeyId)
		assert.Equal(t, "test-be", resp.Backend)
		assert.NotEmpty(t, resp.PublicKeyPem)
		assert.NotNil(t, resp.RotatedAt)
	})

	t.Run("key_not_found_returns_error", func(t *testing.T) {
		_, err := server.RotateKey(context.Background(), &xkmspb.RotateKeyRequest{
			KeyId:   "nonexistent",
			Backend: "test-be",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

// =============================================================================
// Sign with Backend Tests
// =============================================================================

func TestXKMSServer_Sign_WithBackend(t *testing.T) {
	backend := newMockXKMSBackend()

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	backend.keyProvider.keys["rsa-sign-key"] = rsaKey
	backend.keyProvider.keyAttrs = append(backend.keyProvider.keyAttrs, &xkmssdk.KeyAttributes{
		CN: "rsa-sign-key", KeyAlgorithm: x509.RSA, KeyType: xkmssdk.KeyTypeSigning,
	})

	_, edPriv, edErr := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, edErr)
	backend.keyProvider.keys["ed25519-sign-key"] = edPriv
	backend.keyProvider.keyAttrs = append(backend.keyProvider.keyAttrs, &xkmssdk.KeyAttributes{
		CN: "ed25519-sign-key", KeyAlgorithm: x509.Ed25519, KeyType: xkmssdk.KeyTypeSigning,
	})

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("signs_data_with_rsa_key", func(t *testing.T) {
		data := []byte("data to sign")
		resp, err := server.Sign(context.Background(), &xkmspb.SignRequest{
			KeyId:   "rsa-sign-key",
			Backend: "test-be",
			Data:    data,
			Hash:    "sha256",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Signature)
	})

	t.Run("signs_data_with_ed25519_key", func(t *testing.T) {
		data := []byte("ed25519 data to sign")
		resp, err := server.Sign(context.Background(), &xkmspb.SignRequest{
			KeyId:   "ed25519-sign-key",
			Backend: "test-be",
			Data:    data,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Signature)
	})

	t.Run("sign_empty_data_non_ed25519_returns_error", func(t *testing.T) {
		_, err := server.Sign(context.Background(), &xkmspb.SignRequest{
			KeyId:   "rsa-sign-key",
			Backend: "test-be",
			Data:    nil,
			Hash:    "sha256",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("sign_key_not_found", func(t *testing.T) {
		_, err := server.Sign(context.Background(), &xkmspb.SignRequest{
			KeyId:   "nonexistent-key",
			Backend: "test-be",
			Data:    []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

// =============================================================================
// Verify with Backend Tests
// =============================================================================

func TestXKMSServer_Verify_WithBackend(t *testing.T) {
	backend := newMockXKMSBackend()

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	backend.keyProvider.keys["rsa-verify-key"] = rsaKey
	backend.keyProvider.keyAttrs = append(backend.keyProvider.keyAttrs, &xkmssdk.KeyAttributes{
		CN: "rsa-verify-key", KeyAlgorithm: x509.RSA, KeyType: xkmssdk.KeyTypeSigning,
	})

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	backend.keyProvider.keys["ecdsa-verify-key"] = ecKey
	backend.keyProvider.keyAttrs = append(backend.keyProvider.keyAttrs, &xkmssdk.KeyAttributes{
		CN: "ecdsa-verify-key", KeyAlgorithm: x509.ECDSA, KeyType: xkmssdk.KeyTypeSigning,
	})

	_, edPriv, edErr := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, edErr)
	backend.keyProvider.keys["ed25519-verify-key"] = edPriv
	backend.keyProvider.keyAttrs = append(backend.keyProvider.keyAttrs, &xkmssdk.KeyAttributes{
		CN: "ed25519-verify-key", KeyAlgorithm: x509.Ed25519, KeyType: xkmssdk.KeyTypeSigning,
	})

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("verifies_valid_rsa_signature", func(t *testing.T) {
		data := []byte("verify this data")
		hasher := crypto.SHA256.New()
		hasher.Write(data)
		digest := hasher.Sum(nil)

		sig, signErr := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest)
		require.NoError(t, signErr)

		resp, verifyErr := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:     "rsa-verify-key",
			Backend:   "test-be",
			Data:      data,
			Signature: sig,
			Hash:      "sha256",
		})
		require.NoError(t, verifyErr)
		require.NotNil(t, resp)
		assert.True(t, resp.Valid)
		assert.Contains(t, resp.Message, "valid")
	})

	t.Run("detects_invalid_rsa_signature", func(t *testing.T) {
		data := []byte("verify this data")
		resp, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:     "rsa-verify-key",
			Backend:   "test-be",
			Data:      data,
			Signature: []byte("invalid-signature"),
			Hash:      "sha256",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.False(t, resp.Valid)
		assert.Contains(t, resp.Message, "invalid")
	})

	t.Run("verifies_valid_ecdsa_signature", func(t *testing.T) {
		data := []byte("ecdsa verify data")
		hasher := crypto.SHA256.New()
		hasher.Write(data)
		digest := hasher.Sum(nil)

		sig, signErr := ecdsa.SignASN1(rand.Reader, ecKey, digest)
		require.NoError(t, signErr)

		resp, verifyErr := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:     "ecdsa-verify-key",
			Backend:   "test-be",
			Data:      data,
			Signature: sig,
			Hash:      "sha256",
		})
		require.NoError(t, verifyErr)
		require.NotNil(t, resp)
		assert.True(t, resp.Valid)
	})

	t.Run("verifies_valid_ed25519_signature", func(t *testing.T) {
		data := []byte("ed25519 verify data")
		sig := ed25519.Sign(edPriv, data)

		resp, verifyErr := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:     "ed25519-verify-key",
			Backend:   "test-be",
			Data:      data,
			Signature: sig,
		})
		require.NoError(t, verifyErr)
		require.NotNil(t, resp)
		assert.True(t, resp.Valid)
		assert.Contains(t, resp.Message, "valid")
	})

	t.Run("detects_invalid_ed25519_signature", func(t *testing.T) {
		data := []byte("ed25519 verify data")
		resp, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:     "ed25519-verify-key",
			Backend:   "test-be",
			Data:      data,
			Signature: []byte("bad-sig"),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.False(t, resp.Valid)
	})

	t.Run("verify_empty_data_non_ed25519_returns_error", func(t *testing.T) {
		_, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:     "rsa-verify-key",
			Backend:   "test-be",
			Data:      nil,
			Signature: []byte("sig"),
			Hash:      "sha256",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("verify_key_not_found", func(t *testing.T) {
		_, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:     "nonexistent",
			Backend:   "test-be",
			Data:      []byte("data"),
			Signature: []byte("sig"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

// =============================================================================
// Seal and Unseal with Backend Tests
// =============================================================================

func TestXKMSServer_Seal_WithBackend(t *testing.T) {
	backend := newMockXKMSBackend()
	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("seals_data_without_key", func(t *testing.T) {
		resp, err := server.Seal(context.Background(), &xkmspb.SealRequest{
			Backend: "test-be",
			Data:    []byte("secret data"),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Ciphertext)
	})

	t.Run("seals_data_with_key_id", func(t *testing.T) {
		backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
			{CN: "seal-key", KeyAlgorithm: x509.ECDSA},
		}

		resp, err := server.Seal(context.Background(), &xkmspb.SealRequest{
			Backend: "test-be",
			Data:    []byte("seal with key"),
			KeyId:   "seal-key",
			Aad:     []byte("additional-data"),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Ciphertext)
	})

	t.Run("seal_with_nonexistent_key_returns_error", func(t *testing.T) {
		backend.keyProvider.keyAttrs = nil

		_, err := server.Seal(context.Background(), &xkmspb.SealRequest{
			Backend: "test-be",
			Data:    []byte("seal data"),
			KeyId:   "nonexistent-key",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

func TestXKMSServer_Unseal_WithBackend(t *testing.T) {
	backend := newMockXKMSBackend()
	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("unseals_data_without_key", func(t *testing.T) {
		resp, err := server.Unseal(context.Background(), &xkmspb.UnsealRequest{
			Backend:    "test-be",
			Ciphertext: []byte("encrypted data"),
			Nonce:      []byte("nonce"),
			Tag:        []byte("tag"),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Plaintext)
	})

	t.Run("unseals_data_with_key_id", func(t *testing.T) {
		backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
			{CN: "unseal-key", KeyAlgorithm: x509.ECDSA},
		}

		resp, err := server.Unseal(context.Background(), &xkmspb.UnsealRequest{
			Backend:    "test-be",
			Ciphertext: []byte("encrypted data"),
			KeyId:      "unseal-key",
			Aad:        []byte("aad"),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Plaintext)
	})

	t.Run("unseal_nonexistent_backend_returns_error", func(t *testing.T) {
		_, err := server.Unseal(context.Background(), &xkmspb.UnsealRequest{
			Backend:    "nonexistent",
			Ciphertext: []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("unseal_nonexistent_key_returns_error", func(t *testing.T) {
		backend.keyProvider.keyAttrs = nil

		_, err := server.Unseal(context.Background(), &xkmspb.UnsealRequest{
			Backend:    "test-be",
			Ciphertext: []byte("data"),
			KeyId:      "nonexistent-key",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

// =============================================================================
// Encrypt / Decrypt with Backend Tests
// =============================================================================

func TestXKMSServer_Encrypt_NonSymmetricBackend(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "asym-key", KeyAlgorithm: x509.ECDSA},
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("encrypt_returns_unimplemented_for_non_symmetric_backend", func(t *testing.T) {
		_, err := server.Encrypt(context.Background(), &xkmspb.EncryptRequest{
			KeyId:     "asym-key",
			Backend:   "test-be",
			Plaintext: []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})
}

func TestXKMSServer_Decrypt_WithBackend(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "decrypt-key", KeyAlgorithm: x509.ECDSA, KeyType: xkmssdk.KeyTypeSigning},
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("decrypt_backend_not_found", func(t *testing.T) {
		_, err := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
			KeyId:      "key",
			Backend:    "nonexistent",
			Ciphertext: []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("decrypt_key_not_found", func(t *testing.T) {
		_, err := server.Decrypt(context.Background(), &xkmspb.DecryptRequest{
			KeyId:      "nonexistent",
			Backend:    "test-be",
			Ciphertext: []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

// =============================================================================
// GetKey with non-signing key type Tests
// =============================================================================

func TestXKMSServer_GetKey_NonSigningKey(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "encrypt-key", KeyAlgorithm: x509.RSA, KeyType: xkmssdk.KeyTypeEncryption,
			RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 2048}},
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("returns_key_without_public_key_for_non_signing_type", func(t *testing.T) {
		resp, err := server.GetKey(context.Background(), &xkmspb.GetKeyRequest{
			KeyId:   "encrypt-key",
			Backend: "test-be",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Key)
		assert.Equal(t, "encrypt-key", resp.Key.KeyId)
		assert.Equal(t, int32(2048), resp.Key.KeySize)
		// PublicKeyPem should be empty for non-signing keys
		assert.Empty(t, resp.PublicKeyPem)
	})
}

func TestXKMSServer_GetKey_ECDSAKey(t *testing.T) {
	backend := newMockXKMSBackend()
	ecKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)

	backend.keyProvider.keys["ecdsa-key"] = ecKey
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "ecdsa-key", KeyAlgorithm: x509.ECDSA, KeyType: xkmssdk.KeyTypeSigning,
			ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P384()}},
	}

	cleanup := setupXKMSBackend(t, "test-be", backend)
	defer cleanup()

	server := newTestXKMSServer()

	t.Run("returns_ecdsa_key_info_with_curve", func(t *testing.T) {
		resp, err := server.GetKey(context.Background(), &xkmspb.GetKeyRequest{
			KeyId:   "ecdsa-key",
			Backend: "test-be",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Key)
		assert.Equal(t, "ECDSA", resp.Key.Algorithm)
		assert.NotEmpty(t, resp.Key.Curve)
		assert.NotEmpty(t, resp.PublicKeyPem)
	})
}
