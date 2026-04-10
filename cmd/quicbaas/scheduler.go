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
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

var (
	schedulerDescription string
	schedulerType        string
	schedulerSchedule    string
	schedulerHandler     string
	schedulerPayload     string
	schedulerPriority    int32
	schedulerTimeout     string
	schedulerMaxRetries  int32
	schedulerDelay       string
	schedulerExecuteAt   string
	schedulerLimit       int
	schedulerStatus      string
	schedulerOutputFmt   string
	schedulerForce       bool
	schedulerReason      string
	schedulerCursor      string
)

// SchedulerClient defines the interface for scheduler operations.
type SchedulerClient interface {
	SchedulerCreateJob(ctx stdcontext.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error)
	SchedulerGetJob(ctx stdcontext.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error)
	SchedulerUpdateJob(ctx stdcontext.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error)
	SchedulerDeleteJob(ctx stdcontext.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error)
	SchedulerListJobs(ctx stdcontext.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error)
	SchedulerTriggerJob(ctx stdcontext.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error)
	SchedulerPauseJob(ctx stdcontext.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error)
	SchedulerResumeJob(ctx stdcontext.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error)
	SchedulerCancelJob(ctx stdcontext.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error)
	SchedulerGetJobHistory(ctx stdcontext.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error)
	SchedulerGetStatus(ctx stdcontext.Context) (*service.GetSchedulerStatusResponse, error)
	Close() error
}

// SchedulerClientFactory is a function type for creating scheduler clients.
type SchedulerClientFactory func() (SchedulerClient, error)

// schedulerClientFactory creates scheduler clients. Defaults to createSchedulerClient.
var schedulerClientFactory SchedulerClientFactory = createSchedulerClient

// createSchedulerClient creates a client for scheduler operations based on transport.
func createSchedulerClient() (SchedulerClient, error) {
	switch transport {
	case "grpc", "":
		return createSchedulerGRPCClient()
	case "rest":
		return createSchedulerRESTClientFactory()
	case "mcp":
		return createSchedulerMCPClientFactory()
	case "quic":
		return createSchedulerQUICClientFactory()
	case "unix":
		return createSchedulerUnixClient()
	default:
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createSchedulerClient",
			"unsupported transport: %s (supported: grpc, rest, mcp, quic, unix)", transport,
		)
	}
}

// createSchedulerGRPCClient creates a gRPC client for scheduler operations.
func createSchedulerGRPCClient() (SchedulerClient, error) {
	opts := []grpc.ClientOption{
		grpc.WithTimeout(timeout),
	}
	opts = append(opts, grpcTLSOption()...)
	return grpc.NewClient(address, opts...)
}

// createSchedulerUnixClient creates a gRPC client over Unix socket for scheduler operations.
func createSchedulerUnixClient() (SchedulerClient, error) {
	// For Unix socket transport, use the gRPC client with the Unix socket address.
	// The address should be the Unix socket path (e.g., /tmp/quicbaas.sock).
	opts := []grpc.ClientOption{
		grpc.WithTimeout(timeout),
	}
	opts = append(opts, grpcTLSOption()...)
	return grpc.NewClient(address, opts...)
}

// getSchedulerLogLevel returns the current log level, defaulting to "info".
//
//nolint:unused // Called from scheduler_rest_client.go (rest) and scheduler_quic_client.go (quic).
func getSchedulerLogLevel() string {
	if logLevel != "" {
		return logLevel
	}
	return "info"
}

// constructSchedulerRESTBaseURL constructs the base URL for REST API calls.
//
//nolint:unused // Called from scheduler_rest_client.go (rest build tag).
func constructSchedulerRESTBaseURL(addr string) string {
	// If address already has a scheme, use it as-is
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}

	// Determine scheme based on TLS configuration
	scheme := "http"
	if tlsEnabled || tlsSkipVerify {
		scheme = "https"
	}

	return fmt.Sprintf("%s://%s", scheme, addr)
}

