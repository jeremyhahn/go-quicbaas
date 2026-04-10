//go:build rag

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

package pipeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	// Verify all defaults are set
	assert.True(t, config.EnableCache)
	assert.True(t, config.EnableHybrid)
	assert.True(t, config.EnableRerank)
	assert.Equal(t, "default", config.DefaultNamespace)
	assert.Equal(t, 100, config.MaxConcurrentQueries)
	assert.Equal(t, 30*time.Second, config.QueryTimeout)
	assert.Equal(t, 10, config.WorkerPoolSize)
	assert.Equal(t, 10, config.DefaultTopK)
	assert.Equal(t, 8000, config.DefaultMaxTokens)
	assert.Equal(t, float32(0.7), config.DefaultMinScore)
	assert.True(t, config.EnableCircuitBreaker)
	assert.Equal(t, 5, config.CircuitBreakerThreshold)
	assert.Equal(t, 60*time.Second, config.CircuitBreakerTimeout)
	assert.True(t, config.EnableMetrics)
	assert.False(t, config.EnableTracing)
	assert.Equal(t, "quicbaas_rag", config.MetricsNamespace)

	// Verify default config is valid
	err := config.Validate()
	assert.NoError(t, err)
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name: "negative max concurrent queries",
			modify: func(c *Config) {
				c.MaxConcurrentQueries = -1
			},
			wantErr: true,
			errMsg:  "max_concurrent_queries must be positive",
		},
		{
			name: "zero max concurrent queries",
			modify: func(c *Config) {
				c.MaxConcurrentQueries = 0
			},
			wantErr: true,
			errMsg:  "max_concurrent_queries must be positive",
		},
		{
			name: "negative query timeout",
			modify: func(c *Config) {
				c.QueryTimeout = -1 * time.Second
			},
			wantErr: true,
			errMsg:  "query_timeout must be positive",
		},
		{
			name: "zero query timeout",
			modify: func(c *Config) {
				c.QueryTimeout = 0
			},
			wantErr: true,
			errMsg:  "query_timeout must be positive",
		},
		{
			name: "negative default topK",
			modify: func(c *Config) {
				c.DefaultTopK = -1
			},
			wantErr: true,
			errMsg:  "default_top_k must be positive",
		},
		{
			name: "zero default topK",
			modify: func(c *Config) {
				c.DefaultTopK = 0
			},
			wantErr: true,
			errMsg:  "default_top_k must be positive",
		},
		{
			name: "negative default max tokens",
			modify: func(c *Config) {
				c.DefaultMaxTokens = -1
			},
			wantErr: true,
			errMsg:  "default_max_tokens must be positive",
		},
		{
			name: "zero default max tokens",
			modify: func(c *Config) {
				c.DefaultMaxTokens = 0
			},
			wantErr: true,
			errMsg:  "default_max_tokens must be positive",
		},
		{
			name: "min score below 0",
			modify: func(c *Config) {
				c.DefaultMinScore = -0.1
			},
			wantErr: true,
			errMsg:  "default_min_score must be between 0 and 1",
		},
		{
			name: "min score above 1",
			modify: func(c *Config) {
				c.DefaultMinScore = 1.1
			},
			wantErr: true,
			errMsg:  "default_min_score must be between 0 and 1",
		},
		{
			name: "min score at boundary 0",
			modify: func(c *Config) {
				c.DefaultMinScore = 0.0
			},
			wantErr: false,
		},
		{
			name: "min score at boundary 1",
			modify: func(c *Config) {
				c.DefaultMinScore = 1.0
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			tt.modify(&config)

			err := config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfigBuilder(t *testing.T) {
	// Test builder pattern
	builder := NewConfigBuilder()

	config := builder.
		WithCache(false).
		WithHybrid(false).
		WithRerank(false).
		WithNamespace("custom").
		WithMaxConcurrentQueries(50).
		WithQueryTimeout(15 * time.Second).
		WithDefaultTopK(20).
		WithDefaultMaxTokens(4000).
		WithDefaultMinScore(0.8).
		WithCircuitBreaker(false).
		WithMetrics(false).
		WithTracing(true).
		Build()

	assert.False(t, config.EnableCache)
	assert.False(t, config.EnableHybrid)
	assert.False(t, config.EnableRerank)
	assert.Equal(t, "custom", config.DefaultNamespace)
	assert.Equal(t, 50, config.MaxConcurrentQueries)
	assert.Equal(t, 15*time.Second, config.QueryTimeout)
	assert.Equal(t, 20, config.DefaultTopK)
	assert.Equal(t, 4000, config.DefaultMaxTokens)
	assert.Equal(t, float32(0.8), config.DefaultMinScore)
	assert.False(t, config.EnableCircuitBreaker)
	assert.False(t, config.EnableMetrics)
	assert.True(t, config.EnableTracing)
}

func TestConfigBuilderValidation(t *testing.T) {
	// Builder should produce invalid config if values are wrong
	builder := NewConfigBuilder()
	config := builder.
		WithMaxConcurrentQueries(-1).
		Build()

	err := config.Validate()
	assert.Error(t, err)
}

func TestConfigCopy(t *testing.T) {
	original := DefaultConfig()
	original.DefaultNamespace = "test"
	original.EnableCache = false

	copy := original.Copy()
	assert.Equal(t, original.DefaultNamespace, copy.DefaultNamespace)
	assert.Equal(t, original.EnableCache, copy.EnableCache)

	// Modify copy
	copy.DefaultNamespace = "modified"
	assert.NotEqual(t, original.DefaultNamespace, copy.DefaultNamespace)
}
