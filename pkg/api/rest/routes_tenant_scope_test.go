//go:build rest

package rest

import (
	"context"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/permscope"
)

// tenantSelfServicePrefixes are the route families a tenant's own admin
// manages after provisioning, so none of them may require a permission
// go-qrdb confines to the system tenant. Provisioning, entitlements and
// quotas stay with the system admin.
var tenantSelfServicePrefixes = []string{
	"/api/v1/tenants/{tenant_id}/apps/",
	"/api/v1/tenants/{tenant_id}/policies/",
	"/api/v1/tenants/{tenant_id}/providers/",
	"/api/v1/tenants/{tenant_id}/roles/",
	"/api/v1/tenants/{tenant_id}/users/",
}

func isTenantSelfServiceRoute(pattern string) bool {
	for _, prefix := range tenantSelfServicePrefixes {
		if strings.HasPrefix(pattern, prefix) {
			return true
		}
	}
	return false
}

func TestRoutes_TenantRoutesRequireNoCrossScopePermission(t *testing.T) {
	resolver, err := permscope.New()
	require.NoError(t, err)

	for _, spec := range Routes() {
		key := spec.Method + " " + spec.Pattern
		if !isTenantSelfServiceRoute(spec.Pattern) || spec.Resource == "" {
			continue
		}
		perm := qrdbsdk.Permission(spec.Resource + ":" + spec.Action)
		scope, err := resolver.Scope(context.Background(), perm)
		require.NoErrorf(t, err, "%s requires %s, which go-qrdb declares no scope for", key, perm)
		assert.NotEqualf(t, qrdbsdk.PermissionScopeCross, scope,
			"%s requires %s, which go-qrdb confines to the system tenant, so the tenant's admin is refused", key, perm)
	}
}

func TestRoutes_TenantIdentityRoutesRequireGoQrdbIdentityPermissions(t *testing.T) {
	want := map[string]string{
		"POST /api/v1/tenants/{tenant_id}/roles/":                         "roles:create",
		"POST /api/v1/tenants/{tenant_id}/roles/reconcile":                "roles:create",
		"PUT /api/v1/tenants/{tenant_id}/roles/{role_id}":                 "roles:update",
		"POST /api/v1/tenants/{tenant_id}/users/":                         "users:create",
		"PUT /api/v1/tenants/{tenant_id}/users/{user_id}":                 "users:update",
		"POST /api/v1/tenants/{tenant_id}/users/{user_id}/roles":          "roles:assign",
		"POST /api/v1/tenants/{tenant_id}/apps/":                          "tenants:update",
		"PUT /api/v1/tenants/{tenant_id}/apps/{client_id}":                "tenants:update",
		"POST /api/v1/tenants/{tenant_id}/apps/{client_id}/rotate-secret": "tenants:update",
	}

	got := make(map[string]string, len(want))
	for _, spec := range Routes() {
		key := spec.Method + " " + spec.Pattern
		if _, ok := want[key]; ok {
			got[key] = spec.Resource + ":" + spec.Action
		}
	}
	assert.Equal(t, want, got)
}
