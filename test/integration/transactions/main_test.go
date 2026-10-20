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

package transactions

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

	// Run tests
	code := m.Run()

	// Exit with test result code
	os.Exit(code)
}

// waitForClusterReady waits for the QuicBaaS cluster to be ready.
func waitForClusterReady() bool {
	endpoint := getRESTEndpoint()
	client := helpers.NewHTTPClient(5 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		if clusterReady(client, endpoint) {
			fmt.Println("Cluster is ready for transaction tests")
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// clusterReady reports whether the node's readiness check answers 200.
func clusterReady(client *http.Client, endpoint string) bool {
	resp, err := client.Get(endpoint + "/api/v1/health/ready")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// getRESTEndpoint returns the primary REST API endpoint.
func getRESTEndpoint() string {
	if endpoint := os.Getenv("QUICBAAS_NODE1_URL"); endpoint != "" {
		return endpoint
	}
	return "https://test-node1:8080"
}
