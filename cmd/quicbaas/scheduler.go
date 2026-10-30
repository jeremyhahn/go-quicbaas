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

// Package main provides scheduler CLI commands for QuicBaaS.
package main

import (
	stdcontext "context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// The scheduler's job commands. Like the placement commands they go through
// the SDK's Async().Scheduler() (sdkScheduler) on the client the global
// --transport selects, so one SDK client serves every transport.

var (
	schedulerDescription string
	schedulerType        string
	schedulerSchedule    string
	schedulerHandler     string
	schedulerPayload     string
	schedulerPriority    int
	schedulerTimeout     string
	schedulerMaxRetries  int
	schedulerDelay       string
	schedulerExecuteAt   string
	schedulerLimit       int
	schedulerStatus      string
	schedulerOutputFmt   string
	schedulerForce       bool
	schedulerReason      string
	schedulerCursor      string
	schedulerScope       string
)

// schedulerJobScopes maps the --scope flag values to SDK owner types. The
// values are the ones the REST scope query parameter accepts.
var schedulerJobScopes = map[string]scheduler.OwnerType{
	"tenant":    scheduler.OwnerTypeTenant,
	"delegated": scheduler.OwnerTypeDelegated,
	"system":    scheduler.OwnerTypeSystem,
}

// addSchedulerScopeFlag adds the --scope flag to a by-ID job command.
func addSchedulerScopeFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&schedulerScope, "scope", "",
		"Scope the job is listed under (tenant, delegated, system); default is the tenant's own jobs")
}

// parseSchedulerScope returns the scope named by --scope, or nil when the
// flag is unset so the request keeps the tenant scope.
func parseSchedulerScope(op string) (*scheduler.OwnerType, error) {
	if schedulerScope == "" {
		return nil, nil
	}
	scope, ok := schedulerJobScopes[schedulerScope]
	if !ok {
		return nil, errors.Newf(errors.ErrInvalidInput, op,
			"invalid job scope %q (tenant, delegated, system)", schedulerScope)
	}
	return &scope, nil
}

// parseSchedulerJobOptions returns the by-ID job options for --scope: none
// when the flag is unset, otherwise WithJobScope.
func parseSchedulerJobOptions(op string) ([]scheduler.JobOption, error) {
	scope, err := parseSchedulerScope(op)
	if err != nil || scope == nil {
		return nil, err
	}
	return []scheduler.JobOption{scheduler.WithJobScope(*scope)}, nil
}

// schedulerJobTypes maps the --type flag values to SDK job types.
var schedulerJobTypes = map[string]scheduler.JobType{
	"one-shot":  scheduler.JobTypeOneShot,
	"recurring": scheduler.JobTypeRecurring,
	"delayed":   scheduler.JobTypeDelayed,
}

// schedulerJobStatuses maps the --status flag values to SDK job statuses.
var schedulerJobStatuses = map[string]scheduler.JobStatus{
	"pending":   scheduler.JobStatusPending,
	"running":   scheduler.JobStatusRunning,
	"completed": scheduler.JobStatusCompleted,
	"failed":    scheduler.JobStatusFailed,
	"paused":    scheduler.JobStatusPaused,
	"canceled":  scheduler.JobStatusCanceled,
}

// schedulerName renders an SDK scheduler enum the way the CLI flags spell
// it: ONE_SHOT becomes one-shot, PENDING becomes pending.
func schedulerName(v fmt.Stringer) string {
	return strings.ToLower(strings.ReplaceAll(v.String(), "_", "-"))
}

// runSchedulerCall resolves the SDK scheduler client and runs call under
// the global timeout.
func runSchedulerCall(call func(stdcontext.Context, scheduler.Client) error) error {
	client, err := sdkScheduler()
	if err != nil {
		return err
	}
	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), globalTimeout)
	defer cancel()
	return call(ctx, client)
}

// parseSchedulerJobID parses a job ID argument for the command op.
func parseSchedulerJobID(op, arg string) (uint64, error) {
	jobID, err := strconv.ParseUint(arg, 10, 64)
	if err != nil {
		return 0, errors.Wrapf(err, errors.ErrInvalidInput, op, "invalid job ID format")
	}
	return jobID, nil
}

// newSchedulerCommand creates the scheduler command group.
func newSchedulerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scheduler",
		Short: "Job scheduler and workload placement commands",
		Long: `Scheduler commands for managing scheduled jobs and placing workloads.

The job commands create, manage, and monitor scheduled jobs for system
maintenance, tenant operations, and background processing. The node and
placement commands register node facts and decide which node a workload
runs on.`,
	}

	cmd.PersistentFlags().StringVar(&schedulerOutputFmt, "output", "table", "Output format (json, yaml, table, raw)")

	cmd.AddCommand(newSchedulerJobCommand())
	cmd.AddCommand(newSchedulerStatusCommand())
	// Workload placement: node facts and placement decisions
	cmd.AddCommand(newSchedulerNodeCommand())
	cmd.AddCommand(newSchedulerPlacementCommand())

	return cmd
}

