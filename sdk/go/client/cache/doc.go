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

// Package cache provides the Cache client for the QuicBaaS SDK.
//
// The Cache client supports cache management operations including
// statistics retrieval, cache clearing, and configuration management.
//
// # Basic Usage
//
//	client := cache.NewClient(transport)
//
//	// Get cache statistics
//	stats, err := client.GetCacheStats(ctx, &cache.GetCacheStatsRequest{})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	log.Printf("Hit Rate: %.2f%%, Size: %d/%d", stats.HitRate*100, stats.Size, stats.MaxSize)
//
//	// Clear cache entries
//	resp, err := client.ClearCache(ctx, &cache.ClearCacheRequest{All: true})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	log.Printf("Cleared %d entries", resp.EntriesCleared)
//
// # Configuration Management
//
// The cache client allows runtime configuration updates:
//
//	// Get current config
//	config, err := client.GetCacheConfig(ctx, &cache.GetCacheConfigRequest{})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Update cache configuration
//	newSize := 10000
//	newTTL := 5 * time.Minute
//	resp, err := client.UpdateCacheConfig(ctx, &cache.UpdateCacheConfigRequest{
//	    L1Size: &newSize,
//	    L1TTL:  &newTTL,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
// # Entity Type Filtering
//
// Statistics and cache operations can be filtered by entity type:
//
//	// Get stats for specific entity type
//	stats, err := client.GetCacheStats(ctx, &cache.GetCacheStatsRequest{
//	    EntityType: "users",
//	})
//
//	// Clear cache for specific entity type
//	resp, err := client.ClearCache(ctx, &cache.ClearCacheRequest{
//	    EntityType: "users",
//	})
package cache
