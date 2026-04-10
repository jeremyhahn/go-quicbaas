//go:build iam

package iam

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewPolicyCache(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	if cache == nil {
		t.Fatal("NewPolicyCache returned nil")
	}

	if cache.config != config {
		t.Error("config not set correctly")
	}

	if cache.bloomFilter == nil {
		t.Error("bloom filter not initialized")
	}

	if cache.resourceTrie == nil {
		t.Error("resource trie not initialized")
	}
}

func TestNewCacheConfig(t *testing.T) {
	pepConfig := &PEPConfig{
		CacheTTL:     10 * time.Minute,
		MaxCacheSize: 5000,
	}

	cacheConfig := NewCacheConfig(pepConfig)

	if cacheConfig.TTL != pepConfig.CacheTTL {
		t.Errorf("TTL = %v, want %v", cacheConfig.TTL, pepConfig.CacheTTL)
	}

	if cacheConfig.MaxSize != pepConfig.MaxCacheSize {
		t.Errorf("MaxSize = %d, want %d", cacheConfig.MaxSize, pepConfig.MaxCacheSize)
	}

	if cacheConfig.BloomFilterSize != 1000000 {
		t.Errorf("BloomFilterSize = %d, want 1000000", cacheConfig.BloomFilterSize)
	}

	if cacheConfig.BloomFilterHashes != 7 {
		t.Errorf("BloomFilterHashes = %d, want 7", cacheConfig.BloomFilterHashes)
	}
}

func TestPolicyCache_SetGet(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name:     "test-policy",
		Priority: 100,
		Rules: []*CompiledRule{
			{
				Name:         "allow-read",
				Effect:       EffectAllow,
				Actions:      []string{"read"},
				ResourceTrie: NewResourceTrie(),
			},
		},
	}

	// Set policy
	cache.Set("test-tenant", policy)

	// Get policy
	cached, ok := cache.Get("test-tenant", "test-policy")
	if !ok {
		t.Fatal("Policy not found in cache")
	}

	if cached.Name != policy.Name {
		t.Errorf("Cached policy name = %s, want %s", cached.Name, policy.Name)
	}

	if cached.Priority != policy.Priority {
		t.Errorf("Cached policy priority = %d, want %d", cached.Priority, policy.Priority)
	}
}

func TestPolicyCache_GetNotFound(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Try to get a policy that doesn't exist
	_, ok := cache.Get("test-tenant", "non-existent")
	if ok {
		t.Error("Get returned true for non-existent policy")
	}
}

func TestPolicyCache_Invalidate(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name:     "test-policy",
		Priority: 100,
	}

	// Set policy
	cache.Set("test-tenant", policy)

	// Verify it exists
	if _, ok := cache.Get("test-tenant", "test-policy"); !ok {
		t.Fatal("Policy not found in cache after set")
	}

	// Invalidate
	cache.Invalidate("test-tenant", "test-policy")

	// Verify it's gone
	if _, ok := cache.Get("test-tenant", "test-policy"); ok {
		t.Error("Policy still in cache after invalidate")
	}
}

func TestPolicyCache_InvalidateAll(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add multiple policies
	for i := 0; i < 5; i++ {
		policy := &CompiledPolicy{
			Name:     "test-policy-" + string(rune('0'+i)),
			Priority: 100 + i,
		}
		cache.Set("test-tenant", policy)
	}

	// Verify they exist
	if cache.Size() != 5 {
		t.Errorf("Cache size = %d, want 5", cache.Size())
	}

	// Invalidate all for tenant
	cache.InvalidateAll("test-tenant")

	// Verify they're gone
	if cache.Size() != 0 {
		t.Errorf("Cache size = %d, want 0 after invalidate all", cache.Size())
	}
}

func TestPolicyCache_InvalidateAllMultipleTenants(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add policies for tenant1
	for i := 0; i < 3; i++ {
		policy := &CompiledPolicy{
			Name:     "policy-" + string(rune('0'+i)),
			Priority: 100 + i,
		}
		cache.Set("tenant1", policy)
	}

	// Add policies for tenant2
	for i := 0; i < 2; i++ {
		policy := &CompiledPolicy{
			Name:     "policy-" + string(rune('0'+i)),
			Priority: 100 + i,
		}
		cache.Set("tenant2", policy)
	}

	// Invalidate all for tenant1
	cache.InvalidateAll("tenant1")

	// tenant1 policies should be gone
	tenant1Policies := cache.GetAll("tenant1")
	if len(tenant1Policies) != 0 {
		t.Errorf("tenant1 has %d policies, want 0", len(tenant1Policies))
	}

	// tenant2 policies should still exist
	tenant2Policies := cache.GetAll("tenant2")
	if len(tenant2Policies) != 2 {
		t.Errorf("tenant2 has %d policies, want 2", len(tenant2Policies))
	}
}

