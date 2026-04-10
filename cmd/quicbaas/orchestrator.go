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

// Package main provides orchestrator CLI commands for QuicBaaS agent management.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// Orchestrator Command Flags
// =============================================================================

var (
	orchOutputFmt   string
	orchBackend     string
	orchModel       string
	orchSystemPrmpt string
	orchProjectID   string

	// Task flags.
	orchTaskSubject     string
	orchTaskDescription string
	orchTaskTeamName    string
	orchTaskAssignedTo  string
	orchTaskStatus      string
	orchTaskAgentID     string
	orchTaskResult      string

	// Job flags.
	orchJobMaxIterations int
	orchJobPollInterval  string

	// Workflow flags.
	orchWorkflowName string
	orchWorkflowFile string

	// Display flags.
	orchVerbosity string
)

// =============================================================================
// Cobra Command Tree
// =============================================================================

// newOrchestratorCommand creates the orchestrator command group.
func newOrchestratorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "orchestrator",
		Short: "AI agent orchestration commands",
		Long: `Orchestrator commands for managing AI agent sessions, tasks, and backends.

These commands interact with the QuicBaaS orchestrator SDK client to:
  - Spawn and manage agent sessions across backends
  - Create, assign, and track orchestration tasks
  - List available agent backends
  - Check orchestrator health

Examples:
  quicbaas orchestrator spawn --backend claude
  quicbaas orchestrator prompt <sessionID> "Write tests for the auth module"
  quicbaas orchestrator task create --subject "Build the project" --team backend`,
	}

	cmd.PersistentFlags().StringVar(&orchOutputFmt, "output", "table", "Output format (json, yaml, table)")

	// Session commands.
	cmd.AddCommand(newOrchSpawnCommand())
	cmd.AddCommand(newOrchPromptCommand())
	cmd.AddCommand(newOrchStatusCommand())
	cmd.AddCommand(newOrchCloseCommand())
	cmd.AddCommand(newOrchSessionsCommand())
	cmd.AddCommand(newOrchBackendsCommand())
	cmd.AddCommand(newOrchHealthCommand())

	// Task command group.
	cmd.AddCommand(newOrchTaskCommand())

	// Decompose command.
	cmd.AddCommand(newOrchDecomposeCommand())

	// Job commands.
	cmd.AddCommand(newOrchRunCommand())
	cmd.AddCommand(newOrchJobCommand())

	// Workflow commands.
	cmd.AddCommand(newOrchWorkflowCommand())

	return cmd
}

// orchContext creates a context with the global tenant ID and timeout.
func orchContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	ctx = sdktransport.ContextWithTenant(ctx, globalTenantID)
	return ctx, cancel
}

// =============================================================================
// Spawn Command
// =============================================================================

// newOrchSpawnCommand creates the spawn subcommand.
func newOrchSpawnCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spawn",
		Short: "Spawn a new agent session",
		Long: `Create a persistent agent session on a specified backend.

Examples:
  quicbaas orchestrator spawn --backend claude
  quicbaas orchestrator spawn --backend ollama --model llama3
  quicbaas orchestrator spawn --backend claude --project proj_123`,
		RunE: runOrchSpawn,
	}

	cmd.Flags().StringVar(&orchBackend, "backend", "", "Agent backend name (required)")
	cmd.Flags().StringVar(&orchModel, "model", "", "Model to use for the session")
	cmd.Flags().StringVar(&orchSystemPrmpt, "system-prompt", "", "System prompt for the session")
	cmd.Flags().StringVar(&orchProjectID, "project", "", "Project ID to associate with the session")

	if err := cmd.MarkFlagRequired("backend"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark backend flag as required: %v\n", err)
	}

	return cmd
}

// runOrchSpawn executes the spawn command.
func runOrchSpawn(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	req := &orchestratorSDK.SpawnSessionRequest{
		Backend:   orchBackend,
		ProjectID: orchProjectID,
		Model:     orchModel,
	}

	resp, err := client.SpawnSession(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatOrchSpawnResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Prompt Command
// =============================================================================

// newOrchPromptCommand creates the prompt subcommand.
func newOrchPromptCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "prompt <sessionID> <text>",
		Short: "Send a prompt to an agent session",
		Long: `Send a prompt to an active agent session and display the response.

Examples:
  quicbaas orchestrator prompt sess_abc123 "Write unit tests for the auth handler"
  quicbaas orchestrator prompt sess_abc123 "Refactor the database layer" --output json`,
		Args: cobra.ExactArgs(2),
		RunE: runOrchPrompt,
	}
}

// runOrchPrompt executes the prompt command.
func runOrchPrompt(cmd *cobra.Command, args []string) error {
	sessionID := args[0]
	promptText := args[1]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	req := &orchestratorSDK.PromptSessionRequest{
		SessionID: sessionID,
		Prompt:    promptText,
	}

	resp, err := client.PromptSession(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatOrchPromptResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Status Command
// =============================================================================

// newOrchStatusCommand creates the status subcommand.
func newOrchStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <sessionID>",
		Short: "Get agent session status",
		Long: `Retrieve the current status of an active agent session.

Examples:
  quicbaas orchestrator status sess_abc123
  quicbaas orchestrator status sess_abc123 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchStatus,
	}
}

// runOrchStatus executes the status command.
func runOrchStatus(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.SessionStatus(ctx, &orchestratorSDK.SessionStatusRequest{
		SessionID: sessionID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatOrchStatusResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Close Command
// =============================================================================

// newOrchCloseCommand creates the close subcommand.
func newOrchCloseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "close <sessionID>",
		Short: "Close an agent session",
		Long: `Terminate an active agent session and release its resources.

Examples:
  quicbaas orchestrator close sess_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchClose,
	}
}

