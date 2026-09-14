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
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// ScopeNotGrantedError reports that a client asked for a permission its
// application record was never granted. It is surfaced at the token endpoint
// as the RFC 6749 §5.2 "invalid_scope" error.
type ScopeNotGrantedError struct {
	// ClientID is the application that made the request.
	ClientID string
	// Scope is the first requested scope that no grant covers.
	Scope string
}

// Error implements the error interface.
func (e *ScopeNotGrantedError) Error() string {
	return "iam: client " + e.ClientID + " was not granted scope " + e.Scope
}

// ResolveGrantedScopes returns the scope set a token may carry for a client
// whose application record holds granted, when the client requested requested.
//
// It implements RFC 6749 §3.3 for a service principal: an empty request yields
// the whole grant, and a non-empty request yields exactly the requested scopes.
// A requested PERMISSION the grant does not cover is refused with a
// *ScopeNotGrantedError rather than silently dropped, so a misconfigured caller
// learns which entitlement it is missing instead of receiving a token that
// fails later at the resource server.
//
// A client whose application carries NO grant therefore receives no permissions
// at all, whatever it asks for. That is the point: before go-quicbaas#653 the
// token endpoint copied the request straight into the token, so the scp claim
// — which qrdb's RBAC authorizer unions in as effective permissions — was
// whatever the client named for itself.
//
// OIDC PROTOCOL SCOPES PASS THROUGH UNGRANTED. "openid", "profile", "email"
// and "offline_access" select claims and token shape; they confer no authority
// and no resource server maps them to a permission, so bounding them by a
// permission grant would refuse every ordinary OIDC client for no security
// gain. The two vocabularies are told apart structurally, by the colon that
// makes a qrdb permission a "resource:action" — the same split this provider's
// own scopes_supported metadata publishes.
//
// Coverage honors the same whole-segment "*" wildcards as qrdb's RBAC
// authorizer and platformtoken.AuthorizedContext.HasScope, so a grant of
// "orchestrator:*" covers "orchestrator:hook" exactly as the live PDP would
// read it. The permission is split with qrdb's own re-exported helpers, which
// keeps the separator and segment rules a single source of truth with the PDP.
func ResolveGrantedScopes(clientID string, granted, requested []string) ([]string, error) {
	grantSet := make(map[string]struct{}, len(granted))
	for _, g := range granted {
		if g = strings.TrimSpace(g); g != "" {
			grantSet[g] = struct{}{}
		}
	}

	if len(requested) == 0 {
		out := make([]string, 0, len(granted))
		for _, g := range granted {
			if g = strings.TrimSpace(g); g != "" {
				out = append(out, g)
			}
		}
		return dedupeScopes(out), nil
	}

	out := make([]string, 0, len(requested))
	for _, want := range requested {
		want = strings.TrimSpace(want)
		if want == "" {
			continue
		}
		if IsPermissionScope(want) && !scopeCoveredByGrant(grantSet, want) {
			return nil, &ScopeNotGrantedError{ClientID: clientID, Scope: want}
		}
		out = append(out, want)
	}
	return dedupeScopes(out), nil
}

// IsPermissionScope reports whether scope names a qrdb permission
// ("resource:action") rather than an OIDC protocol scope ("openid"). The colon
// is the whole distinction: qrdb's permission vocabulary is colon-separated
// and OIDC's registered scope values are single words.
func IsPermissionScope(scope string) bool {
	return strings.Contains(scope, ":")
}

// PermissionScopes returns the permission-shaped subset of scopes: the part of
// an issued scope set that carries authority and therefore belongs in the scp
// claim, which qrdb's RBAC authorizer unions into the identity's effective
// permissions.
func PermissionScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if IsPermissionScope(s) {
			out = append(out, s)
		}
	}
	return out
}

// scopeCoveredByGrant reports whether grantSet holds a permission that covers
// want, honoring whole-segment "*" wildcards on either component.
func scopeCoveredByGrant(grantSet map[string]struct{}, want string) bool {
	resource := qrdbsdk.PermissionResource(qrdbsdk.Permission(want))
	action := qrdbsdk.PermissionAction(qrdbsdk.Permission(want))
	candidates := [...]string{want, resource + ":*", "*:" + action, "*:*"}
	for _, c := range candidates {
		if _, ok := grantSet[c]; ok {
			return true
		}
	}
	return false
}

// dedupeScopes returns scopes with duplicates removed, order preserved.
func dedupeScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	out := scopes[:0]
	for _, s := range scopes {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
