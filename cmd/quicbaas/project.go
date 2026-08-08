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

// Package main provides agentic project management CLI commands for QuicBaaS.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// =============================================================================
// Project Command Flags
// =============================================================================

var (
	projectOutputFmt        string
	projectName             string
	projectDescription      string
	projectClearDescription bool
	projectWorkspaceAgentID string
	projectWorkspacePath    string
	projectHostAffinity     string
	projectDefaultLeadAgent string
	projectGitRepoURL       string
	projectGitBranch        string
	projectGitCurrentSHA    string
	projectTokenBudget      int64
	projectGPUMinutes       int64
	projectMaxConcurrency   int
	projectMetadata         map[string]string
	projectAgentID          string
	projectRole             string
)

// =============================================================================
// Typed Errors
// =============================================================================

// InvalidProjectRoleError indicates an unrecognized --role value.
type InvalidProjectRoleError struct {
	Role string
}

// Error implements the error interface.
func (e *InvalidProjectRoleError) Error() string {
	return "invalid project role: " + e.Role + " (valid: " + strings.Join(projectRoleNames(), ", ") + ")"
}

// ProjectSelectorRequiredError indicates neither a project ID argument nor
// --name was supplied to a command that needs to identify one project.
type ProjectSelectorRequiredError struct {
	Op string
}

// Error implements the error interface.
func (e *ProjectSelectorRequiredError) Error() string {
	return e.Op + ": a project ID argument or --name is required"
}

// projectRoles is the canonical role lookup used to validate --role. Map-based
// dispatch keeps validation O(1) as the role vocabulary grows.
var projectRoles = map[string]projectSDK.ProjectRole{
	string(projectSDK.ProjectRoleLead):     projectSDK.ProjectRoleLead,
	string(projectSDK.ProjectRoleWorker):   projectSDK.ProjectRoleWorker,
	string(projectSDK.ProjectRoleObserver): projectSDK.ProjectRoleObserver,
	string(projectSDK.ProjectRoleDenied):   projectSDK.ProjectRoleDenied,
}

// projectRoleNames returns the valid role names in a stable order for use in
// help text and error messages.
func projectRoleNames() []string {
	names := make([]string, 0, len(projectRoles))
	for name := range projectRoles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// parseProjectRole validates a --role value against the canonical vocabulary.
func parseProjectRole(s string) (projectSDK.ProjectRole, error) {
	role, ok := projectRoles[strings.ToLower(strings.TrimSpace(s))]
	if !ok {
		return "", &InvalidProjectRoleError{Role: s}
	}
	return role, nil
}

// =============================================================================
// Cobra Command Tree
// =============================================================================

// newProjectCommand creates the project management command group.
func newProjectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Agentic project management commands",
		Long: `Manage agentic projects — the unit that binds a workspace, a git
repository, agent permissions, and resource quotas together.

Projects are tenant-scoped: every command operates within the tenant named by
the global --tenant flag.

Examples:
  quicbaas project create --name web-api --description "Public API service"
  quicbaas project list --output json
  quicbaas project get proj_abc123
  quicbaas project get --name web-api
  quicbaas project update proj_abc123 --token-budget 500000
  quicbaas project grant-role proj_abc123 --agent agent_abc --role lead
  quicbaas project delete proj_abc123`,
	}

	cmd.PersistentFlags().StringVar(&projectOutputFmt, "output", "table", "Output format (json, yaml, table)")

	cmd.AddCommand(newProjectCreateCommand())
	cmd.AddCommand(newProjectListCommand())
	cmd.AddCommand(newProjectGetCommand())
	cmd.AddCommand(newProjectUpdateCommand())
	cmd.AddCommand(newProjectDeleteCommand())
	cmd.AddCommand(newProjectGrantRoleCommand())
	cmd.AddCommand(newProjectRevokeRoleCommand())

	return cmd
}

