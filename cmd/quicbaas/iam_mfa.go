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

// Package main provides IAM MFA CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

var (
	// MFA command flags.
	mfaMethod string
	mfaCode   string
	mfaUserID string
)

// newIAMMFACommand creates the MFA command group.
func newIAMMFACommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mfa",
		Short: "Multi-Factor Authentication management commands",
		Long: `Manage MFA (Multi-Factor Authentication) for users.

MFA provides an additional layer of security by requiring users to verify
their identity using a time-based one-time password (TOTP) from an
authenticator app.

All MFA commands require the --tenant and --user flags to specify the context.`,
	}

	// Add persistent flags for MFA commands
	cmd.PersistentFlags().StringVar(&mfaUserID, "user", "", "User ID (required)")

	// Add subcommands
	cmd.AddCommand(newMFAStatusCommand())
	cmd.AddCommand(newMFAEnrollCommand())
	cmd.AddCommand(newMFAVerifyCommand())
	cmd.AddCommand(newMFADisableCommand())
	cmd.AddCommand(newMFABackupCodesCommand())

	return cmd
}

// newMFAStatusCommand creates the MFA status command.
func newMFAStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get MFA status for a user",
		Long: `Check the MFA enrollment status for a user.

Examples:
  # Get MFA status for a user
  quicbaas iam mfa status --tenant acme-corp --user user_abc123

  # Get MFA status as JSON
  quicbaas iam mfa status --tenant acme-corp --user user_abc123 --output json`,
		RunE: runMFAStatusCommand,
	}

	return cmd
}

// runMFAStatusCommand executes the MFA status command.
func runMFAStatusCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.mfa.status", "--tenant flag is required")
	}
	if mfaUserID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.mfa.status", "--user flag is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Getting MFA status for user: %s in tenant: %s", mfaUserID, globalTenantID)
	status, err := iamClient.GetMFAStatus(ctx, globalTenantID, mfaUserID)
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
		return encoder.Encode(status)

	default:
		fmt.Println("MFA Status:")
		fmt.Printf("  Enabled: %t\n", status.Enabled)
		if status.Method != "" {
			fmt.Printf("  Method: %s\n", status.Method)
		}
		if status.EnrolledAt != nil {
			fmt.Printf("  Enrolled At: %s\n", status.EnrolledAt.Format("2006-01-02 15:04:05"))
		}
		if status.LastVerified != nil {
			fmt.Printf("  Last Verified: %s\n", status.LastVerified.Format("2006-01-02 15:04:05"))
		}
		if status.BackupCodesRemaining > 0 {
			fmt.Printf("  Backup Codes Remaining: %d\n", status.BackupCodesRemaining)
		}
	}

	return nil
}

// newMFAEnrollCommand creates the MFA enroll command.
func newMFAEnrollCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Enroll a user in MFA",
		Long: `Begin MFA enrollment for a user.

This command starts the MFA enrollment process and returns a secret that
can be added to an authenticator app. After adding the secret, use the
'verify' command to complete enrollment.

Examples:
  # Begin MFA enrollment
  quicbaas iam mfa enroll --tenant acme-corp --user user_abc123

  # Begin MFA enrollment with specific method
  quicbaas iam mfa enroll --tenant acme-corp --user user_abc123 --method totp`,
		RunE: runMFAEnrollCommand,
	}

	cmd.Flags().StringVar(&mfaMethod, "method", "totp", "MFA method (totp)")

	return cmd
}

// runMFAEnrollCommand executes the MFA enroll command.
func runMFAEnrollCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.mfa.enroll", "--tenant flag is required")
	}
	if mfaUserID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.mfa.enroll", "--user flag is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Beginning MFA enrollment for user: %s in tenant: %s", mfaUserID, globalTenantID)
	enrollment, err := iamClient.BeginMFAEnrollment(ctx, globalTenantID, mfaUserID, mfaMethod)
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
		return encoder.Encode(enrollment)

	default:
		fmt.Println("MFA Enrollment Started:")
		fmt.Printf("  Enrollment ID: %s\n", enrollment.EnrollmentID)
		fmt.Printf("  Secret: %s\n", enrollment.Secret)
		fmt.Printf("  Provisioning URI: %s\n", enrollment.ProvisioningURI)
		fmt.Printf("  Expires At: %s\n", enrollment.ExpiresAt.Format("2006-01-02 15:04:05"))
		fmt.Println("\nScan the QR code or enter the secret in your authenticator app.")
		fmt.Println("Then use 'quicbaas iam mfa verify' to complete enrollment.")
	}

	return nil
}

// newMFAVerifyCommand creates the MFA verify command.
func newMFAVerifyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <enrollment-id>",
		Short: "Complete MFA enrollment by verifying a code",
		Long: `Complete MFA enrollment by providing a verification code.

After starting enrollment with 'enroll', use this command to verify
the TOTP code from your authenticator app and complete the enrollment.

Examples:
  # Complete MFA enrollment
  quicbaas iam mfa verify enroll_abc123 --code 123456`,
		Args: cobra.ExactArgs(1),
		RunE: runMFAVerifyCommand,
	}

	cmd.Flags().StringVar(&mfaCode, "code", "", "TOTP code from authenticator app (required)")
	cmd.MarkFlagRequired("code")

	return cmd
}

