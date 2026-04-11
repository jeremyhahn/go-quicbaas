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

package crypto

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Crypto"
)

// Client provides encryption/decryption as a service.
type Client interface {
	// Encrypt encrypts plaintext using the specified key.
	Encrypt(ctx context.Context, req *EncryptRequest) (*EncryptResponse, error)

	// Decrypt decrypts ciphertext using the specified key.
	Decrypt(ctx context.Context, req *DecryptRequest) (*DecryptResponse, error)

	// Sign signs data using the specified key.
	Sign(ctx context.Context, req *SignRequest) (*SignResponse, error)

	// Verify verifies a signature using the specified key.
	Verify(ctx context.Context, req *VerifyRequest) (*VerifyResponse, error)

	// Rewrap re-encrypts ciphertext with the latest version of the key.
	Rewrap(ctx context.Context, req *RewrapRequest) (*RewrapResponse, error)

	// EncryptBatch performs multiple encrypt operations in a single request.
	EncryptBatch(ctx context.Context, req *EncryptBatchRequest) (*EncryptBatchResponse, error)

	// DecryptBatch performs multiple decrypt operations in a single request.
	DecryptBatch(ctx context.Context, req *DecryptBatchRequest) (*DecryptBatchResponse, error)

	// GenerateDataKey generates a data encryption key for envelope encryption.
	GenerateDataKey(ctx context.Context, req *GenerateDataKeyRequest) (*GenerateDataKeyResponse, error)

	// GenerateWrappedDataKey generates an encrypted data key without plaintext.
	GenerateWrappedDataKey(ctx context.Context, req *GenerateWrappedDataKeyRequest) (*GenerateWrappedDataKeyResponse, error)

	// GenerateHMAC generates an HMAC of data using a named key.
	GenerateHMAC(ctx context.Context, req *GenerateHMACRequest) (*GenerateHMACResponse, error)

	// VerifyHMAC verifies an HMAC against data using a named key.
	VerifyHMAC(ctx context.Context, req *VerifyHMACRequest) (*VerifyHMACResponse, error)

	// GenerateRandom generates high-quality random bytes.
	GenerateRandom(ctx context.Context, req *GenerateRandomRequest) (*GenerateRandomResponse, error)

	// Hash computes a cryptographic hash of data.
	Hash(ctx context.Context, req *HashRequest) (*HashResponse, error)
}

// =============================================================================
// Types
// =============================================================================

// EncryptRequest is a request to encrypt data.
type EncryptRequest struct {
	KeyID     string
	Plaintext []byte
	Context   []byte // Optional AAD (additional authenticated data)
}

// EncryptResponse is the response from encryption.
type EncryptResponse struct {
	Ciphertext []byte
	KeyVersion int
}

// DecryptRequest is a request to decrypt data.
type DecryptRequest struct {
	KeyID      string
	Ciphertext []byte
	Context    []byte // Optional AAD (must match encryption)
}

// DecryptResponse is the response from decryption.
type DecryptResponse struct {
	Plaintext []byte
}

// SignRequest is a request to sign data.
type SignRequest struct {
	KeyID     string
	Data      []byte
	HashAlg   HashAlgorithm
	PreHashed bool // When true, Data is already a hash digest (e.g., from crypto.Signer)
}

// HashAlgorithm represents the hash algorithm for signing.
type HashAlgorithm int

const (
	// HashSHA256 is SHA-256 hash algorithm.
	HashSHA256 HashAlgorithm = iota
	// HashSHA384 is SHA-384 hash algorithm.
	HashSHA384
	// HashSHA512 is SHA-512 hash algorithm.
	HashSHA512
)

// String returns the string representation of the hash algorithm.
func (h HashAlgorithm) String() string {
	switch h {
	case HashSHA256:
		return "SHA-256"
	case HashSHA384:
		return "SHA-384"
	case HashSHA512:
		return "SHA-512"
	default:
		return "UNKNOWN"
	}
}

