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
	agentsSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// Orchestrator Command Flags
// =============================================================================

var (
	orchOutputFmt    string
	orchBackend      string
	orchModel        string
	orchSystemPrmpt  string
	orchProjectID    string
	orchSandbox      string
	orchHostAffinity string

	// Agent references: the Agent or pool a spawned session runs as, the
	// role it plays, and the Agent or pool a job's lead runs as.
	orchSpawnAgent string
	orchSpawnPool  string
	orchSpawnRole  string
	orchJobLead    string

	// Run-time prompt material appended to what a spawn, a job or a task's
	// attempts run with.
	orchSpawnPromptAppend promptAppendFlags
	orchJobPromptAppend   promptAppendFlags
	orchTaskPromptAppend  promptAppendFlags

	// Task flags.
	orchTaskSubject     string
	orchTaskDescription string
	orchTaskTeamName    string
	orchTaskAssignedTo  string
	orchTaskStatus      string
	orchTaskAgentID     string
	orchTaskResult      string
	orchTaskProjectID   string
	orchTaskStatuses    []string
	orchTaskBlockKind   string
	orchTaskBlockReason string
	orchTaskAutoSpawn   bool
	orchTaskBackend     string
	orchTaskGpuRequired bool

	// Job flags.
	orchJobMaxIterations int
	orchJobPollInterval  string
	orchJobTaskID        string

	// List paging flags, one set per list command.
	orchSessionsPage pageFlags
	orchTaskListPage pageFlags
	orchJobListPage  pageFlags

	// Workflow flags.
	orchWorkflowName string
	orchWorkflowFile string
	orchJobRunner    string

	// Pooled provider account flags.
	orchAccountID       string
	orchAccountAuto     bool
	orchAccountStrategy string
	orchAccountAllow    []string

	// Rotation flags.
	orchRotateReason       string
	orchRotateHoldOutgoing bool

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
		Short: "Software Factory (agent orchestration) commands",
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
	cmd.AddCommand(newOrchRotateAccountCommand())
	cmd.AddCommand(newOrchSessionsCommand())
	cmd.AddCommand(newOrchAttachCommand())
	cmd.AddCommand(newOrchBackendsCommand())
	cmd.AddCommand(newOrchHealthCommand())
	cmd.AddCommand(newOrchSpendPolicyCommand())

	// Task command group.
	cmd.AddCommand(newOrchTaskCommand())

	// Decompose command.
	cmd.AddCommand(newOrchDecomposeCommand())

	// Job commands.
	cmd.AddCommand(newOrchRunCommand())
	cmd.AddCommand(newOrchJobCommand())

	// Workflow commands.
	cmd.AddCommand(newOrchWorkflowCommand())

	// Stateless inference command.
	cmd.AddCommand(newOrchInferenceCommand())
	cmd.AddCommand(newOrchCostSettingsCommand())
	cmd.AddCommand(newOrchCostCommand())

	return cmd
}

// orchContext creates a context with the global tenant ID and timeout.
func orchContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(orchTenantContext(), globalTimeout)
	return ctx, cancel
}

// orchTenantContext returns a tenant-stamped context with no deadline. Use it
// for open-ended streaming attaches, where the global request timeout would
// cut the stream off mid-turn; one-shot calls use orchContext instead.
func orchTenantContext() context.Context {
	return sdktransport.ContextWithTenant(context.Background(), globalTenantID)
}

// =============================================================================
// Spawn Command
// =============================================================================

