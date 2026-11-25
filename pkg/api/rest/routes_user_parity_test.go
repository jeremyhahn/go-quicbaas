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

import "testing"

// =============================================================================
// go-quicbaas#1700 — the REST half of tenant user create parity.
//
// `iam user create` reaches REST as POST /api/v1/tenants/{tenant_id}/users.
// When the PEP mapper was frozen that route took its permission from its path
// prefix (tenants:create), which go-qrdb rbac treats as cross-scope, so only a
// system-tenant identity could create a user over REST while a tenant admin
// could over gRPC, QUIC, the Unix socket and MCP.
//
// The grant is duplicated here ON PURPOSE (pkg/api/rest must not import
// pkg/api/grpc), as TestRunnersForgesPermissionsMatchOtherTransports does. The
// gRPC side is pinned by pkg/api/grpc/authz_user_parity_test.go.
// =============================================================================

// TestUserCreatePermissionMatchesGRPC pins POST .../users/ to users:create,
// the permission /quicbaas.iam.v1.IAMService/CreateUser enforces on gRPC
// (mirroring /qrdb.v1.QRDBService/CreateUser). Tenant isolation stays with
// urlTenant: every caller, the system tenant included, acts only in its own
// tenant.
func TestUserCreatePermissionMatchesGRPC(t *testing.T) {
	const key = "POST /api/v1/tenants/{tenant_id}/users/"
	want := frozenPermission{auth: RouteAuthPermission, resource: "users", action: "create"}

	got, ok := frozenRoutePermissions[key]
	if !ok {
		t.Fatalf("route %q has no frozen permission entry", key)
	}
	if got != want {
		t.Errorf("route %q is gated %s:%s, but /quicbaas.iam.v1.IAMService/CreateUser is %s:%s on gRPC; "+
			"the grant must not depend on which transport carried the call",
			key, got.resource, got.action, want.resource, want.action)
	}
}
