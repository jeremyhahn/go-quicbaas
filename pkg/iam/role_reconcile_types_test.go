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

import (
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoleReconcileTypesAreGoQRDBs compiles only while the reconcile result
// and report are go-qrdb's transport types, not local copies of them.
func TestRoleReconcileTypesAreGoQRDBs(t *testing.T) {
	var r qrdbtransport.RoleReconcileResult = RoleReconcileResult{
		RoleID:  "r1",
		Name:    "admin",
		Created: true,
		Added:   []string{"kv:get"},
	}
	var rep *qrdbtransport.ReconcileRolesResponse = &RoleReconcileReport{
		TenantID: "tenant-test",
		Roles:    []RoleReconcileResult{r},
	}

	assert.Equal(t, "tenant-test", rep.TenantID)
	require.Len(t, rep.Roles, 1)
	got := rep.Roles[0]
	assert.Equal(t, "r1", got.RoleID)
	assert.Equal(t, "admin", got.Name)
	assert.True(t, got.Created)
	assert.Equal(t, []string{"kv:get"}, got.Added)
	assert.Empty(t, got.Removed)
}

func TestRoleReconcileChanged(t *testing.T) {
	tests := []struct {
		name   string
		report *RoleReconcileReport
		want   bool
	}{
		{name: "nil report", report: nil, want: false},
		{name: "nil roles", report: &RoleReconcileReport{TenantID: "tenant-test"}, want: false},
		{name: "empty roles", report: &RoleReconcileReport{TenantID: "tenant-test", Roles: []RoleReconcileResult{}}, want: false},
		{
			name: "one changed role",
			report: &RoleReconcileReport{
				TenantID: "tenant-test",
				Roles:    []RoleReconcileResult{{RoleID: "r1", Name: "admin", Added: []string{"kv:get"}}},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, RoleReconcileChanged(tt.report))
		})
	}
}
