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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	operationsSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/operations"
)

var (
	// Upgrade flags.
	upgradeServerAddress  string
	upgradeTimeout        time.Duration
	upgradeMaxUnavailable int
	upgradeWaitTime       time.Duration
	upgradeHealthTimeout  time.Duration
	upgradeDrainTimeout   time.Duration
	upgradeRollbackOnFail bool
	upgradeVerifyCompat   bool
	upgradeSkipBackup     bool
	upgradeOutputFormat   string
	upgradeFollowProgress bool
	upgradeNodeID         uint64
	upgradeOperationID    string

	// Upgrade TLS flags.
	upgradeTLSCert string
	upgradeTLSKey  string
	upgradeTLSCA   string
)

// newUpgradeCommands creates the upgrade command group.
func newUpgradeCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Rolling upgrade operations",
		Long: `Commands for managing rolling upgrades across the cluster.

These commands allow you to upgrade the cluster to a new version with
minimal downtime by upgrading nodes one at a time.

The upgrade command connects to a QuicBaaS server and initiates rolling
upgrades through the Operations API.`,
	}

	// Add subcommands
	cmd.AddCommand(newUpgradeStartCommand())
	cmd.AddCommand(newUpgradeStatusCommand())

	return cmd
}

// addUpgradeTLSFlags adds TLS flags to an upgrade subcommand.
func addUpgradeTLSFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&upgradeTLSCert, "tls-cert", "", "TLS client certificate file")
	cmd.Flags().StringVar(&upgradeTLSKey, "tls-key", "", "TLS client key file")
	cmd.Flags().StringVar(&upgradeTLSCA, "tls-ca", "", "TLS CA certificate file")
}

// createUpgradeGRPCClient creates a gRPC client for upgrade operations with TLS.
func createUpgradeGRPCClient(serverAddr string) (*grpc.Client, error) {
	opts := []grpc.ClientOption{
		grpc.WithTimeout(upgradeTimeout),
	}

	switch {
	case upgradeTLSCert != "" && upgradeTLSKey != "":
		opts = append(opts, grpc.WithTLS(upgradeTLSCert, upgradeTLSKey, upgradeTLSCA))
	case upgradeTLSCA != "":
		opt, err := grpcCAOnlyTLSOption(upgradeTLSCA)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrConfiguration, "createUpgradeGRPCClient",
				"failed to load --tls-ca")
		}
		opts = append(opts, opt)
	default:
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createUpgradeGRPCClient",
			"TLS is required for gRPC connections (use --tls-cert, --tls-key, and --tls-ca)",
		)
	}

	client, err := grpc.NewClient(serverAddr, opts...)
	if err != nil {
		return nil, api.NewConnectionError("cli.upgrade", "connect", serverAddr, err)
	}

	return client, nil
}

// reconcileUpgradeSDKConfig promotes the upgrade command's transport flags
// into the global SDK configuration that createSDKClient reads when it builds
// the singleton SDK client.
//
// The upgrade subcommand registers its own --tls-cert / --tls-key / --tls-ca
// (and --server) flags so the standalone usage examples in its help text work.
// Because cobra resolves a command-local flag in preference to a persistent
// flag of the same name, those local flags SHADOW the root command's global
// --tls-cert / --tls-key / --tls-ca. Without this reconciliation the SDK path
// (createSDKClient -> buildTLSConfig) would read the now-empty global TLS vars
// and the gRPC transport would reject the dial with "TLS configuration is
// required for all connections".
//
// Each upgrade-specific value is copied into its global counterpart only when
// the upgrade flag is set and the global is still empty, so an explicit global
// flag always wins and a transport already selected via --transport is never
// clobbered.
func reconcileUpgradeSDKConfig() {
	if upgradeServerAddress != "" {
		address = upgradeServerAddress
	}
	if upgradeTLSCA != "" && tlsCA == "" {
		tlsCA = upgradeTLSCA
	}
	if upgradeTLSCert != "" && tlsCert == "" {
		tlsCert = upgradeTLSCert
	}
	if upgradeTLSKey != "" && tlsKey == "" {
		tlsKey = upgradeTLSKey
	}
}

