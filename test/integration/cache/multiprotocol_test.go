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

//go:build integration && quic

package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Cache Result Types
// =============================================================================

// CacheStatsResult contains cache statistics returned from any protocol.
type CacheStatsResult struct {
	Enabled     bool
	HitRate     float64
	Size        uint64
	MaxSize     uint64
	Evictions   uint64
	Hits        uint64
	Misses      uint64
	L1Size      int32
	L1Capacity  int32
	L1Hits      int64
	L1Misses    int64
	L1HitRate   float64
	L1Evictions int64
}

// CacheConfigResult contains cache configuration returned from any protocol.
type CacheConfigResult struct {
	L1Size          int
	L1TTL           time.Duration
	CleanupInterval time.Duration
	Enabled         bool
}

// CacheConfigUpdate contains cache configuration update parameters.
type CacheConfigUpdate struct {
	L1Size          *int
	L1TTL           *time.Duration
	CleanupInterval *time.Duration
}

// ClearCacheResult contains the result of clearing cache.
type ClearCacheResult struct {
	Success        bool
	EntriesCleared uint64
}

// =============================================================================
// Protocol Interface
// =============================================================================

// cacheProtocol abstracts the different protocols for cache testing.
type cacheProtocol interface {
	Setup(t *testing.T, ctx context.Context) error
	Teardown(t *testing.T) error
	GetStats(ctx context.Context) (*CacheStatsResult, error)
	ClearCache(ctx context.Context, cacheType string) error
	GetConfig(ctx context.Context) (*CacheConfigResult, error)
	UpdateConfig(ctx context.Context, config *CacheConfigUpdate) error
	Name() string
}

// =============================================================================
// Main Test Functions
// =============================================================================

// TestMultiprotocol_GetCacheStats tests cache stats retrieval across all protocols.
func TestMultiprotocol_GetCacheStats(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := getAllProtocols()

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol must be available", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			// Test: Get cache stats successfully
			t.Run("Success", func(t *testing.T) {
				stats, err := protocol.GetStats(ctx)
				require.NoError(t, err, "GetStats should succeed for %s", protocol.Name())
				require.NotNil(t, stats, "Stats should not be nil")

				// Verify stats have reasonable values
				assert.GreaterOrEqual(t, stats.L1Size, int32(0), "L1Size should be non-negative")
				assert.GreaterOrEqual(t, stats.L1Capacity, int32(0), "L1Capacity should be non-negative")
				assert.GreaterOrEqual(t, stats.L1HitRate, float64(0), "L1HitRate should be non-negative")
				assert.LessOrEqual(t, stats.L1HitRate, float64(1), "L1HitRate should be <= 1")

				t.Logf("[%s] Cache Stats: enabled=%v, l1_size=%d, l1_capacity=%d, hit_rate=%.2f",
					protocol.Name(), stats.Enabled, stats.L1Size, stats.L1Capacity, stats.L1HitRate)
			})

			// Test: Stats remain consistent across multiple calls
			t.Run("Consistency", func(t *testing.T) {
				stats1, err := protocol.GetStats(ctx)
				require.NoError(t, err)

				stats2, err := protocol.GetStats(ctx)
				require.NoError(t, err)

				// Enabled flag should be consistent
				assert.Equal(t, stats1.Enabled, stats2.Enabled, "Enabled should be consistent")

				t.Logf("[%s] Consistency check passed", protocol.Name())
			})
		})
	}
}