// runOrchClose executes the close command.
func runOrchClose(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.CloseSession(ctx, &orchestratorSDK.CloseSessionRequest{
		SessionID: sessionID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatOrchCloseResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Sessions Command
// =============================================================================

// newOrchSessionsCommand creates the sessions subcommand.
func newOrchSessionsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List active agent sessions",
		Long: `List all active agent sessions across backends.

Examples:
  quicbaas orchestrator sessions
  quicbaas orchestrator sessions --output json`,
		Args: cobra.NoArgs,
		RunE: runOrchSessions,
	}
}

// runOrchSessions executes the sessions command.
func runOrchSessions(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.ListSessions(ctx, &orchestratorSDK.ListSessionsRequest{})
	if err != nil {
		return formatError(err)
	}

	return formatOrchSessionsResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Backends Command
// =============================================================================

// newOrchBackendsCommand creates the backends subcommand.
func newOrchBackendsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "backends",
		Short: "List available agent backends",
		Long: `List all registered agent backends and their capabilities.

Examples:
  quicbaas orchestrator backends
  quicbaas orchestrator backends --output json`,
		Args: cobra.NoArgs,
		RunE: runOrchBackends,
	}
}

// runOrchBackends executes the backends command.
func runOrchBackends(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.ListBackends(ctx)
	if err != nil {
		return formatError(err)
	}

	return formatOrchBackendsResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Health Command
// =============================================================================

// newOrchHealthCommand creates the health subcommand.
func newOrchHealthCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check orchestrator health",
		Long: `Perform a health check on the orchestrator and all active sessions.

Examples:
  quicbaas orchestrator health
  quicbaas orchestrator health --output json`,
		Args: cobra.NoArgs,
		RunE: runOrchHealth,
	}
}

// runOrchHealth executes the health command.
func runOrchHealth(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.Health(ctx)
	if err != nil {
		return formatError(err)
	}

	return formatOrchHealthResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Task Command Group
// =============================================================================

// newOrchTaskCommand creates the task command group.
func newOrchTaskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Task management commands",
		Long: `Task management commands for creating, assigning, and tracking orchestration tasks.

Examples:
  quicbaas orchestrator task create --subject "Build the project" --team backend
  quicbaas orchestrator task list --status pending
  quicbaas orchestrator task get task_abc123
  quicbaas orchestrator task claim task_abc123 --agent agent_001
  quicbaas orchestrator task complete task_abc123 --result "All tests passing"`,
	}

	cmd.AddCommand(newOrchTaskCreateCommand())
	cmd.AddCommand(newOrchTaskGetCommand())
	cmd.AddCommand(newOrchTaskListCommand())
	cmd.AddCommand(newOrchTaskClaimCommand())
	cmd.AddCommand(newOrchTaskCompleteCommand())

	return cmd
}

// =============================================================================
// Task Create Command
// =============================================================================

// newOrchTaskCreateCommand creates the task create subcommand.
func newOrchTaskCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new orchestration task",
		Long: `Create a new task for agent orchestration.

Examples:
  quicbaas orchestrator task create --subject "Build the project"
  quicbaas orchestrator task create --subject "Run all tests" --team qa
  quicbaas orchestrator task create --subject "Deploy to staging" --team devops --assigned-to agent_001`,
		RunE: runOrchTaskCreate,
	}

	cmd.Flags().StringVar(&orchTaskSubject, "subject", "", "Task subject (required)")
	cmd.Flags().StringVar(&orchTaskDescription, "description", "", "Task description")
	cmd.Flags().StringVar(&orchTaskTeamName, "team", "", "Team name for the task")
	cmd.Flags().StringVar(&orchTaskAssignedTo, "assigned-to", "", "Agent ID to assign the task to")

	if err := cmd.MarkFlagRequired("subject"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark subject flag as required: %v\n", err)
	}

	return cmd
}

