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
)

var (
	// Node management flags.
	nodeHealthCheckInterval time.Duration
	nodeHealthCheckTimeout  time.Duration
	nodeEvacuationTimeout   time.Duration
	nodeVerifyData          bool
	nodeAutoReplace         bool
	nodeGracePeriod         time.Duration
	nodeMaxTransfers        int
	nodeOutputFormat        string
)

// Global node replacement manager.
var currentNodeManager *NodeReplacementManager

// newNodeCommands creates the node management command group.
func newNodeCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Node management operations",
		Long: `Commands for managing cluster nodes.

These commands allow you to add, remove, and replace nodes in the cluster,
with automatic data evacuation and health checking.`,
	}

	// Add subcommands
	cmd.AddCommand(newNodeAddCommand())
	cmd.AddCommand(newNodeRemoveCommand())
	cmd.AddCommand(newNodeReplaceCommand())
	cmd.AddCommand(newNodeListCommand())

	return cmd
}

// newNodeAddCommand creates the node add command.
func newNodeAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <node-id> <address>",
		Short: "Add node to cluster",
		Long: `Add a new node to the cluster.

This command registers a new node with the cluster and performs initial
health checks to verify the node is ready to accept traffic.

The node address should be in the format: host:port

Examples:
  # Add a new node
  quicbaas node add node-4 10.0.0.4:50051

  # Add node with custom health check settings
  quicbaas node add node-4 10.0.0.4:50051 \
    --health-check-interval 30s \
    --health-check-timeout 10s`,
		Args: cobra.ExactArgs(2),
		RunE: runNodeAdd,
	}

	cmd.Flags().DurationVar(&nodeHealthCheckInterval, "health-check-interval", 30*time.Second, "Health check interval")
	cmd.Flags().DurationVar(&nodeHealthCheckTimeout, "health-check-timeout", 5*time.Second, "Health check timeout")
	cmd.Flags().StringVar(&nodeOutputFormat, "output", "table", "Output format (json, table)")

	return cmd
}

// runNodeAdd executes the node add command.
func runNodeAdd(cmd *cobra.Command, args []string) error {
	nodeID := args[0]
	address := args[1]

	ctx := context.Background()

	// Get or create node manager
	nodeMgr, err := getNodeManager()
	if err != nil {
		return err
	}

	fmt.Printf("Adding node '%s' at address '%s'...\n", nodeID, address)

	// Add the node
	if err := nodeMgr.AddNode(ctx, nodeID, address); err != nil {
		return api.NewOperationFailedError("add node", err)
	}

	fmt.Printf("Node '%s' added successfully\n", nodeID)

	// Display node state
	node, exists := nodeMgr.GetNodeState(nodeID)
	if exists {
		return displayNodeState(node)
	}

	return nil
}

// newNodeRemoveCommand creates the node remove command.
func newNodeRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <node-id>",
		Short: "Remove node from cluster",
		Long: `Remove a node from the cluster safely.

This command performs a graceful node removal:
  1. Marks the node for removal
  2. Evacuates all data from the node to other nodes
  3. Verifies data evacuation (if enabled)
  4. Removes the node from the cluster

The evacuation process ensures no data is lost and may take time depending
on the amount of data stored on the node.

Examples:
  # Remove a node
  quicbaas node remove node-3

  # Remove node without data verification
  quicbaas node remove node-3 --verify-data=false

  # Remove node with custom evacuation timeout
  quicbaas node remove node-3 --evacuation-timeout 60m`,
		Args: cobra.ExactArgs(1),
		RunE: runNodeRemove,
	}

	cmd.Flags().DurationVar(&nodeEvacuationTimeout, "evacuation-timeout", 30*time.Minute, "Evacuation timeout")
	cmd.Flags().BoolVar(&nodeVerifyData, "verify-data", true, "Verify data after evacuation")
	cmd.Flags().StringVar(&nodeOutputFormat, "output", "table", "Output format (json, table)")

	return cmd
}

// runNodeRemove executes the node remove command.
func runNodeRemove(cmd *cobra.Command, args []string) error {
	nodeID := args[0]

	ctx := context.Background()

	// Get or create node manager
	nodeMgr, err := getNodeManager()
	if err != nil {
		return err
	}

	fmt.Printf("Removing node '%s'...\n", nodeID)
	fmt.Println("This will evacuate all data from the node before removal.")
	fmt.Println()

	// Remove the node
	report, err := nodeMgr.RemoveNode(ctx, nodeID)
	if err != nil {
		return api.NewOperationFailedError("remove node", err)
	}

	return displayNodeReplacementReport(report)
}

