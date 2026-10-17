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

// Package service implements the unified service facade for QuicBaaS.
// This file contains the Crypto service implementation for encryption as a service.
package service

import (
	"context"
	stderrors "errors"
	"time"

	types "github.com/jeremyhahn/go-xkms/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// =============================================================================
// Crypto Service Implementation
// =============================================================================

// cryptoService provides encryption-as-a-service operations.
// All cryptographic operations use keys managed by the CryptoManager
// without exposing key material to clients.
type cryptoService struct {
	cryptoManager interfaces.CryptoManager
	kmsManager    interfaces.KMSManager
	logger        interfaces.Logger
	metrics       *cryptoMetrics
	safetyTracker *AEADSafetyTracker
	// kmsSvc is the KMS service used for per-key authorization and audit.
	// When set together with authzEnabled, every crypto call is gated by
	// checkKeyAuthz before the underlying operation proceeds.
	kmsSvc       *kmsServiceImpl
	authzEnabled bool
}

// cryptoMetrics holds Prometheus metrics for crypto operations.
type cryptoMetrics struct {
	encryptOperationsTotal *metrics.CounterVec
	decryptOperationsTotal *metrics.CounterVec
	signOperationsTotal    *metrics.CounterVec
	verifyOperationsTotal  *metrics.CounterVec
	hmacOperationsTotal    *metrics.CounterVec
	operationDuration      *metrics.HistogramVec
	errorsByType           *metrics.CounterVec
}

// newCryptoService creates a new crypto service.
func newCryptoService(cryptoManager interfaces.CryptoManager, kmsManager interfaces.KMSManager, logger interfaces.Logger, safetyTracker *AEADSafetyTracker) *cryptoService {
	svc := &cryptoService{
		cryptoManager: cryptoManager,
		kmsManager:    kmsManager,
		logger:        logger,
		safetyTracker: safetyTracker,
	}

	// Initialize metrics
	svc.initMetrics()

	return svc
}

// checkKeyAuthz gates a crypto operation behind per-key authorization and
// emits a key-use audit event.  When kmsSvc is nil or authzEnabled is false
// the check is skipped (backward-compatible with existing deployments).
func (t *cryptoService) checkKeyAuthz(ctx context.Context, keyID, operation string) error {
	if !t.authzEnabled || t.kmsSvc == nil {
		return nil
	}
	return t.kmsSvc.checkKeyAuthz(ctx, keyID, operation)
}

// scopeKeyID resolves a caller-supplied key id under the caller's tenant,
// the same way every KMS verb does, so a crypto verb never addresses
// another tenant's key.
func scopeKeyID(ctx context.Context, keyID string) (string, error) {
	tenantID, err := prepareTenantContext(ctx)
	if err != nil {
		return "", err
	}
	return tenantScopedKeyID(tenantID, keyID), nil
}

// keyOperationError wraps a failed key operation. A key absent from the
// caller's tenant surfaces as not-found, mirroring the KMS verbs.
func keyOperationError(code errors.ErrorCode, op, operation string, err error) error {
	if stderrors.Is(err, kms.ErrKeyNotFound) {
		code = errors.ErrNotFound
	}
	return &errors.QBError{
		Code: code,
		Op:   op,
		Err:  &api.OperationFailedError{Operation: operation, Cause: err},
	}
}

// initMetrics initializes Prometheus metrics for crypto operations.
func (t *cryptoService) initMetrics() {
	t.metrics = &cryptoMetrics{
		encryptOperationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "crypto",
			Name:      "encrypt_operations_total",
			Help:      "Total number of encrypt operations",
		}, []string{"key_id", "status"}),

		decryptOperationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "crypto",
			Name:      "decrypt_operations_total",
			Help:      "Total number of decrypt operations",
		}, []string{"key_id", "status"}),

		signOperationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "crypto",
			Name:      "sign_operations_total",
			Help:      "Total number of sign operations",
		}, []string{"key_id", "status"}),

		verifyOperationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "crypto",
			Name:      "verify_operations_total",
			Help:      "Total number of verify operations",
		}, []string{"key_id", "status"}),

		hmacOperationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "crypto",
			Name:      "hmac_operations_total",
			Help:      "Total number of HMAC operations",
		}, []string{"key_id", "status"}),

		operationDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "crypto",
			Name:      "operation_duration_seconds",
			Help:      "Duration of crypto operations in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		}, []string{"operation"}),

		errorsByType: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "crypto",
			Name:      "errors_total",
			Help:      "Total crypto errors by type",
		}, []string{"error_type"}),
	}
}