// TestMultiprotocol_ClearCache tests cache clearing across all protocols.
func TestMultiprotocol_ClearCache(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := getAllProtocols()

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol must be available", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			// Test: Clear all cache
			t.Run("ClearAll", func(t *testing.T) {
				err := protocol.ClearCache(ctx, "all")
				require.NoError(t, err, "ClearCache should succeed for %s", protocol.Name())

				// Verify cache was cleared by checking stats
				stats, err := protocol.GetStats(ctx)
				require.NoError(t, err)

				// After clearing, L1Size should be 0 or very small
				assert.LessOrEqual(t, stats.L1Size, int32(10),
					"Cache should be mostly empty after clear")

				t.Logf("[%s] Cache cleared successfully, L1Size=%d", protocol.Name(), stats.L1Size)
			})

			// Test: Clear specific cache type
			t.Run("ClearL1", func(t *testing.T) {
				err := protocol.ClearCache(ctx, "l1")
				require.NoError(t, err, "ClearCache L1 should succeed for %s", protocol.Name())

				t.Logf("[%s] L1 cache cleared successfully", protocol.Name())
			})

			// Test: Clear with empty type (should clear all)
			t.Run("ClearDefault", func(t *testing.T) {
				err := protocol.ClearCache(ctx, "")
				require.NoError(t, err, "ClearCache with empty type should succeed")

				t.Logf("[%s] Default cache clear succeeded", protocol.Name())
			})
		})
	}
}

// TestMultiprotocol_GetConfig tests cache config retrieval across all protocols.
func TestMultiprotocol_GetConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := getAllProtocols()

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol must be available", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			// Test: Get config successfully
			t.Run("Success", func(t *testing.T) {
				config, err := protocol.GetConfig(ctx)
				require.NoError(t, err, "GetConfig should succeed for %s", protocol.Name())
				require.NotNil(t, config, "Config should not be nil")

				// Verify config values are reasonable
				assert.Greater(t, config.L1Size, 0, "L1Size should be positive")
				assert.Greater(t, config.L1TTL, time.Duration(0), "L1TTL should be positive")

				t.Logf("[%s] Cache Config: l1_size=%d, l1_ttl=%v, enabled=%v",
					protocol.Name(), config.L1Size, config.L1TTL, config.Enabled)
			})

			// Test: Config consistency
			t.Run("Consistency", func(t *testing.T) {
				config1, err := protocol.GetConfig(ctx)
				require.NoError(t, err)

				config2, err := protocol.GetConfig(ctx)
				require.NoError(t, err)

				// Configuration should be consistent
				assert.Equal(t, config1.L1Size, config2.L1Size, "L1Size should be consistent")
				assert.Equal(t, config1.Enabled, config2.Enabled, "Enabled should be consistent")

				t.Logf("[%s] Config consistency check passed", protocol.Name())
			})
		})
	}
}

// TestMultiprotocol_UpdateConfig tests cache config updates across all protocols.
func TestMultiprotocol_UpdateConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	protocols := getAllProtocols()

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol must be available", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			// Get original config for restoration
			originalConfig, err := protocol.GetConfig(ctx)
			require.NoError(t, err, "Should get original config")

			// Test: Update L1Size
			t.Run("UpdateL1Size", func(t *testing.T) {
				newSize := originalConfig.L1Size + 100
				update := &CacheConfigUpdate{
					L1Size: &newSize,
				}

				err := protocol.UpdateConfig(ctx, update)
				require.NoError(t, err, "UpdateConfig should succeed for %s", protocol.Name())

				// Verify the update
				updatedConfig, err := protocol.GetConfig(ctx)
				require.NoError(t, err)
				assert.Equal(t, newSize, updatedConfig.L1Size, "L1Size should be updated")

				t.Logf("[%s] Updated L1Size from %d to %d",
					protocol.Name(), originalConfig.L1Size, newSize)
			})

			// Test: Update L1TTL
			t.Run("UpdateL1TTL", func(t *testing.T) {
				newTTL := 10 * time.Minute
				update := &CacheConfigUpdate{
					L1TTL: &newTTL,
				}

				err := protocol.UpdateConfig(ctx, update)
				require.NoError(t, err, "UpdateConfig should succeed for %s", protocol.Name())

				// Verify the update
				updatedConfig, err := protocol.GetConfig(ctx)
				require.NoError(t, err)
				assert.Equal(t, newTTL, updatedConfig.L1TTL, "L1TTL should be updated")

				t.Logf("[%s] Updated L1TTL to %v", protocol.Name(), newTTL)
			})

			// Test: Update multiple fields
			t.Run("UpdateMultipleFields", func(t *testing.T) {
				newSize := originalConfig.L1Size + 50
				newTTL := 15 * time.Minute
				update := &CacheConfigUpdate{
					L1Size: &newSize,
					L1TTL:  &newTTL,
				}

				err := protocol.UpdateConfig(ctx, update)
				require.NoError(t, err, "UpdateConfig should succeed for %s", protocol.Name())

				// Verify the updates
				updatedConfig, err := protocol.GetConfig(ctx)
				require.NoError(t, err)
				assert.Equal(t, newSize, updatedConfig.L1Size, "L1Size should be updated")
				assert.Equal(t, newTTL, updatedConfig.L1TTL, "L1TTL should be updated")

				t.Logf("[%s] Updated multiple fields successfully", protocol.Name())
			})

			// Restore original config
			restoreConfig := &CacheConfigUpdate{
				L1Size: &originalConfig.L1Size,
				L1TTL:  &originalConfig.L1TTL,
			}
			err = protocol.UpdateConfig(ctx, restoreConfig)
			if err != nil {
				t.Logf("[%s] Warning: failed to restore original config: %v", protocol.Name(), err)
			}
		})
	}
}

