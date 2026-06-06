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

package routing

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// testHTTPClient is a package-level HTTP client configured with CA-pinned TLS
// trust for the integration cluster.
var testHTTPClient = helpers.NewHTTPClient(30 * time.Second)

// TestRoutingDebug verifies that the routing debug endpoint reflects config pins.
func TestRoutingDebug(t *testing.T) {
	node1 := os.Getenv("QUICBAAS_NODE1_URL")
	if node1 == "" {
		node1 = "https://test-node1:8080"
	}

	// Expect users pinned to shard 2 in integration config
	resp, err := testHTTPClient.Get(node1 + "/api/v1/debug/route/users/0")
	if err != nil {
		t.Fatalf("failed to call route debug endpoint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "2" {
		t.Fatalf("expected shard 2 for users, got %q", string(b))
	}
}

// TestRoutingDebugModulo verifies fallback modulo routing across IDs.
func TestRoutingDebugModulo(t *testing.T) {
	base := os.Getenv("QUICBAAS_NODE1_URL")
	if base == "" {
		base = "https://test-node1:8080"
	}
	// Config has shards [1,2,3,200,300], but KV routing excludes:
	// - shard 200 (vector, shard_id=200 from config)
	// - shard 300 (timeseries, shard_id=300 from config)
	// - shard 1000 (KMS, shard_id=1000 from config, not in shard list)
	// Effective KV shards: [1, 2, 3]
	// Modulo mapping for ids: 1->2, 2->3, 3->1, 4->2
	cases := []struct {
		id   uint64
		want string
	}{
		{1, "2"}, {2, "3"}, {3, "1"}, {4, "2"},
	}
	for _, c := range cases {
		url := fmt.Sprintf("%s/api/v1/debug/route/%s/%d", base, "events", c.id)
		resp, err := testHTTPClient.Get(url)
		if err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(b) != c.want {
			t.Fatalf("id=%d expected shard %s, status=%d body=%q", c.id, c.want, resp.StatusCode, string(b))
		}
	}
}

// TestRoutingDebugNoIDDefault verifies id==0 defaults to first shard.
func TestRoutingDebugNoIDDefault(t *testing.T) {
	base := os.Getenv("QUICBAAS_NODE1_URL")
	if base == "" {
		base = "https://test-node1:8080"
	}
	url := base + "/api/v1/debug/route/metrics/0"
	resp, err := testHTTPClient.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(b) != "1" {
		t.Fatalf("expected shard 1 for id=0, got status=%d body=%q", resp.StatusCode, string(b))
	}
}

// TestRoutingDebugConsistencyAcrossNodes ensures all nodes produce same decision.
func TestRoutingDebugConsistencyAcrossNodes(t *testing.T) {
	nodes := []string{os.Getenv("QUICBAAS_NODE1_URL"), os.Getenv("QUICBAAS_NODE2_URL"), os.Getenv("QUICBAAS_NODE3_URL")}
	for i := range nodes {
		if nodes[i] == "" {
			nodes[i] = fmt.Sprintf("https://test-node%d:8080", i+1)
		}
	}
	entity := "users" // pinned to 2
	id := uint64(123456)
	var expected string
	for i, base := range nodes {
		url := fmt.Sprintf("%s/api/v1/debug/route/%s/%d", base, entity, id)
		resp, err := testHTTPClient.Get(url)
		if err != nil {
			t.Fatalf("node%d GET %s: %v", i+1, url, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("node%d unexpected status: %d", i+1, resp.StatusCode)
		}
		if i == 0 {
			expected = string(b)
		} else if string(b) != expected {
			t.Fatalf("inconsistent routing: node1=%s node%d=%s", expected, i+1, string(b))
		}
	}
	if expected != "2" {
		t.Fatalf("users should be pinned to 2, got %s", expected)
	}
}

// TestRoutingMultiplePins verifies multiple entity-type pins work as expected.
func TestRoutingMultiplePins(t *testing.T) {
	base := os.Getenv("QUICBAAS_NODE1_URL")
	if base == "" {
		base = "https://test-node1:8080"
	}
	// Note: orders is pinned to shard 1 (not 3) because shard 3 is the default timeseries shard
	// and is excluded from kvShards by default. See quicbaas.yaml for configuration.
	for _, et := range []struct{ et, want string }{{"users", "2"}, {"orders", "1"}} {
		url := base + "/api/v1/debug/route/" + et.et + "/12345"
		resp, err := testHTTPClient.Get(url)
		if err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(b) != et.want {
			t.Fatalf("%s expected shard %s, got status=%d body=%q", et.et, et.want, resp.StatusCode, string(b))
		}
	}
}

// TestRoutingInvalidPinFailsStartup verifies invalid pins cause startup failure.
func TestRoutingInvalidPinFailsStartup(t *testing.T) {
	// Check if daemon binary exists before running test.
	// Try multiple locations as the binary may be in different places
	// depending on the test environment (Docker vs local).
	binaryPath := ""
	for _, path := range []string{"/usr/local/bin/quicbaasd", "/app/bin/quicbaasd", "./bin/quicbaasd"} {
		if _, err := os.Stat(path); err == nil {
			binaryPath = path
			break
		}
	}
	if binaryPath == "" {
		require.Fail(t, "quicbaasd binary must be available at /usr/local/bin/quicbaasd, /app/bin/quicbaasd, or ./bin/quicbaasd")
	}
	t.Logf("Using binary: %s", binaryPath)

	// Use a context with timeout to prevent hanging if the server doesn't exit.
	// The server should fail during startup validation, so 10 seconds is plenty.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use the pre-built daemon binary with invalid shard pin configuration.
	// Shards are [1,2] but we pin users to shard 3 which doesn't exist.
	cmd := exec.CommandContext(ctx, binaryPath, "cluster", "--http-port", "18080", "--grpc-port", "18090", "--raft-port", "16301")
	cmd.Env = append(os.Environ(),
		"QUICBAAS_SERVER_HOST=127.0.0.1",
		"QUICBAAS_STORAGE_DATA_DIR=/tmp/quicbaas-invalid-pin",
		"QUICBAAS_LOGGING_LEVEL=error",
		"QUICBAAS_RAFT_NODE_ID=99",
		"QUICBAAS_RAFT_RAFT_ADDRESS=127.0.0.1:16301",
		"QUICBAAS_SERVER_SHARDS=1,2",
		"QUICBAAS_SERVER_ENTITY_SHARD_PINS=users:3",
	)
	out, err := cmd.CombinedOutput()
	output := string(out)

	// Server should fail immediately with invalid routing config error
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("Server did not exit within timeout - expected startup failure for invalid pins. Output: %s", output)
	}

	if err == nil {
		t.Fatalf("Expected startup failure for invalid routing pins, got success. Output: %s", output)
	}

	// Verify the error message mentions invalid routing config
	if !strings.Contains(output, "invalid routing config") {
		t.Fatalf("Expected 'invalid routing config' in error output, got: %s", output)
	}

	t.Logf("Server correctly rejected invalid routing pins with error: %s", output)
}
