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

package parameter_store

import (
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
		fmt.Println("Cluster not ready for parameter store tests, exiting")
		os.Exit(1)
	}

	// Boot embedded server for multi-protocol tests
	_, srv, err := helpers.SetupEmbeddedServer()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to start embedded server: %v\n", err)
	}

	// Run tests
	code := m.Run()
	if err := helpers.ReleaseDataPlaneTenants(); err != nil {
		fmt.Fprintf(os.Stderr, "release data-plane tenants: %v\n", err)
	}

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

	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		resp, err := client.Get(endpoint + "/api/v1/health/ready")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Println("Cluster is ready for parameter store tests")
				return true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

// getRESTEndpoint returns the REST API endpoint from environment or default.
func getRESTEndpoint() string {
	if endpoint := os.Getenv("QUICBAAS_NODE1_URL"); endpoint != "" {
		return endpoint
	}
	return "https://test-node1:8080"
}

// getGRPCEndpoint returns the gRPC endpoint from environment or default.
func getGRPCEndpoint() string {
	if endpoint := os.Getenv("QUICBAAS_NODE1_GRPC"); endpoint != "" {
		return endpoint
	}
	return "test-node1:50051"
}

// getQUICEndpoint returns the QUIC endpoint from environment or default.
func getQUICEndpoint() string {
	if endpoint := os.Getenv("QUICBAAS_NODE1_QUIC"); endpoint != "" {
		return endpoint
	}
	return "test-node1:4433"
}
