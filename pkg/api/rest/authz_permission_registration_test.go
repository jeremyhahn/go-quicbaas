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
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaaspermission "github.com/jeremyhahn/go-quicbaas/pkg/permission"
)

// registeredPermissionSet returns every permission the qrdb policy decision
// point can resolve, from BOTH contributors to the single registry:
//
//   - go-qrdb's own families, registered by init() in go-qrdb/pkg/permission;
//   - the broker-domain families QuicBaaS contributes through
//     qrdbsdk.RegisterPermissions (pkg/permission), which production performs
//     once at the composition root in server.NewServer.
//
// The helper calls quicbaaspermission.Register itself rather than importing
// pkg/server (which imports this package — an import cycle). Register is
// idempotent, so calling it here is free and keeps the assertion honest.
func registeredPermissionSet(t *testing.T) map[string]struct{} {
	t.Helper()

	quicbaaspermission.Register()

	registered := make(map[string]struct{})
	for _, perm := range qrdbsdk.AllPermissions() {
		registered[string(perm)] = struct{}{}
	}
	require.NotEmpty(t, registered, "qrdb permission registry is empty")
	return registered
}

// TestRESTPermissions_EveryMappingIsRegistered asserts every permission the
// FROZEN route table (routes.go Routes(), routes_permissions_generated.go)
// carries actually exists in the permission registry. Mapping a route onto
// an unregistered permission is strictly worse than leaving it unmapped:
// go-qrdb's role store rejects an unregistered permission on role
// create/update (pkg/role.validatePermissions -> InvalidPermissionError), so
// NO role can ever hold the grant and the route is permanently 403 under
// RequireAuthz — yet the table makes it look wired, so it survives review.
//
// This supersedes the pre-freeze version of this test, which swept the live
// PEP mapper (authz_pep_mapping.go pepMapping/pepSpecialRules) directly; that
// mapper was deleted once its output was frozen into
// routes_permissions_generated.go (module_support.go's local platform-token
// fallback and go-qrdb's module registration both consume the frozen
// Resource/Action directly, so the live mapper had no runtime reader left).
func TestRESTPermissions_EveryMappingIsRegistered(t *testing.T) {
	t.Parallel()

	registered := registeredPermissionSet(t)

	specs := Routes()
	require.NotEmpty(t, specs, "route table is empty; the sweep is vacuous")

	var permissionRoutes int
	for _, spec := range specs {
		if spec.Auth != RouteAuthPermission {
			continue
		}
		permissionRoutes++
		_, ok := registered[spec.Resource+":"+spec.Action]
		assert.Truef(t, ok,
			"%s %s maps to %s:%s, which neither go-qrdb nor pkg/permission registers; "+
				"no role can hold that grant, so the route is unreachable despite looking "+
				"wired. Register the permission in pkg/permission or fix the frozen table.",
			spec.Method, spec.Pattern, spec.Resource, spec.Action)
	}
	require.NotEmpty(t, permissionRoutes, "no RouteAuthPermission routes found; the sweep is vacuous")
}

// TestRESTPermissions_AttestationRoutesResolve pins the attestation surface
// end to end through the frozen route table: each mounted route resolves to
// the registered permission the other transports use for the same verb, and
// the read/write split holds (verification is read-strength; driving the
// host TPM is not).
func TestRESTPermissions_AttestationRoutesResolve(t *testing.T) {
	t.Parallel()

	registered := registeredPermissionSet(t)

	cases := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{"verify", "POST", "/api/v1/attestation/verify", string(quicbaaspermission.AttestationVerify)},
		{"verify-chain", "POST", "/api/v1/attestation/verify-chain", string(quicbaaspermission.AttestationVerify)},
		{"attest-container", "POST", "/api/v1/attestation/attest-container", string(quicbaaspermission.AttestationAttest)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource, action, ok := routePermissionForPath(tc.path, tc.method)
			require.Truef(t, ok, "%s %s resolved no frozen permission", tc.method, tc.path)
			assert.Equal(t, tc.want, resource+":"+action)
			_, found := registered[resource+":"+action]
			assert.Truef(t, found, "permission %s:%s is not registered", resource, action)
		})
	}
}
