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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// IAM service prefixes for routing.
const (
	iamUserServicePrefix       = "quicbaas.IAM.User"
	iamTenantServicePrefix     = "quicbaas.IAM.Tenant"
	iamGroupServicePrefix      = "quicbaas.IAM.Group"
	iamRoleServicePrefix       = "quicbaas.IAM.Role"
	iamPolicyServicePrefix     = "quicbaas.IAM.Policy"
	iamAppServicePrefix        = "quicbaas.IAM.App"
	iamSessionServicePrefix    = "quicbaas.IAM.Session"
	iamAuditServicePrefix      = "quicbaas.IAM.Audit"
	iamAdminServicePrefix      = "quicbaas.IAM.Admin"
	iamGDPRServicePrefix       = "quicbaas.IAM.GDPR"
	iamPEPServicePrefix        = "quicbaas.IAM.PEP"
	iamMFAServicePrefix        = "quicbaas.IAM.MFA"
	iamFederationServicePrefix = "quicbaas.IAM.Federation"
	iamIdentityServicePrefix   = "quicbaas.IAM.Identity"
)

// isIAMService checks if the service name is an IAM service.
func isIAMService(service string) bool {
	return strings.HasPrefix(service, "quicbaas.IAM.")
}

// iamRESTHandler handles IAM service requests over REST.
type iamRESTHandler struct {
	transport *Transport
}

// newIAMRESTHandler creates a new IAM REST handler.
func newIAMRESTHandler(t *Transport) *iamRESTHandler {
	return &iamRESTHandler{transport: t}
}

// handleRequest routes IAM requests to the appropriate handler.
func (h *iamRESTHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	switch {
	case strings.HasPrefix(service, iamUserServicePrefix):
		return h.handleUserRequest(ctx, method, req)
	case strings.HasPrefix(service, iamTenantServicePrefix):
		return h.handleTenantRequest(ctx, method, req)
	case strings.HasPrefix(service, iamGroupServicePrefix):
		return h.handleGroupRequest(ctx, method, req)
	case strings.HasPrefix(service, iamRoleServicePrefix):
		return h.handleRoleRequest(ctx, method, req)
	case strings.HasPrefix(service, iamPolicyServicePrefix):
		return h.handlePolicyRequest(ctx, method, req)
	case strings.HasPrefix(service, iamAppServicePrefix):
		return h.handleAppRequest(ctx, method, req)
	case strings.HasPrefix(service, iamSessionServicePrefix):
		return h.handleSessionRequest(ctx, method, req)
	case strings.HasPrefix(service, iamAuditServicePrefix):
		return h.handleAuditRequest(ctx, method, req)
	case strings.HasPrefix(service, iamAdminServicePrefix):
		return h.handleAdminRequest(ctx, method, req)
	case strings.HasPrefix(service, iamGDPRServicePrefix):
		return h.handleGDPRRequest(ctx, method, req)
	case strings.HasPrefix(service, iamPEPServicePrefix):
		return h.handlePEPRequest(ctx, method, req)
	case strings.HasPrefix(service, iamMFAServicePrefix):
		return h.handleMFARequest(ctx, method, req)
	case strings.HasPrefix(service, iamFederationServicePrefix):
		return h.handleFederationRequest(ctx, method, req)
	case strings.HasPrefix(service, iamIdentityServicePrefix):
		return h.handleIdentityRequest(ctx, method, req)
	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleRequest",
			"unsupported IAM service: "+service)
	}
}

// handleUserRequest handles User service requests.
func (h *iamRESTHandler) handleUserRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "iamRESTHandler.handleUserRequest", "invalid request")
	}

	tenantID := getString(reqMap, "tenant_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleUserRequest", "tenant_id is required")
	}

	switch method {
	case "Create":
		// POST /api/v1/tenants/{tenant_id}/users
		innerReq := getMap(reqMap, "request")
		// Map SDK field names to REST API field names
		body := mapUserCreateRequest(innerReq)
		return h.doRequest(ctx, http.MethodPost, "/api/v1/tenants/"+tenantID+"/users", body)

	case "Get":
		// GET /api/v1/tenants/{tenant_id}/users/{user_id}
		userID := getString(reqMap, "user_id")
		if userID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleUserRequest", "user_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/users/"+userID, nil)

	case "GetByEmail":
		// GET /api/v1/tenants/{tenant_id}/users?email={email}
		email := getString(reqMap, "email")
		if email == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleUserRequest", "email is required")
		}
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/users?email="+email, nil)

	case "FindByUsername":
		// GET /api/v1/tenants/{tenant_id}/users?username={username}
		username := getString(reqMap, "username")
		if username == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleUserRequest", "username is required")
		}
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/users?username="+username, nil)

	case "Update":
		// PUT /api/v1/tenants/{tenant_id}/users/{user_id}
		userID := getString(reqMap, "user_id")
		if userID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleUserRequest", "user_id is required")
		}
		innerReq := getMap(reqMap, "request")
		body := mapUserUpdateRequest(innerReq)
		return h.doRequest(ctx, http.MethodPut, "/api/v1/tenants/"+tenantID+"/users/"+userID, body)

	case "Delete":
		// DELETE /api/v1/tenants/{tenant_id}/users/{user_id}
		userID := getString(reqMap, "user_id")
		if userID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleUserRequest", "user_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, "/api/v1/tenants/"+tenantID+"/users/"+userID, nil)

	case "List":
		// GET /api/v1/tenants/{tenant_id}/users
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/users", nil)

	case "AssignRole":
		// POST /api/v1/tenants/{tenant_id}/users/{user_id}/roles
		userID := getString(reqMap, "user_id")
		roleID := getString(reqMap, "role_id")
		if userID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleUserRequest", "user_id is required")
		}
		body := map[string]interface{}{"role_id": roleID}
		return h.doRequest(ctx, http.MethodPost, "/api/v1/tenants/"+tenantID+"/users/"+userID+"/roles", body)

	case "RevokeRole":
		// DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/roles/{role}
		userID := getString(reqMap, "user_id")
		roleID := getString(reqMap, "role_id")
		if userID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleUserRequest", "user_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, "/api/v1/tenants/"+tenantID+"/users/"+userID+"/roles/"+roleID, nil)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleUserRequest", "unsupported method: "+method)
	}
}

// handleTenantRequest handles Tenant service requests.
func (h *iamRESTHandler) handleTenantRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "iamRESTHandler.handleTenantRequest", "invalid request")
	}

	switch method {
	case "Create":
		// POST /api/v1/tenants
		innerReq := getMap(reqMap, "request")
		if innerReq == nil {
			innerReq = reqMap
		}
		body := mapTenantCreateRequest(innerReq)
		return h.doRequest(ctx, http.MethodPost, "/api/v1/tenants", body)

	case "Get":
		// GET /api/v1/tenants/{tenant_id}
		tenantID := getString(reqMap, "tenant_id")
		if tenantID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleTenantRequest", "tenant_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID, nil)

	case "Update":
		// PUT /api/v1/tenants/{tenant_id}
		tenantID := getString(reqMap, "tenant_id")
		if tenantID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleTenantRequest", "tenant_id is required")
		}
		innerReq := getMap(reqMap, "request")
		if innerReq == nil {
			innerReq = reqMap
		}
		body := mapTenantUpdateRequest(innerReq)
		return h.doRequest(ctx, http.MethodPut, "/api/v1/tenants/"+tenantID, body)

	case "Delete":
		// DELETE /api/v1/tenants/{tenant_id}
		tenantID := getString(reqMap, "tenant_id")
		if tenantID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleTenantRequest", "tenant_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, "/api/v1/tenants/"+tenantID, nil)

	case "List":
		// GET /api/v1/tenants
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants", nil)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleTenantRequest", "unsupported method: "+method)
	}
}

