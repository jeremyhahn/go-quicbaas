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

// Package main provides IAM GDPR compliance CLI commands for QuicBaaS.
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
	// Consent command flags.
	gdprUserID  string
	gdprPurpose string
	gdprVersion string

	// Export command flags.
	gdprExportID     string
	gdprExportFormat string

	// Deletion command flags.
	gdprDeletionID      string
	gdprDeletionReason  string
	gdprGracePeriodDays int
)

// newIAMGDPRCommand creates the GDPR command group.
func newIAMGDPRCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gdpr",
		Short: "GDPR compliance commands",
		Long: `Manage GDPR compliance including consent, data export, and deletion.

GDPR (General Data Protection Regulation) compliance features help manage
user consent, provide data portability through exports, and support the
right to be forgotten through data deletion.

Consent Purposes:
  - marketing        Marketing communications
  - analytics        Analytics and usage tracking
  - personalization  Personalized experiences
  - third_party      Sharing data with third parties
  - data_processing  General data processing
  - data_retention   Extended data retention
  - functional       Functional features and cookies

Export Formats:
  - json  Export data as JSON (default)
  - csv   Export data as CSV
  - xml   Export data as XML

Examples:
  # Grant consent for marketing
  quicbaas iam gdpr consent grant --tenant acme-corp --user-id user_abc \
    --purpose marketing --version 1.0

  # Request data export
  quicbaas iam gdpr export request --tenant acme-corp --user-id user_abc \
    --format json

  # Request data deletion
  quicbaas iam gdpr deletion request --tenant acme-corp --user-id user_abc \
    --reason "User account closure"`,
	}

	// Add subcommand groups
	cmd.AddCommand(newIAMGDPRConsentCommand())
	cmd.AddCommand(newIAMGDPRExportCommand())
	cmd.AddCommand(newIAMGDPRDeletionCommand())

	return cmd
}

// newIAMGDPRConsentCommand creates the consent command group.
func newIAMGDPRConsentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "consent",
		Short: "Manage user consent",
		Long: `Grant, withdraw, and query user consent for various purposes.

Consent records track user agreement to specific data processing purposes
and can be revoked at any time.`,
	}

	cmd.AddCommand(newIAMGDPRConsentGrantCommand())
	cmd.AddCommand(newIAMGDPRConsentWithdrawCommand())
	cmd.AddCommand(newIAMGDPRConsentGetCommand())
	cmd.AddCommand(newIAMGDPRConsentListCommand())

	return cmd
}

// newIAMGDPRConsentGrantCommand creates the consent grant command.
func newIAMGDPRConsentGrantCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grant",
		Short: "Grant consent for a purpose",
		Long: `Record user consent for a specific purpose.

Purposes: marketing, analytics, personalization, third_party,
         data_processing, data_retention, functional

Examples:
  # Grant marketing consent
  quicbaas iam gdpr consent grant --tenant acme-corp --user-id user_abc \
    --purpose marketing --version 1.0

  # Grant analytics consent
  quicbaas iam gdpr consent grant --tenant acme-corp --user-id user_abc \
    --purpose analytics --version 2.1`,
		RunE: runIAMGDPRConsentGrantCommand,
	}

	cmd.Flags().StringVar(&gdprUserID, "user-id", "", "User ID (required)")
	cmd.Flags().StringVar(&gdprPurpose, "purpose", "", "Consent purpose (required)")
	cmd.Flags().StringVar(&gdprVersion, "version", "", "Consent policy version (required)")

	cmd.MarkFlagRequired("user-id")
	cmd.MarkFlagRequired("purpose")
	cmd.MarkFlagRequired("version")

	return cmd
}

