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
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/role"
	"github.com/jeremyhahn/go-qrdb/pkg/tenant"
)

// singleGrantRoleServicer resolves every role to exactly one permission, so
// go-qrdb's real RBAC authorizer decides a principal that holds only granted.
type singleGrantRoleServicer struct {
	role.RoleServicer
	granted permission.Permission
}

func (r *singleGrantRoleServicer) ResolvePermissions(context.Context, []string) ([]permission.Permission, error) {
	return []permission.Permission{r.granted}, nil
}

// authorizeAuditDeleteRoute runs the real RBAC authorizer for the frozen
// route permission of DELETE /api/v1/iam/audit against a principal holding
// only granted, in the principal's own tenant.
func authorizeAuditDeleteRoute(t *testing.T, granted permission.Permission) error {
	t.Helper()
	resource, action, ok := routePermissionForPath("/api/v1/iam/audit", http.MethodDelete)
	require.True(t, ok, "DELETE /api/v1/iam/audit resolved no frozen permission")

	authorizer := rbac.New(&singleGrantRoleServicer{granted: granted})
	identity := &qrdbauth.Identity{Subject: "audit-caller", TenantID: "tenant-a", Roles: []string{"admin"}}
	ctx := tenant.WithTenantID(context.Background(), "tenant-a")
	return authorizer.Authorize(ctx, identity, resource, action)
}

// TestAuditDeleteRoute_RequiresAuditDelete pins issue #1748: pruning audit
// history is a delete, so the route requires audit:delete, the same verb the
// MCP quicbaas_iam_audit_delete tool is gated on.
func TestAuditDeleteRoute_RequiresAuditDelete(t *testing.T) {
	resource, action, ok := routePermissionForPath("/api/v1/iam/audit", http.MethodDelete)
	require.True(t, ok)
	assert.Equal(t, "audit:delete", resource+":"+action)

	assert.NoError(t, authorizeAuditDeleteRoute(t, "audit:delete"))
}

// TestAuditDeleteRoute_QueryOnlyRefused proves a principal that may only
// query audit events is refused at the route, before the handler runs.
func TestAuditDeleteRoute_QueryOnlyRefused(t *testing.T) {
	assert.Error(t, authorizeAuditDeleteRoute(t, "audit:query"))
}
