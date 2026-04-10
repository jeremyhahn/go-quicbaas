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

package kms

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.KMS"
)

// Client provides key management operations.
type Client interface {
	// GenerateKey generates a new cryptographic key.
	GenerateKey(ctx context.Context, req *GenerateKeyRequest) (*GenerateKeyResponse, error)

	// GetKey retrieves information about a key.
	GetKey(ctx context.Context, keyID string) (*KeyInfo, error)

	// GetPublicKey retrieves the public key for an asymmetric key.
	GetPublicKey(ctx context.Context, keyID string) (*GetPublicKeyResponse, error)

	// ListKeys lists all keys.
	ListKeys(ctx context.Context) (*ListKeysResponse, error)

	// RotateKey rotates a key to a new version.
	RotateKey(ctx context.Context, keyID string) (*RotateKeyResponse, error)

	// DeleteKey deletes a key.
	DeleteKey(ctx context.Context, keyID string) error
}

// GenerateKeyRequest is a request to generate a key.
type GenerateKeyRequest struct {
	Name      string
	Algorithm KeyAlgorithm
	Usage     []KeyUsage
}

// KeyAlgorithm represents the key algorithm.
type KeyAlgorithm int

const (
	// AlgorithmAES256GCM is AES-256-GCM symmetric encryption.
	AlgorithmAES256GCM KeyAlgorithm = iota
	// AlgorithmChaCha20Poly1305 is ChaCha20-Poly1305 symmetric encryption.
	AlgorithmChaCha20Poly1305
	// AlgorithmRSA2048 is RSA with 2048-bit key.
	AlgorithmRSA2048
	// AlgorithmRSA4096 is RSA with 4096-bit key.
	AlgorithmRSA4096
	// AlgorithmECDSAP256 is ECDSA with P-256 curve.
	AlgorithmECDSAP256
	// AlgorithmECDSAP384 is ECDSA with P-384 curve.
	AlgorithmECDSAP384
	// AlgorithmED25519 is Ed25519 signature algorithm.
	AlgorithmED25519
)

// String returns the string representation of the algorithm.
func (a KeyAlgorithm) String() string {
	switch a {
	case AlgorithmAES256GCM:
		return "AES-256-GCM"
	case AlgorithmChaCha20Poly1305:
		return "CHACHA20-POLY1305"
	case AlgorithmRSA2048:
		return "RSA-2048"
	case AlgorithmRSA4096:
		return "RSA-4096"
	case AlgorithmECDSAP256:
		return "ECDSA-P256"
	case AlgorithmECDSAP384:
		return "ECDSA-P384"
	case AlgorithmED25519:
		return "ED25519"
	default:
		return "UNKNOWN"
	}
}

// KeyUsage represents how a key can be used.
type KeyUsage int

const (
	// KeyUsageEncrypt allows encryption operations.
	KeyUsageEncrypt KeyUsage = iota
	// KeyUsageDecrypt allows decryption operations.
	KeyUsageDecrypt
	// KeyUsageSign allows signing operations.
	KeyUsageSign
	// KeyUsageVerify allows verification operations.
	KeyUsageVerify
	// KeyUsageWrap allows key wrapping operations.
	KeyUsageWrap
	// KeyUsageUnwrap allows key unwrapping operations.
	KeyUsageUnwrap
)

// String returns the string representation of the key usage.
func (u KeyUsage) String() string {
	switch u {
	case KeyUsageEncrypt:
		return "ENCRYPT"
	case KeyUsageDecrypt:
		return "DECRYPT"
	case KeyUsageSign:
		return "SIGN"
	case KeyUsageVerify:
		return "VERIFY"
	case KeyUsageWrap:
		return "WRAP"
	case KeyUsageUnwrap:
		return "UNWRAP"
	default:
		return "UNKNOWN"
	}
}

// GenerateKeyResponse is the response from key generation.
type GenerateKeyResponse struct {
	KeyID     string
	Version   int
	Algorithm KeyAlgorithm
}

// KeyInfo contains information about a key.
type KeyInfo struct {
	KeyID     string
	Name      string
	Algorithm KeyAlgorithm
	Version   int
	Usage     []KeyUsage
	CreatedAt time.Time
	RotatedAt time.Time
}

// ListKeysResponse is the response from listing keys.
type ListKeysResponse struct {
	Keys []KeyInfo
}

// RotateKeyResponse is the response from key rotation.
type RotateKeyResponse struct {
	KeyID      string
	NewVersion int
}

// GetPublicKeyResponse is the response from getting a public key.
type GetPublicKeyResponse struct {
	KeyID     string
	PublicKey []byte
	Algorithm KeyAlgorithm
	Version   int
}

// getKeyRequest is the internal get key request.
type getKeyRequest struct {
	KeyID string
}

// getPublicKeyRequest is the internal get public key request.
type getPublicKeyRequest struct {
	KeyID string
}

// listKeysRequest is the internal list keys request.
type listKeysRequest struct{}

// rotateKeyRequest is the internal rotate key request.
type rotateKeyRequest struct {
	KeyID string
}

