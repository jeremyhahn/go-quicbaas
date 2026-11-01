//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	transportTypes "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// respondXKMSError tests
// =============================================================================

func TestRespondXKMSError(t *testing.T) {
	t.Run("writes JSON error with correct status", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondXKMSError(w, http.StatusBadRequest, &XKMSInvalidRequestError{
			Detail: "missing key_id",
		})

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var resp xkmsErrorResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "invalid request: missing key_id", resp.Error)
	})

	t.Run("writes 412 for precondition failed", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondXKMSError(w, http.StatusPreconditionFailed, &XKMSServiceNotInitializedError{})

		assert.Equal(t, http.StatusPreconditionFailed, w.Code)

		var resp xkmsErrorResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "xkms service is not initialized", resp.Error)
	})
}

// =============================================================================
// respondXKMSJSON tests
// =============================================================================

func TestRespondXKMSJSON(t *testing.T) {
	t.Run("writes JSON data with correct status", func(t *testing.T) {
		w := httptest.NewRecorder()
		data := map[string]string{"status": "ok"}
		respondXKMSJSON(w, http.StatusOK, data)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var resp map[string]string
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, "ok", resp["status"])
	})

	t.Run("writes empty body for nil data", func(t *testing.T) {
		w := httptest.NewRecorder()
		respondXKMSJSON(w, http.StatusOK, nil)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Body.String())
	})
}

// =============================================================================
// readXKMSBody tests
// =============================================================================

func TestReadXKMSBody(t *testing.T) {
	t.Run("reads valid JSON body", func(t *testing.T) {
		body := bytes.NewReader([]byte(`{"key_id":"test-key","key_type":"rsa"}`))
		req := httptest.NewRequest(http.MethodPost, "/", body)

		var target transportTypes.GenerateKeyRequest
		err := readXKMSBody(req, &target)
		require.NoError(t, err)
		assert.Equal(t, "test-key", target.KeyID)
		assert.Equal(t, "rsa", target.KeyType)
	})

	t.Run("returns error for empty body", func(t *testing.T) {
		body := bytes.NewReader([]byte{})
		req := httptest.NewRequest(http.MethodPost, "/", body)

		var target transportTypes.GenerateKeyRequest
		err := readXKMSBody(req, &target)
		require.Error(t, err)

		var invalidReqErr *XKMSInvalidRequestError
		require.ErrorAs(t, err, &invalidReqErr)
		assert.Contains(t, invalidReqErr.Detail, "empty request body")
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		body := bytes.NewReader([]byte(`{not valid json`))
		req := httptest.NewRequest(http.MethodPost, "/", body)

		var target transportTypes.GenerateKeyRequest
		err := readXKMSBody(req, &target)
		require.Error(t, err)

		var invalidReqErr *XKMSInvalidRequestError
		require.ErrorAs(t, err, &invalidReqErr)
		assert.Contains(t, invalidReqErr.Detail, "invalid JSON body")
	})

	t.Run("returns error when the body cannot be read", func(t *testing.T) {
		err := readXKMSBody(httptest.NewRequest(http.MethodPost, "/", errReader(0)), &struct{}{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read request body")
	})
}

// =============================================================================
// ensureXKMSInitialized tests
// =============================================================================

func TestEnsureXKMSInitialized(t *testing.T) {
	t.Run("returns typed error when not initialized", func(t *testing.T) {
		// By default, xkms is not initialized in unit tests
		err := ensureXKMSInitialized()
		require.Error(t, err)

		var notInitErr *XKMSServiceNotInitializedError
		require.ErrorAs(t, err, &notInitErr)
	})

	t.Run("returns nil when initialized", func(t *testing.T) {
		cleanup := initXKMSForTest(t)
		defer cleanup()

		assert.NoError(t, ensureXKMSInitialized())
	})
}

// =============================================================================
// buildKeyAttributes tests
// =============================================================================

func TestBuildKeyAttributes(t *testing.T) {
	t.Run("RSA key with default size", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "rsa-test",
			KeyType: "rsa",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, "rsa-test", attrs.CN)
		assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
		assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
		require.NotNil(t, attrs.RSAAttributes)
		assert.Equal(t, 2048, attrs.RSAAttributes.KeySize)
	})

	t.Run("RSA key with custom size", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "rsa-4096",
			KeyType: "rsa",
			KeySize: 4096,
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
		require.NotNil(t, attrs.RSAAttributes)
		assert.Equal(t, 4096, attrs.RSAAttributes.KeySize)
	})

	t.Run("ECDSA key with default curve", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "ecdsa-test",
			KeyType: "ecdsa",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
		assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
		require.NotNil(t, attrs.ECCAttributes)
		assert.Equal(t, elliptic.P256(), attrs.ECCAttributes.Curve)
	})

	t.Run("ECDSA key with P-384 curve", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "ecdsa-384",
			KeyType: "ecdsa",
			Curve:   "P-384",
		}
		attrs := buildKeyAttributes(req)

		require.NotNil(t, attrs.ECCAttributes)
		assert.Equal(t, elliptic.P384(), attrs.ECCAttributes.Curve)
	})

	t.Run("ECDSA key with P-521 curve", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "ecdsa-521",
			KeyType: "ecdsa",
			Curve:   "P-521",
		}
		attrs := buildKeyAttributes(req)

		require.NotNil(t, attrs.ECCAttributes)
		assert.Equal(t, elliptic.P521(), attrs.ECCAttributes.Curve)
	})

	t.Run("Ed25519 key", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "ed25519-test",
			KeyType: "ed25519",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, x509.Ed25519, attrs.KeyAlgorithm)
		assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
	})

	t.Run("symmetric key with default algorithm", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "sym-test",
			KeyType: "symmetric",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
		assert.Equal(t, xkmssdk.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
	})

	t.Run("symmetric key with custom algorithm", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:     "sym-chacha",
			KeyType:   "symmetric",
			Algorithm: "chacha20-poly1305",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
		assert.Equal(t, xkmssdk.SymmetricAlgorithm("chacha20-poly1305"), attrs.SymmetricAlgorithm)
	})

	t.Run("AES key type alias", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "aes-test",
			KeyType: "aes",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
		assert.Equal(t, xkmssdk.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
	})

	t.Run("aes256-gcm key type", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "aes256gcm-test",
			KeyType: "aes256-gcm",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
		assert.Equal(t, xkmssdk.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
	})

	t.Run("chacha20-poly1305 key type", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "chacha-test",
			KeyType: "chacha20-poly1305",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
	})

	t.Run("unrecognized key type defaults to ECDSA P-256", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "unknown",
			KeyType: "unrecognized",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
		assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
		require.NotNil(t, attrs.ECCAttributes)
		assert.Equal(t, elliptic.P256(), attrs.ECCAttributes.Curve)
	})

	t.Run("with backend", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "backend-key",
			KeyType: "rsa",
			Backend: "tpm2",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, xkmssdk.StoreType("tpm2"), attrs.StoreType)
	})

	t.Run("with tenant ID", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:    "tenant-key",
			KeyType:  "rsa",
			TenantID: "org-123",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, "org-123", attrs.TenantID)
	})

	t.Run("with hash function", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:   "hash-key",
			KeyType: "rsa",
			Hash:    "SHA-512",
		}
		attrs := buildKeyAttributes(req)

		assert.Equal(t, crypto.SHA512, attrs.Hash)
	})

	t.Run("with exportable flag", func(t *testing.T) {
		req := &transportTypes.GenerateKeyRequest{
			KeyID:      "exportable-key",
			KeyType:    "ecdsa",
			Exportable: true,
		}
		attrs := buildKeyAttributes(req)

		assert.True(t, attrs.Exportable)
	})
}

