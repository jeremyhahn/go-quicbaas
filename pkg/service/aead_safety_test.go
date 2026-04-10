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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// aeadTestLogger is a mock logger for AEAD safety tests with warning/error tracking.
type aeadTestLogger struct {
	mu       sync.Mutex
	fields   map[string]interface{}
	warnings []string
	errors   []string
}

func newAEADTestLogger() *aeadTestLogger {
	return &aeadTestLogger{
		fields:   make(map[string]interface{}),
		warnings: make([]string, 0),
		errors:   make([]string, 0),
	}
}

func (l *aeadTestLogger) Debug(msg string)                          {}
func (l *aeadTestLogger) Debugf(format string, args ...interface{}) {}
func (l *aeadTestLogger) Info(msg string)                           {}
func (l *aeadTestLogger) Infof(format string, args ...interface{})  {}
func (l *aeadTestLogger) Fatal(msg string)                          {}
func (l *aeadTestLogger) Fatalf(format string, args ...interface{}) {}

func (l *aeadTestLogger) Warn(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnings = append(l.warnings, msg)
}

func (l *aeadTestLogger) Warnf(format string, args ...interface{}) {
	l.Warn(format)
}

func (l *aeadTestLogger) Error(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors = append(l.errors, msg)
}

func (l *aeadTestLogger) Errorf(format string, args ...interface{}) {
	l.Error(format)
}

func (l *aeadTestLogger) WithField(key string, value interface{}) interfaces.Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fields[key] = value
	return l
}

func (l *aeadTestLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, v := range fields {
		l.fields[k] = v
	}
	return l
}

func (l *aeadTestLogger) WithError(err error) interfaces.Logger {
	return l.WithField("error", err)
}

func (l *aeadTestLogger) GetWarnings() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.warnings...)
}

func (l *aeadTestLogger) GetErrors() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.errors...)
}

// =============================================================================
// AEADSafetyConfig Tests
// =============================================================================

func TestDefaultAEADSafetyConfig(t *testing.T) {
	config := DefaultAEADSafetyConfig()

	assert.Equal(t, uint64(1<<32), config.MaxOperations, "MaxOperations should be 2^32")
	assert.Equal(t, uint64(375809638400), config.MaxBytes, "MaxBytes should be ~350GB")
	assert.Equal(t, 80, config.WarningPercent, "WarningPercent should be 80")
	assert.Equal(t, 95, config.AutoRotatePercent, "AutoRotatePercent should be 95")
}

// =============================================================================
// AEADUsage.IncrementUsage Tests
// =============================================================================

func TestAEADUsage_IncrementUsage(t *testing.T) {
	tests := []struct {
		name           string
		config         AEADSafetyConfig
		bytesToProcess []uint64
		expectedOps    uint64
		expectedBytes  uint64
		expectExceeded bool
	}{
		{
			name: "normal increment under limits",
			config: AEADSafetyConfig{
				MaxOperations: 1000,
				MaxBytes:      10000,
			},
			bytesToProcess: []uint64{100, 200, 300},
			expectedOps:    3,
			expectedBytes:  600,
			expectExceeded: false,
		},
		{
			name: "exceed operation limit",
			config: AEADSafetyConfig{
				MaxOperations: 3,
				MaxBytes:      100000,
			},
			bytesToProcess: []uint64{100, 200, 300},
			expectedOps:    3,
			expectedBytes:  600,
			expectExceeded: true,
		},
		{
			name: "exceed byte limit",
			config: AEADSafetyConfig{
				MaxOperations: 1000,
				MaxBytes:      500,
			},
			bytesToProcess: []uint64{200, 200, 200},
			expectedOps:    3,
			expectedBytes:  600,
			expectExceeded: true,
		},
		{
			name: "no limits configured",
			config: AEADSafetyConfig{
				MaxOperations: 0,
				MaxBytes:      0,
			},
			bytesToProcess: []uint64{1000000, 2000000},
			expectedOps:    2,
			expectedBytes:  3000000,
			expectExceeded: false,
		},
		{
			name: "exactly at operation limit",
			config: AEADSafetyConfig{
				MaxOperations: 5,
				MaxBytes:      100000,
			},
			bytesToProcess: []uint64{100, 100, 100, 100, 100},
			expectedOps:    5,
			expectedBytes:  500,
			expectExceeded: true,
		},
		{
			name: "exactly at byte limit",
			config: AEADSafetyConfig{
				MaxOperations: 1000,
				MaxBytes:      1000,
			},
			bytesToProcess: []uint64{500, 500},
			expectedOps:    2,
			expectedBytes:  1000,
			expectExceeded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := &AEADUsage{
				Config: tt.config,
			}

			var exceeded bool
			for _, bytes := range tt.bytesToProcess {
				exceeded = usage.IncrementUsage(bytes)
			}

			assert.Equal(t, tt.expectedOps, usage.OperationCount.Load())
			assert.Equal(t, tt.expectedBytes, usage.BytesProcessed.Load())
			assert.Equal(t, tt.expectExceeded, exceeded)

			// Verify LastUpdated was set
			assert.Greater(t, usage.LastUpdated.Load(), int64(0))
		})
	}
}

func TestAEADUsage_IncrementUsage_Concurrent(t *testing.T) {
	usage := &AEADUsage{
		Config: AEADSafetyConfig{
			MaxOperations: 100000,
			MaxBytes:      1000000,
		},
	}

	numGoroutines := 100
	opsPerGoroutine := 100
	bytesPerOp := uint64(50)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				usage.IncrementUsage(bytesPerOp)
			}
		}()
	}

	wg.Wait()

	expectedOps := uint64(numGoroutines * opsPerGoroutine)
	expectedBytes := expectedOps * bytesPerOp

	assert.Equal(t, expectedOps, usage.OperationCount.Load())
	assert.Equal(t, expectedBytes, usage.BytesProcessed.Load())
}

// =============================================================================
// AEADUsage.WarningNeeded Tests
// =============================================================================

