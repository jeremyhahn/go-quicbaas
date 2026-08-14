//go:build rest

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

package rest

import "strings"

// This file holds the pre-auth route allowlist for routes whose path carries a
// variable segment (a tenant id), which the two literal-prefix allowlists —
// publicEndpoints (authz_middleware.go) and defaultDenyPublicPrefixes
// (authz_default_deny.go) — cannot express. Both isAuthzPublicEndpoint and
// isDefaultDenyPublic consult it, so every enforcement posture (legacy
// Authorize, EnforceTokenBinding, AuthorizeViaValidator, EnforceDefaultDeny)
// treats these routes identically.
//
// Unlike the prefix allowlists, matching here is WHOLE-PATH and segment-exact:
// the wildcard matches exactly one non-empty tenant segment and every other
// segment must match literally. A sibling route under the same group is never
// widened.

// preAuthWildcardSegment is the template segment that matches exactly one
// non-empty path segment (the tenant id). Any "{name}" segment is a wildcard;
// this constant names the common one so the templates below read like routes.
const preAuthWildcardSegment = "{tenant_id}"

// preAuthClientIDSegment is the wildcard segment matching an OAuth2 client id.
const preAuthClientIDSegment = "{client_id}"

// preAuthRouteTemplates are the tenant-scoped routes that MUST be reachable
// without a bearer token.
//
// Only the WebAuthn LOGIN ceremony is listed. WebAuthn IS the authentication
// mechanism: the caller holds no token until FinishLogin mints one, so gating
// login/begin behind a bearer makes browser sign-in unreachable. The ceremony
// is self-authorizing — BeginLogin returns a server-generated challenge bound
// to a stored ceremony session, and FinishLogin only succeeds when the
// presented assertion is signed by a private key already enrolled for that
// user and echoes that exact challenge, origin and relying-party id.
//
// The single ceremony mount listed is the canonical xkms-native surface under
// /api/v1/iam (used by the Go SDK REST transport). Browser sign-in (TypeScript
// SDK / SPA login view) rides the unified ceremony at
// /api/v1/auth/login/{begin,finish}, which is public via publicEndpoints.
//
// The sibling ENROLLMENT ceremonies are deliberately absent and stay gated:
//
//   - .../webauthn/registration/{begin,finish,status} (xkms mount) bind a new
//     authenticator to a caller-supplied identity with no proof of ownership,
//     so a public mount would let anyone enroll a passkey for any account.
//
// The pre-auth enrollment path is instead the invite-activation surface
// (/api/v1/iam/invites/{token}/activate/fido2/{begin,finish}), which is
// authorized by the one-time invite token in the URL and is already public via
// its own unwrapped mount plus the "/api/v1/iam/invites" default-deny prefix.
//
// The second entry class is the RFC 7592 client configuration endpoint. Its
// credential is the per-client registration access token (RFC 7592 §2), not a
// platform token: the registrant of a dynamic client holds only the token the
// registration response returned, so gating these three verbs behind the
// platform-token chain would make every legitimate 7592 flow impossible. The
// carve-out is safe because the registration access token is itself a
// fail-closed gate — 256 bits of entropy, stored only as a SHA-256 hash on the
// client record, compared in constant time against the hash for THAT client id,
// with no "accept any token" path (see iam.AppManager.AuthorizeRegistrationAccess).
//
// The sibling CREATE route (POST /oauth2/register, no client-id segment) is
// deliberately NOT listed: creating a client is a privileged operation and stays
// behind the authz chain plus its tenants:create PEP mapping. Whole-path,
// segment-exact matching guarantees the create route can never match a template
// that carries the {client_id} segment.
var preAuthRouteTemplates = []string{
	// Canonical xkms-native WebAuthn login ceremony
	// (iam_xkms_webauthn_mount.go).
	"/api/v1/iam/" + preAuthWildcardSegment + "/webauthn/login/begin",
	"/api/v1/iam/" + preAuthWildcardSegment + "/webauthn/login/finish",
	// Invitation activation. The activation IS the bootstrap: the invited user
	// holds a single-use token and no session, so these must be reachable
	// pre-auth.
	//
	// They are templates rather than a literal prefix because the tenant is a
	// path segment — invitations live in that tenant's own store, so the store
	// cannot be resolved without it. A literal "/api/v1/iam/invites" prefix
	// stopped matching when the tenant moved into the middle of the path, and
	// broadening it to "/api/v1/iam" would have opened every IAM route
	// pre-auth. Segment-wise matching keeps the wildcard confined to exactly
	// one segment.
	"/api/v1/iam/" + preAuthWildcardSegment + "/invites/" + preAuthWildcardSegment,
	"/api/v1/iam/" + preAuthWildcardSegment + "/invites/" + preAuthWildcardSegment + "/activate/fido2/begin",
	"/api/v1/iam/" + preAuthWildcardSegment + "/invites/" + preAuthWildcardSegment + "/activate/fido2/finish",
	"/api/v1/iam/" + preAuthWildcardSegment + "/invites/" + preAuthWildcardSegment + "/activate/piv",
	// RFC 7592 client configuration endpoint (iam.go -> IAMHandler). One
	// template covers GET/PUT/DELETE; the handlers demand the registration
	// access token for every verb.
	"/oauth2/register/" + preAuthClientIDSegment,
}

// preAuthRoutePatterns is preAuthRouteTemplates pre-split into path segments at
// package initialization so a request-time match is a bounded segment
// comparison with no allocation beyond the request path split.
var preAuthRoutePatterns = compilePreAuthRouteTemplates(preAuthRouteTemplates)

// compilePreAuthRouteTemplates splits each template into its path segments.
func compilePreAuthRouteTemplates(templates []string) [][]string {
	patterns := make([][]string, 0, len(templates))
	for _, template := range templates {
		patterns = append(patterns, splitPathSegments(template))
	}
	return patterns
}

// splitPathSegments splits an absolute path into its segments, ignoring a
// trailing slash. It returns nil for a path that is not absolute (so a
// malformed path can never match a template).
func splitPathSegments(path string) []string {
	path = strings.TrimSuffix(path, "/")
	if !strings.HasPrefix(path, "/") {
		return nil
	}
	return strings.Split(path[1:], "/")
}

// isPreAuthRoute reports whether path is one of the tenant-scoped routes that
// bypass authorization entirely. It is whole-path exact (modulo the single
// tenant wildcard), so no neighbouring route is exempted by accident.
func isPreAuthRoute(path string) bool {
	segments := splitPathSegments(path)
	if len(segments) == 0 {
		return false
	}
	for _, pattern := range preAuthRoutePatterns {
		if matchPreAuthPattern(pattern, segments) {
			return true
		}
	}
	return false
}

// matchPreAuthPattern reports whether the request segments satisfy the compiled
// template: equal length, literal segments equal, and every wildcard segment
// non-empty.
func matchPreAuthPattern(pattern, segments []string) bool {
	if len(pattern) != len(segments) {
		return false
	}
	for i, want := range pattern {
		if isPreAuthWildcard(want) {
			if segments[i] == "" {
				return false
			}
			continue
		}
		if segments[i] != want {
			return false
		}
	}
	return true
}

// isPreAuthWildcard reports whether a template segment is a "{name}" wildcard
// matching exactly one non-empty path segment.
func isPreAuthWildcard(segment string) bool {
	return len(segment) > 2 && segment[0] == '{' && segment[len(segment)-1] == '}'
}