// newOrchSpawnCommand creates the spawn subcommand.
func newOrchSpawnCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spawn",
		Short: "Spawn a new agent session",
		Long: `Create a persistent agent session as a tenant Agent, from a pool, or on a raw
backend. An Agent supplies the backend, model and image; a pool fails over
along its Agents when one's accounts are spent or its backend is unavailable.

Examples:
  quicbaas orchestrator spawn --agent claude-coder
  quicbaas orchestrator spawn --pool reviewers --role review
  quicbaas orchestrator spawn --agent claude-coder --prompt-append-file ./context.md
  quicbaas orchestrator spawn --backend ollama --model llama3
  quicbaas orchestrator spawn --backend claude --project proj_123`,
		RunE: runOrchSpawn,
	}

	cmd.Flags().StringVar(&orchSpawnAgent, "agent", "", "Tenant Agent the session runs as")
	cmd.Flags().StringVar(&orchSpawnPool, "pool", "", "Tenant pool the session runs from, failing over along its Agents")
	cmd.Flags().StringVar(&orchSpawnRole, "role", "", "The kind of work the session does (general, coding, review, triage, lead, project_manager)")
	cmd.Flags().StringVar(&orchBackend, "backend", "", "Agent backend name, when no --agent or --pool is named")
	cmd.Flags().StringVar(&orchModel, "model", "", "Model to use for the session")
	cmd.Flags().StringVar(&orchSystemPrmpt, "system-prompt", "", "System prompt for the session")
	cmd.Flags().StringVar(&orchProjectID, "project", "", "Project ID to associate with the session")
	cmd.Flags().StringVar(&orchSandbox, "sandbox", "default",
		"Sandbox selector: default (server policy), required (force container), none (force raw exec)")
	cmd.Flags().StringVar(&orchHostAffinity, "host-affinity", "",
		"Pin the session to a specific fleet node daemon by Name or Host (e.g. 'agent-01' or 'agent-01.example.com:9091'). Empty = any available daemon.")
	cmd.Flags().StringVar(&orchAccountID, "account-id", "",
		"Pin the session to a specific pooled provider account. A bare pin is honored: the session is never rotated off it.")
	cmd.Flags().BoolVar(&orchAccountAuto, "account-auto", false,
		"Select a pooled provider account with the configured strategy, and authorize rotation across the whole tenant/project/provider pool.")
	cmd.Flags().StringVar(&orchAccountStrategy, "account-strategy", "",
		"Account selection strategy: static | least_usage. Empty selects the registry default. Ignored unless --account-auto is set.")
	cmd.Flags().StringSliceVar(&orchAccountAllow, "account-allow", nil,
		"Restrict the session to these pooled account IDs and authorize it to rotate between them. Repeatable.")

	orchSpawnPromptAppend.register(cmd, "the session's resolved prompt (the Agent's own, else its role's built-in charter)")

	cmd.MarkFlagsOneRequired("agent", "pool", "backend")
	cmd.MarkFlagsMutuallyExclusive("agent", "pool", "backend")

	return cmd
}

// parseCLISandbox converts the --sandbox flag value into the SDK SandboxMode.
func parseCLISandbox(s string) (orchestratorSDK.SandboxMode, error) {
	switch s {
	case "", "default":
		return orchestratorSDK.SandboxModeDefault, nil
	case "required":
		return orchestratorSDK.SandboxModeRequired, nil
	case "none":
		return orchestratorSDK.SandboxModeNone, nil
	default:
		return orchestratorSDK.SandboxModeDefault,
			&MessageError{Message: fmt.Sprintf("invalid --sandbox value %q (want: default | required | none)", s)}
	}
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

	mode, parseErr := parseCLISandbox(orchSandbox)
	if parseErr != nil {
		return formatError(parseErr)
	}
	promptAppend, err := orchSpawnPromptAppend.value(cmd.InOrStdin())
	if err != nil {
		return formatError(err)
	}

	req := &orchestratorSDK.SpawnSessionRequest{
		AgentID:      orchSpawnAgent,
		PoolID:       orchSpawnPool,
		Role:         orchSpawnRole,
		Backend:      orchBackend,
		ProjectID:    orchProjectID,
		Model:        orchModel,
		SandboxMode:  mode,
		HostAffinity: orchHostAffinity,
		PromptAppend: promptAppend,

		// Pooled provider account selection. --account-auto and
		// --account-allow are the two things that authorize a later
		// `orchestrator rotate-account`; a bare --account-id is a deliberate
		// pin and is never moved.
		AccountID:       orchAccountID,
		AccountAuto:     orchAccountAuto,
		AccountStrategy: orchAccountStrategy,
		AccountAllow:    orchAccountAllow,
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

// promptFollow controls whether `orchestrator prompt` stays attached to the
// session event stream after the server accepts the turn.
var promptFollow bool

// newOrchPromptCommand creates the prompt subcommand.
func newOrchPromptCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt <sessionID> <text>",
		Short: "Send a prompt to an agent session",
		Long: `Send a prompt to an active agent session and stream the agent's response.

The server accepts the turn and dispatches it in the background — a real agent
turn runs for minutes, far longer than any request/response transport can hold
a connection open. By default the CLI then attaches to the session's event
stream and renders the turn as it happens, so the command still reads as a
synchronous conversation. The turn keeps running on the server even if the CLI
is interrupted; reattach with "quicbaas orchestrator attach".

Pass --follow=false to print the acceptance acknowledgement and exit
immediately, which is what scripts and CI jobs usually want.

Examples:
  quicbaas orchestrator prompt sess_abc123 "Write unit tests for the auth handler"
  quicbaas orchestrator prompt sess_abc123 "Refactor the database layer" --follow=false --output json`,
		Args: cobra.ExactArgs(2),
		RunE: runOrchPrompt,
	}
	cmd.Flags().BoolVar(&promptFollow, "follow", true,
		"Stream the turn's events until it completes; false prints the acknowledgement and exits")
	return cmd
}

