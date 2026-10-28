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
	"net/http"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"google.golang.org/grpc/credentials"
)

// tenantOperatorName names the tenant operator in its errors.
const tenantOperatorName = "tenant operator"

// tenantOperatorAuth memoises the tenant operator of each cluster.
var tenantOperatorAuth = newPrincipalRegistry(tenantOperatorName, enrollTenantOperator, loginWithPIVCA)

// TenantOperatorGRPCCredentials returns per-RPC credentials that send the
// tenant operator's bearer of the first REST endpoint's cluster on every gRPC
// call. The tenant operator is a user of the tenant principal's tenant bound
// to go-qrdb's built-in Operator role, which holds the KV, batch, transaction,
// time-series and vector grants; the tenant's Admin role holds none of them.
// A suite that reads or writes those acts as this principal.
func TenantOperatorGRPCCredentials(t *testing.T) credentials.PerRPCCredentials {
	t.Helper()
	return tenantOperatorAuth.grpcCredentials(t)
}

// NewTenantOperatorHTTPClient returns NewHTTPClient's client with every
// request carrying the tenant operator's bearer of the first REST endpoint's
// cluster.
func NewTenantOperatorHTTPClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	return tenantOperatorAuth.httpClient(t, timeout)
}

// TenantOperatorHTTPClient is NewTenantOperatorHTTPClient for code with no
// test to fail, such as a probe that returns its error.
func TenantOperatorHTTPClient(ctx context.Context, timeout time.Duration) (*http.Client, error) {
	return tenantOperatorAuth.newHTTPClient(ctx, timeout)
}

// AuthTenantOperator returns the tenant operator's bearer for baseURL's
// cluster, for a client that takes a token rather than an *http.Client, such
// as the SDK's quicbaas.WithAuthToken.
func AuthTenantOperator(t *testing.T, baseURL string) string {
	t.Helper()
	return tenantOperatorAuth.mustBearer(t, baseURL)
}

// enrollTenantOperator has the tenant principal, the admin of this suite
// process's tenant, enrol a PIV user in that tenant and bind it to go-qrdb's
// built-in Operator role. The tenant is provisioned first if it is not yet.
func enrollTenantOperator(ctx context.Context, baseURL string) (*enrolledPrincipal, error) {
	adminBearer, err := tenantPrincipalAuth.bearer(ctx, baseURL)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: tenantOperatorName, Step: "tenant admin login", Cause: err}
	}
	tenantID := tenantPrincipalAuth.tenantID(baseURL)

	userID, certPEM, keyPEM, err := enrollTenantPIVUser(ctx, baseURL, tenantID, adminBearer)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: tenantOperatorName, Step: "enroll PIV user", TenantID: tenantID, Cause: err}
	}

	// The tenant is fresh, so its Operator role carries the identifier go-qrdb
	// seeds it under.
	binding := map[string]interface{}{"role_id": qrdbsdk.SystemRoleID(tenantID, qrdbsdk.RoleNameOperator)}
	if _, err := postJSON(ctx, baseURL+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/roles", adminBearer, binding); err != nil {
		return nil, &PrincipalAuthError{Principal: tenantOperatorName, Step: "bind Operator role to user " + userID, TenantID: tenantID, Cause: err}
	}

	return &enrolledPrincipal{tenantID: tenantID, certPEM: certPEM, keyPEM: keyPEM}, nil
}

// forgetTenant is the tenantDeleter of a principal that lives in another
// principal's tenant: that principal's release deletes the tenant, so this one
// only forgets its enrolment.
func forgetTenant(context.Context, string, string) error {
	return nil
}
