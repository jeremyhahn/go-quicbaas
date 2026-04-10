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

package credentials

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// mockGenerator is a test implementation of CredentialGenerator.
type mockGenerator struct {
	genType string
}

func newMockGenerator(genType string) *mockGenerator {
	return &mockGenerator{genType: genType}
}

func (m *mockGenerator) Type() string {
	return m.genType
}

func (m *mockGenerator) ValidateConfig(config map[string]string) error {
	return nil
}

func (m *mockGenerator) Generate(ctx context.Context, role *Role) (*Credential, error) {
	return nil, nil
}

func (m *mockGenerator) Revoke(ctx context.Context, credential *Credential) error {
	return nil
}

func (m *mockGenerator) Rotate(ctx context.Context, old *Credential, role *Role) (*Credential, error) {
	return nil, nil
}

func TestNewGeneratorRegistry(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "creates new registry successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewGeneratorRegistry()

			require.NotNil(t, registry)
			assert.NotNil(t, registry.generators)
			assert.Equal(t, 0, registry.Count())
		})
	}
}

func TestGeneratorRegistry_Register(t *testing.T) {
	tests := []struct {
		name      string
		generator CredentialGenerator
		wantErr   bool
		errCode   errors.ErrorCode
		errMsg    string
	}{
		{
			name:      "success - register postgres generator",
			generator: newMockGenerator("postgres"),
			wantErr:   false,
		},
		{
			name:      "success - register mysql generator",
			generator: newMockGenerator("mysql"),
			wantErr:   false,
		},
		{
			name:      "error - nil generator",
			generator: nil,
			wantErr:   true,
			errCode:   errors.ErrInvalidInput,
			errMsg:    "generator cannot be nil",
		},
		{
			name:      "error - empty type",
			generator: newMockGenerator(""),
			wantErr:   true,
			errCode:   errors.ErrInvalidInput,
			errMsg:    "generator type cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewGeneratorRegistry()
			err := registry.Register(tt.generator)

			if tt.wantErr {
				require.Error(t, err)
				var dragonErr *errors.DragonError
				require.ErrorAs(t, err, &dragonErr)
				assert.Equal(t, tt.errCode, dragonErr.Code)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				assert.Equal(t, 1, registry.Count())
			}
		})
	}
}

func TestGeneratorRegistry_Register_Duplicate(t *testing.T) {
	tests := []struct {
		name    string
		genType string
		wantErr bool
		errCode errors.ErrorCode
	}{
		{
			name:    "error - duplicate postgres generator",
			genType: "postgres",
			wantErr: true,
			errCode: errors.ErrAlreadyExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewGeneratorRegistry()

			// Register first generator
			gen1 := newMockGenerator(tt.genType)
			err := registry.Register(gen1)
			require.NoError(t, err)

			// Try to register duplicate
			gen2 := newMockGenerator(tt.genType)
			err = registry.Register(gen2)

			if tt.wantErr {
				require.Error(t, err)
				var dragonErr *errors.DragonError
				require.ErrorAs(t, err, &dragonErr)
				assert.Equal(t, tt.errCode, dragonErr.Code)
				assert.ErrorIs(t, err, ErrGeneratorAlreadyRegistered)
				assert.Contains(t, err.Error(), "credential generator already registered")
			}
		})
	}
}

func TestGeneratorRegistry_Get(t *testing.T) {
	tests := []struct {
		name         string
		setupGenType string
		getGenType   string
		wantErr      bool
		errCode      errors.ErrorCode
		errMsg       string
	}{
		{
			name:         "success - get postgres generator",
			setupGenType: "postgres",
			getGenType:   "postgres",
			wantErr:      false,
		},
		{
			name:         "error - generator not found",
			setupGenType: "postgres",
			getGenType:   "mysql",
			wantErr:      true,
			errCode:      errors.ErrNotFound,
			errMsg:       "credential generator not found",
		},
		{
			name:         "error - empty type",
			setupGenType: "postgres",
			getGenType:   "",
			wantErr:      true,
			errCode:      errors.ErrInvalidInput,
			errMsg:       "generator type cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewGeneratorRegistry()

			// Setup
			if tt.setupGenType != "" {
				gen := newMockGenerator(tt.setupGenType)
				err := registry.Register(gen)
				require.NoError(t, err)
			}

			// Test
			gen, err := registry.Get(tt.getGenType)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, gen)
				var dragonErr *errors.DragonError
				require.ErrorAs(t, err, &dragonErr)
				assert.Equal(t, tt.errCode, dragonErr.Code)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
				require.NotNil(t, gen)
				assert.Equal(t, tt.getGenType, gen.Type())
			}
		})
	}
}

