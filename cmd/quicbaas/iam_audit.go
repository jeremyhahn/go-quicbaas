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

//go:build iam

// Package main provides IAM audit log CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

var (
	// Audit command flags.
	iamAuditEventType string
	iamAuditCategory  string
	iamAuditOutcome   string
	iamAuditActor     string
	iamAuditResource  string
	iamAuditStartTime string
	iamAuditEndTime   string
	iamAuditLimit     int
	iamAuditOffset    int
)

// newIAMAuditCommand creates the audit command group.
func newIAMAuditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit log management commands",
		Long: `Query and view audit log events.

The audit subsystem records security-relevant events including:
- Authentication events (login, logout, token refresh)
- Authorization decisions (policy evaluations)
- Management operations (user, role, policy changes)

All audit commands require the --tenant flag to specify the tenant context.`,
	}

	// Add subcommands
	cmd.AddCommand(newIAMAuditQueryCommand())
	cmd.AddCommand(newIAMAuditGetCommand())

	return cmd
}

// newIAMAuditQueryCommand creates the audit query command.
func newIAMAuditQueryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query audit events with filters",
		Long: `Query audit log events with optional filters.

Filter options allow narrowing results by event type, category, outcome,
actor, resource, and time range.

Examples:
  # Query all events for a tenant
  quicbaas iam audit query --tenant acme-corp

  # Filter by event type
  quicbaas iam audit query --tenant acme-corp --event-type user.login

  # Filter by category
  quicbaas iam audit query --tenant acme-corp --category authentication

  # Filter by outcome
  quicbaas iam audit query --tenant acme-corp --outcome failure

  # Filter by actor
  quicbaas iam audit query --tenant acme-corp --actor user_abc123

  # Filter by time range
  quicbaas iam audit query --tenant acme-corp \
    --start-time 2025-01-01T00:00:00Z \
    --end-time 2025-01-02T00:00:00Z

  # Combine multiple filters
  quicbaas iam audit query --tenant acme-corp \
    --category authentication \
    --outcome failure \
    --limit 50

  # Output as JSON
  quicbaas iam audit query --tenant acme-corp --output json`,
		RunE: runIAMAuditQueryCommand,
	}

	cmd.Flags().StringVar(&iamAuditEventType, "event-type", "", "Filter by event type (e.g., user.login, policy.created)")
	cmd.Flags().StringVar(&iamAuditCategory, "category", "", "Filter by category (e.g., authentication, authorization, management)")
	cmd.Flags().StringVar(&iamAuditOutcome, "outcome", "", "Filter by outcome (success, failure)")
	cmd.Flags().StringVar(&iamAuditActor, "actor", "", "Filter by actor ID")
	cmd.Flags().StringVar(&iamAuditResource, "resource", "", "Filter by resource ID")
	cmd.Flags().StringVar(&iamAuditStartTime, "start-time", "", "Filter events after this time (RFC3339 format)")
	cmd.Flags().StringVar(&iamAuditEndTime, "end-time", "", "Filter events before this time (RFC3339 format)")
	cmd.Flags().IntVar(&iamAuditLimit, "limit", 100, "Maximum number of events to return")
	cmd.Flags().IntVar(&iamAuditOffset, "offset", 0, "Pagination offset")

	return cmd
}

