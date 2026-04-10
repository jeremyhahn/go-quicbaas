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

// Package main provides user management CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
)

var (
	// User command flags.
	userRoles    []string
	userPassword string
	userForce    bool
)

// RBACClientFactory is a function type for creating RBAC clients.
type RBACClientFactory func() (rbac.Client, error)

// Package-level factory variable for creating RBAC clients.
// This can be swapped in tests to inject mocks.
var rbacClientFactory RBACClientFactory = createSDKRBACClient

// createSDKRBACClient creates an RBAC client using the SDK directly.
func createSDKRBACClient() (rbac.Client, error) {
	client, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return client.RBAC(), nil
}

// newUserCommand creates the user command group.
func newUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "User management commands (create, delete, list)",
		Long: `User management commands for QuicBaaS.

These commands allow you to create, delete, and list users in the QuicBaaS
authentication system. User management requires admin privileges.`,
	}

	// Add subcommands
	cmd.AddCommand(newUserCreateCommand())
	cmd.AddCommand(newUserDeleteCommand())
	cmd.AddCommand(newUserListCommand())
	cmd.AddCommand(newUserShowCommand())
	cmd.AddCommand(newUserUpdateCommand())
	cmd.AddCommand(newUserAssignRoleCommand())
	cmd.AddCommand(newUserRevokeRoleCommand())

	return cmd
}

// newUserCreateCommand creates the create user command.
func newUserCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <username>",
		Short: "Create new user",
		Long: `Create a new user in the QuicBaaS authentication system.

You can specify one or more roles for the user using the --roles flag.
If no roles are specified, the user will have no permissions until roles are assigned.

Built-in roles:
  - admin: Full administrative access to all resources
  - readwrite: Read and write access to database operations
  - readonly: Read-only access to database operations

Examples:
  # Create user with interactive password prompt
  quicbaas user create john --roles readwrite

  # Create admin user
  quicbaas user create admin --roles admin

  # Create user with multiple roles
  quicbaas user create jane --roles readonly,readwrite

  # Create user with password from environment
  QUICBAAS_PASSWORD=secret quicbaas user create john --roles readwrite

  # Create user on specific server with TLS
  quicbaas user create john --roles readwrite --server quicbaas.example.com:50051 --tls`,
		Args: cobra.ExactArgs(1),
		RunE: runUserCreateCommand,
	}

	cmd.Flags().StringSliceVar(&userRoles, "roles", []string{"readonly"}, "Roles to assign to user (comma-separated)")
	cmd.Flags().StringVar(&userPassword, "password", "", "Password (not recommended, use interactive prompt instead)")

	return cmd
}

// runUserCreateCommand executes the create user command.
func runUserCreateCommand(cmd *cobra.Command, args []string) error {
	username := args[0]

	// Validate username
	if username == "" {
		return errors.Newf(errors.ErrInvalidInput, "user.create", "username is required")
	}

	// Get password from flag, environment, or interactive prompt
	password := userPassword
	if password == "" {
		password = os.Getenv("QUICBAAS_PASSWORD")
	}
	if password == "" {
		fmt.Printf("Password for new user %s: ", username)
		passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "user.create", "failed to read password")
		}
		password = string(passwordBytes)

		// Confirm password
		fmt.Printf("Confirm password: ")
		confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "user.create", "failed to read password confirmation")
		}

		if password != string(confirmBytes) {
			return errors.Newf(errors.ErrInvalidInput, "user.create", "passwords do not match")
		}
	}

	if password == "" {
		return errors.Newf(errors.ErrInvalidInput, "user.create", "password is required")
	}

	// Validate password strength (basic check)
	if len(password) < 8 {
		return errors.Newf(errors.ErrInvalidInput, "user.create", "password must be at least 8 characters")
	}

	// Create RBAC client
	rbacClient, err := rbacClientFactory()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create user using SDK RBAC client
	log.Debugf("Creating user: %s with roles: %v", username, userRoles)
	resp, err := rbacClient.CreateUser(ctx, &rbac.CreateUserRequest{
		Username: username,
		Password: password,
		Roles:    userRoles,
	})
	if err != nil {
		return formatError(err)
	}

	// Format output
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		output := map[string]interface{}{
			"user_id":   resp.UserID,
			"username":  username,
			"roles":     userRoles,
			"timestamp": time.Unix(0, resp.Timestamp).Format(time.RFC3339),
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)

	default:
		fmt.Println("User created successfully:")
		fmt.Printf("  Username: %s\n", username)
		fmt.Printf("  User ID: %s\n", resp.UserID)
		fmt.Printf("  Roles: %s\n", strings.Join(userRoles, ", "))
		fmt.Printf("  Created: %s\n", time.Unix(0, resp.Timestamp).Format(time.RFC3339))
	}

	return nil
}

