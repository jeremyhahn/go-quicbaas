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

// Package main: command-handler coverage for project.go.
package main

import (
	"bytes"
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// =============================================================================
// Mock
// =============================================================================

// mockProjectClient implements projectSDK.Client for command-handler tests.
// Unset function fields return benign zero-value responses so each test only
// wires the call it exercises.
type mockProjectClient struct {
	createFn     func(ctx context.Context, req *projectSDK.CreateProjectRequest) (*projectSDK.ProjectInfo, error)
	getFn        func(ctx context.Context, tenantID, projectID string) (*projectSDK.ProjectInfo, error)
	getByNameFn  func(ctx context.Context, tenantID, name string) (*projectSDK.ProjectInfo, error)
	updateFn     func(ctx context.Context, req *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error)
	deleteFn     func(ctx context.Context, tenantID, projectID string) error
	listFn       func(ctx context.Context, tenantID string) (*projectSDK.ListProjectsResponse, error)
	grantRoleFn  func(ctx context.Context, req *projectSDK.GrantAgentRoleRequest) (*projectSDK.GrantAgentRoleResponse, error)
	revokeRoleFn func(ctx context.Context, req *projectSDK.RevokeAgentRoleRequest) (*projectSDK.RevokeAgentRoleResponse, error)
}

func (m *mockProjectClient) Create(ctx context.Context, req *projectSDK.CreateProjectRequest) (*projectSDK.ProjectInfo, error) {
	if m.createFn != nil {
		return m.createFn(ctx, req)
	}
	return &projectSDK.ProjectInfo{ID: "proj_1", Name: req.Name, TenantID: req.TenantID}, nil
}

func (m *mockProjectClient) Get(ctx context.Context, tenantID, projectID string) (*projectSDK.ProjectInfo, error) {
	if m.getFn != nil {
		return m.getFn(ctx, tenantID, projectID)
	}
	return &projectSDK.ProjectInfo{ID: projectID, TenantID: tenantID}, nil
}

func (m *mockProjectClient) GetByName(ctx context.Context, tenantID, name string) (*projectSDK.ProjectInfo, error) {
	if m.getByNameFn != nil {
		return m.getByNameFn(ctx, tenantID, name)
	}
	return &projectSDK.ProjectInfo{ID: "proj_1", Name: name, TenantID: tenantID}, nil
}

func (m *mockProjectClient) Update(ctx context.Context, req *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, req)
	}
	return &projectSDK.ProjectInfo{ID: req.ProjectID, Name: req.Name, TenantID: req.TenantID}, nil
}

func (m *mockProjectClient) Delete(ctx context.Context, tenantID, projectID string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, tenantID, projectID)
	}
	return nil
}

func (m *mockProjectClient) List(ctx context.Context, tenantID string) (*projectSDK.ListProjectsResponse, error) {
	if m.listFn != nil {
		return m.listFn(ctx, tenantID)
	}
	return &projectSDK.ListProjectsResponse{}, nil
}

func (m *mockProjectClient) GrantAgentRole(ctx context.Context, req *projectSDK.GrantAgentRoleRequest) (*projectSDK.GrantAgentRoleResponse, error) {
	if m.grantRoleFn != nil {
		return m.grantRoleFn(ctx, req)
	}
	return &projectSDK.GrantAgentRoleResponse{ProjectID: req.ProjectID, AgentID: req.AgentID, Role: req.Role}, nil
}

func (m *mockProjectClient) RevokeAgentRole(ctx context.Context, req *projectSDK.RevokeAgentRoleRequest) (*projectSDK.RevokeAgentRoleResponse, error) {
	if m.revokeRoleFn != nil {
		return m.revokeRoleFn(ctx, req)
	}
	return &projectSDK.RevokeAgentRoleResponse{ProjectID: req.ProjectID, AgentID: req.AgentID}, nil
}

// =============================================================================
// Helpers
// =============================================================================