// runIAMAuditQueryCommand executes the audit query command.
func runIAMAuditQueryCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.audit.query", "--tenant flag is required")
	}

	// Build filter
	filter := &iam.AuditFilter{
		TenantID: globalTenantID,
		Limit:    iamAuditLimit,
		Offset:   iamAuditOffset,
	}

	// Add actor filter
	if iamAuditActor != "" {
		filter.ActorID = iamAuditActor
	}

	// Add resource filter
	if iamAuditResource != "" {
		filter.ResourceID = iamAuditResource
	}

	// Add action filter (event type)
	if iamAuditEventType != "" {
		filter.Action = iamAuditEventType
	}

	// Add outcome filter
	if iamAuditOutcome != "" {
		if iamAuditOutcome != "success" && iamAuditOutcome != "failure" {
			return errors.Newf(errors.ErrInvalidInput, "iam.audit.query", "--outcome must be 'success' or 'failure'")
		}
		var outcome iam.AuditOutcome
		if iamAuditOutcome == "success" {
			outcome = iam.AuditOutcomeSuccess
		} else {
			outcome = iam.AuditOutcomeFailure
		}
		filter.Outcome = &outcome
	}

	// Parse start time
	if iamAuditStartTime != "" {
		startTime, err := time.Parse(time.RFC3339, iamAuditStartTime)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "iam.audit.query", "invalid start-time format, expected RFC3339")
		}
		filter.StartTime = startTime
	}

	// Parse end time
	if iamAuditEndTime != "" {
		endTime, err := time.Parse(time.RFC3339, iamAuditEndTime)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "iam.audit.query", "invalid end-time format, expected RFC3339")
		}
		filter.EndTime = endTime
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Query audit events using SDK sub-client
	log.Debugf("Querying audit events in tenant: %s", globalTenantID)
	result, err := iamClient.Audit().Query(ctx, filter)
	if err != nil {
		return formatError(err)
	}

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)

	default:
		if len(result.Events) == 0 {
			fmt.Println("No audit events found")
			return nil
		}

		fmt.Printf("%-36s %-20s %-25s %-10s %-20s %-20s\n", "EVENT ID", "TIMESTAMP", "ACTION", "OUTCOME", "ACTOR", "RESOURCE")
		fmt.Println(strings.Repeat("-", 140))
		for _, event := range result.Events {
			actor := ""
			if event.Actor != nil {
				actor = truncateString(event.Actor.ID, 20)
			}
			resource := ""
			if event.Resource != nil {
				resource = truncateString(event.Resource.ID, 20)
			}
			fmt.Printf("%-36s %-20s %-25s %-10s %-20s %-20s\n",
				event.ID,
				event.Timestamp.Format("2006-01-02 15:04:05"),
				truncateString(event.Action, 25),
				event.Outcome.String(),
				actor,
				resource)
		}
		fmt.Printf("\nShowing %d of %d events (offset: %d)\n", len(result.Events), result.TotalCount, filter.Offset)
	}

	return nil
}

// newIAMAuditGetCommand creates the audit get command.
func newIAMAuditGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <event-id>",
		Short: "Get details about a specific audit event",
		Long: `Retrieve detailed information about a specific audit event.

Examples:
  # Get audit event details
  quicbaas iam audit get evt_abc123 --tenant acme-corp

  # Get event details as JSON
  quicbaas iam audit get evt_abc123 --tenant acme-corp --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMAuditGetCommand,
	}

	return cmd
}

// runIAMAuditGetCommand executes the audit get command.
func runIAMAuditGetCommand(cmd *cobra.Command, args []string) error {
	eventID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.audit.get", "--tenant flag is required")
	}

	// Create IAM client with token
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Get audit event using SDK sub-client
	log.Debugf("Getting audit event: %s in tenant: %s", eventID, globalTenantID)
	event, err := iamClient.Audit().Get(ctx, globalTenantID, eventID)
	if err != nil {
		return formatError(err)
	}

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(event)

	default:
		fmt.Println("Audit Event Details:")
		fmt.Printf("  ID:         %s\n", event.ID)
		fmt.Printf("  Tenant:     %s\n", event.TenantID)
		fmt.Printf("  Timestamp:  %s\n", event.Timestamp.Format("2006-01-02 15:04:05 MST"))
		fmt.Printf("  Event Type: %s\n", event.EventType.String())
		fmt.Printf("  Action:     %s\n", event.Action)
		fmt.Printf("  Outcome:    %s\n", event.Outcome.String())
		fmt.Printf("  Severity:   %s\n", event.Severity.String())
		fmt.Println()
		fmt.Println("Actor Information:")
		if event.Actor != nil {
			fmt.Printf("  Actor ID:   %s\n", event.Actor.ID)
			fmt.Printf("  Actor Type: %s\n", event.Actor.Type)
			fmt.Printf("  IP Address: %s\n", event.Actor.IPAddress)
			if event.Actor.UserAgent != "" {
				fmt.Printf("  User Agent: %s\n", event.Actor.UserAgent)
			}
		}
		fmt.Println()
		fmt.Println("Resource Information:")
		if event.Resource != nil {
			fmt.Printf("  Resource ID: %s\n", event.Resource.ID)
			fmt.Printf("  Resource Type: %s\n", event.Resource.Type)
		}

		// Print details if present
		if len(event.Details) > 0 {
			fmt.Println()
			fmt.Println("Additional Details:")
			for key, value := range event.Details {
				fmt.Printf("  %s: %v\n", key, value)
			}
		}
	}

	return nil
}

// truncateString truncates a string to the specified length, adding ellipsis if needed.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