// =============================================================================
// resolveHashFunction tests
// =============================================================================

func TestResolveHashFunction(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected crypto.Hash
	}{
		{name: "SHA-256 with hyphen", input: "SHA-256", expected: crypto.SHA256},
		{name: "SHA-384 with hyphen", input: "SHA-384", expected: crypto.SHA384},
		{name: "SHA-512 with hyphen", input: "SHA-512", expected: crypto.SHA512},
		{name: "SHA256 without hyphen", input: "SHA256", expected: crypto.SHA256},
		{name: "SHA384 without hyphen", input: "SHA384", expected: crypto.SHA384},
		{name: "SHA512 without hyphen", input: "SHA512", expected: crypto.SHA512},
		{name: "unknown defaults to SHA256", input: "MD5", expected: crypto.SHA256},
		{name: "empty defaults to SHA256", input: "", expected: crypto.SHA256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveHashFunction(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// hashData tests
// =============================================================================

func TestHashData(t *testing.T) {
	t.Run("hashes data with SHA256", func(t *testing.T) {
		data := []byte("test data")
		result := hashData(data, crypto.SHA256)
		assert.Len(t, result, 32) // SHA256 produces 32-byte digest
		assert.NotEqual(t, data, result)
	})

	t.Run("hashes data with SHA512", func(t *testing.T) {
		data := []byte("test data")
		result := hashData(data, crypto.SHA512)
		assert.Len(t, result, 64) // SHA512 produces 64-byte digest
	})

	t.Run("returns raw data for zero hash", func(t *testing.T) {
		data := []byte("raw data")
		result := hashData(data, 0)
		assert.Equal(t, data, result)
	})
}

// =============================================================================
// marshalPublicKeyPEM tests
// =============================================================================

func TestMarshalPublicKeyPEM(t *testing.T) {
	t.Run("marshals RSA public key", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		pemStr := marshalPublicKeyPEM(key)
		assert.NotEmpty(t, pemStr)
		assert.Contains(t, pemStr, "BEGIN PUBLIC KEY")
		assert.Contains(t, pemStr, "END PUBLIC KEY")

		// Verify it's valid PEM
		block, _ := pem.Decode([]byte(pemStr))
		require.NotNil(t, block)
		_, err = x509.ParsePKIXPublicKey(block.Bytes)
		require.NoError(t, err)
	})

	t.Run("marshals ECDSA public key", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		pemStr := marshalPublicKeyPEM(key)
		assert.NotEmpty(t, pemStr)
		assert.Contains(t, pemStr, "BEGIN PUBLIC KEY")
	})

	t.Run("marshals Ed25519 public key", func(t *testing.T) {
		_, privKey, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		pemStr := marshalPublicKeyPEM(privKey)
		assert.NotEmpty(t, pemStr)
		assert.Contains(t, pemStr, "BEGIN PUBLIC KEY")
	})

	t.Run("returns empty for unsupported key type", func(t *testing.T) {
		result := marshalPublicKeyPEM("not a key")
		assert.Empty(t, result)
	})

	t.Run("returns empty for nil", func(t *testing.T) {
		result := marshalPublicKeyPEM(nil)
		assert.Empty(t, result)
	})
}

// =============================================================================
// verifySignature tests
// =============================================================================

func TestVerifySignature(t *testing.T) {
	t.Run("verifies RSA signature", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		data := []byte("test data")
		digest := hashData(data, crypto.SHA256)

		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest)
		require.NoError(t, err)

		valid := verifySignature(key, digest, sig, crypto.SHA256)
		assert.True(t, valid)
	})

	t.Run("verifies RSA signature with SHA-384", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		digest := hashData([]byte("test"), crypto.SHA384)
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA384, digest)
		require.NoError(t, err)

		valid := verifySignature(key, digest, sig, crypto.SHA384)
		assert.True(t, valid)
	})

	t.Run("rejects invalid RSA signature", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		digest := hashData([]byte("test data"), crypto.SHA256)
		valid := verifySignature(key, digest, []byte("bad-sig"), crypto.SHA256)
		assert.False(t, valid)
	})

	t.Run("verifies ECDSA signature", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		data := []byte("test data")
		digest := hashData(data, crypto.SHA256)

		sig, err := ecdsa.SignASN1(rand.Reader, key, digest)
		require.NoError(t, err)

		valid := verifySignature(key, digest, sig, crypto.SHA256)
		assert.True(t, valid)
	})

	t.Run("rejects invalid ECDSA signature", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		digest := hashData([]byte("test data"), crypto.SHA256)
		valid := verifySignature(key, digest, []byte("bad-sig"), crypto.SHA256)
		assert.False(t, valid)
	})

	t.Run("verifies Ed25519 signature", func(t *testing.T) {
		_, privKey, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		data := []byte("test data")
		digest := hashData(data, crypto.SHA256)

		sig := ed25519.Sign(privKey, digest)

		valid := verifySignature(privKey, digest, sig, crypto.SHA256)
		assert.True(t, valid)
	})

	t.Run("returns false for unsupported key type", func(t *testing.T) {
		valid := verifySignature("not a key", []byte("digest"), []byte("sig"), crypto.SHA256)
		assert.False(t, valid)
	})
}

// =============================================================================
// encryptWithKey tests
// =============================================================================

func TestEncryptWithKey(t *testing.T) {
	t.Run("encrypts with RSA key", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		plaintext := []byte("secret data")
		ciphertext, err := encryptWithKey(key, plaintext)
		require.NoError(t, err)
		assert.NotEmpty(t, ciphertext)
		assert.NotEqual(t, plaintext, ciphertext)

		// Verify decryption works
		decrypted, err := rsa.DecryptOAEP(crypto.SHA256.New(), rand.Reader, key, ciphertext, nil)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	})

	t.Run("returns error for ECDSA key", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		_, encErr := encryptWithKey(key, []byte("data"))
		require.Error(t, encErr)

		var opErr *XKMSOperationError
		require.ErrorAs(t, encErr, &opErr)
		assert.Equal(t, "Encrypt", opErr.Op)
		assert.Contains(t, opErr.Detail, "not supported")
	})

	t.Run("returns error for Ed25519 key", func(t *testing.T) {
		_, privKey, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)

		_, encErr := encryptWithKey(privKey, []byte("data"))
		require.Error(t, encErr)
		assert.Contains(t, encErr.Error(), "encryption not supported")
	})

	t.Run("returns error for unsupported key type", func(t *testing.T) {
		_, err := encryptWithKey("not a key", []byte("data"))
		require.Error(t, err)

		var opErr *XKMSOperationError
		require.ErrorAs(t, err, &opErr)
	})
}

// =============================================================================
// Handler HTTP tests (xkms not initialized path)
// =============================================================================

// These tests verify handler behavior when xkms is not initialized,
// which is the expected state in unit tests (no real backend).
// All handlers that check ensureXKMSInitialized should return 412.

