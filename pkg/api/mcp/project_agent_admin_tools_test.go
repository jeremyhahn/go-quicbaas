//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Project Service
// =============================================================================

type mockProjectService struct {
	createFunc func(context.Context, *service.CreateProjectRequest) (*service.CreateProjectResponse, error)
	getFunc    func(context.Context, *service.GetProjectRequest) (*service.GetProjectResponse, error)
	listFunc   func(context.Context, *service.ListProjectsRequest) (*service.ListProjectsResponse, error)
	updateFunc func(context.Context, *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error)
	deleteFunc func(context.Context, *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error)
	grantFunc  func(context.Context, *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error)
	revokeFunc func(context.Context, *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error)
}

func (m *mockProjectService) CreateProject(ctx context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, req)
	}
	return &service.CreateProjectResponse{}, nil
}

func (m *mockProjectService) GetProject(ctx context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, req)
	}
	return &service.GetProjectResponse{}, nil
}

func (m *mockProjectService) GetProjectByName(ctx context.Context, req *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
	return &service.GetProjectResponse{}, nil
}

func (m *mockProjectService) UpdateProject(ctx context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, req)
	}
	return &service.UpdateProjectResponse{}, nil
}

func (m *mockProjectService) DeleteProject(ctx context.Context, req *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, req)
	}
	return &service.DeleteProjectResponse{}, nil
}

func (m *mockProjectService) ListProjects(ctx context.Context, req *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, req)
	}
	return &service.ListProjectsResponse{}, nil
}

func (m *mockProjectService) AuthorizeAgent(ctx context.Context, req *service.AuthorizeAgentRequest) error {
	return nil
}

func (m *mockProjectService) GrantAgentRole(ctx context.Context, req *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
	if m.grantFunc != nil {
		return m.grantFunc(ctx, req)
	}
	return &service.GrantAgentRoleResponse{}, nil
}

func (m *mockProjectService) RevokeAgentRole(ctx context.Context, req *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error) {
	if m.revokeFunc != nil {
		return m.revokeFunc(ctx, req)
	}
	return &service.RevokeAgentRoleResponse{}, nil
}

// =============================================================================
// Mock Agent Admin Service
// =============================================================================

type mockAgentAdminService struct {
	disableFunc func(context.Context, *service.DisableAgentRequest) (*service.DisableAgentResponse, error)
	enableFunc  func(context.Context, *service.EnableAgentRequest) (*service.EnableAgentResponse, error)
}

func (m *mockAgentAdminService) DisableAgent(ctx context.Context, req *service.DisableAgentRequest) (*service.DisableAgentResponse, error) {
	if m.disableFunc != nil {
		return m.disableFunc(ctx, req)
	}
	return &service.DisableAgentResponse{AgentID: req.AgentID, Disabled: true}, nil
}

func (m *mockAgentAdminService) EnableAgent(ctx context.Context, req *service.EnableAgentRequest) (*service.EnableAgentResponse, error) {
	if m.enableFunc != nil {
		return m.enableFunc(ctx, req)
	}
	return &service.EnableAgentResponse{}, nil
}

func (m *mockAgentAdminService) GetAgentRegistration(ctx context.Context, req *service.GetAgentRegistrationRequest) (*service.GetAgentRegistrationResponse, error) {
	return &service.GetAgentRegistrationResponse{}, nil
}

func (m *mockAgentAdminService) ListAgentRegistrations(ctx context.Context, req *service.ListAgentRegistrationsRequest) (*service.ListAgentRegistrationsResponse, error) {
	return &service.ListAgentRegistrationsResponse{}, nil
}

// =============================================================================
// Mock QuicBaaS service wiring Project + AgentAdmin
// =============================================================================

type projectAdminMockQuicBaaS struct {
	mockService
	projectSvc service.ProjectService
	adminSvc   service.AgentAdminService
}

func (m *projectAdminMockQuicBaaS) Project() service.ProjectService {
	return m.projectSvc
}

func (m *projectAdminMockQuicBaaS) AgentAdmin() service.AgentAdminService {
	return m.adminSvc
}