// newSchedulerCommand creates the scheduler command group.
func newSchedulerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scheduler",
		Short: "Job scheduler commands",
		Long: `Scheduler commands for managing scheduled jobs.

These commands allow you to create, manage, and monitor scheduled jobs
for system maintenance, tenant operations, and background processing.`,
	}

	cmd.PersistentFlags().StringVar(&schedulerOutputFmt, "output", "table", "Output format (json, yaml, table)")

	// Add job subcommand group
	cmd.AddCommand(newSchedulerJobCommand())
	// Add status as direct subcommand of scheduler
	cmd.AddCommand(newSchedulerStatusCommand())

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
  quicbaas scheduler create backup --type recurring --schedule "0 0 * * *" --handler backup.full
  quicbaas scheduler create grace-disable --type one-shot --execute-at "2025-01-20T00:00:00Z" --handler kms.grace_period
  quicbaas scheduler create cleanup --type delayed --delay 24h --handler audit.cleanup`,
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
	cmd.Flags().Int32Var(&schedulerPriority, "priority", 0, "Job priority")
	cmd.Flags().StringVar(&schedulerTimeout, "timeout", "5m", "Job timeout")
	cmd.Flags().Int32Var(&schedulerMaxRetries, "max-retries", 3, "Maximum retry attempts")

	if err := cmd.MarkFlagRequired("handler"); err != nil {
		// Log but don't fail - flag marking is a best-effort operation
		fmt.Fprintf(os.Stderr, "warning: failed to mark handler flag as required: %v\n", err)
	}

	return cmd
}

// runSchedulerCreateCommand executes the scheduler create command.
func runSchedulerCreateCommand(cmd *cobra.Command, args []string) error {
	jobName := args[0]

	client, err := schedulerClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	// Parse timeout
	jobTimeout, err := time.ParseDuration(schedulerTimeout)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.create", "invalid timeout format")
	}

	// Parse execute-at if provided
	var executeAt time.Time
	if schedulerExecuteAt != "" {
		executeAt, err = time.Parse(time.RFC3339, schedulerExecuteAt)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.create", "invalid execute-at format, use RFC3339")
		}
	}

	// Parse delay if provided
	var delay time.Duration
	if schedulerDelay != "" {
		delay, err = time.ParseDuration(schedulerDelay)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.create", "invalid delay format")
		}
	}

	// Parse payload if provided
	var payload []byte
	if schedulerPayload != "" {
		payload = []byte(schedulerPayload)
	}

	req := &service.CreateSchedulerJobRequest{
		Name:        jobName,
		Description: schedulerDescription,
		Type:        service.SchedulerJobType(schedulerType),
		Schedule:    schedulerSchedule,
		ExecuteAt:   executeAt,
		Delay:       delay,
		Handler:     schedulerHandler,
		Payload:     payload,
		Priority:    schedulerPriority,
		Timeout:     jobTimeout,
		MaxRetries:  schedulerMaxRetries,
	}

	// Set owner based on tenant flag
	if globalTenantID != "" {
		req.OwnerType = service.SchedulerOwnerTypeTenant
		req.OwnerID = globalTenantID
	} else {
		req.OwnerType = service.SchedulerOwnerTypeSystem
	}

	resp, err := client.SchedulerCreateJob(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSchedulerJobResponse(resp.Job, schedulerOutputFmt, os.Stdout)
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
  quicbaas scheduler list
  quicbaas scheduler list --status pending
  quicbaas scheduler list --handler backup.full --limit 50`,
		Args: cobra.NoArgs,
		RunE: runSchedulerListCommand,
	}

	cmd.Flags().StringVar(&schedulerStatus, "status", "", "Filter by job status (pending, running, completed, failed, paused, canceled)")
	cmd.Flags().StringVar(&schedulerHandler, "handler", "", "Filter by handler name")
	cmd.Flags().IntVar(&schedulerLimit, "limit", 100, "Maximum number of jobs to return")
	cmd.Flags().StringVar(&schedulerCursor, "cursor", "", "Pagination cursor")

	return cmd
}

