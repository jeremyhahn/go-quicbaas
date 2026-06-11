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

// Package main provides IAM delegated administration CLI commands for QuicBaaS.
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
	// Admin assignment command flags.
	adminUserID            string
	adminRole              string
	adminTenantIDs         []string
	adminPermissions       []string
	adminDeniedPermissions []string
	adminExpiresAt         string
	adminGrantedBy         string
	adminActive            bool
	adminAssignmentID      string

	// Admin permission check flags.
	adminCheckPermission   string
	adminCheckTenantID     string
	adminCheckResourceType string
	adminCheckResourceID   string

	// Impersonation flags.
	impersonateAdminUserID  string
	impersonateTargetUserID string
	impersonateTenantID     string
	impersonateReason       string
	impersonateDuration     string
	impersonateSessionID    string
)

// newIAMAdminCommand creates the admin command group for delegated administration.
func newIAMAdminCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Delegated administration management commands",
		Long: `Manage delegated administration assignments and impersonation.

Delegated administration allows assigning administrative privileges to users
with fine-grained control over what resources and operations they can access.

Admin Roles:
  - super_admin    Full access to all tenants and system operations
  - tenant_admin   Full access within assigned tenant(s)
  - user_admin     Can manage users within assigned tenant(s)
  - group_admin    Can manage groups within assigned tenant(s)
  - policy_admin   Can manage policies within assigned tenant(s)
  - audit_admin    Can view audit logs within assigned tenant(s)
  - read_only      Read-only access to assigned resources

Examples:
  # Create an admin assignment
  quicbaas iam admin assignment create --user user_abc123 --role tenant_admin \
    --tenant-ids tenant1,tenant2 --granted-by admin_xyz

  # Check admin permission
  quicbaas iam admin check-permission --user-id user_abc123 --permission user:create \
    --tenant-id tenant1

  # Start impersonation session
  quicbaas iam admin impersonate start --admin-user-id admin_xyz \
    --target-user-id user_abc123 --tenant-id tenant1 --reason "Customer support"`,
	}

	// Add subcommand groups
	cmd.AddCommand(newIAMAdminAssignmentCommand())
	cmd.AddCommand(newIAMAdminCheckPermissionCommand())
	cmd.AddCommand(newIAMAdminImpersonateCommand())

	return cmd
}

// newIAMAdminAssignmentCommand creates the assignment command group.
func newIAMAdminAssignmentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assignment",
		Short: "Manage admin assignments",
		Long: `Create, update, delete, and list administrative assignments.

Admin assignments grant users administrative privileges with configurable
scope, permissions, and expiration.`,
	}

	cmd.AddCommand(newIAMAdminAssignmentCreateCommand())
	cmd.AddCommand(newIAMAdminAssignmentGetCommand())
	cmd.AddCommand(newIAMAdminAssignmentUpdateCommand())
	cmd.AddCommand(newIAMAdminAssignmentDeleteCommand())
	cmd.AddCommand(newIAMAdminAssignmentListCommand())

	return cmd
}

// newIAMAdminAssignmentCreateCommand creates the assignment create command.
func newIAMAdminAssignmentCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an admin assignment",
		Long: `Create a new administrative assignment for a user.

Examples:
  # Create a tenant admin assignment
  quicbaas iam admin assignment create --user user_abc123 --role tenant_admin \
    --tenant-ids tenant1,tenant2 --granted-by admin_xyz

  # Create an assignment with custom permissions
  quicbaas iam admin assignment create --user user_abc123 --role user_admin \
    --tenant-ids tenant1 --permissions user:create,user:read,user:update \
    --granted-by admin_xyz

  # Create a time-limited assignment
  quicbaas iam admin assignment create --user user_abc123 --role audit_admin \
    --tenant-ids tenant1 --expires-at 2025-12-31T23:59:59Z --granted-by admin_xyz`,
		RunE: runIAMAdminAssignmentCreateCommand,
	}

	cmd.Flags().StringVar(&adminUserID, "user", "", "User ID to assign admin privileges (required)")
	cmd.Flags().StringVar(&adminRole, "role", "", "Admin role (required): super_admin, tenant_admin, user_admin, group_admin, policy_admin, audit_admin, read_only")
	cmd.Flags().StringSliceVar(&adminTenantIDs, "tenant-ids", nil, "Comma-separated list of tenant IDs this assignment applies to")
	cmd.Flags().StringSliceVar(&adminPermissions, "permissions", nil, "Comma-separated list of additional permissions to grant")
	cmd.Flags().StringSliceVar(&adminDeniedPermissions, "denied-permissions", nil, "Comma-separated list of permissions to explicitly deny")
	cmd.Flags().StringVar(&adminExpiresAt, "expires-at", "", "When this assignment expires (RFC3339 format)")
	cmd.Flags().StringVar(&adminGrantedBy, "granted-by", "", "User ID of the admin granting this assignment (required)")

	cmd.MarkFlagRequired("user")
	cmd.MarkFlagRequired("role")
	cmd.MarkFlagRequired("granted-by")

	return cmd
}