// TestCacheMultiProtocol_Comprehensive runs comprehensive tests across all protocols.
func TestCacheMultiProtocol_Comprehensive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	protocols := getAllProtocols()

	for _, protocol := range protocols {
		t.Run(protocol.Name(), func(t *testing.T) {
			err := protocol.Setup(t, ctx)
			require.NoError(t, err, "%s protocol must be available", protocol.Name())
			defer func() {
				if teardownErr := protocol.Teardown(t); teardownErr != nil {
					t.Logf("Warning: teardown error for %s: %v", protocol.Name(), teardownErr)
				}
			}()

			// Run stats tests
			t.Run("Stats", func(t *testing.T) {
				testCacheStatsComprehensive(t, ctx, protocol)
			})

			// Run clear tests
			t.Run("Clear", func(t *testing.T) {
				testCacheClearComprehensive(t, ctx, protocol)
			})

			// Run config tests
			t.Run("Config", func(t *testing.T) {
				testCacheConfigComprehensive(t, ctx, protocol)
			})
		})
	}
}

// =============================================================================
// Comprehensive Test Functions
// =============================================================================

func testCacheStatsComprehensive(t *testing.T, ctx context.Context, protocol cacheProtocol) {
	t.Helper()

	// Test: Initial stats retrieval
	t.Run("InitialStats", func(t *testing.T) {
		stats, err := protocol.GetStats(ctx)
		require.NoError(t, err)
		require.NotNil(t, stats)

		t.Logf("[%s] Initial stats: enabled=%v, l1_size=%d, hits=%d, misses=%d",
			protocol.Name(), stats.Enabled, stats.L1Size, stats.L1Hits, stats.L1Misses)
	})

	// Test: Stats after operations
	t.Run("StatsAfterOperations", func(t *testing.T) {
		statsBefore, err := protocol.GetStats(ctx)
		require.NoError(t, err)

		// Perform some operations that might affect cache
		for i := 0; i < 5; i++ {
			_, _ = protocol.GetStats(ctx)
		}

		statsAfter, err := protocol.GetStats(ctx)
		require.NoError(t, err)

		// Verify stats are updated (at least not decreasing for hits)
		assert.GreaterOrEqual(t, statsAfter.L1Hits, statsBefore.L1Hits,
			"Hits should not decrease")

		t.Logf("[%s] Stats before=%d hits, after=%d hits",
			protocol.Name(), statsBefore.L1Hits, statsAfter.L1Hits)
	})
}

