//go:build quic

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

package quic

import (
	"context"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Project QUIC surface.
//
// Wire layout (identical to the orchestrator and account sub-dispatch this
// shares an opcode with):
//
//	[opcode=OpOrchestrator][tokenLen][token][payloadLen][subOp:1][json body]
//
// The request and response bodies below are the canonical snake_case project
// wire shape — the same field names REST serves and the SDK project client
// tags. They are declared here rather than reusing the pkg/service request and
// response types because those carry no JSON tags, which would put Go field
// names on the wire and diverge from every other transport.
// =============================================================================

// projectProvider is the narrow view of the composed service this handler
// needs. Mirrors accountProvider in handler_account.go.
type projectProvider interface {
	Project() service.ProjectService
}

// getProjectService retrieves the ProjectService from the handler's service.
// Returns nil when the service does not expose one.
func (h *Handler) getProjectService() service.ProjectService {
	provider, ok := h.service.(projectProvider)
	if !ok {
		return nil
	}
	return provider.Project()
}

// =============================================================================
// Wire Types
// =============================================================================

// projectCreateWire is the project_create request body.
type projectCreateWire struct {
	TenantID         string            `json:"tenant_id"`
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	WorkspaceAgentID string            `json:"workspace_agent_id,omitempty"`
	WorkspacePath    string            `json:"workspace_path,omitempty"`
	HostAffinity     string            `json:"host_affinity,omitempty"`
	DefaultLeadAgent string            `json:"default_lead_agent,omitempty"`
	GitRepoURL       string            `json:"git_repo_url,omitempty"`
	GitDefaultBranch string            `json:"git_default_branch,omitempty"`
	TokenBudget      int64             `json:"token_budget,omitempty"`
	GPUMinutes       int64             `json:"gpu_minutes,omitempty"`
	MaxConcurrency   int               `json:"max_concurrency,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// projectUpdateWire is the project update request body.
type projectUpdateWire struct {
	TenantID         string                               `json:"tenant_id"`
	ProjectID        string                               `json:"project_id"`
	Name             string                               `json:"name,omitempty"`
	Description      string                               `json:"description,omitempty"`
	ClearDescription bool                                 `json:"clear_description,omitempty"`
	WorkspaceAgentID string                               `json:"workspace_agent_id,omitempty"`
	WorkspacePath    string                               `json:"workspace_path,omitempty"`
	HostAffinity     string                               `json:"host_affinity,omitempty"`
	DefaultLeadAgent string                               `json:"default_lead_agent,omitempty"`
	GitRepoURL       string                               `json:"git_repo_url,omitempty"`
	GitDefaultBranch string                               `json:"git_default_branch,omitempty"`
	GitCurrentSHA    string                               `json:"git_current_sha,omitempty"`
	AgentRoles       map[string]orchestration.ProjectRole `json:"agent_roles,omitempty"`
	TokenBudget      int64                                `json:"token_budget,omitempty"`
	GPUMinutes       int64                                `json:"gpu_minutes,omitempty"`
	MaxConcurrency   int                                  `json:"max_concurrency,omitempty"`
	Metadata         map[string]string                    `json:"metadata,omitempty"`
}

// projectIDWire identifies a project by ID. Shared by get and delete.
type projectIDWire struct {
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id"`
}

// projectNameWire identifies a project by name within a tenant.
type projectNameWire struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
}

// projectListWire selects the tenant whose projects to list.
type projectListWire struct {
	TenantID string `json:"tenant_id"`
}

// projectGrantWire is the grant-agent-role request body.
type projectGrantWire struct {
	TenantID  string                    `json:"tenant_id"`
	ProjectID string                    `json:"project_id"`
	AgentID   string                    `json:"agent_id"`
	Role      orchestration.ProjectRole `json:"role"`
}

// projectRevokeWire is the revoke-agent-role request body.
type projectRevokeWire struct {
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id"`
	AgentID   string `json:"agent_id"`
}

// projectEnvelope wraps a single project. orchestration.Project is tagged
// snake_case at source, so it is the canonical body.
type projectEnvelope struct {
	Project   *orchestration.Project `json:"project"`
	Timestamp time.Time              `json:"timestamp"`
}

// projectListEnvelope wraps a page of projects.
type projectListEnvelope struct {
	Projects  []*orchestration.Project `json:"projects"`
	Total     int                      `json:"total"`
	Timestamp time.Time                `json:"timestamp"`
}

// projectDeleteEnvelope confirms a deletion.
type projectDeleteEnvelope struct {
	ProjectID string    `json:"project_id"`
	Deleted   bool      `json:"deleted"`
	Timestamp time.Time `json:"timestamp"`
}

