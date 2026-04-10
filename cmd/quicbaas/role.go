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

// Package main provides role management CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
)

var (
	// Role command flags.
	roleDescription  string
	roleForce        bool
	rolePermissions  []string
	roleOutputFormat string
)

// newRoleCommand creates the role command group.
func newRoleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "role",
		Short: "Role management commands (create, delete, assign, permissions)",
		Long: `Role management commands for QuicBaaS RBAC.

These commands allow you to create custom roles, assign roles to users, and
manage role permissions. Role management requires admin privileges.

Built-in roles (cannot be modified):
  - admin: Full administrative access to all resources
  - readwrite: Read and write access to database operations
  - readonly: Read-only access to database operations`,
	}

	// Add subcommands
	cmd.AddCommand(newRoleCreateCommand())
	cmd.AddCommand(newRoleDeleteCommand())
	cmd.AddCommand(newRoleListCommand())
	cmd.AddCommand(newRoleShowCommand())
	cmd.AddCommand(newRoleAssignCommand())
	cmd.AddCommand(newRoleUnassignCommand())

	return cmd
}

// newRoleCreateCommand creates the create role command.
func newRoleCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create new role",
		Long: `Create a new custom role with specified permissions.

Permissions are specified in the format "resource:action" where:
  - resource: keys, indexes, cluster, users, backups, metrics, health, transactions, watch
  - action: read, write, delete, admin, * (all)

Examples:
  # Create a backup operator role
  quicbaas role create backup-operator --description "Backup operations" \
    --permissions backups:read,backups:write,backups:delete

  # Create a metrics reader role
  quicbaas role create metrics-reader --description "Metrics read access" \
    --permissions metrics:read,health:read

  # Create a cluster admin role
  quicbaas role create cluster-admin --description "Cluster administration" \
    --permissions cluster:*

  # Create a data analyst role (read-only access to data and indexes)
  quicbaas role create data-analyst --description "Data analysis" \
    --permissions keys:read,indexes:read,metrics:read`,
		Args: cobra.ExactArgs(1),
		RunE: runRoleCreateCommand,
	}

	cmd.Flags().StringVar(&roleDescription, "description", "", "Role description")
	cmd.Flags().StringSliceVar(&rolePermissions, "permissions", nil, "Permissions in format resource:action (comma-separated)")
	cmd.MarkFlagRequired("permissions")

	return cmd
}

// runRoleCreateCommand executes the create role command.
func runRoleCreateCommand(cmd *cobra.Command, args []string) error {
	roleName := args[0]

	// Validate role name
	if roleName == "" {
		return errors.Newf(errors.ErrInvalidInput, "role.create", "role name is required")
	}

	// Check for reserved role names
	if roleName == auth.RoleNameAdmin || roleName == auth.RoleNameReadWrite || roleName == auth.RoleNameReadOnly {
		return errors.Newf(errors.ErrInvalidInput, "role.create", "cannot create role with reserved name: %s", roleName)
	}

	// Parse permissions into string format for SDK
	permissionStrings, err := formatPermissionsForSDK(rolePermissions)
	if err != nil {
		return err
	}

	// Create RBAC client
	rbacClient, err := rbacClientFactory()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create role using SDK RBAC client
	log.Infof("Creating role: %s with %d permissions", roleName, len(permissionStrings))
	resp, err := rbacClient.CreateRole(ctx, &rbac.CreateRoleRequest{
		RoleName:    roleName,
		Permissions: permissionStrings,
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
			"role_id":     resp.RoleID,
			"role_name":   roleName,
			"description": roleDescription,
			"permissions": permissionStrings,
			"timestamp":   time.Unix(0, resp.Timestamp).Format(time.RFC3339),
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(output)

	default:
		fmt.Println("Role created successfully:")
		fmt.Printf("  Name: %s\n", roleName)
		fmt.Printf("  Role ID: %s\n", resp.RoleID)
		fmt.Printf("  Description: %s\n", roleDescription)
		fmt.Printf("  Permissions:\n")
		for _, perm := range permissionStrings {
			fmt.Printf("    - %s\n", perm)
		}
	}

	return nil
}

// newRoleDeleteCommand creates the delete role command.
func newRoleDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete role",
		Long: `Delete a custom role from the QuicBaaS authentication system.

Built-in roles (admin, readwrite, readonly) cannot be deleted.

WARNING: Users with this role will lose the associated permissions.
This operation cannot be undone.

Examples:
  # Delete role with confirmation
  quicbaas role delete backup-operator

  # Delete role without confirmation
  quicbaas role delete backup-operator --yes`,
		Args: cobra.ExactArgs(1),
		RunE: runRoleDeleteCommand,
	}

	cmd.Flags().BoolVarP(&roleForce, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

// runRoleDeleteCommand executes the delete role command.
func runRoleDeleteCommand(cmd *cobra.Command, args []string) error {
	roleName := args[0]

	// Check for reserved role names
	if roleName == auth.RoleNameAdmin || roleName == auth.RoleNameReadWrite || roleName == auth.RoleNameReadOnly {
		return errors.Newf(errors.ErrInvalidInput, "role.delete", "cannot delete built-in role: %s", roleName)
	}

	// Confirmation prompt unless --yes is specified
	if !roleForce {
		fmt.Printf("Are you sure you want to delete role '%s'? Users with this role will lose permissions. (y/N): ", roleName)
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

	// Delete role using SDK RBAC client
	log.Infof("Deleting role: %s", roleName)
	resp, err := rbacClient.DeleteRole(ctx, &rbac.DeleteRoleRequest{
		RoleName: roleName,
	})
	if err != nil {
		return formatError(err)
	}

	if resp.Success {
		fmt.Printf("Role '%s' deleted successfully\n", roleName)
	} else {
		fmt.Printf("Failed to delete role '%s': %s\n", roleName, resp.Message)
	}
	return nil
}

// newRoleListCommand creates the list roles command.
func newRoleListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all roles",
		Long: `List all roles in the QuicBaaS authentication system.

This includes both built-in roles and custom roles.

Examples:
  # List all roles (table format)
  quicbaas role list

  # List all roles as JSON
  quicbaas role list --output json`,
		RunE: runRoleListCommand,
	}

	cmd.Flags().StringVar(&roleOutputFormat, "output", "table", "Output format (json, table)")

	return cmd
}