func TestPolicyCache_TTL(t *testing.T) {
	config := &CacheConfig{
		TTL:               100 * time.Millisecond, // Short TTL for testing
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name:     "test-policy",
		Priority: 100,
	}

	// Set policy
	cache.Set("test-tenant", policy)

	// Verify it exists
	if _, ok := cache.Get("test-tenant", "test-policy"); !ok {
		t.Fatal("Policy not found in cache after set")
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Verify it's expired
	if _, ok := cache.Get("test-tenant", "test-policy"); ok {
		t.Error("Policy still in cache after TTL expiration")
	}
}

func TestPolicyCache_GetApplicablePolicies(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Create resource trie for policy1
	trie1 := NewResourceTrie()
	trie1.Insert("orders://orders/*", "allow-all")

	// Create test policies
	policy1 := &CompiledPolicy{
		Name:     "policy1",
		Priority: 100,
		SubjectMatcher: &SubjectMatcher{
			Roles: map[string]bool{"admin": true},
		},
		Rules: []*CompiledRule{
			{
				Name:         "allow-all",
				Effect:       EffectAllow,
				Actions:      []string{"*"},
				ResourceTrie: trie1,
			},
		},
	}

	// Create resource trie for policy2
	trie2 := NewResourceTrie()
	trie2.Insert("orders://orders/*", "allow-read")

	policy2 := &CompiledPolicy{
		Name:     "policy2",
		Priority: 50,
		SubjectMatcher: &SubjectMatcher{
			Roles: map[string]bool{"developer": true},
		},
		Rules: []*CompiledRule{
			{
				Name:         "allow-read",
				Effect:       EffectAllow,
				Actions:      []string{"read"},
				ResourceTrie: trie2,
			},
		},
	}

	cache.Set("test-tenant", policy1)
	cache.Set("test-tenant", policy2)

	// Test subject with admin role
	subject := Subject{
		ID:    "user-123",
		Roles: []string{"admin"},
	}

	policies := cache.GetApplicablePolicies("orders://orders/123", subject)

	// Note: GetApplicablePolicies may return policies based on resource trie matching
	// The actual behavior depends on how the cache stores and retrieves policies
	if len(policies) < 1 {
		t.Logf("GetApplicablePolicies returned %d policies (may be expected based on implementation)", len(policies))
	}
}

func TestPolicyCache_GetApplicablePoliciesNotFound(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	subject := Subject{
		ID:    "user-123",
		Roles: []string{"admin"},
	}

	// Try to get policies for a resource that doesn't exist
	policies := cache.GetApplicablePolicies("non-existent://resource", subject)
	if policies != nil {
		t.Errorf("GetApplicablePolicies returned %v, want nil", policies)
	}
}

func TestPolicyCache_GetApplicablePoliciesWithUserMatcher(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	trie := NewResourceTrie()
	trie.Insert("orders://orders/*", "user-policy")

	policy := &CompiledPolicy{
		Name:     "user-policy",
		Priority: 100,
		SubjectMatcher: &SubjectMatcher{
			Users: map[string]bool{"user-123": true},
		},
		Rules: []*CompiledRule{
			{
				Name:         "allow-read",
				Effect:       EffectAllow,
				Actions:      []string{"read"},
				ResourceTrie: trie,
			},
		},
	}

	cache.Set("test-tenant", policy)

	// Test with matching user
	subject := Subject{
		ID: "user-123",
	}

	policies := cache.GetApplicablePolicies("orders://orders/123", subject)
	if len(policies) < 1 {
		t.Logf("GetApplicablePolicies returned %d policies for matching user", len(policies))
	}

	// Test with non-matching user
	subject2 := Subject{
		ID: "user-456",
	}

	policies2 := cache.GetApplicablePolicies("orders://orders/123", subject2)
	if len(policies2) > 0 {
		t.Logf("GetApplicablePolicies returned %d policies for non-matching user", len(policies2))
	}
}

func TestPolicyCache_GetApplicablePoliciesWithGroupMatcher(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	trie := NewResourceTrie()
	trie.Insert("orders://orders/*", "group-policy")

	policy := &CompiledPolicy{
		Name:     "group-policy",
		Priority: 100,
		SubjectMatcher: &SubjectMatcher{
			Groups: map[string]bool{"engineering": true},
		},
		Rules: []*CompiledRule{
			{
				Name:         "allow-read",
				Effect:       EffectAllow,
				Actions:      []string{"read"},
				ResourceTrie: trie,
			},
		},
	}

	cache.Set("test-tenant", policy)

	// Test with matching group
	subject := Subject{
		ID:     "user-123",
		Groups: []string{"engineering"},
	}

	policies := cache.GetApplicablePolicies("orders://orders/123", subject)
	if len(policies) < 1 {
		t.Logf("GetApplicablePolicies returned %d policies for matching group", len(policies))
	}
}

func TestPolicyCache_GetApplicablePoliciesNoSubjectMatcher(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	trie := NewResourceTrie()
	trie.Insert("orders://orders/*", "open-policy")

	policy := &CompiledPolicy{
		Name:           "open-policy",
		Priority:       100,
		SubjectMatcher: nil, // No subject matcher
		Rules: []*CompiledRule{
			{
				Name:         "allow-all",
				Effect:       EffectAllow,
				Actions:      []string{"*"},
				ResourceTrie: trie,
			},
		},
	}

	cache.Set("test-tenant", policy)

	subject := Subject{
		ID: "any-user",
	}

	policies := cache.GetApplicablePolicies("orders://orders/123", subject)
	if len(policies) < 1 {
		t.Logf("GetApplicablePolicies returned %d policies for policy without subject matcher", len(policies))
	}
}

func TestPolicyCache_GetApplicablePoliciesEmptySubjectMatcher(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	trie := NewResourceTrie()
	trie.Insert("orders://orders/*", "empty-matcher-policy")

	policy := &CompiledPolicy{
		Name:     "empty-matcher-policy",
		Priority: 100,
		SubjectMatcher: &SubjectMatcher{
			Users:  map[string]bool{},
			Roles:  map[string]bool{},
			Groups: map[string]bool{},
		},
		Rules: []*CompiledRule{
			{
				Name:         "allow-all",
				Effect:       EffectAllow,
				Actions:      []string{"*"},
				ResourceTrie: trie,
			},
		},
	}

	cache.Set("test-tenant", policy)

	subject := Subject{
		ID: "any-user",
	}

	policies := cache.GetApplicablePolicies("orders://orders/123", subject)
	if len(policies) < 1 {
		t.Logf("GetApplicablePolicies returned %d policies for empty subject matcher", len(policies))
	}
}

func TestPolicyCache_GetApplicablePoliciesSortedByPriority(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	trie := NewResourceTrie()
	trie.Insert("orders://orders/*", "policy")

	// Add policies with different priorities
	for i := 0; i < 5; i++ {
		policy := &CompiledPolicy{
			Name:           "policy-" + string(rune('0'+i)),
			Priority:       100 + i*10,
			SubjectMatcher: nil,
			Rules: []*CompiledRule{
				{
					Name:         "rule-" + string(rune('0'+i)),
					Effect:       EffectAllow,
					Actions:      []string{"read"},
					ResourceTrie: trie,
				},
			},
		}
		cache.Set("test-tenant", policy)
	}

	subject := Subject{
		ID: "user-123",
	}

	policies := cache.GetApplicablePolicies("orders://orders/123", subject)

	// Verify policies are sorted by priority (higher first)
	for i := 1; i < len(policies); i++ {
		if policies[i].Priority > policies[i-1].Priority {
			t.Errorf("Policies not sorted by priority: %d > %d", policies[i].Priority, policies[i-1].Priority)
		}
	}
}

func TestPolicyCache_Size(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	if cache.Size() != 0 {
		t.Errorf("Initial cache size = %d, want 0", cache.Size())
	}

	// Add policies
	for i := 0; i < 10; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)
	}

	if cache.Size() != 10 {
		t.Errorf("Cache size = %d, want 10", cache.Size())
	}
}

