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

// Package main provides IAM role management CLI commands for QuicBaaS.
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
	// Role command flags.
	iamRoleName        string
	iamRoleDescription string
	iamRoleUserID      string
	iamRoleID          string
)

// newIAMRoleCommand creates the role command group.
func newIAMRoleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "role",
		Short: "Role management commands",
		Long: `Manage roles within a tenant.

Roles define collections of permissions that can be assigned to users.
Users can have multiple roles, and roles can be assigned to groups.

All role commands require the --tenant flag to specify the tenant context.`,
	}

	// Add subcommands
	cmd.AddCommand(newIAMRoleCreateCommand())
	cmd.AddCommand(newIAMRoleGetCommand())
	cmd.AddCommand(newIAMRoleUpdateCommand())
	cmd.AddCommand(newIAMRoleDeleteCommand())
	cmd.AddCommand(newIAMRoleListCommand())
	cmd.AddCommand(newIAMRoleAssignCommand())
	cmd.AddCommand(newIAMRoleRevokeCommand())

	return cmd
}

// newIAMRoleCreateCommand creates the role create command.
func newIAMRoleCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <id>",
		Short: "Create a new role",
		Long: `Create a new role within a tenant.

The role ID must be unique within the tenant and will be used for
role assignment operations.

Examples:
  # Create basic role
  quicbaas iam role create team-lead --tenant acme-corp --name "Team Lead"

  # Create role with description
  quicbaas iam role create team-lead --tenant acme-corp --name "Team Lead" --description "Can manage team resources"`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMRoleCreateCommand,
	}

	cmd.Flags().StringVar(&iamRoleName, "name", "", "Role display name (required)")
	cmd.Flags().StringVar(&iamRoleDescription, "description", "", "Role description")
	cmd.MarkFlagRequired("name")

	return cmd
}

