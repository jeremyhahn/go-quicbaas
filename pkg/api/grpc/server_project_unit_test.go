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

package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock ProjectService (grpc package)
// =============================================================================

type grpcMockProjectService struct {
	createFunc func(context.Context, *service.CreateProjectRequest) (*service.CreateProjectResponse, error)
	getFunc    func(context.Context, *service.GetProjectRequest) (*service.GetProjectResponse, error)
	getByName  func(context.Context, *service.GetProjectByNameRequest) (*service.GetProjectResponse, error)
	updateFunc func(context.Context, *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error)
	deleteFunc func(context.Context, *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error)
	listFunc   func(context.Context, *service.ListProjectsRequest) (*service.ListProjectsResponse, error)
	grantFunc  func(context.Context, *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error)
	revokeFunc func(context.Context, *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error)

	// roles is the mock's agent role map. The default grant/revoke behaviour
	// mutates a single entry, mirroring the real service, so tests can prove
	// the other entries survive.
	roles map[string]orchestration.ProjectRole
}

func sampleProject() *orchestration.Project {
	p := &orchestration.Project{
		ID:       "proj-1",
		TenantID: "tenant-1",
		Name:     "demo",
	}
	p.Workspace.AgentID = "agent-1"
	p.Permissions.AgentRoles = map[string]orchestration.ProjectRole{"agent-1": orchestration.ProjectRole("lead")}
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	return p
}

func (m *grpcMockProjectService) CreateProject(ctx context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, req)
	}
	return &service.CreateProjectResponse{Project: sampleProject(), Timestamp: time.Now()}, nil
}

func (m *grpcMockProjectService) GetProject(ctx context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, req)
	}
	return &service.GetProjectResponse{Project: sampleProject(), Timestamp: time.Now()}, nil
}

func (m *grpcMockProjectService) GetProjectByName(ctx context.Context, req *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
	if m.getByName != nil {
		return m.getByName(ctx, req)
	}
	return &service.GetProjectResponse{Project: sampleProject(), Timestamp: time.Now()}, nil
}

func (m *grpcMockProjectService) UpdateProject(ctx context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, req)
	}
	return &service.UpdateProjectResponse{Project: sampleProject(), Timestamp: time.Now()}, nil
}

func (m *grpcMockProjectService) DeleteProject(ctx context.Context, req *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, req)
	}
	return &service.DeleteProjectResponse{ProjectID: req.ProjectID, Deleted: true, Timestamp: time.Now()}, nil
}

func (m *grpcMockProjectService) ListProjects(ctx context.Context, req *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, req)
	}
	return &service.ListProjectsResponse{Projects: []*orchestration.Project{sampleProject()}, Total: 1, Timestamp: time.Now()}, nil
}

func (m *grpcMockProjectService) AuthorizeAgent(context.Context, *service.AuthorizeAgentRequest) error {
	return nil
}

func (m *grpcMockProjectService) GrantAgentRole(ctx context.Context, req *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
	if m.grantFunc != nil {
		return m.grantFunc(ctx, req)
	}
	if m.roles == nil {
		m.roles = make(map[string]orchestration.ProjectRole)
	}
	m.roles[req.AgentID] = req.Role
	return &service.GrantAgentRoleResponse{
		ProjectID: req.ProjectID,
		AgentID:   req.AgentID,
		Role:      req.Role,
		Timestamp: time.Now(),
	}, nil
}

func (m *grpcMockProjectService) RevokeAgentRole(ctx context.Context, req *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error) {
	if m.revokeFunc != nil {
		return m.revokeFunc(ctx, req)
	}
	if m.roles == nil {
		m.roles = make(map[string]orchestration.ProjectRole)
	}
	m.roles[req.AgentID] = orchestration.ProjectRoleDenied
	return &service.RevokeAgentRoleResponse{
		ProjectID: req.ProjectID,
		AgentID:   req.AgentID,
		Timestamp: time.Now(),
	}, nil
}

func newProjectServer(t *testing.T, svc service.ProjectService) *ProjectServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)
	return NewProjectServer(svc, log)
}

// =============================================================================
// Happy paths
// =============================================================================