// newNodeReplaceCommand creates the node replace command.
func newNodeReplaceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replace <old-id> <new-id> <address>",
		Short: "Replace node",
		Long: `Replace a failed or degraded node with a new node.

This command performs a coordinated node replacement:
  1. Adds the new node to the cluster
  2. Waits for the new node to become healthy
  3. Evacuates data from the old node (if accessible)
  4. Removes the old node from the cluster

This is the recommended way to handle node failures as it ensures
continuous availability and data safety.

Examples:
  # Replace a failed node
  quicbaas node replace node-2 node-5 10.0.0.5:50051

  # Replace node with custom settings
  quicbaas node replace node-2 node-5 10.0.0.5:50051 \
    --verify-data \
    --evacuation-timeout 45m

  # Replace node with auto-replacement enabled
  quicbaas node replace node-2 node-5 10.0.0.5:50051 \
    --auto-replace`,
		Args: cobra.ExactArgs(3),
		RunE: runNodeReplace,
	}

	cmd.Flags().DurationVar(&nodeEvacuationTimeout, "evacuation-timeout", 30*time.Minute, "Evacuation timeout")
	cmd.Flags().BoolVar(&nodeVerifyData, "verify-data", true, "Verify data after evacuation")
	cmd.Flags().BoolVar(&nodeAutoReplace, "auto-replace", false, "Enable automatic replacement on future failures")
	cmd.Flags().StringVar(&nodeOutputFormat, "output", "table", "Output format (json, table)")

	return cmd
}

// runNodeReplace executes the node replace command.
func runNodeReplace(cmd *cobra.Command, args []string) error {
	oldNodeID := args[0]
	newNodeID := args[1]
	newAddress := args[2]

	ctx := context.Background()

	// Get or create node manager
	nodeMgr, err := getNodeManager()
	if err != nil {
		return err
	}

	fmt.Printf("Replacing node '%s' with '%s' at address '%s'...\n", oldNodeID, newNodeID, newAddress)
	fmt.Println()

	// Replace the node
	report, err := nodeMgr.ReplaceNode(ctx, oldNodeID, newNodeID, newAddress)
	if err != nil {
		return api.NewOperationFailedError("replace node", err)
	}

	return displayNodeReplacementReport(report)
}

// newNodeListCommand creates the node list command.
func newNodeListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all cluster nodes",
		Long: `List all nodes in the cluster with their current status.

This command displays information about each node including:
  - Node ID and address
  - Current status (healthy, degraded, failed, etc.)
  - Last health check time
  - Failure count
  - Data statistics (keys, bytes)

Examples:
  # List all nodes
  quicbaas node list

  # List nodes with JSON output
  quicbaas node list --output json`,
		Args: cobra.NoArgs,
		RunE: runNodeList,
	}

	cmd.Flags().StringVar(&nodeOutputFormat, "output", "table", "Output format (json, table)")

	return cmd
}

// runNodeList executes the node list command.
func runNodeList(cmd *cobra.Command, args []string) error {
	// Get or create node manager
	nodeMgr, err := getNodeManager()
	if err != nil {
		return err
	}

	nodes := nodeMgr.GetAllNodes()

	switch nodeOutputFormat {
	case "json":
		data, err := json.MarshalIndent(nodes, "", "  ")
		if err != nil {
			return api.NewMarshalError(err)
		}
		fmt.Println(string(data))

	case "table":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		defer w.Flush()

		fmt.Fprintln(w, "NODE ID\tADDRESS\tSTATUS\tHEALTH CHECKS\tFAILURES\tLAST SEEN")
		fmt.Fprintln(w, "-------\t-------\t------\t-------------\t--------\t---------")

		for _, node := range nodes {
			lastSeen := "-"
			if !node.LastSeenAt.IsZero() {
				lastSeen = time.Since(node.LastSeenAt).Round(time.Second).String() + " ago"
			}

			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n",
				node.NodeID,
				node.Address,
				node.Status.String(),
				node.HealthCheckCount,
				node.FailureCount,
				lastSeen,
			)
		}

		fmt.Fprintln(w)

	default:
		return NewUnsupportedOutputFormatError("node list", nodeOutputFormat)
	}

	return nil
}

// Helper functions