func TestAEADUsage_WarningNeeded(t *testing.T) {
	tests := []struct {
		name           string
		config         AEADSafetyConfig
		opCount        uint64
		bytesProcessed uint64
		expectWarning  bool
		expectMsg      string
	}{
		{
			name: "under warning threshold",
			config: AEADSafetyConfig{
				MaxOperations:  1000,
				MaxBytes:       10000,
				WarningPercent: 80,
			},
			opCount:        500,
			bytesProcessed: 5000,
			expectWarning:  false,
		},
		{
			name: "at operation warning threshold",
			config: AEADSafetyConfig{
				MaxOperations:  1000,
				MaxBytes:       10000,
				WarningPercent: 80,
			},
			opCount:        800,
			bytesProcessed: 5000,
			expectWarning:  true,
			expectMsg:      "operation limit: 80% used (800/1000)",
		},
		{
			name: "at bytes warning threshold",
			config: AEADSafetyConfig{
				MaxOperations:  1000,
				MaxBytes:       10000,
				WarningPercent: 80,
			},
			opCount:        500,
			bytesProcessed: 8000,
			expectWarning:  true,
			expectMsg:      "bytes limit: 80% used (8000/10000)",
		},
		{
			name: "above warning threshold",
			config: AEADSafetyConfig{
				MaxOperations:  1000,
				MaxBytes:       10000,
				WarningPercent: 80,
			},
			opCount:        950,
			bytesProcessed: 9500,
			expectWarning:  true,
			expectMsg:      "operation limit: 95% used (950/1000)",
		},
		{
			name: "warning disabled",
			config: AEADSafetyConfig{
				MaxOperations:  1000,
				MaxBytes:       10000,
				WarningPercent: 0,
			},
			opCount:        900,
			bytesProcessed: 9000,
			expectWarning:  false,
		},
		{
			name: "negative warning percent treated as disabled",
			config: AEADSafetyConfig{
				MaxOperations:  1000,
				MaxBytes:       10000,
				WarningPercent: -1,
			},
			opCount:        900,
			bytesProcessed: 9000,
			expectWarning:  false,
		},
		{
			name: "bytes threshold triggers first",
			config: AEADSafetyConfig{
				MaxOperations:  10000,
				MaxBytes:       1000,
				WarningPercent: 80,
			},
			opCount:        100,
			bytesProcessed: 900,
			expectWarning:  true,
			expectMsg:      "bytes limit: 90% used (900/1000)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := &AEADUsage{
				Config: tt.config,
			}
			usage.OperationCount.Store(tt.opCount)
			usage.BytesProcessed.Store(tt.bytesProcessed)

			warning, msg := usage.WarningNeeded()

			assert.Equal(t, tt.expectWarning, warning)
			if tt.expectWarning && tt.expectMsg != "" {
				assert.Contains(t, msg, tt.expectMsg)
			}
		})
	}
}

// =============================================================================
// AEADUsage.AutoRotateNeeded Tests
// =============================================================================

func TestAEADUsage_AutoRotateNeeded(t *testing.T) {
	tests := []struct {
		name           string
		config         AEADSafetyConfig
		opCount        uint64
		bytesProcessed uint64
		expectRotate   bool
	}{
		{
			name: "under auto-rotate threshold",
			config: AEADSafetyConfig{
				MaxOperations:     1000,
				MaxBytes:          10000,
				AutoRotatePercent: 95,
			},
			opCount:        900,
			bytesProcessed: 9000,
			expectRotate:   false,
		},
		{
			name: "at operation auto-rotate threshold",
			config: AEADSafetyConfig{
				MaxOperations:     1000,
				MaxBytes:          10000,
				AutoRotatePercent: 95,
			},
			opCount:        950,
			bytesProcessed: 8000,
			expectRotate:   true,
		},
		{
			name: "at bytes auto-rotate threshold",
			config: AEADSafetyConfig{
				MaxOperations:     1000,
				MaxBytes:          10000,
				AutoRotatePercent: 95,
			},
			opCount:        800,
			bytesProcessed: 9500,
			expectRotate:   true,
		},
		{
			name: "above auto-rotate threshold",
			config: AEADSafetyConfig{
				MaxOperations:     1000,
				MaxBytes:          10000,
				AutoRotatePercent: 95,
			},
			opCount:        990,
			bytesProcessed: 9900,
			expectRotate:   true,
		},
		{
			name: "auto-rotate disabled",
			config: AEADSafetyConfig{
				MaxOperations:     1000,
				MaxBytes:          10000,
				AutoRotatePercent: 0,
			},
			opCount:        990,
			bytesProcessed: 9900,
			expectRotate:   false,
		},
		{
			name: "negative auto-rotate percent treated as disabled",
			config: AEADSafetyConfig{
				MaxOperations:     1000,
				MaxBytes:          10000,
				AutoRotatePercent: -1,
			},
			opCount:        990,
			bytesProcessed: 9900,
			expectRotate:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := &AEADUsage{
				Config: tt.config,
			}
			usage.OperationCount.Store(tt.opCount)
			usage.BytesProcessed.Store(tt.bytesProcessed)

			rotate := usage.AutoRotateNeeded()

			assert.Equal(t, tt.expectRotate, rotate)
		})
	}
}

// =============================================================================
// AEADUsage.UsagePercent Tests
// =============================================================================

func TestAEADUsage_UsagePercent(t *testing.T) {
	tests := []struct {
		name           string
		config         AEADSafetyConfig
		opCount        uint64
		bytesProcessed uint64
		expectedPct    uint64
	}{
		{
			name: "ops higher than bytes",
			config: AEADSafetyConfig{
				MaxOperations: 1000,
				MaxBytes:      10000,
			},
			opCount:        800,
			bytesProcessed: 5000,
			expectedPct:    80,
		},
		{
			name: "bytes higher than ops",
			config: AEADSafetyConfig{
				MaxOperations: 1000,
				MaxBytes:      10000,
			},
			opCount:        500,
			bytesProcessed: 9000,
			expectedPct:    90,
		},
		{
			name: "both at same percentage",
			config: AEADSafetyConfig{
				MaxOperations: 1000,
				MaxBytes:      10000,
			},
			opCount:        750,
			bytesProcessed: 7500,
			expectedPct:    75,
		},
		{
			name: "no limits returns zero",
			config: AEADSafetyConfig{
				MaxOperations: 0,
				MaxBytes:      0,
			},
			opCount:        1000000,
			bytesProcessed: 1000000,
			expectedPct:    0,
		},
		{
			name: "only ops limit",
			config: AEADSafetyConfig{
				MaxOperations: 1000,
				MaxBytes:      0,
			},
			opCount:        850,
			bytesProcessed: 1000000,
			expectedPct:    85,
		},
		{
			name: "only bytes limit",
			config: AEADSafetyConfig{
				MaxOperations: 0,
				MaxBytes:      10000,
			},
			opCount:        1000000,
			bytesProcessed: 8500,
			expectedPct:    85,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := &AEADUsage{
				Config: tt.config,
			}
			usage.OperationCount.Store(tt.opCount)
			usage.BytesProcessed.Store(tt.bytesProcessed)

			pct := usage.UsagePercent()

			assert.Equal(t, tt.expectedPct, pct)
		})
	}
}