// SignResponse is the response from signing.
type SignResponse struct {
	Signature  []byte
	KeyVersion int
}

// VerifyRequest is a request to verify a signature.
type VerifyRequest struct {
	KeyID     string
	Data      []byte
	Signature []byte
	HashAlg   HashAlgorithm
}

// VerifyResponse is the response from verification.
type VerifyResponse struct {
	Valid bool
}

// RewrapRequest is a request to re-encrypt ciphertext with the latest key version.
type RewrapRequest struct {
	KeyID      string
	Ciphertext []byte
	Context    []byte // Optional AAD
	Nonce      []byte // Optional nonce
}

// RewrapResponse is the response from rewrapping.
type RewrapResponse struct {
	KeyID         string
	OldKeyVersion int
	NewKeyVersion int
	Ciphertext    []byte
	Nonce         []byte
}

// BatchEncryptItem represents a single item in a batch encrypt request.
type BatchEncryptItem struct {
	Plaintext []byte
	Context   []byte
}

// EncryptBatchRequest is a request to encrypt multiple items.
type EncryptBatchRequest struct {
	KeyID string
	Items []BatchEncryptItem
}

// BatchEncryptResult represents the result of encrypting one item.
type BatchEncryptResult struct {
	Ciphertext []byte
	Nonce      []byte
	Error      string
}

// EncryptBatchResponse is the response from batch encryption.
type EncryptBatchResponse struct {
	KeyID      string
	KeyVersion int
	Results    []BatchEncryptResult
}

// BatchDecryptItem represents a single item in a batch decrypt request.
type BatchDecryptItem struct {
	Ciphertext []byte
	Context    []byte
	Nonce      []byte
}

// DecryptBatchRequest is a request to decrypt multiple items.
type DecryptBatchRequest struct {
	KeyID string
	Items []BatchDecryptItem
}

// BatchDecryptResult represents the result of decrypting one item.
type BatchDecryptResult struct {
	Plaintext []byte
	Error     string
}

// DecryptBatchResponse is the response from batch decryption.
type DecryptBatchResponse struct {
	KeyID   string
	Results []BatchDecryptResult
}

// GenerateDataKeyRequest is a request to generate a data encryption key.
type GenerateDataKeyRequest struct {
	// KeyID is the master key for wrapping.
	KeyID string

	// KeySize is the desired data key size in bits (e.g., 256).
	KeySize int

	// Context is optional additional authenticated data.
	Context []byte
}

// GenerateDataKeyResponse is the response from generating a data key.
type GenerateDataKeyResponse struct {
	KeyID          string
	Plaintext      []byte
	CiphertextBlob []byte
}

// GenerateWrappedDataKeyRequest is a request to generate a wrapped-only data key.
type GenerateWrappedDataKeyRequest struct {
	// KeyID is the master key for wrapping.
	KeyID string

	// KeySize is the desired data key size in bits (e.g., 256).
	KeySize int

	// Context is optional additional authenticated data.
	Context []byte
}

// GenerateWrappedDataKeyResponse is the response from generating a wrapped data key.
type GenerateWrappedDataKeyResponse struct {
	KeyID          string
	CiphertextBlob []byte
}

// GenerateHMACRequest is a request to generate an HMAC.
type GenerateHMACRequest struct {
	KeyID   string
	Message []byte
	HashAlg HashAlgorithm
}

// GenerateHMACResponse is the response from HMAC generation.
type GenerateHMACResponse struct {
	KeyID      string
	KeyVersion int
	HMAC       []byte
	HashAlg    HashAlgorithm
}

// VerifyHMACRequest is a request to verify an HMAC.
type VerifyHMACRequest struct {
	KeyID      string
	Message    []byte
	HMAC       []byte
	HashAlg    HashAlgorithm
	KeyVersion int
}

// VerifyHMACResponse is the response from HMAC verification.
type VerifyHMACResponse struct {
	Valid bool
}