// =============================================================================
// Symmetric Encryption Operations
// =============================================================================

// Encrypt encrypts plaintext data using a named encryption key.
func (t *cryptoService) Encrypt(ctx context.Context, req *EncryptRequest) (*EncryptResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("encrypt").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateEncryptRequest(req); err != nil {
		keyID := "unknown"
		if req != nil {
			keyID = req.KeyID
		}
		t.metrics.encryptOperationsTotal.With(keyID, "invalid").Inc()
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	scopedKeyID, err := scopeKeyID(ctx, req.KeyID)
	if err != nil {
		return nil, err
	}

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2).
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "encrypt"); err != nil {
		t.metrics.encryptOperationsTotal.With(req.KeyID, "authz_denied").Inc()
		t.metrics.errorsByType.With("authz_error").Inc()
		return nil, err
	}

	// Track key usage before encryption (if tracker configured)
	if t.safetyTracker != nil {
		if err := t.safetyTracker.RecordKeyEncrypt(ctx, scopedKeyID, uint64(len(req.Plaintext))); err != nil {
			t.metrics.encryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
			t.metrics.errorsByType.With("safety_limit_error").Inc()
			return nil, &errors.QBError{
				Code: errors.ErrEncryption,
				Op:   "crypto.Encrypt",
				Err:  err,
			}
		}
	}

	// Perform encryption
	ciphertext, err := t.cryptoManager.Encrypt(ctx, scopedKeyID, req.Plaintext, req.Context)
	if err != nil {
		t.metrics.encryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("encrypt_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.Encrypt", "encrypt data", err)
	}

	// Get key version from KMS (if available)
	var keyVersion uint64 = 1
	if t.kmsManager != nil {
		if keyInfo, err := t.kmsManager.GetKey(ctx, scopedKeyID); err == nil {
			// Type assert to extract version (implementation-specific)
			if km, ok := keyInfo.(map[string]interface{}); ok {
				if v, ok := km["version"].(uint64); ok {
					keyVersion = v
				}
			}
		}
	}

	resp := &EncryptResponse{
		KeyID:      req.KeyID,
		KeyVersion: keyVersion,
		Ciphertext: ciphertext,
		Nonce:      req.Nonce,
		Timestamp:  time.Now(),
	}

	t.metrics.encryptOperationsTotal.With(req.KeyID, "success").Inc()

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"key_id":      req.KeyID,
			"key_version": keyVersion,
			"data_size":   len(req.Plaintext),
		}).Debug("Encrypted data successfully")
	}

	return resp, nil
}

// Decrypt decrypts ciphertext that was encrypted with a named key.
func (t *cryptoService) Decrypt(ctx context.Context, req *DecryptRequest) (*DecryptResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("decrypt").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateDecryptRequest(req); err != nil {
		keyID := "unknown"
		if req != nil {
			keyID = req.KeyID
		}
		t.metrics.decryptOperationsTotal.With(keyID, "invalid").Inc()
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	scopedKeyID, err := scopeKeyID(ctx, req.KeyID)
	if err != nil {
		return nil, err
	}

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2).
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "decrypt"); err != nil {
		t.metrics.decryptOperationsTotal.With(req.KeyID, "authz_denied").Inc()
		t.metrics.errorsByType.With("authz_error").Inc()
		return nil, err
	}

	// Track key usage before decryption (if tracker configured)
	// Note: For decryption we track the ciphertext size as an approximation
	if t.safetyTracker != nil {
		if err := t.safetyTracker.RecordKeyEncrypt(ctx, scopedKeyID, uint64(len(req.Ciphertext))); err != nil {
			t.metrics.decryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
			t.metrics.errorsByType.With("safety_limit_error").Inc()
			return nil, &errors.QBError{
				Code: errors.ErrDecryption,
				Op:   "crypto.Decrypt",
				Err:  err,
			}
		}
	}

	// Perform decryption
	plaintext, err := t.cryptoManager.Decrypt(ctx, scopedKeyID, req.Ciphertext, req.Context)
	if err != nil {
		t.metrics.decryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("decrypt_error").Inc()
		return nil, keyOperationError(errors.ErrDecryption, "crypto.Decrypt", "decrypt data", err)
	}

	// Use provided key version or default
	keyVersion := req.KeyVersion
	if keyVersion == 0 {
		keyVersion = 1
	}

	resp := &DecryptResponse{
		KeyID:      req.KeyID,
		KeyVersion: keyVersion,
		Plaintext:  plaintext,
		Timestamp:  time.Now(),
	}

	t.metrics.decryptOperationsTotal.With(req.KeyID, "success").Inc()

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"key_id":      req.KeyID,
			"key_version": keyVersion,
			"data_size":   len(plaintext),
		}).Debug("Decrypted data successfully")
	}

	return resp, nil
}

