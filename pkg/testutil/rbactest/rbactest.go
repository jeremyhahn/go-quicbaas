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

// Package rbactest is a role-to-permission double of go-qrdb's RBAC client
// for transport tests that need a caller's effective permissions, such as
// the application grantor gate (iam.WithGrantorPermissions). It mirrors the
// double pkg/iam's token tests use, for packages that cannot import it.
package rbactest

import (
	"context"
	"slices"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// NotAuthorizedError is RoleRBAC's denial: no role of the identity lists
// the exact "resource:action" permission.
type NotAuthorizedError struct {
	Subject    string
	Permission string
}

// Error implements error.
func (e *NotAuthorizedError) Error() string {
	return "rbactest: " + e.Subject + " does not hold " + e.Permission
}

// RoleRBAC resolves an identity's permissions from its roles. It is both the
// qrdb RBAC client and the iam.PermissionResolver that hands it out.
type RoleRBAC struct {
	// PermsByRole maps a role name to the colon "resource:action"
	// permissions that role grants.
	PermsByRole map[string][]string
}

// New returns a RoleRBAC over permsByRole.
func New(permsByRole map[string][]string) *RoleRBAC {
	return &RoleRBAC{PermsByRole: permsByRole}
}

// RBACClient returns r itself, so r serves as an iam.PermissionResolver.
func (r *RoleRBAC) RBACClient() qrdbtransport.RBACClient {
	return r
}

// ListPermissions returns every permission the identity's roles grant.
func (r *RoleRBAC) ListPermissions(_ context.Context, identity *qrdbtransport.Identity) ([]qrdbtransport.PermissionInfo, error) {
	var out []qrdbtransport.PermissionInfo
	for _, role := range identity.Roles {
		for _, name := range r.PermsByRole[role] {
			out = append(out, qrdbtransport.PermissionInfo{Name: name})
		}
	}
	return out, nil
}

// Authorize permits the action only when a role of identity lists the exact
// "resource:action" permission.
func (r *RoleRBAC) Authorize(_ context.Context, identity *qrdbtransport.Identity, resource, action string) error {
	want := resource + ":" + action
	for _, role := range identity.Roles {
		if slices.Contains(r.PermsByRole[role], want) {
			return nil
		}
	}
	return &NotAuthorizedError{Subject: identity.Subject, Permission: want}
}

// CallerContext binds a verified qrdb identity named subject in tenantID
// holding roles, as the transports' authentication does.
func CallerContext(ctx context.Context, subject, tenantID string, roles ...string) context.Context {
	return qrdbsdk.WithIdentity(ctx, &qrdbsdk.Identity{
		Subject:  subject,
		TenantID: tenantID,
		Roles:    roles,
	})
}
