//go:build vector && qrdb_vector

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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// fakeProviderError is a typed error returned by fakeProvider to verify error
// propagation through the bridge and manager.
type fakeProviderError struct{ msg string }

func (e *fakeProviderError) Error() string { return e.msg }

// fakeProvider is an in-memory implementation of the go-quicbaas Provider
// interface used to drive the Manager and bridge code paths without any network
// access. It records which methods were invoked so tests can assert that the
// distinct single-text and batch hooks are honoured.
type fakeProvider struct {
	name        string
	dims        int
	embedCalls  int
	batchCalls  int
	embedErr    error
	batchErr    error
	closeErr    error
	emptyResult bool
}

func (f *fakeProvider) Embed(_ context.Context, text string) ([]float32, error) {
	f.embedCalls++
	if f.embedErr != nil {
		return nil, f.embedErr
	}
	if f.emptyResult {
		return []float32{}, nil
	}
	return []float32{float32(len(text)), 1, 2}, nil
}

func (f *fakeProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	f.batchCalls++
	if f.batchErr != nil {
		return nil, f.batchErr
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = []float32{float32(len(t)), 3, 4}
	}
	return out, nil
}

func (f *fakeProvider) Dimensions() int { return f.dims }
func (f *fakeProvider) MaxTokens() int  { return 100 }
func (f *fakeProvider) Name() string {
	if f.name == "" {
		return "fake"
	}
	return f.name
}
func (f *fakeProvider) Close() error { return f.closeErr }

// validManagerConfig returns a Config that passes go-qrdb's manager validation
// for an Ollama provider (no API key required).
func validManagerConfig() Config {
	return Config{
		Provider:     "ollama",
		APIURL:       "http://localhost:11434",
		Model:        "nomic-embed-text",
		Dimensions:   768,
		MaxBatchSize: 8,
		Timeout:      30,
		CacheEnabled: false,
	}
}

// newManagerWithFake builds a Manager whose default provider name matches the
// registered fakeProvider, exercising the round trip through providerBridgeOut.
func newManagerWithFake(t *testing.T, fp *fakeProvider) *Manager {
	t.Helper()
	cfg := validManagerConfig()
	m, err := NewManager(cfg)
	require.NoError(t, err)
	require.NoError(t, m.RegisterProvider(cfg.Provider, fp))
	t.Cleanup(func() { _ = m.Close() })
	return m
}

// =============================================================================
// NewManager
// =============================================================================

func TestNewManager(t *testing.T) {
	cached := validManagerConfig()
	cached.CacheEnabled = true
	cached.CacheSize = 16

	tests := []struct {
		name      string
		cfg       Config
		wantErr   error
		wantCache bool
	}{
		{name: "empty provider", cfg: Config{}, wantErr: qrdbsdk.ErrEmbeddingProviderRequired},
		{name: "valid without cache", cfg: validManagerConfig()},
		{name: "valid with cache", cfg: cached, wantCache: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := NewManager(tc.cfg)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			t.Cleanup(func() { _ = m.Close() })
			assert.Equal(t, tc.wantCache, m.GetStats().CacheEnabled)
		})
	}
}

// =============================================================================
// RegisterProvider / GetProvider
// =============================================================================

func TestManager_RegisterAndGetProvider(t *testing.T) {
	fp := &fakeProvider{name: "ollama", dims: 768}
	m := newManagerWithFake(t, fp)

	got, err := m.GetProvider("ollama")
	require.NoError(t, err)
	// GetProvider unwraps the bridgeOut wrapper so the original instance returns.
	assert.Same(t, fp, got)
}

func TestManager_GetProvider_NotFound(t *testing.T) {
	m, err := NewManager(validManagerConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	_, err = m.GetProvider("missing")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrProviderNotFound)
}

