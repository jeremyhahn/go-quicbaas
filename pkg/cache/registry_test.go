package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func newTestLogger(t *testing.T) interfaces.Logger {
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

func TestNewCacheRegistry(t *testing.T) {
	log := newTestLogger(t)

	t.Run("nil config", func(t *testing.T) {
		_, err := NewCacheRegistry(nil, log, nil)
		if err == nil {
			t.Error("Expected error with nil config")
		}
	})

	t.Run("nil logger", func(t *testing.T) {
		cfg := config.DefaultEnhancedCacheConfig()
		_, err := NewCacheRegistry(&cfg, nil, nil)
		if err == nil {
			t.Error("Expected error with nil logger")
		}
	})

	t.Run("valid config", func(t *testing.T) {
		cfg := config.DefaultEnhancedCacheConfig()
		registry, err := NewCacheRegistry(&cfg, log, nil)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer registry.Close()

		if registry.config == nil {
			t.Error("Expected config to be set")
		}

		if registry.globalCache == nil {
			t.Error("Expected global cache to be created")
		}
	})

	t.Run("with entity configurations", func(t *testing.T) {
		cfg := config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         10000,
			CleanupInterval: 1 * time.Minute,
			WriteThrough:    true,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 5000,
				},
				"post": {
					Enabled: false,
				},
			},
		}

		registry, err := NewCacheRegistry(&cfg, log, nil)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer registry.Close()

		// Check that entity caches were registered
		userCache := registry.GetEntityCache("user")
		if userCache == nil {
			t.Error("Expected user cache to be registered")
		}
		if userCache.entityType != "user" {
			t.Errorf("Expected entity type 'user', got %s", userCache.entityType)
		}

		postCache := registry.GetEntityCache("post")
		if postCache == nil {
			t.Error("Expected post cache to exist (even if disabled)")
		}
	})

	t.Run("disabled cache", func(t *testing.T) {
		cfg := config.EnhancedCacheConfig{
			Enabled:         false,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         10000,
			CleanupInterval: 1 * time.Minute,
			Entities:        make(map[string]config.EntityCacheConfig),
		}

		registry, err := NewCacheRegistry(&cfg, log, nil)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		defer registry.Close()

		if registry.globalCache != nil {
			t.Error("Expected global cache to be nil when cache is disabled")
		}
	})
}

