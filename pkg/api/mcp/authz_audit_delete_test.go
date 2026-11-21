//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/tenant"
)

// authorizeAuditDeleteRoute runs go-qrdb's real RBAC authorizer for the
// quicbaas_iam_audit_delete route permission against a principal holding
// only granted, in the principal's own tenant.
func authorizeAuditDeleteRoute(t *testing.T, granted permission.Permission) error {
	t.Helper()
	resource, action, ok := getToolQRDBPermission("quicbaas_iam_audit_delete")
	require.True(t, ok, "quicbaas_iam_audit_delete has no route permission")

	authorizer := rbac.New(&secretsVaultRoleServicer{granted: granted})
	identity := &qrdbauth.Identity{Subject: "audit-caller", TenantID: "tenant-a", Roles: []string{"admin"}}
	ctx := tenant.WithTenantID(context.Background(), "tenant-a")
	return authorizer.Authorize(ctx, identity, resource, action)
}

// TestAuditDeleteRoute_RequiresAuditDelete pins issue #1748: pruning audit
// history is a delete, so the route permission is audit:delete, the same
// verb REST gates DELETE /api/v1/iam/audit on.
func TestAuditDeleteRoute_RequiresAuditDelete(t *testing.T) {
	resource, action, ok := getToolQRDBPermission("quicbaas_iam_audit_delete")
	require.True(t, ok)
	assert.Equal(t, "audit", resource)
	assert.Equal(t, "delete", action)

	assert.NoError(t, authorizeAuditDeleteRoute(t, "audit:delete"))
}

// TestAuditDeleteRoute_QueryOnlyRefused proves a principal that may only
// query audit events is refused at the route, before the executor runs.
func TestAuditDeleteRoute_QueryOnlyRefused(t *testing.T) {
	assert.Error(t, authorizeAuditDeleteRoute(t, "audit:query"))
}
