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
// non-empty path segment (the tenant id).
const preAuthWildcardSegment = "{tenant_id}"

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
// Both mounts of the same ceremony are listed: the legacy IAMHandler surface
// under /api/v1/tenants (used by the TypeScript SDK and the SPA login view)
// and the canonical xkms-native surface under /api/v1/iam (used by the Go SDK
// REST transport).
//
// The sibling ENROLLMENT ceremonies are deliberately absent and stay gated:
//
//   - .../webauthn/register/{begin,finish} (IAMHandler) bind a new
//     authenticator to a caller-supplied "user_id" with no proof of ownership,
//     so a public mount would let anyone enroll a passkey for any account.
//   - .../webauthn/registration/{begin,finish,status} (xkms mount) are the same
//     enrollment capability on the canonical surface.
//
// The pre-auth enrollment path is instead the invite-activation surface
// (/api/v1/iam/invites/{token}/activate/fido2/{begin,finish}), which is
// authorized by the one-time invite token in the URL and is already public via
// its own unwrapped mount plus the "/api/v1/iam/invites" default-deny prefix.
var preAuthRouteTemplates = []string{
	// Legacy tenant-scoped WebAuthn login ceremony (iam.go -> IAMHandler).
	"/api/v1/tenants/" + preAuthWildcardSegment + "/webauthn/login/begin",
	"/api/v1/tenants/" + preAuthWildcardSegment + "/webauthn/login/finish",
	// Canonical xkms-native WebAuthn login ceremony
	// (iam_xkms_webauthn_mount.go).
	"/api/v1/iam/" + preAuthWildcardSegment + "/webauthn/login/begin",
	"/api/v1/iam/" + preAuthWildcardSegment + "/webauthn/login/finish",
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
// template: equal length, literal segments equal, and the wildcard segment
// non-empty.
func matchPreAuthPattern(pattern, segments []string) bool {
	if len(pattern) != len(segments) {
		return false
	}
	for i, want := range pattern {
		if want == preAuthWildcardSegment {
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