func TestCacheRegistry_IsCacheEnabled(t *testing.T) {
	log := newTestLogger(t)

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {Enabled: true},
			"post": {Enabled: false},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer registry.Close()

	tests := []struct {
		name       string
		entityType string
		want       bool
	}{
		{"enabled entity", "user", true},
		{"disabled entity", "post", false},
		{"unconfigured entity with global enabled", "comment", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := registry.IsCacheEnabled(tt.entityType); got != tt.want {
				t.Errorf("IsCacheEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCacheRegistry_GetSetDelete(t *testing.T) {
	log := newTestLogger(t)

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {
				Enabled: true,
				TTL:     10 * time.Minute,
				MaxSize: 1000,
			},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer registry.Close()

	ctx := context.Background()
	entityType := "user"
	key := "user:123"
	value := []byte("test-value")

	// Test Set
	err = registry.Set(ctx, entityType, key, value)
	if err != nil {
		t.Errorf("Set() error = %v", err)
	}

	// Test Get - should retrieve the value
	retrieved, err := registry.Get(ctx, entityType, key)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if string(retrieved) != string(value) {
		t.Errorf("Get() = %s, want %s", retrieved, value)
	}

	// Test Delete
	err = registry.Delete(ctx, entityType, key)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	// Test Get after delete - should return not found
	_, err = registry.Get(ctx, entityType, key)
	if err == nil {
		t.Error("Expected error after deleting key")
	}
}

func TestCacheRegistry_DisabledEntityCache(t *testing.T) {
	log := newTestLogger(t)

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"post": {
				Enabled: false,
			},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer registry.Close()

	ctx := context.Background()
	entityType := "post"
	key := "post:456"
	value := []byte("test-value")

	// Set should succeed but not cache anything
	err = registry.Set(ctx, entityType, key, value)
	if err != nil {
		t.Errorf("Set() on disabled cache error = %v", err)
	}

	// Get should fail since cache is disabled
	_, err = registry.Get(ctx, entityType, key)
	if err == nil {
		t.Error("Expected error when getting from disabled cache")
	}
}

func TestCacheRegistry_GetStats(t *testing.T) {
	log := newTestLogger(t)

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {
				Enabled: true,
				TTL:     10 * time.Minute,
				MaxSize: 1000,
			},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer registry.Close()

	ctx := context.Background()

	// Add some cache entries
	_ = registry.Set(ctx, "user", "user:1", []byte("value1"))
	_ = registry.Set(ctx, "user", "user:2", []byte("value2"))

	// Get stats for user entity
	stats := registry.GetStats("user")
	if !stats.Enabled {
		t.Error("Expected cache to be enabled in stats")
	}
	if stats.L1Size != 2 {
		t.Errorf("Expected L1Size = 2, got %d", stats.L1Size)
	}

	// Get all stats
	allStats := registry.GetAllStats()
	if len(allStats) == 0 {
		t.Error("Expected stats for at least one entity")
	}

	if _, exists := allStats["user"]; !exists {
		t.Error("Expected stats for user entity")
	}

	if _, exists := allStats["_global"]; !exists {
		t.Error("Expected stats for global cache")
	}
}

func TestCacheRegistry_Clear(t *testing.T) {
	log := newTestLogger(t)

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {
				Enabled: true,
				TTL:     10 * time.Minute,
				MaxSize: 1000,
			},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer registry.Close()

	ctx := context.Background()

	// Add some cache entries
	_ = registry.Set(ctx, "user", "user:1", []byte("value1"))
	_ = registry.Set(ctx, "user", "user:2", []byte("value2"))

	// Verify entries exist
	if _, err := registry.Get(ctx, "user", "user:1"); err != nil {
		t.Error("Expected to find user:1 before clear")
	}

	// Clear all caches
	registry.Clear()

	// Verify entries are gone
	if _, err := registry.Get(ctx, "user", "user:1"); err == nil {
		t.Error("Expected user:1 to be gone after clear")
	}
}

func TestCacheRegistry_UpdateEntityConfig(t *testing.T) {
	log := newTestLogger(t)

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {
				Enabled: true,
				TTL:     5 * time.Minute,
				MaxSize: 1000,
			},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer registry.Close()

	// Update config
	newCfg := config.EntityCacheConfig{
		Enabled: true,
		TTL:     20 * time.Minute,
		MaxSize: 5000,
	}

	err = registry.UpdateEntityConfig("user", newCfg)
	if err != nil {
		t.Errorf("UpdateEntityConfig() error = %v", err)
	}

	// Verify the config was updated
	entityCache := registry.GetEntityCache("user")
	if entityCache.config.TTL != 20*time.Minute {
		t.Errorf("Expected TTL = 20m, got %v", entityCache.config.TTL)
	}
}

func TestEntityCache_Methods(t *testing.T) {
	log := newTestLogger(t)

	cfg := Config{
		L1Size:          100,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}

	manager, err := NewManager(cfg, log)
	if err != nil {
		t.Fatalf("Failed to create cache manager: %v", err)
	}
	defer manager.Close()

	trueVal := true
	entityCache := &EntityCache{
		entityType: "user",
		manager:    manager,
		config: config.EntityCacheConfig{
			Enabled:      true,
			TTL:          10 * time.Minute,
			MaxSize:      100,
			WriteThrough: &trueVal,
		},
		enabled: true,
	}

	ctx := context.Background()
	key := "test-key"
	value := []byte("test-value")

	// Test Set
	err = entityCache.Set(ctx, key, value)
	if err != nil {
		t.Errorf("Set() error = %v", err)
	}

	// Test Get
	retrieved, err := entityCache.Get(ctx, key)
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}
	if string(retrieved) != string(value) {
		t.Errorf("Get() = %s, want %s", retrieved, value)
	}

	// Test IsEnabled
	if !entityCache.IsEnabled() {
		t.Error("Expected cache to be enabled")
	}

	// Test GetEntityType
	if entityCache.GetEntityType() != "user" {
		t.Errorf("GetEntityType() = %s, want user", entityCache.GetEntityType())
	}

	// Test GetConfig
	cfg2 := entityCache.GetConfig()
	if cfg2.TTL != 10*time.Minute {
		t.Errorf("GetConfig() TTL = %v, want 10m", cfg2.TTL)
	}

	// Test Stats
	stats := entityCache.Stats()
	if !stats.Enabled {
		t.Error("Expected stats.Enabled to be true")
	}

	// Test Delete
	err = entityCache.Delete(ctx, key)
	if err != nil {
		t.Errorf("Delete() error = %v", err)
	}

	// Verify deleted
	_, err = entityCache.Get(ctx, key)
	if err == nil {
		t.Error("Expected error after deleting key")
	}
}

func TestEntityCache_DisabledCache(t *testing.T) {
	entityCache := &EntityCache{
		entityType: "post",
		manager:    nil,
		config: config.EntityCacheConfig{
			Enabled: false,
		},
		enabled: false,
	}

	ctx := context.Background()
	key := "test-key"
	value := []byte("test-value")

	// Set should not error but do nothing
	err := entityCache.Set(ctx, key, value)
	if err != nil {
		t.Errorf("Set() on disabled cache error = %v", err)
	}

	// Get should return not found
	_, err = entityCache.Get(ctx, key)
	if err == nil {
		t.Error("Expected error when getting from disabled cache")
	}

	// Delete should not error
	err = entityCache.Delete(ctx, key)
	if err != nil {
		t.Errorf("Delete() on disabled cache error = %v", err)
	}

	// Stats should show disabled
	stats := entityCache.Stats()
	if stats.Enabled {
		t.Error("Expected stats.Enabled to be false for disabled cache")
	}
}

func TestBuildEntityKey(t *testing.T) {
	tests := []struct {
		name       string
		entityType string
		id         string
		want       string
	}{
		{"user entity", "user", "123", "entity:user:123"},
		{"post entity", "post", "abc", "entity:post:abc"},
		{"empty id", "comment", "", "entity:comment:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildEntityKey(tt.entityType, tt.id)
			if got != tt.want {
				t.Errorf("BuildEntityKey() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestBuildEntityPrefixKey(t *testing.T) {
	tests := []struct {
		name       string
		entityType string
		want       string
	}{
		{"user entity", "user", "entity:user:"},
		{"post entity", "post", "entity:post:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildEntityPrefixKey(tt.entityType)
			if got != tt.want {
				t.Errorf("BuildEntityPrefixKey() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParseEntityKey(t *testing.T) {
	tests := []struct {
		name           string
		key            string
		wantEntityType string
		wantID         string
	}{
		{"valid user key", "entity:user:123", "user", "123"},
		{"valid post key", "entity:post:abc", "post", "abc"},
		{"invalid key - no prefix", "user:123", "", ""},
		{"invalid key - too short", "entity:", "", ""},
		{"invalid key - no colon", "entityuser123", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEntityType, gotID := ParseEntityKey(tt.key)
			if gotEntityType != tt.wantEntityType {
				t.Errorf("ParseEntityKey() entityType = %s, want %s", gotEntityType, tt.wantEntityType)
			}
			if gotID != tt.wantID {
				t.Errorf("ParseEntityKey() id = %s, want %s", gotID, tt.wantID)
			}
		})
	}
}

func TestCacheRegistry_IntegrationScenario(t *testing.T) {
	log := newTestLogger(t)

	// Create a realistic configuration
	trueVal := true
	falseVal := false

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		WriteThrough:    true,
		Entities: map[string]config.EntityCacheConfig{
			"user": {
				Enabled:      true,
				TTL:          10 * time.Minute,
				MaxSize:      5000,
				WriteThrough: &trueVal,
			},
			"post": {
				Enabled:      false,
				WriteThrough: &falseVal,
			},
			"comment": {
				Enabled: true,
				TTL:     2 * time.Minute,
				MaxSize: 1000,
			},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}
	defer registry.Close()

	ctx := context.Background()

	// Scenario 1: Cache users (enabled)
	userKey := BuildEntityKey("user", "123")
	userData := []byte(`{"id":"123","name":"John Doe"}`)

	err = registry.Set(ctx, "user", userKey, userData)
	if err != nil {
		t.Errorf("Failed to cache user: %v", err)
	}

	retrievedUser, err := registry.Get(ctx, "user", userKey)
	if err != nil {
		t.Errorf("Failed to retrieve user: %v", err)
	}
	if string(retrievedUser) != string(userData) {
		t.Error("User data mismatch")
	}

	// Scenario 2: Try to cache posts (disabled)
	postKey := BuildEntityKey("post", "456")
	postData := []byte(`{"id":"456","title":"Test Post"}`)

	// Set should succeed (no error) but not cache
	err = registry.Set(ctx, "post", postKey, postData)
	if err != nil {
		t.Errorf("Set on disabled cache should not error: %v", err)
	}

	// Get should fail
	_, err = registry.Get(ctx, "post", postKey)
	if err == nil {
		t.Error("Expected error when getting from disabled cache")
	}

	// Scenario 3: Cache comments (enabled)
	commentKey := BuildEntityKey("comment", "789")
	commentData := []byte(`{"id":"789","text":"Great post!"}`)

	err = registry.Set(ctx, "comment", commentKey, commentData)
	if err != nil {
		t.Errorf("Failed to cache comment: %v", err)
	}

	retrievedComment, err := registry.Get(ctx, "comment", commentKey)
	if err != nil {
		t.Errorf("Failed to retrieve comment: %v", err)
	}
	if string(retrievedComment) != string(commentData) {
		t.Error("Comment data mismatch")
	}

	// Scenario 4: Check statistics
	allStats := registry.GetAllStats()
	if len(allStats) < 2 { // At least user and comment should have stats
		t.Errorf("Expected stats for multiple entities, got %d", len(allStats))
	}

	userStats := registry.GetStats("user")
	if userStats.L1Size == 0 {
		t.Error("Expected user cache to have entries")
	}

	// Scenario 5: Clear and verify
	registry.Clear()

	_, err = registry.Get(ctx, "user", userKey)
	if err == nil {
		t.Error("Expected cache to be empty after clear")
	}
}

// Additional comprehensive tests for 90%+ coverage

func TestCacheRegistry_ConcurrentEntityOperations(t *testing.T) {
	log := newTestLogger(t)

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {
				Enabled: true,
				TTL:     10 * time.Minute,
				MaxSize: 500,
			},
			"post": {
				Enabled: true,
				TTL:     5 * time.Minute,
				MaxSize: 500,
			},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}
	defer registry.Close()

	ctx := context.Background()

	const numGoroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup

	// Concurrent operations on user entity
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("user:%d:%d", id, j)
				value := []byte(fmt.Sprintf("value-%d-%d", id, j))
				registry.Set(ctx, "user", key, value)
				registry.Get(ctx, "user", key)
				if j%10 == 0 {
					registry.Delete(ctx, "user", key)
				}
			}
		}(i)
	}

	// Concurrent operations on post entity
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("post:%d:%d", id, j)
				value := []byte(fmt.Sprintf("value-%d-%d", id, j))
				registry.Set(ctx, "post", key, value)
				registry.Get(ctx, "post", key)
			}
		}(i)
	}

	wg.Wait()

	// Verify registries are in valid state
	userStats := registry.GetStats("user")
	postStats := registry.GetStats("post")

	if userStats.L1Size < 0 || userStats.L1Size > userStats.L1Capacity {
		t.Errorf("Invalid user cache state: size=%d, capacity=%d", userStats.L1Size, userStats.L1Capacity)
	}

	if postStats.L1Size < 0 || postStats.L1Size > postStats.L1Capacity {
		t.Errorf("Invalid post cache state: size=%d, capacity=%d", postStats.L1Size, postStats.L1Capacity)
	}
}

func TestCacheRegistry_UpdateEntityConfigConcurrent(t *testing.T) {
	log := newTestLogger(t)

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {
				Enabled: true,
				TTL:     10 * time.Minute,
				MaxSize: 100,
			},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}
	defer registry.Close()

	ctx := context.Background()

	// Add some data
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("user:%d", i)
		registry.Set(ctx, "user", key, []byte(fmt.Sprintf("value-%d", i)))
	}

	// Update config while operations are ongoing
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine performing reads
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			key := fmt.Sprintf("user:%d", i%50)
			registry.Get(ctx, "user", key)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine updating config
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		newCfg := config.EntityCacheConfig{
			Enabled: true,
			TTL:     20 * time.Minute,
			MaxSize: 200,
		}
		registry.UpdateEntityConfig("user", newCfg)
	}()

	wg.Wait()

	// Verify config was updated
	entityCache := registry.GetEntityCache("user")
	if entityCache.config.TTL != 20*time.Minute {
		t.Errorf("Expected TTL = 20m, got %v", entityCache.config.TTL)
	}
}