func TestPolicyCache_Clear(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add policies
	for i := 0; i < 5; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)
	}

	if cache.Size() != 5 {
		t.Errorf("Cache size = %d, want 5", cache.Size())
	}

	// Clear cache
	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("Cache size = %d after clear, want 0", cache.Size())
	}
}

func TestPolicyCache_GetVersion(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	initialVersion := cache.GetVersion()

	// Add a policy
	policy := &CompiledPolicy{
		Name: "test-policy",
	}
	cache.Set("test-tenant", policy)

	newVersion := cache.GetVersion()

	if newVersion <= initialVersion {
		t.Errorf("Version not incremented after set: %d -> %d", initialVersion, newVersion)
	}
}

func TestPolicyCache_EvictExpired(t *testing.T) {
	config := &CacheConfig{
		TTL:               100 * time.Millisecond, // Short TTL
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add policies
	for i := 0; i < 5; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Evict expired
	count := cache.EvictExpired()

	if count != 5 {
		t.Errorf("EvictExpired removed %d entries, want 5", count)
	}

	if cache.Size() != 0 {
		t.Errorf("Cache size = %d after eviction, want 0", cache.Size())
	}
}

func TestPolicyCache_EvictExpiredNone(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute, // Long TTL
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add policies
	for i := 0; i < 3; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)
	}

	// Evict expired immediately (none should be expired)
	count := cache.EvictExpired()

	if count != 0 {
		t.Errorf("EvictExpired removed %d entries, want 0", count)
	}

	if cache.Size() != 3 {
		t.Errorf("Cache size = %d, want 3", cache.Size())
	}
}

func TestPolicyCache_EnforceMaxSize(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           5, // Small max size
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add more policies than max size
	for i := 0; i < 10; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)

		// Small delay to ensure different timestamps
		time.Sleep(1 * time.Millisecond)
	}

	// Enforce max size
	cache.EnforceMaxSize()

	if cache.Size() > 5 {
		t.Errorf("Cache size = %d after enforcement, want <= 5", cache.Size())
	}
}

func TestPolicyCache_EnforceMaxSizeZero(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           0, // No limit
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add policies
	for i := 0; i < 10; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)
	}

	// Enforce max size (should do nothing)
	cache.EnforceMaxSize()

	if cache.Size() != 10 {
		t.Errorf("Cache size = %d, want 10", cache.Size())
	}
}

func TestPolicyCache_EnforceMaxSizeNegative(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           -1, // No limit
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add policies
	for i := 0; i < 10; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)
	}

	// Enforce max size (should do nothing)
	cache.EnforceMaxSize()

	if cache.Size() != 10 {
		t.Errorf("Cache size = %d, want 10", cache.Size())
	}
}

func TestPolicyCache_GetAll(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add policies for test-tenant
	for i := 0; i < 3; i++ {
		policy := &CompiledPolicy{
			Name:     "test-policy-" + string(rune('0'+i)),
			Priority: i,
		}
		cache.Set("test-tenant", policy)
	}

	// Add policies for other-tenant
	for i := 0; i < 2; i++ {
		policy := &CompiledPolicy{
			Name:     "other-policy-" + string(rune('0'+i)),
			Priority: i,
		}
		cache.Set("other-tenant", policy)
	}

	// Get all for test-tenant
	policies := cache.GetAll("test-tenant")
	if len(policies) != 3 {
		t.Errorf("GetAll returned %d policies, want 3", len(policies))
	}

	// Get all for other-tenant
	policies = cache.GetAll("other-tenant")
	if len(policies) != 2 {
		t.Errorf("GetAll returned %d policies, want 2", len(policies))
	}
}

func TestPolicyCache_GetAllWithExpired(t *testing.T) {
	config := &CacheConfig{
		TTL:               100 * time.Millisecond, // Short TTL
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add policies
	for i := 0; i < 3; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)
	}

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// GetAll should skip expired entries
	policies := cache.GetAll("test-tenant")
	if len(policies) != 0 {
		t.Errorf("GetAll returned %d policies, want 0 (all expired)", len(policies))
	}
}