// projectAgentRoleEnvelope confirms a role grant or revocation. Role is empty
// on a revocation, where the service unconditionally assigns "denied".
type projectAgentRoleEnvelope struct {
	ProjectID string                    `json:"project_id"`
	AgentID   string                    `json:"agent_id"`
	Role      orchestration.ProjectRole `json:"role,omitempty"`
	Timestamp time.Time                 `json:"timestamp"`
}

// =============================================================================
// Dispatch
// =============================================================================

// projectSubHandler handles one project sub-operation.
type projectSubHandler func(h *Handler, ctx context.Context, req *Request) *Response

// projectSubHandlers is the O(1) sub-opcode dispatch table for the project
// range carried on OpOrchestrator.
var projectSubHandlers = map[byte]projectSubHandler{
	SubOpProjectCreate:          (*Handler).handleProjectCreate,
	SubOpProjectGet:             (*Handler).handleProjectGet,
	SubOpProjectGetByName:       (*Handler).handleProjectGetByName,
	SubOpProjectList:            (*Handler).handleProjectList,
	SubOpProjectUpdate:          (*Handler).handleProjectUpdate,
	SubOpProjectDelete:          (*Handler).handleProjectDelete,
	SubOpProjectGrantAgentRole:  (*Handler).handleProjectGrantAgentRole,
	SubOpProjectRevokeAgentRole: (*Handler).handleProjectRevokeAgentRole,
}

// handleProjectDispatch routes one project sub-operation. Called by
// handleOrchestratorDispatch for sub-opcodes in the project range.
func (h *Handler) handleProjectDispatch(ctx context.Context, subOpcode byte, req *Request) *Response {
	handler, ok := projectSubHandlers[subOpcode]
	if !ok {
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleProjectDispatch", "project", subOpcode))
	}
	return handler(h, ctx, req)
}

// =============================================================================
// Sub-operation handlers
// =============================================================================

