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

// Package main provides IAM tenant management CLI commands for QuicBaaS.
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
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

var (
	// Tenant command flags.
	tenantName        string
	tenantMaxUsers    int
	tenantMFARequired bool
)

// newIAMTenantCommand creates the tenant command group.
func newIAMTenantCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tenant",
		Short: "Tenant management commands",
		Long: `Manage tenants in the QuicBaaS IAM system.

Tenants provide isolation and multi-tenancy capabilities, allowing
multiple organizations or customers to share a QuicBaaS cluster
while maintaining complete data and access control separation.

Each tenant has its own set of users, groups, roles, and policies.`,
	}

	// Add subcommands
	cmd.AddCommand(newIAMTenantCreateCommand())
	cmd.AddCommand(newIAMTenantGetCommand())
	cmd.AddCommand(newIAMTenantUpdateCommand())
	cmd.AddCommand(newIAMTenantDeleteCommand())
	cmd.AddCommand(newIAMTenantListCommand())

	return cmd
}

// newIAMTenantCreateCommand creates the tenant create command.
func newIAMTenantCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <id>",
		Short: "Create a new tenant",
		Long: `Create a new tenant in the QuicBaaS IAM system.

The tenant ID must be unique and will be used to namespace all resources
within this tenant. The tenant name is a human-readable display name.

Examples:
  # Create basic tenant
  quicbaas iam tenant create acme-corp --name "Acme Corporation"

  # Create tenant with settings
  quicbaas iam tenant create acme-corp --name "Acme Corp" --max-users 1000 --mfa-required

  # Create tenant with JSON output
  quicbaas iam tenant create acme-corp --name "Acme Corp" --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMTenantCreateCommand,
	}

	cmd.Flags().StringVar(&tenantName, "name", "", "Tenant display name (required)")
	cmd.Flags().IntVar(&tenantMaxUsers, "max-users", 0, "Maximum number of users (0 = unlimited)")
	cmd.Flags().BoolVar(&tenantMFARequired, "mfa-required", false, "Require MFA for all users")
	cmd.MarkFlagRequired("name")

	return cmd
}

// runIAMTenantCreateCommand executes the tenant create command.
func runIAMTenantCreateCommand(cmd *cobra.Command, args []string) error {
	tenantID := args[0]

	// Validate tenant ID
	if tenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.tenant.create", "tenant ID is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build settings map for MFA requirement
	settings := make(map[string]string)
	if tenantMFARequired {
		settings["mfa_required"] = "true"
	}
	if tenantMaxUsers > 0 {
		settings["max_users"] = fmt.Sprintf("%d", tenantMaxUsers)
	}

	// Create tenant using SDK sub-client
	log.Debugf("Creating tenant: %s", tenantID)
	tenant, err := iamClient.Tenant().Create(ctx, &iam.CreateTenantRequest{
		ID:          tenantID,
		Name:        tenantName,
		DisplayName: tenantName,
		Settings:    settings,
	})
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
		return encoder.Encode(tenant)

	default:
		fmt.Println("Tenant created successfully:")
		fmt.Printf("  ID: %s\n", tenant.ID)
		fmt.Printf("  Name: %s\n", tenant.Name)
		if tenantMaxUsers > 0 {
			fmt.Printf("  Max Users: %d\n", tenantMaxUsers)
		}
		fmt.Printf("  MFA Required: %v\n", tenantMFARequired)
		fmt.Printf("  Created: %s\n", tenant.CreatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMTenantGetCommand creates the tenant get command.
func newIAMTenantGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get tenant details",
		Long: `Retrieve detailed information about a specific tenant.

Examples:
  # Get tenant details
  quicbaas iam tenant get acme-corp

  # Get tenant as JSON
  quicbaas iam tenant get acme-corp --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMTenantGetCommand,
	}

	return cmd
}