func TestProjectServer_HappyPaths(t *testing.T) {
	srv := newProjectServer(t, &grpcMockProjectService{})
	ctx := context.Background()

	created, err := srv.CreateProject(ctx, &pb.CreateProjectRequest{
		TenantId: "tenant-1", Name: "demo",
		AgentRoles: map[string]string{"agent-1": "lead"},
	})
	require.NoError(t, err)
	require.NotNil(t, created.Project)
	assert.Equal(t, "proj-1", created.Project.Id)
	assert.Equal(t, "lead", created.Project.AgentRoles["agent-1"])

	got, err := srv.GetProject(ctx, &pb.GetProjectRequest{TenantId: "tenant-1", ProjectId: "proj-1"})
	require.NoError(t, err)
	assert.Equal(t, "demo", got.Project.Name)

	byName, err := srv.GetProjectByName(ctx, &pb.GetProjectByNameRequest{TenantId: "tenant-1", Name: "demo"})
	require.NoError(t, err)
	assert.Equal(t, "proj-1", byName.Project.Id)

	updated, err := srv.UpdateProject(ctx, &pb.UpdateProjectRequest{
		TenantId: "tenant-1", ProjectId: "proj-1", Name: "demo2",
		AgentRoles: map[string]string{"agent-2": "worker"},
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Project)

	deleted, err := srv.DeleteProject(ctx, &pb.DeleteProjectRequest{TenantId: "tenant-1", ProjectId: "proj-1"})
	require.NoError(t, err)
	assert.True(t, deleted.Deleted)

	listed, err := srv.ListProjects(ctx, &pb.ListProjectsRequest{TenantId: "tenant-1"})
	require.NoError(t, err)
	assert.Equal(t, int32(1), listed.Total)
	assert.Len(t, listed.Projects, 1)
}

// =============================================================================
// Validation errors
// =============================================================================

func TestProjectServer_ValidationErrors(t *testing.T) {
	srv := newProjectServer(t, &grpcMockProjectService{})
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"create-no-tenant", func() error {
			_, err := srv.CreateProject(ctx, &pb.CreateProjectRequest{Name: "x"})
			return err
		}},
		{"create-no-name", func() error {
			_, err := srv.CreateProject(ctx, &pb.CreateProjectRequest{TenantId: "t"})
			return err
		}},
		{"get-no-tenant", func() error {
			_, err := srv.GetProject(ctx, &pb.GetProjectRequest{ProjectId: "p"})
			return err
		}},
		{"get-no-id", func() error {
			_, err := srv.GetProject(ctx, &pb.GetProjectRequest{TenantId: "t"})
			return err
		}},
		{"getbyname-no-tenant", func() error {
			_, err := srv.GetProjectByName(ctx, &pb.GetProjectByNameRequest{Name: "n"})
			return err
		}},
		{"getbyname-no-name", func() error {
			_, err := srv.GetProjectByName(ctx, &pb.GetProjectByNameRequest{TenantId: "t"})
			return err
		}},
		{"update-no-tenant", func() error {
			_, err := srv.UpdateProject(ctx, &pb.UpdateProjectRequest{ProjectId: "p"})
			return err
		}},
		{"update-no-id", func() error {
			_, err := srv.UpdateProject(ctx, &pb.UpdateProjectRequest{TenantId: "t"})
			return err
		}},
		{"delete-no-tenant", func() error {
			_, err := srv.DeleteProject(ctx, &pb.DeleteProjectRequest{ProjectId: "p"})
			return err
		}},
		{"delete-no-id", func() error {
			_, err := srv.DeleteProject(ctx, &pb.DeleteProjectRequest{TenantId: "t"})
			return err
		}},
		{"list-no-tenant", func() error {
			_, err := srv.ListProjects(ctx, &pb.ListProjectsRequest{})
			return err
		}},
		{"grant-no-tenant", func() error {
			_, err := srv.GrantAgentRole(ctx, &pb.GrantAgentRoleRequest{ProjectId: "p", AgentId: "a", Role: "lead"})
			return err
		}},
		{"grant-no-project", func() error {
			_, err := srv.GrantAgentRole(ctx, &pb.GrantAgentRoleRequest{TenantId: "t", AgentId: "a", Role: "lead"})
			return err
		}},
		{"grant-no-agent", func() error {
			_, err := srv.GrantAgentRole(ctx, &pb.GrantAgentRoleRequest{TenantId: "t", ProjectId: "p", Role: "lead"})
			return err
		}},
		{"grant-no-role", func() error {
			_, err := srv.GrantAgentRole(ctx, &pb.GrantAgentRoleRequest{TenantId: "t", ProjectId: "p", AgentId: "a"})
			return err
		}},
		{"revoke-no-tenant", func() error {
			_, err := srv.RevokeAgentRole(ctx, &pb.RevokeAgentRoleRequest{ProjectId: "p", AgentId: "a"})
			return err
		}},
		{"revoke-no-project", func() error {
			_, err := srv.RevokeAgentRole(ctx, &pb.RevokeAgentRoleRequest{TenantId: "t", AgentId: "a"})
			return err
		}},
		{"revoke-no-agent", func() error {
			_, err := srv.RevokeAgentRole(ctx, &pb.RevokeAgentRoleRequest{TenantId: "t", ProjectId: "p"})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

// =============================================================================
// Service-error mapping
// =============================================================================

func TestProjectServer_ServiceErrorMapping(t *testing.T) {
	ctx := context.Background()

	t.Run("not-found", func(t *testing.T) {
		srv := newProjectServer(t, &grpcMockProjectService{
			getFunc: func(context.Context, *service.GetProjectRequest) (*service.GetProjectResponse, error) {
				return nil, &service.ProjectNotFoundError{TenantID: "t", ProjectID: "p"}
			},
		})
		_, err := srv.GetProject(ctx, &pb.GetProjectRequest{TenantId: "t", ProjectId: "p"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("already-exists", func(t *testing.T) {
		srv := newProjectServer(t, &grpcMockProjectService{
			createFunc: func(context.Context, *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
				return nil, &service.ProjectAlreadyExistsError{TenantID: "t", Name: "demo"}
			},
		})
		_, err := srv.CreateProject(ctx, &pb.CreateProjectRequest{TenantId: "t", Name: "demo"})
		assert.Equal(t, codes.AlreadyExists, status.Code(err))
	})

	t.Run("invalid", func(t *testing.T) {
		srv := newProjectServer(t, &grpcMockProjectService{
			updateFunc: func(context.Context, *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
				return nil, &service.ProjectInvalidError{Field: "name", Message: "bad"}
			},
		})
		_, err := srv.UpdateProject(ctx, &pb.UpdateProjectRequest{TenantId: "t", ProjectId: "p"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("internal", func(t *testing.T) {
		srv := newProjectServer(t, &grpcMockProjectService{
			listFunc: func(context.Context, *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
				return nil, errors.New("db down")
			},
		})
		_, err := srv.ListProjects(ctx, &pb.ListProjectsRequest{TenantId: "t"})
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("delete-error", func(t *testing.T) {
		srv := newProjectServer(t, &grpcMockProjectService{
			deleteFunc: func(context.Context, *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
				return nil, &service.ProjectNotFoundError{TenantID: "t", ProjectID: "p"}
			},
		})
		_, err := srv.DeleteProject(ctx, &pb.DeleteProjectRequest{TenantId: "t", ProjectId: "p"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("getbyname-error", func(t *testing.T) {
		srv := newProjectServer(t, &grpcMockProjectService{
			getByName: func(context.Context, *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
				return nil, &service.ProjectNotFoundError{TenantID: "t", Name: "demo"}
			},
		})
		_, err := srv.GetProjectByName(ctx, &pb.GetProjectByNameRequest{TenantId: "t", Name: "demo"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

// =============================================================================
// Agent role membership
// =============================================================================

// TestProjectServer_GrantAgentRole asserts the RPC threads every field into the
// service request and converts the response back faithfully.
func TestProjectServer_GrantAgentRole(t *testing.T) {
	var seen *service.GrantAgentRoleRequest
	stamp := time.Unix(1_700_003_600, 0).UTC()
	srv := newProjectServer(t, &grpcMockProjectService{
		grantFunc: func(_ context.Context, req *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
			seen = req
			return &service.GrantAgentRoleResponse{
				ProjectID: req.ProjectID,
				AgentID:   req.AgentID,
				Role:      req.Role,
				Timestamp: stamp,
			}, nil
		},
	})

	resp, err := srv.GrantAgentRole(context.Background(), &pb.GrantAgentRoleRequest{
		TenantId: "tenant-1", ProjectId: "proj-1", AgentId: "agent-3", Role: "worker",
	})
	require.NoError(t, err)

	require.NotNil(t, seen)
	assert.Equal(t, "tenant-1", seen.TenantID)
	assert.Equal(t, "proj-1", seen.ProjectID)
	assert.Equal(t, "agent-3", seen.AgentID)
	assert.Equal(t, orchestration.ProjectRole("worker"), seen.Role)

	assert.Equal(t, "proj-1", resp.ProjectId)
	assert.Equal(t, "agent-3", resp.AgentId)
	assert.Equal(t, "worker", resp.Role)
	assert.Equal(t, stamp.Unix(), resp.Timestamp)
}

// TestProjectServer_GrantAgentRole_PreservesOtherAgents is the regression for
// the data-loss scenario the dedicated RPC exists to prevent. Routing the grant
// through UpdateProject would send a single-entry agent_roles map, and the
// service REPLACES the whole map from that field — wiping every other agent.
// The grant path must instead mutate exactly one entry.
func TestProjectServer_GrantAgentRole_PreservesOtherAgents(t *testing.T) {
	var updateCalls int
	mock := &grpcMockProjectService{
		roles: map[string]orchestration.ProjectRole{
			"agent-1": orchestration.ProjectRole("lead"),
			"agent-2": orchestration.ProjectRole("observer"),
		},
		updateFunc: func(context.Context, *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			updateCalls++
			return &service.UpdateProjectResponse{Project: sampleProject(), Timestamp: time.Now()}, nil
		},
	}
	srv := newProjectServer(t, mock)

	_, err := srv.GrantAgentRole(context.Background(), &pb.GrantAgentRoleRequest{
		TenantId: "tenant-1", ProjectId: "proj-1", AgentId: "agent-3", Role: "worker",
	})
	require.NoError(t, err)

	assert.Zero(t, updateCalls, "grant must not be emulated through UpdateProject")
	assert.Equal(t, map[string]orchestration.ProjectRole{
		"agent-1": orchestration.ProjectRole("lead"),
		"agent-2": orchestration.ProjectRole("observer"),
		"agent-3": orchestration.ProjectRole("worker"),
	}, mock.roles)
}

// TestProjectServer_RevokeAgentRole asserts field threading, the response
// conversion, and that only the named agent is denied.
func TestProjectServer_RevokeAgentRole(t *testing.T) {
	mock := &grpcMockProjectService{
		roles: map[string]orchestration.ProjectRole{
			"agent-1": orchestration.ProjectRole("lead"),
			"agent-2": orchestration.ProjectRole("observer"),
		},
	}
	srv := newProjectServer(t, mock)

	resp, err := srv.RevokeAgentRole(context.Background(), &pb.RevokeAgentRoleRequest{
		TenantId: "tenant-1", ProjectId: "proj-1", AgentId: "agent-2",
	})
	require.NoError(t, err)
	assert.Equal(t, "proj-1", resp.ProjectId)
	assert.Equal(t, "agent-2", resp.AgentId)
	assert.NotZero(t, resp.Timestamp)

	assert.Equal(t, orchestration.ProjectRole("lead"), mock.roles["agent-1"],
		"revoking agent-2 must not disturb agent-1")
	assert.Equal(t, orchestration.ProjectRoleDenied, mock.roles["agent-2"])
}

// TestProjectServer_AgentRoleErrorMapping covers the service-error path for both
// membership verbs.
func TestProjectServer_AgentRoleErrorMapping(t *testing.T) {
	ctx := context.Background()

	t.Run("grant-not-found", func(t *testing.T) {
		srv := newProjectServer(t, &grpcMockProjectService{
			grantFunc: func(context.Context, *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
				return nil, &service.ProjectNotFoundError{TenantID: "t", ProjectID: "p"}
			},
		})
		_, err := srv.GrantAgentRole(ctx, &pb.GrantAgentRoleRequest{
			TenantId: "t", ProjectId: "p", AgentId: "a", Role: "lead",
		})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("grant-internal", func(t *testing.T) {
		srv := newProjectServer(t, &grpcMockProjectService{
			grantFunc: func(context.Context, *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
				return nil, errors.New("db down")
			},
		})
		_, err := srv.GrantAgentRole(ctx, &pb.GrantAgentRoleRequest{
			TenantId: "t", ProjectId: "p", AgentId: "a", Role: "lead",
		})
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("revoke-not-found", func(t *testing.T) {
		srv := newProjectServer(t, &grpcMockProjectService{
			revokeFunc: func(context.Context, *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error) {
				return nil, &service.ProjectNotFoundError{TenantID: "t", ProjectID: "p"}
			},
		})
		_, err := srv.RevokeAgentRole(ctx, &pb.RevokeAgentRoleRequest{TenantId: "t", ProjectId: "p", AgentId: "a"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("revoke-invalid", func(t *testing.T) {
		srv := newProjectServer(t, &grpcMockProjectService{
			revokeFunc: func(context.Context, *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error) {
				return nil, &service.ProjectInvalidError{Field: "agent_id", Message: "bad"}
			},
		})
		_, err := srv.RevokeAgentRole(ctx, &pb.RevokeAgentRoleRequest{TenantId: "t", ProjectId: "p", AgentId: "a"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

// projectToProto returns nil for a nil input.
func TestProjectToProto_Nil(t *testing.T) {
	assert.Nil(t, projectToProto(nil))
}