// Rewrap re-encrypts ciphertext with the latest version of the key.
func (t *cryptoService) Rewrap(ctx context.Context, req *RewrapRequest) (*RewrapResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("rewrap").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateRewrapRequest(req); err != nil {
		keyID := "unknown"
		if req != nil {
			keyID = req.KeyID
		}
		t.metrics.encryptOperationsTotal.With(keyID, "invalid").Inc()
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	scopedKeyID, err := scopeKeyID(ctx, req.KeyID)
	if err != nil {
		return nil, err
	}

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2).
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "encrypt"); err != nil {
		t.metrics.encryptOperationsTotal.With(req.KeyID, "authz_denied").Inc()
		t.metrics.errorsByType.With("authz_error").Inc()
		return nil, err
	}

	// Track key usage for rewrap operation (if tracker configured)
	if t.safetyTracker != nil {
		if err := t.safetyTracker.RecordKeyEncrypt(ctx, scopedKeyID, uint64(len(req.Ciphertext))); err != nil {
			t.metrics.encryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
			t.metrics.errorsByType.With("safety_limit_error").Inc()
			return nil, &errors.QBError{
				Code: errors.ErrEncryption,
				Op:   "crypto.Rewrap",
				Err:  err,
			}
		}
	}

	// Perform rewrap
	newCiphertext, err := t.cryptoManager.Rewrap(ctx, scopedKeyID, req.Ciphertext, req.Context)
	if err != nil {
		t.metrics.encryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("rewrap_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.Rewrap", "rewrap data", err)
	}

	resp := &RewrapResponse{
		KeyID:         req.KeyID,
		OldKeyVersion: 1, // Would be extracted from ciphertext metadata
		NewKeyVersion: 2, // Would be current key version
		Ciphertext:    newCiphertext,
		Nonce:         req.Nonce,
		Timestamp:     time.Now(),
	}

	t.metrics.encryptOperationsTotal.With(req.KeyID, "success").Inc()

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"key_id":          req.KeyID,
			"old_key_version": resp.OldKeyVersion,
			"new_key_version": resp.NewKeyVersion,
		}).Debug("Rewrapped data successfully")
	}

	return resp, nil
}

// =============================================================================
// Batch Operations
// =============================================================================

// EncryptBatch performs multiple encrypt operations in a single request.
func (t *cryptoService) EncryptBatch(ctx context.Context, req *EncryptBatchRequest) (*EncryptBatchResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("encrypt_batch").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateEncryptBatchRequest(req); err != nil {
		keyID := "unknown"
		if req != nil {
			keyID = req.KeyID
		}
		t.metrics.encryptOperationsTotal.With(keyID, "invalid").Inc()
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	scopedKeyID, err := scopeKeyID(ctx, req.KeyID)
	if err != nil {
		return nil, err
	}

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2).
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "encrypt"); err != nil {
		t.metrics.encryptOperationsTotal.With(req.KeyID, "authz_denied").Inc()
		t.metrics.errorsByType.With("authz_error").Inc()
		return nil, err
	}

	// Track key usage for batch operation (if tracker configured)
	if t.safetyTracker != nil {
		// Calculate total bytes in the batch
		var totalBytes uint64
		for _, item := range req.Items {
			totalBytes += uint64(len(item.Plaintext))
		}

		// Check safety limits before processing batch
		if err := t.safetyTracker.RecordKeyEncrypt(ctx, scopedKeyID, totalBytes); err != nil {
			t.metrics.encryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
			t.metrics.errorsByType.With("safety_limit_error").Inc()
			return nil, &errors.QBError{
				Code: errors.ErrEncryption,
				Op:   "crypto.EncryptBatch",
				Err:  err,
			}
		}
	}

	// Extract plaintexts from items
	plaintexts := make([][]byte, len(req.Items))
	for i, item := range req.Items {
		plaintexts[i] = item.Plaintext
	}

	// Perform batch encryption
	ciphertexts, err := t.cryptoManager.EncryptBatch(ctx, scopedKeyID, plaintexts, nil)
	if err != nil {
		t.metrics.encryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("encrypt_batch_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.EncryptBatch", "encrypt batch", err)
	}

	// Build results
	results := make([]BatchEncryptResult, len(req.Items))
	for i, ciphertext := range ciphertexts {
		results[i] = BatchEncryptResult{
			Ciphertext: ciphertext,
		}
	}

	resp := &EncryptBatchResponse{
		KeyID:      req.KeyID,
		KeyVersion: 1,
		Results:    results,
		Timestamp:  time.Now(),
	}

	t.metrics.encryptOperationsTotal.With(req.KeyID, "success").Inc()

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"key_id":     req.KeyID,
			"batch_size": len(req.Items),
		}).Debug("Encrypted batch successfully")
	}

	return resp, nil
}