// =============================================================================
// AEADSafetyTracker Creation and Options Tests
// =============================================================================

func TestNewAEADSafetyTracker(t *testing.T) {
	t.Run("default configuration", func(t *testing.T) {
		tracker := NewAEADSafetyTracker()

		assert.NotNil(t, tracker)
		assert.NotNil(t, tracker.metrics)
		assert.True(t, tracker.enforceDefaults)
		assert.Equal(t, DefaultAEADSafetyConfig(), tracker.serverConfig)
	})

	t.Run("with custom server config", func(t *testing.T) {
		customConfig := AEADSafetyConfig{
			MaxOperations:     1000,
			MaxBytes:          10000,
			WarningPercent:    70,
			AutoRotatePercent: 90,
		}

		tracker := NewAEADSafetyTracker(WithServerConfig(customConfig))

		assert.Equal(t, customConfig, tracker.serverConfig)
	})

	t.Run("with enforce defaults disabled", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithEnforceDefaults(false))

		assert.False(t, tracker.enforceDefaults)
	})

	t.Run("with logger", func(t *testing.T) {
		logger := newAEADTestLogger()
		tracker := NewAEADSafetyTracker(WithAEADLogger(logger))

		assert.Equal(t, logger, tracker.logger)
	})

	t.Run("with rotation callback", func(t *testing.T) {
		called := false
		callback := func(ctx context.Context, keyID string, isDEK bool) {
			called = true
		}

		tracker := NewAEADSafetyTracker(WithRotationCallback(callback))
		assert.NotNil(t, tracker.rotationCallback)

		tracker.rotationCallback(context.Background(), "test", false)
		assert.True(t, called)
	})

	t.Run("with warning callback", func(t *testing.T) {
		called := false
		callback := func(ctx context.Context, keyID string, isDEK bool, message string) {
			called = true
		}

		tracker := NewAEADSafetyTracker(WithWarningCallback(callback))
		assert.NotNil(t, tracker.warningCallback)

		tracker.warningCallback(context.Background(), "test", false, "warning")
		assert.True(t, called)
	})

	t.Run("with multiple options", func(t *testing.T) {
		logger := newAEADTestLogger()
		customConfig := AEADSafetyConfig{
			MaxOperations:     500,
			MaxBytes:          5000,
			WarningPercent:    75,
			AutoRotatePercent: 92,
		}

		tracker := NewAEADSafetyTracker(
			WithServerConfig(customConfig),
			WithEnforceDefaults(false),
			WithAEADLogger(logger),
		)

		assert.Equal(t, customConfig, tracker.serverConfig)
		assert.False(t, tracker.enforceDefaults)
		assert.Equal(t, logger, tracker.logger)
	})
}

// =============================================================================
// AEADSafetyTracker.RecordKeyEncrypt Tests
// =============================================================================

func TestAEADSafetyTracker_RecordKeyEncrypt(t *testing.T) {
	t.Run("successful record under limit", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		err := tracker.RecordKeyEncrypt(ctx, "test-key", 100)

		assert.NoError(t, err)

		ops, bytes, pct := tracker.GetKeyUsage("test-key")
		assert.Equal(t, uint64(1), ops)
		assert.Equal(t, uint64(100), bytes)
		assert.Equal(t, 1, pct)
	})

	t.Run("exceed operation limit", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     2,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		_ = tracker.RecordKeyEncrypt(ctx, "test-key", 100)
		err := tracker.RecordKeyEncrypt(ctx, "test-key", 100)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "AEAD safety limit exceeded")
		assert.Contains(t, err.Error(), "key test-key")
	})

	t.Run("exceed byte limit", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          150,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		err := tracker.RecordKeyEncrypt(ctx, "test-key", 200)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "AEAD safety limit exceeded")
	})

	t.Run("multiple keys tracked independently", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		_ = tracker.RecordKeyEncrypt(ctx, "key1", 100)
		_ = tracker.RecordKeyEncrypt(ctx, "key1", 200)
		_ = tracker.RecordKeyEncrypt(ctx, "key2", 300)

		ops1, bytes1, _ := tracker.GetKeyUsage("key1")
		ops2, bytes2, _ := tracker.GetKeyUsage("key2")

		assert.Equal(t, uint64(2), ops1)
		assert.Equal(t, uint64(300), bytes1)
		assert.Equal(t, uint64(1), ops2)
		assert.Equal(t, uint64(300), bytes2)
	})
}

// =============================================================================
// AEADSafetyTracker.RecordDEKEncrypt Tests
// =============================================================================

func TestAEADSafetyTracker_RecordDEKEncrypt(t *testing.T) {
	t.Run("successful record under limit", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		err := tracker.RecordDEKEncrypt(ctx, "/tenant/app/dek1", 500)

		assert.NoError(t, err)

		ops, bytes, pct := tracker.GetDEKUsage("/tenant/app/dek1")
		assert.Equal(t, uint64(1), ops)
		assert.Equal(t, uint64(500), bytes)
		assert.Greater(t, pct, 0)
	})

	t.Run("exceed limit returns error", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     1,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		err := tracker.RecordDEKEncrypt(ctx, "/tenant/app/dek1", 100)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "AEAD safety limit exceeded")
		assert.Contains(t, err.Error(), "dek /tenant/app/dek1")
	})

	t.Run("multiple DEKs tracked independently", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		_ = tracker.RecordDEKEncrypt(ctx, "/tenant/app/dek1", 100)
		_ = tracker.RecordDEKEncrypt(ctx, "/tenant/app/dek2", 200)
		_ = tracker.RecordDEKEncrypt(ctx, "/tenant/app/dek1", 150)

		ops1, bytes1, _ := tracker.GetDEKUsage("/tenant/app/dek1")
		ops2, bytes2, _ := tracker.GetDEKUsage("/tenant/app/dek2")

		assert.Equal(t, uint64(2), ops1)
		assert.Equal(t, uint64(250), bytes1)
		assert.Equal(t, uint64(1), ops2)
		assert.Equal(t, uint64(200), bytes2)
	})
}

