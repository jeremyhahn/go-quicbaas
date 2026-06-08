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

//go:build iam

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

// findRoleIDByName resolves a role name to its identifier within a tenant.
// It returns a typed NotFoundError when no role with that name exists.
func findRoleIDByName(ctx context.Context, is *iam.IdentityStore, tenantID, name, op string) (string, error) {
	roles, err := is.ListRoles(ctx, tenantID, iam.ListOptions{})
	if err != nil {
		return "", &errors.QBError{
			Code: errors.ErrInternal,
			Op:   op,
			Err:  err,
		}
	}
	for _, role := range roles {
		if role.Name == name {
			return role.ID, nil
		}
	}
	return "", &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   op,
		Err:  &api.NotFoundError{Resource: "role", ID: name},
	}
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
	if _, err := findRoleIDByName(ctx, is, tenantID, req.RoleName, op); err == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   op,
			Err:  &api.InvalidInputError{Message: "role already exists: " + req.RoleName},
		}
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
