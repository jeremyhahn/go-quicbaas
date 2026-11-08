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

package service

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// AEAD Counter Increment Tests
// =============================================================================

func TestAEADUsage_IncrementUsage_CounterAccuracy(t *testing.T) {
	tests := []struct {
		name               string
		initialOps         uint64
		initialBytes       uint64
		bytesToAdd         uint64
		expectedFinalOps   uint64
		expectedFinalBytes uint64
	}{
		{
			name:               "increment from zero",
			initialOps:         0,
			initialBytes:       0,
			bytesToAdd:         100,
			expectedFinalOps:   1,
			expectedFinalBytes: 100,
		},
		{
			name:               "increment from existing values",
			initialOps:         10,
			initialBytes:       500,
			bytesToAdd:         250,
			expectedFinalOps:   11,
			expectedFinalBytes: 750,
		},
		{
			name:               "increment with zero bytes",
			initialOps:         5,
			initialBytes:       200,
			bytesToAdd:         0,
			expectedFinalOps:   6,
			expectedFinalBytes: 200,
		},
		{
			name:               "increment with large byte value",
			initialOps:         1,
			initialBytes:       0,
			bytesToAdd:         1 << 30, // 1 GB
			expectedFinalOps:   2,
			expectedFinalBytes: 1 << 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := &AEADUsage{
				Config: AEADSafetyConfig{
					MaxOperations: 1 << 32,
					MaxBytes:      375809638400,
				},
			}
			usage.OperationCount.Store(tt.initialOps)
			usage.BytesProcessed.Store(tt.initialBytes)

			usage.IncrementUsage(tt.bytesToAdd)

			assert.Equal(t, tt.expectedFinalOps, usage.OperationCount.Load())
			assert.Equal(t, tt.expectedFinalBytes, usage.BytesProcessed.Load())
		})
	}
}

func TestAEADUsage_BytesCounter_LargeValues(t *testing.T) {
	tests := []struct {
		name               string
		bytesPerOperation  uint64
		numOperations      int
		expectedTotalBytes uint64
	}{
		{
			name:               "small files",
			bytesPerOperation:  1024, // 1 KB
			numOperations:      1000,
			expectedTotalBytes: 1024000,
		},
		{
			name:               "medium files",
			bytesPerOperation:  1 << 20, // 1 MB
			numOperations:      100,
			expectedTotalBytes: 100 * (1 << 20),
		},
		{
			name:               "large files",
			bytesPerOperation:  1 << 30, // 1 GB
			numOperations:      10,
			expectedTotalBytes: 10 * (1 << 30),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := &AEADUsage{
				Config: AEADSafetyConfig{
					MaxOperations: 1 << 32,
					MaxBytes:      math.MaxUint64,
				},
			}

			for i := 0; i < tt.numOperations; i++ {
				usage.IncrementUsage(tt.bytesPerOperation)
			}

			assert.Equal(t, uint64(tt.numOperations), usage.OperationCount.Load())
			assert.Equal(t, tt.expectedTotalBytes, usage.BytesProcessed.Load())
		})
	}
}

// =============================================================================
// Safety Config Inheritance Resolution Tests
// =============================================================================

func TestAEADSafetyTracker_EnforceDefaults_Resolution(t *testing.T) {
	tests := []struct {
		name            string
		enforceDefaults bool
		serverMaxOps    uint64
		serverMaxBytes  uint64
		perKeyMaxOps    uint64
		perKeyMaxBytes  uint64
		usePerKeyConfig bool
		operationsToRun int
		expectError     bool
	}{
		{
			name:            "enforce defaults uses server config",
			enforceDefaults: true,
			serverMaxOps:    10,
			serverMaxBytes:  1000,
			operationsToRun: 15,
			expectError:     true,
		},
		{
			name:            "no enforcement allows unlimited",
			enforceDefaults: false,
			serverMaxOps:    10,
			serverMaxBytes:  1000,
			operationsToRun: 100,
			expectError:     false,
		},
		{
			name:            "per-key config overrides server when enforcing",
			enforceDefaults: true,
			serverMaxOps:    100,
			serverMaxBytes:  10000,
			perKeyMaxOps:    5,
			perKeyMaxBytes:  500,
			usePerKeyConfig: true,
			operationsToRun: 10,
			expectError:     true,
		},
		{
			name:            "per-key config takes precedence",
			enforceDefaults: true,
			serverMaxOps:    5,
			serverMaxBytes:  500,
			perKeyMaxOps:    100,
			perKeyMaxBytes:  10000,
			usePerKeyConfig: true,
			operationsToRun: 50,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewAEADSafetyTracker(
				WithServerConfig(AEADSafetyConfig{
					MaxOperations:     tt.serverMaxOps,
					MaxBytes:          tt.serverMaxBytes,
					WarningPercent:    80,
					AutoRotatePercent: 95,
				}),
				WithEnforceDefaults(tt.enforceDefaults),
			)

			if tt.usePerKeyConfig {
				tracker.SetKeyConfig("test-key", AEADSafetyConfig{
					MaxOperations:     tt.perKeyMaxOps,
					MaxBytes:          tt.perKeyMaxBytes,
					WarningPercent:    80,
					AutoRotatePercent: 95,
				})
			}

			ctx := context.Background()
			var lastErr error
			for i := 0; i < tt.operationsToRun; i++ {
				lastErr = tracker.RecordKeyEncrypt(ctx, "test-key", 50)
			}

			if tt.expectError {
				assert.Error(t, lastErr)
			} else {
				assert.NoError(t, lastErr)
			}
		})
	}
}