// addProjectMutationFlags registers the flags shared by create and update.
func addProjectMutationFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&projectDescription, "description", "", "Human-readable project description")
	cmd.Flags().StringVar(&projectWorkspaceAgentID, "workspace-agent", "", "ID of the agent that owns the workspace")
	cmd.Flags().StringVar(&projectWorkspacePath, "workspace-path", "", "Filesystem path to the workspace on the agent host")
	cmd.Flags().StringVar(&projectHostAffinity, "host-affinity", "", "Pin project operations to a preferred agent host")
	cmd.Flags().StringVar(&projectDefaultLeadAgent, "default-lead-agent", "", "Default lead agent ID for workflow runs")
	cmd.Flags().StringVar(&projectGitRepoURL, "git-repo", "", "Remote git repository URL")
	cmd.Flags().StringVar(&projectGitBranch, "git-branch", "", "Default git branch (e.g. main)")
	cmd.Flags().Int64Var(&projectTokenBudget, "token-budget", 0, "Maximum LLM token budget (0 = unlimited)")
	cmd.Flags().Int64Var(&projectGPUMinutes, "gpu-minutes", 0, "Maximum GPU compute budget in minutes (0 = unlimited)")
	cmd.Flags().IntVar(&projectMaxConcurrency, "max-concurrency", 0, "Maximum concurrent workflow runs (0 = unlimited)")
	cmd.Flags().StringToStringVar(&projectMetadata, "metadata", nil, "Project metadata as key=value pairs (repeatable)")
}

// =============================================================================
// Create Command
// =============================================================================

// newProjectCreateCommand creates the project create subcommand.
func newProjectCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a project",
		Long: `Create a new project within the tenant named by --tenant.

Examples:
  quicbaas project create --name web-api
  quicbaas project create --name web-api --description "Public API service" \
    --git-repo https://github.com/acme/web-api --git-branch main
  quicbaas project create --name ml-train --workspace-agent agent_abc \
    --workspace-path /data/projects/ml-train --token-budget 1000000 --output json`,
		Args: cobra.NoArgs,
		RunE: runProjectCreate,
	}

	cmd.Flags().StringVar(&projectName, "name", "", "Project name, unique within the tenant (required)")
	addProjectMutationFlags(cmd)

	if err := cmd.MarkFlagRequired("name"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark name flag as required: %v\n", err)
	}

	return cmd
}

// runProjectCreate executes the project create command.
func runProjectCreate(cmd *cobra.Command, args []string) error {
	client, err := projectClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	info, err := client.Create(ctx, &projectSDK.CreateProjectRequest{
		TenantID:         globalTenantID,
		Name:             projectName,
		Description:      projectDescription,
		WorkspaceAgentID: projectWorkspaceAgentID,
		WorkspacePath:    projectWorkspacePath,
		HostAffinity:     projectHostAffinity,
		DefaultLeadAgent: projectDefaultLeadAgent,
		GitRepoURL:       projectGitRepoURL,
		GitDefaultBranch: projectGitBranch,
		TokenBudget:      projectTokenBudget,
		GPUMinutes:       projectGPUMinutes,
		MaxConcurrency:   projectMaxConcurrency,
		Metadata:         projectMetadata,
	})
	if err != nil {
		return formatError(err)
	}

	return formatProjectDetail(info, projectOutputFmt, os.Stdout)
}

// =============================================================================
// List Command
// =============================================================================

// newProjectListCommand creates the project list subcommand.
func newProjectListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List projects",
		Long: `List every project in the tenant named by --tenant.

Examples:
  quicbaas project list
  quicbaas project list --tenant acme --output json`,
		Args: cobra.NoArgs,
		RunE: runProjectList,
	}
}

// runProjectList executes the project list command.
func runProjectList(cmd *cobra.Command, args []string) error {
	client, err := projectClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.List(ctx, globalTenantID)
	if err != nil {
		return formatError(err)
	}

	return formatProjectList(resp, projectOutputFmt, os.Stdout)
}

// =============================================================================
// Get Command
// =============================================================================

// newProjectGetCommand creates the project get subcommand.
func newProjectGetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [projectID]",
		Short: "Get project details",
		Long: `Retrieve a single project by ID, or by name with --name.

Examples:
  quicbaas project get proj_abc123
  quicbaas project get --name web-api
  quicbaas project get proj_abc123 --output yaml`,
		Args: cobra.MaximumNArgs(1),
		RunE: runProjectGet,
	}

	cmd.Flags().StringVar(&projectName, "name", "", "Look the project up by name instead of ID")

	return cmd
}

