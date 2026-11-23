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

	// Delete should succeed but do nothing
	if err := registry.Delete(ctx, entityType, key); err != nil {
		t.Errorf("Delete() on disabled cache error = %v", err)
	}

	// Stats should show disabled
	if registry.GetStats(entityType).Enabled {
		t.Error("Expected stats.Enabled to be false for disabled cache")
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
			"comment": {
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

	// Disabled entities are listed, marked disabled
	if stats, exists := allStats["comment"]; !exists || stats.Enabled {
		t.Errorf("Expected disabled stats for comment entity, got %+v (exists=%v)", stats, exists)
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
		{"valid key with colon in ID", "entity:user:id:with:colons", "user", "id:with:colons"},
		{"empty string", "", "", ""},
		{"prefix with entity but no second colon", "entity:user", "", ""},
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

// Additional comprehensive tests for 90%+ coverage

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
		}
	}()

	// Goroutine updating config
	go func() {
		defer wg.Done()
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