// DecryptBatch performs multiple decrypt operations in a single request.
func (t *cryptoService) DecryptBatch(ctx context.Context, req *DecryptBatchRequest) (*DecryptBatchResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("decrypt_batch").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateDecryptBatchRequest(req); err != nil {
		keyID := "unknown"
		if req != nil {
			keyID = req.KeyID
		}
		t.metrics.decryptOperationsTotal.With(keyID, "invalid").Inc()
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	scopedKeyID, err := scopeKeyID(ctx, req.KeyID)
	if err != nil {
		return nil, err
	}

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2).
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "decrypt"); err != nil {
		t.metrics.decryptOperationsTotal.With(req.KeyID, "authz_denied").Inc()
		t.metrics.errorsByType.With("authz_error").Inc()
		return nil, err
	}

	// Track key usage for batch operation (if tracker configured)
	if t.safetyTracker != nil {
		// Calculate total bytes in the batch
		var totalBytes uint64
		for _, item := range req.Items {
			totalBytes += uint64(len(item.Ciphertext))
		}

		// Check safety limits before processing batch
		if err := t.safetyTracker.RecordKeyEncrypt(ctx, scopedKeyID, totalBytes); err != nil {
			t.metrics.decryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
			t.metrics.errorsByType.With("safety_limit_error").Inc()
			return nil, &errors.QBError{
				Code: errors.ErrDecryption,
				Op:   "crypto.DecryptBatch",
				Err:  err,
			}
		}
	}

	// Extract ciphertexts from items
	ciphertexts := make([][]byte, len(req.Items))
	for i, item := range req.Items {
		ciphertexts[i] = item.Ciphertext
	}

	// Perform batch decryption
	plaintexts, err := t.cryptoManager.DecryptBatch(ctx, scopedKeyID, ciphertexts, nil)
	if err != nil {
		t.metrics.decryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("decrypt_batch_error").Inc()
		return nil, keyOperationError(errors.ErrDecryption, "crypto.DecryptBatch", "decrypt batch", err)
	}

	// Build results
	results := make([]BatchDecryptResult, len(req.Items))
	for i, plaintext := range plaintexts {
		results[i] = BatchDecryptResult{
			Plaintext: plaintext,
		}
	}

	resp := &DecryptBatchResponse{
		KeyID:     req.KeyID,
		Results:   results,
		Timestamp: time.Now(),
	}

	t.metrics.decryptOperationsTotal.With(req.KeyID, "success").Inc()

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"key_id":     req.KeyID,
			"batch_size": len(req.Items),
		}).Debug("Decrypted batch successfully")
	}

	return resp, nil
}

// =============================================================================
// Data Key Generation (Envelope Encryption)
// =============================================================================

// GenerateDataKey generates a new data encryption key (DEK) for envelope encryption.
func (t *cryptoService) GenerateDataKey(ctx context.Context, req *GenerateDataKeyRequest) (*GenerateDataKeyResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("generate_data_key").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateGenerateDataKeyRequest(req); err != nil {
		keyID := "unknown"
		if req != nil {
			keyID = req.KeyID
		}
		t.metrics.encryptOperationsTotal.With(keyID, "invalid").Inc()
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	scopedKeyID, err := scopeKeyID(ctx, req.KeyID)
	if err != nil {
		return nil, err
	}

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2).
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "generate_data_key"); err != nil {
		t.metrics.encryptOperationsTotal.With(req.KeyID, "authz_denied").Inc()
		t.metrics.errorsByType.With("authz_error").Inc()
		return nil, err
	}

	// Determine key size from KeyAttributes (default to 32 bytes for AES-256)
	keyBytes := keySizeFromAttributes(req.KeyAttributes)

	// Track key usage for DEK generation (if tracker configured)
	// The wrapped DEK is what gets encrypted with the KEK
	if t.safetyTracker != nil {
		if err := t.safetyTracker.RecordKeyEncrypt(ctx, scopedKeyID, conv.ClampIntToUint64(keyBytes)); err != nil {
			t.metrics.encryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
			t.metrics.errorsByType.With("safety_limit_error").Inc()
			return nil, &errors.QBError{
				Code: errors.ErrEncryption,
				Op:   "crypto.GenerateDataKey",
				Err:  err,
			}
		}
	}

	// Generate data key using KMS
	// KeyAttributes.CN specifies the KEK to use for wrapping
	kekAttrs := &types.KeyAttributes{CN: scopedKeyID}
	plaintext, wrapped, err := t.kmsManager.GenerateDataKey(ctx, kekAttrs, keyBytes)
	if err != nil {
		t.metrics.encryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("generate_data_key_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.GenerateDataKey", "generate data key", err)
	}

	resp := &GenerateDataKeyResponse{
		KeyID:          req.KeyID,
		Plaintext:      plaintext,
		CiphertextBlob: wrapped,
		Timestamp:      time.Now(),
	}

	t.metrics.encryptOperationsTotal.With(req.KeyID, "success").Inc()

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"key_id":   req.KeyID,
			"key_size": keyBytes,
		}).Debug("Generated data key successfully")
	}

	return resp, nil
}