// =============================================================================
// Warning Generation Tests (80% Threshold)
// =============================================================================

func TestAEADSafetyTracker_WarningThreshold_Generation(t *testing.T) {
	tests := []struct {
		name              string
		maxOperations     uint64
		warningPercent    int
		operationsToRun   int
		expectWarning     bool
		expectedCallCount int
	}{
		{
			name:              "warning at exact 80% threshold",
			maxOperations:     100,
			warningPercent:    80,
			operationsToRun:   80,
			expectWarning:     true,
			expectedCallCount: 1,
		},
		{
			name:              "no warning below threshold",
			maxOperations:     100,
			warningPercent:    80,
			operationsToRun:   70,
			expectWarning:     false,
			expectedCallCount: 0,
		},
		{
			name:              "warning triggered multiple times above threshold",
			maxOperations:     100,
			warningPercent:    80,
			operationsToRun:   90,
			expectWarning:     true,
			expectedCallCount: 11, // 80 through 90 all trigger warnings
		},
		{
			name:              "custom warning threshold at 50%",
			maxOperations:     100,
			warningPercent:    50,
			operationsToRun:   55,
			expectWarning:     true,
			expectedCallCount: 6, // 50 through 55
		},
		{
			name:              "warning disabled",
			maxOperations:     100,
			warningPercent:    0,
			operationsToRun:   95,
			expectWarning:     false,
			expectedCallCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var warningCount int
			var mu sync.Mutex

			tracker := NewAEADSafetyTracker(
				WithServerConfig(AEADSafetyConfig{
					MaxOperations:     tt.maxOperations,
					MaxBytes:          1000000,
					WarningPercent:    tt.warningPercent,
					AutoRotatePercent: 95,
				}),
				WithWarningCallback(func(ctx context.Context, keyID string, isDEK bool, msg string) {
					mu.Lock()
					defer mu.Unlock()
					warningCount++
				}),
			)

			ctx := context.Background()
			for i := 0; i < tt.operationsToRun; i++ {
				_ = tracker.RecordKeyEncrypt(ctx, "test-key", 10)
			}

			mu.Lock()
			defer mu.Unlock()
			if tt.expectWarning {
				assert.Greater(t, warningCount, 0)
			} else {
				assert.Equal(t, 0, warningCount)
			}
		})
	}
}

// =============================================================================
// Auto-Rotation Trigger Tests (95% Threshold)
// =============================================================================