// runOrchPrompt executes the prompt command.
func runOrchPrompt(_ *cobra.Command, args []string) error {
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

	// A stateless one-shot, or an explicit opt-out, has nothing to follow.
	if !resp.Accepted || !promptFollow {
		return formatOrchPromptResponse(resp, orchOutputFmt, os.Stdout)
	}

	return followPromptTurn(client, resp, orchOutputFmt, os.Stdout)
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
// Rotate Account Command
// =============================================================================

// newOrchRotateAccountCommand creates the rotate-account subcommand.
func newOrchRotateAccountCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate-account <sessionID>",
		Short: "Move a session onto a different pooled provider account",
		Long: `Move a running agent session onto a different pooled provider account.

The session keeps its identifier, transcript and cost history; the replacement
account opens a new attribution segment. QuicBaaS also rotates automatically
when a turn fails on a classified provider limit — this is the operator's door
to the same verb ("this seat is bad, move my session now").

Only a session spawned with --account-auto or --account-allow may rotate. A
bare --account-id pin is deliberate and is honored.

Examples:
  quicbaas orchestrator rotate-account sess_abc123
  quicbaas orchestrator rotate-account sess_abc123 --reason "seat retired" --hold-outgoing`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchRotateAccount,
	}

	cmd.Flags().StringVar(&orchRotateReason, "reason", "",
		"Reason recorded on the closed account segment and in the session transcript")
	cmd.Flags().BoolVar(&orchRotateHoldOutgoing, "hold-outgoing", false,
		"Hold the outgoing account out of the pool for the server's default backoff so the next selection does not pick it again")

	return cmd
}

// runOrchRotateAccount executes the rotate-account command.
func runOrchRotateAccount(cmd *cobra.Command, args []string) error {
	sessionID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.RotateSessionAccount(ctx, &orchestratorSDK.RotateSessionAccountRequest{
		SessionID:    sessionID,
		Reason:       orchRotateReason,
		HoldOutgoing: orchRotateHoldOutgoing,
	})
	if err != nil {
		return formatError(err)
	}

	return formatOrchRotateAccountResponse(resp, orchOutputFmt, os.Stdout)
}

// =============================================================================
// Sessions Command
// =============================================================================