// GenerateWrappedDataKey generates a new encrypted data encryption key without returning plaintext.
func (t *cryptoService) GenerateWrappedDataKey(ctx context.Context, req *GenerateWrappedDataKeyRequest) (*GenerateWrappedDataKeyResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("generate_wrapped_data_key").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateGenerateWrappedDataKeyRequest(req); err != nil {
		keyID := "unknown"
		if req != nil {
			keyID = req.KeyID
		}
		t.metrics.encryptOperationsTotal.With(keyID, "invalid").Inc()
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	scopedKeyID, err := scopeKeyID(ctx, req.KeyID)
	if err != nil {
		return nil, err
	}

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2).
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "generate_data_key"); err != nil {
		t.metrics.encryptOperationsTotal.With(req.KeyID, "authz_denied").Inc()
		t.metrics.errorsByType.With("authz_error").Inc()
		return nil, err
	}

	// Determine key size from KeyAttributes (default to 32 bytes for AES-256)
	keyBytes := keySizeFromAttributes(req.KeyAttributes)

	// Track key usage for wrapped DEK generation (if tracker configured)
	if t.safetyTracker != nil {
		if err := t.safetyTracker.RecordKeyEncrypt(ctx, scopedKeyID, conv.ClampIntToUint64(keyBytes)); err != nil {
			t.metrics.encryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
			t.metrics.errorsByType.With("safety_limit_error").Inc()
			return nil, &errors.QBError{
				Code: errors.ErrEncryption,
				Op:   "crypto.GenerateWrappedDataKey",
				Err:  err,
			}
		}
	}

	// Generate data key and discard plaintext
	// KeyAttributes.CN specifies the KEK to use for wrapping
	kekAttrs := &types.KeyAttributes{CN: scopedKeyID}
	_, wrapped, err := t.kmsManager.GenerateDataKey(ctx, kekAttrs, keyBytes)
	if err != nil {
		t.metrics.encryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("generate_wrapped_data_key_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.GenerateWrappedDataKey", "generate wrapped data key", err)
	}

	resp := &GenerateWrappedDataKeyResponse{
		KeyID:          req.KeyID,
		CiphertextBlob: wrapped,
		Timestamp:      time.Now(),
	}

	t.metrics.encryptOperationsTotal.With(req.KeyID, "success").Inc()

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"key_id":   req.KeyID,
			"key_size": keyBytes,
		}).Debug("Generated wrapped data key successfully")
	}

	return resp, nil
}

// =============================================================================
// Signing and Verification Operations
// =============================================================================

// Sign creates a digital signature of data using a named signing key.
func (t *cryptoService) Sign(ctx context.Context, req *SignRequest) (*SignResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("sign").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateSignRequest(req); err != nil {
		keyID := "unknown"
		if req != nil {
			keyID = req.KeyID
		}
		t.metrics.signOperationsTotal.With(keyID, "invalid").Inc()
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	scopedKeyID, err := scopeKeyID(ctx, req.KeyID)
	if err != nil {
		return nil, err
	}

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2).
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "sign"); err != nil {
		t.metrics.signOperationsTotal.With(req.KeyID, "authz_denied").Inc()
		t.metrics.errorsByType.With("authz_error").Inc()
		return nil, err
	}

	// Default to SHA-256 if not specified
	hashAlg := string(req.HashAlgorithm)
	if hashAlg == "" {
		hashAlg = "SHA256"
	}

	// Perform signing (message is not pre-hashed)
	signature, err := t.cryptoManager.Sign(ctx, scopedKeyID, req.Message, hashAlg, req.PreHashed)
	if err != nil {
		t.metrics.signOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("sign_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.Sign", "sign data", err)
	}

	resp := &SignResponse{
		KeyID:         req.KeyID,
		KeyVersion:    1,
		Signature:     signature,
		HashAlgorithm: req.HashAlgorithm,
		Timestamp:     time.Now(),
	}

	t.metrics.signOperationsTotal.With(req.KeyID, "success").Inc()

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"key_id":         req.KeyID,
			"hash_algorithm": hashAlg,
			"message_size":   len(req.Message),
		}).Debug("Signed data successfully")
	}

	return resp, nil
}

