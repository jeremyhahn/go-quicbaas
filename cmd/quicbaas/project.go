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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	agentsSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// =============================================================================
// Project Command Flags
// =============================================================================

var (
	// projectListPage pages, orders and searches `project list`.
	projectListPage pageFlags

	projectOutputFmt            string
	projectName                 string
	projectDescription          string
	projectClearDescription     bool
	projectWorkspaceFleetNodeID string
	projectWorkspacePath        string
	projectHostAffinity         string
	projectDefaultLeadAgent     string
	projectGitRepoURL           string
	projectGitBranch            string
	projectGitCurrentSHA        string
	projectDefaultWorkflow      string
	projectClearDefaultWF       bool
	projectAutonomous           bool
	projectContextRepos         []string
	projectClearContextRepo     bool
	projectGitAuthorName        string
	projectGitAuthorEmail       string
	projectGitSigningOn         bool
	projectGitSigningAlg        string
	projectGitSigningKey        string
	projectGitSigningScope      string
	projectGitSigningOwner      string
	projectGitSSHKey            string
	projectGitSSHKeyScope       string
	projectGitSSHKeyOwner       string
	projectGitRetireKey         string
	projectGitRetireScope       string
	projectGitRetireOwner       string
	projectGitRetiredAt         string
	projectGPUMinutes           int64
	projectMaxConcurrency       int
	projectWorkspaceTier        string
	projectWorkspaceTmpfsMB     int
	projectContainerMemMB       int
	projectSessionRotation      string
	projectMetadata             map[string]string
	projectFleetNodeID          string
	projectRole                 string
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

// InvalidContextRepoFlagError indicates a --context-repo value the CLI could
// not read as name=<n>,url=<u>,ref=<r>.
type InvalidContextRepoFlagError struct {
	// Value is the flag value as the operator typed it.
	Value string

	// Reason says what was wrong with it.
	Reason string
}

// Error implements the error interface.
func (e *InvalidContextRepoFlagError) Error() string {
	return "invalid --context-repo " + e.Value + ": " + e.Reason +
		" (expected name=<name>,url=<repo-url>,ref=<branch|tag|sha>)"
}

// contextRepoFlagKeys maps a --context-repo key to the field it fills, giving
// O(1) dispatch and one place to name the accepted vocabulary.
var contextRepoFlagKeys = map[string]func(*projectSDK.ContextRepo, string){
	"name": func(r *projectSDK.ContextRepo, v string) { r.Name = v },
	"url":  func(r *projectSDK.ContextRepo, v string) { r.RepoURL = v },
	"ref":  func(r *projectSDK.ContextRepo, v string) { r.Ref = v },
}

// parseContextRepoFlag reads one --context-repo value.
func parseContextRepoFlag(value string) (projectSDK.ContextRepo, error) {
	var repo projectSDK.ContextRepo
	for _, field := range strings.Split(value, ",") {
		key, val, found := strings.Cut(strings.TrimSpace(field), "=")
		if !found {
			return repo, &InvalidContextRepoFlagError{Value: value, Reason: field + " is not key=value"}
		}
		apply, known := contextRepoFlagKeys[strings.ToLower(strings.TrimSpace(key))]
		if !known {
			return repo, &InvalidContextRepoFlagError{Value: value, Reason: "unknown key " + key}
		}
		apply(&repo, strings.TrimSpace(val))
	}
	switch {
	case repo.Name == "":
		return repo, &InvalidContextRepoFlagError{Value: value, Reason: "name is required"}
	case repo.RepoURL == "":
		return repo, &InvalidContextRepoFlagError{Value: value, Reason: "url is required"}
	case repo.Ref == "":
		return repo, &InvalidContextRepoFlagError{Value: value, Reason: "ref is required"}
	}
	return repo, nil
}

// parseContextRepoFlags reads every --context-repo value. It returns nil when
// the operator passed none, so an update leaves the list alone.
func parseContextRepoFlags(values []string) ([]projectSDK.ContextRepo, error) {
	if len(values) == 0 {
		return nil, nil
	}
	repos := make([]projectSDK.ContextRepo, 0, len(values))
	for _, value := range values {
		repo, err := parseContextRepoFlag(value)
		if err != nil {
			return nil, err
		}
		repos = append(repos, repo)
	}
	return repos, nil
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
  quicbaas project update proj_abc123 --spend-window name=weekly,size=168h,usd=250
  quicbaas project grant-role proj_abc123 --node node_abc --role lead
  quicbaas project delete proj_abc123`,
	}

	cmd.PersistentFlags().StringVar(&projectOutputFmt, "output", "table", "Output format (json, yaml, table)")

	cmd.AddCommand(newProjectCreateCommand())
	cmd.AddCommand(newProjectListCommand())
	cmd.AddCommand(newProjectGetCommand())
	cmd.AddCommand(newProjectUpdateCommand())
	cmd.AddCommand(newProjectDeleteCommand())
	cmd.AddCommand(newProjectParkCommand())
	cmd.AddCommand(newProjectResumeCommand())
	cmd.AddCommand(newProjectGrantRoleCommand())
	cmd.AddCommand(newProjectRevokeRoleCommand())

	return cmd
}

// addProjectMutationFlags registers the flags shared by create and update.
func addProjectMutationFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&projectDescription, "description", "", "Human-readable project description")
	cmd.Flags().StringVar(&projectWorkspaceFleetNodeID, "workspace-node", "", "ID of the fleet node that owns the workspace")
	cmd.Flags().StringVar(&projectWorkspacePath, "workspace-path", "", "Filesystem path to the workspace on the agent host")
	cmd.Flags().StringVar(&projectHostAffinity, "host-affinity", "", "Pin project operations to a preferred agent host")
	cmd.Flags().StringVar(&projectDefaultLeadAgent, "default-lead-agent", "", "The Agent or pool that leads a job under this project: <agent-id>, agent:<id> or pool:<id>")
	cmd.Flags().StringVar(&projectGitRepoURL, "git-repo", "", "Remote git repository URL")
	cmd.Flags().StringVar(&projectGitBranch, "git-branch", "", "Default git branch (e.g. main)")
	cmd.Flags().StringVar(&projectDefaultWorkflow, "default-workflow", "", "Tenant-scoped workflow a Task auto-spawns into (must be registered)")
	cmd.Flags().BoolVar(&projectAutonomous, "autonomous", false, "Turn autonomous development on: the poller adopts work and the dispatcher starts jobs for this project")
	cmd.Flags().StringArrayVar(&projectContextRepos, "context-repo", nil, "Repository the worker clones beside the checkout, as name=<name>,url=<repo-url>,ref=<branch|tag|sha> (repeatable)")
	cmd.Flags().StringVar(&projectGitAuthorName, "git-author-name", "", "Author name recorded on commits made from the workspace")
	cmd.Flags().StringVar(&projectGitAuthorEmail, "git-author-email", "", "Author email recorded on commits made from the workspace")
	cmd.Flags().BoolVar(&projectGitSigningOn, "git-signing", false, "Sign commits with the KMS-held signing key (requires --git-signing-key)")
	cmd.Flags().StringVar(&projectGitSigningAlg, "git-signing-algorithm", "", "Signing key algorithm: ED25519 (default) or ECDSA-P256 (FIPS)")
	cmd.Flags().StringVar(&projectGitSigningKey, "git-signing-key", "", "KMS signing key name")
	cmd.Flags().StringVar(&projectGitSigningScope, "git-signing-key-scope", "", "KMS signing key scope (user, tenant, service)")
	cmd.Flags().StringVar(&projectGitSigningOwner, "git-signing-key-owner", "", "KMS signing key owner")
	cmd.Flags().StringVar(&projectGitSSHKey, "git-ssh-key", "", "KMS SSH key an ssh:// repository is reached with (defaults to the signing key)")
	cmd.Flags().StringVar(&projectGitSSHKeyScope, "git-ssh-key-scope", "", "KMS SSH key scope (user, tenant, service)")
	cmd.Flags().StringVar(&projectGitSSHKeyOwner, "git-ssh-key-owner", "", "KMS SSH key owner")
	addSpendFlags(cmd)
	cmd.Flags().Int64Var(&projectGPUMinutes, "gpu-minutes", 0, "Maximum GPU compute budget in minutes (0 = unlimited)")
	cmd.Flags().IntVar(&projectMaxConcurrency, "max-concurrency", 0, "Maximum concurrent workflow runs (0 = unlimited)")
	cmd.Flags().StringVar(&projectWorkspaceTier, "workspace-tier", "", "Storage tier of the sandboxed checkout: persistent (host bind mount) or ephemeral (sized tmpfs in the container)")
	cmd.Flags().IntVar(&projectWorkspaceTmpfsMB, "workspace-tmpfs-mb", 0, "Size of the ephemeral tier's tmpfs checkout in MiB (0 = unsized; ephemeral tier only)")
	cmd.Flags().IntVar(&projectContainerMemMB, "container-memory-mb", 0, "Container memory limit in MiB (0 = the runtime's default)")
	cmd.Flags().StringVar(&projectSessionRotation, "session-rotation", "", "What a persistent worker session does when its pooled provider account rotates: resume (continue the same conversation; the default) or fresh (start a new conversation on the same branch)")
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
  quicbaas project create --name ml-train --workspace-node node_abc \
    --workspace-path /data/projects/ml-train --spend-window name=daily,size=24h,tokens=1000000 --output json`,
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

	contextRepos, err := parseContextRepoFlags(projectContextRepos)
	if err != nil {
		return formatError(err)
	}
	spendLimits, err := spendLimitsFromFlags(cmd)
	if err != nil {
		return formatError(err)
	}
	var projectSpendOnCreate spend.Limits
	if spendLimits != nil {
		projectSpendOnCreate = *spendLimits
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	defaultLead, err := agentsSDK.ParseAgentRef(projectDefaultLeadAgent)
	if err != nil {
		return formatError(err)
	}
	info, err := client.Create(ctx, &projectSDK.CreateProjectRequest{
		TenantID:             globalTenantID,
		Name:                 projectName,
		Description:          projectDescription,
		WorkspaceFleetNodeID: projectWorkspaceFleetNodeID,
		WorkspacePath:        projectWorkspacePath,
		HostAffinity:         projectHostAffinity,
		DefaultLeadAgent:     defaultLead,
		GitRepoURL:           projectGitRepoURL,
		GitDefaultBranch:     projectGitBranch,
		Spend:                projectSpendOnCreate,
		GPUMinutes:           projectGPUMinutes,
		MaxConcurrency:       projectMaxConcurrency,
		Metadata:             projectMetadata,

		DefaultWorkflow:     projectDefaultWorkflow,
		Autonomous:          projectAutonomous,
		ContextRepos:        contextRepos,
		GitAuthorName:       projectGitAuthorName,
		GitAuthorEmail:      projectGitAuthorEmail,
		GitSigningEnabled:   projectGitSigningOn,
		GitSigningAlgorithm: projectGitSigningAlg,
		GitSigningKeyName:   projectGitSigningKey,
		GitSigningKeyScope:  projectGitSigningScope,
		GitSigningKeyOwner:  projectGitSigningOwner,
		GitSSHKeyName:       projectGitSSHKey,
		GitSSHKeyScope:      projectGitSSHKeyScope,
		GitSSHKeyOwner:      projectGitSSHKeyOwner,
		WorkspaceTier:       projectSDK.WorkspaceTier(projectWorkspaceTier),
		SessionRotation:     projectSDK.SessionRotationPolicy(projectSessionRotation),
		WorkspaceTmpfsMB:    projectWorkspaceTmpfsMB,
		ContainerMemoryMB:   projectContainerMemMB,
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
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List projects",
		Long: `List the projects in the tenant named by --tenant, one page at a time.

Examples:
  quicbaas project list
  quicbaas project list --tenant acme --output json
  quicbaas project list --search web --sort name:desc --limit 50
  quicbaas project list --all`,
		Args: cobra.NoArgs,
		RunE: runProjectList,
	}
	projectListPage.bind(cmd)
	return cmd
}

// runProjectList executes the project list command.
func runProjectList(cmd *cobra.Command, args []string) error {
	client, err := projectClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	projects, env, err := fetchPages(&projectListPage, func(params page.Params) ([]*projectSDK.ProjectInfo, page.Envelope, error) {
		resp, err := client.List(ctx, &projectSDK.ListProjectsRequest{TenantID: globalTenantID, Params: params})
		if err != nil || resp == nil {
			return nil, page.Envelope{}, err
		}
		return resp.Projects, resp.Envelope, nil
	})
	if err != nil {
		return formatError(err)
	}

	resp := &projectSDK.ListProjectsResponse{Projects: projects, Envelope: env}
	return formatProjectList(resp, projectOutputFmt, cmd.OutOrStdout())
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
  quicbaas project update proj_abc123 --gpu-minutes 120 --spend-window name=monthly,size=720h,usd=500
  quicbaas project update proj_abc123 --clear-spend
  quicbaas project update proj_abc123 --clear-description
  quicbaas project update proj_abc123 --git-sha 3f1a9c2 --output json
  quicbaas project update proj_abc123 --git-signing-key signing-kms \
      --git-signing-retire-key signing --git-signing-retired-at 2026-09-24T22:15:00Z`,
		Args: cobra.ExactArgs(1),
		RunE: runProjectUpdate,
	}

	cmd.Flags().StringVar(&projectName, "name", "", "New project name")
	cmd.Flags().BoolVar(&projectClearDescription, "clear-description", false, "Remove the project description")
	cmd.Flags().StringVar(&projectGitCurrentSHA, "git-sha", "", "Record the latest observed commit SHA")
	cmd.Flags().BoolVar(&projectClearDefaultWF, "clear-default-workflow", false, "Remove the default workflow so Tasks are never auto-spawned")
	cmd.Flags().BoolVar(&projectClearContextRepo, "clear-context-repos", false, "Remove every context repository from the project")
	cmd.Flags().StringVar(&projectGitRetireKey, "git-signing-retire-key", "", "KMS key the project signed with before its current one; kept in the trusted signing history so commits it signed before its retirement still verify")
	cmd.Flags().StringVar(&projectGitRetireScope, "git-signing-retire-key-scope", "", "Scope of the key being retired (user, tenant, service)")
	cmd.Flags().StringVar(&projectGitRetireOwner, "git-signing-retire-key-owner", "", "Owner of the key being retired (defaults to the project's tenant)")
	cmd.Flags().StringVar(&projectGitRetiredAt, "git-signing-retired-at", "", "When the retired key stopped being the project's signing key, RFC 3339 (default now)")
	addProjectMutationFlags(cmd)

	return cmd
}

// gitSigningFlag reports the --git-signing value only when the operator
// passed it, so an update leaves signing unchanged unless told otherwise.
func gitSigningFlag(cmd *cobra.Command) *bool {
	if cmd == nil || !cmd.Flags().Changed("git-signing") {
		return nil
	}
	enabled := projectGitSigningOn
	return &enabled
}

// autonomousFlag reports the --autonomous value only when the operator passed
// it, so an update leaves autonomous development unchanged unless told
// otherwise.
func autonomousFlag(cmd *cobra.Command) *bool {
	if cmd == nil || !cmd.Flags().Changed("autonomous") {
		return nil
	}
	enabled := projectAutonomous
	return &enabled
}

// quotaFlagInt64 reports a numeric quota only when the operator passed the
// flag, so an update leaves it unchanged unless told otherwise.
//
// PRESENCE, NOT NON-ZERO, because zero is a real value here: a quota of zero
// means UNLIMITED. Reading the variable alone could not tell "--gpu-minutes 0"
// from "flag not given", so lifting a cap was unexpressible.
func quotaFlagInt64(cmd *cobra.Command, name string, value int64) *int64 {
	if cmd == nil || !cmd.Flags().Changed(name) {
		return nil
	}
	v := value
	return &v
}

// quotaFlagInt is quotaFlagInt64 for the int-valued quota and storage sizes,
// where zero is likewise real: an unsized tmpfs, the runtime's default memory
// limit.
func quotaFlagInt(cmd *cobra.Command, name string, value int) *int {
	if cmd == nil || !cmd.Flags().Changed(name) {
		return nil
	}
	v := value
	return &v
}

// runProjectUpdate executes the project update command.
func runProjectUpdate(cmd *cobra.Command, args []string) error {
	client, err := projectClientFactory()
	if err != nil {
		return formatError(err)
	}

	contextRepos, err := parseContextRepoFlags(projectContextRepos)
	if err != nil {
		return formatError(err)
	}
	spendUpdate, err := spendLimitsFromFlags(cmd)
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	defaultLead, err := agentsSDK.ParseAgentRef(projectDefaultLeadAgent)
	if err != nil {
		return formatError(err)
	}
	info, err := client.Update(ctx, &projectSDK.UpdateProjectRequest{
		TenantID:             globalTenantID,
		ProjectID:            args[0],
		Name:                 projectName,
		Description:          projectDescription,
		ClearDescription:     projectClearDescription,
		WorkspaceFleetNodeID: projectWorkspaceFleetNodeID,
		WorkspacePath:        projectWorkspacePath,
		HostAffinity:         projectHostAffinity,
		DefaultLeadAgent:     defaultLead,
		GitRepoURL:           projectGitRepoURL,
		GitDefaultBranch:     projectGitBranch,
		GitCurrentSHA:        projectGitCurrentSHA,
		Spend:                spendUpdate,
		GPUMinutes:           quotaFlagInt64(cmd, "gpu-minutes", projectGPUMinutes),
		MaxConcurrency:       quotaFlagInt(cmd, "max-concurrency", projectMaxConcurrency),
		Metadata:             projectMetadata,

		DefaultWorkflow:          projectDefaultWorkflow,
		ClearDefaultWorkflow:     projectClearDefaultWF,
		Autonomous:               autonomousFlag(cmd),
		ContextRepos:             contextRepos,
		ClearContextRepos:        projectClearContextRepo,
		GitAuthorName:            projectGitAuthorName,
		GitAuthorEmail:           projectGitAuthorEmail,
		GitSigningEnabled:        gitSigningFlag(cmd),
		GitSigningAlgorithm:      projectGitSigningAlg,
		GitSigningKeyName:        projectGitSigningKey,
		GitSigningKeyScope:       projectGitSigningScope,
		GitSigningKeyOwner:       projectGitSigningOwner,
		GitSSHKeyName:            projectGitSSHKey,
		GitSSHKeyScope:           projectGitSSHKeyScope,
		GitSSHKeyOwner:           projectGitSSHKeyOwner,
		GitSigningRetireKeyName:  projectGitRetireKey,
		GitSigningRetireKeyScope: projectGitRetireScope,
		GitSigningRetireKeyOwner: projectGitRetireOwner,
		GitSigningRetiredAt:      projectGitRetiredAt,
		WorkspaceTier:            projectSDK.WorkspaceTier(projectWorkspaceTier),
		SessionRotation:          projectSDK.SessionRotationPolicy(projectSessionRotation),
		WorkspaceTmpfsMB:         quotaFlagInt(cmd, "workspace-tmpfs-mb", projectWorkspaceTmpfsMB),
		ContainerMemoryMB:        quotaFlagInt(cmd, "container-memory-mb", projectContainerMemMB),
	})
	if err != nil {
		return formatError(err)
	}

	return formatProjectDetail(info, projectOutputFmt, os.Stdout)
}

// =============================================================================
// Park / Resume Commands
// =============================================================================

// newProjectParkCommand creates the project park subcommand.
func newProjectParkCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "park <projectID>",
		Short: "Close a project's intake and let its work in flight finish",
		Long: `Park a project: no task that has not started is dispatched, no issue is
adopted or triaged, and every task already started runs through work, pull
request, review and merge train to its normal end. Nothing is cancelled and
nothing is deleted. Autonomous development is left as it is.

Resume the project to reopen its intake.

Examples:
  quicbaas project park proj_abc123
  quicbaas project park proj_abc123 --output json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectSetParked(args[0], true)
		},
	}
}

// newProjectResumeCommand creates the project resume subcommand.
func newProjectResumeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <projectID>",
		Short: "Reopen a parked project's intake",
		Long: `Resume a parked project: the next sweep dispatches its queued tasks and
adopts and triages new work again. Autonomous development is left as it is.

Examples:
  quicbaas project resume proj_abc123`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectSetParked(args[0], false)
		},
	}
}

