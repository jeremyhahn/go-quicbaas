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

//go:build !iam

package service

import (
	"context"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// RBAC role STORE — non-IAM build.
//
// The role store is the qrdb-backed IdentityStore, which is only compiled into
// IAM builds. Without it there is no place to persist roles, so every verb
// fails closed with a typed not-configured error. There is deliberately NO
// fall-back to any legacy in-memory role manager (the fork was deleted in
// the authz-unification work; role decisions are qrdb-owned).
// =============================================================================

// roleStoreNotConfigured returns the typed error used by all role-store verbs
// in non-IAM builds.
func roleStoreNotConfigured(op string) error {
	return &errors.QBError{
		Code: errors.ErrConfiguration,
		Op:   op,
		Err:  &api.ServiceNotConfiguredError{Service: "identity store"},
	}
}

// CreateRole is not available without the IAM-backed identity store.
func (r *rbacService) CreateRole(_ context.Context, _ *AuthCreateRoleRequest) (*AuthCreateRoleResponse, error) {
	return nil, roleStoreNotConfigured("service.CreateRole")
}

// DeleteRole is not available without the IAM-backed identity store.
func (r *rbacService) DeleteRole(_ context.Context, _ *AuthDeleteRoleRequest) (*AuthDeleteRoleResponse, error) {
	return nil, roleStoreNotConfigured("service.DeleteRole")
}

// ListRoles is not available without the IAM-backed identity store.
func (r *rbacService) ListRoles(_ context.Context, _ *AuthListRolesRequest) (*AuthListRolesResponse, error) {
	return nil, roleStoreNotConfigured("service.ListRoles")
}