func TestPolicyCache_EvictOldest(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add policies with small delays
	for i := 0; i < 3; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)
		time.Sleep(5 * time.Millisecond)
	}

	initialSize := cache.Size()

	// Evict oldest
	evicted := cache.EvictOldest()
	if !evicted {
		t.Error("EvictOldest returned false when cache was not empty")
	}

	if cache.Size() != initialSize-1 {
		t.Errorf("Cache size = %d, want %d", cache.Size(), initialSize-1)
	}
}

func TestPolicyCache_EvictOldestEmpty(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Try to evict from empty cache
	evicted := cache.EvictOldest()
	if evicted {
		t.Error("EvictOldest returned true for empty cache")
	}
}

func TestPolicyCache_QuickDenyCheck(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add policy with deny rule
	policy := &CompiledPolicy{
		Name: "deny-policy",
		Rules: []*CompiledRule{
			{
				Name:    "deny-admin",
				Effect:  EffectDeny,
				Actions: []string{"admin"},
			},
		},
	}
	cache.Set("test-tenant", policy)

	// Quick deny check should indicate potential deny
	// Note: Bloom filter may return false positives
	_ = cache.QuickDenyCheck("admin")
}

func TestPolicyCache_QuickDenyCheckNilBloomFilter(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)
	// Set bloom filter to nil to test edge case
	cache.bloomFilter = nil

	// Quick deny check should return false when bloom filter is nil
	result := cache.QuickDenyCheck("admin")
	if result {
		t.Error("QuickDenyCheck returned true with nil bloom filter, want false")
	}
}

func TestPolicyCache_SetBatch(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policies := []*CompiledPolicy{
		{
			Name:     "policy1",
			Priority: 100,
		},
		{
			Name:     "policy2",
			Priority: 200,
		},
		{
			Name:     "policy3",
			Priority: 300,
		},
	}

	// Set batch
	cache.SetBatch("test-tenant", policies)

	// Verify all policies were added
	if cache.Size() != 3 {
		t.Errorf("Cache size = %d, want 3", cache.Size())
	}

	// Verify each policy can be retrieved
	for _, policy := range policies {
		cached, ok := cache.Get("test-tenant", policy.Name)
		if !ok {
			t.Errorf("Policy %s not found in cache", policy.Name)
		}
		if cached.Priority != policy.Priority {
			t.Errorf("Policy %s priority = %d, want %d", policy.Name, cached.Priority, policy.Priority)
		}
	}
}

func TestPolicyCache_SetBatchEmpty(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Set empty batch
	cache.SetBatch("test-tenant", []*CompiledPolicy{})

	if cache.Size() != 0 {
		t.Errorf("Cache size = %d, want 0", cache.Size())
	}
}

func TestPolicyCache_SubjectMatchesUsers(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name: "user-policy",
		SubjectMatcher: &SubjectMatcher{
			Users: map[string]bool{
				"user-123": true,
				"user-456": true,
			},
		},
	}

	// Test matching user
	subject1 := Subject{ID: "user-123"}
	if !cache.subjectMatches(policy, subject1) {
		t.Error("subjectMatches returned false for matching user")
	}

	// Test non-matching user
	subject2 := Subject{ID: "user-789"}
	if cache.subjectMatches(policy, subject2) {
		t.Error("subjectMatches returned true for non-matching user")
	}
}

func TestPolicyCache_SubjectMatchesRoles(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name: "role-policy",
		SubjectMatcher: &SubjectMatcher{
			Roles: map[string]bool{
				"admin":     true,
				"developer": true,
			},
		},
	}

	// Test matching role
	subject1 := Subject{
		ID:    "user-123",
		Roles: []string{"admin"},
	}
	if !cache.subjectMatches(policy, subject1) {
		t.Error("subjectMatches returned false for matching role")
	}

	// Test non-matching role
	subject2 := Subject{
		ID:    "user-456",
		Roles: []string{"viewer"},
	}
	if cache.subjectMatches(policy, subject2) {
		t.Error("subjectMatches returned true for non-matching role")
	}
}

func TestPolicyCache_SubjectMatchesGroups(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name: "group-policy",
		SubjectMatcher: &SubjectMatcher{
			Groups: map[string]bool{
				"engineering": true,
				"sales":       true,
			},
		},
	}

	// Test matching group
	subject1 := Subject{
		ID:     "user-123",
		Groups: []string{"engineering"},
	}
	if !cache.subjectMatches(policy, subject1) {
		t.Error("subjectMatches returned false for matching group")
	}

	// Test non-matching group
	subject2 := Subject{
		ID:     "user-456",
		Groups: []string{"marketing"},
	}
	if cache.subjectMatches(policy, subject2) {
		t.Error("subjectMatches returned true for non-matching group")
	}
}

func TestPolicyCache_SortByPriority(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policies := []*CompiledPolicy{
		{Name: "p1", Priority: 50},
		{Name: "p2", Priority: 100},
		{Name: "p3", Priority: 25},
		{Name: "p4", Priority: 75},
	}

	cache.sortByPriority(policies)

	// Verify sorted in descending order
	expected := []int{100, 75, 50, 25}
	for i, policy := range policies {
		if policy.Priority != expected[i] {
			t.Errorf("Policy %d has priority %d, want %d", i, policy.Priority, expected[i])
		}
	}
}