// deleteKeyRequest is the internal delete key request.
type deleteKeyRequest struct {
	KeyID string
}

// deleteKeyResponse is the internal delete key response.
type deleteKeyResponse struct{}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new KMS client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// GenerateKey generates a new cryptographic key.
func (c *client) GenerateKey(ctx context.Context, req *GenerateKeyRequest) (*GenerateKeyResponse, error) {
	if err := validateGenerateKeyRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.GenerateKey", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GenerateKey", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("kms.GenerateKey", "nil response from server")
	}

	// Handle both SDK and service response types
	switch r := resp.(type) {
	case *GenerateKeyResponse:
		return r, nil
	case *service.GenerateKeyResponse:
		genResp := &GenerateKeyResponse{
			KeyID: r.Metadata.KeyID,
		}
		if r.Key != nil {
			genResp.Version = int(r.Key.Version)
			genResp.Algorithm = serviceAlgToSDKAlg(r.Key.Algorithm)
		}
		return genResp, nil
	default:
		return nil, errors.NewInternal("kms.GenerateKey", "invalid response type")
	}
}

// GetKey retrieves information about a key.
func (c *client) GetKey(ctx context.Context, keyID string) (*KeyInfo, error) {
	if keyID == "" {
		return nil, errors.NewInvalidInput("kms.GetKey", "key ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.GetKey", "context is required")
	}

	req := &getKeyRequest{
		KeyID: keyID,
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetKey", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("kms.GetKey", "key not found")
	}

	// Handle both SDK and service response types
	switch r := resp.(type) {
	case *KeyInfo:
		return r, nil
	case *service.GetKeyResponse:
		keyInfo := &KeyInfo{
			KeyID: r.Metadata.KeyID,
		}
		if r.Metadata != nil {
			keyInfo.Name = r.Metadata.Name
			keyInfo.Algorithm = serviceAlgToSDKAlg(r.Metadata.Algorithm)
			keyInfo.Version = int(r.Metadata.Version)
			keyInfo.CreatedAt = r.Metadata.CreatedAt
			keyInfo.RotatedAt = r.Metadata.UpdatedAt
			keyInfo.Usage = make([]KeyUsage, len(r.Metadata.Usage))
			for i, u := range r.Metadata.Usage {
				keyInfo.Usage[i] = serviceUsageToSDKUsage(u)
			}
		}
		return keyInfo, nil
	default:
		return nil, errors.NewInternal("kms.GetKey", "invalid response type")
	}
}

// GetPublicKey retrieves the public key for an asymmetric key.
func (c *client) GetPublicKey(ctx context.Context, keyID string) (*GetPublicKeyResponse, error) {
	if keyID == "" {
		return nil, errors.NewInvalidInput("kms.GetPublicKey", "key ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.GetPublicKey", "context is required")
	}

	req := &getPublicKeyRequest{
		KeyID: keyID,
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetPublicKey", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("kms.GetPublicKey", "public key not found")
	}

	// Handle both SDK and service response types
	switch r := resp.(type) {
	case *GetPublicKeyResponse:
		return r, nil
	case *service.GetPublicKeyResponse:
		return &GetPublicKeyResponse{
			KeyID:     r.KeyID,
			PublicKey: r.PublicKey,
			Algorithm: serviceAlgToSDKAlg(r.Algorithm),
			Version:   int(r.Version),
		}, nil
	default:
		return nil, errors.NewInternal("kms.GetPublicKey", "invalid response type")
	}
}

// ListKeys lists all keys.
func (c *client) ListKeys(ctx context.Context) (*ListKeysResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.ListKeys", "context is required")
	}

	req := &listKeysRequest{}

	resp, err := c.transport.Request(ctx, serviceName, "ListKeys", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListKeysResponse{Keys: []KeyInfo{}}, nil
	}

	// Handle both SDK and service response types
	switch r := resp.(type) {
	case *ListKeysResponse:
		return r, nil
	case *service.ListKeysResponse:
		keys := make([]KeyInfo, len(r.Keys))
		for i, k := range r.Keys {
			keys[i] = KeyInfo{
				KeyID:     k.KeyID,
				Algorithm: serviceAlgToSDKAlg(k.Algorithm),
				Version:   int(k.Version),
				CreatedAt: k.CreatedAt,
			}
		}
		return &ListKeysResponse{Keys: keys}, nil
	default:
		return nil, errors.NewInternal("kms.ListKeys", "invalid response type")
	}
}

// RotateKey rotates a key to a new version.
func (c *client) RotateKey(ctx context.Context, keyID string) (*RotateKeyResponse, error) {
	if keyID == "" {
		return nil, errors.NewInvalidInput("kms.RotateKey", "key ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.RotateKey", "context is required")
	}

	req := &rotateKeyRequest{
		KeyID: keyID,
	}

	resp, err := c.transport.Request(ctx, serviceName, "RotateKey", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("kms.RotateKey", "nil response from server")
	}

	// Handle both SDK and service response types
	switch r := resp.(type) {
	case *RotateKeyResponse:
		return r, nil
	case *service.RotateKeyResponse:
		return &RotateKeyResponse{
			KeyID:      r.KeyID,
			NewVersion: int(r.NewVersion),
		}, nil
	default:
		return nil, errors.NewInternal("kms.RotateKey", "invalid response type")
	}
}