// runIAMAdminAssignmentCreateCommand executes the assignment create command.
func runIAMAdminAssignmentCreateCommand(cmd *cobra.Command, args []string) error {
	// Validate admin role
	validRoles := []string{"super_admin", "tenant_admin", "user_admin", "group_admin", "policy_admin", "audit_admin", "read_only"}
	roleValid := false
	for _, r := range validRoles {
		if adminRole == r {
			roleValid = true
			break
		}
	}
	if !roleValid {
		return errors.Newf(errors.ErrInvalidInput, "iam.admin.assignment.create", "invalid admin role: %s. Must be one of: %s", adminRole, strings.Join(validRoles, ", "))
	}

	// Parse expiration if provided
	var expiresAt time.Time
	if adminExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, adminExpiresAt)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "iam.admin.assignment.create", "invalid expires-at format, expected RFC3339")
		}
		expiresAt = t
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Creating admin assignment for user: %s with role: %s", adminUserID, adminRole)

	// Create admin assignment using SDK sub-client
	assignment, err := iamClient.Admin().CreateAssignment(ctx, &iam.CreateAdminAssignmentRequest{
		AdminID:       adminUserID,
		TenantID:      globalTenantID,
		Permissions:   adminPermissions,
		RoleID:        adminRole,
		ExpiresAt:     expiresAt,
		GrantedBy:     adminGrantedBy,
		Justification: fmt.Sprintf("Granted by %s", adminGrantedBy),
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
		return encoder.Encode(assignment)

	default:
		fmt.Println("Admin Assignment Created:")
		fmt.Printf("  ID:         %s\n", assignment.ID)
		fmt.Printf("  Admin ID:   %s\n", assignment.AdminID)
		fmt.Printf("  Role:       %s\n", assignment.RoleName)
		if assignment.TenantID != "" {
			fmt.Printf("  Tenant ID:  %s\n", assignment.TenantID)
		}
		fmt.Printf("  Created By: %s\n", assignment.CreatedBy)
		fmt.Printf("  Created At: %s\n", assignment.CreatedAt.Format("2006-01-02 15:04:05"))
		if !assignment.ExpiresAt.IsZero() {
			fmt.Printf("  Expires At: %s\n", assignment.ExpiresAt.Format("2006-01-02 15:04:05"))
		}
		fmt.Printf("  Status:     %s\n", assignment.Status.String())
	}

	return nil
}

// newIAMAdminAssignmentGetCommand creates the assignment get command.
func newIAMAdminAssignmentGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get details about an admin assignment",
		Long: `Retrieve detailed information about a specific admin assignment.

Examples:
  # Get assignment details
  quicbaas iam admin assignment get --assignment-id adm_abc123

  # Get assignment as JSON
  quicbaas iam admin assignment get --assignment-id adm_abc123 --output json`,
		RunE: runIAMAdminAssignmentGetCommand,
	}

	cmd.Flags().StringVar(&adminAssignmentID, "assignment-id", "", "Assignment ID (required)")
	cmd.MarkFlagRequired("assignment-id")

	return cmd
}