// newSchedulerJobCommand creates the scheduler job command group.
func newSchedulerJobCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "job",
		Short: "Job management commands",
		Long: `Job management commands for creating, updating, and monitoring scheduled jobs.

Examples:
  quicbaas scheduler job create backup --type recurring --schedule "0 0 * * *" --handler backup.full
  quicbaas scheduler job list
  quicbaas scheduler job get 123`,
	}

	cmd.AddCommand(newSchedulerCreateCommand())
	cmd.AddCommand(newSchedulerListCommand())
	cmd.AddCommand(newSchedulerGetCommand())
	cmd.AddCommand(newSchedulerDeleteCommand())
	cmd.AddCommand(newSchedulerTriggerCommand())
	cmd.AddCommand(newSchedulerPauseCommand())
	cmd.AddCommand(newSchedulerResumeCommand())
	cmd.AddCommand(newSchedulerCancelCommand())
	cmd.AddCommand(newSchedulerHistoryCommand())

	return cmd
}

// =============================================================================
// Scheduler Create Command
// =============================================================================

// newSchedulerCreateCommand creates the scheduler create command.
func newSchedulerCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new scheduled job",
		Long: `Create a new scheduled job with the specified parameters.

Examples:
  quicbaas scheduler job create backup --type recurring --schedule "0 0 * * *" --handler backup.full
  quicbaas scheduler job create grace-disable --type one-shot --execute-at "2025-01-20T00:00:00Z" --handler kms.grace_period
  quicbaas scheduler job create cleanup --type delayed --delay 24h --handler audit.cleanup`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerCreateCommand,
	}

	cmd.Flags().StringVar(&schedulerType, "type", "one-shot", "Job type (one-shot, recurring, delayed)")
	cmd.Flags().StringVar(&schedulerDescription, "description", "", "Job description")
	cmd.Flags().StringVar(&schedulerSchedule, "schedule", "", "Cron schedule for recurring jobs")
	cmd.Flags().StringVar(&schedulerExecuteAt, "execute-at", "", "Execution time for one-shot jobs (RFC3339)")
	cmd.Flags().StringVar(&schedulerDelay, "delay", "", "Delay duration for delayed jobs")
	cmd.Flags().StringVar(&schedulerHandler, "handler", "", "Job handler name (required)")
	cmd.Flags().StringVar(&schedulerPayload, "payload", "", "Job payload (JSON)")
	cmd.Flags().IntVar(&schedulerPriority, "priority", 0, "Job priority")
	cmd.Flags().StringVar(&schedulerTimeout, "timeout", "5m", "Job timeout")
	cmd.Flags().IntVar(&schedulerMaxRetries, "max-retries", 3, "Maximum retry attempts")

	if err := cmd.MarkFlagRequired("handler"); err != nil {
		// Log but don't fail - flag marking is a best-effort operation
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to mark handler flag as required: %v\n", err)
	}

	return cmd
}

// buildSchedulerCreateRequest builds the SDK create request for job name
// from the create flags.
func buildSchedulerCreateRequest(name string) (*scheduler.CreateJobRequest, error) {
	jobType, ok := schedulerJobTypes[schedulerType]
	if !ok {
		return nil, errors.Newf(errors.ErrInvalidInput, "scheduler.create",
			"invalid job type %q (one-shot, recurring, delayed)", schedulerType)
	}

	jobTimeout, err := time.ParseDuration(schedulerTimeout)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.create", "invalid timeout format")
	}

	req := &scheduler.CreateJobRequest{
		Name:        name,
		Description: schedulerDescription,
		JobType:     jobType,
		Handler:     schedulerHandler,
		Schedule:    schedulerSchedule,
		Priority:    schedulerPriority,
		Timeout:     jobTimeout,
		MaxRetries:  schedulerMaxRetries,
		OwnerType:   scheduler.OwnerTypeSystem,
	}
	if schedulerExecuteAt != "" {
		req.ExecuteAt, err = time.Parse(time.RFC3339, schedulerExecuteAt)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.create", "invalid execute-at format, use RFC3339")
		}
	}
	if schedulerDelay != "" {
		req.Delay, err = time.ParseDuration(schedulerDelay)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.create", "invalid delay format")
		}
	}
	if schedulerPayload != "" {
		req.Payload = []byte(schedulerPayload)
	}
	if globalTenantID != "" {
		req.OwnerType = scheduler.OwnerTypeTenant
		req.OwnerID = globalTenantID
	}
	return req, nil
}

