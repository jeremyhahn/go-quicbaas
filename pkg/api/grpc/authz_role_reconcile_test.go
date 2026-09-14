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

package grpc

import "testing"

// roleReconcileGrant is the ONE canonical answer to "what does a caller need
// to converge a tenant's built-in roles onto the running build".
//
// It is deliberately the role-create family and not a verb of its own. A
// dedicated roles:reconcile would be absent from every role seeded before it
// existed, so no principal would hold the grant needed to repair that exact
// absence — the drift the verb exists to fix would lock out its own remedy.
// Reusing roles:create grants nothing new: a caller able to define a role can
// already set the permission sets this converges.
//
// A future change that "tidies" this into its own verb reintroduces
// go-quicbaas#573 for every tenant seeded before the tidy-up, which is why the
// value is pinned here rather than left to the mapping table alone.
var roleReconcileGrant = map[string]qrdbToolResourceLike{
	"/quicbaas.iam.v1.IAMService/ReconcileRoles": {"roles", "create"},
}

// TestRoleReconcileGRPCPermission pins the gRPC gate to the canonical grant.
// TestGRPCPermissions_ProtoOptionMatchesAuthzMapping separately proves the
// proto annotation agrees with the table, so the two gRPC enforcement points
// cannot drift from each other either.
func TestRoleReconcileGRPCPermission(t *testing.T) {
	for method, want := range roleReconcileGrant {
		got, ok := grpcMethodPermissions[method]
		if !ok {
			t.Errorf("method %q has no permission mapping: under RequireAuthz it is default-denied, "+
				"so the only repair for drifted built-in roles would be unreachable over gRPC", method)
			continue
		}
		if got.resource != want.resource || got.action != want.action {
			t.Errorf("method %q is gated %s:%s, want %s:%s; a verb of its own would be missing from "+
				"every role seeded before it existed, leaving nobody able to run the repair",
				method, got.resource, got.action, want.resource, want.action)
		}
	}
}