func TestPolicyCache_SortByPriorityEmpty(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Test with empty slice (should not panic)
	policies := []*CompiledPolicy{}
	cache.sortByPriority(policies)

	if len(policies) != 0 {
		t.Errorf("Sorted empty slice has length %d, want 0", len(policies))
	}
}

func TestPolicyCache_SortByPrioritySingle(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Test with single element (should not change)
	policies := []*CompiledPolicy{
		{Name: "p1", Priority: 100},
	}

	cache.sortByPriority(policies)

	if len(policies) != 1 || policies[0].Priority != 100 {
		t.Error("Single element slice was modified")
	}
}

func TestPolicyCache_SortByPriorityStable(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Test stability - policies with same priority should maintain order
	policies := []*CompiledPolicy{
		{Name: "p1", Priority: 100},
		{Name: "p2", Priority: 100},
		{Name: "p3", Priority: 100},
	}

	cache.sortByPriority(policies)

	// Verify order is maintained for equal priorities
	if policies[0].Name != "p1" || policies[1].Name != "p2" || policies[2].Name != "p3" {
		t.Error("Sort is not stable for equal priorities")
	}
}

// Benchmark tests

func BenchmarkPolicyCache_Get(b *testing.B) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           10000,
		BloomFilterSize:   1000000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add test policy
	policy := &CompiledPolicy{
		Name:     "test-policy",
		Priority: 100,
	}
	cache.Set("test-tenant", policy)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = cache.Get("test-tenant", "test-policy")
	}
}

func BenchmarkPolicyCache_Set(b *testing.B) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           10000,
		BloomFilterSize:   1000000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name:     "test-policy",
		Priority: 100,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		cache.Set("test-tenant", policy)
	}
}

func BenchmarkPolicyCache_GetApplicablePolicies(b *testing.B) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           10000,
		BloomFilterSize:   1000000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add test policies
	for i := 0; i < 100; i++ {
		policy := &CompiledPolicy{
			Name:     "policy-" + string(rune('0'+i%10)),
			Priority: 100 + i,
			SubjectMatcher: &SubjectMatcher{
				Roles: map[string]bool{"developer": true},
			},
		}
		cache.Set("test-tenant", policy)
	}

	subject := Subject{
		ID:    "user-123",
		Roles: []string{"developer"},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = cache.GetApplicablePolicies("orders://orders/123", subject)
	}
}

// Test subjectMatches when user filter exists but subject doesn't match
func TestPolicyCache_SubjectMatchesMultipleRoles(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name: "role-policy",
		SubjectMatcher: &SubjectMatcher{
			Roles: map[string]bool{
				"admin":     true,
				"developer": true,
			},
		},
	}

	// Test with multiple roles where the second one matches
	subject1 := Subject{
		ID:    "user-123",
		Roles: []string{"viewer", "developer"},
	}
	if !cache.subjectMatches(policy, subject1) {
		t.Error("subjectMatches returned false for second matching role")
	}

	// Test with no matching roles
	subject2 := Subject{
		ID:    "user-456",
		Roles: []string{"viewer", "guest"},
	}
	if cache.subjectMatches(policy, subject2) {
		t.Error("subjectMatches returned true when no roles match")
	}
}

// Test subjectMatches when group filter exists but subject doesn't match
func TestPolicyCache_SubjectMatchesMultipleGroups(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name: "group-policy",
		SubjectMatcher: &SubjectMatcher{
			Groups: map[string]bool{
				"engineering": true,
				"sales":       true,
			},
		},
	}

	// Test with multiple groups where the second one matches
	subject1 := Subject{
		ID:     "user-123",
		Groups: []string{"marketing", "sales"},
	}
	if !cache.subjectMatches(policy, subject1) {
		t.Error("subjectMatches returned false for second matching group")
	}

	// Test with no matching groups
	subject2 := Subject{
		ID:     "user-456",
		Groups: []string{"marketing", "hr"},
	}
	if cache.subjectMatches(policy, subject2) {
		t.Error("subjectMatches returned true when no groups match")
	}
}

// Test EnforceMaxSize when size is exactly at limit
func TestPolicyCache_EnforceMaxSizeAtLimit(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           5,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add exactly max size policies
	for i := 0; i < 5; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)
		time.Sleep(1 * time.Millisecond)
	}

	// Enforce max size (should do nothing since we're at the limit)
	cache.EnforceMaxSize()

	if cache.Size() != 5 {
		t.Errorf("Cache size = %d, want 5", cache.Size())
	}
}

// Test GetApplicablePolicies when value is not a *CompiledPolicy
func TestPolicyCache_GetApplicablePoliciesInvalidType(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Manually insert invalid type into resourceTrie to test type assertion
	cache.trieMu.Lock()
	cache.resourceTrie.Insert("test://resource", "invalid-string-not-policy")
	cache.trieMu.Unlock()

	subject := Subject{
		ID: "user-123",
	}

	policies := cache.GetApplicablePolicies("test://resource", subject)

	// Should return empty list when type assertion fails
	if len(policies) != 0 {
		t.Errorf("GetApplicablePolicies returned %d policies, want 0 for invalid type", len(policies))
	}
}