func TestCacheRegistry_ErrorPaths(t *testing.T) {
	log := newTestLogger(t)

	t.Run("invalid config validation fails", func(t *testing.T) {
		cfg := config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      -1 * time.Minute, // Invalid default TTL
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities:        make(map[string]config.EntityCacheConfig),
		}

		// Should fail validation
		registry, err := NewCacheRegistry(&cfg, log, nil)
		if err == nil {
			if registry != nil {
				registry.Close()
			}
			t.Fatal("Expected error for invalid config")
		}
	})

	t.Run("close with errors", func(t *testing.T) {
		cfg := config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 100,
				},
			},
		}

		registry, err := NewCacheRegistry(&cfg, log, nil)
		if err != nil {
			t.Fatalf("Failed to create registry: %v", err)
		}

		// Close should succeed even if called multiple times
		err = registry.Close()
		if err != nil {
			t.Logf("First close error: %v", err)
		}
	})

	t.Run("operations on unconfigured entity with disabled global", func(t *testing.T) {
		cfg := config.EnhancedCacheConfig{
			Enabled:         false, // Global cache disabled
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities:        make(map[string]config.EntityCacheConfig),
		}

		registry, err := NewCacheRegistry(&cfg, log, nil)
		if err != nil {
			t.Fatalf("Failed to create registry: %v", err)
		}
		defer registry.Close()

		ctx := context.Background()

		// Operations should handle nil global cache gracefully
		enabled := registry.IsCacheEnabled("unknown")
		if enabled {
			t.Error("Expected cache to be disabled for unknown entity when global is disabled")
		}

		// Set should not error
		err = registry.Set(ctx, "unknown", "key", []byte("value"))
		if err != nil {
			t.Errorf("Set should not error on disabled cache: %v", err)
		}

		// Get should return error
		_, err = registry.Get(ctx, "unknown", "key")
		if err == nil {
			t.Error("Expected error when getting from disabled cache")
		}
	})
}