// runIAMGDPRConsentGrantCommand executes the consent grant command.
func runIAMGDPRConsentGrantCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.gdpr.consent.grant", "--tenant flag is required")
	}

	// Validate purpose
	validPurposes := []string{"marketing", "analytics", "personalization", "third_party", "data_processing", "data_retention", "functional"}
	purposeValid := false
	for _, p := range validPurposes {
		if gdprPurpose == p {
			purposeValid = true
			break
		}
	}
	if !purposeValid {
		return errors.Newf(errors.ErrInvalidInput, "iam.gdpr.consent.grant", "invalid purpose: %s. Must be one of: %s", gdprPurpose, strings.Join(validPurposes, ", "))
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Granting consent for user %s, purpose: %s", gdprUserID, gdprPurpose)

	// Grant consent using SDK sub-client
	if err := iamClient.GDPR().GrantConsent(ctx, globalTenantID, gdprUserID, &iam.GrantConsentRequest{
		Purpose: gdprPurpose,
		Version: gdprVersion,
	}); err != nil {
		return formatError(err)
	}

	// Attempt to retrieve the consent for display. If retrieval fails,
	// the grant itself succeeded so report success with available info.
	consent, getErr := iamClient.GDPR().GetConsent(ctx, globalTenantID, gdprUserID, gdprPurpose)
	if getErr != nil {
		format, fmtErr := cli.ParseOutputFormat(iamOutputFormat)
		if fmtErr != nil {
			return fmtErr
		}
		switch format {
		case cli.OutputFormatJSON:
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(map[string]string{
				"user_id": gdprUserID,
				"tenant":  globalTenantID,
				"purpose": gdprPurpose,
				"status":  "granted",
			})
		default:
			fmt.Println("Consent Granted:")
			fmt.Printf("  User ID:   %s\n", gdprUserID)
			fmt.Printf("  Tenant:    %s\n", globalTenantID)
			fmt.Printf("  Purpose:   %s\n", gdprPurpose)
			fmt.Printf("  Status:    granted\n")
		}
		return nil
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(consent)

	default:
		fmt.Println("Consent Granted:")
		fmt.Printf("  ID:        %s\n", consent.ID)
		fmt.Printf("  User ID:   %s\n", consent.UserID)
		fmt.Printf("  Tenant:    %s\n", consent.TenantID)
		fmt.Printf("  Purpose:   %s\n", consent.Purpose)
		fmt.Printf("  Status:    %s\n", consent.Status.String())
		fmt.Printf("  Granted:   %s\n", consent.GrantedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMGDPRConsentWithdrawCommand creates the consent withdraw command.
func newIAMGDPRConsentWithdrawCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "withdraw",
		Short: "Withdraw consent for a purpose",
		Long: `Withdraw previously granted consent for a specific purpose.

Examples:
  # Withdraw marketing consent
  quicbaas iam gdpr consent withdraw --tenant acme-corp --user-id user_abc \
    --purpose marketing`,
		RunE: runIAMGDPRConsentWithdrawCommand,
	}

	cmd.Flags().StringVar(&gdprUserID, "user-id", "", "User ID (required)")
	cmd.Flags().StringVar(&gdprPurpose, "purpose", "", "Consent purpose (required)")

	cmd.MarkFlagRequired("user-id")
	cmd.MarkFlagRequired("purpose")

	return cmd
}

// runIAMGDPRConsentWithdrawCommand executes the consent withdraw command.
func runIAMGDPRConsentWithdrawCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.gdpr.consent.withdraw", "--tenant flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("Are you sure you want to withdraw consent for '%s'? (y/N): ", gdprPurpose)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Withdraw cancelled")
			return nil
		}
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Withdrawing consent for user %s, purpose: %s", gdprUserID, gdprPurpose)

	// Withdraw consent using SDK sub-client
	if err := iamClient.GDPR().WithdrawConsent(ctx, globalTenantID, gdprUserID, gdprPurpose); err != nil {
		return formatError(err)
	}

	fmt.Printf("Consent for '%s' withdrawn successfully\n", gdprPurpose)
	return nil
}

// newIAMGDPRConsentGetCommand creates the consent get command.
func newIAMGDPRConsentGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get consent status for a purpose",
		Long: `Retrieve the consent status for a specific purpose.

Examples:
  # Get marketing consent status
  quicbaas iam gdpr consent get --tenant acme-corp --user-id user_abc \
    --purpose marketing`,
		RunE: runIAMGDPRConsentGetCommand,
	}

	cmd.Flags().StringVar(&gdprUserID, "user-id", "", "User ID (required)")
	cmd.Flags().StringVar(&gdprPurpose, "purpose", "", "Consent purpose (required)")

	cmd.MarkFlagRequired("user-id")
	cmd.MarkFlagRequired("purpose")

	return cmd
}