// handleGroupRequest handles Group service requests.
func (h *iamRESTHandler) handleGroupRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "iamRESTHandler.handleGroupRequest", "invalid request")
	}

	tenantID := getString(reqMap, "tenant_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleGroupRequest", "tenant_id is required")
	}

	switch method {
	case "Create":
		// POST /api/v1/tenants/{tenant_id}/groups
		innerReq := getMap(reqMap, "request")
		return h.doRequest(ctx, http.MethodPost, "/api/v1/tenants/"+tenantID+"/groups", innerReq)

	case "Get":
		// GET /api/v1/tenants/{tenant_id}/groups/{group_id}
		groupID := getString(reqMap, "group_id")
		if groupID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGroupRequest", "group_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/groups/"+groupID, nil)

	case "Update":
		// PUT /api/v1/tenants/{tenant_id}/groups/{group_id}
		groupID := getString(reqMap, "group_id")
		if groupID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGroupRequest", "group_id is required")
		}
		innerReq := getMap(reqMap, "request")
		return h.doRequest(ctx, http.MethodPut, "/api/v1/tenants/"+tenantID+"/groups/"+groupID, innerReq)

	case "Delete":
		// DELETE /api/v1/tenants/{tenant_id}/groups/{group_id}
		groupID := getString(reqMap, "group_id")
		if groupID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGroupRequest", "group_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, "/api/v1/tenants/"+tenantID+"/groups/"+groupID, nil)

	case "List":
		// GET /api/v1/tenants/{tenant_id}/groups
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/groups", nil)

	case "AddMember":
		// POST /api/v1/tenants/{tenant_id}/groups/{group_id}/members
		groupID := getString(reqMap, "group_id")
		userID := getString(reqMap, "user_id")
		if groupID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGroupRequest", "group_id is required")
		}
		body := map[string]interface{}{"user_id": userID}
		return h.doRequest(ctx, http.MethodPost, "/api/v1/tenants/"+tenantID+"/groups/"+groupID+"/members", body)

	case "RemoveMember":
		// DELETE /api/v1/tenants/{tenant_id}/groups/{group_id}/members/{user_id}
		groupID := getString(reqMap, "group_id")
		userID := getString(reqMap, "user_id")
		if groupID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGroupRequest", "group_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, "/api/v1/tenants/"+tenantID+"/groups/"+groupID+"/members/"+userID, nil)

	case "ListMembers":
		// GET /api/v1/tenants/{tenant_id}/groups/{group_id}/members
		groupID := getString(reqMap, "group_id")
		if groupID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGroupRequest", "group_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/groups/"+groupID+"/members", nil)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleGroupRequest", "unsupported method: "+method)
	}
}

// handleRoleRequest handles Role service requests.
func (h *iamRESTHandler) handleRoleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "iamRESTHandler.handleRoleRequest", "invalid request")
	}

	tenantID := getString(reqMap, "tenant_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleRoleRequest", "tenant_id is required")
	}

	switch method {
	case "Create":
		// POST /api/v1/tenants/{tenant_id}/roles
		innerReq := getMap(reqMap, "request")
		return h.doRequest(ctx, http.MethodPost, "/api/v1/tenants/"+tenantID+"/roles", innerReq)

	case "Get":
		// GET /api/v1/tenants/{tenant_id}/roles/{role_id}
		roleID := getString(reqMap, "role_id")
		if roleID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleRoleRequest", "role_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/roles/"+roleID, nil)

	case "Update":
		// PUT /api/v1/tenants/{tenant_id}/roles/{role_id}
		roleID := getString(reqMap, "role_id")
		if roleID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleRoleRequest", "role_id is required")
		}
		innerReq := getMap(reqMap, "request")
		return h.doRequest(ctx, http.MethodPut, "/api/v1/tenants/"+tenantID+"/roles/"+roleID, innerReq)

	case "Delete":
		// DELETE /api/v1/tenants/{tenant_id}/roles/{role_id}
		roleID := getString(reqMap, "role_id")
		if roleID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleRoleRequest", "role_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, "/api/v1/tenants/"+tenantID+"/roles/"+roleID, nil)

	case "List":
		// GET /api/v1/tenants/{tenant_id}/roles
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/roles", nil)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleRoleRequest", "unsupported method: "+method)
	}
}

// handlePolicyRequest handles Policy service requests.
func (h *iamRESTHandler) handlePolicyRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "iamRESTHandler.handlePolicyRequest", "invalid request")
	}

	tenantID := getString(reqMap, "tenant_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handlePolicyRequest", "tenant_id is required")
	}

	switch method {
	case "Create":
		// POST /api/v1/tenants/{tenant_id}/policies
		innerReq := getMap(reqMap, "request")
		if innerReq == nil {
			innerReq = reqMap
		}
		resp, createErr := h.doRequest(ctx, http.MethodPost, "/api/v1/tenants/"+tenantID+"/policies", innerReq)
		if createErr != nil {
			return nil, createErr
		}
		return mapServerPolicyToSDK(resp), nil

	case "Get":
		// GET /api/v1/tenants/{tenant_id}/policies/{policy_name}
		policyName := getString(reqMap, "policy_name")
		if policyName == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handlePolicyRequest", "policy_name is required")
		}
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/policies/"+policyName, nil)

	case "Update":
		// PUT /api/v1/tenants/{tenant_id}/policies/{policy_name}
		policyName := getString(reqMap, "policy_name")
		if policyName == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handlePolicyRequest", "policy_name is required")
		}
		innerReq := getMap(reqMap, "request")
		if innerReq == nil {
			innerReq = reqMap
		}
		return h.doRequest(ctx, http.MethodPut, "/api/v1/tenants/"+tenantID+"/policies/"+policyName, innerReq)

	case "Delete":
		// DELETE /api/v1/tenants/{tenant_id}/policies/{policy_name}
		policyName := getString(reqMap, "policy_name")
		if policyName == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handlePolicyRequest", "policy_name is required")
		}
		return h.doRequest(ctx, http.MethodDelete, "/api/v1/tenants/"+tenantID+"/policies/"+policyName, nil)

	case "List":
		// GET /api/v1/tenants/{tenant_id}/policies
		resp, listErr := h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/policies", nil)
		if listErr != nil {
			return nil, listErr
		}
		return mapServerPolicyListToSDK(resp), nil

	case "Validate":
		// POST /api/v1/tenants/{tenant_id}/policies/validate
		// Extract policy from wrapper - the SDK sends {tenant_id, policy: {...}}
		innerReq := getMap(reqMap, "policy")
		if innerReq == nil {
			innerReq = getMap(reqMap, "request")
		}
		if innerReq == nil {
			innerReq = reqMap
		}
		return h.doRequest(ctx, http.MethodPost, "/api/v1/tenants/"+tenantID+"/policies/validate", innerReq)

	case "Test", "Simulate":
		// POST /api/v1/tenants/{tenant_id}/policies/test
		innerReq := getMap(reqMap, "request")
		if innerReq == nil {
			innerReq = reqMap
		}
		// Remove tenant_id from body since it's in the URL path
		delete(innerReq, "tenant_id")
		return h.doRequest(ctx, http.MethodPost, "/api/v1/tenants/"+tenantID+"/policies/test", innerReq)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handlePolicyRequest", "unsupported method: "+method)
	}
}

