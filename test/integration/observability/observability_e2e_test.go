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
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestMetricsCardinality validates that metric cardinality is within bounds
func TestMetricsCardinality(t *testing.T) {
	// The metrics scrape route admits any authenticated identity and needs
	// no permission: the data-plane principal is the narrowest one.
	e2eHTTPClient := helpers.NewDataPlaneHTTPClient(t, 30*time.Second)
	nodes := getNodeURLs()
	if len(nodes) == 0 {
		t.Fatal("No nodes available for testing")
	}

	// Cardinality limits - Node 1 typically handles more traffic during tests
	// (leader election, client connections) so it has a higher threshold.
	// HTTP histogram buckets contribute significantly to series count on the leader.
	// Integration tests run many unique operations across all test packages, causing
	// high label cardinality for HTTP request histograms (method + path + status + buckets).
	const (
		baseSeriesLimit       = 10000
		primaryNodeMultiplier = 5.0 // Node 1 can have 5x the base limit (handles all test traffic + histograms)
		uniqueMetricsLimit    = 1000
	)

	// Check cardinality on all nodes
	for i, nodeURL := range nodes {
		t.Run(fmt.Sprintf("Node%d", i+1), func(t *testing.T) {
			resp, err := e2eHTTPClient.Get(nodeURL + "/api/v1/metrics/")
			if err != nil {
				t.Fatalf("Failed to get metrics from node %d: %v", i+1, err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read metrics: %v", err)
			}

			metricsText := string(body)
			lines := strings.Split(metricsText, "\n")

			// Count unique metric names (not including labels)
			metricNames := make(map[string]int)
			totalSeries := 0

			for _, line := range lines {
				// Skip comments and empty lines
				if strings.HasPrefix(line, "#") || line == "" {
					continue
				}

				totalSeries++

				// Extract metric name (before { or space)
				parts := strings.FieldsFunc(line, func(r rune) bool {
					return r == '{' || r == ' '
				})

				if len(parts) > 0 {
					metricName := parts[0]
					metricNames[metricName]++
				}
			}

			// Determine series limit based on node index
			// Node 1 (index 0) handles more traffic as the primary/leader
			seriesLimit := baseSeriesLimit
			if i == 0 {
				seriesLimit = int(float64(baseSeriesLimit) * primaryNodeMultiplier)
			}

			t.Logf("Node %d - Unique metrics: %d, Total series: %d (limit: %d)",
				i+1, len(metricNames), totalSeries, seriesLimit)

			// Cardinality bounds checking
			if len(metricNames) > uniqueMetricsLimit {
				t.Errorf("Node %d has too many unique metrics: %d (limit: %d)",
					i+1, len(metricNames), uniqueMetricsLimit)
			}

			if totalSeries > seriesLimit {
				t.Errorf("Node %d has too many time series: %d (limit: %d)",
					i+1, totalSeries, seriesLimit)
			}

			// Check for high cardinality metrics (more than 100 series per metric)
			for name, count := range metricNames {
				if count > 100 {
					t.Logf("Warning: High cardinality metric on node %d: %s (%d series)",
						i+1, name, count)
				}
			}

			// Verify QuicBaaS metrics are reasonably sized
			// Note: QuicBaaS-specific metrics may not be registered if no operations have occurred
			quicbaasMetrics := 0
			for name := range metricNames {
				if strings.HasPrefix(name, "quicbaas_") {
					quicbaasMetrics++
				}
			}

			if quicbaasMetrics == 0 {
				t.Logf("Note: No QuicBaaS-specific metrics found - metrics may not be instrumented yet")
			}

			t.Logf("Node %d has %d QuicBaaS-specific metrics", i+1, quicbaasMetrics)
		})
	}
}