// runOrchTaskCreate executes the task create command.
func runOrchTaskCreate(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	req := &orchestratorSDK.CreateTaskRequest{
		TenantID:    globalTenantID,
		Subject:     orchTaskSubject,
		Description: orchTaskDescription,
		TeamName:    orchTaskTeamName,
		AssignedTo:  orchTaskAssignedTo,
	}

	resp, err := client.CreateTask(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatOrchTaskResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Task Get Command
// =============================================================================

// newOrchTaskGetCommand creates the task get subcommand.
func newOrchTaskGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <taskID>",
		Short: "Get task details",
		Long: `Retrieve details for a specific orchestration task.

Examples:
  quicbaas orchestrator task get task_abc123
  quicbaas orchestrator task get task_abc123 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchTaskGet,
	}
}

// runOrchTaskGet executes the task get command.
func runOrchTaskGet(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.GetTask(ctx, &orchestratorSDK.GetTaskRequest{
		TenantID: globalTenantID,
		TaskID:   taskID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatOrchTaskResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Task List Command
// =============================================================================

// newOrchTaskListCommand creates the task list subcommand.
func newOrchTaskListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List orchestration tasks",
		Long: `List orchestration tasks with optional filters.

Examples:
  quicbaas orchestrator task list
  quicbaas orchestrator task list --status pending
  quicbaas orchestrator task list --team backend --output json`,
		Args: cobra.NoArgs,
		RunE: runOrchTaskList,
	}

	cmd.Flags().StringVar(&orchTaskStatus, "status", "", "Filter by task status (pending, claimed, in_progress, completed, failed, cancelled)")
	cmd.Flags().StringVar(&orchTaskTeamName, "team", "", "Filter by team name")
	cmd.Flags().StringVar(&orchTaskAssignedTo, "assigned-to", "", "Filter by assigned agent")

	return cmd
}

// runOrchTaskList executes the task list command.
func runOrchTaskList(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	req := &orchestratorSDK.ListTasksRequest{
		TenantID:   globalTenantID,
		Status:     orchTaskStatus,
		TeamName:   orchTaskTeamName,
		AssignedTo: orchTaskAssignedTo,
	}

	resp, err := client.ListTasks(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatOrchTaskListResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Task Claim Command
// =============================================================================

// newOrchTaskClaimCommand creates the task claim subcommand.
func newOrchTaskClaimCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claim <taskID>",
		Short: "Claim a task for an agent",
		Long: `Atomically claim a task for an agent. The task must be in pending status.

Examples:
  quicbaas orchestrator task claim task_abc123 --agent agent_001`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchTaskClaim,
	}

	cmd.Flags().StringVar(&orchTaskAgentID, "agent", "", "Agent ID claiming the task (required)")

	if err := cmd.MarkFlagRequired("agent"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark agent flag as required: %v\n", err)
	}

	return cmd
}

// runOrchTaskClaim executes the task claim command.
func runOrchTaskClaim(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.ClaimTask(ctx, &orchestratorSDK.ClaimTaskRequest{
		TenantID: globalTenantID,
		TaskID:   taskID,
		AgentID:  orchTaskAgentID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatOrchClaimResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Task Complete Command
// =============================================================================

// newOrchTaskCompleteCommand creates the task complete subcommand.
func newOrchTaskCompleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "complete <taskID>",
		Short: "Complete an orchestration task",
		Long: `Mark a task as completed with an optional result.

Examples:
  quicbaas orchestrator task complete task_abc123
  quicbaas orchestrator task complete task_abc123 --result "All tests passing"`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchTaskComplete,
	}

	cmd.Flags().StringVar(&orchTaskResult, "result", "", "Task completion result text")

	return cmd
}

// runOrchTaskComplete executes the task complete command.
func runOrchTaskComplete(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	req := &orchestratorSDK.CompleteTaskRequest{
		TenantID: globalTenantID,
		TaskID:   taskID,
	}

	if orchTaskResult != "" {
		req.Result = &orchestratorSDK.TaskResult{
			Output: orchTaskResult,
		}
	}

	resp, err := client.CompleteTask(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatOrchCompleteResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Formatting Functions
// =============================================================================

// formatOrchSpawnResponse formats a spawn response for output.
func formatOrchSpawnResponse(resp *orchestratorSDK.SpawnSessionResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Session ID:\t%s\n", resp.SessionID)
		fmt.Fprintf(tw, "Backend:\t%s\n", resp.Backend)
		if resp.ContainerID != "" {
			fmt.Fprintf(tw, "Container ID:\t%s\n", resp.ContainerID)
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Session spawned: %s (backend: %s)\n", resp.SessionID, resp.Backend)
		return nil
	}
}

// formatOrchPromptResponse formats a prompt response for output.
func formatOrchPromptResponse(resp *orchestratorSDK.PromptSessionResponse, format string, w io.Writer) error {
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
		if resp.SessionID != "" {
			fmt.Fprintf(tw, "Session ID:\t%s\n", resp.SessionID)
		}
		fmt.Fprintf(tw, "Done:\t%v\n", resp.Done)
		fmt.Fprintf(tw, "\n")
		if err := tw.Flush(); err != nil {
			return err
		}
		if resp.Response != "" {
			fmt.Fprintf(w, "%s\n", resp.Response)
		}
		return nil

	default:
		if resp.Response != "" {
			fmt.Fprintf(w, "%s\n", resp.Response)
		}
		return nil
	}
}

// formatOrchStatusResponse formats a session status response for output.
func formatOrchStatusResponse(resp *orchestratorSDK.SessionStatusResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Session ID:\t%s\n", resp.SessionID)
		fmt.Fprintf(tw, "Backend:\t%s\n", resp.Backend)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Status)
		if resp.ContainerID != "" {
			fmt.Fprintf(tw, "Container ID:\t%s\n", resp.ContainerID)
		}
		if resp.CreatedAt != 0 {
			fmt.Fprintf(tw, "Created:\t%s\n", time.Unix(0, resp.CreatedAt).Format(time.RFC3339))
		}
		if resp.LastActive != 0 {
			fmt.Fprintf(tw, "Last Active:\t%s\n", time.Unix(0, resp.LastActive).Format(time.RFC3339))
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s: %s (%s)\n", resp.SessionID, resp.Backend, resp.Status)
		return nil
	}
}

// formatOrchCloseResponse formats a close session response for output.
func formatOrchCloseResponse(resp *orchestratorSDK.CloseSessionResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Session ID:\t%s\n", resp.SessionID)
		fmt.Fprintf(tw, "Closed:\t%v\n", resp.Closed)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Session %s closed\n", resp.SessionID)
		return nil
	}
}

// formatOrchSessionsResponse formats a list sessions response for output.
func formatOrchSessionsResponse(resp *orchestratorSDK.ListSessionsResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "SESSION ID\tBACKEND\tSTATUS\tLAST ACTIVE\n")
		for _, s := range resp.Sessions {
			lastActive := "-"
			if s.LastActive > 0 {
				lastActive = time.Unix(0, s.LastActive).Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.SessionID, s.Backend, s.Status, lastActive)
		}
		fmt.Fprintf(tw, "\nTotal: %d sessions\n", resp.Count)
		return tw.Flush()

	default:
		for _, s := range resp.Sessions {
			fmt.Fprintf(w, "%s: %s (%s)\n", s.SessionID, s.Backend, s.Status)
		}
		return nil
	}
}

// formatOrchBackendsResponse formats a list backends response for output.
func formatOrchBackendsResponse(resp *orchestratorSDK.ListBackendsResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "NAME\tTYPE\tSTATUS\tAVAILABLE\n")
		for _, b := range resp.Backends {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%v\n", b.Name, b.Type, b.Status, b.Available)
		}
		fmt.Fprintf(tw, "\nTotal: %d backends\n", resp.Count)
		return tw.Flush()

	default:
		for _, b := range resp.Backends {
			fmt.Fprintf(w, "%s (%s)\n", b.Name, b.Type)
		}
		return nil
	}
}

// formatOrchHealthResponse formats a health check response for output.
func formatOrchHealthResponse(resp *orchestratorSDK.HealthCheckReport, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Healthy:\t%v\n", resp.Healthy)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Status)
		if resp.Timestamp != 0 {
			fmt.Fprintf(tw, "Timestamp:\t%s\n", time.Unix(0, resp.Timestamp).Format(time.RFC3339))
		}
		if len(resp.Components) > 0 {
			fmt.Fprintf(tw, "\nCOMPONENT\tSTATUS\n")
			for name, status := range resp.Components {
				fmt.Fprintf(tw, "%s\t%s\n", name, status)
			}
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Health: %s (healthy: %v)\n", resp.Status, resp.Healthy)
		return nil
	}
}

// formatOrchTaskResponse formats a single task for output.
func formatOrchTaskResponse(task *orchestratorSDK.Task, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(task)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(task)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID:\t%s\n", task.ID)
		fmt.Fprintf(tw, "Subject:\t%s\n", task.Subject)
		fmt.Fprintf(tw, "Status:\t%s\n", task.Status)
		if task.TeamName != "" {
			fmt.Fprintf(tw, "Team:\t%s\n", task.TeamName)
		}
		if task.Description != "" {
			fmt.Fprintf(tw, "Description:\t%s\n", task.Description)
		}
		if task.AssignedTo != "" {
			fmt.Fprintf(tw, "Assigned To:\t%s\n", task.AssignedTo)
		}
		if task.Result != nil {
			fmt.Fprintf(tw, "Result:\t%s\n", task.Result.Output)
		}
		fmt.Fprintf(tw, "Version:\t%d\n", task.Version)
		if task.CreatedAt != 0 {
			fmt.Fprintf(tw, "Created:\t%s\n", time.Unix(0, task.CreatedAt).Format(time.RFC3339))
		}
		if task.ClaimedAt != 0 {
			fmt.Fprintf(tw, "Claimed:\t%s\n", time.Unix(0, task.ClaimedAt).Format(time.RFC3339))
		}
		if task.CompletedAt != 0 {
			fmt.Fprintf(tw, "Completed:\t%s\n", time.Unix(0, task.CompletedAt).Format(time.RFC3339))
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Task %s: %s (%s)\n", task.ID, task.Subject, task.Status)
		return nil
	}
}

// formatOrchTaskListResponse formats a task list response for output.
func formatOrchTaskListResponse(resp *orchestratorSDK.ListTasksResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "ID\tSUBJECT\tSTATUS\tTEAM\tASSIGNED TO\n")
		for _, t := range resp.Tasks {
			teamName := t.TeamName
			if teamName == "" {
				teamName = "-"
			}
			assignedTo := t.AssignedTo
			if assignedTo == "" {
				assignedTo = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Subject, t.Status, teamName, assignedTo)
		}
		fmt.Fprintf(tw, "\nTotal: %d tasks\n", resp.Count)
		return tw.Flush()

	default:
		for _, t := range resp.Tasks {
			fmt.Fprintf(w, "%s: %s (%s)\n", t.ID, t.Subject, t.Status)
		}
		return nil
	}
}

// formatOrchClaimResponse formats a claim response for output.
func formatOrchClaimResponse(resp *orchestratorSDK.ClaimTaskResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Task ID:\t%s\n", resp.TaskID)
		fmt.Fprintf(tw, "Agent ID:\t%s\n", resp.AgentID)
		fmt.Fprintf(tw, "Claimed:\t%v\n", resp.Claimed)
		return tw.Flush()

	default:
		if resp.Claimed {
			fmt.Fprintf(w, "Task %s claimed by %s\n", resp.TaskID, resp.AgentID)
		} else {
			fmt.Fprintf(w, "Failed to claim task %s\n", resp.TaskID)
		}
		return nil
	}
}

// formatOrchCompleteResponse formats a complete response for output.
func formatOrchCompleteResponse(resp *orchestratorSDK.CompleteTaskResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Task ID:\t%s\n", resp.TaskID)
		fmt.Fprintf(tw, "Completed:\t%v\n", resp.Completed)
		return tw.Flush()

	default:
		if resp.Completed {
			fmt.Fprintf(w, "Task %s completed\n", resp.TaskID)
		} else {
			fmt.Fprintf(w, "Failed to complete task %s\n", resp.TaskID)
		}
		return nil
	}
}

// =============================================================================
// Decompose Command
// =============================================================================

// newOrchDecomposeCommand creates the decompose subcommand.
func newOrchDecomposeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decompose <task>",
		Short: "Decompose a task into work items",
		Long: `Use an agent to decompose a high-level task into structured work items.

When --backend is specified, the named backend is used directly.
When --backend is omitted, the server's lead agent session is used
(if configured).

Examples:
  quicbaas orchestrator decompose "Implement user authentication"
  quicbaas orchestrator decompose "Build the REST API" --backend ollama
  quicbaas orchestrator decompose "Add caching layer" --backend claude --model claude-3-opus
  quicbaas orchestrator decompose "Refactor storage" --project proj_123`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchDecompose,
	}

	cmd.Flags().StringVar(&orchBackend, "backend", "", "Agent backend name (omit to use lead agent)")
	cmd.Flags().StringVar(&orchModel, "model", "", "Model override for the decomposition")
	cmd.Flags().StringVar(&orchProjectID, "project", "", "Project ID to scope the decomposition")

	return cmd
}

// runOrchDecompose executes the decompose command.
func runOrchDecompose(cmd *cobra.Command, args []string) error {
	taskDescription := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	req := &orchestratorSDK.DecomposeTaskRequest{
		Backend: orchBackend,
		Model:   orchModel,
		Task:    taskDescription,
	}
	if orchProjectID != "" {
		req.ProjectIDs = []string{orchProjectID}
	}

	resp, err := client.DecomposeTask(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatOrchDecomposeResponse(resp, orchOutputFmt, os.Stdout)
}

// formatOrchDecomposeResponse formats a decompose response for output.
func formatOrchDecomposeResponse(resp *orchestratorSDK.DecomposeTaskResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "ID\tPROJECT\tPRIORITY\tTITLE\tDEPENDENCIES\n")
		for _, item := range resp.WorkItems {
			deps := ""
			if len(item.Dependencies) > 0 {
				for i, d := range item.Dependencies {
					if i > 0 {
						deps += ","
					}
					deps += d
				}
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
				item.ID, item.ProjectID, item.Priority, item.Title, deps)
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Decomposed into %d work items:\n", len(resp.WorkItems))
		for _, item := range resp.WorkItems {
			fmt.Fprintf(w, "  [%d] %s: %s\n", item.Priority, item.ID, item.Title)
			if item.Description != "" {
				fmt.Fprintf(w, "      %s\n", item.Description)
			}
		}
		return nil
	}
}

// =============================================================================
// Run Command
// =============================================================================

// newOrchRunCommand creates the run subcommand for starting an orchestration job.
func newOrchRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <description>",
		Short: "Run an autonomous orchestration job",
		Long: `Start an autonomous orchestration control loop that decomposes a task,
assigns work items to agents, executes them, and reviews the results.

The control loop runs server-side. This command polls for status updates
and displays state transitions in real-time.

Use --workflow to select a canned workflow by name, or --workflow-file
to provide a custom workflow YAML definition.

Press Ctrl+C to cancel the running job.

Examples:
  quicbaas orchestrator run "Build a tic-tac-toe game"
  quicbaas orchestrator run "Implement user authentication" --backend claude
  quicbaas orchestrator run "Add caching layer" --backend ollama --model llama3
  quicbaas orchestrator run "Build the REST API" --project proj_123 --max-iterations 5
  quicbaas orchestrator run "Build a game" --workflow software-dev
  quicbaas orchestrator run "Custom task" --workflow-file ./my-workflow.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchRun,
	}

	cmd.Flags().StringVar(&orchBackend, "backend", "", "Lead agent backend name")
	cmd.Flags().StringVar(&orchModel, "model", "", "Lead agent model override")
	cmd.Flags().StringVar(&orchProjectID, "project", "", "Project ID to scope the job")
	cmd.Flags().IntVar(&orchJobMaxIterations, "max-iterations", 10, "Maximum re-delegation iterations")
	cmd.Flags().StringVar(&orchJobPollInterval, "poll-interval", "5s", "Interval between status polls")
	cmd.Flags().StringVar(&orchWorkflowName, "workflow", "", "Use a canned workflow by name (e.g. software-dev)")
	cmd.Flags().StringVar(&orchWorkflowFile, "workflow-file", "", "Path to a custom workflow YAML file")
	cmd.Flags().StringVar(&orchVerbosity, "verbosity", "info", "Display verbosity (info, verbose, debug)")

	return cmd
}