// runSchedulerListCommand executes the scheduler list command.
func runSchedulerListCommand(cmd *cobra.Command, args []string) error {
	client, err := schedulerClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.ListSchedulerJobsRequest{
		Limit:  schedulerLimit,
		Cursor: schedulerCursor,
	}

	if schedulerStatus != "" {
		req.Status = service.SchedulerJobStatus(schedulerStatus)
	}
	if schedulerHandler != "" {
		req.Handler = schedulerHandler
	}
	if globalTenantID != "" {
		req.OwnerType = service.SchedulerOwnerTypeTenant
		req.OwnerID = globalTenantID
	}

	resp, err := client.SchedulerListJobs(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSchedulerJobListResponse(resp, schedulerOutputFmt, os.Stdout)
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
  quicbaas scheduler get 123
  quicbaas scheduler get 123 --tenant acme`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerGetCommand,
	}

	return cmd
}

// runSchedulerGetCommand executes the scheduler get command.
func runSchedulerGetCommand(cmd *cobra.Command, args []string) error {
	jobID, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.get", "invalid job ID format")
	}

	client, err := schedulerClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.GetSchedulerJobRequest{
		JobID:    jobID,
		TenantID: globalTenantID,
	}

	resp, err := client.SchedulerGetJob(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSchedulerJobResponse(resp.Job, schedulerOutputFmt, os.Stdout)
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
  quicbaas scheduler delete 123
  quicbaas scheduler delete 123 --force`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerDeleteCommand,
	}

	cmd.Flags().BoolVar(&schedulerForce, "force", false, "Force deletion even if job is running")

	return cmd
}

// runSchedulerDeleteCommand executes the scheduler delete command.
func runSchedulerDeleteCommand(cmd *cobra.Command, args []string) error {
	jobID, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.delete", "invalid job ID format")
	}

	client, err := schedulerClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.DeleteSchedulerJobRequest{
		JobID:    jobID,
		TenantID: globalTenantID,
		Force:    schedulerForce,
	}

	resp, err := client.SchedulerDeleteJob(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSchedulerJobDeleteResponse(resp, schedulerOutputFmt, os.Stdout)
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
  quicbaas scheduler trigger 123`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerTriggerCommand,
	}

	return cmd
}

// runSchedulerTriggerCommand executes the scheduler trigger command.
func runSchedulerTriggerCommand(cmd *cobra.Command, args []string) error {
	jobID, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.trigger", "invalid job ID format")
	}

	client, err := schedulerClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.TriggerSchedulerJobRequest{
		JobID:    jobID,
		TenantID: globalTenantID,
	}

	resp, err := client.SchedulerTriggerJob(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSchedulerJobTriggerResponse(resp, schedulerOutputFmt, os.Stdout)
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
  quicbaas scheduler pause 123`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerPauseCommand,
	}

	return cmd
}

// runSchedulerPauseCommand executes the scheduler pause command.
func runSchedulerPauseCommand(cmd *cobra.Command, args []string) error {
	jobID, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.pause", "invalid job ID format")
	}

	client, err := schedulerClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.PauseSchedulerJobRequest{
		JobID:    jobID,
		TenantID: globalTenantID,
	}

	resp, err := client.SchedulerPauseJob(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSchedulerJobStatusChangeResponse(resp.JobID, resp.OldStatus, resp.NewStatus, schedulerOutputFmt, os.Stdout)
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
  quicbaas scheduler resume 123`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerResumeCommand,
	}

	return cmd
}

// runSchedulerResumeCommand executes the scheduler resume command.
func runSchedulerResumeCommand(cmd *cobra.Command, args []string) error {
	jobID, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.resume", "invalid job ID format")
	}

	client, err := schedulerClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.ResumeSchedulerJobRequest{
		JobID:    jobID,
		TenantID: globalTenantID,
	}

	resp, err := client.SchedulerResumeJob(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSchedulerJobStatusChangeResponse(resp.JobID, resp.OldStatus, resp.NewStatus, schedulerOutputFmt, os.Stdout)
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
  quicbaas scheduler cancel 123
  quicbaas scheduler cancel 123 --reason "No longer needed"`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerCancelCommand,
	}

	cmd.Flags().StringVar(&schedulerReason, "reason", "", "Cancellation reason")

	return cmd
}