func testCacheClearComprehensive(t *testing.T, ctx context.Context, protocol cacheProtocol) {
	t.Helper()

	// Test: Clear and verify
	t.Run("ClearAndVerify", func(t *testing.T) {
		// Get stats before clear
		statsBefore, err := protocol.GetStats(ctx)
		require.NoError(t, err)

		// Clear cache
		err = protocol.ClearCache(ctx, "all")
		require.NoError(t, err)

		// Get stats after clear
		statsAfter, err := protocol.GetStats(ctx)
		require.NoError(t, err)

		// Cache size should be reduced
		assert.LessOrEqual(t, statsAfter.L1Size, statsBefore.L1Size,
			"Cache size should not increase after clear")

		t.Logf("[%s] Cache clear: before=%d, after=%d",
			protocol.Name(), statsBefore.L1Size, statsAfter.L1Size)
	})

	// Test: Multiple clears
	t.Run("MultipleClears", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			err := protocol.ClearCache(ctx, "all")
			require.NoError(t, err, "Clear %d should succeed", i+1)
		}

		t.Logf("[%s] Multiple clears succeeded", protocol.Name())
	})
}

func testCacheConfigComprehensive(t *testing.T, ctx context.Context, protocol cacheProtocol) {
	t.Helper()

	// Get original config for restoration
	originalConfig, err := protocol.GetConfig(ctx)
	require.NoError(t, err)
	defer func() {
		// Restore original config
		restoreConfig := &CacheConfigUpdate{
			L1Size: &originalConfig.L1Size,
			L1TTL:  &originalConfig.L1TTL,
		}
		_ = protocol.UpdateConfig(ctx, restoreConfig)
	}()

	// Test: Get and verify config
	t.Run("GetConfig", func(t *testing.T) {
		config, err := protocol.GetConfig(ctx)
		require.NoError(t, err)
		require.NotNil(t, config)

		assert.Greater(t, config.L1Size, 0, "L1Size should be positive")

		t.Logf("[%s] Config: l1_size=%d, l1_ttl=%v, cleanup=%v, enabled=%v",
			protocol.Name(), config.L1Size, config.L1TTL,
			config.CleanupInterval, config.Enabled)
	})

	// Test: Update with valid values
	t.Run("UpdateValidValues", func(t *testing.T) {
		newSize := 500
		newTTL := 5 * time.Minute
		update := &CacheConfigUpdate{
			L1Size: &newSize,
			L1TTL:  &newTTL,
		}

		err := protocol.UpdateConfig(ctx, update)
		require.NoError(t, err)

		config, err := protocol.GetConfig(ctx)
		require.NoError(t, err)
		assert.Equal(t, newSize, config.L1Size)
		assert.Equal(t, newTTL, config.L1TTL)

		t.Logf("[%s] Config update succeeded", protocol.Name())
	})

	// Test: Update with only L1Size
	t.Run("UpdateOnlyL1Size", func(t *testing.T) {
		newSize := 600
		update := &CacheConfigUpdate{
			L1Size: &newSize,
		}

		err := protocol.UpdateConfig(ctx, update)
		require.NoError(t, err)

		config, err := protocol.GetConfig(ctx)
		require.NoError(t, err)
		assert.Equal(t, newSize, config.L1Size)

		t.Logf("[%s] L1Size only update succeeded", protocol.Name())
	})

	// Test: Update with only L1TTL
	t.Run("UpdateOnlyL1TTL", func(t *testing.T) {
		newTTL := 7 * time.Minute
		update := &CacheConfigUpdate{
			L1TTL: &newTTL,
		}

		err := protocol.UpdateConfig(ctx, update)
		require.NoError(t, err)

		config, err := protocol.GetConfig(ctx)
		require.NoError(t, err)
		assert.Equal(t, newTTL, config.L1TTL)

		t.Logf("[%s] L1TTL only update succeeded", protocol.Name())
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

// getAllProtocols returns all available protocol implementations.
func getAllProtocols() []cacheProtocol {
	protocols := []cacheProtocol{
		&restProtocolClient{},
		&grpcProtocolClient{},
		&unixSocketProtocolClient{},
	}
	if helpers.IsEmbeddedAvailable() {
		protocols = append(protocols, &embeddedProtocolClient{})
	}
	return protocols
}

// getRESTEndpoint returns the REST API endpoint for testing.
func getRESTEndpoint() string {
	endpoints := helpers.GetRESTEndpoints()
	if len(endpoints) > 0 {
		return endpoints[0]
	}
	return "https://test-node1:8080"
}

// isGRPCUnimplementedError checks if the error indicates an unimplemented feature.
func isGRPCUnimplementedError(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if ok && st.Code() == codes.Unimplemented {
		return true
	}
	errStr := err.Error()
	return strings.Contains(errStr, "not implemented") ||
		strings.Contains(errStr, "Unimplemented")
}

// =============================================================================
// REST Protocol Client Implementation
// =============================================================================

type restProtocolClient struct {
	endpoint string
	client   *http.Client
}

func (c *restProtocolClient) Name() string { return "REST" }

func (c *restProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	c.endpoint = getRESTEndpoint()
	// The cache routes require admin:status (stats, config read) and
	// admin:capabilities (clear, config update). The tenant principal's
	// Admin role holds both; the Operator role holds only admin:status.
	c.client = helpers.NewTenantHTTPClient(t, 30*time.Second)

	// Verify endpoint is reachable
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.endpoint+"/api/v1/health", nil)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create health check request", Cause: err}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "REST endpoint not reachable at " + c.endpoint, Cause: err}
	}
	resp.Body.Close()

	// Probe to verify cache stats feature is available
	statsReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.endpoint+"/api/v1/cache/stats", nil)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create cache stats probe request", Cause: err}
	}

	statsResp, err := c.client.Do(statsReq)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "cache stats probe request failed", Cause: err}
	}
	defer statsResp.Body.Close()

	if statsResp.StatusCode == http.StatusNotFound {
		return &qberrors.MessageError{Message: "cache stats feature not available: endpoint returned 404"}
	}
	if statsResp.StatusCode == http.StatusNotImplemented {
		return &qberrors.MessageError{Message: "cache stats feature not available: endpoint returned 501 not implemented"}
	}

	return nil
}

