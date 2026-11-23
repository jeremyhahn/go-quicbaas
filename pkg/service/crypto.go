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
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"sync"
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

// isKeyNotFound reports whether a key lookup failed because the key does not
// exist (in the caller's scope), as opposed to failing to be read.
func isKeyNotFound(err error) bool {
	return stderrors.Is(err, kms.ErrKeyNotFound) || errors.IsNotFound(err)
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

	// Check, count and seal under one key version.
	var ciphertext []byte
	stage, err := t.sealAtCheckedVersion(ctx, scopedKeyID, "service.Encrypt", 1, uint64(len(req.Plaintext)), func() ([][]byte, error) {
		var sealErr error
		ciphertext, sealErr = t.cryptoManager.Encrypt(ctx, scopedKeyID, req.Plaintext, req.Context)
		return [][]byte{ciphertext}, sealErr
	})
	switch stage {
	case 0:
	case sealStageVersion:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "version_disabled").Inc()
		t.metrics.errorsByType.With("key_version_error").Inc()
		return nil, err
	case sealStageLimit:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
		t.metrics.errorsByType.With("safety_limit_error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrEncryption,
			Op:   "crypto.Encrypt",
			Err:  err,
		}
	case sealStageSeal:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("encrypt_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.Encrypt", "encrypt data", err)
	default:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "version_moved").Inc()
		t.metrics.errorsByType.With("key_version_error").Inc()
		return nil, WrapError(err, errors.ErrUnavailable, "service.Encrypt", "key version changed during the operation")
	}

	keyVersion := t.keyVersion(ctx, scopedKeyID, ciphertext)

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

// keyVersion returns the key version the first decodable ciphertext records
// or, when none records one, the current version of scopedKeyID as the KMS
// manager reports it. It returns 1 when no KMS manager is configured, the lookup
// fails, or the metadata carries no version.
func (t *cryptoService) keyVersion(ctx context.Context, scopedKeyID string, ciphertexts ...[]byte) uint64 {
	// The version a ciphertext records is the one that produced it: another
	// node can rotate the key between encryption and this lookup, and the
	// durable current version would then name a version the ciphertext was
	// not encrypted under.
	for _, ciphertext := range ciphertexts {
		if v, err := kms.CiphertextKeyVersion(ciphertext); err == nil {
			return v
		}
	}
	version, err := t.currentKeyVersion(ctx, scopedKeyID)
	if err != nil {
		return 1
	}
	return version
}

// currentKeyVersion returns the key's current version, 1 when the KMS manager
// records none, and the KMS manager's lookup error.
func (t *cryptoService) currentKeyVersion(ctx context.Context, scopedKeyID string) (uint64, error) {
	var version uint64 = 1
	if t.kmsManager == nil {
		return version, nil
	}
	// The durable record, not this node's cached metadata: another node's
	// rotation does not invalidate the cache, and a stale version would gate
	// encryption on a version that has since been rotated out and disabled.
	if cv, ok := t.kmsManager.(interface {
		CurrentKeyVersion(ctx context.Context, keyID string) (int, error)
	}); ok {
		v, err := cv.CurrentKeyVersion(ctx, scopedKeyID)
		if err != nil {
			return 0, err
		}
		if v > 0 {
			version = conv.ClampIntToUint64(v)
		}
		return version, nil
	}
	keyInfo, err := t.kmsManager.GetKey(ctx, scopedKeyID)
	if err != nil {
		return 0, err
	}
	switch info := keyInfo.(type) {
	case *kms.KeyMetadata:
		if info.Version > 0 {
			version = conv.ClampIntToUint64(info.Version)
		}
	case map[string]interface{}:
		if v, ok := info["version"].(uint64); ok {
			version = v
		}
	}
	return version, nil
}

