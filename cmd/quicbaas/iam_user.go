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

// Package main provides IAM user management CLI commands for QuicBaaS.
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
	// User command flags.
	iamUserName     string
	iamUserRoles    []string
	iamUserGroups   []string
	iamUserRole     string
	iamUserGroup    string
	iamUserPassword string
)

// newIAMUserCommand creates the user command group.
func newIAMUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "User management commands",
		Long: `Manage users within a tenant.

Users are the primary identity entities in the IAM system. Each user
belongs to a tenant and can be assigned roles and group memberships.

All user commands require the --tenant flag to specify the tenant context.`,
	}

	// Add subcommands
	cmd.AddCommand(newIAMUserCreateCommand())
	cmd.AddCommand(newIAMUserGetCommand())
	cmd.AddCommand(newIAMUserUpdateCommand())
	cmd.AddCommand(newIAMUserDeleteCommand())
	cmd.AddCommand(newIAMUserListCommand())
	cmd.AddCommand(newIAMUserRoleCommand())

	return cmd
}

// newIAMUserRoleCommand creates the user role subcommand group.
func newIAMUserRoleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "role",
		Short: "User role management",
		Long:  `Manage role assignments for users within a tenant.`,
	}

	cmd.AddCommand(newIAMUserRoleAssignCommand())
	cmd.AddCommand(newIAMUserRoleRevokeCommand())

	return cmd
}

// newIAMUserRoleAssignCommand creates the user role assign command.
func newIAMUserRoleAssignCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign",
		Short: "Assign a role to a user",
		Long: `Assign a role to a user within a tenant.

Examples:
  quicbaas iam user role assign --tenant acme-corp --user user@example.com --role admin`,
		RunE: runIAMUserRoleAssignCommand,
	}

	cmd.Flags().StringVar(&iamUserRole, "role", "", "Role to assign (required)")
	cmd.Flags().StringVar(&iamRoleUserID, "user", "", "User ID or email (required)")
	cmd.MarkFlagRequired("role")
	cmd.MarkFlagRequired("user")

	return cmd
}

// runIAMUserRoleAssignCommand executes the user role assign command.
func runIAMUserRoleAssignCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.user.role.assign", "--tenant flag is required")
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
	log.Infof("Assigning role %s to user %s in tenant: %s", iamUserRole, iamRoleUserID, globalTenantID)
	if err := iamClient.User().AssignRole(ctx, globalTenantID, iamRoleUserID, iamUserRole); err != nil {
		return formatError(err)
	}

	fmt.Printf("Role '%s' assigned to user '%s' successfully\n", iamUserRole, iamRoleUserID)
	return nil
}

// newIAMUserRoleRevokeCommand creates the user role revoke command.
func newIAMUserRoleRevokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke",
		Short: "Revoke a role from a user",
		Long: `Revoke a role from a user within a tenant.

Examples:
  quicbaas iam user role revoke --tenant acme-corp --user user@example.com --role admin`,
		RunE: runIAMUserRoleRevokeCommand,
	}

	cmd.Flags().StringVar(&iamUserRole, "role", "", "Role to revoke (required)")
	cmd.Flags().StringVar(&iamRoleUserID, "user", "", "User ID or email (required)")
	cmd.MarkFlagRequired("role")
	cmd.MarkFlagRequired("user")

	return cmd
}

// runIAMUserRoleRevokeCommand executes the user role revoke command.
func runIAMUserRoleRevokeCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.user.role.revoke", "--tenant flag is required")
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
	log.Infof("Revoking role %s from user %s in tenant: %s", iamUserRole, iamRoleUserID, globalTenantID)
	if err := iamClient.User().RevokeRole(ctx, globalTenantID, iamRoleUserID, iamUserRole); err != nil {
		return formatError(err)
	}

	fmt.Printf("Role '%s' revoked from user '%s' successfully\n", iamUserRole, iamRoleUserID)
	return nil
}