func (c *restProtocolClient) Teardown(t *testing.T) error {
	c.client = nil
	return nil
}

func (c *restProtocolClient) GetStats(ctx context.Context) (*CacheStatsResult, error) {
	url := c.endpoint + "/api/v1/cache/stats"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(body)}
	}

	var result struct {
		Enabled     bool    `json:"enabled"`
		L1Size      int32   `json:"l1_size"`
		L1Capacity  int32   `json:"l1_capacity"`
		L1Hits      int64   `json:"l1_hits"`
		L1Misses    int64   `json:"l1_misses"`
		L1HitRate   float64 `json:"l1_hit_rate"`
		L1Evictions int64   `json:"l1_evictions"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &CacheStatsResult{
		Enabled:     result.Enabled,
		L1Size:      result.L1Size,
		L1Capacity:  result.L1Capacity,
		L1Hits:      result.L1Hits,
		L1Misses:    result.L1Misses,
		L1HitRate:   result.L1HitRate,
		L1Evictions: result.L1Evictions,
	}, nil
}

func (c *restProtocolClient) ClearCache(ctx context.Context, cacheType string) error {
	url := c.endpoint + "/api/v1/cache/clear"

	reqBody := map[string]interface{}{
		"type": cacheType,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(body)}
	}

	return nil
}

func (c *restProtocolClient) GetConfig(ctx context.Context) (*CacheConfigResult, error) {
	url := c.endpoint + "/api/v1/cache/config"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(body)}
	}

	var result struct {
		L1Size         int   `json:"l1_size"`
		L1TTLSeconds   int64 `json:"l1_ttl_seconds"`
		CleanupSeconds int64 `json:"cleanup_interval_seconds"`
		Enabled        bool  `json:"enabled"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &CacheConfigResult{
		L1Size:          result.L1Size,
		L1TTL:           time.Duration(result.L1TTLSeconds) * time.Second,
		CleanupInterval: time.Duration(result.CleanupSeconds) * time.Second,
		Enabled:         result.Enabled,
	}, nil
}

