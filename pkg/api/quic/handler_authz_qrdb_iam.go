//go:build quic && iam

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

package quic

// getIAMQRDBPermission maps IAM opcodes onto qrdb-canonical (resource, action)
// permissions. Only the IAM surfaces that the qrdb permission registry defines a
// resource family for — tenants, users, roles — are mapped. IAM group, app, and
// policy operations have no qrdb permission family, so they are reported
// unmapped (handled=false) and denied under RequireAuthz default-deny.
func getIAMQRDBPermission(opcode byte) (resource, action string, handled bool) {
	switch opcode {
	// Tenant operations -> tenants
	case OpIAMCreateTenant:
		return "tenants", "create", true
	case OpIAMGetTenant:
		return "tenants", "read", true
	case OpIAMListTenants:
		return "tenants", "list", true
	case OpIAMUpdateTenant:
		return "tenants", "update", true
	case OpIAMDeleteTenant:
		return "tenants", "delete", true
	case OpIAMTenant:
		return "tenants", "update", true

	// User operations -> users
	case OpIAMCreateUser:
		return "users", "create", true
	case OpIAMGetUser:
		return "users", "read", true
	case OpIAMListUsers:
		return "users", "list", true
	case OpIAMUpdateUser:
		return "users", "update", true
	case OpIAMDeleteUser:
		return "users", "delete", true
	case OpIAMUser:
		return "users", "update", true

	// Role operations -> roles
	case OpIAMCreateRole:
		return "roles", "create", true
	case OpIAMGetRole:
		return "roles", "read", true
	case OpIAMListRoles:
		return "roles", "list", true
	case OpIAMUpdateRole:
		return "roles", "update", true
	case OpIAMDeleteRole:
		return "roles", "delete", true
	case OpIAMAssignRole:
		return "roles", "assign", true
	case OpIAMRevokeRole:
		return "roles", "unassign", true
	case OpIAMRole:
		return "roles", "update", true

	default:
		return "", "", false
	}
}