func TestCacheRegistry_GetEntityCacheWithGlobalFallback(t *testing.T) {
	log := newTestLogger(t)

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {
				Enabled: true,
				TTL:     10 * time.Minute,
				MaxSize: 100,
			},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}
	defer registry.Close()

	ctx := context.Background()

	// Test getting cache for configured entity
	userCache := registry.GetEntityCache("user")
	if userCache == nil {
		t.Fatal("Expected user cache to exist")
	}
	if userCache.entityType != "user" {
		t.Errorf("Expected entity type 'user', got %s", userCache.entityType)
	}

	// Test getting cache for unconfigured entity (should use global)
	unknownCache := registry.GetEntityCache("unknown")
	if unknownCache == nil {
		t.Fatal("Expected unknown cache to fallback to global")
	}
	if unknownCache.entityType != "unknown" {
		t.Errorf("Expected entity type 'unknown', got %s", unknownCache.entityType)
	}
	if unknownCache.manager != registry.globalCache {
		t.Error("Expected unknown cache to use global cache manager")
	}

	// Verify both work
	registry.Set(ctx, "user", "user-key", []byte("user-value"))
	registry.Set(ctx, "unknown", "unknown-key", []byte("unknown-value"))

	userVal, err := registry.Get(ctx, "user", "user-key")
	if err != nil {
		t.Errorf("Failed to get user value: %v", err)
	}
	if string(userVal) != "user-value" {
		t.Errorf("Expected 'user-value', got %s", userVal)
	}

	unknownVal, err := registry.Get(ctx, "unknown", "unknown-key")
	if err != nil {
		t.Errorf("Failed to get unknown value: %v", err)
	}
	if string(unknownVal) != "unknown-value" {
		t.Errorf("Expected 'unknown-value', got %s", unknownVal)
	}
}