// DeleteKey deletes a key.
func (c *client) DeleteKey(ctx context.Context, keyID string) error {
	if keyID == "" {
		return errors.NewInvalidInput("kms.DeleteKey", "key ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("kms.DeleteKey", "context is required")
	}

	req := &deleteKeyRequest{
		KeyID: keyID,
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteKey", req)
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	// Handle both SDK and service response types
	switch resp.(type) {
	case *deleteKeyResponse:
		return nil
	case *service.DeleteKeyResponse:
		return nil
	default:
		return errors.NewInternal("kms.DeleteKey", "invalid response type")
	}
}

// Validation functions

func validateGenerateKeyRequest(req *GenerateKeyRequest) error {
	if req == nil {
		return errors.NewInvalidInput("kms.GenerateKey", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("kms.GenerateKey", "key name is required")
	}

	switch req.Algorithm {
	case AlgorithmAES256GCM, AlgorithmChaCha20Poly1305,
		AlgorithmRSA2048, AlgorithmRSA4096,
		AlgorithmECDSAP256, AlgorithmECDSAP384, AlgorithmED25519:
		// Valid algorithms
	default:
		return errors.NewInvalidInput("kms.GenerateKey", "invalid algorithm").
			WithContext("algorithm", int(req.Algorithm))
	}

	for i, usage := range req.Usage {
		switch usage {
		case KeyUsageEncrypt, KeyUsageDecrypt, KeyUsageSign,
			KeyUsageVerify, KeyUsageWrap, KeyUsageUnwrap:
			// Valid usage
		default:
			return errors.NewInvalidInput("kms.GenerateKey", "invalid key usage").
				WithContext("usage_index", i).
				WithContext("usage", int(usage))
		}
	}

	return nil
}

// sdkAlgToServiceAlg converts SDK KeyAlgorithm to service KeyAlgorithm.
func sdkAlgToServiceAlg(alg KeyAlgorithm) service.KeyAlgorithm {
	switch alg {
	case AlgorithmAES256GCM:
		return service.KeyAlgAES256GCM
	case AlgorithmChaCha20Poly1305:
		return service.KeyAlgChaCha20Poly1305
	case AlgorithmRSA2048:
		return service.KeyAlgRSA2048
	case AlgorithmRSA4096:
		return service.KeyAlgRSA4096
	case AlgorithmECDSAP256:
		return service.KeyAlgECDSAP256
	case AlgorithmECDSAP384:
		return service.KeyAlgECDSAP384
	case AlgorithmED25519:
		return service.KeyAlgEd25519
	default:
		return service.KeyAlgAES256GCM
	}
}

// serviceAlgToSDKAlg converts service KeyAlgorithm to SDK KeyAlgorithm.
func serviceAlgToSDKAlg(alg service.KeyAlgorithm) KeyAlgorithm {
	switch alg {
	case service.KeyAlgAES256GCM:
		return AlgorithmAES256GCM
	case service.KeyAlgChaCha20Poly1305:
		return AlgorithmChaCha20Poly1305
	case service.KeyAlgRSA2048:
		return AlgorithmRSA2048
	case service.KeyAlgRSA4096:
		return AlgorithmRSA4096
	case service.KeyAlgECDSAP256:
		return AlgorithmECDSAP256
	case service.KeyAlgECDSAP384:
		return AlgorithmECDSAP384
	case service.KeyAlgEd25519:
		return AlgorithmED25519
	default:
		return AlgorithmAES256GCM
	}
}

// sdkUsageToServiceUsage converts SDK KeyUsage to service KeyUsage.
func sdkUsageToServiceUsage(usage KeyUsage) service.KeyUsage {
	switch usage {
	case KeyUsageEncrypt:
		return service.KeyUsageEncrypt
	case KeyUsageDecrypt:
		return service.KeyUsageDecrypt
	case KeyUsageSign:
		return service.KeyUsageSign
	case KeyUsageVerify:
		return service.KeyUsageVerify
	case KeyUsageWrap:
		return service.KeyUsageWrapKey
	case KeyUsageUnwrap:
		return service.KeyUsageWrapKey
	default:
		return service.KeyUsageEncrypt
	}
}

// serviceUsageToSDKUsage converts service KeyUsage to SDK KeyUsage.
func serviceUsageToSDKUsage(usage service.KeyUsage) KeyUsage {
	switch usage {
	case service.KeyUsageEncrypt:
		return KeyUsageEncrypt
	case service.KeyUsageDecrypt:
		return KeyUsageDecrypt
	case service.KeyUsageSign:
		return KeyUsageSign
	case service.KeyUsageVerify:
		return KeyUsageVerify
	case service.KeyUsageWrapKey:
		return KeyUsageWrap
	default:
		return KeyUsageEncrypt
	}
}