func TestAEADSafetyTracker_AutoRotationThreshold_Trigger(t *testing.T) {
	tests := []struct {
		name              string
		maxOperations     uint64
		autoRotatePercent int
		operationsToRun   int
		expectRotation    bool
		expectedCallCount int
	}{
		{
			name:              "rotation at exact 95% threshold",
			maxOperations:     100,
			autoRotatePercent: 95,
			operationsToRun:   95,
			expectRotation:    true,
			expectedCallCount: 1,
		},
		{
			name:              "no rotation below threshold",
			maxOperations:     100,
			autoRotatePercent: 95,
			operationsToRun:   90,
			expectRotation:    false,
			expectedCallCount: 0,
		},
		{
			name:              "rotation triggered multiple times above threshold",
			maxOperations:     100,
			autoRotatePercent: 95,
			operationsToRun:   98,
			expectRotation:    true,
			expectedCallCount: 4, // 95, 96, 97, 98
		},
		{
			name:              "custom rotation threshold at 80%",
			maxOperations:     100,
			autoRotatePercent: 80,
			operationsToRun:   85,
			expectRotation:    true,
			expectedCallCount: 6, // 80-85
		},
		{
			name:              "rotation disabled",
			maxOperations:     100,
			autoRotatePercent: 0,
			operationsToRun:   99,
			expectRotation:    false,
			expectedCallCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rotationCount int
			var mu sync.Mutex

			tracker := NewAEADSafetyTracker(
				WithServerConfig(AEADSafetyConfig{
					MaxOperations:     tt.maxOperations,
					MaxBytes:          1000000,
					WarningPercent:    80,
					AutoRotatePercent: tt.autoRotatePercent,
				}),
				WithRotationCallback(func(ctx context.Context, keyID string, isDEK bool) {
					mu.Lock()
					defer mu.Unlock()
					rotationCount++
				}),
			)

			ctx := context.Background()
			for i := 0; i < tt.operationsToRun; i++ {
				_ = tracker.RecordKeyEncrypt(ctx, "test-key", 10)
			}

			mu.Lock()
			defer mu.Unlock()
			if tt.expectRotation {
				assert.Greater(t, rotationCount, 0)
			} else {
				assert.Equal(t, 0, rotationCount)
			}
		})
	}
}

// =============================================================================
// SafetyCheck Query Functionality Tests
// =============================================================================

func TestAEADSafetyTracker_SafetyCheck_QueryFunctionality(t *testing.T) {
	tests := []struct {
		name             string
		keyUsagePercents map[string]int // keyID -> usage percent
		dekUsagePercents map[string]int // dekPath -> usage percent
		threshold        int
		expectedKeyCount int
		expectedDEKCount int
	}{
		{
			name: "query returns items above 80% threshold",
			keyUsagePercents: map[string]int{
				"key1": 85,
				"key2": 50,
				"key3": 90,
			},
			dekUsagePercents: map[string]int{
				"/dek1": 75,
				"/dek2": 82,
			},
			threshold:        80,
			expectedKeyCount: 2,
			expectedDEKCount: 1,
		},
		{
			name: "query with 0 threshold returns all",
			keyUsagePercents: map[string]int{
				"key1": 10,
				"key2": 20,
			},
			dekUsagePercents: map[string]int{
				"/dek1": 5,
			},
			threshold:        0,
			expectedKeyCount: 2,
			expectedDEKCount: 1,
		},
		{
			name: "query with 100 threshold returns none below",
			keyUsagePercents: map[string]int{
				"key1": 95,
				"key2": 99,
			},
			threshold:        100,
			expectedKeyCount: 0,
			expectedDEKCount: 0,
		},
		{
			name:             "empty tracker returns no results",
			keyUsagePercents: map[string]int{},
			dekUsagePercents: map[string]int{},
			threshold:        50,
			expectedKeyCount: 0,
			expectedDEKCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewAEADSafetyTracker(
				WithServerConfig(AEADSafetyConfig{
					MaxOperations:     100,
					MaxBytes:          10000,
					WarningPercent:    80,
					AutoRotatePercent: 95,
				}),
			)

			ctx := context.Background()

			// Set up key usage
			for keyID, pct := range tt.keyUsagePercents {
				for i := 0; i < pct; i++ {
					_ = tracker.RecordKeyEncrypt(ctx, keyID, 50)
				}
			}

			// Set up DEK usage
			for dekPath, pct := range tt.dekUsagePercents {
				for i := 0; i < pct; i++ {
					_ = tracker.RecordDEKEncrypt(ctx, dekPath, 50)
				}
			}

			results := tracker.SafetyCheck(tt.threshold)

			keyCount := 0
			dekCount := 0
			for _, r := range results {
				if r.Type == "key" {
					keyCount++
				} else if r.Type == "dek" {
					dekCount++
				}
			}

			assert.Equal(t, tt.expectedKeyCount, keyCount)
			assert.Equal(t, tt.expectedDEKCount, dekCount)
		})
	}
}

// =============================================================================
// SafetyReport Generation Tests
// =============================================================================