// newOrchSessionsCommand creates the sessions subcommand.
func newOrchSessionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List active agent sessions",
		Long: `List active agent sessions across backends, one page at a time.

Examples:
  quicbaas orchestrator sessions
  quicbaas orchestrator sessions --limit 50 --offset 50
  quicbaas orchestrator sessions --search claude --sort last_active:desc
  quicbaas orchestrator sessions --all --output json`,
		Args: cobra.NoArgs,
		RunE: runOrchSessions,
	}
	orchSessionsPage.bind(cmd)
	return cmd
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

	sessions, env, err := fetchPages(&orchSessionsPage, func(params page.Params) ([]*orchestratorSDK.SessionStatusResponse, page.Envelope, error) {
		resp, err := client.ListSessions(ctx, &orchestratorSDK.ListSessionsRequest{Params: params})
		if err != nil || resp == nil {
			return nil, page.Envelope{}, err
		}
		return resp.Sessions, resp.Envelope, nil
	})
	if err != nil {
		return formatError(err)
	}

	resp := &orchestratorSDK.ListSessionsResponse{Sessions: sessions, Count: len(sessions), Envelope: env}
	return formatOrchSessionsResponse(resp, orchOutputFmt, cmd.OutOrStdout())
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
	cmd.AddCommand(newOrchTaskBlockCommand())
	cmd.AddCommand(newOrchTaskReadmitCommand())
	cmd.AddCommand(newOrchTaskDeleteCommand())

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
	cmd.Flags().BoolVar(&orchTaskAutoSpawn, "auto-spawn", false, "Dispatch a one-shot job immediately on the best-matching agent")
	cmd.Flags().StringVar(&orchTaskBackend, "backend", "", "Restrict auto-spawn agent selection to this backend (e.g. ollama, acp)")
	cmd.Flags().BoolVar(&orchTaskGpuRequired, "gpu", false, "Restrict auto-spawn to GPU-capable agents")
	orchTaskPromptAppend.register(cmd, "the prompt of every session an attempt at the task spawns")

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

	promptAppend, err := orchTaskPromptAppend.value(cmd.InOrStdin())
	if err != nil {
		return formatError(err)
	}
	req := &orchestratorSDK.CreateTaskRequest{
		TenantID:     globalTenantID,
		Subject:      orchTaskSubject,
		Description:  orchTaskDescription,
		TeamName:     orchTaskTeamName,
		AssignedTo:   orchTaskAssignedTo,
		AutoSpawn:    orchTaskAutoSpawn,
		Backend:      orchTaskBackend,
		GpuRequired:  orchTaskGpuRequired,
		PromptAppend: promptAppend,
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
  quicbaas orchestrator task list --team backend --output json
  quicbaas orchestrator task list --search login --sort created_at:desc --limit 50
  quicbaas orchestrator task list --all`,
		Args: cobra.NoArgs,
		RunE: runOrchTaskList,
	}
	orchTaskListPage.bind(cmd)

	cmd.Flags().StringVar(&orchTaskStatus, "status", "", "Filter by task status (pending, claimed, in_progress, completed, failed, cancelled)")
	cmd.Flags().StringVar(&orchTaskProjectID, "project", "", "Filter by project ID")
	cmd.Flags().StringSliceVar(&orchTaskStatuses, "statuses", nil, "Filter by any of these statuses (comma-separated, e.g. blocked,pending)")
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

	tasks, env, err := fetchPages(&orchTaskListPage, func(params page.Params) ([]*orchestratorSDK.Task, page.Envelope, error) {
		resp, err := client.ListTasks(ctx, &orchestratorSDK.ListTasksRequest{
			TenantID:   globalTenantID,
			Status:     orchTaskStatus,
			TeamName:   orchTaskTeamName,
			AssignedTo: orchTaskAssignedTo,
			ProjectID:  orchTaskProjectID,
			Statuses:   orchTaskStatuses,
			Params:     params,
		})
		if err != nil || resp == nil {
			return nil, page.Envelope{}, err
		}
		return resp.Tasks, resp.Envelope, nil
	})
	if err != nil {
		return formatError(err)
	}

	resp := &orchestratorSDK.ListTasksResponse{Tasks: tasks, Count: len(tasks), Envelope: env}
	return formatOrchTaskListResponse(resp, orchOutputFmt, cmd.OutOrStdout())
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
// Task Block / Readmit Commands
// =============================================================================

// newOrchTaskBlockCommand creates the task block subcommand.
func newOrchTaskBlockCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "block <taskID>",
		Short: "Park a task until a person readmits it",
		Long: `Park a pending, claimed or in-progress task. The kind says why
(needs_info, blocked, escalated) and the reason is surfaced to whoever the
task waits for. A blocked task is not dispatched until it is readmitted.

Examples:
  quicbaas orchestrator task block task_abc123 --kind needs_info --reason "which branch?"
  quicbaas orchestrator task block task_abc123 --kind escalated --reason "attempt budget exhausted"`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchTaskBlock,
	}

	cmd.Flags().StringVar(&orchTaskBlockKind, "kind", "", "Why the task is parked: needs_info, blocked or escalated (required)")
	cmd.Flags().StringVar(&orchTaskBlockReason, "reason", "", "Human-readable reason (required)")

	for _, flag := range []string{"kind", "reason"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to mark %s flag as required: %v\n", flag, err)
		}
	}

	return cmd
}

// runOrchTaskBlock executes the task block command.
func runOrchTaskBlock(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.BlockTask(ctx, &orchestratorSDK.BlockTaskRequest{
		TenantID: globalTenantID,
		TaskID:   taskID,
		Kind:     orchTaskBlockKind,
		Reason:   orchTaskBlockReason,
	})
	if err != nil {
		return formatError(err)
	}

	return formatOrchBlockResponse(resp, orchOutputFmt, os.Stdout)
}

// newOrchTaskReadmitCommand creates the task readmit subcommand.
func newOrchTaskReadmitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "readmit <taskID>",
		Short: "Return a blocked task to the queue",
		Long: `Return a blocked task to pending, clearing its block, assignment and
retry hold so it can be claimed again.

Examples:
  quicbaas orchestrator task readmit task_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchTaskReadmit,
	}
}

