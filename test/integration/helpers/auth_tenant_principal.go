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
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
)

const (
	// tenantPrincipalName names the tenant principal in its errors.
	tenantPrincipalName = "tenant"

	// TenantPrincipalPrefix prefixes the tenant each suite process
	// provisions for its tenant principal.
	TenantPrincipalPrefix = "itest-tenant"
)

// tenantPrincipalAuth memoises the tenant principal of each cluster.
var tenantPrincipalAuth = newPrincipalRegistry(tenantPrincipalName, enrollTenantPrincipal, loginWithPIVCA)

// namedTenantPrincipals memoises, per name, a tenant principal of each
// cluster, each in a tenant of its own.
var namedTenantPrincipals = newNamedPrincipalRegistries(func(name string) *principalRegistry {
	return newPrincipalRegistry(tenantPrincipalName+" "+name, enrollNamedTenantPrincipal(name), loginWithPIVCA)
})

// AuthTenantPrincipal returns a bearer for the tenant principal of the
// cluster at baseURL: the admin of a tenant provisioned for this suite
// process, logged in by its PIV cert. Tenant-scoped services refuse a caller
// whose request binds no tenant, so a suite that drives secrets, KMS or any
// other tenant-scoped service its Admin role grants acts as this principal.
// The Admin role grants no KV, batch, transaction, time-series or vector
// verb: a suite that drives those acts as the tenant operator instead
// (TenantOperatorGRPCCredentials).
// A package that uses it calls ReleaseDataPlaneTenants from TestMain.
func AuthTenantPrincipal(t *testing.T, baseURL string) string {
	t.Helper()
	return tenantPrincipalAuth.mustBearer(t, baseURL)
}

// TenantPrincipalID returns the tenant of the tenant principal of the cluster
// at baseURL, provisioning it on first use. A request naming a tenant must
// name this one: a tenant-bound caller is refused for any other.
func TenantPrincipalID(t *testing.T, baseURL string) string {
	t.Helper()
	tenantPrincipalAuth.mustBearer(t, baseURL)
	return tenantPrincipalAuth.tenantID(baseURL)
}

// TenantGRPCCredentials returns per-RPC credentials that send the tenant
// principal's bearer of the first REST endpoint's cluster on every gRPC call,
// over TCP or the Unix socket.
func TenantGRPCCredentials(t *testing.T) credentials.PerRPCCredentials {
	t.Helper()
	return tenantPrincipalAuth.grpcCredentials(t)
}

// TenantGRPCClientOption returns the grpcclient option that sends the tenant
// principal's bearer on every call, so a client of tenant-scoped RPCs (KMS,
// DEK) acts in the tenant's own namespace.
func TenantGRPCClientOption(t *testing.T) grpcclient.ClientOption {
	t.Helper()
	return grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(TenantGRPCCredentials(t)))
}

// NewTenantHTTPClient returns NewHTTPClient's client with every request
// carrying the tenant principal's bearer of the first REST endpoint's cluster.
func NewTenantHTTPClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	return tenantPrincipalAuth.httpClient(t, timeout)
}

// TenantHTTPClient is NewTenantHTTPClient for code with no test to fail, such
// as TestMain setup that must act inside the tenant principal's tenant.
func TenantHTTPClient(ctx context.Context, timeout time.Duration) (*http.Client, error) {
	return tenantPrincipalAuth.newHTTPClient(ctx, timeout)
}

// NamedTenantPrincipalID returns the tenant ID of the tenant principal called
// name, enrolling it on baseURL first if needed. Each name is its own tenant,
// apart from the shared principal and from every other name, so a test that
// needs two tenants asks for two names. A name is lowercase letters, digits
// and hyphens; anything else fails the test.
func NamedTenantPrincipalID(t *testing.T, baseURL, name string) string {
	t.Helper()
	registry := namedTenantPrincipals.mustRegistry(t, name)
	registry.mustBearer(t, baseURL)
	return registry.tenantID(baseURL)
}

// NamedTenantGRPCCredentials returns gRPC per-RPC credentials that send the
// bearer of the tenant principal called name.
func NamedTenantGRPCCredentials(t *testing.T, name string) credentials.PerRPCCredentials {
	t.Helper()
	return namedTenantPrincipals.mustRegistry(t, name).grpcCredentials(t)
}

// NewNamedTenantHTTPClient returns an HTTP client that authenticates every
// request as the tenant principal called name.
func NewNamedTenantHTTPClient(t *testing.T, name string, timeout time.Duration) *http.Client {
	t.Helper()
	return namedTenantPrincipals.mustRegistry(t, name).httpClient(t, timeout)
}

// enrollTenantPrincipal has the system admin provision a tenant for this
// suite process (ProvisionTenant), whose admin cert is the principal's login
// credential. The tenant is unique per process so suite packages running
// against one cluster never share or collide on a principal.
func enrollTenantPrincipal(ctx context.Context, baseURL string) (*enrolledPrincipal, error) {
	return enrollTenantPrincipalWithPrefix(ctx, baseURL, TenantPrincipalPrefix)
}

// enrollNamedTenantPrincipal returns the enroller of the principal called
// name, whose tenant ID carries the name after TenantPrincipalPrefix.
func enrollNamedTenantPrincipal(name string) principalEnroller {
	return func(ctx context.Context, baseURL string) (*enrolledPrincipal, error) {
		return enrollTenantPrincipalWithPrefix(ctx, baseURL, TenantPrincipalPrefix+"-"+name)
	}
}

// enrollTenantPrincipalWithPrefix provisions a fresh tenant named prefix plus
// a unique suffix, as the system admin, and returns its admin certificate.
func enrollTenantPrincipalWithPrefix(ctx context.Context, baseURL, prefix string) (*enrolledPrincipal, error) {
	systemBearer, _, err := authPIV(ctx, baseURL, pivCertPath(), pivKeyPath(), pivCAFile())
	if err != nil {
		return nil, &PrincipalAuthError{Principal: tenantPrincipalName, Step: "system admin login", Cause: err}
	}

	tenantID := prefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	tenant, err := ProvisionTenant(ctx, baseURL, tenantID, systemBearer)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: tenantPrincipalName, Step: "provision tenant", TenantID: tenantID, Cause: err}
	}
	return &enrolledPrincipal{tenantID: tenantID, certPEM: tenant.AdminCertPEM, keyPEM: tenant.AdminKeyPEM}, nil
}
