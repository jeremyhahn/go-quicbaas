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

// Package main provides fleet management CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// =============================================================================
// Fleet Command Flags
// =============================================================================

var (
	fleetOutputFmt string
	nodeName       string
	nodeHost       string
	nodeType       string
	nodeBackend    string
	nodeModel      string
	nodeWorkDir    string
	nodeStatus     string

	// fleetListPage pages, orders and searches `fleet list`.
	fleetListPage pageFlags
)

// =============================================================================
// Cobra Command Tree
// =============================================================================

// newFleetCommand creates the fleet fleet management command group.
func newFleetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fleet",
		Short: "Fleet management commands",
		Long: `Fleet management commands for registering, monitoring, deploying and
running the fleet nodes that host agent sessions.

These commands interact with the QuicBaaS orchestrator SDK client to:
  - Register fleet nodes with the fleet registry
  - List and filter registered fleet nodes
  - Get detailed fleet node status
  - Remove fleet nodes from the fleet
  - Send heartbeats to keep fleet nodes alive

They also deploy, start, stop and undeploy the node daemon on a remote host,
and run it in the foreground (fleet serve).

Examples:
  quicbaas fleet register --name worker-1 --host node01:9090 --type claude
  quicbaas fleet list --tenant my-tenant
  quicbaas fleet status node_abc123 --tenant my-tenant
  quicbaas fleet heartbeat node_abc123 --tenant my-tenant
  quicbaas fleet remove node_abc123 --tenant my-tenant`,
	}

	cmd.PersistentFlags().StringVar(&fleetOutputFmt, "output", "table", "Output format (json, yaml, table)")

	cmd.AddCommand(newFleetRegisterCommand())
	cmd.AddCommand(newFleetListCommand())
	cmd.AddCommand(newFleetStatusCommand())
	cmd.AddCommand(newFleetRemoveCommand())
	cmd.AddCommand(newFleetHeartbeatCommand())
	cmd.AddCommand(newFleetServeCommand())
	cmd.AddCommand(newFleetDeployCommand())
	cmd.AddCommand(newFleetUndeployCommand())
	cmd.AddCommand(newFleetStopCommand())
	cmd.AddCommand(newFleetStartCommand())

	return cmd
}

// =============================================================================
// Register Command
// =============================================================================

// newFleetRegisterCommand creates the fleet register subcommand.
func newFleetRegisterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a fleet node",
		Long: `Register a new node with the fleet registry.

The node will be assigned a unique ID and set to "registered" status.

Examples:
  quicbaas fleet register --name worker-1 --host node01:9090 --type claude
  quicbaas fleet register --name worker-2 --host node02:9090 --type ollama --backend ollama --work-dir /data/fleet
  quicbaas fleet register --name worker-3 --host node03:9090 --type claude --tenant my-tenant --output json`,
		RunE: runFleetRegister,
	}

	cmd.Flags().StringVar(&nodeName, "name", "", "Fleet node name (required)")
	cmd.Flags().StringVar(&nodeHost, "host", "", "Fleet node host address (required)")
	cmd.Flags().StringVar(&nodeType, "type", "", "Fleet node type (required)")
	cmd.Flags().StringVar(&nodeBackend, "backend", "", "Primary backend name")
	cmd.Flags().StringVar(&nodeModel, "model", "", "Default model identifier")
	cmd.Flags().StringVar(&nodeWorkDir, "work-dir", "", "Fleet node working directory")

	if err := cmd.MarkFlagRequired("name"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark name flag as required: %v\n", err)
	}
	if err := cmd.MarkFlagRequired("host"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark host flag as required: %v\n", err)
	}
	if err := cmd.MarkFlagRequired("type"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark type flag as required: %v\n", err)
	}

	return cmd
}

