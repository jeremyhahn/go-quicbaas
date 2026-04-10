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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"encoding/base64"
	"time"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// mcpCryptoClient implements the CryptoClient interface using MCP transport.
type mcpCryptoClient struct {
	sdk quicbaas.Client
}

// createMCPCryptoClient creates an MCP client for crypto operations.
func createMCPCryptoClient() (CryptoClient, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return &mcpCryptoClient{sdk: sdk}, nil
}

// KMSEncrypt encrypts plaintext using the specified key.
func (c *mcpCryptoClient) KMSEncrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	args := map[string]interface{}{
		"key_id":    req.KeyID,
		"plaintext": base64.StdEncoding.EncodeToString(req.Plaintext),
	}
	if len(req.Context) > 0 {
		args["context"] = req.Context
	}

	result, err := c.callTool(ctx, "quicbaas_crypto_encrypt", args)
	if err != nil {
		return nil, err
	}

	return parseCryptoEncryptResponse(result)
}

// KMSDecrypt decrypts ciphertext using the specified key.
func (c *mcpCryptoClient) KMSDecrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	args := map[string]interface{}{
		"key_id":     req.KeyID,
		"ciphertext": base64.StdEncoding.EncodeToString(req.Ciphertext),
	}
	if len(req.Context) > 0 {
		args["context"] = req.Context
	}

	result, err := c.callTool(ctx, "quicbaas_crypto_decrypt", args)
	if err != nil {
		return nil, err
	}

	return parseCryptoDecryptResponse(result)
}

// KMSSign signs data using the specified key.
func (c *mcpCryptoClient) KMSSign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	args := map[string]interface{}{
		"key_id":  req.KeyID,
		"message": base64.StdEncoding.EncodeToString(req.Message),
	}
	if req.HashAlgorithm != "" {
		args["hash_algorithm"] = string(req.HashAlgorithm)
	}

	result, err := c.callTool(ctx, "quicbaas_crypto_sign", args)
	if err != nil {
		return nil, err
	}

	return parseCryptoSignResponse(result)
}

// KMSVerify verifies a signature using the specified key.
func (c *mcpCryptoClient) KMSVerify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	args := map[string]interface{}{
		"key_id":    req.KeyID,
		"message":   base64.StdEncoding.EncodeToString(req.Message),
		"signature": base64.StdEncoding.EncodeToString(req.Signature),
	}
	if req.HashAlgorithm != "" {
		args["hash_algorithm"] = string(req.HashAlgorithm)
	}

	result, err := c.callTool(ctx, "quicbaas_crypto_verify", args)
	if err != nil {
		return nil, err
	}

	return parseCryptoVerifyResponse(result)
}

// KMSHash computes a cryptographic hash.
func (c *mcpCryptoClient) KMSHash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	args := map[string]interface{}{
		"data": base64.StdEncoding.EncodeToString(req.Data),
	}
	if req.Algorithm != "" {
		args["algorithm"] = string(req.Algorithm)
	}

	result, err := c.callTool(ctx, "quicbaas_crypto_hash", args)
	if err != nil {
		return nil, err
	}

	return parseCryptoHashResponse(result)
}

// KMSGenerateHMAC generates an HMAC using the specified key.
func (c *mcpCryptoClient) KMSGenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	args := map[string]interface{}{
		"key_id":  req.KeyID,
		"message": base64.StdEncoding.EncodeToString(req.Message),
	}
	if req.HashAlgorithm != "" {
		args["hash_algorithm"] = string(req.HashAlgorithm)
	}

	result, err := c.callTool(ctx, "quicbaas_crypto_hmac", args)
	if err != nil {
		return nil, err
	}

	return parseCryptoHMACResponse(result)
}

// KMSVerifyHMAC verifies an HMAC using the specified key.
func (c *mcpCryptoClient) KMSVerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	args := map[string]interface{}{
		"key_id":  req.KeyID,
		"message": base64.StdEncoding.EncodeToString(req.Message),
		"hmac":    base64.StdEncoding.EncodeToString(req.HMAC),
	}
	if req.HashAlgorithm != "" {
		args["hash_algorithm"] = string(req.HashAlgorithm)
	}

	result, err := c.callTool(ctx, "quicbaas_crypto_verify_hmac", args)
	if err != nil {
		return nil, err
	}

	return parseCryptoVerifyHMACResponse(result)
}

// KMSRewrap re-encrypts data with the latest key version.
func (c *mcpCryptoClient) KMSRewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	args := map[string]interface{}{
		"key_id":     req.KeyID,
		"ciphertext": base64.StdEncoding.EncodeToString(req.Ciphertext),
	}
	if len(req.Context) > 0 {
		args["context"] = req.Context
	}

	result, err := c.callTool(ctx, "quicbaas_crypto_rewrap", args)
	if err != nil {
		return nil, err
	}

	return parseCryptoRewrapResponse(result)
}

// KMSGenerateDataKey generates a new data encryption key.
func (c *mcpCryptoClient) KMSGenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	args := map[string]interface{}{
		"key_id": req.KeyID,
	}

	result, err := c.callTool(ctx, "quicbaas_crypto_datakey", args)
	if err != nil {
		return nil, err
	}

	return parseCryptoGenerateDataKeyResponse(result)
}

