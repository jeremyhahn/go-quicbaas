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
// This file contains the AEAD safety tracking implementation per NIST SP 800-38D.
package service

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// =============================================================================
// AEAD Safety Configuration
// =============================================================================

// AEADSafetyConfig holds the safety limits per NIST SP 800-38D recommendations.
// These limits prevent cryptographic weaknesses from nonce reuse and key overuse.
type AEADSafetyConfig struct {
	// MaxOperations is the maximum encrypt/decrypt operations (NIST recommends 2^32)
	MaxOperations uint64 `json:"max_operations"`

	// MaxBytes is the maximum bytes to encrypt with a single key
	// NIST SP 800-38D recommends 2^39 bits (~64GB) per key
	MaxBytes uint64 `json:"max_bytes"`

	// WarningPercent triggers warnings at this percentage of limits (default: 80)
	WarningPercent int `json:"warning_percent"`

	// AutoRotatePercent triggers automatic rotation at this percentage (default: 95)
	AutoRotatePercent int `json:"auto_rotate_percent"`
}

// DefaultAEADSafetyConfig returns NIST-compliant safety defaults.
func DefaultAEADSafetyConfig() AEADSafetyConfig {
	return AEADSafetyConfig{
		MaxOperations:     1 << 32,      // 2^32 operations (4,294,967,296)
		MaxBytes:          375809638400, // ~350 GB (conservative)
		WarningPercent:    80,
		AutoRotatePercent: 95,
	}
}

// =============================================================================
// AEAD Usage Tracker
// =============================================================================

// AEADUsage tracks usage statistics for a key or DEK.
type AEADUsage struct {
	// OperationCount is the current number of operations
	OperationCount atomic.Uint64

	// BytesProcessed is the current bytes encrypted
	BytesProcessed atomic.Uint64

	// LastUpdated is the timestamp of the last operation
	LastUpdated atomic.Int64

	// Config is the safety configuration for this key. It is read and
	// written only under mu.
	Config AEADSafetyConfig

	// Version is the key version the counters belong to (0 when unknown).
	// It is read and written only under mu.
	Version uint64

	// mu serialises Config reads and writes with the counter add and limit
	// check of a reservation and with a counter reset, so a reservation never
	// sees a half-written config and a reset never lands between an add and
	// its limit check.
	mu sync.Mutex
}

// snapshot returns the counters and config of u as one consistent view.
func (u *AEADUsage) snapshot() (ops, bytes uint64, cfg AEADSafetyConfig) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.OperationCount.Load(), u.BytesProcessed.Load(), u.Config
}

// setConfig replaces u's config.
func (u *AEADUsage) setConfig(cfg AEADSafetyConfig) {
	u.mu.Lock()
	u.Config = cfg
	u.mu.Unlock()
}

// resetCounters zeroes u's operation and byte counters.
func (u *AEADUsage) resetCounters() {
	u.mu.Lock()
	u.OperationCount.Store(0)
	u.BytesProcessed.Store(0)
	u.mu.Unlock()
}

// retireLocked returns a copy of the counters of u's version and restarts
// u's counters for the next version; u.mu must be held.
func (u *AEADUsage) retireLocked() *AEADUsage {
	retired := &AEADUsage{Config: u.Config, Version: u.Version}
	retired.OperationCount.Store(u.OperationCount.Load())
	retired.BytesProcessed.Store(u.BytesProcessed.Load())
	retired.LastUpdated.Store(u.LastUpdated.Load())
	u.OperationCount.Store(0)
	u.BytesProcessed.Store(0)
	return retired
}

// usagePercentOf is the highest share of a configured limit that ops and
// bytes use under cfg.
func usagePercentOf(ops, bytes uint64, cfg AEADSafetyConfig) uint64 {
	var maxPercent uint64

	if cfg.MaxOperations > 0 {
		opsPercent := (ops * 100) / cfg.MaxOperations
		if opsPercent > maxPercent {
			maxPercent = opsPercent
		}
	}

	if cfg.MaxBytes > 0 {
		bytesPercent := (bytes * 100) / cfg.MaxBytes
		if bytesPercent > maxPercent {
			maxPercent = bytesPercent
		}
	}

	return maxPercent
}

