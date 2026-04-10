//go:build vector

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

package embedding

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, "voyage", config.Provider)
	assert.Equal(t, "voyage-code-2", config.Model)
	assert.Equal(t, 1536, config.Dimensions)
	assert.Equal(t, 128, config.MaxBatchSize)
	assert.Equal(t, 30, config.Timeout)
	assert.True(t, config.CacheEnabled)
	assert.Equal(t, 10000, config.CacheSize)
	assert.Equal(t, 3600, config.CacheTTL)
	assert.True(t, config.CacheCompress)
	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 1000, config.RetryBackoffMS)
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid voyage config",
			config: Config{
				Provider:     "voyage",
				APIURL:       "https://api.voyageai.com",
				APIKey:       "test-key",
				Model:        "voyage-code-2",
				Dimensions:   1536,
				MaxBatchSize: 128,
				Timeout:      30,
			},
			wantErr: false,
		},
		{
			name: "valid openai config",
			config: Config{
				Provider:     "openai",
				APIURL:       "https://api.openai.com",
				APIKey:       "sk-test",
				Model:        "text-embedding-3-small",
				Dimensions:   1536,
				MaxBatchSize: 128,
				Timeout:      30,
			},
			wantErr: false,
		},
		{
			name: "valid ollama config",
			config: Config{
				Provider:     "ollama",
				APIURL:       "http://localhost:11434",
				APIKey:       "", // No key needed for ollama
				Model:        "nomic-embed-text",
				Dimensions:   768,
				MaxBatchSize: 32,
				Timeout:      30,
			},
			wantErr: false,
		},
		{
			name: "valid lmstudio config",
			config: Config{
				Provider:     "lmstudio",
				APIURL:       "http://localhost:1234",
				APIKey:       "", // Optional for lmstudio
				Model:        "nomic-ai/nomic-embed-text-v1.5-GGUF",
				Dimensions:   768,
				MaxBatchSize: 32,
				Timeout:      30,
			},
			wantErr: false,
		},
		{
			name: "missing provider",
			config: Config{
				APIURL:       "https://api.voyageai.com",
				APIKey:       "test-key",
				Model:        "voyage-code-2",
				Dimensions:   1536,
				MaxBatchSize: 128,
				Timeout:      30,
			},
			wantErr: true,
			errMsg:  "provider is required",
		},
		{
			name: "invalid provider",
			config: Config{
				Provider:     "invalid",
				APIURL:       "https://api.voyageai.com",
				APIKey:       "test-key",
				Model:        "voyage-code-2",
				Dimensions:   1536,
				MaxBatchSize: 128,
				Timeout:      30,
			},
			wantErr: true,
			errMsg:  "provider must be one of",
		},
		{
			name: "missing api url",
			config: Config{
				Provider:     "voyage",
				APIKey:       "test-key",
				Model:        "voyage-code-2",
				Dimensions:   1536,
				MaxBatchSize: 128,
				Timeout:      30,
			},
			wantErr: true,
			errMsg:  "api_url is required",
		},
		{
			name: "missing api key for voyage",
			config: Config{
				Provider:     "voyage",
				APIURL:       "https://api.voyageai.com",
				APIKey:       "",
				Model:        "voyage-code-2",
				Dimensions:   1536,
				MaxBatchSize: 128,
				Timeout:      30,
			},
			wantErr: true,
			errMsg:  "api_key is required for Voyage AI",
		},
		{
			name: "missing model",
			config: Config{
				Provider:     "voyage",
				APIURL:       "https://api.voyageai.com",
				APIKey:       "test-key",
				Dimensions:   1536,
				MaxBatchSize: 128,
				Timeout:      30,
			},
			wantErr: true,
			errMsg:  "model is required",
		},
		{
			name: "invalid dimensions - zero",
			config: Config{
				Provider:     "voyage",
				APIURL:       "https://api.voyageai.com",
				APIKey:       "test-key",
				Model:        "voyage-code-2",
				Dimensions:   0,
				MaxBatchSize: 128,
				Timeout:      30,
			},
			wantErr: true,
			errMsg:  "dimensions must be positive",
		},
		{
			name: "invalid dimensions - negative",
			config: Config{
				Provider:     "voyage",
				APIURL:       "https://api.voyageai.com",
				APIKey:       "test-key",
				Model:        "voyage-code-2",
				Dimensions:   -1,
				MaxBatchSize: 128,
				Timeout:      30,
			},
			wantErr: true,
			errMsg:  "dimensions must be positive",
		},
		{
			name: "invalid max batch size - zero",
			config: Config{
				Provider:     "voyage",
				APIURL:       "https://api.voyageai.com",
				APIKey:       "test-key",
				Model:        "voyage-code-2",
				Dimensions:   1536,
				MaxBatchSize: 0,
				Timeout:      30,
			},
			wantErr: true,
			errMsg:  "max_batch_size must be positive",
		},
		{
			name: "invalid timeout - zero",
			config: Config{
				Provider:     "voyage",
				APIURL:       "https://api.voyageai.com",
				APIKey:       "test-key",
				Model:        "voyage-code-2",
				Dimensions:   1536,
				MaxBatchSize: 128,
				Timeout:      0,
			},
			wantErr: true,
			errMsg:  "timeout must be positive",
		},
		{
			name: "invalid timeout - negative",
			config: Config{
				Provider:     "voyage",
				APIURL:       "https://api.voyageai.com",
				APIKey:       "test-key",
				Model:        "voyage-code-2",
				Dimensions:   1536,
				MaxBatchSize: 128,
				Timeout:      -1,
			},
			wantErr: true,
			errMsg:  "timeout must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
