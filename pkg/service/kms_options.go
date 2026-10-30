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
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// =============================================================================
// KMS Service Options
// =============================================================================

// KMSAuditLogger interface for audit logging of KMS operations.
type KMSAuditLogger interface {
	LogKeyExport(ctx context.Context, username, userID, keyID, algorithm, wrappingKeyID, wrappingAlgorithm, ipAddr string, success bool) error
	LogKeyGenerate(ctx context.Context, username, userID, keyID, keyName, algorithm, ipAddr string, success bool) error
	LogKeyImport(ctx context.Context, username, userID, keyID, keyName, algorithm, ipAddr string, success bool) error
	LogKeyDeletion(ctx context.Context, username, userID, keyID, ipAddr string, success bool) error
	// LogGracePeriodDisable logs when a key version is disabled due to grace period expiration.
	// The jobID parameter is the scheduler job ID that executed this action.
	LogGracePeriodDisable(ctx context.Context, keyID string, version uint64, jobID string, success bool, errorMsg string) error
}

// KMSServiceOption configures the KMS service.
type KMSServiceOption func(*kmsServiceImpl)

// WithKMSAuditLogger sets the audit logger for the KMS service.
func WithKMSAuditLogger(logger KMSAuditLogger) KMSServiceOption {
	return func(k *kmsServiceImpl) {
		k.auditLogger = logger
	}
}

// WithBackendRegistry sets the backend registry for the KMS service.
func WithBackendRegistry(registry *kms.BackendRegistry) KMSServiceOption {
	return func(k *kmsServiceImpl) {
		k.backendRegistry = registry
	}
}

// WithKMSAuthzEnabled enables or disables per-key authorization enforcement.
// When enabled, every crypto operation (Encrypt, Decrypt, Sign, Verify,
// GenerateDataKey) on the KMS service checks the stored KeyPolicy and the
// authenticated Subject in the request context before proceeding.
//
// Set to false only in test or dev environments where full auth is not
// configured. Production deployments must set this to true.
func WithKMSAuthzEnabled(enabled bool) KMSServiceOption {
	return func(k *kmsServiceImpl) {
		k.authzEnabled = enabled
	}
}

// =============================================================================
// Context Keys for Audit Information
// =============================================================================

type contextKey string

const (
	contextKeyUsername contextKey = "audit_username"
	contextKeyUserID   contextKey = "audit_user_id"
	contextKeyIPAddr   contextKey = "audit_ip_addr"
)

// WithAuditContext adds audit information to the context.
func WithAuditContext(ctx context.Context, username, userID, ipAddr string) context.Context {
	ctx = context.WithValue(ctx, contextKeyUsername, username)
	ctx = context.WithValue(ctx, contextKeyUserID, userID)
	ctx = context.WithValue(ctx, contextKeyIPAddr, ipAddr)
	return ctx
}

// extractAuditContext extracts audit information from the context.
func extractAuditContext(ctx context.Context) (username, userID, ipAddr string) {
	if v, ok := ctx.Value(contextKeyUsername).(string); ok {
		username = v
	}
	if v, ok := ctx.Value(contextKeyUserID).(string); ok {
		userID = v
	}
	if v, ok := ctx.Value(contextKeyIPAddr).(string); ok {
		ipAddr = v
	}
	return
}

// =============================================================================
// Additional KMS Service Methods
// =============================================================================

