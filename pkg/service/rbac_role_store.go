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

package service

import (
	"context"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// RBAC role STORE — IAM build.
//
// CreateRole / DeleteRole / ListRoles are backed by the qrdb-supplied
// IdentityStore (resolved through quicBaaS.GetIdentityStore). Roles are
// tenant-scoped: the tenant is read from the request context and the
// operation fails closed when it is absent. Permissions are passed through to
// the IdentityStore verbatim (no auth.ResourceKeys defaulting) — the store
// persists the permission strings as supplied. See authz-unification
// Stage 1.2.
// =============================================================================

// roleStore resolves the IdentityStore and the request tenant, failing closed
// with typed errors when either is unavailable.
func (r *rbacService) roleStore(ctx context.Context, op string) (*iam.IdentityStore, string, error) {
	tenantID := iam.TenantFromContext(ctx)
	if tenantID == "" {
		return nil, "", errors.NewMissingTenantContextError(op)
	}

	is, err := r.svc.GetIdentityStore(ctx)
	if err != nil {
		return nil, "", &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   op,
			Err:  err,
		}
	}
	if is == nil {
		return nil, "", &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   op,
			Err:  &api.ServiceNotConfiguredError{Service: "identity store"},
		}
	}

	return is, tenantID, nil
}

// findRoleIDByName resolves a role name to its identifier within a tenant,
// through the store's by-name LOOKUP. It returns a typed NotFoundError when no
// role with that name exists, and an internal error when the store could not be
// asked — two conditions callers must tell apart.
//
// It used to list the tenant's roles and scan the page. That was wrong against
// the store the server actually configures: `ListOptions{Limit: 0}` becomes
// qrdb's `PageSize: 0`, which qrdb reads as its DEFAULT page of 100 rather than
// "all rows", so every role past the first hundred was invisible here — a
// revocation that could not run, a deletion that 404'd on a role that exists,
// and a duplicate-name check that passed for a name already taken
// (go-quicbaas#694, the same defect as #541 but on the live RBAC path).
func findRoleIDByName(ctx context.Context, is *iam.IdentityStore, tenantID, name, op string) (string, error) {
	role, err := is.GetRoleByName(ctx, tenantID, name)
	if err != nil {
		if iam.IsRoleNotFound(err) {
			return "", &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   op,
				Err:  &api.NotFoundError{Resource: "role", ID: name},
			}
		}
		return "", &errors.QBError{
			Code: errors.ErrInternal,
			Op:   op,
			Err:  err,
		}
	}
	return role.ID, nil
}

// resolveUserAndRole resolves the request user (by username) and role (by name)
// within the request tenant, returning the user ID and role ID. It fails closed
// with typed errors when either the username or the role name does not resolve.
// This is the shared body of AssignRole/RevokeRole.
func (r *rbacService) resolveUserAndRole(ctx context.Context, username, roleName, op string) (is *iam.IdentityStore, tenantID, userID, roleID string, err error) {
	if username == "" || roleName == "" {
		return nil, "", "", "", &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   op,
			Err:  &api.InvalidInputError{Message: "username and role name are required"},
		}
	}
	is, tenantID, err = r.roleStore(ctx, op)
	if err != nil {
		return nil, "", "", "", err
	}
	user, err := is.GetUserByUsername(ctx, tenantID, username)
	if err != nil {
		return nil, "", "", "", &errors.QBError{
			Code: errors.ErrNotFound,
			Op:   op,
			Err:  &api.NotFoundError{Resource: "user", ID: username},
		}
	}
	roleID, err = findRoleIDByName(ctx, is, tenantID, roleName, op)
	if err != nil {
		return nil, "", "", "", err
	}
	return is, tenantID, user.ID, roleID, nil
}

// AssignRole binds a named role to a user (by username) through the qrdb-backed
// IdentityStore, so the binding lands in the SAME qrdb user record the policy
// decision point reads.
func (r *rbacService) AssignRole(ctx context.Context, req *AuthAssignRoleRequest) (*AuthAssignRoleResponse, error) {
	const op = "service.AssignRole"

	is, tenantID, userID, roleID, err := r.resolveUserAndRole(ctx, req.Username, req.RoleName, op)
	if err != nil {
		return nil, err
	}

	if err := is.AssignRole(ctx, tenantID, userID, roleID); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   op,
			Err:  err,
		}
	}

	return &AuthAssignRoleResponse{
		Success: true,
		Message: "Role " + req.RoleName + " assigned to user " + req.Username,
	}, nil
}