// runIAMGDPRConsentGetCommand executes the consent get command.
func runIAMGDPRConsentGetCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.gdpr.consent.get", "--tenant flag is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Getting consent for user %s, purpose: %s", gdprUserID, gdprPurpose)

	// Get consent using SDK sub-client
	consent, err := iamClient.GDPR().GetConsent(ctx, globalTenantID, gdprUserID, gdprPurpose)
	if err != nil {
		return formatError(err)
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(consent)

	default:
		fmt.Println("Consent Details:")
		fmt.Printf("  ID:        %s\n", consent.ID)
		fmt.Printf("  User ID:   %s\n", consent.UserID)
		fmt.Printf("  Tenant:    %s\n", consent.TenantID)
		fmt.Printf("  Purpose:   %s\n", consent.Purpose)
		fmt.Printf("  Status:    %s\n", consent.Status.String())
		if !consent.GrantedAt.IsZero() {
			fmt.Printf("  Granted:   %s\n", consent.GrantedAt.Format("2006-01-02 15:04:05"))
		}
		if !consent.WithdrawnAt.IsZero() {
			fmt.Printf("  Withdrawn: %s\n", consent.WithdrawnAt.Format("2006-01-02 15:04:05"))
		}
		if !consent.ExpiresAt.IsZero() {
			fmt.Printf("  Expires:   %s\n", consent.ExpiresAt.Format("2006-01-02 15:04:05"))
		}
	}

	return nil
}

// newIAMGDPRConsentListCommand creates the consent list command.
func newIAMGDPRConsentListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all consents for a user",
		Long: `List all consent records for a user.

Examples:
  # List all consents for a user
  quicbaas iam gdpr consent list --tenant acme-corp --user-id user_abc`,
		RunE: runIAMGDPRConsentListCommand,
	}

	cmd.Flags().StringVar(&gdprUserID, "user-id", "", "User ID (required)")
	cmd.MarkFlagRequired("user-id")

	return cmd
}

// runIAMGDPRConsentListCommand executes the consent list command.
func runIAMGDPRConsentListCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.gdpr.consent.list", "--tenant flag is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Listing consents for user: %s", gdprUserID)

	// List consents using SDK sub-client
	resp, err := iamClient.GDPR().ListConsents(ctx, globalTenantID, gdprUserID)
	if err != nil {
		return formatError(err)
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp.Consents)

	default:
		if len(resp.Consents) == 0 {
			fmt.Println("No consent records found")
			return nil
		}

		fmt.Printf("%-20s %-12s %-20s %-20s\n", "PURPOSE", "STATUS", "GRANTED", "WITHDRAWN")
		fmt.Println(strings.Repeat("-", 85))
		for _, c := range resp.Consents {
			granted := "-"
			if !c.GrantedAt.IsZero() {
				granted = c.GrantedAt.Format("2006-01-02 15:04")
			}
			withdrawn := "-"
			if !c.WithdrawnAt.IsZero() {
				withdrawn = c.WithdrawnAt.Format("2006-01-02 15:04")
			}
			fmt.Printf("%-20s %-12s %-20s %-20s\n",
				c.Purpose,
				c.Status.String(),
				granted,
				withdrawn)
		}
		fmt.Printf("\nTotal: %d consent records\n", len(resp.Consents))
	}

	return nil
}

// newIAMGDPRExportCommand creates the export command group.
func newIAMGDPRExportCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Manage data export requests",
		Long: `Request and manage user data exports for data portability.

Data exports allow users to download their personal data in machine-readable
formats as required by GDPR Article 20.`,
	}

	cmd.AddCommand(newIAMGDPRExportRequestCommand())
	cmd.AddCommand(newIAMGDPRExportStatusCommand())

	return cmd
}

// newIAMGDPRExportRequestCommand creates the export request command.
func newIAMGDPRExportRequestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Request a data export",
		Long: `Request a data export for a user.

The export will be generated asynchronously. Use 'export status' to check
progress and obtain the download link.

Formats: json (default), csv, xml

Examples:
  # Request JSON export
  quicbaas iam gdpr export request --tenant acme-corp --user-id user_abc

  # Request CSV export
  quicbaas iam gdpr export request --tenant acme-corp --user-id user_abc \
    --format csv`,
		RunE: runIAMGDPRExportRequestCommand,
	}

	cmd.Flags().StringVar(&gdprUserID, "user-id", "", "User ID (required)")
	cmd.Flags().StringVar(&gdprExportFormat, "format", "json", "Export format: json, csv, xml")

	cmd.MarkFlagRequired("user-id")

	return cmd
}

