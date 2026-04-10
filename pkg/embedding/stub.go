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

//go:build !vector

package embedding

import (
	"context"
	"errors"
	"time"
)

// Common errors.
var (
	ErrProviderNotFound    = errors.New("embedding provider not found")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrRateLimitExceeded   = errors.New("rate limit exceeded")
	ErrInvalidInput        = errors.New("invalid input")
	ErrDimensionsMismatch  = errors.New("dimensions mismatch")
	ErrTokenLimitExceeded  = errors.New("token limit exceeded")
	ErrModelNotFound       = errors.New("model not found")
	ErrProviderUnavailable = errors.New("provider unavailable")
)

// OpenAI model constants.
const (
	DefaultOpenAIURL         = "https://api.openai.com"
	DefaultOpenAITimeout     = 30 * time.Second
	ModelTextEmbedding3Small = "text-embedding-3-small"
	ModelTextEmbedding3Large = "text-embedding-3-large"
	ModelTextEmbeddingAda002 = "text-embedding-ada-002"
)

// Voyage API constants.
const (
	VoyageAPIURL = "https://api.voyageai.com/v1/embeddings"
)

// Provider defines the interface for embedding providers.
type Provider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() int
	MaxTokens() int
	Name() string
	Close() error
}

// Metadata contains information about the embedding request.
type Metadata struct {
	Model      string
	Dimensions int
	Tokens     int
	Source     string
	Language   string
	Tags       map[string]string
}

// Result represents an embedding result with metadata.
type Result struct {
	Embedding []float32
	Metadata  Metadata
}

// BatchResult represents a batch embedding result.
type BatchResult struct {
	Embeddings [][]float32
	Metadata   []Metadata
}

// Config holds configuration for embedding providers.
type Config struct {
	Provider       string `yaml:"provider" json:"provider"`
	APIURL         string `yaml:"api_url" json:"api_url"`
	APIKey         string `yaml:"api_key" json:"api_key"`
	Model          string `yaml:"model" json:"model"`
	Dimensions     int    `yaml:"dimensions" json:"dimensions"`
	MaxBatchSize   int    `yaml:"max_batch_size" json:"max_batch_size"`
	Timeout        int    `yaml:"timeout_seconds" json:"timeout_seconds"`
	CacheEnabled   bool   `yaml:"cache_enabled" json:"cache_enabled"`
	CacheSize      int    `yaml:"cache_size" json:"cache_size"`
	CacheTTL       int    `yaml:"cache_ttl_seconds" json:"cache_ttl_seconds"`
	CacheCompress  bool   `yaml:"cache_compress" json:"cache_compress"`
	MaxRetries     int    `yaml:"max_retries" json:"max_retries"`
	RetryBackoffMS int    `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
}

// DefaultConfig returns default configuration.
func DefaultConfig() Config {
	return Config{
		Provider:       "voyage",
		APIURL:         "",
		APIKey:         "",
		Model:          "voyage-code-2",
		Dimensions:     1536,
		MaxBatchSize:   128,
		Timeout:        30,
		CacheEnabled:   true,
		CacheSize:      10000,
		CacheTTL:       3600,
		CacheCompress:  true,
		MaxRetries:     3,
		RetryBackoffMS: 1000,
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	return ErrVectorNotEnabled
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

// Manager manages multiple embedding providers with caching and fallbacks.
type Manager struct{}

// NewManager creates a new embedding manager.
func NewManager(_ Config) (*Manager, error) {
	return nil, ErrVectorNotEnabled
}

// RegisterProvider registers a new embedding provider.
func (m *Manager) RegisterProvider(_ string, _ Provider) error {
	return ErrVectorNotEnabled
}

// GetProvider returns a provider by name.
func (m *Manager) GetProvider(_ string) (Provider, error) {
	return nil, ErrVectorNotEnabled
}

// Embed generates an embedding using the configured provider.
func (m *Manager) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrVectorNotEnabled
}

// EmbedBatch generates embeddings for multiple texts.
func (m *Manager) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrVectorNotEnabled
}

// Dimensions returns the dimensions of embeddings from the current provider.
func (m *Manager) Dimensions() (int, error) {
	return 0, ErrVectorNotEnabled
}

// Close closes all providers and releases resources.
func (m *Manager) Close() error {
	return ErrVectorNotEnabled
}

// GetStats returns statistics about the manager.
func (m *Manager) GetStats() Stats {
	return Stats{}
}

// Cache is an LRU cache for embeddings with optional compression.
type Cache struct{}

// NewCache creates a new LRU cache.
func NewCache(_, _ int, _ bool) (*Cache, error) {
	return nil, ErrVectorNotEnabled
}

// Get retrieves an embedding from the cache.
func (c *Cache) Get(_ string) ([]float32, bool) {
	return nil, false
}

// Put stores an embedding in the cache.
func (c *Cache) Put(_ string, _ []float32) {}

// Size returns the current number of items in the cache.
func (c *Cache) Size() int {
	return 0
}

// Hits returns the number of cache hits.
func (c *Cache) Hits() uint64 {
	return 0
}

// Misses returns the number of cache misses.
func (c *Cache) Misses() uint64 {
	return 0
}

// Evictions returns the number of evictions.
func (c *Cache) Evictions() uint64 {
	return 0
}

// Clear removes all items from the cache.
func (c *Cache) Clear() {}

// OpenAIProvider implements the Provider interface for OpenAI-compatible embeddings.
type OpenAIProvider struct{}

// NewOpenAIProvider creates a new OpenAI-compatible embedding provider.
func NewOpenAIProvider(_ Config) (*OpenAIProvider, error) {
	return nil, ErrVectorNotEnabled
}

// Embed generates an embedding for a single text.
func (p *OpenAIProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrVectorNotEnabled
}

// EmbedBatch generates embeddings for multiple texts in a single request.
func (p *OpenAIProvider) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrVectorNotEnabled
}

// Dimensions returns the dimensionality of embeddings produced by this provider.
func (p *OpenAIProvider) Dimensions() int {
	return 0
}

// MaxTokens returns the maximum number of tokens this provider can handle.
func (p *OpenAIProvider) MaxTokens() int {
	return 0
}

// Name returns the name of this provider.
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Close closes the provider and releases resources.
func (p *OpenAIProvider) Close() error {
	return nil
}

// VoyageProvider implements the Provider interface for Voyage AI.
type VoyageProvider struct{}

// NewVoyageProvider creates a new Voyage AI embedding provider.
func NewVoyageProvider(_ Config) (*VoyageProvider, error) {
	return nil, ErrVectorNotEnabled
}

// Embed generates an embedding for a single text.
func (v *VoyageProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrVectorNotEnabled
}

// EmbedBatch generates embeddings for multiple texts in a single request.
func (v *VoyageProvider) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrVectorNotEnabled
}

// Dimensions returns the dimensionality of embeddings produced by this provider.
func (v *VoyageProvider) Dimensions() int {
	return 0
}

// MaxTokens returns the maximum number of tokens this provider can handle.
func (v *VoyageProvider) MaxTokens() int {
	return 0
}

// Name returns the name of this provider.
func (v *VoyageProvider) Name() string {
	return "voyage"
}

// Close closes the provider and releases resources.
func (v *VoyageProvider) Close() error {
	return nil
}
