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

package cluster

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
		fmt.Println("Cluster not ready, exiting")
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Exit with test result code
	os.Exit(code)
}

// waitForClusterReady waits for the QuicBaaS cluster to be ready.
// The cluster test stack serves HTTPS with the integration CA, so the probe
// uses helpers.NewHTTPClient, which trusts the integration CA at
// helpers.IntegrationCAPath. A dedicated client is used to avoid polluting
// http.DefaultTransport's connection pool with keepalive connections that
// could cause later tests to hang when reusing stale connections.
func waitForClusterReady() bool {
	endpoints := getRESTEndpoints()
	client := helpers.NewHTTPClient(5 * time.Second)

	deadline := time.After(4 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		// Any one node serving is enough: the failover targets run with a
		// node deliberately stopped, which may be node1.
		for _, endpoint := range endpoints {
			resp, err := client.Get(endpoint + "/api/v1/health/ready")
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Println("Cluster is ready for cluster tests")
				return true
			}
		}
		select {
		case <-deadline:
			return false
		case <-ticker.C:
		}
	}
}

// getRESTEndpoints returns the REST API endpoints of the configured nodes.
func getRESTEndpoints() []string {
	var endpoints []string
	for _, env := range []string{"QUICBAAS_NODE1_URL", "QUICBAAS_NODE2_URL", "QUICBAAS_NODE3_URL"} {
		if endpoint := os.Getenv(env); endpoint != "" {
			endpoints = append(endpoints, endpoint)
		}
	}
	if len(endpoints) == 0 {
		return []string{"https://test-node1:8080"}
	}
	return endpoints
}