// checkVersionUsable refuses a ciphertext whose key version is no longer
// usable: once a rotated-out version's grace period ends it is disabled, and
// data sealed under it must not decrypt. A version with no recorded status is
// in its default (enabled) state, as KMSService treats it.
func (t *cryptoService) checkVersionUsable(ctx context.Context, scopedKeyID string, ciphertext []byte) error {
	if t.kmsSvc == nil {
		return nil
	}
	version, err := kms.CiphertextKeyVersion(ciphertext)
	if stderrors.Is(err, kms.ErrInvalidCiphertextKeyVersion) {
		// A version that is not a positive integer names no key version: refuse it rather than check another version's status.
		return WrapError(err, errors.ErrInvalidInput, "service.Decrypt", "ciphertext key version is invalid")
	}
	if err != nil {
		// A ciphertext that does not decode names no version; the crypto backend rejects ciphertext it cannot decode, so its own error is what the caller sees.
		return nil
	}
	status, _, err := t.kmsSvc.lookupVersionStatus(ctx, scopedKeyID, version)
	if err != nil {
		// A status that cannot be read may be a disabled one: refuse rather than decrypt under it.
		return WrapError(err, errors.ErrUnavailable, "service.Decrypt", "key version status unavailable")
	}
	switch status {
	case "", KeyStatusEnabled, KeyStatusRotating:
		return nil
	default:
		return api.NewKeyDisabledError("service.Decrypt", scopedKeyID)
	}
}

// checkCurrentVersionUsable refuses an operation that would encrypt under the
// key's current version when that version is not usable, so nothing is sealed
// or wrapped under a version the Decrypt gate would then refuse. A version
// whose status cannot be read is refused too.
func (t *cryptoService) checkCurrentVersionUsable(ctx context.Context, scopedKeyID, op string) (uint64, error) {
	if t.kmsSvc == nil {
		return 0, nil
	}
	version, err := t.currentKeyVersion(ctx, scopedKeyID)
	if err != nil {
		if isKeyNotFound(err) {
			// A key that does not exist is a permanent NotFound, not an outage to retry.
			return 0, keyOperationError(errors.ErrNotFound, op, "get key version", err)
		}
		return 0, WrapError(err, errors.ErrUnavailable, op, "key version unavailable")
	}
	status, _, err := t.kmsSvc.lookupVersionStatus(ctx, scopedKeyID, version)
	if err != nil {
		return 0, WrapError(err, errors.ErrUnavailable, op, "key version status unavailable")
	}
	switch status {
	case "", KeyStatusEnabled, KeyStatusRotating:
		return version, nil
	default:
		return 0, api.NewKeyDisabledError(op, scopedKeyID)
	}
}

// sealVersionAttempts bounds the retries of an encrypting operation whose
// key version moved between its status check and its seal.
const sealVersionAttempts = 3

// errKeyVersionMoved: the key was rotated during every attempt of an operation.
var errKeyVersionMoved = stderrors.New("key version changed during the operation")

// sealStage names the step at which sealAtCheckedVersion failed.
type sealStage int

const (
	sealStageVersion sealStage = iota + 1 // status check refused or unavailable
	sealStageLimit                        // AEAD usage limit
	sealStageSeal                         // the seal itself
	sealStageMoved                        // the version kept moving
)

// sealAtCheckedVersion checks the key's current version, counts the
// operation (ops uses over nbytes bytes) against it, and runs seal, so that
// what seal returns is sealed under the version whose status was checked and
// against which the use was counted. The key's read lock is held from before
// the use is recorded until seal returns (AEADSafetyTracker.KeyLock), so a
// concurrent rotation cannot reset the usage recorded. Output sealed under
// another version (a rotation landed in between) is discarded: its use is
// counted against the version it consumed and the operation is retried, up
// to sealVersionAttempts times. On failure the returned stage names the step
// that failed.
func (t *cryptoService) sealAtCheckedVersion(ctx context.Context, scopedKeyID, op string, ops, nbytes uint64, seal func() ([][]byte, error)) (sealStage, error) {
	for attempt := 0; attempt < sealVersionAttempts; attempt++ {
		v, err := t.checkCurrentVersionUsable(ctx, scopedKeyID, op)
		if err != nil {
			return sealStageVersion, err
		}
		var lock *sync.RWMutex
		if t.safetyTracker != nil {
			// A newer current version than the usage was counted under is a
			// rotation (possibly on another node): the budget starts over.
			t.safetyTracker.ObserveKeyVersion(scopedKeyID, v)
			lock = t.safetyTracker.KeyLock(scopedKeyID)
			lock.RLock()
			if err := t.safetyTracker.RecordKeyEncryptN(ctx, scopedKeyID, ops, nbytes); err != nil {
				lock.RUnlock()
				return sealStageLimit, err
			}
		}
		out, err := seal()
		if lock != nil {
			lock.RUnlock()
		}
		if err != nil {
			return sealStageSeal, err
		}
		cv, moved := sealedVersionMoved(v, out)
		if !moved {
			return 0, nil
		}
		if cv > v && t.safetyTracker != nil {
			// The discarded output consumed version cv: count it there. A
			// cv below v was already counted against v.
			t.safetyTracker.ObserveKeyVersion(scopedKeyID, cv)
			lock = t.safetyTracker.KeyLock(scopedKeyID)
			lock.RLock()
			err := t.safetyTracker.RecordKeyEncryptN(ctx, scopedKeyID, ops, nbytes)
			lock.RUnlock()
			if err != nil {
				return sealStageLimit, err
			}
		}
	}
	return sealStageMoved, errKeyVersionMoved
}

