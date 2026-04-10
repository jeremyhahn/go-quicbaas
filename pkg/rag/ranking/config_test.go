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

package ranking

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, float32(1.2), config.BM25K1)
	assert.Equal(t, float32(0.75), config.BM25B)
	assert.Equal(t, float32(0.7), config.VectorWeight)
	assert.Equal(t, float32(0.3), config.KeywordWeight)
	assert.Equal(t, 50, config.RerankTopK)
	assert.Equal(t, true, config.EnableRerank)
	assert.Equal(t, float32(60), config.RRFK)
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name: "invalid BM25K1 - zero",
			config: Config{
				BM25K1:        0,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: true,
		},
		{
			name: "invalid BM25K1 - negative",
			config: Config{
				BM25K1:        -1.0,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: true,
		},
		{
			name: "invalid BM25B - negative",
			config: Config{
				BM25K1:        1.2,
				BM25B:         -0.1,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: true,
		},
		{
			name: "invalid BM25B - greater than 1",
			config: Config{
				BM25K1:        1.2,
				BM25B:         1.5,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: true,
		},
		{
			name: "invalid VectorWeight - negative",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  -0.1,
				KeywordWeight: 0.3,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: true,
		},
		{
			name: "invalid VectorWeight - greater than 1",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  1.5,
				KeywordWeight: 0.3,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: true,
		},
		{
			name: "invalid KeywordWeight - negative",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: -0.1,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: true,
		},
		{
			name: "invalid weights sum - greater than 1",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.6,
				KeywordWeight: 0.5,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: true,
		},
		{
			name: "invalid RerankTopK - zero",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RerankTopK:    0,
				RRFK:          60,
			},
			wantErr: true,
		},
		{
			name: "invalid RerankTopK - negative",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RerankTopK:    -10,
				RRFK:          60,
			},
			wantErr: true,
		},
		{
			name: "invalid RRFK - zero",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RerankTopK:    50,
				RRFK:          0,
			},
			wantErr: true,
		},
		{
			name: "invalid RRFK - negative",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RerankTopK:    50,
				RRFK:          -60,
			},
			wantErr: true,
		},
		{
			name: "valid custom config",
			config: Config{
				BM25K1:        1.5,
				BM25B:         0.8,
				VectorWeight:  0.6,
				KeywordWeight: 0.4,
				RerankTopK:    100,
				EnableRerank:  false,
				RRFK:          100,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, ErrInvalidConfig, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestConfig_ValidateBoundaries(t *testing.T) {
	// Test boundary conditions
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "BM25B exactly 0",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: false,
		},
		{
			name: "BM25B exactly 1",
			config: Config{
				BM25K1:        1.2,
				BM25B:         1.0,
				VectorWeight:  0.7,
				KeywordWeight: 0.3,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: false,
		},
		{
			name: "weights sum exactly 1",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.6,
				KeywordWeight: 0.4,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: false,
		},
		{
			name: "weights sum slightly over 1",
			config: Config{
				BM25K1:        1.2,
				BM25B:         0.75,
				VectorWeight:  0.6,
				KeywordWeight: 0.41,
				RerankTopK:    50,
				RRFK:          60,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
