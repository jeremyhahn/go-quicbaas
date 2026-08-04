//go:build mcp

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

package mcp

import (
	"context"
	"encoding/base64"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func (r *ToolRegistry) registerCryptoTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_encrypt",
		Description: "Encrypt plaintext data using a named encryption key with AEAD",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Encryption key identifier",
				},
				"plaintext": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded plaintext data",
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded additional authenticated data",
				},
				"nonce": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded nonce (algorithm-dependent)",
				},
			},
			"required": []string{"key_id", "plaintext"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_decrypt",
		Description: "Decrypt ciphertext that was encrypted with a named key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Decryption key identifier",
				},
				"ciphertext": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded ciphertext data",
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded additional authenticated data",
				},
				"nonce": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded nonce",
				},
				"key_version": map[string]interface{}{
					"type":        "integer",
					"description": "Specific key version to use",
				},
			},
			"required": []string{"key_id", "ciphertext"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_rewrap",
		Description: "Re-encrypt ciphertext with the latest version of the key without client-side decryption",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
				"ciphertext": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded ciphertext to rewrap",
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded additional authenticated data",
				},
				"nonce": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded nonce",
				},
			},
			"required": []string{"key_id", "ciphertext"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_sign",
		Description: "Create a digital signature of data using a named signing key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Signing key identifier",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded message to sign",
				},
				"hash_algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Hash algorithm (SHA256, SHA384, SHA512, etc.)",
					"enum":        []string{"SHA256", "SHA384", "SHA512", "SHA3-256", "SHA3-384", "SHA3-512", "BLAKE2B-256", "BLAKE2B-512"},
				},
				"pre_hashed": map[string]interface{}{
					"type":        "boolean",
					"description": "When true, message is already a hash digest and is signed directly without hashing again",
				},
			},
			"required": []string{"key_id", "message"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_verify",
		Description: "Verify a digital signature against data using a named key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Verification key identifier",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded signed message",
				},
				"signature": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded signature to verify",
				},
				"hash_algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Hash algorithm used",
				},
				"key_version": map[string]interface{}{
					"type":        "integer",
					"description": "Specific key version to use",
				},
			},
			"required": []string{"key_id", "message", "signature"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_hmac",
		Description: "Generate an HMAC of data using a named key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "HMAC key identifier",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded message to MAC",
				},
				"hash_algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Hash algorithm (SHA256, SHA384, SHA512)",
					"enum":        []string{"SHA256", "SHA384", "SHA512"},
				},
			},
			"required": []string{"key_id", "message"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_verify_hmac",
		Description: "Verify an HMAC against data using a named key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "HMAC key identifier",
				},
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded message",
				},
				"hmac": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded HMAC to verify",
				},
				"hash_algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Hash algorithm used",
				},
				"key_version": map[string]interface{}{
					"type":        "integer",
					"description": "Specific key version to use",
				},
			},
			"required": []string{"key_id", "message", "hmac"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_hash",
		Description: "Compute a cryptographic hash of data (no key required)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"data": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded data to hash",
				},
				"algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Hash algorithm",
					"enum":        []string{"SHA256", "SHA384", "SHA512", "SHA3-256", "SHA3-384", "SHA3-512", "BLAKE2B-256", "BLAKE2B-512"},
				},
			},
			"required": []string{"data", "algorithm"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_random",
		Description: "Generate cryptographically secure random bytes",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"num_bytes": map[string]interface{}{
					"type":        "integer",
					"description": "Number of random bytes to generate",
					"minimum":     1,
					"maximum":     1024,
				},
			},
			"required": []string{"num_bytes"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_datakey",
		Description: "Generate a data encryption key for envelope encryption (returns plaintext and encrypted)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Master key identifier for wrapping",
				},
				"algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Data key algorithm",
					"default":     "AES-256-GCM",
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded additional authenticated data",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_datakey_wrapped",
		Description: "Generate a wrapped data encryption key (returns only encrypted version)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Master key identifier for wrapping",
				},
				"algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Data key algorithm",
					"default":     "AES-256-GCM",
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded additional authenticated data",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_encrypt_batch",
		Description: "Perform multiple encrypt operations in a single request for high throughput",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Encryption key identifier",
				},
				"items": map[string]interface{}{
					"type":        "array",
					"description": "Array of items to encrypt",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"plaintext": map[string]interface{}{"type": "string"},
							"context":   map[string]interface{}{"type": "string"},
						},
						"required": []string{"plaintext"},
					},
				},
			},
			"required": []string{"key_id", "items"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_crypto_decrypt_batch",
		Description: "Perform multiple decrypt operations in a single request for high throughput",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Decryption key identifier",
				},
				"items": map[string]interface{}{
					"type":        "array",
					"description": "Array of items to decrypt",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"ciphertext": map[string]interface{}{"type": "string"},
							"context":    map[string]interface{}{"type": "string"},
							"nonce":      map[string]interface{}{"type": "string"},
						},
						"required": []string{"ciphertext"},
					},
				},
			},
			"required": []string{"key_id", "items"},
		},
	})
}