// IncrementUsage atomically increments the usage counters.
// Returns true if the limits are exceeded after this operation.
func (u *AEADUsage) IncrementUsage(bytesProcessed uint64) (limitExceeded bool) {
	return u.IncrementUsageBy(1, bytesProcessed)
}

// IncrementUsageBy records operations AEAD operations processing
// bytesProcessed bytes in total. A batch reserves one operation per item so
// that it cannot bypass MaxOperations.
func (u *AEADUsage) IncrementUsageBy(operations, bytesProcessed uint64) (limitExceeded bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.incrementLocked(operations, bytesProcessed)
}

// incrementLocked adds to the counters and checks the limits; u.mu must be
// held.
func (u *AEADUsage) incrementLocked(operations, bytesProcessed uint64) (limitExceeded bool) {
	if operations == 0 {
		operations = 1
	}
	u.OperationCount.Add(operations)
	u.BytesProcessed.Add(bytesProcessed)
	u.LastUpdated.Store(time.Now().UnixNano())

	// Check if we've exceeded limits
	if u.Config.MaxOperations > 0 && u.OperationCount.Load() >= u.Config.MaxOperations {
		return true
	}
	if u.Config.MaxBytes > 0 && u.BytesProcessed.Load() >= u.Config.MaxBytes {
		return true
	}
	return false
}

// WarningNeeded checks if a warning should be generated.
func (u *AEADUsage) WarningNeeded() (bool, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Config.WarningPercent <= 0 {
		return false, ""
	}

	threshold := uint64(u.Config.WarningPercent) // #nosec G115 -- WarningPercent is bounded 0-100

	if u.Config.MaxOperations > 0 {
		currentPercent := (u.OperationCount.Load() * 100) / u.Config.MaxOperations
		if currentPercent >= threshold {
			return true, fmt.Sprintf("operation limit: %d%% used (%d/%d)",
				currentPercent, u.OperationCount.Load(), u.Config.MaxOperations)
		}
	}

	if u.Config.MaxBytes > 0 {
		currentPercent := (u.BytesProcessed.Load() * 100) / u.Config.MaxBytes
		if currentPercent >= threshold {
			return true, fmt.Sprintf("bytes limit: %d%% used (%d/%d)",
				currentPercent, u.BytesProcessed.Load(), u.Config.MaxBytes)
		}
	}

	return false, ""
}

// AutoRotateNeeded checks if automatic rotation should be triggered.
func (u *AEADUsage) AutoRotateNeeded() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.Config.AutoRotatePercent <= 0 {
		return false
	}

	threshold := uint64(u.Config.AutoRotatePercent) // #nosec G115 -- AutoRotatePercent is bounded 0-100

	if u.Config.MaxOperations > 0 {
		currentPercent := (u.OperationCount.Load() * 100) / u.Config.MaxOperations
		if currentPercent >= threshold {
			return true
		}
	}

	if u.Config.MaxBytes > 0 {
		currentPercent := (u.BytesProcessed.Load() * 100) / u.Config.MaxBytes
		if currentPercent >= threshold {
			return true
		}
	}

	return false
}

// UsagePercent returns the highest percentage of limit used.
func (u *AEADUsage) UsagePercent() uint64 {
	ops, bytes, cfg := u.snapshot()
	return usagePercentOf(ops, bytes, cfg)
}

// =============================================================================
// AEAD Safety Tracker Service
// =============================================================================

