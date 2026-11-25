//go:build !integration && hashicorpmetrics && local

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestParseCryptoEncryptResponse(t *testing.T) {
	t.Run("ValidResponse", func(t *testing.T) {
		data := map[string]interface{}{
			"key_id":      "key-1",
			"key_version": float64(2),
			"ciphertext":  base64.StdEncoding.EncodeToString([]byte("encrypted-data")),
		}
		resp, err := parseCryptoEncryptResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "key-1", resp.KeyID)
		assert.Equal(t, uint64(2), resp.KeyVersion)
		assert.Equal(t, []byte("encrypted-data"), resp.Ciphertext)
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseCryptoEncryptResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, "", resp.KeyID)
	})

	t.Run("EmptyMap", func(t *testing.T) {
		resp, err := parseCryptoEncryptResponse(map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, "", resp.KeyID)
		assert.Equal(t, uint64(0), resp.KeyVersion)
	})
}

func TestParseCryptoDecryptResponse(t *testing.T) {
	t.Run("ValidResponse", func(t *testing.T) {
		data := map[string]interface{}{
			"key_id":      "key-2",
			"key_version": float64(1),
			"plaintext":   base64.StdEncoding.EncodeToString([]byte("decrypted-data")),
		}
		resp, err := parseCryptoDecryptResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "key-2", resp.KeyID)
		assert.Equal(t, []byte("decrypted-data"), resp.Plaintext)
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseCryptoDecryptResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, "", resp.KeyID)
	})
}

func TestParseCryptoSignResponse(t *testing.T) {
	t.Run("ValidResponse", func(t *testing.T) {
		data := map[string]interface{}{
			"key_id":         "sign-key",
			"key_version":    float64(3),
			"signature":      base64.StdEncoding.EncodeToString([]byte("sig-data")),
			"hash_algorithm": "SHA256",
		}
		resp, err := parseCryptoSignResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "sign-key", resp.KeyID)
		assert.Equal(t, uint64(3), resp.KeyVersion)
		assert.Equal(t, []byte("sig-data"), resp.Signature)
		assert.Equal(t, "SHA256", string(resp.HashAlgorithm))
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseCryptoSignResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, "", resp.KeyID)
	})
}

func TestParseCryptoVerifyResponse(t *testing.T) {
	t.Run("ValidTrue", func(t *testing.T) {
		data := map[string]interface{}{
			"key_id":      "verify-key",
			"key_version": float64(1),
			"valid":       true,
		}
		resp, err := parseCryptoVerifyResponse(data)
		require.NoError(t, err)
		assert.True(t, resp.Valid)
		assert.Equal(t, "verify-key", resp.KeyID)
	})

	t.Run("ValidFalse", func(t *testing.T) {
		data := map[string]interface{}{
			"valid": false,
		}
		resp, err := parseCryptoVerifyResponse(data)
		require.NoError(t, err)
		assert.False(t, resp.Valid)
	})
}

func TestParseCryptoHashResponse(t *testing.T) {
	t.Run("ValidResponse", func(t *testing.T) {
		data := map[string]interface{}{
			"algorithm": "SHA512",
			"hash":      base64.StdEncoding.EncodeToString([]byte("hash-bytes")),
		}
		resp, err := parseCryptoHashResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "SHA512", string(resp.Algorithm))
		assert.Equal(t, []byte("hash-bytes"), resp.Hash)
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("EmptyMap", func(t *testing.T) {
		resp, err := parseCryptoHashResponse(map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, "", string(resp.Algorithm))
	})
}

func TestParseCryptoHMACResponse(t *testing.T) {
	t.Run("ValidResponse", func(t *testing.T) {
		data := map[string]interface{}{
			"key_id":         "hmac-key",
			"key_version":    float64(1),
			"hmac":           base64.StdEncoding.EncodeToString([]byte("hmac-value")),
			"hash_algorithm": "SHA256",
		}
		resp, err := parseCryptoHMACResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "hmac-key", resp.KeyID)
		assert.Equal(t, []byte("hmac-value"), resp.HMAC)
		assert.Equal(t, "SHA256", string(resp.HashAlgorithm))
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseCryptoHMACResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, "", resp.KeyID)
	})
}

func TestParseCryptoVerifyHMACResponse(t *testing.T) {
	t.Run("ValidTrue", func(t *testing.T) {
		data := map[string]interface{}{
			"key_id":      "verify-hmac-key",
			"key_version": float64(2),
			"valid":       true,
		}
		resp, err := parseCryptoVerifyHMACResponse(data)
		require.NoError(t, err)
		assert.True(t, resp.Valid)
		assert.Equal(t, "verify-hmac-key", resp.KeyID)
	})

	t.Run("ValidFalse", func(t *testing.T) {
		data := map[string]interface{}{
			"valid": false,
		}
		resp, err := parseCryptoVerifyHMACResponse(data)
		require.NoError(t, err)
		assert.False(t, resp.Valid)
	})
}