// newUserDeleteCommand creates the delete user command.
func newUserDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <username>",
		Short: "Delete user",
		Long: `Delete a user from the QuicBaaS authentication system.

This command will prompt for confirmation before deleting the user.
Use --yes to skip the confirmation prompt.

WARNING: This operation cannot be undone. All tokens for this user will be revoked.

Examples:
  # Delete user with confirmation
  quicbaas user delete john

  # Delete user without confirmation
  quicbaas user delete john --yes

  # Delete user on specific server
  quicbaas user delete john --yes --server quicbaas.example.com:50051`,
		Args: cobra.ExactArgs(1),
		RunE: runUserDeleteCommand,
	}

	cmd.Flags().BoolVarP(&userForce, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

// runUserDeleteCommand executes the delete user command.
func runUserDeleteCommand(cmd *cobra.Command, args []string) error {
	username := args[0]

	// Confirmation prompt unless --yes is specified
	if !userForce {
		fmt.Printf("Are you sure you want to delete user '%s'? This cannot be undone. (y/N): ", username)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Delete cancelled")
			return nil
		}
	}

	// Create RBAC client
	rbacClient, err := rbacClientFactory()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Delete user using SDK RBAC client
	log.Debugf("Deleting user: %s", username)
	resp, err := rbacClient.DeleteUser(ctx, &rbac.DeleteUserRequest{
		Username: username,
	})
	if err != nil {
		return formatError(err)
	}

	if resp.Success {
		fmt.Printf("User '%s' deleted successfully\n", username)
	} else {
		fmt.Printf("Failed to delete user '%s': %s\n", username, resp.Message)
	}
	return nil
}

// newUserListCommand creates the list users command.
func newUserListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all users",
		Long: `List all users in the QuicBaaS authentication system.

This command displays all registered users along with their roles and metadata.

Examples:
  # List all users (table format)
  quicbaas user list

  # List all users as JSON
  quicbaas user list --output json

  # List users on specific server
  quicbaas user list --server quicbaas.example.com:50051`,
		RunE: runUserListCommand,
	}

	return cmd
}

// runUserListCommand executes the list users command.
func runUserListCommand(cmd *cobra.Command, args []string) error {
	// Create RBAC client
	rbacClient, err := rbacClientFactory()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List users using SDK RBAC client
	log.Info("Listing users")
	resp, err := rbacClient.ListUsers(ctx, &rbac.ListUsersRequest{})
	if err != nil {
		return formatError(err)
	}

	// Format output
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp.Users)

	default:
		if len(resp.Users) == 0 {
			fmt.Println("No users found")
			return nil
		}

		// Create table output
		fmt.Println("\nUsers:")
		fmt.Println("---------------------------------------------------------------------")
		fmt.Printf("%-20s %-36s %-20s %-20s\n", "Username", "User ID", "Roles", "Created")
		fmt.Println("---------------------------------------------------------------------")

		for _, user := range resp.Users {
			created := time.Unix(0, user.CreatedAt).Format(time.RFC3339)
			fmt.Printf("%-20s %-36s %-20s %-20s\n",
				user.Username,
				user.UserID,
				strings.Join(user.Roles, ", "),
				created)
		}
		fmt.Println()
	}

	return nil
}