// runIAMAdminAssignmentGetCommand executes the assignment get command.
func runIAMAdminAssignmentGetCommand(cmd *cobra.Command, args []string) error {
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Getting admin assignment: %s", adminAssignmentID)

	// Get assignment using SDK sub-client
	assignment, err := iamClient.Admin().GetAssignment(ctx, adminAssignmentID)
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
		return encoder.Encode(assignment)

	default:
		fmt.Println("Admin Assignment Details:")
		fmt.Printf("  ID:         %s\n", assignment.ID)
		fmt.Printf("  Admin ID:   %s\n", assignment.AdminID)
		if assignment.AdminName != "" {
			fmt.Printf("  Admin Name: %s\n", assignment.AdminName)
		}
		fmt.Printf("  Role:       %s\n", assignment.RoleName)
		if assignment.TenantID != "" {
			fmt.Printf("  Tenant ID:  %s\n", assignment.TenantID)
		}
		fmt.Printf("  Scope:      %s\n", assignment.Scope.String())
		if assignment.ScopeID != "" {
			fmt.Printf("  Scope ID:   %s\n", assignment.ScopeID)
		}
		if len(assignment.Permissions) > 0 {
			fmt.Printf("  Permissions: %s\n", strings.Join(assignment.Permissions, ", "))
		}
		fmt.Printf("  Created By: %s\n", assignment.CreatedBy)
		fmt.Printf("  Created At: %s\n", assignment.CreatedAt.Format("2006-01-02 15:04:05"))
		if !assignment.ExpiresAt.IsZero() {
			fmt.Printf("  Expires At: %s\n", assignment.ExpiresAt.Format("2006-01-02 15:04:05"))
		}
		if !assignment.LastUsedAt.IsZero() {
			fmt.Printf("  Last Used:  %s\n", assignment.LastUsedAt.Format("2006-01-02 15:04:05"))
		}
		fmt.Printf("  Status:     %s\n", assignment.Status.String())
	}

	return nil
}

// newIAMAdminAssignmentUpdateCommand creates the assignment update command.
func newIAMAdminAssignmentUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update an admin assignment",
		Long: `Update an existing administrative assignment.

Examples:
  # Update assignment role
  quicbaas iam admin assignment update --assignment-id adm_abc123 --role policy_admin

  # Add more tenants
  quicbaas iam admin assignment update --assignment-id adm_abc123 --tenant-ids tenant1,tenant2,tenant3

  # Deactivate an assignment
  quicbaas iam admin assignment update --assignment-id adm_abc123 --active=false`,
		RunE: runIAMAdminAssignmentUpdateCommand,
	}

	cmd.Flags().StringVar(&adminAssignmentID, "assignment-id", "", "Assignment ID (required)")
	cmd.Flags().StringVar(&adminRole, "role", "", "Updated admin role")
	cmd.Flags().StringSliceVar(&adminTenantIDs, "tenant-ids", nil, "Updated tenant IDs")
	cmd.Flags().StringSliceVar(&adminPermissions, "permissions", nil, "Updated permissions")
	cmd.Flags().StringSliceVar(&adminDeniedPermissions, "denied-permissions", nil, "Updated denied permissions")
	cmd.Flags().StringVar(&adminExpiresAt, "expires-at", "", "Updated expiration (RFC3339 format)")
	cmd.Flags().BoolVar(&adminActive, "active", true, "Whether the assignment is active")

	cmd.MarkFlagRequired("assignment-id")

	return cmd
}

// runIAMAdminAssignmentUpdateCommand executes the assignment update command.
func runIAMAdminAssignmentUpdateCommand(cmd *cobra.Command, args []string) error {
	// Build update request based on flags that were set
	updateReq := &iam.UpdateAdminAssignmentRequest{}

	if cmd.Flags().Changed("role") {
		// Validate admin role
		validRoles := []string{"super_admin", "tenant_admin", "user_admin", "group_admin", "policy_admin", "audit_admin", "read_only"}
		roleValid := false
		for _, r := range validRoles {
			if adminRole == r {
				roleValid = true
				break
			}
		}
		if !roleValid {
			return errors.Newf(errors.ErrInvalidInput, "iam.admin.assignment.update", "invalid admin role: %s", adminRole)
		}
	}

	if cmd.Flags().Changed("permissions") {
		updateReq.Permissions = adminPermissions
	}

	if cmd.Flags().Changed("expires-at") {
		t, err := time.Parse(time.RFC3339, adminExpiresAt)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "iam.admin.assignment.update", "invalid expires-at format")
		}
		updateReq.ExpiresAt = t
	}

	if cmd.Flags().Changed("active") {
		if adminActive {
			status := iam.AdminAssignmentStatusActive
			updateReq.Status = &status
		} else {
			status := iam.AdminAssignmentStatusSuspended
			updateReq.Status = &status
		}
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Updating admin assignment: %s", adminAssignmentID)

	// Update assignment using SDK sub-client
	assignment, err := iamClient.Admin().UpdateAssignment(ctx, adminAssignmentID, updateReq)
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
		return encoder.Encode(assignment)

	default:
		fmt.Printf("Admin assignment '%s' updated successfully\n", adminAssignmentID)
	}

	return nil
}