// runOrchRun executes the run command.
func runOrchRun(cmd *cobra.Command, args []string) error {
	description := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	req := &orchestratorSDK.StartJobRequest{
		TenantID:      globalTenantID,
		Description:   description,
		LeadBackend:   orchBackend,
		LeadModel:     orchModel,
		ProjectID:     orchProjectID,
		MaxIterations: orchJobMaxIterations,
		PollInterval:  orchJobPollInterval,
		WorkflowName:  orchWorkflowName,
		WorkflowFile:  orchWorkflowFile,
	}

	resp, err := client.StartJob(ctx, req)
	if err != nil {
		return formatError(err)
	}

	jobID := resp.JobID
	fmt.Fprintf(os.Stdout, "Job started: %s\n", jobID)

	display := newEventDisplay(os.Stdout)

	// Attempt to subscribe to real-time job events.
	// This is best-effort: if the transport doesn't support streaming,
	// the polling loop provides the same information.
	var eventCh <-chan *orchestratorSDK.JobEvent
	eventBg := sdktransport.ContextWithTenant(context.Background(), globalTenantID)
	eventCtx, eventCancel := context.WithCancel(eventBg)
	defer eventCancel()

	events, subErr := client.SubscribeJobEvents(eventCtx, &orchestratorSDK.SubscribeJobEventsRequest{
		TenantID: globalTenantID,
		JobID:    jobID,
	})
	if subErr == nil {
		eventCh = events
		fmt.Fprintf(os.Stdout, "Streaming job events...\n")
	} else {
		fmt.Fprintf(os.Stderr, "Event streaming unavailable: %v (falling back to polling)\n", subErr)
	}

	// Set up signal handling for graceful cancellation.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Parse poll interval for the ticker.
	pollDuration, parseErr := time.ParseDuration(orchJobPollInterval)
	if parseErr != nil {
		pollDuration = 5 * time.Second
	}

	ticker := time.NewTicker(pollDuration)
	defer ticker.Stop()

	var lastState string

	for {
		select {
		case <-sigCh:
			fmt.Fprintf(os.Stdout, "\nCancelling job %s...\n", jobID)
			cancelCtx, cancelCancel := orchContext()
			_, cancelErr := client.CancelJob(cancelCtx, &orchestratorSDK.CancelJobRequest{
				TenantID: globalTenantID,
				JobID:    jobID,
			})
			cancelCancel()
			if cancelErr != nil {
				return formatError(cancelErr)
			}
			fmt.Fprintf(os.Stdout, "Job %s cancelled.\n", jobID)
			return nil

		case evt, ok := <-eventCh:
			if !ok {
				// Stream closed, disable this case by nilling the channel.
				eventCh = nil
				continue
			}
			// Filter display based on verbosity setting.
			// The audit trail always captures everything; this only controls display.
			show := false
			switch orchVerbosity {
			case "debug":
				show = true
			case "verbose":
				show = !strings.HasPrefix(evt.Type, "agent:") ||
					evt.Type == "agent:assistant" || evt.Type == "agent:result"
			default: // "info"
				show = !strings.HasPrefix(evt.Type, "agent:") &&
					!strings.HasPrefix(evt.Type, "worker:prompt_")
			}
			if show {
				display.render(evt)
			}

		case <-ticker.C:
			pollCtx, pollCancel := orchContext()
			status, statusErr := client.GetJobStatus(pollCtx, &orchestratorSDK.GetJobStatusRequest{
				TenantID: globalTenantID,
				JobID:    jobID,
			})
			pollCancel()
			if statusErr != nil {
				return formatError(statusErr)
			}

			// Print state transitions.
			if status.State != lastState {
				display.stateChange(status.State, status.Iteration, status.TaskCount, status.Completed, status.Failed)
				lastState = status.State
			}

			// Exit on terminal states.
			if status.State == "completed" {
				fmt.Fprintf(os.Stdout, "Job %s completed successfully.\n", jobID)
				return nil
			}
			if status.State == "failed" {
				fmt.Fprintf(os.Stderr, "%s\n", NewJobFailedError("orchestrator.run", jobID, status.Error).Error())
				return NewJobFailedError("orchestrator.run", jobID, status.Error)
			}
		}
	}
}

