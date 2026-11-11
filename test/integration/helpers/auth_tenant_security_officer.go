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
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// EnrollTenantSecurityOfficer has admin, a provisioned tenant's admin, enrol a
// PIV user in that tenant and bind it to go-qrdb's built-in Security Officer
// role, and returns that user's session. go-qrdb grants role create, update
// and delete to the Security Officer and not to the tenant Admin, so a suite
// that manages roles acts as this principal. In the returned session's
// tenant, AdminUserID, AdminCertPEM and AdminKeyPEM are the officer's own.
func EnrollTenantSecurityOfficer(ctx context.Context, baseURL string, admin *TenantAdminSession) (*TenantAdminSession, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	tenantID := admin.Tenant().TenantID

	adminBearer, err := admin.Bearer(ctx)
	if err != nil {
		return nil, err
	}

	userID, certPEM, keyPEM, err := enrollTenantPIVUser(ctx, baseURL, tenantID, adminBearer)
	if err != nil {
		return nil, err
	}

	// The tenant is fresh, so its Security Officer role carries the identifier
	// go-qrdb seeds it under.
	binding := map[string]interface{}{"role_id": qrdbsdk.SystemRoleID(tenantID, qrdbsdk.RoleNameSO)}
	if _, err := postJSON(ctx, baseURL+"/api/v1/tenants/"+tenantID+"/users/"+userID+"/roles", adminBearer, binding); err != nil {
		return nil, &TenantProvisionError{Step: "bind Security Officer role to user " + userID, TenantID: tenantID, Cause: err}
	}

	return NewTenantAdminSession(baseURL, &ProvisionedTenant{
		TenantID:     tenantID,
		AdminUserID:  userID,
		AdminCertPEM: certPEM,
		AdminKeyPEM:  keyPEM,
	}), nil
}