// =============================================================================
// Callback Tests
// =============================================================================

func TestAEADSafetyTracker_WarningCallback(t *testing.T) {
	t.Run("warning callback invoked at threshold", func(t *testing.T) {
		var callbackInvoked atomic.Bool
		var receivedKeyID string
		var receivedIsDEK bool
		var receivedMessage string

		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithWarningCallback(func(ctx context.Context, keyID string, isDEK bool, message string) {
				callbackInvoked.Store(true)
				receivedKeyID = keyID
				receivedIsDEK = isDEK
				receivedMessage = message
			}),
		)

		ctx := context.Background()
		// Perform 80 operations to hit warning threshold
		for i := 0; i < 80; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "warn-key", 50)
		}

		assert.True(t, callbackInvoked.Load())
		assert.Equal(t, "warn-key", receivedKeyID)
		assert.False(t, receivedIsDEK)
		assert.Contains(t, receivedMessage, "operation limit")
	})

	t.Run("warning callback for DEK", func(t *testing.T) {
		var callbackInvoked atomic.Bool
		var receivedIsDEK bool

		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          1000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithWarningCallback(func(ctx context.Context, keyID string, isDEK bool, message string) {
				callbackInvoked.Store(true)
				receivedIsDEK = isDEK
			}),
		)

		ctx := context.Background()
		_ = tracker.RecordDEKEncrypt(ctx, "/dek/path", 800)

		assert.True(t, callbackInvoked.Load())
		assert.True(t, receivedIsDEK)
	})

	t.Run("warning callback not invoked under threshold", func(t *testing.T) {
		var callbackInvoked atomic.Bool

		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithWarningCallback(func(ctx context.Context, keyID string, isDEK bool, message string) {
				callbackInvoked.Store(true)
			}),
		)

		ctx := context.Background()
		_ = tracker.RecordKeyEncrypt(ctx, "test-key", 50)

		assert.False(t, callbackInvoked.Load())
	})
}

func TestAEADSafetyTracker_RotationCallback(t *testing.T) {
	t.Run("rotation callback invoked at auto-rotate threshold", func(t *testing.T) {
		var callbackInvoked atomic.Bool
		var receivedKeyID string
		var receivedIsDEK bool

		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithRotationCallback(func(ctx context.Context, keyID string, isDEK bool) {
				callbackInvoked.Store(true)
				receivedKeyID = keyID
				receivedIsDEK = isDEK
			}),
		)

		ctx := context.Background()
		// Perform 95 operations to hit rotation threshold
		for i := 0; i < 95; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "rotate-key", 50)
		}

		assert.True(t, callbackInvoked.Load())
		assert.Equal(t, "rotate-key", receivedKeyID)
		assert.False(t, receivedIsDEK)
	})

	t.Run("rotation callback for DEK", func(t *testing.T) {
		var callbackInvoked atomic.Bool
		var receivedIsDEK bool
		var receivedDEKPath string

		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          1000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithRotationCallback(func(ctx context.Context, keyID string, isDEK bool) {
				callbackInvoked.Store(true)
				receivedIsDEK = isDEK
				receivedDEKPath = keyID
			}),
		)

		ctx := context.Background()
		_ = tracker.RecordDEKEncrypt(ctx, "/tenant/dek", 950)

		assert.True(t, callbackInvoked.Load())
		assert.True(t, receivedIsDEK)
		assert.Equal(t, "/tenant/dek", receivedDEKPath)
	})

	t.Run("rotation callback not invoked under threshold", func(t *testing.T) {
		var callbackInvoked atomic.Bool

		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithRotationCallback(func(ctx context.Context, keyID string, isDEK bool) {
				callbackInvoked.Store(true)
			}),
		)

		ctx := context.Background()
		for i := 0; i < 90; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "test-key", 50)
		}

		assert.False(t, callbackInvoked.Load())
	})
}

func TestAEADSafetyTracker_BothCallbacks(t *testing.T) {
	t.Run("both warning and rotation callbacks invoked in sequence", func(t *testing.T) {
		var warningCalled atomic.Bool
		var rotationCalled atomic.Bool

		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithWarningCallback(func(ctx context.Context, keyID string, isDEK bool, message string) {
				warningCalled.Store(true)
			}),
			WithRotationCallback(func(ctx context.Context, keyID string, isDEK bool) {
				rotationCalled.Store(true)
			}),
		)

		ctx := context.Background()
		// Hit warning threshold
		for i := 0; i < 80; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "test-key", 50)
		}

		assert.True(t, warningCalled.Load())
		assert.False(t, rotationCalled.Load())

		// Hit rotation threshold
		for i := 0; i < 15; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "test-key", 50)
		}

		assert.True(t, rotationCalled.Load())
	})
}

// =============================================================================
// Config Inheritance Tests
// =============================================================================

