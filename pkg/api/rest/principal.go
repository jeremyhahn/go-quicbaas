//go:build rest

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
	"net/http"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// requireSubject extracts the canonical authenticated principal that the
// authz middleware chain injects via injectPrincipal ->
// auth.SubjectIntoContext. It is the single identity source for handlers on
// the hardened surface; handlers must never derive identity from query
// parameters or ad-hoc context keys. When no authenticated Subject is
// present it writes a 401 response and returns ok == false.
func requireSubject(w http.ResponseWriter, r *http.Request) (interfaces.Subject, bool) {
	subject, ok := auth.SubjectFromContext(r.Context())
	if !ok || !subject.Authenticated || subject.ID == "" {
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrUnauthorized, "user not authenticated"))
		return interfaces.Subject{}, false
	}
	return subject, true
}

// requireTenantSubject extracts the authenticated Subject and additionally
// requires it to carry a tenant binding. Tenant-scoped handlers (sessions)
// use it so the tenant always comes from the verified principal, never from
// the request. When the Subject carries no tenant it writes a 400 response
// and returns ok == false.
func requireTenantSubject(w http.ResponseWriter, r *http.Request) (interfaces.Subject, bool) {
	subject, ok := requireSubject(w, r)
	if !ok {
		return interfaces.Subject{}, false
	}
	// A system-tenant principal (empty TenantID or the literal
	// qrdbsdk.SystemTenantID) carries no per-tenant binding at all — it is
	// the structurally privileged platform scope, not an isolated tenant
	// named "system" — so it fails the same "tenant ID is required" gate as
	// an empty TenantID rather than being silently scoped into a
	// tenant/system/ namespace. This is one face of the platform-wide
	// operator-isolation rule stated in iam.ResolveRequestTenant: a
	// system-tenant principal never gains implicit reach into a tenant's
	// data — cross-tenant access exists only behind permissions qrdb
	// registers ScopeCross.
	if qrdbsdk.IsSystemTenant(subject.TenantID) {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return interfaces.Subject{}, false
	}
	return subject, true
}

// queryIdentityMatches enforces that explicit user_id / tenant_id query
// parameters, when present, agree with the authenticated principal. The
// parameters are NOT an identity source — direct HTTP callers may still emit
// them on the self-scoped session surface, so silently ignoring a mismatch
// would return the caller's own data under another user's requested identity.
// Cross-user access must use the admin surface
// (/api/v1/tenants/{tenant_id}/users/{user_id}/sessions), which is
// path-scoped and PEP-mapped. Writes a 403 response and returns false on
// mismatch.
func queryIdentityMatches(w http.ResponseWriter, r *http.Request, subject interfaces.Subject) bool {
	if userID := r.URL.Query().Get("user_id"); userID != "" && !subjectUserMatches(subject, userID) {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrForbidden, "user_id parameter does not match authenticated user"))
		return false
	}
	if tenantID := r.URL.Query().Get("tenant_id"); tenantID != "" && tenantID != subject.TenantID {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrForbidden, "tenant_id parameter does not match authenticated tenant"))
		return false
	}
	return true
}

// subjectUserMatches reports whether an explicit user identifier refers to
// the authenticated principal. The platform mints two first-class
// identifiers for every user — the stable subject ID (the token `sub`, the
// broker's external user ID) and the human-readable username claim — and a
// legitimate caller may self-reference with either representation. Any other
// value names a foreign identity and must be rejected by the caller.
func subjectUserMatches(subject interfaces.Subject, userID string) bool {
	if userID == subject.ID {
		return true
	}
	return subject.Username != "" && userID == subject.Username
}