// runProjectGet executes the project get command.
func runProjectGet(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && projectName == "" {
		return formatError(&ProjectSelectorRequiredError{Op: "project.get"})
	}

	client, err := projectClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	var info *projectSDK.ProjectInfo
	if len(args) > 0 {
		info, err = client.Get(ctx, globalTenantID, args[0])
	} else {
		info, err = client.GetByName(ctx, globalTenantID, projectName)
	}
	if err != nil {
		return formatError(err)
	}

	return formatProjectDetail(info, projectOutputFmt, os.Stdout)
}

// =============================================================================
// Update Command
// =============================================================================

// newProjectUpdateCommand creates the project update subcommand.
func newProjectUpdateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <projectID>",
		Short: "Update a project",
		Long: `Update an existing project. Only the flags you pass are applied;
every other field keeps its current value.

Use --clear-description to remove the description (passing an empty
--description leaves it unchanged).

Examples:
  quicbaas project update proj_abc123 --description "Now with GPUs"
  quicbaas project update proj_abc123 --token-budget 500000 --gpu-minutes 120
  quicbaas project update proj_abc123 --clear-description
  quicbaas project update proj_abc123 --git-sha 3f1a9c2 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runProjectUpdate,
	}

	cmd.Flags().StringVar(&projectName, "name", "", "New project name")
	cmd.Flags().BoolVar(&projectClearDescription, "clear-description", false, "Remove the project description")
	cmd.Flags().StringVar(&projectGitCurrentSHA, "git-sha", "", "Record the latest observed commit SHA")
	addProjectMutationFlags(cmd)

	return cmd
}

// runProjectUpdate executes the project update command.
func runProjectUpdate(cmd *cobra.Command, args []string) error {
	client, err := projectClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	info, err := client.Update(ctx, &projectSDK.UpdateProjectRequest{
		TenantID:         globalTenantID,
		ProjectID:        args[0],
		Name:             projectName,
		Description:      projectDescription,
		ClearDescription: projectClearDescription,
		WorkspaceAgentID: projectWorkspaceAgentID,
		WorkspacePath:    projectWorkspacePath,
		HostAffinity:     projectHostAffinity,
		DefaultLeadAgent: projectDefaultLeadAgent,
		GitRepoURL:       projectGitRepoURL,
		GitDefaultBranch: projectGitBranch,
		GitCurrentSHA:    projectGitCurrentSHA,
		TokenBudget:      projectTokenBudget,
		GPUMinutes:       projectGPUMinutes,
		MaxConcurrency:   projectMaxConcurrency,
		Metadata:         projectMetadata,
	})
	if err != nil {
		return formatError(err)
	}

	return formatProjectDetail(info, projectOutputFmt, os.Stdout)
}

// =============================================================================
// Delete Command
// =============================================================================

// newProjectDeleteCommand creates the project delete subcommand.
func newProjectDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <projectID>",
		Short: "Delete a project",
		Long: `Delete a project and its permission and quota records.

Examples:
  quicbaas project delete proj_abc123
  quicbaas project delete proj_abc123 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runProjectDelete,
	}
}

// runProjectDelete executes the project delete command.
func runProjectDelete(cmd *cobra.Command, args []string) error {
	client, err := projectClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := client.Delete(ctx, globalTenantID, args[0]); err != nil {
		return formatError(err)
	}

	return formatProjectDeleted(args[0], projectOutputFmt, os.Stdout)
}

// =============================================================================
// Grant / Revoke Agent Role Commands
// =============================================================================