// Verify verifies a digital signature against data using a named key.
func (t *cryptoService) Verify(ctx context.Context, req *VerifyRequest) (*VerifyResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("verify").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateVerifyRequest(req); err != nil {
		keyID := "unknown"
		if req != nil {
			keyID = req.KeyID
		}
		t.metrics.verifyOperationsTotal.With(keyID, "invalid").Inc()
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	scopedKeyID, err := scopeKeyID(ctx, req.KeyID)
	if err != nil {
		return nil, err
	}

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2).
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "verify"); err != nil {
		t.metrics.verifyOperationsTotal.With(req.KeyID, "authz_denied").Inc()
		t.metrics.errorsByType.With("authz_error").Inc()
		return nil, err
	}

	// Default to SHA-256 if not specified
	hashAlg := string(req.HashAlgorithm)
	if hashAlg == "" {
		hashAlg = "SHA256"
	}

	// Perform verification
	valid, err := t.cryptoManager.Verify(ctx, scopedKeyID, req.Message, req.Signature, hashAlg)
	if err != nil {
		t.metrics.verifyOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("verify_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.Verify", "verify signature", err)
	}

	resp := &VerifyResponse{
		KeyID:      req.KeyID,
		KeyVersion: req.KeyVersion,
		Valid:      valid,
		Timestamp:  time.Now(),
	}

	status := "success"
	if !valid {
		status = "invalid_signature"
	}
	t.metrics.verifyOperationsTotal.With(req.KeyID, status).Inc()

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"key_id":         req.KeyID,
			"hash_algorithm": hashAlg,
			"valid":          valid,
		}).Debug("Verified signature")
	}

	return resp, nil
}

// =============================================================================
// HMAC Operations
// =============================================================================

// GenerateHMAC generates an HMAC (Hash-based Message Authentication Code) of data.
func (t *cryptoService) GenerateHMAC(ctx context.Context, req *HMACRequest) (*HMACResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("generate_hmac").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateHMACRequest(req); err != nil {
		keyID := "unknown"
		if req != nil {
			keyID = req.KeyID
		}
		t.metrics.hmacOperationsTotal.With(keyID, "invalid").Inc()
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	scopedKeyID, err := scopeKeyID(ctx, req.KeyID)
	if err != nil {
		return nil, err
	}

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2).
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "sign"); err != nil {
		t.metrics.hmacOperationsTotal.With(req.KeyID, "authz_denied").Inc()
		t.metrics.errorsByType.With("authz_error").Inc()
		return nil, err
	}

	// Default to SHA-256 if not specified
	hashAlg := string(req.HashAlgorithm)
	if hashAlg == "" {
		hashAlg = "SHA256"
	}

	// Generate HMAC
	hmac, err := t.cryptoManager.GenerateHMAC(ctx, scopedKeyID, req.Message, hashAlg)
	if err != nil {
		t.metrics.hmacOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("generate_hmac_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.GenerateHMAC", "generate HMAC", err)
	}

	resp := &HMACResponse{
		KeyID:         req.KeyID,
		KeyVersion:    1,
		HMAC:          hmac,
		HashAlgorithm: req.HashAlgorithm,
		Timestamp:     time.Now(),
	}

	t.metrics.hmacOperationsTotal.With(req.KeyID, "success").Inc()

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"key_id":         req.KeyID,
			"hash_algorithm": hashAlg,
			"message_size":   len(req.Message),
		}).Debug("Generated HMAC successfully")
	}

	return resp, nil
}