// withMockProject installs a projectClientFactory returning the supplied mock
// and restores the previous factory, timeout, and flag state on cleanup.
func withMockProject(t *testing.T, mc *mockProjectClient) {
	t.Helper()
	origFactory := projectClientFactory
	origTimeout := globalTimeout
	origTenant := globalTenantID
	origOutput := projectOutputFmt

	projectClientFactory = func() (projectSDK.Client, error) { return mc, nil }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	globalTenantID = "acme"
	projectOutputFmt = "table"

	t.Cleanup(func() {
		projectClientFactory = origFactory
		globalTimeout = origTimeout
		globalTenantID = origTenant
		projectOutputFmt = origOutput
		resetProjectFlags()
	})
}

// withFailingProjectFactory installs a projectClientFactory that always fails.
func withFailingProjectFactory(t *testing.T, err error) {
	t.Helper()
	orig := projectClientFactory
	projectClientFactory = func() (projectSDK.Client, error) { return nil, err }
	t.Cleanup(func() { projectClientFactory = orig })
}

// resetProjectFlags clears every package-level project flag.
func resetProjectFlags() {
	projectName = ""
	projectDescription = ""
	projectClearDescription = false
	projectWorkspaceAgentID = ""
	projectWorkspacePath = ""
	projectHostAffinity = ""
	projectDefaultLeadAgent = ""
	projectGitRepoURL = ""
	projectGitBranch = ""
	projectGitCurrentSHA = ""
	projectTokenBudget = 0
	projectGPUMinutes = 0
	projectMaxConcurrency = 0
	projectMetadata = nil
	projectAgentID = ""
	projectRole = ""
	projectDefaultWorkflow = ""
	projectClearDefaultWF = false
	projectAutonomous = false
	projectContextRepos = nil
	projectClearContextRepo = false
}

// =============================================================================
// Command Tree
// =============================================================================