// =============================================================================
// Job Command Group
// =============================================================================

// newOrchJobCommand creates the job command group.
func newOrchJobCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "job",
		Short: "Job management commands",
		Long: `Job management commands for checking status and cancelling orchestration jobs.

Examples:
  quicbaas orchestrator job status <jobID>
  quicbaas orchestrator job cancel <jobID>`,
	}

	cmd.AddCommand(newOrchJobStatusCommand())
	cmd.AddCommand(newOrchJobListCommand())
	cmd.AddCommand(newOrchJobEventsCommand())
	cmd.AddCommand(newOrchJobCancelCommand())

	return cmd
}

// =============================================================================
// Job List Command
// =============================================================================

// newOrchJobListCommand creates the job list subcommand.
func newOrchJobListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List orchestration jobs",
		Long: `List all orchestration jobs for the current tenant.

Examples:
  quicbaas orchestrator job list
  quicbaas orchestrator job list --output json
  quicbaas orchestrator job list --output yaml`,
		Args: cobra.NoArgs,
		RunE: runOrchJobList,
	}
}

// runOrchJobList executes the job list command.
func runOrchJobList(cmd *cobra.Command, args []string) error {
	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.ListJobs(ctx, &orchestratorSDK.ListJobsRequest{
		TenantID: globalTenantID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatOrchListJobsResponse(resp, orchOutputFmt, os.Stdout)
}

// formatOrchListJobsResponse formats a list jobs response for output.
func formatOrchListJobsResponse(resp *orchestratorSDK.ListJobsResponse, format string, w io.Writer) error {
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
		if len(resp.Jobs) == 0 {
			fmt.Fprintf(w, "No jobs found.\n")
			return nil
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "JOB ID\tSTATE\tITERATION\tTASKS\tCOMPLETED\tFAILED\tEVENTS\tDESCRIPTION\n")
		for _, j := range resp.Jobs {
			desc := j.Description
			if len(desc) > 40 {
				desc = desc[:37] + "..."
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\n",
				j.JobID, j.State, j.Iteration,
				j.TaskCount, j.Completed, j.Failed,
				j.EventCount, desc)
		}
		return tw.Flush()

	default:
		if len(resp.Jobs) == 0 {
			fmt.Fprintf(w, "No jobs found.\n")
			return nil
		}
		for _, j := range resp.Jobs {
			fmt.Fprintf(w, "Job:         %s\n", j.JobID)
			fmt.Fprintf(w, "State:       %s\n", j.State)
			fmt.Fprintf(w, "Iteration:   %d\n", j.Iteration)
			fmt.Fprintf(w, "Tasks:       %d\n", j.TaskCount)
			fmt.Fprintf(w, "Completed:   %d\n", j.Completed)
			fmt.Fprintf(w, "Failed:      %d\n", j.Failed)
			fmt.Fprintf(w, "Events:      %d\n", j.EventCount)
			fmt.Fprintf(w, "Description: %s\n", j.Description)
			if j.Error != "" {
				fmt.Fprintf(w, "Error:       %s\n", j.Error)
			}
			fmt.Fprintf(w, "Started:     %s\n", time.Unix(0, j.StartedAt).Format(time.RFC3339))
			fmt.Fprintf(w, "Updated:     %s\n", time.Unix(0, j.UpdatedAt).Format(time.RFC3339))
			fmt.Fprintln(w)
		}
		return nil
	}
}

// =============================================================================
// Job Status Command
// =============================================================================

// newOrchJobStatusCommand creates the job status subcommand.
func newOrchJobStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <jobID>",
		Short: "Get orchestration job status",
		Long: `Retrieve the current status of an orchestration job.

Examples:
  quicbaas orchestrator job status job_abc123
  quicbaas orchestrator job status job_abc123 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchJobStatus,
	}
}

// runOrchJobStatus executes the job status command.
func runOrchJobStatus(cmd *cobra.Command, args []string) error {
	jobID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	status, err := client.GetJobStatus(ctx, &orchestratorSDK.GetJobStatusRequest{
		TenantID: globalTenantID,
		JobID:    jobID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatOrchJobStatusResponse(status, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Job Events Command
// =============================================================================

// newOrchJobEventsCommand creates the job events subcommand.
func newOrchJobEventsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events <jobID>",
		Short: "View the complete audit trail for a job",
		Long: `Display the full audit trail for an orchestration job, including
agent prompts, chain-of-thought, tool calls, responses, and state transitions.

The audit trail captures EVERYTHING that happened during execution. Use
--verbosity to control how much detail is displayed:

  info    — state transitions and summaries only
  verbose — above + agent prompts and responses
  debug   — above + raw protocol messages and token usage

Examples:
  quicbaas orchestrator job events job_abc123
  quicbaas orchestrator job events job_abc123 --verbosity debug
  quicbaas orchestrator job events job_abc123 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchJobEvents,
	}
	cmd.Flags().StringVar(&orchVerbosity, "verbosity", "verbose",
		"Display verbosity (info, verbose, debug)")
	return cmd
}