// runSchedulerCancelCommand executes the scheduler cancel command.
func runSchedulerCancelCommand(cmd *cobra.Command, args []string) error {
	jobID, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.cancel", "invalid job ID format")
	}

	client, err := schedulerClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.CancelSchedulerJobRequest{
		JobID:    jobID,
		TenantID: globalTenantID,
		Reason:   schedulerReason,
	}

	resp, err := client.SchedulerCancelJob(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSchedulerJobStatusChangeResponse(resp.JobID, resp.OldStatus, resp.NewStatus, schedulerOutputFmt, os.Stdout)
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
  quicbaas scheduler history 123
  quicbaas scheduler history 123 --limit 20`,
		Args: cobra.ExactArgs(1),
		RunE: runSchedulerHistoryCommand,
	}

	cmd.Flags().IntVar(&schedulerLimit, "limit", 50, "Maximum number of executions to return")
	cmd.Flags().StringVar(&schedulerCursor, "cursor", "", "Pagination cursor")

	return cmd
}

// runSchedulerHistoryCommand executes the scheduler history command.
func runSchedulerHistoryCommand(cmd *cobra.Command, args []string) error {
	jobID, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "scheduler.history", "invalid job ID format")
	}

	client, err := schedulerClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &service.GetSchedulerJobHistoryRequest{
		JobID:    jobID,
		TenantID: globalTenantID,
		Limit:    schedulerLimit,
		Cursor:   schedulerCursor,
	}

	resp, err := client.SchedulerGetJobHistory(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSchedulerJobHistoryResponse(resp, schedulerOutputFmt, os.Stdout)
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
	client, err := schedulerClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	resp, err := client.SchedulerGetStatus(ctx)
	if err != nil {
		return formatError(err)
	}

	return formatSchedulerStatusResponse(resp, schedulerOutputFmt, os.Stdout)
}

// =============================================================================
// Formatting Functions
// =============================================================================

// formatSchedulerJobResponse formats a scheduler job for output.
func formatSchedulerJobResponse(job *service.SchedulerJob, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(job)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(job)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID:\t%d\n", job.ID)
		fmt.Fprintf(tw, "Name:\t%s\n", job.Name)
		if job.Description != "" {
			fmt.Fprintf(tw, "Description:\t%s\n", job.Description)
		}
		fmt.Fprintf(tw, "Type:\t%s\n", job.Type)
		fmt.Fprintf(tw, "Handler:\t%s\n", job.Handler)
		fmt.Fprintf(tw, "Status:\t%s\n", job.Status)
		if job.Schedule != "" {
			fmt.Fprintf(tw, "Schedule:\t%s\n", job.Schedule)
		}
		if !job.NextRun.IsZero() {
			fmt.Fprintf(tw, "Next Run:\t%s\n", job.NextRun.Format(time.RFC3339))
		}
		if !job.LastRun.IsZero() {
			fmt.Fprintf(tw, "Last Run:\t%s\n", job.LastRun.Format(time.RFC3339))
		}
		fmt.Fprintf(tw, "Run Count:\t%d\n", job.RunCount)
		fmt.Fprintf(tw, "Fail Count:\t%d\n", job.FailCount)
		if job.LastError != "" {
			fmt.Fprintf(tw, "Last Error:\t%s\n", job.LastError)
		}
		fmt.Fprintf(tw, "Priority:\t%d\n", job.Priority)
		fmt.Fprintf(tw, "Max Retries:\t%d\n", job.MaxRetries)
		fmt.Fprintf(tw, "Created:\t%s\n", job.CreatedAt.Format(time.RFC3339))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Job created: %s (ID: %d)\n", job.Name, job.ID)
		return nil
	}
}

// formatSchedulerJobListResponse formats a job list for output.
func formatSchedulerJobListResponse(resp *service.ListSchedulerJobsResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tNAME\tTYPE\tHANDLER\tSTATUS\tNEXT RUN\n")
		for _, job := range resp.Jobs {
			nextRun := "-"
			if !job.NextRun.IsZero() {
				nextRun = job.NextRun.Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
				job.ID, job.Name, job.Type, job.Handler, job.Status, nextRun)
		}
		fmt.Fprintf(tw, "\nTotal: %d jobs\n", resp.TotalCount)
		if resp.HasMore {
			fmt.Fprintf(tw, "Next cursor: %s\n", resp.NextCursor)
		}
		return tw.Flush()

	default:
		for _, job := range resp.Jobs {
			fmt.Fprintf(w, "%d: %s (%s) - %s\n", job.ID, job.Name, job.Type, job.Status)
		}
		return nil
	}
}

// formatSchedulerJobDeleteResponse formats a job delete response for output.
func formatSchedulerJobDeleteResponse(resp *service.DeleteSchedulerJobResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Job ID:\t%d\n", resp.JobID)
		fmt.Fprintf(tw, "Deleted:\t%v\n", resp.Deleted)
		return tw.Flush()

	default:
		if resp.Deleted {
			fmt.Fprintf(w, "Job %d deleted successfully\n", resp.JobID)
		} else {
			fmt.Fprintf(w, "Job %d was not deleted\n", resp.JobID)
		}
		return nil
	}
}

// formatSchedulerJobTriggerResponse formats a job trigger response for output.
func formatSchedulerJobTriggerResponse(resp *service.TriggerSchedulerJobResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Job ID:\t%d\n", resp.JobID)
		fmt.Fprintf(tw, "Status:\t%s\n", resp.Status)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Job %d triggered (status: %s)\n", resp.JobID, resp.Status)
		return nil
	}
}

// formatSchedulerJobStatusChangeResponse formats a status change response for output.
func formatSchedulerJobStatusChangeResponse(jobID uint64, oldStatus, newStatus service.SchedulerJobStatus, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	response := map[string]interface{}{
		"job_id":     jobID,
		"old_status": oldStatus,
		"new_status": newStatus,
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(response)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(response)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Job ID:\t%d\n", jobID)
		fmt.Fprintf(tw, "Old Status:\t%s\n", oldStatus)
		fmt.Fprintf(tw, "New Status:\t%s\n", newStatus)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Job %d status changed: %s -> %s\n", jobID, oldStatus, newStatus)
		return nil
	}
}

// formatSchedulerJobHistoryResponse formats a job history response for output.
func formatSchedulerJobHistoryResponse(resp *service.GetSchedulerJobHistoryResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tSTATUS\tSTARTED\tDURATION\tERROR\n")
		for _, exec := range resp.Executions {
			startedAt := exec.StartedAt.Format(time.RFC3339)
			duration := exec.Duration.String()
			errMsg := "-"
			if exec.Error != "" {
				// Truncate long error messages
				if len(exec.Error) > 50 {
					errMsg = exec.Error[:47] + "..."
				} else {
					errMsg = exec.Error
				}
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n",
				exec.ID, exec.Status, startedAt, duration, errMsg)
		}
		if resp.HasMore {
			fmt.Fprintf(tw, "\nMore results available. Use --cursor %s\n", resp.NextCursor)
		}
		return tw.Flush()

	default:
		for _, exec := range resp.Executions {
			fmt.Fprintf(w, "%d: %s (%s)\n", exec.ID, exec.Status, exec.Duration.String())
		}
		return nil
	}
}

// formatSchedulerStatusResponse formats scheduler status for output.
func formatSchedulerStatusResponse(resp *service.GetSchedulerStatusResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		status := "Stopped"
		if resp.Running {
			status = "Running"
		}
		fmt.Fprintf(tw, "Status:\t%s\n", status)
		if resp.Stats != nil {
			fmt.Fprintf(tw, "Total Jobs:\t%d\n", resp.Stats.JobCount)
			fmt.Fprintf(tw, "Running Jobs:\t%d\n", resp.Stats.RunningJobs)
			fmt.Fprintf(tw, "Success Count:\t%d\n", resp.Stats.SuccessCount)
			fmt.Fprintf(tw, "Failure Count:\t%d\n", resp.Stats.FailureCount)
			if !resp.Stats.StartedAt.IsZero() {
				fmt.Fprintf(tw, "Started At:\t%s\n", resp.Stats.StartedAt.Format(time.RFC3339))
			}
			if !resp.Stats.LastTickTime.IsZero() {
				fmt.Fprintf(tw, "Last Tick:\t%s\n", resp.Stats.LastTickTime.Format(time.RFC3339))
			}
		}
		return tw.Flush()

	default:
		status := "stopped"
		if resp.Running {
			status = "running"
		}
		fmt.Fprintf(w, "Scheduler: %s\n", status)
		return nil
	}
}