func TestAEADSafetyTracker_ConfigInheritance(t *testing.T) {
	t.Run("enforce defaults enabled uses server config", func(t *testing.T) {
		serverConfig := AEADSafetyConfig{
			MaxOperations:     50,
			MaxBytes:          5000,
			WarningPercent:    70,
			AutoRotatePercent: 90,
		}

		tracker := NewAEADSafetyTracker(
			WithServerConfig(serverConfig),
			WithEnforceDefaults(true),
		)

		ctx := context.Background()
		// Exceed the server limit
		for i := 0; i < 50; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "test-key", 100)
		}

		err := tracker.RecordKeyEncrypt(ctx, "test-key", 100)
		assert.Error(t, err)
	})

	t.Run("enforce defaults disabled allows unlimited operations", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithEnforceDefaults(false),
		)

		ctx := context.Background()
		// Should allow many operations without error
		for i := 0; i < 1000; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "test-key", 1000)
			assert.NoError(t, err)
		}
	})

	t.Run("per-key config overrides server config", func(t *testing.T) {
		serverConfig := AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}

		keyConfig := AEADSafetyConfig{
			MaxOperations:     10,
			MaxBytes:          1000,
			WarningPercent:    70,
			AutoRotatePercent: 85,
		}

		tracker := NewAEADSafetyTracker(
			WithServerConfig(serverConfig),
			WithEnforceDefaults(true),
		)

		tracker.SetKeyConfig("restricted-key", keyConfig)

		ctx := context.Background()
		// Should hit the lower per-key limit
		for i := 0; i < 10; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "restricted-key", 50)
		}

		err := tracker.RecordKeyEncrypt(ctx, "restricted-key", 50)
		assert.Error(t, err)

		// Other keys should still use server config
		for i := 0; i < 50; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "normal-key", 50)
			assert.NoError(t, err)
		}
	})

	t.Run("per-DEK config overrides server config", func(t *testing.T) {
		serverConfig := AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}

		dekConfig := AEADSafetyConfig{
			MaxOperations:     5,
			MaxBytes:          500,
			WarningPercent:    60,
			AutoRotatePercent: 80,
		}

		tracker := NewAEADSafetyTracker(
			WithServerConfig(serverConfig),
			WithEnforceDefaults(true),
		)

		tracker.SetDEKConfig("/restricted/dek", dekConfig)

		ctx := context.Background()
		for i := 0; i < 5; i++ {
			_ = tracker.RecordDEKEncrypt(ctx, "/restricted/dek", 50)
		}

		err := tracker.RecordDEKEncrypt(ctx, "/restricted/dek", 50)
		assert.Error(t, err)
	})

	t.Run("clear key config reverts to server defaults", func(t *testing.T) {
		serverConfig := AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}

		customConfig := AEADSafetyConfig{
			MaxOperations:     10,
			MaxBytes:          1000,
			WarningPercent:    70,
			AutoRotatePercent: 85,
		}

		tracker := NewAEADSafetyTracker(
			WithServerConfig(serverConfig),
			WithEnforceDefaults(true),
		)

		// Set custom config
		tracker.SetKeyConfig("test-key", customConfig)

		// Clear it
		tracker.ClearKeyConfig("test-key")

		ctx := context.Background()
		// Should now use server config allowing more operations
		for i := 0; i < 50; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "test-key", 100)
			assert.NoError(t, err)
		}
	})

	t.Run("clear DEK config reverts to server defaults", func(t *testing.T) {
		serverConfig := AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}

		customConfig := AEADSafetyConfig{
			MaxOperations:     5,
			MaxBytes:          500,
			WarningPercent:    60,
			AutoRotatePercent: 80,
		}

		tracker := NewAEADSafetyTracker(
			WithServerConfig(serverConfig),
			WithEnforceDefaults(true),
		)

		tracker.SetDEKConfig("/test/dek", customConfig)
		tracker.ClearDEKConfig("/test/dek")

		ctx := context.Background()
		for i := 0; i < 50; i++ {
			err := tracker.RecordDEKEncrypt(ctx, "/test/dek", 100)
			assert.NoError(t, err)
		}
	})

	t.Run("clear config with enforce defaults disabled allows unlimited", func(t *testing.T) {
		customConfig := AEADSafetyConfig{
			MaxOperations:     10,
			MaxBytes:          1000,
			WarningPercent:    70,
			AutoRotatePercent: 85,
		}

		tracker := NewAEADSafetyTracker(
			WithEnforceDefaults(false),
		)

		tracker.SetKeyConfig("test-key", customConfig)
		tracker.ClearKeyConfig("test-key")

		ctx := context.Background()
		// Should allow unlimited operations
		for i := 0; i < 200; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "test-key", 1000)
			assert.NoError(t, err)
		}
	})
}

// =============================================================================
// SafetyCheck Tests
// =============================================================================

func TestAEADSafetyTracker_SafetyCheck(t *testing.T) {
	t.Run("returns keys above threshold", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		// key1: 50% usage
		for i := 0; i < 50; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "key1", 50)
		}
		// key2: 85% usage
		for i := 0; i < 85; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "key2", 50)
		}
		// key3: 30% usage
		for i := 0; i < 30; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "key3", 50)
		}

		results := tracker.SafetyCheck(80)

		assert.Len(t, results, 1)
		assert.Equal(t, "key2", results[0].ID)
		assert.Equal(t, "key", results[0].Type)
		assert.Equal(t, uint64(85), results[0].OperationCount)
		assert.Equal(t, 85, results[0].UsagePercent)
	})

	t.Run("returns both keys and DEKs above threshold", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		// Key at 90%
		for i := 0; i < 90; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "high-key", 50)
		}
		// DEK at 85%
		for i := 0; i < 85; i++ {
			_ = tracker.RecordDEKEncrypt(ctx, "/high/dek", 50)
		}
		// Another key at 40%
		for i := 0; i < 40; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "low-key", 50)
		}

		results := tracker.SafetyCheck(80)

		require.Len(t, results, 2)

		// Find results by type
		var keyResult, dekResult *SafetyCheckResult
		for i := range results {
			if results[i].Type == "key" {
				keyResult = &results[i]
			} else if results[i].Type == "dek" {
				dekResult = &results[i]
			}
		}

		require.NotNil(t, keyResult)
		require.NotNil(t, dekResult)

		assert.Equal(t, "high-key", keyResult.ID)
		assert.Equal(t, 90, keyResult.UsagePercent)

		assert.Equal(t, "/high/dek", dekResult.ID)
		assert.Equal(t, 85, dekResult.UsagePercent)
	})

	t.Run("empty results when all below threshold", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		_ = tracker.RecordKeyEncrypt(ctx, "key1", 100)
		_ = tracker.RecordKeyEncrypt(ctx, "key2", 200)

		results := tracker.SafetyCheck(90)

		assert.Empty(t, results)
	})

	t.Run("threshold of 0 returns all", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		_ = tracker.RecordKeyEncrypt(ctx, "key1", 100)
		_ = tracker.RecordKeyEncrypt(ctx, "key2", 200)
		_ = tracker.RecordDEKEncrypt(ctx, "/dek1", 150)

		results := tracker.SafetyCheck(0)

		assert.Len(t, results, 3)
	})
}

// =============================================================================
// SafetyReport Tests
// =============================================================================