// runOrchJobEvents displays the complete audit trail for a job.
func runOrchJobEvents(cmd *cobra.Command, args []string) error {
	jobID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	status, err := client.GetJobStatus(ctx, &orchestratorSDK.GetJobStatusRequest{
		TenantID: globalTenantID,
		JobID:    jobID,
	})
	if err != nil {
		return formatError(err)
	}

	if orchOutputFmt == "json" || orchOutputFmt == "yaml" {
		return formatOrchJobStatusResponse(status, orchOutputFmt, os.Stdout)
	}

	if len(status.Events) == 0 {
		fmt.Fprintf(os.Stdout, "No events recorded for job %s\n", jobID)
		return nil
	}

	fmt.Fprintf(os.Stdout, "Audit trail for job %s (%d events)\n\n",
		status.JobID, len(status.Events))

	display := newEventDisplay(os.Stdout)
	for _, evt := range status.Events {
		if !shouldDisplayAuditEvent(evt.Type, orchVerbosity) {
			continue
		}
		ts := time.Unix(0, evt.Timestamp).Format("15:04:05.000")
		display.renderAudit(ts, evt.Type, evt.Details)
	}

	return nil
}

// shouldDisplayAuditEvent filters audit events based on verbosity level.
func shouldDisplayAuditEvent(eventType, verbosity string) bool {
	switch verbosity {
	case "debug":
		return true
	case "verbose":
		return !strings.HasPrefix(eventType, "agent:") ||
			eventType == "agent:assistant" || eventType == "agent:result"
	default: // "info"
		return strings.HasPrefix(eventType, "orchestration:") ||
			strings.HasPrefix(eventType, "lead:")
	}
}