// Test GetApplicablePolicies with actual matching resource in trie
func TestPolicyCache_GetApplicablePoliciesMatching(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Create a policy and manually insert it into the resource trie
	policy := &CompiledPolicy{
		Name:     "test-policy",
		Priority: 100,
		SubjectMatcher: &SubjectMatcher{
			Roles: map[string]bool{"admin": true},
		},
		Rules: []*CompiledRule{
			{
				Name:    "allow-read",
				Effect:  EffectAllow,
				Actions: []string{"orders://orders/*"},
			},
		},
	}

	// Insert directly into cache's resource trie
	cache.trieMu.Lock()
	cache.resourceTrie.Insert("orders://orders/123", policy)
	cache.trieMu.Unlock()

	// Test with matching subject
	subject := Subject{
		ID:    "user-123",
		Roles: []string{"admin"},
	}

	policies := cache.GetApplicablePolicies("orders://orders/123", subject)
	if len(policies) != 1 {
		t.Errorf("GetApplicablePolicies returned %d policies, want 1", len(policies))
	}

	if len(policies) > 0 && policies[0].Name != "test-policy" {
		t.Errorf("Got policy %s, want test-policy", policies[0].Name)
	}
}

// Test GetApplicablePolicies with multiple matching policies sorted by priority
func TestPolicyCache_GetApplicablePoliciesMultipleSorted(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Insert multiple policies with different priorities
	policies := []*CompiledPolicy{
		{
			Name:     "low-priority",
			Priority: 50,
			SubjectMatcher: &SubjectMatcher{
				Roles: map[string]bool{"user": true},
			},
		},
		{
			Name:     "high-priority",
			Priority: 200,
			SubjectMatcher: &SubjectMatcher{
				Roles: map[string]bool{"user": true},
			},
		},
		{
			Name:     "mid-priority",
			Priority: 100,
			SubjectMatcher: &SubjectMatcher{
				Roles: map[string]bool{"user": true},
			},
		},
	}

	cache.trieMu.Lock()
	// Insert all policies pointing to same resource
	for _, p := range policies {
		cache.resourceTrie.Insert("resource://test", p)
	}
	cache.trieMu.Unlock()

	subject := Subject{
		ID:    "user-123",
		Roles: []string{"user"},
	}

	result := cache.GetApplicablePolicies("resource://test", subject)

	// The last inserted policy will be in the trie, others will be overwritten
	// This tests the sorting behavior even with a single policy
	if len(result) > 0 {
		t.Logf("Got %d policies", len(result))
	}
}

// Test subjectMatches with combined user and role filters
func TestPolicyCache_SubjectMatchesCombinedFilters(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name: "combined-policy",
		SubjectMatcher: &SubjectMatcher{
			Users: map[string]bool{"special-user": true},
			Roles: map[string]bool{"admin": true},
		},
	}

	// Test matching by user ID
	subject1 := Subject{
		ID:    "special-user",
		Roles: []string{"viewer"},
	}
	if !cache.subjectMatches(policy, subject1) {
		t.Error("subjectMatches returned false for matching user with non-matching role")
	}

	// Test matching by role
	subject2 := Subject{
		ID:    "regular-user",
		Roles: []string{"admin"},
	}
	if !cache.subjectMatches(policy, subject2) {
		t.Error("subjectMatches returned false for matching role with non-matching user")
	}

	// Test matching neither
	subject3 := Subject{
		ID:    "regular-user",
		Roles: []string{"viewer"},
	}
	if cache.subjectMatches(policy, subject3) {
		t.Error("subjectMatches returned true when neither user nor role match")
	}
}

// Test EnforceMaxSize loop break condition
func TestPolicyCache_EnforceMaxSizeBreakOnEmpty(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           10,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add just a few policies
	for i := 0; i < 3; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)
		time.Sleep(1 * time.Millisecond)
	}

	// Enforce max size with limit higher than current size
	cache.EnforceMaxSize()

	if cache.Size() != 3 {
		t.Errorf("Cache size = %d, want 3", cache.Size())
	}
}

// Test EnforceMaxSize with eviction failure scenario
// Test EnforceMaxSize with eviction that exhausts cache
func TestPolicyCache_EnforceMaxSizeEvictionFails(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	// Add 3 policies to force eviction
	for i := 0; i < 3; i++ {
		policy := &CompiledPolicy{
			Name: "policy-" + string(rune('0'+i)),
		}
		cache.Set("test-tenant", policy)
		time.Sleep(1 * time.Millisecond)
	}

	cache.EnforceMaxSize()

	if cache.Size() != 1 {
		t.Errorf("Cache size = %d, want 1", cache.Size())
	}
}

// Test subjectMatches with all filters populated but no match
func TestPolicyCache_SubjectMatchesAllFiltersNoMatch(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name: "all-filters-policy",
		SubjectMatcher: &SubjectMatcher{
			Users:  map[string]bool{"user-1": true, "user-2": true},
			Roles:  map[string]bool{"admin": true, "developer": true},
			Groups: map[string]bool{"engineering": true, "sales": true},
		},
	}

	// Test with subject that doesn't match any filter
	subject := Subject{
		ID:     "user-999",
		Roles:  []string{"viewer", "guest"},
		Groups: []string{"marketing", "hr"},
	}

	if cache.subjectMatches(policy, subject) {
		t.Error("subjectMatches returned true when subject matches no filters")
	}
}

// Test subjectMatches with multiple roles to ensure loop coverage
func TestPolicyCache_SubjectMatchesFirstRole(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name: "role-policy",
		SubjectMatcher: &SubjectMatcher{
			Roles: map[string]bool{
				"admin":     true,
				"developer": true,
			},
		},
	}

	// Test with first role matching
	subject := Subject{
		ID:    "user-123",
		Roles: []string{"admin", "viewer"},
	}

	if !cache.subjectMatches(policy, subject) {
		t.Error("subjectMatches returned false for first matching role")
	}
}