func TestAEADSafetyTracker_SafetyReport(t *testing.T) {
	t.Run("returns all tracked keys and DEKs", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		_ = tracker.RecordKeyEncrypt(ctx, "key1", 100)
		_ = tracker.RecordKeyEncrypt(ctx, "key2", 200)
		_ = tracker.RecordDEKEncrypt(ctx, "/dek1", 150)
		_ = tracker.RecordDEKEncrypt(ctx, "/dek2", 250)

		report := tracker.SafetyReport()

		assert.Len(t, report, 4)

		// Verify all items are present
		ids := make(map[string]bool)
		for _, r := range report {
			ids[r.ID] = true
			assert.NotEmpty(t, r.Type)
			assert.Greater(t, r.OperationCount, uint64(0))
			assert.Greater(t, r.BytesProcessed, uint64(0))
		}

		assert.True(t, ids["key1"])
		assert.True(t, ids["key2"])
		assert.True(t, ids["/dek1"])
		assert.True(t, ids["/dek2"])
	})

	t.Run("empty report when no operations recorded", func(t *testing.T) {
		tracker := NewAEADSafetyTracker()

		report := tracker.SafetyReport()

		assert.Empty(t, report)
	})

	t.Run("report includes correct limits", func(t *testing.T) {
		config := AEADSafetyConfig{
			MaxOperations:     1000,
			MaxBytes:          50000,
			WarningPercent:    75,
			AutoRotatePercent: 90,
		}

		tracker := NewAEADSafetyTracker(WithServerConfig(config))

		ctx := context.Background()
		_ = tracker.RecordKeyEncrypt(ctx, "test-key", 1000)

		report := tracker.SafetyReport()

		require.Len(t, report, 1)
		assert.Equal(t, uint64(1000), report[0].MaxOperations)
		assert.Equal(t, uint64(50000), report[0].MaxBytes)
	})
}

// =============================================================================
// Reset Usage Tests
// =============================================================================

func TestAEADSafetyTracker_ResetUsage(t *testing.T) {
	t.Run("reset key usage after rotation", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		// Perform some operations
		for i := 0; i < 50; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "test-key", 100)
		}

		ops, bytes, pct := tracker.GetKeyUsage("test-key")
		assert.Equal(t, uint64(50), ops)
		assert.Greater(t, bytes, uint64(0))
		assert.Greater(t, pct, 0)

		// Reset usage
		tracker.ResetKeyUsage("test-key")

		ops, bytes, pct = tracker.GetKeyUsage("test-key")
		assert.Equal(t, uint64(0), ops)
		assert.Equal(t, uint64(0), bytes)
		assert.Equal(t, 0, pct)

		// Should be able to use again
		err := tracker.RecordKeyEncrypt(ctx, "test-key", 100)
		assert.NoError(t, err)

		ops, bytes, _ = tracker.GetKeyUsage("test-key")
		assert.Equal(t, uint64(1), ops)
		assert.Equal(t, uint64(100), bytes)
	})

	t.Run("reset DEK usage after rotation", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		for i := 0; i < 90; i++ {
			_ = tracker.RecordDEKEncrypt(ctx, "/test/dek", 100)
		}

		ops, bytes, pct := tracker.GetDEKUsage("/test/dek")
		assert.Equal(t, uint64(90), ops)
		assert.Greater(t, bytes, uint64(0))
		assert.Greater(t, pct, 0)

		tracker.ResetDEKUsage("/test/dek")

		ops, bytes, pct = tracker.GetDEKUsage("/test/dek")
		assert.Equal(t, uint64(0), ops)
		assert.Equal(t, uint64(0), bytes)
		assert.Equal(t, 0, pct)
	})

	t.Run("reset non-existent key does not panic", func(t *testing.T) {
		tracker := NewAEADSafetyTracker()

		assert.NotPanics(t, func() {
			tracker.ResetKeyUsage("non-existent")
			tracker.ResetDEKUsage("/non/existent")
		})
	})

	t.Run("reset after limit exceeded allows new operations", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     10,
			MaxBytes:          1000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		// Hit the limit
		for i := 0; i < 10; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "test-key", 50)
		}

		err := tracker.RecordKeyEncrypt(ctx, "test-key", 50)
		assert.Error(t, err)

		// Reset
		tracker.ResetKeyUsage("test-key")

		// Should work again
		for i := 0; i < 9; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "test-key", 50)
			assert.NoError(t, err)
		}
	})
}

// =============================================================================
// GetUsage Tests
// =============================================================================

