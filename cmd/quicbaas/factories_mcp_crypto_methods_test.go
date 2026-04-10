//go:build !integration && hashicorpmetrics && local

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// TestMCPCryptoClient_KMSEncrypt tests the MCP crypto encrypt method.
func TestMCPCryptoClient_KMSEncrypt(t *testing.T) {
	ctx := context.Background()
	ct := base64.StdEncoding.EncodeToString([]byte("encrypted-data"))

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":      "key-1",
			"key_version": float64(2),
			"ciphertext":  ct,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSEncrypt(ctx, &service.EncryptRequest{
			KeyID:     "key-1",
			Plaintext: []byte("hello"),
		})
		require.NoError(t, err)
		assert.Equal(t, "key-1", resp.KeyID)
		assert.Equal(t, uint64(2), resp.KeyVersion)
	})

	t.Run("WithContext", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":      "key-1",
			"key_version": float64(1),
			"ciphertext":  ct,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSEncrypt(ctx, &service.EncryptRequest{
			KeyID:     "key-1",
			Plaintext: []byte("hello"),
			Context:   []byte("tenant=t1"),
		})
		require.NoError(t, err)
		assert.Equal(t, "key-1", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("connection refused"))}
		resp, err := c.KMSEncrypt(ctx, &service.EncryptRequest{KeyID: "k", Plaintext: []byte("x")})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPCryptoClient_KMSDecrypt tests the MCP crypto decrypt method.
