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

package catalog

import (
	"context"
	"errors"
	"time"

	xkms "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// KMSDEKProviderError represents an error from the KMS DEK provider.
type KMSDEKProviderError struct {
	Op      string
	KeyPath string
	Err     error
}

// Error implements the error interface.
func (e *KMSDEKProviderError) Error() string {
	if e.KeyPath != "" {
		return "kms dek provider: " + e.Op + " key=" + e.KeyPath + ": " + e.Err.Error()
	}
	return "kms dek provider: " + e.Op + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *KMSDEKProviderError) Unwrap() error {
	return e.Err
}

// Default DEK size in bytes (256-bit AES key).
const defaultDEKSizeBytes = 32

// Sentinel errors for KMSDEKProvider.
var (
	// ErrKMSManagerNil indicates the KMS manager is nil.
	ErrKMSManagerNil = errors.New("KMS manager cannot be nil")

	// ErrKeyPathEmpty indicates an empty key path was provided.
	ErrKeyPathEmpty = errors.New("key path cannot be empty")

	// ErrKeyMetadataNil indicates the key metadata from KMS was nil.
	ErrKeyMetadataNil = errors.New("key metadata from KMS is nil")

	// ErrGenerateDataKeyFailed indicates GenerateDataKey returned empty key material.
	ErrGenerateDataKeyFailed = errors.New("GenerateDataKey returned empty key material")

	// ErrUnwrapDEKFailed indicates the wrapped DEK could not be decrypted.
	ErrUnwrapDEKFailed = errors.New("failed to unwrap DEK")
)

// KMSDEKProvider implements DEKProvider by wrapping a KMSManager.
// It provides Data Encryption Keys for object encryption by delegating
// to the underlying KMS for key management operations.
type KMSDEKProvider struct {
	kms    interfaces.KMSManager
	logger interfaces.Logger
}

// KMSDEKProviderConfig contains configuration for creating a KMSDEKProvider.
type KMSDEKProviderConfig struct {
	// KMSManager is the KMS manager to delegate to.
	KMSManager interfaces.KMSManager

	// Logger is an optional logger for debugging.
	Logger interfaces.Logger
}

// NewKMSDEKProvider creates a new KMSDEKProvider that wraps the given KMS manager.
// The provider translates DEK requests to KMS operations, allowing the encryption
// manager to work with any KMS backend (software, Vault, HSM, etc.).
func NewKMSDEKProvider(cfg *KMSDEKProviderConfig) (*KMSDEKProvider, error) {
	if cfg == nil {
		return nil, &KMSDEKProviderError{
			Op:  "NewKMSDEKProvider",
			Err: ErrConfigNil,
		}
	}

	if cfg.KMSManager == nil {
		return nil, &KMSDEKProviderError{
			Op:  "NewKMSDEKProvider",
			Err: ErrKMSManagerNil,
		}
	}

	return &KMSDEKProvider{
		kms:    cfg.KMSManager,
		logger: cfg.Logger,
	}, nil
}

// GetDEK retrieves or generates a data encryption key using envelope encryption.
// When req.WrappedKey is nil, a new DEK is generated via GenerateDataKey and
// the plaintext + wrapped key are returned. When req.WrappedKey is provided,
// the wrapped DEK is decrypted (unwrapped) using the KEK identified by Path.
func (p *KMSDEKProvider) GetDEK(ctx context.Context, req *GetDEKRequest) (*GetDEKResponse, error) {
	if req == nil {
		return nil, &KMSDEKProviderError{
			Op:  "GetDEK",
			Err: ErrRequestNil,
		}
	}

	if req.Path == "" {
		return nil, &KMSDEKProviderError{
			Op:  "GetDEK",
			Err: ErrKeyPathEmpty,
		}
	}

	// Check context cancellation
	if ctx.Err() != nil {
		return nil, &KMSDEKProviderError{
			Op:      "GetDEK",
			KeyPath: req.Path,
			Err:     ctx.Err(),
		}
	}

	// Get key metadata from KMS to validate the KEK exists
	keyMeta, err := p.kms.GetKey(ctx, req.Path)
	if err != nil {
		if p.logger != nil {
			p.logger.WithFields(map[string]interface{}{
				"key_path": req.Path,
				"error":    err.Error(),
			}).Debug("Failed to get key from KMS")
		}
		return nil, &KMSDEKProviderError{
			Op:      "GetDEK",
			KeyPath: req.Path,
			Err:     err,
		}
	}

	if keyMeta == nil {
		return nil, &KMSDEKProviderError{
			Op:      "GetDEK",
			KeyPath: req.Path,
			Err:     ErrKeyMetadataNil,
		}
	}

	// Convert KMS metadata to DEK metadata
	dekMeta := p.convertToDEKMetadata(req.Path, keyMeta)

	// Envelope encryption: generate new DEK or unwrap existing one
	if len(req.WrappedKey) > 0 {
		return p.unwrapDEK(ctx, req, dekMeta)
	}
	return p.generateDEK(ctx, req, dekMeta)
}

// generateDEK creates a new DEK by calling GenerateDataKey on the KMS.
// The KEK identified by req.Path wraps the new DEK for safe storage.
func (p *KMSDEKProvider) generateDEK(ctx context.Context, req *GetDEKRequest, dekMeta *DEKMetadata) (*GetDEKResponse, error) {
	attrs := &xkms.KeyAttributes{
		CN: req.Path,
	}

	plaintext, wrapped, err := p.kms.GenerateDataKey(ctx, attrs, defaultDEKSizeBytes)
	if err != nil {
		if p.logger != nil {
			p.logger.WithFields(map[string]interface{}{
				"key_path": req.Path,
				"error":    err.Error(),
			}).Debug("Failed to generate data key from KMS")
		}
		return nil, &KMSDEKProviderError{
			Op:      "GetDEK.GenerateDataKey",
			KeyPath: req.Path,
			Err:     err,
		}
	}

	if len(plaintext) == 0 || len(wrapped) == 0 {
		return nil, &KMSDEKProviderError{
			Op:      "GetDEK.GenerateDataKey",
			KeyPath: req.Path,
			Err:     ErrGenerateDataKeyFailed,
		}
	}

	keyMaterial, err := secure.NewBufferFromBytes(plaintext)
	if err != nil {
		return nil, &KMSDEKProviderError{
			Op:      "GetDEK.GenerateDataKey.Protect",
			KeyPath: req.Path,
			Err:     err,
		}
	}

	if p.logger != nil {
		p.logger.WithFields(map[string]interface{}{
			"key_path": req.Path,
			"version":  dekMeta.Version,
			"status":   dekMeta.Status,
		}).Debug("Generated new DEK from KMS")
	}

	return &GetDEKResponse{
		DEK:         dekMeta,
		KeyMaterial: keyMaterial,
		WrappedKey:  wrapped,
		Timestamp:   time.Now(),
	}, nil
}

// unwrapDEK decrypts a previously wrapped DEK using the KEK identified by Path.
func (p *KMSDEKProvider) unwrapDEK(ctx context.Context, req *GetDEKRequest, dekMeta *DEKMetadata) (*GetDEKResponse, error) {
	plaintext, err := p.kms.Decrypt(ctx, req.Path, req.WrappedKey, nil)
	if err != nil {
		if p.logger != nil {
			p.logger.WithFields(map[string]interface{}{
				"key_path": req.Path,
				"error":    err.Error(),
			}).Debug("Failed to unwrap DEK")
		}
		return nil, &KMSDEKProviderError{
			Op:      "GetDEK.UnwrapDEK",
			KeyPath: req.Path,
			Err:     ErrUnwrapDEKFailed,
		}
	}

	keyMaterial, err := secure.NewBufferFromBytes(plaintext)
	if err != nil {
		return nil, &KMSDEKProviderError{
			Op:      "GetDEK.UnwrapDEK.Protect",
			KeyPath: req.Path,
			Err:     err,
		}
	}

	if p.logger != nil {
		p.logger.WithFields(map[string]interface{}{
			"key_path": req.Path,
			"version":  dekMeta.Version,
			"status":   dekMeta.Status,
		}).Debug("Unwrapped DEK from KMS")
	}

	return &GetDEKResponse{
		DEK:         dekMeta,
		KeyMaterial: keyMaterial,
		WrappedKey:  req.WrappedKey,
		Timestamp:   time.Now(),
	}, nil
}

// convertToDEKMetadata converts KMS key metadata to DEK metadata.
// The exact structure depends on the KMS backend, so we handle the
// conversion using type assertions with fallbacks.
func (p *KMSDEKProvider) convertToDEKMetadata(path string, keyMeta interface{}) *DEKMetadata {
	dek := &DEKMetadata{
		Path:      path,
		Version:   1,
		Status:    DEKStatusActive,
		IsDefault: false,
		CreatedAt: time.Now(),
		RotatedAt: time.Now(),
	}

	// Try to extract version and status from KMS metadata
	// The metadata structure varies by KMS backend
	if meta, ok := keyMeta.(map[string]interface{}); ok {
		if version, exists := meta["version"]; exists {
			switch v := version.(type) {
			case uint64:
				dek.Version = v
			// A negative version is malformed metadata; keep the default
			// rather than wrapping it to a huge unsigned value.
			case int64:
				if v >= 0 {
					dek.Version = uint64(v)
				}
			case int:
				if v >= 0 {
					dek.Version = uint64(v)
				}
			case float64:
				dek.Version = uint64(v)
			}
		}

		if status, exists := meta["status"]; exists {
			if s, ok := status.(string); ok {
				switch s {
				case "active", "ACTIVE", "enabled", "ENABLED":
					dek.Status = DEKStatusActive
				case "disabled", "DISABLED":
					dek.Status = DEKStatusDisabled
				case "rotating", "ROTATING":
					dek.Status = DEKStatusRotating
				}
			}
		}

		if createdAt, exists := meta["created_at"]; exists {
			if t, ok := createdAt.(time.Time); ok {
				dek.CreatedAt = t
			}
		}

		if rotatedAt, exists := meta["rotated_at"]; exists {
			if t, ok := rotatedAt.(time.Time); ok {
				dek.RotatedAt = t
			}
		}
	}

	// Handle kms.KeyMetadata type from our KMS package
	if meta, ok := keyMeta.(interface{ GetVersion() uint64 }); ok {
		dek.Version = meta.GetVersion()
	}

	if meta, ok := keyMeta.(interface{ GetStatus() string }); ok {
		switch meta.GetStatus() {
		case "active", "ACTIVE", "enabled", "ENABLED":
			dek.Status = DEKStatusActive
		case "disabled", "DISABLED":
			dek.Status = DEKStatusDisabled
		case "rotating", "ROTATING":
			dek.Status = DEKStatusRotating
		}
	}

	if meta, ok := keyMeta.(interface{ GetCreatedAt() time.Time }); ok {
		dek.CreatedAt = meta.GetCreatedAt()
	}

	if meta, ok := keyMeta.(interface{ GetRotatedAt() time.Time }); ok {
		dek.RotatedAt = meta.GetRotatedAt()
	}

	return dek
}

// Compile-time interface check.
var _ DEKProvider = (*KMSDEKProvider)(nil)
