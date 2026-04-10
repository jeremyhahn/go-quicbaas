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

// Package cli provides E2E integration tests for QuicBaaS CLI timeseries commands.
package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Timeseries Command Definitions
// =============================================================================

// =============================================================================
// Timeseries Write Command Definitions
// =============================================================================

// TimeseriesWriteCommand returns a command to write a single timeseries data point.
// Timeseries operations are supported via gRPC, REST, and MCP transports.
func TimeseriesWriteCommand(metric string, value float64) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("timeseries write %s", metric),
		Args:           []string{"timeseries", "write", "--metric", metric, "--value", fmt.Sprintf("%f", value)},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesWriteWithTimestampCommand writes data with explicit timestamp.
// Timeseries operations are supported via gRPC, REST, and MCP transports.
func TimeseriesWriteWithTimestampCommand(metric string, value float64, timestamp string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries write %s with timestamp", metric),
		Args: []string{
			"timeseries", "write",
			"--metric", metric,
			"--value", fmt.Sprintf("%f", value),
			"--timestamp", timestamp,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesWriteWithTagsCommand writes data with tags.
// Timeseries operations are supported via gRPC, REST, and MCP transports.
func TimeseriesWriteWithTagsCommand(metric string, value float64, tags map[string]string) CommandDef {
	args := []string{"timeseries", "write", "--metric", metric, "--value", fmt.Sprintf("%f", value)}
	for k, v := range tags {
		args = append(args, "--tags", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries write %s with tags", metric),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesWriteFullCommand writes data with all options.
// Timeseries operations are supported via gRPC, REST, and MCP transports.
func TimeseriesWriteFullCommand(metric string, value float64, tags map[string]string, timestamp string) CommandDef {
	args := []string{
		"timeseries", "write",
		"--metric", metric,
		"--value", fmt.Sprintf("%f", value),
	}
	for k, v := range tags {
		args = append(args, "--tags", fmt.Sprintf("%s=%s", k, v))
	}
	if timestamp != "" {
		args = append(args, "--timestamp", timestamp)
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries write full %s", metric),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesWriteBatchCommand writes data from a JSON file.
// Timeseries operations are supported via gRPC, REST, and MCP transports.
func TimeseriesWriteBatchCommand(jsonFile string) CommandDef {
	return CommandDef{
		Name: "timeseries write batch",
		Args: []string{
			"timeseries", "write",
			"--batch", jsonFile,
		},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// =============================================================================
// Timeseries Query Command Definitions
// =============================================================================

// TimeseriesQueryCommand returns a basic query command.
func TimeseriesQueryCommand(metric, startTime, endTime string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries query %s", metric),
		Args: []string{
			"timeseries", "query",
			"--metric", metric,
			"--start", startTime,
			"--end", endTime,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesQueryWithTagsCommand queries data filtered by tags.
func TimeseriesQueryWithTagsCommand(metric, startTime, endTime string, tags map[string]string) CommandDef {
	args := []string{
		"timeseries", "query",
		"--metric", metric,
		"--start", startTime,
		"--end", endTime,
	}
	for k, v := range tags {
		args = append(args, "--tags", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries query %s with tags", metric),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesQueryWithLimitCommand queries data with a limit.
func TimeseriesQueryWithLimitCommand(metric, startTime, endTime string, limit int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries query %s limit %d", metric, limit),
		Args: []string{
			"timeseries", "query",
			"--metric", metric,
			"--start", startTime,
			"--end", endTime,
			"--limit", fmt.Sprintf("%d", limit),
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesQueryWithOrderCommand queries data with ordering.
func TimeseriesQueryWithOrderCommand(metric, startTime, endTime, order string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries query %s order %s", metric, order),
		Args: []string{
			"timeseries", "query",
			"--metric", metric,
			"--start", startTime,
			"--end", endTime,
			"--order", order,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesQueryJSONCommand queries with JSON output.
func TimeseriesQueryJSONCommand(metric, startTime, endTime string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries query %s json", metric),
		Args: []string{
			"timeseries", "query",
			"--metric", metric,
			"--start", startTime,
			"--end", endTime,
			"--output", "json",
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// =============================================================================
// Timeseries Aggregate Command Definitions
// =============================================================================

// TimeseriesAggregateCommand returns a basic aggregation command.
func TimeseriesAggregateCommand(metric, startTime, endTime, aggregation string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries query %s agg %s", metric, aggregation),
		Args: []string{
			"timeseries", "query",
			"--metric", metric,
			"--start", startTime,
			"--end", endTime,
			"--aggregation", aggregation,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesAggregateWithIntervalCommand returns an aggregation command with interval.
func TimeseriesAggregateWithIntervalCommand(metric, startTime, endTime, aggregation string, intervalSec int) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries query %s agg %s interval %ds", metric, aggregation, intervalSec),
		Args: []string{
			"timeseries", "query",
			"--metric", metric,
			"--start", startTime,
			"--end", endTime,
			"--aggregation", aggregation,
			"--interval", fmt.Sprintf("%d", intervalSec),
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesAggregateWithGroupByCommand returns an aggregation command with group-by.
func TimeseriesAggregateWithGroupByCommand(metric, startTime, endTime, aggregation string, groupBy []string) CommandDef {
	args := []string{
		"timeseries", "query",
		"--metric", metric,
		"--start", startTime,
		"--end", endTime,
		"--aggregation", aggregation,
	}
	for _, g := range groupBy {
		args = append(args, "--group-by", g)
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries query %s agg %s group-by %v", metric, aggregation, groupBy),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesAggregateFullCommand returns a full aggregation command with all options.
func TimeseriesAggregateFullCommand(metric, startTime, endTime, aggregation string, intervalSec int, groupBy []string, tags map[string]string) CommandDef {
	args := []string{
		"timeseries", "query",
		"--metric", metric,
		"--start", startTime,
		"--end", endTime,
		"--aggregation", aggregation,
		"--interval", fmt.Sprintf("%d", intervalSec),
	}
	for _, g := range groupBy {
		args = append(args, "--group-by", g)
	}
	for k, v := range tags {
		args = append(args, "--tags", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries query full %s agg %s", metric, aggregation),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// =============================================================================
// Timeseries Delete Command Definitions
// =============================================================================

// TimeseriesDeleteCommand returns a delete command for a metric.
func TimeseriesDeleteCommand(metric string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("timeseries delete %s", metric),
		Args:           []string{"timeseries", "delete", "--metric", metric},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesDeleteWithTimeRangeCommand deletes data within a time range.
func TimeseriesDeleteWithTimeRangeCommand(metric, startTime, endTime string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries delete %s with time range", metric),
		Args: []string{
			"timeseries", "delete",
			"--metric", metric,
			"--start", startTime,
			"--end", endTime,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesDeleteWithTagsCommand deletes data matching specific tags.
func TimeseriesDeleteWithTagsCommand(metric string, tags map[string]string) CommandDef {
	args := []string{"timeseries", "delete", "--metric", metric}
	for k, v := range tags {
		args = append(args, "--tags", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries delete %s with tags", metric),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesDeleteFullCommand deletes with all options.
func TimeseriesDeleteFullCommand(metric, startTime, endTime string, tags map[string]string) CommandDef {
	args := []string{
		"timeseries", "delete",
		"--metric", metric,
		"--start", startTime,
		"--end", endTime,
	}
	for k, v := range tags {
		args = append(args, "--tags", fmt.Sprintf("%s=%s", k, v))
	}
	return CommandDef{
		Name:           fmt.Sprintf("timeseries delete full %s", metric),
		Args:           args,
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// =============================================================================
// Timeseries Retention Command Definitions
// =============================================================================

// TimeseriesGetRetentionCommand gets the retention policy for a metric.
func TimeseriesGetRetentionCommand(metric string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("timeseries get-retention %s", metric),
		Args:           []string{"timeseries", "get-retention", metric},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesSetRetentionCommand sets a retention policy for a metric.
func TimeseriesSetRetentionCommand(metric string, durationSec int64) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries set-retention %s %ds", metric, durationSec),
		Args: []string{
			"timeseries", "set-retention", metric,
			"--duration", fmt.Sprintf("%d", durationSec),
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesSetRetentionWithResolutionCommand sets retention with resolution.
func TimeseriesSetRetentionWithResolutionCommand(metric string, durationSec, resolutionSec int64) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries set-retention %s %ds resolution %ds", metric, durationSec, resolutionSec),
		Args: []string{
			"timeseries", "set-retention", metric,
			"--duration", fmt.Sprintf("%d", durationSec),
			"--resolution", fmt.Sprintf("%d", resolutionSec),
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesSetRetentionFullCommand sets retention with all options.
func TimeseriesSetRetentionFullCommand(metric string, durationSec, resolutionSec int64, aggregation string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries set-retention full %s", metric),
		Args: []string{
			"timeseries", "set-retention", metric,
			"--duration", fmt.Sprintf("%d", durationSec),
			"--resolution", fmt.Sprintf("%d", resolutionSec),
			"--aggregation", aggregation,
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesRetentionJSONCommand gets retention in JSON format.
func TimeseriesRetentionJSONCommand(metric string) CommandDef {
	return CommandDef{
		Name: fmt.Sprintf("timeseries get-retention %s json", metric),
		Args: []string{
			"timeseries", "get-retention", metric,
			"--output", "json",
		},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// =============================================================================
// Timeseries List Metrics Command Definitions
// =============================================================================

// TimeseriesListMetricsCommand lists all metrics.
func TimeseriesListMetricsCommand() CommandDef {
	return CommandDef{
		Name:           "timeseries list-metrics",
		Args:           []string{"timeseries", "list-metrics"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesListMetricsWithPrefixCommand lists metrics with a prefix filter.
func TimeseriesListMetricsWithPrefixCommand(prefix string) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("timeseries list-metrics prefix %s", prefix),
		Args:           []string{"timeseries", "list-metrics", "--prefix", prefix},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesListMetricsWithLimitCommand lists metrics with a limit.
func TimeseriesListMetricsWithLimitCommand(limit int) CommandDef {
	return CommandDef{
		Name:           fmt.Sprintf("timeseries list-metrics limit %d", limit),
		Args:           []string{"timeseries", "list-metrics", "--limit", fmt.Sprintf("%d", limit)},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// TimeseriesListMetricsJSONCommand lists metrics in JSON format.
func TimeseriesListMetricsJSONCommand() CommandDef {
	return CommandDef{
		Name:           "timeseries list-metrics json",
		Args:           []string{"timeseries", "list-metrics", "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        30 * time.Second,
		RequiresUnseal: true,
		SkipTransports: tsTransportsToSkip,
	}
}

// =============================================================================
// Timeseries Help Command Definitions
// =============================================================================

// TimeseriesHelpCommand shows timeseries help.
// Help commands are CLI-local and work on all transports.
func TimeseriesHelpCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries help",
		Args:          []string{"timeseries", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"write", "query", "delete"},
		Timeout:       10 * time.Second,
	}
}

// TimeseriesWriteHelpCommand shows write subcommand help.
// Help commands are CLI-local and work on all transports.
func TimeseriesWriteHelpCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries write help",
		Args:          []string{"timeseries", "write", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"metric", "value"},
		Timeout:       10 * time.Second,
	}
}

// TimeseriesQueryHelpCommand shows query subcommand help.
// Help commands are CLI-local and work on all transports.
func TimeseriesQueryHelpCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries query help",
		Args:          []string{"timeseries", "query", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"metric", "start", "end"},
		Timeout:       10 * time.Second,
	}
}

// TimeseriesDeleteHelpCommand shows delete subcommand help.
// Help commands are CLI-local and work on all transports.
func TimeseriesDeleteHelpCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries delete help",
		Args:          []string{"timeseries", "delete", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"metric"},
		Timeout:       10 * time.Second,
	}
}

// TimeseriesRetentionHelpCommand shows retention subcommand help.
// Help commands are CLI-local and work on all transports.
func TimeseriesRetentionHelpCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries set-retention help",
		Args:          []string{"timeseries", "set-retention", "--help"},
		ExpectSuccess: true,
		ExpectOutput:  []string{"duration"},
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Timeseries Error Command Definitions
// =============================================================================

// TimeseriesWriteMissingMetricCommand tests missing metric error.
// Error validation is CLI-local and works on all transports.
func TimeseriesWriteMissingMetricCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries write error missing metric",
		Args:          []string{"timeseries", "write", "--value", "100.0"},
		ExpectSuccess: false,
		ExpectError:   []string{"metric"},
		Timeout:       10 * time.Second,
	}
}

// TimeseriesWriteMissingValueCommand tests missing value error.
// Error validation is CLI-local and works on all transports.
func TimeseriesWriteMissingValueCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries write error missing value",
		Args:          []string{"timeseries", "write", "--metric", "test.metric"},
		ExpectSuccess: false,
		ExpectError:   []string{"value"},
		Timeout:       10 * time.Second,
	}
}

// TimeseriesQueryMissingMetricCommand tests missing metric in query.
// Error validation is CLI-local and works on all transports.
func TimeseriesQueryMissingMetricCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries query error missing metric",
		Args:          []string{"timeseries", "query", "--start", "2024-01-01T00:00:00Z", "--end", "2024-01-02T00:00:00Z"},
		ExpectSuccess: false,
		ExpectError:   []string{"metric"},
		Timeout:       10 * time.Second,
	}
}

// TimeseriesQueryMissingTimeRangeCommand tests missing time range.
// Error validation is CLI-local and works on all transports.
func TimeseriesQueryMissingTimeRangeCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries query error missing time range",
		Args:          []string{"timeseries", "query", "--metric", "test.metric"},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// TimeseriesQueryInvalidTimeFormatCommand tests invalid time format.
// Error validation is CLI-local and works on all transports.
func TimeseriesQueryInvalidTimeFormatCommand() CommandDef {
	return CommandDef{
		Name: "timeseries query error invalid time format",
		Args: []string{
			"timeseries", "query",
			"--metric", "test.metric",
			"--start", "invalid-time",
			"--end", "also-invalid",
		},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// TimeseriesSetRetentionMissingDurationCommand tests missing duration.
// Error validation is CLI-local and works on all transports.
func TimeseriesSetRetentionMissingDurationCommand(metric string) CommandDef {
	return CommandDef{
		Name:          fmt.Sprintf("timeseries set-retention error missing duration %s", metric),
		Args:          []string{"timeseries", "set-retention", metric},
		ExpectSuccess: false,
		ExpectError:   []string{"duration"},
		Timeout:       10 * time.Second,
	}
}

// TimeseriesDeleteMissingMetricCommand tests missing metric in delete.
// Error validation is CLI-local and works on all transports.
func TimeseriesDeleteMissingMetricCommand() CommandDef {
	return CommandDef{
		Name:          "timeseries delete error missing metric",
		Args:          []string{"timeseries", "delete"},
		ExpectSuccess: false,
		ExpectError:   []string{"metric"},
		Timeout:       10 * time.Second,
	}
}

// TimeseriesAggregateInvalidTypeCommand tests invalid aggregation type.
// Error validation is CLI-local and works on all transports.
func TimeseriesAggregateInvalidTypeCommand() CommandDef {
	return CommandDef{
		Name: "timeseries query error invalid aggregation",
		Args: []string{
			"timeseries", "query",
			"--metric", "test.metric",
			"--start", "2024-01-01T00:00:00Z",
			"--end", "2024-01-02T00:00:00Z",
			"--aggregation", "invalid_type",
		},
		ExpectSuccess: false,
		Timeout:       10 * time.Second,
	}
}

// =============================================================================
// Tests - Help Commands
// =============================================================================

// TestTimeseriesHelp tests timeseries help commands.
// Help commands are CLI-local and work on all transports.
func TestTimeseriesHelp(t *testing.T) {
	RunForAllTransports(t, "TimeseriesHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesHelpCommand())
	})

	RunForAllTransports(t, "TimeseriesWriteHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesWriteHelpCommand())
	})

	RunForAllTransports(t, "TimeseriesQueryHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesQueryHelpCommand())
	})

	RunForAllTransports(t, "TimeseriesDeleteHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesDeleteHelpCommand())
	})

	RunForAllTransports(t, "TimeseriesRetentionHelp", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesRetentionHelpCommand())
	})
}

// =============================================================================
// Tests - Write Operations
// =============================================================================

// TestTimeseriesWriteBasic tests basic write operations.
func TestTimeseriesWriteBasic(t *testing.T) {
	RunForTransports(t, "WriteBasic", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.write.basic.%d", time.Now().UnixNano())

		// Write a single data point
		cmd := TimeseriesWriteCommand(metric, 42.5)
		_, _, err := runner.Run(cmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesWriteWithTimestamp tests write with explicit timestamp.
func TestTimeseriesWriteWithTimestamp(t *testing.T) {
	RunForTransports(t, "WriteWithTimestamp", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.write.timestamp.%d", time.Now().UnixNano())
		timestamp := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)

		cmd := TimeseriesWriteWithTimestampCommand(metric, 123.45, timestamp)
		_, _, err := runner.Run(cmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesWriteWithTags tests write with tags.
func TestTimeseriesWriteWithTags(t *testing.T) {
	RunForTransports(t, "WriteWithTags", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.write.tags.%d", time.Now().UnixNano())
		tags := map[string]string{
			"host":   "server1",
			"region": "us-east-1",
			"env":    "production",
		}

		cmd := TimeseriesWriteWithTagsCommand(metric, 99.9, tags)
		_, _, err := runner.Run(cmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesWriteMultipleDataPoints tests writing multiple data points.
func TestTimeseriesWriteMultipleDataPoints(t *testing.T) {
	RunForTransports(t, "WriteMultiple", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.write.multiple.%d", time.Now().UnixNano())

		// Write multiple data points
		values := []float64{10.0, 20.0, 30.0, 40.0, 50.0}
		for _, v := range values {
			cmd := TimeseriesWriteCommand(metric, v)
			_, _, err := runner.Run(cmd)
			require.NoError(t, err)
		}

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// =============================================================================
// Tests - Query Operations
// =============================================================================

// TestTimeseriesQueryBasic tests basic query operations.
func TestTimeseriesQueryBasic(t *testing.T) {
	RunForTransports(t, "QueryBasic", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.query.basic.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write test data
		writeCmd := TimeseriesWriteCommand(metric, 75.0)
		_, _, err := runner.Run(writeCmd)
		require.NoError(t, err)

		// Query data
		queryCmd := TimeseriesQueryCommand(metric, startTime, endTime)
		_, _, err = runner.Run(queryCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesQueryWithTags tests query with tag filtering.
func TestTimeseriesQueryWithTags(t *testing.T) {
	RunForTransports(t, "QueryWithTags", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.query.tags.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write data with different tags
		tags1 := map[string]string{"host": "server1"}
		tags2 := map[string]string{"host": "server2"}

		writeCmd1 := TimeseriesWriteWithTagsCommand(metric, 100.0, tags1)
		_, _, err := runner.Run(writeCmd1)
		require.NoError(t, err)

		writeCmd2 := TimeseriesWriteWithTagsCommand(metric, 200.0, tags2)
		_, _, err = runner.Run(writeCmd2)
		require.NoError(t, err)

		// Query with tag filter
		queryCmd := TimeseriesQueryWithTagsCommand(metric, startTime, endTime, tags1)
		_, _, err = runner.Run(queryCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesQueryWithLimit tests query with limit.
func TestTimeseriesQueryWithLimit(t *testing.T) {
	RunForTransports(t, "QueryWithLimit", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.query.limit.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write multiple data points
		for i := 0; i < 10; i++ {
			writeCmd := TimeseriesWriteCommand(metric, float64(i*10))
			_, _, err := runner.Run(writeCmd)
			require.NoError(t, err)
		}

		// Query with limit
		queryCmd := TimeseriesQueryWithLimitCommand(metric, startTime, endTime, 5)
		_, _, err := runner.Run(queryCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesQueryJSON tests query with JSON output.
func TestTimeseriesQueryJSON(t *testing.T) {
	RunForTransports(t, "QueryJSON", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.query.json.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write test data
		writeCmd := TimeseriesWriteCommand(metric, 42.0)
		_, _, err := runner.Run(writeCmd)
		require.NoError(t, err)

		// Query with JSON output
		queryCmd := TimeseriesQueryJSONCommand(metric, startTime, endTime)
		stdout, _, err := runner.Run(queryCmd)
		require.NoError(t, err)

		// Verify JSON output
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// =============================================================================
// Tests - Aggregate Operations
// =============================================================================

// TestTimeseriesAggregateSum tests sum aggregation.
func TestTimeseriesAggregateSum(t *testing.T) {
	RunForTransports(t, "AggregateSum", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.agg.sum.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write test data
		values := []float64{10.0, 20.0, 30.0}
		for _, v := range values {
			writeCmd := TimeseriesWriteCommand(metric, v)
			_, _, err := runner.Run(writeCmd)
			require.NoError(t, err)
		}

		// Query with sum aggregation
		aggCmd := TimeseriesAggregateCommand(metric, startTime, endTime, "sum")
		_, _, err := runner.Run(aggCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesAggregateAvg tests average aggregation.
func TestTimeseriesAggregateAvg(t *testing.T) {
	RunForTransports(t, "AggregateAvg", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.agg.avg.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write test data
		values := []float64{10.0, 20.0, 30.0}
		for _, v := range values {
			writeCmd := TimeseriesWriteCommand(metric, v)
			_, _, err := runner.Run(writeCmd)
			require.NoError(t, err)
		}

		// Query with avg aggregation
		aggCmd := TimeseriesAggregateCommand(metric, startTime, endTime, "avg")
		_, _, err := runner.Run(aggCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesAggregateMin tests min aggregation.
func TestTimeseriesAggregateMin(t *testing.T) {
	RunForTransports(t, "AggregateMin", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.agg.min.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write test data
		values := []float64{10.0, 20.0, 30.0}
		for _, v := range values {
			writeCmd := TimeseriesWriteCommand(metric, v)
			_, _, err := runner.Run(writeCmd)
			require.NoError(t, err)
		}

		// Query with min aggregation
		aggCmd := TimeseriesAggregateCommand(metric, startTime, endTime, "min")
		_, _, err := runner.Run(aggCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesAggregateMax tests max aggregation.
func TestTimeseriesAggregateMax(t *testing.T) {
	RunForTransports(t, "AggregateMax", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.agg.max.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write test data
		values := []float64{10.0, 20.0, 30.0}
		for _, v := range values {
			writeCmd := TimeseriesWriteCommand(metric, v)
			_, _, err := runner.Run(writeCmd)
			require.NoError(t, err)
		}

		// Query with max aggregation
		aggCmd := TimeseriesAggregateCommand(metric, startTime, endTime, "max")
		_, _, err := runner.Run(aggCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesAggregateCount tests count aggregation.
func TestTimeseriesAggregateCount(t *testing.T) {
	RunForTransports(t, "AggregateCount", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.agg.count.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write test data
		values := []float64{10.0, 20.0, 30.0}
		for _, v := range values {
			writeCmd := TimeseriesWriteCommand(metric, v)
			_, _, err := runner.Run(writeCmd)
			require.NoError(t, err)
		}

		// Query with count aggregation
		aggCmd := TimeseriesAggregateCommand(metric, startTime, endTime, "count")
		_, _, err := runner.Run(aggCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesAggregateWithInterval tests aggregation with interval.
func TestTimeseriesAggregateWithInterval(t *testing.T) {
	RunForTransports(t, "AggregateWithInterval", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.agg.interval.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write test data
		values := []float64{10.0, 20.0, 30.0}
		for _, v := range values {
			writeCmd := TimeseriesWriteCommand(metric, v)
			_, _, err := runner.Run(writeCmd)
			require.NoError(t, err)
		}

		// Query with interval
		aggCmd := TimeseriesAggregateWithIntervalCommand(metric, startTime, endTime, "avg", 300)
		_, _, err := runner.Run(aggCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesAggregateWithGroupBy tests aggregation with group-by.
func TestTimeseriesAggregateWithGroupBy(t *testing.T) {
	RunForTransports(t, "AggregateWithGroupBy", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.agg.groupby.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write test data with tags
		tags := map[string]string{"host": "server1", "region": "us-east-1"}
		writeCmd := TimeseriesWriteWithTagsCommand(metric, 42.0, tags)
		_, _, err := runner.Run(writeCmd)
		require.NoError(t, err)

		// Query with group-by
		aggCmd := TimeseriesAggregateWithGroupByCommand(metric, startTime, endTime, "sum", []string{"host"})
		_, _, err = runner.Run(aggCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesAggregateAllTypes tests all aggregation types.
func TestTimeseriesAggregateAllTypes(t *testing.T) {
	aggregations := []string{"sum", "avg", "min", "max", "count"}

	for _, agg := range aggregations {
		t.Run(fmt.Sprintf("Aggregate_%s", agg), func(t *testing.T) {
			RunForTransports(t, fmt.Sprintf("Aggregate_%s", agg), tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				if !runner.RequireTimeseriesAvailable() {
					return
				}
				metric := fmt.Sprintf("test.agg.all.%s.%d", agg, time.Now().UnixNano())
				startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
				endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

				// Write test data
				values := []float64{10.0, 20.0, 30.0}
				for _, v := range values {
					writeCmd := TimeseriesWriteCommand(metric, v)
					_, _, err := runner.Run(writeCmd)
					require.NoError(t, err)
				}

				// Query with aggregation
				aggCmd := TimeseriesAggregateCommand(metric, startTime, endTime, agg)
				_, _, err := runner.Run(aggCmd)
				require.NoError(t, err)

				// Cleanup
				cleanupTimeseriesMetric(t, runner, metric)
			})
		})
	}
}

// =============================================================================
// Tests - Delete Operations
// =============================================================================

// TestTimeseriesDeleteBasic tests basic delete operations.
func TestTimeseriesDeleteBasic(t *testing.T) {
	RunForTransports(t, "DeleteBasic", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.delete.basic.%d", time.Now().UnixNano())

		// Write test data
		writeCmd := TimeseriesWriteCommand(metric, 42.0)
		_, _, err := runner.Run(writeCmd)
		require.NoError(t, err)

		// Delete all data for metric
		deleteCmd := TimeseriesDeleteCommand(metric)
		_, _, err = runner.Run(deleteCmd)
		require.NoError(t, err)
	})
}

// TestTimeseriesDeleteWithTimeRange tests delete with time range.
func TestTimeseriesDeleteWithTimeRange(t *testing.T) {
	RunForTransports(t, "DeleteWithTimeRange", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.delete.timerange.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Write test data
		writeCmd := TimeseriesWriteCommand(metric, 42.0)
		_, _, err := runner.Run(writeCmd)
		require.NoError(t, err)

		// Delete data within time range
		deleteCmd := TimeseriesDeleteWithTimeRangeCommand(metric, startTime, endTime)
		_, _, err = runner.Run(deleteCmd)
		require.NoError(t, err)
	})
}

// TestTimeseriesDeleteWithTags tests delete with tag filter.
func TestTimeseriesDeleteWithTags(t *testing.T) {
	RunForTransports(t, "DeleteWithTags", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.delete.tags.%d", time.Now().UnixNano())
		tags := map[string]string{"host": "server-to-delete"}

		// Write test data with tags
		writeCmd := TimeseriesWriteWithTagsCommand(metric, 42.0, tags)
		_, _, err := runner.Run(writeCmd)
		require.NoError(t, err)

		// Delete data matching tags
		deleteCmd := TimeseriesDeleteWithTagsCommand(metric, tags)
		_, _, err = runner.Run(deleteCmd)
		require.NoError(t, err)
	})
}

// =============================================================================
// Tests - Retention Operations
// =============================================================================

// TestTimeseriesRetentionBasic tests basic retention policy operations.
func TestTimeseriesRetentionBasic(t *testing.T) {
	RunForTransports(t, "RetentionBasic", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.retention.basic.%d", time.Now().UnixNano())

		// Write test data to create metric
		writeCmd := TimeseriesWriteCommand(metric, 42.0)
		_, _, err := runner.Run(writeCmd)
		require.NoError(t, err)

		// Set retention policy (30 days)
		setCmd := TimeseriesSetRetentionCommand(metric, 2592000)
		_, _, err = runner.Run(setCmd)
		require.NoError(t, err)

		// Get retention policy
		getCmd := TimeseriesGetRetentionCommand(metric)
		_, _, err = runner.Run(getCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesRetentionWithResolution tests retention with resolution.
func TestTimeseriesRetentionWithResolution(t *testing.T) {
	RunForTransports(t, "RetentionWithResolution", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.retention.resolution.%d", time.Now().UnixNano())

		// Write test data to create metric
		writeCmd := TimeseriesWriteCommand(metric, 42.0)
		_, _, err := runner.Run(writeCmd)
		require.NoError(t, err)

		// Set retention with resolution
		setCmd := TimeseriesSetRetentionWithResolutionCommand(metric, 7776000, 300)
		_, _, err = runner.Run(setCmd)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesRetentionFull tests full retention with all options.
func TestTimeseriesRetentionFull(t *testing.T) {
	RunForTransports(t, "RetentionFull", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.retention.full.%d", time.Now().UnixNano())

		// Write test data to create metric
		writeCmd := TimeseriesWriteCommand(metric, 42.0)
		_, _, err := runner.Run(writeCmd)
		require.NoError(t, err)

		// Set full retention policy
		setCmd := TimeseriesSetRetentionFullCommand(metric, 15552000, 600, "avg")
		_, _, err = runner.Run(setCmd)
		require.NoError(t, err)

		// Get retention in JSON format
		getJSONCmd := TimeseriesRetentionJSONCommand(metric)
		stdout, _, err := runner.Run(getJSONCmd)
		require.NoError(t, err)

		// Verify JSON output
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)

		// Cleanup
		cleanupTimeseriesMetric(t, runner, metric)
	})
}

// TestTimeseriesRetentionVariousDurations tests various retention durations.
func TestTimeseriesRetentionVariousDurations(t *testing.T) {
	durations := []struct {
		name        string
		durationSec int64
	}{
		{"1Hour", 3600},
		{"1Day", 86400},
		{"7Days", 604800},
		{"30Days", 2592000},
		{"90Days", 7776000},
		{"365Days", 31536000},
	}

	for _, d := range durations {
		t.Run(fmt.Sprintf("Retention_%s", d.name), func(t *testing.T) {
			RunForTransports(t, fmt.Sprintf("Retention_%s", d.name), tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				if !runner.RequireTimeseriesAvailable() {
					return
				}
				metric := fmt.Sprintf("test.retention.%s.%d", d.name, time.Now().UnixNano())

				// Write test data to create metric
				writeCmd := TimeseriesWriteCommand(metric, 42.0)
				_, _, err := runner.Run(writeCmd)
				require.NoError(t, err)

				// Set retention
				setCmd := TimeseriesSetRetentionCommand(metric, d.durationSec)
				_, _, err = runner.Run(setCmd)
				require.NoError(t, err)

				// Get retention
				getCmd := TimeseriesGetRetentionCommand(metric)
				_, _, err = runner.Run(getCmd)
				require.NoError(t, err)

				// Cleanup
				cleanupTimeseriesMetric(t, runner, metric)
			})
		})
	}
}

// =============================================================================
// Tests - List Metrics
// =============================================================================

// TestTimeseriesListMetrics tests metric listing operations.
func TestTimeseriesListMetrics(t *testing.T) {
	RunForTransports(t, "ListMetrics", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		prefix := fmt.Sprintf("test.list.%d", time.Now().UnixNano())
		metrics := []string{
			prefix + ".metric1",
			prefix + ".metric2",
			prefix + ".metric3",
		}

		// Write data to create metrics
		for _, m := range metrics {
			writeCmd := TimeseriesWriteCommand(m, 42.0)
			_, _, err := runner.Run(writeCmd)
			require.NoError(t, err)
		}

		// List all metrics
		listCmd := TimeseriesListMetricsCommand()
		_, _, err := runner.Run(listCmd)
		require.NoError(t, err)

		// List with prefix
		listPrefixCmd := TimeseriesListMetricsWithPrefixCommand(prefix)
		_, _, err = runner.Run(listPrefixCmd)
		require.NoError(t, err)

		// List with limit
		listLimitCmd := TimeseriesListMetricsWithLimitCommand(2)
		_, _, err = runner.Run(listLimitCmd)
		require.NoError(t, err)

		// List in JSON format
		listJSONCmd := TimeseriesListMetricsJSONCommand()
		stdout, _, err := runner.Run(listJSONCmd)
		require.NoError(t, err)

		// Verify JSON output
		var result map[string]interface{}
		err = json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err)

		// Cleanup
		for _, m := range metrics {
			cleanupTimeseriesMetric(t, runner, m)
		}
	})
}

// =============================================================================
// Tests - Error Handling
// =============================================================================

// TestTimeseriesWriteErrors tests write error handling.
// Error validation is CLI-local and works on all transports.
func TestTimeseriesWriteErrors(t *testing.T) {
	RunForAllTransports(t, "WriteMissingMetric", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesWriteMissingMetricCommand())
	})

	RunForAllTransports(t, "WriteMissingValue", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesWriteMissingValueCommand())
	})
}

// TestTimeseriesQueryErrors tests query error handling.
// Error validation is CLI-local and works on all transports.
func TestTimeseriesQueryErrors(t *testing.T) {
	RunForAllTransports(t, "QueryMissingMetric", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesQueryMissingMetricCommand())
	})

	RunForAllTransports(t, "QueryMissingTimeRange", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesQueryMissingTimeRangeCommand())
	})

	RunForAllTransports(t, "QueryInvalidTimeFormat", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesQueryInvalidTimeFormatCommand())
	})
}

// TestTimeseriesDeleteErrors tests delete error handling.
// Error validation is CLI-local and works on all transports.
func TestTimeseriesDeleteErrors(t *testing.T) {
	RunForAllTransports(t, "DeleteMissingMetric", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesDeleteMissingMetricCommand())
	})
}

// TestTimeseriesRetentionErrors tests retention error handling.
// Error validation is CLI-local and works on all transports.
func TestTimeseriesRetentionErrors(t *testing.T) {
	RunForAllTransports(t, "RetentionMissingDuration", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesSetRetentionMissingDurationCommand("test.metric"))
	})
}

// TestTimeseriesAggregateErrors tests aggregate error handling.
// Error validation is CLI-local and works on all transports.
func TestTimeseriesAggregateErrors(t *testing.T) {
	RunForAllTransports(t, "AggregateInvalidType", func(t *testing.T, runner *TestRunner) {
		runner.RunAndAssert(TimeseriesAggregateInvalidTypeCommand())
	})
}

// =============================================================================
// Tests - Complete Lifecycle
// =============================================================================

// TestTimeseriesLifecycle tests complete timeseries data lifecycle.
func TestTimeseriesLifecycle(t *testing.T) {
	RunForTransports(t, "Lifecycle", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.lifecycle.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// 1. Write data points
		tags := map[string]string{
			"host":   "server1",
			"region": "us-east-1",
		}
		writeCmd := TimeseriesWriteWithTagsCommand(metric, 75.5, tags)
		_, _, err := runner.Run(writeCmd)
		require.NoError(t, err)

		// Write more data points
		writeCmd2 := TimeseriesWriteWithTagsCommand(metric, 82.3, map[string]string{"host": "server2", "region": "us-east-1"})
		_, _, err = runner.Run(writeCmd2)
		require.NoError(t, err)

		// 2. Query data
		queryCmd := TimeseriesQueryCommand(metric, startTime, endTime)
		_, _, err = runner.Run(queryCmd)
		require.NoError(t, err)

		// 3. Query with aggregation
		aggCmd := TimeseriesAggregateCommand(metric, startTime, endTime, "avg")
		_, _, err = runner.Run(aggCmd)
		require.NoError(t, err)

		// 4. Set retention policy
		retCmd := TimeseriesSetRetentionCommand(metric, 2592000)
		_, _, err = runner.Run(retCmd)
		require.NoError(t, err)

		// 5. Get retention policy
		getRetCmd := TimeseriesGetRetentionCommand(metric)
		_, _, err = runner.Run(getRetCmd)
		require.NoError(t, err)

		// 6. List metrics
		listCmd := TimeseriesListMetricsCommand()
		stdout, _, err := runner.Run(listCmd)
		require.NoError(t, err)
		assert.Contains(t, stdout, metric)

		// 7. Delete data
		deleteCmd := TimeseriesDeleteCommand(metric)
		_, _, err = runner.Run(deleteCmd)
		require.NoError(t, err)
	})
}

// TestTimeseriesLifecycleWithSuite tests lifecycle using predefined suite.
func TestTimeseriesLifecycleWithSuite(t *testing.T) {
	RunForTransports(t, "LifecycleSuite", tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireTimeseriesAvailable() {
			return
		}
		metric := fmt.Sprintf("test.suite.%d", time.Now().UnixNano())
		startTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		endTime := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		// Use the predefined suite from commands.go
		suite := TimeSeriesSuite(metric, startTime, endTime)
		runner.RunSuite(suite)
	})
}

// =============================================================================
// Tests - Output Formats
// =============================================================================

// TestTimeseriesOutputFormats tests all output formats.
func TestTimeseriesOutputFormats(t *testing.T) {
	formats := []string{"table", "json", "yaml"}

	for _, format := range formats {
		t.Run(fmt.Sprintf("ListMetrics_%s", format), func(t *testing.T) {
			RunForTransports(t, fmt.Sprintf("ListMetrics_%s", format), tsTransportsToSkip, func(t *testing.T, runner *TestRunner) {
				if !runner.RequireTimeseriesAvailable() {
					return
				}
				cmd := CommandDef{
					Name:           fmt.Sprintf("timeseries list-metrics %s", format),
					Args:           []string{"timeseries", "list-metrics", "--output", format},
					ExpectSuccess:  true,
					Timeout:        30 * time.Second,
					RequiresUnseal: true,
					SkipTransports: tsTransportsToSkip,
				}
				runner.RunAndAssert(cmd)
			})
		})
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

// cleanupTimeseriesMetric attempts to delete a test metric.
func cleanupTimeseriesMetric(t *testing.T, runner *TestRunner, metric string) {
	t.Helper()

	deleteCmd := CommandDef{
		Name:           fmt.Sprintf("cleanup metric %s", metric),
		Args:           []string{"timeseries", "delete", "--metric", metric},
		SkipTransports: tsTransportsToSkip,
	}
	_, _, _ = runner.Run(deleteCmd)
}

// =============================================================================
// Test Suites
// =============================================================================

// TimeseriesWriteSuite returns commands for write testing.
func TimeseriesWriteSuite(metric string) []CommandDef {
	tags := map[string]string{"host": "testhost"}
	return []CommandDef{
		TimeseriesWriteCommand(metric, 42.0),
		TimeseriesWriteWithTimestampCommand(metric, 50.0, time.Now().Format(time.RFC3339)),
		TimeseriesWriteWithTagsCommand(metric, 60.0, tags),
	}
}

// TimeseriesQuerySuite returns commands for query testing.
func TimeseriesQuerySuite(metric, startTime, endTime string) []CommandDef {
	return []CommandDef{
		TimeseriesQueryCommand(metric, startTime, endTime),
		TimeseriesQueryWithLimitCommand(metric, startTime, endTime, 10),
		TimeseriesQueryJSONCommand(metric, startTime, endTime),
	}
}

// TimeseriesAggregateSuite returns commands for aggregation testing.
func TimeseriesAggregateSuite(metric, startTime, endTime string) []CommandDef {
	return []CommandDef{
		TimeseriesAggregateCommand(metric, startTime, endTime, "sum"),
		TimeseriesAggregateCommand(metric, startTime, endTime, "avg"),
		TimeseriesAggregateCommand(metric, startTime, endTime, "min"),
		TimeseriesAggregateCommand(metric, startTime, endTime, "max"),
		TimeseriesAggregateCommand(metric, startTime, endTime, "count"),
	}
}

// TimeseriesRetentionSuite returns commands for retention testing.
func TimeseriesRetentionSuite(metric string) []CommandDef {
	return []CommandDef{
		TimeseriesSetRetentionCommand(metric, 2592000),
		TimeseriesGetRetentionCommand(metric),
		TimeseriesSetRetentionWithResolutionCommand(metric, 7776000, 300),
	}
}

// TimeseriesHelpSuite returns help commands.
func TimeseriesHelpSuite() []CommandDef {
	return []CommandDef{
		TimeseriesHelpCommand(),
		TimeseriesWriteHelpCommand(),
		TimeseriesQueryHelpCommand(),
		TimeseriesDeleteHelpCommand(),
		TimeseriesRetentionHelpCommand(),
	}
}

// TimeseriesErrorSuite returns error case commands.
func TimeseriesErrorSuite() []CommandDef {
	return []CommandDef{
		TimeseriesWriteMissingMetricCommand(),
		TimeseriesWriteMissingValueCommand(),
		TimeseriesQueryMissingMetricCommand(),
		TimeseriesQueryMissingTimeRangeCommand(),
		TimeseriesQueryInvalidTimeFormatCommand(),
		TimeseriesDeleteMissingMetricCommand(),
		TimeseriesSetRetentionMissingDurationCommand("test.metric"),
		TimeseriesAggregateInvalidTypeCommand(),
	}
}

// generateMetricPrefix generates a unique metric prefix for tests.
func generateMetricPrefix() string {
	return fmt.Sprintf("test.ts.%d", time.Now().UnixNano())
}

// generateTestTags generates common test tags.
func generateTestTags(host, region string) map[string]string {
	return map[string]string{
		"host":   host,
		"region": region,
	}
}

// formatTimeRange returns start and end time strings for a given duration before now.
func formatTimeRange(lookback time.Duration) (startTime, endTime string) {
	now := time.Now()
	startTime = now.Add(-lookback).Format(time.RFC3339)
	endTime = now.Add(1 * time.Hour).Format(time.RFC3339)
	return
}

// stringSliceContains checks if a slice contains a string.
func stringSliceContains(slice []string, str string) bool {
	for _, s := range slice {
		if strings.Contains(s, str) {
			return true
		}
	}
	return false
}
