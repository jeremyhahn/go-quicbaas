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

// Package main provides agent fleet management CLI commands for QuicBaaS.
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
)

// =============================================================================
// Agent Command Flags
// =============================================================================

var (
	agentOutputFmt string
	agentName      string
	agentHost      string
	agentType      string
	agentBackend   string
	agentModel     string
	agentWorkDir   string
	agentStatus    string
)

// =============================================================================
// Cobra Command Tree
// =============================================================================

// newAgentCommand creates the agent fleet management command group.
func newAgentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent fleet management commands",
		Long: `Agent fleet management commands for registering, monitoring, and managing agents.

These commands interact with the QuicBaaS orchestrator SDK client to:
  - Register agents with the fleet registry
  - List and filter registered agents
  - Get detailed agent status
  - Remove agents from the fleet
  - Send heartbeats to keep agents alive

Examples:
  quicbaas agent register --name worker-1 --host node01:9090 --type claude
  quicbaas agent list --tenant my-tenant
  quicbaas agent status agent_abc123 --tenant my-tenant
  quicbaas agent heartbeat agent_abc123 --tenant my-tenant
  quicbaas agent remove agent_abc123 --tenant my-tenant`,
	}

	cmd.PersistentFlags().StringVar(&agentOutputFmt, "output", "table", "Output format (json, yaml, table)")

	cmd.AddCommand(newAgentRegisterCommand())
	cmd.AddCommand(newAgentListCommand())
	cmd.AddCommand(newAgentStatusCommand())
	cmd.AddCommand(newAgentRemoveCommand())
	cmd.AddCommand(newAgentHeartbeatCommand())
	cmd.AddCommand(newAgentServeCommand())
	cmd.AddCommand(newAgentDeployCommand())

	return cmd
}

// =============================================================================
// Register Command
// =============================================================================

// newAgentRegisterCommand creates the agent register subcommand.
func newAgentRegisterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register an agent with the fleet",
		Long: `Register a new agent with the fleet registry.

The agent will be assigned a unique ID and set to "registered" status.

Examples:
  quicbaas agent register --name worker-1 --host node01:9090 --type claude
  quicbaas agent register --name worker-2 --host node02:9090 --type ollama --backend ollama --work-dir /data/agents
  quicbaas agent register --name worker-3 --host node03:9090 --type claude --tenant my-tenant --output json`,
		RunE: runAgentRegister,
	}

	cmd.Flags().StringVar(&agentName, "name", "", "Agent name (required)")
	cmd.Flags().StringVar(&agentHost, "host", "", "Agent host address (required)")
	cmd.Flags().StringVar(&agentType, "type", "", "Agent type (required)")
	cmd.Flags().StringVar(&agentBackend, "backend", "", "Agent backend name")
	cmd.Flags().StringVar(&agentModel, "model", "", "Agent model identifier")
	cmd.Flags().StringVar(&agentWorkDir, "work-dir", "", "Agent working directory")

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

// runAgentRegister executes the agent register command.
func runAgentRegister(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &orchestratorSDK.RegisterAgentRequest{
		TenantID:  globalTenantID,
		Name:      agentName,
		Host:      agentHost,
		AgentType: agentType,
		Backend:   agentBackend,
		Model:     agentModel,
		WorkDir:   agentWorkDir,
	}

	resp, err := client.RegisterAgent(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatAgentRegistrationResponse(resp, agentOutputFmt, os.Stdout)
}

// =============================================================================
// List Command
// =============================================================================

// newAgentListCommand creates the agent list subcommand.
func newAgentListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered agents",
		Long: `List all registered agents with optional filters.

Examples:
  quicbaas agent list --tenant my-tenant
  quicbaas agent list --tenant my-tenant --status running
  quicbaas agent list --tenant my-tenant --host node01:9090 --output json
  quicbaas agent list --tenant my-tenant --type claude`,
		Args: cobra.NoArgs,
		RunE: runAgentList,
	}

	cmd.Flags().StringVar(&agentHost, "host", "", "Filter by host address")
	cmd.Flags().StringVar(&agentStatus, "status", "", "Filter by agent status (registered, running, idle, offline, error)")
	cmd.Flags().StringVar(&agentType, "type", "", "Filter by agent type")
	cmd.Flags().StringVar(&agentBackend, "backend", "", "Filter by backend name")

	return cmd
}