func TestAEADSafetyTracker_SafetyReport_Generation(t *testing.T) {
	t.Run("report includes all fields", func(t *testing.T) {
		config := AEADSafetyConfig{
			MaxOperations:     1000,
			MaxBytes:          100000,
			WarningPercent:    75,
			AutoRotatePercent: 92,
		}

		tracker := NewAEADSafetyTracker(WithServerConfig(config))

		ctx := context.Background()
		_ = tracker.RecordKeyEncrypt(ctx, "key1", 500)
		_ = tracker.RecordKeyEncrypt(ctx, "key1", 300)
		_ = tracker.RecordDEKEncrypt(ctx, "/dek1", 1000)

		report := tracker.SafetyReport()

		require.Len(t, report, 2)

		for _, item := range report {
			assert.NotEmpty(t, item.ID)
			assert.NotEmpty(t, item.Type)
			assert.Greater(t, item.OperationCount, uint64(0))
			assert.Greater(t, item.BytesProcessed, uint64(0))
			assert.Equal(t, config.MaxOperations, item.MaxOperations)
			assert.Equal(t, config.MaxBytes, item.MaxBytes)
		}
	})

	t.Run("report includes per-key config when set", func(t *testing.T) {
		serverConfig := AEADSafetyConfig{
			MaxOperations:     1000,
			MaxBytes:          100000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}

		keyConfig := AEADSafetyConfig{
			MaxOperations:     500,
			MaxBytes:          50000,
			WarningPercent:    70,
			AutoRotatePercent: 90,
		}

		tracker := NewAEADSafetyTracker(WithServerConfig(serverConfig))
		tracker.SetKeyConfig("custom-key", keyConfig)

		ctx := context.Background()
		_ = tracker.RecordKeyEncrypt(ctx, "custom-key", 100)
		_ = tracker.RecordKeyEncrypt(ctx, "default-key", 100)

		report := tracker.SafetyReport()
		require.Len(t, report, 2)

		for _, item := range report {
			if item.ID == "custom-key" {
				assert.Equal(t, keyConfig.MaxOperations, item.MaxOperations)
				assert.Equal(t, keyConfig.MaxBytes, item.MaxBytes)
			} else {
				assert.Equal(t, serverConfig.MaxOperations, item.MaxOperations)
				assert.Equal(t, serverConfig.MaxBytes, item.MaxBytes)
			}
		}
	})
}

// =============================================================================
// Per-Key Safety Configuration Tests
// =============================================================================

func TestAEADSafetyTracker_PerKeySafetyConfig(t *testing.T) {
	t.Run("set and apply per-key config", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
		)

		// Set restrictive per-key config
		tracker.SetKeyConfig("restricted-key", AEADSafetyConfig{
			MaxOperations:     5,
			MaxBytes:          500,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		})

		ctx := context.Background()

		// Should hit per-key limit, not server limit
		for i := 0; i < 5; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "restricted-key", 50)
			if i < 4 {
				assert.NoError(t, err)
			}
		}

		err := tracker.RecordKeyEncrypt(ctx, "restricted-key", 50)
		assert.Error(t, err)

		// Other keys should use server config
		for i := 0; i < 50; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "normal-key", 50)
			assert.NoError(t, err)
		}
	})

	t.Run("clear per-key config reverts to server defaults", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
		)

		// Set and then clear per-key config
		tracker.SetKeyConfig("test-key", AEADSafetyConfig{
			MaxOperations:     5,
			MaxBytes:          500,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		})
		tracker.ClearKeyConfig("test-key")

		ctx := context.Background()

		// Should now use server config (100 ops limit)
		for i := 0; i < 50; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "test-key", 50)
			assert.NoError(t, err)
		}
	})
}

// =============================================================================
// Per-DEK Safety Configuration Tests
// =============================================================================

func TestAEADSafetyTracker_PerDEKSafetyConfig(t *testing.T) {
	t.Run("set and apply per-DEK config", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
		)

		// Set restrictive per-DEK config
		tracker.SetDEKConfig("/tenant/restricted", AEADSafetyConfig{
			MaxOperations:     3,
			MaxBytes:          300,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		})

		ctx := context.Background()

		// Should hit per-DEK limit
		for i := 0; i < 3; i++ {
			err := tracker.RecordDEKEncrypt(ctx, "/tenant/restricted", 50)
			if i < 2 {
				assert.NoError(t, err)
			}
		}

		err := tracker.RecordDEKEncrypt(ctx, "/tenant/restricted", 50)
		assert.Error(t, err)
	})

	t.Run("clear per-DEK config reverts to server defaults", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
		)

		tracker.SetDEKConfig("/tenant/test", AEADSafetyConfig{
			MaxOperations:     3,
			MaxBytes:          300,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		})
		tracker.ClearDEKConfig("/tenant/test")

		ctx := context.Background()

		// Should now use server config
		for i := 0; i < 50; i++ {
			err := tracker.RecordDEKEncrypt(ctx, "/tenant/test", 50)
			assert.NoError(t, err)
		}
	})
}