// newProjectGrantRoleCommand creates the project grant-role subcommand.
func newProjectGrantRoleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grant-role <projectID>",
		Short: "Grant an agent a role in a project",
		Long: `Set or update the role an agent holds within a project.

Granting the "denied" role is equivalent to revoke-role.

Examples:
  quicbaas project grant-role proj_abc123 --agent agent_abc --role lead
  quicbaas project grant-role proj_abc123 --agent agent_xyz --role observer`,
		Args: cobra.ExactArgs(1),
		RunE: runProjectGrantRole,
	}

	cmd.Flags().StringVar(&projectAgentID, "agent", "", "Agent ID to grant the role to (required)")
	cmd.Flags().StringVar(&projectRole, "role", "", "Role to assign: "+strings.Join(projectRoleNames(), ", ")+" (required)")

	for _, name := range []string{"agent", "role"} {
		if err := cmd.MarkFlagRequired(name); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to mark %s flag as required: %v\n", name, err)
		}
	}

	return cmd
}

// runProjectGrantRole executes the project grant-role command.
func runProjectGrantRole(cmd *cobra.Command, args []string) error {
	role, err := parseProjectRole(projectRole)
	if err != nil {
		return formatError(err)
	}

	client, err := projectClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.GrantAgentRole(ctx, &projectSDK.GrantAgentRoleRequest{
		TenantID:  globalTenantID,
		ProjectID: args[0],
		AgentID:   projectAgentID,
		Role:      role,
	})
	if err != nil {
		return formatError(err)
	}

	return formatProjectRoleGrant(resp, projectOutputFmt, os.Stdout)
}

// newProjectRevokeRoleCommand creates the project revoke-role subcommand.
func newProjectRevokeRoleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke-role <projectID>",
		Short: "Revoke an agent's role in a project",
		Long: `Remove an agent's access to a project by setting its role to "denied".

Examples:
  quicbaas project revoke-role proj_abc123 --agent agent_abc`,
		Args: cobra.ExactArgs(1),
		RunE: runProjectRevokeRole,
	}

	cmd.Flags().StringVar(&projectAgentID, "agent", "", "Agent ID to revoke (required)")

	if err := cmd.MarkFlagRequired("agent"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark agent flag as required: %v\n", err)
	}

	return cmd
}

// runProjectRevokeRole executes the project revoke-role command.
func runProjectRevokeRole(cmd *cobra.Command, args []string) error {
	client, err := projectClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.RevokeAgentRole(ctx, &projectSDK.RevokeAgentRoleRequest{
		TenantID:  globalTenantID,
		ProjectID: args[0],
		AgentID:   projectAgentID,
	})
	if err != nil {
		return formatError(err)
	}

	return formatProjectRoleRevoke(resp, projectOutputFmt, os.Stdout)
}

// =============================================================================
// Formatting Functions
// =============================================================================

// formatProjectDetail renders a single project for output.
func formatProjectDetail(info *projectSDK.ProjectInfo, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(info)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(info)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID:\t%s\n", info.ID)
		fmt.Fprintf(tw, "Tenant ID:\t%s\n", info.TenantID)
		fmt.Fprintf(tw, "Name:\t%s\n", info.Name)
		if info.Description != "" {
			fmt.Fprintf(tw, "Description:\t%s\n", info.Description)
		}
		if info.Workspace.AgentID != "" {
			fmt.Fprintf(tw, "Workspace Agent:\t%s\n", info.Workspace.AgentID)
		}
		if info.Workspace.Path != "" {
			fmt.Fprintf(tw, "Workspace Path:\t%s\n", info.Workspace.Path)
		}
		if info.HostAffinity != "" {
			fmt.Fprintf(tw, "Host Affinity:\t%s\n", info.HostAffinity)
		}
		if info.DefaultLeadAgent != "" {
			fmt.Fprintf(tw, "Default Lead Agent:\t%s\n", info.DefaultLeadAgent)
		}
		if info.Git.RepoURL != "" {
			fmt.Fprintf(tw, "Git Repo:\t%s\n", info.Git.RepoURL)
		}
		if info.Git.DefaultBranch != "" {
			fmt.Fprintf(tw, "Git Branch:\t%s\n", info.Git.DefaultBranch)
		}
		if info.Git.CurrentSHA != "" {
			fmt.Fprintf(tw, "Git SHA:\t%s\n", info.Git.CurrentSHA)
		}
		fmt.Fprintf(tw, "Token Budget:\t%s\n", formatProjectQuota(info.Quotas.TokenBudget))
		fmt.Fprintf(tw, "GPU Minutes:\t%s\n", formatProjectQuota(info.Quotas.GPUMinutes))
		fmt.Fprintf(tw, "Max Concurrency:\t%s\n", formatProjectQuota(int64(info.Quotas.MaxConcurrency)))
		if !info.CreatedAt.IsZero() {
			fmt.Fprintf(tw, "Created:\t%s\n", info.CreatedAt.Format(time.RFC3339))
		}
		if !info.UpdatedAt.IsZero() {
			fmt.Fprintf(tw, "Updated:\t%s\n", info.UpdatedAt.Format(time.RFC3339))
		}
		if len(info.Permissions.AgentRoles) > 0 {
			fmt.Fprintf(tw, "\nAgent Roles:\n")
			for _, agentID := range sortedAgentIDs(info.Permissions.AgentRoles) {
				fmt.Fprintf(tw, "  %s:\t%s\n", agentID, info.Permissions.AgentRoles[agentID])
			}
		}
		if len(info.Metadata) > 0 {
			fmt.Fprintf(tw, "\nMetadata:\n")
			for _, k := range sortedKeys(info.Metadata) {
				fmt.Fprintf(tw, "  %s:\t%s\n", k, info.Metadata[k])
			}
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s: %s (tenant: %s)\n", info.ID, info.Name, info.TenantID)
		return nil
	}
}

