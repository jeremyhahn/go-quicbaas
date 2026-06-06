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
