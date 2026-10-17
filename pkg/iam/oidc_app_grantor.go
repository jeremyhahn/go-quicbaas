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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

var (
	// ErrScopeNotGrantable is the coded sentinel ScopeNotGrantableError
	// unwraps to: the caller asked to grant a permission it does not hold.
	ErrScopeNotGrantable = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.app.update",
		Err:  &IAMMessageError{Message: "application update grants a scope the caller does not hold"},
	}

	// ErrGrantorUnresolved is the coded sentinel GrantorUnresolvedError
	// unwraps to: the caller's permissions could not be resolved, so no
	// permission scope may be granted.
	ErrGrantorUnresolved = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.app.update",
		Err:  &IAMMessageError{Message: "application update grants scopes but the caller's permissions cannot be resolved"},
	}
)

// ScopeNotGrantableError reports that an application update submitted a
// permission scope the calling identity's qrdb effective permissions do not
// cover. The whole update is refused and the application is unchanged.
type ScopeNotGrantableError struct {
	// ClientID is the application the update targeted.
	ClientID string
	// Scope is the first submitted permission scope the caller does not hold.
	Scope string
}

// Error implements error.
func (e *ScopeNotGrantableError) Error() string {
	return "iam: caller may not grant scope " + e.Scope + " to client " + e.ClientID
}

// Unwrap returns ErrScopeNotGrantable so callers can match the coded sentinel.
func (e *ScopeNotGrantableError) Unwrap() error {
	return ErrScopeNotGrantable
}

// GrantorUnresolvedError reports that an application update carried
// permission scopes but the caller's authority could not be resolved: no
// identity in the context, no qrdb RBAC client, or an RBAC error. The gate
// fails closed, so the update is refused and the application is unchanged.
type GrantorUnresolvedError struct {
	// ClientID is the application the update targeted.
	ClientID string
	// Reason says which part of the resolution was missing.
	Reason string
	// Cause is the RBAC error, when there was one.
	Cause error
}

// Error implements error.
func (e *GrantorUnresolvedError) Error() string {
	msg := "iam: cannot grant scopes to client " + e.ClientID + ": " + e.Reason
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns ErrGrantorUnresolved, so the refusal is coded permission
// denied, followed by the RBAC cause when there was one.
func (e *GrantorUnresolvedError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrGrantorUnresolved}
	}
	return []error{ErrGrantorUnresolved, e.Cause}
}

// AppManagerOption configures an AppManager.
type AppManagerOption func(*AppManager)

// WithGrantorPermissions sets the qrdb RBAC resolver the grantor gate reads
// the calling identity's effective permissions from. Without it, an
// application update that submits permission scopes is refused.
func WithGrantorPermissions(resolver PermissionResolver) AppManagerOption {
	return func(m *AppManager) {
		m.grantor = resolver
	}
}

// authorizeScopeGrant is the grantor gate for application updates. Every
// permission scope in scopes, including any the application already holds,
// must be covered by the calling identity's qrdb effective permissions in
// tenantID; the first one that is not refuses the update. OIDC protocol
// scopes confer no authority and pass, as they do in ResolveGrantedScopes.
// The gate fails closed: no identity, no RBAC client or an RBAC error refuses
// any update that carries a permission scope.
func (m *AppManager) authorizeScopeGrant(ctx context.Context, tenantID, clientID string, scopes []string) error {
	wanted := PermissionScopes(scopes)
	if len(wanted) == 0 {
		return nil
	}

	identity := grantorIdentity(ctx)
	if identity == nil {
		return &GrantorUnresolvedError{ClientID: clientID, Reason: "no caller identity"}
	}
	if m.grantor == nil {
		return &GrantorUnresolvedError{ClientID: clientID, Reason: "no permission resolver"}
	}
	rbac := m.grantor.RBACClient()
	if rbac == nil {
		return &GrantorUnresolvedError{ClientID: clientID, Reason: "no RBAC client"}
	}

	held, err := listEffectivePermissions(ctx, rbac, tenantID, identity)
	if err != nil {
		return &GrantorUnresolvedError{ClientID: clientID, Reason: "permission resolution failed", Cause: err}
	}
	heldSet := make(map[string]struct{}, len(held))
	for _, p := range held {
		heldSet[p] = struct{}{}
	}
	for _, want := range wanted {
		if !scopeCoveredByGrant(heldSet, want) {
			return &ScopeNotGrantableError{ClientID: clientID, Scope: want}
		}
	}
	return nil
}

// grantorIdentity returns the calling identity as qrdb's RBAC sees it: the
// verified qrdb identity when the transport bound one, otherwise the
// authenticated quicbaas Subject the transport projected (the same
// projection the server's PEP authorizer hands qrdb). It returns nil when the
// context carries neither.
func grantorIdentity(ctx context.Context) *qrdbtransport.Identity {
	if identity := qrdbsdk.IdentityFromContext(ctx); identity != nil && identity.Subject != "" {
		return &qrdbtransport.Identity{
			Subject:  identity.Subject,
			TenantID: identity.TenantID,
			Roles:    identity.Roles,
			Metadata: identity.Metadata,
		}
	}
	subject, bound := auth.SubjectFromContext(ctx)
	if !bound || !subject.Authenticated || subject.ID == "" {
		return nil
	}
	return &qrdbtransport.Identity{
		Subject:  subject.ID,
		TenantID: subject.TenantID,
		Roles:    subject.Roles,
	}
}