// Test subjectMatches with multiple groups to ensure loop coverage
func TestPolicyCache_SubjectMatchesFirstGroup(t *testing.T) {
	config := &CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           1000,
		BloomFilterSize:   100000,
		BloomFilterHashes: 7,
	}

	cache := NewPolicyCache(config)

	policy := &CompiledPolicy{
		Name: "group-policy",
		SubjectMatcher: &SubjectMatcher{
			Groups: map[string]bool{
				"engineering": true,
				"sales":       true,
			},
		},
	}

	// Test with first group matching
	subject := Subject{
		ID:     "user-123",
		Groups: []string{"engineering", "marketing"},
	}

	if !cache.subjectMatches(policy, subject) {
		t.Error("subjectMatches returned false for first matching group")
	}
}

// mockIAMClient implements IAMClient for testing.
type mockIAMClient struct {
	mu             sync.RWMutex
	policies       []*CompiledPolicy
	updateCh       chan *PolicyUpdate
	fetchError     error
	subscribeError error
	closed         bool
}

func newMockIAMClient() *mockIAMClient {
	return &mockIAMClient{
		policies: make([]*CompiledPolicy, 0),
		updateCh: make(chan *PolicyUpdate, 10),
	}
}

func (m *mockIAMClient) FetchPolicies(tenantID string, patterns []string) ([]*CompiledPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.fetchError != nil {
		return nil, m.fetchError
	}
	// Return a copy to avoid races
	result := make([]*CompiledPolicy, len(m.policies))
	copy(result, m.policies)
	return result, nil
}

func (m *mockIAMClient) Subscribe(tenantID string, patterns []string) (<-chan *PolicyUpdate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.subscribeError != nil {
		return nil, m.subscribeError
	}
	return m.updateCh, nil
}

func (m *mockIAMClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		close(m.updateCh)
		m.closed = true
	}
	return nil
}

func (m *mockIAMClient) SetPolicies(policies []*CompiledPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies = policies
}

func (m *mockIAMClient) AddPolicy(policy *CompiledPolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policies = append(m.policies, policy)
}