// newUpgradeStartCommand creates the upgrade start command.
func newUpgradeStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start <version>",
		Short: "Start rolling upgrade",
		Long: `Start a rolling upgrade to a new version.

This command initiates a rolling upgrade by connecting to the QuicBaaS server
and using the Operations API. The server performs the coordinated upgrade:
  1. Verifies version compatibility
  2. Creates pre-upgrade backup (optional)
  3. For each node:
     - Drains traffic from the node
     - Stops the node gracefully
     - Upgrades to the new version
     - Starts the node
     - Waits for health verification
  4. Waits between nodes for cluster stabilization

The process ensures continuous availability with at most 'max-unavailable'
nodes down at any given time.

If any node fails to upgrade, the server can automatically rollback
all upgraded nodes to the previous version (if rollback-on-failure is enabled).

Examples:
  # Basic rolling upgrade (connects to default server)
  quicbaas upgrade start v2.0.0 --tls-cert cert.pem --tls-key key.pem --tls-ca ca.pem

  # Upgrade a specific node
  quicbaas upgrade start v2.0.0 --node-id 1 --tls-cert cert.pem --tls-key key.pem

  # Upgrade with custom server address
  quicbaas upgrade start v2.0.0 --server localhost:50051 --tls-cert cert.pem --tls-key key.pem

  # Upgrade with follow mode to monitor progress
  quicbaas upgrade start v2.0.0 --follow --tls-cert cert.pem --tls-key key.pem

  # Upgrade with JSON output
  quicbaas upgrade start v2.0.0 --output json --tls-cert cert.pem --tls-key key.pem`,
		Args: cobra.ExactArgs(1),
		RunE: runUpgradeStart,
	}

	cmd.Flags().StringVar(&upgradeServerAddress, "server", "", "QuicBaaS server address (defaults to global --address)")
	cmd.Flags().DurationVar(&upgradeTimeout, "timeout", 30*time.Second, "Request timeout")
	cmd.Flags().Uint64Var(&upgradeNodeID, "node-id", 0, "Specific node ID to upgrade (0 = all nodes)")
	cmd.Flags().IntVar(&upgradeMaxUnavailable, "max-unavailable", 1, "Max nodes down simultaneously")
	cmd.Flags().DurationVar(&upgradeWaitTime, "wait-time", 30*time.Second, "Wait time between node upgrades")
	cmd.Flags().DurationVar(&upgradeHealthTimeout, "health-timeout", 2*time.Minute, "Health check timeout")
	cmd.Flags().DurationVar(&upgradeDrainTimeout, "drain-timeout", 5*time.Minute, "Node drain timeout")
	cmd.Flags().BoolVar(&upgradeRollbackOnFail, "rollback-on-failure", true, "Rollback on upgrade failure")
	cmd.Flags().BoolVar(&upgradeVerifyCompat, "verify-compatibility", true, "Verify version compatibility")
	cmd.Flags().BoolVar(&upgradeSkipBackup, "skip-backup", false, "Skip pre-upgrade backup")
	cmd.Flags().StringVar(&upgradeOutputFormat, "output", "table", "Output format (json, table)")
	cmd.Flags().BoolVar(&upgradeFollowProgress, "follow", false, "Follow upgrade progress")
	addUpgradeTLSFlags(cmd)

	return cmd
}

// runUpgradeStart executes the upgrade start command.
func runUpgradeStart(cmd *cobra.Command, args []string) error {
	targetVersion := args[0]

	// Use server address from flag or fall back to global address
	serverAddr := upgradeServerAddress
	if serverAddr == "" {
		serverAddr = address
	}
	if serverAddr == "" {
		serverAddr = "localhost:50051"
	}

	// Map the upgrade command's own transport flags onto the global SDK
	// configuration before the singleton SDK client is built (see
	// reconcileUpgradeSDKConfig for why the local flags otherwise shadow the
	// global TLS flags and break the SDK gRPC dial).
	reconcileUpgradeSDKConfig()

	ctx, cancel := context.WithTimeout(context.Background(), upgradeTimeout)
	defer cancel()

	// Route through the SDK Operations client so the upgrade verb works
	// across every transport (rest / grpc / quic / mcp / unix / embedded)
	// rather than being pinned to a hardcoded gRPC connection.
	client, err := operationsClientFactory()
	if err != nil {
		return err
	}

	fmt.Printf("Starting rolling upgrade to version %s...\n", targetVersion)
	fmt.Printf("  Server:           %s\n", serverAddr)
	fmt.Printf("  Node ID:          %d (0 = all nodes)\n", upgradeNodeID)
	fmt.Printf("  Max unavailable:  %d\n", upgradeMaxUnavailable)
	fmt.Printf("  Wait time:        %s\n", upgradeWaitTime)
	fmt.Printf("  Health timeout:   %s\n", upgradeHealthTimeout)
	fmt.Printf("  Rollback on fail: %v\n", upgradeRollbackOnFail)
	fmt.Println()

	// Call UpgradeNode through the active SDK transport.
	sdkResp, err := client.UpgradeNode(ctx, &operationsSDK.UpgradeNodeRequest{
		NodeID:        upgradeNodeID,
		TargetVersion: targetVersion,
	})
	if err != nil {
		return api.NewOperationFailedError("upgrade", err)
	}

	resp := upgradeStartRespFromSDK(sdkResp)

	fmt.Printf("Upgrade operation started: %s\n", resp.OperationId)
	fmt.Printf("Started at: %s\n", time.Unix(0, resp.Started).Format(time.RFC3339))

	// If follow mode is enabled, poll for progress
	if upgradeFollowProgress {
		return followUpgradeProgress(ctx, client, resp.OperationId, upgradeOutputFormat)
	}

	// Output the initial response
	return displayUpgradeStartResponse(resp)
}