func (r *ToolRegistry) executeCryptoEncrypt(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		Encrypt(context.Context, *service.EncryptRequest) (*service.EncryptResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	// Decode base64-encoded plaintext
	plaintextB64 := getString(args, "plaintext")
	plaintext, err := base64.StdEncoding.DecodeString(plaintextB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("plaintext", err)
	}

	// Decode base64-encoded context if provided
	var contextData []byte
	if contextB64 := getString(args, "context"); contextB64 != "" {
		contextData, err = base64.StdEncoding.DecodeString(contextB64)
		if err != nil {
			return nil, api.NewInvalidBase64Error("context", err)
		}
	}

	// Decode base64-encoded nonce if provided
	var nonce []byte
	if nonceB64 := getString(args, "nonce"); nonceB64 != "" {
		nonce, err = base64.StdEncoding.DecodeString(nonceB64)
		if err != nil {
			return nil, api.NewInvalidBase64Error("nonce", err)
		}
	}

	req := &service.EncryptRequest{
		KeyID:     getString(args, "key_id"),
		Plaintext: plaintext,
		Context:   contextData,
		Nonce:     nonce,
	}

	resp, err := cryptoSvc.Encrypt(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCryptoDecrypt(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		Decrypt(context.Context, *service.DecryptRequest) (*service.DecryptResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	// Decode base64-encoded ciphertext
	ciphertextB64 := getString(args, "ciphertext")
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("ciphertext", err)
	}

	// Decode base64-encoded context if provided
	var contextData []byte
	if contextB64 := getString(args, "context"); contextB64 != "" {
		contextData, err = base64.StdEncoding.DecodeString(contextB64)
		if err != nil {
			return nil, api.NewInvalidBase64Error("context", err)
		}
	}

	// Decode base64-encoded nonce if provided
	var nonce []byte
	if nonceB64 := getString(args, "nonce"); nonceB64 != "" {
		nonce, err = base64.StdEncoding.DecodeString(nonceB64)
		if err != nil {
			return nil, api.NewInvalidBase64Error("nonce", err)
		}
	}

	req := &service.DecryptRequest{
		KeyID:      getString(args, "key_id"),
		Ciphertext: ciphertext,
		Context:    contextData,
		Nonce:      nonce,
		KeyVersion: getUint64(args, "key_version"),
	}

	resp, err := cryptoSvc.Decrypt(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCryptoRewrap(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		Rewrap(context.Context, *service.RewrapRequest) (*service.RewrapResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	// Decode base64-encoded ciphertext
	ciphertextB64 := getString(args, "ciphertext")
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("ciphertext", err)
	}

	// Decode base64-encoded context if provided
	var contextData []byte
	if contextB64 := getString(args, "context"); contextB64 != "" {
		contextData, err = base64.StdEncoding.DecodeString(contextB64)
		if err != nil {
			return nil, api.NewInvalidBase64Error("context", err)
		}
	}

	// Decode base64-encoded nonce if provided
	var nonce []byte
	if nonceB64 := getString(args, "nonce"); nonceB64 != "" {
		nonce, err = base64.StdEncoding.DecodeString(nonceB64)
		if err != nil {
			return nil, api.NewInvalidBase64Error("nonce", err)
		}
	}

	req := &service.RewrapRequest{
		KeyID:      getString(args, "key_id"),
		Ciphertext: ciphertext,
		Context:    contextData,
		Nonce:      nonce,
	}

	resp, err := cryptoSvc.Rewrap(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCryptoSign(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		Sign(context.Context, *service.SignRequest) (*service.SignResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	// Decode base64-encoded message
	messageB64 := getString(args, "message")
	message, err := base64.StdEncoding.DecodeString(messageB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("message", err)
	}

	req := &service.SignRequest{
		KeyID:         getString(args, "key_id"),
		Message:       message,
		HashAlgorithm: service.HashAlgorithm(getString(args, "hash_algorithm")),
		PreHashed:     getBool(args, "pre_hashed"),
	}

	resp, err := cryptoSvc.Sign(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCryptoVerify(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		Verify(context.Context, *service.VerifyRequest) (*service.VerifyResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	// Decode base64-encoded message
	messageB64 := getString(args, "message")
	message, err := base64.StdEncoding.DecodeString(messageB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("message", err)
	}

	// Decode base64-encoded signature
	signatureB64 := getString(args, "signature")
	signature, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("signature", err)
	}

	req := &service.VerifyRequest{
		KeyID:         getString(args, "key_id"),
		Message:       message,
		Signature:     signature,
		HashAlgorithm: service.HashAlgorithm(getString(args, "hash_algorithm")),
		KeyVersion:    getUint64(args, "key_version"),
	}

	resp, err := cryptoSvc.Verify(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCryptoHMAC(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		GenerateHMAC(context.Context, *service.HMACRequest) (*service.HMACResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	// Decode base64-encoded message
	messageB64 := getString(args, "message")
	message, err := base64.StdEncoding.DecodeString(messageB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("message", err)
	}

	req := &service.HMACRequest{
		KeyID:         getString(args, "key_id"),
		Message:       message,
		HashAlgorithm: service.HashAlgorithm(getString(args, "hash_algorithm")),
	}

	resp, err := cryptoSvc.GenerateHMAC(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCryptoVerifyHMAC(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		VerifyHMAC(context.Context, *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	// Decode base64-encoded message
	messageB64 := getString(args, "message")
	message, err := base64.StdEncoding.DecodeString(messageB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("message", err)
	}

	// Decode base64-encoded hmac
	hmacB64 := getString(args, "hmac")
	hmacData, err := base64.StdEncoding.DecodeString(hmacB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("hmac", err)
	}

	req := &service.VerifyHMACRequest{
		KeyID:         getString(args, "key_id"),
		Message:       message,
		HMAC:          hmacData,
		HashAlgorithm: service.HashAlgorithm(getString(args, "hash_algorithm")),
		KeyVersion:    getUint64(args, "key_version"),
	}

	resp, err := cryptoSvc.VerifyHMAC(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCryptoHash(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		Hash(context.Context, *service.HashRequest) (*service.HashResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	// Decode base64-encoded data
	dataB64 := getString(args, "data")
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("data", err)
	}

	req := &service.HashRequest{
		Data:      data,
		Algorithm: service.HashAlgorithm(getString(args, "algorithm")),
	}

	resp, err := cryptoSvc.Hash(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCryptoRandom(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		GenerateRandom(context.Context, *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	req := &service.GenerateRandomRequest{
		NumBytes: int(getUint64(args, "num_bytes")), //nolint:gosec // G115: uint64 to int conversion for byte count,
	}

	resp, err := cryptoSvc.GenerateRandom(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

// algorithmToKeyAttributes converts an algorithm string to KeyAttributes for data key generation.
func algorithmToKeyAttributes(algorithm string) *kctypes.KeyAttributes {
	attrs := &kctypes.KeyAttributes{
		CN: "data-key",
	}

	switch algorithm {
	case "AES-256-GCM", "AES256GCM":
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
	case "AES-192-GCM", "AES192GCM":
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES192GCM
	case "AES-128-GCM", "AES128GCM":
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES128GCM
	case "CHACHA20-POLY1305", "ChaCha20-Poly1305":
		attrs.SymmetricAlgorithm = kctypes.SymmetricChaCha20Poly1305
	case "XCHACHA20-POLY1305", "XChaCha20-Poly1305":
		attrs.SymmetricAlgorithm = kctypes.SymmetricXChaCha20Poly1305
	default:
		// Default to AES-256-GCM
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
	}

	return attrs
}

func (r *ToolRegistry) executeCryptoDataKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		GenerateDataKey(context.Context, *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	algorithm := getString(args, "algorithm")
	if algorithm == "" {
		algorithm = "AES-256-GCM"
	}

	// Decode base64-encoded context if provided
	var contextData []byte
	var err error
	if contextB64 := getString(args, "context"); contextB64 != "" {
		contextData, err = base64.StdEncoding.DecodeString(contextB64)
		if err != nil {
			return nil, api.NewInvalidBase64Error("context", err)
		}
	}

	req := &service.GenerateDataKeyRequest{
		KeyID:         getString(args, "key_id"),
		KeyAttributes: algorithmToKeyAttributes(algorithm),
		Context:       contextData,
	}

	resp, err := cryptoSvc.GenerateDataKey(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCryptoDataKeyWrapped(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		GenerateWrappedDataKey(context.Context, *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	algorithm := getString(args, "algorithm")
	if algorithm == "" {
		algorithm = "AES-256-GCM"
	}

	// Decode base64-encoded context if provided
	var contextData []byte
	var err error
	if contextB64 := getString(args, "context"); contextB64 != "" {
		contextData, err = base64.StdEncoding.DecodeString(contextB64)
		if err != nil {
			return nil, api.NewInvalidBase64Error("context", err)
		}
	}

	req := &service.GenerateWrappedDataKeyRequest{
		KeyID:         getString(args, "key_id"),
		KeyAttributes: algorithmToKeyAttributes(algorithm),
		Context:       contextData,
	}

	resp, err := cryptoSvc.GenerateWrappedDataKey(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCryptoEncryptBatch(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		EncryptBatch(context.Context, *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	itemsRaw, ok := args["items"].([]interface{})
	if !ok {
		return nil, api.NewInvalidTypeError("items", "an array")
	}

	items := make([]service.BatchEncryptItem, len(itemsRaw))
	for i, itemRaw := range itemsRaw {
		itemMap, ok := itemRaw.(map[string]interface{})
		if !ok {
			return nil, api.NewBatchItemMessageError(i, "must be an object")
		}

		// Decode base64-encoded plaintext
		plaintextB64 := getString(itemMap, "plaintext")
		plaintext, err := base64.StdEncoding.DecodeString(plaintextB64)
		if err != nil {
			return nil, api.NewBatchItemError(i, "invalid base64 plaintext", err)
		}

		// Decode base64-encoded context if provided
		var contextData []byte
		if contextB64 := getString(itemMap, "context"); contextB64 != "" {
			contextData, err = base64.StdEncoding.DecodeString(contextB64)
			if err != nil {
				return nil, api.NewBatchItemError(i, "invalid base64 context", err)
			}
		}

		items[i] = service.BatchEncryptItem{
			Plaintext: plaintext,
			Context:   contextData,
		}
	}

	req := &service.EncryptBatchRequest{
		KeyID: getString(args, "key_id"),
		Items: items,
	}

	resp, err := cryptoSvc.EncryptBatch(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeCryptoDecryptBatch(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cryptoSvc, ok := r.service.(interface {
		DecryptBatch(context.Context, *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("crypto")
	}

	itemsRaw, ok := args["items"].([]interface{})
	if !ok {
		return nil, api.NewInvalidTypeError("items", "an array")
	}

	items := make([]service.BatchDecryptItem, len(itemsRaw))
	for i, itemRaw := range itemsRaw {
		itemMap, ok := itemRaw.(map[string]interface{})
		if !ok {
			return nil, api.NewBatchItemMessageError(i, "must be an object")
		}

		// Decode base64-encoded ciphertext
		ciphertextB64 := getString(itemMap, "ciphertext")
		ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
		if err != nil {
			return nil, api.NewBatchItemError(i, "invalid base64 ciphertext", err)
		}

		// Decode base64-encoded context if provided
		var contextData []byte
		if contextB64 := getString(itemMap, "context"); contextB64 != "" {
			contextData, err = base64.StdEncoding.DecodeString(contextB64)
			if err != nil {
				return nil, api.NewBatchItemError(i, "invalid base64 context", err)
			}
		}

		// Decode base64-encoded nonce if provided
		var nonce []byte
		if nonceB64 := getString(itemMap, "nonce"); nonceB64 != "" {
			nonce, err = base64.StdEncoding.DecodeString(nonceB64)
			if err != nil {
				return nil, api.NewBatchItemError(i, "invalid base64 nonce", err)
			}
		}

		items[i] = service.BatchDecryptItem{
			Ciphertext: ciphertext,
			Context:    contextData,
			Nonce:      nonce,
		}
	}

	req := &service.DecryptBatchRequest{
		KeyID: getString(args, "key_id"),
		Items: items,
	}

	resp, err := cryptoSvc.DecryptBatch(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}