// =============================================================================
// Job Cancel Command
// =============================================================================

// newOrchJobCancelCommand creates the job cancel subcommand.
func newOrchJobCancelCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <jobID>",
		Short: "Cancel an orchestration job",
		Long: `Cancel a running orchestration job.

Examples:
  quicbaas orchestrator job cancel job_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchJobCancel,
	}
}

// runOrchJobCancel executes the job cancel command.
func runOrchJobCancel(cmd *cobra.Command, args []string) error {
	jobID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.CancelJob(ctx, &orchestratorSDK.CancelJobRequest{
		TenantID: globalTenantID,
		JobID:    jobID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatOrchCancelJobResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Job Output Formatters
// =============================================================================

func formatOrchJobStatusResponse(status *orchestratorSDK.JobStatus, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(status)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(status)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "JOB ID\tSTATE\tITERATION\tTASKS\tCOMPLETED\tFAILED\n")
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%d\n",
			status.JobID, status.State, status.Iteration,
			status.TaskCount, status.Completed, status.Failed)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Job:         %s\n", status.JobID)
		fmt.Fprintf(w, "State:       %s\n", status.State)
		fmt.Fprintf(w, "Iteration:   %d\n", status.Iteration)
		fmt.Fprintf(w, "Tasks:       %d\n", status.TaskCount)
		fmt.Fprintf(w, "Completed:   %d\n", status.Completed)
		fmt.Fprintf(w, "Failed:      %d\n", status.Failed)
		fmt.Fprintf(w, "Description: %s\n", status.Description)
		if status.Error != "" {
			fmt.Fprintf(w, "Error:       %s\n", status.Error)
		}
		fmt.Fprintf(w, "Events:      %d\n", len(status.Events))
		fmt.Fprintf(w, "Started:     %s\n", time.Unix(0, status.StartedAt).Format(time.RFC3339))
		fmt.Fprintf(w, "Updated:     %s\n", time.Unix(0, status.UpdatedAt).Format(time.RFC3339))
		if len(status.Events) > 0 {
			fmt.Fprintf(w, "\nUse 'quicbaas orchestrator job events %s' to view the full audit trail.\n", status.JobID)
		}
		return nil
	}
}

// formatOrchCancelJobResponse formats a cancel job response for output.
func formatOrchCancelJobResponse(resp *orchestratorSDK.CancelJobResponse, format string, w io.Writer) error {
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

	default:
		if resp.Cancelled {
			fmt.Fprintf(w, "Job %s cancelled.\n", resp.JobID)
		} else {
			fmt.Fprintf(w, "Job %s could not be cancelled.\n", resp.JobID)
		}
		return nil
	}
}

// =============================================================================
// Workflow Command Group
// =============================================================================

// newOrchWorkflowCommand creates the workflow command group.
func newOrchWorkflowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Workflow management commands",
		Long: `Workflow management commands for listing and inspecting available