// =============================================================================
// Edge Cases Tests
// =============================================================================

func TestAEADSafetyTracker_EdgeCases_ZeroLimits(t *testing.T) {
	t.Run("zero max operations means unlimited", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     0, // No limit
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
		)

		ctx := context.Background()

		// Should never error on operation count
		for i := 0; i < 1000; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "test-key", 5)
			assert.NoError(t, err)
		}

		ops, _, _ := tracker.GetKeyUsage("test-key")
		assert.Equal(t, uint64(1000), ops)
	})

	t.Run("zero max bytes means unlimited", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     10000,
				MaxBytes:          0, // No limit
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
		)

		ctx := context.Background()

		// Should never error on byte count
		for i := 0; i < 100; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "test-key", 1<<30) // 1 GB each
			assert.NoError(t, err)
		}

		_, bytes, _ := tracker.GetKeyUsage("test-key")
		assert.Equal(t, uint64(100*(1<<30)), bytes)
	})

	t.Run("both limits zero means fully unlimited", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     0,
				MaxBytes:          0,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
		)

		ctx := context.Background()

		for i := 0; i < 500; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "test-key", 10000)
			assert.NoError(t, err)
		}
	})
}

func TestAEADSafetyTracker_EdgeCases_OverflowPrevention(t *testing.T) {
	t.Run("large operation counts do not overflow", func(t *testing.T) {
		usage := &AEADUsage{
			Config: AEADSafetyConfig{
				MaxOperations: math.MaxUint64,
				MaxBytes:      math.MaxUint64,
			},
		}

		// Start near the edge
		startOps := uint64(math.MaxUint64 - 10)
		usage.OperationCount.Store(startOps)
		usage.BytesProcessed.Store(0)

		// This should not panic or wrap around unexpectedly
		for i := 0; i < 5; i++ {
			usage.IncrementUsage(100)
		}

		expectedOps := startOps + 5
		assert.Equal(t, expectedOps, usage.OperationCount.Load())
	})

	t.Run("large byte counts do not overflow", func(t *testing.T) {
		usage := &AEADUsage{
			Config: AEADSafetyConfig{
				MaxOperations: math.MaxUint64,
				MaxBytes:      math.MaxUint64,
			},
		}

		startBytes := uint64(math.MaxUint64 - 1000)
		usage.BytesProcessed.Store(startBytes)
		usage.IncrementUsage(500)

		expectedBytes := startBytes + 500
		assert.Equal(t, expectedBytes, usage.BytesProcessed.Load())
	})

	t.Run("usage percent calculation with near-max values", func(t *testing.T) {
		usage := &AEADUsage{
			Config: AEADSafetyConfig{
				MaxOperations: 1 << 62, // Large but won't overflow when multiplied by 100
				MaxBytes:      1 << 62,
			},
		}

		usage.OperationCount.Store(1 << 60) // ~25%
		usage.BytesProcessed.Store(1 << 61) // ~50%

		pct := usage.UsagePercent()
		assert.Greater(t, pct, uint64(0))
		assert.LessOrEqual(t, pct, uint64(100))
	})
}

func TestAEADSafetyTracker_EdgeCases_EmptyIDs(t *testing.T) {
	t.Run("empty key ID is tracked", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
		)

		ctx := context.Background()
		err := tracker.RecordKeyEncrypt(ctx, "", 100)
		assert.NoError(t, err)

		ops, bytes, _ := tracker.GetKeyUsage("")
		assert.Equal(t, uint64(1), ops)
		assert.Equal(t, uint64(100), bytes)
	})

	t.Run("empty DEK path is tracked", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
		)

		ctx := context.Background()
		err := tracker.RecordDEKEncrypt(ctx, "", 200)
		assert.NoError(t, err)

		ops, bytes, _ := tracker.GetDEKUsage("")
		assert.Equal(t, uint64(1), ops)
		assert.Equal(t, uint64(200), bytes)
	})
}

