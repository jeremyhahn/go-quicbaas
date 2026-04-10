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
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// Manager manages multiple embedding providers with caching and fallbacks.
type Manager struct {
	providers map[string]Provider
	cache     *Cache
	config    Config
	mu        sync.RWMutex
}

// NewManager creates a new embedding manager.
func NewManager(config Config) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, api.NewConfigurationError("embedding.NewManager", "validate config", err)
	}

	m := &Manager{
		providers: make(map[string]Provider),
		config:    config,
	}

	// Initialize cache if enabled
	if config.CacheEnabled {
		var err error
		m.cache, err = NewCache(config.CacheSize, config.CacheTTL, config.CacheCompress)
		if err != nil {
			return nil, api.NewConfigurationError("embedding.NewManager", "create cache", err)
		}
	}

	return m, nil
}

// RegisterProvider registers a new embedding provider.
func (m *Manager) RegisterProvider(name string, provider Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.providers[name]; exists {
		return api.NewEmbeddingProviderAlreadyRegisteredError("embedding.Manager.RegisterProvider", name)
	}

	m.providers[name] = provider
	return nil
}

// GetProvider returns a provider by name.
func (m *Manager) GetProvider(name string) (Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	provider, exists := m.providers[name]
	if !exists {
		return nil, ErrProviderNotFound
	}

	return provider, nil
}

// Embed generates an embedding using the configured provider.
func (m *Manager) Embed(ctx context.Context, text string) ([]float32, error) {
	// Check cache first
	if m.cache != nil {
		if cached, ok := m.cache.Get(text); ok {
			return cached, nil
		}
	}

	// Get provider
	provider, err := m.GetProvider(m.config.Provider)
	if err != nil {
		return nil, err
	}

	// Generate embedding
	embedding, err := provider.Embed(ctx, text)
	if err != nil {
		return nil, err
	}

	// Cache result
	if m.cache != nil {
		m.cache.Put(text, embedding)
	}

	return embedding, nil
}

// EmbedBatch generates embeddings for multiple texts.
func (m *Manager) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrInvalidInput
	}

	// Get provider
	provider, err := m.GetProvider(m.config.Provider)
	if err != nil {
		return nil, err
	}

	// Check cache for each text
	var uncachedTexts []string
	var uncachedIndices []int
	results := make([][]float32, len(texts))

	if m.cache != nil {
		for i, text := range texts {
			if cached, ok := m.cache.Get(text); ok {
				results[i] = cached
			} else {
				uncachedTexts = append(uncachedTexts, text)
				uncachedIndices = append(uncachedIndices, i)
			}
		}
	} else {
		uncachedTexts = texts
		uncachedIndices = make([]int, len(texts))
		for i := range texts {
			uncachedIndices[i] = i
		}
	}

	// Generate embeddings for uncached texts
	if len(uncachedTexts) > 0 {
		embeddings, err := provider.EmbedBatch(ctx, uncachedTexts)
		if err != nil {
			return nil, err
		}

		// Store in cache and results
		for i, embedding := range embeddings {
			idx := uncachedIndices[i]
			results[idx] = embedding

			if m.cache != nil {
				m.cache.Put(uncachedTexts[i], embedding)
			}
		}
	}

	return results, nil
}

// Dimensions returns the dimensions of embeddings from the current provider.
func (m *Manager) Dimensions() (int, error) {
	provider, err := m.GetProvider(m.config.Provider)
	if err != nil {
		return 0, err
	}
	return provider.Dimensions(), nil
}

// Close closes all providers and releases resources.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	for name, provider := range m.providers {
		if err := provider.Close(); err != nil {
			errs = append(errs, &api.EmbeddingCloseProviderError{Name: name, Cause: err})
		}
	}

	if m.cache != nil {
		m.cache.Clear()
	}

	if len(errs) > 0 {
		return api.NewEmbeddingCloseError("embedding.Manager.Close", errs)
	}

	return nil
}

// GetStats returns statistics about the manager.
func (m *Manager) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := Stats{
		ProvidersRegistered: len(m.providers),
		ProviderNames:       make([]string, 0, len(m.providers)),
		DefaultProvider:     m.config.Provider,
		CacheEnabled:        m.cache != nil,
	}

	for name := range m.providers {
		stats.ProviderNames = append(stats.ProviderNames, name)
	}

	if m.cache != nil {
		stats.CacheSize = m.cache.Size()
		stats.CacheHits = m.cache.Hits()
		stats.CacheMisses = m.cache.Misses()
		if stats.CacheHits+stats.CacheMisses > 0 {
			stats.CacheHitRate = float64(stats.CacheHits) / float64(stats.CacheHits+stats.CacheMisses)
		}
	}

	return stats
}

// Stats holds statistics about the embedding manager.
type Stats struct {
	ProvidersRegistered int
	ProviderNames       []string
	DefaultProvider     string
	CacheEnabled        bool
	CacheSize           int
	CacheHits           uint64
	CacheMisses         uint64
	CacheHitRate        float64
}