// ExportKey exports a key wrapped with another key.
// The source key's material is encrypted using the wrapping key,
// allowing secure transport of the key to another system.
func (k *kmsServiceImpl) ExportKey(ctx context.Context, req *ExportKeyRequest) (*ExportKeyResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.ExportKey", "request is nil")
	}
	if req.KeyID == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.ExportKey", "key_id is required")
	}
	if req.WrappingKeyID == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.ExportKey", "wrapping_key_id is required")
	}
	if req.WrappingAlgorithm == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.ExportKey", "wrapping_algorithm is required")
	}

	// Validate wrapping algorithm
	if !isValidWrappingAlgorithm(req.WrappingAlgorithm) {
		return nil, NewError(errors.ErrInvalidInput, "kms.ExportKey", "unsupported wrapping algorithm: "+req.WrappingAlgorithm)
	}

	// Both keys resolve under the caller's tenant, so another tenant's key
	// id is not found and the wrapped blob binds the tenant-scoped id.
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	scopedKeyID, err := k.resolveTenantKey(ctx, tenantID, req.KeyID, "resolveTenantKey")
	if err != nil {
		return nil, err
	}
	scopedWrappingKeyID, err := k.resolveTenantKey(ctx, tenantID, req.WrappingKeyID, "resolveTenantKey")
	if err != nil {
		return nil, err
	}

	// Get source key metadata to verify it exists and is exportable
	keyMeta, err := k.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		k.logAuditExport(ctx, req.KeyID, "", req.WrappingKeyID, req.WrappingAlgorithm, false)
		return nil, NewError(errors.ErrNotFound, "kms.ExportKey", "source key not found: "+req.KeyID)
	}

	// Check if key is exportable and extract metadata
	// Handle both service.KeyMetadata and kms.KeyMetadata types
	var algorithm string
	var keyVersion uint64
	var exportable bool
	var enabled bool

	switch meta := keyMeta.(type) {
	case *KeyMetadata:
		exportable = meta.Exportable
		algorithm = string(meta.Algorithm)
		keyVersion = meta.Version
		enabled = meta.Status == KeyStatusEnabled
	case *kms.KeyMetadata:
		version, versionErr := kmsKeyVersion("kms.ExportKey", meta.Version)
		if versionErr != nil {
			k.logAuditExport(ctx, req.KeyID, "", req.WrappingKeyID, req.WrappingAlgorithm, false)
			return nil, versionErr
		}
		exportable = meta.Exportable
		algorithm = meta.Algorithm
		keyVersion = version
		enabled = meta.State == kms.KeyStateEnabled
	default:
		// Unknown type - for security, reject the export
		k.logAuditExport(ctx, req.KeyID, "", req.WrappingKeyID, req.WrappingAlgorithm, false)
		return nil, NewError(errors.ErrInternal, "kms.ExportKey", "unable to verify key exportability: unknown metadata type")
	}

	// Check exportable flag
	if !exportable {
		k.logAuditExport(ctx, req.KeyID, algorithm, req.WrappingKeyID, req.WrappingAlgorithm, false)
		return nil, NewError(errors.ErrForbidden, "kms.ExportKey", "key "+req.KeyID+" is not exportable")
	}

	// Verify key is enabled
	if !enabled {
		k.logAuditExport(ctx, req.KeyID, algorithm, req.WrappingKeyID, req.WrappingAlgorithm, false)
		return nil, NewError(errors.ErrForbidden, "kms.ExportKey", "key "+req.KeyID+" is not enabled")
	}

	// If a specific version was requested, use it; otherwise use the current version
	exportVersion := keyVersion
	if req.Version > 0 {
		exportVersion = req.Version
	}

	// Verify wrapping key exists and is enabled
	wrappingKeyMeta, err := k.kmsManager.GetKey(ctx, scopedWrappingKeyID)
	if err != nil {
		k.logAuditExport(ctx, req.KeyID, algorithm, req.WrappingKeyID, req.WrappingAlgorithm, false)
		return nil, NewError(errors.ErrNotFound, "kms.ExportKey", "wrapping key not found: "+req.WrappingKeyID)
	}

	// Check wrapping key status
	var wrapEnabled bool
	switch wrapMeta := wrappingKeyMeta.(type) {
	case *KeyMetadata:
		wrapEnabled = wrapMeta.Status == KeyStatusEnabled
	case *kms.KeyMetadata:
		wrapEnabled = wrapMeta.State == kms.KeyStateEnabled
	default:
		wrapEnabled = true // Allow if we can't determine status
	}
	if !wrapEnabled {
		k.logAuditExport(ctx, req.KeyID, algorithm, req.WrappingKeyID, req.WrappingAlgorithm, false)
		return nil, NewError(errors.ErrForbidden, "kms.ExportKey", "wrapping key "+req.WrappingKeyID+" is not enabled")
	}

	// Generate a data key that represents the exportable key material.
	// This uses the source key to generate a wrapped data key that can be exported.
	// The plaintext is then wrapped with the wrapping key for secure transport.
	keyAttrs := k.algorithmToKeyAttributes(scopedKeyID, KeyAlgorithm(algorithm), true)
	plaintextKeyMaterial, _, err := k.kmsManager.GenerateDataKey(ctx, keyAttrs, 32)
	if err != nil {
		k.logAuditExport(ctx, req.KeyID, algorithm, req.WrappingKeyID, req.WrappingAlgorithm, false)
		return nil, WrapError(err, errors.ErrInternal, "kms.ExportKey", "failed to generate export key material")
	}

	// Additional authenticated data for the wrapping operation
	aad := []byte(fmt.Sprintf("export:%s:v%d", scopedKeyID, exportVersion))

	// Wrap the plaintext key material using the wrapping key for secure transport
	wrappedKey, err := k.kmsManager.Encrypt(ctx, scopedWrappingKeyID, plaintextKeyMaterial, aad)
	if err != nil {
		k.logAuditExport(ctx, req.KeyID, algorithm, req.WrappingKeyID, req.WrappingAlgorithm, false)
		return nil, WrapError(err, errors.ErrInternal, "kms.ExportKey", "failed to wrap key with wrapping key")
	}

	// Log successful export
	k.logAuditExport(ctx, req.KeyID, algorithm, req.WrappingKeyID, req.WrappingAlgorithm, true)

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":             req.KeyID,
			"version":            exportVersion,
			"wrapping_key_id":    req.WrappingKeyID,
			"wrapping_algorithm": req.WrappingAlgorithm,
			"format":             req.Format,
		}).Info("Exported key successfully")
	}

	return &ExportKeyResponse{
		KeyID:             req.KeyID,
		Version:           exportVersion,
		WrappedKey:        wrappedKey,
		WrappingKeyID:     req.WrappingKeyID,
		WrappingAlgorithm: req.WrappingAlgorithm,
		Format:            req.Format,
		Timestamp:         time.Now(),
	}, nil
}

