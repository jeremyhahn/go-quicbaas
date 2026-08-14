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
// REST PEP can produce actually exists in the permission registry. It sweeps
// BOTH halves of the PEP: the CRUD-shaped prefix table (pepMapping, each of
// whose four verbs is a distinct permission) and the fully-expanded special
// rule list (pepSpecialRules, which already folds in the barrier, ceremony,
// credential-wire, SSH, xkms and legacy-seal op tables).
//
// It is the REST half of the dead-endpoint guard whose MCP twin is
// TestMCPPermissions_EveryMappingIsRegistered. Mapping a route onto an
// unregistered permission is strictly worse than leaving it unmapped: go-qrdb's
// role store rejects an unregistered permission on role create/update
// (pkg/role.validatePermissions -> InvalidPermissionError), so NO role can ever
// hold the grant and the route is permanently 403 under RequireAuthz — yet the
// table makes it look wired, so it survives review.
func TestRESTPermissions_EveryMappingIsRegistered(t *testing.T) {
	t.Parallel()

	registered := registeredPermissionSet(t)

	check := func(where, resource, action string) {
		t.Helper()
		if resource == "" || action == "" {
			// A crudVerbs field left empty means the method is not routed for
			// that resource; actionForMethod reports ok=false and the PEP
			// denies. There is no permission to register.
			return
		}
		_, ok := registered[resource+":"+action]
		assert.Truef(t, ok,
			"REST %s maps to %s:%s, which neither go-qrdb nor pkg/permission registers; "+
				"no role can hold that grant, so the route is unreachable despite looking "+
				"wired. Register the permission in pkg/permission or fix the mapping.",
			where, resource, action)
	}

	for prefix, entry := range pepMapping {
		check(prefix+" [GET item]", entry.resource, entry.verbs.get)
		check(prefix+" [GET collection]", entry.resource, entry.verbs.list)
		check(prefix+" [POST/PUT/PATCH]", entry.resource, entry.verbs.create)
		check(prefix+" [DELETE]", entry.resource, entry.verbs.del)
	}

	require.NotEmpty(t, pepSpecialRules, "pepSpecialRules is empty; the sweep is vacuous")
	for _, rule := range pepSpecialRules {
		check(rule.method+" "+rule.prefix+rule.suffix, rule.resource, rule.action)
	}
}

// TestRESTPermissions_AttestationRoutesResolve pins the attestation surface end
// to end through the live PEP: each mounted route resolves to the registered
// permission the other transports use for the same verb, and the read/write
// split holds (verification is read-strength; driving the host TPM is not).
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
			resource, action, ok := mapRequestToPermission(tc.path, tc.method)
			require.Truef(t, ok, "%s %s resolved no permission", tc.method, tc.path)
			assert.Equal(t, tc.want, resource+":"+action)
			_, found := registered[resource+":"+action]
			assert.Truef(t, found, "permission %s:%s is not registered", resource, action)
		})
	}
}

// TestRESTPermissions_UnknownAttestationSubPathTakesStrongAction is the
// error-path twin: a sub-path under /api/v1/attestation with no special rule
// must inherit the coarse-entry write-strength action, never the read-strength
// one. A future route added without a rule therefore fails closed.
func TestRESTPermissions_UnknownAttestationSubPathTakesStrongAction(t *testing.T) {
	t.Parallel()

	resource, action, ok := mapRequestToPermission("/api/v1/attestation/some-future-verb", "POST")
	require.True(t, ok, "unmapped attestation sub-path must still resolve the coarse entry")
	assert.Equal(t, string(quicbaaspermission.AttestationAttest), resource+":"+action)
}
