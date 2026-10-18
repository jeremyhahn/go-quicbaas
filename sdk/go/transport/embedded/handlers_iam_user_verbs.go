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

package embedded

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The quicbaas.IAM.User lookup, role and approval verbs. Each one routes to
// the IdentityStore method the REST, gRPC or MCP transport already calls for
// the same verb; the user model and its lifecycle rules belong to the store.
//
// Suspend and Unsuspend are deliberately absent: no other transport serves
// them as verbs, so an "unknown method" here matches the rest of the stack.

const iamUserService = "quicbaas.IAM.User"

// iamUserSuccess is the void-verb reply the SDK client's
// isUserSuccessResponse accepts; its typed response structs are unexported.
var iamUserSuccess = map[string]bool{"success": true}

// handleIAMUserGetByEmail serves quicbaas.IAM.User/GetByEmail through
// IdentityStore.GetUserByEmail, as the gRPC transport does.
func handleIAMUserGetByEmail(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	email, _ := iamRequestField(req, "Email")
	if tenantID == "" || email == "" {
		return nil, NewInvalidRequestTypeError(iamUserService, "GetByEmail",
			"struct with TenantID + Email fields", typeNameOf(req))
	}
	u, err := store.GetUserByEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}
	return userToSDK(u), nil
}

// handleIAMUserFindByUsername serves quicbaas.IAM.User/FindByUsername
// through IdentityStore.GetUserByUsername, as the MCP transport does. The
// tenant is passed through unchanged: the SDK client allows it to be empty.
func handleIAMUserFindByUsername(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	username, _ := iamRequestField(req, "Username")
	if username == "" {
		return nil, NewInvalidRequestTypeError(iamUserService, "FindByUsername",
			"struct with TenantID + Username fields", typeNameOf(req))
	}
	u, err := store.GetUserByUsername(ctx, tenantID, username)
	if err != nil {
		return nil, err
	}
	return userToSDK(u), nil
}

// handleIAMUserAssignRole serves quicbaas.IAM.User/AssignRole through
// IdentityStore.AssignRole, as the gRPC transport does.
func handleIAMUserAssignRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, tenantID, userID, roleID, err := iamUserRoleRequest(ctx, svc, req, "AssignRole")
	if err != nil {
		return nil, err
	}
	if err := store.AssignRole(ctx, tenantID, userID, roleID); err != nil {
		return nil, err
	}
	return iamUserSuccess, nil
}

// handleIAMUserRevokeRole serves quicbaas.IAM.User/RevokeRole through
// IdentityStore.RevokeRole, as the gRPC transport does.
func handleIAMUserRevokeRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	store, tenantID, userID, roleID, err := iamUserRoleRequest(ctx, svc, req, "RevokeRole")
	if err != nil {
		return nil, err
	}
	if err := store.RevokeRole(ctx, tenantID, userID, roleID); err != nil {
		return nil, err
	}
	return iamUserSuccess, nil
}

// handleIAMUserApprove serves quicbaas.IAM.User/Approve. Like the REST
// approve route it moves the user to the active lifecycle through
// IdentityStore.UpdateUser; the store decides whether the move is legal.
func handleIAMUserApprove(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return setIAMUserLifecycle(ctx, svc, req, "Approve", iam.LifecycleActive)
}

// handleIAMUserReject serves quicbaas.IAM.User/Reject. Like the REST reject
// route it moves the user to the disabled lifecycle through
// IdentityStore.UpdateUser. The reason is accepted but not stored, which is
// also what the REST route does with it.
func handleIAMUserReject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	return setIAMUserLifecycle(ctx, svc, req, "Reject", iam.LifecycleDisabled)
}

// iamUserRoleRequest resolves the store and the tenant, user and role IDs a
// role assignment verb carries.
func iamUserRoleRequest(ctx context.Context, svc service.QuicBaaS, req interface{}, method string) (*iam.IdentityStore, string, uint64, string, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, "", 0, "", err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	userID, _ := iamRequestField(req, "UserID")
	roleID, _ := iamRequestField(req, "RoleID")
	if tenantID == "" || userID == "" || roleID == "" {
		return nil, "", 0, "", NewInvalidRequestTypeError(iamUserService, method,
			"struct with TenantID + UserID + RoleID fields", typeNameOf(req))
	}
	id, err := parseIAMUserID(iamUserService, method, userID)
	if err != nil {
		return nil, "", 0, "", err
	}
	return store, tenantID, id, roleID, nil
}

// setIAMUserLifecycle moves the user a request names to the given lifecycle.
func setIAMUserLifecycle(ctx context.Context, svc service.QuicBaaS, req interface{}, method string, state iam.Lifecycle) (interface{}, error) {
	store, err := identityStoreFor(ctx, svc)
	if err != nil {
		return nil, err
	}
	tenantID, _ := iamRequestField(req, "TenantID")
	userID, _ := iamRequestField(req, "UserID")
	if tenantID == "" || userID == "" {
		return nil, NewInvalidRequestTypeError(iamUserService, method,
			"struct with TenantID + UserID fields", typeNameOf(req))
	}
	id, err := parseIAMUserID(iamUserService, method, userID)
	if err != nil {
		return nil, err
	}
	if _, err := store.UpdateUser(ctx, tenantID, id, iam.UpdateUserOptions{Lifecycle: &state}); err != nil {
		return nil, err
	}
	return iamUserSuccess, nil
}
