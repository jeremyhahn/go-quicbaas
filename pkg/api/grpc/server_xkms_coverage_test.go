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

// =============================================================================
// Seal / Unseal with Mock Backend
// =============================================================================

func TestXKMSServer_Seal_WithMockBackend(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("seals_data_successfully", func(t *testing.T) {
		resp, err := server.Seal(context.Background(), &xkmspb.SealRequest{
			Backend: "test-be",
			Data:    []byte("sensitive-data"),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Ciphertext)
		assert.Equal(t, string(xkmssdk.BackendTypeSoftware), resp.Backend)
	})

	t.Run("seals_data_with_aad", func(t *testing.T) {
		resp, err := server.Seal(context.Background(), &xkmspb.SealRequest{
			Backend: "test-be",
			Data:    []byte("sensitive-data"),
			Aad:     []byte("additional-auth-data"),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Ciphertext)
	})

	t.Run("seal_with_key_id_not_found", func(t *testing.T) {
		_, err := server.Seal(context.Background(), &xkmspb.SealRequest{
			Backend: "test-be",
			Data:    []byte("sensitive-data"),
			KeyId:   "nonexistent-key",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
		assert.Contains(t, st.Message(), "key not found")
	})

	t.Run("seal_with_nonexistent_backend_and_key_id", func(t *testing.T) {
		_, err := server.Seal(context.Background(), &xkmspb.SealRequest{
			Backend: "nonexistent",
			Data:    []byte("data"),
			KeyId:   "some-key",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
		assert.Contains(t, st.Message(), "backend not found")
	})
}

func TestXKMSServer_Unseal_WithMockBackend(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("unseals_data_successfully", func(t *testing.T) {
		resp, err := server.Unseal(context.Background(), &xkmspb.UnsealRequest{
			Backend:    "test-be",
			Ciphertext: []byte("sealed-data"),
			Nonce:      []byte("test-nonce"),
			Tag:        []byte("test-tag"),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Plaintext)
	})

	t.Run("unseals_with_aad", func(t *testing.T) {
		resp, err := server.Unseal(context.Background(), &xkmspb.UnsealRequest{
			Backend:    "test-be",
			Ciphertext: []byte("sealed-data"),
			Nonce:      []byte("test-nonce"),
			Tag:        []byte("test-tag"),
			Aad:        []byte("additional-auth-data"),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("unseal_nonexistent_backend_returns_not_found", func(t *testing.T) {
		_, err := server.Unseal(context.Background(), &xkmspb.UnsealRequest{
			Backend:    "nonexistent",
			Ciphertext: []byte("data"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("unseal_with_key_id_not_found", func(t *testing.T) {
		_, err := server.Unseal(context.Background(), &xkmspb.UnsealRequest{
			Backend:    "test-be",
			Ciphertext: []byte("data"),
			KeyId:      "nonexistent-key",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
		assert.Contains(t, st.Message(), "key not found")
	})
}

// =============================================================================
// xkmsExtractPublicKeyPEM Tests
// =============================================================================

func TestXkmsExtractPublicKeyPEM(t *testing.T) {
	t.Run("extracts_from_rsa_private_key", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		pem := xkmsExtractPublicKeyPEM(key)
		assert.Contains(t, pem, "PUBLIC KEY")
	})

	t.Run("extracts_from_ecdsa_private_key", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		pem := xkmsExtractPublicKeyPEM(key)
		assert.Contains(t, pem, "PUBLIC KEY")
	})

	t.Run("extracts_from_ed25519_private_key", func(t *testing.T) {
		_, privKey, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		pem := xkmsExtractPublicKeyPEM(privKey)
		assert.Contains(t, pem, "PUBLIC KEY")
	})

	t.Run("extracts_from_crypto_signer", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		// Pass as crypto.Signer (not as *ecdsa.PrivateKey)
		var signer crypto.Signer = key
		pem := xkmsExtractPublicKeyPEM(signer)
		assert.Contains(t, pem, "PUBLIC KEY")
	})

	t.Run("returns_empty_for_unsupported_type", func(t *testing.T) {
		pem := xkmsExtractPublicKeyPEM("not-a-key")
		assert.Empty(t, pem)
	})

	t.Run("returns_empty_for_nil", func(t *testing.T) {
		pem := xkmsExtractPublicKeyPEM(nil)
		assert.Empty(t, pem)
	})
}

// =============================================================================
// xkmsFindKeyAttributes Tests
// =============================================================================

func TestXkmsFindKeyAttributes(t *testing.T) {
	backend := newMockXKMSBackend()
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "key-1", KeyAlgorithm: x509.ECDSA},
		{CN: "key-2", KeyAlgorithm: x509.RSA},
	}

	t.Run("finds_existing_key", func(t *testing.T) {
		attrs, err := xkmsFindKeyAttributes(backend, "key-1")
		require.NoError(t, err)
		assert.Equal(t, "key-1", attrs.CN)
	})

	t.Run("returns_error_for_missing_key", func(t *testing.T) {
		_, err := xkmsFindKeyAttributes(backend, "nonexistent")
		require.Error(t, err)
		var keyNotFound *XKMSKeyNotFoundError
		assert.ErrorAs(t, err, &keyNotFound)
		assert.Equal(t, "nonexistent", keyNotFound.KeyID)
	})
}

// =============================================================================
// Certificate Operations - Error Paths (no backends)
// =============================================================================

func TestXKMSServer_SaveCert_NoBackends(t *testing.T) {
	xkmssdk.ServiceReset()
	server := newTestXKMSServer()

	t.Run("returns_unavailable_when_no_backends", func(t *testing.T) {
		_, err := server.SaveCert(context.Background(), &xkmspb.SaveCertRequest{
			KeyId:   "test",
			CertPem: "-----BEGIN CERTIFICATE-----\ndata\n-----END CERTIFICATE-----",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

func TestXKMSServer_GetCert_NoBackends(t *testing.T) {
	xkmssdk.ServiceReset()
	server := newTestXKMSServer()

	t.Run("returns_unavailable_when_no_backends", func(t *testing.T) {
		_, err := server.GetCert(context.Background(), &xkmspb.GetCertRequest{
			KeyId: "test",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

func TestXKMSServer_DeleteCert_NoBackends(t *testing.T) {
	xkmssdk.ServiceReset()
	server := newTestXKMSServer()

	t.Run("returns_unavailable_when_no_backends", func(t *testing.T) {
		_, err := server.DeleteCert(context.Background(), &xkmspb.DeleteCertRequest{
			KeyId: "test",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

// =============================================================================
// SaveCert - Bad Certificate Parse
// =============================================================================

func TestXKMSServer_SaveCert_InvalidCertificateData(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("returns_error_for_valid_pem_with_bad_certificate", func(t *testing.T) {
		// PEM block with garbage certificate data
		badPEM := "-----BEGIN CERTIFICATE-----\nZGF0YQ==\n-----END CERTIFICATE-----"
		_, err := server.SaveCert(context.Background(), &xkmspb.SaveCertRequest{
			KeyId:   "test-key",
			CertPem: badPEM,
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "failed to parse certificate")
	})
}

// =============================================================================
// ListKeys - Pagination Edge Cases
// =============================================================================

func TestXKMSServer_ListKeys_Pagination(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "key-1", KeyAlgorithm: x509.ECDSA, KeyType: xkmssdk.KeyTypeSigning},
		{CN: "key-2", KeyAlgorithm: x509.RSA, KeyType: xkmssdk.KeyTypeSigning, RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 2048}},
		{CN: "key-3", KeyAlgorithm: x509.ECDSA, KeyType: xkmssdk.KeyTypeSigning, ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()}},
	}

	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("returns_all_keys_with_default_limit", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{
			Backend: "test-be",
		})
		require.NoError(t, err)
		assert.Len(t, resp.Keys, 3)
		assert.Equal(t, int32(3), resp.Total)
	})

	t.Run("paginates_with_limit", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{
			Backend: "test-be",
			Limit:   2,
		})
		require.NoError(t, err)
		assert.Len(t, resp.Keys, 2)
		assert.Equal(t, int32(3), resp.Total)
	})

	t.Run("paginates_with_offset", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{
			Backend: "test-be",
			Limit:   10,
			Offset:  2,
		})
		require.NoError(t, err)
		assert.Len(t, resp.Keys, 1)
		assert.Equal(t, "key-3", resp.Keys[0].KeyId)
	})

	t.Run("offset_beyond_total_returns_empty", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{
			Backend: "test-be",
			Offset:  100,
		})
		require.NoError(t, err)
		assert.Empty(t, resp.Keys)
	})

	t.Run("includes_rsa_key_size", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{
			Backend: "test-be",
			Limit:   10,
			Offset:  1,
		})
		require.NoError(t, err)
		require.Len(t, resp.Keys, 2)
		assert.Equal(t, int32(2048), resp.Keys[0].KeySize)
	})

	t.Run("includes_ecc_curve", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &xkmspb.ListKeysRequest{
			Backend: "test-be",
			Limit:   1,
			Offset:  2,
		})
		require.NoError(t, err)
		require.Len(t, resp.Keys, 1)
		assert.NotEmpty(t, resp.Keys[0].Curve)
	})
}

// =============================================================================
// Sign / Verify with Ed25519
// =============================================================================

func TestXKMSServer_Sign_Ed25519(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	_, edKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	backend.keyProvider.keys["ed-key"] = edKey
	backend.keyProvider.keyAttrs = []*xkmssdk.KeyAttributes{
		{CN: "ed-key", KeyAlgorithm: x509.Ed25519, KeyType: xkmssdk.KeyTypeSigning},
	}

	err = xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("signs_with_ed25519", func(t *testing.T) {
		resp, err := server.Sign(context.Background(), &xkmspb.SignRequest{
			KeyId:   "ed-key",
			Backend: "test-be",
			Data:    []byte("test data"),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.NotEmpty(t, resp.Signature)
	})

	t.Run("verifies_ed25519_signature", func(t *testing.T) {
		signResp, err := server.Sign(context.Background(), &xkmspb.SignRequest{
			KeyId:   "ed-key",
			Backend: "test-be",
			Data:    []byte("test data"),
		})
		require.NoError(t, err)

		verifyResp, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:     "ed-key",
			Backend:   "test-be",
			Data:      []byte("test data"),
			Signature: signResp.Signature,
		})
		require.NoError(t, err)
		assert.True(t, verifyResp.Valid)
		assert.Contains(t, verifyResp.Message, "valid")
	})

	t.Run("invalid_ed25519_signature_returns_false", func(t *testing.T) {
		verifyResp, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:     "ed-key",
			Backend:   "test-be",
			Data:      []byte("test data"),
			Signature: []byte("bad-signature-that-is-not-valid"),
		})
		require.NoError(t, err)
		assert.False(t, verifyResp.Valid)
		assert.Contains(t, verifyResp.Message, "invalid")
	})
}

// =============================================================================
// Sign / Verify with ECDSA - data required check
// =============================================================================

func TestXKMSServer_Sign_ECDSA_EmptyData(t *testing.T) {
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

	t.Run("returns_error_for_empty_data_with_non_ed25519", func(t *testing.T) {
		_, err := server.Sign(context.Background(), &xkmspb.SignRequest{
			KeyId:   "ec-key",
			Backend: "test-be",
			Data:    nil,
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "data is required")
	})
}

// =============================================================================
// Verify with ECDSA - data required check
// =============================================================================

func TestXKMSServer_Verify_ECDSA_EmptyData(t *testing.T) {
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

	t.Run("returns_error_for_empty_data_with_non_ed25519", func(t *testing.T) {
		_, err := server.Verify(context.Background(), &xkmspb.VerifyRequest{
			KeyId:     "ec-key",
			Backend:   "test-be",
			Data:      nil,
			Signature: []byte("some-sig"),
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "data is required")
	})
}

// =============================================================================
// ListBackends with Capabilities
// =============================================================================

func TestXKMSServer_ListBackends_WithCapabilities(t *testing.T) {
	xkmssdk.ServiceReset()
	backend := newMockXKMSBackend()
	err := xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends:       map[string]xkmssdk.ServiceBackend{"test-be": backend},
		DefaultBackend: "test-be",
	})
	require.NoError(t, err)
	defer xkmssdk.ServiceReset()

	server := newTestXKMSServer()

	t.Run("returns_backend_with_capabilities", func(t *testing.T) {
		resp, err := server.ListBackends(context.Background(), &xkmspb.ListBackendsRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Backends, 1)
		b := resp.Backends[0]
		assert.Equal(t, "test-be", b.Name)
		assert.True(t, b.SupportsSigning)
		assert.True(t, b.SupportsDecryption)
		assert.True(t, b.SupportsRotation)
	})
}