// handleAppRequest handles Application service requests.
func (h *iamRESTHandler) handleAppRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "iamRESTHandler.handleAppRequest", "invalid request")
	}

	tenantID := getString(reqMap, "tenant_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleAppRequest", "tenant_id is required")
	}

	switch method {
	case "Register":
		// POST /api/v1/tenants/{tenant_id}/apps
		innerReq := getMap(reqMap, "app")
		body := mapAppRegisterRequest(innerReq)
		return h.doRequest(ctx, http.MethodPost, "/api/v1/tenants/"+tenantID+"/apps", body)

	case "Get":
		// GET /api/v1/tenants/{tenant_id}/apps/{client_id}
		clientID := getString(reqMap, "client_id")
		if clientID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleAppRequest", "client_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/apps/"+clientID, nil)

	case "Update":
		// PUT /api/v1/tenants/{tenant_id}/apps/{client_id}
		clientID := getString(reqMap, "client_id")
		if clientID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleAppRequest", "client_id is required")
		}
		innerReq := getMap(reqMap, "app")
		body := mapAppRegisterRequest(innerReq)
		return h.doRequest(ctx, http.MethodPut, "/api/v1/tenants/"+tenantID+"/apps/"+clientID, body)

	case "Delete":
		// DELETE /api/v1/tenants/{tenant_id}/apps/{client_id}
		clientID := getString(reqMap, "client_id")
		if clientID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleAppRequest", "client_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, "/api/v1/tenants/"+tenantID+"/apps/"+clientID, nil)

	case "List":
		// GET /api/v1/tenants/{tenant_id}/apps
		return h.doRequest(ctx, http.MethodGet, "/api/v1/tenants/"+tenantID+"/apps", nil)

	case "RotateSecret":
		// POST /api/v1/tenants/{tenant_id}/apps/{client_id}/rotate-secret
		clientID := getString(reqMap, "client_id")
		if clientID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleAppRequest", "client_id is required")
		}
		return h.doRequest(ctx, http.MethodPost, "/api/v1/tenants/"+tenantID+"/apps/"+clientID+"/rotate-secret", nil)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleAppRequest", "unsupported method: "+method)
	}
}

// handleSessionRequest handles Session service requests.
//
// List and RevokeAll are parameterized (tenant_id + user_id) operations: the
// SDK session client requires both identifiers and the gRPC transport serves
// them as PEP-authorized verbs against the named user. The REST mapping
// therefore targets the path-scoped admin surface
// /api/v1/tenants/{tenant_id}/users/{user_id}/sessions — NOT the self-scoped
// /api/v1/iam/sessions surface, which derives identity exclusively from the
// authenticated Subject and rejects user_id / tenant_id parameters that name
// anyone else. Get / Revoke / RevokeOther remain self-scoped.
func (h *iamRESTHandler) handleSessionRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, _ := toMap(req)

	switch method {
	case "List":
		// GET /api/v1/tenants/{tenant_id}/users/{user_id}/sessions?limit=N
		innerReq := getMap(reqMap, "request")
		if innerReq == nil {
			innerReq = reqMap
		}
		tenantID := getString(innerReq, "tenant_id")
		if tenantID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleSessionRequest", "tenant_id is required")
		}
		userID := getString(innerReq, "user_id")
		if userID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleSessionRequest", "user_id is required")
		}
		sessionPath := "/api/v1/tenants/" + tenantID + "/users/" + userID + "/sessions"
		if limit := sessionLimitParam(innerReq); limit != "" {
			sessionPath += "?limit=" + limit
		}
		return h.doRequest(ctx, http.MethodGet, sessionPath, nil)

	case "Get":
		// GET /api/v1/iam/sessions/{id}
		sessionID := getString(reqMap, "session_id")
		if sessionID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleSessionRequest", "session_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, "/api/v1/iam/sessions/"+sessionID, nil)

	case "Revoke":
		// DELETE /api/v1/iam/sessions/{id}
		sessionID := getString(reqMap, "session_id")
		if sessionID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleSessionRequest", "session_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, "/api/v1/iam/sessions/"+sessionID, nil)

	case "RevokeAll":
		// DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/sessions
		tenantID := getString(reqMap, "tenant_id")
		if tenantID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleSessionRequest", "tenant_id is required")
		}
		userID := getString(reqMap, "user_id")
		if userID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleSessionRequest", "user_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, "/api/v1/tenants/"+tenantID+"/users/"+userID+"/sessions", nil)

	case "RevokeOther":
		// POST /api/v1/iam/sessions/revoke-others
		return h.doRequest(ctx, http.MethodPost, "/api/v1/iam/sessions/revoke-others", nil)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleSessionRequest", "unsupported method: "+method)
	}
}

// sessionLimitParam extracts a positive limit value from a session request
// map as its query-parameter string form. It accepts the float64 shape JSON
// decoding produces and the string shape map-driven callers may supply; a
// missing, zero, or non-positive limit yields "".
func sessionLimitParam(reqMap map[string]interface{}) string {
	limitVal, ok := reqMap["limit"]
	if !ok {
		return ""
	}
	switch lv := limitVal.(type) {
	case float64:
		if int(lv) > 0 {
			return strconv.Itoa(int(lv))
		}
	case string:
		if lv != "" && lv != "0" {
			return lv
		}
	}
	return ""
}

// handleAuditRequest handles Audit service requests.
func (h *iamRESTHandler) handleAuditRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, _ := toMap(req)

	switch method {
	case "Query":
		// GET /api/v1/iam/audit with query params
		auditPath := "/api/v1/iam/audit"
		// Extract filter fields for query params
		filterMap := getMap(reqMap, "filter")
		if filterMap == nil {
			filterMap = reqMap
		}
		queryParams := []string{}
		if tenantID := getString(filterMap, "tenant_id"); tenantID != "" {
			queryParams = append(queryParams, "tenant_id="+tenantID)
		}
		if actorID := getString(filterMap, "actor_id"); actorID != "" {
			queryParams = append(queryParams, "actor_id="+actorID)
		}
		if outcome := getString(filterMap, "outcome"); outcome != "" {
			queryParams = append(queryParams, "outcome="+outcome)
		}
		if len(queryParams) > 0 {
			auditPath += "?" + strings.Join(queryParams, "&")
		}
		return h.doRequest(ctx, http.MethodGet, auditPath, nil)

	case "Get":
		// GET /api/v1/iam/audit/{id}?tenant_id={tenant_id}
		eventID := getString(reqMap, "event_id")
		if eventID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleAuditRequest", "event_id is required")
		}
		auditGetPath := "/api/v1/iam/audit/" + eventID
		if tenantID := getString(reqMap, "tenant_id"); tenantID != "" {
			auditGetPath += "?tenant_id=" + tenantID
		}
		return h.doRequest(ctx, http.MethodGet, auditGetPath, nil)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleAuditRequest", "unsupported method: "+method)
	}
}

// handlePEPRequest handles PEP (Policy Enforcement Point) service requests.
func (h *iamRESTHandler) handlePEPRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, _ := toMap(req)

	switch method {
	case "Check":
		// POST /api/v1/iam/check
		// Server expects CheckRequest fields: subject, action, resource, context
		body := getMap(reqMap, "request")
		if body == nil {
			body = getMap(reqMap, "check")
		}
		if body == nil {
			body = reqMap
		}
		return h.doRequest(ctx, http.MethodPost, "/api/v1/iam/check", body)

	case "CheckBatch":
		// POST /api/v1/iam/check/batch
		// Server expects {"checks": [...]}
		body := getMap(reqMap, "request")
		if body == nil {
			body = reqMap
		}
		// Map SDK "requests" field to server "checks" field
		if requests, ok := body["requests"]; ok {
			body["checks"] = requests
			delete(body, "requests")
		}
		return h.doRequest(ctx, http.MethodPost, "/api/v1/iam/check/batch", body)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handlePEPRequest", "unsupported method: "+method)
	}
}

