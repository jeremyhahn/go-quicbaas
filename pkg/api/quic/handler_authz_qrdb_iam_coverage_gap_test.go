//go:build quic

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

import "testing"

// TestGetIAMQRDBPermission_RemainingMappings covers the IAM opcode -> qrdb
// (resource, action) entries not already pinned by the base test: the update/
// get/delete variants and the legacy bundled OpIAM{Tenant,User,Role} verbs that
// all alias to an update permission. A drift in any of these (or one silently
// becoming unmapped, which under RequireAuthz default-deny would lock the surface
// out) is the regression this catches.
func TestGetIAMQRDBPermission_RemainingMappings(t *testing.T) {
	cases := []struct {
		opcode   byte
		resource string
		action   string
	}{
		{OpIAMTenant, "tenants", "update"},
		{OpIAMUpdateUser, "users", "update"},
		{OpIAMUser, "users", "update"},
		{OpIAMGetRole, "roles", "read"},
		{OpIAMUpdateRole, "roles", "update"},
		{OpIAMDeleteRole, "roles", "delete"},
		{OpIAMRole, "roles", "update"},
	}
	for _, c := range cases {
		res, act, ok := getIAMQRDBPermission(c.opcode)
		if !ok {
			t.Fatalf("opcode 0x%02X unexpectedly unmapped", c.opcode)
		}
		if res != c.resource || act != c.action {
			t.Fatalf("opcode 0x%02X = (%q,%q), want (%q,%q)", c.opcode, res, act, c.resource, c.action)
		}
	}
}
