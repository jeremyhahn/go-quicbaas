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

package cache

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/stretchr/testify/require"
)

func newTestL2Logger(t *testing.T) interfaces.Logger {
	cfg := logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	}
	log, err := logger.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	return newLoggerAdapter(log)
}

// TestL2CacheIntegration tests the full L2 cache integration with write-through.
func TestL2CacheIntegration(t *testing.T) {
	log := newTestL2Logger(t)

	t.Run("L1 hit", func(t *testing.T) {
		l2Cache := NewMockL2Cache()
		config := Config{
			L1Size:          10,
			L1TTL:           1 * time.Minute,
			CleanupInterval: 1 * time.Minute,
			L2Cache:         l2Cache,
			L2TTL:           5 * time.Minute,
			WritePolicy:     WriteThroughSync,
		}

		cm, err := NewManager(config, log)
		if err != nil {
			t.Fatalf("Failed to create cache manager: %v", err)
		}
		defer cm.Close()

		ctx := context.Background()
		key := "test-key"
		value := []byte("test-value")

		// Set value
		err = cm.Set(ctx, key, value)
		if err != nil {
			t.Fatalf("Failed to set value: %v", err)
		}

		// Get should hit L1
		retrieved, err := cm.Get(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get value: %v", err)
		}

		if string(retrieved) != string(value) {
			t.Errorf("Expected %s, got %s", value, retrieved)
		}

		// Verify metrics
		stats := cm.Stats()
		if stats.L1Hits != 1 {
			t.Errorf("Expected 1 L1 hit, got %d", stats.L1Hits)
		}
		if stats.L2Hits != 0 {
			t.Errorf("Expected 0 L2 hits, got %d", stats.L2Hits)
		}
	})

	t.Run("WriteThroughSync policy", func(t *testing.T) {
		l2Cache := NewMockL2Cache()
		config := Config{
			L1Size:          10,
			L1TTL:           1 * time.Minute,
			CleanupInterval: 1 * time.Minute,
			L2Cache:         l2Cache,
			L2TTL:           5 * time.Minute,
			WritePolicy:     WriteThroughSync,
		}

		cm, err := NewManager(config, log)
		if err != nil {
			t.Fatalf("Failed to create cache manager: %v", err)
		}
		defer cm.Close()

		ctx := context.Background()
		key := "sync-key"
		value := []byte("sync-value")

		// Set value with sync policy
		err = cm.Set(ctx, key, value)
		if err != nil {
			t.Fatalf("Failed to set value: %v", err)
		}

		// Verify value is in L2 immediately (sync write)
		l2Value, err := l2Cache.Get(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get L2 value: %v", err)
		}

		if string(l2Value) != string(value) {
			t.Errorf("Expected %s in L2, got %s", value, l2Value)
		}

		stats := cm.Stats()
		if stats.L2Sets != 1 {
			t.Errorf("Expected 1 L2 set, got %d", stats.L2Sets)
		}
	})

	t.Run("WriteThroughAsync policy", func(t *testing.T) {
		l2Cache := NewMockL2Cache()
		config := Config{
			L1Size:          10,
			L1TTL:           1 * time.Minute,
			CleanupInterval: 1 * time.Minute,
			L2Cache:         l2Cache,
			L2TTL:           5 * time.Minute,
			WritePolicy:     WriteThroughAsync,
		}

		cm, err := NewManager(config, log)
		if err != nil {
			t.Fatalf("Failed to create cache manager: %v", err)
		}
		defer cm.Close()

		ctx := context.Background()
		key := "async-key"
		value := []byte("async-value")

		// Set value with async policy
		err = cm.Set(ctx, key, value)
		if err != nil {
			t.Fatalf("Failed to set value: %v", err)
		}

		// The L2 write runs in its own goroutine; wait for its metric.
		require.Eventually(t, func() bool {
			return cm.GetMetrics().Get("l2_sets") == 1
		}, 5*time.Second, time.Millisecond)

		// Verify value is in L2 (async write should have completed)
		l2Value, err := l2Cache.Get(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get L2 value: %v", err)
		}

		if string(l2Value) != string(value) {
			t.Errorf("Expected %s in L2, got %s", value, l2Value)
		}
	})

	t.Run("Delete from both L1 and L2", func(t *testing.T) {
		l2Cache := NewMockL2Cache()
		config := Config{
			L1Size:          10,
			L1TTL:           1 * time.Minute,
			CleanupInterval: 1 * time.Minute,
			L2Cache:         l2Cache,
			L2TTL:           5 * time.Minute,
			WritePolicy:     WriteThroughSync,
		}

		cm, err := NewManager(config, log)
		if err != nil {
			t.Fatalf("Failed to create cache manager: %v", err)
		}
		defer cm.Close()

		ctx := context.Background()
		key := "delete-key"
		value := []byte("delete-value")

		// Set value
		err = cm.Set(ctx, key, value)
		if err != nil {
			t.Fatalf("Failed to set value: %v", err)
		}

		// Verify in both caches
		_, err = cm.Get(ctx, key)
		if err != nil {
			t.Fatalf("Value should exist: %v", err)
		}

		// Delete
		err = cm.Delete(ctx, key)
		if err != nil {
			t.Fatalf("Failed to delete value: %v", err)
		}

		// Verify deleted from L1
		_, err = cm.Get(ctx, key)
		if err == nil {
			t.Error("Value should be deleted from L1")
		}

		// Verify deleted from L2
		_, err = l2Cache.Get(ctx, key)
		if err == nil {
			t.Error("Value should be deleted from L2")
		}
	})

	t.Run("Stats with L2 metrics", func(t *testing.T) {
		l2Cache := NewMockL2Cache()
		config := Config{
			L1Size:          10,
			L1TTL:           1 * time.Minute,
			CleanupInterval: 1 * time.Minute,
			L2Cache:         l2Cache,
			L2TTL:           5 * time.Minute,
			WritePolicy:     WriteThroughSync,
		}

		cm, err := NewManager(config, log)
		if err != nil {
			t.Fatalf("Failed to create cache manager: %v", err)
		}
		defer cm.Close()

		ctx := context.Background()

		// Perform various operations
		cm.Set(ctx, "key1", []byte("value1"))
		cm.Set(ctx, "key2", []byte("value2"))
		cm.Get(ctx, "key1")        // L1 hit
		cm.Get(ctx, "nonexistent") // L1 miss, L2 miss

		// Set directly in L2 for L2 hit test
		l2Cache.Set(ctx, "key3", []byte("value3"), 5*time.Minute)
		cm.Get(ctx, "key3") // L1 miss, L2 hit

		stats := cm.Stats()

		// Verify L2 is enabled
		if !stats.L2Enabled {
			t.Error("L2 should be enabled")
		}

		// Verify hit rates are calculated
		if stats.L1HitRate == 0 {
			t.Error("L1 hit rate should be calculated")
		}

		if stats.L2HitRate == 0 {
			t.Error("L2 hit rate should be calculated")
		}

		// Verify error count
		if stats.L2Errors != 0 {
			t.Errorf("Expected 0 L2 errors, got %d", stats.L2Errors)
		}
	})
}