// runSchedulerCreateCommand executes the scheduler create command.
func runSchedulerCreateCommand(cmd *cobra.Command, args []string) error {
	req, err := buildSchedulerCreateRequest(args[0])
	if err != nil {
		return err
	}
	return runSchedulerCall(func(ctx stdcontext.Context, client scheduler.Client) error {
		resp, err := client.CreateJob(ctx, req)
		if err != nil {
			return formatError(err)
		}
		return formatSchedulerJobCreated(cmd.OutOrStdout(), req.Name, resp)
	})
}

// =============================================================================
// Scheduler List Command
// =============================================================================

// newSchedulerListCommand creates the scheduler list command.
func newSchedulerListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List scheduled jobs",
		Long: `List all scheduled jobs with optional filtering.

Examples:
  quicbaas scheduler job list
  quicbaas scheduler job list --status pending
  quicbaas scheduler job list --handler backup.full --limit 50`,
		Args: cobra.NoArgs,
		RunE: runSchedulerListCommand,
	}

	cmd.Flags().StringVar(&schedulerStatus, "status", "", "Filter by job status (pending, running, completed, failed, paused, canceled)")
	cmd.Flags().StringVar(&schedulerHandler, "handler", "", "Filter by handler name")
	cmd.Flags().IntVar(&schedulerLimit, "limit", 100, "Maximum number of jobs to return")
	cmd.Flags().StringVar(&schedulerCursor, "cursor", "", "Pagination cursor")

	return cmd
}

// buildSchedulerListOptions builds the SDK list options from the list flags.
func buildSchedulerListOptions() (*scheduler.ListJobsOptions, error) {
	opts := &scheduler.ListJobsOptions{
		Limit:   schedulerLimit,
		Cursor:  schedulerCursor,
		Handler: schedulerHandler,
	}
	if schedulerStatus != "" {
		status, ok := schedulerJobStatuses[schedulerStatus]
		if !ok {
			return nil, errors.Newf(errors.ErrInvalidInput, "scheduler.list",
				"invalid job status %q (pending, running, completed, failed, paused, canceled)", schedulerStatus)
		}
		opts.Status = &status
	}
	if globalTenantID != "" {
		owner := scheduler.OwnerTypeTenant
		opts.OwnerType = &owner
		opts.OwnerID = globalTenantID
	}
	return opts, nil
}

// runSchedulerListCommand executes the scheduler list command.
func runSchedulerListCommand(cmd *cobra.Command, args []string) error {
	opts, err := buildSchedulerListOptions()
	if err != nil {
		return err
	}
	return runSchedulerCall(func(ctx stdcontext.Context, client scheduler.Client) error {
		resp, err := client.ListJobs(ctx, opts)
		if err != nil {
			return formatError(err)
		}
		return formatSchedulerJobList(cmd.OutOrStdout(), resp)
	})
}

// =============================================================================
// Scheduler Get Command
// =============================================================================

// newSchedulerGetCommand creates the scheduler get command.
func newSchedulerGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <job-id>",
		Short: "Get job details",
		Long: `Retrieve details for a specific scheduled job.

Examples:
  quicbaas scheduler job get 123
  quicbaas scheduler job get 123 --tenant acme`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerGetCommand,
	}
	addSchedulerScopeFlag(cmd)

	return cmd
}

// runSchedulerGetCommand executes the scheduler get command.
func runSchedulerGetCommand(cmd *cobra.Command, args []string) error {
	jobID, err := parseSchedulerJobID("scheduler.get", args[0])
	if err != nil {
		return err
	}
	opts, err := parseSchedulerJobOptions("scheduler.get")
	if err != nil {
		return err
	}
	return runSchedulerCall(func(ctx stdcontext.Context, client scheduler.Client) error {
		job, err := client.GetJob(ctx, jobID, opts...)
		if err != nil {
			return formatError(err)
		}
		return formatSchedulerJob(cmd.OutOrStdout(), job)
	})
}

// =============================================================================
// Scheduler Delete Command
// =============================================================================

// newSchedulerDeleteCommand creates the scheduler delete command.
func newSchedulerDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <job-id>",
		Short: "Delete a scheduled job",
		Long: `Delete a scheduled job.

Examples:
  quicbaas scheduler job delete 123
  quicbaas scheduler job delete 123 --force`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerDeleteCommand,
	}
	addSchedulerScopeFlag(cmd)

	cmd.Flags().BoolVar(&schedulerForce, "force", false, "Force deletion even if job is running")

	return cmd
}