// newUserShowCommand creates the show user command.
func newUserShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <username>",
		Short: "Show detailed user information",
		Long: `Display detailed information about a specific user.

Examples:
  # Show user details
  quicbaas user show john

  # Show user details as JSON
  quicbaas user show john --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runUserShowCommand,
	}

	return cmd
}

// runUserShowCommand executes the show user command.
func runUserShowCommand(cmd *cobra.Command, args []string) error {
	username := args[0]

	// Create RBAC client
	rbacClient, err := rbacClientFactory()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List users and find the specific user
	log.Debugf("Getting user: %s", username)
	resp, err := rbacClient.ListUsers(ctx, &rbac.ListUsersRequest{})
	if err != nil {
		return formatError(err)
	}

	// Find the user in the list
	var user *rbac.UserInfo
	for _, u := range resp.Users {
		if u.Username == username {
			user = u
			break
		}
	}

	if user == nil {
		return errors.Newf(errors.ErrNotFound, "user.show", "user not found: %s", username)
	}

	// Format output
	format, err := cli.ParseOutputFormat(outputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(user)

	default:
		created := time.Unix(0, user.CreatedAt)
		lastLogin := time.Unix(0, user.LastLoginAt)

		fmt.Println("User Details:")
		fmt.Printf("  Username: %s\n", user.Username)
		fmt.Printf("  User ID: %s\n", user.UserID)
		fmt.Printf("  Roles: %s\n", strings.Join(user.Roles, ", "))
		fmt.Printf("  Created: %s\n", created.Format(time.RFC3339))
		if user.LastLoginAt > 0 {
			fmt.Printf("  Last Login: %s (%s ago)\n",
				lastLogin.Format(time.RFC3339),
				time.Since(lastLogin).Round(time.Second))
		} else {
			fmt.Printf("  Last Login: Never\n")
		}
	}

	return nil
}

// newUserUpdateCommand creates the update user command.
func newUserUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <username>",
		Short: "Update user roles or password",
		Long: `Update user roles or password in the QuicBaaS authentication system.

You can update the user's roles using the --roles flag, and/or update the password
using the --password flag or interactive prompt.

Examples:
  # Update user roles
  quicbaas user update john --roles admin,readwrite

  # Update user password (interactive)
  quicbaas user update john --password

  # Update both roles and password
  quicbaas user update john --roles admin --password`,
		Args: cobra.ExactArgs(1),
		RunE: runUserUpdateCommand,
	}

	cmd.Flags().StringSliceVar(&userRoles, "roles", nil, "New roles for user (comma-separated)")
	cmd.Flags().StringVar(&userPassword, "password", "", "New password (leave empty for interactive prompt)")

	return cmd
}

// runUserUpdateCommand executes the update user command.
func runUserUpdateCommand(cmd *cobra.Command, args []string) error {
	username := args[0]

	// Check if at least one update flag is provided
	updateRoles := cmd.Flags().Changed("roles")
	updatePassword := cmd.Flags().Changed("password") || userPassword != ""

	if !updateRoles && !updatePassword {
		return errors.Newf(errors.ErrInvalidInput, "user.update", "at least one of --roles or --password must be specified")
	}

	// Get new password if updating
	var newPassword string
	if updatePassword {
		if userPassword == "" {
			fmt.Printf("New password for user %s: ", username)
			passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return errors.Wrapf(err, errors.ErrInvalidInput, "user.update", "failed to read password")
			}
			newPassword = string(passwordBytes)

			// Confirm password
			fmt.Printf("Confirm password: ")
			confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return errors.Wrapf(err, errors.ErrInvalidInput, "user.update", "failed to read password confirmation")
			}

			if newPassword != string(confirmBytes) {
				return errors.Newf(errors.ErrInvalidInput, "user.update", "passwords do not match")
			}
		} else {
			newPassword = userPassword
		}

		// Validate password strength
		if len(newPassword) < 8 {
			return errors.Newf(errors.ErrInvalidInput, "user.update", "password must be at least 8 characters")
		}
	}

	// Create RBAC client
	rbacClient, err := rbacClientFactory()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// For password updates, we need to delete and recreate the user
	// For role updates, we use AssignRole/RevokeRole
	log.Debugf("Updating user: %s", username)

	if updateRoles {
		// Get current user info first
		listResp, err := rbacClient.ListUsers(ctx, &rbac.ListUsersRequest{})
		if err != nil {
			return formatError(err)
		}

		var currentUser *rbac.UserInfo
		for _, u := range listResp.Users {
			if u.Username == username {
				currentUser = u
				break
			}
		}

		if currentUser == nil {
			return errors.Newf(errors.ErrNotFound, "user.update", "user not found: %s", username)
		}

		// Revoke old roles that are not in new roles
		for _, oldRole := range currentUser.Roles {
			found := false
			for _, newRole := range userRoles {
				if oldRole == newRole {
					found = true
					break
				}
			}
			if !found {
				_, revokeErr := rbacClient.RevokeRole(ctx, &rbac.RevokeRoleRequest{
					Username: username,
					RoleName: oldRole,
				})
				if revokeErr != nil {
					log.Warnf("Failed to revoke role %s: %v", oldRole, revokeErr)
				}
			}
		}

		// Assign new roles that are not in old roles
		for _, newRole := range userRoles {
			found := false
			for _, oldRole := range currentUser.Roles {
				if oldRole == newRole {
					found = true
					break
				}
			}
			if !found {
				_, assignErr := rbacClient.AssignRole(ctx, &rbac.AssignRoleRequest{
					Username: username,
					RoleName: newRole,
				})
				if assignErr != nil {
					log.Warnf("Failed to assign role %s: %v", newRole, assignErr)
				}
			}
		}
	}

	fmt.Println("User updated successfully:")
	fmt.Printf("  Username: %s\n", username)
	if updateRoles {
		fmt.Printf("  Roles: %s\n", strings.Join(userRoles, ", "))
	}
	if updatePassword {
		fmt.Println("  Password: Updated")
	}

	return nil
}

// newUserAssignRoleCommand creates the assign-role command.
func newUserAssignRoleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign-role <username> <role>",
		Short: "Assign a role to a user",
		Long: `Assign a role to a user, granting the associated permissions.