// GenerateRandomRequest is a request to generate random bytes.
type GenerateRandomRequest struct {
	NumBytes int
}

// GenerateRandomResponse is the response from random byte generation.
type GenerateRandomResponse struct {
	RandomBytes []byte
}

// HashRequest is a request to compute a hash.
type HashRequest struct {
	Data    []byte
	HashAlg HashAlgorithm
}

// HashResponse is the response from hashing.
type HashResponse struct {
	HashAlg HashAlgorithm
	Hash    []byte
}

// =============================================================================
// Client implementation
// =============================================================================

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new crypto client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Encrypt encrypts plaintext using the specified key.
func (c *client) Encrypt(ctx context.Context, req *EncryptRequest) (*EncryptResponse, error) {
	if err := validateEncryptRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.Encrypt", "context is required")
	}

	// Convert SDK request to service request for embedded transport
	svcReq := &service.EncryptRequest{
		KeyID:     req.KeyID,
		Plaintext: req.Plaintext,
		Context:   req.Context,
	}

	resp, err := c.transport.Request(ctx, serviceName, "Encrypt", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.Encrypt", "nil response from server")
	}

	// Handle both SDK and service response types
	switch r := resp.(type) {
	case *EncryptResponse:
		return r, nil
	case *service.EncryptResponse:
		return &EncryptResponse{
			Ciphertext: r.Ciphertext,
			KeyVersion: int(r.KeyVersion),
		}, nil
	default:
		return nil, errors.NewInternal("crypto.Encrypt", "invalid response type")
	}
}

// Decrypt decrypts ciphertext using the specified key.
func (c *client) Decrypt(ctx context.Context, req *DecryptRequest) (*DecryptResponse, error) {
	if err := validateDecryptRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.Decrypt", "context is required")
	}

	// Convert SDK request to service request for embedded transport
	svcReq := &service.DecryptRequest{
		KeyID:      req.KeyID,
		Ciphertext: req.Ciphertext,
		Context:    req.Context,
	}

	resp, err := c.transport.Request(ctx, serviceName, "Decrypt", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.Decrypt", "nil response from server")
	}

	// Handle both SDK and service response types
	switch r := resp.(type) {
	case *DecryptResponse:
		return r, nil
	case *service.DecryptResponse:
		return &DecryptResponse{
			Plaintext: r.Plaintext,
		}, nil
	default:
		return nil, errors.NewInternal("crypto.Decrypt", "invalid response type")
	}
}

// Sign signs data using the specified key.
func (c *client) Sign(ctx context.Context, req *SignRequest) (*SignResponse, error) {
	if err := validateSignRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.Sign", "context is required")
	}

	// Convert SDK request to service request for embedded transport
	svcReq := &service.SignRequest{
		KeyID:         req.KeyID,
		Message:       req.Data,
		HashAlgorithm: hashAlgToServiceHashAlg(req.HashAlg),
		PreHashed:     req.PreHashed,
	}

	resp, err := c.transport.Request(ctx, serviceName, "Sign", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.Sign", "nil response from server")
	}

	// Handle both SDK and service response types
	switch r := resp.(type) {
	case *SignResponse:
		return r, nil
	case *service.SignResponse:
		return &SignResponse{
			Signature:  r.Signature,
			KeyVersion: int(r.KeyVersion),
		}, nil
	default:
		return nil, errors.NewInternal("crypto.Sign", "invalid response type")
	}
}

// Verify verifies a signature using the specified key.
func (c *client) Verify(ctx context.Context, req *VerifyRequest) (*VerifyResponse, error) {
	if err := validateVerifyRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.Verify", "context is required")
	}

	// Convert SDK request to service request for embedded transport
	svcReq := &service.VerifyRequest{
		KeyID:         req.KeyID,
		Message:       req.Data,
		Signature:     req.Signature,
		HashAlgorithm: hashAlgToServiceHashAlg(req.HashAlg),
	}

	resp, err := c.transport.Request(ctx, serviceName, "Verify", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.Verify", "nil response from server")
	}

	// Handle both SDK and service response types
	switch r := resp.(type) {
	case *VerifyResponse:
		return r, nil
	case *service.VerifyResponse:
		return &VerifyResponse{
			Valid: r.Valid,
		}, nil
	default:
		return nil, errors.NewInternal("crypto.Verify", "invalid response type")
	}
}

