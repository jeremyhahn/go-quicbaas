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

// Package rest — rbac_handler.go maps the quicbaas.RBAC client verbs onto the
// tenant-scoped IAM role routes the server serves:
//
//	CreateRole  POST   /api/v1/tenants/{tenant_id}/roles/
//	ListRoles   GET    /api/v1/tenants/{tenant_id}/roles/
//	DeleteRole  DELETE /api/v1/tenants/{tenant_id}/roles/{role_id}
//	AssignRole  POST   /api/v1/tenants/{tenant_id}/users/{user_id}/roles
//	RevokeRole  DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/roles/{role}
//
// CreateRole sends the role name as the role ID, so every verb that takes a
// role name addresses the role by that ID. The server keys users by a
// numeric ID, so the client's Username must be that number.

package rest

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// isRBACService reports whether the supplied service name resolves to the
// RBAC service.
func isRBACService(service string) bool {
	return trimRESTServicePrefix(service) == "RBAC"
}

// rbacRoleWire is the server's iam.Role JSON shape.
type rbacRoleWire struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions,omitempty"`
	Created     time.Time `json:"created"`
}

// rbacCreateRoleWire is the body of POST .../roles/.
type rbacCreateRoleWire struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions,omitempty"`
}

// rbacListRolesWire is the body GET .../roles/ returns.
type rbacListRolesWire struct {
	Roles []rbacRoleWire `json:"roles"`
}

// rbacAssignRoleWire is the body of POST .../users/{user_id}/roles.
type rbacAssignRoleWire struct {
	RoleID string `json:"role_id"`
}

// rbacRESTHandler sends RBAC verbs to the tenant-scoped IAM role routes.
type rbacRESTHandler struct {
	transport *Transport
}

// newRBACRESTHandler constructs a handler bound to t.
func newRBACRESTHandler(t *Transport) *rbacRESTHandler {
	return &rbacRESTHandler{transport: t}
}

// rbacMethods is the O(1) RBAC verb dispatch table.
var rbacMethods = map[string]func(*rbacRESTHandler, context.Context, interface{}) (interface{}, error){
	"CreateRole": (*rbacRESTHandler).createRole,
	"ListRoles":  (*rbacRESTHandler).listRoles,
	"DeleteRole": (*rbacRESTHandler).deleteRole,
	"AssignRole": (*rbacRESTHandler).assignRole,
	"RevokeRole": (*rbacRESTHandler).revokeRole,
}

// handleRequest dispatches one RBAC verb.
func (h *rbacRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	fn, ok := rbacMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("rest.rbac."+method, "unsupported RBAC method")
	}
	return fn(h, ctx, req)
}

// tenantPath returns /api/v1/tenants/{tenant_id} for the request's tenant:
// the context tenant first, then the transport's default tenant.
func (h *rbacRESTHandler) tenantPath(ctx context.Context, op string) (string, error) {
	tenantID := transport.TenantFromContext(ctx)
	if tenantID == "" {
		tenantID = h.transport.defaultTenant
	}
	if tenantID == "" {
		return "", errors.NewInvalidInput(op,
			"tenant is required; configure rest.WithDefaultTenant or use transport.ContextWithTenant")
	}
	return "/api/v1/tenants/" + url.PathEscape(tenantID), nil
}

// userRolesPath returns .../users/{user_id}/roles for a numeric username.
func (h *rbacRESTHandler) userRolesPath(ctx context.Context, op, username string) (string, error) {
	userID, err := strconv.ParseUint(username, 10, 64)
	if err != nil {
		return "", errors.NewInvalidInput(op, "username must be the numeric user ID")
	}
	base, err := h.tenantPath(ctx, op)
	if err != nil {
		return "", err
	}
	return base + "/users/" + strconv.FormatUint(userID, 10) + "/roles", nil
}

func (h *rbacRESTHandler) createRole(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "rest.rbac.CreateRole"
	r, ok := req.(*rbac.CreateRoleRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	base, err := h.tenantPath(ctx, op)
	if err != nil {
		return nil, err
	}
	body := &rbacCreateRoleWire{ID: r.RoleName, Name: r.RoleName, Permissions: r.Permissions}
	var wire rbacRoleWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, base+"/roles/", body, &wire); err != nil {
		return nil, err
	}
	return &rbac.CreateRoleResponse{RoleID: wire.ID, Timestamp: wire.Created.UnixNano()}, nil
}

func (h *rbacRESTHandler) listRoles(ctx context.Context, req interface{}) (interface{}, error) {
	base, err := h.tenantPath(ctx, "rest.rbac.ListRoles")
	if err != nil {
		return nil, err
	}
	endpoint := base + "/roles/"
	if r, ok := req.(*rbac.ListRolesRequest); ok && r != nil {
		query := url.Values{}
		if r.Limit > 0 {
			query.Set("limit", strconv.FormatUint(r.Limit, 10))
		}
		if r.Offset > 0 {
			query.Set("offset", strconv.FormatUint(r.Offset, 10))
		}
		if len(query) > 0 {
			endpoint += "?" + query.Encode()
		}
	}

	var wire rbacListRolesWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	roles := make([]*rbac.RoleInfo, 0, len(wire.Roles))
	for i := range wire.Roles {
		role := &wire.Roles[i]
		roles = append(roles, &rbac.RoleInfo{
			RoleID:      role.ID,
			RoleName:    role.Name,
			Permissions: role.Permissions,
			CreatedAt:   role.Created.UnixNano(),
		})
	}
	return &rbac.ListRolesResponse{Roles: roles, TotalCount: uint64(len(roles))}, nil
}

func (h *rbacRESTHandler) deleteRole(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "rest.rbac.DeleteRole"
	r, ok := req.(*rbac.DeleteRoleRequest)
	if !ok || r == nil || r.RoleName == "" {
		return nil, errors.NewInvalidInput(op, "role name is required")
	}
	base, err := h.tenantPath(ctx, op)
	if err != nil {
		return nil, err
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete,
		base+"/roles/"+url.PathEscape(r.RoleName), nil, nil); err != nil {
		return nil, err
	}
	return &rbac.DeleteRoleResponse{Success: true}, nil
}

func (h *rbacRESTHandler) assignRole(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "rest.rbac.AssignRole"
	r, ok := req.(*rbac.AssignRoleRequest)
	if !ok || r == nil || r.RoleName == "" {
		return nil, errors.NewInvalidInput(op, "role name is required")
	}
	path, err := h.userRolesPath(ctx, op, r.Username)
	if err != nil {
		return nil, err
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, path,
		&rbacAssignRoleWire{RoleID: r.RoleName}, nil); err != nil {
		return nil, err
	}
	return &rbac.AssignRoleResponse{Success: true}, nil
}

func (h *rbacRESTHandler) revokeRole(ctx context.Context, req interface{}) (interface{}, error) {
	const op = "rest.rbac.RevokeRole"
	r, ok := req.(*rbac.RevokeRoleRequest)
	if !ok || r == nil || r.RoleName == "" {
		return nil, errors.NewInvalidInput(op, "role name is required")
	}
	path, err := h.userRolesPath(ctx, op, r.Username)
	if err != nil {
		return nil, err
	}
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete,
		path+"/"+url.PathEscape(r.RoleName), nil, nil); err != nil {
		return nil, err
	}
	return &rbac.RevokeRoleResponse{Success: true}, nil
}