// runRoleListCommand executes the list roles command.
func runRoleListCommand(cmd *cobra.Command, args []string) error {
	// Create RBAC client
	rbacClient, err := rbacClientFactory()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List roles using SDK RBAC client
	log.Info("Listing roles")
	resp, err := rbacClient.ListRoles(ctx, &rbac.ListRolesRequest{})
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
		return encoder.Encode(resp.Roles)

	default:
		if len(resp.Roles) == 0 {
			fmt.Println("No roles found")
			return nil
		}

		// Create table output
		fmt.Println("\nRoles:")
		fmt.Println("---------------------------------------------------------------------")
		fmt.Printf("%-20s %-36s %-15s %-10s\n", "Name", "Role ID", "Permissions", "Type")
		fmt.Println("---------------------------------------------------------------------")

		for _, role := range resp.Roles {
			roleType := "Custom"
			if role.RoleName == auth.RoleNameAdmin || role.RoleName == auth.RoleNameReadWrite || role.RoleName == auth.RoleNameReadOnly {
				roleType = "Built-in"
			}

			fmt.Printf("%-20s %-36s %-15s %-10s\n",
				role.RoleName,
				role.RoleID,
				fmt.Sprintf("%d permission(s)", len(role.Permissions)),
				roleType)
		}
		fmt.Println()
	}

	return nil
}

// newRoleShowCommand creates the show role command.
func newRoleShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show detailed role information",
		Long: `Display detailed information about a specific role including all permissions.

Examples:
  # Show role details
  quicbaas role show admin

  # Show role details as JSON
  quicbaas role show admin --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runRoleShowCommand,
	}

	cmd.Flags().StringVar(&roleOutputFormat, "output", "table", "Output format (json, table)")

	return cmd
}

// runRoleShowCommand executes the show role command.
func runRoleShowCommand(cmd *cobra.Command, args []string) error {
	roleName := args[0]

	// Create RBAC client
	rbacClient, err := rbacClientFactory()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List roles and find the specific role
	log.Infof("Getting role: %s", roleName)
	resp, err := rbacClient.ListRoles(ctx, &rbac.ListRolesRequest{})
	if err != nil {
		return formatError(err)
	}

	// Find the role in the list
	var role *rbac.RoleInfo
	for _, r := range resp.Roles {
		if r.RoleName == roleName {
			role = r
			break
		}
	}

	if role == nil {
		return errors.Newf(errors.ErrNotFound, "role.show", "role not found: %s", roleName)
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
		return encoder.Encode(role)

	default:
		roleType := "Custom"
		if role.RoleName == auth.RoleNameAdmin || role.RoleName == auth.RoleNameReadWrite || role.RoleName == auth.RoleNameReadOnly {
			roleType = "Built-in"
		}

		fmt.Println("Role Details:")
		fmt.Printf("  Name: %s\n", role.RoleName)
		fmt.Printf("  Role ID: %s\n", role.RoleID)
		fmt.Printf("  Type: %s\n", roleType)
		fmt.Printf("  Created: %s\n", time.Unix(0, role.CreatedAt).Format(time.RFC3339))
		fmt.Printf("  Permissions:\n")
		if len(role.Permissions) == 0 {
			fmt.Println("    (none)")
		} else {
			for _, perm := range role.Permissions {
				fmt.Printf("    - %s\n", perm)
			}
		}
	}

	return nil
}

// newRoleAssignCommand creates the assign role command.
func newRoleAssignCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign <role> <username>",
		Short: "Assign role to user",
		Long: `Assign a role to a user, granting the associated permissions.