// AEADSafetyTracker tracks usage across all keys and DEKs.
type AEADSafetyTracker struct {
	// keyUsage maps key IDs to their usage trackers
	keyUsage syncMap[string, *AEADUsage]

	// dekUsage maps DEK paths to their usage trackers
	dekUsage syncMap[string, *AEADUsage]

	// keyConfigs stores per-key configuration overrides
	// This allows configurations to persist even if the usage tracker is reset
	keyConfigs syncMap[string, AEADSafetyConfig]

	// dekConfigs stores per-DEK configuration overrides
	dekConfigs syncMap[string, AEADSafetyConfig]

	// keyGates holds each key's rotation gate: crypto calls hold it for
	// reading (BeginKeyUse) and a rotation holds it for writing
	// (RotateKeyUsage).
	keyGates syncMap[string, *sync.RWMutex]

	// retiredKeyUsage holds the counters of key versions that are no longer
	// their key's latest (keyUsage holds the latest), keyed by
	// retiredUsageKey. They persist: a retained version stays decryptable and
	// its decrypts are still charged to it.
	retiredKeyUsage syncMap[string, *AEADUsage]

	// keyVersionSource, when set, reports a key's current version from
	// authoritative replicated state. Key usage is counted per version: a
	// rotation performed on any node starts the new version's count from
	// zero here once it is observed, and the old version keeps its count.
	keyVersionSource atomic.Pointer[func(ctx context.Context, keyID string) (uint64, error)]

	// serverConfig is the server-level default configuration
	serverConfig AEADSafetyConfig

	// enforceDefaults applies server defaults when no per-key config exists
	enforceDefaults bool

	// logger for warnings and events
	logger interfaces.Logger

	// metrics for observability
	metrics *aeadMetrics

	// rotationCallback is called when auto-rotation is needed
	rotationCallback func(ctx context.Context, keyID string, isDEK bool)

	// warningCallback is called when warning threshold is reached
	warningCallback func(ctx context.Context, keyID string, isDEK bool, message string)
}

// aeadMetrics holds Prometheus metrics for AEAD safety.
type aeadMetrics struct {
	operationsTotal *metrics.CounterVec
	bytesProcessed  *metrics.CounterVec
	limitWarnings   *metrics.CounterVec
	autoRotations   *metrics.CounterVec
	limitExceeded   *metrics.CounterVec
	usagePercent    *metrics.GaugeVec
}

// AEADSafetyTrackerOption configures the safety tracker.
type AEADSafetyTrackerOption func(*AEADSafetyTracker)

// WithServerConfig sets the server-level default configuration.
func WithServerConfig(config AEADSafetyConfig) AEADSafetyTrackerOption {
	return func(t *AEADSafetyTracker) {
		t.serverConfig = config
	}
}

// WithEnforceDefaults enables enforcement of server defaults.
func WithEnforceDefaults(enforce bool) AEADSafetyTrackerOption {
	return func(t *AEADSafetyTracker) {
		t.enforceDefaults = enforce
	}
}

// WithLogger sets the logger for warnings.
func WithAEADLogger(logger interfaces.Logger) AEADSafetyTrackerOption {
	return func(t *AEADSafetyTracker) {
		t.logger = logger
	}
}

// WithRotationCallback sets the callback for auto-rotation triggers.
func WithRotationCallback(cb func(ctx context.Context, keyID string, isDEK bool)) AEADSafetyTrackerOption {
	return func(t *AEADSafetyTracker) {
		t.rotationCallback = cb
	}
}

// WithWarningCallback sets the callback for warning triggers.
func WithWarningCallback(cb func(ctx context.Context, keyID string, isDEK bool, message string)) AEADSafetyTrackerOption {
	return func(t *AEADSafetyTracker) {
		t.warningCallback = cb
	}
}

// NewAEADSafetyTracker creates a new safety tracker.
func NewAEADSafetyTracker(opts ...AEADSafetyTrackerOption) *AEADSafetyTracker {
	t := &AEADSafetyTracker{
		serverConfig:    DefaultAEADSafetyConfig(),
		enforceDefaults: true,
	}

	for _, opt := range opts {
		opt(t)
	}

	t.initMetrics()
	return t
}

// initMetrics initializes Prometheus metrics.
func (t *AEADSafetyTracker) initMetrics() {
	t.metrics = &aeadMetrics{
		operationsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "aead_safety",
			Name:      "operations_total",
			Help:      "Total number of AEAD operations",
		}, []string{"key_id", "type"}),

		bytesProcessed: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "aead_safety",
			Name:      "bytes_processed_total",
			Help:      "Total bytes processed in AEAD operations",
		}, []string{"key_id", "type"}),

		limitWarnings: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "aead_safety",
			Name:      "limit_warnings_total",
			Help:      "Total number of limit warnings generated",
		}, []string{"key_id", "type"}),

		autoRotations: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "aead_safety",
			Name:      "auto_rotations_total",
			Help:      "Total number of auto-rotations triggered",
		}, []string{"key_id", "type"}),

		limitExceeded: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "aead_safety",
			Name:      "limit_exceeded_total",
			Help:      "Total number of times limits were exceeded",
		}, []string{"key_id", "type"}),

		usagePercent: metrics.NewGaugeVec(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "aead_safety",
			Name:      "usage_percent",
			Help:      "Current usage percentage of limits",
		}, []string{"key_id", "type"}),
	}
}