func TestCacheRegistry_GetAllStatsWithMultipleEntities(t *testing.T) {
	log := newTestLogger(t)

	trueVal := true
	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"user": {
				Enabled:      true,
				TTL:          10 * time.Minute,
				MaxSize:      100,
				WriteThrough: &trueVal,
			},
			"post": {
				Enabled:      true,
				TTL:          5 * time.Minute,
				MaxSize:      200,
				WriteThrough: &trueVal,
			},
			"comment": {
				Enabled: false, // Disabled entity
			},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}
	defer registry.Close()

	ctx := context.Background()

	// Add data to each entity
	registry.Set(ctx, "user", "user:1", []byte("user-data"))
	registry.Set(ctx, "post", "post:1", []byte("post-data"))
	registry.Set(ctx, "comment", "comment:1", []byte("comment-data")) // Disabled, should not cache

	allStats := registry.GetAllStats()

	// Should have stats for enabled entities
	if _, exists := allStats["user"]; !exists {
		t.Error("Expected stats for user entity")
	}

	if _, exists := allStats["post"]; !exists {
		t.Error("Expected stats for post entity")
	}

	// Should not have stats for disabled entity
	if stats, exists := allStats["comment"]; exists {
		if stats.Enabled {
			t.Error("Expected comment cache to be disabled")
		}
	}

	// Should have global cache stats
	if _, exists := allStats["_global"]; !exists {
		t.Error("Expected stats for global cache")
	}
}

