// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"strings"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// projectServicePrefix is the SDK service name for project lifecycle
// management.
const projectServicePrefix = "quicbaas.Project"

// isProjectService reports whether svc names the Project service.
func isProjectService(svc string) bool {
	return strings.HasPrefix(svc, projectServicePrefix)
}

// projectHandlerFunc converts an SDK request to proto, invokes gRPC, and
// converts the proto response back to the SDK type.
type projectHandlerFunc func(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error)

// projectMethodMap gives O(1) dispatch from SDK method name to handler.
// GrantAgentRole and RevokeAgentRole are dedicated RPCs rather than
// UpdateProject calls: UpdateProject.agent_roles replaces the whole role map,
// so emulating a single-agent grant there would drop every other agent's role.
var projectMethodMap = map[string]projectHandlerFunc{
	"Create":          handleProjectCreate,
	"Get":             handleProjectGet,
	"GetByName":       handleProjectGetByName,
	"Update":          handleProjectUpdate,
	"Delete":          handleProjectDelete,
	"List":            handleProjectList,
	"GrantAgentRole":  handleProjectGrantAgentRole,
	"RevokeAgentRole": handleProjectRevokeAgentRole,
}

// projectHandler adapts the SDK project surface to the generated
// ProjectService gRPC client. The Unix transport delegates to gRPC verbatim,
// so this handler also serves `--transport unix`.
type projectHandler struct {
	conn *grpc.ClientConn
}

// newProjectHandler creates a project gRPC handler bound to conn.
func newProjectHandler(conn *grpc.ClientConn) *projectHandler {
	return &projectHandler{conn: conn}
}

// handleRequest dispatches a project method.
func (h *projectHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	handler, ok := projectMethodMap[method]
	if !ok {
		return nil, errors.NewInvalidInput("grpc.project."+method, "unknown project method")
	}
	return handler(ctx, h.conn, req)
}

// =============================================================================
// Method handlers
// =============================================================================