// TestWritePolicyValidation tests write policy validation.
func TestWritePolicyValidation(t *testing.T) {
	l2Cache := NewMockL2Cache()

	tests := []struct {
		name        string
		policy      WritePolicy
		shouldError bool
	}{
		{"WriteThroughSync", WriteThroughSync, false},
		{"WriteThroughAsync", WriteThroughAsync, false},
		{"WriteBack (now implemented)", WriteBack, false},
		{"Invalid policy", WritePolicy(99), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				L1Size:          10,
				L1TTL:           1 * time.Minute,
				CleanupInterval: 1 * time.Minute,
				L2Cache:         l2Cache,
				L2TTL:           5 * time.Minute,
				WritePolicy:     tt.policy,
			}

			err := config.Validate()
			if tt.shouldError && err == nil {
				t.Error("Expected validation error")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Unexpected validation error: %v", err)
			}
		})
	}
}

// TestL2CacheOptional tests that L2 cache is optional.
func TestL2CacheOptional(t *testing.T) {
	log := newTestL2Logger(t)

	config := Config{
		L1Size:          10,
		L1TTL:           1 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		L2Cache:         nil, // No L2 cache
		WritePolicy:     WriteThroughSync,
	}

	cm, err := NewManager(config, log)
	if err != nil {
		t.Fatalf("Failed to create cache manager without L2: %v", err)
	}
	defer cm.Close()

	ctx := context.Background()
	key := "test-key"
	value := []byte("test-value")

	// Operations should work without L2
	err = cm.Set(ctx, key, value)
	if err != nil {
		t.Fatalf("Failed to set value: %v", err)
	}

	retrieved, err := cm.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to get value: %v", err)
	}

	if string(retrieved) != string(value) {
		t.Errorf("Expected %s, got %s", value, retrieved)
	}

	stats := cm.Stats()
	if stats.L2Enabled {
		t.Error("L2 should not be enabled")
	}
}