// RevokeRole unbinds a named role from a user (by username) through the
// qrdb-backed IdentityStore.
func (r *rbacService) RevokeRole(ctx context.Context, req *AuthRevokeRoleRequest) (*AuthRevokeRoleResponse, error) {
	const op = "service.RevokeRole"

	is, tenantID, userID, roleID, err := r.resolveUserAndRole(ctx, req.Username, req.RoleName, op)
	if err != nil {
		return nil, err
	}

	if err := is.RevokeRole(ctx, tenantID, userID, roleID); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   op,
			Err:  err,
		}
	}

	return &AuthRevokeRoleResponse{
		Success: true,
		Message: "Role " + req.RoleName + " revoked from user " + req.Username,
	}, nil
}

// CreateRole creates a tenant-scoped role in the IdentityStore. Permissions are
// stored verbatim.
func (r *rbacService) CreateRole(ctx context.Context, req *AuthCreateRoleRequest) (*AuthCreateRoleResponse, error) {
	const op = "service.CreateRole"

	if req.RoleName == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   op,
			Err:  &api.RequiredFieldError{Field: "role name"},
		}
	}

	is, tenantID, err := r.roleStore(ctx, op)
	if err != nil {
		return nil, err
	}

	// Reject duplicate role names within the tenant.
	//
	// The three outcomes are distinct and all three matter: the name resolves
	// (refuse the duplicate), the tenant holds no such role (proceed), or the
	// store could not be asked (refuse the create). Only the middle case may
	// continue — admitting a create because the lookup FAILED would let a store
	// outage produce the duplicate this check exists to prevent.
	switch _, err := findRoleIDByName(ctx, is, tenantID, req.RoleName, op); {
	case err == nil:
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   op,
			Err:  &api.InvalidInputError{Message: "role already exists: " + req.RoleName},
		}
	case !errors.IsNotFound(err):
		return nil, err
	}

	role, err := is.CreateRole(ctx, tenantID, iam.CreateRoleOptions{
		Name:        req.RoleName,
		Description: "Custom role: " + req.RoleName,
		Permissions: req.Permissions,
	})
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   op,
			Err:  err,
		}
	}

	return &AuthCreateRoleResponse{
		RoleID:    role.ID,
		Timestamp: time.Now().UnixNano(),
	}, nil
}

// DeleteRole removes a tenant-scoped role from the IdentityStore. The request
// carries the role name, which is resolved to its identifier first.
func (r *rbacService) DeleteRole(ctx context.Context, req *AuthDeleteRoleRequest) (*AuthDeleteRoleResponse, error) {
	const op = "service.DeleteRole"

	if req.RoleName == "" {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   op,
			Err:  &api.RequiredFieldError{Field: "role name"},
		}
	}

	is, tenantID, err := r.roleStore(ctx, op)
	if err != nil {
		return nil, err
	}

	roleID, err := findRoleIDByName(ctx, is, tenantID, req.RoleName, op)
	if err != nil {
		return nil, err
	}

	if err := is.DeleteRole(ctx, tenantID, roleID); err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   op,
			Err:  err,
		}
	}

	return &AuthDeleteRoleResponse{
		Success: true,
		Message: "Role " + req.RoleName + " deleted successfully",
	}, nil
}

// ListRoles returns the tenant-scoped roles from the IdentityStore, applying
// the requested pagination window.
func (r *rbacService) ListRoles(ctx context.Context, req *AuthListRolesRequest) (*AuthListRolesResponse, error) {
	const op = "service.ListRoles"

	is, tenantID, err := r.roleStore(ctx, op)
	if err != nil {
		return nil, err
	}

	opts := iam.ListOptions{
		Limit:  int(req.Limit),
		Offset: int(req.Offset),
	}

	roles, err := is.ListRoles(ctx, tenantID, opts)
	if err != nil {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   op,
			Err:  err,
		}
	}

	roleInfos := make([]*RoleInfo, 0, len(roles))
	for _, role := range roles {
		roleInfos = append(roleInfos, &RoleInfo{
			RoleID:      role.ID,
			RoleName:    role.Name,
			Permissions: role.Permissions,
			CreatedAt:   role.Created.UnixNano(),
		})
	}

	return &AuthListRolesResponse{
		Roles:      roleInfos,
		TotalCount: uint64(len(roleInfos)),
	}, nil
}