// VerifyHMAC verifies an HMAC against data using a named key.
func (t *cryptoService) VerifyHMAC(ctx context.Context, req *VerifyHMACRequest) (*VerifyHMACResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("verify_hmac").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateVerifyHMACRequest(req); err != nil {
		keyID := "unknown"
		if req != nil {
			keyID = req.KeyID
		}
		t.metrics.hmacOperationsTotal.With(keyID, "invalid").Inc()
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	scopedKeyID, err := scopeKeyID(ctx, req.KeyID)
	if err != nil {
		return nil, err
	}

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2).
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "verify"); err != nil {
		t.metrics.hmacOperationsTotal.With(req.KeyID, "authz_denied").Inc()
		t.metrics.errorsByType.With("authz_error").Inc()
		return nil, err
	}

	// Default to SHA-256 if not specified
	hashAlg := string(req.HashAlgorithm)
	if hashAlg == "" {
		hashAlg = "SHA256"
	}

	// Verify HMAC
	valid, err := t.cryptoManager.VerifyHMAC(ctx, scopedKeyID, req.Message, req.HMAC, hashAlg)
	if err != nil {
		t.metrics.hmacOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("verify_hmac_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.VerifyHMAC", "verify HMAC", err)
	}

	resp := &VerifyHMACResponse{
		KeyID:      req.KeyID,
		KeyVersion: req.KeyVersion,
		Valid:      valid,
		Timestamp:  time.Now(),
	}

	status := "success"
	if !valid {
		status = "invalid_hmac"
	}
	t.metrics.hmacOperationsTotal.With(req.KeyID, status).Inc()

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"key_id":         req.KeyID,
			"hash_algorithm": hashAlg,
			"valid":          valid,
		}).Debug("Verified HMAC")
	}

	return resp, nil
}

// =============================================================================
// Utility Operations
// =============================================================================

// GenerateRandom generates high-quality random bytes.
func (t *cryptoService) GenerateRandom(ctx context.Context, req *GenerateRandomRequest) (*GenerateRandomResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("generate_random").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateGenerateRandomRequest(req); err != nil {
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	// Generate random bytes
	randomBytes, err := t.cryptoManager.GenerateRandom(ctx, req.NumBytes)
	if err != nil {
		t.metrics.errorsByType.With("generate_random_error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrEncryption,
			Op:   "crypto.GenerateRandom",
			Err:  &api.OperationFailedError{Operation: "generate random bytes", Cause: err},
		}
	}

	resp := &GenerateRandomResponse{
		RandomBytes: randomBytes,
		Timestamp:   time.Now(),
	}

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"num_bytes": req.NumBytes,
		}).Debug("Generated random bytes successfully")
	}

	return resp, nil
}

// Hash computes a cryptographic hash of data.
func (t *cryptoService) Hash(ctx context.Context, req *HashRequest) (*HashResponse, error) {
	start := time.Now()
	defer func() {
		t.metrics.operationDuration.With("hash").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := t.validateHashRequest(req); err != nil {
		t.metrics.errorsByType.With("validation_error").Inc()
		return nil, err
	}

	// Compute hash
	hashValue, err := t.cryptoManager.Hash(ctx, req.Data, string(req.Algorithm))
	if err != nil {
		t.metrics.errorsByType.With("hash_error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrEncryption,
			Op:   "crypto.Hash",
			Err:  &api.OperationFailedError{Operation: "compute hash", Cause: err},
		}
	}

	resp := &HashResponse{
		Algorithm: req.Algorithm,
		Hash:      hashValue,
		Timestamp: time.Now(),
	}

	if t.logger != nil {
		t.logger.WithFields(map[string]interface{}{
			"algorithm": req.Algorithm,
			"data_size": len(req.Data),
		}).Debug("Computed hash successfully")
	}

	return resp, nil
}

// =============================================================================
// Validation Helpers
// =============================================================================

func (t *cryptoService) validateEncryptRequest(req *EncryptRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateEncryptRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateEncryptRequest",
			Err:  &api.RequiredFieldError{Field: "key_id"},
		}
	}
	if len(req.Plaintext) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateEncryptRequest",
			Err:  &api.EmptyCollectionError{Field: "plaintext"},
		}
	}
	return nil
}

func (t *cryptoService) validateDecryptRequest(req *DecryptRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateDecryptRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateDecryptRequest",
			Err:  &api.RequiredFieldError{Field: "key_id"},
		}
	}
	if len(req.Ciphertext) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateDecryptRequest",
			Err:  &api.EmptyCollectionError{Field: "ciphertext"},
		}
	}
	return nil
}

func (t *cryptoService) validateRewrapRequest(req *RewrapRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateRewrapRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateRewrapRequest",
			Err:  &api.RequiredFieldError{Field: "key_id"},
		}
	}
	if len(req.Ciphertext) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateRewrapRequest",
			Err:  &api.EmptyCollectionError{Field: "ciphertext"},
		}
	}
	return nil
}