// sealedVersionMoved reports the first version recorded by out that differs
// from the checked version v. Nothing moved when v is 0 (no KMS service:
// nothing was checked); output that records no version is skipped, as
// keyVersion does.
func sealedVersionMoved(v uint64, out [][]byte) (uint64, bool) {
	if v == 0 {
		return 0, false
	}
	for _, ciphertext := range out {
		cv, err := kms.CiphertextKeyVersion(ciphertext)
		if err != nil {
			continue
		}
		if cv != v {
			return cv, true
		}
	}
	return 0, false
}

// cryptoItemFunc encrypts or decrypts one item under keyID bound to encContext.
type cryptoItemFunc func(ctx context.Context, keyID string, data, encContext []byte) ([]byte, error)

// cryptoBatchFunc encrypts or decrypts every item under keyID bound to one
// shared encContext.
type cryptoBatchFunc func(ctx context.Context, keyID string, items [][]byte, encContext []byte) ([][]byte, error)

// processBatchItems runs data[i] through the crypto manager bound to
// contexts[i]. When every context is byte-equal (all empty included) it makes
// one batch call with that context; otherwise it processes each item with its
// own context. Any item failure fails the whole batch with no partial result.
func processBatchItems(ctx context.Context, keyID string, data, contexts [][]byte, batch cryptoBatchFunc, single cryptoItemFunc) ([][]byte, error) {
	if shared, ok := sharedBatchContext(contexts); ok {
		return batch(ctx, keyID, data, shared)
	}
	results := make([][]byte, len(data))
	for i := range data {
		result, err := single(ctx, keyID, data[i], contexts[i])
		if err != nil {
			return nil, err
		}
		results[i] = result
	}
	return results, nil
}