// RecordKeyEncrypt records an encryption operation for a key.
// The operation is charged to the key's current version.
func (t *AEADSafetyTracker) RecordKeyEncrypt(ctx context.Context, keyID string, bytesEncrypted uint64) error {
	return t.recordOperation(ctx, keyID, t.currentKeyVersion(ctx, keyID), 1, bytesEncrypted, false)
}

// RecordKeyVersionUse records operations under one version of a key: a
// decrypt is charged to the version its ciphertext is sealed under, an
// encrypt to the version its ciphertext was sealed under. Each version keeps
// its own counters, so a rotation never restores the allowance of a version
// that stays usable. Version 0 (unknown) charges the key's latest version.
func (t *AEADSafetyTracker) RecordKeyVersionUse(ctx context.Context, keyID string, version, operations, bytesProcessed uint64) error {
	return t.recordOperation(ctx, keyID, version, operations, bytesProcessed, false)
}

// RecordKeyEncryptBatch records a batch of operations encryptions under a
// key, reserving the whole batch against the key's limits before any item is
// encrypted.
func (t *AEADSafetyTracker) RecordKeyEncryptBatch(ctx context.Context, keyID string, operations, bytesEncrypted uint64) error {
	return t.recordOperation(ctx, keyID, t.currentKeyVersion(ctx, keyID), operations, bytesEncrypted, false)
}

// RecordDEKEncrypt records an encryption operation for a DEK.
func (t *AEADSafetyTracker) RecordDEKEncrypt(ctx context.Context, dekPath string, bytesEncrypted uint64) error {
	return t.recordOperation(ctx, dekPath, 0, 1, bytesEncrypted, true)
}

// recordOperation is the common implementation for recording operations.
// version is the key version charged (keys only; 0 when unknown).
func (t *AEADSafetyTracker) recordOperation(ctx context.Context, id string, version, operations, bytesProcessed uint64, isDEK bool) error {
	var usageMap *syncMap[string, *AEADUsage]
	var keyType string

	if isDEK {
		usageMap = &t.dekUsage
		keyType = "dek"
	} else {
		usageMap = &t.keyUsage
		keyType = "key"
	}

	var usage *AEADUsage
	var limitExceeded bool
	if isDEK {
		usage = usageMap.LoadOrStore(id, &AEADUsage{
			Config: t.getEffectiveConfig(id, isDEK),
		})
		limitExceeded = usage.IncrementUsageBy(operations, bytesProcessed)
	} else {
		usage, limitExceeded = t.chargeKeyVersion(id, version, operations, bytesProcessed, true)
	}

	// Record metrics
	t.metrics.operationsTotal.With(id, keyType).Add(float64(operations))
	t.metrics.bytesProcessed.With(id, keyType).Add(float64(bytesProcessed))
	t.metrics.usagePercent.With(id, keyType).Set(float64(usage.UsagePercent()))

	// Check for warning threshold
	if warning, msg := usage.WarningNeeded(); warning {
		t.metrics.limitWarnings.With(id, keyType).Inc()
		if t.warningCallback != nil {
			t.warningCallback(ctx, id, isDEK, msg)
		}
		if t.logger != nil {
			t.logger.WithFields(map[string]interface{}{
				"key_id":  id,
				"type":    keyType,
				"message": msg,
			}).Warn("AEAD safety warning threshold reached")
		}
	}

	// Check for auto-rotation threshold
	if usage.AutoRotateNeeded() {
		t.metrics.autoRotations.With(id, keyType).Inc()
		if t.rotationCallback != nil {
			t.rotationCallback(ctx, id, isDEK)
		}
		if t.logger != nil {
			t.logger.WithFields(map[string]interface{}{
				"key_id": id,
				"type":   keyType,
			}).Warn("AEAD safety auto-rotation triggered")
		}
	}

	// If limit exceeded, return error
	if limitExceeded {
		t.metrics.limitExceeded.With(id, keyType).Inc()
		if t.logger != nil {
			t.logger.WithFields(map[string]interface{}{
				"key_id": id,
				"type":   keyType,
			}).Error("AEAD safety limit exceeded")
		}
		return api.NewAEADLimitExceededError("service.AEADSafety.checkLimits", keyType, id)
	}

	return nil
}