// runSchedulerDeleteCommand executes the scheduler delete command.
func runSchedulerDeleteCommand(cmd *cobra.Command, args []string) error {
	jobID, err := parseSchedulerJobID("scheduler.delete", args[0])
	if err != nil {
		return err
	}
	scope, err := parseSchedulerScope("scheduler.delete")
	if err != nil {
		return err
	}
	return runSchedulerCall(func(ctx stdcontext.Context, client scheduler.Client) error {
		resp, err := client.DeleteJobWithOptions(ctx, &scheduler.DeleteJobRequest{
			JobID:    jobID,
			TenantID: globalTenantID,
			Scope:    scope,
			Force:    schedulerForce,
		})
		if err != nil {
			return formatError(err)
		}
		return formatSchedulerJobDeleted(cmd.OutOrStdout(), resp)
	})
}

// =============================================================================
// Scheduler Trigger Command
// =============================================================================

// newSchedulerTriggerCommand creates the scheduler trigger command.
func newSchedulerTriggerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trigger <job-id>",
		Short: "Trigger immediate job execution",
		Long: `Trigger immediate execution of a scheduled job.

Examples:
  quicbaas scheduler job trigger 123`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerTriggerCommand,
	}
	addSchedulerScopeFlag(cmd)

	return cmd
}

// runSchedulerTriggerCommand executes the scheduler trigger command.
func runSchedulerTriggerCommand(cmd *cobra.Command, args []string) error {
	jobID, err := parseSchedulerJobID("scheduler.trigger", args[0])
	if err != nil {
		return err
	}
	opts, err := parseSchedulerJobOptions("scheduler.trigger")
	if err != nil {
		return err
	}
	return runSchedulerCall(func(ctx stdcontext.Context, client scheduler.Client) error {
		resp, err := client.TriggerJob(ctx, jobID, opts...)
		if err != nil {
			return formatError(err)
		}
		return formatSchedulerJobTriggered(cmd.OutOrStdout(), resp)
	})
}

// =============================================================================
// Scheduler Pause Command
// =============================================================================

// newSchedulerPauseCommand creates the scheduler pause command.
func newSchedulerPauseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pause <job-id>",
		Short: "Pause a scheduled job",
		Long: `Pause a scheduled job to prevent future executions.

Examples:
  quicbaas scheduler job pause 123`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerPauseCommand,
	}
	addSchedulerScopeFlag(cmd)

	return cmd
}

// runSchedulerPauseCommand executes the scheduler pause command.
func runSchedulerPauseCommand(cmd *cobra.Command, args []string) error {
	jobID, err := parseSchedulerJobID("scheduler.pause", args[0])
	if err != nil {
		return err
	}
	opts, err := parseSchedulerJobOptions("scheduler.pause")
	if err != nil {
		return err
	}
	return runSchedulerCall(func(ctx stdcontext.Context, client scheduler.Client) error {
		resp, err := client.PauseJob(ctx, jobID, opts...)
		if err != nil {
			return formatError(err)
		}
		return formatSchedulerJobStatusChange(cmd.OutOrStdout(), resp.JobID, resp.OldStatus, resp.NewStatus)
	})
}

// =============================================================================
// Scheduler Resume Command
// =============================================================================

// newSchedulerResumeCommand creates the scheduler resume command.
func newSchedulerResumeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resume <job-id>",
		Short: "Resume a paused job",
		Long: `Resume a previously paused scheduled job.

Examples:
  quicbaas scheduler job resume 123`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerResumeCommand,
	}
	addSchedulerScopeFlag(cmd)

	return cmd
}

// runSchedulerResumeCommand executes the scheduler resume command.
func runSchedulerResumeCommand(cmd *cobra.Command, args []string) error {
	jobID, err := parseSchedulerJobID("scheduler.resume", args[0])
	if err != nil {
		return err
	}
	opts, err := parseSchedulerJobOptions("scheduler.resume")
	if err != nil {
		return err
	}
	return runSchedulerCall(func(ctx stdcontext.Context, client scheduler.Client) error {
		resp, err := client.ResumeJob(ctx, jobID, opts...)
		if err != nil {
			return formatError(err)
		}
		return formatSchedulerJobStatusChange(cmd.OutOrStdout(), resp.JobID, resp.OldStatus, resp.NewStatus)
	})
}

// =============================================================================
// Scheduler Cancel Command
// =============================================================================