// runMFAVerifyCommand executes the MFA verify command.
func runMFAVerifyCommand(cmd *cobra.Command, args []string) error {
	enrollmentID := args[0]

	if mfaCode == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.mfa.verify", "--code flag is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Completing MFA enrollment: %s", enrollmentID)
	if err := iamClient.CompleteMFAEnrollment(ctx, enrollmentID, mfaCode); err != nil {
		return formatError(err)
	}

	fmt.Println("MFA enrollment completed successfully!")
	fmt.Println("\nIMPORTANT: Save your backup codes! Use 'quicbaas iam mfa backup-codes' to view them.")

	return nil
}

// newMFADisableCommand creates the MFA disable command.
func newMFADisableCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable MFA for a user",
		Long: `Disable MFA for a user.

WARNING: This will remove MFA protection from the user's account.

Examples:
  # Disable MFA for a user
  quicbaas iam mfa disable --tenant acme-corp --user user_abc123

  # Disable MFA without confirmation
  quicbaas iam mfa disable --tenant acme-corp --user user_abc123 --yes`,
		RunE: runMFADisableCommand,
	}

	return cmd
}

// runMFADisableCommand executes the MFA disable command.
func runMFADisableCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.mfa.disable", "--tenant flag is required")
	}
	if mfaUserID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.mfa.disable", "--user flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("Are you sure you want to disable MFA for user '%s'? This will reduce account security. (y/N): ", mfaUserID)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Disable cancelled")
			return nil
		}
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Disabling MFA for user: %s in tenant: %s", mfaUserID, globalTenantID)
	if err := iamClient.DisableMFA(ctx, globalTenantID, mfaUserID); err != nil {
		return formatError(err)
	}

	fmt.Printf("MFA disabled for user '%s'\n", mfaUserID)
	return nil
}

// newMFABackupCodesCommand creates the MFA backup-codes command group.
func newMFABackupCodesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup-codes",
		Short: "Manage backup codes",
		Long: `Manage backup codes for MFA recovery.

Backup codes can be used to access your account if you lose access to
your authenticator app.`,
	}

	cmd.AddCommand(newMFABackupCodesStatusCommand())
	cmd.AddCommand(newMFABackupCodesGenerateCommand())

	return cmd
}

// newMFABackupCodesStatusCommand creates the backup-codes status command.
func newMFABackupCodesStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Get backup code status",
		Long: `Check the status of backup codes for a user.

Examples:
  # Get backup code status
  quicbaas iam mfa backup-codes status --tenant acme-corp --user user_abc123`,
		RunE: runMFABackupCodesStatusCommand,
	}

	return cmd
}

// runMFABackupCodesStatusCommand executes the backup-codes status command.
func runMFABackupCodesStatusCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.mfa.backup-codes.status", "--tenant flag is required")
	}
	if mfaUserID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.mfa.backup-codes.status", "--user flag is required")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Getting backup code status for user: %s in tenant: %s", mfaUserID, globalTenantID)
	status, getErr := iamClient.GetBackupCodeStatus(ctx, globalTenantID, mfaUserID)
	if getErr != nil {
		// If MFA is not enrolled, return a zero-value status instead of
		// an error so callers can handle the response uniformly.
		status = &cli.BackupCodeStatus{
			RemainingCodes: 0,
			TotalCodes:     0,
		}
	}

	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(status)

	default:
		fmt.Println("Backup Code Status:")
		fmt.Printf("  Total Codes: %d\n", status.TotalCodes)
		fmt.Printf("  Remaining Codes: %d\n", status.RemainingCodes)
		if status.GeneratedAt != nil {
			fmt.Printf("  Generated At: %s\n", status.GeneratedAt.Format("2006-01-02 15:04:05"))
		}
		if status.RemainingCodes <= 2 {
			fmt.Println("\nWARNING: Low backup codes remaining! Consider generating new codes.")
		}
	}

	return nil
}

// newMFABackupCodesGenerateCommand creates the backup-codes generate command.
func newMFABackupCodesGenerateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate new backup codes",
		Long: `Generate new backup codes for a user.

WARNING: This will invalidate all existing backup codes.

Examples:
  # Generate new backup codes
  quicbaas iam mfa backup-codes generate --tenant acme-corp --user user_abc123`,
		RunE: runMFABackupCodesGenerateCommand,
	}

	return cmd
}

// runMFABackupCodesGenerateCommand executes the backup-codes generate command.
func runMFABackupCodesGenerateCommand(cmd *cobra.Command, args []string) error {
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.mfa.backup-codes.generate", "--tenant flag is required")
	}
	if mfaUserID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.mfa.backup-codes.generate", "--user flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Print("This will invalidate all existing backup codes. Continue? (y/N): ")
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Generate cancelled")
			return nil
		}
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}
	defer iamClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Generating backup codes for user: %s in tenant: %s", mfaUserID, globalTenantID)
	codes, err := iamClient.GenerateBackupCodes(ctx, globalTenantID, mfaUserID)
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
		return encoder.Encode(map[string][]string{"codes": codes})

	default:
		fmt.Println("New Backup Codes Generated:")
		fmt.Println("\nIMPORTANT: Save these codes in a secure location!")
		fmt.Println("Each code can only be used once.")
		fmt.Println()
		for i, code := range codes {
			fmt.Printf("  %2d. %s\n", i+1, code)
		}
		fmt.Printf("\nTotal: %d codes\n", len(codes))
	}

	return nil
}