func (t *cryptoService) validateEncryptBatchRequest(req *EncryptBatchRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateEncryptBatchRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateEncryptBatchRequest",
			Err:  &api.RequiredFieldError{Field: "key_id"},
		}
	}
	if len(req.Items) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateEncryptBatchRequest",
			Err:  &api.EmptyCollectionError{Field: "items"},
		}
	}
	return nil
}

func (t *cryptoService) validateDecryptBatchRequest(req *DecryptBatchRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateDecryptBatchRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateDecryptBatchRequest",
			Err:  &api.RequiredFieldError{Field: "key_id"},
		}
	}
	if len(req.Items) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateDecryptBatchRequest",
			Err:  &api.EmptyCollectionError{Field: "items"},
		}
	}
	return nil
}

func (t *cryptoService) validateGenerateDataKeyRequest(req *GenerateDataKeyRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateGenerateDataKeyRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateGenerateDataKeyRequest",
			Err:  &api.RequiredFieldError{Field: "key_id"},
		}
	}
	return nil
}

func (t *cryptoService) validateGenerateWrappedDataKeyRequest(req *GenerateWrappedDataKeyRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateGenerateWrappedDataKeyRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateGenerateWrappedDataKeyRequest",
			Err:  &api.RequiredFieldError{Field: "key_id"},
		}
	}
	return nil
}

func (t *cryptoService) validateSignRequest(req *SignRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateSignRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateSignRequest",
			Err:  &api.RequiredFieldError{Field: "key_id"},
		}
	}
	if len(req.Message) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateSignRequest",
			Err:  &api.EmptyCollectionError{Field: "message"},
		}
	}
	return nil
}

func (t *cryptoService) validateVerifyRequest(req *VerifyRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateVerifyRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateVerifyRequest",
			Err:  &api.RequiredFieldError{Field: "key_id"},
		}
	}
	if len(req.Message) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateVerifyRequest",
			Err:  &api.EmptyCollectionError{Field: "message"},
		}
	}
	if len(req.Signature) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateVerifyRequest",
			Err:  &api.EmptyCollectionError{Field: "signature"},
		}
	}
	return nil
}

func (t *cryptoService) validateHMACRequest(req *HMACRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateHMACRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateHMACRequest",
			Err:  &api.RequiredFieldError{Field: "key_id"},
		}
	}
	if len(req.Message) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateHMACRequest",
			Err:  &api.EmptyCollectionError{Field: "message"},
		}
	}
	return nil
}

func (t *cryptoService) validateVerifyHMACRequest(req *VerifyHMACRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateVerifyHMACRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.KeyID == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateVerifyHMACRequest",
			Err:  &api.RequiredFieldError{Field: "key_id"},
		}
	}
	if len(req.Message) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateVerifyHMACRequest",
			Err:  &api.EmptyCollectionError{Field: "message"},
		}
	}
	if len(req.HMAC) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateVerifyHMACRequest",
			Err:  &api.EmptyCollectionError{Field: "hmac"},
		}
	}
	return nil
}

func (t *cryptoService) validateGenerateRandomRequest(req *GenerateRandomRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateGenerateRandomRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if req.NumBytes <= 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateGenerateRandomRequest",
			Err:  &api.InvalidInputError{Message: "num_bytes must be positive"},
		}
	}
	if req.NumBytes > 1024*1024 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateGenerateRandomRequest",
			Err:  &api.InvalidInputError{Message: "num_bytes cannot exceed 1MB"},
		}
	}
	return nil
}

func (t *cryptoService) validateHashRequest(req *HashRequest) error {
	if req == nil {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateHashRequest",
			Err:  &api.NilInputError{Name: "request"},
		}
	}
	if len(req.Data) == 0 {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateHashRequest",
			Err:  &api.EmptyCollectionError{Field: "data"},
		}
	}
	if req.Algorithm == "" {
		return &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "crypto.validateHashRequest",
			Err:  &api.RequiredFieldError{Field: "algorithm"},
		}
	}
	return nil
}

// keySizeFromAttributes extracts the key size in bytes from KeyAttributes.
// Returns default size based on the symmetric algorithm specified.
func keySizeFromAttributes(attrs *types.KeyAttributes) int {
	if attrs == nil {
		return 32 // Default to AES-256 (32 bytes)
	}

	switch attrs.SymmetricAlgorithm {
	case types.SymmetricAES128GCM:
		return 16
	case types.SymmetricAES192GCM:
		return 24
	case types.SymmetricAES256GCM:
		return 32
	case types.SymmetricChaCha20Poly1305, types.SymmetricXChaCha20Poly1305:
		return 32
	default:
		return 32 // Default to 32 bytes (256 bits)
	}
}