// runIAMGDPRExportRequestCommand executes the export request command.
func runIAMGDPRExportRequestCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.gdpr.export.request", "--tenant flag is required")
	}

	// Validate format
	validFormats := []string{"json", "csv", "xml"}
	formatValid := false
	for _, f := range validFormats {
		if gdprExportFormat == f {
			formatValid = true
			break
		}
	}
	if !formatValid {
		return errors.Newf(errors.ErrInvalidInput, "iam.gdpr.export.request", "invalid format: %s. Must be one of: %s", gdprExportFormat, strings.Join(validFormats, ", "))
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Requesting data export for user: %s, format: %s", gdprUserID, gdprExportFormat)

	// Determine export format
	var exportFormat iam.ExportFormat
	switch gdprExportFormat {
	case "json":
		exportFormat = iam.ExportFormatJSON
	case "csv":
		exportFormat = iam.ExportFormatCSV
	case "xml":
		exportFormat = iam.ExportFormatXML
	}

	// Request export using SDK sub-client
	exportReq, err := iamClient.GDPR().RequestExport(ctx, globalTenantID, gdprUserID, &iam.ExportRequest{
		Format: exportFormat,
	})
	if err != nil {
		return formatError(err)
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(exportReq)

	default:
		fmt.Println("Data Export Request Created:")
		fmt.Printf("  Export ID:    %s\n", exportReq.ID)
		fmt.Printf("  User ID:      %s\n", exportReq.UserID)
		fmt.Printf("  Tenant:       %s\n", exportReq.TenantID)
		fmt.Printf("  Format:       %s\n", exportReq.Format.String())
		fmt.Printf("  Status:       %s\n", exportReq.Status.String())
		fmt.Printf("  Requested:    %s\n", exportReq.RequestedAt.Format("2006-01-02 15:04:05"))
		fmt.Println("\nUse 'quicbaas iam gdpr export status --export-id " + exportReq.ID + "' to check progress.")
	}

	return nil
}

// newIAMGDPRExportStatusCommand creates the export status command.
func newIAMGDPRExportStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get export status",
		Long: `Get the status of a data export request.

Examples:
  # Check export status
  quicbaas iam gdpr export status --export-id exp_abc123 --user-id user_abc`,
		RunE: runIAMGDPRExportStatusCommand,
	}

	cmd.Flags().StringVar(&gdprExportID, "export-id", "", "Export request ID (required)")
	cmd.Flags().StringVar(&gdprUserID, "user-id", "", "User ID (required)")
	cmd.MarkFlagRequired("export-id")
	cmd.MarkFlagRequired("user-id")

	return cmd
}

// runIAMGDPRExportStatusCommand executes the export status command.
func runIAMGDPRExportStatusCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.gdpr.export.status", "--tenant flag is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Getting export status: %s", gdprExportID)

	// Get export status using SDK sub-client
	exportReq, err := iamClient.GDPR().GetExportStatus(ctx, globalTenantID, gdprUserID, gdprExportID)
	if err != nil {
		return formatError(err)
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(exportReq)

	default:
		fmt.Println("Data Export Status:")
		fmt.Printf("  Export ID:    %s\n", exportReq.ID)
		fmt.Printf("  User ID:      %s\n", exportReq.UserID)
		fmt.Printf("  Tenant:       %s\n", exportReq.TenantID)
		fmt.Printf("  Format:       %s\n", exportReq.Format.String())
		fmt.Printf("  Status:       %s\n", exportReq.Status.String())
		fmt.Printf("  Requested:    %s\n", exportReq.RequestedAt.Format("2006-01-02 15:04:05"))

		if !exportReq.CompletedAt.IsZero() {
			fmt.Printf("  Completed:    %s\n", exportReq.CompletedAt.Format("2006-01-02 15:04:05"))
		}
		if exportReq.DownloadURL != "" {
			fmt.Printf("  Download URL: %s\n", exportReq.DownloadURL)
		}
		if !exportReq.ExpiresAt.IsZero() {
			fmt.Printf("  Link Expires: %s\n", exportReq.ExpiresAt.Format("2006-01-02 15:04:05"))
		}
		if exportReq.SizeBytes > 0 {
			fmt.Printf("  File Size:    %d bytes\n", exportReq.SizeBytes)
		}
		if exportReq.ErrorMessage != "" {
			fmt.Printf("  Error:        %s\n", exportReq.ErrorMessage)
		}
	}

	return nil
}

