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

//go:build vector

package server

import (
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
)

// startEmbedding initializes the embedding manager if embedding configuration
// is available. The embedding manager provides vector embedding generation
// capabilities via configurable providers (ollama, voyage, openai, lmstudio).
func (s *Server) startEmbedding() error {
	// Build embedding config from RAG embedding settings or Ollama config.
	// The embedding manager uses its own Config type, so we map from
	// the server-level configuration.
	embeddingCfg := s.buildEmbeddingConfig()
	if embeddingCfg == nil {
		s.logger.Info("Embedding manager is disabled, skipping initialization")
		return nil
	}

	s.logger.Info("Initializing embedding manager...")

	// Create embedding manager
	mgr, err := embedding.NewManager(*embeddingCfg)
	if err != nil {
		// Embedding is optional - if initialization fails (e.g., provider unavailable),
		// log a warning and continue without embedding support instead of failing
		// the entire server.
		s.logger.WithFields(map[string]interface{}{
			"error":    err.Error(),
			"provider": embeddingCfg.Provider,
		}).Warn("Embedding manager initialization failed - continuing without embedding support. " +
			"Fix configuration and restart server to enable embedding features.")
		return nil
	}

	// Register providers based on configuration
	if err := s.registerEmbeddingProviders(mgr, embeddingCfg); err != nil {
		s.logger.WithFields(map[string]interface{}{
			"error": err.Error(),
		}).Warn("Failed to register some embedding providers - continuing with available providers")
	}

	s.embeddingManager = mgr
	s.logger.WithFields(map[string]interface{}{
		"provider": embeddingCfg.Provider,
		"model":    embeddingCfg.Model,
	}).Info("Embedding manager initialized successfully")

	return nil
}

// stopEmbedding gracefully shuts down the embedding manager.
func (s *Server) stopEmbedding() error {
	if s.embeddingManager == nil {
		return nil
	}

	s.logger.Debug("Closing embedding manager...")
	if err := s.embeddingManager.Close(); err != nil {
		return api.NewOperationFailedError("close embedding manager", err)
	}

	s.embeddingManager = nil
	s.logger.Debug("Embedding manager closed")
	return nil
}

// buildEmbeddingConfig constructs an embedding.Config from the server configuration.
// It checks RAG embedding settings first, then falls back to Ollama config.
// Returns nil if no embedding configuration is available.
func (s *Server) buildEmbeddingConfig() *embedding.Config {
	// Check RAG embedding configuration first
	if s.config.RAG.Enabled && s.config.RAG.Embedding.Provider != "" {
		timeout := s.config.RAG.Embedding.Timeout
		if timeout <= 0 {
			timeout = 120
		}
		dimensions := s.config.RAG.Embedding.Dimensions
		if dimensions <= 0 {
			dimensions = 768
		}
		return &embedding.Config{
			Provider:       s.config.RAG.Embedding.Provider,
			APIURL:         s.config.RAG.Embedding.APIURL,
			APIKey:         s.config.RAG.Embedding.APIKey,
			Model:          s.config.RAG.Embedding.Model,
			Dimensions:     dimensions,
			Timeout:        timeout,
			MaxBatchSize:   128,
			CacheEnabled:   true,
			CacheSize:      10000,
			CacheTTL:       3600,
			CacheCompress:  true,
			MaxRetries:     3,
			RetryBackoffMS: 1000,
		}
	}

	// Fall back to Ollama configuration
	if s.config.Ollama.Enabled {
		// Merge with defaults to fill in zero-value fields (Timeout, MaxRetries, etc.)
		ollamaCfg := s.config.Ollama.MergeDefaults()

		timeout := int(ollamaCfg.Timeout.Seconds())
		if timeout <= 0 {
			timeout = 120
		}
		maxRetries := ollamaCfg.MaxRetries
		if maxRetries <= 0 {
			maxRetries = 3
		}
		retryBackoffMS := int(ollamaCfg.RetryBackoff.Milliseconds())
		if retryBackoffMS <= 0 {
			retryBackoffMS = 1000
		}

		return &embedding.Config{
			Provider:       "ollama",
			APIURL:         ollamaCfg.BaseURL,
			Model:          ollamaCfg.DefaultEmbeddingModel,
			Dimensions:     768, // Default; OllamaProvider auto-adjusts for known models
			MaxBatchSize:   128,
			Timeout:        timeout,
			CacheEnabled:   true,
			CacheSize:      10000,
			CacheTTL:       3600,
			CacheCompress:  true,
			MaxRetries:     maxRetries,
			RetryBackoffMS: retryBackoffMS,
		}
	}

	// No embedding configuration available - create a default config with
	// Ollama provider so embedding is always available when vector build tag is set.
	// This allows the embedding service to be available even without explicit
	// RAG or Ollama configuration.
	return &embedding.Config{
		Provider:       "ollama",
		APIURL:         "http://localhost:11434",
		Model:          "nomic-embed-text",
		Dimensions:     768,
		MaxBatchSize:   128,
		Timeout:        120,
		CacheEnabled:   true,
		CacheSize:      10000,
		CacheTTL:       3600,
		CacheCompress:  true,
		MaxRetries:     3,
		RetryBackoffMS: 1000,
	}
}

