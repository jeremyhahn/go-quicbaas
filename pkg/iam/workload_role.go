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

package iam

import (
	"context"
	"fmt"
	"strings"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// WorkloadRoleBinder binds a built-in role to a service's workload principal,
// its SPIFFE ID, in go-qrdb IAM. It is the service plane's RBAC grant
// (grants.RoleBinder).
//
// The principal is a MACHINE identity, like a fleet node's: a user record
// whose username is the SPIFFE ID, the CommonName of the service's SVID (the
// subject go-qrdb's mTLS authenticator resolves roles by), active, restricted
// to mTLS, with no email login key and no group membership. It is not
// reachable by any human login ceremony. A revoke removes the role; the
// record stays, holding no role.
type WorkloadRoleBinder struct {
	Store *IdentityStore
}

// WorkloadPrincipalError reports a principal that is not a SPIFFE ID.
type WorkloadPrincipalError struct {
	Principal string
}

func (e *WorkloadPrincipalError) Error() string {
	return fmt.Sprintf("workload principal %q is not a spiffe:// ID", e.Principal)
}

// AssignRole binds the role named roleName in tenantID to principal, creating
// the principal's record on first use. Binding a role it already holds is a
// no-op.
func (b *WorkloadRoleBinder) AssignRole(ctx context.Context, tenantID, principal, roleName string) error {
	roleID, err := b.roleID(ctx, tenantID, principal, roleName)
	if err != nil {
		return err
	}
	user, err := b.Store.userStorage.GetUserByUsername(ctx, tenantID, principal)
	if err != nil && !dberrors.IsNotFound(err) {
		return err
	}
	if err == nil && user != nil {
		return b.bind(ctx, user, roleID)
	}
	now := time.Now().UTC()
	user = &User{
		TenantID:    tenantID,
		Username:    principal,
		Name:        principal,
		Roles:       []string{roleID},
		AuthMethods: []string{AuthMethodMTLS},
		Lifecycle:   LifecycleActive,
		Created:     now,
		Updated:     now,
	}
	if err := b.Store.userStorage.CreateUser(ctx, user); err != nil {
		if !isAlreadyExistsError(err) {
			return err
		}
		// A concurrent grant created the record: bind onto it.
		existing, getErr := b.Store.userStorage.GetUserByUsername(ctx, tenantID, principal)
		if getErr != nil {
			return getErr
		}
		return b.bind(ctx, existing, roleID)
	}
	return nil
}

// RevokeRole removes the role named roleName from principal. A principal
// with no record, or one that does not hold the role, is already revoked.
func (b *WorkloadRoleBinder) RevokeRole(ctx context.Context, tenantID, principal, roleName string) error {
	roleID, err := b.roleID(ctx, tenantID, principal, roleName)
	if err != nil {
		return err
	}
	user, err := b.Store.userStorage.GetUserByUsername(ctx, tenantID, principal)
	if dberrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(user.Roles))
	for _, id := range user.Roles {
		if id != roleID {
			kept = append(kept, id)
		}
	}
	if len(kept) == len(user.Roles) {
		return nil
	}
	user.Roles = kept
	user.Updated = time.Now().UTC()
	return b.Store.userStorage.UpdateUser(ctx, user)
}

func (b *WorkloadRoleBinder) roleID(ctx context.Context, tenantID, principal, roleName string) (string, error) {
	if b == nil || b.Store == nil {
		return "", fmt.Errorf("workload role binder: no identity store")
	}
	if !strings.HasPrefix(principal, "spiffe://") {
		return "", &WorkloadPrincipalError{Principal: principal}
	}
	r, err := b.Store.GetRoleByName(ctx, tenantID, roleName)
	if err != nil {
		return "", fmt.Errorf("role %q in tenant %s: %w", roleName, tenantID, err)
	}
	return r.ID, nil
}

// bind adds roleID to an existing principal's roles. The record must be a
// workload's own: mTLS-only with no email login key, so a grant can never
// widen a human account that happens to share the name.
func (b *WorkloadRoleBinder) bind(ctx context.Context, user *User, roleID string) error {
	if user.Email != "" || len(user.AuthMethods) != 1 || user.AuthMethods[0] != AuthMethodMTLS {
		return fmt.Errorf("user %q is not a workload principal", user.Username)
	}
	for _, id := range user.Roles {
		if id == roleID {
			return nil
		}
	}
	user.Roles = append(user.Roles, roleID)
	user.Updated = time.Now().UTC()
	return b.Store.userStorage.UpdateUser(ctx, user)
}
