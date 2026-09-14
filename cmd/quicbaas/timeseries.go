//go:build timeseries

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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	stdlog "log"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

var (
	// TimeSeries flags.
	tsMetric      string
	tsTags        map[string]string
	tsValue       float64
	tsTimestamp   string
	tsStartTime   string
	tsEndTime     string
	tsAggregation string
	tsInterval    int32
	tsGroupBy     []string
	tsLimit       int
	tsPrefix      string
	tsCursor      string

	// Retention policy flags.
	tsDurationSec   int64
	tsResolutionSec int64
)

// validAggregations defines the set of valid aggregation function names.
var validAggregations = map[string]struct{}{
	"sum":   {},
	"avg":   {},
	"min":   {},
	"max":   {},
	"count": {},
}

// newTimeSeriesCommands creates the time series command group.
func newTimeSeriesCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "timeseries",
		Aliases: []string{"ts"},
		Short:   "Time series data commands",
		Long: `Time series data commands for interacting with QuicBaaS time series storage.

These commands allow you to write, query, and manage time series data.
All commands connect to a running QuicBaaS server via gRPC.`,
	}

	// Add persistent flags for all timeseries commands
	// Note: --address and --transport are global flags defined in main.go
	cmd.PersistentFlags().StringVar(&outputFormat, "output", "table", "Output format (json, yaml, table, raw)")

	// TLS flags
	cmd.PersistentFlags().BoolVar(&tlsEnabled, "tls", false, "Enable TLS")
	cmd.PersistentFlags().StringVar(&tlsCert, "tls-cert", "", "TLS client certificate file")
	cmd.PersistentFlags().StringVar(&tlsKey, "tls-key", "", "TLS client key file")
	cmd.PersistentFlags().StringVar(&tlsCA, "tls-ca", "", "TLS CA certificate file")

	// Connection flags
	cmd.PersistentFlags().IntVar(&maxRetries, "max-retries", 3, "Maximum number of retry attempts")
	cmd.PersistentFlags().DurationVar(&retryBackoff, "retry-backoff", 100*time.Millisecond, "Initial retry backoff duration")

	// Add subcommands
	cmd.AddCommand(newTSWriteCommand())
	cmd.AddCommand(newTSWriteBatchCommand())
	cmd.AddCommand(newTSQueryCommand())
	cmd.AddCommand(newTSDeleteCommand())
	cmd.AddCommand(newTSGetRetentionCommand())
	cmd.AddCommand(newTSSetRetentionCommand())
	cmd.AddCommand(newTSListMetricsCommand())

	return cmd
}

// newTSWriteCommand creates the write command for single data points.
func newTSWriteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "write",
		Short: "Write a single time series data point",
		Long: `Write a single data point to time series storage.

Examples:
  # Write a CPU usage metric
  quicbaas ts write --metric cpu.usage --value 75.5 --tags host=server1,region=us-east-1

  # Write with explicit timestamp
  quicbaas ts write --metric memory.used --value 4096 --timestamp "2024-01-15T10:00:00Z"`,
		RunE: runTSWriteCommand,
	}

	cmd.Flags().StringVar(&tsMetric, "metric", "", "Metric name (required)")
	cmd.Flags().Float64Var(&tsValue, "value", 0, "Metric value (required)")
	cmd.Flags().StringToStringVar(&tsTags, "tags", nil, "Tags as key=value pairs")
	cmd.Flags().StringVar(&tsTimestamp, "timestamp", "", "Timestamp (RFC3339 format, defaults to now)")

	if err := cmd.MarkFlagRequired("metric"); err != nil {
		stdlog.Fatalf("failed to mark 'metric' flag as required: %v", err)
	}
	if err := cmd.MarkFlagRequired("value"); err != nil {
		stdlog.Fatalf("failed to mark 'value' flag as required: %v", err)
	}

	return cmd
}