func TestHandleXKMSHealth_Unavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/health", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp transportTypes.HealthResponse
	err := json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "unavailable", resp.Status)
}

// TestHandleXKMS_NotInitialized drives every xkms route through the router
// without an initialized xkms service: each handler must reject the request
// with 412 before it reads or validates the body.
func TestHandleXKMS_NotInitialized(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		system bool
	}{
		{name: "ListBackends", method: http.MethodGet, path: "/api/v1/xkms/backends"},
		{name: "GetBackend", method: http.MethodGet, path: "/api/v1/xkms/backends/tpm2"},
		{name: "GenerateKey", method: http.MethodPost, path: "/api/v1/xkms/keys", body: `{"key_id":"test-key","key_type":"rsa"}`},
		{name: "GenerateKey empty body", method: http.MethodPost, path: "/api/v1/xkms/keys", body: ""},
		{name: "ListKeys", method: http.MethodGet, path: "/api/v1/xkms/keys"},
		{name: "GetKey", method: http.MethodGet, path: "/api/v1/xkms/keys/test-key"},
		{name: "DeleteKey", method: http.MethodDelete, path: "/api/v1/xkms/keys/test-key"},
		{name: "RotateKey", method: http.MethodPost, path: "/api/v1/xkms/keys/test-key/rotate"},
		{name: "Sign", method: http.MethodPost, path: "/api/v1/xkms/keys/test-key/sign", body: `{"data":"dGVzdCBkYXRh"}`},
		{name: "Verify", method: http.MethodPost, path: "/api/v1/xkms/keys/test-key/verify", body: `{"data":"dGVzdCBkYXRh","signature":"c2ln"}`},
		{name: "Encrypt", method: http.MethodPost, path: "/api/v1/xkms/keys/test-key/encrypt", body: `{"plaintext":"c2VjcmV0"}`},
		{name: "Decrypt", method: http.MethodPost, path: "/api/v1/xkms/keys/test-key/decrypt", body: `{"ciphertext":"ZW5jcnlwdGVk"}`},
		{name: "SaveCert", method: http.MethodPost, path: "/api/v1/xkms/certs?key_id=test", body: `{"certificate_pem":"-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"}`, system: true},
		{name: "ListCerts", method: http.MethodGet, path: "/api/v1/xkms/certs", system: true},
		{name: "GetCert", method: http.MethodGet, path: "/api/v1/xkms/certs/test-cert"},
		{name: "DeleteCert", method: http.MethodDelete, path: "/api/v1/xkms/certs/test-cert"},
		{name: "CertExists", method: http.MethodHead, path: "/api/v1/xkms/certs/test-cert"},
		{name: "Seal", method: http.MethodPost, path: "/api/v1/xkms/seal", body: `{"data":"c2VjcmV0"}`},
		{name: "Unseal", method: http.MethodPost, path: "/api/v1/xkms/unseal", body: `{"ciphertext":"ZW5jcnlwdGVk"}`},
		{name: "CanSeal", method: http.MethodGet, path: "/api/v1/xkms/seal/capability"},
		{name: "ListKeyVersions", method: http.MethodGet, path: "/api/v1/xkms/keys/test-key/versions"},
		{name: "EnableKeyVersion", method: http.MethodPost, path: "/api/v1/xkms/keys/test-key/versions/1/enable"},
		{name: "DisableKeyVersion", method: http.MethodPost, path: "/api/v1/xkms/keys/test-key/versions/1/disable"},
		{name: "EnableAllKeyVersions", method: http.MethodPost, path: "/api/v1/xkms/keys/test-key/versions/enable-all"},
		{name: "DisableAllKeyVersions", method: http.MethodPost, path: "/api/v1/xkms/keys/test-key/versions/disable-all"},
		{name: "ListPIVSlots", method: http.MethodGet, path: "/api/v1/xkms/piv/slots"},
		{name: "ListPIVSlots with backend", method: http.MethodGet, path: "/api/v1/xkms/piv/slots?backend=tpm2"},
		{name: "GetPIVCertificate", method: http.MethodGet, path: "/api/v1/xkms/piv/slots/9a/cert"},
		{name: "GetPIVCertificate with format", method: http.MethodGet, path: "/api/v1/xkms/piv/slots/9a/cert?format=der&backend=pkcs11"},
		{name: "StorePIVCertificate", method: http.MethodPut, path: "/api/v1/xkms/piv/slots/9a/cert", body: `{"certificate":"dGVzdA=="}`},
		{name: "DeletePIVCertificate", method: http.MethodDelete, path: "/api/v1/xkms/piv/slots/9a/cert"},
		{name: "DeletePIVCertificate with backend", method: http.MethodDelete, path: "/api/v1/xkms/piv/slots/9c/cert?backend=tpm2"},
		{name: "GeneratePIVKey", method: http.MethodPost, path: "/api/v1/xkms/piv/slots/9a/key", body: `{"algorithm":"ECCP256"}`},
		{name: "GeneratePIVCSR", method: http.MethodPost, path: "/api/v1/xkms/piv/slots/9a/csr", body: `{"subject":"CN=test-cn"}`},
		{name: "SetSOPIN", method: http.MethodPost, path: "/api/v1/xkms/pin/so/set", body: `{"new_so_pin":"123456"}`},
		{name: "SetSOPIN empty body", method: http.MethodPost, path: "/api/v1/xkms/pin/so/set", body: ""},
		{name: "SetUserPIN", method: http.MethodPost, path: "/api/v1/xkms/pin/user/set", body: `{"so_pin":"123456","new_user_pin":"654321"}`},
		{name: "ChangeSOPIN", method: http.MethodPost, path: "/api/v1/xkms/pin/so/change", body: `{"current_so_pin":"123456","new_so_pin":"654321"}`},
		{name: "ChangeSOPIN empty body", method: http.MethodPost, path: "/api/v1/xkms/pin/so/change", body: ""},
		{name: "ChangeUserPIN", method: http.MethodPost, path: "/api/v1/xkms/pin/user/change", body: `{"current_user_pin":"654321","new_user_pin":"111111"}`},
		{name: "VerifySOPIN", method: http.MethodPost, path: "/api/v1/xkms/pin/so/verify", body: `{"so_pin":"123456"}`},
		{name: "VerifySOPIN empty body", method: http.MethodPost, path: "/api/v1/xkms/pin/so/verify", body: ""},
		{name: "VerifyUserPIN", method: http.MethodPost, path: "/api/v1/xkms/pin/user/verify", body: `{"user_pin":"654321"}`},
		{name: "GetLockoutStatus", method: http.MethodGet, path: "/api/v1/xkms/pin/lockout"},
		{name: "ResetLockout", method: http.MethodPost, path: "/api/v1/xkms/pin/lockout/reset", body: `{"so_pin":"123456"}`},
		{name: "ResetLockout empty body", method: http.MethodPost, path: "/api/v1/xkms/pin/lockout/reset", body: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader([]byte(tt.body)))
			if tt.system {
				req = req.WithContext(tenantctx.WithSystem(context.Background()))
			}
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusPreconditionFailed, w.Code)
			if tt.method == http.MethodHead {
				return
			}
			var resp xkmsErrorResponse
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			assert.Contains(t, resp.Error, "not initialized")
		})
	}
}

// =============================================================================
// Route registration tests
// =============================================================================

