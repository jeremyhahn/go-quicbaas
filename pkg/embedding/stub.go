//go:build !vector

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
	"time"
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

// Provider defines the embedding provider interface (stub for non-vector builds).
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

// DefaultConfig returns the default Config.
func DefaultConfig() Config {
	return Config{
		Provider:       "voyage",
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

// EmbeddingManagerInterface defines the interface that Manager implements.
type EmbeddingManagerInterface interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimensions() (int, error)
	GetProvider(name string) (Provider, error)
	GetStats() Stats
}

// Manager stub for non-vector builds.
type Manager struct{}

// NewManager returns an error since vector support is disabled.
func NewManager(_ Config) (*Manager, error) {
	return nil, ErrVectorNotEnabled
}

func (m *Manager) RegisterProvider(_ string, _ Provider) error { return ErrVectorNotEnabled }
func (m *Manager) GetProvider(_ string) (Provider, error)      { return nil, ErrVectorNotEnabled }
func (m *Manager) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrVectorNotEnabled
}
func (m *Manager) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrVectorNotEnabled
}
func (m *Manager) Dimensions() (int, error) { return 0, ErrVectorNotEnabled }
func (m *Manager) Close() error             { return ErrVectorNotEnabled }
func (m *Manager) GetStats() Stats          { return Stats{} }

// OllamaConfig holds configuration for the Ollama provider stub.
type OllamaConfig struct {
	BaseURL      string
	Model        string
	Dimensions   int
	Timeout      time.Duration
	MaxRetries   int
	RetryBackoff time.Duration
}

// DefaultOllamaConfig returns the default Ollama configuration.
func DefaultOllamaConfig() OllamaConfig {
	return OllamaConfig{
		BaseURL:      "http://localhost:11434",
		Model:        "nomic-embed-text",
		Timeout:      120 * time.Second,
		MaxRetries:   3,
		RetryBackoff: time.Second,
	}
}

// LMStudioConfig holds configuration for the LMStudio provider stub.
type LMStudioConfig struct {
	BaseURL        string
	Model          string
	Dimensions     int
	Timeout        int
	MaxRetries     int
	RetryBackoffMS int
}

// DefaultLMStudioConfig returns the default LMStudio configuration.
func DefaultLMStudioConfig() LMStudioConfig {
	return LMStudioConfig{
		BaseURL:        "http://localhost:1234",
		Model:          "nomic-ai/nomic-embed-text-v1.5-GGUF",
		Timeout:        60,
		MaxRetries:     3,
		RetryBackoffMS: 1000,
	}
}

// OpenAIProvider stub.
type OpenAIProvider struct{}

func NewOpenAIProvider(_ Config) (*OpenAIProvider, error) { return nil, ErrVectorNotEnabled }
func (p *OpenAIProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrVectorNotEnabled
}
func (p *OpenAIProvider) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrVectorNotEnabled
}
func (p *OpenAIProvider) Dimensions() int { return 0 }
func (p *OpenAIProvider) MaxTokens() int  { return 0 }
func (p *OpenAIProvider) Name() string    { return "openai" }
func (p *OpenAIProvider) Close() error    { return nil }

// OllamaProvider stub.
type OllamaProvider struct{}

func NewOllamaProvider(_ OllamaConfig) (*OllamaProvider, error) { return nil, ErrVectorNotEnabled }
func (p *OllamaProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrVectorNotEnabled
}
func (p *OllamaProvider) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrVectorNotEnabled
}
func (p *OllamaProvider) Dimensions() int { return 0 }
func (p *OllamaProvider) MaxTokens() int  { return 0 }
func (p *OllamaProvider) Name() string    { return "ollama" }
func (p *OllamaProvider) Close() error    { return nil }

// LMStudioProvider stub.
type LMStudioProvider struct{}

func NewLMStudioProvider(_ LMStudioConfig) (*LMStudioProvider, error) {
	return nil, ErrVectorNotEnabled
}
func (p *LMStudioProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrVectorNotEnabled
}
func (p *LMStudioProvider) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrVectorNotEnabled
}
func (p *LMStudioProvider) Dimensions() int { return 0 }
func (p *LMStudioProvider) MaxTokens() int  { return 0 }
func (p *LMStudioProvider) Name() string    { return "lmstudio" }
func (p *LMStudioProvider) Close() error    { return nil }

// VoyageProvider stub.
type VoyageProvider struct{}

func NewVoyageProvider(_ Config) (*VoyageProvider, error) { return nil, ErrVectorNotEnabled }
func (v *VoyageProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, ErrVectorNotEnabled
}
func (v *VoyageProvider) EmbedBatch(_ context.Context, _ []string) ([][]float32, error) {
	return nil, ErrVectorNotEnabled
}
func (v *VoyageProvider) Dimensions() int { return 0 }
func (v *VoyageProvider) MaxTokens() int  { return 0 }
func (v *VoyageProvider) Name() string    { return "voyage" }
func (v *VoyageProvider) Close() error    { return nil }