// runProjectSetParked parks or resumes one project through the project update
// path, which is the one verb every transport carries.
func runProjectSetParked(projectID string, parked bool) error {
	client, err := projectClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	info, err := client.Update(ctx, &projectSDK.UpdateProjectRequest{
		TenantID:  globalTenantID,
		ProjectID: projectID,
		Parked:    &parked,
	})
	if err != nil {
		return formatError(err)
	}
	return formatProjectDetail(info, projectOutputFmt, os.Stdout)
}

// formatProjectParked renders a project's park state for the detail view.
func formatProjectParked(info *projectSDK.ProjectInfo) string {
	if !info.Parked {
		return "no"
	}
	state := "yes"
	if !info.ParkedAt.IsZero() {
		state += ", since " + info.ParkedAt.Format(time.RFC3339)
	}
	if info.ParkedBy != "" {
		state += " by " + info.ParkedBy
	}
	return state
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
		Short: "Grant a fleet node a role in a project",
		Long: `Set or update the role a fleet node holds within a project.

Granting the "denied" role is equivalent to revoke-role.

Examples:
  quicbaas project grant-role proj_abc123 --node node_abc --role lead
  quicbaas project grant-role proj_abc123 --node node_xyz --role observer`,
		Args: cobra.ExactArgs(1),
		RunE: runProjectGrantRole,
	}

	cmd.Flags().StringVar(&projectFleetNodeID, "node", "", "Fleet node ID to grant the role to (required)")
	cmd.Flags().StringVar(&projectRole, "role", "", "Role to assign: "+strings.Join(projectRoleNames(), ", ")+" (required)")

	for _, name := range []string{"node", "role"} {
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

	resp, err := client.GrantFleetNodeRole(ctx, &projectSDK.GrantFleetNodeRoleRequest{
		TenantID:    globalTenantID,
		ProjectID:   args[0],
		FleetNodeID: projectFleetNodeID,
		Role:        role,
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
		Short: "Revoke a fleet node's role in a project",
		Long: `Remove a fleet node's access to a project by setting its role to "denied".

Examples:
  quicbaas project revoke-role proj_abc123 --node node_abc`,
		Args: cobra.ExactArgs(1),
		RunE: runProjectRevokeRole,
	}

	cmd.Flags().StringVar(&projectFleetNodeID, "node", "", "Fleet node ID to revoke (required)")

	if err := cmd.MarkFlagRequired("node"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark node flag as required: %v\n", err)
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

	resp, err := client.RevokeFleetNodeRole(ctx, &projectSDK.RevokeFleetNodeRoleRequest{
		TenantID:    globalTenantID,
		ProjectID:   args[0],
		FleetNodeID: projectFleetNodeID,
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
		if info.Workspace.FleetNodeID != "" {
			fmt.Fprintf(tw, "Workspace Agent:\t%s\n", info.Workspace.FleetNodeID)
		}
		if info.Workspace.Path != "" {
			fmt.Fprintf(tw, "Workspace Path:\t%s\n", info.Workspace.Path)
		}
		if info.HostAffinity != "" {
			fmt.Fprintf(tw, "Host Affinity:\t%s\n", info.HostAffinity)
		}
		if !info.DefaultLeadAgent.IsZero() {
			fmt.Fprintf(tw, "Default Lead Agent:\t%s\n", info.DefaultLeadAgent.String())
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
		if info.DefaultWorkflow != "" {
			fmt.Fprintf(tw, "Default Workflow:\t%s\n", info.DefaultWorkflow)
		}
		fmt.Fprintf(tw, "Autonomous:\t%v\n", info.Autonomous)
		fmt.Fprintf(tw, "Parked:\t%s\n", formatProjectParked(info))
		if info.Git.Author.Name != "" || info.Git.Author.Email != "" {
			fmt.Fprintf(tw, "Git Author:\t%s <%s>\n", info.Git.Author.Name, info.Git.Author.Email)
		}
		if info.Git.Signing.Enabled {
			fmt.Fprintf(tw, "Git Signing:\t%s (%s)\n", info.Git.Signing.Key.Name, orDash(info.Git.Signing.Algorithm))
		}
		if info.Git.SSHKey.Name != "" {
			fmt.Fprintf(tw, "Git SSH Key:\t%s\n", info.Git.SSHKey.Name)
		}
		fmt.Fprintf(tw, "Spend Limits:\t%s\n", formatSpendLimits(info.Spend))
		fmt.Fprintf(tw, "GPU Minutes:\t%s\n", formatProjectQuota(info.Quotas.GPUMinutes))
		fmt.Fprintf(tw, "Max Concurrency:\t%s\n", formatProjectQuota(int64(info.Quotas.MaxConcurrency)))
		if info.Storage.WorkspaceTier != "" {
			fmt.Fprintf(tw, "Workspace Tier:\t%s\n", info.Storage.WorkspaceTier)
		}
		if info.Storage.WorkspaceTmpfsMB != 0 {
			fmt.Fprintf(tw, "Workspace Tmpfs:\t%d MiB\n", info.Storage.WorkspaceTmpfsMB)
		}
		if info.Storage.ContainerMemoryMB != 0 {
			fmt.Fprintf(tw, "Container Memory:\t%d MiB\n", info.Storage.ContainerMemoryMB)
		}
		fmt.Fprintf(tw, "Session Rotation:\t%s\n", formatSessionRotation(info.SessionRotation))
		if !info.CreatedAt.IsZero() {
			fmt.Fprintf(tw, "Created:\t%s\n", info.CreatedAt.Format(time.RFC3339))
		}
		if !info.UpdatedAt.IsZero() {
			fmt.Fprintf(tw, "Updated:\t%s\n", info.UpdatedAt.Format(time.RFC3339))
		}
		if len(info.Permissions.FleetNodeRoles) > 0 {
			fmt.Fprintf(tw, "\nAgent Roles:\n")
			for _, agentID := range sortedAgentIDs(info.Permissions.FleetNodeRoles) {
				fmt.Fprintf(tw, "  %s:\t%s\n", agentID, info.Permissions.FleetNodeRoles[agentID])
			}
		}
		if len(info.ContextRepos) > 0 {
			fmt.Fprintf(tw, "\nContext Repos:\n")
			for _, repo := range info.ContextRepos {
				fmt.Fprintf(tw, "  %s:\t%s@%s\n", repo.Name, repo.RepoURL, repo.Ref)
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
				p.ID, p.Name, orDash(p.DefaultLeadAgent.String()), orDash(p.Git.RepoURL), updated)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		writePageFooter(w, len(resp.Projects), resp.Envelope)
		return nil

	default:
		for _, p := range resp.Projects {
			fmt.Fprintf(w, "%s: %s\n", p.ID, p.Name)
		}
		writePageFooter(w, len(resp.Projects), resp.Envelope)
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
func formatProjectRoleGrant(resp *projectSDK.GrantFleetNodeRoleResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Agent ID:\t%s\n", resp.FleetNodeID)
		fmt.Fprintf(tw, "Role:\t%s\n", resp.Role)
		if !resp.Timestamp.IsZero() {
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Agent %s granted role %s in project %s\n", resp.FleetNodeID, resp.Role, resp.ProjectID)
		return nil
	}
}

// formatProjectRoleRevoke renders a revoke-role response for output.
func formatProjectRoleRevoke(resp *projectSDK.RevokeFleetNodeRoleResponse, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Agent ID:\t%s\n", resp.FleetNodeID)
		fmt.Fprintf(tw, "Revoked:\t%v\n", true)
		if !resp.Timestamp.IsZero() {
			fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Agent %s revoked from project %s\n", resp.FleetNodeID, resp.ProjectID)
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

// formatSessionRotation renders a project's session rotation policy, naming
// the default for a project that never chose one.
func formatSessionRotation(policy projectSDK.SessionRotationPolicy) string {
	if policy == projectSDK.SessionRotationUnset {
		return string(projectSDK.SessionRotationResume) + " (default)"
	}
	return string(policy)
}
