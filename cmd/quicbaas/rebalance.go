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
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
)

// newRebalanceCommands creates the rebalance command group.
func newRebalanceCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rebalance",
		Short: "Cluster rebalancing operations",
		Long: `Commands for managing cluster data rebalancing.

These commands help maintain balanced data distribution across cluster nodes
by moving data from overloaded shards to underutilized ones.

Note: Use the global --address flag to specify the server address.`,
	}

	// Add subcommands
	cmd.AddCommand(newRebalanceStartCommand())
	cmd.AddCommand(newRebalanceStatusCommand())
	cmd.AddCommand(newRebalancePlanCommand())

	return cmd
}

// newRebalanceStartCommand creates the rebalance start command.
func newRebalanceStartCommand() *cobra.Command {
	var (
		targetImbalance float64
		maxConcurrent   int
		minKeys         int64
		dryRun          bool
		rateLimit       int
		outputFormat    string
		follow          bool
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start cluster rebalancing",
		Long: `Start rebalancing data across cluster shards.

This command analyzes the current data distribution and creates a rebalance
plan to move data from overloaded shards to underutilized ones. The process
aims to achieve a balanced distribution based on the target imbalance ratio.

Configuration options:
  - Target imbalance: Maximum acceptable deviation from average (default: 10%)
  - Max concurrent: Number of simultaneous shard transfers (default: 2)
  - Min keys: Minimum keys per shard to trigger rebalance (default: 1000)
  - Dry run: Plan without executing (default: false)
  - Rate limit: Bandwidth limit in KB/s (default: 10 MB/s)

Note: Use the global --address flag to specify the server address.

Examples:
  # Start rebalancing with default settings
  quicbaas rebalance start

  # Start rebalancing with custom imbalance target
  quicbaas rebalance start --target-imbalance 0.05

  # Dry run to see rebalance plan without executing
  quicbaas rebalance start --dry-run

  # Rebalance with limited concurrency and bandwidth
  quicbaas rebalance start \
    --max-concurrent 1 \
    --rate-limit 5120 \
    --follow

  # Connect to a specific server address
  quicbaas --address localhost:50051 rebalance start`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Use global timeout from main.go
			ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
			defer cancel()

			// Create SDK client using global address and transport from main.go
			sdkClient, err := createSDKClient()
			if err != nil {
				return api.NewOperationFailedError("create client", err)
			}
			defer func() { _ = closeSDKClient() }()

			fmt.Println("Starting cluster rebalance...")
			fmt.Printf("  Server:           %s\n", address)
			fmt.Printf("  Target imbalance: %.2f%%\n", targetImbalance*100)
			fmt.Printf("  Max concurrent:   %d\n", maxConcurrent)
			fmt.Printf("  Min keys/shard:   %d\n", minKeys)
			fmt.Printf("  Dry run:          %v\n", dryRun)
			fmt.Printf("  Rate limit:       %d KB/s\n", rateLimit)
			fmt.Println()

			// Call the SDK RebalanceCluster
			req := &cluster.RebalanceClusterRequest{
				Strategy: "round_robin",
			}

			resp, err := sdkClient.Cluster().RebalanceCluster(ctx, req)
			if err != nil {
				return api.NewOperationFailedError("rebalance", err)
			}

			fmt.Printf("Rebalance operation started: %s\n", resp.OperationID)
			fmt.Printf("Started at: %s\n", time.Unix(0, resp.Started).Format(time.RFC3339))

			// If follow mode is enabled, poll for progress
			if follow {
				return followRebalanceProgressSDK(ctx, sdkClient.Cluster(), resp.OperationID, outputFormat)
			}

			return nil
		},
	}

	// Only define command-specific flags; --address and --timeout are global flags defined in main.go
	cmd.Flags().Float64Var(&targetImbalance, "target-imbalance", 0.1, "Target imbalance ratio (0.1 = 10%)")
	cmd.Flags().IntVar(&maxConcurrent, "max-concurrent", 2, "Max simultaneous transfers")
	cmd.Flags().Int64Var(&minKeys, "min-keys", 1000, "Minimum keys per shard")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Plan without executing")
	cmd.Flags().IntVar(&rateLimit, "rate-limit", 10240, "Rate limit in KB/s")
	cmd.Flags().StringVar(&outputFormat, "output", "table", "Output format (json, table)")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow rebalance progress")

	return cmd
}