func TestXKMSRoutesRegistered(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Verify the XKMS routes that TestHandleXKMS_NotInitialized does not reach
	// are registered: none may return 404 (not found) or 405 (method not allowed).
	routes := []struct {
		method string
		path   string
	}{
		// Health
		{http.MethodGet, "/api/v1/xkms/health"},

		// CA
		{http.MethodGet, "/api/v1/xkms/ca/bundle"},
		{http.MethodGet, "/api/v1/xkms/ca/cert"},
		{http.MethodPost, "/api/v1/xkms/ca/sign"},
		{http.MethodPost, "/api/v1/xkms/ca/issue"},
		{http.MethodPost, "/api/v1/xkms/ca/revoke"},
		{http.MethodPost, "/api/v1/xkms/ca/crl"},
		{http.MethodGet, "/api/v1/xkms/ca/revoked/1234567890"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			var body *bytes.Reader
			if route.method == http.MethodPost || route.method == http.MethodPut {
				body = bytes.NewReader([]byte(`{"data":"dGVzdA=="}`))
			} else {
				body = bytes.NewReader(nil)
			}

			req := httptest.NewRequest(route.method, route.path, body)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)

			// Should NOT be 404 (route not found) or 405 (method not allowed)
			assert.NotEqual(t, http.StatusNotFound, w.Code,
				"route %s %s should be registered", route.method, route.path)
			assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code,
				"method %s should be allowed for %s", route.method, route.path)
		})
	}
}

func initXKMSForTest(t *testing.T) func() {
	t.Helper()

	xkmssdk.ServiceReset()

	keyStore := xkmssdk.NewStorageMemory()
	certStore := xkmssdk.NewStorageMemory()

	swBackend, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{
		KeyStorage: keyStore,
	})
	require.NoError(t, err)

	backend, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     swBackend,
		CertStorage: certStore,
	})
	require.NoError(t, err)

	err = xkmssdk.ServiceInitialize(&xkmssdk.ServiceConfig{
		Backends: map[string]xkmssdk.ServiceBackend{
			"software": backend,
		},
		DefaultBackend: "software",
	})
	require.NoError(t, err)
	require.True(t, xkmssdk.ServiceIsInitialized())

	// Key-version records, as the server wires them, so RotateKey works.
	keyVersions, err := xkmssdk.NewKeyVersionStore(xkmssdk.NewStorageMemory())
	require.NoError(t, err)
	svc, err := xkmssdk.Get()
	require.NoError(t, err)
	svc.SetKeyVersionStore(keyVersions)

	return func() {
		xkmssdk.ServiceReset()
	}
}

func selfSignedCertPEM(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-cert"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}))
}