// Rewrap re-encrypts ciphertext with the latest version of the key.
func (c *client) Rewrap(ctx context.Context, req *RewrapRequest) (*RewrapResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("crypto.Rewrap", "request is required")
	}
	if req.KeyID == "" {
		return nil, errors.NewInvalidInput("crypto.Rewrap", "key ID is required")
	}
	if len(req.Ciphertext) == 0 {
		return nil, errors.NewInvalidInput("crypto.Rewrap", "ciphertext is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.Rewrap", "context is required")
	}

	svcReq := &service.RewrapRequest{
		KeyID:      req.KeyID,
		Ciphertext: req.Ciphertext,
		Context:    req.Context,
		Nonce:      req.Nonce,
	}

	resp, err := c.transport.Request(ctx, serviceName, "Rewrap", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.Rewrap", "nil response from server")
	}

	switch r := resp.(type) {
	case *RewrapResponse:
		return r, nil
	case *service.RewrapResponse:
		return &RewrapResponse{
			KeyID:         r.KeyID,
			OldKeyVersion: int(r.OldKeyVersion),
			NewKeyVersion: int(r.NewKeyVersion),
			Ciphertext:    r.Ciphertext,
			Nonce:         r.Nonce,
		}, nil
	default:
		return nil, errors.NewInternal("crypto.Rewrap", "invalid response type")
	}
}

// EncryptBatch performs multiple encrypt operations in a single request.
func (c *client) EncryptBatch(ctx context.Context, req *EncryptBatchRequest) (*EncryptBatchResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("crypto.EncryptBatch", "request is required")
	}
	if req.KeyID == "" {
		return nil, errors.NewInvalidInput("crypto.EncryptBatch", "key ID is required")
	}
	if len(req.Items) == 0 {
		return nil, errors.NewInvalidInput("crypto.EncryptBatch", "items are required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.EncryptBatch", "context is required")
	}

	svcItems := make([]service.BatchEncryptItem, len(req.Items))
	for i, item := range req.Items {
		svcItems[i] = service.BatchEncryptItem{
			Plaintext: item.Plaintext,
			Context:   item.Context,
		}
	}
	svcReq := &service.EncryptBatchRequest{
		KeyID: req.KeyID,
		Items: svcItems,
	}

	resp, err := c.transport.Request(ctx, serviceName, "EncryptBatch", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.EncryptBatch", "nil response from server")
	}

	switch r := resp.(type) {
	case *EncryptBatchResponse:
		return r, nil
	case *service.EncryptBatchResponse:
		results := make([]BatchEncryptResult, len(r.Results))
		for i, res := range r.Results {
			results[i] = BatchEncryptResult{
				Ciphertext: res.Ciphertext,
				Nonce:      res.Nonce,
				Error:      res.Error,
			}
		}
		return &EncryptBatchResponse{
			KeyID:      r.KeyID,
			KeyVersion: int(r.KeyVersion),
			Results:    results,
		}, nil
	default:
		return nil, errors.NewInternal("crypto.EncryptBatch", "invalid response type")
	}
}