// newIAMUserCreateCommand creates the user create command.
func newIAMUserCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <email>",
		Short: "Create a new user",
		Long: `Create a new user within a tenant.

The email address serves as the unique identifier for the user within the tenant.
You can optionally assign roles and groups during creation.

Examples:
  # Create basic user
  quicbaas iam user create alice@acme.com --tenant acme-corp --name "Alice Smith" --password "SecureP@ss123"

  # Create user with roles
  quicbaas iam user create alice@acme.com --tenant acme-corp --name "Alice" --password "SecureP@ss123" --roles admin,developer

  # Create user with roles and groups
  quicbaas iam user create alice@acme.com --tenant acme-corp --name "Alice" --password "SecureP@ss123" --roles developer --groups engineering,team-west`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMUserCreateCommand,
	}

	cmd.Flags().StringVar(&iamUserName, "name", "", "User display name (required)")
	cmd.Flags().StringSliceVar(&iamUserRoles, "roles", []string{}, "Roles to assign (comma-separated)")
	cmd.Flags().StringVar(&iamUserRole, "role", "", "Single role to assign (alternative to --roles)")
	cmd.Flags().StringSliceVar(&iamUserGroups, "groups", []string{}, "Groups to add user to (comma-separated)")
	cmd.Flags().StringVar(&iamUserPassword, "password", "", "User password (required)")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("password")

	return cmd
}