// runOrchTaskReadmit executes the task readmit command.
func runOrchTaskReadmit(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.ReadmitTask(ctx, &orchestratorSDK.ReadmitTaskRequest{
		TenantID: globalTenantID,
		TaskID:   taskID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatOrchReadmitResponse(resp, orchOutputFmt, os.Stdout)
}

// formatOrchBlockResponse renders a BlockTaskResponse.
func formatOrchBlockResponse(resp *orchestratorSDK.BlockTaskResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Kind:\t%s\n", resp.Kind)
		fmt.Fprintf(tw, "Blocked:\t%v\n", resp.Blocked)
		return tw.Flush()

	default:
		if resp.Blocked {
			fmt.Fprintf(w, "Task %s blocked (%s)\n", resp.TaskID, resp.Kind)
		} else {
			fmt.Fprintf(w, "Failed to block task %s\n", resp.TaskID)
		}
		return nil
	}
}

// formatOrchReadmitResponse renders a ReadmitTaskResponse.
func formatOrchReadmitResponse(resp *orchestratorSDK.ReadmitTaskResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Readmitted:\t%v\n", resp.Readmitted)
		return tw.Flush()

	default:
		if resp.Readmitted {
			fmt.Fprintf(w, "Task %s readmitted\n", resp.TaskID)
		} else {
			fmt.Fprintf(w, "Failed to readmit task %s\n", resp.TaskID)
		}
		return nil
	}
}

// =============================================================================
// Task Delete Command
// =============================================================================

