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

package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

const (
	// Default test configuration.
	defaultOllamaURL   = "http://localhost:11434"
	defaultOllamaModel = "tinyllama"

	// Timeouts for Ollama readiness checks.
	ollamaReadyTimeout  = 120 * time.Second
	healthCheckInterval = 3 * time.Second

	// Prompt timeout accounts for cold-start model loading.
	promptTimeout = 180 * time.Second
)

// testEnv holds the shared test environment state.
type testEnv struct {
	ollamaURL       string
	ollamaModel     string
	ollamaAvailable bool
}

var env testEnv

// TestMain sets up and tears down the test environment.
func TestMain(m *testing.M) {
	env = testEnv{
		ollamaURL:   getEnvOrDefault("OLLAMA_URL", defaultOllamaURL),
		ollamaModel: getEnvOrDefault("OLLAMA_MODEL", defaultOllamaModel),
	}

	// Check Ollama availability with retry loop.
	if waitForOllama() {
		env.ollamaAvailable = true
		fmt.Printf("Ollama service is available at %s (model: %s)\n", env.ollamaURL, env.ollamaModel)
	} else {
		fmt.Printf("WARNING: Ollama service not available at %s, agent integration tests will be skipped\n", env.ollamaURL)
	}

	os.Exit(m.Run())
}

// waitForOllama polls the Ollama /api/tags endpoint until it responds or times out.
func waitForOllama() bool {
	client := &http.Client{Timeout: 5 * time.Second}
	url := env.ollamaURL + "/api/tags"

	ctx, cancel := context.WithTimeout(context.Background(), ollamaReadyTimeout)
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
