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
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// newClusterCommands creates the cluster management command group.
func newClusterCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Cluster management commands (status)",
		Long: `Cluster management commands for monitoring QuicBaaS cluster nodes.

These commands allow you to view cluster status and health.
All commands connect to a running QuicBaaS server via gRPC.

Note: Use the global --address flag to specify the server address.`,
	}

	// Add subcommands
	cmd.AddCommand(newClusterStatusCommand())

	return cmd
}

// newClusterStatusCommand creates the cluster status command.
func newClusterStatusCommand() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show cluster status and node information",
		Long: `Display the current status of the QuicBaaS cluster.

Shows information about all nodes including:
  - Node ID
  - Address
  - Role (leader/follower)
  - Health status
  - Last heartbeat time

Note: Use the global --address flag to specify the server address.

Example:
  quicbaas cluster status --address localhost:50051`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Use global timeout from main.go
			ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
			defer cancel()

			// Create SDK client using global configuration from main.go
			client, err := createSDKClient()
			if err != nil {
				return api.NewOperationFailedError("create client", err)
			}
			defer func() { _ = closeSDKClient() }()

			// Get cluster status using SDK
			sdkStatus, err := client.Cluster().Status(ctx)
			if err != nil {
				return api.NewOperationFailedError("get cluster status", err)
			}

			// Convert SDK response to CLI response for output formatting
			cliResp := &cli.ClusterStatusResponse{
				Healthy: sdkStatus.Healthy,
				Nodes:   make([]cli.NodeInfo, len(sdkStatus.Members)),
			}

			// Parse LeaderID from string to uint64
			if sdkStatus.LeaderID != "" {
				if leaderID, err := strconv.ParseUint(sdkStatus.LeaderID, 10, 64); err == nil {
					cliResp.LeaderID = leaderID
				}
			}

			// Convert members to nodes
			for i, member := range sdkStatus.Members {
				var nodeID uint64
				if member.ID != "" {
					nodeID, _ = strconv.ParseUint(member.ID, 10, 64)
				}
				cliResp.Nodes[i] = cli.NodeInfo{
					NodeID:    nodeID,
					Address:   member.Address,
					IsLeader:  member.IsLeader,
					IsHealthy: member.Healthy,
				}
			}
			cliResp.NodeCount = len(sdkStatus.Members)
			cliResp.ReadyNodes = len(sdkStatus.Members) // Assume all members are ready

			// Display output based on format
			switch outputFormat {
			case "json":
				return printClusterStatusJSON(cliResp)
			case "table":
				return printClusterStatusTableCLI(cliResp)
			default:
				return api.NewInvalidFormatMessageError("output format", outputFormat)
			}
		},
	}

	// Only define command-specific flags; --address and --timeout are global flags defined in main.go
	cmd.Flags().StringVar(&outputFormat, "output", "table", "Output format (json, table)")

	return cmd
}

// printClusterStatusJSON prints cluster status in JSON format.
func printClusterStatusJSON(status interface{}) error {
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return api.NewMarshalError(err)
	}
	fmt.Println(string(data))
	return nil
}

// printClusterStatusTableCLI prints CLI cluster status in table format.
func printClusterStatusTableCLI(status interface{}) error {
	// Type assertion to get the actual CLI response
	cliResp, ok := status.(*cli.ClusterStatusResponse)
	if !ok {
		// Fallback to generic printing
		return printClusterStatusJSON(status)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	defer w.Flush()

	// Print cluster summary
	fmt.Fprintln(w, "CLUSTER STATUS")
	fmt.Fprintln(w, "--------------")
	healthStatus := "Healthy"
	if !cliResp.Healthy {
		healthStatus = "Unhealthy"
	}
	fmt.Fprintf(w, "Health:\t%s\n", healthStatus)
	fmt.Fprintf(w, "Leader ID:\t%d\n", cliResp.LeaderID)
	fmt.Fprintf(w, "Total Nodes:\t%d\n", cliResp.NodeCount)
	fmt.Fprintf(w, "Ready Nodes:\t%d\n", cliResp.ReadyNodes)
	fmt.Fprintln(w)

	// Print node details if available
	if len(cliResp.Nodes) > 0 {
		fmt.Fprintln(w, "NODE ID\tADDRESS\tROLE\tHEALTH\tLAST SEEN")
		fmt.Fprintln(w, "-------\t-------\t----\t------\t---------")

		for _, node := range cliResp.Nodes {
			role := "Follower"
			if node.IsLeader {
				role = "Leader"
			}

			health := "Healthy"
			if !node.IsHealthy {
				health = "Unhealthy"
			}

			lastSeen := "-"
			if !node.LastSeen.IsZero() {
				lastSeen = time.Since(node.LastSeen).Round(time.Second).String() + " ago"
			}

			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
				node.NodeID,
				node.Address,
				role,
				health,
				lastSeen,
			)
		}
		fmt.Fprintln(w)
	}

	// Print Raft group information if available
	if len(cliResp.RaftGroupInfo) > 0 {
		fmt.Fprintln(w, "RAFT GROUPS")
		fmt.Fprintln(w, "-----------")
		fmt.Fprintln(w, "GROUP ID\tLEADER ID\tMEMBERS")
		fmt.Fprintln(w, "--------\t---------\t-------")

		for groupID, groupInfo := range cliResp.RaftGroupInfo {
			fmt.Fprintf(w, "%d\t%d\t%d\n",
				groupID,
				groupInfo.LeaderID,
				groupInfo.MemberCount,
			)
		}
		fmt.Fprintln(w)
	}

	return nil
}