// newIAMAdminAssignmentDeleteCommand creates the assignment delete command.
func newIAMAdminAssignmentDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete an admin assignment",
		Long: `Delete an administrative assignment.

WARNING: This will immediately revoke the user's administrative privileges.

Examples:
  # Delete with confirmation
  quicbaas iam admin assignment delete --assignment-id adm_abc123

  # Delete without confirmation
  quicbaas iam admin assignment delete --assignment-id adm_abc123 --yes`,
		RunE: runIAMAdminAssignmentDeleteCommand,
	}

	cmd.Flags().StringVar(&adminAssignmentID, "assignment-id", "", "Assignment ID (required)")
	cmd.MarkFlagRequired("assignment-id")

	return cmd
}

// runIAMAdminAssignmentDeleteCommand executes the assignment delete command.
func runIAMAdminAssignmentDeleteCommand(cmd *cobra.Command, args []string) error {
	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("Are you sure you want to delete admin assignment '%s'? This will revoke administrative privileges. (y/N): ", adminAssignmentID)
		var response string
		fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Delete cancelled")
			return nil
		}
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Deleting admin assignment: %s", adminAssignmentID)

	// Delete assignment using SDK sub-client
	if err := iamClient.Admin().DeleteAssignment(ctx, adminAssignmentID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Admin assignment '%s' deleted successfully\n", adminAssignmentID)
	return nil
}

// newIAMAdminAssignmentListCommand creates the assignment list command.
func newIAMAdminAssignmentListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List admin assignments",
		Long: `List administrative assignments with optional filters.

Examples:
  # List all assignments
  quicbaas iam admin assignment list

  # List assignments for a specific user
  quicbaas iam admin assignment list --user user_abc123

  # List assignments for a specific tenant
  quicbaas iam admin assignment list --tenant tenant1

  # List only active assignments
  quicbaas iam admin assignment list --active-only`,
		RunE: runIAMAdminAssignmentListCommand,
	}

	cmd.Flags().StringVar(&adminUserID, "user", "", "Filter by user ID")
	cmd.Flags().StringVar(&adminRole, "role", "", "Filter by admin role")
	cmd.Flags().BoolVar(&adminActive, "active-only", false, "Show only active assignments")

	return cmd
}

// runIAMAdminAssignmentListCommand executes the assignment list command.
func runIAMAdminAssignmentListCommand(cmd *cobra.Command, args []string) error {
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Info("Listing admin assignments")

	// Build list request
	listReq := &iam.ListAdminAssignmentsRequest{
		AdminID:  adminUserID,
		TenantID: globalTenantID,
	}

	// Filter by active status if requested
	if adminActive {
		status := iam.AdminAssignmentStatusActive
		listReq.Status = &status
	}

	// List assignments using SDK sub-client
	resp, err := iamClient.Admin().ListAssignments(ctx, listReq)
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
		return encoder.Encode(resp.Assignments)

	default:
		if len(resp.Assignments) == 0 {
			fmt.Println("No admin assignments found")
			return nil
		}

		fmt.Printf("%-36s %-20s %-15s %-20s %-8s\n", "ASSIGNMENT ID", "ADMIN ID", "ROLE", "CREATED BY", "STATUS")
		fmt.Println(strings.Repeat("-", 100))
		for _, a := range resp.Assignments {
			fmt.Printf("%-36s %-20s %-15s %-20s %-8s\n",
				a.ID,
				truncateString(a.AdminID, 20),
				a.RoleName,
				truncateString(a.CreatedBy, 20),
				a.Status.String())
		}
		fmt.Printf("\nTotal: %d assignments\n", len(resp.Assignments))
	}

	return nil
}

