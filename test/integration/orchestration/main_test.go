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

package orchestration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

const (
	// Default test configuration.
	defaultQuicBaaSURL = "http://localhost:8080"
	defaultOllamaURL   = "http://localhost:11434"
	defaultOllamaModel = "tinyllama"

	// Timeouts for readiness checks.
	healthCheckInterval = 2 * time.Second
	readyTimeout        = 60 * time.Second

	// Prompt timeout accounts for cold-start model loading.
	promptTimeout = 180 * time.Second
)

// testEnv holds the shared test environment state.
type testEnv struct {
	quicbaasURL       string
	ollamaURL         string
	ollamaModel       string
	quicbaasAvailable bool
	ollamaAvailable   bool
}

var env testEnv

// TestMain sets up and tears down the orchestration integration test environment.
func TestMain(m *testing.M) {
	env = testEnv{
		quicbaasURL: getEnvOrDefault("QUICBAAS_URL", defaultQuicBaaSURL),
		ollamaURL:   getEnvOrDefault("OLLAMA_URL", defaultOllamaURL),
		ollamaModel: getEnvOrDefault("OLLAMA_MODEL", defaultOllamaModel),
	}

	// Verify service availability - fail fast if services are not ready.
	// Use the standard health endpoint; the orchestrator is tested in-process.
	if !waitForService(env.quicbaasURL + "/api/v1/health/ready") {
		fmt.Fprintf(os.Stderr, "FATAL: QuicBaaS service not available at %s after %s\n", env.quicbaasURL, readyTimeout)
		os.Exit(1)
	}
	env.quicbaasAvailable = true
	fmt.Printf("QuicBaaS service is available at %s\n", env.quicbaasURL)

	if !waitForService(env.ollamaURL + "/api/tags") {
		fmt.Fprintf(os.Stderr, "FATAL: Ollama service not available at %s after %s\n", env.ollamaURL, readyTimeout)
		os.Exit(1)
	}
	env.ollamaAvailable = true
	fmt.Printf("Ollama service is available at %s (model: %s)\n", env.ollamaURL, env.ollamaModel)

	os.Exit(m.Run())
}

// waitForService polls the given URL until it responds with HTTP 200 or times out.
func waitForService(url string) bool {
	client := helpers.NewHTTPClient(5 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), readyTimeout)
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

// getEnvOrDefault returns the environment variable value or a default.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