// runTSWriteCommand executes the write command.
func runTSWriteCommand(cmd *cobra.Command, args []string) error {
	client, err := timeSeriesClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	// Parse timestamp
	var ts time.Time
	if tsTimestamp != "" {
		ts, err = time.Parse(time.RFC3339, tsTimestamp)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "write", "invalid timestamp format")
		}
	} else {
		ts = time.Now()
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &cli.WritePointRequest{
		Metric:    tsMetric,
		Tags:      tsTags,
		Value:     tsValue,
		Timestamp: ts,
	}

	resp, err := client.TSWritePoint(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatTSWriteResponse(resp)
}

// newTSWriteBatchCommand creates the batch write command.
func newTSWriteBatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "write-batch <json-file>",
		Short: "Write multiple time series data points",
		Long: `Write multiple data points to time series storage atomically.

The input file should be a JSON array of data points.

Example JSON:
[
  {"metric": "cpu.usage", "value": 75.5, "tags": {"host": "server1"}, "timestamp": "2024-01-15T10:00:00Z"},
  {"metric": "cpu.usage", "value": 82.3, "tags": {"host": "server2"}, "timestamp": "2024-01-15T10:00:00Z"}
]

Examples:
  quicbaas ts write-batch points.json`,
		Args: cobra.ExactArgs(1),
		RunE: runTSWriteBatchCommand,
	}

	return cmd
}