// TestManager_GetProvider_NativeProvider exercises the providerBridgeIn path by
// registering a native go-qrdb provider (not a go-quicbaas Provider) directly
// into the underlying manager.
func TestManager_GetProvider_NativeProvider(t *testing.T) {
	cfg := validManagerConfig()
	m, err := NewManager(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	native := &nativeQRDBProvider{dims: 12}
	require.NoError(t, m.inner.RegisterProvider(cfg.Provider, native))

	got, err := m.GetProvider(cfg.Provider)
	require.NoError(t, err)
	_, isBridgeIn := got.(*providerBridgeIn)
	assert.True(t, isBridgeIn, "native provider should be wrapped in providerBridgeIn")
	assert.Equal(t, 12, got.Dimensions())
	assert.Equal(t, "native", got.Name())
	assert.Equal(t, 0, got.MaxTokens())
}

// =============================================================================
// Embed / EmbedBatch
// =============================================================================

func TestManager_Embed_Success(t *testing.T) {
	fp := &fakeProvider{name: "ollama", dims: 3}
	m := newManagerWithFake(t, fp)

	vec, err := m.Embed(context.Background(), "hello")
	require.NoError(t, err)
	require.Len(t, vec, 3)
	assert.Equal(t, 1, fp.embedCalls, "single Embed should call the single-text hook")
}

func TestManager_Embed_Error(t *testing.T) {
	fp := &fakeProvider{name: "ollama", embedErr: &fakeProviderError{msg: "embed failed"}}
	m := newManagerWithFake(t, fp)

	_, err := m.Embed(context.Background(), "hello")
	require.Error(t, err)
	var fe *fakeProviderError
	assert.True(t, errors.As(err, &fe))
}

func TestManager_EmbedBatch_Success(t *testing.T) {
	fp := &fakeProvider{name: "ollama"}
	m := newManagerWithFake(t, fp)

	out, err := m.EmbedBatch(context.Background(), []string{"a", "bb", "ccc"})
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, 1, fp.batchCalls, "batch path must use the EmbedBatch hook")
	assert.Equal(t, 0, fp.embedCalls)
}

