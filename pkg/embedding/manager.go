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
	"context"
	"errors"

	qembed "github.com/jeremyhahn/go-qrdb/pkg/embedding"
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
// It wraps qembed.Manager and exposes the single-text Embed/EmbedBatch API
// used throughout go-quicbaas while delegating all real work to go-qrdb.
type Manager struct {
	inner    *qembed.Manager
	provider string // default provider name from Config.Provider
}

// Compile-time assertion that Manager satisfies EmbeddingManagerInterface.
var _ EmbeddingManagerInterface = (*Manager)(nil)

// NewManager creates a new Manager from the given Config.
func NewManager(config Config) (*Manager, error) {
	inner, err := qembed.NewManager(config)
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
	var pnf *qembed.ProviderNotFoundError
	if errors.As(err, &pnf) {
		return ErrProviderNotFound
	}
	switch err {
	case qembed.ErrProviderRequired:
		return ErrProviderRequired
	case qembed.ErrProviderInvalid:
		return ErrProviderInvalid
	case qembed.ErrAPIURLRequired:
		return ErrAPIURLRequired
	case qembed.ErrAPIKeyRequired:
		return ErrAPIKeyRequired
	case qembed.ErrModelRequired:
		return ErrModelRequired
	}
	return err
}
