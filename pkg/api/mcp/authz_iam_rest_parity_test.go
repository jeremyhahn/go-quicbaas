//go:build mcp

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

package mcp

import "testing"

// iamRESTParityTenantScopedTools are the federation and gdpr tools that read a
// tenant_id argument: they must be in iamTenantScopedTools, or a tenant admin
// could name another tenant.
var iamRESTParityTenantScopedTools = []string{
	"quicbaas_iam_federation_createprovider",
	"quicbaas_iam_federation_getprovider",
	"quicbaas_iam_federation_listproviders",
	"quicbaas_iam_federation_updateprovider",
	"quicbaas_iam_federation_deleteprovider",
	"quicbaas_iam_federation_linkaccount",
	"quicbaas_iam_federation_getuserlinks",
	"quicbaas_iam_federation_unlinkaccount",
	"quicbaas_iam_gdpr_consent_grant",
	"quicbaas_iam_gdpr_consent_withdraw",
	"quicbaas_iam_gdpr_consent_get",
	"quicbaas_iam_gdpr_consent_list",
	"quicbaas_iam_gdpr_export_request",
	"quicbaas_iam_gdpr_deletion_request",
	"quicbaas_iam_policy_create",
	"quicbaas_iam_policy_get",
	"quicbaas_iam_policy_list",
	"quicbaas_iam_policy_update",
	"quicbaas_iam_policy_delete",
	"quicbaas_iam_policy_test",
	"quicbaas_iam_policy_validate",
}

// TestIAMRESTParityMCPPermissions pins the IAM federation, gdpr and policy
// tools to exactly the grant of their REST route
// (pkg/api/rest/routes_permissions_generated.go), by owner ruling. The gRPC
// twins are pinned in pkg/api/grpc/authz_iam_rest_parity_test.go.
func TestIAMRESTParityMCPPermissions(t *testing.T) {
	want := map[string]qrdbToolResource{
		"quicbaas_iam_federation_createprovider": {"tenants", "create"},
		"quicbaas_iam_federation_getprovider":    {"tenants", "read"},
		"quicbaas_iam_federation_listproviders":  {"tenants", "read"},
		"quicbaas_iam_federation_updateprovider": {"tenants", "create"},
		"quicbaas_iam_federation_deleteprovider": {"tenants", "delete"},
		"quicbaas_iam_federation_linkaccount":    {"tenants", "create"},
		"quicbaas_iam_federation_getuserlinks":   {"tenants", "read"},
		"quicbaas_iam_federation_unlinkaccount":  {"tenants", "delete"},
		"quicbaas_iam_gdpr_consent_grant":        {"tenants", "create"},
		"quicbaas_iam_gdpr_consent_withdraw":     {"tenants", "create"},
		"quicbaas_iam_gdpr_consent_get":          {"tenants", "read"},
		"quicbaas_iam_gdpr_consent_list":         {"tenants", "read"},
		"quicbaas_iam_gdpr_export_request":       {"tenants", "create"},
		"quicbaas_iam_gdpr_deletion_request":     {"tenants", "create"},
		"quicbaas_iam_policy_create":             {"tenants", "create"},
		"quicbaas_iam_policy_get":                {"tenants", "read"},
		"quicbaas_iam_policy_list":               {"tenants", "read"},
		"quicbaas_iam_policy_update":             {"tenants", "create"},
		"quicbaas_iam_policy_delete":             {"tenants", "delete"},
		"quicbaas_iam_policy_test":               {"tenants", "create"},
		"quicbaas_iam_policy_validate":           {"tenants", "create"},
	}

	for tool, expected := range want {
		got, ok := qrdbToolExactMapping[tool]
		if !ok {
			t.Errorf("tool %q has no permission mapping: the authz gate is default-deny, "+
				"so every call is refused", tool)
			continue
		}
		if got != expected {
			t.Errorf("tool %q is gated %v, but its REST route takes %v; "+
				"the grant must not depend on which transport carried the call",
				tool, got, expected)
		}
	}
}

// TestIAMRESTParityMCPTenantScoped pins the cross-tenant guard on every
// mapped federation, gdpr and policy tool that reads tenant_id.
func TestIAMRESTParityMCPTenantScoped(t *testing.T) {
	for _, tool := range iamRESTParityTenantScopedTools {
		if _, ok := iamTenantScopedTools[tool]; !ok {
			t.Errorf("tool %q reads tenant_id but is not in iamTenantScopedTools: "+
				"a tenant admin could name another tenant", tool)
		}
	}
}

// TestIAMRESTParityMCPStillDead pins the login handshake and the erasure
// tools as declared dead: unmapped, and not public. The gdpr export-status,
// deletion-cancel and deletion-status tools are dead as well: they look the
// export or deletion record up by ID with no tenant check.
func TestIAMRESTParityMCPStillDead(t *testing.T) {
	for _, tool := range []string{
		"quicbaas_iam_federation_beginlogin",
		"quicbaas_iam_federation_handlecallback",
		"quicbaas_iam_gdpr_deletion_process",
		"quicbaas_iam_gdpr_export_generate",
		"quicbaas_iam_gdpr_export_status",
		"quicbaas_iam_gdpr_deletion_cancel",
		"quicbaas_iam_gdpr_deletion_status",
	} {
		if _, ok := qrdbToolExactMapping[tool]; ok {
			t.Errorf("tool %q must carry no grant", tool)
		}
		if !isDeclaredUnmappedTool(tool) {
			t.Errorf("tool %q must stay declared dead", tool)
		}
		if publicTools[tool] {
			t.Errorf("tool %q must not be public", tool)
		}
	}
}
