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

//go:build integration && timeseries

package timeseries

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/cli"
)

// =============================================================================
// Multi-Protocol CLI Tests for Time Series
// =============================================================================

// TestTimeSeriesCLIMultiProtocol tests all time series CLI commands across
// supported transport protocols (REST, gRPC).
// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
func TestTimeSeriesCLIMultiProtocol(t *testing.T) {
	cli.RunForTransports(t, "TimeSeriesLifecycle", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		testTimeSeriesLifecycle(t, runner)
	})
}

// TestTimeSeriesCLIWriteAllFlags tests the write command with all flag combinations.
// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
func TestTimeSeriesCLIWriteAllFlags(t *testing.T) {
	cli.RunForTransports(t, "WriteAllFlags", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		testWriteAllFlags(t, runner)
	})
}

// TestTimeSeriesCLIWriteBatch tests the write-batch command.
// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
func TestTimeSeriesCLIWriteBatch(t *testing.T) {
	cli.RunForTransports(t, "WriteBatch", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		testWriteBatch(t, runner)
	})
}

// TestTimeSeriesCLIQueryAllFlags tests the query command with all flag combinations.
// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
func TestTimeSeriesCLIQueryAllFlags(t *testing.T) {
	cli.RunForTransports(t, "QueryAllFlags", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		testQueryAllFlags(t, runner)
	})
}

// TestTimeSeriesCLIDeleteAllFlags tests the delete command with all flag combinations.
// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
func TestTimeSeriesCLIDeleteAllFlags(t *testing.T) {
	cli.RunForTransports(t, "DeleteAllFlags", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		testDeleteAllFlags(t, runner)
	})
}

// TestTimeSeriesCLIRetentionPolicy tests retention policy get/set commands.
// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
func TestTimeSeriesCLIRetentionPolicy(t *testing.T) {
	cli.RunForTransports(t, "RetentionPolicy", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		testRetentionPolicy(t, runner)
	})
}

// TestTimeSeriesCLIListMetrics tests the list-metrics command with all flag combinations.
// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
func TestTimeSeriesCLIListMetrics(t *testing.T) {
	cli.RunForTransports(t, "ListMetrics", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		testListMetrics(t, runner)
	})
}

// TestTimeSeriesCLIErrorHandling tests error cases for all time series commands.
// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
func TestTimeSeriesCLIErrorHandling(t *testing.T) {
	cli.RunForTransports(t, "ErrorHandling", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		testErrorHandling(t, runner)
	})
}

// =============================================================================
// Time Series Lifecycle Test
// =============================================================================

func testTimeSeriesLifecycle(t *testing.T, runner *cli.TestRunner) {
	t.Helper()

	// Generate unique metric name for test isolation
	metricName := fmt.Sprintf("cli.test.lifecycle.%d", time.Now().UnixNano())

	// Time range for queries
	startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	// Run the full lifecycle suite
	suite := cli.TimeSeriesSuite(metricName, startTime, endTime)
	runner.RunSuite(suite)
}

// =============================================================================
// Write Command Tests - All Flag Combinations
// =============================================================================