// upgradeStartRespFromSDK adapts the SDK UpgradeNodeResponse to the proto
// display DTO used by the upgrade output formatters. The SDK response
// carries a JobID/Status pair; OperationId is sourced from JobID and the
// server-reported start time is not part of the SDK shape, so Started is
// left at zero (rendered as the Unix epoch by the formatter).
func upgradeStartRespFromSDK(sdkResp *operationsSDK.UpgradeNodeResponse) *pb.UpgradeNodeResponse {
	if sdkResp == nil {
		return &pb.UpgradeNodeResponse{}
	}
	return &pb.UpgradeNodeResponse{
		OperationId: sdkResp.JobID,
	}
}

// upgradeStatusRespFromSDK adapts the SDK GetUpgradeStatusResponse to the
// proto display DTO. NodesUpgraded/TotalNodes are not part of the SDK
// response shape and are left at zero.
func upgradeStatusRespFromSDK(sdkResp *operationsSDK.GetUpgradeStatusResponse) *pb.GetUpgradeStatusResponse {
	if sdkResp == nil {
		return &pb.GetUpgradeStatusResponse{}
	}
	return &pb.GetUpgradeStatusResponse{
		OperationId:     sdkResp.JobID,
		PercentComplete: sdkResp.Progress,
		CurrentVersion:  sdkResp.CurrentVersion,
		TargetVersion:   sdkResp.TargetVersion,
		Status:          sdkResp.Status,
	}
}

// followUpgradeProgress polls the server for upgrade progress until completion.
func followUpgradeProgress(ctx context.Context, client OperationsClient, operationID string, outputFormat string) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	fmt.Println("\nFollowing upgrade progress (press Ctrl+C to stop)...")
	fmt.Println()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			sdkResp, err := client.GetUpgradeStatus(ctx, &operationsSDK.GetUpgradeStatusRequest{
				JobID: operationID,
			})
			if err != nil {
				fmt.Printf("Warning: failed to get upgrade status: %v\n", err)
				continue
			}

			resp := upgradeStatusRespFromSDK(sdkResp)
			displayUpgradeProgress(resp)

			// Check if upgrade is complete
			if resp.Status == "completed" || resp.Status == "failed" || resp.Status == "rolled_back" {
				return displayUpgradeStatusResponse(resp, outputFormat)
			}
		}
	}
}

// newUpgradeStatusCommand creates the upgrade status command.
func newUpgradeStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check upgrade progress",
		Long: `Check the progress of an ongoing rolling upgrade.

This command connects to the QuicBaaS server and queries the status of
an upgrade operation. It displays real-time information including:
  - Total number of nodes
  - Nodes completed, failed, and pending
  - Current upgrade progress percentage
  - Per-node status and timing

Examples:
  # Check upgrade status for a specific operation
  quicbaas upgrade status --operation op-123 --tls-cert cert.pem --tls-key key.pem

  # Check current upgrade status (no operation ID needed)
  quicbaas upgrade status --tls-cert cert.pem --tls-key key.pem

  # Check upgrade status with JSON output
  quicbaas upgrade status --output json --tls-cert cert.pem --tls-key key.pem

  # Check upgrade status from a specific server
  quicbaas upgrade status --server localhost:50051 --tls-cert cert.pem --tls-key key.pem`,
		RunE: runUpgradeStatus,
	}

	cmd.Flags().StringVar(&upgradeServerAddress, "server", "", "QuicBaaS server address (defaults to global --address)")
	cmd.Flags().DurationVar(&upgradeTimeout, "timeout", 30*time.Second, "Request timeout")
	cmd.Flags().StringVar(&upgradeOperationID, "operation", "", "Operation ID to query (empty = current upgrade)")
	cmd.Flags().StringVar(&upgradeOutputFormat, "output", "table", "Output format (json, table)")
	addUpgradeTLSFlags(cmd)

	return cmd
}

