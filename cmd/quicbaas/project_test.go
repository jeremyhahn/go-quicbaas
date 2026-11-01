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
	"encoding/json"
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
	listFn       func(ctx context.Context, req *projectSDK.ListProjectsRequest) (*projectSDK.ListProjectsResponse, error)
	grantRoleFn  func(ctx context.Context, req *projectSDK.GrantFleetNodeRoleRequest) (*projectSDK.GrantFleetNodeRoleResponse, error)
	revokeRoleFn func(ctx context.Context, req *projectSDK.RevokeFleetNodeRoleRequest) (*projectSDK.RevokeFleetNodeRoleResponse, error)
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

func (m *mockProjectClient) List(ctx context.Context, req *projectSDK.ListProjectsRequest) (*projectSDK.ListProjectsResponse, error) {
	if m.listFn != nil {
		return m.listFn(ctx, req)
	}
	return &projectSDK.ListProjectsResponse{}, nil
}

func (m *mockProjectClient) GrantFleetNodeRole(ctx context.Context, req *projectSDK.GrantFleetNodeRoleRequest) (*projectSDK.GrantFleetNodeRoleResponse, error) {
	if m.grantRoleFn != nil {
		return m.grantRoleFn(ctx, req)
	}
	return &projectSDK.GrantFleetNodeRoleResponse{ProjectID: req.ProjectID, FleetNodeID: req.FleetNodeID, Role: req.Role}, nil
}

func (m *mockProjectClient) RevokeFleetNodeRole(ctx context.Context, req *projectSDK.RevokeFleetNodeRoleRequest) (*projectSDK.RevokeFleetNodeRoleResponse, error) {
	if m.revokeRoleFn != nil {
		return m.revokeRoleFn(ctx, req)
	}
	return &projectSDK.RevokeFleetNodeRoleResponse{ProjectID: req.ProjectID, FleetNodeID: req.FleetNodeID}, nil
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
	projectWorkspaceFleetNodeID = ""
	projectWorkspacePath = ""
	projectHostAffinity = ""
	projectDefaultLeadAgent = ""
	projectGitRepoURL = ""
	projectGitBranch = ""
	projectGitCurrentSHA = ""
	spendWindowValues, spendClear = nil, false
	projectGPUMinutes = 0
	projectMaxConcurrency = 0
	projectWorkspaceTier = ""
	projectWorkspaceTmpfsMB = 0
	projectContainerMemMB = 0
	projectSessionRotation = ""
	projectMetadata = nil
	projectFleetNodeID = ""
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

	want := []string{"create", "list", "get", "update", "delete", "park", "resume", "grant-role", "revoke-role"}
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
	require.NoError(t, cmd.Flags().Set("spend-window", "name=daily,size=24h,tokens=1000,usd=12.50"))

	require.NoError(t, runProjectCreate(cmd, nil))

	require.NotNil(t, captured)
	assert.Equal(t, "web-api", captured.Name)
	assert.Equal(t, "acme", captured.TenantID, "create must scope to the global --tenant")
	assert.Equal(t, "Public API", captured.Description)
	assert.Equal(t, "https://github.com/acme/web-api", captured.GitRepoURL)
	require.Len(t, captured.Spend.Windows, 1)
	assert.Equal(t, int64(1000), captured.Spend.Windows[0].MaxTokens)
	assert.Equal(t, int64(12_500_000), captured.Spend.Windows[0].MaxEquivalentMicros)
}