// DecryptBatch performs multiple decrypt operations in a single request.
func (c *client) DecryptBatch(ctx context.Context, req *DecryptBatchRequest) (*DecryptBatchResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("crypto.DecryptBatch", "request is required")
	}
	if req.KeyID == "" {
		return nil, errors.NewInvalidInput("crypto.DecryptBatch", "key ID is required")
	}
	if len(req.Items) == 0 {
		return nil, errors.NewInvalidInput("crypto.DecryptBatch", "items are required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.DecryptBatch", "context is required")
	}

	svcItems := make([]service.BatchDecryptItem, len(req.Items))
	for i, item := range req.Items {
		svcItems[i] = service.BatchDecryptItem{
			Ciphertext: item.Ciphertext,
			Context:    item.Context,
			Nonce:      item.Nonce,
		}
	}
	svcReq := &service.DecryptBatchRequest{
		KeyID: req.KeyID,
		Items: svcItems,
	}

	resp, err := c.transport.Request(ctx, serviceName, "DecryptBatch", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.DecryptBatch", "nil response from server")
	}

	switch r := resp.(type) {
	case *DecryptBatchResponse:
		return r, nil
	case *service.DecryptBatchResponse:
		results := make([]BatchDecryptResult, len(r.Results))
		for i, res := range r.Results {
			results[i] = BatchDecryptResult{
				Plaintext: res.Plaintext,
				Error:     res.Error,
			}
		}
		return &DecryptBatchResponse{
			KeyID:   r.KeyID,
			Results: results,
		}, nil
	default:
		return nil, errors.NewInternal("crypto.DecryptBatch", "invalid response type")
	}
}

// GenerateDataKey generates a data encryption key for envelope encryption.
func (c *client) GenerateDataKey(ctx context.Context, req *GenerateDataKeyRequest) (*GenerateDataKeyResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("crypto.GenerateDataKey", "request is required")
	}
	if req.KeyID == "" {
		return nil, errors.NewInvalidInput("crypto.GenerateDataKey", "key ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.GenerateDataKey", "context is required")
	}

	svcReq := &service.GenerateDataKeyRequest{
		KeyID:   req.KeyID,
		Context: req.Context,
	}

	resp, err := c.transport.Request(ctx, serviceName, "GenerateDataKey", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.GenerateDataKey", "nil response from server")
	}

	switch r := resp.(type) {
	case *GenerateDataKeyResponse:
		return r, nil
	case *service.GenerateDataKeyResponse:
		return &GenerateDataKeyResponse{
			KeyID:          r.KeyID,
			Plaintext:      r.Plaintext,
			CiphertextBlob: r.CiphertextBlob,
		}, nil
	default:
		return nil, errors.NewInternal("crypto.GenerateDataKey", "invalid response type")
	}
}

// GenerateWrappedDataKey generates an encrypted data key without plaintext.
func (c *client) GenerateWrappedDataKey(ctx context.Context, req *GenerateWrappedDataKeyRequest) (*GenerateWrappedDataKeyResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("crypto.GenerateWrappedDataKey", "request is required")
	}
	if req.KeyID == "" {
		return nil, errors.NewInvalidInput("crypto.GenerateWrappedDataKey", "key ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.GenerateWrappedDataKey", "context is required")
	}

	svcReq := &service.GenerateWrappedDataKeyRequest{
		KeyID:   req.KeyID,
		Context: req.Context,
	}

	resp, err := c.transport.Request(ctx, serviceName, "GenerateWrappedDataKey", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.GenerateWrappedDataKey", "nil response from server")
	}

	switch r := resp.(type) {
	case *GenerateWrappedDataKeyResponse:
		return r, nil
	case *service.GenerateWrappedDataKeyResponse:
		return &GenerateWrappedDataKeyResponse{
			KeyID:          r.KeyID,
			CiphertextBlob: r.CiphertextBlob,
		}, nil
	default:
		return nil, errors.NewInternal("crypto.GenerateWrappedDataKey", "invalid response type")
	}
}