// UnwrapKey unwraps an encrypted key and imports it into the KMS.
// The wrapped key material is decrypted using the wrapping key,
// then imported as a new key with the specified attributes.
func (k *kmsServiceImpl) UnwrapKey(ctx context.Context, req *UnwrapKeyRequest) (*UnwrapKeyResponse, error) {
	if req == nil {
		return nil, NewError(errors.ErrInvalidInput, "kms.UnwrapKey", "request is nil")
	}
	if req.KeyID == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.UnwrapKey", "key_id is required")
	}
	if len(req.WrappedKey) == 0 {
		return nil, NewError(errors.ErrInvalidInput, "kms.UnwrapKey", "wrapped_key is required")
	}
	if req.WrappingKeyID == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.UnwrapKey", "wrapping_key_id is required")
	}
	if req.WrappingAlgorithm == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.UnwrapKey", "wrapping_algorithm is required")
	}
	if req.Algorithm == "" {
		return nil, NewError(errors.ErrInvalidInput, "kms.UnwrapKey", "algorithm is required")
	}

	// Validate wrapping algorithm
	if !isValidWrappingAlgorithm(req.WrappingAlgorithm) {
		return nil, NewError(errors.ErrInvalidInput, "kms.UnwrapKey", "unsupported wrapping algorithm: "+req.WrappingAlgorithm)
	}

	// Every key id resolves under the caller's tenant: the wrapping key of
	// another tenant is not found, and a blob exported by another tenant
	// fails authentication because its AAD binds that tenant's scoped id.
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := rejectReservedKeyName(req.KeyID, "UnwrapKey"); err != nil {
		return nil, err
	}
	scopedKeyID := tenantScopedKeyID(tenantID, req.KeyID)
	scopedWrappingKeyID, err := k.resolveTenantKey(ctx, tenantID, req.WrappingKeyID, "resolveTenantKey")
	if err != nil {
		return nil, err
	}

	// Check if target key ID already exists
	_, err = k.kmsManager.GetKey(ctx, scopedKeyID)
	if err == nil {
		// Key already exists
		k.logAuditImport(ctx, req.KeyID, req.Name, req.Algorithm, false)
		return nil, NewError(errors.ErrConflict, "kms.UnwrapKey", "key already exists: "+req.KeyID)
	}

	// Verify wrapping key exists and is enabled
	wrappingKeyMeta, err := k.kmsManager.GetKey(ctx, scopedWrappingKeyID)
	if err != nil {
		k.logAuditImport(ctx, req.KeyID, req.Name, req.Algorithm, false)
		return nil, NewError(errors.ErrNotFound, "kms.UnwrapKey", "wrapping key not found: "+req.WrappingKeyID)
	}

	// Check wrapping key status
	if meta, ok := wrappingKeyMeta.(*KeyMetadata); ok {
		if meta.Status != KeyStatusEnabled {
			k.logAuditImport(ctx, req.KeyID, req.Name, req.Algorithm, false)
			return nil, NewError(errors.ErrForbidden, "kms.UnwrapKey", "wrapping key "+req.WrappingKeyID+" is not enabled")
		}
	}

	// Additional authenticated data for the unwrapping operation
	// This must match what was used during ExportKey
	// Use the original source key ID and version if provided, otherwise fall back to the new key ID
	sourceKeyID := req.SourceKeyID
	if sourceKeyID == "" {
		sourceKeyID = req.KeyID // Fallback for backwards compatibility
	}
	sourceVersion := req.SourceVersion
	aad := []byte(fmt.Sprintf("export:%s:v%d", tenantScopedKeyID(tenantID, sourceKeyID), sourceVersion))

	// Unwrap the key material using the wrapping key
	unwrappedKeyMaterial, err := k.kmsManager.Decrypt(ctx, scopedWrappingKeyID, req.WrappedKey, aad)
	if err != nil {
		k.logAuditImport(ctx, req.KeyID, req.Name, req.Algorithm, false)
		return nil, WrapError(err, errors.ErrInternal, "kms.UnwrapKey", "failed to unwrap key material")
	}

	// Validate key material size based on algorithm
	if err := k.validateKeyMaterialSize(unwrappedKeyMaterial, KeyAlgorithm(req.Algorithm)); err != nil {
		k.logAuditImport(ctx, req.KeyID, req.Name, req.Algorithm, false)
		return nil, err
	}

	// Import the unwrapped key into the KMS, after recording the tenant
	// as its owner
	claim, err := k.claimTenantKey(ctx, tenantID, req.KeyID, "UnwrapKey")
	if err != nil {
		k.logAuditImport(ctx, req.KeyID, req.Name, req.Algorithm, false)
		return nil, err
	}
	_, err = k.kmsManager.ImportKey(ctx, scopedKeyID, unwrappedKeyMaterial, req.Algorithm)
	if err != nil {
		k.logAuditImport(ctx, req.KeyID, req.Name, req.Algorithm, false)
		return nil, k.abandonTenantKeyClaim(ctx, claim, "UnwrapKey",
			WrapError(err, errors.ErrInternal, "kms.UnwrapKey", "failed to import unwrapped key"))
	}
	if err := k.completeTenantKeyClaim(ctx, claim, "UnwrapKey"); err != nil {
		k.logAuditImport(ctx, req.KeyID, req.Name, req.Algorithm, false)
		return nil, err
	}

	// Log successful import
	k.logAuditImport(ctx, req.KeyID, req.Name, req.Algorithm, true)

	if k.logger != nil {
		k.logger.WithFields(map[string]interface{}{
			"key_id":             req.KeyID,
			"name":               req.Name,
			"algorithm":          req.Algorithm,
			"wrapping_key_id":    req.WrappingKeyID,
			"wrapping_algorithm": req.WrappingAlgorithm,
		}).Info("Unwrapped and imported key successfully")
	}

	return &UnwrapKeyResponse{
		KeyID:     req.KeyID,
		Version:   1,
		Algorithm: KeyAlgorithm(req.Algorithm),
		Status:    KeyStatusEnabled,
		Key: &KeyHandle{
			KeyID:     req.KeyID,
			Version:   1,
			Algorithm: KeyAlgorithm(req.Algorithm),
			Status:    KeyStatusEnabled,
			CreatedAt: time.Now(),
		},
		Timestamp: time.Now(),
	}, nil
}

