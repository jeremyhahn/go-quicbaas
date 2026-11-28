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
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// WorkloadRoleBinder binds a built-in role to a service's workload principal,
// its SPIFFE ID, in go-qrdb IAM. It is the service plane's RBAC grant
// (grants.RoleBinder).
//
// The principal is a MACHINE identity, like a fleet node's: a user record
// whose username is the SPIFFE ID, the CommonName of the service's SVID (the
// subject go-qrdb's mTLS authenticator resolves roles by), active, restricted
// to mTLS, with no email login key and no group membership. Its login key is
// an SPKI certificate binding from the service's current SVID, the key
// go-qrdb's mTLS login resolves it by. It is not reachable by any human
// login ceremony. A revoke removes the role; the
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

// WorkloadSVIDError reports an SVID that cannot be the workload principal's
// login key: none, or one that does not carry the principal's SPIFFE ID.
type WorkloadSVIDError struct {
	Principal string
	Reason    string
}

func (e *WorkloadSVIDError) Error() string {
	return fmt.Sprintf("workload principal %q: %s", e.Principal, e.Reason)
}

// AssignRole binds the role named roleName in tenantID to principal, creating
// the principal's record on first use. svid is the service's current SVID:
// its SPKI binding is the principal's login key, and an existing record's
// bindings converge to exactly it. Binding a role it already holds under the
// same SVID is a no-op.
func (b *WorkloadRoleBinder) AssignRole(ctx context.Context, tenantID, principal, roleName string, svid *x509.Certificate) error {
	roleID, err := b.roleID(ctx, tenantID, principal, roleName)
	if err != nil {
		return err
	}
	binding, err := svidBinding(principal, svid)
	if err != nil {
		return err
	}
	user, err := b.Store.userStorage.GetUserByUsername(ctx, tenantID, principal)
	if err != nil && !dberrors.IsNotFound(err) {
		return err
	}
	if err == nil && user != nil {
		return b.bind(ctx, user, roleID, binding)
	}
	now := time.Now().UTC()
	user = &User{
		TenantID:     tenantID,
		Username:     principal,
		Name:         principal,
		Roles:        []string{roleID},
		AuthMethods:  []string{AuthMethodMTLS},
		Lifecycle:    LifecycleActive,
		CertBindings: []qrdbsdk.CertBinding{binding},
		Created:      now,
		Updated:      now,
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
		return b.bind(ctx, existing, roleID, binding)
	}
	return nil
}

// svidBinding answers the SPKI certificate binding of svid, refusing a nil
// SVID or one whose URI SANs do not name principal.
func svidBinding(principal string, svid *x509.Certificate) (qrdbsdk.CertBinding, error) {
	if svid == nil {
		return qrdbsdk.CertBinding{}, &WorkloadSVIDError{Principal: principal, Reason: "no SVID"}
	}
	named := false
	for _, u := range svid.URIs {
		if u != nil && u.String() == principal {
			named = true
			break
		}
	}
	if !named {
		return qrdbsdk.CertBinding{}, &WorkloadSVIDError{Principal: principal, Reason: "the SVID does not carry the principal's SPIFFE ID"}
	}
	return qrdbsdk.CertBinding{
		Thumbprint: qrdbsdk.ComputeSPKIThumbprint(svid),
		Kind:       qrdbsdk.CertBindingSPKIThumbprint,
		NotAfter:   svid.NotAfter,
	}, nil
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

// bind converges an existing principal's certificate bindings to exactly
// binding and adds roleID to its roles. The record must be a workload's own: mTLS-only with no email login key, so a grant can never
// widen a human account that happens to share the name.
func (b *WorkloadRoleBinder) bind(ctx context.Context, user *User, roleID string, binding qrdbsdk.CertBinding) error {
	if user.Email != "" || len(user.AuthMethods) != 1 || user.AuthMethods[0] != AuthMethodMTLS {
		return fmt.Errorf("user %q is not a workload principal", user.Username)
	}
	changed := false
	if len(user.CertBindings) != 1 || !sameBinding(user.CertBindings[0], binding) {
		user.CertBindings = []qrdbsdk.CertBinding{binding}
		changed = true
	}
	if !hasRole(user.Roles, roleID) {
		user.Roles = append(user.Roles, roleID)
		changed = true
	}
	if !changed {
		return nil
	}
	user.Updated = time.Now().UTC()
	return b.Store.userStorage.UpdateUser(ctx, user)
}

func sameBinding(a, b qrdbsdk.CertBinding) bool {
	return a.Thumbprint == b.Thumbprint && a.Kind == b.Kind && a.NotAfter.Equal(b.NotAfter)
}

func hasRole(roles []string, roleID string) bool {
	for _, id := range roles {
		if id == roleID {
			return true
		}
	}
	return false
}