// GenerateHMAC generates an HMAC of data using a named key.
func (c *client) GenerateHMAC(ctx context.Context, req *GenerateHMACRequest) (*GenerateHMACResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("crypto.GenerateHMAC", "request is required")
	}
	if req.KeyID == "" {
		return nil, errors.NewInvalidInput("crypto.GenerateHMAC", "key ID is required")
	}
	if len(req.Message) == 0 {
		return nil, errors.NewInvalidInput("crypto.GenerateHMAC", "message is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.GenerateHMAC", "context is required")
	}

	svcReq := &service.HMACRequest{
		KeyID:         req.KeyID,
		Message:       req.Message,
		HashAlgorithm: hashAlgToServiceHashAlg(req.HashAlg),
	}

	resp, err := c.transport.Request(ctx, serviceName, "GenerateHMAC", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.GenerateHMAC", "nil response from server")
	}

	switch r := resp.(type) {
	case *GenerateHMACResponse:
		return r, nil
	case *service.HMACResponse:
		return &GenerateHMACResponse{
			KeyID:      r.KeyID,
			KeyVersion: int(r.KeyVersion),
			HMAC:       r.HMAC,
			HashAlg:    serviceHashAlgToHashAlg(r.HashAlgorithm),
		}, nil
	default:
		return nil, errors.NewInternal("crypto.GenerateHMAC", "invalid response type")
	}
}

// VerifyHMAC verifies an HMAC against data using a named key.
func (c *client) VerifyHMAC(ctx context.Context, req *VerifyHMACRequest) (*VerifyHMACResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("crypto.VerifyHMAC", "request is required")
	}
	if req.KeyID == "" {
		return nil, errors.NewInvalidInput("crypto.VerifyHMAC", "key ID is required")
	}
	if len(req.Message) == 0 {
		return nil, errors.NewInvalidInput("crypto.VerifyHMAC", "message is required")
	}
	if len(req.HMAC) == 0 {
		return nil, errors.NewInvalidInput("crypto.VerifyHMAC", "HMAC is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.VerifyHMAC", "context is required")
	}

	svcReq := &service.VerifyHMACRequest{
		KeyID:         req.KeyID,
		Message:       req.Message,
		HMAC:          req.HMAC,
		HashAlgorithm: hashAlgToServiceHashAlg(req.HashAlg),
		KeyVersion:    uint64(req.KeyVersion),
	}

	resp, err := c.transport.Request(ctx, serviceName, "VerifyHMAC", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.VerifyHMAC", "nil response from server")
	}

	switch r := resp.(type) {
	case *VerifyHMACResponse:
		return r, nil
	case *service.VerifyHMACResponse:
		return &VerifyHMACResponse{
			Valid: r.Valid,
		}, nil
	default:
		return nil, errors.NewInternal("crypto.VerifyHMAC", "invalid response type")
	}
}

// GenerateRandom generates high-quality random bytes.
func (c *client) GenerateRandom(ctx context.Context, req *GenerateRandomRequest) (*GenerateRandomResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("crypto.GenerateRandom", "request is required")
	}
	if req.NumBytes <= 0 {
		return nil, errors.NewInvalidInput("crypto.GenerateRandom", "num bytes must be positive")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.GenerateRandom", "context is required")
	}

	svcReq := &service.GenerateRandomRequest{
		NumBytes: req.NumBytes,
	}

	resp, err := c.transport.Request(ctx, serviceName, "GenerateRandom", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.GenerateRandom", "nil response from server")
	}

	switch r := resp.(type) {
	case *GenerateRandomResponse:
		return r, nil
	case *service.GenerateRandomResponse:
		return &GenerateRandomResponse{
			RandomBytes: r.RandomBytes,
		}, nil
	default:
		return nil, errors.NewInternal("crypto.GenerateRandom", "invalid response type")
	}
}