func (c *restProtocolClient) UpdateConfig(ctx context.Context, config *CacheConfigUpdate) error {
	url := c.endpoint + "/api/v1/cache/config"

	reqBody := make(map[string]interface{})
	if config.L1Size != nil {
		reqBody["l1_size"] = *config.L1Size
	}
	if config.L1TTL != nil {
		reqBody["l1_ttl_seconds"] = int64(config.L1TTL.Seconds())
	}
	if config.CleanupInterval != nil {
		reqBody["cleanup_interval_seconds"] = int64(config.CleanupInterval.Seconds())
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url,
		bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &qberrors.MessageError{Message: "status " + strconv.Itoa(resp.StatusCode) + ": " + string(body)}
	}

	return nil
}

// =============================================================================
// gRPC Protocol Client Implementation
// =============================================================================

type grpcProtocolClient struct {
	conn   *grpc.ClientConn
	client proto.QuicBaaSClient
}

func (c *grpcProtocolClient) Name() string { return "gRPC" }

func (c *grpcProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	endpoints := helpers.GetClusterEndpoints()
	if len(endpoints) == 0 {
		return &qberrors.MessageError{Message: "no gRPC endpoints available"}
	}

	endpoint := endpoints[0]

	// The cache RPCs require admin:status and admin:capabilities, which the
	// tenant principal's Admin role holds.
	conn, err := helpers.SetupGRPCConnectionToEndpoint(t, ctx, endpoint,
		grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t)))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to connect to gRPC endpoint " + endpoint, Cause: err}
	}
	c.conn = conn
	c.client = proto.NewQuicBaaSClient(conn)

	// Probe to verify cache stats feature is available
	_, err = c.client.GetCacheStats(ctx, &proto.GetCacheStatsRequest{})
	if err != nil {
		if isGRPCUnimplementedError(err) {
			c.conn.Close()
			return &qberrors.WrappedMessageError{Message: "cache stats feature not available", Cause: err}
		}
		// Other errors might be transient, still return them to skip
		c.conn.Close()
		return &qberrors.WrappedMessageError{Message: "cache stats probe failed", Cause: err}
	}

	return nil
}

func (c *grpcProtocolClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *grpcProtocolClient) GetStats(ctx context.Context) (*CacheStatsResult, error) {
	req := &proto.GetCacheStatsRequest{}
	resp, err := c.client.GetCacheStats(ctx, req)
	if err != nil {
		return nil, err
	}

	return &CacheStatsResult{
		Enabled:     resp.Enabled,
		L1Size:      resp.L1Size,
		L1Capacity:  resp.L1Capacity,
		L1Hits:      resp.L1Hits,
		L1Misses:    resp.L1Misses,
		L1HitRate:   resp.L1HitRate,
		L1Evictions: resp.L1Evictions,
	}, nil
}

func (c *grpcProtocolClient) ClearCache(ctx context.Context, cacheType string) error {
	req := &proto.ClearCacheRequest{}
	// Note: ClearCacheRequest has no fields in the current proto definition
	// The cacheType parameter is ignored for proto-based clearing
	_ = cacheType
	_, err := c.client.ClearCache(ctx, req)
	return err
}

func (c *grpcProtocolClient) GetConfig(ctx context.Context) (*CacheConfigResult, error) {
	req := &proto.GetCacheConfigRequest{}
	resp, err := c.client.GetCacheConfig(ctx, req)
	if err != nil {
		return nil, err
	}

	return &CacheConfigResult{
		L1Size:          int(resp.L1Size),
		L1TTL:           time.Duration(resp.L1TtlSeconds) * time.Second,
		CleanupInterval: time.Duration(resp.CleanupIntervalSeconds) * time.Second,
		Enabled:         resp.Enabled,
	}, nil
}