// runFleetRegister executes the fleet register command.
func runFleetRegister(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &orchestratorSDK.RegisterFleetNodeRequest{
		TenantID: globalTenantID,
		Name:     nodeName,
		Host:     nodeHost,
		NodeType: nodeType,
		Backend:  nodeBackend,
		Model:    nodeModel,
		WorkDir:  nodeWorkDir,
	}

	resp, err := client.RegisterFleetNode(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatFleetRegistrationResponse(resp, fleetOutputFmt, os.Stdout)
}

// =============================================================================
// List Command
// =============================================================================

// newFleetListCommand creates the fleet list subcommand.
func newFleetListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered fleet nodes",
		Long: `List all registered fleet nodes with optional filters.

Examples:
  quicbaas fleet list --tenant my-tenant
  quicbaas fleet list --tenant my-tenant --status running
  quicbaas fleet list --tenant my-tenant --host node01:9090 --output json
  quicbaas fleet list --tenant my-tenant --type claude
  quicbaas fleet list --tenant my-tenant --search node01 --sort name --limit 50
  quicbaas fleet list --tenant my-tenant --all`,
		Args: cobra.NoArgs,
		RunE: runFleetList,
	}
	fleetListPage.bind(cmd)

	cmd.Flags().StringVar(&nodeHost, "host", "", "Filter by host address")
	cmd.Flags().StringVar(&nodeStatus, "status", "", "Filter by fleet node status (registered, running, idle, offline, error)")
	cmd.Flags().StringVar(&nodeType, "type", "", "Filter by fleet node type")
	cmd.Flags().StringVar(&nodeBackend, "backend", "", "Filter by backend name")

	return cmd
}

// runFleetList executes the fleet list command.
func runFleetList(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	nodes, env, err := fetchPages(&fleetListPage, func(params page.Params) ([]*orchestratorSDK.FleetNode, page.Envelope, error) {
		resp, err := client.ListFleetNodes(ctx, &orchestratorSDK.ListFleetNodesRequest{
			TenantID: globalTenantID,
			Host:     nodeHost,
			Status:   nodeStatus,
			NodeType: nodeType,
			Backend:  nodeBackend,
			Params:   params,
		})
		if err != nil || resp == nil {
			return nil, page.Envelope{}, err
		}
		return resp.Nodes, resp.Envelope, nil
	})
	if err != nil {
		return formatError(err)
	}

	resp := &orchestratorSDK.ListFleetNodesResponse{Nodes: nodes, Count: len(nodes), Envelope: env}
	return formatFleetListResponse(resp, fleetOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// Status Command
// =============================================================================

// newFleetStatusCommand creates the fleet status subcommand.
func newFleetStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <nodeID>",
		Short: "Get fleet node details",
		Long: `Retrieve detailed status for one fleet node.

Examples:
  quicbaas fleet status node_abc123 --tenant my-tenant
  quicbaas fleet status node_abc123 --tenant my-tenant --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runFleetStatus,
	}
}

// runFleetStatus executes the fleet status command.
func runFleetStatus(cmd *cobra.Command, args []string) error {
	nodeID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	node, err := client.GetFleetNode(ctx, &orchestratorSDK.GetFleetNodeRequest{
		TenantID: globalTenantID,
		NodeID:   nodeID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatFleetDetailResponse(node, fleetOutputFmt, os.Stdout)
}

// =============================================================================
// Remove Command
// =============================================================================

// newFleetRemoveCommand creates the fleet remove subcommand.
func newFleetRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <nodeID>",
		Short: "Remove a fleet node",
		Long: `Remove a registered node from the fleet registry.

Examples:
  quicbaas fleet remove node_abc123 --tenant my-tenant
  quicbaas fleet remove node_abc123 --tenant my-tenant --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runFleetRemove,
	}
}