func TestAEADSafetyTracker_GetUsage(t *testing.T) {
	t.Run("get key usage returns correct values", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		_ = tracker.RecordKeyEncrypt(ctx, "test-key", 1000)
		_ = tracker.RecordKeyEncrypt(ctx, "test-key", 2000)

		ops, bytes, pct := tracker.GetKeyUsage("test-key")

		assert.Equal(t, uint64(2), ops)
		assert.Equal(t, uint64(3000), bytes)
		assert.Greater(t, pct, 0)
	})

	t.Run("get DEK usage returns correct values", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		_ = tracker.RecordDEKEncrypt(ctx, "/test/dek", 500)
		_ = tracker.RecordDEKEncrypt(ctx, "/test/dek", 1500)

		ops, bytes, pct := tracker.GetDEKUsage("/test/dek")

		assert.Equal(t, uint64(2), ops)
		assert.Equal(t, uint64(2000), bytes)
		assert.Greater(t, pct, 0)
	})

	t.Run("get non-existent key returns zeros", func(t *testing.T) {
		tracker := NewAEADSafetyTracker()

		ops, bytes, pct := tracker.GetKeyUsage("non-existent")

		assert.Equal(t, uint64(0), ops)
		assert.Equal(t, uint64(0), bytes)
		assert.Equal(t, 0, pct)
	})

	t.Run("get non-existent DEK returns zeros", func(t *testing.T) {
		tracker := NewAEADSafetyTracker()

		ops, bytes, pct := tracker.GetDEKUsage("/non/existent")

		assert.Equal(t, uint64(0), ops)
		assert.Equal(t, uint64(0), bytes)
		assert.Equal(t, 0, pct)
	})
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestAEADSafetyTracker_ConcurrentAccess(t *testing.T) {
	t.Run("concurrent RecordKeyEncrypt operations", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100000,
			MaxBytes:          10000000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		numGoroutines := 50
		opsPerGoroutine := 100
		bytesPerOp := uint64(100)

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < opsPerGoroutine; j++ {
					_ = tracker.RecordKeyEncrypt(ctx, "concurrent-key", bytesPerOp)
				}
			}()
		}

		wg.Wait()

		ops, bytes, _ := tracker.GetKeyUsage("concurrent-key")
		expectedOps := uint64(numGoroutines * opsPerGoroutine)
		expectedBytes := expectedOps * bytesPerOp

		assert.Equal(t, expectedOps, ops)
		assert.Equal(t, expectedBytes, bytes)
	})

	t.Run("concurrent RecordDEKEncrypt operations", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100000,
			MaxBytes:          10000000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		numGoroutines := 50
		opsPerGoroutine := 100
		bytesPerOp := uint64(150)

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < opsPerGoroutine; j++ {
					_ = tracker.RecordDEKEncrypt(ctx, "/concurrent/dek", bytesPerOp)
				}
			}()
		}

		wg.Wait()

		ops, bytes, _ := tracker.GetDEKUsage("/concurrent/dek")
		expectedOps := uint64(numGoroutines * opsPerGoroutine)
		expectedBytes := expectedOps * bytesPerOp

		assert.Equal(t, expectedOps, ops)
		assert.Equal(t, expectedBytes, bytes)
	})

	t.Run("concurrent mixed operations", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100000,
			MaxBytes:          10000000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		var wg sync.WaitGroup
		numGoroutines := 10

		// Keys
		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()
				keyID := "key-" + string(rune('0'+id))
				for j := 0; j < 50; j++ {
					_ = tracker.RecordKeyEncrypt(ctx, keyID, 100)
				}
			}(i)
		}

		// DEKs
		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()
				dekPath := "/dek-" + string(rune('0'+id))
				for j := 0; j < 50; j++ {
					_ = tracker.RecordDEKEncrypt(ctx, dekPath, 100)
				}
			}(i)
		}

		wg.Wait()

		report := tracker.SafetyReport()
		assert.Len(t, report, numGoroutines*2)
	})

	t.Run("concurrent callbacks invocation", func(t *testing.T) {
		var warningCount atomic.Int32
		var rotationCount atomic.Int32

		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     1000,
				MaxBytes:          100000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithWarningCallback(func(ctx context.Context, keyID string, isDEK bool, message string) {
				warningCount.Add(1)
			}),
			WithRotationCallback(func(ctx context.Context, keyID string, isDEK bool) {
				rotationCount.Add(1)
			}),
		)

		ctx := context.Background()
		var wg sync.WaitGroup
		numGoroutines := 10

		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					_ = tracker.RecordKeyEncrypt(ctx, "callback-key", 100)
				}
			}()
		}

		wg.Wait()

		// Callbacks should have been invoked
		assert.Greater(t, warningCount.Load(), int32(0))
		assert.Greater(t, rotationCount.Load(), int32(0))
	})
}

// =============================================================================
// Edge Cases and Error Handling Tests
// =============================================================================

func TestAEADSafetyTracker_EdgeCases(t *testing.T) {
	t.Run("zero bytes operation increments counter", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     10,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		for i := 0; i < 5; i++ {
			err := tracker.RecordKeyEncrypt(ctx, "test-key", 0)
			assert.NoError(t, err)
		}

		ops, bytes, _ := tracker.GetKeyUsage("test-key")
		assert.Equal(t, uint64(5), ops)
		assert.Equal(t, uint64(0), bytes)
	})

	t.Run("large byte count single operation", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          1000000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		err := tracker.RecordKeyEncrypt(ctx, "test-key", 2000000)

		assert.Error(t, err)
	})

	t.Run("LastUpdated timestamp is set", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		beforeTime := time.Now().UnixNano()
		_ = tracker.RecordKeyEncrypt(ctx, "test-key", 100)
		afterTime := time.Now().UnixNano()

		// Access the usage directly to check LastUpdated
		val, ok := tracker.keyUsage.Load("test-key")
		require.True(t, ok)
		usage := val.(*AEADUsage)

		lastUpdated := usage.LastUpdated.Load()
		assert.GreaterOrEqual(t, lastUpdated, beforeTime)
		assert.LessOrEqual(t, lastUpdated, afterTime)
	})

	t.Run("metrics are recorded", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(WithServerConfig(AEADSafetyConfig{
			MaxOperations:     100,
			MaxBytes:          10000,
			WarningPercent:    80,
			AutoRotatePercent: 95,
		}))

		ctx := context.Background()
		_ = tracker.RecordKeyEncrypt(ctx, "test-key", 1000)

		// Metrics should be initialized and not nil
		assert.NotNil(t, tracker.metrics)
		assert.NotNil(t, tracker.metrics.operationsTotal)
		assert.NotNil(t, tracker.metrics.bytesProcessed)
		assert.NotNil(t, tracker.metrics.usagePercent)
	})

	t.Run("logger is called on warning", func(t *testing.T) {
		logger := newAEADTestLogger()
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithAEADLogger(logger),
		)

		ctx := context.Background()
		for i := 0; i < 80; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "test-key", 100)
		}

		// Check logger was called
		assert.Greater(t, len(logger.GetWarnings()), 0)
	})

	t.Run("logger is called on auto-rotation", func(t *testing.T) {
		logger := newAEADTestLogger()
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     100,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithAEADLogger(logger),
		)

		ctx := context.Background()
		for i := 0; i < 95; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "test-key", 100)
		}

		warnings := logger.GetWarnings()
		assert.Greater(t, len(warnings), 0)

		// Should have auto-rotation warning
		found := false
		for _, w := range warnings {
			if w == "AEAD safety auto-rotation triggered" {
				found = true
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("logger is called on limit exceeded", func(t *testing.T) {
		logger := newAEADTestLogger()
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     5,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithAEADLogger(logger),
		)

		ctx := context.Background()
		for i := 0; i < 5; i++ {
			_ = tracker.RecordKeyEncrypt(ctx, "test-key", 100)
		}
		_ = tracker.RecordKeyEncrypt(ctx, "test-key", 100)

		errors := logger.GetErrors()
		assert.Greater(t, len(errors), 0)

		// Should have limit exceeded error
		found := false
		for _, e := range errors {
			if e == "AEAD safety limit exceeded" {
				found = true
				break
			}
		}
		assert.True(t, found)
	})
}

