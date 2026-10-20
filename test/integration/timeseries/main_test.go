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

//go:build integration

package timeseries

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestMain sets up and tears down the test environment.
func TestMain(m *testing.M) {
	// Wait for cluster to be ready
	if !waitForClusterReady() {
		fmt.Println("Cluster not ready, exiting")
		os.Exit(1)
	}

	// Boot embedded server for multi-protocol tests
	_, srv, err := helpers.SetupEmbeddedServer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to start embedded server: %v\n", err)
	}

	// Run tests
	code := m.Run()

	// Cleanup
	if srv != nil {
		helpers.TeardownEmbeddedServer(srv)
	}
	os.Exit(code)
}

// waitForClusterReady waits for the QuicBaaS cluster to be ready.
func waitForClusterReady() bool {
	endpoint := getRESTEndpoint()
	client := helpers.NewHTTPClient(5 * time.Second)

	ready := pollUntil(context.Background(), 3*time.Minute, 250*time.Millisecond, func() bool {
		resp, err := client.Get(endpoint + "/api/v1/health/ready")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
	if ready {
		fmt.Println("Cluster is ready for timeseries tests")
	}
	return ready
}

// getRESTEndpoint returns the primary REST API endpoint.
func getRESTEndpoint() string {
	if endpoint := os.Getenv("QUICBAAS_NODE1_URL"); endpoint != "" {
		return endpoint
	}
	return "https://test-node1:8080"
}

// getRESTEndpoints returns all REST API endpoints for the 3-node cluster.
func getRESTEndpoints() []string {
	node1 := os.Getenv("QUICBAAS_NODE1_URL")
	if node1 == "" {
		node1 = "https://test-node1:8080"
	}

	node2 := os.Getenv("QUICBAAS_NODE2_URL")
	if node2 == "" {
		node2 = "https://test-node2:8080"
	}

	node3 := os.Getenv("QUICBAAS_NODE3_URL")
	if node3 == "" {
		node3 = "https://test-node3:8080"
	}

	return []string{node1, node2, node3}
}
