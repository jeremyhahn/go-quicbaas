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
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
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

// DataPlaneGRPCCredentialsE is DataPlaneGRPCCredentials for a goroutine
// other than the test goroutine: it returns the enrollment error instead of
// failing the test, because t.FailNow off the test goroutine exits it
// through runtime.Goexit and skips the caller's cleanup (#1593).
func DataPlaneGRPCCredentialsE(ctx context.Context) (credentials.PerRPCCredentials, error) {
	return dataPlaneAuth.newGRPCCredentials(ctx)
}

// NewDataPlaneHTTPClient returns NewHTTPClient's client with every request
// carrying the data-plane bearer of the first REST endpoint's cluster.
func NewDataPlaneHTTPClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	return dataPlaneAuth.httpClient(t, timeout)
}

// enrollDataPlanePrincipal has the system admin provision a tenant for this
// suite process (ProvisionTenant), then create a role in it holding exactly
// dataplane.Permissions and a PIV user bound to that role.
// The tenant is unique per process so suite packages running against one
// cluster never share or collide on a principal.
func enrollDataPlanePrincipal(ctx context.Context, baseURL string) (*enrolledPrincipal, error) {
	systemBearer, _, err := authPIV(ctx, baseURL, pivCertPath(), pivKeyPath(), pivCAFile())
	if err != nil {
		return nil, &PrincipalAuthError{Principal: dataPlanePrincipalName, Step: "system admin login", Cause: err}
	}

	// The system admin provisions the tenant, then creates the data-plane
	// role, enrolls its PIV user and binds the role (go-quicbaas#2212).
	tenantID := dataplane.TenantPrefix + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := ProvisionTenant(ctx, baseURL, tenantID, systemBearer); err != nil {
		return nil, &PrincipalAuthError{Principal: dataPlanePrincipalName, Step: "provision tenant", TenantID: tenantID, Cause: err}
	}

	roleID, err := ensureDataPlaneRole(ctx, baseURL, tenantID, systemBearer)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: dataPlanePrincipalName, Step: "create role", TenantID: tenantID, Cause: err}
	}

	userID, certPEM, keyPEM, err := enrollTenantPIVUser(ctx, baseURL, tenantID, systemBearer)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: dataPlanePrincipalName, Step: "enroll PIV user", TenantID: tenantID, Cause: err}
	}

	binding := map[string]interface{}{"role_id": roleID}
	if _, err := postJSON(ctx, baseURL+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/roles", systemBearer, binding); err != nil {
		return nil, &PrincipalAuthError{Principal: dataPlanePrincipalName, Step: "bind role to user " + userID, TenantID: tenantID, Cause: err}
	}

	return &enrolledPrincipal{tenantID: tenantID, certPEM: certPEM, keyPEM: keyPEM}, nil
}

// DataPlaneRoleMismatchError reports that the role id the data-plane
// principal needs already names a role that is not the data-plane role of its
// tenant, so reusing it would bind the principal to the wrong grant.
type DataPlaneRoleMismatchError struct {
	// RoleID is the id that already exists.
	RoleID string

	// TenantID is the tenant the role was wanted in.
	TenantID string

	// OwnerTenantID is the tenant the existing role belongs to.
	OwnerTenantID string

	// Permissions are the existing role's permissions.
	Permissions []string
}

func (e *DataPlaneRoleMismatchError) Error() string {
	return "role " + e.RoleID + " already exists in tenant " + e.OwnerTenantID +
		" with permissions [" + strings.Join(e.Permissions, " ") + "], want tenant " +
		e.TenantID + " with [" + strings.Join(dataplane.Permissions, " ") + "]"
}

// ensureDataPlaneRole creates the data-plane role in tenantID and returns its
// id. A 409 means the id is already claimed: the existing role is reused when
// it is this tenant's role holding exactly dataplane.Permissions (an earlier
// attempt that created it before failing a later step), and refused otherwise.
func ensureDataPlaneRole(ctx context.Context, baseURL, tenantID, bearer string) (string, error) {
	roleID := dataplane.TenantRoleID(tenantID)
	role := map[string]interface{}{
		"id":          roleID,
		"name":        dataplane.RoleID,
		"permissions": dataplane.Permissions,
	}
	rolesURL := baseURL + "/api/v1/tenants/" + tenantID + "/roles"
	_, err := postJSON(ctx, rolesURL, bearer, role)
	var status *AuthBadStatusError
	if err == nil || !errors.As(err, &status) || status.Status != http.StatusConflict {
		return roleID, err
	}

	body, gerr := getJSON(ctx, rolesURL+"/"+url.PathEscape(roleID), bearer)
	if gerr != nil {
		return "", errors.Join(err, gerr)
	}
	var existing struct {
		TenantID    string   `json:"tenant_id"`
		Permissions []string `json:"permissions"`
	}
	if uerr := json.Unmarshal(body, &existing); uerr != nil {
		return "", &AuthError{Op: "decode role", URL: rolesURL + "/" + roleID, Cause: uerr}
	}
	want := slices.Clone(dataplane.Permissions)
	got := slices.Clone(existing.Permissions)
	slices.Sort(want)
	slices.Sort(got)
	if existing.TenantID != tenantID || !slices.Equal(got, want) {
		return "", &DataPlaneRoleMismatchError{
			RoleID:        roleID,
			TenantID:      tenantID,
			OwnerTenantID: existing.TenantID,
			Permissions:   existing.Permissions,
		}
	}
	return roleID, nil
}