// TestNewProjectCommand_Tree verifies the full CRUD surface is wired, so the
// CLI stays at parity with the REST project routes.
func TestNewProjectCommand_Tree(t *testing.T) {
	cmd := newProjectCommand()

	want := []string{"create", "list", "get", "update", "delete", "grant-role", "revoke-role"}
	got := make(map[string]bool, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for _, name := range want {
		assert.True(t, got[name], "missing project subcommand %q", name)
	}

	assert.NotNil(t, cmd.PersistentFlags().Lookup("output"), "project group must accept --output")
}

// =============================================================================
// Create
// =============================================================================

func TestRunProjectCreate_Happy(t *testing.T) {
	var captured *projectSDK.CreateProjectRequest
	mc := &mockProjectClient{
		createFn: func(_ context.Context, req *projectSDK.CreateProjectRequest) (*projectSDK.ProjectInfo, error) {
			captured = req
			return &projectSDK.ProjectInfo{ID: "proj_1", Name: req.Name, TenantID: req.TenantID}, nil
		},
	}
	withMockProject(t, mc)

	// Construct first: registering a flag assigns its default to the
	// package-level variable, which would clobber values set beforehand.
	cmd := newProjectCreateCommand()
	projectName = "web-api"
	projectDescription = "Public API"
	projectGitRepoURL = "https://github.com/acme/web-api"
	projectTokenBudget = 1000

	require.NoError(t, runProjectCreate(cmd, nil))

	require.NotNil(t, captured)
	assert.Equal(t, "web-api", captured.Name)
	assert.Equal(t, "acme", captured.TenantID, "create must scope to the global --tenant")
	assert.Equal(t, "Public API", captured.Description)
	assert.Equal(t, "https://github.com/acme/web-api", captured.GitRepoURL)
	assert.Equal(t, int64(1000), captured.TokenBudget)
}

func TestRunProjectCreate_FactoryError(t *testing.T) {
	withFailingProjectFactory(t, stderrors.New("no transport"))

	cmd := newProjectCreateCommand()
	err := runProjectCreate(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no transport")
}

func TestRunProjectCreate_TransportError(t *testing.T) {
	mc := &mockProjectClient{
		createFn: func(_ context.Context, _ *projectSDK.CreateProjectRequest) (*projectSDK.ProjectInfo, error) {
			return nil, stderrors.New("create boom")
		},
	}
	withMockProject(t, mc)
	cmd := newProjectCreateCommand()
	projectName = "web-api"

	require.Error(t, runProjectCreate(cmd, nil))
}

// =============================================================================
// List
// =============================================================================

func TestRunProjectList_Happy(t *testing.T) {
	var capturedTenant string
	mc := &mockProjectClient{
		listFn: func(_ context.Context, tenantID string) (*projectSDK.ListProjectsResponse, error) {
			capturedTenant = tenantID
			return &projectSDK.ListProjectsResponse{
				Projects: []*projectSDK.ProjectInfo{{ID: "proj_1", Name: "web-api"}},
				Total:    1,
			}, nil
		},
	}
	withMockProject(t, mc)

	require.NoError(t, runProjectList(newProjectListCommand(), nil))
	assert.Equal(t, "acme", capturedTenant)
}

func TestRunProjectList_TransportError(t *testing.T) {
	mc := &mockProjectClient{
		listFn: func(_ context.Context, _ string) (*projectSDK.ListProjectsResponse, error) {
			return nil, stderrors.New("list boom")
		},
	}
	withMockProject(t, mc)

	require.Error(t, runProjectList(newProjectListCommand(), nil))
}

// =============================================================================
// Get
// =============================================================================

func TestRunProjectGet_ByID(t *testing.T) {
	var capturedID string
	mc := &mockProjectClient{
		getFn: func(_ context.Context, _, projectID string) (*projectSDK.ProjectInfo, error) {
			capturedID = projectID
			return &projectSDK.ProjectInfo{ID: projectID, Name: "web-api"}, nil
		},
		getByNameFn: func(_ context.Context, _, _ string) (*projectSDK.ProjectInfo, error) {
			t.Fatal("GetByName must not be used when a project ID argument is given")
			return nil, nil
		},
	}
	withMockProject(t, mc)

	require.NoError(t, runProjectGet(newProjectGetCommand(), []string{"proj_42"}))
	assert.Equal(t, "proj_42", capturedID)
}

func TestRunProjectGet_ByName(t *testing.T) {
	var capturedName string
	mc := &mockProjectClient{
		getByNameFn: func(_ context.Context, _, name string) (*projectSDK.ProjectInfo, error) {
			capturedName = name
			return &projectSDK.ProjectInfo{ID: "proj_1", Name: name}, nil
		},
	}
	withMockProject(t, mc)
	cmd := newProjectGetCommand()
	projectName = "web-api"

	require.NoError(t, runProjectGet(cmd, nil))
	assert.Equal(t, "web-api", capturedName)
}

// TestRunProjectGet_NoSelector verifies get refuses to run without an ID or a
// --name, rather than issuing a request that cannot identify a project.
func TestRunProjectGet_NoSelector(t *testing.T) {
	mc := &mockProjectClient{
		getFn: func(_ context.Context, _, _ string) (*projectSDK.ProjectInfo, error) {
			t.Fatal("no lookup should be attempted without a selector")
			return nil, nil
		},
	}
	withMockProject(t, mc)

	err := runProjectGet(newProjectGetCommand(), nil)
	require.Error(t, err)

	var selErr *ProjectSelectorRequiredError
	require.True(t, stderrors.As(err, &selErr))
}

// =============================================================================
// Update
// =============================================================================

func TestRunProjectUpdate_Happy(t *testing.T) {
	var captured *projectSDK.UpdateProjectRequest
	mc := &mockProjectClient{
		updateFn: func(_ context.Context, req *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
			captured = req
			return &projectSDK.ProjectInfo{ID: req.ProjectID}, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectUpdateCommand()
	projectClearDescription = true
	projectGitCurrentSHA = "3f1a9c2"

	require.NoError(t, runProjectUpdate(cmd, []string{"proj_42"}))

	require.NotNil(t, captured)
	assert.Equal(t, "proj_42", captured.ProjectID)
	assert.True(t, captured.ClearDescription)
	assert.Equal(t, "3f1a9c2", captured.GitCurrentSHA)
}

func TestRunProjectUpdate_TransportError(t *testing.T) {
	mc := &mockProjectClient{
		updateFn: func(_ context.Context, _ *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
			return nil, stderrors.New("update boom")
		},
	}
	withMockProject(t, mc)

	require.Error(t, runProjectUpdate(newProjectUpdateCommand(), []string{"proj_42"}))
}

// =============================================================================
// Delete
// =============================================================================

func TestRunProjectDelete_Happy(t *testing.T) {
	var capturedID string
	mc := &mockProjectClient{
		deleteFn: func(_ context.Context, _, projectID string) error {
			capturedID = projectID
			return nil
		},
	}
	withMockProject(t, mc)

	require.NoError(t, runProjectDelete(newProjectDeleteCommand(), []string{"proj_42"}))
	assert.Equal(t, "proj_42", capturedID)
}

func TestRunProjectDelete_TransportError(t *testing.T) {
	mc := &mockProjectClient{
		deleteFn: func(_ context.Context, _, _ string) error { return stderrors.New("delete boom") },
	}
	withMockProject(t, mc)

	require.Error(t, runProjectDelete(newProjectDeleteCommand(), []string{"proj_42"}))
}

// =============================================================================
// Grant / Revoke Role
// =============================================================================

func TestRunProjectGrantRole_Happy(t *testing.T) {
	var captured *projectSDK.GrantAgentRoleRequest
	mc := &mockProjectClient{
		grantRoleFn: func(_ context.Context, req *projectSDK.GrantAgentRoleRequest) (*projectSDK.GrantAgentRoleResponse, error) {
			captured = req
			return &projectSDK.GrantAgentRoleResponse{ProjectID: req.ProjectID, AgentID: req.AgentID, Role: req.Role}, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectGrantRoleCommand()
	projectAgentID = "agent_abc"
	projectRole = "LEAD" // exercises case normalization

	require.NoError(t, runProjectGrantRole(cmd, []string{"proj_42"}))

	require.NotNil(t, captured)
	assert.Equal(t, projectSDK.ProjectRoleLead, captured.Role)
	assert.Equal(t, "agent_abc", captured.AgentID)
}

// TestRunProjectGrantRole_InvalidRole verifies an unknown role is rejected
// before any request is issued.
func TestRunProjectGrantRole_InvalidRole(t *testing.T) {
	mc := &mockProjectClient{
		grantRoleFn: func(_ context.Context, _ *projectSDK.GrantAgentRoleRequest) (*projectSDK.GrantAgentRoleResponse, error) {
			t.Fatal("an invalid role must not reach the server")
			return nil, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectGrantRoleCommand()
	projectAgentID = "agent_abc"
	projectRole = "superuser"

	err := runProjectGrantRole(cmd, []string{"proj_42"})
	require.Error(t, err)

	var roleErr *InvalidProjectRoleError
	require.True(t, stderrors.As(err, &roleErr))
	assert.Contains(t, err.Error(), "superuser")
}

func TestRunProjectRevokeRole_Happy(t *testing.T) {
	var captured *projectSDK.RevokeAgentRoleRequest
	mc := &mockProjectClient{
		revokeRoleFn: func(_ context.Context, req *projectSDK.RevokeAgentRoleRequest) (*projectSDK.RevokeAgentRoleResponse, error) {
			captured = req
			return &projectSDK.RevokeAgentRoleResponse{ProjectID: req.ProjectID, AgentID: req.AgentID}, nil
		},
	}
	withMockProject(t, mc)
	cmd := newProjectRevokeRoleCommand()
	projectAgentID = "agent_abc"

	require.NoError(t, runProjectRevokeRole(cmd, []string{"proj_42"}))
	require.NotNil(t, captured)
	assert.Equal(t, "agent_abc", captured.AgentID)
}

func TestRunProjectRevokeRole_TransportError(t *testing.T) {
	mc := &mockProjectClient{
		revokeRoleFn: func(_ context.Context, _ *projectSDK.RevokeAgentRoleRequest) (*projectSDK.RevokeAgentRoleResponse, error) {
			return nil, stderrors.New("revoke boom")
		},
	}
	withMockProject(t, mc)
	cmd := newProjectRevokeRoleCommand()
	projectAgentID = "agent_abc"

	require.Error(t, runProjectRevokeRole(cmd, []string{"proj_42"}))
}

// =============================================================================
// Role Parsing
// =============================================================================

func TestParseProjectRole_Valid(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want projectSDK.ProjectRole
	}{
		{"lead", projectSDK.ProjectRoleLead},
		{"WORKER", projectSDK.ProjectRoleWorker},
		{"  observer  ", projectSDK.ProjectRoleObserver},
		{"denied", projectSDK.ProjectRoleDenied},
	} {
		got, err := parseProjectRole(tc.in)
		require.NoErrorf(t, err, "role %q", tc.in)
		assert.Equal(t, tc.want, got)
	}
}

func TestParseProjectRole_Invalid(t *testing.T) {
	for _, in := range []string{"", "admin", "lead ish"} {
		_, err := parseProjectRole(in)
		require.Errorf(t, err, "role %q must be rejected", in)
		assert.Contains(t, err.Error(), "invalid project role")
	}
}

// =============================================================================
// Formatting
// =============================================================================

func TestFormatProjectDetail_Formats(t *testing.T) {
	info := &projectSDK.ProjectInfo{
		ID:          "proj_1",
		TenantID:    "acme",
		Name:        "web-api",
		Description: "Public API",
		Git:         projectSDK.ProjectGit{RepoURL: "https://example.com/r.git", DefaultBranch: "main"},
		Quotas:      projectSDK.ProjectQuotas{TokenBudget: 42},
		Permissions: projectSDK.ProjectPermissions{
			AgentRoles: map[string]projectSDK.ProjectRole{"agent_b": projectSDK.ProjectRoleWorker, "agent_a": projectSDK.ProjectRoleLead},
		},
		Metadata:  map[string]string{"env": "prod"},
		CreatedAt: time.Unix(1700000000, 0),
	}

	for _, format := range []string{"table", "json", "yaml", ""} {
		var buf bytes.Buffer
		require.NoErrorf(t, formatProjectDetail(info, format, &buf), "format %q", format)
		assert.Containsf(t, buf.String(), "proj_1", "format %q must render the project ID", format)
	}

	// Table output must order agent roles and metadata deterministically.
	var table bytes.Buffer
	require.NoError(t, formatProjectDetail(info, "table", &table))
	out := table.String()
	assert.Less(t, strings.Index(out, "agent_a"), strings.Index(out, "agent_b"))
	assert.Contains(t, out, "unlimited", "a zero quota must render as unlimited")
}

func TestFormatProject_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	info := &projectSDK.ProjectInfo{ID: "proj_1"}

	require.Error(t, formatProjectDetail(info, "xml", &buf))
	require.Error(t, formatProjectList(&projectSDK.ListProjectsResponse{}, "xml", &buf))
	require.Error(t, formatProjectDeleted("proj_1", "xml", &buf))
	require.Error(t, formatProjectRoleGrant(&projectSDK.GrantAgentRoleResponse{}, "xml", &buf))
	require.Error(t, formatProjectRoleRevoke(&projectSDK.RevokeAgentRoleResponse{}, "xml", &buf))
}

func TestFormatProjectList_TableAndJSON(t *testing.T) {
	resp := &projectSDK.ListProjectsResponse{
		Projects: []*projectSDK.ProjectInfo{
			{ID: "proj_1", Name: "web-api", UpdatedAt: time.Unix(1700000000, 0)},
			{ID: "proj_2", Name: "ml-train"},
		},
		Total: 2,
	}

	var table bytes.Buffer
	require.NoError(t, formatProjectList(resp, "table", &table))
	assert.Contains(t, table.String(), "Total: 2 projects")
	assert.Contains(t, table.String(), "-", "empty columns must render as a dash")

	var jsonOut bytes.Buffer
	require.NoError(t, formatProjectList(resp, "json", &jsonOut))
	assert.Contains(t, jsonOut.String(), "ml-train")
}

// =============================================================================
// DefaultWorkflow + git identity flags
// =============================================================================

func TestRunProjectCreate_ForwardsDefaultWorkflowAndGitIdentity(t *testing.T) {
	var captured *projectSDK.CreateProjectRequest
	mc := &mockProjectClient{
		createFn: func(_ context.Context, req *projectSDK.CreateProjectRequest) (*projectSDK.ProjectInfo, error) {
			captured = req
			return &projectSDK.ProjectInfo{ID: "proj_1", Name: req.Name, TenantID: req.TenantID}, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectCreateCommand()
	require.NoError(t, cmd.Flags().Set("name", "api"))
	require.NoError(t, cmd.Flags().Set("default-workflow", "sdlc"))
	require.NoError(t, cmd.Flags().Set("git-author-name", "agent-api"))
	require.NoError(t, cmd.Flags().Set("git-author-email", "agent-api@example.test"))
	require.NoError(t, cmd.Flags().Set("git-signing", "true"))
	require.NoError(t, cmd.Flags().Set("git-signing-algorithm", "ECDSA-P256"))
	require.NoError(t, cmd.Flags().Set("git-signing-key", "api-signing"))
	require.NoError(t, cmd.Flags().Set("git-signing-key-scope", "tenant"))
	require.NoError(t, cmd.Flags().Set("git-signing-key-owner", "acme"))
	require.NoError(t, cmd.Flags().Set("git-ssh-key", "api-access"))
	require.NoError(t, cmd.Flags().Set("git-ssh-key-scope", "service"))
	require.NoError(t, cmd.Flags().Set("git-ssh-key-owner", "deploy"))

	require.NoError(t, runProjectCreate(cmd, nil))
	require.NotNil(t, captured)
	assert.Equal(t, "sdlc", captured.DefaultWorkflow)
	assert.Equal(t, "agent-api", captured.GitAuthorName)
	assert.Equal(t, "agent-api@example.test", captured.GitAuthorEmail)
	assert.True(t, captured.GitSigningEnabled)
	assert.Equal(t, "ECDSA-P256", captured.GitSigningAlgorithm)
	assert.Equal(t, "api-signing", captured.GitSigningKeyName)
	assert.Equal(t, "tenant", captured.GitSigningKeyScope)
	assert.Equal(t, "acme", captured.GitSigningKeyOwner)
	assert.Equal(t, "api-access", captured.GitSSHKeyName)
	assert.Equal(t, "service", captured.GitSSHKeyScope)
	assert.Equal(t, "deploy", captured.GitSSHKeyOwner)
}

// TestRunProjectUpdate_SigningFlagIsTriState: --git-signing not passed leaves
// signing unchanged (nil); --git-signing=false turns it off.
func TestRunProjectUpdate_SigningFlagIsTriState(t *testing.T) {
	var captured *projectSDK.UpdateProjectRequest
	mc := &mockProjectClient{
		updateFn: func(_ context.Context, req *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
			captured = req
			return &projectSDK.ProjectInfo{ID: req.ProjectID}, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectUpdateCommand()
	require.NoError(t, cmd.Flags().Set("default-workflow", "sdlc"))
	require.NoError(t, cmd.Flags().Set("git-author-name", "agent"))
	require.NoError(t, runProjectUpdate(cmd, []string{"proj_42"}))
	require.NotNil(t, captured)
	assert.Nil(t, captured.GitSigningEnabled, "--git-signing was not passed; signing must be left unchanged")
	assert.Equal(t, "sdlc", captured.DefaultWorkflow)
	assert.Equal(t, "agent", captured.GitAuthorName)
	assert.False(t, captured.ClearDefaultWorkflow)

	cmd = newProjectUpdateCommand()
	require.NoError(t, cmd.Flags().Set("git-signing", "false"))
	require.NoError(t, cmd.Flags().Set("clear-default-workflow", "true"))
	require.NoError(t, cmd.Flags().Set("git-ssh-key", "rotated"))
	require.NoError(t, runProjectUpdate(cmd, []string{"proj_42"}))
	require.NotNil(t, captured.GitSigningEnabled)
	assert.False(t, *captured.GitSigningEnabled)
	assert.True(t, captured.ClearDefaultWorkflow)
	assert.Equal(t, "rotated", captured.GitSSHKeyName)

	assert.Nil(t, gitSigningFlag(nil), "no command means no flag")
}

func TestFormatProjectDetail_ShowsGitIdentity(t *testing.T) {
	info := &projectSDK.ProjectInfo{
		ID: "proj_1", Name: "api", TenantID: "acme", DefaultWorkflow: "sdlc",
		Git: projectSDK.ProjectGit{
			Author:  projectSDK.GitAuthor{Name: "agent-api", Email: "agent-api@example.test"},
			Signing: projectSDK.CommitSigning{Enabled: true, Key: projectSDK.KMSKeyRef{Name: "api-signing"}},
			SSHKey:  projectSDK.KMSKeyRef{Name: "api-access"},
		},
	}
	var out bytes.Buffer
	require.NoError(t, formatProjectDetail(info, "table", &out))
	for _, want := range []string{"Default Workflow:", "sdlc", "agent-api <agent-api@example.test>", "api-signing", "api-access"} {
		assert.Contains(t, out.String(), want)
	}
}

// =============================================================================
// --autonomous and --context-repo
// =============================================================================

// TestRunProjectCreate_ForwardsAutonomyAndContextRepos: both flags reach the
// SDK request, and the created project renders them back in describe output.
func TestRunProjectCreate_ForwardsAutonomyAndContextRepos(t *testing.T) {
	var captured *projectSDK.CreateProjectRequest
	mc := &mockProjectClient{
		createFn: func(_ context.Context, req *projectSDK.CreateProjectRequest) (*projectSDK.ProjectInfo, error) {
			captured = req
			return &projectSDK.ProjectInfo{
				ID: "proj_1", Name: req.Name, TenantID: req.TenantID,
				Autonomous:   req.Autonomous,
				ContextRepos: req.ContextRepos,
			}, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectCreateCommand()
	require.NoError(t, cmd.Flags().Set("name", "api"))
	require.NoError(t, cmd.Flags().Set("autonomous", "true"))
	require.NoError(t, cmd.Flags().Set("context-repo",
		"name=handbook,url=https://forge.example/org/handbook,ref=main"))
	require.NoError(t, cmd.Flags().Set("context-repo",
		"name=specs,url=https://forge.example/org/specs,ref=v2"))

	require.NoError(t, runProjectCreate(cmd, nil))
	require.NotNil(t, captured)
	assert.True(t, captured.Autonomous)
	require.Len(t, captured.ContextRepos, 2)
	assert.Equal(t, projectSDK.ContextRepo{
		Name: "handbook", RepoURL: "https://forge.example/org/handbook", Ref: "main",
	}, captured.ContextRepos[0])
	assert.Equal(t, projectSDK.ContextRepo{
		Name: "specs", RepoURL: "https://forge.example/org/specs", Ref: "v2",
	}, captured.ContextRepos[1])
}

// TestRunProjectUpdate_AutonomousFlagIsTriState: --autonomous not passed
// leaves the switch unchanged (nil); --autonomous=false turns it off.
func TestRunProjectUpdate_AutonomousFlagIsTriState(t *testing.T) {
	var captured *projectSDK.UpdateProjectRequest
	mc := &mockProjectClient{
		updateFn: func(_ context.Context, req *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
			captured = req
			return &projectSDK.ProjectInfo{ID: req.ProjectID}, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectUpdateCommand()
	require.NoError(t, cmd.Flags().Set("git-sha", "abc123"))
	require.NoError(t, runProjectUpdate(cmd, []string{"proj_42"}))
	require.NotNil(t, captured)
	assert.Nil(t, captured.Autonomous, "--autonomous was not passed; it must be left unchanged")
	assert.Nil(t, captured.ContextRepos, "no --context-repo means the list is left alone")
	assert.False(t, captured.ClearContextRepos)

	cmd = newProjectUpdateCommand()
	require.NoError(t, cmd.Flags().Set("autonomous", "false"))
	require.NoError(t, cmd.Flags().Set("context-repo",
		"name=specs,url=https://forge.example/org/specs,ref=v2"))
	require.NoError(t, runProjectUpdate(cmd, []string{"proj_42"}))
	require.NotNil(t, captured.Autonomous)
	assert.False(t, *captured.Autonomous)
	require.Len(t, captured.ContextRepos, 1)
	assert.Equal(t, "specs", captured.ContextRepos[0].Name)

	cmd = newProjectUpdateCommand()
	require.NoError(t, cmd.Flags().Set("autonomous", "true"))
	require.NoError(t, cmd.Flags().Set("clear-context-repos", "true"))
	require.NoError(t, runProjectUpdate(cmd, []string{"proj_42"}))
	require.NotNil(t, captured.Autonomous)
	assert.True(t, *captured.Autonomous)
	assert.True(t, captured.ClearContextRepos)

	assert.Nil(t, autonomousFlag(nil), "no command means no flag")
}

// TestRunProject_RejectsMalformedContextRepoFlag: a bad --context-repo is a
// typed error and the client is never called.
func TestRunProject_RejectsMalformedContextRepoFlag(t *testing.T) {
	called := false
	mc := &mockProjectClient{
		createFn: func(context.Context, *projectSDK.CreateProjectRequest) (*projectSDK.ProjectInfo, error) {
			called = true
			return &projectSDK.ProjectInfo{}, nil
		},
		updateFn: func(context.Context, *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
			called = true
			return &projectSDK.ProjectInfo{}, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectCreateCommand()
	require.NoError(t, cmd.Flags().Set("name", "api"))
	require.NoError(t, cmd.Flags().Set("context-repo", "handbook"))
	require.Error(t, runProjectCreate(cmd, nil))
	assert.False(t, called, "a malformed flag must not reach the client")

	resetProjectFlags()
	cmd = newProjectUpdateCommand()
	require.NoError(t, cmd.Flags().Set("context-repo", "name=handbook,url=https://forge.example/a"))
	require.Error(t, runProjectUpdate(cmd, []string{"proj_42"}))
	assert.False(t, called, "a malformed flag must not reach the client")
}

// TestParseContextRepoFlag_Table covers the accepted form and every rejection.
func TestParseContextRepoFlag_Table(t *testing.T) {
	repo, err := parseContextRepoFlag(" name=handbook , url=https://forge.example/a , ref=main ")
	require.NoError(t, err)
	assert.Equal(t, projectSDK.ContextRepo{
		Name: "handbook", RepoURL: "https://forge.example/a", Ref: "main",
	}, repo)

	bad := []struct {
		name  string
		value string
	}{
		{"not key=value", "handbook"},
		{"unknown key", "name=handbook,branch=main,url=https://forge.example/a,ref=main"},
		{"missing name", "url=https://forge.example/a,ref=main"},
		{"missing url", "name=handbook,ref=main"},
		{"missing ref", "name=handbook,url=https://forge.example/a"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseContextRepoFlag(tc.value)
			var typed *InvalidContextRepoFlagError
			require.True(t, stderrors.As(err, &typed), "want *InvalidContextRepoFlagError, got %v", err)
			assert.Contains(t, typed.Error(), tc.value)
		})
	}

	// No flags means no list, so an update leaves the project's repos alone.
	repos, err := parseContextRepoFlags(nil)
	require.NoError(t, err)
	assert.Nil(t, repos)
}

// TestFormatProjectDetail_ShowsAutonomyAndContextRepos pins the describe/get
// output for both fields.
func TestFormatProjectDetail_ShowsAutonomyAndContextRepos(t *testing.T) {
	info := &projectSDK.ProjectInfo{
		ID: "proj_1", Name: "api", TenantID: "acme",
		Autonomous: true,
		ContextRepos: []projectSDK.ContextRepo{
			{Name: "handbook", RepoURL: "https://forge.example/org/handbook", Ref: "main"},
		},
	}
	var out bytes.Buffer
	require.NoError(t, formatProjectDetail(info, "table", &out))
	for _, want := range []string{
		"Autonomous:", "true",
		"Context Repos:", "handbook", "https://forge.example/org/handbook@main",
	} {
		assert.Contains(t, out.String(), want)
	}

	// An operator-driven project says so rather than staying silent.
	var quiet bytes.Buffer
	require.NoError(t, formatProjectDetail(&projectSDK.ProjectInfo{ID: "p", Name: "n", TenantID: "t"}, "table", &quiet))
	assert.Contains(t, quiet.String(), "Autonomous:")
	assert.NotContains(t, quiet.String(), "Context Repos:")
}
