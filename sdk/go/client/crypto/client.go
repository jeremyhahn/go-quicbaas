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
}

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

// Validation functions

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
