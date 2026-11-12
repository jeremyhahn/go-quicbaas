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

//go:build integration && agents

package agents

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

	// Timeouts for readiness checks.
	healthCheckInterval = 2 * time.Second
	readyTimeout        = 3 * time.Minute
)

// testEnv holds the shared test environment state.
type testEnv struct {
	quicbaasURL       string
	ollamaURL         string
	quicbaasAvailable bool
	ollamaAvailable   bool
}

var env testEnv

// TestMain sets up the agent E2E integration test environment.
// It fails fast with os.Exit(1) if required services are not available.
func TestMain(m *testing.M) {
	env = testEnv{
		quicbaasURL: getEnvOrDefault("QUICBAAS_URL", defaultQuicBaaSURL),
		ollamaURL:   getEnvOrDefault("OLLAMA_URL", defaultOllamaURL),
	}

	// Verify QuicBaaS orchestrator is available - fail fast, no skipping.
	// The orchestrator health route is gated (orchestrator:health), so the
	// probe presents the tenant operator's bearer.
	orchestratorHealthURL := env.quicbaasURL + "/api/v1/orchestrator/health"
	if !waitForService(orchestratorHealthURL, operatorProbeClient) {
		fmt.Fprintf(os.Stderr, "FATAL: QuicBaaS orchestrator not available at %s after %s\n",
			orchestratorHealthURL, readyTimeout)
		releaseAndExit(1)
	}
	env.quicbaasAvailable = true
	fmt.Printf("QuicBaaS orchestrator is available at %s\n", env.quicbaasURL)

	// Verify Ollama is available - fail fast, no skipping. Ollama is another
	// service, so its probe never carries a QuicBaaS bearer.
	ollamaTagsURL := env.ollamaURL + "/api/tags"
	if !waitForService(ollamaTagsURL, plainProbeClient) {
		fmt.Fprintf(os.Stderr, "FATAL: Ollama service not available at %s after %s\n",
			ollamaTagsURL, readyTimeout)
		releaseAndExit(1)
	}
	env.ollamaAvailable = true
	fmt.Printf("Ollama service is available at %s\n", env.ollamaURL)

	releaseAndExit(m.Run())
}

// releaseAndExit releases the tenant the tenant operator was enrolled in and
// ends the process with code.
func releaseAndExit(code int) {
	if err := helpers.ReleaseDataPlaneTenants(); err != nil {
		fmt.Fprintf(os.Stderr, "release data-plane tenants: %v\n", err)
	}
	os.Exit(code)
}

// probeClient builds the HTTP client one readiness poll uses.
type probeClient func(ctx context.Context) (*http.Client, error)

// plainProbeClient presents no credential.
func plainProbeClient(context.Context) (*http.Client, error) {
	return helpers.NewHTTPClient(5 * time.Second), nil
}

// operatorProbeClient presents the tenant operator's bearer. Enrolling the
// operator needs the stack, so it fails until the stack is up and the next
// poll tries again.
func operatorProbeClient(ctx context.Context) (*http.Client, error) {
	return helpers.TenantOperatorHTTPClient(ctx, 5*time.Second)
}

// waitForService polls the given URL until it responds with HTTP 200 or the
// context deadline expires. Uses a ticker for polling instead of time.Sleep.
// newClient builds the client of each poll.
func waitForService(url string, newClient probeClient) bool {
	ctx, cancel := context.WithTimeout(context.Background(), readyTimeout)
	defer cancel()

	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			client, clientErr := newClient(ctx)
			if clientErr != nil {
				continue
			}
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