// Hash computes a cryptographic hash of data.
func (c *client) Hash(ctx context.Context, req *HashRequest) (*HashResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("crypto.Hash", "request is required")
	}
	if len(req.Data) == 0 {
		return nil, errors.NewInvalidInput("crypto.Hash", "data is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("crypto.Hash", "context is required")
	}

	svcReq := &service.HashRequest{
		Data:      req.Data,
		Algorithm: hashAlgToServiceHashAlg(req.HashAlg),
	}

	resp, err := c.transport.Request(ctx, serviceName, "Hash", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("crypto.Hash", "nil response from server")
	}

	switch r := resp.(type) {
	case *HashResponse:
		return r, nil
	case *service.HashResponse:
		return &HashResponse{
			HashAlg: serviceHashAlgToHashAlg(r.Algorithm),
			Hash:    r.Hash,
		}, nil
	default:
		return nil, errors.NewInternal("crypto.Hash", "invalid response type")
	}
}

// =============================================================================
// Validation functions
// =============================================================================

func validateEncryptRequest(req *EncryptRequest) error {
	if req == nil {
		return errors.NewInvalidInput("crypto.Encrypt", "request is required")
	}
	if req.KeyID == "" {
		return errors.NewInvalidInput("crypto.Encrypt", "key ID is required")
	}
	if len(req.Plaintext) == 0 {
		return errors.NewInvalidInput("crypto.Encrypt", "plaintext is required")
	}
	return nil
}

func validateDecryptRequest(req *DecryptRequest) error {
	if req == nil {
		return errors.NewInvalidInput("crypto.Decrypt", "request is required")
	}
	if req.KeyID == "" {
		return errors.NewInvalidInput("crypto.Decrypt", "key ID is required")
	}
	if len(req.Ciphertext) == 0 {
		return errors.NewInvalidInput("crypto.Decrypt", "ciphertext is required")
	}
	return nil
}

func validateSignRequest(req *SignRequest) error {
	if req == nil {
		return errors.NewInvalidInput("crypto.Sign", "request is required")
	}
	if req.KeyID == "" {
		return errors.NewInvalidInput("crypto.Sign", "key ID is required")
	}
	if len(req.Data) == 0 {
		return errors.NewInvalidInput("crypto.Sign", "data is required")
	}

	switch req.HashAlg {
	case HashSHA256, HashSHA384, HashSHA512:
		// Valid hash algorithms
	default:
		return errors.NewInvalidInput("crypto.Sign", "invalid hash algorithm").
			WithContext("hash_algorithm", int(req.HashAlg))
	}

	return nil
}

func validateVerifyRequest(req *VerifyRequest) error {
	if req == nil {
		return errors.NewInvalidInput("crypto.Verify", "request is required")
	}
	if req.KeyID == "" {
		return errors.NewInvalidInput("crypto.Verify", "key ID is required")
	}
	if len(req.Data) == 0 {
		return errors.NewInvalidInput("crypto.Verify", "data is required")
	}
	if len(req.Signature) == 0 {
		return errors.NewInvalidInput("crypto.Verify", "signature is required")
	}

	switch req.HashAlg {
	case HashSHA256, HashSHA384, HashSHA512:
		// Valid hash algorithms
	default:
		return errors.NewInvalidInput("crypto.Verify", "invalid hash algorithm").
			WithContext("hash_algorithm", int(req.HashAlg))
	}

	return nil
}

// =============================================================================
// Conversion helpers
// =============================================================================

// hashAlgToServiceHashAlg converts SDK HashAlgorithm to service HashAlgorithm.
func hashAlgToServiceHashAlg(h HashAlgorithm) service.HashAlgorithm {
	switch h {
	case HashSHA256:
		return service.HashAlgSHA256
	case HashSHA384:
		return service.HashAlgSHA384
	case HashSHA512:
		return service.HashAlgSHA512
	default:
		return service.HashAlgSHA256
	}
}

// serviceHashAlgToHashAlg converts service HashAlgorithm to SDK HashAlgorithm.
func serviceHashAlgToHashAlg(h service.HashAlgorithm) HashAlgorithm {
	switch h {
	case service.HashAlgSHA256:
		return HashSHA256
	case service.HashAlgSHA384:
		return HashSHA384
	case service.HashAlgSHA512:
		return HashSHA512
	default:
		return HashSHA256
	}
}
