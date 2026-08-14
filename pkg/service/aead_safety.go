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
	"sync"
	"sync/atomic"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
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

	// Config is the safety configuration for this key
	Config AEADSafetyConfig
}

// IncrementUsage atomically increments the usage counters.
// Returns true if the limits are exceeded after this operation.
func (u *AEADUsage) IncrementUsage(bytesProcessed uint64) (limitExceeded bool) {
	u.OperationCount.Add(1)
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
	var maxPercent uint64

	if u.Config.MaxOperations > 0 {
		opsPercent := (u.OperationCount.Load() * 100) / u.Config.MaxOperations
		if opsPercent > maxPercent {
			maxPercent = opsPercent
		}
	}

	if u.Config.MaxBytes > 0 {
		bytesPercent := (u.BytesProcessed.Load() * 100) / u.Config.MaxBytes
		if bytesPercent > maxPercent {
			maxPercent = bytesPercent
		}
	}

	return maxPercent
}

// =============================================================================
// AEAD Safety Tracker Service
// =============================================================================

// AEADSafetyTracker tracks usage across all keys and DEKs.
type AEADSafetyTracker struct {
	// keyUsage maps key IDs to their usage trackers
	keyUsage sync.Map

	// dekUsage maps DEK paths to their usage trackers
	dekUsage sync.Map

	// keyConfigs stores per-key configuration overrides
	// This allows configurations to persist even if the usage tracker is reset
	keyConfigs sync.Map

	// dekConfigs stores per-DEK configuration overrides
	dekConfigs sync.Map

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
func (t *AEADSafetyTracker) RecordKeyEncrypt(ctx context.Context, keyID string, bytesEncrypted uint64) error {
	return t.recordOperation(ctx, keyID, bytesEncrypted, false)
}

// RecordDEKEncrypt records an encryption operation for a DEK.
func (t *AEADSafetyTracker) RecordDEKEncrypt(ctx context.Context, dekPath string, bytesEncrypted uint64) error {
	return t.recordOperation(ctx, dekPath, bytesEncrypted, true)
}

// recordOperation is the common implementation for recording operations.
func (t *AEADSafetyTracker) recordOperation(ctx context.Context, id string, bytesProcessed uint64, isDEK bool) error {
	var usageMap *sync.Map
	var keyType string

	if isDEK {
		usageMap = &t.dekUsage
		keyType = "dek"
	} else {
		usageMap = &t.keyUsage
		keyType = "key"
	}

	// Get or create usage tracker
	val, _ := usageMap.LoadOrStore(id, &AEADUsage{
		Config: t.getEffectiveConfig(id, isDEK),
	})
	usage := val.(*AEADUsage) //nolint:errcheck // type assertion guaranteed by sync.Map contract

	// Increment counters
	limitExceeded := usage.IncrementUsage(bytesProcessed)

	// Record metrics
	t.metrics.operationsTotal.With(id, keyType).Inc()
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
	var configMap *sync.Map
	if isDEK {
		configMap = &t.dekConfigs
	} else {
		configMap = &t.keyConfigs
	}

	// Check for stored per-key configuration
	if val, ok := configMap.Load(id); ok {
		return val.(AEADSafetyConfig) //nolint:errcheck // type assertion guaranteed by sync.Map contract
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
	val, _ := t.keyUsage.LoadOrStore(keyID, &AEADUsage{})
	usage := val.(*AEADUsage) //nolint:errcheck // type assertion guaranteed by sync.Map contract
	usage.Config = config
}

// SetDEKConfig sets the safety configuration for a specific DEK.
// The configuration is stored in a separate map and applied to both new and
// existing usage trackers for the DEK.
func (t *AEADSafetyTracker) SetDEKConfig(dekPath string, config AEADSafetyConfig) {
	// Store in the config map for persistence
	t.dekConfigs.Store(dekPath, config)

	// Also update the existing usage tracker if one exists
	val, _ := t.dekUsage.LoadOrStore(dekPath, &AEADUsage{})
	usage := val.(*AEADUsage) //nolint:errcheck // type assertion guaranteed by sync.Map contract
	usage.Config = config
}

// ClearKeyConfig removes per-key configuration (reverts to server defaults).
func (t *AEADSafetyTracker) ClearKeyConfig(keyID string) {
	// Remove from config map
	t.keyConfigs.Delete(keyID)

	// Update existing usage tracker to use defaults
	if val, ok := t.keyUsage.Load(keyID); ok {
		usage := val.(*AEADUsage) //nolint:errcheck // type assertion guaranteed by sync.Map contract
		if t.enforceDefaults {
			usage.Config = t.serverConfig
		} else {
			usage.Config = AEADSafetyConfig{}
		}
	}
}

// ClearDEKConfig removes per-DEK configuration (reverts to server defaults).
func (t *AEADSafetyTracker) ClearDEKConfig(dekPath string) {
	// Remove from config map
	t.dekConfigs.Delete(dekPath)

	// Update existing usage tracker to use defaults
	if val, ok := t.dekUsage.Load(dekPath); ok {
		usage := val.(*AEADUsage) //nolint:errcheck // type assertion guaranteed by sync.Map contract
		if t.enforceDefaults {
			usage.Config = t.serverConfig
		} else {
			usage.Config = AEADSafetyConfig{}
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
	if val, ok := t.keyUsage.Load(keyID); ok {
		usage := val.(*AEADUsage)                                                                  //nolint:errcheck // type assertion guaranteed by sync.Map contract
		return usage.OperationCount.Load(), usage.BytesProcessed.Load(), int(usage.UsagePercent()) //nolint:gosec // G115: percentage bounded 0-100
	}
	return 0, 0, 0
}

// GetDEKUsage returns the current usage for a DEK.
func (t *AEADSafetyTracker) GetDEKUsage(dekPath string) (operationCount, bytesProcessed uint64, usagePercent int) {
	if val, ok := t.dekUsage.Load(dekPath); ok {
		usage := val.(*AEADUsage)                                                                  //nolint:errcheck // type assertion guaranteed by sync.Map contract
		return usage.OperationCount.Load(), usage.BytesProcessed.Load(), int(usage.UsagePercent()) //nolint:gosec // G115: percentage bounded 0-100
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

	t.keyUsage.Range(func(key, value interface{}) bool {
		keyID := key.(string)                              //nolint:errcheck // type assertion guaranteed by sync.Map contract
		usage := value.(*AEADUsage)                        //nolint:errcheck // type assertion guaranteed by sync.Map contract
		if int(usage.UsagePercent()) >= thresholdPercent { //nolint:gosec // G115: percentage bounded 0-100
			results = append(results, SafetyCheckResult{
				ID:             keyID,
				Type:           "key",
				OperationCount: usage.OperationCount.Load(),
				BytesProcessed: usage.BytesProcessed.Load(),
				UsagePercent:   int(usage.UsagePercent()), //nolint:gosec // G115: percentage bounded 0-100
				MaxOperations:  usage.Config.MaxOperations,
				MaxBytes:       usage.Config.MaxBytes,
			})
		}
		return true
	})

	t.dekUsage.Range(func(key, value interface{}) bool {
		dekPath := key.(string)                            //nolint:errcheck // type assertion guaranteed by sync.Map contract
		usage := value.(*AEADUsage)                        //nolint:errcheck // type assertion guaranteed by sync.Map contract
		if int(usage.UsagePercent()) >= thresholdPercent { //nolint:gosec // G115: percentage bounded 0-100
			results = append(results, SafetyCheckResult{
				ID:             dekPath,
				Type:           "dek",
				OperationCount: usage.OperationCount.Load(),
				BytesProcessed: usage.BytesProcessed.Load(),
				UsagePercent:   int(usage.UsagePercent()), //nolint:gosec // G115: percentage bounded 0-100
				MaxOperations:  usage.Config.MaxOperations,
				MaxBytes:       usage.Config.MaxBytes,
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
	if val, ok := t.keyUsage.Load(keyID); ok {
		usage := val.(*AEADUsage) //nolint:errcheck // type assertion guaranteed by sync.Map contract
		usage.OperationCount.Store(0)
		usage.BytesProcessed.Store(0)
	}
}

// ResetDEKUsage resets the usage counters for a DEK (called after rotation).
func (t *AEADSafetyTracker) ResetDEKUsage(dekPath string) {
	if val, ok := t.dekUsage.Load(dekPath); ok {
		usage := val.(*AEADUsage) //nolint:errcheck // type assertion guaranteed by sync.Map contract
		usage.OperationCount.Store(0)
		usage.BytesProcessed.Store(0)
	}
}
