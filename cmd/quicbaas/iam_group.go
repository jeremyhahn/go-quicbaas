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

// Package main provides IAM group management CLI commands for QuicBaaS.
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
	// Group command flags.
	iamGroupName        string
	iamGroupDescription string
	iamGroupID          string
	iamGroupUserID      string
)

// newIAMGroupCommand creates the group command group.
func newIAMGroupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Group management commands",
		Long: `Manage groups within a tenant.

Groups provide a way to organize users and assign permissions collectively.
Users can be members of multiple groups, and groups can have roles assigned.

All group commands require the --tenant flag to specify the tenant context.`,
	}

	// Add subcommands
	cmd.AddCommand(newIAMGroupCreateCommand())
	cmd.AddCommand(newIAMGroupGetCommand())
	cmd.AddCommand(newIAMGroupUpdateCommand())
	cmd.AddCommand(newIAMGroupDeleteCommand())
	cmd.AddCommand(newIAMGroupListCommand())
	cmd.AddCommand(newIAMGroupAddMemberCommand())
	cmd.AddCommand(newIAMGroupRemoveMemberCommand())
	cmd.AddCommand(newIAMGroupMembersCommand())

	return cmd
}

// newIAMGroupCreateCommand creates the group create command.
func newIAMGroupCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <id>",
		Short: "Create a new group",
		Long: `Create a new group within a tenant.

The group ID must be unique within the tenant and will be used for
group membership operations.

Examples:
  # Create basic group
  quicbaas iam group create engineering --tenant acme-corp --name "Engineering Team"

  # Create group with description
  quicbaas iam group create engineering --tenant acme-corp --name "Engineering" --description "All engineering staff"`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMGroupCreateCommand,
	}

	cmd.Flags().StringVar(&iamGroupName, "name", "", "Group display name (required)")
	cmd.Flags().StringVar(&iamGroupDescription, "description", "", "Group description")
	cmd.MarkFlagRequired("name")

	return cmd
}