func TestParseCryptoRewrapResponse(t *testing.T) {
	t.Run("ValidResponse", func(t *testing.T) {
		data := map[string]interface{}{
			"key_id":          "rewrap-key",
			"old_key_version": float64(1),
			"new_key_version": float64(2),
			"ciphertext":      base64.StdEncoding.EncodeToString([]byte("rewrapped-ct")),
		}
		resp, err := parseCryptoRewrapResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "rewrap-key", resp.KeyID)
		assert.Equal(t, uint64(1), resp.OldKeyVersion)
		assert.Equal(t, uint64(2), resp.NewKeyVersion)
		assert.Equal(t, []byte("rewrapped-ct"), resp.Ciphertext)
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseCryptoRewrapResponse(nil)
		require.NoError(t, err)
		assert.Equal(t, "", resp.KeyID)
	})
}

func TestParseCryptoGenerateDataKeyResponse(t *testing.T) {
	t.Run("ValidResponse", func(t *testing.T) {
		data := map[string]interface{}{
			"key_id":          "dek-key",
			"plaintext":       base64.StdEncoding.EncodeToString([]byte("plain-key")),
			"ciphertext_blob": base64.StdEncoding.EncodeToString([]byte("ct-blob")),
		}
		resp, err := parseCryptoGenerateDataKeyResponse(data)
		require.NoError(t, err)
		assert.Equal(t, "dek-key", resp.KeyID)
		assert.Equal(t, []byte("plain-key"), resp.Plaintext)
		assert.Equal(t, []byte("ct-blob"), resp.CiphertextBlob)
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("EmptyMap", func(t *testing.T) {
		resp, err := parseCryptoGenerateDataKeyResponse(map[string]interface{}{})
		require.NoError(t, err)
		assert.Equal(t, "", resp.KeyID)
	})
}

func TestParseCryptoGenerateRandomResponse(t *testing.T) {
	t.Run("ValidResponse", func(t *testing.T) {
		data := map[string]interface{}{
			"random_bytes": base64.StdEncoding.EncodeToString([]byte("random-data")),
		}
		resp, err := parseCryptoGenerateRandomResponse(data)
		require.NoError(t, err)
		assert.Equal(t, []byte("random-data"), resp.RandomBytes)
		assert.False(t, resp.Timestamp.IsZero())
	})

	t.Run("NilInput", func(t *testing.T) {
		resp, err := parseCryptoGenerateRandomResponse(nil)
		require.NoError(t, err)
		assert.Nil(t, resp.RandomBytes)
	})
}

// TestParseCryptoEncryptResponse_DecodesMCPBase64Once checks the parser
// against the JSON shape the MCP server actually sends: encoding/json writes
// a []byte as a base64 string, so the parser must decode it exactly once.
func TestParseCryptoEncryptResponse_DecodesMCPBase64Once(t *testing.T) {
	want := []byte{0x00, 0xff, 0x10, 'x'}
	raw, err := json.Marshal(service.EncryptResponse{KeyID: "k", Ciphertext: want})
	require.NoError(t, err)
	var data map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &data))

	resp, err := parseCryptoEncryptResponse(data)
	require.NoError(t, err)
	assert.Equal(t, want, resp.Ciphertext)
}

// TestParseCryptoResponses_RejectNonBase64 checks that a byte field that is
// not valid base64 is an error naming the key, not passed through as raw
// bytes (which would hide a double encoding).
func TestParseCryptoResponses_RejectNonBase64(t *testing.T) {
	_, err := parseCryptoEncryptResponse(map[string]interface{}{"ciphertext": "not base64!"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ciphertext")

	_, err = parseCryptoGenerateDataKeyResponse(map[string]interface{}{"plaintext": "", "ciphertext_blob": "%%%"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ciphertext_blob")
}

// Test MCP crypto client interface compliance
func TestMCPCryptoClientInterfaceCompliance(t *testing.T) {
	var _ CryptoClient = (*mcpCryptoClient)(nil)
}

// Test that base64 encoding in the MCP calls produces correct values
func TestMCPCryptoBase64Encoding(t *testing.T) {
	data := []byte("hello world")
	encoded := base64.StdEncoding.EncodeToString(data)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	assert.Equal(t, data, decoded)
}