// getEffectiveConfig returns the effective configuration for a key/DEK.
// It looks up per-key configuration from the config maps first, then falls
// back to server defaults if no per-key config exists and enforceDefaults is enabled.
func (t *AEADSafetyTracker) getEffectiveConfig(id string, isDEK bool) AEADSafetyConfig {
	// Look up per-key/DEK config from the appropriate config map
	var configMap *syncMap[string, AEADSafetyConfig]
	if isDEK {
		configMap = &t.dekConfigs
	} else {
		configMap = &t.keyConfigs
	}

	// Check for stored per-key configuration
	if config, ok := configMap.Load(id); ok {
		return config
	}

	// Fall back to server defaults if enforcing
	if t.enforceDefaults {
		return t.serverConfig
	}

	// No limits if not enforcing defaults
	return AEADSafetyConfig{}
}

// SetKeyConfig sets the safety configuration for a specific key.
// The configuration is stored in a separate map and applied to both new and
// existing usage trackers for the key.
func (t *AEADSafetyTracker) SetKeyConfig(keyID string, config AEADSafetyConfig) {
	// Store in the config map for persistence
	t.keyConfigs.Store(keyID, config)

	// Also update the existing usage tracker if one exists
	usage := t.keyUsage.LoadOrStore(keyID, &AEADUsage{})
	usage.setConfig(config)
}

// SetDEKConfig sets the safety configuration for a specific DEK.
// The configuration is stored in a separate map and applied to both new and
// existing usage trackers for the DEK.
func (t *AEADSafetyTracker) SetDEKConfig(dekPath string, config AEADSafetyConfig) {
	// Store in the config map for persistence
	t.dekConfigs.Store(dekPath, config)

	// Also update the existing usage tracker if one exists
	usage := t.dekUsage.LoadOrStore(dekPath, &AEADUsage{})
	usage.setConfig(config)
}

// ClearKeyConfig removes per-key configuration (reverts to server defaults).
func (t *AEADSafetyTracker) ClearKeyConfig(keyID string) {
	// Remove from config map
	t.keyConfigs.Delete(keyID)

	// Update existing usage tracker to use defaults
	if usage, ok := t.keyUsage.Load(keyID); ok {
		if t.enforceDefaults {
			usage.setConfig(t.serverConfig)
		} else {
			usage.setConfig(AEADSafetyConfig{})
		}
	}
}

// ClearDEKConfig removes per-DEK configuration (reverts to server defaults).
func (t *AEADSafetyTracker) ClearDEKConfig(dekPath string) {
	// Remove from config map
	t.dekConfigs.Delete(dekPath)

	// Update existing usage tracker to use defaults
	if usage, ok := t.dekUsage.Load(dekPath); ok {
		if t.enforceDefaults {
			usage.setConfig(t.serverConfig)
		} else {
			usage.setConfig(AEADSafetyConfig{})
		}
	}
}

// GetKeyConfig returns the current configuration for a key.
// Returns the per-key config if set, otherwise the effective default config.
func (t *AEADSafetyTracker) GetKeyConfig(keyID string) AEADSafetyConfig {
	return t.getEffectiveConfig(keyID, false)
}

// GetDEKConfig returns the current configuration for a DEK.
// Returns the per-DEK config if set, otherwise the effective default config.
func (t *AEADSafetyTracker) GetDEKConfig(dekPath string) AEADSafetyConfig {
	return t.getEffectiveConfig(dekPath, true)
}

// GetKeyUsage returns the current usage for a key.
func (t *AEADSafetyTracker) GetKeyUsage(keyID string) (operationCount, bytesProcessed uint64, usagePercent int) {
	if usage, ok := t.keyUsage.Load(keyID); ok {
		ops, bytes, cfg := usage.snapshot()
		return ops, bytes, conv.ClampUint64ToInt(usagePercentOf(ops, bytes, cfg))
	}
	return 0, 0, 0
}