// newSchedulerCancelCommand creates the scheduler cancel command.
func newSchedulerCancelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel <job-id>",
		Short: "Cancel a scheduled job",
		Long: `Cancel a scheduled job. The job will not be deleted but will stop future executions.

Examples:
  quicbaas scheduler job cancel 123
  quicbaas scheduler job cancel 123 --reason "No longer needed"`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerCancelCommand,
	}
	addSchedulerScopeFlag(cmd)

	cmd.Flags().StringVar(&schedulerReason, "reason", "", "Cancellation reason")

	return cmd
}

// runSchedulerCancelCommand executes the scheduler cancel command.
func runSchedulerCancelCommand(cmd *cobra.Command, args []string) error {
	jobID, err := parseSchedulerJobID("scheduler.cancel", args[0])
	if err != nil {
		return err
	}
	opts, err := parseSchedulerJobOptions("scheduler.cancel")
	if err != nil {
		return err
	}
	return runSchedulerCall(func(ctx stdcontext.Context, client scheduler.Client) error {
		resp, err := client.CancelJob(ctx, jobID, schedulerReason, opts...)
		if err != nil {
			return formatError(err)
		}
		return formatSchedulerJobStatusChange(cmd.OutOrStdout(), resp.JobID, resp.OldStatus, resp.NewStatus)
	})
}

// =============================================================================
// Scheduler History Command
// =============================================================================

// newSchedulerHistoryCommand creates the scheduler history command.
func newSchedulerHistoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history <job-id>",
		Short: "View job execution history",
		Long: `View the execution history for a scheduled job.

Examples:
  quicbaas scheduler job history 123
  quicbaas scheduler job history 123 --limit 20`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerHistoryCommand,
	}
	addSchedulerScopeFlag(cmd)

	cmd.Flags().IntVar(&schedulerLimit, "limit", 50, "Maximum number of executions to return")

	return cmd
}

// runSchedulerHistoryCommand executes the scheduler history command.
func runSchedulerHistoryCommand(cmd *cobra.Command, args []string) error {
	jobID, err := parseSchedulerJobID("scheduler.history", args[0])
	if err != nil {
		return err
	}
	opts, err := parseSchedulerJobOptions("scheduler.history")
	if err != nil {
		return err
	}
	return runSchedulerCall(func(ctx stdcontext.Context, client scheduler.Client) error {
		resp, err := client.GetJobHistory(ctx, jobID, &scheduler.HistoryOptions{Limit: schedulerLimit}, opts...)
		if err != nil {
			return formatError(err)
		}
		return formatSchedulerJobHistory(cmd.OutOrStdout(), resp)
	})
}

// =============================================================================
// Scheduler Status Command
// =============================================================================

// newSchedulerStatusCommand creates the scheduler status command.
func newSchedulerStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show scheduler status",
		Long: `Display scheduler status and statistics.

Examples:
  quicbaas scheduler status`,
		Args: cobra.NoArgs,
		RunE: runSchedulerStatusCommand,
	}

	return cmd
}

// runSchedulerStatusCommand executes the scheduler status command.
func runSchedulerStatusCommand(cmd *cobra.Command, args []string) error {
	return runSchedulerCall(func(ctx stdcontext.Context, client scheduler.Client) error {
		status, err := client.GetStatus(ctx)
		if err != nil {
			return formatError(err)
		}
		return formatSchedulerStatus(cmd.OutOrStdout(), status)
	})
}

// =============================================================================
// Formatting Functions
// =============================================================================

// writeSchedulerOutput renders v as JSON or YAML, or as text: table writes
// the table form and raw, when set, the raw form (table otherwise).
func writeSchedulerOutput(w io.Writer, v interface{}, table func(*tabwriter.Writer), raw func(io.Writer)) error {
	format, err := cli.ParseOutputFormat(schedulerOutputFmt)
	if err != nil {
		return err
	}
	switch {
	case format == cli.OutputFormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case format == cli.OutputFormatYAML:
		// The output types carry JSON tags only; going through their JSON
		// form gives YAML the same snake_case keys as --output json.
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var generic interface{}
		if err := json.Unmarshal(data, &generic); err != nil {
			return err
		}
		return yaml.NewEncoder(w).Encode(generic)
	case format == cli.OutputFormatTable || raw == nil:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		table(tw)
		return tw.Flush()
	default:
		raw(w)
		return nil
	}
}