// handleMFARequest handles MFA service requests.
func (h *iamRESTHandler) handleMFARequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, err := toMap(req)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInvalidInput, "iamRESTHandler.handleMFARequest", "invalid request")
	}

	tenantID := getString(reqMap, "tenant_id")
	userID := getString(reqMap, "user_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleMFARequest", "tenant_id is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleMFARequest", "user_id is required")
	}

	basePath := "/api/v1/tenants/" + tenantID + "/users/" + userID + "/mfa"

	switch method {
	case "BeginEnrollment", "BeginMFAEnrollment":
		// POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/enroll
		// Server expects MFAEnrollRequest: {"method": "..."}
		enrollBody := map[string]interface{}{}
		if mfaMethod := getString(reqMap, "method"); mfaMethod != "" {
			enrollBody["method"] = mfaMethod
		}
		return h.doRequest(ctx, http.MethodPost, basePath+"/enroll", enrollBody)

	case "CompleteEnrollment", "CompleteMFAEnrollment":
		// POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/verify
		// Server expects MFACompleteRequest: {"enrollment_id": "...", "code": "..."}
		verifyBody := map[string]interface{}{
			"enrollment_id": getString(reqMap, "enrollment_id"),
			"code":          getString(reqMap, "code"),
		}
		return h.doRequest(ctx, http.MethodPost, basePath+"/verify", verifyBody)

	case "CancelEnrollment":
		// DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/enroll/{enrollment_id}
		enrollmentID := getString(reqMap, "enrollment_id")
		if enrollmentID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleMFARequest", "enrollment_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, basePath+"/enroll/"+enrollmentID, nil)

	case "GetStatus", "GetMFAStatus":
		// GET /api/v1/tenants/{tenant_id}/users/{user_id}/mfa
		return h.doRequest(ctx, http.MethodGet, basePath, nil)

	case "Disable", "DisableMFA":
		// DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/mfa
		return h.doRequest(ctx, http.MethodDelete, basePath, nil)

	case "BeginChallenge":
		// POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/challenge
		// Server expects MFAChallengeRequest: {"session_token": "..."}
		challengeBody := map[string]interface{}{
			"session_token": getString(reqMap, "session_token"),
		}
		return h.doRequest(ctx, http.MethodPost, basePath+"/challenge", challengeBody)

	case "VerifyChallenge":
		// POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/verify-challenge
		// Server expects MFAVerifyRequest: {"challenge_id": "...", "code": "..."}
		verifyChallengeBody := map[string]interface{}{
			"challenge_id": getString(reqMap, "challenge_id"),
			"code":         getString(reqMap, "code"),
		}
		return h.doRequest(ctx, http.MethodPost, basePath+"/verify-challenge", verifyChallengeBody)

	case "GenerateBackupCodes":
		// POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/backup-codes
		return h.doRequest(ctx, http.MethodPost, basePath+"/backup-codes", nil)

	case "GetBackupCodeStatus":
		// GET /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/backup-codes
		return h.doRequest(ctx, http.MethodGet, basePath+"/backup-codes", nil)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleMFARequest", "unsupported method: "+method)
	}
}

// handleAdminRequest handles Admin (delegated administration) service requests.
func (h *iamRESTHandler) handleAdminRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, _ := toMap(req)

	switch method {
	case "CreateAssignment":
		// POST /api/v1/admin/assignments
		return h.doRequest(ctx, http.MethodPost, "/api/v1/admin/assignments", mapAdminCreateAssignmentRequest(reqMap))

	case "GetAssignment":
		// GET /api/v1/admin/assignments/{assignment_id}
		assignmentID := getString(reqMap, "assignment_id")
		if assignmentID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleAdminRequest", "assignment_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, "/api/v1/admin/assignments/"+assignmentID, nil)

	case "UpdateAssignment":
		// PUT /api/v1/admin/assignments/{assignment_id}
		assignmentID := getString(reqMap, "assignment_id")
		if assignmentID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleAdminRequest", "assignment_id is required")
		}
		body := getMap(reqMap, "update")
		if body == nil {
			body = reqMap
		}
		// Map SDK status field to server's active field
		// AdminAssignmentStatusActive = 0, StatusSuspended = 2
		if statusVal, ok := body["status"]; ok {
			switch s := statusVal.(type) {
			case float64:
				// 0 = active, anything else = inactive
				active := int(s) == 0
				body["active"] = active
			case int:
				active := s == 0
				body["active"] = active
			}
			delete(body, "status")
		}
		return h.doRequest(ctx, http.MethodPut, "/api/v1/admin/assignments/"+assignmentID, body)

	case "DeleteAssignment":
		// DELETE /api/v1/admin/assignments/{assignment_id}
		assignmentID := getString(reqMap, "assignment_id")
		if assignmentID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleAdminRequest", "assignment_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, "/api/v1/admin/assignments/"+assignmentID, nil)

	case "ListAssignments":
		// GET /api/v1/admin/assignments?active_only=...
		listPath := "/api/v1/admin/assignments"
		listQueryParts := []string{}

		// Extract filter from nested wrapper
		filterMap := getMap(reqMap, "filter")
		if filterMap == nil {
			filterMap = reqMap
		}

		// Check active_only directly
		if activeOnly, ok := filterMap["active_only"]; ok {
			if boolVal, isBool := activeOnly.(bool); isBool && boolVal {
				listQueryParts = append(listQueryParts, "active_only=true")
			}
		}

		// Map status filter to active_only: status 0 = AdminAssignmentStatusActive
		if statusVal, ok := filterMap["status"]; ok {
			switch s := statusVal.(type) {
			case float64:
				if int(s) == 0 {
					listQueryParts = append(listQueryParts, "active_only=true")
				}
			case int:
				if s == 0 {
					listQueryParts = append(listQueryParts, "active_only=true")
				}
			}
		}

		// Check include_expired and negate as active_only
		if includeExpired, ok := filterMap["include_expired"]; ok {
			if boolVal, isBool := includeExpired.(bool); isBool && !boolVal {
				listQueryParts = append(listQueryParts, "active_only=true")
			}
		}

		if userID := getString(filterMap, "user_id"); userID != "" {
			listQueryParts = append(listQueryParts, "user_id="+userID)
		}
		if userID := getString(filterMap, "admin_id"); userID != "" {
			listQueryParts = append(listQueryParts, "user_id="+userID)
		}
		if tenantID := getString(filterMap, "tenant_id"); tenantID != "" {
			listQueryParts = append(listQueryParts, "tenant_id="+tenantID)
		}
		if len(listQueryParts) > 0 {
			listPath += "?" + strings.Join(listQueryParts, "&")
		}
		return h.doRequest(ctx, http.MethodGet, listPath, nil)

	case "CheckPermission":
		// POST /api/v1/admin/check-permission
		body := getMap(reqMap, "check")
		if body == nil {
			body = reqMap
		}
		return h.doRequest(ctx, http.MethodPost, "/api/v1/admin/check-permission", body)

	case "StartImpersonation":
		// POST /api/v1/admin/impersonation/start
		body := getMap(reqMap, "impersonation")
		if body == nil {
			body = reqMap
		}
		body = mapImpersonationRequest(body)
		return h.doRequest(ctx, http.MethodPost, "/api/v1/admin/impersonation/start", body)

	case "EndImpersonation":
		// POST /api/v1/admin/impersonation/{session_id}/end
		sessionID := getString(reqMap, "session_id")
		if sessionID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleAdminRequest", "session_id is required")
		}
		return h.doRequest(ctx, http.MethodPost, "/api/v1/admin/impersonation/"+sessionID+"/end", nil)

	case "GetActiveImpersonations", "ListActiveImpersonations":
		// GET /api/v1/admin/impersonation?active=true&admin_user_id=...
		listAdminUserID := getString(reqMap, "admin_user_id")
		listPath := "/api/v1/admin/impersonation?active=true"
		if listAdminUserID != "" {
			listPath += "&admin_user_id=" + listAdminUserID
		}
		return h.doRequest(ctx, http.MethodGet, listPath, nil)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleAdminRequest", "unsupported method: "+method)
	}
}