// runIAMTenantGetCommand executes the tenant get command.
func runIAMTenantGetCommand(cmd *cobra.Command, args []string) error {
	tenantID := args[0]

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Get tenant using SDK sub-client
	log.Debugf("Getting tenant: %s", tenantID)
	tenant, err := iamClient.Tenant().Get(ctx, tenantID)
	if err != nil {
		return formatError(err)
	}

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	// Extract settings for display
	maxUsers := 0
	mfaRequired := false
	if tenant.Settings != nil {
		if v, ok := tenant.Settings["max_users"]; ok {
			fmt.Sscanf(v, "%d", &maxUsers)
		}
		if v, ok := tenant.Settings["mfa_required"]; ok {
			mfaRequired = v == "true"
		}
	}
	if tenant.Quotas != nil && tenant.Quotas.MaxUsers > 0 {
		maxUsers = tenant.Quotas.MaxUsers
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(tenant)

	default:
		fmt.Println("Tenant Details:")
		fmt.Printf("  ID: %s\n", tenant.ID)
		fmt.Printf("  Name: %s\n", tenant.Name)
		fmt.Printf("  Max Users: %d\n", maxUsers)
		fmt.Printf("  MFA Required: %v\n", mfaRequired)
		fmt.Printf("  Created: %s\n", tenant.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Updated: %s\n", tenant.UpdatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMTenantUpdateCommand creates the tenant update command.
func newIAMTenantUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update tenant settings",
		Long: `Update an existing tenant's settings.

At least one flag must be specified to update the tenant.

Examples:
  # Update tenant name
  quicbaas iam tenant update acme-corp --name "Acme Corporation Inc."

  # Update MFA requirement
  quicbaas iam tenant update acme-corp --mfa-required

  # Update max users
  quicbaas iam tenant update acme-corp --max-users 5000`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMTenantUpdateCommand,
	}

	cmd.Flags().StringVar(&tenantName, "name", "", "New tenant name")
	cmd.Flags().IntVar(&tenantMaxUsers, "max-users", -1, "Maximum number of users (0 = unlimited)")
	cmd.Flags().BoolVar(&tenantMFARequired, "mfa-required", false, "Require MFA for all users")

	return cmd
}

// runIAMTenantUpdateCommand executes the tenant update command.
func runIAMTenantUpdateCommand(cmd *cobra.Command, args []string) error {
	tenantID := args[0]

	// Check if at least one update flag is provided
	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("max-users") && !cmd.Flags().Changed("mfa-required") {
		return errors.Newf(errors.ErrInvalidInput, "iam.tenant.update", "at least one of --name, --max-users, or --mfa-required must be specified")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build update request - fetch current tenant name if --name was not specified,
	// since the server requires name to be non-empty on update.
	updateReq := &iam.UpdateTenantRequest{}
	if cmd.Flags().Changed("name") {
		updateReq.DisplayName = tenantName
	} else {
		currentTenant, fetchErr := iamClient.Tenant().Get(ctx, tenantID)
		if fetchErr != nil {
			return formatError(fetchErr)
		}
		updateReq.DisplayName = currentTenant.DisplayName
		if updateReq.DisplayName == "" {
			updateReq.DisplayName = currentTenant.Name
		}
	}

	// Build settings map for MFA and max users
	settings := make(map[string]string)
	if cmd.Flags().Changed("mfa-required") {
		settings["mfa_required"] = fmt.Sprintf("%t", tenantMFARequired)
	}
	if cmd.Flags().Changed("max-users") {
		settings["max_users"] = fmt.Sprintf("%d", tenantMaxUsers)
	}
	if len(settings) > 0 {
		updateReq.Settings = settings
	}

	// Update tenant using SDK sub-client
	log.Debugf("Updating tenant: %s", tenantID)
	tenant, err := iamClient.Tenant().Update(ctx, tenantID, updateReq)
	if err != nil {
		return formatError(err)
	}

	// Extract settings for display
	maxUsers := 0
	mfaRequired := false
	if tenant.Settings != nil {
		if v, ok := tenant.Settings["max_users"]; ok {
			fmt.Sscanf(v, "%d", &maxUsers)
		}
		if v, ok := tenant.Settings["mfa_required"]; ok {
			mfaRequired = v == "true"
		}
	}
	if tenant.Quotas != nil && tenant.Quotas.MaxUsers > 0 {
		maxUsers = tenant.Quotas.MaxUsers
	}

	fmt.Println("Tenant updated successfully:")
	fmt.Printf("  ID: %s\n", tenant.ID)
	fmt.Printf("  Name: %s\n", tenant.Name)
	fmt.Printf("  Max Users: %d\n", maxUsers)
	fmt.Printf("  MFA Required: %v\n", mfaRequired)

	return nil
}

// newIAMTenantDeleteCommand creates the tenant delete command.
func newIAMTenantDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a tenant",
		Long: `Delete a tenant from the QuicBaaS IAM system.

WARNING: This operation is irreversible and will delete all users, groups,
roles, applications, and policies associated with this tenant.

Examples:
  # Delete tenant with confirmation
  quicbaas iam tenant delete acme-corp

  # Delete tenant without confirmation
  quicbaas iam tenant delete acme-corp --yes`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMTenantDeleteCommand,
	}

	return cmd
}