// newRebalanceStatusCommand creates the rebalance status command.
func newRebalanceStatusCommand() *cobra.Command {
	var (
		operationID  string
		outputFormat string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check rebalance progress",
		Long: `Check the progress of an ongoing rebalance operation.

This command displays real-time statistics about the rebalance including:
  - Number of moves completed
  - Keys and bytes rebalanced
  - Current imbalance ratio
  - Estimated completion time

Note: Use the global --address flag to specify the server address.

Examples:
  # Check rebalance status
  quicbaas rebalance status --operation op-123

  # Check rebalance status with JSON output
  quicbaas rebalance status --operation op-123 --output json

  # Connect to a specific server address
  quicbaas --address localhost:50051 rebalance status --operation op-123`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Use global timeout from main.go
			ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
			defer cancel()

			// Create SDK client using global address and transport from main.go
			sdkClient, err := createSDKClient()
			if err != nil {
				return api.NewOperationFailedError("create client", err)
			}
			defer func() { _ = closeSDKClient() }()

			// Get rebalance progress via SDK
			req := &cluster.GetRebalanceProgressRequest{
				OperationID: operationID,
			}

			resp, err := sdkClient.Cluster().GetRebalanceProgress(ctx, req)
			if err != nil {
				return api.NewOperationFailedError("get rebalance progress", err)
			}

			return displayRebalanceProgressSDK(resp, outputFormat)
		},
	}

	// Only define command-specific flags; --address and --timeout are global flags defined in main.go
	cmd.Flags().StringVar(&operationID, "operation", "", "Operation ID to check (required)")
	cmd.Flags().StringVar(&outputFormat, "output", "table", "Output format (json, table)")
	cmd.MarkFlagRequired("operation")

	return cmd
}

// newRebalancePlanCommand creates the rebalance plan command.
func newRebalancePlanCommand() *cobra.Command {
	var (
		targetImbalance float64
		outputFormat    string
	)

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Show rebalance plan (dry-run)",
		Long: `Show the rebalance plan without executing it.

This command analyzes the current cluster state and displays what moves
would be performed to achieve balance. It's equivalent to running
'quicbaas rebalance start --dry-run'.

The plan includes:
  - Source and target shards for each move
  - Number of keys and bytes to move
  - Estimated time for each move
  - Overall estimated completion time

Note: Use the global --address flag to specify the server address.

Examples:
  # Show rebalance plan
  quicbaas rebalance plan

  # Show plan with JSON output
  quicbaas rebalance plan --output json

  # Show plan with custom target imbalance
  quicbaas rebalance plan --target-imbalance 0.05

  # Connect to a specific server address
  quicbaas --address localhost:50051 rebalance plan`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Use global timeout from main.go
			ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
			defer cancel()

			// Create SDK client using global address and transport from main.go
			sdkClient, err := createSDKClient()
			if err != nil {
				return api.NewOperationFailedError("create client", err)
			}
			defer func() { _ = closeSDKClient() }()

			fmt.Println("Generating rebalance plan (dry run)...")
			fmt.Printf("  Server:           %s\n", address)
			fmt.Printf("  Target imbalance: %.2f%%\n", targetImbalance*100)
			fmt.Println()

			// Call the SDK RebalanceCluster
			req := &cluster.RebalanceClusterRequest{
				Strategy: "round_robin",
			}

			resp, err := sdkClient.Cluster().RebalanceCluster(ctx, req)
			if err != nil {
				return api.NewOperationFailedError("generate plan", err)
			}

			fmt.Printf("Plan generated for operation: %s\n", resp.OperationID)
			fmt.Println("Note: Use 'quicbaas rebalance start' to execute this plan")

			return nil
		},
	}

	// Only define command-specific flags; --address and --timeout are global flags defined in main.go
	cmd.Flags().Float64Var(&targetImbalance, "target-imbalance", 0.1, "Target imbalance ratio (0.1 = 10%)")
	cmd.Flags().StringVar(&outputFormat, "output", "table", "Output format (json, table)")

	return cmd
}

// Helper functions