// runIAMRoleCreateCommand executes the role create command.
func runIAMRoleCreateCommand(cmd *cobra.Command, args []string) error {
	roleID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.create", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create role using SDK sub-client
	log.Infof("Creating role: %s in tenant: %s", roleID, globalTenantID)
	role, err := iamClient.Role().Create(ctx, globalTenantID, &iam.CreateRoleRequest{
		ID:          roleID,
		Name:        roleID,
		DisplayName: iamRoleName,
		Description: iamRoleDescription,
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
		return encoder.Encode(role)

	default:
		fmt.Println("Role created successfully:")
		fmt.Printf("  ID: %s\n", role.ID)
		fmt.Printf("  Name: %s\n", role.DisplayName)
		fmt.Printf("  Tenant: %s\n", role.TenantID)
		if role.Description != "" {
			fmt.Printf("  Description: %s\n", role.Description)
		}
		fmt.Printf("  Created: %s\n", role.CreatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMRoleGetCommand creates the role get command.
func newIAMRoleGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get role details",
		Long: `Retrieve detailed information about a specific role.

Examples:
  # Get role details
  quicbaas iam role get team-lead --tenant acme-corp

  # Get role as JSON
  quicbaas iam role get team-lead --tenant acme-corp --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMRoleGetCommand,
	}

	return cmd
}

// runIAMRoleGetCommand executes the role get command.
func runIAMRoleGetCommand(cmd *cobra.Command, args []string) error {
	roleID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.get", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Get role using SDK sub-client
	log.Infof("Getting role: %s in tenant: %s", roleID, globalTenantID)
	role, err := iamClient.Role().Get(ctx, globalTenantID, roleID)
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
		return encoder.Encode(role)

	default:
		fmt.Println("Role Details:")
		fmt.Printf("  ID: %s\n", role.ID)
		fmt.Printf("  Name: %s\n", role.DisplayName)
		fmt.Printf("  Tenant: %s\n", role.TenantID)
		fmt.Printf("  Description: %s\n", role.Description)
		fmt.Printf("  Created: %s\n", role.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Updated: %s\n", role.UpdatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMRoleUpdateCommand creates the role update command.
func newIAMRoleUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update role information",
		Long: `Update an existing role's information.

At least one flag must be specified to update the role.

Examples:
  # Update role name
  quicbaas iam role update team-lead --tenant acme-corp --name "Senior Team Lead"

  # Update role description
  quicbaas iam role update team-lead --tenant acme-corp --description "Can manage team and resources"`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMRoleUpdateCommand,
	}

	cmd.Flags().StringVar(&iamRoleName, "name", "", "New role name")
	cmd.Flags().StringVar(&iamRoleDescription, "description", "", "New role description")

	return cmd
}

// runIAMRoleUpdateCommand executes the role update command.
func runIAMRoleUpdateCommand(cmd *cobra.Command, args []string) error {
	roleID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.update", "--tenant flag is required")
	}

	// Check if at least one update flag is provided
	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("description") {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.update", "at least one of --name or --description must be specified")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build update request
	updateReq := &iam.UpdateRoleRequest{}
	if cmd.Flags().Changed("name") {
		updateReq.DisplayName = iamRoleName
	}
	if cmd.Flags().Changed("description") {
		updateReq.Description = iamRoleDescription
	}

	// Update role using SDK sub-client
	log.Infof("Updating role: %s in tenant: %s", roleID, globalTenantID)
	role, err := iamClient.Role().Update(ctx, globalTenantID, roleID, updateReq)
	if err != nil {
		return formatError(err)
	}

	fmt.Println("Role updated successfully:")
	fmt.Printf("  ID: %s\n", role.ID)
	fmt.Printf("  Name: %s\n", role.DisplayName)
	fmt.Printf("  Description: %s\n", role.Description)

	return nil
}

// newIAMRoleDeleteCommand creates the role delete command.
func newIAMRoleDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a role",
		Long: `Delete a role from the tenant.

WARNING: This operation is irreversible and will revoke this role
from all users who have been assigned it.

Examples:
  # Delete role with confirmation
  quicbaas iam role delete team-lead --tenant acme-corp

  # Delete role without confirmation
  quicbaas iam role delete team-lead --tenant acme-corp --yes`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMRoleDeleteCommand,
	}

	return cmd
}

// runIAMRoleDeleteCommand executes the role delete command.
func runIAMRoleDeleteCommand(cmd *cobra.Command, args []string) error {
	roleID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.delete", "--tenant flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("Are you sure you want to delete role '%s'? This cannot be undone. (y/N): ", roleID)
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

	// Delete role using SDK sub-client
	log.Infof("Deleting role: %s from tenant: %s", roleID, globalTenantID)
	if err := iamClient.Role().Delete(ctx, globalTenantID, roleID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Role '%s' deleted successfully\n", roleID)
	return nil
}

// newIAMRoleListCommand creates the role list command.
func newIAMRoleListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List roles in a tenant",
		Long: `List all roles within a tenant.

Examples:
  # List all roles
  quicbaas iam role list --tenant acme-corp

  # List roles as JSON
  quicbaas iam role list --tenant acme-corp --output json`,
		RunE: runIAMRoleListCommand,
	}

	return cmd
}

// runIAMRoleListCommand executes the role list command.
func runIAMRoleListCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.list", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List roles using SDK sub-client
	log.Infof("Listing roles in tenant: %s", globalTenantID)
	resp, err := iamClient.Role().List(ctx, globalTenantID, &iam.ListRolesRequest{})
	if err != nil {
		return formatError(err)
	}

	roles := resp.Roles

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(roles)

	default:
		if len(roles) == 0 {
			fmt.Println("No roles found")
			return nil
		}

		fmt.Printf("%-25s %-30s %-50s %s\n", "ID", "NAME", "DESCRIPTION", "CREATED")
		fmt.Println(strings.Repeat("-", 130))
		for _, role := range roles {
			desc := role.Description
			if len(desc) > 50 {
				desc = desc[:47] + "..."
			}
			fmt.Printf("%-25s %-30s %-50s %s\n",
				role.ID,
				role.DisplayName,
				desc,
				role.CreatedAt.Format("2006-01-02 15:04"))
		}
		fmt.Printf("\nTotal: %d roles\n", len(roles))
	}

	return nil
}

// newIAMRoleAssignCommand creates the role assign command.
func newIAMRoleAssignCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign [<user-id> <role-id>]",
		Short: "Assign a role to a user",
		Long: `Assign a role to a user within a tenant.

You can use either positional arguments or flags:

Examples:
  # Using positional arguments
  quicbaas iam role assign user_abc123 team-lead --tenant acme-corp

  # Using flags
  quicbaas iam role assign --tenant acme-corp --user user_abc123 --role team-lead`,
		Args: cobra.MaximumNArgs(2),
		RunE: runIAMRoleAssignCommand,
	}

	cmd.Flags().StringVar(&iamRoleUserID, "user", "", "User ID to assign role to")
	cmd.Flags().StringVar(&iamRoleID, "role", "", "Role ID to assign")

	return cmd
}

// runIAMRoleAssignCommand executes the role assign command.
func runIAMRoleAssignCommand(cmd *cobra.Command, args []string) error {
	// Support both positional args and flags
	userID := iamRoleUserID
	roleID := iamRoleID

	// If positional args provided, use them
	if len(args) >= 2 {
		userID = args[0]
		roleID = args[1]
	}

	// Validate required values
	if userID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.assign", "user ID is required (use --user flag or positional argument)")
	}
	if roleID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.assign", "role ID is required (use --role flag or positional argument)")
	}
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.assign", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Assign role using SDK sub-client
	log.Infof("Assigning role %s to user %s in tenant: %s", roleID, userID, globalTenantID)
	if err := iamClient.User().AssignRole(ctx, globalTenantID, userID, roleID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Role '%s' assigned to user '%s' successfully\n", roleID, userID)
	return nil
}

// newIAMRoleRevokeCommand creates the role revoke command.
func newIAMRoleRevokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke [<user-id> <role-id>]",
		Short: "Revoke a role from a user",
		Long: `Revoke a role from a user within a tenant.

You can use either positional arguments or flags:

Examples:
  # Using positional arguments
  quicbaas iam role revoke user_abc123 team-lead --tenant acme-corp

  # Using flags
  quicbaas iam role revoke --tenant acme-corp --user user_abc123 --role team-lead`,
		Args: cobra.MaximumNArgs(2),
		RunE: runIAMRoleRevokeCommand,
	}

	cmd.Flags().StringVar(&iamRoleUserID, "user", "", "User ID to revoke role from")
	cmd.Flags().StringVar(&iamRoleID, "role", "", "Role ID to revoke")

	return cmd
}

// runIAMRoleRevokeCommand executes the role revoke command.
func runIAMRoleRevokeCommand(cmd *cobra.Command, args []string) error {
	// Support both positional args and flags
	userID := iamRoleUserID
	roleID := iamRoleID

	// If positional args provided, use them
	if len(args) >= 2 {
		userID = args[0]
		roleID = args[1]
	}

	// Validate required values
	if userID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.revoke", "user ID is required (use --user flag or positional argument)")
	}
	if roleID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.revoke", "role ID is required (use --role flag or positional argument)")
	}
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.role.revoke", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Revoke role using SDK sub-client
	log.Infof("Revoking role %s from user %s in tenant: %s", roleID, userID, globalTenantID)
	if err := iamClient.User().RevokeRole(ctx, globalTenantID, userID, roleID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Role '%s' revoked from user '%s' successfully\n", roleID, userID)
	return nil
}
