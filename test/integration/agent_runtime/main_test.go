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

//go:build integration && agent_runtime && legacy_enrollment_token

package agent_runtime_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

const (
	// defaultCentralURL is the mock central server used in lifecycle tests.
	// Docker Compose maps the central QuicBaaS service here.
	defaultCentralURL = "http://localhost:8080"

	// defaultOllamaURL is the Ollama model server used in backend tests.
	// qwen3:1.7b is the fastest model for integration verification.
	defaultOllamaURL = "http://localhost:11434"

	// defaultOllamaModel is the expected Ollama model. qwen3:1.7b is the
	// fastest available per the QuicBaaS remote agent plan.
	defaultOllamaModel = "qwen3:1.7b"

	// healthCheckInterval is the polling interval used while waiting for
	// a service to become ready.
	healthCheckInterval = 2 * time.Second

	// serviceReadyTimeout is the maximum time to wait for a service to
	// respond before failing.
	serviceReadyTimeout = 60 * time.Second

	// promptTimeout is the per-test deadline for operations that involve
	// a model inference call (cold-start model loading included).
	promptTimeout = 120 * time.Second
)

// testEnv holds shared state initialised in TestMain. Fields are
// read-only after TestMain returns.
type testEnv struct {
	centralURL   string
	ollamaURL    string
	ollamaModel  string
	centralAvail bool
	ollamaAvail  bool
}

// env is the package-level test environment populated by TestMain.
var env testEnv

// TestMain initialises the agent_runtime integration test environment.
// It probes each required service and records availability so individual
// tests can decide to skip or fail-fast accordingly.
func TestMain(m *testing.M) {
	env = testEnv{
		centralURL:  getEnvOrDefault("QUICBAAS_URL", defaultCentralURL),
		ollamaURL:   getEnvOrDefault("OLLAMA_URL", defaultOllamaURL),
		ollamaModel: getEnvOrDefault("OLLAMA_MODEL", defaultOllamaModel),
	}

	if waitForHTTP(env.centralURL+"/api/v1/health/ready", serviceReadyTimeout) {
		env.centralAvail = true
		fmt.Printf("[agent_runtime] central QuicBaaS available at %s\n", env.centralURL)
	} else {
		fmt.Fprintf(os.Stderr, "[agent_runtime] WARNING: central QuicBaaS not available at %s — daemon lifecycle tests will use in-process mock server\n", env.centralURL)
	}

	if waitForHTTP(env.ollamaURL+"/api/tags", serviceReadyTimeout) {
		env.ollamaAvail = true
		fmt.Printf("[agent_runtime] Ollama available at %s (model=%s)\n", env.ollamaURL, env.ollamaModel)
	} else {
		fmt.Printf("[agent_runtime] Ollama not available at %s — Ollama backend tests will be skipped\n", env.ollamaURL)
	}

	os.Exit(m.Run())
}

// waitForHTTP polls url until it returns HTTP 200 or the deadline expires.
// Uses channels+select to avoid time.Sleep (per project conventions).
func waitForHTTP(url string, timeout time.Duration) bool {
	client := &http.Client{Timeout: 5 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			resp, err := client.Get(url) //nolint:noctx // context carried through deadline
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return true
				}
			}
		}
	}
}

// getEnvOrDefault returns the environment variable value or defaultVal.
func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
