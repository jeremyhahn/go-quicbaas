// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package iam

import "testing"

func TestAuditCategory_String(t *testing.T) {
	tests := []struct {
		category AuditCategory
		expected string
	}{
		{AuditCategoryAuthentication, "authentication"},
		{AuditCategoryAuthorization, "authorization"},
		{AuditCategoryManagement, "management"},
		{AuditCategoryDataAccess, "data_access"},
		{AuditCategorySecurity, "security"},
		{AuditCategory(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.category.String(); got != tt.expected {
				t.Errorf("AuditCategory.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAdminRole_String(t *testing.T) {
	tests := []struct {
		role     AdminRole
		expected string
	}{
		{AdminRoleSuperAdmin, "super_admin"},
		{AdminRoleTenantAdmin, "tenant_admin"},
		{AdminRoleUserAdmin, "user_admin"},
		{AdminRoleGroupAdmin, "group_admin"},
		{AdminRolePolicyAdmin, "policy_admin"},
		{AdminRoleAppAdmin, "app_admin"},
		{AdminRoleAuditViewer, "audit_viewer"},
		{AdminRoleReadOnly, "read_only"},
		{AdminRole(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.role.String(); got != tt.expected {
				t.Errorf("AdminRole.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}