func TestAEADSafetyTracker_EdgeCases_SpecialCharacters(t *testing.T) {
	specialKeys := []string{
		"/path/with/slashes",
		"key:with:colons",
		"key-with-dashes",
		"key_with_underscores",
		"key.with.dots",
		"key with spaces",
		"unicode-key-\u4e2d\u6587",
	}

	for _, keyID := range specialKeys {
		t.Run("key: "+keyID, func(t *testing.T) {
			tracker := NewAEADSafetyTracker(
				WithServerConfig(AEADSafetyConfig{
					MaxOperations:     100,
					MaxBytes:          10000,
					WarningPercent:    80,
					AutoRotatePercent: 95,
				}),
			)

			ctx := context.Background()
			err := tracker.RecordKeyEncrypt(ctx, keyID, 100)
			assert.NoError(t, err)

			ops, bytes, _ := tracker.GetKeyUsage(keyID)
			assert.Equal(t, uint64(1), ops)
			assert.Equal(t, uint64(100), bytes)
		})
	}
}

// =============================================================================
// KMS Service Layer Integration Tests
// =============================================================================

func TestKMSService_SafetyCheck_Integration(t *testing.T) {
	t.Run("safety check with populated tracker using per-key config", func(t *testing.T) {
		mockMgr := newMockKMSManager()
		logger := newMockLogger()
		svc := newKMSService(mockMgr, logger)

		// Get the tracker and set per-key config with smaller limits for testing
		tracker := svc.GetSafetyTracker()
		ctx := systemTenantCtx()

		// Set per-key config with max 100 operations
		tracker.SetKeyConfig("high-usage-key", AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		})
		tracker.SetKeyConfig("low-usage-key", AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		})

		// Populate usage: 85% for high-usage, 40% for low-usage
		for i := 0; i < 85; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "high-usage-key", 100)
		}
		for i := 0; i < 40; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "low-usage-key", 100)
		}

		// Check at 80% threshold - only high-usage-key should be returned
		resp, err := svc.SafetyCheck(ctx, &SafetyCheckRequest{
			ThresholdPercent: 80,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, 1, len(resp.Items))
		assert.Equal(t, "high-usage-key", resp.Items[0].ID)
	})

	t.Run("safety check with default threshold", func(t *testing.T) {
		mockMgr := newMockKMSManager()
		logger := newMockLogger()
		svc := newKMSService(mockMgr, logger)

		ctx := systemTenantCtx()
		resp, err := svc.SafetyCheck(ctx, &SafetyCheckRequest{
			ThresholdPercent: 0, // Should default to 80
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, float64(80), resp.ThresholdPercent)
	})

	t.Run("safety check with negative threshold uses default", func(t *testing.T) {
		mockMgr := newMockKMSManager()
		logger := newMockLogger()
		svc := newKMSService(mockMgr, logger)

		ctx := systemTenantCtx()
		resp, err := svc.SafetyCheck(ctx, &SafetyCheckRequest{
			ThresholdPercent: -10,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, float64(80), resp.ThresholdPercent)
	})
}

func TestKMSService_SafetyReport_Integration(t *testing.T) {
	t.Run("safety report includes server config", func(t *testing.T) {
		mockMgr := newMockKMSManager()
		logger := newMockLogger()
		svc := newKMSService(mockMgr, logger)

		ctx := systemTenantCtx()
		resp, err := svc.SafetyReport(ctx, &SafetyReportRequest{})

		require.NoError(t, err)
		require.NotNil(t, resp)

		// Server config should match defaults
		assert.True(t, resp.ServerConfig.Enforce)
		assert.Equal(t, uint64(1<<32), resp.ServerConfig.MaxOperations)
		assert.Equal(t, 80, resp.ServerConfig.WarningPercent)
		assert.Equal(t, 95, resp.ServerConfig.AutoRotatePercent)
	})

	t.Run("safety report aggregates totals", func(t *testing.T) {
		mockMgr := newMockKMSManager()
		logger := newMockLogger()
		svc := newKMSService(mockMgr, logger)

		tracker := svc.GetSafetyTracker()
		ctx := systemTenantCtx()

		_ = tracker.RecordKeyEncrypt(ctx, "key1", 100)
		_ = tracker.RecordKeyEncrypt(ctx, "key1", 200)
		_ = tracker.RecordKeyEncrypt(ctx, "key2", 300)
		_ = tracker.RecordDEKEncrypt(ctx, "/dek1", 400)

		resp, err := svc.SafetyReport(ctx, &SafetyReportRequest{})

		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, uint64(4), resp.TotalOperations)
		assert.Equal(t, uint64(1000), resp.TotalBytesProcessed)
		assert.Len(t, resp.Items, 3)
	})
}