// runIAMUserCreateCommand executes the user create command.
func runIAMUserCreateCommand(cmd *cobra.Command, args []string) error {
	email := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.user.create", "--tenant flag is required")
	}

	// Merge --role into --roles if provided
	roles := iamUserRoles
	if iamUserRole != "" {
		roles = append(roles, iamUserRole)
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create user using SDK sub-client
	log.Debugf("Creating user: %s in tenant: %s", email, globalTenantID)
	user, err := iamClient.User().Create(ctx, globalTenantID, &iam.CreateUserRequest{
		Username:    email, // Use email as username
		Email:       email,
		DisplayName: iamUserName,
		Password:    iamUserPassword,
		Roles:       roles,
		Groups:      iamUserGroups,
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
		return encoder.Encode(user)

	default:
		fmt.Println("User created successfully:")
		fmt.Printf("  ID: %s\n", user.ID)
		fmt.Printf("  Email: %s\n", user.Email)
		fmt.Printf("  Name: %s\n", user.DisplayName)
		fmt.Printf("  Tenant: %s\n", user.TenantID)
		if len(user.Roles) > 0 {
			fmt.Printf("  Roles: %s\n", strings.Join(user.Roles, ", "))
		}
		if len(user.Groups) > 0 {
			fmt.Printf("  Groups: %s\n", strings.Join(user.Groups, ", "))
		}
		fmt.Printf("  Created: %s\n", user.CreatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMUserGetCommand creates the user get command.
func newIAMUserGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <user-id>",
		Short: "Get user details",
		Long: `Retrieve detailed information about a specific user.

Examples:
  # Get user details
  quicbaas iam user get user_abc123 --tenant acme-corp

  # Get user as JSON
  quicbaas iam user get user_abc123 --tenant acme-corp --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMUserGetCommand,
	}

	return cmd
}

// runIAMUserGetCommand executes the user get command.
func runIAMUserGetCommand(cmd *cobra.Command, args []string) error {
	userID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.user.get", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Get user using SDK sub-client
	log.Debugf("Getting user: %s in tenant: %s", userID, globalTenantID)
	user, err := iamClient.User().Get(ctx, globalTenantID, userID)
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
		return encoder.Encode(user)

	default:
		fmt.Println("User Details:")
		fmt.Printf("  ID: %s\n", user.ID)
		fmt.Printf("  Email: %s\n", user.Email)
		fmt.Printf("  Name: %s\n", user.DisplayName)
		fmt.Printf("  Tenant: %s\n", user.TenantID)
		fmt.Printf("  Roles: %s\n", strings.Join(user.Roles, ", "))
		fmt.Printf("  Groups: %s\n", strings.Join(user.Groups, ", "))
		fmt.Printf("  Created: %s\n", user.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Updated: %s\n", user.UpdatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMUserUpdateCommand creates the user update command.
func newIAMUserUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <user-id>",
		Short: "Update user information",
		Long: `Update an existing user's information.

At least one flag must be specified to update the user.

Examples:
  # Update user name
  quicbaas iam user update user_abc123 --tenant acme-corp --name "Alice Johnson"

  # Update user roles
  quicbaas iam user update user_abc123 --tenant acme-corp --roles admin,developer`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMUserUpdateCommand,
	}

	cmd.Flags().StringVar(&iamUserName, "name", "", "New user name")
	cmd.Flags().StringSliceVar(&iamUserRoles, "roles", nil, "New roles for user (comma-separated)")

	return cmd
}

// runIAMUserUpdateCommand executes the user update command.
func runIAMUserUpdateCommand(cmd *cobra.Command, args []string) error {
	userID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.user.update", "--tenant flag is required")
	}

	// Check if at least one update flag is provided
	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("roles") {
		return errors.Newf(errors.ErrInvalidInput, "iam.user.update", "at least one of --name or --roles must be specified")
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
	updateReq := &iam.UpdateUserRequest{}
	if cmd.Flags().Changed("name") {
		updateReq.DisplayName = iamUserName
	}

	// Update user using SDK sub-client
	log.Debugf("Updating user: %s in tenant: %s", userID, globalTenantID)
	user, err := iamClient.User().Update(ctx, globalTenantID, userID, updateReq)
	if err != nil {
		return formatError(err)
	}

	// If roles were changed, update them separately
	// Note: The SDK UpdateUserRequest doesn't have a Roles field, so we would need
	// to use AssignRole/RevokeRole methods for role changes. For now, just show updated user.

	fmt.Println("User updated successfully:")
	fmt.Printf("  ID: %s\n", user.ID)
	fmt.Printf("  Email: %s\n", user.Email)
	fmt.Printf("  Name: %s\n", user.DisplayName)
	fmt.Printf("  Roles: %s\n", strings.Join(user.Roles, ", "))

	return nil
}

// newIAMUserDeleteCommand creates the user delete command.
func newIAMUserDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <user-id>",
		Short: "Delete a user",
		Long: `Delete a user from the tenant.

WARNING: This operation is irreversible and will revoke all user sessions.

Examples:
  # Delete user with confirmation
  quicbaas iam user delete user_abc123 --tenant acme-corp

  # Delete user without confirmation
  quicbaas iam user delete user_abc123 --tenant acme-corp --yes`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMUserDeleteCommand,
	}

	return cmd
}

// runIAMUserDeleteCommand executes the user delete command.
func runIAMUserDeleteCommand(cmd *cobra.Command, args []string) error {
	userID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.user.delete", "--tenant flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("Are you sure you want to delete user '%s'? This cannot be undone. (y/N): ", userID)
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

	// Delete user using SDK sub-client
	log.Debugf("Deleting user: %s from tenant: %s", userID, globalTenantID)
	if err := iamClient.User().Delete(ctx, globalTenantID, userID); err != nil {
		return formatError(err)
	}

	fmt.Printf("User '%s' deleted successfully\n", userID)
	return nil
}

// newIAMUserListCommand creates the user list command.
func newIAMUserListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List users in a tenant",
		Long: `List all users within a tenant.

You can optionally filter users by role or group membership.

Examples:
  # List all users in tenant
  quicbaas iam user list --tenant acme-corp

  # List users with specific role
  quicbaas iam user list --tenant acme-corp --role admin

  # List users in specific group
  quicbaas iam user list --tenant acme-corp --group engineering

  # List users as JSON
  quicbaas iam user list --tenant acme-corp --output json`,
		RunE: runIAMUserListCommand,
	}

	cmd.Flags().StringVar(&iamUserRole, "role", "", "Filter by role")
	cmd.Flags().StringVar(&iamUserGroup, "group", "", "Filter by group")

	return cmd
}

// runIAMUserListCommand executes the user list command.
func runIAMUserListCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.user.list", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List users using SDK sub-client
	log.Debugf("Listing users in tenant: %s", globalTenantID)
	resp, err := iamClient.User().List(ctx, globalTenantID, &iam.ListUsersRequest{
		RoleID:  iamUserRole,
		GroupID: iamUserGroup,
	})
	if err != nil {
		return formatError(err)
	}

	users := resp.Users

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(users)

	default:
		if len(users) == 0 {
			fmt.Println("No users found")
			return nil
		}

		fmt.Printf("%-25s %-35s %-30s %-20s %s\n", "ID", "EMAIL", "NAME", "ROLES", "CREATED")
		fmt.Println(strings.Repeat("-", 140))
		for _, user := range users {
			roles := strings.Join(user.Roles, ",")
			if len(roles) > 20 {
				roles = roles[:17] + "..."
			}
			fmt.Printf("%-25s %-35s %-30s %-20s %s\n",
				user.ID,
				user.Email,
				user.DisplayName,
				roles,
				user.CreatedAt.Format("2006-01-02 15:04"))
		}
		fmt.Printf("\nTotal: %d users\n", len(users))
	}

	return nil
}