func (h *Handler) handleProjectCreate(ctx context.Context, req *Request) *Response {
	projectSvc := h.getProjectService()
	if projectSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Project"))
	}
	var wire projectCreateWire
	if err := DecodePayload(req.Payload, &wire); err != nil {
		return h.errorResponse(err)
	}
	resp, err := projectSvc.CreateProject(ctx, &service.CreateProjectRequest{
		TenantID:         wire.TenantID,
		Name:             wire.Name,
		Description:      wire.Description,
		WorkspaceAgentID: wire.WorkspaceAgentID,
		WorkspacePath:    wire.WorkspacePath,
		HostAffinity:     wire.HostAffinity,
		DefaultLeadAgent: wire.DefaultLeadAgent,
		GitRepoURL:       wire.GitRepoURL,
		GitDefaultBranch: wire.GitDefaultBranch,
		TokenBudget:      wire.TokenBudget,
		GPUMinutes:       wire.GPUMinutes,
		MaxConcurrency:   wire.MaxConcurrency,
		Metadata:         wire.Metadata,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	if resp == nil {
		return h.errorResponse(api.NewNoResponseError("project.Create"))
	}
	return h.projectResponse(&projectEnvelope{Project: resp.Project, Timestamp: resp.Timestamp})
}

func (h *Handler) handleProjectGet(ctx context.Context, req *Request) *Response {
	projectSvc := h.getProjectService()
	if projectSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Project"))
	}
	var wire projectIDWire
	if err := DecodePayload(req.Payload, &wire); err != nil {
		return h.errorResponse(err)
	}
	resp, err := projectSvc.GetProject(ctx, &service.GetProjectRequest{
		TenantID:  wire.TenantID,
		ProjectID: wire.ProjectID,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	if resp == nil {
		return h.errorResponse(api.NewNoResponseError("project.Get"))
	}
	return h.projectResponse(&projectEnvelope{Project: resp.Project, Timestamp: resp.Timestamp})
}

func (h *Handler) handleProjectGetByName(ctx context.Context, req *Request) *Response {
	projectSvc := h.getProjectService()
	if projectSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Project"))
	}
	var wire projectNameWire
	if err := DecodePayload(req.Payload, &wire); err != nil {
		return h.errorResponse(err)
	}
	resp, err := projectSvc.GetProjectByName(ctx, &service.GetProjectByNameRequest{
		TenantID: wire.TenantID,
		Name:     wire.Name,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	if resp == nil {
		return h.errorResponse(api.NewNoResponseError("project.GetByName"))
	}
	return h.projectResponse(&projectEnvelope{Project: resp.Project, Timestamp: resp.Timestamp})
}

func (h *Handler) handleProjectList(ctx context.Context, req *Request) *Response {
	projectSvc := h.getProjectService()
	if projectSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Project"))
	}
	var wire projectListWire
	if err := DecodePayload(req.Payload, &wire); err != nil {
		return h.errorResponse(err)
	}
	resp, err := projectSvc.ListProjects(ctx, &service.ListProjectsRequest{TenantID: wire.TenantID})
	if err != nil {
		return h.errorResponse(err)
	}
	if resp == nil {
		return h.errorResponse(api.NewNoResponseError("project.List"))
	}
	projects := resp.Projects
	if projects == nil {
		projects = []*orchestration.Project{}
	}
	return h.projectResponse(&projectListEnvelope{
		Projects:  projects,
		Total:     resp.Total,
		Timestamp: resp.Timestamp,
	})
}

func (h *Handler) handleProjectUpdate(ctx context.Context, req *Request) *Response {
	projectSvc := h.getProjectService()
	if projectSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Project"))
	}
	var wire projectUpdateWire
	if err := DecodePayload(req.Payload, &wire); err != nil {
		return h.errorResponse(err)
	}
	resp, err := projectSvc.UpdateProject(ctx, &service.UpdateProjectRequest{
		TenantID:         wire.TenantID,
		ProjectID:        wire.ProjectID,
		Name:             wire.Name,
		Description:      wire.Description,
		ClearDescription: wire.ClearDescription,
		WorkspaceAgentID: wire.WorkspaceAgentID,
		WorkspacePath:    wire.WorkspacePath,
		HostAffinity:     wire.HostAffinity,
		DefaultLeadAgent: wire.DefaultLeadAgent,
		GitRepoURL:       wire.GitRepoURL,
		GitDefaultBranch: wire.GitDefaultBranch,
		GitCurrentSHA:    wire.GitCurrentSHA,
		AgentRoles:       wire.AgentRoles,
		TokenBudget:      wire.TokenBudget,
		GPUMinutes:       wire.GPUMinutes,
		MaxConcurrency:   wire.MaxConcurrency,
		Metadata:         wire.Metadata,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	if resp == nil {
		return h.errorResponse(api.NewNoResponseError("project.Update"))
	}
	return h.projectResponse(&projectEnvelope{Project: resp.Project, Timestamp: resp.Timestamp})
}

func (h *Handler) handleProjectDelete(ctx context.Context, req *Request) *Response {
	projectSvc := h.getProjectService()
	if projectSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Project"))
	}
	var wire projectIDWire
	if err := DecodePayload(req.Payload, &wire); err != nil {
		return h.errorResponse(err)
	}
	resp, err := projectSvc.DeleteProject(ctx, &service.DeleteProjectRequest{
		TenantID:  wire.TenantID,
		ProjectID: wire.ProjectID,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	if resp == nil {
		return h.errorResponse(api.NewNoResponseError("project.Delete"))
	}
	return h.projectResponse(&projectDeleteEnvelope{
		ProjectID: resp.ProjectID,
		Deleted:   resp.Deleted,
		Timestamp: resp.Timestamp,
	})
}

func (h *Handler) handleProjectGrantAgentRole(ctx context.Context, req *Request) *Response {
	projectSvc := h.getProjectService()
	if projectSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Project"))
	}
	var wire projectGrantWire
	if err := DecodePayload(req.Payload, &wire); err != nil {
		return h.errorResponse(err)
	}
	resp, err := projectSvc.GrantAgentRole(ctx, &service.GrantAgentRoleRequest{
		TenantID:  wire.TenantID,
		ProjectID: wire.ProjectID,
		AgentID:   wire.AgentID,
		Role:      wire.Role,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	if resp == nil {
		return h.errorResponse(api.NewNoResponseError("project.GrantAgentRole"))
	}
	return h.projectResponse(&projectAgentRoleEnvelope{
		ProjectID: resp.ProjectID,
		AgentID:   resp.AgentID,
		Role:      resp.Role,
		Timestamp: resp.Timestamp,
	})
}

func (h *Handler) handleProjectRevokeAgentRole(ctx context.Context, req *Request) *Response {
	projectSvc := h.getProjectService()
	if projectSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Project"))
	}
	var wire projectRevokeWire
	if err := DecodePayload(req.Payload, &wire); err != nil {
		return h.errorResponse(err)
	}
	resp, err := projectSvc.RevokeAgentRole(ctx, &service.RevokeAgentRoleRequest{
		TenantID:  wire.TenantID,
		ProjectID: wire.ProjectID,
		AgentID:   wire.AgentID,
	})
	if err != nil {
		return h.errorResponse(err)
	}
	if resp == nil {
		return h.errorResponse(api.NewNoResponseError("project.RevokeAgentRole"))
	}
	return h.projectResponse(&projectAgentRoleEnvelope{
		ProjectID: resp.ProjectID,
		AgentID:   resp.AgentID,
		Timestamp: resp.Timestamp,
	})
}

// projectResponse encodes a project wire envelope into a wire response.
func (h *Handler) projectResponse(envelope interface{}) *Response {
	payload, err := EncodePayload(envelope)
	if err != nil {
		return h.errorResponse(err)
	}
	return &Response{Status: StatusOK, Payload: payload}
}