// newOrchTaskDeleteCommand creates the task delete subcommand.
func newOrchTaskDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <taskID>",
		Short: "Delete a task and release its source reservation",
		Long: `Delete a task, its artifacts, its claim mark and its source reservation,
so the issue or ticket the task was adopted from can be adopted again.

A task a job currently holds a lease on is refused: finish or cancel the job
first.

Examples:
  quicbaas orchestrator task delete task_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchTaskDelete,
	}
}

// runOrchTaskDelete executes the task delete command.
func runOrchTaskDelete(cmd *cobra.Command, args []string) error {
	taskID := args[0]

	client, err := orchestratorClientFactory()
	if err != nil {
		return formatError(err)
	}
	defer client.Close()

	ctx, cancel := orchContext()
	defer cancel()

	resp, err := client.DeleteTask(ctx, &orchestratorSDK.DeleteTaskRequest{
		TenantID: globalTenantID,
		TaskID:   taskID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatOrchDeleteResponse(resp, orchOutputFmt, os.Stdout)
}

// formatOrchDeleteResponse renders a DeleteTaskResponse.
func formatOrchDeleteResponse(resp *orchestratorSDK.DeleteTaskResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Deleted:\t%v\n", resp.Deleted)
		return tw.Flush()

	default:
		if resp.Deleted {
			fmt.Fprintf(w, "Task %s deleted\n", resp.TaskID)
		} else {
			fmt.Fprintf(w, "Failed to delete task %s\n", resp.TaskID)
		}
		return nil
	}
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
		if resp.Accepted {
			// The turn was dispatched, not finished. Report what the operator
			// needs to find it again rather than "Done: false", which reads
			// like a failure.
			fmt.Fprintf(tw, "Turn ID:\t%s\n", resp.TurnID)
			fmt.Fprintf(tw, "Status:\taccepted\n")
			fmt.Fprintf(tw, "\n")
			if err := tw.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(w, "Agent is working. Follow it with:\n  quicbaas orchestrator attach %s --from %d\n",
				resp.SessionID, resp.Sequence)
			return nil
		}
		fmt.Fprintf(tw, "Done:\t%v\n", resp.Done)
		// Surface why the agent stopped whenever it is not the ordinary
		// end of a turn, so a truncated answer is not mistaken for a
		// complete one.
		if resp.StopReason != "" && resp.StopReason != orchestration.StopReasonEndTurn {
			fmt.Fprintf(tw, "Stop Reason:\t%s\n", resp.StopReason)
		}
		fmt.Fprintf(tw, "\n")
		if err := tw.Flush(); err != nil {
			return err
		}
		if resp.Response != "" {
			fmt.Fprintf(w, "%s\n", resp.Response)
		}
		return nil

	default:
		if resp.Accepted {
			fmt.Fprintf(w, "Agent is working. Follow it with:\n  quicbaas orchestrator attach %s --from %d\n",
				resp.SessionID, resp.Sequence)
			return nil
		}
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

// formatOrchRotateAccountResponse formats a rotation result for output.
func formatOrchRotateAccountResponse(resp *orchestratorSDK.RotateSessionAccountResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Previous Account:\t%s\n", resp.PreviousAccountID)
		fmt.Fprintf(tw, "Account:\t%s\n", resp.AccountID)
		fmt.Fprintf(tw, "Strategy:\t%s\n", resp.Strategy)
		fmt.Fprintf(tw, "Mode:\t%s\n", resp.Mode)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Session %s moved from account %s to %s (%s)\n",
			resp.SessionID, resp.PreviousAccountID, resp.AccountID, resp.Mode)
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
		if err := tw.Flush(); err != nil {
			return err
		}
		writePageFooter(w, len(resp.Sessions), resp.Envelope)
		return nil

	default:
		for _, s := range resp.Sessions {
			fmt.Fprintf(w, "%s: %s (%s)\n", s.SessionID, s.Backend, s.Status)
		}
		writePageFooter(w, len(resp.Sessions), resp.Envelope)
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
		if task.AutoSpawn {
			fmt.Fprintf(tw, "Auto-Spawn:\t%v\n", task.AutoSpawn)
		}
		if task.SpawnedJobID != "" {
			fmt.Fprintf(tw, "Spawned Job:\t%s\n", task.SpawnedJobID)
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
		if task.SpawnedAt != 0 {
			fmt.Fprintf(tw, "Spawned:\t%s\n", time.Unix(0, task.SpawnedAt).Format(time.RFC3339))
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
		if err := tw.Flush(); err != nil {
			return err
		}
		writePageFooter(w, len(resp.Tasks), resp.Envelope)
		return nil

	default:
		for _, t := range resp.Tasks {
			fmt.Fprintf(w, "%s: %s (%s)\n", t.ID, t.Subject, t.Status)
		}
		writePageFooter(w, len(resp.Tasks), resp.Envelope)
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
  quicbaas orchestrator run "Implement user authentication" --lead claude-lead
  quicbaas orchestrator run "Add caching layer" --lead pool:leads
  quicbaas orchestrator run "Fix the flaky test" --lead claude-lead --prompt-append "Touch only pkg/cache"
  quicbaas orchestrator run "Build the REST API" --project proj_123 --max-iterations 5
  quicbaas orchestrator run "Build a game" --workflow software-dev
  quicbaas orchestrator run "Custom task" --workflow-file ./my-workflow.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runOrchRun,
	}

	cmd.Flags().StringVar(&orchJobLead, "lead", "", "The Agent or pool the lead runs as: <agent-id>, agent:<id> or pool:<id> (default: the project's default lead)")
	cmd.Flags().StringVar(&orchProjectID, "project", "", "Project ID to scope the job")
	cmd.Flags().IntVar(&orchJobMaxIterations, "max-iterations", 10, "Maximum re-delegation iterations")
	cmd.Flags().StringVar(&orchJobPollInterval, "poll-interval", "5s", "Interval between status polls")
	cmd.Flags().StringVar(&orchWorkflowName, "workflow", "", "Use a canned workflow by name (e.g. software-dev)")
	cmd.Flags().StringVar(&orchWorkflowFile, "workflow-file", "", "Path to a custom workflow YAML file")
	cmd.Flags().StringVar(&orchJobRunner, "runner", "", "Job runner driving the job's states (default: workflow)")
	cmd.Flags().StringVar(&orchVerbosity, "verbosity", "info", "Display verbosity (info, verbose, debug)")
	orchJobPromptAppend.register(cmd, "the prompt of every session the job spawns, the lead's included")

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

	lead, err := agentsSDK.ParseAgentRef(orchJobLead)
	if err != nil {
		return formatError(err)
	}
	promptAppend, err := orchJobPromptAppend.value(cmd.InOrStdin())
	if err != nil {
		return formatError(err)
	}
	req := &orchestratorSDK.StartJobRequest{
		TenantID:      globalTenantID,
		Description:   description,
		Lead:          lead,
		ProjectID:     orchProjectID,
		MaxIterations: orchJobMaxIterations,
		PollInterval:  orchJobPollInterval,
		WorkflowName:  orchWorkflowName,
		WorkflowFile:  orchWorkflowFile,
		Runner:        orchJobRunner,
		PromptAppend:  promptAppend,
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
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List orchestration jobs",
		Long: `List the current tenant's orchestration jobs, newest first.