// getNodeManager gets or creates the node replacement manager.
func getNodeManager() (*NodeReplacementManager, error) {
	if currentNodeManager != nil {
		return currentNodeManager, nil
	}

	// Create storage backend
	storage, err := createStorageFromURL("memory://")
	if err != nil {
		return nil, api.NewOperationFailedError("create storage", err)
	}

	// Get default configuration values
	defaults := DefaultNodeReplacementConfig()

	// Apply defaults for unset values (zero values)
	healthCheckInterval := nodeHealthCheckInterval
	if healthCheckInterval <= 0 {
		healthCheckInterval = defaults.HealthCheckInterval
	}

	healthCheckTimeout := nodeHealthCheckTimeout
	if healthCheckTimeout <= 0 {
		healthCheckTimeout = defaults.HealthCheckTimeout
	}

	evacuationTimeout := nodeEvacuationTimeout
	if evacuationTimeout <= 0 {
		evacuationTimeout = defaults.EvacuationTimeout
	}

	gracePeriod := nodeGracePeriod
	if gracePeriod <= 0 {
		gracePeriod = defaults.GracePeriod
	}

	maxConcurrentTransfers := nodeMaxTransfers
	if maxConcurrentTransfers <= 0 {
		maxConcurrentTransfers = defaults.MaxConcurrentTransfers
	}

	// Create node replacement configuration with defaults applied
	config := &NodeReplacementConfig{
		HealthCheckInterval:    healthCheckInterval,
		HealthCheckTimeout:     healthCheckTimeout,
		EvacuationTimeout:      evacuationTimeout,
		VerifyData:             nodeVerifyData,
		AutoReplace:            nodeAutoReplace,
		GracePeriod:            gracePeriod,
		MaxConcurrentTransfers: maxConcurrentTransfers,
	}

	// Create node manager
	nodeMgr, err := NewNodeReplacementManager(storage, config, asInterfaceLogger(log))
	if err != nil {
		return nil, api.NewOperationFailedError("create node manager", err)
	}

	currentNodeManager = nodeMgr
	return nodeMgr, nil
}

// displayNodeState displays a node's state.
func displayNodeState(node *NodeState) error {
	switch nodeOutputFormat {
	case "json":
		data, err := json.MarshalIndent(node, "", "  ")
		if err != nil {
			return api.NewMarshalError(err)
		}
		fmt.Println(string(data))

	case "table":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		defer w.Flush()

		fmt.Fprintln(w, "\n=== Node State ===")
		fmt.Fprintf(w, "Node ID:\t%s\n", node.NodeID)
		fmt.Fprintf(w, "Address:\t%s\n", node.Address)
		fmt.Fprintf(w, "Status:\t%s\n", node.Status.String())
		fmt.Fprintf(w, "Joined At:\t%s\n", node.JoinedAt.Format(time.RFC3339))
		fmt.Fprintf(w, "Last Seen:\t%s\n", node.LastSeenAt.Format(time.RFC3339))
		fmt.Fprintf(w, "Health Checks:\t%d\n", node.HealthCheckCount)
		fmt.Fprintf(w, "Failures:\t%d\n", node.FailureCount)
		fmt.Fprintf(w, "Keys:\t%d\n", node.KeyCount)
		fmt.Fprintf(w, "Bytes:\t%s\n", formatBytes(uint64(node.ByteCount)))
		fmt.Fprintln(w)

	default:
		return NewUnsupportedOutputFormatError("displayNodeState", nodeOutputFormat)
	}

	return nil
}

// displayNodeReplacementReport displays node replacement report.
func displayNodeReplacementReport(report *NodeReplacementReport) error {
	switch nodeOutputFormat {
	case "json":
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return api.NewMarshalError(err)
		}
		fmt.Println(string(data))

	case "table":
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		defer w.Flush()

		if report.NewNodeID != "" {
			fmt.Fprintln(w, "\n=== Node Replacement Report ===")
			fmt.Fprintf(w, "Old Node:\t%s\n", report.OldNodeID)
			fmt.Fprintf(w, "New Node:\t%s\n", report.NewNodeID)
		} else {
			fmt.Fprintln(w, "\n=== Node Removal Report ===")
			fmt.Fprintf(w, "Node ID:\t%s\n", report.OldNodeID)
		}

		fmt.Fprintf(w, "Start Time:\t%s\n", report.StartTime.Format(time.RFC3339))
		fmt.Fprintf(w, "End Time:\t%s\n", report.EndTime.Format(time.RFC3339))
		fmt.Fprintf(w, "Duration:\t%s\n", report.Duration.Round(time.Second))
		fmt.Fprintln(w)

		fmt.Fprintf(w, "Keys Evacuated:\t%d\n", report.KeysEvacuated)
		fmt.Fprintf(w, "Bytes Evacuated:\t%s\n", formatBytes(uint64(report.BytesEvacuated)))
		fmt.Fprintf(w, "Evacuation Time:\t%s\n", report.EvacuationTime.Round(time.Second))

		if report.VerificationTime > 0 {
			fmt.Fprintf(w, "Verification Time:\t%s\n", report.VerificationTime.Round(time.Second))
		}

		fmt.Fprintf(w, "Success:\t%v\n", report.Success)

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

		if report.Success {
			if report.NewNodeID != "" {
				fmt.Fprintln(w, "Node replacement completed successfully!")
			} else {
				fmt.Fprintln(w, "Node removal completed successfully!")
			}
		} else {
			fmt.Fprintln(w, "Operation completed with errors")
		}

		fmt.Fprintln(w)

	default:
		return NewUnsupportedOutputFormatError("displayNodeReplacementReport", nodeOutputFormat)
	}

	return nil
}
