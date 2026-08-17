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

package iam

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PolicyCache is a thread-safe TTL cache of raw stored policies, keyed by
// tenant+name. It is the surviving half of the PEP cache/sync/subscribe
// distribution layer (P6.1 end-state of the unified login-plane plan): the
// duplicated local ABAC engine that used to compile policies into
// CompiledPolicy for fast local evaluation was deleted once qrdb shipped its
// own live-reload seam (qrdb's abac.Decorator, driven via ABACSync, is now
// the single decision-affecting compiled representation). This cache no
// longer influences any authorization decision — PolicyEngine.Evaluate never
// reads it — it exists purely so PolicyEngine's CRUD methods keep their
// historical InvalidateAll-on-mutation contract for any caller that still
// polls it (see NewPolicyCache/InvalidateAll callers in policy.go).
type PolicyCache struct {
	// policies maps tenantID:policyName -> cacheEntry
	policies sync.Map

	// version tracks cache version to prevent stale data
	version atomic.Int64

	// config holds cache configuration
	config *CacheConfig
}

// CacheConfig holds cache configuration.
type CacheConfig struct {
	// TTL is the cache entry time-to-live
	TTL time.Duration

	// MaxSize is the maximum number of cached policies
	MaxSize int

	// BloomFilterSize and BloomFilterHashes are retained for API
	// compatibility with existing callers of NewCacheConfig; the bloom
	// filter itself was removed along with the deleted local ABAC engine
	// (it existed solely to short-circuit local deny-rule evaluation).
	BloomFilterSize   int
	BloomFilterHashes int
}

// NewCacheConfig creates a cache config from PEP config.
func NewCacheConfig(pepConfig *PEPConfig) *CacheConfig {
	return &CacheConfig{
		TTL:               pepConfig.CacheTTL,
		MaxSize:           pepConfig.MaxCacheSize,
		BloomFilterSize:   1000000, // 1M bits ~= 125KB
		BloomFilterHashes: 7,       // Optimal for ~1% false positive rate
	}
}

// PEPConfig holds the configuration a PolicyCache is built from. Retained
// for NewCacheConfig's API shape; the PEP struct that used to embed a
// PolicyCache alongside a local compiler/evaluator was deleted (it was
// never wired into production — pkg/server drives PolicyEngine directly).
type PEPConfig struct {
	// TenantID is the tenant this cache serves.
	TenantID string

	// CacheTTL is the cache entry time-to-live.
	CacheTTL time.Duration

	// MaxCacheSize is the maximum number of cached policies.
	MaxCacheSize int
}

// DefaultPEPConfig returns a PEPConfig with sensible defaults.
func DefaultPEPConfig(tenantID string) *PEPConfig {
	return &PEPConfig{
		TenantID:     tenantID,
		CacheTTL:     5 * time.Minute,
		MaxCacheSize: 10000,
	}
}

// cacheEntry wraps a policy with cache metadata.
type cacheEntry struct {
	policy    *Policy
	cachedAt  time.Time
	expiresAt time.Time
	version   int64
}

// NewPolicyCache creates a new policy cache.
func NewPolicyCache(config *CacheConfig) *PolicyCache {
	return &PolicyCache{
		config: config,
	}
}

// Get retrieves a policy from the cache.
func (c *PolicyCache) Get(tenantID, name string) (*Policy, bool) {
	key := c.makeKey(tenantID, name)

	value, ok := c.policies.Load(key)
	if !ok {
		return nil, false
	}

	entry := value.(*cacheEntry) //nolint:errcheck // type assertion guaranteed by sync.Map contract

	// Check if entry is expired
	if time.Now().After(entry.expiresAt) {
		c.policies.Delete(key)
		return nil, false
	}

	return entry.policy, true
}

// Set stores a policy in the cache.
func (c *PolicyCache) Set(tenantID string, policy *Policy) {
	key := c.makeKey(tenantID, policy.Metadata.Name)
	now := time.Now()

	entry := &cacheEntry{
		policy:    policy,
		cachedAt:  now,
		expiresAt: now.Add(c.config.TTL),
		version:   c.version.Add(1),
	}

	c.policies.Store(key, entry)
}

