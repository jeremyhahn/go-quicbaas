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
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
)

// DataPlaneAuthError reports which step of provisioning or logging in the
// data-plane principal failed.
type DataPlaneAuthError struct {
	// Step names the failed step, e.g. "create role".
	Step string

	// TenantID is the data-plane tenant, empty before it is created.
	TenantID string

	// Cause is the underlying failure.
	Cause error
}

func (e *DataPlaneAuthError) Error() string {
	if e.TenantID == "" {
		return "data-plane auth: " + e.Step + ": " + e.Cause.Error()
	}
	return "data-plane auth: " + e.Step + " in tenant " + e.TenantID + ": " + e.Cause.Error()
}

func (e *DataPlaneAuthError) Unwrap() error { return e.Cause }

// dataPlanePrincipal is the enrolled data-plane user of one cluster: the
// client cert it logs in with and its current bearer.
type dataPlanePrincipal struct {
	tenantID string
	certPEM  []byte
	keyPEM   []byte
	token    pivCachedToken
}

// dataPlaneAuth memoises one principal per base URL, so a suite process
// provisions its tenant once and only logs in again when the bearer expires.
// The mutex is held across enrollment so concurrent tests cannot enroll twice.
var dataPlaneAuth = struct {
	sync.Mutex
	principals map[string]*dataPlanePrincipal
}{principals: make(map[string]*dataPlanePrincipal)}

// AuthDataPlane returns a bearer for the data-plane principal of the cluster
// at baseURL: a user of a tenant provisioned for this suite process whose only
// role grants dataplane.Permissions. Data-plane RPCs and routes refuse a
// caller with no bearer, so every suite that reads or writes entities acts as
// this principal.
func AuthDataPlane(t *testing.T, baseURL string) string {
	t.Helper()
	bearer, err := dataPlaneBearer(context.Background(), baseURL)
	if err != nil {
		t.Fatalf("AuthDataPlane(%q): %v", baseURL, err)
	}
	return bearer
}

// DataPlaneGRPCCredentials returns per-RPC credentials that send the
// data-plane bearer of the first REST endpoint's cluster on every call.
func DataPlaneGRPCCredentials(t *testing.T) credentials.PerRPCCredentials {
	t.Helper()
	creds, err := dataplane.NewBearerCredentials(dataPlaneTokenSource(t))
	if err != nil {
		t.Fatalf("DataPlaneGRPCCredentials: %v", err)
	}
	return creds
}

// NewDataPlaneHTTPClient returns NewHTTPClient's client with every request
// carrying the data-plane bearer of the first REST endpoint's cluster.
func NewDataPlaneHTTPClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	client := NewHTTPClient(timeout)
	transport, err := dataplane.NewBearerTransport(dataPlaneTokenSource(t), client.Transport)
	if err != nil {
		t.Fatalf("NewDataPlaneHTTPClient: %v", err)
	}
	client.Transport = transport
	return client
}

// dataPlaneTokenSource enrolls the principal up front, so a provisioning
// failure fails the test with its cause rather than surfacing later as a
// refused call, and then refreshes the bearer on demand.
func dataPlaneTokenSource(t *testing.T) dataplane.TokenSource {
	t.Helper()
	baseURL := GetRESTEndpoints()[0]
	AuthDataPlane(t, baseURL)
	return func(ctx context.Context) (string, error) {
		return dataPlaneBearer(ctx, baseURL)
	}
}

func dataPlaneBearer(ctx context.Context, baseURL string) (string, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	dataPlaneAuth.Lock()
	defer dataPlaneAuth.Unlock()

	principal, ok := dataPlaneAuth.principals[baseURL]
	if !ok {
		enrolled, err := enrollDataPlanePrincipal(ctx, baseURL)
		if err != nil {
			return "", err
		}
		principal = enrolled
		dataPlaneAuth.principals[baseURL] = principal
	}
	if time.Now().Before(principal.token.ExpiresAt) {
		return principal.token.Bearer, nil
	}

	bearer, expiresIn, err := postLoginWithCert(ctx, baseURL, principal.certPEM, principal.keyPEM, pivCAFile())
	if err != nil {
		return "", &DataPlaneAuthError{Step: "PIV login", TenantID: principal.tenantID, Cause: err}
	}
	principal.token = pivCachedToken{Bearer: bearer, ExpiresAt: bearerExpiry(expiresIn)}
	return bearer, nil
}

// enrollDataPlanePrincipal has the system admin provision a tenant for this
// suite process (ProvisionTenant); that tenant's admin then creates a role in
// it holding exactly dataplane.Permissions and a PIV user bound to that role. The tenant is unique per process so suite packages
// running against one cluster never share or collide on a principal.
func enrollDataPlanePrincipal(ctx context.Context, baseURL string) (*dataPlanePrincipal, error) {
	systemBearer, _, err := authPIV(ctx, baseURL, pivCertPath(), pivKeyPath(), pivCAFile())
	if err != nil {
		return nil, &DataPlaneAuthError{Step: "system admin login", Cause: err}
	}

	// The system admin only provisions the tenant. Everything inside it is
	// done by the tenant's own admin, whose bearer is bound to the tenant.
	tenantID := dataplane.TenantPrefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	tenant, err := ProvisionTenant(ctx, baseURL, tenantID, systemBearer)
	if err != nil {
		return nil, &DataPlaneAuthError{Step: "provision tenant", TenantID: tenantID, Cause: err}
	}

	role := map[string]interface{}{
		"id":          dataplane.RoleID,
		"name":        dataplane.RoleID,
		"permissions": dataplane.Permissions,
	}
	if _, err := postJSON(ctx, baseURL+"/api/v1/tenants/"+tenantID+"/roles", tenant.AdminBearer, role); err != nil {
		return nil, &DataPlaneAuthError{Step: "create role", TenantID: tenantID, Cause: err}
	}

	userID, certPEM, keyPEM, err := enrollTenantPIVUser(ctx, baseURL, tenantID, tenant.AdminBearer)
	if err != nil {
		return nil, &DataPlaneAuthError{Step: "enroll PIV user", TenantID: tenantID, Cause: err}
	}

	binding := map[string]interface{}{"role_id": dataplane.RoleID}
	if _, err := postJSON(ctx, baseURL+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/roles", tenant.AdminBearer, binding); err != nil {
		return nil, &DataPlaneAuthError{Step: "bind role to user " + userID, TenantID: tenantID, Cause: err}
	}

	return &dataPlanePrincipal{tenantID: tenantID, certPEM: certPEM, keyPEM: keyPEM}, nil
}

// bearerExpiry refreshes a minute before the server's expiry, or after 15
// minutes when the login response states none.
func bearerExpiry(expiresIn int) time.Time {
	if expiresIn > 60 {
		return time.Now().Add(time.Duration(expiresIn-60) * time.Second)
	}
	return time.Now().Add(15 * time.Minute)
}
