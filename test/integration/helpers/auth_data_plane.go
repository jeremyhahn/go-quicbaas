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

//go:build integration

package helpers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
)

// dataPlanePrincipalName names the data-plane principal in its errors.
const dataPlanePrincipalName = "data-plane"

// dataPlaneAuth memoises the data-plane principal of each cluster.
var dataPlaneAuth = newPrincipalRegistry(dataPlanePrincipalName, enrollDataPlanePrincipal, loginWithPIVCA)

// AuthDataPlane returns a bearer for the data-plane principal of the cluster
// at baseURL: a user of a tenant provisioned for this suite process whose only
// role grants dataplane.Permissions. Data-plane RPCs and routes refuse a
// caller with no bearer, so every suite that reads or writes entities acts as
// this principal.
func AuthDataPlane(t *testing.T, baseURL string) string {
	t.Helper()
	return dataPlaneAuth.mustBearer(t, baseURL)
}

// ReleaseDataPlaneTenants deletes the tenants AuthDataPlane,
// AuthTenantPrincipal and the named tenant principals provisioned for each cluster this process used. The
// tenants outlive every test, so no t.Cleanup can delete them: a package that
// uses either principal calls this from TestMain after m.Run. Releasing when
// nothing was provisioned is a no-op.
func ReleaseDataPlaneTenants() error {
	ctx, cancel := context.WithTimeout(context.Background(), tenantSetupTimeout)
	defer cancel()
	return errors.Join(
		dataPlaneAuth.release(ctx, deleteTenantAsSystemAdmin),
		tenantOperatorAuth.release(ctx, forgetTenant),
		tenantPrincipalAuth.release(ctx, deleteTenantAsSystemAdmin),
		namedTenantPrincipals.release(ctx, deleteTenantAsSystemAdmin),
	)
}

// DataPlaneGRPCCredentials returns per-RPC credentials that send the
// data-plane bearer of the first REST endpoint's cluster on every call.
func DataPlaneGRPCCredentials(t *testing.T) credentials.PerRPCCredentials {
	t.Helper()
	return dataPlaneAuth.grpcCredentials(t)
}

// NewDataPlaneHTTPClient returns NewHTTPClient's client with every request
// carrying the data-plane bearer of the first REST endpoint's cluster.
func NewDataPlaneHTTPClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	return dataPlaneAuth.httpClient(t, timeout)
}

// enrollDataPlanePrincipal has the system admin provision a tenant for this
// suite process (ProvisionTenant); that tenant's admin then creates a role in
// it holding exactly dataplane.Permissions and a PIV user bound to that role.
// The tenant is unique per process so suite packages running against one
// cluster never share or collide on a principal.
func enrollDataPlanePrincipal(ctx context.Context, baseURL string) (*enrolledPrincipal, error) {
	systemBearer, _, err := authPIV(ctx, baseURL, pivCertPath(), pivKeyPath(), pivCAFile())
	if err != nil {
		return nil, &PrincipalAuthError{Principal: dataPlanePrincipalName, Step: "system admin login", Cause: err}
	}

	// The system admin only provisions the tenant. Everything inside it is
	// done by the tenant's own admin, whose bearer is bound to the tenant.
	tenantID := dataplane.TenantPrefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	tenant, err := ProvisionTenant(ctx, baseURL, tenantID, systemBearer)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: dataPlanePrincipalName, Step: "provision tenant", TenantID: tenantID, Cause: err}
	}

	role := map[string]interface{}{
		"id":          dataplane.RoleID,
		"name":        dataplane.RoleID,
		"permissions": dataplane.Permissions,
	}
	if _, err := postJSON(ctx, baseURL+"/api/v1/tenants/"+tenantID+"/roles", tenant.AdminBearer, role); err != nil {
		return nil, &PrincipalAuthError{Principal: dataPlanePrincipalName, Step: "create role", TenantID: tenantID, Cause: err}
	}

	userID, certPEM, keyPEM, err := enrollTenantPIVUser(ctx, baseURL, tenantID, tenant.AdminBearer)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: dataPlanePrincipalName, Step: "enroll PIV user", TenantID: tenantID, Cause: err}
	}

	binding := map[string]interface{}{"role_id": dataplane.RoleID}
	if _, err := postJSON(ctx, baseURL+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/roles", tenant.AdminBearer, binding); err != nil {
		return nil, &PrincipalAuthError{Principal: dataPlanePrincipalName, Step: "bind role to user " + userID, TenantID: tenantID, Cause: err}
	}

	return &enrolledPrincipal{tenantID: tenantID, certPEM: certPEM, keyPEM: keyPEM}, nil
}