func TestRunProjectCreate_FactoryError(t *testing.T) {
	withFailingProjectFactory(t, stderrors.New("no transport"))

	cmd := newProjectCreateCommand()
	err := runProjectCreate(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no transport")
}

func TestRunProject_TransportErrorIsReturned(t *testing.T) {
	transportErr := stderrors.New("transport boom")
	for _, tc := range []struct {
		name string
		mc   *mockProjectClient
		run  func() error
	}{
		{
			name: "create",
			mc: &mockProjectClient{
				createFn: func(_ context.Context, _ *projectSDK.CreateProjectRequest) (*projectSDK.ProjectInfo, error) {
					return nil, transportErr
				},
			},
			run: func() error {
				cmd := newProjectCreateCommand()
				projectName = "web-api"
				return runProjectCreate(cmd, nil)
			},
		},
		{
			name: "list",
			mc: &mockProjectClient{
				listFn: func(_ context.Context, _ *projectSDK.ListProjectsRequest) (*projectSDK.ListProjectsResponse, error) {
					return nil, transportErr
				},
			},
			run: func() error { return runProjectList(newProjectListCommand(), nil) },
		},
		{
			name: "update",
			mc: &mockProjectClient{
				updateFn: func(_ context.Context, _ *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
					return nil, transportErr
				},
			},
			run: func() error { return runProjectUpdate(newProjectUpdateCommand(), []string{"proj_42"}) },
		},
		{
			name: "delete",
			mc: &mockProjectClient{
				deleteFn: func(_ context.Context, _, _ string) error { return transportErr },
			},
			run: func() error { return runProjectDelete(newProjectDeleteCommand(), []string{"proj_42"}) },
		},
		{
			name: "revoke-role",
			mc: &mockProjectClient{
				revokeRoleFn: func(_ context.Context, _ *projectSDK.RevokeFleetNodeRoleRequest) (*projectSDK.RevokeFleetNodeRoleResponse, error) {
					return nil, transportErr
				},
			},
			run: func() error {
				cmd := newProjectRevokeRoleCommand()
				projectFleetNodeID = "agent_abc"
				return runProjectRevokeRole(cmd, []string{"proj_42"})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withMockProject(t, tc.mc)
			require.Error(t, tc.run())
		})
	}
}

// =============================================================================
// List
// =============================================================================

func TestRunProjectList_Happy(t *testing.T) {
	var capturedTenant string
	mc := &mockProjectClient{
		listFn: func(_ context.Context, req *projectSDK.ListProjectsRequest) (*projectSDK.ListProjectsResponse, error) {
			capturedTenant = req.TenantID
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

// =============================================================================
// Grant / Revoke Role
// =============================================================================

func TestRunProjectGrantRole_Happy(t *testing.T) {
	var captured *projectSDK.GrantFleetNodeRoleRequest
	mc := &mockProjectClient{
		grantRoleFn: func(_ context.Context, req *projectSDK.GrantFleetNodeRoleRequest) (*projectSDK.GrantFleetNodeRoleResponse, error) {
			captured = req
			return &projectSDK.GrantFleetNodeRoleResponse{ProjectID: req.ProjectID, FleetNodeID: req.FleetNodeID, Role: req.Role}, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectGrantRoleCommand()
	projectFleetNodeID = "agent_abc"
	projectRole = "LEAD" // exercises case normalization

	require.NoError(t, runProjectGrantRole(cmd, []string{"proj_42"}))

	require.NotNil(t, captured)
	assert.Equal(t, projectSDK.ProjectRoleLead, captured.Role)
	assert.Equal(t, "agent_abc", captured.FleetNodeID)
}

// TestRunProjectGrantRole_InvalidRole verifies an unknown role is rejected
// before any request is issued.
func TestRunProjectGrantRole_InvalidRole(t *testing.T) {
	mc := &mockProjectClient{
		grantRoleFn: func(_ context.Context, _ *projectSDK.GrantFleetNodeRoleRequest) (*projectSDK.GrantFleetNodeRoleResponse, error) {
			t.Fatal("an invalid role must not reach the server")
			return nil, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectGrantRoleCommand()
	projectFleetNodeID = "agent_abc"
	projectRole = "superuser"

	err := runProjectGrantRole(cmd, []string{"proj_42"})
	require.Error(t, err)

	var roleErr *InvalidProjectRoleError
	require.True(t, stderrors.As(err, &roleErr))
	assert.Contains(t, err.Error(), "superuser")
}

func TestRunProjectRevokeRole_Happy(t *testing.T) {
	var captured *projectSDK.RevokeFleetNodeRoleRequest
	mc := &mockProjectClient{
		revokeRoleFn: func(_ context.Context, req *projectSDK.RevokeFleetNodeRoleRequest) (*projectSDK.RevokeFleetNodeRoleResponse, error) {
			captured = req
			return &projectSDK.RevokeFleetNodeRoleResponse{ProjectID: req.ProjectID, FleetNodeID: req.FleetNodeID}, nil
		},
	}
	withMockProject(t, mc)
	cmd := newProjectRevokeRoleCommand()
	projectFleetNodeID = "agent_abc"

	require.NoError(t, runProjectRevokeRole(cmd, []string{"proj_42"}))
	require.NotNil(t, captured)
	assert.Equal(t, "agent_abc", captured.FleetNodeID)
}

// =============================================================================
// Role Parsing
// =============================================================================

func TestParseProjectRole(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    projectSDK.ProjectRole
		wantErr bool
	}{
		{in: "lead", want: projectSDK.ProjectRoleLead},
		{in: "WORKER", want: projectSDK.ProjectRoleWorker},
		{in: "  observer  ", want: projectSDK.ProjectRoleObserver},
		{in: "denied", want: projectSDK.ProjectRoleDenied},
		{in: "", wantErr: true},
		{in: "admin", wantErr: true},
		{in: "lead ish", wantErr: true},
	} {
		got, err := parseProjectRole(tc.in)
		if tc.wantErr {
			require.Errorf(t, err, "role %q must be rejected", tc.in)
			assert.Contains(t, err.Error(), "invalid project role")
			continue
		}
		require.NoErrorf(t, err, "role %q", tc.in)
		assert.Equal(t, tc.want, got)
	}
}

// =============================================================================
// Formatting
// =============================================================================

func TestFormatProjectDetail(t *testing.T) {
	full := &projectSDK.ProjectInfo{
		ID:          "proj_1",
		TenantID:    "acme",
		Name:        "web-api",
		Description: "Public API",
		Git:         projectSDK.ProjectGit{RepoURL: "https://example.com/r.git", DefaultBranch: "main"},
		Quotas:      projectSDK.ProjectQuotas{GPUMinutes: 42},
		Permissions: projectSDK.ProjectPermissions{
			FleetNodeRoles: map[string]projectSDK.ProjectRole{"agent_b": projectSDK.ProjectRoleWorker, "agent_a": projectSDK.ProjectRoleLead},
		},
		Metadata:  map[string]string{"env": "prod"},
		CreatedAt: time.Unix(1700000000, 0),
	}
	minimal := &projectSDK.ProjectInfo{ID: "proj_1", Name: "api", TenantID: "acme"}
	storage := &projectSDK.ProjectInfo{
		ID: "proj_1", TenantID: "acme", Name: "api",
		Storage: projectSDK.ProjectStorage{
			WorkspaceTier:     projectSDK.WorkspaceTierEphemeral,
			WorkspaceTmpfsMB:  4096,
			ContainerMemoryMB: 8192,
		},
	}

	for _, tc := range []struct {
		name        string
		info        *projectSDK.ProjectInfo
		format      string
		contains    []string
		notContains []string
		check       func(t *testing.T, out string)
	}{
		{name: "json", info: full, format: "json", contains: []string{"proj_1"}},
		{name: "yaml", info: full, format: "yaml", contains: []string{"proj_1"}},
		{name: "default format", info: full, format: "", contains: []string{"proj_1"}},
		{
			name: "table orders roles and renders a zero quota as unlimited", info: full, format: "table",
			contains: []string{"proj_1", "unlimited"},
			check: func(t *testing.T, out string) {
				assert.Less(t, strings.Index(out, "agent_a"), strings.Index(out, "agent_b"))
			},
		},
		{
			name: "git identity",
			info: &projectSDK.ProjectInfo{
				ID: "proj_1", Name: "api", TenantID: "acme", DefaultWorkflow: "sdlc",
				Git: projectSDK.ProjectGit{
					Author:  projectSDK.GitAuthor{Name: "agent-api", Email: "agent-api@example.test"},
					Signing: projectSDK.CommitSigning{Enabled: true, Key: projectSDK.KMSKeyRef{Name: "api-signing"}},
					SSHKey:  projectSDK.KMSKeyRef{Name: "api-access"},
				},
			},
			format:   "table",
			contains: []string{"Default Workflow:", "sdlc", "agent-api <agent-api@example.test>", "api-signing", "api-access"},
		},
		{
			name: "autonomy and context repos",
			info: &projectSDK.ProjectInfo{
				ID: "proj_1", Name: "api", TenantID: "acme",
				Autonomous: true,
				ContextRepos: []projectSDK.ContextRepo{
					{Name: "handbook", RepoURL: "https://forge.example/org/handbook", Ref: "main"},
				},
			},
			format: "table",
			contains: []string{
				"Autonomous:", "true",
				"Context Repos:", "handbook", "https://forge.example/org/handbook@main",
			},
		},
		{
			// An operator-driven project says so rather than staying silent.
			name: "operator-driven project without context repos", info: minimal, format: "table",
			contains:    []string{"Autonomous:"},
			notContains: []string{"Context Repos:"},
		},
		{
			name: "storage table", info: storage, format: "table",
			contains: []string{"Workspace Tier:", "ephemeral", "4096 MiB", "8192 MiB"},
		},
		{
			name: "storage json", info: storage, format: "json",
			check: func(t *testing.T, out string) {
				var decoded struct {
					Storage map[string]interface{} `json:"storage"`
				}
				require.NoError(t, json.Unmarshal([]byte(out), &decoded))
				assert.Equal(t, "ephemeral", decoded.Storage["workspace_tier"])
				assert.Equal(t, float64(4096), decoded.Storage["workspace_tmpfs_mb"])
				assert.Equal(t, float64(8192), decoded.Storage["container_memory_mb"])
			},
		},
		{
			name: "default session rotation and unparked", info: minimal, format: "table",
			contains: []string{"Session Rotation:", "resume (default)"},
			check: func(t *testing.T, out string) {
				assert.Regexp(t, `Parked:\s+no`, out)
			},
		},
		{
			name:        "fresh session rotation",
			info:        &projectSDK.ProjectInfo{ID: "proj_1", Name: "api", SessionRotation: projectSDK.SessionRotationFresh},
			format:      "table",
			contains:    []string{"fresh"},
			notContains: []string{"(default)"},
		},
		{
			name: "parked",
			info: &projectSDK.ProjectInfo{
				ID: "proj_1", Name: "api", Parked: true, ParkedAt: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC), ParkedBy: "ops",
			},
			format:   "table",
			contains: []string{"yes, since 2026-09-25T12:00:00Z by ops"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, formatProjectDetail(tc.info, tc.format, &buf))
			out := buf.String()
			for _, want := range tc.contains {
				assert.Contains(t, out, want)
			}
			for _, unwanted := range tc.notContains {
				assert.NotContains(t, out, unwanted)
			}
			if tc.check != nil {
				tc.check(t, out)
			}
		})
	}
}

func TestFormatProject_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	info := &projectSDK.ProjectInfo{ID: "proj_1"}

	require.Error(t, formatProjectDetail(info, "xml", &buf))
	require.Error(t, formatProjectList(&projectSDK.ListProjectsResponse{}, "xml", &buf))
	require.Error(t, formatProjectDeleted("proj_1", "xml", &buf))
	require.Error(t, formatProjectRoleGrant(&projectSDK.GrantFleetNodeRoleResponse{}, "xml", &buf))
	require.Error(t, formatProjectRoleRevoke(&projectSDK.RevokeFleetNodeRoleResponse{}, "xml", &buf))
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
	assert.Contains(t, table.String(), "Showing 1–2 of 2.")
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
// =============================================================================
// Storage tier flags
// =============================================================================

// TestRunProjectCreate_ForwardsStorage: the three storage flags reach the
// create request.
func TestRunProjectCreate_ForwardsStorage(t *testing.T) {
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
	require.NoError(t, cmd.Flags().Set("workspace-tier", "ephemeral"))
	require.NoError(t, cmd.Flags().Set("workspace-tmpfs-mb", "4096"))
	require.NoError(t, cmd.Flags().Set("container-memory-mb", "8192"))

	require.NoError(t, runProjectCreate(cmd, nil))
	require.NotNil(t, captured)
	assert.Equal(t, projectSDK.WorkspaceTierEphemeral, captured.WorkspaceTier)
	assert.Equal(t, 4096, captured.WorkspaceTmpfsMB)
	assert.Equal(t, 8192, captured.ContainerMemoryMB)
}

// TestRunProjectUpdate_StorageSizesArePresenceBased: a size flag not passed
// leaves the stored size alone (nil); an explicit 0 clears it.
func TestRunProjectUpdate_StorageSizesArePresenceBased(t *testing.T) {
	var captured *projectSDK.UpdateProjectRequest
	mc := &mockProjectClient{
		updateFn: func(_ context.Context, req *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
			captured = req
			return &projectSDK.ProjectInfo{ID: req.ProjectID}, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectUpdateCommand()
	require.NoError(t, cmd.Flags().Set("description", "renamed"))
	require.NoError(t, runProjectUpdate(cmd, []string{"proj_42"}))
	require.NotNil(t, captured)
	assert.Empty(t, captured.WorkspaceTier, "--workspace-tier was not passed; it must be left unchanged")
	assert.Nil(t, captured.WorkspaceTmpfsMB, "--workspace-tmpfs-mb was not passed; it must be left unchanged")
	assert.Nil(t, captured.ContainerMemoryMB, "--container-memory-mb was not passed; it must be left unchanged")

	resetProjectFlags()
	cmd = newProjectUpdateCommand()
	require.NoError(t, cmd.Flags().Set("workspace-tier", "persistent"))
	require.NoError(t, cmd.Flags().Set("workspace-tmpfs-mb", "0"))
	require.NoError(t, cmd.Flags().Set("container-memory-mb", "0"))
	require.NoError(t, runProjectUpdate(cmd, []string{"proj_42"}))
	assert.Equal(t, projectSDK.WorkspaceTierPersistent, captured.WorkspaceTier)
	require.NotNil(t, captured.WorkspaceTmpfsMB)
	assert.Equal(t, 0, *captured.WorkspaceTmpfsMB)
	require.NotNil(t, captured.ContainerMemoryMB)
	assert.Equal(t, 0, *captured.ContainerMemoryMB)
}

// TestFormatProjectDetail_ShowsStorage: the table output names the tier and
// both sizes, and JSON output carries them where `jq .storage` finds them.
// =============================================================================
// Session rotation flag
// =============================================================================

// TestRunProject_ForwardsSessionRotation: --session-rotation reaches the
// create and update requests, and an update that does not pass it leaves the
// policy unchanged (empty).
func TestRunProject_ForwardsSessionRotation(t *testing.T) {
	var created *projectSDK.CreateProjectRequest
	var updated *projectSDK.UpdateProjectRequest
	mc := &mockProjectClient{
		createFn: func(_ context.Context, req *projectSDK.CreateProjectRequest) (*projectSDK.ProjectInfo, error) {
			created = req
			return &projectSDK.ProjectInfo{ID: "proj_1", Name: req.Name, TenantID: req.TenantID}, nil
		},
		updateFn: func(_ context.Context, req *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
			updated = req
			return &projectSDK.ProjectInfo{ID: req.ProjectID}, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectCreateCommand()
	require.NoError(t, cmd.Flags().Set("name", "api"))
	require.NoError(t, cmd.Flags().Set("session-rotation", "fresh"))
	require.NoError(t, runProjectCreate(cmd, nil))
	require.NotNil(t, created)
	assert.Equal(t, projectSDK.SessionRotationFresh, created.SessionRotation)

	resetProjectFlags()
	cmd = newProjectUpdateCommand()
	require.NoError(t, cmd.Flags().Set("description", "renamed"))
	require.NoError(t, runProjectUpdate(cmd, []string{"proj_42"}))
	require.NotNil(t, updated)
	assert.Empty(t, updated.SessionRotation, "--session-rotation was not passed; it must be left unchanged")

	resetProjectFlags()
	cmd = newProjectUpdateCommand()
	require.NoError(t, cmd.Flags().Set("session-rotation", "resume"))
	require.NoError(t, runProjectUpdate(cmd, []string{"proj_42"}))
	assert.Equal(t, projectSDK.SessionRotationResume, updated.SessionRotation)
}

// TestRunProjectCreate_ASessionRotationTheServerRefusesIsReported: the CLI
// does not second-guess the policy; the server's refusal is what the operator
// sees.
func TestRunProjectCreate_ASessionRotationTheServerRefusesIsReported(t *testing.T) {
	refusal := stderrors.New("project: invalid session rotation policy: \"frseh\"")
	mc := &mockProjectClient{
		createFn: func(context.Context, *projectSDK.CreateProjectRequest) (*projectSDK.ProjectInfo, error) {
			return nil, refusal
		},
	}
	withMockProject(t, mc)

	cmd := newProjectCreateCommand()
	require.NoError(t, cmd.Flags().Set("name", "api"))
	require.NoError(t, cmd.Flags().Set("session-rotation", "frseh"))
	err := runProjectCreate(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid session rotation policy")
}

// TestFormatProjectDetail_ShowsSessionRotation: the table names the policy,
// and a project that never chose one is shown running the default.
// =============================================================================
// park / resume
// =============================================================================

// TestRunProjectPark_SendsOnlyTheParkSwitch: park and resume are one-field
// updates, so neither can carry another edit along with it.
func TestRunProjectPark_SendsOnlyTheParkSwitch(t *testing.T) {
	var captured []*projectSDK.UpdateProjectRequest
	mc := &mockProjectClient{
		updateFn: func(_ context.Context, req *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
			captured = append(captured, req)
			return &projectSDK.ProjectInfo{ID: req.ProjectID, Name: "api", Parked: *req.Parked}, nil
		},
	}
	withMockProject(t, mc)

	require.NoError(t, newProjectParkCommand().RunE(newProjectParkCommand(), []string{"proj_1"}))
	require.NoError(t, newProjectResumeCommand().RunE(newProjectResumeCommand(), []string{"proj_1"}))
	require.Len(t, captured, 2)
	for i, want := range []bool{true, false} {
		req := captured[i]
		require.NotNil(t, req.Parked)
		assert.Equal(t, want, *req.Parked)
		assert.Equal(t, "proj_1", req.ProjectID)
		assert.Nil(t, req.Autonomous, "park must not touch autonomy")
		assert.Empty(t, req.Name)
	}
}

func TestRunProjectPark_ReportsARefusal(t *testing.T) {
	mc := &mockProjectClient{
		updateFn: func(_ context.Context, _ *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
			return nil, stderrors.New("forbidden")
		},
	}
	withMockProject(t, mc)
	require.Error(t, runProjectSetParked("proj_1", true))

	withFailingProjectFactory(t, stderrors.New("no transport"))
	require.Error(t, runProjectSetParked("proj_1", false))
}

func TestRunProjectUpdate_RetiresASigningKey(t *testing.T) {
	var captured *projectSDK.UpdateProjectRequest
	mc := &mockProjectClient{
		updateFn: func(_ context.Context, req *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
			captured = req
			return &projectSDK.ProjectInfo{ID: req.ProjectID}, nil
		},
	}
	withMockProject(t, mc)

	cmd := newProjectUpdateCommand()
	require.NoError(t, cmd.ParseFlags([]string{
		"--git-signing-retire-key", "signing", "--git-signing-retire-key-scope", "service",
		"--git-signing-retire-key-owner", "agent", "--git-signing-retired-at", "2026-09-24T22:15:00Z",
	}))
	require.NoError(t, runProjectUpdate(cmd, []string{"proj_42"}))

	require.NotNil(t, captured)
	assert.Equal(t, "signing", captured.GitSigningRetireKeyName)
	assert.Equal(t, "service", captured.GitSigningRetireKeyScope)
	assert.Equal(t, "agent", captured.GitSigningRetireKeyOwner)
	assert.Equal(t, "2026-09-24T22:15:00Z", captured.GitSigningRetiredAt)
}

func TestRunProjectUpdate_ARefusedRetirementIsReported(t *testing.T) {
	mc := &mockProjectClient{
		updateFn: func(_ context.Context, _ *projectSDK.UpdateProjectRequest) (*projectSDK.ProjectInfo, error) {
			return nil, stderrors.New("project invalid: git_signing_retired_at: must be an RFC 3339 time")
		},
	}
	withMockProject(t, mc)

	cmd := newProjectUpdateCommand()
	require.NoError(t, cmd.ParseFlags([]string{"--git-signing-retire-key", "signing", "--git-signing-retired-at", "soon"}))
	err := runProjectUpdate(cmd, []string{"proj_42"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git_signing_retired_at")
}
