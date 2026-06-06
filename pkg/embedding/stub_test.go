//go:build !vector || !qrdb_vector

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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStub_NewManager confirms the stub build refuses to construct a Manager.
func TestStub_NewManager(t *testing.T) {
	m, err := NewManager(DefaultConfig())
	assert.Nil(t, m)
	assert.ErrorIs(t, err, ErrVectorNotEnabled)
}

func TestStub_ConfigValidate(t *testing.T) {
	cfg := DefaultConfig()
	assert.ErrorIs(t, cfg.Validate(), ErrVectorNotEnabled)
}

func TestStub_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "voyage", cfg.Provider)
	assert.Equal(t, "voyage-code-2", cfg.Model)
	assert.Equal(t, 1536, cfg.Dimensions)
}

func TestStub_DefaultOllamaConfig(t *testing.T) {
	cfg := DefaultOllamaConfig()
	assert.Equal(t, "http://localhost:11434", cfg.BaseURL)
	assert.Equal(t, "nomic-embed-text", cfg.Model)
}

func TestStub_DefaultLMStudioConfig(t *testing.T) {
	cfg := DefaultLMStudioConfig()
	assert.Equal(t, "http://localhost:1234", cfg.BaseURL)
	assert.Equal(t, 3, cfg.MaxRetries)
}

// TestStub_ManagerMethods drives every Manager stub method to confirm the
// disabled-vector contract.
func TestStub_ManagerMethods(t *testing.T) {
	ctx := context.Background()
	m := &Manager{}

	assert.ErrorIs(t, m.RegisterProvider("x", nil), ErrVectorNotEnabled)

	p, err := m.GetProvider("x")
	assert.Nil(t, p)
	assert.ErrorIs(t, err, ErrVectorNotEnabled)

	_, err = m.Embed(ctx, "t")
	assert.ErrorIs(t, err, ErrVectorNotEnabled)

	_, err = m.EmbedBatch(ctx, []string{"t"})
	assert.ErrorIs(t, err, ErrVectorNotEnabled)

	_, err = m.Dimensions()
	assert.ErrorIs(t, err, ErrVectorNotEnabled)

	assert.ErrorIs(t, m.Close(), ErrVectorNotEnabled)
	assert.Equal(t, Stats{}, m.GetStats())
}

// TestStub_Providers drives every provider stub constructor and method.
func TestStub_Providers(t *testing.T) {
	ctx := context.Background()

	t.Run("openai", func(t *testing.T) {
		p, err := NewOpenAIProvider(DefaultConfig())
		assert.Nil(t, p)
		assert.ErrorIs(t, err, ErrVectorNotEnabled)
		assertStubProvider(t, ctx, &OpenAIProvider{}, "openai")
	})
	t.Run("ollama", func(t *testing.T) {
		p, err := NewOllamaProvider(DefaultOllamaConfig())
		assert.Nil(t, p)
		assert.ErrorIs(t, err, ErrVectorNotEnabled)
		assertStubProvider(t, ctx, &OllamaProvider{}, "ollama")
	})
	t.Run("lmstudio", func(t *testing.T) {
		p, err := NewLMStudioProvider(DefaultLMStudioConfig())
		assert.Nil(t, p)
		assert.ErrorIs(t, err, ErrVectorNotEnabled)
		assertStubProvider(t, ctx, &LMStudioProvider{}, "lmstudio")
	})
	t.Run("voyage", func(t *testing.T) {
		p, err := NewVoyageProvider(DefaultConfig())
		assert.Nil(t, p)
		assert.ErrorIs(t, err, ErrVectorNotEnabled)
		assertStubProvider(t, ctx, &VoyageProvider{}, "voyage")
	})
}

// assertStubProvider verifies the common disabled-vector behaviour of a stub
// provider value.
func assertStubProvider(t *testing.T, ctx context.Context, p Provider, name string) {
	t.Helper()
	assert.Equal(t, name, p.Name())
	assert.Equal(t, 0, p.Dimensions())
	assert.Equal(t, 0, p.MaxTokens())
	assert.NoError(t, p.Close())

	_, err := p.Embed(ctx, "t")
	assert.ErrorIs(t, err, ErrVectorNotEnabled)

	_, err = p.EmbedBatch(ctx, []string{"t"})
	assert.ErrorIs(t, err, ErrVectorNotEnabled)
}

func TestStub_ErrVectorNotEnabled(t *testing.T) {
	require.Error(t, ErrVectorNotEnabled)
	assert.Contains(t, ErrVectorNotEnabled.Error(), "vector")
}
