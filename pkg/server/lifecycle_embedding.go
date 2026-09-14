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
	"context"
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

	// Preflight the provider name. go-qrdb's validateManagerConfig accepts only
	// its four built-in providers as a manager's *configured default*, so a
	// provider outside that set makes NewManager fail -- and the generic
	// warning below would report it as if the backend were merely unavailable,
	// sending an operator to debug a sidecar that is running fine. Name the
	// real cause instead.
	if _, ok := qrdbManagerDefaultProviders[embeddingCfg.Provider]; !ok {
		s.logger.WithFields(map[string]interface{}{
			"provider":          embeddingCfg.Provider,
			"accepted_defaults": "openai, ollama, lmstudio, voyage",
		}).Error("Embedding provider cannot be the configured default, so embedding support is disabled. " +
			"Set rag.embedding.provider to an accepted provider and select this one per request " +
			"or through SetDefaultProvider. Tracked by go-qrdb#528.")
		return nil
	}

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

	// Register the extra providers first, so that one failing does not stop the
	// configured default from registering.
	s.registerAdditionalEmbeddingProviders(mgr)

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

// qrdbManagerDefaultProviders mirrors go-qrdb's validateManagerConfig allowlist
// (go-qrdb pkg/embedding/manager.go:83): the provider names it accepts as an
// embedding manager's *configured default*.
//
// RegisterProvider itself applies no such allowlist, so a provider outside this
// set -- the NPU sidecar, for one -- registers and can be selected per request
// or via SetDefaultProvider. Only naming it in configuration is blocked.
// go-qrdb#528 asks for this to be validated against the registry instead.
var qrdbManagerDefaultProviders = map[string]struct{}{
	"openai":   {},
	"ollama":   {},
	"lmstudio": {},
	"voyage":   {},
}

// embeddingProviderFactory builds an embedding provider from the resolved
// embedding configuration.
type embeddingProviderFactory func(ctx context.Context, cfg *embedding.Config) (embedding.Provider, error)

// embeddingProviderFactories maps a configured provider name to its constructor.
//
// Map-based dispatch rather than a switch: lookup is O(1) and adding a provider
// is a single entry plus a constructor, with no shared error-handling code to
// duplicate per arm.
var embeddingProviderFactories = map[string]embeddingProviderFactory{
	"ollama":                  newOllamaEmbeddingProvider,
	"voyage":                  newVoyageEmbeddingProvider,
	"openai":                  newOpenAIEmbeddingProvider,
	"lmstudio":                newLMStudioEmbeddingProvider,
	embedding.NPUProviderName: newNPUEmbeddingProvider,
}

// fallbackEmbeddingDimensions is the vector width given to an additional
// provider entry that states none and whose provider has no fixed width of its
// own. It suits the text providers, where the width follows whichever model the
// operator names and 768 is the common case.
const fallbackEmbeddingDimensions = 768

// embeddingProviderNativeDimensions records the providers whose vector width is
// a property of the provider rather than a choice the operator makes, so an
// entry that omits `dimensions` gets a width that can actually work.
//
// Only the NPU qualifies today: it serves one exported model,
// all-MiniLM-L6-v2, and cross-checks the width the sidecar declares against its
// own configured width, rejecting every response that disagrees. Handing it the
// generic 768 therefore does not produce a merely suboptimal provider, it
// produces one that fails closed on every request.
var embeddingProviderNativeDimensions = map[string]int{
	embedding.NPUProviderName: embedding.DefaultNPUDimensions,
}

// defaultEmbeddingDimensionsFor returns the width to apply to an entry that
// omits one. An explicitly configured width always wins; this only fills a gap.
func defaultEmbeddingDimensionsFor(provider string) int {
	if dimensions, ok := embeddingProviderNativeDimensions[provider]; ok {
		return dimensions
	}
	return fallbackEmbeddingDimensions
}

// newOllamaEmbeddingProvider builds the Ollama provider, which remains the
// default embedding backend.
func newOllamaEmbeddingProvider(_ context.Context, cfg *embedding.Config) (embedding.Provider, error) {
	return embedding.NewOllamaProvider(embedding.OllamaConfig{
		BaseURL:      cfg.APIURL,
		Model:        cfg.Model,
		Dimensions:   cfg.Dimensions,
		Timeout:      time.Duration(cfg.Timeout) * time.Second,
		MaxRetries:   cfg.MaxRetries,
		RetryBackoff: time.Duration(cfg.RetryBackoffMS) * time.Millisecond,
	})
}

// newVoyageEmbeddingProvider builds the Voyage AI provider.
func newVoyageEmbeddingProvider(_ context.Context, cfg *embedding.Config) (embedding.Provider, error) {
	return embedding.NewVoyageProvider(*cfg)
}

// newOpenAIEmbeddingProvider builds the OpenAI provider.
func newOpenAIEmbeddingProvider(_ context.Context, cfg *embedding.Config) (embedding.Provider, error) {
	return embedding.NewOpenAIProvider(*cfg)
}

// newLMStudioEmbeddingProvider builds the LM Studio provider.
func newLMStudioEmbeddingProvider(_ context.Context, cfg *embedding.Config) (embedding.Provider, error) {
	return embedding.NewLMStudioProvider(embedding.LMStudioConfig{
		BaseURL:        cfg.APIURL,
		Model:          cfg.Model,
		Dimensions:     cfg.Dimensions,
		Timeout:        cfg.Timeout,
		MaxRetries:     cfg.MaxRetries,
		RetryBackoffMS: cfg.RetryBackoffMS,
	})
}

