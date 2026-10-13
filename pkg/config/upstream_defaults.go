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

package config

import (
	"errors"
	"strconv"
	"strings"
)

// Inference upstream defaults. Each provider's default endpoint is stated
// here, once, as the defaults-layer value; every other layer (the RAG
// factory, the server wiring, the embedded fleet node, the CLI flag
// defaults) reads it rather than restating it. An operator overrides any of
// them through the config file, the environment or a flag.
const (
	// DefaultOllamaBaseURL is the Ollama API root: the local server.
	DefaultOllamaBaseURL = "http://localhost:11434"

	// DefaultOpenAIBaseURL is the OpenAI API root.
	DefaultOpenAIBaseURL = "https://api.openai.com"

	// DefaultVoyageEmbeddingsURL is the Voyage AI embeddings endpoint.
	DefaultVoyageEmbeddingsURL = "https://api.voyageai.com/v1/embeddings"

	// DefaultLMStudioBaseURL is the LM Studio API root: the local server.
	DefaultLMStudioBaseURL = "http://localhost:1234"
)

// defaultEmbeddingAPIURLs maps an embedding provider to its defaults-layer
// endpoint. A provider absent from the map has no default and must be given
// an api_url explicitly.
var defaultEmbeddingAPIURLs = map[string]string{
	"ollama":   DefaultOllamaBaseURL,
	"openai":   DefaultOpenAIBaseURL,
	"voyage":   DefaultVoyageEmbeddingsURL,
	"lmstudio": DefaultLMStudioBaseURL,
}

// DefaultEmbeddingAPIURL returns the defaults-layer endpoint for an embedding
// provider, or "" when the provider has none.
func DefaultEmbeddingAPIURL(provider string) string {
	return defaultEmbeddingAPIURLs[provider]
}

// ResolveAPIURL returns the endpoint the embedding provider dials: the
// configured api_url when set, otherwise the provider's defaults-layer
// endpoint. A provider with neither is an UpstreamEndpointMissingError, so a
// missing endpoint surfaces at the point of use instead of dialling a guess.
func (c *EmbeddingConfig) ResolveAPIURL() (string, error) {
	if url := strings.TrimSpace(c.APIURL); url != "" {
		return url, nil
	}
	if url := DefaultEmbeddingAPIURL(c.Provider); url != "" {
		return url, nil
	}
	return "", &UpstreamEndpointMissingError{Key: "rag.embedding.api_url", Provider: c.Provider}
}

// ErrUpstreamEndpointMissing is the sentinel matched by errors.Is when an
// upstream endpoint resolves empty after every configuration layer.
var ErrUpstreamEndpointMissing = errors.New("config: upstream endpoint is not configured")

// UpstreamEndpointMissingError names the configuration key that resolved
// empty and the provider that needed it.
type UpstreamEndpointMissingError struct {
	Key      string
	Provider string
}

// Error implements the error interface.
func (e *UpstreamEndpointMissingError) Error() string {
	return ErrUpstreamEndpointMissing.Error() + ": set " + e.Key + " for provider " + strconv.Quote(e.Provider)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UpstreamEndpointMissingError) Unwrap() error {
	return ErrUpstreamEndpointMissing
}
