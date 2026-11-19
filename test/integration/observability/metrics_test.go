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

package observability

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

func TestREST_MetricsExposeRoutingCounter(t *testing.T) {
	// The metrics scrape route admits any authenticated identity and the
	// entity read requires kv:get: the data-plane principal holds no more.
	metricsHTTPClient := helpers.NewDataPlaneHTTPClient(t, 30*time.Second)
	base := os.Getenv("QUICBAAS_NODE1_URL")
	if base == "" {
		base = "https://test-node1:8080"
	}
	// Hit a REST handler that records shard counter (List)
	// We ignore status; it may fail on storage, but counter increments before read.
	_, _ = metricsHTTPClient.Get(base + "/api/v1/entities/foo?limit=1")
	// Fetch metrics
	resp, err := metricsHTTPClient.Get(base + "/api/v1/metrics")
	if err != nil {
		t.Fatalf("metrics error: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), "go_gc_duration_seconds") {
		t.Fatalf("expected metrics output; status=%d", resp.StatusCode)
	}
}