// newIAMAdminCheckPermissionCommand creates the check-permission command.
func newIAMAdminCheckPermissionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-permission",
		Short: "Check if a user has an admin permission",
		Long: `Check if an administrative user has a specific permission.

This evaluates the user's admin assignments to determine if they have
the requested permission within the specified scope.

Examples:
  # Check user:create permission
  quicbaas iam admin check-permission --user-id user_abc123 --permission user:create

  # Check permission within a tenant
  quicbaas iam admin check-permission --user-id user_abc123 --permission user:create \
    --tenant-id tenant1

  # Check permission on a specific resource
  quicbaas iam admin check-permission --user-id user_abc123 --permission user:update \
    --tenant-id tenant1 --resource-type user --resource-id user_xyz`,
		RunE: runIAMAdminCheckPermissionCommand,
	}

	cmd.Flags().StringVar(&adminUserID, "user-id", "", "User ID to check permissions for (required)")
	cmd.Flags().StringVar(&adminCheckPermission, "permission", "", "Permission to check (required)")
	cmd.Flags().StringVar(&adminCheckTenantID, "tenant-id", "", "Tenant context for the check")
	cmd.Flags().StringVar(&adminCheckResourceType, "resource-type", "", "Resource type being accessed")
	cmd.Flags().StringVar(&adminCheckResourceID, "resource-id", "", "Specific resource ID being accessed")

	cmd.MarkFlagRequired("user-id")
	cmd.MarkFlagRequired("permission")

	return cmd
}

// runIAMAdminCheckPermissionCommand executes the check-permission command.
func runIAMAdminCheckPermissionCommand(cmd *cobra.Command, args []string) error {
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Checking permission '%s' for user: %s", adminCheckPermission, adminUserID)

	// Check permission using SDK sub-client
	result, err := iamClient.Admin().CheckPermission(ctx, &iam.AdminCheckRequest{
		AdminID:      adminUserID,
		TenantID:     adminCheckTenantID,
		Permission:   adminCheckPermission,
		ResourceType: adminCheckResourceType,
		ResourceID:   adminCheckResourceID,
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
		return encoder.Encode(result)

	default:
		fmt.Println("Permission Check Result:")
		fmt.Printf("  User ID: %s\n", adminUserID)
		fmt.Printf("  Permission: %s\n", adminCheckPermission)
		fmt.Printf("  Allowed: %t\n", result.Allowed)
		if result.AssignmentID != "" {
			fmt.Printf("  Assignment ID: %s\n", result.AssignmentID)
		}
		if result.Scope != iam.AdminScopeGlobal {
			fmt.Printf("  Scope: %s\n", result.Scope.String())
		}
		if result.DenialReason != "" {
			fmt.Printf("  Denial Reason: %s\n", result.DenialReason)
		}
	}

	return nil
}

// newIAMAdminImpersonateCommand creates the impersonate command group.
func newIAMAdminImpersonateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "impersonate",
		Short: "Manage impersonation sessions",
		Long: `Start, end, and list impersonation sessions.

Impersonation allows administrators to act as another user for support
and debugging purposes. All impersonation sessions are logged for audit.`,
	}

	cmd.AddCommand(newIAMAdminImpersonateStartCommand())
	cmd.AddCommand(newIAMAdminImpersonateEndCommand())
	cmd.AddCommand(newIAMAdminImpersonateListCommand())

	return cmd
}

// newIAMAdminImpersonateStartCommand creates the impersonate start command.
func newIAMAdminImpersonateStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start an impersonation session",
		Long: `Start an impersonation session to act as another user.

Requires user:impersonate permission. All actions taken during impersonation
are logged with both the admin and target user identifiers.

Examples:
  # Start impersonation with default duration (1 hour)
  quicbaas iam admin impersonate start --admin-user-id admin_xyz \
    --target-user-id user_abc123 --tenant-id tenant1 --reason "Customer support"

  # Start impersonation with custom duration
  quicbaas iam admin impersonate start --admin-user-id admin_xyz \
    --target-user-id user_abc123 --tenant-id tenant1 --reason "Debugging issue" \
    --duration 30m`,
		RunE: runIAMAdminImpersonateStartCommand,
	}

	cmd.Flags().StringVar(&impersonateAdminUserID, "admin-user-id", "", "Admin user ID starting impersonation (required)")
	cmd.Flags().StringVar(&impersonateTargetUserID, "target-user-id", "", "User ID to impersonate (required)")
	cmd.Flags().StringVar(&impersonateTenantID, "tenant-id", "", "Tenant context (required)")
	cmd.Flags().StringVar(&impersonateReason, "reason", "", "Reason for impersonation (required)")
	cmd.Flags().StringVar(&impersonateDuration, "duration", "1h", "Duration of impersonation session")

	cmd.MarkFlagRequired("admin-user-id")
	cmd.MarkFlagRequired("target-user-id")
	cmd.MarkFlagRequired("tenant-id")
	cmd.MarkFlagRequired("reason")

	return cmd
}