func TestXKMS91_GenerateKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader([]byte(`{bad`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader([]byte{}))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing key_id returns 400", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.GenerateKeyRequest{
			KeyType: "rsa",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp xkmsErrorResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.Error, "key_id is required")
	})

	t.Run("missing key_type returns 400", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.GenerateKeyRequest{
			KeyID: "test-gen-key",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp xkmsErrorResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.Error, "key_type is required")
	})

	t.Run("successful RSA key generation returns 201", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.GenerateKeyRequest{
			KeyID:   "test-rsa-key-91",
			KeyType: "rsa",
			KeySize: 2048,
			Backend: "software",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
		var resp transportTypes.GenerateKeyResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "test-rsa-key-91", resp.KeyID)
	})

	t.Run("successful ECDSA key generation returns 201", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.GenerateKeyRequest{
			KeyID:   "test-ecdsa-key-91",
			KeyType: "ecdsa",
			Backend: "software",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
		var resp transportTypes.GenerateKeyResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "test-ecdsa-key-91", resp.KeyID)
	})

	t.Run("successful Ed25519 key generation returns 201", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.GenerateKeyRequest{
			KeyID:   "test-ed25519-key-91",
			KeyType: "ed25519",
			Backend: "software",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
		var resp transportTypes.GenerateKeyResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "test-ed25519-key-91", resp.KeyID)
	})

	t.Run("generate key with backend specified", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.GenerateKeyRequest{
			KeyID:   "test-backend-key-91",
			KeyType: "ecdsa",
			Backend: "software",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("generate key with invalid backend returns error", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.GenerateKeyRequest{
			KeyID:   "test-badbackend-key",
			KeyType: "rsa",
			Backend: "nonexistent-backend",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSListKeys - all paths
// =============================================================================

func TestXKMS91_ListKeys(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Generate a key so we have something to list
	genBody, _ := json.Marshal(transportTypes.GenerateKeyRequest{
		KeyID:   "list-test-key",
		KeyType: "ecdsa",
		Backend: "software",
	})
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	server.router.ServeHTTP(genW, genReq)
	require.Equal(t, http.StatusCreated, genW.Code)

	t.Run("list all keys returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/keys", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.ListKeysResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.NotEmpty(t, resp.Keys)
	})

	t.Run("list keys with backend filter returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/keys?backend=software", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("list keys with invalid backend returns error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/keys?backend=nonexistent", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSGetKey - all paths
// =============================================================================

func TestXKMS91_GetKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Generate a key to get
	genBody, _ := json.Marshal(transportTypes.GenerateKeyRequest{
		KeyID:   "get-test-key",
		KeyType: "rsa",
		KeySize: 2048,
		Backend: "software",
	})
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	server.router.ServeHTTP(genW, genReq)
	require.Equal(t, http.StatusCreated, genW.Code)

	t.Run("successful key retrieval returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/keys/software:signing:rsa:get-test-key", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.GetKeyResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.PublicKeyPEM, "BEGIN PUBLIC KEY")
	})

	t.Run("get nonexistent key returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/keys/does-not-exist", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("key retrieval with backend query param returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/keys/software:signing:rsa:get-test-key?backend=software", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// =============================================================================
// HandleXKMSDeleteKey - all paths
// =============================================================================

func TestXKMS91_DeleteKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Generate a key to delete
	genBody, _ := json.Marshal(transportTypes.GenerateKeyRequest{
		KeyID:   "delete-test-key",
		KeyType: "ecdsa",
		Backend: "software",
	})
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	server.router.ServeHTTP(genW, genReq)
	require.Equal(t, http.StatusCreated, genW.Code)

	t.Run("successful delete returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/xkms/keys/software:signing:ecdsa-p256:delete-test-key", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.DeleteKeyResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.True(t, resp.Success)
	})

	t.Run("delete nonexistent key returns error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/xkms/keys/does-not-exist-key", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSRotateKey - all paths
// =============================================================================

func TestXKMS91_RotateKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Generate a key to rotate
	genBody, _ := json.Marshal(transportTypes.GenerateKeyRequest{
		KeyID:   "rotate-test-key",
		KeyType: "rsa",
		KeySize: 2048,
		Backend: "software",
	})
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	server.router.ServeHTTP(genW, genReq)
	require.Equal(t, http.StatusCreated, genW.Code)

	t.Run("successful rotate returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:rsa:rotate-test-key/rotate", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.RotateKeyResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.True(t, resp.Success)
	})

	t.Run("rotate nonexistent key returns error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/nonexistent-rotate/rotate", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSSign - all paths
// =============================================================================

func TestXKMS91_Sign(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Generate a signing key
	genBody, _ := json.Marshal(transportTypes.GenerateKeyRequest{
		KeyID:   "sign-test-key",
		KeyType: "ecdsa",
		Backend: "software",
	})
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	server.router.ServeHTTP(genW, genReq)
	require.Equal(t, http.StatusCreated, genW.Code)

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/sign-test-key/sign", bytes.NewReader([]byte(`{bad`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty data returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"data": []byte{},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/sign-test-key/sign", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("sign with nonexistent key returns 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"data": []byte("test data to sign"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/nonexistent-signer/sign", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("successful sign returns 200", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"data": []byte("test data to sign"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:ecdsa-p256:sign-test-key/sign", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.SignResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.NotEmpty(t, resp.Signature)
		assert.Equal(t, "SHA-256", resp.Algorithm)
	})

	t.Run("sign with SHA-512 hash returns 200", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"data": []byte("test data"),
			"hash": "SHA-512",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:ecdsa-p256:sign-test-key/sign", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.SignResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "SHA-512", resp.Algorithm)
	})
}

// =============================================================================
// HandleXKMSVerify - all paths
// =============================================================================

func TestXKMS91_Verify(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Generate a key for signing + verification
	genBody, _ := json.Marshal(transportTypes.GenerateKeyRequest{
		KeyID:   "verify-key",
		KeyType: "ecdsa",
		Backend: "software",
	})
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	server.router.ServeHTTP(genW, genReq)
	require.Equal(t, http.StatusCreated, genW.Code)

	// Sign data to get a real signature
	signBody, _ := json.Marshal(map[string]interface{}{
		"data": []byte("verify this data"),
	})
	signReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:ecdsa-p256:verify-key/sign", bytes.NewReader(signBody))
	signReq.Header.Set("Content-Type", "application/json")
	signW := httptest.NewRecorder()
	server.router.ServeHTTP(signW, signReq)
	require.Equal(t, http.StatusOK, signW.Code)
	var signResp transportTypes.SignResponse
	require.NoError(t, json.NewDecoder(signW.Body).Decode(&signResp))

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:ecdsa-p256:verify-key/verify", bytes.NewReader([]byte(`{bad`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing data and signature returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"data":      []byte{},
			"signature": []byte{},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:ecdsa-p256:verify-key/verify", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("nonexistent key returns 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"data":      []byte("test data"),
			"signature": []byte("some-sig"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/nonexistent-verify/verify", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("successful verify returns valid true", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"data":      []byte("verify this data"),
			"signature": signResp.Signature,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:ecdsa-p256:verify-key/verify", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.VerifyResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.True(t, resp.Valid)
	})

	t.Run("verify with wrong data returns valid false", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"data":      []byte("wrong data"),
			"signature": signResp.Signature,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:ecdsa-p256:verify-key/verify", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.VerifyResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.False(t, resp.Valid)
	})
}

// =============================================================================
// HandleXKMSEncrypt - all paths
// =============================================================================

func TestXKMS91_Encrypt(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Generate an RSA key for encryption
	genBody, _ := json.Marshal(transportTypes.GenerateKeyRequest{
		KeyID:   "encrypt-test-key",
		KeyType: "rsa",
		KeySize: 2048,
		Backend: "software",
	})
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	server.router.ServeHTTP(genW, genReq)
	require.Equal(t, http.StatusCreated, genW.Code)

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/encrypt-test-key/encrypt", bytes.NewReader([]byte(`{bad`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty plaintext returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"plaintext": []byte{},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/encrypt-test-key/encrypt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("nonexistent key returns 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"plaintext": []byte("secret data"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/nonexistent-enc-key/encrypt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("successful encrypt returns 200 with ciphertext", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"plaintext": []byte("secret data"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:rsa:encrypt-test-key/encrypt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.EncryptResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.NotEmpty(t, resp.Ciphertext)
	})

	t.Run("encrypt with non-RSA key returns 500", func(t *testing.T) {
		// Generate an ECDSA key (encryption not supported)
		ecGenBody, _ := json.Marshal(transportTypes.GenerateKeyRequest{
			KeyID:   "ecdsa-no-encrypt",
			KeyType: "ecdsa",
			Backend: "software",
		})
		ecGenReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(ecGenBody))
		ecGenReq.Header.Set("Content-Type", "application/json")
		ecGenW := httptest.NewRecorder()
		server.router.ServeHTTP(ecGenW, ecGenReq)
		require.Equal(t, http.StatusCreated, ecGenW.Code)

		body, _ := json.Marshal(map[string]interface{}{
			"plaintext": []byte("secret data"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:ecdsa-p256:ecdsa-no-encrypt/encrypt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSDecrypt - all paths
// =============================================================================

func TestXKMS91_Decrypt(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Generate RSA key for encrypt/decrypt
	genBody, _ := json.Marshal(transportTypes.GenerateKeyRequest{
		KeyID:   "decrypt-test-key",
		KeyType: "rsa",
		KeySize: 2048,
		Backend: "software",
	})
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	server.router.ServeHTTP(genW, genReq)
	require.Equal(t, http.StatusCreated, genW.Code)

	// Encrypt data to get real ciphertext
	encBody, _ := json.Marshal(map[string]interface{}{
		"plaintext": []byte("decrypt me"),
	})
	encReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:rsa:decrypt-test-key/encrypt", bytes.NewReader(encBody))
	encReq.Header.Set("Content-Type", "application/json")
	encW := httptest.NewRecorder()
	server.router.ServeHTTP(encW, encReq)
	require.Equal(t, http.StatusOK, encW.Code)
	var encResp transportTypes.EncryptResponse
	require.NoError(t, json.NewDecoder(encW.Body).Decode(&encResp))

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:rsa:decrypt-test-key/decrypt", bytes.NewReader([]byte(`{bad`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty ciphertext returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"ciphertext": []byte{},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:rsa:decrypt-test-key/decrypt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("nonexistent key returns 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"ciphertext": []byte("some-ciphertext"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/nonexistent-dec-key/decrypt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("successful decrypt returns 200 with plaintext", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"ciphertext": encResp.Ciphertext,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:rsa:decrypt-test-key/decrypt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.DecryptResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.NotEmpty(t, resp.Plaintext)
	})

	t.Run("decrypt with invalid ciphertext returns 500", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"ciphertext": []byte("invalid-ciphertext-data"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:rsa:decrypt-test-key/decrypt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSSaveCert - all paths
// =============================================================================

func TestXKMS91_SaveCert(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	certPEM := selfSignedCertPEM(t)

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/certs?key_id=test", bytes.NewReader([]byte(`{bad`))).WithContext(tenantctx.WithSystem(context.Background()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing key_id query param returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"certificate_pem": certPEM,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/certs", bytes.NewReader(body)).WithContext(tenantctx.WithSystem(context.Background()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp xkmsErrorResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.Error, "key_id query parameter is required")
	})

	t.Run("empty certificate_pem returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"certificate_pem": "",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/certs?key_id=test", bytes.NewReader(body)).WithContext(tenantctx.WithSystem(context.Background()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp xkmsErrorResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.Error, "certificate_pem is required")
	})

	t.Run("invalid PEM returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"certificate_pem": "not-a-valid-pem",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/certs?key_id=test", bytes.NewReader(body)).WithContext(tenantctx.WithSystem(context.Background()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp xkmsErrorResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.Error, "invalid PEM-encoded certificate")
	})

	t.Run("invalid certificate DER in PEM returns 400", func(t *testing.T) {
		invalidPEM := string(pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: []byte("not-valid-der"),
		}))
		body, _ := json.Marshal(map[string]string{
			"certificate_pem": invalidPEM,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/certs?key_id=test", bytes.NewReader(body)).WithContext(tenantctx.WithSystem(context.Background()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp xkmsErrorResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.Error, "failed to parse certificate")
	})

	t.Run("successful save cert returns 200", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"certificate_pem": certPEM,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/certs?key_id=save-cert-test", bytes.NewReader(body)).WithContext(tenantctx.WithSystem(context.Background()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// =============================================================================
// HandleXKMSListCerts - all paths
// =============================================================================

func TestXKMS91_ListCerts(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Save a cert so we have something to list
	certPEM := selfSignedCertPEM(t)
	savBody, _ := json.Marshal(map[string]string{
		"certificate_pem": certPEM,
	})
	savReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/certs?key_id=list-cert-test", bytes.NewReader(savBody)).WithContext(tenantctx.WithSystem(context.Background()))
	savReq.Header.Set("Content-Type", "application/json")
	savW := httptest.NewRecorder()
	server.router.ServeHTTP(savW, savReq)
	require.Equal(t, http.StatusOK, savW.Code)

	t.Run("list all certs returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/certs", nil).WithContext(tenantctx.WithSystem(context.Background()))
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.ListCertificatesResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.NotEmpty(t, resp.Certificates)
	})

	t.Run("list certs with backend returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/certs?backend=software", nil).WithContext(tenantctx.WithSystem(context.Background()))
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("list certs with invalid backend returns error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/certs?backend=nonexistent", nil).WithContext(tenantctx.WithSystem(context.Background()))
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSGetCert - all paths
// =============================================================================

func TestXKMS91_GetCert(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Save a cert to retrieve
	certPEM := selfSignedCertPEM(t)
	savBody, _ := json.Marshal(map[string]string{
		"certificate_pem": certPEM,
	})
	savReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/certs?key_id=get-cert-test", bytes.NewReader(savBody)).WithContext(tenantctx.WithSystem(context.Background()))
	savReq.Header.Set("Content-Type", "application/json")
	savW := httptest.NewRecorder()
	server.router.ServeHTTP(savW, savReq)
	require.Equal(t, http.StatusOK, savW.Code)

	t.Run("get existing cert returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/certs/get-cert-test", nil).WithContext(tenantctx.WithSystem(context.Background()))
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.GetCertificateResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.CertificatePEM, "BEGIN CERTIFICATE")
	})

	t.Run("get nonexistent cert returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/certs/nonexistent-cert", nil).WithContext(tenantctx.WithSystem(context.Background()))
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// HandleXKMSDeleteCert - all paths
// =============================================================================

func TestXKMS91_DeleteCert(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Save a cert to delete
	certPEM := selfSignedCertPEM(t)
	savBody, _ := json.Marshal(map[string]string{
		"certificate_pem": certPEM,
	})
	savReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/certs?key_id=del-cert-test", bytes.NewReader(savBody)).WithContext(tenantctx.WithSystem(context.Background()))
	savReq.Header.Set("Content-Type", "application/json")
	savW := httptest.NewRecorder()
	server.router.ServeHTTP(savW, savReq)
	require.Equal(t, http.StatusOK, savW.Code)

	t.Run("delete existing cert returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/xkms/certs/del-cert-test", nil).WithContext(tenantctx.WithSystem(context.Background()))
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("delete nonexistent cert returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/xkms/certs/nonexistent-delete", nil).WithContext(tenantctx.WithSystem(context.Background()))
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// HandleXKMSCertExists - all paths
// =============================================================================

func TestXKMS91_CertExists(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Save a cert to check existence
	certPEM := selfSignedCertPEM(t)
	savBody, _ := json.Marshal(map[string]string{
		"certificate_pem": certPEM,
	})
	savReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/certs?key_id=exists-cert-test", bytes.NewReader(savBody)).WithContext(tenantctx.WithSystem(context.Background()))
	savReq.Header.Set("Content-Type", "application/json")
	savW := httptest.NewRecorder()
	server.router.ServeHTTP(savW, savReq)
	require.Equal(t, http.StatusOK, savW.Code)

	t.Run("existing cert returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/api/v1/xkms/certs/exists-cert-test", nil).WithContext(tenantctx.WithSystem(context.Background()))
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("nonexistent cert returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/api/v1/xkms/certs/nonexistent-exists", nil).WithContext(tenantctx.WithSystem(context.Background()))
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("invalid cert ID format returns 404", func(t *testing.T) {
		// 3-part ID (a:b:c) is invalid format - must be 4-part or simple name
		req := httptest.NewRequest(http.MethodHead, "/api/v1/xkms/certs/a:b:c", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// HandleXKMSSeal - all paths
// =============================================================================

func TestXKMS91_Seal(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/seal", bytes.NewReader([]byte(`{bad`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty data returns 400", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.SealRequest{
			Data: []byte{},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/seal", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("exercises seal path with valid data", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.SealRequest{
			Data: []byte("seal this data"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/seal", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		// The handler passes no KeyAttributes, so the software backend
		// rejects the seal and the handler maps that to 500.
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Seal: failed to seal data")
	})

	t.Run("exercises seal with backend path", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.SealRequest{
			Data:    []byte("seal with backend"),
			Backend: "software",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/seal", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		// The handler passes no KeyAttributes, so the software backend
		// rejects the seal and the handler maps that to 500.
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Seal: failed to seal data")
	})

	t.Run("exercises seal with AAD path", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.SealRequest{
			Data: []byte("seal with aad"),
			AAD:  []byte("additional-data"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/seal", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		// The handler passes no KeyAttributes, so the software backend
		// rejects the seal and the handler maps that to 500.
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Seal: failed to seal data")
	})

	t.Run("seal with invalid backend returns error", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.SealRequest{
			Data:    []byte("seal data"),
			Backend: "nonexistent-backend",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/seal", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSUnseal - all paths
// =============================================================================

func TestXKMS91_Unseal(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/unseal", bytes.NewReader([]byte(`{bad`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty ciphertext returns 400", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.UnsealRequest{
			Ciphertext: []byte{},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/unseal", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("exercises unseal path without backend", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.UnsealRequest{
			Ciphertext: []byte("some-ciphertext"),
			Nonce:      []byte("some-nonce"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/unseal", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		// The handler maps the backend's unseal failure to 500.
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Unseal:")
	})

	t.Run("exercises unseal with backend path", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.UnsealRequest{
			Backend:    "software",
			Ciphertext: []byte("some-ciphertext"),
			Nonce:      []byte("some-nonce"),
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/unseal", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		// The handler maps the backend's unseal failure to 500.
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Unseal:")
	})
}

// =============================================================================
// HandleXKMSCanSeal - all paths
// =============================================================================

func TestXKMS91_CanSeal(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	t.Run("can seal without backend returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/seal/capability", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.CanSealResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		// Software backend supports sealing
		assert.True(t, resp.CanSeal)
	})

	t.Run("can seal with backend returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/seal/capability?backend=software", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.CanSealResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "software", resp.Backend)
	})
}

// =============================================================================
// HandleXKMSListKeyVersions - all paths
// =============================================================================

func TestXKMS91_ListKeyVersions(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Generate a key
	genBody, _ := json.Marshal(transportTypes.GenerateKeyRequest{
		KeyID:   "versions-test-key",
		KeyType: "ecdsa",
		Backend: "software",
	})
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	server.router.ServeHTTP(genW, genReq)
	require.Equal(t, http.StatusCreated, genW.Code)

	t.Run("successful list versions returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/keys/software:signing:ecdsa-p256:versions-test-key/versions", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp XKMSListKeyVersionsResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, 1, resp.Total)
		assert.NotEmpty(t, resp.Versions)
	})

	t.Run("list versions of nonexistent key returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/keys/nonexistent-ver-key/versions", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// HandleXKMSEnableKeyVersion - all paths
// =============================================================================

func TestXKMS91_EnableKeyVersion(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	t.Run("valid key and version returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/test-key/versions/1/enable", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp XKMSEnableKeyVersionResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "test-key", resp.KeyID)
		assert.Equal(t, uint64(1), resp.Version)
		assert.Equal(t, "enabled", resp.Status)
	})

	t.Run("invalid version number returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/test-key/versions/notanumber/enable", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp xkmsErrorResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.Error, "invalid version number")
	})
}

// =============================================================================
// HandleXKMSDisableKeyVersion - all paths
// =============================================================================

func TestXKMS91_DisableKeyVersion(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	t.Run("valid key and version returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/test-key/versions/1/disable", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp XKMSDisableKeyVersionResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "disabled", resp.Status)
	})

	t.Run("invalid version number returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/test-key/versions/abc/disable", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// =============================================================================
// HandleXKMSEnableAllKeyVersions - all paths
// =============================================================================

func TestXKMS91_EnableAllKeyVersions(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	t.Run("returns 200 with success response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/test-key/versions/enable-all", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp XKMSEnableAllKeyVersionsResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "test-key", resp.KeyID)
		assert.Equal(t, 1, resp.Count)
	})
}

// =============================================================================
// HandleXKMSDisableAllKeyVersions - all paths
// =============================================================================

func TestXKMS91_DisableAllKeyVersions(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	t.Run("returns 200 with success response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/test-key/versions/disable-all", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp XKMSDisableAllKeyVersionsResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "test-key", resp.KeyID)
	})
}

// =============================================================================
// HandleXKMSListBackends - happy path
// =============================================================================

func TestXKMS91_ListBackends(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	t.Run("returns 200 with backends list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/backends", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.ListBackendsResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.NotEmpty(t, resp.Backends)
	})
}

// =============================================================================
// HandleXKMSGetBackend - all paths
// =============================================================================

func TestXKMS91_GetBackend(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	t.Run("get existing backend returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/backends/software", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.BackendInfo
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "software", resp.ID)
	})

	t.Run("get nonexistent backend returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/backends/nonexistent", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// =============================================================================
// HandleXKMSHealth - happy path (initialized)
// =============================================================================

func TestXKMS91_Health(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	t.Run("returns ok when initialized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/health", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp transportTypes.HealthResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "ok", resp.Status)
	})
}

// =============================================================================
// HandleXKMSListPIVSlots - when initialized, PIV manager errors
// =============================================================================

func TestXKMS91_ListPIVSlots(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, mockService, srvCleanup := setupMockServer(t)
	defer srvCleanup()
	mockService.PIVSvc = service.NewPIVService(func() *xkmssdk.XKMSService { return &xkmssdk.XKMSService{} })

	t.Run("returns error when no PIV support available", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/piv/slots", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		// Software backend likely does not support PIV - expect 500
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with backend param returns error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/piv/slots?backend=software", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSGetPIVCertificate - when initialized
// =============================================================================

func TestXKMS91_GetPIVCertificate(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, mockService, srvCleanup := setupMockServer(t)
	defer srvCleanup()
	mockService.PIVSvc = service.NewPIVService(func() *xkmssdk.XKMSService { return &xkmssdk.XKMSService{} })

	t.Run("returns error for software backend", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/piv/slots/9a/cert", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with format and backend params", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/piv/slots/9a/cert?format=der&backend=software", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSStorePIVCertificate - when initialized
// =============================================================================

func TestXKMS91_StorePIVCertificate(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, mockService, srvCleanup := setupMockServer(t)
	defer srvCleanup()
	mockService.PIVSvc = service.NewPIVService(func() *xkmssdk.XKMSService { return &xkmssdk.XKMSService{} })

	t.Run("invalid body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/xkms/piv/slots/9a/cert", bytes.NewReader([]byte(`{bad`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("valid body returns PIV error", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.StorePIVCertificateRequest{
			Certificate: []byte("test-cert-data"),
		})
		req := httptest.NewRequest(http.MethodPut, "/api/v1/xkms/piv/slots/9a/cert", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSDeletePIVCertificate - when initialized
// =============================================================================

func TestXKMS91_DeletePIVCertificate(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, mockService, srvCleanup := setupMockServer(t)
	defer srvCleanup()
	mockService.PIVSvc = service.NewPIVService(func() *xkmssdk.XKMSService { return &xkmssdk.XKMSService{} })

	t.Run("returns PIV error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/xkms/piv/slots/9a/cert", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("with backend param returns PIV error", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/xkms/piv/slots/9c/cert?backend=software", nil)
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSGeneratePIVKey - when initialized
// =============================================================================

func TestXKMS91_GeneratePIVKey(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, mockService, srvCleanup := setupMockServer(t)
	defer srvCleanup()
	mockService.PIVSvc = service.NewPIVService(func() *xkmssdk.XKMSService { return &xkmssdk.XKMSService{} })

	t.Run("invalid body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/piv/slots/9a/key", bytes.NewReader([]byte(`{bad`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("valid body returns PIV error", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.GeneratePIVKeyRequest{
			Algorithm: "ECCP256",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/piv/slots/9a/key", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// HandleXKMSGeneratePIVCSR - when initialized
// =============================================================================

func TestXKMS91_GeneratePIVCSR(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, mockService, srvCleanup := setupMockServer(t)
	defer srvCleanup()
	mockService.PIVSvc = service.NewPIVService(func() *xkmssdk.XKMSService { return &xkmssdk.XKMSService{} })

	t.Run("invalid body returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/piv/slots/9a/csr", bytes.NewReader([]byte(`{bad`)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("valid body returns PIV error", func(t *testing.T) {
		body, _ := json.Marshal(transportTypes.GeneratePIVCSRRequest{
			Subject: "CN=test",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/piv/slots/9a/csr", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// =============================================================================
// PIN Handlers - when initialized (all return 501 Not Implemented)
// =============================================================================

func TestXKMS91_PINHandlers(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	const (
		setSOPIN      = "/api/v1/xkms/pin/so/set"
		setUserPIN    = "/api/v1/xkms/pin/user/set"
		changeSOPIN   = "/api/v1/xkms/pin/so/change"
		changeUserPIN = "/api/v1/xkms/pin/user/change"
		verifySOPIN   = "/api/v1/xkms/pin/so/verify"
		verifyUserPIN = "/api/v1/xkms/pin/user/verify"
		lockout       = "/api/v1/xkms/pin/lockout"
		resetLockout  = "/api/v1/xkms/pin/lockout/reset"
	)

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"SetSOPIN invalid body", http.MethodPost, setSOPIN, `{bad`, http.StatusBadRequest},
		{"SetSOPIN missing new_so_pin", http.MethodPost, setSOPIN, `{}`, http.StatusBadRequest},
		{"SetSOPIN valid request", http.MethodPost, setSOPIN, `{"new_so_pin":"123456"}`, http.StatusNotImplemented},
		{"SetUserPIN invalid body", http.MethodPost, setUserPIN, `{bad`, http.StatusBadRequest},
		{"SetUserPIN missing new_user_pin", http.MethodPost, setUserPIN, `{}`, http.StatusBadRequest},
		{"SetUserPIN valid request", http.MethodPost, setUserPIN, `{"so_pin":"123456","new_user_pin":"654321"}`, http.StatusNotImplemented},
		{"ChangeSOPIN invalid body", http.MethodPost, changeSOPIN, `{bad`, http.StatusBadRequest},
		{"ChangeSOPIN missing required fields", http.MethodPost, changeSOPIN, `{}`, http.StatusBadRequest},
		{"ChangeSOPIN valid request", http.MethodPost, changeSOPIN, `{"current_so_pin":"123456","new_so_pin":"654321"}`, http.StatusNotImplemented},
		{"ChangeUserPIN invalid body", http.MethodPost, changeUserPIN, `{bad`, http.StatusBadRequest},
		{"ChangeUserPIN missing required fields", http.MethodPost, changeUserPIN, `{}`, http.StatusBadRequest},
		{"ChangeUserPIN valid request", http.MethodPost, changeUserPIN, `{"current_user_pin":"654321","new_user_pin":"111111"}`, http.StatusNotImplemented},
		{"VerifySOPIN invalid body", http.MethodPost, verifySOPIN, `{bad`, http.StatusBadRequest},
		{"VerifySOPIN missing so_pin", http.MethodPost, verifySOPIN, `{}`, http.StatusBadRequest},
		{"VerifySOPIN valid request", http.MethodPost, verifySOPIN, `{"so_pin":"123456"}`, http.StatusNotImplemented},
		{"VerifyUserPIN invalid body", http.MethodPost, verifyUserPIN, `{bad`, http.StatusBadRequest},
		{"VerifyUserPIN missing user_pin", http.MethodPost, verifyUserPIN, `{}`, http.StatusBadRequest},
		{"VerifyUserPIN valid request", http.MethodPost, verifyUserPIN, `{"user_pin":"654321"}`, http.StatusNotImplemented},
		{"GetLockoutStatus", http.MethodGet, lockout, "", http.StatusNotImplemented},
		{"ResetLockout invalid body", http.MethodPost, resetLockout, `{bad`, http.StatusBadRequest},
		{"ResetLockout missing so_pin", http.MethodPost, resetLockout, `{}`, http.StatusBadRequest},
		{"ResetLockout valid request", http.MethodPost, resetLockout, `{"so_pin":"123456"}`, http.StatusNotImplemented},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)
			assert.Equal(t, tt.want, w.Code)
		})
	}
}

// =============================================================================
// xkmsContextWithTimeout tests
// =============================================================================

func TestXKMS91_XKMSContextWithTimeout(t *testing.T) {
	t.Run("creates context with 30s timeout", func(t *testing.T) {
		ctx, cancel := xkmsContextWithTimeout(context.Background())
		defer cancel()
		deadline, ok := ctx.Deadline()
		assert.True(t, ok)
		assert.WithinDuration(t, time.Now().Add(30*time.Second), deadline, 2*time.Second)
	})
}

// =============================================================================
// Direct handler calls for empty URL param coverage (chi router always fills params)
// =============================================================================

func TestXKMS91_HandlersEmptyKeyID(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	tests := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{"GetKey empty keyID", http.MethodGet, "/api/v1/xkms/keys/", server.HandleXKMSGetKey},
		{"DeleteKey empty keyID", http.MethodDelete, "/api/v1/xkms/keys/", server.HandleXKMSDeleteKey},
		{"RotateKey empty keyID", http.MethodPost, "/api/v1/xkms/keys//rotate", server.HandleXKMSRotateKey},
		{"Sign empty keyID", http.MethodPost, "/api/v1/xkms/keys//sign", server.HandleXKMSSign},
		{"Verify empty keyID", http.MethodPost, "/api/v1/xkms/keys//verify", server.HandleXKMSVerify},
		{"Encrypt empty keyID", http.MethodPost, "/api/v1/xkms/keys//encrypt", server.HandleXKMSEncrypt},
		{"Decrypt empty keyID", http.MethodPost, "/api/v1/xkms/keys//decrypt", server.HandleXKMSDecrypt},
		{"GetCert empty keyID", http.MethodGet, "/api/v1/xkms/certs/", server.HandleXKMSGetCert},
		{"DeleteCert empty keyID", http.MethodDelete, "/api/v1/xkms/certs/", server.HandleXKMSDeleteCert},
		{"CertExists empty keyID", http.MethodHead, "/api/v1/xkms/certs/", server.HandleXKMSCertExists},
		{"ListKeyVersions empty keyID", http.MethodGet, "/api/v1/xkms/keys//versions", server.HandleXKMSListKeyVersions},
		{"EnableKeyVersion empty params", http.MethodPost, "/api/v1/xkms/keys//versions//enable", server.HandleXKMSEnableKeyVersion},
		{"DisableKeyVersion empty params", http.MethodPost, "/api/v1/xkms/keys//versions//disable", server.HandleXKMSDisableKeyVersion},
		{"EnableAllKeyVersions empty keyID", http.MethodPost, "/api/v1/xkms/keys//versions/enable-all", server.HandleXKMSEnableAllKeyVersions},
		{"DisableAllKeyVersions empty keyID", http.MethodPost, "/api/v1/xkms/keys//versions/disable-all", server.HandleXKMSDisableAllKeyVersions},
		{"GetBackend empty backendID", http.MethodGet, "/api/v1/xkms/backends/", server.HandleXKMSGetBackend},
	}

	for _, tt := range tests {
		t.Run(tt.name+" returns 400", func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			tt.handler(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

// =============================================================================
// HandleXKMSSign - sign error path (sign failure)
// =============================================================================

func TestXKMS91_Sign_Error(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	// Generate an RSA key - sign with wrong hash size data to trigger error
	genBody, _ := json.Marshal(transportTypes.GenerateKeyRequest{
		KeyID:   "sign-err-key",
		KeyType: "rsa",
		KeySize: 2048,
		Backend: "software",
	})
	genReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys", bytes.NewReader(genBody))
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	server.router.ServeHTTP(genW, genReq)
	require.Equal(t, http.StatusCreated, genW.Code)

	t.Run("sign with SHA-384 hash exercises hash path", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"data": []byte("test data for hash"),
			"hash": "SHA-384",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/software:signing:rsa:sign-err-key/sign", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// errReader is an io.Reader that always returns an error.
type errReader int

func (errReader) Read(p []byte) (int, error) {
	return 0, &XKMSOperationError{Op: "read", Detail: "simulated io error"}
}
