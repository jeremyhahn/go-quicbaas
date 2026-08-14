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
	stderrors "errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ProjectServer wraps the project service for gRPC endpoints.
type ProjectServer struct {
	pb.UnimplementedProjectServiceServer
	projectSvc service.ProjectService
	logger     *logger.Logger
}

// NewProjectServer creates a new gRPC server for project operations.
func NewProjectServer(projectSvc service.ProjectService, log *logger.Logger) *ProjectServer {
	return &ProjectServer{
		projectSvc: projectSvc,
		logger:     log.WithField("component", "project-grpc-server"),
	}
}

// CreateProject handles the CreateProject RPC.
func (s *ProjectServer) CreateProject(ctx context.Context, req *pb.CreateProjectRequest) (*pb.CreateProjectResponse, error) {
	s.logger.WithField("name", req.Name).Debug("Processing CreateProject request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	var agentRoles map[string]orchestration.ProjectRole
	if len(req.AgentRoles) > 0 {
		agentRoles = make(map[string]orchestration.ProjectRole, len(req.AgentRoles))
		for k, v := range req.AgentRoles {
			agentRoles[k] = orchestration.ProjectRole(v)
		}
	}

	resp, err := s.projectSvc.CreateProject(ctx, &service.CreateProjectRequest{
		TenantID:         req.TenantId,
		Name:             req.Name,
		Description:      req.Description,
		WorkspaceAgentID: req.WorkspaceAgentId,
		WorkspacePath:    req.WorkspacePath,
		HostAffinity:     req.HostAffinity,
		DefaultLeadAgent: req.DefaultLeadAgent,
		GitRepoURL:       req.GitRepoUrl,
		GitDefaultBranch: req.GitDefaultBranch,
		AgentRoles:       agentRoles,
		TokenBudget:      req.TokenBudget,
		GPUMinutes:       req.GpuMinutes,
		MaxConcurrency:   int(req.MaxConcurrency),
		Metadata:         req.Metadata,
	})
	if err != nil {
		return nil, convertProjectError(err)
	}

	return &pb.CreateProjectResponse{
		Project:   projectToProto(resp.Project),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetProject handles the GetProject RPC.
func (s *ProjectServer) GetProject(ctx context.Context, req *pb.GetProjectRequest) (*pb.GetProjectResponse, error) {
	s.logger.WithField("project_id", req.ProjectId).Debug("Processing GetProject request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}

	resp, err := s.projectSvc.GetProject(ctx, &service.GetProjectRequest{
		TenantID:  req.TenantId,
		ProjectID: req.ProjectId,
	})
	if err != nil {
		return nil, convertProjectError(err)
	}

	return &pb.GetProjectResponse{
		Project:   projectToProto(resp.Project),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetProjectByName handles the GetProjectByName RPC.
func (s *ProjectServer) GetProjectByName(ctx context.Context, req *pb.GetProjectByNameRequest) (*pb.GetProjectResponse, error) {
	s.logger.WithField("name", req.Name).Debug("Processing GetProjectByName request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	resp, err := s.projectSvc.GetProjectByName(ctx, &service.GetProjectByNameRequest{
		TenantID: req.TenantId,
		Name:     req.Name,
	})
	if err != nil {
		return nil, convertProjectError(err)
	}

	return &pb.GetProjectResponse{
		Project:   projectToProto(resp.Project),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// UpdateProject handles the UpdateProject RPC.
func (s *ProjectServer) UpdateProject(ctx context.Context, req *pb.UpdateProjectRequest) (*pb.UpdateProjectResponse, error) {
	s.logger.WithField("project_id", req.ProjectId).Debug("Processing UpdateProject request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}

	var agentRoles map[string]orchestration.ProjectRole
	if len(req.AgentRoles) > 0 {
		agentRoles = make(map[string]orchestration.ProjectRole, len(req.AgentRoles))
		for k, v := range req.AgentRoles {
			agentRoles[k] = orchestration.ProjectRole(v)
		}
	}

	resp, err := s.projectSvc.UpdateProject(ctx, &service.UpdateProjectRequest{
		TenantID:         req.TenantId,
		ProjectID:        req.ProjectId,
		Name:             req.Name,
		Description:      req.Description,
		ClearDescription: req.ClearDescription,
		WorkspaceAgentID: req.WorkspaceAgentId,
		WorkspacePath:    req.WorkspacePath,
		HostAffinity:     req.HostAffinity,
		DefaultLeadAgent: req.DefaultLeadAgent,
		GitRepoURL:       req.GitRepoUrl,
		GitDefaultBranch: req.GitDefaultBranch,
		GitCurrentSHA:    req.GitCurrentSha,
		AgentRoles:       agentRoles,
		TokenBudget:      req.TokenBudget,
		GPUMinutes:       req.GpuMinutes,
		MaxConcurrency:   int(req.MaxConcurrency),
		Metadata:         req.Metadata,
	})
	if err != nil {
		return nil, convertProjectError(err)
	}

	return &pb.UpdateProjectResponse{
		Project:   projectToProto(resp.Project),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// DeleteProject handles the DeleteProject RPC.
func (s *ProjectServer) DeleteProject(ctx context.Context, req *pb.DeleteProjectRequest) (*pb.DeleteProjectResponse, error) {
	s.logger.WithField("project_id", req.ProjectId).Debug("Processing DeleteProject request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}

	resp, err := s.projectSvc.DeleteProject(ctx, &service.DeleteProjectRequest{
		TenantID:  req.TenantId,
		ProjectID: req.ProjectId,
	})
	if err != nil {
		return nil, convertProjectError(err)
	}

	return &pb.DeleteProjectResponse{
		ProjectId: resp.ProjectID,
		Deleted:   resp.Deleted,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// ListProjects handles the ListProjects RPC.
func (s *ProjectServer) ListProjects(ctx context.Context, req *pb.ListProjectsRequest) (*pb.ListProjectsResponse, error) {
	s.logger.WithField("tenant_id", req.TenantId).Debug("Processing ListProjects request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	resp, err := s.projectSvc.ListProjects(ctx, &service.ListProjectsRequest{
		TenantID: req.TenantId,
	})
	if err != nil {
		return nil, convertProjectError(err)
	}

	projects := make([]*pb.Project, 0, len(resp.Projects))
	for _, p := range resp.Projects {
		projects = append(projects, projectToProto(p))
	}

	return &pb.ListProjectsResponse{
		Projects:  projects,
		Total:     int32(resp.Total),
		Timestamp: time.Now().Unix(),
	}, nil
}

// GrantAgentRole handles the GrantAgentRole RPC. It sets one agent's role
// without replacing the rest of the project's role map, which is what an
// UpdateProject-based emulation would do.
func (s *ProjectServer) GrantAgentRole(ctx context.Context, req *pb.GrantAgentRoleRequest) (*pb.GrantAgentRoleResponse, error) {
	s.logger.WithField("project_id", req.ProjectId).
		WithField("agent_id", req.AgentId).
		Debug("Processing GrantAgentRole request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}
	if req.Role == "" {
		return nil, status.Error(codes.InvalidArgument, "role is required")
	}

	resp, err := s.projectSvc.GrantAgentRole(ctx, &service.GrantAgentRoleRequest{
		TenantID:  req.TenantId,
		ProjectID: req.ProjectId,
		AgentID:   req.AgentId,
		Role:      orchestration.ProjectRole(req.Role),
	})
	if err != nil {
		return nil, convertProjectError(err)
	}

	return &pb.GrantAgentRoleResponse{
		ProjectId: resp.ProjectID,
		AgentId:   resp.AgentID,
		Role:      string(resp.Role),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// RevokeAgentRole handles the RevokeAgentRole RPC. It denies one agent and
// leaves every other agent's role intact.
func (s *ProjectServer) RevokeAgentRole(ctx context.Context, req *pb.RevokeAgentRoleRequest) (*pb.RevokeAgentRoleResponse, error) {
	s.logger.WithField("project_id", req.ProjectId).
		WithField("agent_id", req.AgentId).
		Debug("Processing RevokeAgentRole request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	resp, err := s.projectSvc.RevokeAgentRole(ctx, &service.RevokeAgentRoleRequest{
		TenantID:  req.TenantId,
		ProjectID: req.ProjectId,
		AgentID:   req.AgentId,
	})
	if err != nil {
		return nil, convertProjectError(err)
	}

	return &pb.RevokeAgentRoleResponse{
		ProjectId: resp.ProjectID,
		AgentId:   resp.AgentID,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// =============================================================================
// Helpers
// =============================================================================

// projectToProto converts an orchestration.Project to the proto message.
func projectToProto(p *orchestration.Project) *pb.Project {
	if p == nil {
		return nil
	}
	pb := &pb.Project{
		Id:               p.ID,
		TenantId:         p.TenantID,
		Name:             p.Name,
		Description:      p.Description,
		WorkspaceAgentId: p.Workspace.AgentID,
		WorkspacePath:    p.Workspace.Path,
		HostAffinity:     p.HostAffinity,
		DefaultLeadAgent: p.DefaultLeadAgent,
		GitRepoUrl:       p.Git.RepoURL,
		GitDefaultBranch: p.Git.DefaultBranch,
		GitCurrentSha:    p.Git.CurrentSHA,
		TokenBudget:      p.Quotas.TokenBudget,
		GpuMinutes:       p.Quotas.GPUMinutes,
		MaxConcurrency:   int32(p.Quotas.MaxConcurrency),
		Metadata:         p.Metadata,
		CreatedAtUnix:    p.CreatedAt.Unix(),
		UpdatedAtUnix:    p.UpdatedAt.Unix(),
	}
	if p.Permissions.AgentRoles != nil {
		pb.AgentRoles = make(map[string]string, len(p.Permissions.AgentRoles))
		for k, v := range p.Permissions.AgentRoles {
			pb.AgentRoles[k] = string(v)
		}
	}
	return pb
}

// convertProjectError maps service-layer project errors to gRPC status codes.
func convertProjectError(err error) error {
	var notFound *service.ProjectNotFoundError
	var alreadyExists *service.ProjectAlreadyExistsError
	var invalid *service.ProjectInvalidError

	switch {
	case stderrors.As(err, &notFound):
		return status.Error(codes.NotFound, err.Error())
	case stderrors.As(err, &alreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case stderrors.As(err, &invalid):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// Compile-time interface satisfaction check.
var _ pb.ProjectServiceServer = (*ProjectServer)(nil)