// runIAMAdminImpersonateStartCommand executes the impersonate start command.
func runIAMAdminImpersonateStartCommand(cmd *cobra.Command, args []string) error {
	// Parse duration
	duration, err := time.ParseDuration(impersonateDuration)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "iam.admin.impersonate.start", "invalid duration format")
	}

	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Starting impersonation: admin %s impersonating %s", impersonateAdminUserID, impersonateTargetUserID)

	// Start impersonation using SDK sub-client
	session, err := iamClient.Admin().StartImpersonation(ctx, &iam.ImpersonationRequest{
		AdminUserID:  impersonateAdminUserID,
		TargetUserID: impersonateTargetUserID,
		TenantID:     impersonateTenantID,
		Reason:       impersonateReason,
		Duration:     duration,
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
		return encoder.Encode(session)

	default:
		fmt.Println("Impersonation Session Started:")
		fmt.Printf("  Session ID:     %s\n", session.ID)
		fmt.Printf("  Admin User:     %s\n", session.AdminID)
		fmt.Printf("  Target User:    %s\n", session.TargetUserID)
		fmt.Printf("  Tenant:         %s\n", session.TenantID)
		fmt.Printf("  Reason:         %s\n", session.Reason)
		fmt.Printf("  Started At:     %s\n", session.StartedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Expires At:     %s\n", session.ExpiresAt.Format("2006-01-02 15:04:05"))
		fmt.Println("\nIMPORTANT: All actions during this session are logged with the admin identifier.")
	}

	return nil
}

// newIAMAdminImpersonateEndCommand creates the impersonate end command.
func newIAMAdminImpersonateEndCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "end",
		Short: "End an impersonation session",
		Long: `End an active impersonation session.

Examples:
  # End an impersonation session
  quicbaas iam admin impersonate end --session-id imp_abc123`,
		RunE: runIAMAdminImpersonateEndCommand,
	}

	cmd.Flags().StringVar(&impersonateSessionID, "session-id", "", "Impersonation session ID (required)")
	cmd.MarkFlagRequired("session-id")

	return cmd
}

// runIAMAdminImpersonateEndCommand executes the impersonate end command.
func runIAMAdminImpersonateEndCommand(cmd *cobra.Command, args []string) error {
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Ending impersonation session: %s", impersonateSessionID)

	// End impersonation using SDK sub-client
	if err := iamClient.Admin().EndImpersonation(ctx, impersonateSessionID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Impersonation session '%s' ended successfully\n", impersonateSessionID)
	return nil
}

// newIAMAdminImpersonateListCommand creates the impersonate list command.
func newIAMAdminImpersonateListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active impersonation sessions",
		Long: `List active impersonation sessions for an admin user.

Examples:
  # List active sessions for an admin
  quicbaas iam admin impersonate list --admin-user-id admin_xyz`,
		RunE: runIAMAdminImpersonateListCommand,
	}

	cmd.Flags().StringVar(&impersonateAdminUserID, "admin-user-id", "", "Admin user ID (required)")
	cmd.MarkFlagRequired("admin-user-id")

	return cmd
}

// runIAMAdminImpersonateListCommand executes the impersonate list command.
func runIAMAdminImpersonateListCommand(cmd *cobra.Command, args []string) error {
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Debugf("Listing active impersonation sessions for: %s", impersonateAdminUserID)

	// List active impersonations using SDK sub-client
	resp, err := iamClient.Admin().ListActiveImpersonations(ctx, impersonateAdminUserID)
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
		return encoder.Encode(resp.Sessions)

	default:
		if len(resp.Sessions) == 0 {
			fmt.Println("No active impersonation sessions")
			return nil
		}

		fmt.Printf("%-36s %-20s %-20s %-20s %-20s\n", "SESSION ID", "TARGET USER", "TENANT", "STARTED", "EXPIRES")
		fmt.Println(strings.Repeat("-", 120))
		for _, s := range resp.Sessions {
			fmt.Printf("%-36s %-20s %-20s %-20s %-20s\n",
				s.ID,
				truncateString(s.TargetUserID, 20),
				truncateString(s.TenantID, 20),
				s.StartedAt.Format("2006-01-02 15:04"),
				s.ExpiresAt.Format("2006-01-02 15:04"))
		}
		fmt.Printf("\nTotal: %d active sessions\n", len(resp.Sessions))
	}

	return nil
}