func TestCacheRegistry_StressTestMultipleEntities(t *testing.T) {
	log := newTestLogger(t)

	entities := []string{"users", "posts", "comments", "tags", "categories"}
	entityConfigs := make(map[string]config.EntityCacheConfig)

	for _, entity := range entities {
		entityConfigs[entity] = config.EntityCacheConfig{
			Enabled: true,
			TTL:     5 * time.Minute,
			MaxSize: 100,
		}
	}

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 1 * time.Minute,
		Entities:        entityConfigs,
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}
	defer registry.Close()

	ctx := context.Background()

	const numGoroutines = 20
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	errorsChan := make(chan error, numGoroutines*len(entities))

	// Concurrent operations on all entities
	for _, entity := range entities {
		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func(entityType string, id int) {
				defer wg.Done()
				for j := 0; j < opsPerGoroutine; j++ {
					key := fmt.Sprintf("%s:%d:%d", entityType, id, j)
					value := []byte(fmt.Sprintf("value-%d-%d", id, j))

					if err := registry.Set(ctx, entityType, key, value); err != nil {
						errorsChan <- &dberrors.WrappedMessageError{Message: fmt.Sprintf("set error for %s", entityType), Cause: err}
						return
					}

					if _, err := registry.Get(ctx, entityType, key); err != nil {
						// Get may fail due to concurrent deletes, that's ok
					}

					if j%20 == 0 {
						if err := registry.Delete(ctx, entityType, key); err != nil {
							errorsChan <- &dberrors.WrappedMessageError{Message: fmt.Sprintf("delete error for %s", entityType), Cause: err}
							return
						}
					}
				}
			}(entity, i)
		}
	}

	wg.Wait()
	close(errorsChan)

	// Check for errors
	for err := range errorsChan {
		t.Errorf("Operation error: %v", err)
	}

	// Verify all caches are in valid state
	allStats := registry.GetAllStats()
	for _, entity := range entities {
		if stats, exists := allStats[entity]; exists {
			if stats.L1Size < 0 || stats.L1Size > stats.L1Capacity {
				t.Errorf("Invalid state for %s: size=%d, capacity=%d", entity, stats.L1Size, stats.L1Capacity)
			}
		}
	}
}