A user can have multiple roles, and will have the combined permissions of all assigned roles.

Examples:
  # Assign role to user
  quicbaas role assign readwrite john

  # Assign admin role to user
  quicbaas role assign admin jane

  # Assign custom role to user
  quicbaas role assign backup-operator backup-user`,
		Args: cobra.ExactArgs(2),
		RunE: runRoleAssignCommand,
	}

	return cmd
}

// runRoleAssignCommand executes the assign role command.
func runRoleAssignCommand(cmd *cobra.Command, args []string) error {
	roleName := args[0]
	username := args[1]

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

// newRoleUnassignCommand creates the unassign role command.
func newRoleUnassignCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unassign <role> <username>",
		Short: "Remove role from user",
		Long: `Remove a role from a user, revoking the associated permissions.

Examples:
  # Remove role from user
  quicbaas role unassign readwrite john

  # Remove role from user without confirmation
  quicbaas role unassign admin jane --yes`,
		Args: cobra.ExactArgs(2),
		RunE: runRoleUnassignCommand,
	}

	cmd.Flags().BoolVarP(&roleForce, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

// runRoleUnassignCommand executes the unassign role command.
func runRoleUnassignCommand(cmd *cobra.Command, args []string) error {
	roleName := args[0]
	username := args[1]

	// Confirmation prompt unless --yes is specified
	if !roleForce {
		fmt.Printf("Remove role '%s' from user '%s'? (y/N): ", roleName, username)
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

	// Unassign role using SDK RBAC client
	log.Infof("Removing role '%s' from user '%s'", roleName, username)
	resp, err := rbacClient.RevokeRole(ctx, &rbac.RevokeRoleRequest{
		Username: username,
		RoleName: roleName,
	})
	if err != nil {
		return formatError(err)
	}

	if resp.Success {
		fmt.Printf("Role '%s' removed from user '%s' successfully\n", roleName, username)
	} else {
		fmt.Printf("Failed to remove role: %s\n", resp.Message)
	}
	return nil
}

// formatPermissionsForSDK converts permission strings to SDK format.
// Input format: "resource:action" (e.g., "keys:read", "backups:*")
// Output format: "resource:action" strings for the SDK.
func formatPermissionsForSDK(permStrings []string) ([]string, error) {
	result := make([]string, 0, len(permStrings))

	for _, permStr := range permStrings {
		parts := strings.Split(permStr, ":")
		if len(parts) != 2 {
			return nil, errors.Newf(errors.ErrInvalidInput, "formatPermissionsForSDK", "invalid permission format: %s (expected resource:action)", permStr)
		}

		resource := strings.TrimSpace(parts[0])
		action := strings.TrimSpace(parts[1])

		if resource == "" || action == "" {
			return nil, errors.Newf(errors.ErrInvalidInput, "formatPermissionsForSDK", "invalid permission format: %s", permStr)
		}

		// Validate resource
		validResources := []string{
			auth.ResourceKeys, auth.ResourceIndexes, auth.ResourceCluster,
			auth.ResourceUsers, auth.ResourceBackups, auth.ResourceMetrics,
			auth.ResourceHealth, auth.ResourceTransactions, auth.ResourceWatch,
			auth.ResourceAll,
		}
		validResource := false
		for _, vr := range validResources {
			if resource == vr {
				validResource = true
				break
			}
		}
		if !validResource {
			return nil, errors.Newf(errors.ErrInvalidInput, "formatPermissionsForSDK", "invalid resource: %s", resource)
		}

		// Validate action
		validActions := []string{auth.ActionRead, auth.ActionWrite, auth.ActionDelete, auth.ActionAdmin, auth.ActionAll}
		validAction := false
		for _, va := range validActions {
			if action == va {
				validAction = true
				break
			}
		}
		if !validAction {
			return nil, errors.Newf(errors.ErrInvalidInput, "formatPermissionsForSDK", "invalid action: %s", action)
		}

		result = append(result, permStr)
	}

	return result, nil
}
