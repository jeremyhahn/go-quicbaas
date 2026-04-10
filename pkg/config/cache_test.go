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

package config

import (
	"testing"
	"time"
)

func TestDefaultEnhancedCacheConfig(t *testing.T) {
	cfg := DefaultEnhancedCacheConfig()

	if !cfg.Enabled {
		t.Error("Expected cache to be enabled by default")
	}

	if cfg.DefaultTTL != 5*time.Minute {
		t.Errorf("Expected default TTL of 5m, got %v", cfg.DefaultTTL)
	}

	if cfg.MaxSize != 10000 {
		t.Errorf("Expected max size of 10000, got %d", cfg.MaxSize)
	}

	if cfg.CleanupInterval != 1*time.Minute {
		t.Errorf("Expected cleanup interval of 1m, got %v", cfg.CleanupInterval)
	}

	if !cfg.WriteThrough {
		t.Error("Expected write-through to be enabled by default")
	}

	if cfg.L2Enabled {
		t.Error("Expected L2 cache to be disabled by default")
	}

	if cfg.Entities == nil {
		t.Error("Expected entities map to be initialized")
	}

	// Test new secrets-related fields
	if cfg.L2RedisUseSecrets {
		t.Error("Expected L2RedisUseSecrets to be false by default")
	}

	if cfg.L2RedisSecretPath != InternalSecretRedisPassword {
		t.Errorf("Expected L2RedisSecretPath to be %q, got %q", InternalSecretRedisPassword, cfg.L2RedisSecretPath)
	}
}

func TestEnhancedCacheConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  EnhancedCacheConfig
		wantErr bool
	}{
		{
			name:    "valid default config",
			config:  DefaultEnhancedCacheConfig(),
			wantErr: false,
		},
		{
			name: "negative max size",
			config: EnhancedCacheConfig{
				Enabled:         true,
				DefaultTTL:      5 * time.Minute,
				MaxSize:         -100,
				CleanupInterval: 1 * time.Minute,
				Entities:        make(map[string]EntityCacheConfig),
			},
			wantErr: true,
		},
		{
			name: "negative default TTL",
			config: EnhancedCacheConfig{
				Enabled:         true,
				DefaultTTL:      -5 * time.Minute,
				MaxSize:         1000,
				CleanupInterval: 1 * time.Minute,
				Entities:        make(map[string]EntityCacheConfig),
			},
			wantErr: true,
		},
		{
			name: "zero cleanup interval",
			config: EnhancedCacheConfig{
				Enabled:         true,
				DefaultTTL:      5 * time.Minute,
				MaxSize:         1000,
				CleanupInterval: 0,
				Entities:        make(map[string]EntityCacheConfig),
			},
			wantErr: true,
		},
		{
			name: "L2 enabled without redis address",
			config: EnhancedCacheConfig{
				Enabled:         true,
				DefaultTTL:      5 * time.Minute,
				MaxSize:         1000,
				CleanupInterval: 1 * time.Minute,
				L2Enabled:       true,
				L2RedisAddr:     "",
				Entities:        make(map[string]EntityCacheConfig),
			},
			wantErr: true,
		},
		{
			name: "L2 enabled with redis address",
			config: EnhancedCacheConfig{
				Enabled:         true,
				DefaultTTL:      5 * time.Minute,
				MaxSize:         1000,
				CleanupInterval: 1 * time.Minute,
				L2Enabled:       true,
				L2RedisAddr:     "localhost:6379",
				L2TTL:           30 * time.Minute,
				Entities:        make(map[string]EntityCacheConfig),
			},
			wantErr: false,
		},
		{
			name: "L2 enabled with secrets - valid",
			config: EnhancedCacheConfig{
				Enabled:           true,
				DefaultTTL:        5 * time.Minute,
				MaxSize:           1000,
				CleanupInterval:   1 * time.Minute,
				L2Enabled:         true,
				L2RedisAddr:       "localhost:6379",
				L2TTL:             30 * time.Minute,
				L2RedisUseSecrets: true,
				L2RedisSecretPath: "cache/redis/password",
				Entities:          make(map[string]EntityCacheConfig),
			},
			wantErr: false,
		},
		{
			name: "L2 enabled with secrets - empty secret path",
			config: EnhancedCacheConfig{
				Enabled:           true,
				DefaultTTL:        5 * time.Minute,
				MaxSize:           1000,
				CleanupInterval:   1 * time.Minute,
				L2Enabled:         true,
				L2RedisAddr:       "localhost:6379",
				L2TTL:             30 * time.Minute,
				L2RedisUseSecrets: true,
				L2RedisSecretPath: "",
				Entities:          make(map[string]EntityCacheConfig),
			},
			wantErr: true,
		},
		{
			name: "L2 enabled with secrets and plain password - valid but warns",
			config: EnhancedCacheConfig{
				Enabled:           true,
				DefaultTTL:        5 * time.Minute,
				MaxSize:           1000,
				CleanupInterval:   1 * time.Minute,
				L2Enabled:         true,
				L2RedisAddr:       "localhost:6379",
				L2TTL:             30 * time.Minute,
				L2RedisUseSecrets: true,
				L2RedisSecretPath: "cache/redis/password",
				L2RedisPassword:   "plaintext",
				Entities:          make(map[string]EntityCacheConfig),
			},
			wantErr: false, // Valid but plain password is ignored
		},
		{
			name: "invalid entity config - negative TTL",
			config: EnhancedCacheConfig{
				Enabled:         true,
				DefaultTTL:      5 * time.Minute,
				MaxSize:         1000,
				CleanupInterval: 1 * time.Minute,
				Entities: map[string]EntityCacheConfig{
					"user": {
						Enabled: true,
						TTL:     -10 * time.Minute,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid entity config - negative max size",
			config: EnhancedCacheConfig{
				Enabled:         true,
				DefaultTTL:      5 * time.Minute,
				MaxSize:         1000,
				CleanupInterval: 1 * time.Minute,
				Entities: map[string]EntityCacheConfig{
					"post": {
						Enabled: true,
						MaxSize: -100,
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEnhancedCacheConfig_GetEntityConfig(t *testing.T) {
	trueVal := true
	falseVal := false

	cfg := EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		WriteThrough:    true,
		Entities: map[string]EntityCacheConfig{
			"user": {
				Enabled:      true,
				TTL:          10 * time.Minute,
				MaxSize:      5000,
				WriteThrough: &trueVal,
			},
			"post": {
				Enabled:      false,
				TTL:          0, // Will use default
				MaxSize:      0, // Will use proportional
				WriteThrough: &falseVal,
			},
			"comment": {
				Enabled: true,
				TTL:     2 * time.Minute,
				MaxSize: 1000,
				// WriteThrough not set, will use default
			},
		},
	}

	tests := []struct {
		name          string
		entityType    string
		expectEnabled bool
		expectTTL     time.Duration
		expectMaxSize int
		expectWT      bool
	}{
		{
			name:          "user entity - fully configured",
			entityType:    "user",
			expectEnabled: true,
			expectTTL:     10 * time.Minute,
			expectMaxSize: 5000,
			expectWT:      true,
		},
		{
			name:          "post entity - disabled with defaults",
			entityType:    "post",
			expectEnabled: false,
			expectTTL:     5 * time.Minute, // Uses default
			expectMaxSize: 0,               // Will be 0 since not set
			expectWT:      false,
		},
		{
			name:          "comment entity - partial config",
			entityType:    "comment",
			expectEnabled: true,
			expectTTL:     2 * time.Minute,
			expectMaxSize: 1000,
			expectWT:      true, // Uses default
		},
		{
			name:          "unconfigured entity",
			entityType:    "tag",
			expectEnabled: true,            // Uses global default
			expectTTL:     5 * time.Minute, // Uses default
			expectMaxSize: 0,               // Will be 0 (proportional)
			expectWT:      true,            // Uses default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entityCfg := cfg.GetEntityConfig(tt.entityType)

			if entityCfg.Enabled != tt.expectEnabled {
				t.Errorf("Expected enabled=%v, got %v", tt.expectEnabled, entityCfg.Enabled)
			}

			if entityCfg.TTL != tt.expectTTL {
				t.Errorf("Expected TTL=%v, got %v", tt.expectTTL, entityCfg.TTL)
			}

			if entityCfg.MaxSize != tt.expectMaxSize {
				t.Errorf("Expected MaxSize=%d, got %d", tt.expectMaxSize, entityCfg.MaxSize)
			}

			if entityCfg.WriteThrough != nil && *entityCfg.WriteThrough != tt.expectWT {
				t.Errorf("Expected WriteThrough=%v, got %v", tt.expectWT, *entityCfg.WriteThrough)
			}
		})
	}
}

func TestEnhancedCacheConfig_IsEntityCacheEnabled(t *testing.T) {
	cfg := EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		WriteThrough:    true,
		Entities: map[string]EntityCacheConfig{
			"user": {Enabled: true},
			"post": {Enabled: false},
		},
	}

	tests := []struct {
		name       string
		entityType string
		want       bool
	}{
		{"enabled entity", "user", true},
		{"disabled entity", "post", false},
		{"unconfigured entity - global enabled", "comment", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.IsEntityCacheEnabled(tt.entityType); got != tt.want {
				t.Errorf("IsEntityCacheEnabled() = %v, want %v", got, tt.want)
			}
		})
	}

	// Test with globally disabled cache
	cfg.Enabled = false
	if cfg.IsEntityCacheEnabled("user") {
		t.Error("Expected cache to be disabled when globally disabled")
	}
}

func TestEnhancedCacheConfig_GetEntityTTL(t *testing.T) {
	cfg := EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]EntityCacheConfig{
			"user": {Enabled: true, TTL: 10 * time.Minute},
			"post": {Enabled: true, TTL: 0}, // Will use default
		},
	}

	tests := []struct {
		name       string
		entityType string
		want       time.Duration
	}{
		{"entity with custom TTL", "user", 10 * time.Minute},
		{"entity with zero TTL", "post", 5 * time.Minute},
		{"unconfigured entity", "comment", 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.GetEntityTTL(tt.entityType); got != tt.want {
				t.Errorf("GetEntityTTL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnhancedCacheConfig_GetEntityMaxSize(t *testing.T) {
	cfg := EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		Entities: map[string]EntityCacheConfig{
			"user": {Enabled: true, MaxSize: 5000},
			"post": {Enabled: true, MaxSize: 0}, // Will use proportional
		},
	}

	// Entity with custom max size
	if got := cfg.GetEntityMaxSize("user"); got != 5000 {
		t.Errorf("GetEntityMaxSize(user) = %d, want 5000", got)
	}

	// Entity with zero max size - should get proportional share
	// With 2 entities configured, proportional = 10000 / 3 = 3333
	postSize := cfg.GetEntityMaxSize("post")
	expectedProportional := cfg.MaxSize / (len(cfg.Entities) + 1)
	if postSize != expectedProportional {
		t.Errorf("GetEntityMaxSize(post) = %d, want %d", postSize, expectedProportional)
	}

	// Unconfigured entity with no entities configured
	cfg2 := EnhancedCacheConfig{
		Enabled:    true,
		DefaultTTL: 5 * time.Minute,
		MaxSize:    10000,
		Entities:   make(map[string]EntityCacheConfig),
	}
	if got := cfg2.GetEntityMaxSize("user"); got != 10000 {
		t.Errorf("GetEntityMaxSize(user) with no entities = %d, want 10000", got)
	}
}

func TestEnhancedCacheConfig_GetEntityWriteThrough(t *testing.T) {
	trueVal := true
	falseVal := false

	cfg := EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		WriteThrough:    true,
		Entities: map[string]EntityCacheConfig{
			"user":    {Enabled: true, WriteThrough: &trueVal},
			"post":    {Enabled: true, WriteThrough: &falseVal},
			"comment": {Enabled: true}, // Will use default
		},
	}

	tests := []struct {
		name       string
		entityType string
		want       bool
	}{
		{"entity with write-through enabled", "user", true},
		{"entity with write-through disabled", "post", false},
		{"entity using default", "comment", true},
		{"unconfigured entity", "tag", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.GetEntityWriteThrough(tt.entityType); got != tt.want {
				t.Errorf("GetEntityWriteThrough() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnhancedCacheConfig_Clone(t *testing.T) {
	trueVal := true

	original := &EnhancedCacheConfig{
		Enabled:           true,
		DefaultTTL:        5 * time.Minute,
		MaxSize:           10000,
		CleanupInterval:   1 * time.Minute,
		WriteThrough:      true,
		L2Enabled:         true,
		L2TTL:             30 * time.Minute,
		L2RedisAddr:       "localhost:6379",
		L2RedisPassword:   "secret",
		L2RedisDB:         0,
		L2RedisUseSecrets: true,
		L2RedisSecretPath: "cache/redis/password",
		Entities: map[string]EntityCacheConfig{
			"user": {
				Enabled:      true,
				TTL:          10 * time.Minute,
				MaxSize:      5000,
				WriteThrough: &trueVal,
			},
		},
	}

	clone := original.Clone()

	// Verify clone matches original
	if clone.Enabled != original.Enabled {
		t.Error("Clone mismatch: Enabled")
	}
	if clone.DefaultTTL != original.DefaultTTL {
		t.Error("Clone mismatch: DefaultTTL")
	}
	if clone.MaxSize != original.MaxSize {
		t.Error("Clone mismatch: MaxSize")
	}
	if clone.L2RedisUseSecrets != original.L2RedisUseSecrets {
		t.Error("Clone mismatch: L2RedisUseSecrets")
	}
	if clone.L2RedisSecretPath != original.L2RedisSecretPath {
		t.Error("Clone mismatch: L2RedisSecretPath")
	}

	// Verify deep copy - modifying clone should not affect original
	clone.Enabled = false
	if !original.Enabled {
		t.Error("Modifying clone affected original: Enabled")
	}

	clone.L2RedisUseSecrets = false
	if !original.L2RedisUseSecrets {
		t.Error("Modifying clone affected original: L2RedisUseSecrets")
	}

	clone.Entities["user"] = EntityCacheConfig{Enabled: false}
	if !original.Entities["user"].Enabled {
		t.Error("Modifying clone's entity affected original")
	}

	clone.Entities["post"] = EntityCacheConfig{Enabled: true}
	if _, exists := original.Entities["post"]; exists {
		t.Error("Adding entity to clone affected original")
	}

	// Test clone of nil
	var nilCfg *EnhancedCacheConfig
	cloneNil := nilCfg.Clone()
	if cloneNil != nil {
		t.Error("Clone of nil should return nil")
	}
}

func TestEntityCacheConfig_MergeWithDefaults(t *testing.T) {
	trueVal := true

	cfg := EnhancedCacheConfig{
		Enabled:         true,
		DefaultTTL:      5 * time.Minute,
		MaxSize:         10000,
		CleanupInterval: 1 * time.Minute,
		WriteThrough:    true,
	}

	tests := []struct {
		name    string
		entity  EntityCacheConfig
		wantTTL time.Duration
		wantWT  bool
	}{
		{
			name: "all values set",
			entity: EntityCacheConfig{
				Enabled:      true,
				TTL:          10 * time.Minute,
				MaxSize:      5000,
				WriteThrough: &trueVal,
			},
			wantTTL: 10 * time.Minute,
			wantWT:  true,
		},
		{
			name: "TTL not set",
			entity: EntityCacheConfig{
				Enabled:      true,
				TTL:          0,
				MaxSize:      5000,
				WriteThrough: &trueVal,
			},
			wantTTL: 5 * time.Minute, // Should use default
			wantWT:  true,
		},
		{
			name: "WriteThrough not set",
			entity: EntityCacheConfig{
				Enabled: true,
				TTL:     10 * time.Minute,
				MaxSize: 5000,
			},
			wantTTL: 10 * time.Minute,
			wantWT:  true, // Should use default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := cfg.mergeWithDefaults(tt.entity)

			if merged.TTL != tt.wantTTL {
				t.Errorf("mergeWithDefaults() TTL = %v, want %v", merged.TTL, tt.wantTTL)
			}

			if merged.WriteThrough != nil && *merged.WriteThrough != tt.wantWT {
				t.Errorf("mergeWithDefaults() WriteThrough = %v, want %v", *merged.WriteThrough, tt.wantWT)
			}
		})
	}
}

// TestInternalSecretRedisPassword tests the constant value.
func TestInternalSecretRedisPassword(t *testing.T) {
	expectedPath := "cache/redis/password"
	if InternalSecretRedisPassword != expectedPath {
		t.Errorf("InternalSecretRedisPassword = %q, want %q", InternalSecretRedisPassword, expectedPath)
	}
}