// runFleetRemove executes the fleet remove command.
func runFleetRemove(cmd *cobra.Command, args []string) error {
	nodeID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.RemoveFleetNode(ctx, &orchestratorSDK.RemoveFleetNodeRequest{
		TenantID: globalTenantID,
		NodeID:   nodeID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatFleetRemoveResponse(resp, fleetOutputFmt, os.Stdout)
}

// =============================================================================
// Heartbeat Command
// =============================================================================

// newFleetHeartbeatCommand creates the fleet heartbeat subcommand.
func newFleetHeartbeatCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "heartbeat <nodeID>",
		Short: "Send a heartbeat for a fleet node",
		Long: `Send a heartbeat to update the fleet node's last seen timestamp.

Examples:
  quicbaas fleet heartbeat node_abc123 --tenant my-tenant
  quicbaas fleet heartbeat node_abc123 --tenant my-tenant --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runFleetHeartbeat,
	}
}

// runFleetHeartbeat executes the fleet heartbeat command.
func runFleetHeartbeat(cmd *cobra.Command, args []string) error {
	nodeID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.FleetNodeHeartbeat(ctx, &orchestratorSDK.FleetNodeHeartbeatRequest{
		TenantID: globalTenantID,
		NodeID:   nodeID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatFleetHeartbeatResponse(resp, fleetOutputFmt, os.Stdout)
}

// =============================================================================
// Formatting Functions
// =============================================================================

// formatFleetRegistrationResponse formats a register fleet response for output.
func formatFleetRegistrationResponse(resp *orchestratorSDK.RegisterFleetNodeResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID:\t%s\n", resp.Node.ID)
		fmt.Fprintf(tw, "Name:\t%s\n", resp.Node.Name)
		fmt.Fprintf(tw, "Host:\t%s\n", resp.Node.Host)
		fmt.Fprintf(tw, "Type:\t%s\n", resp.Node.NodeType)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Node.Status)
		if resp.Node.Backend != "" {
			fmt.Fprintf(tw, "Backend:\t%s\n", resp.Node.Backend)
		}
		if resp.Node.WorkDir != "" {
			fmt.Fprintf(tw, "Work Dir:\t%s\n", resp.Node.WorkDir)
		}
		if resp.Node.RegisteredAt != 0 {
			fmt.Fprintf(tw, "Registered:\t%s\n", time.Unix(0, resp.Node.RegisteredAt).Format(time.RFC3339))
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Fleet node registered: %s (name: %s, host: %s, status: %s)\n",
			resp.Node.ID, resp.Node.Name, resp.Node.Host, resp.Node.Status)
		return nil
	}
}

// formatFleetListResponse formats a list fleets response for output.
func formatFleetListResponse(resp *orchestratorSDK.ListFleetNodesResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tNAME\tHOST\tTYPE\tSTATUS\tLAST SEEN\n")
		for _, a := range resp.Nodes {
			lastSeen := "-"
			if a.LastSeen > 0 {
				lastSeen = time.Unix(0, a.LastSeen).Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				a.ID, a.Name, a.Host, a.NodeType, a.Status, lastSeen)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		writePageFooter(w, len(resp.Nodes), resp.Envelope)
		return nil

	default:
		for _, a := range resp.Nodes {
			fmt.Fprintf(w, "%s: %s (%s) [%s]\n", a.ID, a.Name, a.Host, a.Status)
		}
		writePageFooter(w, len(resp.Nodes), resp.Envelope)
		return nil
	}
}

// formatFleetDetailResponse formats a single fleet detail response for output.
func formatFleetDetailResponse(node *orchestratorSDK.FleetNode, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(node)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(node)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID:\t%s\n", node.ID)
		fmt.Fprintf(tw, "Tenant ID:\t%s\n", node.TenantID)
		fmt.Fprintf(tw, "Name:\t%s\n", node.Name)
		fmt.Fprintf(tw, "Host:\t%s\n", node.Host)
		fmt.Fprintf(tw, "Type:\t%s\n", node.NodeType)
		fmt.Fprintf(tw, "Status:\t%s\n", node.Status)
		if node.Backend != "" {
			fmt.Fprintf(tw, "Backend:\t%s\n", node.Backend)
		}
		if node.WorkDir != "" {
			fmt.Fprintf(tw, "Work Dir:\t%s\n", node.WorkDir)
		}
		fmt.Fprintf(tw, "Version:\t%d\n", node.Version)
		if node.RegisteredAt != 0 {
			fmt.Fprintf(tw, "Registered:\t%s\n", time.Unix(0, node.RegisteredAt).Format(time.RFC3339))
		}
		lastSeen := "-"
		if node.LastSeen > 0 {
			lastSeen = time.Unix(0, node.LastSeen).Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "Last Seen:\t%s\n", lastSeen)
		if len(node.Metadata) > 0 {
			fmt.Fprintf(tw, "\nMetadata:\n")
			for k, v := range node.Metadata {
				fmt.Fprintf(tw, "  %s:\t%s\n", k, v)
			}
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s: %s (%s) [%s] on %s\n",
			node.ID, node.Name, node.NodeType, node.Status, node.Host)
		return nil
	}
}

// formatFleetRemoveResponse formats a remove fleet response for output.
func formatFleetRemoveResponse(resp *orchestratorSDK.RemoveFleetNodeResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Node ID:\t%s\n", resp.NodeID)
		fmt.Fprintf(tw, "Removed:\t%v\n", resp.Removed)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Fleet node %s removed\n", resp.NodeID)
		return nil
	}
}

// formatFleetHeartbeatResponse formats a heartbeat response for output.
func formatFleetHeartbeatResponse(resp *orchestratorSDK.FleetNodeHeartbeatResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Node ID:\t%s\n", resp.NodeID)
		fmt.Fprintf(tw, "Heartbeat:\t%v\n", resp.Heartbeat)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Heartbeat sent for fleet node %s\n", resp.NodeID)
		return nil
	}
}