// schedulerJobView is the CLI's rendering of an SDK job.
type schedulerJobView struct {
	ID          uint64            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	OwnerType   string            `json:"owner_type"`
	OwnerID     string            `json:"owner_id,omitempty"`
	Type        string            `json:"type"`
	Schedule    string            `json:"schedule,omitempty"`
	Delay       time.Duration     `json:"delay,omitempty"`
	Handler     string            `json:"handler"`
	Payload     []byte            `json:"payload,omitempty"`
	Priority    int               `json:"priority"`
	Timeout     time.Duration     `json:"timeout"`
	MaxRetries  int               `json:"max_retries"`
	Status      string            `json:"status"`
	LastRun     time.Time         `json:"last_run,omitempty"`
	NextRun     time.Time         `json:"next_run,omitempty"`
	RunCount    uint64            `json:"run_count"`
	FailCount   uint64            `json:"fail_count"`
	LastError   string            `json:"last_error,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// newSchedulerJobView converts an SDK job for output.
func newSchedulerJobView(job *scheduler.JobInfo) schedulerJobView {
	return schedulerJobView{
		ID:          job.JobID,
		Name:        job.Name,
		Description: job.Description,
		OwnerType:   schedulerName(job.OwnerType),
		OwnerID:     job.OwnerID,
		Type:        schedulerName(job.JobType),
		Schedule:    job.Schedule,
		Delay:       job.Delay,
		Handler:     job.Handler,
		Payload:     job.Payload,
		Priority:    job.Priority,
		Timeout:     job.Timeout,
		MaxRetries:  job.MaxRetries,
		Status:      schedulerName(job.Status),
		LastRun:     job.LastRunAt,
		NextRun:     job.NextRunAt,
		RunCount:    job.ExecutionCount,
		FailCount:   job.FailureCount,
		LastError:   job.LastError,
		Labels:      job.Tags,
		CreatedAt:   job.CreatedAt,
		UpdatedAt:   job.UpdatedAt,
	}
}

// formatSchedulerJob writes one job.
func formatSchedulerJob(w io.Writer, job *scheduler.JobInfo) error {
	view := newSchedulerJobView(job)
	return writeSchedulerOutput(w, view, func(tw *tabwriter.Writer) {
		fmt.Fprintf(tw, "ID:\t%d\n", view.ID)
		fmt.Fprintf(tw, "Name:\t%s\n", view.Name)
		if view.Description != "" {
			fmt.Fprintf(tw, "Description:\t%s\n", view.Description)
		}
		fmt.Fprintf(tw, "Type:\t%s\n", view.Type)
		fmt.Fprintf(tw, "Handler:\t%s\n", view.Handler)
		fmt.Fprintf(tw, "Status:\t%s\n", view.Status)
		if view.Schedule != "" {
			fmt.Fprintf(tw, "Schedule:\t%s\n", view.Schedule)
		}
		if !view.NextRun.IsZero() {
			fmt.Fprintf(tw, "Next Run:\t%s\n", view.NextRun.Format(time.RFC3339))
		}
		if !view.LastRun.IsZero() {
			fmt.Fprintf(tw, "Last Run:\t%s\n", view.LastRun.Format(time.RFC3339))
		}
		fmt.Fprintf(tw, "Run Count:\t%d\n", view.RunCount)
		fmt.Fprintf(tw, "Fail Count:\t%d\n", view.FailCount)
		if view.LastError != "" {
			fmt.Fprintf(tw, "Last Error:\t%s\n", view.LastError)
		}
		fmt.Fprintf(tw, "Priority:\t%d\n", view.Priority)
		fmt.Fprintf(tw, "Max Retries:\t%d\n", view.MaxRetries)
		fmt.Fprintf(tw, "Created:\t%s\n", view.CreatedAt.Format(time.RFC3339))
	}, func(w io.Writer) {
		fmt.Fprintf(w, "Job %s (ID: %d) - %s\n", view.Name, view.ID, view.Status)
	})
}

// schedulerJobCreatedView is the CLI's rendering of a create response,
// which carries the new job's ID and first run only.
type schedulerJobCreatedView struct {
	ID      uint64    `json:"id"`
	Name    string    `json:"name"`
	NextRun time.Time `json:"next_run,omitempty"`
}

// formatSchedulerJobCreated writes the job the create command made.
func formatSchedulerJobCreated(w io.Writer, name string, resp *scheduler.CreateJobResponse) error {
	view := schedulerJobCreatedView{ID: resp.JobID, Name: name, NextRun: resp.NextRunAt}
	return writeSchedulerOutput(w, view, func(tw *tabwriter.Writer) {
		fmt.Fprintf(tw, "ID:\t%d\n", view.ID)
		fmt.Fprintf(tw, "Name:\t%s\n", view.Name)
		if !view.NextRun.IsZero() {
			fmt.Fprintf(tw, "Next Run:\t%s\n", view.NextRun.Format(time.RFC3339))
		}
	}, func(w io.Writer) {
		fmt.Fprintf(w, "Job created: %s (ID: %d)\n", view.Name, view.ID)
	})
}

// schedulerJobListView is the CLI's rendering of a list response.
type schedulerJobListView struct {
	Jobs       []schedulerJobView `json:"jobs"`
	TotalCount int                `json:"total_count"`
	HasMore    bool               `json:"has_more"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// formatSchedulerJobList writes a page of jobs.
func formatSchedulerJobList(w io.Writer, resp *scheduler.ListJobsResponse) error {
	view := schedulerJobListView{
		Jobs:       make([]schedulerJobView, 0, len(resp.Jobs)),
		TotalCount: resp.TotalCount,
		HasMore:    resp.HasMore,
		NextCursor: resp.NextCursor,
	}
	for i := range resp.Jobs {
		view.Jobs = append(view.Jobs, newSchedulerJobView(&resp.Jobs[i]))
	}
	return writeSchedulerOutput(w, view, func(tw *tabwriter.Writer) {
		fmt.Fprintf(tw, "ID\tNAME\tTYPE\tHANDLER\tSTATUS\tNEXT RUN\n")
		for _, job := range view.Jobs {
			nextRun := "-"
			if !job.NextRun.IsZero() {
				nextRun = job.NextRun.Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", job.ID, job.Name, job.Type, job.Handler, job.Status, nextRun)
		}
		fmt.Fprintf(tw, "\nTotal: %d jobs\n", view.TotalCount)
		if view.HasMore {
			fmt.Fprintf(tw, "Next cursor: %s\n", view.NextCursor)
		}
	}, func(w io.Writer) {
		for _, job := range view.Jobs {
			fmt.Fprintf(w, "%d: %s (%s) - %s\n", job.ID, job.Name, job.Type, job.Status)
		}
	})
}

// schedulerJobDeletedView is the CLI's rendering of a delete response.
type schedulerJobDeletedView struct {
	JobID   uint64 `json:"job_id"`
	Deleted bool   `json:"deleted"`
}

// formatSchedulerJobDeleted writes the outcome of a delete.
func formatSchedulerJobDeleted(w io.Writer, resp *scheduler.DeleteJobResponse) error {
	view := schedulerJobDeletedView{JobID: resp.JobID, Deleted: resp.Deleted}
	return writeSchedulerOutput(w, view, func(tw *tabwriter.Writer) {
		fmt.Fprintf(tw, "Job ID:\t%d\n", view.JobID)
		fmt.Fprintf(tw, "Deleted:\t%v\n", view.Deleted)
	}, func(w io.Writer) {
		if view.Deleted {
			fmt.Fprintf(w, "Job %d deleted successfully\n", view.JobID)
		} else {
			fmt.Fprintf(w, "Job %d was not deleted\n", view.JobID)
		}
	})
}

// schedulerJobTriggeredView is the CLI's rendering of a trigger response.
type schedulerJobTriggeredView struct {
	JobID  uint64 `json:"job_id"`
	Status string `json:"status"`
}

// formatSchedulerJobTriggered writes the outcome of a trigger.
func formatSchedulerJobTriggered(w io.Writer, resp *scheduler.TriggerJobResponse) error {
	view := schedulerJobTriggeredView{JobID: resp.JobID, Status: schedulerName(resp.Status)}
	return writeSchedulerOutput(w, view, func(tw *tabwriter.Writer) {
		fmt.Fprintf(tw, "Job ID:\t%d\n", view.JobID)
		fmt.Fprintf(tw, "Status:\t%s\n", view.Status)
	}, func(w io.Writer) {
		fmt.Fprintf(w, "Job %d triggered (status: %s)\n", view.JobID, view.Status)
	})
}

// schedulerJobStatusChangeView is the CLI's rendering of a pause, resume or
// cancel response.
type schedulerJobStatusChangeView struct {
	JobID     uint64 `json:"job_id"`
	OldStatus string `json:"old_status"`
	NewStatus string `json:"new_status"`
}

// formatSchedulerJobStatusChange writes a job's status transition.
func formatSchedulerJobStatusChange(w io.Writer, jobID uint64, oldStatus, newStatus scheduler.JobStatus) error {
	view := schedulerJobStatusChangeView{JobID: jobID, OldStatus: schedulerName(oldStatus), NewStatus: schedulerName(newStatus)}
	return writeSchedulerOutput(w, view, func(tw *tabwriter.Writer) {
		fmt.Fprintf(tw, "Job ID:\t%d\n", view.JobID)
		fmt.Fprintf(tw, "Old Status:\t%s\n", view.OldStatus)
		fmt.Fprintf(tw, "New Status:\t%s\n", view.NewStatus)
	}, func(w io.Writer) {
		fmt.Fprintf(w, "Job %d status changed: %s -> %s\n", view.JobID, view.OldStatus, view.NewStatus)
	})
}

// schedulerExecutionView is the CLI's rendering of one job execution.
type schedulerExecutionView struct {
	ID        uint64        `json:"id"`
	JobID     uint64        `json:"job_id"`
	Status    string        `json:"status"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	Error     string        `json:"error,omitempty"`
	Output    string        `json:"output,omitempty"`
}

// schedulerJobHistoryView is the CLI's rendering of a history response.
type schedulerJobHistoryView struct {
	JobID      uint64                   `json:"job_id"`
	Executions []schedulerExecutionView `json:"executions"`
	HasMore    bool                     `json:"has_more"`
}

// schedulerHistoryErrorWidth is the widest error the history table shows.
const schedulerHistoryErrorWidth = 50

// formatSchedulerJobHistory writes a job's executions.
func formatSchedulerJobHistory(w io.Writer, resp *scheduler.JobHistoryResponse) error {
	view := schedulerJobHistoryView{
		JobID:      resp.JobID,
		Executions: make([]schedulerExecutionView, 0, len(resp.Executions)),
		HasMore:    resp.HasMore,
	}
	for _, exec := range resp.Executions {
		view.Executions = append(view.Executions, schedulerExecutionView{
			ID:        exec.ExecutionID,
			JobID:     exec.JobID,
			Status:    schedulerName(exec.Status),
			StartedAt: exec.StartedAt,
			EndedAt:   exec.CompletedAt,
			Duration:  exec.Duration,
			Error:     exec.Error,
			Output:    string(exec.Result),
		})
	}
	return writeSchedulerOutput(w, view, func(tw *tabwriter.Writer) {
		fmt.Fprintf(tw, "ID\tSTATUS\tSTARTED\tDURATION\tERROR\n")
		for _, exec := range view.Executions {
			errMsg := "-"
			if exec.Error != "" {
				errMsg = exec.Error
				if len(errMsg) > schedulerHistoryErrorWidth {
					errMsg = errMsg[:schedulerHistoryErrorWidth-3] + "..."
				}
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n",
				exec.ID, exec.Status, exec.StartedAt.Format(time.RFC3339), exec.Duration, errMsg)
		}
		if view.HasMore {
			fmt.Fprintf(tw, "\nMore results available. Raise --limit to see them.\n")
		}
	}, func(w io.Writer) {
		for _, exec := range view.Executions {
			fmt.Fprintf(w, "%d: %s (%s)\n", exec.ID, exec.Status, exec.Duration)
		}
	})
}

// schedulerStatusView is the CLI's rendering of the scheduler status.
type schedulerStatusView struct {
	Running      bool          `json:"running"`
	TotalJobs    uint64        `json:"total_jobs"`
	RunningJobs  uint64        `json:"running_jobs"`
	SuccessCount uint64        `json:"success_count"`
	FailureCount uint64        `json:"failure_count"`
	Uptime       time.Duration `json:"uptime,omitempty"`
	LastTick     time.Time     `json:"last_tick,omitempty"`
}

// formatSchedulerStatus writes the scheduler status.
func formatSchedulerStatus(w io.Writer, status *scheduler.SchedulerStatus) error {
	view := schedulerStatusView{
		Running:      status.Healthy,
		TotalJobs:    status.TotalJobs,
		RunningJobs:  status.RunningJobs,
		SuccessCount: status.CompletedJobs,
		FailureCount: status.FailedJobs,
		Uptime:       status.UptimeDuration,
		LastTick:     status.LastHeartbeat,
	}
	state := "stopped"
	if view.Running {
		state = "running"
	}
	return writeSchedulerOutput(w, view, func(tw *tabwriter.Writer) {
		fmt.Fprintf(tw, "Status:\t%s\n", state)
		fmt.Fprintf(tw, "Total Jobs:\t%d\n", view.TotalJobs)
		fmt.Fprintf(tw, "Running Jobs:\t%d\n", view.RunningJobs)
		fmt.Fprintf(tw, "Success Count:\t%d\n", view.SuccessCount)
		fmt.Fprintf(tw, "Failure Count:\t%d\n", view.FailureCount)
		if view.Uptime > 0 {
			fmt.Fprintf(tw, "Uptime:\t%s\n", view.Uptime.Round(time.Second))
		}
		if !view.LastTick.IsZero() {
			fmt.Fprintf(tw, "Last Tick:\t%s\n", view.LastTick.Format(time.RFC3339))
		}
	}, func(w io.Writer) {
		fmt.Fprintf(w, "Scheduler: %s\n", state)
	})
}
