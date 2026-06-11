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
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// PolicyCache implements a thread-safe LRU cache for compiled policies.
// It uses lock-free operations where possible for maximum performance.
type PolicyCache struct {
	// policies maps tenantID:policyName -> cacheEntry
	policies sync.Map

	// bloomFilter provides quick deny checks
	bloomFilter *BloomFilter

	// resourceTrie provides fast resource pattern matching
	resourceTrie *ResourceTrie

	// version tracks cache version to prevent stale data
	version atomic.Int64

	// config holds cache configuration
	config *CacheConfig

	// mu protects access to the resource trie
	trieMu sync.RWMutex
}

// CacheConfig holds cache configuration.
type CacheConfig struct {
	// TTL is the cache entry time-to-live
	TTL time.Duration

	// MaxSize is the maximum number of cached policies
	MaxSize int

	// BloomFilterSize is the bloom filter size in bits
	BloomFilterSize int

	// BloomFilterHashes is the number of hash functions for the bloom filter
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

// cacheEntry wraps a compiled policy with metadata.
type cacheEntry struct {
	policy    *CompiledPolicy
	cachedAt  time.Time
	expiresAt time.Time
	version   int64
}

// NewPolicyCache creates a new policy cache.
func NewPolicyCache(config *CacheConfig) *PolicyCache {
	return &PolicyCache{
		bloomFilter:  NewBloomFilter(uint64(config.BloomFilterSize), uint32(config.BloomFilterHashes)), //nolint:gosec // G115: config values are validated positive
		resourceTrie: NewResourceTrie(),
		config:       config,
	}
}

// Get retrieves a compiled policy from the cache.
func (c *PolicyCache) Get(tenantID, name string) (*CompiledPolicy, bool) {
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

// Set stores a compiled policy in the cache.
func (c *PolicyCache) Set(tenantID string, policy *CompiledPolicy) {
	key := c.makeKey(tenantID, policy.Name)
	now := time.Now()

	entry := &cacheEntry{
		policy:    policy,
		cachedAt:  now,
		expiresAt: now.Add(c.config.TTL),
		version:   c.version.Add(1),
	}

	c.policies.Store(key, entry)

	// Update bloom filter for deny rules
	c.updateBloomFilter(policy)

	// Update resource trie
	c.updateResourceTrie(policy)
}

// Invalidate removes a specific policy from the cache.
func (c *PolicyCache) Invalidate(tenantID, name string) {
	key := c.makeKey(tenantID, name)
	c.policies.Delete(key)
	c.version.Add(1)
}

// InvalidateAll removes all policies for a tenant from the cache.
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

// QuickDenyCheck performs a fast bloom filter check for potential deny rules.
// Returns true if a deny rule might apply (requires full evaluation).
func (c *PolicyCache) QuickDenyCheck(resource string) bool {
	if c.bloomFilter == nil {
		return false
	}
	return c.bloomFilter.Contains([]byte(resource))
}

// GetVersion returns the current cache version.
func (c *PolicyCache) GetVersion() int64 {
	return c.version.Load()
}

// GetApplicablePolicies returns all policies that might apply to a request.
// This uses the resource trie for fast O(k) lookups.
func (c *PolicyCache) GetApplicablePolicies(resource string, subject Subject) []*CompiledPolicy {
	c.trieMu.RLock()
	defer c.trieMu.RUnlock()

	// Get policies matching the resource pattern
	value, _, found := c.resourceTrie.Match(resource)
	if !found {
		return nil
	}

	// The trie stores a single policy per pattern
	// In a production system, we might store multiple policies per pattern
	var policies []*CompiledPolicy
	if policy, ok := value.(*CompiledPolicy); ok {
		policies = []*CompiledPolicy{policy}
	}

	// Filter by subject
	var applicable []*CompiledPolicy
	for _, policy := range policies {
		if c.subjectMatches(policy, subject) {
			applicable = append(applicable, policy)
		}
	}

	// Sort by priority (higher first)
	// Use stable sort to maintain insertion order for equal priorities
	c.sortByPriority(applicable)

	return applicable
}

// GetAll returns all cached policies for a tenant, sorted by priority (higher first).
func (c *PolicyCache) GetAll(tenantID string) []*CompiledPolicy {
	prefix := tenantID + ":"
	var policies []*CompiledPolicy

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

	// Sort policies by priority (higher first) to ensure consistent evaluation order
	c.sortByPriority(policies)

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

// SetBatch stores multiple compiled policies for a tenant in the cache.
// This is an alias for the batch interface expected by PolicyEngine.
func (c *PolicyCache) SetBatch(tenantID string, policies []*CompiledPolicy) {
	for _, policy := range policies {
		c.Set(tenantID, policy)
	}
}

// makeKey creates a cache key from tenant ID and policy name.
func (c *PolicyCache) makeKey(tenantID, name string) string {
	return tenantID + ":" + name
}

// updateBloomFilter updates the bloom filter with deny rules from a policy.
func (c *PolicyCache) updateBloomFilter(policy *CompiledPolicy) {
	// Filter rules by effect to find deny rules
	for _, rule := range policy.Rules {
		if rule.Effect == EffectDeny {
			for _, resource := range rule.Actions {
				c.bloomFilter.Add([]byte(resource))
			}
		}
	}
}

// updateResourceTrie updates the resource trie with a policy's resources.
func (c *PolicyCache) updateResourceTrie(policy *CompiledPolicy) {
	c.trieMu.Lock()
	defer c.trieMu.Unlock()

	// Add all resource patterns from all rules
	for _, rule := range policy.Rules {
		for _, resource := range rule.Actions {
			c.resourceTrie.Insert(resource, policy)
		}
	}
}

// subjectMatches checks if a policy applies to a subject.
func (c *PolicyCache) subjectMatches(policy *CompiledPolicy, subject Subject) bool {
	if policy.SubjectMatcher == nil {
		// No subject restrictions - matches all
		return true
	}

	matcher := policy.SubjectMatcher

	// Check if subject matches any user filter
	if len(matcher.Users) > 0 {
		if _, ok := matcher.Users[subject.ID]; ok {
			return true
		}
	}

	// Check if any of subject's roles match
	if len(matcher.Roles) > 0 {
		for _, role := range subject.Roles {
			if _, ok := matcher.Roles[role]; ok {
				return true
			}
		}
	}

	// Check if any of subject's groups match
	if len(matcher.Groups) > 0 {
		for _, group := range subject.Groups {
			if _, ok := matcher.Groups[group]; ok {
				return true
			}
		}
	}

	// If no specific filters, policy applies
	if len(matcher.Users) == 0 && len(matcher.Roles) == 0 && len(matcher.Groups) == 0 {
		return true
	}

	return false
}

// sortByPriority sorts policies by priority (higher first) using insertion sort.
// This is efficient for small arrays which is typical for applicable policies.
// #nosec G602 -- bounds are managed by loop - j >= 0 check prevents underflow, j+1 <= i is bounded
func (c *PolicyCache) sortByPriority(policies []*CompiledPolicy) {
	for i := 1; i < len(policies); i++ {
		key := policies[i]
		j := i - 1

		// Move elements with lower priority one position ahead
		for j >= 0 && policies[j].Priority < key.Priority {
			policies[j+1] = policies[j]
			j--
		}
		policies[j+1] = key
	}
}

// EvictExpired removes expired entries from the cache.
// This should be called periodically to clean up stale entries.
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

// EvictOldest evicts the oldest entry from the cache.
// This is used when the cache is full and we need to make room.
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

// EnforceMaxSize ensures the cache does not exceed MaxSize.
// Evicts oldest entries if necessary.
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