// TestGetKeyConfig tests the GetKeyConfig function.
func TestGetKeyConfig(t *testing.T) {
	t.Run("returns default config when no per-key config exists", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     1000,
				MaxBytes:          10000,
				WarningPercent:    80,
				AutoRotatePercent: 95,
			}),
			WithEnforceDefaults(true),
		)

		config := tracker.GetKeyConfig("nonexistent-key")
		if config.MaxOperations != 1000 {
			t.Errorf("expected MaxOperations 1000, got %d", config.MaxOperations)
		}
		if config.MaxBytes != 10000 {
			t.Errorf("expected MaxBytes 10000, got %d", config.MaxBytes)
		}
	})

	t.Run("returns per-key config when set", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithEnforceDefaults(true),
		)

		customConfig := AEADSafetyConfig{
			MaxOperations:     500,
			MaxBytes:          5000,
			WarningPercent:    70,
			AutoRotatePercent: 90,
		}
		tracker.SetKeyConfig("test-key", customConfig)

		config := tracker.GetKeyConfig("test-key")
		if config.MaxOperations != 500 {
			t.Errorf("expected MaxOperations 500, got %d", config.MaxOperations)
		}
		if config.MaxBytes != 5000 {
			t.Errorf("expected MaxBytes 5000, got %d", config.MaxBytes)
		}
	})

	t.Run("returns empty config when enforce defaults is false", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithEnforceDefaults(false),
		)

		config := tracker.GetKeyConfig("nonexistent-key")
		if config.MaxOperations != 0 {
			t.Errorf("expected MaxOperations 0, got %d", config.MaxOperations)
		}
		if config.MaxBytes != 0 {
			t.Errorf("expected MaxBytes 0, got %d", config.MaxBytes)
		}
	})
}

// TestGetDEKConfig tests the GetDEKConfig function.
func TestGetDEKConfig(t *testing.T) {
	t.Run("returns default config when no per-DEK config exists", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithServerConfig(AEADSafetyConfig{
				MaxOperations:     2000,
				MaxBytes:          20000,
				WarningPercent:    75,
				AutoRotatePercent: 90,
			}),
			WithEnforceDefaults(true),
		)

		config := tracker.GetDEKConfig("/some/dek/path")
		if config.MaxOperations != 2000 {
			t.Errorf("expected MaxOperations 2000, got %d", config.MaxOperations)
		}
		if config.MaxBytes != 20000 {
			t.Errorf("expected MaxBytes 20000, got %d", config.MaxBytes)
		}
	})

	t.Run("returns per-DEK config when set", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithEnforceDefaults(true),
		)

		customConfig := AEADSafetyConfig{
			MaxOperations:     1500,
			MaxBytes:          15000,
			WarningPercent:    85,
			AutoRotatePercent: 92,
		}
		tracker.SetDEKConfig("/custom/dek/path", customConfig)

		config := tracker.GetDEKConfig("/custom/dek/path")
		if config.MaxOperations != 1500 {
			t.Errorf("expected MaxOperations 1500, got %d", config.MaxOperations)
		}
		if config.MaxBytes != 15000 {
			t.Errorf("expected MaxBytes 15000, got %d", config.MaxBytes)
		}
	})

	t.Run("returns empty config when enforce defaults is false", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithEnforceDefaults(false),
		)

		config := tracker.GetDEKConfig("/nonexistent/dek")
		if config.MaxOperations != 0 {
			t.Errorf("expected MaxOperations 0, got %d", config.MaxOperations)
		}
		if config.MaxBytes != 0 {
			t.Errorf("expected MaxBytes 0, got %d", config.MaxBytes)
		}
	})

	t.Run("different DEKs can have different configs", func(t *testing.T) {
		tracker := NewAEADSafetyTracker(
			WithEnforceDefaults(true),
		)

		tracker.SetDEKConfig("/dek/one", AEADSafetyConfig{MaxOperations: 100})
		tracker.SetDEKConfig("/dek/two", AEADSafetyConfig{MaxOperations: 200})

		config1 := tracker.GetDEKConfig("/dek/one")
		config2 := tracker.GetDEKConfig("/dek/two")

		if config1.MaxOperations != 100 {
			t.Errorf("expected DEK one MaxOperations 100, got %d", config1.MaxOperations)
		}
		if config2.MaxOperations != 200 {
			t.Errorf("expected DEK two MaxOperations 200, got %d", config2.MaxOperations)
		}
	})
}

func TestAEADSafetyTracker_ClearDEKConfig_WithoutEnforceDefaults(t *testing.T) {
	tracker := NewAEADSafetyTracker(
		WithServerConfig(AEADSafetyConfig{
			MaxOperations:  1000,
			MaxBytes:       1000000,
			WarningPercent: 80,
		}),
		// enforceDefaults is false by default
	)

	tracker.SetDEKConfig("transit/test-dek", AEADSafetyConfig{
		MaxOperations:  500,
		MaxBytes:       500000,
		WarningPercent: 70,
	})

	// Record usage to create the usage entry
	_ = tracker.RecordDEKEncrypt(context.Background(), "transit/test-dek", 100)

	// Clear the DEK config -- without enforceDefaults, sets usage.Config to zero value
	tracker.ClearDEKConfig("transit/test-dek")

	config := tracker.GetDEKConfig("transit/test-dek")
	assert.NotNil(t, config)
}

func TestAEADSafetyTracker_ClearDEKConfig_EnforceDefaultsFalse(t *testing.T) {
	tracker := NewAEADSafetyTracker(
		WithServerConfig(AEADSafetyConfig{
			MaxOperations:  1000,
			MaxBytes:       1000000,
			WarningPercent: 80,
		}),
		WithEnforceDefaults(false), // Explicitly disable enforce defaults
	)

	tracker.SetDEKConfig("transit/test-dek", AEADSafetyConfig{
		MaxOperations:  500,
		MaxBytes:       500000,
		WarningPercent: 70,
	})

	// Record usage to create the usage entry
	_ = tracker.RecordDEKEncrypt(context.Background(), "transit/test-dek", 100)

	// Clear the DEK config: without enforceDefaults, sets usage.Config to zero value
	tracker.ClearDEKConfig("transit/test-dek")

	// Verify the config was cleared (reverted to zero config, not server defaults)
	config := tracker.GetDEKConfig("transit/test-dek")
	assert.NotNil(t, config)
	// With enforceDefaults=false, the config should be the zero value
	assert.Equal(t, uint64(0), config.MaxOperations)
	assert.Equal(t, uint64(0), config.MaxBytes)
}