// runIAMGroupCreateCommand executes the group create command.
func runIAMGroupCreateCommand(cmd *cobra.Command, args []string) error {
	groupID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.create", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create group using SDK sub-client
	log.Debugf("Creating group: %s in tenant: %s", groupID, globalTenantID)
	group, err := iamClient.Group().Create(ctx, globalTenantID, &iam.CreateGroupRequest{
		ID:          groupID,
		Name:        iamGroupName,
		Description: iamGroupDescription,
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
		return encoder.Encode(group)

	default:
		fmt.Println("Group created successfully:")
		fmt.Printf("  ID: %s\n", group.ID)
		fmt.Printf("  Name: %s\n", group.DisplayName)
		fmt.Printf("  Tenant: %s\n", group.TenantID)
		if group.Description != "" {
			fmt.Printf("  Description: %s\n", group.Description)
		}
		fmt.Printf("  Created: %s\n", group.CreatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMGroupGetCommand creates the group get command.
func newIAMGroupGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get group details",
		Long: `Retrieve detailed information about a specific group.

Examples:
  # Get group details
  quicbaas iam group get engineering --tenant acme-corp

  # Get group as JSON
  quicbaas iam group get engineering --tenant acme-corp --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMGroupGetCommand,
	}

	return cmd
}

// runIAMGroupGetCommand executes the group get command.
func runIAMGroupGetCommand(cmd *cobra.Command, args []string) error {
	groupID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.get", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Get group using SDK sub-client
	log.Debugf("Getting group: %s in tenant: %s", groupID, globalTenantID)
	group, err := iamClient.Group().Get(ctx, globalTenantID, groupID)
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
		return encoder.Encode(group)

	default:
		fmt.Println("Group Details:")
		fmt.Printf("  ID: %s\n", group.ID)
		fmt.Printf("  Name: %s\n", group.DisplayName)
		fmt.Printf("  Tenant: %s\n", group.TenantID)
		fmt.Printf("  Description: %s\n", group.Description)
		fmt.Printf("  Member Count: %d\n", group.MemberCount)
		fmt.Printf("  Created: %s\n", group.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Updated: %s\n", group.UpdatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// newIAMGroupUpdateCommand creates the group update command.
func newIAMGroupUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update group information",
		Long: `Update an existing group's information.

At least one flag must be specified to update the group.

Examples:
  # Update group name
  quicbaas iam group update engineering --tenant acme-corp --name "Engineering Department"

  # Update group description
  quicbaas iam group update engineering --tenant acme-corp --description "All engineering personnel"`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMGroupUpdateCommand,
	}

	cmd.Flags().StringVar(&iamGroupName, "name", "", "New group name")
	cmd.Flags().StringVar(&iamGroupDescription, "description", "", "New group description")

	return cmd
}

// runIAMGroupUpdateCommand executes the group update command.
func runIAMGroupUpdateCommand(cmd *cobra.Command, args []string) error {
	groupID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.update", "--tenant flag is required")
	}

	// Check if at least one update flag is provided
	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("description") {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.update", "at least one of --name or --description must be specified")
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
	updateReq := &iam.UpdateGroupRequest{}
	if cmd.Flags().Changed("name") {
		updateReq.Name = iamGroupName
	}
	if cmd.Flags().Changed("description") {
		updateReq.Description = iamGroupDescription
	}

	// Update group using SDK sub-client
	log.Debugf("Updating group: %s in tenant: %s", groupID, globalTenantID)
	group, err := iamClient.Group().Update(ctx, globalTenantID, groupID, updateReq)
	if err != nil {
		return formatError(err)
	}

	fmt.Println("Group updated successfully:")
	fmt.Printf("  ID: %s\n", group.ID)
	fmt.Printf("  Name: %s\n", group.DisplayName)
	fmt.Printf("  Description: %s\n", group.Description)

	return nil
}

// newIAMGroupDeleteCommand creates the group delete command.
func newIAMGroupDeleteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a group",
		Long: `Delete a group from the tenant.

WARNING: This operation is irreversible and will remove all group memberships.

Examples:
  # Delete group with confirmation
  quicbaas iam group delete engineering --tenant acme-corp

  # Delete group without confirmation
  quicbaas iam group delete engineering --tenant acme-corp --yes`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMGroupDeleteCommand,
	}

	return cmd
}

// runIAMGroupDeleteCommand executes the group delete command.
func runIAMGroupDeleteCommand(cmd *cobra.Command, args []string) error {
	groupID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.delete", "--tenant flag is required")
	}

	// Confirmation prompt unless --yes is specified
	if !iamForce {
		fmt.Printf("Are you sure you want to delete group '%s'? This cannot be undone. (y/N): ", groupID)
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

	// Delete group using SDK sub-client
	log.Debugf("Deleting group: %s from tenant: %s", groupID, globalTenantID)
	if err := iamClient.Group().Delete(ctx, globalTenantID, groupID); err != nil {
		return formatError(err)
	}

	fmt.Printf("Group '%s' deleted successfully\n", groupID)
	return nil
}

// newIAMGroupListCommand creates the group list command.
func newIAMGroupListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List groups in a tenant",
		Long: `List all groups within a tenant.

Examples:
  # List all groups
  quicbaas iam group list --tenant acme-corp

  # List groups as JSON
  quicbaas iam group list --tenant acme-corp --output json`,
		RunE: runIAMGroupListCommand,
	}

	return cmd
}

// runIAMGroupListCommand executes the group list command.
func runIAMGroupListCommand(cmd *cobra.Command, args []string) error {
	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.list", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List groups using SDK sub-client
	log.Debugf("Listing groups in tenant: %s", globalTenantID)
	resp, err := iamClient.Group().List(ctx, globalTenantID, &iam.ListGroupsRequest{})
	if err != nil {
		return formatError(err)
	}

	groups := resp.Groups

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(groups)

	default:
		if len(groups) == 0 {
			fmt.Println("No groups found")
			return nil
		}

		fmt.Printf("%-25s %-30s %-12s %-40s %s\n", "ID", "NAME", "MEMBERS", "DESCRIPTION", "CREATED")
		fmt.Println(strings.Repeat("-", 130))
		for _, group := range groups {
			desc := group.Description
			if len(desc) > 40 {
				desc = desc[:37] + "..."
			}
			fmt.Printf("%-25s %-30s %-12d %-40s %s\n",
				group.ID,
				group.DisplayName,
				group.MemberCount,
				desc,
				group.CreatedAt.Format("2006-01-02 15:04"))
		}
		fmt.Printf("\nTotal: %d groups\n", len(groups))
	}

	return nil
}

// newIAMGroupAddMemberCommand creates the add member command.
func newIAMGroupAddMemberCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-member [<group-id> <user-id>]",
		Short: "Add a user to a group",
		Long: `Add a user to a group within a tenant.

You can use either positional arguments or flags:

Examples:
  # Using positional arguments
  quicbaas iam group add-member engineering user_abc123 --tenant acme-corp

  # Using flags
  quicbaas iam group add-member --tenant acme-corp --group engineering --user user_abc123`,
		Args: cobra.MaximumNArgs(2),
		RunE: runIAMGroupAddMemberCommand,
	}

	cmd.Flags().StringVar(&iamGroupID, "group", "", "Group ID to add user to")
	cmd.Flags().StringVar(&iamGroupUserID, "user", "", "User ID to add to group")

	return cmd
}

// runIAMGroupAddMemberCommand executes the add member command.
func runIAMGroupAddMemberCommand(cmd *cobra.Command, args []string) error {
	// Support both positional args and flags
	groupID := iamGroupID
	userID := iamGroupUserID

	// If positional args provided, use them
	if len(args) >= 2 {
		groupID = args[0]
		userID = args[1]
	} else if len(args) == 1 {
		groupID = args[0]
	}

	// Validate required values
	if groupID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.add-member", "group ID is required (use --group flag or positional argument)")
	}
	if userID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.add-member", "user ID is required (use --user flag or positional argument)")
	}
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.add-member", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Add member using SDK sub-client
	log.Debugf("Adding user %s to group %s in tenant: %s", userID, groupID, globalTenantID)
	if err := iamClient.Group().AddMember(ctx, globalTenantID, groupID, userID); err != nil {
		return formatError(err)
	}

	fmt.Printf("User '%s' added to group '%s' successfully\n", userID, groupID)
	return nil
}

// newIAMGroupRemoveMemberCommand creates the remove member command.
func newIAMGroupRemoveMemberCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove-member [<group-id> <user-id>]",
		Short: "Remove a user from a group",
		Long: `Remove a user from a group within a tenant.

You can use either positional arguments or flags:

Examples:
  # Using positional arguments
  quicbaas iam group remove-member engineering user_abc123 --tenant acme-corp

  # Using flags
  quicbaas iam group remove-member --tenant acme-corp --group engineering --user user_abc123`,
		Args: cobra.MaximumNArgs(2),
		RunE: runIAMGroupRemoveMemberCommand,
	}

	cmd.Flags().StringVar(&iamGroupID, "group", "", "Group ID to remove user from")
	cmd.Flags().StringVar(&iamGroupUserID, "user", "", "User ID to remove from group")

	return cmd
}

// runIAMGroupRemoveMemberCommand executes the remove member command.
func runIAMGroupRemoveMemberCommand(cmd *cobra.Command, args []string) error {
	// Support both positional args and flags
	groupID := iamGroupID
	userID := iamGroupUserID

	// If positional args provided, use them
	if len(args) >= 2 {
		groupID = args[0]
		userID = args[1]
	} else if len(args) == 1 {
		groupID = args[0]
	}

	// Validate required values
	if groupID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.remove-member", "group ID is required (use --group flag or positional argument)")
	}
	if userID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.remove-member", "user ID is required (use --user flag or positional argument)")
	}
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.remove-member", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Remove member using SDK sub-client
	log.Debugf("Removing user %s from group %s in tenant: %s", userID, groupID, globalTenantID)
	if err := iamClient.Group().RemoveMember(ctx, globalTenantID, groupID, userID); err != nil {
		return formatError(err)
	}

	fmt.Printf("User '%s' removed from group '%s' successfully\n", userID, groupID)
	return nil
}

// newIAMGroupMembersCommand creates the members list command.
func newIAMGroupMembersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "members <group-id>",
		Short: "List group members",
		Long: `List all members of a specific group.

Examples:
  # List group members
  quicbaas iam group members engineering --tenant acme-corp

  # List members as JSON
  quicbaas iam group members engineering --tenant acme-corp --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runIAMGroupMembersCommand,
	}

	return cmd
}

// runIAMGroupMembersCommand executes the members list command.
func runIAMGroupMembersCommand(cmd *cobra.Command, args []string) error {
	groupID := args[0]

	// Validate required flags
	if globalTenantID == "" {
		return errors.Newf(errors.ErrInvalidInput, "iam.group.members", "--tenant flag is required")
	}

	// Create IAM client
	iamClient, err := createIAMClient()
	if err != nil {
		return err
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// List members using SDK sub-client
	log.Debugf("Listing members of group %s in tenant: %s", groupID, globalTenantID)
	resp, err := iamClient.Group().ListMembers(ctx, globalTenantID, groupID)
	if err != nil {
		return formatError(err)
	}

	members := resp.Members

	// Format output
	format, err := cli.ParseOutputFormat(iamOutputFormat)
	if err != nil {
		return err
	}

	switch format {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(members)

	default:
		if len(members) == 0 {
			fmt.Printf("No members in group '%s'\n", groupID)
			return nil
		}

		fmt.Printf("Members of group '%s':\n\n", groupID)
		fmt.Printf("%-25s %-35s %-30s\n", "USER ID", "EMAIL", "NAME")
		fmt.Println(strings.Repeat("-", 95))
		for _, member := range members {
			fmt.Printf("%-25s %-35s %-30s\n",
				member.UserID,
				member.Email,
				member.DisplayName)
		}
		fmt.Printf("\nTotal: %d members\n", len(members))
	}

	return nil
}