func TestMCPCryptoClient_KMSDecrypt(t *testing.T) {
	ctx := context.Background()
	pt := base64.StdEncoding.EncodeToString([]byte("decrypted-data"))

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":      "key-2",
			"key_version": float64(3),
			"plaintext":   pt,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSDecrypt(ctx, &service.DecryptRequest{
			KeyID:      "key-2",
			Ciphertext: []byte("cipher"),
		})
		require.NoError(t, err)
		assert.Equal(t, "key-2", resp.KeyID)
		assert.Equal(t, uint64(3), resp.KeyVersion)
	})

	t.Run("WithContext", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":    "key-2",
			"plaintext": pt,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSDecrypt(ctx, &service.DecryptRequest{
			KeyID:      "key-2",
			Ciphertext: []byte("cipher"),
			Context:    []byte("env=prod"),
		})
		require.NoError(t, err)
		assert.Equal(t, "key-2", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSDecrypt(ctx, &service.DecryptRequest{KeyID: "k", Ciphertext: []byte("c")})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPCryptoClient_KMSSign tests the MCP crypto sign method.
func TestMCPCryptoClient_KMSSign(t *testing.T) {
	ctx := context.Background()
	sig := base64.StdEncoding.EncodeToString([]byte("signature-bytes"))

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":         "sign-key",
			"key_version":    float64(1),
			"signature":      sig,
			"hash_algorithm": "SHA256",
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSSign(ctx, &service.SignRequest{
			KeyID:   "sign-key",
			Message: []byte("message"),
		})
		require.NoError(t, err)
		assert.Equal(t, "sign-key", resp.KeyID)
		assert.Equal(t, service.HashAlgorithm("SHA256"), resp.HashAlgorithm)
	})

	t.Run("WithHashAlgorithm", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":    "sign-key",
			"signature": sig,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSSign(ctx, &service.SignRequest{
			KeyID:         "sign-key",
			Message:       []byte("msg"),
			HashAlgorithm: service.HashAlgorithm("SHA512"),
		})
		require.NoError(t, err)
		assert.Equal(t, "sign-key", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSSign(ctx, &service.SignRequest{KeyID: "k", Message: []byte("m")})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPCryptoClient_KMSVerify tests the MCP crypto verify method.
func TestMCPCryptoClient_KMSVerify(t *testing.T) {
	ctx := context.Background()

	t.Run("Valid", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":      "verify-key",
			"key_version": float64(1),
			"valid":       true,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSVerify(ctx, &service.VerifyRequest{
			KeyID:     "verify-key",
			Message:   []byte("msg"),
			Signature: []byte("sig"),
		})
		require.NoError(t, err)
		assert.True(t, resp.Valid)
	})

	t.Run("WithHashAlgorithm", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id": "verify-key",
			"valid":  false,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSVerify(ctx, &service.VerifyRequest{
			KeyID:         "verify-key",
			Message:       []byte("msg"),
			Signature:     []byte("sig"),
			HashAlgorithm: service.HashAlgorithm("SHA384"),
		})
		require.NoError(t, err)
		assert.False(t, resp.Valid)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSVerify(ctx, &service.VerifyRequest{KeyID: "k", Message: []byte("m"), Signature: []byte("s")})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPCryptoClient_KMSHash tests the MCP crypto hash method.
func TestMCPCryptoClient_KMSHash(t *testing.T) {
	ctx := context.Background()
	h := base64.StdEncoding.EncodeToString([]byte("hash-value"))

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"algorithm": "SHA256",
			"hash":      h,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSHash(ctx, &service.HashRequest{
			Data: []byte("data"),
		})
		require.NoError(t, err)
		assert.Equal(t, service.HashAlgorithm("SHA256"), resp.Algorithm)
	})

	t.Run("WithAlgorithm", func(t *testing.T) {
		respMap := map[string]interface{}{
			"algorithm": "SHA512",
			"hash":      h,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSHash(ctx, &service.HashRequest{
			Data:      []byte("data"),
			Algorithm: service.HashAlgorithm("SHA512"),
		})
		require.NoError(t, err)
		assert.Equal(t, service.HashAlgorithm("SHA512"), resp.Algorithm)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSHash(ctx, &service.HashRequest{Data: []byte("d")})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPCryptoClient_KMSGenerateHMAC tests the MCP crypto HMAC generation method.
func TestMCPCryptoClient_KMSGenerateHMAC(t *testing.T) {
	ctx := context.Background()
	hmacVal := base64.StdEncoding.EncodeToString([]byte("hmac-value"))

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":         "hmac-key",
			"key_version":    float64(1),
			"hmac":           hmacVal,
			"hash_algorithm": "SHA256",
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSGenerateHMAC(ctx, &service.HMACRequest{
			KeyID:   "hmac-key",
			Message: []byte("message"),
		})
		require.NoError(t, err)
		assert.Equal(t, "hmac-key", resp.KeyID)
		assert.Equal(t, service.HashAlgorithm("SHA256"), resp.HashAlgorithm)
	})

	t.Run("WithHashAlgorithm", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id": "hmac-key",
			"hmac":   hmacVal,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSGenerateHMAC(ctx, &service.HMACRequest{
			KeyID:         "hmac-key",
			Message:       []byte("msg"),
			HashAlgorithm: service.HashAlgorithm("SHA384"),
		})
		require.NoError(t, err)
		assert.Equal(t, "hmac-key", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSGenerateHMAC(ctx, &service.HMACRequest{KeyID: "k", Message: []byte("m")})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPCryptoClient_KMSVerifyHMAC tests the MCP crypto HMAC verification method.
func TestMCPCryptoClient_KMSVerifyHMAC(t *testing.T) {
	ctx := context.Background()

	t.Run("Valid", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":      "hmac-key",
			"key_version": float64(2),
			"valid":       true,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSVerifyHMAC(ctx, &service.VerifyHMACRequest{
			KeyID:   "hmac-key",
			Message: []byte("message"),
			HMAC:    []byte("hmac-val"),
		})
		require.NoError(t, err)
		assert.True(t, resp.Valid)
		assert.Equal(t, "hmac-key", resp.KeyID)
	})

	t.Run("WithHashAlgorithm", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id": "hmac-key",
			"valid":  false,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSVerifyHMAC(ctx, &service.VerifyHMACRequest{
			KeyID:         "hmac-key",
			Message:       []byte("msg"),
			HMAC:          []byte("hm"),
			HashAlgorithm: service.HashAlgorithm("SHA512"),
		})
		require.NoError(t, err)
		assert.False(t, resp.Valid)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSVerifyHMAC(ctx, &service.VerifyHMACRequest{KeyID: "k", Message: []byte("m"), HMAC: []byte("h")})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPCryptoClient_KMSRewrap tests the MCP crypto rewrap method.
func TestMCPCryptoClient_KMSRewrap(t *testing.T) {
	ctx := context.Background()
	ct := base64.StdEncoding.EncodeToString([]byte("new-ciphertext"))

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":          "rewrap-key",
			"old_key_version": float64(1),
			"new_key_version": float64(2),
			"ciphertext":      ct,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSRewrap(ctx, &service.RewrapRequest{
			KeyID:      "rewrap-key",
			Ciphertext: []byte("old-cipher"),
		})
		require.NoError(t, err)
		assert.Equal(t, "rewrap-key", resp.KeyID)
		assert.Equal(t, uint64(1), resp.OldKeyVersion)
		assert.Equal(t, uint64(2), resp.NewKeyVersion)
	})

	t.Run("WithContext", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":     "rewrap-key",
			"ciphertext": ct,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSRewrap(ctx, &service.RewrapRequest{
			KeyID:      "rewrap-key",
			Ciphertext: []byte("old-cipher"),
			Context:    []byte("t=v"),
		})
		require.NoError(t, err)
		assert.Equal(t, "rewrap-key", resp.KeyID)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSRewrap(ctx, &service.RewrapRequest{KeyID: "k", Ciphertext: []byte("c")})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPCryptoClient_KMSGenerateDataKey tests the MCP crypto data key generation method.
func TestMCPCryptoClient_KMSGenerateDataKey(t *testing.T) {
	ctx := context.Background()
	pt := base64.StdEncoding.EncodeToString([]byte("plaintext-dek"))
	ctb := base64.StdEncoding.EncodeToString([]byte("ciphertext-blob"))

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"key_id":          "dek-key",
			"plaintext":       pt,
			"ciphertext_blob": ctb,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSGenerateDataKey(ctx, &service.GenerateDataKeyRequest{
			KeyID: "dek-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "dek-key", resp.KeyID)
		assert.NotEmpty(t, resp.Plaintext)
		assert.NotEmpty(t, resp.CiphertextBlob)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSGenerateDataKey(ctx, &service.GenerateDataKeyRequest{KeyID: "k"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPCryptoClient_KMSGenerateRandom tests the MCP crypto random generation method.
func TestMCPCryptoClient_KMSGenerateRandom(t *testing.T) {
	ctx := context.Background()
	rb := base64.StdEncoding.EncodeToString([]byte("random-bytes-data"))

	t.Run("Success", func(t *testing.T) {
		respMap := map[string]interface{}{
			"random_bytes": rb,
		}
		c := &mcpCryptoClient{sdk: newTestSDKClient(respMap, nil)}
		resp, err := c.KMSGenerateRandom(ctx, &service.GenerateRandomRequest{
			NumBytes: 32,
		})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.RandomBytes)
	})

	t.Run("TransportError", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("fail"))}
		resp, err := c.KMSGenerateRandom(ctx, &service.GenerateRandomRequest{NumBytes: 16})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestMCPCryptoClient_Close tests the MCP crypto client close method.
func TestMCPCryptoClient_Close(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, nil)}
		err := c.Close()
		assert.NoError(t, err)
	})
}

// TestMCPCryptoClient_CallTool tests the internal callTool method.
func TestMCPCryptoClient_CallTool(t *testing.T) {
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		expected := map[string]interface{}{"result": "ok"}
		c := &mcpCryptoClient{sdk: newTestSDKClient(expected, nil)}
		result, err := c.callTool(ctx, "test_tool", map[string]interface{}{"arg": "val"})
		require.NoError(t, err)
		assert.Equal(t, expected, result)
	})

	t.Run("Error", func(t *testing.T) {
		c := &mcpCryptoClient{sdk: newTestSDKClient(nil, fmt.Errorf("transport error"))}
		result, err := c.callTool(ctx, "test_tool", nil)
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// TestCreateMCPCryptoClient tests the factory function.
func TestCreateMCPCryptoClient(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	t.Run("Success", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newTestSDKClient(nil, nil), nil
		}
		client, err := createMCPCryptoClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("FactoryError", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, fmt.Errorf("factory error")
		}
		client, err := createMCPCryptoClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}