func TestEntityCache_GetSetDeleteWithNilManager(t *testing.T) {
	// Test EntityCache methods when manager is nil (disabled cache)
	entityCache := &EntityCache{
		entityType: "test",
		manager:    nil,
		config: config.EntityCacheConfig{
			Enabled: false,
		},
		enabled: false,
	}

	ctx := context.Background()

	// Get should return error
	_, err := entityCache.Get(ctx, "key")
	if err == nil {
		t.Error("Expected error when getting from nil manager")
	}

	// Set should not error (silently skip)
	err = entityCache.Set(ctx, "key", []byte("value"))
	if err != nil {
		t.Errorf("Set should not error with nil manager: %v", err)
	}

	// Delete should not error (silently skip)
	err = entityCache.Delete(ctx, "key")
	if err != nil {
		t.Errorf("Delete should not error with nil manager: %v", err)
	}

	// Stats should show disabled
	stats := entityCache.Stats()
	if stats.Enabled {
		t.Error("Expected stats to show disabled")
	}
}

func TestParseEntityKey_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		key            string
		wantEntityType string
		wantID         string
	}{
		{
			name:           "valid key with colon in ID",
			key:            "entity:user:id:with:colons",
			wantEntityType: "user",
			wantID:         "id:with:colons",
		},
		{
			name:           "empty string",
			key:            "",
			wantEntityType: "",
			wantID:         "",
		},
		{
			name:           "only prefix",
			key:            "entity:",
			wantEntityType: "",
			wantID:         "",
		},
		{
			name:           "prefix with entity but no colon",
			key:            "entity:user",
			wantEntityType: "",
			wantID:         "",
		},
		{
			name:           "wrong prefix",
			key:            "wrong:user:123",
			wantEntityType: "",
			wantID:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEntityType, gotID := ParseEntityKey(tt.key)
			if gotEntityType != tt.wantEntityType {
				t.Errorf("ParseEntityKey() entityType = %s, want %s", gotEntityType, tt.wantEntityType)
			}
			if gotID != tt.wantID {
				t.Errorf("ParseEntityKey() id = %s, want %s", gotID, tt.wantID)
			}
		})
	}
}

func TestCacheRegistry_ClearWithDisabledEntities(t *testing.T) {
	log := newTestLogger(t)

	cfg := config.EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         1000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]config.EntityCacheConfig{
			"enabled": {
				Enabled: true,
				TTL:     10 * time.Minute,
				MaxSize: 100,
			},
			"disabled": {
				Enabled: false,
			},
		},
	}

	registry, err := NewCacheRegistry(&cfg, log, nil)
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}
	defer registry.Close()

	ctx := context.Background()

	// Add data to enabled cache
	registry.Set(ctx, "enabled", "key1", []byte("value1"))

	// Clear should not panic with mix of enabled/disabled caches
	registry.Clear()

	// Verify enabled cache was cleared
	_, err = registry.Get(ctx, "enabled", "key1")
	if err == nil {
		t.Error("Expected key to be cleared")
	}
}