// logAuditExport logs a key export operation to the audit logger.
func (k *kmsServiceImpl) logAuditExport(ctx context.Context, keyID, algorithm, wrappingKeyID, wrappingAlgorithm string, success bool) {
	if k.auditLogger == nil {
		return
	}
	username, userID, ipAddr := extractAuditContext(ctx)
	if err := k.auditLogger.LogKeyExport(ctx, username, userID, keyID, algorithm, wrappingKeyID, wrappingAlgorithm, ipAddr, success); err != nil {
		if k.logger != nil {
			k.logger.WithError(err).WithFields(map[string]interface{}{
				"key_id":    keyID,
				"operation": "export",
			}).Error("failed to write key export audit log")
		}
	}
}

// logAuditImport logs a key import operation to the audit logger.
func (k *kmsServiceImpl) logAuditImport(ctx context.Context, keyID, keyName, algorithm string, success bool) {
	if k.auditLogger == nil {
		return
	}
	username, userID, ipAddr := extractAuditContext(ctx)
	if err := k.auditLogger.LogKeyImport(ctx, username, userID, keyID, keyName, algorithm, ipAddr, success); err != nil {
		if k.logger != nil {
			k.logger.WithError(err).WithFields(map[string]interface{}{
				"key_id":    keyID,
				"operation": "import",
			}).Error("failed to write key import audit log")
		}
	}
}

// validateKeyMaterialSize validates that the key material has the correct size
// for the specified algorithm.
func (k *kmsServiceImpl) validateKeyMaterialSize(material []byte, algorithm KeyAlgorithm) error {
	expectedSize := 0
	switch algorithm {
	case KeyAlgAES128GCM:
		expectedSize = 16
	case KeyAlgAES192GCM:
		expectedSize = 24
	case KeyAlgAES256GCM:
		expectedSize = 32
	case KeyAlgChaCha20Poly1305:
		expectedSize = 32
	case KeyAlgXChaCha20Poly1305:
		expectedSize = 32
	default:
		// For asymmetric algorithms, we don't validate size here
		// as it depends on the specific key format
		return nil
	}

	if len(material) != expectedSize {
		return NewError(errors.ErrInvalidInput, "kms.validateKeyMaterialSize",
			fmt.Sprintf("invalid key material size for %s: expected %d bytes, got %d", algorithm, expectedSize, len(material)))
	}

	return nil
}
