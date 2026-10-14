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

	var fleetNodeRoles map[string]orchestration.ProjectRole
	if len(req.AgentRoles) > 0 {
		fleetNodeRoles = make(map[string]orchestration.ProjectRole, len(req.AgentRoles))
		for k, v := range req.AgentRoles {
			fleetNodeRoles[k] = orchestration.ProjectRole(v)
		}
	}

	resp, err := s.projectSvc.CreateProject(ctx, &service.CreateProjectRequest{
		TenantID:             req.TenantId,
		Name:                 req.Name,
		Description:          req.Description,
		WorkspaceFleetNodeID: req.WorkspaceAgentId,
		WorkspacePath:        req.WorkspacePath,
		HostAffinity:         req.HostAffinity,
		DefaultLeadAgent:     AgentRefFromProto(req.GetDefaultLeadAgent()),
		GitRepoURL:           req.GitRepoUrl,
		GitDefaultBranch:     req.GitDefaultBranch,
		FleetNodeRoles:       fleetNodeRoles,
		Spend:                spendLimitsFromProto(req.GetSpend()),
		GPUMinutes:           req.GpuMinutes,
		MaxConcurrency:       int(req.MaxConcurrency),
		Metadata:             req.Metadata,

		DefaultWorkflow:     req.DefaultWorkflow,
		Autonomous:          req.Autonomous,
		ContextRepos:        contextReposFromProto(req.ContextRepos),
		GitAuthorName:       req.GitAuthorName,
		GitAuthorEmail:      req.GitAuthorEmail,
		GitSigningEnabled:   req.GitSigningEnabled,
		GitSigningAlgorithm: req.GitSigningAlgorithm,
		GitSigningKeyName:   req.GitSigningKeyName,
		GitSigningKeyScope:  req.GitSigningKeyScope,
		GitSigningKeyOwner:  req.GitSigningKeyOwner,
		GitSSHKeyName:       req.GitSshKeyName,
		GitSSHKeyScope:      req.GitSshKeyScope,
		GitSSHKeyOwner:      req.GitSshKeyOwner,
		WorkspaceTier:       orchestration.WorkspaceTier(req.WorkspaceTier),
		WorkspaceTmpfsMB:    int(req.WorkspaceTmpfsMb),
		ContainerMemoryMB:   int(req.ContainerMemoryMb),
		SessionRotation:     orchestration.SessionRotationPolicy(req.SessionRotation),

		WorkspaceManaged:       req.WorkspaceManaged,
		GitCredentialSecretRef: req.GitCredentialSecretRef,
		GitCredentialUsername:  req.GitCredentialUsername,
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

	var fleetNodeRoles map[string]orchestration.ProjectRole
	if len(req.AgentRoles) > 0 {
		fleetNodeRoles = make(map[string]orchestration.ProjectRole, len(req.AgentRoles))
		for k, v := range req.AgentRoles {
			fleetNodeRoles[k] = orchestration.ProjectRole(v)
		}
	}

	retirement, err := service.ParseGitSigningRetirement(req.GitSigningRetireKeyName,
		req.GitSigningRetireKeyScope, req.GitSigningRetireKeyOwner, req.GitSigningRetiredAt)
	if err != nil {
		return nil, convertProjectError(err)
	}

	resp, err := s.projectSvc.UpdateProject(ctx, &service.UpdateProjectRequest{
		TenantID:             req.TenantId,
		ProjectID:            req.ProjectId,
		Name:                 req.Name,
		Description:          req.Description,
		ClearDescription:     req.ClearDescription,
		WorkspaceFleetNodeID: req.WorkspaceAgentId,
		WorkspacePath:        req.WorkspacePath,
		HostAffinity:         req.HostAffinity,
		DefaultLeadAgent:     AgentRefFromProto(req.GetDefaultLeadAgent()),
		GitRepoURL:           req.GitRepoUrl,
		GitDefaultBranch:     req.GitDefaultBranch,
		GitCurrentSHA:        req.GitCurrentSha,
		FleetNodeRoles:       fleetNodeRoles,
		Spend:                optionalSpendLimits(req.GetSpend()),
		GPUMinutes:           req.GpuMinutes,
		MaxConcurrency:       intPtrFromInt32(req.MaxConcurrency),
		Metadata:             req.Metadata,

		DefaultWorkflow:      req.DefaultWorkflow,
		ClearDefaultWorkflow: req.ClearDefaultWorkflow,
		Autonomous:           req.Autonomous,
		Parked:               req.Parked,
		ContextRepos:         contextReposFromProto(req.ContextRepos),
		ClearContextRepos:    req.ClearContextRepos,
		GitAuthorName:        req.GitAuthorName,
		GitAuthorEmail:       req.GitAuthorEmail,
		GitSigningEnabled:    req.GitSigningEnabled,
		GitSigningAlgorithm:  req.GitSigningAlgorithm,
		GitSigningKeyName:    req.GitSigningKeyName,
		GitSigningKeyScope:   req.GitSigningKeyScope,
		GitSigningKeyOwner:   req.GitSigningKeyOwner,
		GitSSHKeyName:        req.GitSshKeyName,
		GitSSHKeyScope:       req.GitSshKeyScope,
		GitSSHKeyOwner:       req.GitSshKeyOwner,
		GitSigningRetire:     retirement,
		WorkspaceTier:        orchestration.WorkspaceTier(req.WorkspaceTier),
		WorkspaceTmpfsMB:     intPtrFromInt32(req.WorkspaceTmpfsMb),
		ContainerMemoryMB:    intPtrFromInt32(req.ContainerMemoryMb),
		SessionRotation:      orchestration.SessionRotationPolicy(req.SessionRotation),
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

	page, err := pageFromRPC(req.GetLimit(), req.GetOffset(), req.GetSort(), req.GetQ())
	if err != nil {
		return nil, err
	}

	resp, err := s.projectSvc.ListProjects(ctx, &service.ListProjectsRequest{
		TenantID: req.TenantId,
		Page:     page,
	})
	if err != nil {
		return nil, convertListError(err, convertProjectError)
	}

	projects := make([]*pb.Project, 0, len(resp.Projects))
	for _, p := range resp.Projects {
		projects = append(projects, projectToProto(p))
	}

	return &pb.ListProjectsResponse{
		Projects:   projects,
		Total:      pageInt32(resp.Total),
		Timestamp:  time.Now().Unix(),
		Limit:      pageInt32(resp.Limit),
		Offset:     pageInt32(resp.Offset),
		NextOffset: pageInt32(resp.NextOffset),
		Facets:     facetsToProto(resp.Facets),
	}, nil
}

// GrantFleetNodeRole handles the GrantFleetNodeRole RPC. It sets one agent's role
// without replacing the rest of the project's role map, which is what an
// UpdateProject-based emulation would do.
func (s *ProjectServer) GrantFleetNodeRole(ctx context.Context, req *pb.GrantFleetNodeRoleRequest) (*pb.GrantFleetNodeRoleResponse, error) {
	s.logger.WithField("project_id", req.ProjectId).
		WithField("agent_id", req.NodeId).
		Debug("Processing GrantFleetNodeRole request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}
	if req.Role == "" {
		return nil, status.Error(codes.InvalidArgument, "role is required")
	}

	resp, err := s.projectSvc.GrantFleetNodeRole(ctx, &service.GrantFleetNodeRoleRequest{
		TenantID:    req.TenantId,
		ProjectID:   req.ProjectId,
		FleetNodeID: req.NodeId,
		Role:        orchestration.ProjectRole(req.Role),
	})
	if err != nil {
		return nil, convertProjectError(err)
	}

	return &pb.GrantFleetNodeRoleResponse{
		ProjectId: resp.ProjectID,
		NodeId:    resp.FleetNodeID,
		Role:      string(resp.Role),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// RevokeFleetNodeRole handles the RevokeFleetNodeRole RPC. It denies one agent and
// leaves every other agent's role intact.
func (s *ProjectServer) RevokeFleetNodeRole(ctx context.Context, req *pb.RevokeFleetNodeRoleRequest) (*pb.RevokeFleetNodeRoleResponse, error) {
	s.logger.WithField("project_id", req.ProjectId).
		WithField("agent_id", req.NodeId).
		Debug("Processing RevokeFleetNodeRole request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.ProjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "project_id is required")
	}
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	resp, err := s.projectSvc.RevokeFleetNodeRole(ctx, &service.RevokeFleetNodeRoleRequest{
		TenantID:    req.TenantId,
		ProjectID:   req.ProjectId,
		FleetNodeID: req.NodeId,
	})
	if err != nil {
		return nil, convertProjectError(err)
	}

	return &pb.RevokeFleetNodeRoleResponse{
		ProjectId: resp.ProjectID,
		NodeId:    resp.FleetNodeID,
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
		WorkspaceAgentId: p.Workspace.FleetNodeID,
		WorkspacePath:    p.Workspace.Path,
		HostAffinity:     p.HostAffinity,
		DefaultLeadAgent: AgentRefToProto(p.DefaultLeadAgent),
		GitRepoUrl:       p.Git.RepoURL,
		GitDefaultBranch: p.Git.DefaultBranch,
		GitCurrentSha:    p.Git.CurrentSHA,
		Spend:            spendLimitsToProto(p.Spend),
		GpuMinutes:       p.Quotas.GPUMinutes,
		MaxConcurrency:   int32(p.Quotas.MaxConcurrency),
		Metadata:         p.Metadata,
		CreatedAtUnix:    p.CreatedAt.Unix(),
		UpdatedAtUnix:    p.UpdatedAt.Unix(),

		DefaultWorkflow:     p.DefaultWorkflow,
		Autonomous:          p.Autonomous,
		ContextRepos:        contextReposToProto(p.ContextRepos),
		GitAuthorName:       p.Git.Author.Name,
		GitAuthorEmail:      p.Git.Author.Email,
		GitSigningEnabled:   p.Git.Signing.Enabled,
		GitSigningAlgorithm: p.Git.Signing.Algorithm,
		GitSigningKeyName:   p.Git.Signing.Key.Name,
		GitSigningKeyScope:  p.Git.Signing.Key.Scope,
		GitSigningKeyOwner:  p.Git.Signing.Key.Owner,
		GitSshKeyName:       p.Git.SSHKey.Name,
		GitSshKeyScope:      p.Git.SSHKey.Scope,
		GitSshKeyOwner:      p.Git.SSHKey.Owner,
		WorkspaceTier:       string(p.Storage.WorkspaceTier),
		WorkspaceTmpfsMb:    int32(p.Storage.WorkspaceTmpfsMB),
		ContainerMemoryMb:   int32(p.Storage.ContainerMemoryMB),
		SessionRotation:     string(p.SessionRotation),
		Parked:              p.Parked,
		ParkedBy:            p.ParkedBy,
		GitSigningRetired:   retiredSigningKeysToProto(p.Git.Signing.Retired),
		WorkspaceManaged:    p.Provisioning.Managed,
		ProvisioningState:   string(p.Provisioning.State),
		ProvisioningError:   p.Provisioning.Error,
	}
	if !p.ParkedAt.IsZero() {
		pb.ParkedAtUnix = p.ParkedAt.Unix()
	}
	if !p.Provisioning.UpdatedAt.IsZero() {
		pb.ProvisioningUpdatedAtUnix = p.Provisioning.UpdatedAt.Unix()
	}
	if p.Permissions.FleetNodeRoles != nil {
		pb.AgentRoles = make(map[string]string, len(p.Permissions.FleetNodeRoles))
		for k, v := range p.Permissions.FleetNodeRoles {
			pb.AgentRoles[k] = string(v)
		}
	}
	return pb
}

// contextReposToProto converts the domain context repository list to proto.
// A nil list stays nil so an empty project carries no repeated field.
// retiredSigningKeysToProto renders a project's trusted signing history.
func retiredSigningKeysToProto(keys []orchestration.RetiredSigningKey) []*pb.RetiredSigningKey {
	if len(keys) == 0 {
		return nil
	}
	out := make([]*pb.RetiredSigningKey, 0, len(keys))
	for _, key := range keys {
		out = append(out, &pb.RetiredSigningKey{PublicKey: key.PublicKey, Name: key.Name, RetiredAtUnix: key.RetiredAt.Unix()})
	}
	return out
}

func contextReposToProto(repos []orchestration.ContextRepo) []*pb.ContextRepo {
	if repos == nil {
		return nil
	}
	out := make([]*pb.ContextRepo, 0, len(repos))
	for _, repo := range repos {
		out = append(out, &pb.ContextRepo{Name: repo.Name, RepoUrl: repo.RepoURL, Ref: repo.Ref})
	}
	return out
}

// contextReposFromProto converts the proto context repository list to the
// domain type, preserving nil so an update can tell "unset" from "empty".
func contextReposFromProto(repos []*pb.ContextRepo) []orchestration.ContextRepo {
	if repos == nil {
		return nil
	}
	out := make([]orchestration.ContextRepo, 0, len(repos))
	for _, repo := range repos {
		if repo == nil {
			continue
		}
		out = append(out, orchestration.ContextRepo{Name: repo.Name, RepoURL: repo.RepoUrl, Ref: repo.Ref})
	}
	return out
}

// convertProjectError maps service-layer project errors to gRPC status codes.
func convertProjectError(err error) error {
	if mapped, ok := referenceStatus(err); ok {
		return mapped
	}
	var notFound *service.ProjectNotFoundError
	var alreadyExists *service.ProjectAlreadyExistsError
	var bound *service.ProjectBoundError
	var invalid *service.ProjectInvalidError
	var accessDenied *service.ProjectAccessDeniedError
	var unresolved *service.ProjectUnresolvedError
	var roleInvalid *service.ProjectRoleInvalidError

	if isAgentCatalogError(err) {
		return agentCatalogStatusError(err)
	}
	switch {
	case stderrors.As(err, &accessDenied):
		return status.Error(codes.PermissionDenied, err.Error())
	case stderrors.As(err, &unresolved):
		return status.Error(codes.NotFound, err.Error())
	case stderrors.As(err, &roleInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case stderrors.As(err, &notFound):
		return status.Error(codes.NotFound, err.Error())
	case stderrors.As(err, &alreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case stderrors.As(err, &bound):
		// The project still has an SDLC binding: unbind first.
		return status.Error(codes.FailedPrecondition, err.Error())
	case stderrors.As(err, &invalid):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// Compile-time interface satisfaction check.
var _ pb.ProjectServiceServer = (*ProjectServer)(nil)

// intPtrFromInt32 narrows an optional proto int32 to the optional int the
// project record keeps, preserving absence. A quota of zero means UNLIMITED
// and a storage size of zero means unsized / the runtime's default, so absent
// and zero are different instructions and must stay different all the way to
// the service.
func intPtrFromInt32(v *int32) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}