// GetDEKUsage returns the current usage for a DEK.
func (t *AEADSafetyTracker) GetDEKUsage(dekPath string) (operationCount, bytesProcessed uint64, usagePercent int) {
	if usage, ok := t.dekUsage.Load(dekPath); ok {
		return usage.OperationCount.Load(), usage.BytesProcessed.Load(), conv.ClampUint64ToInt(usage.UsagePercent())
	}
	return 0, 0, 0
}

// SafetyCheck returns all keys/DEKs that are above the warning threshold.
type SafetyCheckResult struct {
	ID             string `json:"id"`
	Type           string `json:"type"` // "key" or "dek"
	OperationCount uint64 `json:"operation_count"`
	BytesProcessed uint64 `json:"bytes_processed"`
	UsagePercent   int    `json:"usage_percent"`
	MaxOperations  uint64 `json:"max_operations"`
	MaxBytes       uint64 `json:"max_bytes"`
}

// SafetyCheck returns all keys/DEKs above the threshold percentage.
func (t *AEADSafetyTracker) SafetyCheck(thresholdPercent int) []SafetyCheckResult {
	var results []SafetyCheckResult

	t.keyUsage.Range(func(keyID string, usage *AEADUsage) bool {
		ops, bytes, cfg := usage.snapshot()
		percent := conv.ClampUint64ToInt(usagePercentOf(ops, bytes, cfg))
		if percent >= thresholdPercent {
			results = append(results, SafetyCheckResult{
				ID:             keyID,
				Type:           "key",
				OperationCount: ops,
				BytesProcessed: bytes,
				UsagePercent:   percent,
				MaxOperations:  cfg.MaxOperations,
				MaxBytes:       cfg.MaxBytes,
			})
		}
		return true
	})

	t.dekUsage.Range(func(dekPath string, usage *AEADUsage) bool {
		ops, bytes, cfg := usage.snapshot()
		percent := conv.ClampUint64ToInt(usagePercentOf(ops, bytes, cfg))
		if percent >= thresholdPercent {
			results = append(results, SafetyCheckResult{
				ID:             dekPath,
				Type:           "dek",
				OperationCount: ops,
				BytesProcessed: bytes,
				UsagePercent:   percent,
				MaxOperations:  cfg.MaxOperations,
				MaxBytes:       cfg.MaxBytes,
			})
		}
		return true
	})

	return results
}

// SafetyReport returns usage statistics for all keys/DEKs.
func (t *AEADSafetyTracker) SafetyReport() []SafetyCheckResult {
	return t.SafetyCheck(0) // Return all
}

// ResetUsage resets the usage counters for a key (called after rotation).
func (t *AEADSafetyTracker) ResetKeyUsage(keyID string) {
	if usage, ok := t.keyUsage.Load(keyID); ok {
		usage.resetCounters()
	}
}

// ResetDEKUsage resets the usage counters for a DEK (called after rotation).
func (t *AEADSafetyTracker) ResetDEKUsage(dekPath string) {
	if usage, ok := t.dekUsage.Load(dekPath); ok {
		usage.resetCounters()
	}
}

// keyGate returns keyID's rotation gate, creating it on first use.
func (t *AEADSafetyTracker) keyGate(keyID string) *sync.RWMutex {
	if gate, ok := t.keyGates.Load(keyID); ok {
		return gate
	}
	return t.keyGates.LoadOrStore(keyID, &sync.RWMutex{})
}

// BeginKeyUse holds keyID's rotation gate for reading until the returned end
// is called. A crypto call holds it from its usage reservation through the
// cryptographic operation, so a key's usage is only ever charged to the
// version that is current while the gate is held: RotateKeyUsage waits for
// every in-flight use to end before it publishes a new version and resets the
// count. The holder must not call RotateKeyUsage or BeginKeyUse on the same
// key before end, and the tracker's warning and rotation callbacks, which run
// inside the hold, must not rotate the key synchronously.
func (t *AEADSafetyTracker) BeginKeyUse(keyID string) (end func()) {
	gate := t.keyGate(keyID)
	gate.RLock()
	return gate.RUnlock
}

