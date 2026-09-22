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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// EmbeddingManagerInterface defines the interface that Manager implements.
// This is kept local to avoid circular imports with pkg/interfaces.
type EmbeddingManagerInterface interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() (int, error)
	GetProvider(name string) (Provider, error)
	GetStats() Stats
}

// Manager manages multiple embedding providers with caching.
// It wraps qrdbsdk.EmbeddingManager and exposes the single-text Embed/EmbedBatch API
// used throughout go-quicbaas while delegating all real work to go-qrdb.
type Manager struct {
	inner    *qrdbsdk.EmbeddingManager
	provider string // default provider name from Config.Provider
}

// Compile-time assertion that Manager satisfies EmbeddingManagerInterface.
var _ EmbeddingManagerInterface = (*Manager)(nil)

// NewManager creates a new Manager from the given Config.
func NewManager(config Config) (*Manager, error) {
	inner, err := qrdbsdk.NewEmbeddingManager(config)
	if err != nil {
		return nil, err
	}
	return &Manager{inner: inner, provider: config.Provider}, nil
}

// RegisterProvider registers a new embedding provider.
// The provider must implement the go-quicbaas Provider interface.
// It is wrapped internally into the go-qrdb Provider shape.
func (m *Manager) RegisterProvider(name string, provider Provider) error {
	if m.inner == nil {
		return ErrProviderNotFound
	}
	return m.inner.RegisterProvider(name, &providerBridgeOut{inner: provider})
}

// GetProvider returns a provider by name as a go-quicbaas Provider.
func (m *Manager) GetProvider(name string) (Provider, error) {
	if m.inner == nil {
		return nil, ErrProviderNotFound
	}
	qp, err := m.inner.GetProvider(name)
	if err != nil {
		return nil, mapQRDBError(err)
	}
	// If the underlying provider is a providerBridgeOut wrapping a go-quicbaas
	// Provider, unwrap it to avoid double-wrapping.
	if pbo, ok := qp.(*providerBridgeOut); ok {
		return pbo.inner, nil
	}
	return &providerBridgeIn{inner: qp, model: ""}, nil
}

// Embed generates an embedding for a single text using the configured provider.
func (m *Manager) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.inner == nil {
		return nil, ErrProviderNotFound
	}
	result, err := m.inner.Embed(ctx, text)
	if err != nil {
		return nil, mapQRDBError(err)
	}
	return result, nil
}

// EmbedBatch generates embeddings for multiple texts.
// It retrieves the registered provider directly and calls EmbedBatch to
// preserve the distinct batch execution path expected by callers and mocks.
func (m *Manager) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if m.inner == nil {
		return nil, ErrProviderNotFound
	}
	if len(texts) == 0 {
		return nil, ErrInvalidInput
	}

	// Retrieve the underlying provider so we can call EmbedBatch directly.
	// This mirrors the old Manager behaviour and preserves test mock semantics.
	qp, err := m.inner.GetProvider(m.provider)
	if err != nil {
		return nil, mapQRDBError(err)
	}

	// If the underlying provider is our bridgeOut wrapper, unwrap and call
	// EmbedBatch on the go-quicbaas Provider to honour the EmbedBatch hook.
	if pbo, ok := qp.(*providerBridgeOut); ok {
		return pbo.inner.EmbedBatch(ctx, texts)
	}

	// Native go-qrdb provider — use the Manager's batching path.
	result, err := m.inner.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, mapQRDBError(err)
	}
	return result, nil
}

// Dimensions returns the dimensions of embeddings from the current provider.
func (m *Manager) Dimensions() (int, error) {
	if m.inner == nil {
		return 0, ErrProviderNotFound
	}
	return m.inner.Dimensions()
}

// Close closes all providers and releases resources.
func (m *Manager) Close() error {
	if m.inner == nil {
		return nil
	}
	return m.inner.Close()
}

// GetStats returns statistics about the manager.
func (m *Manager) GetStats() Stats {
	if m.inner == nil {
		return Stats{}
	}
	return statsFromManager(m.inner.GetStats())
}

// mapQRDBError translates go-qrdb embedding errors to go-quicbaas sentinel
// errors where an exact match exists, passing through all other errors unchanged.
func mapQRDBError(err error) error {
	if err == nil {
		return nil
	}
	var pnf *qrdbsdk.EmbeddingProviderNotFoundError
	if errors.As(err, &pnf) {
		return ErrProviderNotFound
	}
	switch {
	case errors.Is(err, qrdbsdk.ErrEmbeddingProviderRequired):
		return ErrProviderRequired
	// go-qrdb#528 stopped returning this sentinel: NewEmbeddingManager now
	// accepts any provider name and an unregistered one surfaces as
	// *EmbeddingProviderNotFoundError, which the errors.As above already
	// maps. The arm is kept so an error built against an older qrdb still
	// maps to the same public error rather than falling through to the
	// generic case; go-qrdb re-exports the sentinel under the same
	// suppression for the same reason. Removing the arm is a deliberate
	// error-mapping change for this repository to make, not a side effect
	// of a dependency bump.
	//nolint:staticcheck // SA1019: deliberate, see above.
	case errors.Is(err, qrdbsdk.ErrEmbeddingProviderInvalid):
		return ErrProviderInvalid
	case errors.Is(err, qrdbsdk.ErrEmbeddingAPIURLRequired):
		return ErrAPIURLRequired
	case errors.Is(err, qrdbsdk.ErrEmbeddingAPIKeyRequired):
		return ErrAPIKeyRequired
	case errors.Is(err, qrdbsdk.ErrEmbeddingModelRequired):
		return ErrModelRequired
	}
	return err
}
