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
	"math"
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
// GrantFleetNodeRole and RevokeFleetNodeRole are dedicated RPCs rather than
// UpdateProject calls: UpdateProject.agent_roles replaces the whole role map,
// so emulating a single-agent grant there would drop every other agent's role.
var projectMethodMap = map[string]projectHandlerFunc{
	"Create":              handleProjectCreate,
	"Get":                 handleProjectGet,
	"GetByName":           handleProjectGetByName,
	"Update":              handleProjectUpdate,
	"Delete":              handleProjectDelete,
	"List":                handleProjectList,
	"GrantFleetNodeRole":  handleProjectGrantFleetNodeRole,
	"RevokeFleetNodeRole": handleProjectRevokeFleetNodeRole,
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
	tmpfsMB, err := int32ToProto(op, "workspace_tmpfs_mb", sdkReq.WorkspaceTmpfsMB)
	if err != nil {
		return nil, err
	}
	memoryMB, err := int32ToProto(op, "container_memory_mb", sdkReq.ContainerMemoryMB)
	if err != nil {
		return nil, err
	}
	resp, err := pb.NewProjectServiceClient(conn).CreateProject(ctx, &pb.CreateProjectRequest{
		TenantId:         sdkReq.TenantID,
		Name:             sdkReq.Name,
		Description:      sdkReq.Description,
		WorkspaceAgentId: sdkReq.WorkspaceFleetNodeID,
		WorkspacePath:    sdkReq.WorkspacePath,
		HostAffinity:     sdkReq.HostAffinity,
		DefaultLeadAgent: agentRefToProto(sdkReq.DefaultLeadAgent),
		GitRepoUrl:       sdkReq.GitRepoURL,
		GitDefaultBranch: sdkReq.GitDefaultBranch,
		Spend:            spendLimitsToProto(sdkReq.Spend),
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
		WorkspaceTier:       string(sdkReq.WorkspaceTier),
		WorkspaceTmpfsMb:    tmpfsMB,
		ContainerMemoryMb:   memoryMB,
		SessionRotation:     string(sdkReq.SessionRotation),

		WorkspaceManaged:       sdkReq.WorkspaceManaged,
		GitCredentialSecretRef: sdkReq.GitCredentialSecretRef,
		GitCredentialUsername:  sdkReq.GitCredentialUsername,
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
	tmpfsMB, err := optionalInt32ToProto(op, "workspace_tmpfs_mb", sdkReq.WorkspaceTmpfsMB)
	if err != nil {
		return nil, err
	}
	memoryMB, err := optionalInt32ToProto(op, "container_memory_mb", sdkReq.ContainerMemoryMB)
	if err != nil {
		return nil, err
	}
	maxConcurrency, err := optionalInt32ToProto(op, "max_concurrency", sdkReq.MaxConcurrency)
	if err != nil {
		return nil, err
	}
	resp, err := pb.NewProjectServiceClient(conn).UpdateProject(ctx, &pb.UpdateProjectRequest{
		TenantId:         sdkReq.TenantID,
		ProjectId:        sdkReq.ProjectID,
		Name:             sdkReq.Name,
		Description:      sdkReq.Description,
		ClearDescription: sdkReq.ClearDescription,
		WorkspaceAgentId: sdkReq.WorkspaceFleetNodeID,
		WorkspacePath:    sdkReq.WorkspacePath,
		HostAffinity:     sdkReq.HostAffinity,
		DefaultLeadAgent: agentRefToProto(sdkReq.DefaultLeadAgent),
		GitRepoUrl:       sdkReq.GitRepoURL,
		GitDefaultBranch: sdkReq.GitDefaultBranch,
		GitCurrentSha:    sdkReq.GitCurrentSHA,
		AgentRoles:       projectRolesToProto(sdkReq.FleetNodeRoles),
		Spend:            optionalSpendLimitsToProto(sdkReq.Spend),
		GpuMinutes:       sdkReq.GPUMinutes,
		MaxConcurrency:   maxConcurrency,
		Metadata:         sdkReq.Metadata,

		DefaultWorkflow:          sdkReq.DefaultWorkflow,
		ClearDefaultWorkflow:     sdkReq.ClearDefaultWorkflow,
		Autonomous:               sdkReq.Autonomous,
		Parked:                   sdkReq.Parked,
		ContextRepos:             contextReposToProto(sdkReq.ContextRepos),
		ClearContextRepos:        sdkReq.ClearContextRepos,
		GitAuthorName:            sdkReq.GitAuthorName,
		GitAuthorEmail:           sdkReq.GitAuthorEmail,
		GitSigningEnabled:        sdkReq.GitSigningEnabled,
		GitSigningAlgorithm:      sdkReq.GitSigningAlgorithm,
		GitSigningKeyName:        sdkReq.GitSigningKeyName,
		GitSigningKeyScope:       sdkReq.GitSigningKeyScope,
		GitSigningKeyOwner:       sdkReq.GitSigningKeyOwner,
		GitSshKeyName:            sdkReq.GitSSHKeyName,
		GitSshKeyScope:           sdkReq.GitSSHKeyScope,
		GitSshKeyOwner:           sdkReq.GitSSHKeyOwner,
		GitSigningRetireKeyName:  sdkReq.GitSigningRetireKeyName,
		GitSigningRetireKeyScope: sdkReq.GitSigningRetireKeyScope,
		GitSigningRetireKeyOwner: sdkReq.GitSigningRetireKeyOwner,
		GitSigningRetiredAt:      sdkReq.GitSigningRetiredAt,
		WorkspaceTier:            string(sdkReq.WorkspaceTier),
		WorkspaceTmpfsMb:         tmpfsMB,
		ContainerMemoryMb:        memoryMB,
		SessionRotation:          string(sdkReq.SessionRotation),
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
	paging := pageFieldsFromParams(sdkReq.Params)
	resp, err := pb.NewProjectServiceClient(conn).ListProjects(ctx, &pb.ListProjectsRequest{
		TenantId: sdkReq.TenantID,
		Limit:    paging.limit,
		Offset:   paging.offset,
		Sort:     paging.sort,
		Q:        paging.q,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &project.ListProjectsResponse{
		Projects: make([]*project.ProjectInfo, 0, len(resp.Projects)),
		Envelope: pageEnvelopeFromProto(resp.GetTotal(), resp.GetLimit(), resp.GetOffset(), resp.GetNextOffset()),
	}
	for _, p := range resp.Projects {
		out.Projects = append(out.Projects, projectInfoFromProto(p))
	}
	return out, nil
}

// handleProjectGrantFleetNodeRole sets one agent's role through the dedicated RPC,
// leaving the project's other role entries untouched.
func handleProjectGrantFleetNodeRole(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.project.GrantFleetNodeRole"
	sdkReq, ok := req.(*project.GrantFleetNodeRoleRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewProjectServiceClient(conn).GrantFleetNodeRole(ctx, &pb.GrantFleetNodeRoleRequest{
		TenantId:  sdkReq.TenantID,
		ProjectId: sdkReq.ProjectID,
		NodeId:    sdkReq.FleetNodeID,
		Role:      string(sdkReq.Role),
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &project.GrantFleetNodeRoleResponse{
		ProjectID:   resp.ProjectId,
		FleetNodeID: resp.NodeId,
		Role:        project.ProjectRole(resp.Role),
		Timestamp:   timeFromUnix(resp.Timestamp),
	}, nil
}

// handleProjectRevokeFleetNodeRole denies one agent through the dedicated RPC.
// See handleProjectGrantFleetNodeRole.
func handleProjectRevokeFleetNodeRole(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.project.RevokeFleetNodeRole"
	sdkReq, ok := req.(*project.RevokeFleetNodeRoleRequest)
	if !ok {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewProjectServiceClient(conn).RevokeFleetNodeRole(ctx, &pb.RevokeFleetNodeRoleRequest{
		TenantId:  sdkReq.TenantID,
		ProjectId: sdkReq.ProjectID,
		NodeId:    sdkReq.FleetNodeID,
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &project.RevokeFleetNodeRoleResponse{
		ProjectID:   resp.ProjectId,
		FleetNodeID: resp.NodeId,
		Timestamp:   timeFromUnix(resp.Timestamp),
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
			FleetNodeID: p.WorkspaceAgentId,
			Path:        p.WorkspacePath,
		},
		Storage: project.ProjectStorage{
			WorkspaceTier:     project.WorkspaceTier(p.WorkspaceTier),
			WorkspaceTmpfsMB:  int(p.WorkspaceTmpfsMb),
			ContainerMemoryMB: int(p.ContainerMemoryMb),
		},
		SessionRotation:  project.SessionRotationPolicy(p.SessionRotation),
		HostAffinity:     p.HostAffinity,
		DefaultLeadAgent: agentRefFromProto(p.GetDefaultLeadAgent()),
		DefaultWorkflow:  p.DefaultWorkflow,
		Autonomous:       p.Autonomous,
		Parked:           p.Parked,
		ParkedAt:         timeFromUnix(p.ParkedAtUnix),
		ParkedBy:         p.ParkedBy,
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
				Retired:   retiredSigningKeysFromProto(p.GitSigningRetired),
			},
			SSHKey: project.KMSKeyRef{Name: p.GitSshKeyName, Scope: p.GitSshKeyScope, Owner: p.GitSshKeyOwner},
		},
		Provisioning: project.ProjectProvisioning{
			Managed:   p.WorkspaceManaged,
			State:     project.ProvisioningState(p.ProvisioningState),
			Error:     p.ProvisioningError,
			UpdatedAt: timeFromUnix(p.ProvisioningUpdatedAtUnix),
		},
		Permissions: project.ProjectPermissions{
			FleetNodeRoles: projectRolesFromProto(p.AgentRoles),
		},
		Quotas: project.ProjectQuotas{
			GPUMinutes:     p.GpuMinutes,
			MaxConcurrency: int(p.MaxConcurrency),
		},
		Spend:     spendLimitsFromProto(p.GetSpend()),
		Metadata:  p.Metadata,
		CreatedAt: timeFromUnix(p.CreatedAtUnix),
		UpdatedAt: timeFromUnix(p.UpdatedAtUnix),
	}
}

// int32ToProto narrows a request field to the int32 the wire carries. A value
// outside that range is refused rather than truncated: a wrapped value could
// land on a plausible limit the operator never asked for. A negative value
// passes through so the server's validation refuses it with the same error
// every transport returns.
func int32ToProto(op, field string, v int) (int32, error) {
	if v > math.MaxInt32 || v < math.MinInt32 {
		return 0, errors.NewInvalidInput(op, field+" is out of range")
	}
	return int32(v), nil
}

// optionalInt32ToProto is int32ToProto preserving absence. Zero is an
// instruction in these fields (an unlimited concurrency cap, an unsized
// storage limit), so nil ("leave it alone") and zero must stay different
// across the transport.
func optionalInt32ToProto(op, field string, v *int) (*int32, error) {
	if v == nil {
		return nil, nil
	}
	n, err := int32ToProto(op, field, *v)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// retiredSigningKeysFromProto reads a project's trusted signing history.
func retiredSigningKeysFromProto(keys []*pb.RetiredSigningKey) []project.RetiredSigningKey {
	if len(keys) == 0 {
		return nil
	}
	out := make([]project.RetiredSigningKey, 0, len(keys))
	for _, key := range keys {
		out = append(out, project.RetiredSigningKey{
			PublicKey: key.GetPublicKey(), Name: key.GetName(), RetiredAt: timeFromUnix(key.GetRetiredAtUnix()),
		})
	}
	return out
}
