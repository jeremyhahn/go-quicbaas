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

// Package service provides the unified service facade for QuicBaaS.
package service

import (
	"context"
	"crypto/elliptic"
	"strings"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// =============================================================================
// Additional KMS Service Methods
// =============================================================================

// SetBackendRegistry sets the backend registry for the KMS service.
func (k *kmsServiceImpl) SetBackendRegistry(registry *kms.BackendRegistry) {
	k.backendRegistry = registry
}

// SetAuditLogger sets the audit logger for the KMS service.
func (k *kmsServiceImpl) SetAuditLogger(logger KMSAuditLogger) {
	k.auditLogger = logger
}

// SetKeySafety sets AEAD safety limits for a key.
func (k *kmsServiceImpl) SetKeySafety(ctx context.Context, req *SetKeySafetyRequest) (*SetKeySafetyResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetKeySafety", "request is nil")
	}
	if req.KeyID == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetKeySafety", "key ID is required")
	}

	// Verify the key exists first
	_, err := k.kmsManager.GetKey(ctx, req.KeyID)
	if err != nil {
		return nil, NewError(errors.ErrNotFound, "kms.SetKeySafety", "key not found: "+req.KeyID)
	}

	// Validate percentage values
	if req.WarningPercent < 0 || req.WarningPercent > 100 {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetKeySafety", "warning percent must be between 0 and 100")
	}
	if req.AutoRotatePercent < 0 || req.AutoRotatePercent > 100 {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetKeySafety", "auto rotate percent must be between 0 and 100")
	}
	if req.WarningPercent > 0 && req.AutoRotatePercent > 0 && req.WarningPercent >= req.AutoRotatePercent {
		return nil, NewError(errors.ErrInvalidInput, "kms.SetKeySafety", "warning percent must be less than auto rotate percent")
	}

	// Build the safety configuration
	config := AEADSafetyConfig{
		MaxOperations:     req.MaxOperations,
		MaxBytes:          req.MaxBytes,
		WarningPercent:    req.WarningPercent,
		AutoRotatePercent: req.AutoRotatePercent,
	}

	// Apply defaults if not specified
	defaultConfig := DefaultAEADSafetyConfig()
	if config.MaxOperations == 0 {
		config.MaxOperations = defaultConfig.MaxOperations
	}
	if config.MaxBytes == 0 {
		config.MaxBytes = defaultConfig.MaxBytes
	}
	if config.WarningPercent == 0 {
		config.WarningPercent = defaultConfig.WarningPercent
	}
	if config.AutoRotatePercent == 0 {
		config.AutoRotatePercent = defaultConfig.AutoRotatePercent
	}

	// Set the configuration in the safety tracker
	k.safetyTracker.SetKeyConfig(req.KeyID, config)

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":              req.KeyID,
			"max_operations":      config.MaxOperations,
			"max_bytes":           config.MaxBytes,
			"warning_percent":     config.WarningPercent,
			"auto_rotate_percent": config.AutoRotatePercent,
		}).Info("Set AEAD safety configuration for key")
	}

	return &SetKeySafetyResponse{
		KeyID:     req.KeyID,
		Config:    AEADSafetyConfigDTO(config),
		Timestamp: time.Now(),
	}, nil
}

// ClearKeySafety clears AEAD safety limits for a key, reverting to server defaults.
func (k *kmsServiceImpl) ClearKeySafety(ctx context.Context, req *ClearKeySafetyRequest) (*ClearKeySafetyResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.ClearKeySafety", "request is nil")
	}
	if req.KeyID == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.ClearKeySafety", "key ID is required")
	}

	// Verify the key exists first
	_, err := k.kmsManager.GetKey(ctx, req.KeyID)
	if err != nil {
		return nil, NewError(errors.ErrNotFound, "kms.ClearKeySafety", "key not found: "+req.KeyID)
	}

	// Clear the per-key configuration (reverts to server defaults)
	k.safetyTracker.ClearKeyConfig(req.KeyID)

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id": req.KeyID,
		}).Info("Cleared AEAD safety configuration for key")
	}

	return &ClearKeySafetyResponse{
		KeyID:     req.KeyID,
		Timestamp: time.Now(),
	}, nil
}

// algorithmAttributeApplier applies algorithm-specific settings to KeyAttributes.
type algorithmAttributeApplier func(attrs *kctypes.KeyAttributes)