// Invalidate removes a specific policy from the cache.
func (c *PolicyCache) Invalidate(tenantID, name string) {
	key := c.makeKey(tenantID, name)
	c.policies.Delete(key)
	c.version.Add(1)
}

// InvalidateAll removes all policies for a tenant from the cache. Driven by
// every PolicyEngine CRUD mutation (create/update/delete), unchanged from
// its historical contract.
func (c *PolicyCache) InvalidateAll(tenantID string) {
	prefix := tenantID + ":"

	c.policies.Range(func(key, value interface{}) bool {
		if strings.HasPrefix(key.(string), prefix) { //nolint:errcheck // type assertion guaranteed by sync.Map contract
			c.policies.Delete(key)
		}
		return true
	})

	c.version.Add(1)
}

// GetVersion returns the current cache version.
func (c *PolicyCache) GetVersion() int64 {
	return c.version.Load()
}

// GetAll returns all cached policies for a tenant, sorted by priority
// (higher first).
func (c *PolicyCache) GetAll(tenantID string) []*Policy {
	prefix := tenantID + ":"
	var policies []*Policy

	c.policies.Range(func(key, value interface{}) bool {
		if strings.HasPrefix(key.(string), prefix) { //nolint:errcheck // type assertion guaranteed by sync.Map contract
			entry := value.(*cacheEntry) //nolint:errcheck // type assertion guaranteed by sync.Map contract
			// Skip expired entries
			if time.Now().Before(entry.expiresAt) {
				policies = append(policies, entry.policy)
			}
		}
		return true
	})

	sort.SliceStable(policies, func(i, j int) bool {
		return policies[i].Metadata.Priority > policies[j].Metadata.Priority
	})

	return policies
}

// Size returns the number of cached policies.
func (c *PolicyCache) Size() int {
	count := 0
	c.policies.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// Clear removes all entries from the cache.
func (c *PolicyCache) Clear() {
	c.policies.Range(func(key, value interface{}) bool {
		c.policies.Delete(key)
		return true
	})
	c.version.Add(1)
}

// SetBatch stores multiple policies for a tenant in the cache.
func (c *PolicyCache) SetBatch(tenantID string, policies []*Policy) {
	for _, policy := range policies {
		c.Set(tenantID, policy)
	}
}

// makeKey creates a cache key from tenant ID and policy name.
func (c *PolicyCache) makeKey(tenantID, name string) string {
	return tenantID + ":" + name
}

// EvictExpired removes expired entries from the cache. This should be
// called periodically to clean up stale entries.
func (c *PolicyCache) EvictExpired() int {
	now := time.Now()
	count := 0

	c.policies.Range(func(key, value interface{}) bool {
		entry := value.(*cacheEntry) //nolint:errcheck // type assertion guaranteed by sync.Map contract
		if now.After(entry.expiresAt) {
			c.policies.Delete(key)
			count++
		}
		return true
	})

	if count > 0 {
		c.version.Add(1)
	}

	return count
}

// EvictOldest evicts the oldest entry from the cache. This is used when the
// cache is full and we need to make room.
func (c *PolicyCache) EvictOldest() bool {
	var oldestKey interface{}
	var oldestTime time.Time

	c.policies.Range(func(key, value interface{}) bool {
		entry := value.(*cacheEntry) //nolint:errcheck // type assertion guaranteed by sync.Map contract
		if oldestKey == nil || entry.cachedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.cachedAt
		}
		return true
	})

	if oldestKey != nil {
		c.policies.Delete(oldestKey)
		c.version.Add(1)
		return true
	}

	return false
}

// EnforceMaxSize ensures the cache does not exceed MaxSize. Evicts oldest
// entries if necessary.
func (c *PolicyCache) EnforceMaxSize() {
	if c.config.MaxSize <= 0 {
		return
	}

	for c.Size() > c.config.MaxSize {
		if !c.EvictOldest() {
			break
		}
	}
}