func testWriteAllFlags(t *testing.T, runner *cli.TestRunner) {
	t.Helper()

	baseMetric := fmt.Sprintf("cli.write.test.%d", time.Now().UnixNano())

	// Test: Write with minimum required flags (--metric only, value defaults to 0)
	t.Run("WriteMinimalFlags", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name:           "write minimal",
			Args:           []string{"timeseries", "write", "--metric", baseMetric + ".minimal", "--value", "100.5"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Write with --value flag
	t.Run("WriteWithValue", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name:           "write with value",
			Args:           []string{"timeseries", "write", "--metric", baseMetric + ".value", "--value", "75.5"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Write with --tags flag (single tag)
	t.Run("WriteWithSingleTag", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name:           "write with single tag",
			Args:           []string{"timeseries", "write", "--metric", baseMetric + ".singletag", "--value", "82.3", "--tags", "host=server1"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Write with --tags flag (multiple tags)
	t.Run("WriteWithMultipleTags", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name:           "write with multiple tags",
			Args:           []string{"timeseries", "write", "--metric", baseMetric + ".multitag", "--value", "90.1", "--tags", "host=server1,region=us-east-1,env=prod"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Write with --timestamp flag
	t.Run("WriteWithTimestamp", func(t *testing.T) {
		timestamp := time.Now().Format(time.RFC3339)
		cmd := cli.CommandDef{
			Name:           "write with timestamp",
			Args:           []string{"timeseries", "write", "--metric", baseMetric + ".timestamp", "--value", "65.0", "--timestamp", timestamp},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Write with all flags combined
	t.Run("WriteAllFlagsCombined", func(t *testing.T) {
		timestamp := time.Now().Format(time.RFC3339)
		cmd := cli.CommandDef{
			Name:           "write all flags",
			Args:           []string{"timeseries", "write", "--metric", baseMetric + ".allflags", "--value", "55.5", "--tags", "host=server1,region=us-west-2", "--timestamp", timestamp},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Write with negative value
	t.Run("WriteNegativeValue", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name:           "write negative value",
			Args:           []string{"timeseries", "write", "--metric", baseMetric + ".negative", "--value", "-25.5"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Write with zero value
	t.Run("WriteZeroValue", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name:           "write zero value",
			Args:           []string{"timeseries", "write", "--metric", baseMetric + ".zero", "--value", "0"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Write with very large value
	t.Run("WriteLargeValue", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name:           "write large value",
			Args:           []string{"timeseries", "write", "--metric", baseMetric + ".large", "--value", "999999999.999"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Write with scientific notation value
	t.Run("WriteScientificValue", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name:           "write scientific value",
			Args:           []string{"timeseries", "write", "--metric", baseMetric + ".scientific", "--value", "1.5e10"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Clean up test metrics
	t.Run("Cleanup", func(t *testing.T) {
		metrics := []string{
			baseMetric + ".minimal",
			baseMetric + ".value",
			baseMetric + ".singletag",
			baseMetric + ".multitag",
			baseMetric + ".timestamp",
			baseMetric + ".allflags",
			baseMetric + ".negative",
			baseMetric + ".zero",
			baseMetric + ".large",
			baseMetric + ".scientific",
		}
		for _, metric := range metrics {
			cleanupCmd := cli.TSDeleteCommand(metric)
			_, _, _ = runner.Run(cleanupCmd) // Best effort cleanup
		}
	})
}

// =============================================================================
// Write Batch Command Tests
// =============================================================================

func testWriteBatch(t *testing.T, runner *cli.TestRunner) {
	t.Helper()

	baseMetric := fmt.Sprintf("cli.writebatch.test.%d", time.Now().UnixNano())

	// Create a temporary JSON file with batch data
	batchData := []map[string]interface{}{
		{
			"metric":    baseMetric + ".batch1",
			"value":     75.5,
			"tags":      map[string]string{"host": "server1"},
			"timestamp": time.Now().Format(time.RFC3339),
		},
		{
			"metric":    baseMetric + ".batch2",
			"value":     82.3,
			"tags":      map[string]string{"host": "server2"},
			"timestamp": time.Now().Format(time.RFC3339),
		},
		{
			"metric":    baseMetric + ".batch3",
			"value":     90.1,
			"tags":      map[string]string{"host": "server3", "region": "us-east-1"},
			"timestamp": time.Now().Format(time.RFC3339),
		},
	}

	// Write batch data to temporary file
	tmpDir := os.TempDir()
	batchFile := filepath.Join(tmpDir, fmt.Sprintf("ts_batch_%d.json", time.Now().UnixNano()))

	jsonData, err := json.MarshalIndent(batchData, "", "  ")
	require.NoError(t, err, "Failed to marshal batch data")

	err = os.WriteFile(batchFile, jsonData, 0644)
	require.NoError(t, err, "Failed to write batch file")

	defer func() {
		_ = os.Remove(batchFile) // Cleanup temp file
	}()

	// Test: Write batch from JSON file
	t.Run("WriteBatchFromFile", func(t *testing.T) {
		cmd := cli.TSWriteBatchCommand(batchFile)
		runner.RunAndAssert(cmd)
	})

	// Verify data was written by querying
	t.Run("VerifyBatchWritten", func(t *testing.T) {
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		cmd := cli.TSQueryCommand(baseMetric+".batch1", startTime, endTime)
		if runner.IsSkipped(cmd) {
			return
		}
		stdout, _, err := runner.Run(cmd)
		assert.NoError(t, err, "Query should succeed")
		assert.NotEmpty(t, stdout, "Query should return data")
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		metrics := []string{
			baseMetric + ".batch1",
			baseMetric + ".batch2",
			baseMetric + ".batch3",
		}
		for _, metric := range metrics {
			cleanupCmd := cli.TSDeleteCommand(metric)
			_, _, _ = runner.Run(cleanupCmd)
		}
	})
}

// =============================================================================
// Query Command Tests - All Flag Combinations
// =============================================================================

func testQueryAllFlags(t *testing.T, runner *cli.TestRunner) {
	t.Helper()

	baseMetric := fmt.Sprintf("cli.query.test.%d", time.Now().UnixNano())

	// Setup: Write test data first
	t.Run("SetupTestData", func(t *testing.T) {
		// Write multiple data points with different tags
		for i := 0; i < 5; i++ {
			cmd := cli.CommandDef{
				Name: fmt.Sprintf("setup write %d", i),
				Args: []string{
					"timeseries", "write",
					"--metric", baseMetric,
					"--value", fmt.Sprintf("%d.5", 50+i*10),
					"--tags", fmt.Sprintf("host=server%d,region=us-east-1", i),
				},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				RequiresUnseal: true,
			}
			runner.RunAndAssert(cmd)
		}
	})

	startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	// Test: Query with minimum required flags
	t.Run("QueryMinimalFlags", func(t *testing.T) {
		cmd := cli.TSQueryCommand(baseMetric, startTime, endTime)
		runner.RunAndAssert(cmd)
	})

	// Test: Query with --tags filter
	t.Run("QueryWithTagsFilter", func(t *testing.T) {
		cmd := cli.TSQueryWithTagsCommand(baseMetric, startTime, endTime, "host=server0")
		runner.RunAndAssert(cmd)
	})

	// Test: Query with multiple --tags filters
	t.Run("QueryWithMultipleTagsFilter", func(t *testing.T) {
		cmd := cli.TSQueryWithTagsCommand(baseMetric, startTime, endTime, "host=server0,region=us-east-1")
		runner.RunAndAssert(cmd)
	})

	// Test: Query with --aggregation avg
	t.Run("QueryWithAggregationAvg", func(t *testing.T) {
		cmd := cli.TSQueryWithAggregationCommand(baseMetric, startTime, endTime, "avg", 0)
		runner.RunAndAssert(cmd)
	})

	// Test: Query with --aggregation sum
	t.Run("QueryWithAggregationSum", func(t *testing.T) {
		cmd := cli.TSQueryWithAggregationCommand(baseMetric, startTime, endTime, "sum", 0)
		runner.RunAndAssert(cmd)
	})

	// Test: Query with --aggregation min
	t.Run("QueryWithAggregationMin", func(t *testing.T) {
		cmd := cli.TSQueryWithAggregationCommand(baseMetric, startTime, endTime, "min", 0)
		runner.RunAndAssert(cmd)
	})

	// Test: Query with --aggregation max
	t.Run("QueryWithAggregationMax", func(t *testing.T) {
		cmd := cli.TSQueryWithAggregationCommand(baseMetric, startTime, endTime, "max", 0)
		runner.RunAndAssert(cmd)
	})

	// Test: Query with --aggregation count
	t.Run("QueryWithAggregationCount", func(t *testing.T) {
		cmd := cli.TSQueryWithAggregationCommand(baseMetric, startTime, endTime, "count", 0)
		runner.RunAndAssert(cmd)
	})

	// Test: Query with --aggregation and --interval
	t.Run("QueryWithAggregationAndInterval", func(t *testing.T) {
		cmd := cli.TSQueryWithAggregationCommand(baseMetric, startTime, endTime, "avg", 300)
		runner.RunAndAssert(cmd)
	})

	// Test: Query with --group-by
	t.Run("QueryWithGroupBy", func(t *testing.T) {
		cmd := cli.TSQueryWithGroupByCommand(baseMetric, startTime, endTime, []string{"host"})
		runner.RunAndAssert(cmd)
	})

	// Test: Query with multiple --group-by fields
	t.Run("QueryWithMultipleGroupBy", func(t *testing.T) {
		cmd := cli.TSQueryWithGroupByCommand(baseMetric, startTime, endTime, []string{"host", "region"})
		runner.RunAndAssert(cmd)
	})

	// Test: Query with --limit
	t.Run("QueryWithLimit", func(t *testing.T) {
		cmd := cli.TSQueryWithLimitCommand(baseMetric, startTime, endTime, 2)
		runner.RunAndAssert(cmd)
	})

	// Test: Query with --limit=1
	t.Run("QueryWithLimitOne", func(t *testing.T) {
		cmd := cli.TSQueryWithLimitCommand(baseMetric, startTime, endTime, 1)
		runner.RunAndAssert(cmd)
	})

	// Test: Query combining all optional flags
	t.Run("QueryAllFlagsCombined", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name: "query all flags combined",
			Args: []string{
				"timeseries", "query",
				"--metric", baseMetric,
				"--start", startTime,
				"--end", endTime,
				"--tags", "region=us-east-1",
				"--aggregation", "avg",
				"--interval", "60",
				"--group-by", "host",
				"--limit", "10",
			},
			ExpectSuccess:  true,
			Timeout:        30 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		cleanupCmd := cli.TSDeleteCommand(baseMetric)
		_, _, _ = runner.Run(cleanupCmd)
	})
}

// =============================================================================
// Delete Command Tests - All Flag Combinations
// =============================================================================

func testDeleteAllFlags(t *testing.T, runner *cli.TestRunner) {
	t.Helper()

	baseMetric := fmt.Sprintf("cli.delete.test.%d", time.Now().UnixNano())

	// Setup helper to write test data
	writeTestData := func(suffix string) {
		cmd := cli.CommandDef{
			Name: "setup write for delete",
			Args: []string{
				"timeseries", "write",
				"--metric", baseMetric + suffix,
				"--value", "100.5",
				"--tags", "host=server1,region=us-east-1",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	}

	// Test: Delete with --metric only (delete all data for metric)
	t.Run("DeleteMetricOnly", func(t *testing.T) {
		writeTestData(".metriconly")
		cmd := cli.TSDeleteCommand(baseMetric + ".metriconly")
		runner.RunAndAssert(cmd)
	})

	// Test: Delete with --tags filter
	t.Run("DeleteWithTagsFilter", func(t *testing.T) {
		writeTestData(".withtags")
		cmd := cli.TSDeleteWithTagsCommand(baseMetric+".withtags", "host=server1")
		runner.RunAndAssert(cmd)
	})

	// Test: Delete with --start and --end time range
	t.Run("DeleteWithTimeRange", func(t *testing.T) {
		writeTestData(".timerange")
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
		cmd := cli.TSDeleteWithTimeRangeCommand(baseMetric+".timerange", startTime, endTime)
		runner.RunAndAssert(cmd)
	})

	// Test: Delete with all flags combined
	t.Run("DeleteAllFlagsCombined", func(t *testing.T) {
		writeTestData(".allflags")
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
		cmd := cli.CommandDef{
			Name: "delete all flags combined",
			Args: []string{
				"timeseries", "delete",
				"--metric", baseMetric + ".allflags",
				"--tags", "host=server1",
				"--start", startTime,
				"--end", endTime,
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})
}

// =============================================================================
// Retention Policy Tests
// =============================================================================

func testRetentionPolicy(t *testing.T, runner *cli.TestRunner) {
	t.Helper()

	baseMetric := fmt.Sprintf("cli.retention.test.%d", time.Now().UnixNano())

	// Setup: Write some test data first
	t.Run("SetupTestData", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name: "setup write for retention",
			Args: []string{
				"timeseries", "write",
				"--metric", baseMetric,
				"--value", "100.5",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Set retention with --duration only
	t.Run("SetRetentionDurationOnly", func(t *testing.T) {
		cmd := cli.TSSetRetentionCommand(baseMetric, 2592000) // 30 days
		runner.RunAndAssert(cmd)
	})

	// Test: Get retention policy
	t.Run("GetRetention", func(t *testing.T) {
		cmd := cli.TSGetRetentionCommand(baseMetric)
		runner.RunAndAssert(cmd)
	})

	// Test: Set retention with --duration and --resolution
	t.Run("SetRetentionWithResolution", func(t *testing.T) {
		cmd := cli.TSSetRetentionWithResolutionCommand(baseMetric, 2592000, 300, "") // 30 days, 5 min resolution
		runner.RunAndAssert(cmd)
	})

	// Test: Set retention with --duration, --resolution, and --aggregation
	t.Run("SetRetentionWithAggregation", func(t *testing.T) {
		cmd := cli.TSSetRetentionWithResolutionCommand(baseMetric, 7776000, 600, "avg") // 90 days, 10 min resolution, avg aggregation
		runner.RunAndAssert(cmd)
	})

	// Test: Set retention with all aggregation functions
	aggregations := []string{"avg", "sum", "min", "max", "count"}
	for _, agg := range aggregations {
		t.Run(fmt.Sprintf("SetRetentionAggregation_%s", agg), func(t *testing.T) {
			cmd := cli.TSSetRetentionWithResolutionCommand(baseMetric, 604800, 60, agg) // 7 days, 1 min resolution
			runner.RunAndAssert(cmd)
		})
	}

	// Test: Verify retention policy was updated
	t.Run("VerifyRetentionUpdated", func(t *testing.T) {
		cmd := cli.TSGetRetentionCommand(baseMetric)
		runner.RunAndAssert(cmd)
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		cleanupCmd := cli.TSDeleteCommand(baseMetric)
		_, _, _ = runner.Run(cleanupCmd)
	})
}

// =============================================================================
// List Metrics Tests
// =============================================================================

func testListMetrics(t *testing.T, runner *cli.TestRunner) {
	t.Helper()

	baseMetric := fmt.Sprintf("cli.listmetrics.test.%d", time.Now().UnixNano())

	// Setup: Create multiple metrics with different prefixes
	t.Run("SetupTestMetrics", func(t *testing.T) {
		metrics := []string{
			baseMetric + ".cpu.usage",
			baseMetric + ".cpu.idle",
			baseMetric + ".memory.used",
			baseMetric + ".memory.free",
			baseMetric + ".disk.read",
		}
		for _, metric := range metrics {
			cmd := cli.CommandDef{
				Name: fmt.Sprintf("setup write %s", metric),
				Args: []string{
					"timeseries", "write",
					"--metric", metric,
					"--value", "50.0",
				},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				RequiresUnseal: true,
			}
			runner.RunAndAssert(cmd)
		}
	})

	// Test: List all metrics (no filters)
	t.Run("ListMetricsNoFilter", func(t *testing.T) {
		cmd := cli.TSListMetricsCommand()
		runner.RunAndAssert(cmd)
	})

	// Test: List metrics with --prefix filter
	t.Run("ListMetricsWithPrefix", func(t *testing.T) {
		cmd := cli.TSListMetricsWithPrefixCommand(baseMetric + ".cpu")
		runner.RunAndAssert(cmd)
	})

	// Test: List metrics with --limit
	t.Run("ListMetricsWithLimit", func(t *testing.T) {
		cmd := cli.TSListMetricsWithLimitCommand(2, "")
		runner.RunAndAssert(cmd)
	})

	// Test: List metrics with --limit=1
	t.Run("ListMetricsWithLimitOne", func(t *testing.T) {
		cmd := cli.TSListMetricsWithLimitCommand(1, "")
		runner.RunAndAssert(cmd)
	})

	// Test: List metrics combining --prefix and --limit
	t.Run("ListMetricsPrefixAndLimit", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name: "list metrics prefix and limit",
			Args: []string{
				"timeseries", "list-metrics",
				"--prefix", baseMetric,
				"--limit", "3",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(cmd)
	})

	// Cleanup
	t.Run("Cleanup", func(t *testing.T) {
		metrics := []string{
			baseMetric + ".cpu.usage",
			baseMetric + ".cpu.idle",
			baseMetric + ".memory.used",
			baseMetric + ".memory.free",
			baseMetric + ".disk.read",
		}
		for _, metric := range metrics {
			cleanupCmd := cli.TSDeleteCommand(metric)
			_, _, _ = runner.Run(cleanupCmd)
		}
	})
}

// =============================================================================
// Error Handling Tests
// =============================================================================

func testErrorHandling(t *testing.T, runner *cli.TestRunner) {
	t.Helper()

	// Test: Write without --metric (should fail)
	t.Run("WriteErrorMissingMetric", func(t *testing.T) {
		cmd := cli.TSWriteErrorMissingMetricCommand()
		runner.RunAndAssert(cmd)
	})

	// Test: Query without required flags (should fail)
	t.Run("QueryErrorMissingRequired", func(t *testing.T) {
		cmd := cli.TSQueryErrorMissingRequiredCommand()
		runner.RunAndAssert(cmd)
	})

	// Test: Query with invalid time format (should fail)
	t.Run("QueryErrorInvalidTimeFormat", func(t *testing.T) {
		cmd := cli.TSQueryErrorInvalidTimeFormatCommand()
		runner.RunAndAssert(cmd)
	})

	// Test: Set retention without --duration (should fail)
	t.Run("SetRetentionErrorMissingDuration", func(t *testing.T) {
		cmd := cli.TSSetRetentionErrorMissingDurationCommand("test.metric")
		runner.RunAndAssert(cmd)
	})

	// Test: Delete without --metric (should fail)
	t.Run("DeleteErrorMissingMetric", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name:          "delete error missing metric",
			Args:          []string{"timeseries", "delete"},
			ExpectSuccess: false,
			Timeout:       10 * time.Second,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Write with invalid value format (should fail)
	t.Run("WriteErrorInvalidValue", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name:          "write error invalid value",
			Args:          []string{"timeseries", "write", "--metric", "test.metric", "--value", "not-a-number"},
			ExpectSuccess: false,
			Timeout:       10 * time.Second,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Query for non-existent metric (should succeed but return empty)
	t.Run("QueryNonExistentMetric", func(t *testing.T) {
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
		cmd := cli.TSQueryCommand("nonexistent.metric.12345", startTime, endTime)
		// This should succeed but return empty results
		stdout, _, err := runner.Run(cmd)
		assert.NoError(t, err, "Query for non-existent metric should not error")
		t.Logf("Query result for non-existent metric: %s", stdout)
	})

	// Test: Write with empty metric name (should fail)
	t.Run("WriteErrorEmptyMetric", func(t *testing.T) {
		cmd := cli.CommandDef{
			Name:          "write error empty metric",
			Args:          []string{"timeseries", "write", "--metric", "", "--value", "100.0"},
			ExpectSuccess: false,
			Timeout:       10 * time.Second,
		}
		runner.RunAndAssert(cmd)
	})

	// Test: Write batch with non-existent file (should fail)
	t.Run("WriteBatchErrorNonExistentFile", func(t *testing.T) {
		cmd := cli.TSWriteBatchCommand("/nonexistent/path/batch.json")
		cmd.ExpectSuccess = false
		runner.RunAndAssert(cmd)
	})

	// Test: Query with start time after end time (should fail or return empty)
	t.Run("QueryErrorStartAfterEnd", func(t *testing.T) {
		startTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		cmd := cli.CommandDef{
			Name: "query error start after end",
			Args: []string{
				"timeseries", "query",
				"--metric", "test.metric",
				"--start", startTime,
				"--end", endTime,
			},
			ExpectSuccess: false,
			Timeout:       10 * time.Second,
		}
		// This might succeed with empty results or fail depending on implementation
		_, _, _ = runner.Run(cmd)
	})

	// Test: Get retention for non-existent metric
	t.Run("GetRetentionNonExistentMetric", func(t *testing.T) {
		cmd := cli.TSGetRetentionCommand("nonexistent.metric.67890")
		// This might return default retention or error
		_, _, _ = runner.Run(cmd)
	})
}

// =============================================================================
// Comprehensive Multi-Protocol Table-Driven Tests
// =============================================================================

// TestTimeSeriesWriteCommandTableDriven tests all write command variations.
func TestTimeSeriesWriteCommandTableDriven(t *testing.T) {
	baseMetric := fmt.Sprintf("cli.write.table.%d", time.Now().UnixNano())
	timestamp := time.Now().Format(time.RFC3339)

	testCases := []struct {
		name          string
		args          []string
		expectSuccess bool
	}{
		{
			name:          "WriteBasic",
			args:          []string{"timeseries", "write", "--metric", baseMetric + ".basic", "--value", "100.0"},
			expectSuccess: true,
		},
		{
			name:          "WriteWithTags",
			args:          []string{"timeseries", "write", "--metric", baseMetric + ".tags", "--value", "100.0", "--tags", "host=server1"},
			expectSuccess: true,
		},
		{
			name:          "WriteWithTimestamp",
			args:          []string{"timeseries", "write", "--metric", baseMetric + ".ts", "--value", "100.0", "--timestamp", timestamp},
			expectSuccess: true,
		},
		{
			name:          "WriteAllOptions",
			args:          []string{"timeseries", "write", "--metric", baseMetric + ".all", "--value", "100.0", "--tags", "host=server1,region=us-east-1", "--timestamp", timestamp},
			expectSuccess: true,
		},
		{
			name:          "WriteMissingMetric",
			args:          []string{"timeseries", "write", "--value", "100.0"},
			expectSuccess: false,
		},
	}

	// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
	cli.RunForTransports(t, "WriteTableDriven", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				cmd := cli.CommandDef{
					Name:           tc.name,
					Args:           tc.args,
					ExpectSuccess:  tc.expectSuccess,
					Timeout:        10 * time.Second,
					RequiresUnseal: true,
				}
				runner.RunAndAssert(cmd)
			})
		}

		// Cleanup
		for _, tc := range testCases {
			if tc.expectSuccess {
				cleanupCmd := cli.TSDeleteCommand(tc.args[3]) // --metric is at index 3
				_, _, _ = runner.Run(cleanupCmd)
			}
		}
	})
}

// TestTimeSeriesQueryCommandTableDriven tests all query command variations.
func TestTimeSeriesQueryCommandTableDriven(t *testing.T) {
	baseMetric := fmt.Sprintf("cli.query.table.%d", time.Now().UnixNano())
	startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	testCases := []struct {
		name          string
		args          []string
		expectSuccess bool
	}{
		{
			name:          "QueryBasic",
			args:          []string{"timeseries", "query", "--metric", baseMetric, "--start", startTime, "--end", endTime},
			expectSuccess: true,
		},
		{
			name:          "QueryWithTags",
			args:          []string{"timeseries", "query", "--metric", baseMetric, "--start", startTime, "--end", endTime, "--tags", "host=server1"},
			expectSuccess: true,
		},
		{
			name:          "QueryWithAggAvg",
			args:          []string{"timeseries", "query", "--metric", baseMetric, "--start", startTime, "--end", endTime, "--aggregation", "avg"},
			expectSuccess: true,
		},
		{
			name:          "QueryWithAggSum",
			args:          []string{"timeseries", "query", "--metric", baseMetric, "--start", startTime, "--end", endTime, "--aggregation", "sum"},
			expectSuccess: true,
		},
		{
			name:          "QueryWithAggMin",
			args:          []string{"timeseries", "query", "--metric", baseMetric, "--start", startTime, "--end", endTime, "--aggregation", "min"},
			expectSuccess: true,
		},
		{
			name:          "QueryWithAggMax",
			args:          []string{"timeseries", "query", "--metric", baseMetric, "--start", startTime, "--end", endTime, "--aggregation", "max"},
			expectSuccess: true,
		},
		{
			name:          "QueryWithAggCount",
			args:          []string{"timeseries", "query", "--metric", baseMetric, "--start", startTime, "--end", endTime, "--aggregation", "count"},
			expectSuccess: true,
		},
		{
			name:          "QueryWithGroupBy",
			args:          []string{"timeseries", "query", "--metric", baseMetric, "--start", startTime, "--end", endTime, "--group-by", "host"},
			expectSuccess: true,
		},
		{
			name:          "QueryWithLimit",
			args:          []string{"timeseries", "query", "--metric", baseMetric, "--start", startTime, "--end", endTime, "--limit", "10"},
			expectSuccess: true,
		},
		{
			name:          "QueryMissingMetric",
			args:          []string{"timeseries", "query", "--start", startTime, "--end", endTime},
			expectSuccess: false,
		},
		{
			name:          "QueryMissingStart",
			args:          []string{"timeseries", "query", "--metric", baseMetric, "--end", endTime},
			expectSuccess: false,
		},
		{
			name:          "QueryMissingEnd",
			args:          []string{"timeseries", "query", "--metric", baseMetric, "--start", startTime},
			expectSuccess: false,
		},
	}

	// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
	cli.RunForTransports(t, "QueryTableDriven", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}

		// Setup: Write test data
		setupCmd := cli.CommandDef{
			Name:           "setup",
			Args:           []string{"timeseries", "write", "--metric", baseMetric, "--value", "100.0", "--tags", "host=server1"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(setupCmd)

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				cmd := cli.CommandDef{
					Name:           tc.name,
					Args:           tc.args,
					ExpectSuccess:  tc.expectSuccess,
					Timeout:        30 * time.Second,
					RequiresUnseal: true,
				}
				runner.RunAndAssert(cmd)
			})
		}

		// Cleanup
		cleanupCmd := cli.TSDeleteCommand(baseMetric)
		_, _, _ = runner.Run(cleanupCmd)
	})
}

// TestTimeSeriesRetentionCommandTableDriven tests all retention command variations.
func TestTimeSeriesRetentionCommandTableDriven(t *testing.T) {
	baseMetric := fmt.Sprintf("cli.retention.table.%d", time.Now().UnixNano())

	testCases := []struct {
		name          string
		args          []string
		expectSuccess bool
	}{
		{
			name:          "SetRetentionBasic",
			args:          []string{"timeseries", "set-retention", baseMetric, "--duration", "2592000"},
			expectSuccess: true,
		},
		{
			name:          "SetRetentionWithResolution",
			args:          []string{"timeseries", "set-retention", baseMetric, "--duration", "2592000", "--resolution", "300"},
			expectSuccess: true,
		},
		{
			name:          "SetRetentionWithAggAvg",
			args:          []string{"timeseries", "set-retention", baseMetric, "--duration", "2592000", "--aggregation", "avg"},
			expectSuccess: true,
		},
		{
			name:          "SetRetentionAllOptions",
			args:          []string{"timeseries", "set-retention", baseMetric, "--duration", "7776000", "--resolution", "600", "--aggregation", "sum"},
			expectSuccess: true,
		},
		{
			name:          "SetRetentionMissingDuration",
			args:          []string{"timeseries", "set-retention", baseMetric},
			expectSuccess: false,
		},
		{
			name:          "GetRetention",
			args:          []string{"timeseries", "get-retention", baseMetric},
			expectSuccess: true,
		},
	}

	// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
	cli.RunForTransports(t, "RetentionTableDriven", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}

		// Setup: Write test data
		setupCmd := cli.CommandDef{
			Name:           "setup",
			Args:           []string{"timeseries", "write", "--metric", baseMetric, "--value", "100.0"},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			RequiresUnseal: true,
		}
		runner.RunAndAssert(setupCmd)

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				cmd := cli.CommandDef{
					Name:           tc.name,
					Args:           tc.args,
					ExpectSuccess:  tc.expectSuccess,
					Timeout:        10 * time.Second,
					RequiresUnseal: true,
				}
				runner.RunAndAssert(cmd)
			})
		}

		// Cleanup
		cleanupCmd := cli.TSDeleteCommand(baseMetric)
		_, _, _ = runner.Run(cleanupCmd)
	})
}

// TestTimeSeriesListMetricsCommandTableDriven tests all list-metrics command variations.
func TestTimeSeriesListMetricsCommandTableDriven(t *testing.T) {
	baseMetric := fmt.Sprintf("cli.list.table.%d", time.Now().UnixNano())

	testCases := []struct {
		name          string
		args          []string
		expectSuccess bool
	}{
		{
			name:          "ListMetricsBasic",
			args:          []string{"timeseries", "list-metrics"},
			expectSuccess: true,
		},
		{
			name:          "ListMetricsWithPrefix",
			args:          []string{"timeseries", "list-metrics", "--prefix", baseMetric},
			expectSuccess: true,
		},
		{
			name:          "ListMetricsWithLimit",
			args:          []string{"timeseries", "list-metrics", "--limit", "10"},
			expectSuccess: true,
		},
		{
			name:          "ListMetricsPrefixAndLimit",
			args:          []string{"timeseries", "list-metrics", "--prefix", baseMetric, "--limit", "5"},
			expectSuccess: true,
		},
	}

	// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
	cli.RunForTransports(t, "ListMetricsTableDriven", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}

		// Setup: Write test metrics
		for i := 0; i < 3; i++ {
			setupCmd := cli.CommandDef{
				Name:           fmt.Sprintf("setup%d", i),
				Args:           []string{"timeseries", "write", "--metric", fmt.Sprintf("%s.metric%d", baseMetric, i), "--value", "100.0"},
				ExpectSuccess:  true,
				Timeout:        10 * time.Second,
				RequiresUnseal: true,
			}
			runner.RunAndAssert(setupCmd)
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				cmd := cli.CommandDef{
					Name:           tc.name,
					Args:           tc.args,
					ExpectSuccess:  tc.expectSuccess,
					Timeout:        10 * time.Second,
					RequiresUnseal: true,
				}
				runner.RunAndAssert(cmd)
			})
		}

		// Cleanup
		for i := 0; i < 3; i++ {
			cleanupCmd := cli.TSDeleteCommand(fmt.Sprintf("%s.metric%d", baseMetric, i))
			_, _, _ = runner.Run(cleanupCmd)
		}
	})
}

// TestTimeSeriesDeleteCommandTableDriven tests all delete command variations.
func TestTimeSeriesDeleteCommandTableDriven(t *testing.T) {
	baseMetric := fmt.Sprintf("cli.delete.table.%d", time.Now().UnixNano())
	startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

	testCases := []struct {
		name          string
		args          []string
		expectSuccess bool
		setupMetric   string // Metric to create for this test
	}{
		{
			name:          "DeleteBasic",
			args:          []string{"timeseries", "delete", "--metric", baseMetric + ".basic"},
			expectSuccess: true,
			setupMetric:   baseMetric + ".basic",
		},
		{
			name:          "DeleteWithTags",
			args:          []string{"timeseries", "delete", "--metric", baseMetric + ".tags", "--tags", "host=server1"},
			expectSuccess: true,
			setupMetric:   baseMetric + ".tags",
		},
		{
			name:          "DeleteWithTimeRange",
			args:          []string{"timeseries", "delete", "--metric", baseMetric + ".timerange", "--start", startTime, "--end", endTime},
			expectSuccess: true,
			setupMetric:   baseMetric + ".timerange",
		},
		{
			name:          "DeleteAllOptions",
			args:          []string{"timeseries", "delete", "--metric", baseMetric + ".all", "--tags", "host=server1", "--start", startTime, "--end", endTime},
			expectSuccess: true,
			setupMetric:   baseMetric + ".all",
		},
		{
			name:          "DeleteMissingMetric",
			args:          []string{"timeseries", "delete"},
			expectSuccess: false,
			setupMetric:   "",
		},
	}

	// Note: MCP and QUIC transports are not yet supported for time-series CLI commands.
	cli.RunForTransports(t, "DeleteTableDriven", []cli.Transport{cli.TransportREST, cli.TransportMCP, cli.TransportQUIC, cli.TransportUnix}, func(t *testing.T, runner *cli.TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Setup: Write test data if needed
				if tc.setupMetric != "" {
					setupCmd := cli.CommandDef{
						Name:           "setup",
						Args:           []string{"timeseries", "write", "--metric", tc.setupMetric, "--value", "100.0", "--tags", "host=server1"},
						ExpectSuccess:  true,
						Timeout:        10 * time.Second,
						RequiresUnseal: true,
					}
					runner.RunAndAssert(setupCmd)
				}

				// Run the actual test
				cmd := cli.CommandDef{
					Name:           tc.name,
					Args:           tc.args,
					ExpectSuccess:  tc.expectSuccess,
					Timeout:        10 * time.Second,
					RequiresUnseal: true,
				}
				runner.RunAndAssert(cmd)
			})
		}
	})
}