// sharedBatchContext returns the context every item shares, and false when
// the items' contexts differ. A nil and an empty context are equal.
func sharedBatchContext(contexts [][]byte) ([]byte, bool) {
	if len(contexts) == 0 {
		return nil, true
	}
	for _, c := range contexts[1:] {
		if !bytes.Equal(c, contexts[0]) {
			return nil, false
		}
	}
	return contexts[0], true
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

	// The backend decrypts under the version the ciphertext names, so that is
	// the version reported, and an explicit version that differs is refused.
	keyVersion := req.KeyVersion
	if v, verr := kms.CiphertextKeyVersion(req.Ciphertext); verr == nil {
		if req.KeyVersion != 0 && req.KeyVersion != v {
			t.metrics.decryptOperationsTotal.With(req.KeyID, "validation_error").Inc()
			t.metrics.errorsByType.With("validation_error").Inc()
			return nil, NewError(errors.ErrInvalidInput, "crypto.Decrypt",
				fmt.Sprintf("key version %d does not match the ciphertext's key version %d", req.KeyVersion, v))
		}
		keyVersion = v
	}
	if keyVersion == 0 {
		keyVersion = 1
	}

	if err := t.checkVersionUsable(ctx, scopedKeyID, req.Ciphertext); err != nil {
		t.metrics.decryptOperationsTotal.With(req.KeyID, "version_disabled").Inc()
		t.metrics.errorsByType.With("key_version_error").Inc()
		return nil, err
	}

	// Track key usage before decryption (if tracker configured)
	// Note: For decryption we track the ciphertext size as an approximation
	if t.safetyTracker != nil {
		// Held until the operation returns, so a concurrent rotation cannot
		// reset the usage this operation records (AEADSafetyTracker.KeyLock).
		keyLock := t.safetyTracker.KeyLock(scopedKeyID)
		keyLock.RLock()
		defer keyLock.RUnlock()
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

	// Per-key authorization check (NIST 800-53 AC-3/AC-6, AU-2). A rewrap
	// decrypts under the source key version and encrypts under the
	// destination; both are versions of the one key the request names, and a
	// per-key check names a key, not a version, so both checks run on it.
	for _, operation := range []string{"decrypt", "encrypt"} {
		if err := t.checkKeyAuthz(ctx, scopedKeyID, operation); err != nil {
			t.metrics.encryptOperationsTotal.With(req.KeyID, "authz_denied").Inc()
			t.metrics.errorsByType.With("authz_error").Inc()
			return nil, err
		}
	}

	// Check, count and seal under one key version.
	var newCiphertext []byte
	stage, err := t.sealAtCheckedVersion(ctx, scopedKeyID, "service.Rewrap", 1, uint64(len(req.Ciphertext)), func() ([][]byte, error) {
		var sealErr error
		newCiphertext, sealErr = t.cryptoManager.Rewrap(ctx, scopedKeyID, req.Ciphertext, req.Context)
		return [][]byte{newCiphertext}, sealErr
	})
	switch stage {
	case 0:
	case sealStageVersion:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "version_disabled").Inc()
		t.metrics.errorsByType.With("key_version_error").Inc()
		return nil, err
	case sealStageLimit:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
		t.metrics.errorsByType.With("safety_limit_error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrEncryption,
			Op:   "crypto.Rewrap",
			Err:  err,
		}
	case sealStageSeal:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("rewrap_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.Rewrap", "rewrap data", err)
	default:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "version_moved").Inc()
		t.metrics.errorsByType.With("key_version_error").Inc()
		return nil, WrapError(err, errors.ErrUnavailable, "service.Rewrap", "key version changed during the operation")
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

	// Extract plaintexts and their per-item contexts (AAD)
	plaintexts := make([][]byte, len(req.Items))
	contexts := make([][]byte, len(req.Items))
	for i, item := range req.Items {
		plaintexts[i] = item.Plaintext
		contexts[i] = item.Context
	}

	// Each item is one AEAD operation under the key.
	var totalBytes uint64
	for _, item := range req.Items {
		totalBytes += uint64(len(item.Plaintext))
	}

	// Check, count and perform batch encryption under one key version,
	// binding each item to its own context.
	var ciphertexts [][]byte
	stage, err := t.sealAtCheckedVersion(ctx, scopedKeyID, "service.EncryptBatch", uint64(len(req.Items)), totalBytes, func() ([][]byte, error) {
		var sealErr error
		ciphertexts, sealErr = processBatchItems(ctx, scopedKeyID, plaintexts, contexts, t.cryptoManager.EncryptBatch, t.cryptoManager.Encrypt)
		return ciphertexts, sealErr
	})
	switch stage {
	case 0:
	case sealStageVersion:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "version_disabled").Inc()
		t.metrics.errorsByType.With("key_version_error").Inc()
		return nil, err
	case sealStageLimit:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
		t.metrics.errorsByType.With("safety_limit_error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrEncryption,
			Op:   "crypto.EncryptBatch",
			Err:  err,
		}
	case sealStageSeal:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("encrypt_batch_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.EncryptBatch", "encrypt batch", err)
	default:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "version_moved").Inc()
		t.metrics.errorsByType.With("key_version_error").Inc()
		return nil, WrapError(err, errors.ErrUnavailable, "service.EncryptBatch", "key version changed during the operation")
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
		KeyVersion: t.keyVersion(ctx, scopedKeyID, ciphertexts...),
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

	// A disabled key version on any item fails the whole batch.
	for _, item := range req.Items {
		if err := t.checkVersionUsable(ctx, scopedKeyID, item.Ciphertext); err != nil {
			t.metrics.decryptOperationsTotal.With(req.KeyID, "version_disabled").Inc()
			t.metrics.errorsByType.With("key_version_error").Inc()
			return nil, err
		}
	}

	// Track key usage for batch operation (if tracker configured)
	if t.safetyTracker != nil {
		// Calculate total bytes in the batch
		var totalBytes uint64
		for _, item := range req.Items {
			totalBytes += uint64(len(item.Ciphertext))
		}

		// Check safety limits before processing batch
		// Each item is one AEAD operation under the key.
		// Held until the operation returns, so a concurrent rotation cannot
		// reset the usage this operation records (AEADSafetyTracker.KeyLock).
		keyLock := t.safetyTracker.KeyLock(scopedKeyID)
		keyLock.RLock()
		defer keyLock.RUnlock()
		if err := t.safetyTracker.RecordKeyDecryptN(ctx, scopedKeyID, uint64(len(req.Items)), totalBytes); err != nil {
			t.metrics.decryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
			t.metrics.errorsByType.With("safety_limit_error").Inc()
			return nil, &errors.QBError{
				Code: errors.ErrDecryption,
				Op:   "crypto.DecryptBatch",
				Err:  err,
			}
		}
	}

	// Extract ciphertexts and their per-item contexts (AAD)
	ciphertexts := make([][]byte, len(req.Items))
	contexts := make([][]byte, len(req.Items))
	for i, item := range req.Items {
		ciphertexts[i] = item.Ciphertext
		contexts[i] = item.Context
	}

	// Perform batch decryption, verifying each item against its own context
	plaintexts, err := processBatchItems(ctx, scopedKeyID, ciphertexts, contexts, t.cryptoManager.DecryptBatch, t.cryptoManager.Decrypt)
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

	// Check, count and generate the data key under one key version. The
	// wrapped DEK is what gets encrypted with the KEK; KeyAttributes.CN
	// specifies the KEK to use for wrapping.
	kekAttrs := &types.KeyAttributes{CN: scopedKeyID}
	var plaintext, wrapped []byte
	stage, err := t.sealAtCheckedVersion(ctx, scopedKeyID, "service.GenerateDataKey", 1, conv.ClampIntToUint64(keyBytes), func() ([][]byte, error) {
		// A previous attempt's plaintext DEK is discarded: wipe it.
		clear(plaintext)
		var sealErr error
		plaintext, wrapped, sealErr = t.kmsManager.GenerateDataKey(ctx, kekAttrs, keyBytes)
		return [][]byte{wrapped}, sealErr
	})
	switch stage {
	case 0:
	case sealStageVersion:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "version_disabled").Inc()
		t.metrics.errorsByType.With("key_version_error").Inc()
		return nil, err
	case sealStageLimit:
		clear(plaintext)
		t.metrics.encryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
		t.metrics.errorsByType.With("safety_limit_error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrEncryption,
			Op:   "crypto.GenerateDataKey",
			Err:  err,
		}
	case sealStageSeal:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("generate_data_key_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.GenerateDataKey", "generate data key", err)
	default:
		clear(plaintext)
		t.metrics.encryptOperationsTotal.With(req.KeyID, "version_moved").Inc()
		t.metrics.errorsByType.With("key_version_error").Inc()
		return nil, WrapError(err, errors.ErrUnavailable, "service.GenerateDataKey", "key version changed during the operation")
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

	// Check, count and generate the data key under one key version, and
	// discard the plaintext. KeyAttributes.CN specifies the KEK to use for
	// wrapping.
	kekAttrs := &types.KeyAttributes{CN: scopedKeyID}
	var wrapped []byte
	stage, err := t.sealAtCheckedVersion(ctx, scopedKeyID, "service.GenerateWrappedDataKey", 1, conv.ClampIntToUint64(keyBytes), func() ([][]byte, error) {
		var sealErr error
		_, wrapped, sealErr = t.kmsManager.GenerateDataKey(ctx, kekAttrs, keyBytes)
		return [][]byte{wrapped}, sealErr
	})
	switch stage {
	case 0:
	case sealStageVersion:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "version_disabled").Inc()
		t.metrics.errorsByType.With("key_version_error").Inc()
		return nil, err
	case sealStageLimit:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "safety_limit_exceeded").Inc()
		t.metrics.errorsByType.With("safety_limit_error").Inc()
		return nil, &errors.QBError{
			Code: errors.ErrEncryption,
			Op:   "crypto.GenerateWrappedDataKey",
			Err:  err,
		}
	case sealStageSeal:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "error").Inc()
		t.metrics.errorsByType.With("generate_wrapped_data_key_error").Inc()
		return nil, keyOperationError(errors.ErrEncryption, "crypto.GenerateWrappedDataKey", "generate wrapped data key", err)
	default:
		t.metrics.encryptOperationsTotal.With(req.KeyID, "version_moved").Inc()
		t.metrics.errorsByType.With("key_version_error").Inc()
		return nil, WrapError(err, errors.ErrUnavailable, "service.GenerateWrappedDataKey", "key version changed during the operation")
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
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "generate_mac"); err != nil {
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
	if err := t.checkKeyAuthz(ctx, scopedKeyID, "verify_mac"); err != nil {
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