// handleGDPRRequest handles GDPR compliance service requests.
func (h *iamRESTHandler) handleGDPRRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, _ := toMap(req)

	tenantID := getString(reqMap, "tenant_id")
	userID := getString(reqMap, "user_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleGDPRRequest", "tenant_id is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleGDPRRequest", "user_id is required")
	}

	basePath := "/api/v1/tenants/" + tenantID + "/users/" + userID + "/gdpr"

	switch method {
	case "GrantConsent":
		// POST /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr/consent
		body := getMap(reqMap, "consent")
		if body == nil {
			body = reqMap
		}
		return h.doRequest(ctx, http.MethodPost, basePath+"/consent", body)

	case "WithdrawConsent":
		// PUT /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr/consent/{purpose}/withdraw
		purpose := getString(reqMap, "purpose")
		if purpose == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGDPRRequest", "purpose is required")
		}
		// Server expects optional {"ip_address": "...", "user_agent": "..."}
		withdrawBody := map[string]interface{}{}
		if ip := getString(reqMap, "ip_address"); ip != "" {
			withdrawBody["ip_address"] = ip
		}
		if ua := getString(reqMap, "user_agent"); ua != "" {
			withdrawBody["user_agent"] = ua
		}
		var withdrawReqBody interface{}
		if len(withdrawBody) > 0 {
			withdrawReqBody = withdrawBody
		}
		return h.doRequest(ctx, http.MethodPut, basePath+"/consent/"+purpose+"/withdraw", withdrawReqBody)

	case "GetConsent":
		// GET /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr/consent/{purpose}
		purpose := getString(reqMap, "purpose")
		if purpose == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGDPRRequest", "purpose is required")
		}
		return h.doRequest(ctx, http.MethodGet, basePath+"/consent/"+purpose, nil)

	case "ListConsents":
		// GET /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr/consents
		resp, listErr := h.doRequest(ctx, http.MethodGet, basePath+"/consents", nil)
		if listErr != nil {
			return nil, listErr
		}
		// Server may return a JSON array directly; wrap in object for SDK compatibility.
		if arr, ok := resp.([]interface{}); ok {
			return map[string]interface{}{
				"consents":    arr,
				"total_count": len(arr),
			}, nil
		}
		return resp, nil

	case "RequestExport":
		// POST /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr/export
		body := getMap(reqMap, "export")
		if body == nil {
			body = reqMap
		}
		// Convert format from SDK int enum to server string value.
		body = mapExportRequestBody(body, userID)
		return h.doRequest(ctx, http.MethodPost, basePath+"/export", body)

	case "GetExportStatus":
		// GET /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr/export/{export_id}
		exportID := getString(reqMap, "export_id")
		if exportID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGDPRRequest", "export_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, basePath+"/export/"+exportID, nil)

	case "GenerateExport":
		// GET /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr/export/{export_id}/download
		exportID := getString(reqMap, "export_id")
		if exportID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGDPRRequest", "export_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, basePath+"/export/"+exportID+"/download", nil)

	case "RequestDeletion":
		// POST /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr/deletion
		body := getMap(reqMap, "deletion")
		if body == nil {
			body = reqMap
		}
		// The typed SDK sends ScheduledFor (absolute time); the server's
		// REST handler reads grace_period_days. Translate so the server
		// applies the operator's grace window instead of triggering
		// immediate async deletion (gracePeriodDays==0).
		if _, hasGrace := body["grace_period_days"]; !hasGrace {
			if scheduledStr := getString(body, "scheduled_for"); scheduledStr != "" {
				if t, perr := time.Parse(time.RFC3339Nano, scheduledStr); perr == nil {
					if days := int(time.Until(t).Hours() / 24); days > 0 {
						body["grace_period_days"] = days
					}
				}
			}
		}
		return h.doRequest(ctx, http.MethodPost, basePath+"/deletion", body)

	case "CancelDeletion":
		// DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr/deletion/{deletion_id}
		deletionID := getString(reqMap, "deletion_id")
		if deletionID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGDPRRequest", "deletion_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, basePath+"/deletion/"+deletionID, nil)

	case "GetDeletionStatus":
		// GET /api/v1/tenants/{tenant_id}/users/{user_id}/gdpr/deletion/{deletion_id}
		deletionID := getString(reqMap, "deletion_id")
		if deletionID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleGDPRRequest", "deletion_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, basePath+"/deletion/"+deletionID, nil)

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleGDPRRequest", "unsupported method: "+method)
	}
}

// handleFederationRequest handles Federation (identity provider management) service requests.
func (h *iamRESTHandler) handleFederationRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, _ := toMap(req)

	tenantID := getString(reqMap, "tenant_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleFederationRequest", "tenant_id is required")
	}

	basePath := "/api/v1/tenants/" + tenantID + "/providers"

	switch method {
	case "CreateProvider":
		// POST /api/v1/tenants/{tenant_id}/providers
		// Server expects RegisterProviderOptions fields directly, not wrapped
		providerBody := getMap(reqMap, "provider")
		if providerBody == nil {
			providerBody = getMap(reqMap, "request")
		}
		if providerBody == nil {
			// Remove routing fields that are already in the URL path
			providerBody = make(map[string]interface{})
			for k, v := range reqMap {
				if k != "tenant_id" {
					providerBody[k] = v
				}
			}
		}
		providerBody = mapProviderCreateRequest(providerBody)
		return h.doRequest(ctx, http.MethodPost, basePath, providerBody)

	case "GetProvider":
		// GET /api/v1/tenants/{tenant_id}/providers/{provider_id}
		providerID := getString(reqMap, "provider_id")
		if providerID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleFederationRequest", "provider_id is required")
		}
		return h.doRequest(ctx, http.MethodGet, basePath+"/"+providerID, nil)

	case "UpdateProvider":
		// PUT /api/v1/tenants/{tenant_id}/providers/{provider_id}
		providerID := getString(reqMap, "provider_id")
		if providerID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleFederationRequest", "provider_id is required")
		}
		// Server expects UpdateProviderOptions fields directly, not wrapped
		updateBody := getMap(reqMap, "update")
		if updateBody == nil {
			updateBody = getMap(reqMap, "request")
		}
		if updateBody == nil {
			// Remove routing fields that are already in the URL path
			updateBody = make(map[string]interface{})
			for k, v := range reqMap {
				if k != "tenant_id" && k != "provider_id" {
					updateBody[k] = v
				}
			}
		}
		updateBody = mapProviderUpdateRequest(updateBody)
		return h.doRequest(ctx, http.MethodPut, basePath+"/"+providerID, updateBody)

	case "DeleteProvider":
		// DELETE /api/v1/tenants/{tenant_id}/providers/{provider_id}
		providerID := getString(reqMap, "provider_id")
		if providerID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleFederationRequest", "provider_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, basePath+"/"+providerID, nil)

	case "ListProviders":
		// GET /api/v1/tenants/{tenant_id}/providers
		resp, listErr := h.doRequest(ctx, http.MethodGet, basePath, nil)
		if listErr != nil {
			return nil, listErr
		}
		return extractProvidersFromResponse(resp), nil

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleFederationRequest", "unsupported method: "+method)
	}
}

