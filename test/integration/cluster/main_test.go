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
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestMain sets up and tears down the test environment.
func TestMain(m *testing.M) {
	// The readiness probe and the KV helpers authenticate as the fixture
	// system admin over gRPC; without these the probe can only time out.
	if missing := missingClusterEnv(); len(missing) > 0 {
		fmt.Printf("Cluster environment incomplete (run make integration-test-cluster): %v unset or missing\n", missing)
		os.Exit(1)
	}

	// Wait for cluster to be ready
	if !waitForClusterReady() {
		fmt.Println("Cluster not ready, exiting")
		os.Exit(1)
	}

	// Run tests
	code := m.Run()
	if err := helpers.ReleaseDataPlaneTenants(); err != nil {
		fmt.Fprintf(os.Stderr, "release data-plane tenants: %v\n", err)
	}

	// Exit with test result code
	os.Exit(code)
}

// clusterEnv names the https REST URLs and gRPC endpoints every runner of
// this package (make integration-test-cluster, docker-compose.integration.yml)
// exports; the helpers have no default for them.
var clusterEnv = []string{
	"QUICBAAS_NODE1_URL", "QUICBAAS_NODE2_URL", "QUICBAAS_NODE3_URL",
	"QUICBAAS_NODE1_GRPC", "QUICBAAS_NODE2_GRPC", "QUICBAAS_NODE3_GRPC",
}

// fixtureFile is certfixture material the helpers read: the path in the
// override variable when it is set, else the file under the mounted
// /etc/quicbaas/certs directory.
type fixtureFile struct {
	env  string
	path string
}

// clusterFixtures mirrors the helpers' resolution of the admin PIV material
// and the fixture CA's keystore directory.
var clusterFixtures = []fixtureFile{
	{env: "QUICBAAS_TEST_PIV_CERT", path: filepath.Join(filepath.Dir(helpers.IntegrationCAPath), "admin.crt")},
	{env: "QUICBAAS_TEST_PIV_KEY", path: filepath.Join(filepath.Dir(helpers.IntegrationCAPath), "admin.key")},
	{env: "QUICBAAS_TEST_PIV_CA", path: helpers.IntegrationCAPath},
	{env: "QUICBAAS_TEST_CERTS_DIR", path: helpers.FixtureCertsDir()},
}

// resolve returns the override when it is set, else the default path.
func (f fixtureFile) resolve() string {
	if v := os.Getenv(f.env); v != "" {
		return v
	}
	return f.path
}

// missingClusterEnv lists the unset endpoint variables and every fixture
// path that does not resolve to an existing file or directory.
func missingClusterEnv() []string {
	var missing []string
	for _, env := range clusterEnv {
		if os.Getenv(env) == "" {
			missing = append(missing, env)
		}
	}
	for _, f := range clusterFixtures {
		path := f.resolve()
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, f.env+"="+path)
		}
	}
	return missing
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
				// Health readiness does not mean shard 1 has a leader: an
				// election still in progress fails every authenticated call.
				if err := helpers.WaitForClusterShardLeaders(context.Background(), helpers.ClusterShards, 2*time.Minute); err != nil {
					fmt.Printf("Shard leaders not elected: %v\n", err)
					return false
				}
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