// newIAMGDPRDeletionCommand creates the deletion command group.
func newIAMGDPRDeletionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deletion",
		Short: "Manage data deletion requests",
		Long: `Request and manage user data deletion for the right to be forgotten.

Data deletion requests support a grace period (default 30 days) during which
the deletion can be cancelled. After the grace period, data is permanently
deleted.`,
	}

	cmd.AddCommand(newIAMGDPRDeletionRequestCommand())
	cmd.AddCommand(newIAMGDPRDeletionCancelCommand())
	cmd.AddCommand(newIAMGDPRDeletionStatusCommand())

	return cmd
}

// newIAMGDPRDeletionRequestCommand creates the deletion request command.
func newIAMGDPRDeletionRequestCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Request data deletion",
		Long: `Request deletion of user data.

The deletion includes a grace period (default 30 days) during which the
request can be cancelled. After the grace period, data is permanently deleted.

Examples:
  # Request deletion with default grace period
  quicbaas iam gdpr deletion request --tenant acme-corp --user-id user_abc \
    --reason "User account closure"

  # Request deletion with custom grace period
  quicbaas iam gdpr deletion request --tenant acme-corp --user-id user_abc \
    --reason "GDPR request" --grace-period-days 14`,
		RunE: runIAMGDPRDeletionRequestCommand,
	}

	cmd.Flags().StringVar(&gdprUserID, "user-id", "", "User ID (required)")
	cmd.Flags().StringVar(&gdprDeletionReason, "reason", "", "Reason for deletion (required)")
	cmd.Flags().IntVar(&gdprGracePeriodDays, "grace-period-days", 30, "Grace period before permanent deletion")

	cmd.MarkFlagRequired("user-id")
	cmd.MarkFlagRequired("reason")

	return cmd
}

// runIAMGDPRDeletionRequestCommand executes the deletion request command.
func runIAMGDPRDeletionRequestCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.gdpr.deletion.request", "--tenant flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("WARNING: This will schedule permanent deletion of user '%s' data.\n", gdprUserID)
		fmt.Printf("Deletion will occur after %d day(s) grace period.\n", gdprGracePeriodDays)
		fmt.Print("Are you sure you want to proceed? (y/N): ")
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Deletion request cancelled")
			return nil
		}
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Requesting data deletion for user: %s", gdprUserID)

	// Calculate scheduled deletion time
	scheduledFor := time.Now().AddDate(0, 0, gdprGracePeriodDays)

	// Request deletion using SDK sub-client
	deletionReq, err := iamClient.GDPR().RequestDeletion(ctx, globalTenantID, gdprUserID, &iam.DeletionRequest{
		Reason:       gdprDeletionReason,
		ScheduledFor: scheduledFor,
	})
	if err != nil {
		return formatError(err)
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(deletionReq)

	default:
		fmt.Println("Data Deletion Request Created:")
		fmt.Printf("  Deletion ID:         %s\n", deletionReq.ID)
		fmt.Printf("  User ID:             %s\n", deletionReq.UserID)
		fmt.Printf("  Tenant:              %s\n", deletionReq.TenantID)
		fmt.Printf("  Status:              %s\n", deletionReq.Status.String())
		fmt.Printf("  Reason:              %s\n", deletionReq.Reason)
		fmt.Printf("  Requested:           %s\n", deletionReq.RequestedAt.Format("2006-01-02 15:04:05"))
		if !deletionReq.ScheduledFor.IsZero() {
			fmt.Printf("  Scheduled For:       %s\n", deletionReq.ScheduledFor.Format("2006-01-02 15:04:05"))
		}
		fmt.Println("\nTo cancel this request, use: quicbaas iam gdpr deletion cancel --deletion-id " + deletionReq.ID)
	}

	return nil
}