func TestManager_EmbedBatch_EmptyInput(t *testing.T) {
	fp := &fakeProvider{name: "ollama"}
	m := newManagerWithFake(t, fp)

	_, err := m.EmbedBatch(context.Background(), nil)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestManager_EmbedBatch_ProviderError(t *testing.T) {
	fp := &fakeProvider{name: "ollama", batchErr: &fakeProviderError{msg: "batch failed"}}
	m := newManagerWithFake(t, fp)

	_, err := m.EmbedBatch(context.Background(), []string{"a", "b"})
	require.Error(t, err)
	var fe *fakeProviderError
	assert.True(t, errors.As(err, &fe))
}

func TestManager_EmbedBatch_ProviderNotRegistered(t *testing.T) {
	m, err := NewManager(validManagerConfig())
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	// No provider registered under the configured name.
	_, err = m.EmbedBatch(context.Background(), []string{"a"})
	assert.ErrorIs(t, err, ErrProviderNotFound)
}

// TestManager_EmbedBatch_NativeProvider drives the EmbedBatch path through a
// native go-qrdb provider (the non-bridgeOut branch).
func TestManager_EmbedBatch_NativeProvider(t *testing.T) {
	cfg := validManagerConfig()
	m, err := NewManager(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	require.NoError(t, m.inner.RegisterProvider(cfg.Provider, &nativeQRDBProvider{dims: 2}))

	out, err := m.EmbedBatch(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Len(t, out[0], 2)
}

// =============================================================================
// Dimensions / GetStats / Close
// =============================================================================

func TestManager_Dimensions_Success(t *testing.T) {
	fp := &fakeProvider{name: "ollama", dims: 768}
	m := newManagerWithFake(t, fp)

	d, err := m.Dimensions()
	require.NoError(t, err)
	assert.Equal(t, 768, d)
}

func TestManager_GetStats_Success(t *testing.T) {
	fp := &fakeProvider{name: "ollama"}
	m := newManagerWithFake(t, fp)

	stats := m.GetStats()
	assert.Equal(t, 1, stats.ProvidersRegistered)
	assert.Equal(t, "ollama", stats.DefaultProvider)
	assert.Contains(t, stats.ProviderNames, "ollama")
}

func TestManager_Close_Success(t *testing.T) {
	fp := &fakeProvider{name: "ollama"}
	cfg := validManagerConfig()
	m, err := NewManager(cfg)
	require.NoError(t, err)
	require.NoError(t, m.RegisterProvider(cfg.Provider, fp))

	assert.NoError(t, m.Close())
}

// TestManager_NilInner checks that every method of a zero-value Manager (no
// underlying go-qrdb manager) fails safely instead of panicking.
func TestManager_NilInner(t *testing.T) {
	tests := []struct {
		name    string
		call    func(t *testing.T, m *Manager) error
		wantErr error
	}{
		{"RegisterProvider", func(_ *testing.T, m *Manager) error {
			return m.RegisterProvider("x", &fakeProvider{})
		}, ErrProviderNotFound},
		{"GetProvider", func(_ *testing.T, m *Manager) error {
			_, err := m.GetProvider("x")
			return err
		}, ErrProviderNotFound},
		{"Embed", func(_ *testing.T, m *Manager) error {
			_, err := m.Embed(context.Background(), "x")
			return err
		}, ErrProviderNotFound},
		{"EmbedBatch", func(_ *testing.T, m *Manager) error {
			_, err := m.EmbedBatch(context.Background(), []string{"a"})
			return err
		}, ErrProviderNotFound},
		{"Dimensions", func(t *testing.T, m *Manager) error {
			d, err := m.Dimensions()
			assert.Equal(t, 0, d)
			return err
		}, ErrProviderNotFound},
		{"GetStats", func(t *testing.T, m *Manager) error {
			assert.Equal(t, Stats{}, m.GetStats())
			return nil
		}, nil},
		{"Close", func(_ *testing.T, m *Manager) error {
			return m.Close()
		}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(t, &Manager{})
			if tc.wantErr == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// =============================================================================
// mapQRDBError
// =============================================================================

func TestMapQRDBError(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want error
	}{
		{"nil", nil, nil},
		{"provider not found", &qrdbsdk.EmbeddingProviderNotFoundError{Name: "x"}, ErrProviderNotFound},
		{"provider required", qrdbsdk.ErrEmbeddingProviderRequired, ErrProviderRequired},
		{"api url required", qrdbsdk.ErrEmbeddingAPIURLRequired, ErrAPIURLRequired},
		{"api key required", qrdbsdk.ErrEmbeddingAPIKeyRequired, ErrAPIKeyRequired},
		{"model required", qrdbsdk.ErrEmbeddingModelRequired, ErrModelRequired},
		{"passthrough", &fakeProviderError{msg: "other"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapQRDBError(tc.in)
			if tc.name == "passthrough" {
				// Unmapped errors pass through unchanged.
				assert.Equal(t, tc.in, got)
				return
			}
			if tc.want == nil {
				assert.NoError(t, got)
				return
			}
			assert.ErrorIs(t, got, tc.want)
		})
	}
}

// TestManager_EmbedBatch_NativeProvider_Error covers the error branch of the
// native go-qrdb provider path in Manager.EmbedBatch.
func TestManager_EmbedBatch_NativeProvider_Error(t *testing.T) {
	cfg := validManagerConfig()
	m, err := NewManager(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	wantErr := errors.New("native batch failure")
	require.NoError(t, m.inner.RegisterProvider(cfg.Provider, &nativeQRDBProvider{embedErr: wantErr}))

	_, err = m.EmbedBatch(context.Background(), []string{"a", "b"})
	assert.ErrorIs(t, err, wantErr)
}