func TestKMSService_SafetyTracker_Lifecycle(t *testing.T) {
	t.Run("tracker persists across operations", func(t *testing.T) {
		mockMgr := newMockKMSManager()
		logger := newMockLogger()
		svc := newKMSService(mockMgr, logger)

		tracker1 := svc.GetSafetyTracker()
		ctx := context.Background()

		_ = tracker1.RecordKeyEncrypt(ctx, "test-key", 100)

		tracker2 := svc.GetSafetyTracker()
		ops, bytes, _ := tracker2.GetKeyUsage("test-key")

		assert.Equal(t, uint64(1), ops)
		assert.Equal(t, uint64(100), bytes)
	})

	t.Run("multiple services have independent trackers", func(t *testing.T) {
		mockMgr1 := newMockKMSManager()
		mockMgr2 := newMockKMSManager()
		logger := newMockLogger()

		svc1 := newKMSService(mockMgr1, logger)
		svc2 := newKMSService(mockMgr2, logger)

		ctx := context.Background()

		tracker1 := svc1.GetSafetyTracker()
		tracker2 := svc2.GetSafetyTracker()

		_ = tracker1.RecordKeyEncrypt(ctx, "shared-key", 100)

		ops1, _, _ := tracker1.GetKeyUsage("shared-key")
		ops2, _, _ := tracker2.GetKeyUsage("shared-key")

		assert.Equal(t, uint64(1), ops1)
		assert.Equal(t, uint64(0), ops2) // Independent tracker
	})
}

// =============================================================================
// Timestamp Tests
// =============================================================================

func TestAEADUsage_LastUpdated_Timestamp(t *testing.T) {
	t.Run("timestamp is set on each increment", func(t *testing.T) {
		usage := &AEADUsage{
			Config: AEADSafetyConfig{
				MaxOperations: 100,
				MaxBytes:      10000,
			},
		}

		before := time.Now().UnixNano()
		usage.IncrementUsage(100)
		after := time.Now().UnixNano()

		lastUpdated := usage.LastUpdated.Load()
		assert.GreaterOrEqual(t, lastUpdated, before)
		assert.LessOrEqual(t, lastUpdated, after)
	})

	t.Run("timestamp is updated on subsequent increments", func(t *testing.T) {
		usage := &AEADUsage{
			Config: AEADSafetyConfig{
				MaxOperations: 100,
				MaxBytes:      10000,
			},
		}

		usage.IncrementUsage(100)
		first := usage.LastUpdated.Load()

		time.Sleep(time.Millisecond)

		usage.IncrementUsage(100)
		second := usage.LastUpdated.Load()

		assert.Greater(t, second, first)
	})
}

// =============================================================================
// Callback Context Tests
// =============================================================================

func TestAEADSafetyTracker_Callbacks_ReceiveCorrectContext(t *testing.T) {
	t.Run("warning callback receives context", func(t *testing.T) {
		var receivedCtx context.Context

		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithWarningCallback(func(ctx context.Context, keyID string, isDEK bool, msg string) {
				receivedCtx = ctx
			}),
		)

		type ctxKey string
		ctx := context.WithValue(context.Background(), ctxKey("test-key"), "test-value")

		for i := 0; i < 80; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "test-key", 50)
		}

		require.NotNil(t, receivedCtx)
		assert.Equal(t, "test-value", receivedCtx.Value(ctxKey("test-key")))
	})

	t.Run("rotation callback receives context", func(t *testing.T) {
		var receivedCtx context.Context

		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithRotationCallback(func(ctx context.Context, keyID string, isDEK bool) {
				receivedCtx = ctx
			}),
		)

		type ctxKey string
		ctx := context.WithValue(context.Background(), ctxKey("rotation-test"), "value123")

		for i := 0; i < 95; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "test-key", 50)
		}

		require.NotNil(t, receivedCtx)
		assert.Equal(t, "value123", receivedCtx.Value(ctxKey("rotation-test")))
	})
}