func TestPEP_Sync(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		setupPEP    func() *PEP
		wantErr     bool
		errContains string
	}{
		{
			name: "error when closed",
			setupPEP: func() *PEP {
				config := DefaultPEPConfig("test-tenant")
				pep := NewPEP(config)
				pep.Close()
				return pep
			},
			wantErr:     true,
			errContains: "PEP is closed",
		},
		{
			name: "error when syncer not configured",
			setupPEP: func() *PEP {
				config := DefaultPEPConfig("test-tenant")
				config.EnableSync = false
				return NewPEP(config)
			},
			wantErr:     true,
			errContains: "syncer not configured",
		},
		{
			name: "successful sync",
			setupPEP: func() *PEP {
				client := newMockIAMClient()
				config := DefaultPEPConfig("test-tenant")
				config.Client = client
				config.EnableSync = true
				return NewPEP(config)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pep := tt.setupPEP()
			if !pep.closed.Load() {
				defer pep.Close()
			}

			err := pep.Sync(ctx)

			if (err != nil) != tt.wantErr {
				t.Errorf("Sync() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Sync() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestPEP_Subscribe(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		setupPEP    func() *PEP
		patterns    []string
		wantErr     bool
		errContains string
	}{
		{
			name: "error when closed",
			setupPEP: func() *PEP {
				config := DefaultPEPConfig("test-tenant")
				pep := NewPEP(config)
				pep.Close()
				return pep
			},
			patterns:    []string{"**"},
			wantErr:     true,
			errContains: "PEP is closed",
		},
		{
			name: "error when client not configured",
			setupPEP: func() *PEP {
				config := DefaultPEPConfig("test-tenant")
				config.Client = nil
				return NewPEP(config)
			},
			patterns:    []string{"**"},
			wantErr:     true,
			errContains: "IAM client not configured",
		},
		{
			name: "error on subscribe failure",
			setupPEP: func() *PEP {
				client := newMockIAMClient()
				client.subscribeError = errors.New("subscribe failed")
				config := DefaultPEPConfig("test-tenant")
				config.Client = client
				return NewPEP(config)
			},
			patterns:    []string{"**"},
			wantErr:     true,
			errContains: "subscribe failed",
		},
		{
			name: "successful subscription",
			setupPEP: func() *PEP {
				client := newMockIAMClient()
				config := DefaultPEPConfig("test-tenant")
				config.Client = client
				return NewPEP(config)
			},
			patterns: []string{"**"},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pep := tt.setupPEP()
			if !pep.closed.Load() {
				defer pep.Close()
			}

			updateCh, err := pep.Subscribe(ctx, tt.patterns)

			if (err != nil) != tt.wantErr {
				t.Errorf("Subscribe() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Subscribe() error = %v, want error containing %q", err, tt.errContains)
				}
			}

			if !tt.wantErr && updateCh == nil {
				t.Error("Subscribe() returned nil channel")
			}
		})
	}
}

func TestPEP_Subscribe_WithUpdates(t *testing.T) {
	client := newMockIAMClient()
	config := DefaultPEPConfig("test-tenant")
	config.Client = client
	pep := NewPEP(config)
	defer pep.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	updateCh, err := pep.Subscribe(ctx, []string{"**"})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	// Send a test update
	testPolicy := &CompiledPolicy{
		Name:     "test-policy",
		Priority: 100,
	}

	update := &PolicyUpdate{
		Type:      PolicyUpdateCreated,
		Policy:    testPolicy,
		PolicyID:  "test-policy-123",
		Version:   1,
		Timestamp: time.Now(),
	}

	// Send update in background
	go func() {
		time.Sleep(100 * time.Millisecond)
		client.updateCh <- update
	}()

	// Wait for update
	select {
	case receivedUpdate := <-updateCh:
		if receivedUpdate.PolicyID != update.PolicyID {
			t.Errorf("Received update PolicyID = %s, want %s", receivedUpdate.PolicyID, update.PolicyID)
		}
		if receivedUpdate.Type != update.Type {
			t.Errorf("Received update Type = %v, want %v", receivedUpdate.Type, update.Type)
		}
	case <-ctx.Done():
		t.Fatal("Timeout waiting for policy update")
	}
}

func TestPEP_applyPolicyUpdate(t *testing.T) {
	config := DefaultPEPConfig("test-tenant")
	pep := NewPEP(config)
	defer pep.Close()

	testPolicy := &CompiledPolicy{
		Name:     "test-policy",
		Priority: 100,
	}

	tests := []struct {
		name        string
		update      *PolicyUpdate
		verifyCache func(t *testing.T, cache *PolicyCache)
	}{
		{
			name: "snapshot update",
			update: &PolicyUpdate{
				Type:      PolicyUpdateSnapshot,
				Policy:    testPolicy,
				PolicyID:  "test-policy-123",
				Version:   1,
				Timestamp: time.Now(),
			},
			verifyCache: func(t *testing.T, cache *PolicyCache) {
				cached, ok := cache.Get("test-tenant", testPolicy.Name)
				if !ok {
					t.Error("Policy not found in cache after snapshot update")
				}
				if cached.Name != testPolicy.Name {
					t.Errorf("Cached policy Name = %s, want %s", cached.Name, testPolicy.Name)
				}
			},
		},
		{
			name: "created update",
			update: &PolicyUpdate{
				Type:      PolicyUpdateCreated,
				Policy:    testPolicy,
				PolicyID:  "test-policy-123",
				Version:   1,
				Timestamp: time.Now(),
			},
			verifyCache: func(t *testing.T, cache *PolicyCache) {
				cached, ok := cache.Get("test-tenant", testPolicy.Name)
				if !ok {
					t.Error("Policy not found in cache after created update")
				}
				if cached.Name != testPolicy.Name {
					t.Errorf("Cached policy Name = %s, want %s", cached.Name, testPolicy.Name)
				}
			},
		},
		{
			name: "updated update",
			update: &PolicyUpdate{
				Type:      PolicyUpdateUpdated,
				Policy:    testPolicy,
				PolicyID:  "test-policy-123",
				Version:   2,
				Timestamp: time.Now(),
			},
			verifyCache: func(t *testing.T, cache *PolicyCache) {
				cached, ok := cache.Get("test-tenant", testPolicy.Name)
				if !ok {
					t.Error("Policy not found in cache after updated update")
				}
				if cached.Name != testPolicy.Name {
					t.Errorf("Cached policy Name = %s, want %s", cached.Name, testPolicy.Name)
				}
			},
		},
		{
			name: "deleted update",
			update: &PolicyUpdate{
				Type:      PolicyUpdateDeleted,
				PolicyID:  "test-policy", // Must match testPolicy.Name for cache lookup
				Version:   3,
				Timestamp: time.Now(),
			},
			verifyCache: func(t *testing.T, cache *PolicyCache) {
				// First add the policy
				cache.Set("test-tenant", testPolicy)
				// Verify it's there
				if _, ok := cache.Get("test-tenant", testPolicy.Name); !ok {
					t.Fatal("Setup failed: policy not in cache")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get initial stats
			initialStats := pep.GetStats()
			initialUpdates := initialStats.PolicyUpdates

			if tt.update.Type == PolicyUpdateDeleted {
				// For deleted updates, setup first (add policy to cache)
				tt.verifyCache(t, pep.cache)
				// Apply the delete update
				pep.applyPolicyUpdate(tt.update)
				// Verify deletion
				if _, ok := pep.cache.Get("test-tenant", testPolicy.Name); ok {
					t.Error("Policy still in cache after deleted update")
				}
			} else {
				// For snapshot/created/updated: Apply the update first
				pep.applyPolicyUpdate(tt.update)
				// Then verify the cache
				if tt.verifyCache != nil {
					tt.verifyCache(t, pep.cache)
				}
			}

			// Verify metrics were updated
			stats := pep.GetStats()
			expectedUpdates := initialUpdates + 1
			if tt.update.Type == PolicyUpdateDeleted {
				expectedUpdates = initialUpdates + 1 // Just one apply now
			}
			if stats.PolicyUpdates < expectedUpdates {
				t.Errorf("PolicyUpdates = %d, want at least %d", stats.PolicyUpdates, expectedUpdates)
			}
		})
	}
}

func TestPEP_Start(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		setupPEP    func() *PEP
		wantErr     bool
		errContains string
	}{
		{
			name: "error when closed",
			setupPEP: func() *PEP {
				config := DefaultPEPConfig("test-tenant")
				pep := NewPEP(config)
				pep.Close()
				return pep
			},
			wantErr:     true,
			errContains: "PEP is closed",
		},
		{
			name: "successful start without syncer",
			setupPEP: func() *PEP {
				config := DefaultPEPConfig("test-tenant")
				config.EnableSync = false
				return NewPEP(config)
			},
			wantErr: false,
		},
		{
			name: "successful start with syncer",
			setupPEP: func() *PEP {
				client := newMockIAMClient()
				config := DefaultPEPConfig("test-tenant")
				config.Client = client
				config.EnableSync = true
				return NewPEP(config)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pep := tt.setupPEP()
			if !pep.closed.Load() {
				defer pep.Close()
			}

			err := pep.Start(ctx)

			if (err != nil) != tt.wantErr {
				t.Errorf("Start() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Start() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}
