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

	// ImportKey imports an externally generated key into the KMS.
	ImportKey(ctx context.Context, req *ImportKeyRequest) (*ImportKeyResponse, error)

	// ExportKey exports a key wrapped with a wrapping key.
	ExportKey(ctx context.Context, req *ExportKeyRequest) (*ExportKeyResponse, error)

	// RevokeKey marks a key as revoked and unusable.
	// RevokeKey marks a key as revoked and unusable. The reason is recorded
	// for audit; it may be empty.
	RevokeKey(ctx context.Context, keyID, reason string) error

	// ListKeyVersions returns all versions of a key.
	ListKeyVersions(ctx context.Context, keyID string) (*ListKeyVersionsResponse, error)

	// UpdateKeyStatus enables or disables a key or key version.
	UpdateKeyStatus(ctx context.Context, req *UpdateKeyStatusRequest) (*UpdateKeyStatusResponse, error)

	// GetKeyMetadata retrieves metadata for a key without exposing key material.
	GetKeyMetadata(ctx context.Context, keyID string) (*KeyMetadataResponse, error)

	// UpdateKeyMetadata updates mutable metadata fields for a key.
	UpdateKeyMetadata(ctx context.Context, req *UpdateKeyMetadataRequest) (*KeyMetadataResponse, error)

	// SetKeyPolicy sets the access control policy for a key.
	SetKeyPolicy(ctx context.Context, req *SetKeyPolicyRequest) error

	// GetKeyPolicy retrieves the access control policy for a key.
	GetKeyPolicy(ctx context.Context, keyID string) (*KeyPolicy, error)

	// SetKeyRotationPolicy sets or replaces the automatic rotation policy for a key.
	// When AutoRotate is true, IntervalSeconds must be positive.
	SetKeyRotationPolicy(ctx context.Context, req *SetKeyRotationPolicyRequest) (*KeyRotationPolicy, error)

	// GetKeyRotationPolicy retrieves the current automatic rotation policy for a key.
	// Returns (nil, nil) when no rotation policy has been configured.
	GetKeyRotationPolicy(ctx context.Context, keyID string) (*KeyRotationPolicy, error)

	// DeleteKeyRotationPolicy removes the rotation policy for a key, disabling auto-rotation.
	DeleteKeyRotationPolicy(ctx context.Context, keyID string) error

	// GetSupportedAlgorithms returns the list of supported algorithms.
	GetSupportedAlgorithms(ctx context.Context) (*SupportedAlgorithmsResponse, error)

	// BackendStatus returns health and capability information about the KMS backend.
	BackendStatus(ctx context.Context) (*BackendStatusResponse, error)

	// UnwrapKey imports a wrapped key into the KMS.
	UnwrapKey(ctx context.Context, req *UnwrapKeyRequest) (*UnwrapKeyResponse, error)
}