func newProjectAdminRegistry(t *testing.T, projectSvc service.ProjectService, adminSvc service.AgentAdminService) *ToolRegistry {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	svc := &projectAdminMockQuicBaaS{projectSvc: projectSvc, adminSvc: adminSvc}
	return NewToolRegistry(svc, log)
}

// =============================================================================
// Project Tool Tests
// =============================================================================

func TestExecuteProjectTool_AllHappyPaths(t *testing.T) {
	reg := newProjectAdminRegistry(t, &mockProjectService{}, nil)
	ctx := context.Background()

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"project_create", map[string]interface{}{"tenant_id": "t1", "name": "p1"}},
		{"project_get", map[string]interface{}{"tenant_id": "t1", "project_id": "p1"}},
		{"project_list", map[string]interface{}{"tenant_id": "t1"}},
		{"project_update", map[string]interface{}{"tenant_id": "t1", "project_id": "p1", "name": "p2"}},
		{"project_delete", map[string]interface{}{"tenant_id": "t1", "project_id": "p1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := reg.executeProjectTool(ctx, tc.name, tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestExecuteProjectUpdate_WithAgentRoles(t *testing.T) {
	var captured *service.UpdateProjectRequest
	svc := &mockProjectService{
		updateFunc: func(_ context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			captured = req
			return &service.UpdateProjectResponse{}, nil
		},
	}
	reg := newProjectAdminRegistry(t, svc, nil)

	_, err := reg.executeProjectTool(context.Background(), "project_update", map[string]interface{}{
		"tenant_id":  "t1",
		"project_id": "p1",
		"agent_roles": map[string]interface{}{
			"agent-a": "lead",
			"agent-b": 42, // non-string, should be skipped
		},
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "lead", string(captured.AgentRoles["agent-a"]))
	_, hasB := captured.AgentRoles["agent-b"]
	assert.False(t, hasB)
}

func TestExecuteProjectTool_ServiceUnavailable(t *testing.T) {
	reg := newProjectAdminRegistry(t, nil, nil) // Project() returns nil
	ctx := context.Background()

	for _, name := range []string{"project_create", "project_get", "project_list", "project_update", "project_delete"} {
		t.Run(name, func(t *testing.T) {
			_, err := reg.executeProjectTool(ctx, name, map[string]interface{}{})
			require.Error(t, err)
		})
	}
}

func TestExecuteProjectTool_ServiceError(t *testing.T) {
	sentinel := errors.New("project boom")
	svc := &mockProjectService{
		createFunc: func(context.Context, *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			return nil, sentinel
		},
		getFunc: func(context.Context, *service.GetProjectRequest) (*service.GetProjectResponse, error) {
			return nil, sentinel
		},
		listFunc: func(context.Context, *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
			return nil, sentinel
		},
		updateFunc: func(context.Context, *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			return nil, sentinel
		},
		deleteFunc: func(context.Context, *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
			return nil, sentinel
		},
	}
	reg := newProjectAdminRegistry(t, svc, nil)
	ctx := context.Background()

	for _, name := range []string{"project_create", "project_get", "project_list", "project_update", "project_delete"} {
		t.Run(name, func(t *testing.T) {
			_, err := reg.executeProjectTool(ctx, name, map[string]interface{}{})
			require.ErrorIs(t, err, sentinel)
		})
	}
}

func TestExecuteProjectTool_UnknownTool(t *testing.T) {
	reg := newProjectAdminRegistry(t, &mockProjectService{}, nil)
	_, err := reg.executeProjectTool(context.Background(), "project_nonexistent", map[string]interface{}{})
	require.Error(t, err)
}

func TestIsProjectTool(t *testing.T) {
	assert.True(t, isProjectTool("project_create"))
	assert.True(t, isProjectTool("project_delete"))
	assert.False(t, isProjectTool("agent_disable"))
	assert.False(t, isProjectTool(""))
}

// =============================================================================
// Agent Admin Tool Tests
// =============================================================================

func TestExecuteAgentAdminTool_HappyPaths(t *testing.T) {
	reg := newProjectAdminRegistry(t, &mockProjectService{}, &mockAgentAdminService{})
	ctx := context.Background()

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"agent_disable", map[string]interface{}{"tenant_id": "t1", "agent_id": "a1"}},
		{"agent_enable", map[string]interface{}{"tenant_id": "t1", "agent_id": "a1"}},
		{"project_grant_agent", map[string]interface{}{"tenant_id": "t1", "project_id": "p1", "agent_id": "a1", "role": "worker"}},
		{"project_revoke_agent", map[string]interface{}{"tenant_id": "t1", "project_id": "p1", "agent_id": "a1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := reg.executeAgentAdminTool(ctx, tc.name, tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestExecuteAgentAdminTool_AdminServiceUnavailable(t *testing.T) {
	// AgentAdmin() returns nil -> disable/enable should fail.
	reg := newProjectAdminRegistry(t, &mockProjectService{}, nil)
	ctx := context.Background()

	for _, name := range []string{"agent_disable", "agent_enable"} {
		t.Run(name, func(t *testing.T) {
			_, err := reg.executeAgentAdminTool(ctx, name, map[string]interface{}{})
			require.Error(t, err)
		})
	}
}

func TestExecuteAgentAdminTool_ProjectServiceUnavailable(t *testing.T) {
	// Project() returns nil -> grant/revoke should fail.
	reg := newProjectAdminRegistry(t, nil, &mockAgentAdminService{})
	ctx := context.Background()

	for _, name := range []string{"project_grant_agent", "project_revoke_agent"} {
		t.Run(name, func(t *testing.T) {
			_, err := reg.executeAgentAdminTool(ctx, name, map[string]interface{}{})
			require.Error(t, err)
		})
	}
}

func TestExecuteAgentAdminTool_ServiceError(t *testing.T) {
	sentinel := errors.New("admin boom")
	adminSvc := &mockAgentAdminService{
		disableFunc: func(context.Context, *service.DisableAgentRequest) (*service.DisableAgentResponse, error) {
			return nil, sentinel
		},
		enableFunc: func(context.Context, *service.EnableAgentRequest) (*service.EnableAgentResponse, error) {
			return nil, sentinel
		},
	}
	projSvc := &mockProjectService{
		grantFunc: func(context.Context, *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
			return nil, sentinel
		},
		revokeFunc: func(context.Context, *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error) {
			return nil, sentinel
		},
	}
	reg := newProjectAdminRegistry(t, projSvc, adminSvc)
	ctx := context.Background()

	for _, name := range []string{"agent_disable", "agent_enable", "project_grant_agent", "project_revoke_agent"} {
		t.Run(name, func(t *testing.T) {
			_, err := reg.executeAgentAdminTool(ctx, name, map[string]interface{}{})
			require.ErrorIs(t, err, sentinel)
		})
	}
}

func TestExecuteAgentAdminTool_UnknownTool(t *testing.T) {
	reg := newProjectAdminRegistry(t, &mockProjectService{}, &mockAgentAdminService{})
	_, err := reg.executeAgentAdminTool(context.Background(), "agent_unknown", map[string]interface{}{})
	require.Error(t, err)
}

func TestIsAgentAdminTool(t *testing.T) {
	assert.True(t, isAgentAdminTool("agent_disable"))
	assert.True(t, isAgentAdminTool("project_grant_agent"))
	assert.False(t, isAgentAdminTool("project_create"))
	assert.False(t, isAgentAdminTool(""))
}

// =============================================================================
// DefaultWorkflow + git identity — MCP tool arguments reach the service
// =============================================================================

func TestExecuteProjectCreate_ForwardsDefaultWorkflowAndGitIdentity(t *testing.T) {
	var captured *service.CreateProjectRequest
	svc := &mockProjectService{
		createFunc: func(_ context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			captured = req
			return &service.CreateProjectResponse{}, nil
		},
	}
	reg := newProjectAdminRegistry(t, svc, nil)

	_, err := reg.executeProjectTool(context.Background(), "project_create", map[string]interface{}{
		"tenant_id": "t1", "name": "api",
		"default_workflow":      "sdlc",
		"git_author_name":       "agent-api",
		"git_author_email":      "agent-api@example.test",
		"git_signing_enabled":   true,
		"git_signing_algorithm": "ECDSA-P256",
		"git_signing_key_name":  "api-signing",
		"git_signing_key_scope": "tenant",
		"git_signing_key_owner": "t1",
		"git_ssh_key_name":      "api-access",
		"git_ssh_key_scope":     "service",
		"git_ssh_key_owner":     "deploy",
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "sdlc", captured.DefaultWorkflow)
	assert.Equal(t, "agent-api", captured.GitAuthorName)
	assert.Equal(t, "agent-api@example.test", captured.GitAuthorEmail)
	assert.True(t, captured.GitSigningEnabled)
	assert.Equal(t, "ECDSA-P256", captured.GitSigningAlgorithm)
	assert.Equal(t, "api-signing", captured.GitSigningKeyName)
	assert.Equal(t, "tenant", captured.GitSigningKeyScope)
	assert.Equal(t, "t1", captured.GitSigningKeyOwner)
	assert.Equal(t, "api-access", captured.GitSSHKeyName)
	assert.Equal(t, "service", captured.GitSSHKeyScope)
	assert.Equal(t, "deploy", captured.GitSSHKeyOwner)
}

func TestExecuteProjectUpdate_SigningEnabledIsTriState(t *testing.T) {
	var captured *service.UpdateProjectRequest
	svc := &mockProjectService{
		updateFunc: func(_ context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			captured = req
			return &service.UpdateProjectResponse{}, nil
		},
	}
	reg := newProjectAdminRegistry(t, svc, nil)
	ctx := context.Background()

	_, err := reg.executeProjectTool(ctx, "project_update", map[string]interface{}{
		"tenant_id": "t1", "project_id": "p1", "default_workflow": "sdlc", "git_author_name": "agent",
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Nil(t, captured.GitSigningEnabled, "an absent git_signing_enabled must reach the service as nil")
	assert.Equal(t, "sdlc", captured.DefaultWorkflow)
	assert.Equal(t, "agent", captured.GitAuthorName)

	_, err = reg.executeProjectTool(ctx, "project_update", map[string]interface{}{
		"tenant_id": "t1", "project_id": "p1",
		"clear_default_workflow": true,
		"git_signing_enabled":    false,
		"git_signing_key_name":   "k", "git_signing_key_scope": "s", "git_signing_key_owner": "o",
		"git_signing_algorithm": "ED25519",
		"git_ssh_key_name":      "a", "git_ssh_key_scope": "b", "git_ssh_key_owner": "c",
		"git_author_email": "agent@example.test",
	})
	require.NoError(t, err)
	require.NotNil(t, captured.GitSigningEnabled)
	assert.False(t, *captured.GitSigningEnabled)
	assert.True(t, captured.ClearDefaultWorkflow)
	assert.Equal(t, "k", captured.GitSigningKeyName)
	assert.Equal(t, "s", captured.GitSigningKeyScope)
	assert.Equal(t, "o", captured.GitSigningKeyOwner)
	assert.Equal(t, "ED25519", captured.GitSigningAlgorithm)
	assert.Equal(t, "a", captured.GitSSHKeyName)
	assert.Equal(t, "b", captured.GitSSHKeyScope)
	assert.Equal(t, "c", captured.GitSSHKeyOwner)
	assert.Equal(t, "agent@example.test", captured.GitAuthorEmail)

	// A non-boolean value is not a boolean: it must not be read as "off".
	_, err = reg.executeProjectTool(ctx, "project_update", map[string]interface{}{
		"tenant_id": "t1", "project_id": "p1", "git_signing_enabled": "false",
	})
	require.NoError(t, err)
	assert.Nil(t, captured.GitSigningEnabled)
}

// TestProjectToolSchemas_DeclareTheNewFields pins the tool contracts an MCP
// client discovers.
func TestProjectToolSchemas_DeclareTheNewFields(t *testing.T) {
	reg := newProjectAdminRegistry(t, &mockProjectService{}, nil)
	fields := []string{
		"default_workflow", "git_author_name", "git_author_email",
		"git_signing_enabled", "git_signing_algorithm",
		"git_signing_key_name", "git_signing_key_scope", "git_signing_key_owner",
		"git_ssh_key_name", "git_ssh_key_scope", "git_ssh_key_owner",
	}
	byName := map[string]ToolDefinition{}
	for _, tool := range reg.ListTools() {
		byName[tool.Name] = tool
	}
	for _, toolName := range []string{"project_create", "project_update"} {
		tool, ok := byName[toolName]
		require.True(t, ok, "%s must be registered", toolName)
		props, ok := tool.InputSchema["properties"].(map[string]interface{})
		require.True(t, ok)
		for _, f := range fields {
			_, declared := props[f]
			assert.True(t, declared, "%s must declare %s", toolName, f)
		}
		_, clear := props["clear_default_workflow"]
		assert.Equal(t, toolName == "project_update", clear, "only project_update declares clear_default_workflow")
	}
}

// =============================================================================
// Autonomy + context repositories over MCP
// =============================================================================

// TestExecuteProjectTool_AutonomyAndContextReposRoundTrip drives create → get
// and update → get through the tool registry, asserting both fields reach the
// service and come back on the project.
func TestExecuteProjectTool_AutonomyAndContextReposRoundTrip(t *testing.T) {
	stored := &orchestration.Project{ID: "p1", TenantID: "t1", Name: "api"}
	var createReq *service.CreateProjectRequest
	var updateReq *service.UpdateProjectRequest

	svc := &mockProjectService{
		createFunc: func(_ context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			createReq = req
			stored.Autonomous = req.Autonomous
			stored.ContextRepos = req.ContextRepos
			return &service.CreateProjectResponse{Project: stored}, nil
		},
		updateFunc: func(_ context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			updateReq = req
			if req.Autonomous != nil {
				stored.Autonomous = *req.Autonomous
			}
			if req.ClearContextRepos {
				stored.ContextRepos = nil
			} else if req.ContextRepos != nil {
				stored.ContextRepos = req.ContextRepos
			}
			return &service.UpdateProjectResponse{Project: stored}, nil
		},
		getFunc: func(context.Context, *service.GetProjectRequest) (*service.GetProjectResponse, error) {
			return &service.GetProjectResponse{Project: stored}, nil
		},
	}
	reg := newProjectAdminRegistry(t, svc, nil)
	ctx := context.Background()

	get := func() *orchestration.Project {
		t.Helper()
		result, err := reg.executeProjectTool(ctx, "project_get", map[string]interface{}{
			"tenant_id": "t1", "project_id": "p1",
		})
		require.NoError(t, err)
		resp, ok := result.(*service.GetProjectResponse)
		require.True(t, ok, "project_get returned %T", result)
		return resp.Project
	}

	createResult, err := reg.executeProjectTool(ctx, "project_create", map[string]interface{}{
		"tenant_id": "t1", "name": "api",
		"autonomous": true,
		"context_repos": []interface{}{
			map[string]interface{}{
				"name":     "handbook",
				"repo_url": "https://forge.example/org/handbook",
				"ref":      "main",
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, createResult)
	require.NotNil(t, createReq)
	assert.True(t, createReq.Autonomous)
	require.Len(t, createReq.ContextRepos, 1)
	assert.Equal(t, orchestration.ContextRepo{
		Name: "handbook", RepoURL: "https://forge.example/org/handbook", Ref: "main",
	}, createReq.ContextRepos[0])

	got := get()
	assert.True(t, got.Autonomous)
	require.Len(t, got.ContextRepos, 1)
	assert.Equal(t, "handbook", got.ContextRepos[0].Name)

	// An absent autonomous is nil (unchanged); an absent context_repos is nil.
	_, err = reg.executeProjectTool(ctx, "project_update", map[string]interface{}{
		"tenant_id": "t1", "project_id": "p1", "name": "renamed",
	})
	require.NoError(t, err)
	require.NotNil(t, updateReq)
	assert.Nil(t, updateReq.Autonomous, "an absent autonomous must reach the service as nil")
	assert.Nil(t, updateReq.ContextRepos, "an absent context_repos must reach the service as nil")
	got = get()
	assert.True(t, got.Autonomous)
	assert.Len(t, got.ContextRepos, 1)

	// An explicit false turns it off; a list replaces.
	_, err = reg.executeProjectTool(ctx, "project_update", map[string]interface{}{
		"tenant_id": "t1", "project_id": "p1",
		"autonomous": false,
		"context_repos": []interface{}{
			map[string]interface{}{"name": "specs", "repo_url": "https://forge.example/org/specs", "ref": "v2"},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, updateReq.Autonomous)
	assert.False(t, *updateReq.Autonomous)
	got = get()
	assert.False(t, got.Autonomous)
	require.Len(t, got.ContextRepos, 1)
	assert.Equal(t, "specs", got.ContextRepos[0].Name)

	// And clear_context_repos empties the list.
	_, err = reg.executeProjectTool(ctx, "project_update", map[string]interface{}{
		"tenant_id": "t1", "project_id": "p1", "clear_context_repos": true,
	})
	require.NoError(t, err)
	assert.True(t, updateReq.ClearContextRepos)
	assert.Empty(t, get().ContextRepos)
}

// TestExecuteProjectTool_RefusesMalformedContextRepos: a context_repos
// argument that is not a list of objects is a typed error, not a silent drop.
func TestExecuteProjectTool_RefusesMalformedContextRepos(t *testing.T) {
	called := false
	svc := &mockProjectService{
		createFunc: func(context.Context, *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			called = true
			return &service.CreateProjectResponse{}, nil
		},
		updateFunc: func(context.Context, *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			called = true
			return &service.UpdateProjectResponse{}, nil
		},
	}
	reg := newProjectAdminRegistry(t, svc, nil)
	ctx := context.Background()

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"not a list", map[string]interface{}{
			"tenant_id": "t1", "name": "api", "context_repos": "handbook",
		}},
		{"element is not an object", map[string]interface{}{
			"tenant_id": "t1", "name": "api", "context_repos": []interface{}{"handbook"},
		}},
	}
	for _, tc := range cases {
		t.Run("create/"+tc.name, func(t *testing.T) {
			called = false
			_, err := reg.executeProjectTool(ctx, "project_create", tc.args)
			var argErr *ContextRepoArgumentError
			require.True(t, errors.As(err, &argErr), "want *ContextRepoArgumentError, got %v", err)
			assert.Equal(t, "context_repos", argErr.Key)
			assert.False(t, called, "the service must not be reached with a malformed argument")
		})
	}

	_, err := reg.executeProjectTool(ctx, "project_update", map[string]interface{}{
		"tenant_id": "t1", "project_id": "p1", "context_repos": 42,
	})
	var argErr *ContextRepoArgumentError
	require.True(t, errors.As(err, &argErr), "want *ContextRepoArgumentError, got %v", err)
	assert.Equal(t, -1, argErr.Index)
	assert.Contains(t, argErr.Error(), "context_repos")
	assert.Contains(t, (&ContextRepoArgumentError{Key: "context_repos", Index: 2}).Error(), "element 2")
}

// TestProjectToolSchemas_DeclareAutonomyAndContextRepos pins the tool contract
// an MCP client discovers for the two new fields.
func TestProjectToolSchemas_DeclareAutonomyAndContextRepos(t *testing.T) {
	reg := newProjectAdminRegistry(t, &mockProjectService{}, nil)
	byName := map[string]ToolDefinition{}
	for _, tool := range reg.ListTools() {
		byName[tool.Name] = tool
	}
	for _, toolName := range []string{"project_create", "project_update"} {
		tool, ok := byName[toolName]
		require.True(t, ok, "%s must be registered", toolName)
		props, ok := tool.InputSchema["properties"].(map[string]interface{})
		require.True(t, ok)
		for _, f := range []string{"autonomous", "context_repos"} {
			_, declared := props[f]
			assert.True(t, declared, "%s must declare %s", toolName, f)
		}
		_, clear := props["clear_context_repos"]
		assert.Equal(t, toolName == "project_update", clear,
			"only project_update declares clear_context_repos")
	}
}