// runIAMTenantDeleteCommand executes the tenant delete command.
func runIAMTenantDeleteCommand(cmd *cobra.Command, args []string) error {
	tenantID := args[0]

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("WARNING: This will delete tenant '%s' and all associated resources.\n", tenantID)
		fmt.Printf("This action cannot be undone. Are you sure? (y/N): ")
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Delete cancelled")
			return nil
		}
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Delete tenant using SDK sub-client
	log.Debugf("Deleting tenant: %s", tenantID)
	if err := iamClient.Tenant().Delete(ctx, tenantID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Tenant '%s' deleted successfully\n", tenantID)
	return nil
}

// newIAMTenantListCommand creates the tenant list command.
func newIAMTenantListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all tenants",
		Long: `List all tenants in the QuicBaaS IAM system.

Examples:
  # List all tenants
  quicbaas iam tenant list

  # List tenants as JSON
  quicbaas iam tenant list --output json`,
		RunE: runIAMTenantListCommand,
	}

	return cmd
}

// runIAMTenantListCommand executes the tenant list command.
func runIAMTenantListCommand(cmd *cobra.Command, args []string) error {
	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List tenants using SDK sub-client
	log.Info("Listing tenants")
	resp, err := iamClient.Tenant().List(ctx, &iam.ListTenantsRequest{})
	if err != nil {
		return formatError(err)
	}

	tenants := resp.Tenants

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(tenants)

	default:
		if len(tenants) == 0 {
			fmt.Println("No tenants found")
			return nil
		}

		fmt.Printf("%-30s %-40s %-10s %-12s %s\n", "ID", "NAME", "MAX USERS", "MFA REQUIRED", "CREATED")
		fmt.Println(strings.Repeat("-", 120))
		for _, tenant := range tenants {
			maxUsers := "unlimited"
			mfaRequired := false
			if tenant.Settings != nil {
				if v, ok := tenant.Settings["max_users"]; ok && v != "0" {
					maxUsers = v
				}
				if v, ok := tenant.Settings["mfa_required"]; ok {
					mfaRequired = v == "true"
				}
			}
			if tenant.Quotas != nil && tenant.Quotas.MaxUsers > 0 {
				maxUsers = fmt.Sprintf("%d", tenant.Quotas.MaxUsers)
			}
			fmt.Printf("%-30s %-40s %-10s %-12v %s\n",
				tenant.ID,
				tenant.Name,
				maxUsers,
				mfaRequired,
				tenant.CreatedAt.Format("2006-01-02 15:04"))
		}
		fmt.Printf("\nTotal: %d tenants\n", len(tenants))
	}

	return nil
}