orchestration workflow definitions.

Workflows are loaded from:
  - ./workflows/ (project local directory)

Examples:
  quicbaas orchestrator workflow list
  quicbaas orchestrator workflow show software-dev`,
	}

	cmd.AddCommand(newOrchWorkflowListCommand())
	cmd.AddCommand(newOrchWorkflowShowCommand())

	return cmd
}

// =============================================================================
// Workflow List Command
// =============================================================================

// newOrchWorkflowListCommand creates the workflow list subcommand.
func newOrchWorkflowListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available workflows",
		Long: `List all available orchestration workflow definitions.

Workflows are loaded from the local ./workflows/ directory.

Examples:
  quicbaas orchestrator workflow list
  quicbaas orchestrator workflow list --output json`,
		Args: cobra.NoArgs,
		RunE: runOrchWorkflowList,
	}
}

// runOrchWorkflowList executes the workflow list command.
func runOrchWorkflowList(cmd *cobra.Command, args []string) error {
	loader, err := loadLocalWorkflows()
	if err != nil {
		return formatError(err)
	}

	defs := loader.ListDefinitions()
	return formatWorkflowListResponse(defs, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Workflow Show Command
// =============================================================================

// newOrchWorkflowShowCommand creates the workflow show subcommand.
func newOrchWorkflowShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show a workflow definition",
		Long: `Display the full YAML definition of a workflow by name.

Examples:
  quicbaas orchestrator workflow show software-dev
  quicbaas orchestrator workflow show code-review --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchWorkflowShow,
	}
}

// runOrchWorkflowShow executes the workflow show command.
func runOrchWorkflowShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	loader, err := loadLocalWorkflows()
	if err != nil {
		return formatError(err)
	}

	def, err := loader.Get(name)
	if err != nil {
		return formatError(err)
	}

	return formatWorkflowShowResponse(def, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Workflow Loader Helper
// =============================================================================

// loadLocalWorkflows creates a WorkflowLoader and loads workflows from the
// local ./workflows/ directory. This is a client-side operation that does
// not require a server connection.
func loadLocalWorkflows() (*orchestration.WorkflowLoader, error) {
	loader := orchestration.NewWorkflowLoader()

	workflowDir := "workflows"
	if _, err := os.Stat(workflowDir); err == nil {
		if err := loader.LoadFromDir(workflowDir); err != nil {
			return nil, err
		}
	}

	return loader, nil
}

// =============================================================================
// Workflow Formatters
// =============================================================================

// formatWorkflowListResponse formats a list of workflow definitions for output.
func formatWorkflowListResponse(defs []*orchestration.WorkflowDefinition, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(defs)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(defs)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "NAME\tMODE\tAGENTS\tDESCRIPTION\n")
		for _, def := range defs {
			agentCount := len(def.Agents)
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
				def.Name, string(def.Mode), agentCount, def.Description)
		}
		fmt.Fprintf(tw, "\nTotal: %d workflows\n", len(defs))
		return tw.Flush()

	default:
		for _, def := range defs {
			fmt.Fprintf(w, "%s (%s) - %s\n", def.Name, string(def.Mode), def.Description)
		}
		return nil
	}
}

// formatWorkflowShowResponse formats a workflow definition for detailed output.
func formatWorkflowShowResponse(def *orchestration.WorkflowDefinition, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(def)

	default:
		// YAML is the natural representation for workflow definitions.
		return yaml.NewEncoder(w).Encode(def)
	}
}