func handleProjectCreate(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.project.Create"
	sdkReq, ok := req.(*project.CreateProjectRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewProjectServiceClient(conn).CreateProject(ctx, &pb.CreateProjectRequest{
		TenantId:         sdkReq.TenantID,
		Name:             sdkReq.Name,
		Description:      sdkReq.Description,
		WorkspaceAgentId: sdkReq.WorkspaceAgentID,
		WorkspacePath:    sdkReq.WorkspacePath,
		HostAffinity:     sdkReq.HostAffinity,
		DefaultLeadAgent: sdkReq.DefaultLeadAgent,
		GitRepoUrl:       sdkReq.GitRepoURL,
		GitDefaultBranch: sdkReq.GitDefaultBranch,
		TokenBudget:      sdkReq.TokenBudget,
		GpuMinutes:       sdkReq.GPUMinutes,
		MaxConcurrency:   int32(sdkReq.MaxConcurrency),
		Metadata:         sdkReq.Metadata,

		DefaultWorkflow:     sdkReq.DefaultWorkflow,
		Autonomous:          sdkReq.Autonomous,
		ContextRepos:        contextReposToProto(sdkReq.ContextRepos),
		GitAuthorName:       sdkReq.GitAuthorName,
		GitAuthorEmail:      sdkReq.GitAuthorEmail,
		GitSigningEnabled:   sdkReq.GitSigningEnabled,
		GitSigningAlgorithm: sdkReq.GitSigningAlgorithm,
		GitSigningKeyName:   sdkReq.GitSigningKeyName,
		GitSigningKeyScope:  sdkReq.GitSigningKeyScope,
		GitSigningKeyOwner:  sdkReq.GitSigningKeyOwner,
		GitSshKeyName:       sdkReq.GitSSHKeyName,
		GitSshKeyScope:      sdkReq.GitSSHKeyScope,
		GitSshKeyOwner:      sdkReq.GitSSHKeyOwner,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return projectInfoFromProto(resp.Project), nil
}

func handleProjectGet(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.project.Get"
	sdkReq, ok := req.(*project.GetProjectRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewProjectServiceClient(conn).GetProject(ctx, &pb.GetProjectRequest{
		TenantId:  sdkReq.TenantID,
		ProjectId: sdkReq.ProjectID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return projectInfoFromProto(resp.Project), nil
}

func handleProjectGetByName(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.project.GetByName"
	sdkReq, ok := req.(*project.GetProjectByNameRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewProjectServiceClient(conn).GetProjectByName(ctx, &pb.GetProjectByNameRequest{
		TenantId: sdkReq.TenantID,
		Name:     sdkReq.Name,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return projectInfoFromProto(resp.Project), nil
}

func handleProjectUpdate(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.project.Update"
	sdkReq, ok := req.(*project.UpdateProjectRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewProjectServiceClient(conn).UpdateProject(ctx, &pb.UpdateProjectRequest{
		TenantId:         sdkReq.TenantID,
		ProjectId:        sdkReq.ProjectID,
		Name:             sdkReq.Name,
		Description:      sdkReq.Description,
		ClearDescription: sdkReq.ClearDescription,
		WorkspaceAgentId: sdkReq.WorkspaceAgentID,
		WorkspacePath:    sdkReq.WorkspacePath,
		HostAffinity:     sdkReq.HostAffinity,
		DefaultLeadAgent: sdkReq.DefaultLeadAgent,
		GitRepoUrl:       sdkReq.GitRepoURL,
		GitDefaultBranch: sdkReq.GitDefaultBranch,
		GitCurrentSha:    sdkReq.GitCurrentSHA,
		AgentRoles:       projectRolesToProto(sdkReq.AgentRoles),
		TokenBudget:      sdkReq.TokenBudget,
		GpuMinutes:       sdkReq.GPUMinutes,
		MaxConcurrency:   int32PtrFromInt(sdkReq.MaxConcurrency),
		Metadata:         sdkReq.Metadata,

		DefaultWorkflow:      sdkReq.DefaultWorkflow,
		ClearDefaultWorkflow: sdkReq.ClearDefaultWorkflow,
		Autonomous:           sdkReq.Autonomous,
		ContextRepos:         contextReposToProto(sdkReq.ContextRepos),
		ClearContextRepos:    sdkReq.ClearContextRepos,
		GitAuthorName:        sdkReq.GitAuthorName,
		GitAuthorEmail:       sdkReq.GitAuthorEmail,
		GitSigningEnabled:    sdkReq.GitSigningEnabled,
		GitSigningAlgorithm:  sdkReq.GitSigningAlgorithm,
		GitSigningKeyName:    sdkReq.GitSigningKeyName,
		GitSigningKeyScope:   sdkReq.GitSigningKeyScope,
		GitSigningKeyOwner:   sdkReq.GitSigningKeyOwner,
		GitSshKeyName:        sdkReq.GitSSHKeyName,
		GitSshKeyScope:       sdkReq.GitSSHKeyScope,
		GitSshKeyOwner:       sdkReq.GitSSHKeyOwner,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return projectInfoFromProto(resp.Project), nil
}

func handleProjectDelete(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.project.Delete"
	sdkReq, ok := req.(*project.DeleteProjectRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewProjectServiceClient(conn).DeleteProject(ctx, &pb.DeleteProjectRequest{
		TenantId:  sdkReq.TenantID,
		ProjectId: sdkReq.ProjectID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &project.DeleteProjectResponse{Deleted: resp.Deleted}, nil
}

func handleProjectList(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.project.List"
	sdkReq, ok := req.(*project.ListProjectsRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewProjectServiceClient(conn).ListProjects(ctx, &pb.ListProjectsRequest{
		TenantId: sdkReq.TenantID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &project.ListProjectsResponse{
		Projects: make([]*project.ProjectInfo, 0, len(resp.Projects)),
		Total:    int(resp.Total),
	}
	for _, p := range resp.Projects {
		out.Projects = append(out.Projects, projectInfoFromProto(p))
	}
	return out, nil
}

// handleProjectGrantAgentRole sets one agent's role through the dedicated RPC,
// leaving the project's other role entries untouched.
func handleProjectGrantAgentRole(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.project.GrantAgentRole"
	sdkReq, ok := req.(*project.GrantAgentRoleRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewProjectServiceClient(conn).GrantAgentRole(ctx, &pb.GrantAgentRoleRequest{
		TenantId:  sdkReq.TenantID,
		ProjectId: sdkReq.ProjectID,
		AgentId:   sdkReq.AgentID,
		Role:      string(sdkReq.Role),
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &project.GrantAgentRoleResponse{
		ProjectID: resp.ProjectId,
		AgentID:   resp.AgentId,
		Role:      project.ProjectRole(resp.Role),
		Timestamp: timeFromUnix(resp.Timestamp),
	}, nil
}

// handleProjectRevokeAgentRole denies one agent through the dedicated RPC.
// See handleProjectGrantAgentRole.
func handleProjectRevokeAgentRole(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.project.RevokeAgentRole"
	sdkReq, ok := req.(*project.RevokeAgentRoleRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewProjectServiceClient(conn).RevokeAgentRole(ctx, &pb.RevokeAgentRoleRequest{
		TenantId:  sdkReq.TenantID,
		ProjectId: sdkReq.ProjectID,
		AgentId:   sdkReq.AgentID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &project.RevokeAgentRoleResponse{
		ProjectID: resp.ProjectId,
		AgentID:   resp.AgentId,
		Timestamp: timeFromUnix(resp.Timestamp),
	}, nil
}

// =============================================================================
// Converters
// =============================================================================

// projectRolesToProto flattens the typed SDK role map to the proto string map.
// A nil map stays nil so the server can distinguish "no change" from "clear".
func projectRolesToProto(roles map[string]project.ProjectRole) map[string]string {
	if roles == nil {
		return nil
	}
	out := make(map[string]string, len(roles))
	for agentID, role := range roles {
		out[agentID] = string(role)
	}
	return out
}

// projectRolesFromProto lifts the proto string map back to the typed SDK role
// map, preserving nil.
func projectRolesFromProto(roles map[string]string) map[string]project.ProjectRole {
	if roles == nil {
		return nil
	}
	out := make(map[string]project.ProjectRole, len(roles))
	for agentID, role := range roles {
		out[agentID] = project.ProjectRole(role)
	}
	return out
}

// contextReposToProto converts the SDK context repository list to proto,
// preserving nil so an update can tell "unchanged" from "empty".
func contextReposToProto(repos []project.ContextRepo) []*pb.ContextRepo {
	if repos == nil {
		return nil
	}
	out := make([]*pb.ContextRepo, 0, len(repos))
	for _, repo := range repos {
		out = append(out, &pb.ContextRepo{Name: repo.Name, RepoUrl: repo.RepoURL, Ref: repo.Ref})
	}
	return out
}

// contextReposFromProto lifts the proto context repository list back to the
// SDK type, preserving nil.
func contextReposFromProto(repos []*pb.ContextRepo) []project.ContextRepo {
	if repos == nil {
		return nil
	}
	out := make([]project.ContextRepo, 0, len(repos))
	for _, repo := range repos {
		if repo == nil {
			continue
		}
		out = append(out, project.ContextRepo{Name: repo.Name, RepoURL: repo.RepoUrl, Ref: repo.Ref})
	}
	return out
}

// projectInfoFromProto converts the flat proto record to the nested SDK type.
func projectInfoFromProto(p *pb.Project) *project.ProjectInfo {
	if p == nil {
		return nil
	}
	return &project.ProjectInfo{
		ID:          p.Id,
		TenantID:    p.TenantId,
		Name:        p.Name,
		Description: p.Description,
		Workspace: project.ProjectWorkspace{
			AgentID: p.WorkspaceAgentId,
			Path:    p.WorkspacePath,
		},
		HostAffinity:     p.HostAffinity,
		DefaultLeadAgent: p.DefaultLeadAgent,
		DefaultWorkflow:  p.DefaultWorkflow,
		Autonomous:       p.Autonomous,
		ContextRepos:     contextReposFromProto(p.ContextRepos),
		Git: project.ProjectGit{
			RepoURL:       p.GitRepoUrl,
			DefaultBranch: p.GitDefaultBranch,
			CurrentSHA:    p.GitCurrentSha,
			Author:        project.GitAuthor{Name: p.GitAuthorName, Email: p.GitAuthorEmail},
			Signing: project.CommitSigning{
				Enabled:   p.GitSigningEnabled,
				Algorithm: p.GitSigningAlgorithm,
				Key:       project.KMSKeyRef{Name: p.GitSigningKeyName, Scope: p.GitSigningKeyScope, Owner: p.GitSigningKeyOwner},
			},
			SSHKey: project.KMSKeyRef{Name: p.GitSshKeyName, Scope: p.GitSshKeyScope, Owner: p.GitSshKeyOwner},
		},
		Permissions: project.ProjectPermissions{
			AgentRoles: projectRolesFromProto(p.AgentRoles),
		},
		Quotas: project.ProjectQuotas{
			TokenBudget:    p.TokenBudget,
			GPUMinutes:     p.GpuMinutes,
			MaxConcurrency: int(p.MaxConcurrency),
		},
		Metadata:  p.Metadata,
		CreatedAt: timeFromUnix(p.CreatedAtUnix),
		UpdatedAt: timeFromUnix(p.UpdatedAtUnix),
	}
}

// int32PtrFromInt widens the optional concurrency cap for the wire,
// preserving absence: a quota of zero means UNLIMITED, so nil and zero are
// different instructions and must stay different across the transport.
func int32PtrFromInt(v *int) *int32 {
	if v == nil {
		return nil
	}
	n := int32(*v) //nolint:gosec // G115: a concurrency cap is small by construction
	return &n
}