// runTSWriteBatchCommand executes the batch write command.
func runTSWriteBatchCommand(cmd *cobra.Command, args []string) error {
	client, err := timeSeriesClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	// Read JSON file
	data, err := os.ReadFile(args[0])
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "write-batch", "failed to read file")
	}

	// Parse points
	var points []struct {
		Metric    string            `json:"metric"`
		Value     float64           `json:"value"`
		Tags      map[string]string `json:"tags"`
		Timestamp string            `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &points); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "write-batch", "invalid JSON")
	}

	// Convert to CLI request
	cliPoints := make([]cli.WritePointRequest, len(points))
	for i, p := range points {
		ts, err := time.Parse(time.RFC3339, p.Timestamp)
		if err != nil {
			ts = time.Now()
		}
		cliPoints[i] = cli.WritePointRequest{
			Metric:    p.Metric,
			Tags:      p.Tags,
			Value:     p.Value,
			Timestamp: ts,
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &cli.WriteBatchRequest{
		Points: cliPoints,
	}

	resp, err := client.TSWriteBatch(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatTSWriteBatchResponse(resp)
}

// newTSQueryCommand creates the query command.
func newTSQueryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query time series data",
		Long: `Query time series data with optional aggregation.

Examples:
  # Query CPU usage for the last hour
  quicbaas ts query --metric cpu.usage --start "2024-01-15T09:00:00Z" --end "2024-01-15T10:00:00Z"

  # Query with aggregation
  quicbaas ts query --metric cpu.usage --start "2024-01-15T09:00:00Z" --end "2024-01-15T10:00:00Z" \
    --aggregation avg --interval 300

  # Query with grouping
  quicbaas ts query --metric cpu.usage --start "2024-01-15T09:00:00Z" --end "2024-01-15T10:00:00Z" \
    --group-by host,region`,
		RunE: runTSQueryCommand,
	}

	cmd.Flags().StringVar(&tsMetric, "metric", "", "Metric name (required)")
	cmd.Flags().StringVar(&tsStartTime, "start", "", "Start time (RFC3339 format, required)")
	cmd.Flags().StringVar(&tsEndTime, "end", "", "End time (RFC3339 format, required)")
	cmd.Flags().StringToStringVar(&tsTags, "tags", nil, "Filter by tags")
	cmd.Flags().StringVar(&tsAggregation, "aggregation", "", "Aggregation function (avg, sum, min, max, count)")
	cmd.Flags().Int32Var(&tsInterval, "interval", 0, "Aggregation interval in seconds")
	cmd.Flags().StringSliceVar(&tsGroupBy, "group-by", nil, "Group by tags")
	cmd.Flags().IntVar(&tsLimit, "limit", 100, "Maximum number of points to return")

	if err := cmd.MarkFlagRequired("metric"); err != nil {
		stdlog.Fatalf("failed to mark 'metric' flag as required: %v", err)
	}
	if err := cmd.MarkFlagRequired("start"); err != nil {
		stdlog.Fatalf("failed to mark 'start' flag as required: %v", err)
	}
	if err := cmd.MarkFlagRequired("end"); err != nil {
		stdlog.Fatalf("failed to mark 'end' flag as required: %v", err)
	}

	return cmd
}

// runTSQueryCommand executes the query command.
func runTSQueryCommand(cmd *cobra.Command, args []string) error {
	client, err := timeSeriesClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	startTime, err := time.Parse(time.RFC3339, tsStartTime)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "query", "invalid start time format")
	}

	endTime, err := time.Parse(time.RFC3339, tsEndTime)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "query", "invalid end time format")
	}

	// Validate aggregation type if provided
	if tsAggregation != "" {
		if _, ok := validAggregations[tsAggregation]; !ok {
			return errors.New(errors.ErrInvalidInput, "query",
				fmt.Sprintf("invalid aggregation type %q: must be one of sum, avg, min, max, count", tsAggregation))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &cli.TSQueryRequest{
		Metric:      tsMetric,
		Tags:        tsTags,
		StartTime:   startTime,
		EndTime:     endTime,
		Aggregation: tsAggregation,
		IntervalSec: tsInterval,
		GroupBy:     tsGroupBy,
		Limit:       tsLimit,
	}

	resp, err := client.TSQuery(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatTSQueryResponse(resp)
}

// newTSDeleteCommand creates the delete command.
func newTSDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete time series data",
		Long: `Delete time series data for a metric.

Examples:
  # Delete all data for a metric
  quicbaas ts delete --metric cpu.usage

  # Delete data with specific tags
  quicbaas ts delete --metric cpu.usage --tags host=server1

  # Delete data within a time range
  quicbaas ts delete --metric cpu.usage --start "2024-01-01T00:00:00Z" --end "2024-01-02T00:00:00Z"`,
		RunE: runTSDeleteCommand,
	}

	cmd.Flags().StringVar(&tsMetric, "metric", "", "Metric name (required)")
	cmd.Flags().StringToStringVar(&tsTags, "tags", nil, "Filter by tags")
	cmd.Flags().StringVar(&tsStartTime, "start", "", "Start time (RFC3339 format)")
	cmd.Flags().StringVar(&tsEndTime, "end", "", "End time (RFC3339 format)")

	if err := cmd.MarkFlagRequired("metric"); err != nil {
		stdlog.Fatalf("failed to mark 'metric' flag as required: %v", err)
	}

	return cmd
}

// runTSDeleteCommand executes the delete command.
func runTSDeleteCommand(cmd *cobra.Command, args []string) error {
	client, err := timeSeriesClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	req := &cli.TSDeleteRequest{
		Metric: tsMetric,
		Tags:   tsTags,
	}

	if tsStartTime != "" && tsEndTime != "" {
		startTime, err := time.Parse(time.RFC3339, tsStartTime)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "delete", "invalid start time format")
		}
		endTime, err := time.Parse(time.RFC3339, tsEndTime)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "delete", "invalid end time format")
		}
		req.StartTime = &startTime
		req.EndTime = &endTime
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.TSDeleteSeries(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatTSDeleteResponse(resp)
}

// newTSGetRetentionCommand creates the get retention policy command.
func newTSGetRetentionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get-retention <metric>",
		Short: "Get retention policy for a metric",
		Long: `Retrieve the retention policy for a metric.

Examples:
  quicbaas ts get-retention cpu.usage`,
		Args: cobra.ExactArgs(1),
		RunE: runTSGetRetentionCommand,
	}

	return cmd
}

// runTSGetRetentionCommand executes the get retention command.
func runTSGetRetentionCommand(cmd *cobra.Command, args []string) error {
	client, err := timeSeriesClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &cli.TSGetRetentionPolicyRequest{
		Metric: args[0],
	}

	resp, err := client.TSGetRetentionPolicy(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatTSGetRetentionResponse(resp)
}

// newTSSetRetentionCommand creates the set retention policy command.
func newTSSetRetentionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-retention <metric>",
		Short: "Set retention policy for a metric",
		Long: `Set the retention policy for a metric.

Examples:
  # Set 30 day retention with 5 minute resolution
  quicbaas ts set-retention cpu.usage --duration 2592000 --resolution 300 --aggregation avg`,
		Args: cobra.ExactArgs(1),
		RunE: runTSSetRetentionCommand,
	}

	cmd.Flags().Int64Var(&tsDurationSec, "duration", 0, "Retention duration in seconds (required)")
	cmd.Flags().Int64Var(&tsResolutionSec, "resolution", 0, "Data resolution in seconds")
	cmd.Flags().StringVar(&tsAggregation, "aggregation", "avg", "Aggregation function for downsampling")

	if err := cmd.MarkFlagRequired("duration"); err != nil {
		stdlog.Fatalf("failed to mark 'duration' flag as required: %v", err)
	}

	return cmd
}

// runTSSetRetentionCommand executes the set retention command.
func runTSSetRetentionCommand(cmd *cobra.Command, args []string) error {
	client, err := timeSeriesClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &cli.TSSetRetentionPolicyRequest{
		Metric: args[0],
		Policy: &cli.TSRetentionPolicy{
			Duration:    time.Duration(tsDurationSec) * time.Second,
			Resolution:  time.Duration(tsResolutionSec) * time.Second,
			Aggregation: tsAggregation,
		},
	}

	resp, err := client.TSSetRetentionPolicy(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatTSSetRetentionResponse(resp)
}

// newTSListMetricsCommand creates the list metrics command.
func newTSListMetricsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-metrics",
		Short: "List available metrics",
		Long: `List all available metrics in time series storage.

Examples:
  # List all metrics
  quicbaas ts list-metrics

  # List metrics with prefix
  quicbaas ts list-metrics --prefix cpu

  # List with pagination
  quicbaas ts list-metrics --limit 50 --cursor next_page_token`,
		RunE: runTSListMetricsCommand,
	}

	cmd.Flags().StringVar(&tsPrefix, "prefix", "", "Filter metrics by prefix")
	cmd.Flags().IntVar(&tsLimit, "limit", 100, "Maximum number of metrics to return")
	cmd.Flags().StringVar(&tsCursor, "cursor", "", "Pagination cursor")

	return cmd
}

// runTSListMetricsCommand executes the list metrics command.
func runTSListMetricsCommand(cmd *cobra.Command, args []string) error {
	client, err := timeSeriesClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &cli.TSListMetricsRequest{
		Prefix: tsPrefix,
		Limit:  tsLimit,
		Cursor: tsCursor,
	}

	resp, err := client.TSListMetrics(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatTSListMetricsResponse(resp)
}

// Output formatting functions.

func formatTSWriteResponse(resp *cli.WritePointResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if resp.Success {
			fmt.Println("Data point written successfully")
		} else {
			fmt.Println("Failed to write data point")
		}
	}
	return nil
}

func formatTSWriteBatchResponse(resp *cli.WriteBatchResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if resp.Success {
			fmt.Printf("Batch write successful: %d points written\n", resp.PointsWritten)
		} else {
			fmt.Println("Failed to write batch")
		}
	}
	return nil
}

func formatTSQueryResponse(resp *cli.TSQueryResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Metric: %s\n", resp.Metric)
		fmt.Printf("Points: %d\n", len(resp.Points))
		if resp.AggregatedValue != nil {
			fmt.Printf("Aggregated Value: %.4f\n", *resp.AggregatedValue)
		}
		if len(resp.Points) > 0 {
			fmt.Println("\nData Points:")
			fmt.Println("TIMESTAMP                    VALUE          TAGS")
			fmt.Println("-------------------------------------------------------")
			for _, p := range resp.Points {
				tagsStr := formatTags(p.Tags)
				fmt.Printf("%-28s  %-12.4f  %s\n", p.Timestamp.Format(time.RFC3339), p.Value, tagsStr)
			}
		}
	}
	return nil
}

func formatTSDeleteResponse(resp *cli.TSDeleteResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if resp.Success {
			fmt.Println("Time series data deleted successfully")
		} else {
			fmt.Println("Failed to delete time series data")
		}
	}
	return nil
}

func formatTSGetRetentionResponse(resp *cli.TSGetRetentionPolicyResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Metric: %s\n", resp.Metric)
		if resp.Policy != nil {
			fmt.Printf("Duration: %v\n", resp.Policy.Duration)
			fmt.Printf("Resolution: %v\n", resp.Policy.Resolution)
			fmt.Printf("Aggregation: %s\n", resp.Policy.Aggregation)
		} else {
			fmt.Println("No retention policy set")
		}
	}
	return nil
}

func formatTSSetRetentionResponse(resp *cli.TSSetRetentionPolicyResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		if resp.Success {
			fmt.Println("Retention policy set successfully")
		} else {
			fmt.Println("Failed to set retention policy")
		}
	}
	return nil
}

func formatTSListMetricsResponse(resp *cli.TSListMetricsResponse) error {
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	default:
		fmt.Printf("Metrics (%d):\n", len(resp.Metrics))
		for _, m := range resp.Metrics {
			fmt.Printf("  - %s\n", m)
		}
		if resp.NextCursor != "" {
			fmt.Printf("\nNext cursor: %s\n", resp.NextCursor)
		}
	}
	return nil
}

func formatTags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	var parts []string
	for k, v := range tags {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
