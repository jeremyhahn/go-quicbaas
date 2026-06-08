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

package platformtoken

// AuthorizedContext is the verified identity a downstream service acts on after
// a platform token (and, where applicable, its DPoP proof) has been validated.
// It is the bridge between the token contract and the service's own
// authorization: scopes map to RBAC permissions and tenant_id sets row-level
// security.
//
// Scope catalog convention: scopes are the qrdb-canonical colon
// "resource:action", e.g. "keys:get", "tenant_barrier:init". The scp claim
// carries the subject's qrdb-RESOLVED effective permissions, so a service
// grants a capability when the token's scp set contains the corresponding
// permission. A token lacking the required permission is denied (403).
type AuthorizedContext struct {
	// Subject is the stable user/service id (sub); used for audit.
	Subject string
	// TenantID is the tenant the request operates within. Downstream services
	// set this as the row-level-security tenant (e.g. app.current_tenant).
	TenantID string
	// Scopes is the authoritative granted scope set (from scp).
	Scopes []string
	// Roles is optional supplemental RBAC input (from roles).
	Roles []string
	// Confirmation is the sender-constraint binding present on the token, if any.
	Confirmation *Confirmation
}

// Authorize projects validated claims into an AuthorizedContext. It is a pure
// transformation; the token must already be verified.
func Authorize(claims *Claims) *AuthorizedContext {
	return &AuthorizedContext{
		Subject:      claims.Subject,
		TenantID:     claims.TenantID,
		Scopes:       claims.scopes(),
		Roles:        claims.Roles,
		Confirmation: claims.Confirmation,
	}
}

// HasScope reports whether the authorized context was granted scope.
func (a *AuthorizedContext) HasScope(scope string) bool {
	for _, s := range a.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// HasAnyScope reports whether any of the supplied scopes was granted.
func (a *AuthorizedContext) HasAnyScope(scopes ...string) bool {
	for _, want := range scopes {
		if a.HasScope(want) {
			return true
		}
	}
	return false
}

// HasRole reports whether the supplemental roles claim contains role.
func (a *AuthorizedContext) HasRole(role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// RequireScope enforces that the context was granted scope, returning a typed
// ScopeDeniedError (which the caller maps to 403) when it was not. This is the
// scope -> RBAC enforcement helper downstream services call per protected
// operation.
func (a *AuthorizedContext) RequireScope(scope string) error {
	if a.HasScope(scope) {
		return nil
	}
	return &ScopeDeniedError{Required: scope}
}

// RequireAnyScope enforces that at least one of scopes was granted. It returns
// a ScopeDeniedError naming the first required scope when none match.
func (a *AuthorizedContext) RequireAnyScope(scopes ...string) error {
	if a.HasAnyScope(scopes...) {
		return nil
	}
	required := ""
	if len(scopes) > 0 {
		required = scopes[0]
	}
	return &ScopeDeniedError{Required: required}
}
