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

//go:build integration && rag

package rag

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
)

const (
	// Default test configuration
	defaultOllamaURL       = "http://localhost:11434"
	defaultOllamaModel     = "all-minilm"
	defaultEmbedDimensions = 384

	// Timeouts
	clusterReadyTimeout = 3 * time.Minute
	ollamaReadyTimeout  = 3 * time.Minute // Ollama cold-starts model on first inference
	healthCheckInterval = 2 * time.Second
)

// testEnv holds the test environment state.
type testEnv struct {
	ollamaURL        string
	ollamaModel      string
	embedDimensions  int
	ollamaAvailable  bool
	clusterAvailable bool
	restEndpoint     string
}

var env testEnv

// TestMain sets up and tears down the test environment.
func TestMain(m *testing.M) {
	// Initialize test environment
	env = testEnv{
		ollamaURL:       getEnvOrDefault("OLLAMA_URL", defaultOllamaURL),
		ollamaModel:     getEnvOrDefault("OLLAMA_MODEL", defaultOllamaModel),
		embedDimensions: defaultEmbedDimensions,
		restEndpoint:    getEnvOrDefault("QUICBAAS_NODE1_URL", "https://test-node1:8080"),
	}

	// Wait for cluster to be ready
	if !waitForClusterReady() {
		fmt.Fprintf(os.Stderr, "FATAL: QuicBaaS cluster not ready at %s after %s\n", env.restEndpoint, clusterReadyTimeout)
		os.Exit(1)
	}
	env.clusterAvailable = true
	fmt.Println("QuicBaaS cluster is ready")

	// Check Ollama availability and detect dimensions
	dims := checkOllamaAvailable()
	if dims <= 0 {
		fmt.Fprintf(os.Stderr, "FATAL: Ollama service not available at %s after %s\n", env.ollamaURL, ollamaReadyTimeout)
		os.Exit(1)
	}
	env.ollamaAvailable = true
	env.embedDimensions = dims
	fmt.Printf("Ollama service is available at %s (dimensions: %d)\n", env.ollamaURL, dims)

	// Run tests
	code := m.Run()

	// Exit with test result code
	os.Exit(code)
}

// waitForClusterReady waits for the QuicBaaS cluster to be ready.
func waitForClusterReady() bool {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
	url := env.restEndpoint + "/api/v1/health/ready"

	ctx, cancel := context.WithTimeout(context.Background(), clusterReadyTimeout)
	defer cancel()

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			resp, err := client.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return true
				}
			}
		}
	}
}

// checkOllamaAvailable checks if Ollama service is available and responsive.
// Returns the detected embedding dimensions, or 0 if unavailable.
func checkOllamaAvailable() int {
	config := embedding.Config{
		Provider:   "openai",
		APIURL:     env.ollamaURL,
		Model:      env.ollamaModel,
		Dimensions: 0, // Auto-detect from first response
		Timeout:    30,
		MaxRetries: 1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), ollamaReadyTimeout)
	defer cancel()

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
			provider, err := embedding.NewOpenAIProvider(config)
			if err != nil {
				fmt.Printf("  Ollama check: provider error: %v\n", err)
				continue
			}

			embedCtx, embedCancel := context.WithTimeout(context.Background(), 60*time.Second)
			emb, err := provider.Embed(embedCtx, "test")
			embedCancel()
			dims := provider.Dimensions()
			provider.Close()

			if err == nil {
				if dims == 0 && len(emb) > 0 {
					dims = len(emb)
				}
				return dims
			}
			fmt.Printf("  Ollama check: embed error: %v\n", err)
		}
	}
}

// getEnvOrDefault returns the environment variable value or a default.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// requireOllama fails the test if Ollama is not available.
func requireOllama(t *testing.T) {
	t.Helper()
	require.True(t, env.ollamaAvailable,
		"Ollama service must be available at %s", env.ollamaURL)
}

// requireCluster fails the test if the cluster is not available.
func requireCluster(t *testing.T) {
	t.Helper()
	require.True(t, env.clusterAvailable,
		"QuicBaaS cluster must be available at %s", env.restEndpoint)
}

// getTestEmbeddingConfig returns embedding configuration for tests.
func getTestEmbeddingConfig() embedding.Config {
	return embedding.Config{
		Provider:     "openai",
		APIURL:       env.ollamaURL,
		Model:        env.ollamaModel,
		Dimensions:   0, // Auto-detect (Ollama OpenAI-compat API may return different dims than native)
		MaxBatchSize: 32,
		Timeout:      120, // Increased for integration tests with real Ollama
		MaxRetries:   3,
	}
}

// getRESTEndpoint returns the primary REST API endpoint.
func getRESTEndpoint() string {
	return env.restEndpoint
}