With --task, the listing is narrowed to the attempts at one task: every job
that ran it, including the ones that have already concluded. That is the
task's attempt history.

Examples:
  quicbaas orchestrator job list
  quicbaas orchestrator job list --task 8bf2fb5f30e2f0c87223ed219d1418c7
  quicbaas orchestrator job list --output json
  quicbaas orchestrator job list --output yaml
  quicbaas orchestrator job list --search deploy --sort started_at --limit 50
  quicbaas orchestrator job list --all`,
		Args: cobra.NoArgs,
		RunE: runOrchJobList,
	}
	cmd.Flags().StringVar(&orchJobTaskID, "task", "", "Narrow the listing to the attempts at one task")
	orchJobListPage.bind(cmd)
	return cmd
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

	jobs, env, err := fetchPages(&orchJobListPage, func(params page.Params) ([]orchestratorSDK.JobSummary, page.Envelope, error) {
		resp, err := client.ListJobs(ctx, &orchestratorSDK.ListJobsRequest{
			TenantID: globalTenantID,
			TaskID:   orchJobTaskID,
			Params:   params,
		})
		if err != nil || resp == nil {
			return nil, page.Envelope{}, err
		}
		return resp.Jobs, resp.Envelope, nil
	})
	if err != nil {
		return formatError(err)
	}

	resp := &orchestratorSDK.ListJobsResponse{Jobs: jobs, Envelope: env}
	return formatOrchListJobsResponse(resp, orchOutputFmt, cmd.OutOrStdout())
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
			writePageFooter(w, 0, resp.Envelope)
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
		if err := tw.Flush(); err != nil {
			return err
		}
		writePageFooter(w, len(resp.Jobs), resp.Envelope)
		return nil

	default:
		if len(resp.Jobs) == 0 {
			fmt.Fprintf(w, "No jobs found.\n")
			writePageFooter(w, 0, resp.Envelope)
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
		writePageFooter(w, len(resp.Jobs), resp.Envelope)
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
		if status.Stage != "" {
			fmt.Fprintf(w, "Stage:       %s\n", status.Stage)
		}
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

	defs := loader.ListDefinitions(orchestration.SystemTenantID)
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

	def, err := loader.Get(orchestration.SystemTenantID, name)
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
		if err := loader.LoadFromDir(orchestration.SystemTenantID, workflowDir); err != nil {
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