// handleIdentityRequest handles Identity (account linking) service requests.
func (h *iamRESTHandler) handleIdentityRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	reqMap, _ := toMap(req)

	tenantID := getString(reqMap, "tenant_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleIdentityRequest", "tenant_id is required")
	}

	userID := getString(reqMap, "user_id")
	if userID == "" {
		return nil, errors.NewInvalidInput("iamRESTHandler.handleIdentityRequest", "user_id is required")
	}

	basePath := "/api/v1/tenants/" + tenantID + "/users/" + userID + "/links"

	switch method {
	case "LinkIdentity":
		// POST /api/v1/tenants/{tenant_id}/users/{user_id}/links
		// Server expects LinkAccountOptions: {"provider_id": "...", "external_id": "...", "access_token": "..."}
		linkBody := map[string]interface{}{
			"provider_id": getString(reqMap, "provider_id"),
			"external_id": getString(reqMap, "external_id"),
		}
		if token := getString(reqMap, "access_token"); token != "" {
			linkBody["access_token"] = token
		}
		return h.doRequest(ctx, http.MethodPost, basePath, linkBody)

	case "UnlinkIdentity":
		// DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/links/{provider_id}
		providerID := getString(reqMap, "provider_id")
		if providerID == "" {
			return nil, errors.NewInvalidInput("iamRESTHandler.handleIdentityRequest", "provider_id is required")
		}
		return h.doRequest(ctx, http.MethodDelete, basePath+"/"+providerID, nil)

	case "ListUserIdentities":
		// GET /api/v1/tenants/{tenant_id}/users/{user_id}/links
		resp, listErr := h.doRequest(ctx, http.MethodGet, basePath, nil)
		if listErr != nil {
			return nil, listErr
		}
		return extractIdentitiesFromResponse(resp), nil

	default:
		return nil, errors.NewUnimplemented("iamRESTHandler.handleIdentityRequest", "unsupported method: "+method)
	}
}

// doRequest performs an HTTP request with the given method, path, and body.
func (h *iamRESTHandler) doRequest(ctx context.Context, httpMethod, path string, body interface{}) (interface{}, error) {
	if h.transport.closed.Load() {
		return nil, errors.NewConnection("iamRESTHandler.doRequest", "transport is closed")
	}

	url := h.transport.baseURL + h.transport.basePath + path

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeInvalidInput, "iamRESTHandler.doRequest", "failed to encode request body")
		}
		reqBody = bytes.NewReader(data)
	}

	httpReq, err := http.NewRequestWithContext(ctx, httpMethod, url, reqBody)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "iamRESTHandler.doRequest", "failed to create HTTP request")
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	h.transport.mu.RLock()
	for k, v := range h.transport.headers {
		httpReq.Header.Set(k, v)
	}
	h.transport.mu.RUnlock()

	resp, err := h.transport.client.Do(httpReq)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeConnection, "iamRESTHandler.doRequest", "HTTP request failed")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "iamRESTHandler.doRequest", "failed to read response body")
	}

	// Check for error responses
	if resp.StatusCode >= 400 {
		var errResp map[string]interface{}
		if err := json.Unmarshal(respBody, &errResp); err == nil {
			errMsg := ""
			if msg, ok := errResp["error"].(string); ok {
				errMsg = msg
			} else if msg, ok := errResp["message"].(string); ok {
				errMsg = msg
			}
			if detail, ok := errResp["error_description"].(string); ok && detail != "" {
				errMsg = errMsg + ": " + detail
			}

			switch {
			case resp.StatusCode == http.StatusNotFound:
				return nil, errors.NewNotFound("iamRESTHandler.doRequest", errMsg)
			case resp.StatusCode == http.StatusConflict:
				return nil, errors.NewAlreadyExists("iamRESTHandler.doRequest", errMsg)
			case resp.StatusCode == http.StatusBadRequest:
				return nil, errors.NewInvalidInput("iamRESTHandler.doRequest", errMsg)
			case resp.StatusCode == http.StatusUnauthorized:
				return nil, errors.NewUnauthorized("iamRESTHandler.doRequest", errMsg)
			case resp.StatusCode == http.StatusForbidden:
				return nil, errors.NewForbidden("iamRESTHandler.doRequest", errMsg)
			default:
				return nil, errors.NewInternal("iamRESTHandler.doRequest", errMsg)
			}
		}
		return nil, errors.NewInternal("iamRESTHandler.doRequest", "HTTP "+resp.Status)
	}

	// Handle no-content responses
	if resp.StatusCode == http.StatusNoContent || len(respBody) == 0 {
		return nil, nil
	}

	// Parse response
	var result interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Try to return as raw string if not JSON
		return string(respBody), nil
	}

	return result, nil
}

// mapUserCreateRequest maps SDK CreateUserRequest fields to REST API fields.
// SDK sends: username, email, display_name, auth_methods, roles, groups, metadata
// REST expects: id, email, username, name, auth_methods, roles, groups, attributes, enabled
func mapUserCreateRequest(req map[string]interface{}) map[string]interface{} {
	if req == nil {
		return nil
	}
	body := make(map[string]interface{})

	// Map email
	if email := getString(req, "email"); email != "" {
		body["email"] = email
	}

	// Map username - the SDK may send email as username, but REST expects a different format
	// If username looks like an email, derive a valid username from it
	username := getString(req, "username")
	if username != "" {
		// Check if username contains invalid characters for the REST API
		// Valid: 3-64 alphanumeric characters, dash, or underscore
		// If it looks like an email, extract the local part before @
		if idx := strings.Index(username, "@"); idx > 0 {
			username = username[:idx]
		}
		// Replace any remaining dots with underscores
		username = strings.ReplaceAll(username, ".", "_")
		body["username"] = username
	}

	// Map display_name to name
	if displayName := getString(req, "display_name"); displayName != "" {
		body["name"] = displayName
	}

	// Map metadata to attributes
	if metadata, ok := req["metadata"].(map[string]interface{}); ok && len(metadata) > 0 {
		body["attributes"] = metadata
	}

	// Map roles (initial role IDs). The server validates the references
	// before creating the user, so they must not be dropped here.
	if roles, ok := req["roles"].([]interface{}); ok && len(roles) > 0 {
		body["roles"] = roles
	}

	// Map groups (initial group memberships). Like roles, the server
	// validates the references before creating the user, so they must not
	// be dropped here.
	if groups, ok := req["groups"].([]interface{}); ok && len(groups) > 0 {
		body["groups"] = groups
	}

	// Map enabled (default to true if not specified)
	if enabled, ok := req["enabled"].(bool); ok {
		body["enabled"] = enabled
	} else {
		body["enabled"] = true
	}

	return body
}

// mapUserUpdateRequest maps SDK UpdateUserRequest fields to REST API fields.
func mapUserUpdateRequest(req map[string]interface{}) map[string]interface{} {
	if req == nil {
		return nil
	}
	body := make(map[string]interface{})

	// Map display_name to name
	if displayName := getString(req, "display_name"); displayName != "" {
		body["name"] = displayName
	}

	// Map metadata to attributes
	if metadata, ok := req["metadata"].(map[string]interface{}); ok && len(metadata) > 0 {
		body["attributes"] = metadata
	}

	// Map enabled
	if enabled, ok := req["enabled"].(bool); ok {
		body["enabled"] = enabled
	}

	return body
}