// newNPUEmbeddingProvider builds the NPU provider, which reaches an OpenVINO
// sidecar over HTTP.
//
// The sidecar owns the device, so nothing here links a vendor runtime and the
// NPU may live on a different host from the daemon. APIURL is the sidecar
// endpoint; when it is empty the provider's own default applies.
func newNPUEmbeddingProvider(ctx context.Context, cfg *embedding.Config) (embedding.Provider, error) {
	npuCfg := embedding.DefaultNPUConfig()
	if cfg.APIURL != "" {
		npuCfg.BaseURL = cfg.APIURL
	}
	if cfg.Model != "" {
		npuCfg.Model = cfg.Model
	}
	if cfg.Dimensions > 0 {
		npuCfg.Dimensions = cfg.Dimensions
	}
	if cfg.Timeout > 0 {
		npuCfg.Timeout = time.Duration(cfg.Timeout) * time.Second
	}
	if cfg.MaxBatchSize > 0 {
		npuCfg.MaxBatchSize = cfg.MaxBatchSize
	}
	// A plain key in configuration is supported for parity with the other
	// providers; an inference.SecretRef is the preferred path and is set by
	// callers constructing the provider directly.
	npuCfg.APIKey = cfg.APIKey

	return embedding.NewNPUProvider(ctx, npuCfg)
}

// registerAdditionalEmbeddingProviders registers every provider listed under
// rag.embedding.additional.
//
// This is the path by which a provider go-qrdb will not accept as a manager
// default -- "npu", served by the OpenVINO sidecar -- becomes registered and
// therefore selectable per request or promotable to the service default. Without
// it the provider would exist in the binary and be unreachable at runtime.
//
// A failure here is logged and skipped rather than fatal: an unreachable extra
// provider must not take down the embedding subsystem that the default provider
// is serving perfectly well.
func (s *Server) registerAdditionalEmbeddingProviders(mgr *embedding.Manager) {
	for _, extra := range s.buildAdditionalEmbeddingConfigs() {
		if err := s.registerEmbeddingProviders(mgr, extra); err != nil {
			s.logger.WithFields(map[string]interface{}{
				"error":    err.Error(),
				"provider": extra.Provider,
			}).Warn("Failed to register an additional embedding provider - continuing without it")
			continue
		}
		s.logger.WithFields(map[string]interface{}{
			"provider": extra.Provider,
			"api_url":  extra.APIURL,
		}).Info("Registered an additional embedding provider")
	}
}

// buildAdditionalEmbeddingConfigs maps rag.embedding.additional onto the
// embedding package's config type, filling in defaults so an entry only has to
// name what differs. The vector width defaults per provider rather than
// globally -- see embeddingProviderNativeDimensions.
func (s *Server) buildAdditionalEmbeddingConfigs() []*embedding.Config {
	if !s.config.RAG.Enabled || len(s.config.RAG.Embedding.Additional) == 0 {
		return nil
	}

	out := make([]*embedding.Config, 0, len(s.config.RAG.Embedding.Additional))
	for _, extra := range s.config.RAG.Embedding.Additional {
		if extra.Provider == "" {
			s.logger.Warn("Skipping an embedding.additional entry with no provider name")
			continue
		}

		timeout := extra.Timeout
		if timeout <= 0 {
			timeout = 120
		}
		dimensions := extra.Dimensions
		if dimensions <= 0 {
			dimensions = defaultEmbeddingDimensionsFor(extra.Provider)
		}

		out = append(out, &embedding.Config{
			Provider:       extra.Provider,
			APIURL:         extra.APIURL,
			APIKey:         extra.APIKey,
			Model:          extra.Model,
			Dimensions:     dimensions,
			Timeout:        timeout,
			MaxBatchSize:   128,
			CacheEnabled:   true,
			CacheSize:      10000,
			CacheTTL:       3600,
			CacheCompress:  true,
			MaxRetries:     3,
			RetryBackoffMS: 1000,
		})
	}
	return out
}

// registerEmbeddingProviders registers the configured embedding provider with
// the manager.
func (s *Server) registerEmbeddingProviders(mgr *embedding.Manager, cfg *embedding.Config) error {
	factory, ok := embeddingProviderFactories[cfg.Provider]
	if !ok {
		return &api.UnknownProviderError{Provider: cfg.Provider}
	}

	// Only credential resolution consults the context, and no resolver is wired
	// at server start, so a background context is sufficient here.
	provider, err := factory(context.Background(), cfg)
	if err != nil {
		s.logger.WithFields(map[string]interface{}{
			"error":    err.Error(),
			"provider": cfg.Provider,
		}).Warn("Failed to create embedding provider")
		return &api.OperationFailedError{
			Operation: "create " + cfg.Provider + " provider",
			Cause:     err,
		}
	}

	if err := mgr.RegisterProvider(cfg.Provider, provider); err != nil {
		return &api.OperationFailedError{
			Operation: "register " + cfg.Provider + " provider",
			Cause:     err,
		}
	}

	return nil
}