// runAgentList executes the agent list command.
func runAgentList(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &orchestratorSDK.ListAgentsRequest{
		TenantID:  globalTenantID,
		Host:      agentHost,
		Status:    agentStatus,
		AgentType: agentType,
		Backend:   agentBackend,
	}

	resp, err := client.ListAgents(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatAgentListResponse(resp, agentOutputFmt, os.Stdout)
}

// =============================================================================
// Status Command
// =============================================================================

// newAgentStatusCommand creates the agent status subcommand.
func newAgentStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <agentID>",
		Short: "Get agent details",
		Long: `Retrieve detailed status for a specific agent.

Examples:
  quicbaas agent status agent_abc123 --tenant my-tenant
  quicbaas agent status agent_abc123 --tenant my-tenant --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentStatus,
	}
}

// runAgentStatus executes the agent status command.
func runAgentStatus(cmd *cobra.Command, args []string) error {
	agentID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	agent, err := client.GetAgent(ctx, &orchestratorSDK.GetAgentRequest{
		TenantID: globalTenantID,
		AgentID:  agentID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatAgentDetailResponse(agent, agentOutputFmt, os.Stdout)
}

// =============================================================================
// Remove Command
// =============================================================================

// newAgentRemoveCommand creates the agent remove subcommand.
func newAgentRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <agentID>",
		Short: "Remove an agent from the fleet",
		Long: `Remove a registered agent from the fleet registry.

Examples:
  quicbaas agent remove agent_abc123 --tenant my-tenant
  quicbaas agent remove agent_abc123 --tenant my-tenant --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentRemove,
	}
}