// algorithmToAttrsMap provides O(1) lookup from KeyAlgorithm to the corresponding
// attribute applier function. Each applier sets the correct fields on KeyAttributes
// for the given algorithm (symmetric algo, key algo, RSA/ECC attributes, etc.).
var algorithmToAttrsMap = map[KeyAlgorithm]algorithmAttributeApplier{
	KeyAlgAES128GCM:         func(a *kctypes.KeyAttributes) { a.SymmetricAlgorithm = kctypes.SymmetricAES128GCM },
	KeyAlgAES192GCM:         func(a *kctypes.KeyAttributes) { a.SymmetricAlgorithm = kctypes.SymmetricAES192GCM },
	KeyAlgAES256GCM:         func(a *kctypes.KeyAttributes) { a.SymmetricAlgorithm = kctypes.SymmetricAES256GCM },
	KeyAlgChaCha20Poly1305:  func(a *kctypes.KeyAttributes) { a.SymmetricAlgorithm = kctypes.SymmetricChaCha20Poly1305 },
	KeyAlgXChaCha20Poly1305: func(a *kctypes.KeyAttributes) { a.SymmetricAlgorithm = kctypes.SymmetricXChaCha20Poly1305 },
	KeyAlgRSA2048: func(a *kctypes.KeyAttributes) {
		a.KeyAlgorithm = 1 // x509.RSA
		a.RSAAttributes = &kctypes.RSAAttributes{KeySize: 2048}
	},
	KeyAlgRSA3072: func(a *kctypes.KeyAttributes) {
		a.KeyAlgorithm = 1
		a.RSAAttributes = &kctypes.RSAAttributes{KeySize: 3072}
	},
	KeyAlgRSA4096: func(a *kctypes.KeyAttributes) {
		a.KeyAlgorithm = 1
		a.RSAAttributes = &kctypes.RSAAttributes{KeySize: 4096}
	},
	KeyAlgECDSAP256: func(a *kctypes.KeyAttributes) {
		a.KeyAlgorithm = 3 // x509.ECDSA
		a.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P256()}
	},
	KeyAlgECDSAP384: func(a *kctypes.KeyAttributes) {
		a.KeyAlgorithm = 3
		a.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P384()}
	},
	KeyAlgECDSAP521: func(a *kctypes.KeyAttributes) {
		a.KeyAlgorithm = 3
		a.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P521()}
	},
	KeyAlgEd25519: func(a *kctypes.KeyAttributes) {
		a.KeyAlgorithm = 4 // x509.Ed25519
	},
}

// algorithmToKeyAttributes converts a key algorithm and options to KeyAttributes.
func (k *kmsServiceImpl) algorithmToKeyAttributes(keyID string, algorithm KeyAlgorithm, exportable bool) *kctypes.KeyAttributes {
	attrs := &kctypes.KeyAttributes{
		CN:         keyID,
		Exportable: exportable,
	}

	if applier, ok := algorithmToAttrsMap[algorithm]; ok {
		applier(attrs)
	} else {
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
	}

	return attrs
}

// =============================================================================
// Helper Functions
// =============================================================================

// normalizeDEKPath normalizes a DEK path by cleaning up redundant slashes.
// The path is trimmed of leading and trailing slashes to ensure consistent
// storage and lookup. The original path format is preserved in API responses.
// Empty string returns empty string.
func normalizeDEKPath(path string) string {
	// Handle empty path
	if path == "" {
		return ""
	}

	// Trim leading and trailing slashes for consistent storage
	path = strings.Trim(path, "/")

	// Handle root path case
	if path == "" {
		return ""
	}

	return path
}

// isValidWrappingAlgorithm checks if a wrapping algorithm is valid.
// Supported algorithms follow JWE/JOSE standards.
func isValidWrappingAlgorithm(algorithm string) bool {
	validAlgorithms := map[string]bool{
		// AES Key Wrap variants
		"AES-KW":  true,
		"AES-KWP": true,
		"AES-GCM": true,

		// RSA-OAEP with SHA-2 variants (JOSE naming convention)
		"RSA-OAEP":        true,
		"RSA-OAEP-SHA256": true,
		"RSA-OAEP-SHA384": true,
		"RSA-OAEP-SHA512": true,

		// ECDH-ES key wrapping variants (JOSE naming convention)
		"ECDH-ES+A128KW": true,
		"ECDH-ES+A192KW": true,
		"ECDH-ES+A256KW": true,
	}
	return validAlgorithms[algorithm]
}