// GenerateKeyRequest is a request to generate a key.
type GenerateKeyRequest struct {
	Name      string
	Algorithm KeyAlgorithm
	Usage     []KeyUsage

	// Exportable marks the key material as exportable from the KMS.
	Exportable bool `json:"exportable,omitempty"`

	// DeletionProtection prevents accidental deletion of the key.
	DeletionProtection bool `json:"deletion_protection,omitempty"`

	// Description is a human-readable description.
	Description string `json:"description,omitempty"`

	// Tags are optional key-value metadata.
	Tags map[string]string `json:"tags,omitempty"`

	// Owner identifies the principal that owns the key.
	Owner string `json:"owner,omitempty"`
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

// KeyStatus represents the lifecycle state of a key.
type KeyStatus int

const (
	// KeyStatusEnabled indicates the key is active.
	KeyStatusEnabled KeyStatus = iota
	// KeyStatusDisabled indicates the key is disabled.
	KeyStatusDisabled
	// KeyStatusPendingDeletion indicates the key is pending deletion.
	KeyStatusPendingDeletion
	// KeyStatusDeleted indicates the key has been deleted.
	KeyStatusDeleted
	// KeyStatusRotating indicates the key is being rotated.
	KeyStatusRotating
	// KeyStatusCompromised indicates the key has been compromised.
	KeyStatusCompromised
)

// String returns the string representation of the key status.
func (s KeyStatus) String() string {
	switch s {
	case KeyStatusEnabled:
		return "ENABLED"
	case KeyStatusDisabled:
		return "DISABLED"
	case KeyStatusPendingDeletion:
		return "PENDING_DELETION"
	case KeyStatusDeleted:
		return "DELETED"
	case KeyStatusRotating:
		return "ROTATING"
	case KeyStatusCompromised:
		return "COMPROMISED"
	default:
		return "UNKNOWN"
	}
}

// ExportFormat specifies the format for exported key material.
type ExportFormat int

const (
	// ExportFormatRaw exports raw key bytes.
	ExportFormatRaw ExportFormat = iota
	// ExportFormatJWK exports as JSON Web Key.
	ExportFormatJWK
	// ExportFormatPEM exports as PEM.
	ExportFormatPEM
)

// String returns the string representation of the export format.
func (f ExportFormat) String() string {
	switch f {
	case ExportFormatRaw:
		return "RAW"
	case ExportFormatJWK:
		return "JWK"
	case ExportFormatPEM:
		return "PEM"
	default:
		return "UNKNOWN"
	}
}

// WrappingAlgorithm specifies the algorithm used for key wrapping.
type WrappingAlgorithm int

const (
	// WrapAlgAESKW is RFC 3394 AES Key Wrap.
	WrapAlgAESKW WrappingAlgorithm = iota
	// WrapAlgAESKWP is RFC 5649 AES Key Wrap with Padding.
	WrapAlgAESKWP
	// WrapAlgAESGCM is AES-GCM envelope encryption.
	WrapAlgAESGCM
	// WrapAlgRSAOAEPSHA256 is RSA-OAEP with SHA-256.
	WrapAlgRSAOAEPSHA256
	// WrapAlgRSAOAEPSHA384 is RSA-OAEP with SHA-384.
	WrapAlgRSAOAEPSHA384
	// WrapAlgRSAOAEPSHA512 is RSA-OAEP with SHA-512.
	WrapAlgRSAOAEPSHA512
	// WrapAlgECDHESA256KW is ECDH-ES+A256KW.
	WrapAlgECDHESA256KW
)

// GenerateKeyResponse is the response from key generation.
type GenerateKeyResponse struct {
	KeyID     string
	Version   int
	Algorithm KeyAlgorithm

	// Exportable reports whether the generated key material is exportable.
	Exportable bool `json:"exportable,omitempty"`

	// DeletionProtection reports whether the key is deletion-protected.
	DeletionProtection bool `json:"deletion_protection,omitempty"`
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

// ImportKeyRequest is a request to import a key into the KMS.
type ImportKeyRequest struct {
	// KeyMaterial is the raw key material to import.
	KeyMaterial []byte `json:"key_material,omitempty"`

	// WrappingKeyID is the key used to decrypt the material (if wrapped).
	WrappingKeyID string

	// Name is the display name for the imported key.
	Name string

	// Description is a human-readable description.
	Description string

	// Algorithm specifies the key algorithm.
	Algorithm KeyAlgorithm

	// Usage specifies permitted operations.
	Usage []KeyUsage

	// Tags are optional key-value metadata.
	Tags map[string]string
}

// ImportKeyResponse is the response from importing a key.
type ImportKeyResponse struct {
	KeyID     string
	Version   int
	Algorithm KeyAlgorithm
}

// ExportKeyRequest is a request to export a key.
type ExportKeyRequest struct {
	// KeyID is the key to export.
	KeyID string

	// WrappingKeyID is the key used to wrap the exported material.
	WrappingKeyID string

	// WrappingAlgorithm specifies the wrapping algorithm.
	WrappingAlgorithm WrappingAlgorithm

	// Format specifies the export format.
	Format ExportFormat

	// Version optionally specifies a specific key version (0 = latest).
	Version int
}

// ExportKeyResponse is the response from exporting a key.
type ExportKeyResponse struct {
	KeyID             string
	Format            ExportFormat
	WrappingKeyID     string
	WrappingAlgorithm WrappingAlgorithm
	WrappedMaterial   []byte
	Version           int
}

// KeyVersionInfo represents metadata for a specific key version.
type KeyVersionInfo struct {
	Version    int
	Status     KeyStatus
	Algorithm  KeyAlgorithm
	CreatedAt  time.Time
	DisabledAt time.Time
}

// ListKeyVersionsResponse is the response from listing key versions.
type ListKeyVersionsResponse struct {
	KeyID    string
	Versions []KeyVersionInfo
}

// UpdateKeyStatusRequest is a request to update a key's status.
type UpdateKeyStatusRequest struct {
	// KeyID is the key to update.
	KeyID string

	// Version optionally targets a specific version (0 = all versions).
	Version int

	// Status is the new status.
	Status KeyStatus
}

// UpdateKeyStatusResponse is the response from updating a key's status.
type UpdateKeyStatusResponse struct {
	KeyID   string
	Version int
	Status  KeyStatus
}

// KeyMetadataResponse contains non-sensitive metadata about a key.
type KeyMetadataResponse struct {
	KeyID              string
	Name               string
	Description        string
	Algorithm          KeyAlgorithm
	Status             KeyStatus
	Usage              []KeyUsage
	Version            int
	Exportable         bool
	DeletionProtection bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
	LastUsedAt         time.Time
	Tags               map[string]string
	Owner              string
}

// UpdateKeyMetadataRequest is a request to update key metadata.
type UpdateKeyMetadataRequest struct {
	// KeyID is the key to update.
	KeyID string

	// Name is the new display name (empty = no change).
	Name string

	// Description is the new description (empty = no change).
	Description string

	// Tags are tags to add or update.
	Tags map[string]string

	// RemoveTags are tag keys to remove.
	RemoveTags []string
}

// KeyPolicy defines access control and usage policies for a key.
type KeyPolicy struct {
	KeyID             string
	Version           int
	AllowedOperations []KeyUsage
	AllowedPrincipals []string
	DeniedPrincipals  []string
	ExpiresAt         time.Time
	RotationRequired  bool
	MaxUsageCount     uint64
	RequireMFA        bool
	UpdatedAt         time.Time
}

// SetKeyPolicyRequest is a request to set a key's access policy.
type SetKeyPolicyRequest struct {
	// KeyID is the key to set policy for.
	KeyID string

	// Policy is the policy to apply.
	Policy *KeyPolicy
}

// KeyRotationPolicy defines the automatic cryptoperiod rotation settings for a key.
type KeyRotationPolicy struct {
	// Enabled indicates if the policy is active.
	Enabled bool

	// AutoRotate enables fully automatic rotation without manual intervention.
	// When true, IntervalSeconds must be positive.
	AutoRotate bool

	// IntervalSeconds is the rotation frequency in seconds.
	IntervalSeconds int64

	// NextRotationAt is when the next rotation is scheduled.
	NextRotationAt time.Time

	// RetainVersions is how many old key versions to keep after rotation.
	RetainVersions int
}

// SetKeyRotationPolicyRequest is a request to set a key's automatic rotation policy.
type SetKeyRotationPolicyRequest struct {
	// KeyID is the key to configure rotation for.
	KeyID string

	// AutoRotate enables automatic rotation. Requires IntervalSeconds > 0.
	AutoRotate bool

	// IntervalSeconds is the rotation interval in seconds.
	IntervalSeconds int64

	// RetainVersions is how many old versions to keep.
	RetainVersions int

	// Enabled marks the policy as enabled.
	Enabled bool
}

// SupportedAlgorithmsResponse contains the list of supported algorithms.
type SupportedAlgorithmsResponse struct {
	Algorithms []string
}

// BackendStatusResponse contains health and capability information about the KMS backend.
type BackendStatusResponse struct {
	BackendType  string
	Status       string
	Device       string
	Region       string
	KeyID        string
	PCRBanks     []string
	PCRRegisters []int
	Capabilities map[string]bool
}

// UnwrapKeyRequest is a request to import a wrapped key.
type UnwrapKeyRequest struct {
	// KeyID is the target key ID for the imported key.
	KeyID string

	// WrappingKeyID is the key used to unwrap the material.
	WrappingKeyID string

	// WrappingAlgorithm specifies the wrapping algorithm.
	WrappingAlgorithm WrappingAlgorithm

	// WrappedMaterial is the wrapped key bytes.
	WrappedMaterial []byte

	// Algorithm is the algorithm of the unwrapped key.
	Algorithm KeyAlgorithm

	// Name is the display name for the imported key.
	Name string

	// Description is a human-readable description.
	Description string

	// SourceKeyID is the source key ID for AAD verification.
	SourceKeyID string

	// SourceVersion is the source key version for AAD verification.
	SourceVersion int
}

// UnwrapKeyResponse is the response from unwrapping a key.
type UnwrapKeyResponse struct {
	KeyID     string
	Version   int
	Algorithm KeyAlgorithm
}

// =============================================================================
// Internal request types (unexported, used for transport serialization)
// =============================================================================

// getKeyRequest is the internal get key request.
type getKeyRequest struct {
	KeyID string `json:"key_id"`
}

// getPublicKeyRequest is the internal get public key request.
type getPublicKeyRequest struct {
	KeyID string `json:"key_id"`
}

// listKeysRequest is the internal list keys request.
type listKeysRequest struct{}

// rotateKeyRequest is the internal rotate key request.
type rotateKeyRequest struct {
	KeyID string `json:"key_id"`
}

// deleteKeyRequest is the internal delete key request.
type deleteKeyRequest struct {
	KeyID string `json:"key_id"`
}

// deleteKeyResponse is the internal delete key response.
type deleteKeyResponse struct{}

// revokeKeyRequest is the internal revoke key request.
type revokeKeyRequest struct {
	KeyID string `json:"key_id"`

	// Reason records why the key was revoked; transports map it into
	// service.RevokeKeyRequest.Reason for the audit trail.
	Reason string `json:"reason,omitempty"`
}

// revokeKeyResponse is the internal revoke key response.
type revokeKeyResponse struct{}

// listKeyVersionsRequest is the internal list key versions request.
type listKeyVersionsRequest struct {
	KeyID string `json:"key_id"`
}

// getKeyMetadataRequest is the internal get key metadata request.
type getKeyMetadataRequest struct {
	KeyID string `json:"key_id"`
}

// getKeyPolicyRequest is the internal get key policy request.
type getKeyPolicyRequest struct {
	KeyID string `json:"key_id"`
}

// setKeyPolicyResponse is the internal set key policy response.
type setKeyPolicyResponse struct{}

// setKeyRotationPolicyRequest is the internal set rotation policy request.
type setKeyRotationPolicyRequest struct {
	KeyID           string `json:"key_id"`
	AutoRotate      bool   `json:"auto_rotate"`
	IntervalSeconds int64  `json:"interval_seconds"`
	RetainVersions  int    `json:"retain_versions,omitempty"`
	Enabled         bool   `json:"enabled"`
}

// getKeyRotationPolicyRequest is the internal get rotation policy request.
type getKeyRotationPolicyRequest struct {
	KeyID string `json:"key_id"`
}

// deleteKeyRotationPolicyRequest is the internal delete rotation policy request.
type deleteKeyRotationPolicyRequest struct {
	KeyID string `json:"key_id"`
}

// deleteKeyRotationPolicyResponse is the internal delete rotation policy response.
type deleteKeyRotationPolicyResponse struct{}

// getSupportedAlgorithmsRequest is the internal get supported algorithms request.
type getSupportedAlgorithmsRequest struct{}

// backendStatusRequest is the internal backend status request.
type backendStatusRequest struct{}

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
		if r.Metadata != nil {
			genResp.Exportable = r.Metadata.Exportable
			genResp.DeletionProtection = r.Metadata.DeletionProtection
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
		if r.Metadata == nil {
			return nil, errors.NewInternal("kms.GetKey", "response missing key metadata")
		}
		keyInfo := &KeyInfo{
			KeyID:     r.Metadata.KeyID,
			Name:      r.Metadata.Name,
			Algorithm: serviceAlgToSDKAlg(r.Metadata.Algorithm),
			Version:   int(r.Metadata.Version),
			CreatedAt: r.Metadata.CreatedAt,
			RotatedAt: r.Metadata.UpdatedAt,
			Usage:     make([]KeyUsage, len(r.Metadata.Usage)),
		}
		for i, u := range r.Metadata.Usage {
			keyInfo.Usage[i] = serviceUsageToSDKUsage(u)
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

// ImportKey imports an externally generated key into the KMS.
func (c *client) ImportKey(ctx context.Context, req *ImportKeyRequest) (*ImportKeyResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("kms.ImportKey", "request is required")
	}
	if len(req.KeyMaterial) == 0 {
		return nil, errors.NewInvalidInput("kms.ImportKey", "key material is required")
	}
	if req.Name == "" {
		return nil, errors.NewInvalidInput("kms.ImportKey", "key name is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.ImportKey", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ImportKey", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("kms.ImportKey", "nil response from server")
	}

	switch r := resp.(type) {
	case *ImportKeyResponse:
		return r, nil
	case *service.ImportKeyResponse:
		if r.Metadata == nil {
			return nil, errors.NewInternal("kms.ImportKey", "response carries no key metadata")
		}
		importResp := &ImportKeyResponse{
			KeyID: r.Metadata.KeyID,
		}
		if r.Key != nil {
			importResp.Version = int(r.Key.Version)
			importResp.Algorithm = serviceAlgToSDKAlg(r.Key.Algorithm)
		}
		return importResp, nil
	default:
		return nil, errors.NewInternal("kms.ImportKey", "invalid response type")
	}
}

// ExportKey exports a key wrapped with a wrapping key.
func (c *client) ExportKey(ctx context.Context, req *ExportKeyRequest) (*ExportKeyResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("kms.ExportKey", "request is required")
	}
	if req.KeyID == "" {
		return nil, errors.NewInvalidInput("kms.ExportKey", "key ID is required")
	}
	if req.WrappingKeyID == "" {
		return nil, errors.NewInvalidInput("kms.ExportKey", "wrapping key ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.ExportKey", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ExportKey", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("kms.ExportKey", "nil response from server")
	}

	switch r := resp.(type) {
	case *ExportKeyResponse:
		return r, nil
	case *service.ExportKeyResponse:
		return &ExportKeyResponse{
			KeyID:           r.KeyID,
			WrappingKeyID:   r.WrappingKeyID,
			WrappedMaterial: r.WrappedKey,
			Version:         int(r.Version),
		}, nil
	default:
		return nil, errors.NewInternal("kms.ExportKey", "invalid response type")
	}
}

// RevokeKey marks a key as revoked and unusable. The reason is recorded
// for audit; it may be empty.
func (c *client) RevokeKey(ctx context.Context, keyID, reason string) error {
	if keyID == "" {
		return errors.NewInvalidInput("kms.RevokeKey", "key ID is required")
	}
	if ctx == nil {
		return errors.NewInvalidInput("kms.RevokeKey", "context is required")
	}

	req := &revokeKeyRequest{
		KeyID:  keyID,
		Reason: reason,
	}

	resp, err := c.transport.Request(ctx, serviceName, "RevokeKey", req)
	if err != nil {
		return err
	}

	if resp == nil {
		return nil
	}

	switch resp.(type) {
	case *revokeKeyResponse:
		return nil
	case *service.RevokeKeyResponse:
		return nil
	default:
		return errors.NewInternal("kms.RevokeKey", "invalid response type")
	}
}

// ListKeyVersions returns all versions of a key.
func (c *client) ListKeyVersions(ctx context.Context, keyID string) (*ListKeyVersionsResponse, error) {
	if keyID == "" {
		return nil, errors.NewInvalidInput("kms.ListKeyVersions", "key ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.ListKeyVersions", "context is required")
	}

	req := &listKeyVersionsRequest{
		KeyID: keyID,
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListKeyVersions", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListKeyVersionsResponse{KeyID: keyID, Versions: []KeyVersionInfo{}}, nil
	}

	switch r := resp.(type) {
	case *ListKeyVersionsResponse:
		return r, nil
	case *service.ListKeyVersionsResponse:
		versions := make([]KeyVersionInfo, len(r.Versions))
		for i, v := range r.Versions {
			versions[i] = KeyVersionInfo{
				Version:   int(v.Version),
				Status:    serviceStatusToSDKStatus(v.Status),
				Algorithm: serviceAlgToSDKAlg(v.Algorithm),
				CreatedAt: v.CreatedAt,
			}
		}
		return &ListKeyVersionsResponse{
			KeyID:    r.KeyID,
			Versions: versions,
		}, nil
	default:
		return nil, errors.NewInternal("kms.ListKeyVersions", "invalid response type")
	}
}

// UpdateKeyStatus enables or disables a key or key version.
func (c *client) UpdateKeyStatus(ctx context.Context, req *UpdateKeyStatusRequest) (*UpdateKeyStatusResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("kms.UpdateKeyStatus", "request is required")
	}
	if req.KeyID == "" {
		return nil, errors.NewInvalidInput("kms.UpdateKeyStatus", "key ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.UpdateKeyStatus", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "UpdateKeyStatus", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("kms.UpdateKeyStatus", "nil response from server")
	}

	switch r := resp.(type) {
	case *UpdateKeyStatusResponse:
		return r, nil
	case *service.UpdateKeyStatusResponse:
		return &UpdateKeyStatusResponse{
			KeyID:   r.KeyID,
			Version: int(r.Version),
			Status:  serviceStatusToSDKStatus(r.NewStatus),
		}, nil
	default:
		return nil, errors.NewInternal("kms.UpdateKeyStatus", "invalid response type")
	}
}

// GetKeyMetadata retrieves metadata for a key without exposing key material.
func (c *client) GetKeyMetadata(ctx context.Context, keyID string) (*KeyMetadataResponse, error) {
	if keyID == "" {
		return nil, errors.NewInvalidInput("kms.GetKeyMetadata", "key ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.GetKeyMetadata", "context is required")
	}

	req := &getKeyMetadataRequest{
		KeyID: keyID,
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetKeyMetadata", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("kms.GetKeyMetadata", "key metadata not found")
	}

	switch r := resp.(type) {
	case *KeyMetadataResponse:
		return r, nil
	case *service.GetKeyMetadataResponse:
		return serviceMetadataToSDK(r.Metadata), nil
	default:
		return nil, errors.NewInternal("kms.GetKeyMetadata", "invalid response type")
	}
}

// UpdateKeyMetadata updates mutable metadata fields for a key.
func (c *client) UpdateKeyMetadata(ctx context.Context, req *UpdateKeyMetadataRequest) (*KeyMetadataResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("kms.UpdateKeyMetadata", "request is required")
	}
	if req.KeyID == "" {
		return nil, errors.NewInvalidInput("kms.UpdateKeyMetadata", "key ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.UpdateKeyMetadata", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "UpdateKeyMetadata", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("kms.UpdateKeyMetadata", "nil response from server")
	}

	switch r := resp.(type) {
	case *KeyMetadataResponse:
		return r, nil
	case *service.UpdateKeyMetadataResponse:
		return serviceMetadataToSDK(r.Metadata), nil
	default:
		return nil, errors.NewInternal("kms.UpdateKeyMetadata", "invalid response type")
	}
}

// SetKeyPolicy sets the access control policy for a key.
func (c *client) SetKeyPolicy(ctx context.Context, req *SetKeyPolicyRequest) error {
	if req == nil {
		return errors.NewInvalidInput("kms.SetKeyPolicy", "request is required")
	}
	if req.KeyID == "" {
		return errors.NewInvalidInput("kms.SetKeyPolicy", "key ID is required")
	}
	if req.Policy == nil {
		return errors.NewInvalidInput("kms.SetKeyPolicy", "policy is required")
	}
	if ctx == nil {
		return errors.NewInvalidInput("kms.SetKeyPolicy", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SetKeyPolicy", req)
	if err != nil {
		return err
	}

	if resp == nil {
		return nil
	}

	switch resp.(type) {
	case *setKeyPolicyResponse:
		return nil
	case *service.SetKeyPolicyResponse:
		return nil
	default:
		return errors.NewInternal("kms.SetKeyPolicy", "invalid response type")
	}
}

// GetKeyPolicy retrieves the access control policy for a key.
func (c *client) GetKeyPolicy(ctx context.Context, keyID string) (*KeyPolicy, error) {
	if keyID == "" {
		return nil, errors.NewInvalidInput("kms.GetKeyPolicy", "key ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.GetKeyPolicy", "context is required")
	}

	req := &getKeyPolicyRequest{
		KeyID: keyID,
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetKeyPolicy", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("kms.GetKeyPolicy", "key policy not found")
	}

	switch r := resp.(type) {
	case *KeyPolicy:
		return r, nil
	case *service.GetKeyPolicyResponse:
		return servicePolicyToSDK(r.Policy), nil
	default:
		return nil, errors.NewInternal("kms.GetKeyPolicy", "invalid response type")
	}
}

// GetSupportedAlgorithms returns the list of supported algorithms.
func (c *client) GetSupportedAlgorithms(ctx context.Context) (*SupportedAlgorithmsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.GetSupportedAlgorithms", "context is required")
	}

	req := &getSupportedAlgorithmsRequest{}

	resp, err := c.transport.Request(ctx, serviceName, "GetSupportedAlgorithms", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &SupportedAlgorithmsResponse{Algorithms: []string{}}, nil
	}

	switch r := resp.(type) {
	case *SupportedAlgorithmsResponse:
		return r, nil
	case []string:
		return &SupportedAlgorithmsResponse{
			Algorithms: r,
		}, nil
	default:
		return nil, errors.NewInternal("kms.GetSupportedAlgorithms", "invalid response type")
	}
}

// BackendStatus returns health and capability information about the KMS backend.
func (c *client) BackendStatus(ctx context.Context) (*BackendStatusResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.BackendStatus", "context is required")
	}

	req := &backendStatusRequest{}

	resp, err := c.transport.Request(ctx, serviceName, "BackendStatus", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("kms.BackendStatus", "nil response from server")
	}

	switch r := resp.(type) {
	case *BackendStatusResponse:
		return r, nil
	case *service.BackendStatusResponse:
		pcrRegisters := make([]int, len(r.PCRRegisters))
		for i, v := range r.PCRRegisters {
			pcrRegisters[i] = int(v)
		}
		return &BackendStatusResponse{
			BackendType:  r.BackendType,
			Status:       r.Status,
			Device:       r.Device,
			Region:       r.Region,
			KeyID:        r.KeyID,
			PCRBanks:     r.PCRBanks,
			PCRRegisters: pcrRegisters,
			Capabilities: r.Capabilities,
		}, nil
	default:
		return nil, errors.NewInternal("kms.BackendStatus", "invalid response type")
	}
}

// UnwrapKey imports a wrapped key into the KMS.
func (c *client) UnwrapKey(ctx context.Context, req *UnwrapKeyRequest) (*UnwrapKeyResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("kms.UnwrapKey", "request is required")
	}
	if req.WrappingKeyID == "" {
		return nil, errors.NewInvalidInput("kms.UnwrapKey", "wrapping key ID is required")
	}
	if len(req.WrappedMaterial) == 0 {
		return nil, errors.NewInvalidInput("kms.UnwrapKey", "wrapped material is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.UnwrapKey", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "UnwrapKey", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("kms.UnwrapKey", "nil response from server")
	}

	switch r := resp.(type) {
	case *UnwrapKeyResponse:
		return r, nil
	case *service.UnwrapKeyResponse:
		return &UnwrapKeyResponse{
			KeyID:     r.KeyID,
			Version:   int(r.Version),
			Algorithm: serviceAlgToSDKAlg(r.Algorithm),
		}, nil
	default:
		return nil, errors.NewInternal("kms.UnwrapKey", "invalid response type")
	}
}

// SetKeyRotationPolicy sets or replaces the automatic rotation policy for a key.
func (c *client) SetKeyRotationPolicy(ctx context.Context, req *SetKeyRotationPolicyRequest) (*KeyRotationPolicy, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("kms.SetKeyRotationPolicy", "request is required")
	}
	if req.KeyID == "" {
		return nil, errors.NewInvalidInput("kms.SetKeyRotationPolicy", "key ID is required")
	}
	if req.AutoRotate && req.IntervalSeconds <= 0 {
		return nil, errors.NewInvalidInput("kms.SetKeyRotationPolicy", "interval_seconds must be positive when auto_rotate is true")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.SetKeyRotationPolicy", "context is required")
	}

	internalReq := &setKeyRotationPolicyRequest{
		KeyID:           req.KeyID,
		AutoRotate:      req.AutoRotate,
		IntervalSeconds: req.IntervalSeconds,
		RetainVersions:  req.RetainVersions,
		Enabled:         req.Enabled,
	}

	resp, err := c.transport.Request(ctx, serviceName, "SetKeyRotationPolicy", internalReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("kms.SetKeyRotationPolicy", "nil response from server")
	}

	switch r := resp.(type) {
	case *KeyRotationPolicy:
		return r, nil
	case *service.SetKeyRotationPolicyResponse:
		return serviceRotationPolicyToSDK(r.Policy), nil
	default:
		return nil, errors.NewInternal("kms.SetKeyRotationPolicy", "invalid response type")
	}
}

// GetKeyRotationPolicy retrieves the current automatic rotation policy for a key.
func (c *client) GetKeyRotationPolicy(ctx context.Context, keyID string) (*KeyRotationPolicy, error) {
	if keyID == "" {
		return nil, errors.NewInvalidInput("kms.GetKeyRotationPolicy", "key ID is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("kms.GetKeyRotationPolicy", "context is required")
	}

	req := &getKeyRotationPolicyRequest{KeyID: keyID}

	resp, err := c.transport.Request(ctx, serviceName, "GetKeyRotationPolicy", req)
	if err != nil {
		return nil, err
	}

	// Nil response or nil policy means no policy is configured — return nil without error.
	if resp == nil {
		return nil, nil
	}

	switch r := resp.(type) {
	case *KeyRotationPolicy:
		return r, nil
	case *service.GetKeyRotationPolicyResponse:
		return serviceRotationPolicyToSDK(r.Policy), nil
	default:
		return nil, errors.NewInternal("kms.GetKeyRotationPolicy", "invalid response type")
	}
}

// DeleteKeyRotationPolicy removes the rotation policy for a key.
func (c *client) DeleteKeyRotationPolicy(ctx context.Context, keyID string) error {
	if keyID == "" {
		return errors.NewInvalidInput("kms.DeleteKeyRotationPolicy", "key ID is required")
	}
	if ctx == nil {
		return errors.NewInvalidInput("kms.DeleteKeyRotationPolicy", "context is required")
	}

	req := &deleteKeyRotationPolicyRequest{KeyID: keyID}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteKeyRotationPolicy", req)
	if err != nil {
		return err
	}

	if resp == nil {
		return nil
	}

	switch resp.(type) {
	case *deleteKeyRotationPolicyResponse:
		return nil
	case *service.DeleteKeyRotationPolicyResponse:
		return nil
	default:
		return errors.NewInternal("kms.DeleteKeyRotationPolicy", "invalid response type")
	}
}

// serviceRotationPolicyToSDK converts a service RotationPolicy to the SDK type.
func serviceRotationPolicyToSDK(p *service.RotationPolicy) *KeyRotationPolicy {
	if p == nil {
		return nil
	}
	return &KeyRotationPolicy{
		Enabled:         p.Enabled,
		AutoRotate:      p.AutoRotate,
		IntervalSeconds: int64(p.Interval / time.Second),
		NextRotationAt:  p.NextRotation,
		RetainVersions:  p.RetainVersions,
	}
}

// =============================================================================
// Validation functions
// =============================================================================

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

// =============================================================================
// Conversion helpers
// =============================================================================

// serviceMetadataToSDK converts service KeyMetadata to SDK KeyMetadataResponse.
func serviceMetadataToSDK(m *service.KeyMetadata) *KeyMetadataResponse {
	if m == nil {
		return nil
	}
	resp := &KeyMetadataResponse{
		KeyID:              m.KeyID,
		Name:               m.Name,
		Description:        m.Description,
		Algorithm:          serviceAlgToSDKAlg(m.Algorithm),
		Status:             serviceStatusToSDKStatus(m.Status),
		Version:            int(m.Version),
		Exportable:         m.Exportable,
		DeletionProtection: m.DeletionProtection,
		CreatedAt:          m.CreatedAt,
		UpdatedAt:          m.UpdatedAt,
		Tags:               m.Tags,
		Owner:              m.Owner,
	}
	resp.Usage = make([]KeyUsage, len(m.Usage))
	for i, u := range m.Usage {
		resp.Usage[i] = serviceUsageToSDKUsage(u)
	}
	return resp
}

// servicePolicyToSDK converts service KeyPolicy to SDK KeyPolicy.
func servicePolicyToSDK(p *service.KeyPolicy) *KeyPolicy {
	if p == nil {
		return nil
	}
	policy := &KeyPolicy{
		KeyID:             p.KeyID,
		Version:           int(p.Version),
		AllowedPrincipals: p.AllowedPrincipals,
		DeniedPrincipals:  p.DeniedPrincipals,
		RotationRequired:  p.RotationRequired,
		MaxUsageCount:     derefUint64(p.MaxUsageCount),
		RequireMFA:        p.RequireMFA,
		UpdatedAt:         p.UpdatedAt,
	}
	policy.AllowedOperations = make([]KeyUsage, len(p.AllowedOperations))
	for i, u := range p.AllowedOperations {
		policy.AllowedOperations[i] = serviceUsageToSDKUsage(u)
	}
	return policy
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

// serviceStatusToSDKStatus converts service KeyStatus to SDK KeyStatus.
func serviceStatusToSDKStatus(s service.KeyStatus) KeyStatus {
	switch s {
	case service.KeyStatusEnabled:
		return KeyStatusEnabled
	case service.KeyStatusDisabled:
		return KeyStatusDisabled
	case service.KeyStatusPendingDeletion:
		return KeyStatusPendingDeletion
	case service.KeyStatusDeleted:
		return KeyStatusDeleted
	case service.KeyStatusRotating:
		return KeyStatusRotating
	case service.KeyStatusCompromised:
		return KeyStatusCompromised
	default:
		return KeyStatusEnabled
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

// derefUint64 safely dereferences a *uint64, returning 0 if nil.
func derefUint64(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}