// mapAdminCreateAssignmentRequest maps SDK CreateAdminAssignmentRequest fields
// to REST API CreateAdminAssignmentOptions fields.
// SDK sends: user_id, tenant_id, admin_role (int scope), role_id (string role name),
//
//	permissions ([]string), granted_by, expires_at, metadata
//
// REST expects: user_id, admin_role (string), tenant_ids ([]string),
//
//	permissions ([]AdminPermission), granted_by, expires_at, metadata
func mapAdminCreateAssignmentRequest(req map[string]interface{}) map[string]interface{} {
	if req == nil {
		return nil
	}
	body := make(map[string]interface{})

	// Copy user_id directly.
	if userID := getString(req, "user_id"); userID != "" {
		body["user_id"] = userID
	}

	// Map role_id (SDK) -> admin_role (server).
	// The CLI passes the role name (e.g. "tenant_admin") as RoleID.
	// The server expects this as the admin_role string field.
	if roleID := getString(req, "role_id"); roleID != "" {
		body["admin_role"] = roleID
	}

	// Copy tenant_ids directly if present. Do NOT convert the singular
	// tenant_id field to tenant_ids: tenant_id is the CLI global context
	// flag (defaults to "default") and is semantically different from the
	// assignment's tenant scope (tenant_ids). This matches gRPC behavior
	// where tenant_ids is read separately from tenant_id.
	if tenantIDs, ok := req["tenant_ids"]; ok && tenantIDs != nil {
		body["tenant_ids"] = tenantIDs
	}

	// Copy permissions directly (AdminPermission is string-based).
	if perms, ok := req["permissions"]; ok && perms != nil {
		body["permissions"] = perms
	}

	// Copy granted_by directly.
	if grantedBy := getString(req, "granted_by"); grantedBy != "" {
		body["granted_by"] = grantedBy
	}

	// Copy expires_at, but skip zero time values that would cause
	// assignments to be created with an immediate expiration.
	if expiresAt, ok := req["expires_at"]; ok && expiresAt != nil {
		if str, isStr := expiresAt.(string); !isStr || (str != "0001-01-01T00:00:00Z" && str != "0001-01-01T00:00:00.000Z") {
			body["expires_at"] = expiresAt
		}
	}

	// Copy metadata directly.
	if metadata, ok := req["metadata"]; ok && metadata != nil {
		body["metadata"] = metadata
	}

	return body
}

// mapAppRegisterRequest maps SDK RegisterAppRequest fields to the format
// expected by the REST server. Converts GrantTypes from int enum values
// to string values (e.g., "authorization_code", "client_credentials").
func mapAppRegisterRequest(req map[string]interface{}) map[string]interface{} {
	if req == nil {
		return nil
	}
	body := make(map[string]interface{})

	// Copy simple fields directly
	for k, v := range req {
		if k == "grant_types" {
			body[k] = convertGrantTypes(v)
		} else if k == "resource_prefix" {
			body[k] = v
		} else {
			body[k] = v
		}
	}

	return body
}

// convertGrantTypes converts GrantType values from int enum to string.
// The SDK uses GrantType int enum (0=authorization_code, 1=client_credentials, etc.)
// but the REST server expects string values.
func convertGrantTypes(v interface{}) []string {
	grantTypeStrings := map[int]string{
		0: "authorization_code",
		1: "client_credentials",
		2: "refresh_token",
		3: "urn:ietf:params:oauth:grant-type:device_code",
		4: "implicit",
		5: "password",
	}

	switch gt := v.(type) {
	case []interface{}:
		result := make([]string, 0, len(gt))
		for _, item := range gt {
			switch val := item.(type) {
			case float64:
				if s, ok := grantTypeStrings[int(val)]; ok {
					result = append(result, s)
				}
			case string:
				result = append(result, val)
			}
		}
		return result
	case []string:
		return gt
	default:
		return nil
	}
}

// mapImpersonationRequest maps SDK ImpersonationRequest fields to the format
// expected by the REST server. Converts Duration from time.Duration (nanoseconds)
// to a string representation (e.g., "1h", "30m").
func mapImpersonationRequest(req map[string]interface{}) map[string]interface{} {
	if req == nil {
		return nil
	}
	body := make(map[string]interface{})

	// Copy fields, converting duration
	for k, v := range req {
		if k == "duration" {
			body[k] = convertDuration(v)
		} else {
			body[k] = v
		}
	}

	return body
}

// convertDuration converts a duration value from nanoseconds (as float64 from
// JSON unmarshal) to a human-readable duration string for the REST server.
func convertDuration(v interface{}) string {
	switch d := v.(type) {
	case float64:
		dur := time.Duration(int64(d))
		if dur <= 0 {
			return ""
		}
		return formatDuration(dur)
	case string:
		return d
	default:
		return ""
	}
}

// formatDuration formats a time.Duration into a compact string like "1h30m".
func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	h := int(math.Floor(d.Hours()))
	m := int(math.Floor(d.Minutes())) % 60
	s := int(math.Floor(d.Seconds())) % 60

	if h > 0 && m > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh", h)
	}
	if m > 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%ds", s)
}

// mapProviderCreateRequest maps SDK CreateProviderOptions fields to the
// server's RegisterProviderOptions format.
// SDK field -> Server field:
//
//	provider_id -> id
//	issuer_url  -> issuer
func mapProviderCreateRequest(req map[string]interface{}) map[string]interface{} {
	if req == nil {
		return nil
	}
	body := make(map[string]interface{})

	for k, v := range req {
		switch k {
		case "provider_id":
			body["id"] = v
		case "issuer_url":
			body["issuer"] = v
		case "tenant_id":
			// Skip - already in URL path
		default:
			body[k] = v
		}
	}

	// Default enabled to true if not specified
	if _, ok := body["enabled"]; !ok {
		body["enabled"] = true
	}

	return body
}

// mapProviderUpdateRequest maps SDK UpdateProviderOptions fields to the
// server's UpdateProviderOptions format.
func mapProviderUpdateRequest(req map[string]interface{}) map[string]interface{} {
	if req == nil {
		return nil
	}
	body := make(map[string]interface{})

	for k, v := range req {
		switch k {
		case "provider_id", "tenant_id":
			// Skip - already in URL path
		case "issuer_url":
			body["issuer"] = v
		default:
			body[k] = v
		}
	}

	return body
}

// toMap converts a request to a map[string]interface{}.
func toMap(req interface{}) (map[string]interface{}, error) {
	if req == nil {
		return nil, nil
	}

	if m, ok := req.(map[string]interface{}); ok {
		return m, nil
	}

	// Marshal and unmarshal to get a map
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}

	return m, nil
}

// getString extracts a string value from a map.
func getString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// getMap extracts a nested map from a map.
func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if m == nil {
		return nil
	}
	if v, ok := m[key].(map[string]interface{}); ok {
		return v
	}
	return nil
}

