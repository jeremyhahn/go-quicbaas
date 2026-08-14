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

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// deadAuthWebAuthnPrefix is the path prefix that was listed on BOTH pre-auth
// allowlists while matching no mounted route. WebAuthn is mounted per tenant
// (/api/v1/iam/{tenant_id}/webauthn in iam_xkms_webauthn_mount.go), never
// under /api/v1/auth/webauthn, so the entry exempted nothing today — but as a
// PREFIX it pre-authorized any future mount beneath it, including a
// registration leg that must stay gated.
const deadAuthWebAuthnPrefix = "/api/v1/auth/webauthn"

// TestAuthWebAuthnPrefix_MatchesNoMountedRoute proves the premise of the
// deletion against the real chi route tree: no route is mounted at or beneath
// /api/v1/auth/webauthn, so removing the allowlist entry cannot make a reachable
// endpoint unreachable.
func TestAuthWebAuthnPrefix_MatchesNoMountedRoute(t *testing.T) {
	for _, route := range walkMountedRoutes(t) {
		method, path := route[0], route[1]
		assert.Falsef(t,
			path == deadAuthWebAuthnPrefix || strings.HasPrefix(path, deadAuthWebAuthnPrefix+"/"),
			"%s %s is mounted beneath %s; the allowlist entry was NOT dead and must be "+
				"restored (or the route re-homed under the per-tenant webauthn mounts)",
			method, path, deadAuthWebAuthnPrefix)
	}
}

// TestAuthWebAuthnPrefix_NotOnEitherAllowlist asserts the dead prefix is gone
// from BOTH pre-auth allowlists, so a future mount beneath it is gated by
// default instead of inheriting a public exemption. Health-probe precedent:
// TestPublicEndpointLists_HealthEntriesAgree.
func TestAuthWebAuthnPrefix_NotOnEitherAllowlist(t *testing.T) {
	assert.NotContains(t, publicEndpoints, deadAuthWebAuthnPrefix,
		"dead prefix %s must be gone from publicEndpoints", deadAuthWebAuthnPrefix)
	assert.NotContains(t, defaultDenyPublicPrefixes, deadAuthWebAuthnPrefix,
		"dead prefix %s must be gone from defaultDenyPublicPrefixes", deadAuthWebAuthnPrefix)

	// A hypothetical future mount beneath the deleted prefix must be gated under
	// both postures. These are exactly the paths the prefix would have exempted.
	for _, path := range []string{
		deadAuthWebAuthnPrefix,
		deadAuthWebAuthnPrefix + "/register",
		deadAuthWebAuthnPrefix + "/register/begin",
		deadAuthWebAuthnPrefix + "/registration/begin",
		deadAuthWebAuthnPrefix + "/login/begin",
	} {
		assert.Falsef(t, isAuthzPublicEndpoint(path),
			"%s must not be pre-auth public under the per-group gates", path)
		assert.Falsef(t, isDefaultDenyPublic(path),
			"%s must not be pre-auth public under default-deny", path)
	}
}

// TestTenantWebAuthnLoginRemainsPublic is the non-regression half: deleting the
// dead prefix must not disturb the WebAuthn login legs that ARE public. They are
// admitted through preAuthRouteTemplates (authz_public_routes.go), whose variable
// tenant segment a literal prefix cannot express — which is why the /api/v1/auth
// entry was never what made them reachable.
func TestTenantWebAuthnLoginRemainsPublic(t *testing.T) {
	for _, path := range []string{
		"/api/v1/iam/acme/webauthn/login/begin",
		"/api/v1/iam/acme/webauthn/login/finish",
	} {
		assert.Truef(t, isAuthzPublicEndpoint(path),
			"%s must stay pre-auth public under the per-group gates", path)
		assert.Truef(t, isDefaultDenyPublic(path),
			"%s must stay pre-auth public under default-deny", path)
	}
}

// TestTenantWebAuthnRegistrationStaysGated pins the other half of the WebAuthn
// surface: the ENROLLMENT legs bind a new authenticator to an identity and must
// remain gated under both postures. A resurrected /api/v1/auth/webauthn prefix,
// or a broadened per-tenant template, would show up here.
func TestTenantWebAuthnRegistrationStaysGated(t *testing.T) {
	for _, path := range []string{
		// The deleted legacy /api/v1/tenants mount must never regain a
		// pre-auth exemption.
		"/api/v1/tenants/acme/webauthn/register/begin",
		"/api/v1/tenants/acme/webauthn/login/begin",
		"/api/v1/iam/acme/webauthn/registration/begin",
		"/api/v1/iam/acme/webauthn/registration/finish",
	} {
		assert.Falsef(t, isAuthzPublicEndpoint(path),
			"%s enrolls an authenticator and must stay gated", path)
		assert.Falsef(t, isDefaultDenyPublic(path),
			"%s enrolls an authenticator and must stay gated", path)
	}
}