// formatProjectList renders a list of projects for output.
func formatProjectList(resp *projectSDK.ListProjectsResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ID\tNAME\tLEAD AGENT\tGIT REPO\tUPDATED\n")
		for _, p := range resp.Projects {
			updated := "-"
			if !p.UpdatedAt.IsZero() {
				updated = p.UpdatedAt.Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				p.ID, p.Name, orDash(p.DefaultLeadAgent), orDash(p.Git.RepoURL), updated)
		}
		fmt.Fprintf(tw, "\nTotal: %d projects\n", resp.Total)
		return tw.Flush()

	default:
		for _, p := range resp.Projects {
			fmt.Fprintf(w, "%s: %s\n", p.ID, p.Name)
		}
		return nil
	}
}

// formatProjectDeleted renders a delete confirmation for output.
func formatProjectDeleted(projectID, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	result := map[string]interface{}{"project_id": projectID, "deleted": true}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(result)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Project ID:\t%s\n", projectID)
		fmt.Fprintf(tw, "Deleted:\t%v\n", true)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Project %s deleted\n", projectID)
		return nil
	}
}

// formatProjectRoleGrant renders a grant-role response for output.
func formatProjectRoleGrant(resp *projectSDK.GrantAgentRoleResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Project ID:\t%s\n", resp.ProjectID)
		fmt.Fprintf(tw, "Agent ID:\t%s\n", resp.AgentID)
		fmt.Fprintf(tw, "Role:\t%s\n", resp.Role)
		if !resp.Timestamp.IsZero() {
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Agent %s granted role %s in project %s\n", resp.AgentID, resp.Role, resp.ProjectID)
		return nil
	}
}

// formatProjectRoleRevoke renders a revoke-role response for output.
func formatProjectRoleRevoke(resp *projectSDK.RevokeAgentRoleResponse, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Project ID:\t%s\n", resp.ProjectID)
		fmt.Fprintf(tw, "Agent ID:\t%s\n", resp.AgentID)
		fmt.Fprintf(tw, "Revoked:\t%v\n", true)
		if !resp.Timestamp.IsZero() {
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Agent %s revoked from project %s\n", resp.AgentID, resp.ProjectID)
		return nil
	}
}

// formatProjectQuota renders a quota value, showing 0 as "unlimited".
func formatProjectQuota(v int64) string {
	if v == 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%d", v)
}

// orDash returns s, or "-" when s is empty, for stable table columns.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// sortedKeys returns the map keys in ascending order so table output is
// deterministic across runs.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedAgentIDs returns the agent IDs of a role map in ascending order.
func sortedAgentIDs(m map[string]projectSDK.ProjectRole) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