// displayRebalanceReport displays rebalance report.
func displayRebalanceReport(report *RebalanceReport, outputFormat string) error {
	switch outputFormat {
	case "json":
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return api.NewMarshalError(err)
		}
		fmt.Println(string(data))

	case "table":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		defer w.Flush()

		fmt.Fprintln(w, "\n=== Rebalance Report ===")
		fmt.Fprintf(w, "Start Time:\t%s\n", report.StartTime.Format(time.RFC3339))
		fmt.Fprintf(w, "End Time:\t%s\n", report.EndTime.Format(time.RFC3339))
		fmt.Fprintf(w, "Duration:\t%s\n", report.Duration.Round(time.Second))
		fmt.Fprintln(w)

		fmt.Fprintf(w, "Moves Completed:\t%d\n", report.MovesCompleted)
		fmt.Fprintf(w, "Moves Failed:\t%d\n", report.MovesFailed)
		fmt.Fprintf(w, "Keys Rebalanced:\t%d\n", report.KeysRebalanced)
		fmt.Fprintf(w, "Bytes Rebalanced:\t%s\n", formatBytes(uint64(report.BytesRebalanced)))
		fmt.Fprintln(w)

		fmt.Fprintf(w, "Imbalance Before:\t%.2f%%\n", report.ImbalanceBefore*100)
		fmt.Fprintf(w, "Imbalance After:\t%.2f%%\n", report.ImbalanceAfter*100)

		improvement := (report.ImbalanceBefore - report.ImbalanceAfter) / report.ImbalanceBefore * 100
		if improvement > 0 {
			fmt.Fprintf(w, "Improvement:\t%.2f%%\n", improvement)
		}

		if len(report.Errors) > 0 {
			fmt.Fprintf(w, "\nErrors:\t%d\n", len(report.Errors))
			for i, err := range report.Errors {
				if i < 5 { // Show first 5 errors
					fmt.Fprintf(w, "  %d.\t%v\n", i+1, err)
				}
			}
			if len(report.Errors) > 5 {
				fmt.Fprintf(w, "  ...\t(%d more errors)\n", len(report.Errors)-5)
			}
		}

		fmt.Fprintln(w)

		if report.MovesFailed > 0 {
			fmt.Fprintf(w, "\nWarning: Rebalance completed with %d failed moves\n", report.MovesFailed)
		} else if report.MovesCompleted > 0 {
			fmt.Fprintln(w, "Rebalance completed successfully!")
		} else {
			fmt.Fprintln(w, "Cluster is already balanced - no moves required")
		}

		fmt.Fprintln(w)

	default:
		return NewUnsupportedOutputFormatError("displayRebalanceReport", outputFormat)
	}

	return nil
}

// displayRebalanceProgressSDK displays rebalance progress from SDK response.
func displayRebalanceProgressSDK(resp *cluster.GetRebalanceProgressResponse, outputFormat string) error {
	switch outputFormat {
	case "json":
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return api.NewMarshalError(err)
		}
		fmt.Println(string(data))

	case "table":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		defer w.Flush()

		fmt.Fprintln(w, "\n=== Rebalance Progress ===")
		fmt.Fprintf(w, "Operation ID:\t%s\n", resp.OperationID)
		fmt.Fprintf(w, "Status:\t%s\n", resp.Status)
		fmt.Fprintf(w, "Progress:\t%.1f%%\n", resp.PercentComplete)
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Shards Moved:\t%d/%d\n", resp.ShardsMoved, resp.TotalShards)
		fmt.Fprintf(w, "Elapsed Time:\t%s\n", time.Duration(resp.ElapsedTime).Round(time.Second))
		fmt.Fprintln(w)

	default:
		return NewUnsupportedOutputFormatError("displayRebalanceProgressSDK", outputFormat)
	}

	return nil
}

// followRebalanceProgressSDK polls for rebalance progress until completion using the SDK cluster client.
func followRebalanceProgressSDK(ctx context.Context, clusterClient cluster.Client, operationID string, outputFormat string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	fmt.Println("\nFollowing rebalance progress (press Ctrl+C to stop)...")
	fmt.Println()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			req := &cluster.GetRebalanceProgressRequest{
				OperationID: operationID,
			}

			resp, err := clusterClient.GetRebalanceProgress(ctx, req)
			if err != nil {
				fmt.Printf("  Warning: failed to get progress: %v\n", err)
				continue
			}

			// Display progress
			fmt.Printf("\r  Progress: %.1f%% | Status: %s | Shards: %d/%d | Elapsed: %s",
				resp.PercentComplete,
				resp.Status,
				resp.ShardsMoved,
				resp.TotalShards,
				time.Duration(resp.ElapsedTime).Round(time.Second),
			)

			// Check if complete
			if resp.Status == "completed" || resp.Status == "failed" {
				fmt.Println()
				fmt.Printf("\nRebalance %s!\n", resp.Status)
				return nil
			}
		}
	}
}