// mapTenantSettings converts SDK settings (map[string]string) to the typed
// struct format the REST server expects (int/bool fields instead of strings).
func mapTenantSettings(settings interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	switch s := settings.(type) {
	case map[string]interface{}:
		for k, v := range s {
			result[k] = convertSettingValue(k, v)
		}
	case map[string]string:
		for k, v := range s {
			result[k] = convertSettingValue(k, v)
		}
	default:
		return nil
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// convertSettingValue converts a setting value from string to its expected
// typed form based on the setting key name.
func convertSettingValue(key string, val interface{}) interface{} {
	str, ok := val.(string)
	if !ok {
		return val
	}

	switch key {
	case "max_users":
		if n, err := strconv.Atoi(str); err == nil {
			return n
		}
	case "mfa_required":
		if b, err := strconv.ParseBool(str); err == nil {
			return b
		}
	}
	return str
}

// mapTenantCreateRequest maps SDK CreateTenantRequest fields to the format
// expected by the REST server. Handles display_name->name fallback and
// settings type conversion. max_users stays inside settings: the server's
// TenantSettings carries it (json "max_users") and engine-level user quotas
// are owned by qrdb's TenantServicer, not the BaaS TenantQuotas struct.
func mapTenantCreateRequest(req map[string]interface{}) map[string]interface{} {
	if req == nil {
		return nil
	}
	body := make(map[string]interface{})

	if id := getString(req, "id"); id != "" {
		body["id"] = id
	}

	// Use name; fall back to display_name if name is empty.
	name := getString(req, "name")
	if name == "" {
		name = getString(req, "display_name")
	}
	if name != "" {
		body["name"] = name
	}

	if settings, ok := req["settings"]; ok && settings != nil {
		if mapped := mapTenantSettings(settings); len(mapped) > 0 {
			body["settings"] = mapped
		}
	}

	return body
}

// mapTenantUpdateRequest maps SDK UpdateTenantRequest fields to the format
// expected by the REST server. Maps display_name to name and converts
// settings from string values to typed values. max_users stays inside
// settings for the same reason as mapTenantCreateRequest.
func mapTenantUpdateRequest(req map[string]interface{}) map[string]interface{} {
	if req == nil {
		return nil
	}
	body := make(map[string]interface{})

	// The SDK sends display_name, but the server expects name.
	name := getString(req, "display_name")
	if name == "" {
		name = getString(req, "name")
	}
	if name != "" {
		body["name"] = name
	}

	if settings, ok := req["settings"]; ok && settings != nil {
		if mapped := mapTenantSettings(settings); len(mapped) > 0 {
			body["settings"] = mapped
		}
	}

	return body
}

// mapServerPolicyToSDK converts a server-format policy response (with apiVersion,
// kind, metadata, spec) to the SDK's flat format (with name, tenant_id, rules).
func mapServerPolicyToSDK(resp interface{}) interface{} {
	policyMap, ok := resp.(map[string]interface{})
	if !ok {
		return resp
	}

	// Check if this is a server-format policy (has apiVersion or metadata)
	_, hasAPIVersion := policyMap["apiVersion"]
	_, hasMetadata := policyMap["metadata"]
	if !hasAPIVersion && !hasMetadata {
		return resp
	}

	result := make(map[string]interface{})

	// Extract metadata fields
	if metadata, ok := policyMap["metadata"].(map[string]interface{}); ok {
		if name, ok := metadata["name"].(string); ok {
			result["name"] = name
		}
		if tenant, ok := metadata["tenant"].(string); ok {
			result["tenant_id"] = tenant
		}
		if desc, ok := metadata["description"].(string); ok {
			result["description"] = desc
		}
		if labels, ok := metadata["labels"]; ok {
			result["labels"] = labels
		}
		if priority, ok := metadata["priority"]; ok {
			result["priority"] = priority
		}
		if createdAt, ok := metadata["created_at"]; ok {
			result["created_at"] = createdAt
		} else if createdAt, ok := metadata["created"]; ok {
			result["created_at"] = createdAt
		}
		if updatedAt, ok := metadata["updated_at"]; ok {
			result["updated_at"] = updatedAt
		} else if updatedAt, ok := metadata["updated"]; ok {
			result["updated_at"] = updatedAt
		}
	}

	// Extract spec fields
	if spec, ok := policyMap["spec"].(map[string]interface{}); ok {
		if rules, ok := spec["rules"]; ok {
			rules = transformServerRulesToSDK(rules)
			result["rules"] = rules
		}
		if targets, ok := spec["targets"]; ok {
			result["targets"] = targets
		}
		if conditions, ok := spec["conditions"]; ok {
			result["conditions"] = conditions
		}
		if effect, ok := spec["effect"]; ok {
			result["effect"] = effect
		}
		if enabled, ok := spec["enabled"]; ok {
			result["enabled"] = enabled
		}
	}

	return result
}

// mapServerPolicyListToSDK converts a list of server-format policies to SDK format.
// The REST server returns {"policies": [...], "limit": N, "offset": N}.

// transformServerRulesToSDK converts server-format rules (actions/resources as
// string arrays, subjects as struct) to SDK format (ActionMatcher/ResourceMatcher
// arrays, SubjectMatcher arrays).
func transformServerRulesToSDK(rulesRaw interface{}) interface{} {
	rulesArr, ok := rulesRaw.([]interface{})
	if !ok {
		return rulesRaw
	}
	for _, ruleRaw := range rulesArr {
		ruleMap, ok := ruleRaw.(map[string]interface{})
		if !ok {
			continue
		}
		// Convert actions from []string to []ActionMatcher
		if actions, ok := ruleMap["actions"].([]interface{}); ok {
			matchers := make([]interface{}, 0, len(actions))
			for _, a := range actions {
				if s, ok := a.(string); ok {
					matchers = append(matchers, map[string]interface{}{"pattern": s})
				}
			}
			ruleMap["actions"] = matchers
		}
		// Convert resources from []string to []ResourceMatcher
		if resources, ok := ruleMap["resources"].([]interface{}); ok {
			matchers := make([]interface{}, 0, len(resources))
			for _, r := range resources {
				if s, ok := r.(string); ok {
					matchers = append(matchers, map[string]interface{}{"pattern": s})
				}
			}
			ruleMap["resources"] = matchers
		}
		// Convert subjects from SubjectSelector (struct) to []SubjectMatcher (array)
		if subjects, ok := ruleMap["subjects"].(map[string]interface{}); ok {
			var subjectMatchers []interface{}
			// Convert authenticated flag to a matcher
			if auth, ok := subjects["authenticated"].(bool); ok && auth {
				subjectMatchers = append(subjectMatchers, map[string]interface{}{
					"type": "authenticated",
				})
			}
			// Convert user IDs
			if users, ok := subjects["users"].([]interface{}); ok {
				for _, u := range users {
					if s, ok := u.(string); ok {
						subjectMatchers = append(subjectMatchers, map[string]interface{}{
							"type": "user", "id": s,
						})
					}
				}
			}
			if len(subjectMatchers) > 0 {
				ruleMap["subjects"] = subjectMatchers
			}
		}
	}
	return rulesArr
}
func mapServerPolicyListToSDK(resp interface{}) interface{} {
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return resp
	}

	policies, ok := respMap["policies"]
	if !ok {
		return resp
	}

	policiesArr, ok := policies.([]interface{})
	if !ok {
		return resp
	}

	mapped := make([]interface{}, len(policiesArr))
	for i, p := range policiesArr {
		mapped[i] = mapServerPolicyToSDK(p)
	}

	result := make(map[string]interface{})
	result["policies"] = mapped
	if total, ok := respMap["total"]; ok {
		result["total_count"] = total
	}

	return result
}

// extractProvidersFromResponse extracts the providers array from the REST
// response wrapper {"providers": [...], "limit": N, "offset": N}.
func extractProvidersFromResponse(resp interface{}) interface{} {
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return resp
	}
	if providers, ok := respMap["providers"]; ok {
		return providers
	}
	return resp
}

// extractIdentitiesFromResponse extracts the identities array from the REST
// response wrapper {"identities": [...], "limit": N, "offset": N}.
func extractIdentitiesFromResponse(resp interface{}) interface{} {
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return resp
	}
	if identities, ok := respMap["identities"]; ok {
		return identities
	}
	return resp
}

// mapExportRequestBody converts SDK ExportRequest fields to the REST server format.
// Converts the format field from SDK int enum to the server string value and
// adds the requested_by field (defaulting to user_id).
func mapExportRequestBody(body map[string]interface{}, userID string) map[string]interface{} {
	if body == nil {
		return nil
	}
	result := make(map[string]interface{}, len(body))
	for k, v := range body {
		result[k] = v
	}

	// Convert format from int enum to string.
	// SDK ExportFormat: 0=json, 1=csv, 2=xml
	exportFormatStrings := map[int]string{
		0: "json",
		1: "csv",
		2: "xml",
	}
	if formatVal, ok := result["format"]; ok {
		switch f := formatVal.(type) {
		case float64:
			if s, found := exportFormatStrings[int(f)]; found {
				result["format"] = s
			}
		case int:
			if s, found := exportFormatStrings[f]; found {
				result["format"] = s
			}
		}
	}

	// Add requested_by if not present (default to user_id).
	if _, ok := result["requested_by"]; !ok {
		result["requested_by"] = userID
	}

	// Remove fields that are in the URL path, not the body.
	delete(result, "tenant_id")
	delete(result, "user_id")

	return result
}
