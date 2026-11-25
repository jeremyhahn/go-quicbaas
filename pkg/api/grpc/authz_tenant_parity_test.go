package grpc

import "testing"

// tenantGrants is the REST-parity grant of each quicbaas.iam.v1.IAMService
// tenant verb the CLI calls over gRPC (pkg/api/rest/routes_permissions_generated.go).
// Unmapped, RequireAuthz refuses each one for every caller, so
// `iam tenant create` over gRPC fails with "method not authorized".
var tenantGrants = map[string]methodPermission{
	// POST /api/v1/tenants/
	"/quicbaas.iam.v1.IAMService/CreateTenant": {"tenants", "create"},
	// GET /api/v1/tenants/{tenant_id}
	"/quicbaas.iam.v1.IAMService/GetTenant": {"tenants", "read"},
	// GET /api/v1/tenants/
	"/quicbaas.iam.v1.IAMService/ListTenants": {"tenants", "list"},
	// PUT /api/v1/tenants/{tenant_id}
	"/quicbaas.iam.v1.IAMService/UpdateTenant": {"tenants", "create"},
	// DELETE /api/v1/tenants/{tenant_id}
	"/quicbaas.iam.v1.IAMService/DeleteTenant": {"tenants", "delete"},
}

func TestTenantGRPCResourceAndAction(t *testing.T) {
	requireGRPCGrants(t, tenantGrants)
}

func TestTenantGRPCAuthorization(t *testing.T) {
	requireGRPCGrantEnforced(t, tenantGrants)
}