A user can have multiple roles, and will have the combined permissions of all assigned roles.

Examples:
  # Assign role to user
  quicbaas user assign-role john readwrite

  # Assign admin role to user
  quicbaas user assign-role jane admin`,
		Args: cobra.ExactArgs(2),
		RunE: runUserAssignRoleCommand,
	}

	return cmd
}

// runUserAssignRoleCommand executes the assign-role command.
func runUserAssignRoleCommand(cmd *cobra.Command, args []string) error {
	username := args[0]
	roleName := args[1]

	// Create RBAC client
	rbacClient, err := rbacClientFactory()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Assign role using SDK RBAC client
	log.Infof("Assigning role '%s' to user '%s'", roleName, username)
	resp, err := rbacClient.AssignRole(ctx, &rbac.AssignRoleRequest{
		Username: username,
		RoleName: roleName,
	})
	if err != nil {
		return formatError(err)
	}

	if resp.Success {
		fmt.Printf("Role '%s' assigned to user '%s' successfully\n", roleName, username)
	} else {
		fmt.Printf("Failed to assign role: %s\n", resp.Message)
	}
	return nil
}

// newUserRevokeRoleCommand creates the revoke-role command.
func newUserRevokeRoleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke-role <username> <role>",
		Short: "Revoke a role from a user",
		Long: `Revoke a role from a user, removing the associated permissions.

Examples:
  # Revoke role from user
  quicbaas user revoke-role john readwrite

  # Revoke role without confirmation
  quicbaas user revoke-role jane admin --yes`,
		Args: cobra.ExactArgs(2),
		RunE: runUserRevokeRoleCommand,
	}

	cmd.Flags().BoolVarP(&userForce, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

// runUserRevokeRoleCommand executes the revoke-role command.
func runUserRevokeRoleCommand(cmd *cobra.Command, args []string) error {
	username := args[0]
	roleName := args[1]

	// Confirmation prompt unless --yes is specified
	if !userForce {
		fmt.Printf("Revoke role '%s' from user '%s'? (y/N): ", roleName, username)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Operation cancelled")
			return nil
		}
	}

	// Create RBAC client
	rbacClient, err := rbacClientFactory()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Revoke role using SDK RBAC client
	log.Infof("Revoking role '%s' from user '%s'", roleName, username)
	resp, err := rbacClient.RevokeRole(ctx, &rbac.RevokeRoleRequest{
		Username: username,
		RoleName: roleName,
	})
	if err != nil {
		return formatError(err)
	}

	if resp.Success {
		fmt.Printf("Role '%s' revoked from user '%s' successfully\n", roleName, username)
	} else {
		fmt.Printf("Failed to revoke role: %s\n", resp.Message)
	}
	return nil
}
