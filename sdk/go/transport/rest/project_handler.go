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

package rest

import (
	"context"
	"net/http"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// projectRESTHandler handles Project REST requests with proper response type conversion.
type projectRESTHandler struct {
	transport *Transport
}

// newProjectRESTHandler creates a new project REST handler.
func newProjectRESTHandler(t *Transport) *projectRESTHandler {
	return &projectRESTHandler{transport: t}
}

// isProjectService returns true when the service name identifies the Project service.
func isProjectService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Project"
}

// projectMethodHandler defines a handler function for project methods.
type projectMethodHandler func(ctx context.Context, req interface{}) (interface{}, error)

// handleRequest routes Project requests using map-based O(1) dispatch.
func (h *projectRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	handlers := map[string]projectMethodHandler{
		"Create":          h.handleCreate,
		"Get":             h.handleGet,
		"GetByName":       h.handleGetByName,
		"Update":          h.handleUpdate,
		"Delete":          h.handleDelete,
		"List":            h.handleList,
		"GrantAgentRole":  h.handleGrantAgentRole,
		"RevokeAgentRole": h.handleRevokeAgentRole,
	}

	handler, ok := handlers[method]
	if !ok {
		return h.transport.requestWithRetry(ctx, "quicbaas.Project", method, req)
	}

	return handler(ctx, req)
}

// handleCreate handles POST /api/v1/projects.
func (h *projectRESTHandler) handleCreate(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*project.CreateProjectRequest)
	if !ok {
		return nil, errors.NewInvalidInput("project.Create", "invalid request type")
	}
	var resp project.ProjectInfo
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/projects", sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleGet handles GET /api/v1/projects/{id}.
func (h *projectRESTHandler) handleGet(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*project.GetProjectRequest)
	if !ok {
		return nil, errors.NewInvalidInput("project.Get", "invalid request type")
	}
	url := "/api/v1/projects/" + sdkReq.ProjectID + "?tenant_id=" + sdkReq.TenantID
	var resp project.ProjectInfo
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleGetByName handles GET /api/v1/projects?name={name}&tenant_id={id}.
func (h *projectRESTHandler) handleGetByName(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*project.GetProjectByNameRequest)
	if !ok {
		return nil, errors.NewInvalidInput("project.GetByName", "invalid request type")
	}
	url := "/api/v1/projects?tenant_id=" + sdkReq.TenantID + "&name=" + sdkReq.Name
	var resp project.ProjectInfo
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleUpdate handles PUT /api/v1/projects/{id}.
func (h *projectRESTHandler) handleUpdate(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*project.UpdateProjectRequest)
	if !ok {
		return nil, errors.NewInvalidInput("project.Update", "invalid request type")
	}
	url := "/api/v1/projects/" + sdkReq.ProjectID + "?tenant_id=" + sdkReq.TenantID
	var resp project.ProjectInfo
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, url, sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleDelete handles DELETE /api/v1/projects/{id}.
func (h *projectRESTHandler) handleDelete(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*project.DeleteProjectRequest)
	if !ok {
		return nil, errors.NewInvalidInput("project.Delete", "invalid request type")
	}
	url := "/api/v1/projects/" + sdkReq.ProjectID + "?tenant_id=" + sdkReq.TenantID
	var resp project.DeleteProjectResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleList handles GET /api/v1/projects?tenant_id={id}.
func (h *projectRESTHandler) handleList(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*project.ListProjectsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("project.List", "invalid request type")
	}
	url := "/api/v1/projects?tenant_id=" + sdkReq.TenantID
	var resp project.ListProjectsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleGrantAgentRole handles PUT /api/v1/projects/{id}/permissions/{agent_id}.
func (h *projectRESTHandler) handleGrantAgentRole(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*project.GrantAgentRoleRequest)
	if !ok {
		return nil, errors.NewInvalidInput("project.GrantAgentRole", "invalid request type")
	}
	url := "/api/v1/projects/" + sdkReq.ProjectID + "/permissions/" + sdkReq.AgentID + "?tenant_id=" + sdkReq.TenantID
	body := struct {
		Role project.ProjectRole `json:"role"`
	}{Role: sdkReq.Role}
	var resp project.GrantAgentRoleResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, url, &body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleRevokeAgentRole handles DELETE /api/v1/projects/{id}/permissions/{agent_id}.
func (h *projectRESTHandler) handleRevokeAgentRole(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*project.RevokeAgentRoleRequest)
	if !ok {
		return nil, errors.NewInvalidInput("project.RevokeAgentRole", "invalid request type")
	}
	url := "/api/v1/projects/" + sdkReq.ProjectID + "/permissions/" + sdkReq.AgentID + "?tenant_id=" + sdkReq.TenantID
	var resp project.RevokeAgentRoleResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