// newIAMGDPRDeletionCancelCommand creates the deletion cancel command.
func newIAMGDPRDeletionCancelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cancel",
		Short: "Cancel a deletion request",
		Long: `Cancel a pending deletion request during the grace period.

Once the grace period has ended and deletion has started, it cannot be cancelled.

Examples:
  # Cancel a deletion request
  quicbaas iam gdpr deletion cancel --deletion-id del_abc123 --user-id user_abc`,
		RunE: runIAMGDPRDeletionCancelCommand,
	}

	cmd.Flags().StringVar(&gdprDeletionID, "deletion-id", "", "Deletion request ID (required)")
	cmd.Flags().StringVar(&gdprUserID, "user-id", "", "User ID (required)")
	cmd.MarkFlagRequired("deletion-id")
	cmd.MarkFlagRequired("user-id")

	return cmd
}

// runIAMGDPRDeletionCancelCommand executes the deletion cancel command.
func runIAMGDPRDeletionCancelCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.gdpr.deletion.cancel", "--tenant flag is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Cancelling deletion request: %s", gdprDeletionID)

	// Cancel deletion using SDK sub-client
	if err := iamClient.GDPR().CancelDeletion(ctx, globalTenantID, gdprUserID, gdprDeletionID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Deletion request '%s' cancelled successfully\n", gdprDeletionID)
	return nil
}

// newIAMGDPRDeletionStatusCommand creates the deletion status command.
func newIAMGDPRDeletionStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get deletion status",
		Long: `Get the status of a data deletion request.

Examples:
  # Check deletion status
  quicbaas iam gdpr deletion status --deletion-id del_abc123 --user-id user_abc`,
		RunE: runIAMGDPRDeletionStatusCommand,
	}

	cmd.Flags().StringVar(&gdprDeletionID, "deletion-id", "", "Deletion request ID (required)")
	cmd.Flags().StringVar(&gdprUserID, "user-id", "", "User ID (required)")
	cmd.MarkFlagRequired("deletion-id")
	cmd.MarkFlagRequired("user-id")

	return cmd
}

// runIAMGDPRDeletionStatusCommand executes the deletion status command.
func runIAMGDPRDeletionStatusCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.gdpr.deletion.status", "--tenant flag is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Getting deletion status: %s", gdprDeletionID)

	// Get deletion status using SDK sub-client
	deletionReq, err := iamClient.GDPR().GetDeletionStatus(ctx, globalTenantID, gdprUserID, gdprDeletionID)
	if err != nil {
		return formatError(err)
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(deletionReq)

	default:
		fmt.Println("Data Deletion Status:")
		fmt.Printf("  Deletion ID:         %s\n", deletionReq.ID)
		fmt.Printf("  User ID:             %s\n", deletionReq.UserID)
		fmt.Printf("  Tenant:              %s\n", deletionReq.TenantID)
		fmt.Printf("  Status:              %s\n", deletionReq.Status.String())
		fmt.Printf("  Reason:              %s\n", deletionReq.Reason)
		fmt.Printf("  Requested:           %s\n", deletionReq.RequestedAt.Format("2006-01-02 15:04:05"))

		if !deletionReq.ScheduledFor.IsZero() {
			fmt.Printf("  Scheduled For:       %s\n", deletionReq.ScheduledFor.Format("2006-01-02 15:04:05"))
		}
		if !deletionReq.ProcessingStartedAt.IsZero() {
			fmt.Printf("  Started:             %s\n", deletionReq.ProcessingStartedAt.Format("2006-01-02 15:04:05"))
		}
		if !deletionReq.CompletedAt.IsZero() {
			fmt.Printf("  Completed:           %s\n", deletionReq.CompletedAt.Format("2006-01-02 15:04:05"))
		}
		if !deletionReq.CancelledAt.IsZero() {
			fmt.Printf("  Cancelled:           %s\n", deletionReq.CancelledAt.Format("2006-01-02 15:04:05"))
		}
		if deletionReq.ErrorMessage != "" {
			fmt.Printf("  Error:               %s\n", deletionReq.ErrorMessage)
		}
	}

	return nil
}