// runUpgradeStatus executes the upgrade status command.
func runUpgradeStatus(cmd *cobra.Command, args []string) error {
	// Map the upgrade command's own transport flags onto the global SDK
	// configuration before the singleton SDK client is built.
	reconcileUpgradeSDKConfig()

	ctx, cancel := context.WithTimeout(context.Background(), upgradeTimeout)
	defer cancel()

	// Route through the SDK Operations client for multi-transport support.
	client, err := operationsClientFactory()
	if err != nil {
		return err
	}

	sdkResp, err := client.GetUpgradeStatus(ctx, &operationsSDK.GetUpgradeStatusRequest{
		JobID: upgradeOperationID,
	})
	if err != nil {
		return api.NewOperationFailedError("get upgrade status", err)
	}

	return displayUpgradeStatusResponse(upgradeStatusRespFromSDK(sdkResp), upgradeOutputFormat)
}

// Helper functions

// displayUpgradeStartResponse displays the initial upgrade response.
func displayUpgradeStartResponse(resp *pb.UpgradeNodeResponse) error {
	switch upgradeOutputFormat {
	case "json":
		data, err := json.MarshalIndent(map[string]interface{}{
			"operation_id": resp.OperationId,
			"started":      time.Unix(0, resp.Started).Format(time.RFC3339),
		}, "", "  ")
		if err != nil {
			return api.NewMarshalError(err)
		}
		fmt.Println(string(data))

	case "table":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		defer w.Flush()

		fmt.Fprintln(w, "\n=== Upgrade Started ===")
		fmt.Fprintf(w, "Operation ID:\t%s\n", resp.OperationId)
		fmt.Fprintf(w, "Started:\t%s\n", time.Unix(0, resp.Started).Format(time.RFC3339))
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Use 'quicbaas upgrade status --operation "+resp.OperationId+"' to check progress")
		fmt.Fprintln(w)

	default:
		return NewUnsupportedOutputFormatError("displayUpgradeStartResponse", upgradeOutputFormat)
	}

	return nil
}

// displayUpgradeProgress displays upgrade progress during follow mode.
func displayUpgradeProgress(resp *pb.GetUpgradeStatusResponse) {
	fmt.Printf("\r[%s] Progress: %.1f%% | Nodes: %d/%d | Status: %s",
		time.Now().Format("15:04:05"),
		resp.PercentComplete,
		resp.NodesUpgraded,
		resp.TotalNodes,
		resp.Status,
	)
}

// displayUpgradeStatusResponse displays the upgrade status response.
func displayUpgradeStatusResponse(resp *pb.GetUpgradeStatusResponse, outputFormat string) error {
	switch outputFormat {
	case "json":
		data, err := json.MarshalIndent(map[string]interface{}{
			"operation_id":     resp.OperationId,
			"nodes_upgraded":   resp.NodesUpgraded,
			"total_nodes":      resp.TotalNodes,
			"percent_complete": resp.PercentComplete,
			"current_version":  resp.CurrentVersion,
			"target_version":   resp.TargetVersion,
			"status":           resp.Status,
		}, "", "  ")
		if err != nil {
			return api.NewMarshalError(err)
		}
		fmt.Println(string(data))

	case "table":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		defer w.Flush()

		fmt.Fprintln(w, "\n=== Upgrade Status ===")
		fmt.Fprintf(w, "Operation ID:\t%s\n", resp.OperationId)
		fmt.Fprintf(w, "Status:\t%s\n", resp.Status)
		fmt.Fprintf(w, "Progress:\t%.1f%%\n", resp.PercentComplete)
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Nodes Upgraded:\t%d\n", resp.NodesUpgraded)
		fmt.Fprintf(w, "Total Nodes:\t%d\n", resp.TotalNodes)
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Current Version:\t%s\n", resp.CurrentVersion)
		fmt.Fprintf(w, "Target Version:\t%s\n", resp.TargetVersion)
		fmt.Fprintln(w)

		// Display status-specific messages
		switch resp.Status {
		case "completed":
			fmt.Fprintln(w, "Rolling upgrade completed successfully!")
		case "failed":
			fmt.Fprintln(w, "Upgrade failed. Check server logs for details.")
		case "rolled_back":
			fmt.Fprintln(w, "Upgrade failed and was rolled back.")
		case "in_progress":
			fmt.Fprintln(w, "Upgrade in progress...")
		case "initiated":
			fmt.Fprintln(w, "Upgrade initiated, waiting to start...")
		default:
			fmt.Fprintf(w, "Status: %s\n", resp.Status)
		}
		fmt.Fprintln(w)

	default:
		return NewUnsupportedOutputFormatError("displayUpgradeStatusResponse", outputFormat)
	}

	return nil
}