// KMSGenerateRandom generates cryptographically secure random bytes.
func (c *mcpCryptoClient) KMSGenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	args := map[string]interface{}{
		"num_bytes": req.NumBytes,
	}

	result, err := c.callTool(ctx, "quicbaas_crypto_random", args)
	if err != nil {
		return nil, err
	}

	return parseCryptoGenerateRandomResponse(result)
}

// Close closes the underlying SDK client.
func (c *mcpCryptoClient) Close() error {
	return c.sdk.Close()
}

func (c *mcpCryptoClient) callTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	return c.sdk.Transport().Request(ctx, "MCP", toolName, args)
}

// =============================================================================
// Response Parsing Functions
// =============================================================================

// parseCryptoEncryptResponse parses the MCP response into an EncryptResponse.
func parseCryptoEncryptResponse(result interface{}) (*service.EncryptResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.EncryptResponse{
		KeyID:      getString(data, "key_id"),
		KeyVersion: uint64(getInt64(data, "key_version")),
		Ciphertext: getBytes(data, "ciphertext"),
	}
	return resp, nil
}

// parseCryptoDecryptResponse parses the MCP response into a DecryptResponse.
func parseCryptoDecryptResponse(result interface{}) (*service.DecryptResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.DecryptResponse{
		KeyID:      getString(data, "key_id"),
		KeyVersion: uint64(getInt64(data, "key_version")),
		Plaintext:  getBytes(data, "plaintext"),
	}
	return resp, nil
}

// parseCryptoSignResponse parses the MCP response into a SignResponse.
func parseCryptoSignResponse(result interface{}) (*service.SignResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.SignResponse{
		KeyID:         getString(data, "key_id"),
		KeyVersion:    uint64(getInt64(data, "key_version")),
		Signature:     getBytes(data, "signature"),
		HashAlgorithm: service.HashAlgorithm(getString(data, "hash_algorithm")),
		Timestamp:     time.Now(),
	}
	return resp, nil
}

// parseCryptoVerifyResponse parses the MCP response into a VerifyResponse.
func parseCryptoVerifyResponse(result interface{}) (*service.VerifyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.VerifyResponse{
		KeyID:      getString(data, "key_id"),
		KeyVersion: uint64(getInt64(data, "key_version")),
		Valid:      getBool(data, "valid"),
		Timestamp:  time.Now(),
	}
	return resp, nil
}

// parseCryptoHashResponse parses the MCP response into a HashResponse.
func parseCryptoHashResponse(result interface{}) (*service.HashResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.HashResponse{
		Algorithm: service.HashAlgorithm(getString(data, "algorithm")),
		Hash:      getBytes(data, "hash"),
		Timestamp: time.Now(),
	}
	return resp, nil
}

// parseCryptoHMACResponse parses the MCP response into an HMACResponse.
func parseCryptoHMACResponse(result interface{}) (*service.HMACResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.HMACResponse{
		KeyID:         getString(data, "key_id"),
		KeyVersion:    uint64(getInt64(data, "key_version")),
		HMAC:          getBytes(data, "hmac"),
		HashAlgorithm: service.HashAlgorithm(getString(data, "hash_algorithm")),
		Timestamp:     time.Now(),
	}
	return resp, nil
}

// parseCryptoVerifyHMACResponse parses the MCP response into a VerifyHMACResponse.
func parseCryptoVerifyHMACResponse(result interface{}) (*service.VerifyHMACResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.VerifyHMACResponse{
		KeyID:      getString(data, "key_id"),
		KeyVersion: uint64(getInt64(data, "key_version")),
		Valid:      getBool(data, "valid"),
		Timestamp:  time.Now(),
	}
	return resp, nil
}

// parseCryptoRewrapResponse parses the MCP response into a RewrapResponse.
func parseCryptoRewrapResponse(result interface{}) (*service.RewrapResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.RewrapResponse{
		KeyID:         getString(data, "key_id"),
		OldKeyVersion: uint64(getInt64(data, "old_key_version")),
		NewKeyVersion: uint64(getInt64(data, "new_key_version")),
		Ciphertext:    getBytes(data, "ciphertext"),
		Timestamp:     time.Now(),
	}
	return resp, nil
}

// parseCryptoGenerateDataKeyResponse parses the MCP response into a GenerateDataKeyResponse.
func parseCryptoGenerateDataKeyResponse(result interface{}) (*service.GenerateDataKeyResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.GenerateDataKeyResponse{
		KeyID:          getString(data, "key_id"),
		Plaintext:      getBytes(data, "plaintext"),
		CiphertextBlob: getBytes(data, "ciphertext_blob"),
		Timestamp:      time.Now(),
	}
	return resp, nil
}

// parseCryptoGenerateRandomResponse parses the MCP response into a GenerateRandomResponse.
func parseCryptoGenerateRandomResponse(result interface{}) (*service.GenerateRandomResponse, error) {
	data, err := toJSONMap(result)
	if err != nil {
		return nil, err
	}

	resp := &service.GenerateRandomResponse{
		RandomBytes: getBytes(data, "random_bytes"),
		Timestamp:   time.Now(),
	}
	return resp, nil
}

// Compile-time interface check.
var _ CryptoClient = (*mcpCryptoClient)(nil)