// RotateKeyUsage holds keyID's rotation gate for writing, runs rotate, which
// returns the key's new version, and when it succeeds makes that version the
// key's latest, then releases the gate. The old version keeps its counters:
// it stays decryptable, and its decrypts are still charged to it
// (RecordKeyVersionUse), so a rotation never grants a retained version a new
// allowance. No crypto call holding the gate (BeginKeyUse) overlaps rotate,
// so a use reserved before the rotation stays on the old version and a use
// after it is counted against the new one. rotate must not call BeginKeyUse
// or RotateKeyUsage on the same key, and the tracker's callbacks must not
// rotate synchronously.
func (t *AEADSafetyTracker) RotateKeyUsage(keyID string, rotate func() (newVersion uint64, err error)) error {
	gate := t.keyGate(keyID)
	gate.Lock()
	defer gate.Unlock()

	newVersion, err := rotate()
	if err != nil {
		return err
	}
	t.chargeKeyVersion(keyID, newVersion, 0, 0, false)
	return nil
}

// SetKeyVersionSource sets the function that reports a key's current
// version from authoritative (replicated) state. nil removes it.
func (t *AEADSafetyTracker) SetKeyVersionSource(source func(ctx context.Context, keyID string) (uint64, error)) {
	if source == nil {
		t.keyVersionSource.Store(nil)
		return
	}
	t.keyVersionSource.Store(&source)
}

// currentKeyVersion reports the version a new encryption under keyID is
// charged to: the newer of the version source's answer and the latest
// version this tracker has seen. 0 means unknown, which charges the latest
// tracked version.
func (t *AEADSafetyTracker) currentKeyVersion(ctx context.Context, keyID string) uint64 {
	var version uint64
	if source := t.keyVersionSource.Load(); source != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		if v, err := (*source)(ctx, keyID); err == nil {
			version = v
		}
	}
	if usage, ok := t.keyUsage.Load(keyID); ok {
		usage.mu.Lock()
		if usage.Version > version {
			version = usage.Version
		}
		usage.mu.Unlock()
	}
	return version
}

// retiredUsageKey names the counters of a version of keyID that is no longer
// its latest.
func retiredUsageKey(keyID string, version uint64) string {
	return keyID + "\x00v" + strconv.FormatUint(version, 10)
}

// chargeKeyVersion charges operations to version's counters of keyID (when
// charge is set) and returns them with whether a limit is exceeded. The
// key's keyUsage entry holds its latest version; a newer version moves the
// latest's counters to retiredKeyUsage and starts from zero, and an older
// version is charged to its retired counters, which persist. Version 0
// charges the latest version. The decision and the charge happen under the
// latest entry's lock, so a concurrent version change cannot move a charge
// to another version's counters.
func (t *AEADSafetyTracker) chargeKeyVersion(keyID string, version, operations, bytesProcessed uint64, charge bool) (*AEADUsage, bool) {
	usage := t.keyUsage.LoadOrStore(keyID, &AEADUsage{
		Config: t.getEffectiveConfig(keyID, false),
	})
	usage.mu.Lock()
	defer usage.mu.Unlock()

	if version != 0 && usage.Version != 0 && version < usage.Version {
		retired := t.retiredKeyUsage.LoadOrStore(retiredUsageKey(keyID, version), &AEADUsage{
			Config:  usage.Config,
			Version: version,
		})
		if !charge {
			return retired, false
		}
		exceeded := retired.IncrementUsageBy(operations, bytesProcessed)
		return retired, exceeded
	}
	if version > usage.Version {
		previous := usage.Version
		if previous == 0 && version > 1 && (usage.OperationCount.Load() > 0 || usage.BytesProcessed.Load() > 0) {
			// Counts of an unknown version, seen before the key's version
			// was: they belong to the version this one superseded.
			previous = version - 1
		}
		if previous != 0 {
			usage.Version = previous
			t.retiredKeyUsage.Store(retiredUsageKey(keyID, previous), usage.retireLocked())
		}
		usage.Version = version
	}
	if !charge {
		return usage, false
	}
	exceeded := usage.incrementLocked(operations, bytesProcessed)
	return usage, exceeded
}
