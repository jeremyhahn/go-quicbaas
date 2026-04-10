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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func testLogger(t *testing.T) interfaces.Logger {
	cfg := logger.LogConfig{
		Level:  "error",
		Format: "console",
		Output: "stdout",
	}

	log, err := logger.New(cfg)
	require.NoError(t, err)

	// Return a simple logger wrapper that implements interfaces.Logger
	return &simpleLogger{log}
}

// simpleLogger wraps logger.Logger to implement interfaces.Logger.
type simpleLogger struct {
	*logger.Logger
}

func (l *simpleLogger) WithField(key string, value interface{}) interfaces.Logger {
	return l
}

func (l *simpleLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return l
}

func (l *simpleLogger) WithError(err error) interfaces.Logger {
	return l
}

func TestCacheService(t *testing.T) {
	// Create a mock service with cache manager
	log := testLogger(t)

	cacheConfig := cache.Config{
		L1Size:          100,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}

	cacheMgr, err := cache.NewManager(cacheConfig, log)
	if err != nil {
		t.Fatalf("Failed to create cache manager: %v", err)
	}
	defer cacheMgr.Close()

	svc := &quicBaaS{
		cacheManager: cacheMgr,
		logger:       log,
	}

	cacheSvc := &cacheService{svc: svc}
	ctx := context.Background()

	// Test GetCacheStats
	t.Run("GetCacheStats", func(t *testing.T) {
		resp, err := cacheSvc.GetCacheStats(ctx, &GetCacheStatsRequest{})
		if err != nil {
			t.Fatalf("GetCacheStats failed: %v", err)
		}
		if resp.MaxSize != 100 {
			t.Errorf("Expected max size 100, got %d", resp.MaxSize)
		}
	})

	// Test GetCacheConfig
	t.Run("GetCacheConfig", func(t *testing.T) {
		resp, err := cacheSvc.GetCacheConfig(ctx, &GetCacheConfigRequest{})
		if err != nil {
			t.Fatalf("GetCacheConfig failed: %v", err)
		}
		if resp.L1Size != 100 {
			t.Errorf("Expected L1Size 100, got %d", resp.L1Size)
		}
		if resp.L1TTL != 5*time.Minute {
			t.Errorf("Expected L1TTL 5m, got %v", resp.L1TTL)
		}
		if !resp.Enabled {
			t.Error("Expected cache to be enabled")
		}
	})

	// Test UpdateCacheConfig
	t.Run("UpdateCacheConfig", func(t *testing.T) {
		newSize := 200
		newTTL := 10 * time.Minute

		req := &UpdateCacheConfigRequest{
			L1Size: &newSize,
			L1TTL:  &newTTL,
		}

		resp, err := cacheSvc.UpdateCacheConfig(ctx, req)
		if err != nil {
			t.Fatalf("UpdateCacheConfig failed: %v", err)
		}

		if !resp.Updated {
			t.Error("Expected Updated to be true")
		}
		if resp.NewConfig.L1Size != 200 {
			t.Errorf("Expected new L1Size 200, got %d", resp.NewConfig.L1Size)
		}
		if resp.NewConfig.L1TTL != 10*time.Minute {
			t.Errorf("Expected new L1TTL 10m, got %v", resp.NewConfig.L1TTL)
		}
	})

	// Test ClearCache
	t.Run("ClearCache", func(t *testing.T) {
		// Add some data to cache
		cacheMgr.Set(ctx, "key1", []byte("value1"))
		cacheMgr.Set(ctx, "key2", []byte("value2"))

		resp, err := cacheSvc.ClearCache(ctx, &ClearCacheRequest{All: true})
		if err != nil {
			t.Fatalf("ClearCache failed: %v", err)
		}

		if resp.EntriesCleared != 2 {
			t.Errorf("Expected 2 entries cleared, got %d", resp.EntriesCleared)
		}

		// Verify cache is empty
		stats := cacheMgr.Stats()
		if stats.L1Size != 0 {
			t.Errorf("Expected cache to be empty, got size %d", stats.L1Size)
		}
	})
}

func TestCacheService_NoCacheManager(t *testing.T) {
	// Test behavior when cache manager is nil
	svc := &quicBaaS{
		cacheManager: nil,
	}

	cacheSvc := &cacheService{svc: svc}
	ctx := context.Background()

	t.Run("GetCacheStats_NilManager", func(t *testing.T) {
		_, err := cacheSvc.GetCacheStats(ctx, &GetCacheStatsRequest{})
		if err == nil {
			t.Error("Expected error when cache manager is nil")
		}
	})

	t.Run("ClearCache_NilManager", func(t *testing.T) {
		_, err := cacheSvc.ClearCache(ctx, &ClearCacheRequest{})
		if err == nil {
			t.Error("Expected error when cache manager is nil")
		}
	})

	t.Run("GetCacheConfig_NilManager", func(t *testing.T) {
		_, err := cacheSvc.GetCacheConfig(ctx, &GetCacheConfigRequest{})
		if err == nil {
			t.Error("Expected error when cache manager is nil")
		}
	})

	t.Run("UpdateCacheConfig_NilManager", func(t *testing.T) {
		newSize := 100
		req := &UpdateCacheConfigRequest{L1Size: &newSize}
		_, err := cacheSvc.UpdateCacheConfig(ctx, req)
		if err == nil {
			t.Error("Expected error when cache manager is nil")
		}
	})
}