func (c *grpcProtocolClient) UpdateConfig(ctx context.Context, config *CacheConfigUpdate) error {
	req := &proto.UpdateCacheConfigRequest{}
	if config.L1Size != nil {
		req.L1Size = int32(*config.L1Size)
	}
	if config.L1TTL != nil {
		req.L1TtlSeconds = int64(config.L1TTL.Seconds())
	}
	// Note: CleanupInterval is not in the proto UpdateCacheConfigRequest

	_, err := c.client.UpdateCacheConfig(ctx, req)
	return err
}

// =============================================================================
// =============================================================================
// Unix Socket Protocol Client Implementation
// =============================================================================

type unixSocketProtocolClient struct {
	conn   *grpc.ClientConn
	client proto.QuicBaaSClient
}

func (c *unixSocketProtocolClient) Name() string { return "UnixSocket" }

func (c *unixSocketProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	socketPath := os.Getenv("QUICBAAS_NODE1_UNIX")
	if socketPath == "" {
		socketPath = "/var/run/quicbaas/node1.sock"
	}

	// Check if socket exists
	if _, err := net.Dial("unix", socketPath); err != nil {
		return &qberrors.WrappedMessageError{Message: "unix socket not available at " + socketPath, Cause: err}
	}

	tlsCfg, err := helpers.IntegrationTLSConfig()
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "load integration TLS config", Cause: err}
	}
	tlsCfg.ServerName = "localhost"

	// Create gRPC connection over unix socket
	conn, err := grpc.DialContext(ctx,
		"unix://"+socketPath,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		grpc.WithPerRPCCredentials(helpers.TenantGRPCCredentials(t)),
	)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to connect to unix socket", Cause: err}
	}
	c.conn = conn
	c.client = proto.NewQuicBaaSClient(conn)

	// Probe to verify cache stats feature is available
	_, err = c.client.GetCacheStats(ctx, &proto.GetCacheStatsRequest{})
	if err != nil {
		if isGRPCUnimplementedError(err) {
			c.conn.Close()
			return &qberrors.WrappedMessageError{Message: "cache stats feature not available", Cause: err}
		}
		// Other errors might be transient, still return them to skip
		c.conn.Close()
		return &qberrors.WrappedMessageError{Message: "cache stats probe failed", Cause: err}
	}

	return nil
}

func (c *unixSocketProtocolClient) Teardown(t *testing.T) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *unixSocketProtocolClient) GetStats(ctx context.Context) (*CacheStatsResult, error) {
	req := &proto.GetCacheStatsRequest{}
	resp, err := c.client.GetCacheStats(ctx, req)
	if err != nil {
		return nil, err
	}

	return &CacheStatsResult{
		Enabled:     resp.Enabled,
		L1Size:      resp.L1Size,
		L1Capacity:  resp.L1Capacity,
		L1Hits:      resp.L1Hits,
		L1Misses:    resp.L1Misses,
		L1HitRate:   resp.L1HitRate,
		L1Evictions: resp.L1Evictions,
	}, nil
}

func (c *unixSocketProtocolClient) ClearCache(ctx context.Context, cacheType string) error {
	req := &proto.ClearCacheRequest{}
	// Note: ClearCacheRequest has no fields in the current proto definition
	_ = cacheType
	_, err := c.client.ClearCache(ctx, req)
	return err
}

func (c *unixSocketProtocolClient) GetConfig(ctx context.Context) (*CacheConfigResult, error) {
	req := &proto.GetCacheConfigRequest{}
	resp, err := c.client.GetCacheConfig(ctx, req)
	if err != nil {
		return nil, err
	}

	return &CacheConfigResult{
		L1Size:          int(resp.L1Size),
		L1TTL:           time.Duration(resp.L1TtlSeconds) * time.Second,
		CleanupInterval: time.Duration(resp.CleanupIntervalSeconds) * time.Second,
		Enabled:         resp.Enabled,
	}, nil
}

func (c *unixSocketProtocolClient) UpdateConfig(ctx context.Context, config *CacheConfigUpdate) error {
	req := &proto.UpdateCacheConfigRequest{}
	if config.L1Size != nil {
		req.L1Size = int32(*config.L1Size)
	}
	if config.L1TTL != nil {
		req.L1TtlSeconds = int64(config.L1TTL.Seconds())
	}
	// Note: CleanupInterval is not in the proto UpdateCacheConfigRequest

	_, err := c.client.UpdateCacheConfig(ctx, req)
	return err
}