// registerEmbeddingProviders registers available embedding providers with the manager.
func (s *Server) registerEmbeddingProviders(mgr *embedding.Manager, cfg *embedding.Config) error {
	var lastErr error

	switch cfg.Provider {
	case "ollama":
		ollamaCfg := embedding.OllamaConfig{
			BaseURL:      cfg.APIURL,
			Model:        cfg.Model,
			Dimensions:   cfg.Dimensions,
			Timeout:      time.Duration(cfg.Timeout) * time.Second,
			MaxRetries:   cfg.MaxRetries,
			RetryBackoff: time.Duration(cfg.RetryBackoffMS) * time.Millisecond,
		}
		provider, err := embedding.NewOllamaProvider(ollamaCfg)
		if err != nil {
			lastErr = &api.OperationFailedError{Operation: "create ollama provider", Cause: err}
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Warn("Failed to register ollama embedding provider")
		} else {
			if err := mgr.RegisterProvider("ollama", provider); err != nil {
				lastErr = &api.OperationFailedError{Operation: "register ollama provider", Cause: err}
			}
		}

	case "voyage":
		provider, err := embedding.NewVoyageProvider(*cfg)
		if err != nil {
			lastErr = &api.OperationFailedError{Operation: "create voyage provider", Cause: err}
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Warn("Failed to register voyage embedding provider")
		} else {
			if err := mgr.RegisterProvider("voyage", provider); err != nil {
				lastErr = &api.OperationFailedError{Operation: "register voyage provider", Cause: err}
			}
		}

	case "openai":
		provider, err := embedding.NewOpenAIProvider(*cfg)
		if err != nil {
			lastErr = &api.OperationFailedError{Operation: "create openai provider", Cause: err}
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Warn("Failed to register openai embedding provider")
		} else {
			if err := mgr.RegisterProvider("openai", provider); err != nil {
				lastErr = &api.OperationFailedError{Operation: "register openai provider", Cause: err}
			}
		}

	case "lmstudio":
		lmstudioCfg := embedding.LMStudioConfig{
			BaseURL:        cfg.APIURL,
			Model:          cfg.Model,
			Dimensions:     cfg.Dimensions,
			Timeout:        cfg.Timeout,
			MaxRetries:     cfg.MaxRetries,
			RetryBackoffMS: cfg.RetryBackoffMS,
		}
		provider, err := embedding.NewLMStudioProvider(lmstudioCfg)
		if err != nil {
			lastErr = &api.OperationFailedError{Operation: "create lmstudio provider", Cause: err}
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Warn("Failed to register lmstudio embedding provider")
		} else {
			if err := mgr.RegisterProvider("lmstudio", provider); err != nil {
				lastErr = &api.OperationFailedError{Operation: "register lmstudio provider", Cause: err}
			}
		}

	default:
		lastErr = &api.UnknownProviderError{Provider: cfg.Provider}
	}

	return lastErr
}