func TestGeneratorRegistry_List(t *testing.T) {
	tests := []struct {
		name      string
		genTypes  []string
		wantCount int
	}{
		{
			name:      "empty registry",
			genTypes:  []string{},
			wantCount: 0,
		},
		{
			name:      "single generator",
			genTypes:  []string{"postgres"},
			wantCount: 1,
		},
		{
			name:      "multiple generators",
			genTypes:  []string{"postgres", "mysql", "mongodb"},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewGeneratorRegistry()

			// Setup
			for _, genType := range tt.genTypes {
				gen := newMockGenerator(genType)
				err := registry.Register(gen)
				require.NoError(t, err)
			}

			// Test
			types := registry.List()

			assert.Equal(t, tt.wantCount, len(types))
			for _, expectedType := range tt.genTypes {
				assert.Contains(t, types, expectedType)
			}
		})
	}
}

func TestGeneratorRegistry_Unregister(t *testing.T) {
	tests := []struct {
		name         string
		setupGenType string
		removeType   string
		wantErr      bool
		errCode      errors.ErrorCode
		errMsg       string
	}{
		{
			name:         "success - unregister postgres generator",
			setupGenType: "postgres",
			removeType:   "postgres",
			wantErr:      false,
		},
		{
			name:         "error - generator not found",
			setupGenType: "postgres",
			removeType:   "mysql",
			wantErr:      true,
			errCode:      errors.ErrNotFound,
			errMsg:       "credential generator not found",
		},
		{
			name:         "error - empty type",
			setupGenType: "postgres",
			removeType:   "",
			wantErr:      true,
			errCode:      errors.ErrInvalidInput,
			errMsg:       "generator type cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewGeneratorRegistry()

			// Setup
			if tt.setupGenType != "" {
				gen := newMockGenerator(tt.setupGenType)
				err := registry.Register(gen)
				require.NoError(t, err)
			}

			initialCount := registry.Count()

			// Test
			err := registry.Unregister(tt.removeType)

			if tt.wantErr {
				require.Error(t, err)
				var dragonErr *errors.DragonError
				require.ErrorAs(t, err, &dragonErr)
				assert.Equal(t, tt.errCode, dragonErr.Code)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Equal(t, initialCount, registry.Count())
			} else {
				require.NoError(t, err)
				assert.Equal(t, initialCount-1, registry.Count())

				// Verify it's really gone
				_, err = registry.Get(tt.removeType)
				require.Error(t, err)
			}
		})
	}
}

func TestGeneratorRegistry_Count(t *testing.T) {
	tests := []struct {
		name     string
		genTypes []string
		want     int
	}{
		{
			name:     "empty registry",
			genTypes: []string{},
			want:     0,
		},
		{
			name:     "one generator",
			genTypes: []string{"postgres"},
			want:     1,
		},
		{
			name:     "three generators",
			genTypes: []string{"postgres", "mysql", "mongodb"},
			want:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewGeneratorRegistry()

			for _, genType := range tt.genTypes {
				gen := newMockGenerator(genType)
				err := registry.Register(gen)
				require.NoError(t, err)
			}

			count := registry.Count()
			assert.Equal(t, tt.want, count)
		})
	}
}

func TestGeneratorRegistry_Concurrent(t *testing.T) {
	t.Run("concurrent register and get", func(t *testing.T) {
		registry := NewGeneratorRegistry()
		done := make(chan bool)

		// Concurrent registration
		for i := 0; i < 10; i++ {
			go func(idx int) {
				gen := newMockGenerator(string(rune('a' + idx)))
				_ = registry.Register(gen)
				done <- true
			}(i)
		}

		// Wait for all registrations
		for i := 0; i < 10; i++ {
			<-done
		}

		// Concurrent reads
		for i := 0; i < 10; i++ {
			go func(idx int) {
				_, _ = registry.Get(string(rune('a' + idx)))
				done <- true
			}(i)
		}

		// Wait for all reads
		for i := 0; i < 10; i++ {
			<-done
		}

		// Verify no race conditions occurred
		count := registry.Count()
		assert.True(t, count > 0 && count <= 10)
	})
}