// =============================================================================
// Embedded Protocol Client Implementation
// =============================================================================

// embeddedProtocolClient provides direct in-process access using the SDK's
// embedded transport. It starts a local embedded QuicBaaS server and tests
// the embedded protocol path through the SDK.
type embeddedProtocolClient struct {
	client   quicbaas.Client
	embedded *helpers.EmbeddedServer
}

func (c *embeddedProtocolClient) Name() string { return "Embedded" }

func (c *embeddedProtocolClient) Setup(t *testing.T, ctx context.Context) error {
	// Start an embedded server for testing
	svc, err := helpers.GetEmbeddedService(t)
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to get embedded service", Cause: err}
	}

	// Create SDK client with embedded transport
	client, err := quicbaas.NewClient("", quicbaas.WithEmbedded(svc))
	if err != nil {
		return &qberrors.WrappedMessageError{Message: "failed to create embedded client", Cause: err}
	}

	c.client = client

	// Probe to verify cache stats feature is available
	_, err = c.client.Cache().GetCacheStats(ctx, &quicbaas.GetCacheStatsRequest{})
	if err != nil {
		c.client.Close()
		errStr := err.Error()
		if strings.Contains(errStr, "not implemented") ||
			strings.Contains(errStr, "Unimplemented") {
			return &qberrors.WrappedMessageError{Message: "cache stats feature not available", Cause: err}
		}
		return &qberrors.WrappedMessageError{Message: "cache stats probe failed", Cause: err}
	}

	return nil
}

func (c *embeddedProtocolClient) Teardown(t *testing.T) error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *embeddedProtocolClient) GetStats(ctx context.Context) (*CacheStatsResult, error) {
	if c.client == nil {
		return nil, &qberrors.MessageError{Message: "embedded client not initialized"}
	}

	resp, err := c.client.Cache().GetCacheStats(ctx, &quicbaas.GetCacheStatsRequest{})
	if err != nil {
		return nil, err
	}

	return &CacheStatsResult{
		HitRate:   resp.HitRate,
		Size:      resp.Size,
		MaxSize:   resp.MaxSize,
		Evictions: resp.Evictions,
		Hits:      resp.Hits,
		Misses:    resp.Misses,
	}, nil
}

func (c *embeddedProtocolClient) ClearCache(ctx context.Context, cacheType string) error {
	if c.client == nil {
		return &qberrors.MessageError{Message: "embedded client not initialized"}
	}

	_, err := c.client.Cache().ClearCache(ctx, &quicbaas.ClearCacheRequest{
		EntityType: cacheType,
		All:        cacheType == "all" || cacheType == "",
	})
	return err
}

func (c *embeddedProtocolClient) GetConfig(ctx context.Context) (*CacheConfigResult, error) {
	if c.client == nil {
		return nil, &qberrors.MessageError{Message: "embedded client not initialized"}
	}

	resp, err := c.client.Cache().GetCacheConfig(ctx, &quicbaas.GetCacheConfigRequest{})
	if err != nil {
		return nil, err
	}

	return &CacheConfigResult{
		L1Size:          resp.L1Size,
		L1TTL:           resp.L1TTL,
		CleanupInterval: resp.CleanupInterval,
		Enabled:         resp.Enabled,
	}, nil
}

func (c *embeddedProtocolClient) UpdateConfig(ctx context.Context, config *CacheConfigUpdate) error {
	if c.client == nil {
		return &qberrors.MessageError{Message: "embedded client not initialized"}
	}

	req := &quicbaas.UpdateCacheConfigRequest{
		L1Size:          config.L1Size,
		L1TTL:           config.L1TTL,
		CleanupInterval: config.CleanupInterval,
	}
	_, err := c.client.Cache().UpdateCacheConfig(ctx, req)
	return err
}