// runAgentRemove executes the agent remove command.
func runAgentRemove(cmd *cobra.Command, args []string) error {
	agentID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.RemoveAgent(ctx, &orchestratorSDK.RemoveAgentRequest{
		TenantID: globalTenantID,
		AgentID:  agentID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatAgentRemoveResponse(resp, agentOutputFmt, os.Stdout)
}

// =============================================================================
// Heartbeat Command
// =============================================================================

// newAgentHeartbeatCommand creates the agent heartbeat subcommand.
func newAgentHeartbeatCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "heartbeat <agentID>",
		Short: "Send heartbeat for an agent",
		Long: `Send a heartbeat to update the agent's last seen timestamp.

Examples:
  quicbaas agent heartbeat agent_abc123 --tenant my-tenant
  quicbaas agent heartbeat agent_abc123 --tenant my-tenant --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runAgentHeartbeat,
	}
}

// runAgentHeartbeat executes the agent heartbeat command.
func runAgentHeartbeat(cmd *cobra.Command, args []string) error {
	agentID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.AgentHeartbeat(ctx, &orchestratorSDK.AgentHeartbeatRequest{
		TenantID: globalTenantID,
		AgentID:  agentID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatAgentHeartbeatResponse(resp, agentOutputFmt, os.Stdout)
}

// =============================================================================
// Formatting Functions
// =============================================================================

// formatAgentRegistrationResponse formats a register agent response for output.
func formatAgentRegistrationResponse(resp *orchestratorSDK.RegisterAgentResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "ID:\t%s\n", resp.Agent.ID)
		fmt.Fprintf(tw, "Name:\t%s\n", resp.Agent.Name)
		fmt.Fprintf(tw, "Host:\t%s\n", resp.Agent.Host)
		fmt.Fprintf(tw, "Type:\t%s\n", resp.Agent.AgentType)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Agent.Status)
		if resp.Agent.Backend != "" {
			fmt.Fprintf(tw, "Backend:\t%s\n", resp.Agent.Backend)
		}
		if resp.Agent.WorkDir != "" {
			fmt.Fprintf(tw, "Work Dir:\t%s\n", resp.Agent.WorkDir)
		}
		if resp.Agent.RegisteredAt != 0 {
			fmt.Fprintf(tw, "Registered:\t%s\n", time.Unix(0, resp.Agent.RegisteredAt).Format(time.RFC3339))
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Agent registered: %s (name: %s, host: %s, status: %s)\n",
			resp.Agent.ID, resp.Agent.Name, resp.Agent.Host, resp.Agent.Status)
		return nil
	}
}

// formatAgentListResponse formats a list agents response for output.
func formatAgentListResponse(resp *orchestratorSDK.ListAgentsResponse, format string, w io.Writer) error {
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
		for _, a := range resp.Agents {
			lastSeen := "-"
			if a.LastSeen > 0 {
				lastSeen = time.Unix(0, a.LastSeen).Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				a.ID, a.Name, a.Host, a.AgentType, a.Status, lastSeen)
		}
		fmt.Fprintf(tw, "\nTotal: %d agents\n", resp.Count)
		return tw.Flush()

	default:
		for _, a := range resp.Agents {
			fmt.Fprintf(w, "%s: %s (%s) [%s]\n", a.ID, a.Name, a.Host, a.Status)
		}
		return nil
	}
}

// formatAgentDetailResponse formats a single agent detail response for output.
func formatAgentDetailResponse(agent *orchestratorSDK.AgentRegistration, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(agent)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(agent)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID:\t%s\n", agent.ID)
		fmt.Fprintf(tw, "Tenant ID:\t%s\n", agent.TenantID)
		fmt.Fprintf(tw, "Name:\t%s\n", agent.Name)
		fmt.Fprintf(tw, "Host:\t%s\n", agent.Host)
		fmt.Fprintf(tw, "Type:\t%s\n", agent.AgentType)
		fmt.Fprintf(tw, "Status:\t%s\n", agent.Status)
		if agent.Backend != "" {
			fmt.Fprintf(tw, "Backend:\t%s\n", agent.Backend)
		}
		if agent.WorkDir != "" {
			fmt.Fprintf(tw, "Work Dir:\t%s\n", agent.WorkDir)
		}
		fmt.Fprintf(tw, "Version:\t%d\n", agent.Version)
		if agent.RegisteredAt != 0 {
			fmt.Fprintf(tw, "Registered:\t%s\n", time.Unix(0, agent.RegisteredAt).Format(time.RFC3339))
		}
		lastSeen := "-"
		if agent.LastSeen > 0 {
			lastSeen = time.Unix(0, agent.LastSeen).Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "Last Seen:\t%s\n", lastSeen)
		if len(agent.Metadata) > 0 {
			fmt.Fprintf(tw, "\nMetadata:\n")
			for k, v := range agent.Metadata {
				fmt.Fprintf(tw, "  %s:\t%s\n", k, v)
			}
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s: %s (%s) [%s] on %s\n",
			agent.ID, agent.Name, agent.AgentType, agent.Status, agent.Host)
		return nil
	}
}

// formatAgentRemoveResponse formats a remove agent response for output.
func formatAgentRemoveResponse(resp *orchestratorSDK.RemoveAgentResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Agent ID:\t%s\n", resp.AgentID)
		fmt.Fprintf(tw, "Removed:\t%v\n", resp.Removed)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Agent %s removed\n", resp.AgentID)
		return nil
	}
}

// formatAgentHeartbeatResponse formats a heartbeat response for output.
func formatAgentHeartbeatResponse(resp *orchestratorSDK.AgentHeartbeatResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Agent ID:\t%s\n", resp.AgentID)
		fmt.Fprintf(tw, "Heartbeat:\t%v\n", resp.Heartbeat)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Heartbeat sent for agent %s\n", resp.AgentID)
		return nil
	}
}
