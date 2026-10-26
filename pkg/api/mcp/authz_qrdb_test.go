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

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetToolQRDBPermission_Mapped(t *testing.T) {
	cases := []struct {
		tool     string
		resource string
		action   string
	}{
		{"qrdb_get", "kv", "get"},
		{"qrdb_put", "kv", "put"},
		{"qrdb_delete", "kv", "delete"},
		// Reads declare the permission of the go-qrdb verb that serves them:
		// list and aggregate scan, cluster status reads the topology, and
		// get-leader reads the shard state (ruling 161).
		{"qrdb_list", "kv", "range"},
		{"quicbaas_aggregate", "kv", "range"},
		{"quicbaas_cluster_status", "admin", "topology"},
		{"quicbaas_get_leader", "admin", "shard_state"},
		{"quicbaas_batch_put", "batch", "execute"},
		// User administration is the quicbaas_iam_user_* family; the
		// quicbaas_user_* / quicbaas_role_* names that used to be pinned here
		// were map entries for tools that were never registered (issue #905).
		{"quicbaas_iam_user_create", "users", "create"},
		{"qrdb_role_assign", "roles", "assign"},
		{"quicbaas_role_revoke", "roles", "unassign"},
		{"qrdb_ceremony_status", "ceremony", "status"},
		{"qrdb_vector_search", "vector", "search_similar"},
		{"qrdb_credential_list", "credentials", "list"},
		{"quicbaas_transaction", "transaction", "execute"},
		// Stage 0.5 broker data-plane surfaces.
		{"quicbaas_object_put", "objstore", "put"},
		{"quicbaas_object_get", "objstore", "get"},
		{"quicbaas_object_batch_put", "objstore", "batch"},
		{"quicbaas_object_admin_run_gc", "objstore", "admin"},
		{"quicbaas_orchestrator_spawn", "orchestrator", "spawn"},
		{"quicbaas_orchestrator_cancel_job", "orchestrator", "job_cancel"},
		{"quicbaas_scheduler_create_job", "scheduler", "create"},
		{"quicbaas_scheduler_trigger_job", "scheduler", "trigger"},
		{"quicbaas_parameter_set", "parameters", "write"},
		{"quicbaas_parameter_search", "parameters", "search"},
		{"quicbaas_dynamic_secrets_generate", "dynamic_secrets", "generate"},
		{"quicbaas_dynamic_secrets_create_role", "dynamic_secrets", "role_create"},
		{"project_create", "project", "create"},
		{"project_grant_fleet_node", "project", "grant"},
		{"quicbaas_ollama_embed", "ollama", "embed"},
		{"quicbaas_ollama_model_info", "ollama", "model_info"},
		// Per-tenant barrier tools (qrdb_tenant_barrier_* prefix).
		{"qrdb_tenant_barrier_init", "tenant_barrier", "init"},
		{"qrdb_tenant_barrier_unseal", "tenant_barrier", "unseal"},
		{"qrdb_tenant_barrier_unseal_share", "tenant_barrier", "unseal"},
		{"qrdb_tenant_barrier_seal", "tenant_barrier", "seal"},
		{"qrdb_tenant_barrier_status", "tenant_barrier", "status"},
		// Enable-encryption (exact map, not prefix).
		{"qrdb_barrier_enable_encryption", "seal", "enable_encryption"},
		{"qrdb_tenant_barrier_enable_encryption", "tenant_barrier", "enable_encryption"},
	}
	for _, c := range cases {
		res, act, ok := getToolQRDBPermission(c.tool)
		require.True(t, ok, c.tool)
		assert.Equal(t, c.resource, res, c.tool)
		assert.Equal(t, c.action, act, c.tool)
	}
}

// TestTrustToolsTrustFamilyPermissions pins the trust tools to the same
// permissions REST and gRPC assign: the pool read is trust:read, and AIA
// import and DANE publish, which change what the node trusts or publishes,
// are trust:write.
func TestTrustToolsTrustFamilyPermissions(t *testing.T) {
	cases := []struct {
		tool   string
		action string
	}{
		{"quicbaas_trust_cross_signed_root_pool", "read"},
		{"quicbaas_trust_auto_import_aia", "write"},
		{"quicbaas_trust_publish_dane", "write"},
	}
	for _, c := range cases {
		res, act, ok := getToolQRDBPermission(c.tool)
		require.True(t, ok, "%s resolved no permission", c.tool)
		assert.Equal(t, "trust", res, c.tool)
		assert.Equal(t, c.action, act, c.tool)
		_, dead := unmappedTools[c.tool]
		assert.False(t, dead, "%s is mapped, so it must not be declared dead", c.tool)
	}
}

// TestGetToolQRDBPermission_Unmapped pins the default-deny half of the mapper.
//
// Two distinct cases must stay unmapped:
//
//  1. the reviewed, deliberately dead surfaces declared in unmappedToolGroups
//     (authz_qrdb_unmapped.go) — no sibling REST/gRPC rule exists to copy, so
//     guessing a permission at a security boundary is worse than denying;
//  2. any name that is not a registered tool, including a NEAR MISS of one
//     ("quicbaas_kms_generate" vs the real "quicbaas_kms_generate_key"). The
//     KMS, crypto and secrets families are mapped exactly, one entry per tool,
//     with no prefix catch-all, so a near miss must not inherit a permission.
func TestGetToolQRDBPermission_Unmapped(t *testing.T) {
	for _, tool := range []string{
		// Declared dead: see unmappedToolGroups.
		"quicbaas_iam_group_create",
		"quicbaas_iam_policy_update",
		"quicbaas_mfa_enroll_begin",
		// Not registered tools.
		"quicbaas_kms_generate",
		"quicbaas_unknown_thing",
	} {
		_, _, ok := getToolQRDBPermission(tool)
		assert.False(t, ok, tool)
	}
}

// TestBarrierToolsGranularAuthzMapping is a per-tool exhaustive guard that
// enumerates every entry in barrierToolNames (the authoritative dispatch map
// in tools_barrier.go) and asserts that getToolQRDBPermission resolves it to
// the canonical per-operation permission expected by the gRPC canon
// (pkg/api/grpc/authz_mapping.go lines 259-276). A tool present in
// barrierToolNames but absent from qrdbToolExactMapping would otherwise be
// default-denied at the gate, making it unreachable even to authorised callers.
func TestBarrierToolsGranularAuthzMapping(t *testing.T) {
	// wantBarrierPerms is the exact gRPC-canonical table for the mapped barrier
	// tools. Values are copied from pkg/api/grpc/authz_mapping.go.
	wantBarrierPerms := map[string]qrdbToolResource{
		"qrdb_barrier_status":                   {"tenant_barrier", "status"},
		"qrdb_barrier_initialize":               {"tenant_barrier", "init"},
		"qrdb_barrier_unseal":                   {"tenant_barrier", "unseal"},
		"qrdb_barrier_seal":                     {"tenant_barrier", "seal"},
		"qrdb_barrier_shamir_initialize":        {"seal", "initialize_shamir"},
		"qrdb_barrier_recovery_generate":        {"seal", "generate_recovery"},
		"qrdb_barrier_recovery_delete":          {"seal", "generate_recovery"},
		"qrdb_barrier_recovery_status":          {"seal", "status"},
		"qrdb_barrier_enable_encryption":        {"seal", "enable_encryption"},
		"qrdb_tenant_barrier_enable_encryption": {"tenant_barrier", "enable_encryption"},
		"qrdb_tenant_update_quotas":             {"tenants", "set_quotas"},
		"qrdb_tenant_update_entitlements":       {"tenants", "set_entitlements"},
		// Share-holder tools: values copied from the REST holder routes.
		"qrdb_barrier_cluster_unseal":            {"seal", "unseal"},
		"qrdb_barrier_officer_list":              {"seal", "officer_list"},
		"qrdb_barrier_officer_register":          {"seal", "officer_register"},
		"qrdb_barrier_recovery_officer_register": {"seal", "officer_register"},
		"qrdb_barrier_officer_revoke":            {"seal", "officer_revoke"},
		"qrdb_barrier_rekey_start":               {"seal", "rekey"},
	}
	// identityGated are the holder tools go-qrdb serves without a permission
	// (the verb checks the caller is the holder). They must stay unmapped and
	// be declared in unmappedToolGroups, never resolve to a permission.
	identityGated := map[string]bool{
		"qrdb_barrier_share_claim_begin":             true,
		"qrdb_barrier_share_claim_complete":          true,
		"qrdb_barrier_recovery_share_claim_begin":    true,
		"qrdb_barrier_recovery_share_claim_complete": true,
		"qrdb_barrier_quorum_begin":                  true,
		"qrdb_barrier_quorum_submit":                 true,
	}
	declaredDead := map[string]bool{}
	for _, g := range unmappedToolGroups {
		for _, tool := range g.tools {
			declaredDead[tool] = true
		}
	}
	for name := range identityGated {
		_, _, ok := getToolQRDBPermission(name)
		assert.Falsef(t, ok, "identity-gated tool %q must not resolve to a permission", name)
		assert.Truef(t, declaredDead[name], "identity-gated tool %q must be declared in unmappedToolGroups", name)
	}

	// Assert the expectation table covers every tool in the dispatch map so
	// this test fails loudly when a new tool is added to barrierToolNames
	// without a corresponding entry here and in qrdbToolExactMapping.
	for name := range barrierToolNames {
		if _, ok := wantBarrierPerms[name]; !ok && !identityGated[name] {
			t.Errorf("barrierToolNames contains %q which has no entry in wantBarrierPerms; add it to both the exact mapping and this table", name)
		}
	}
	for name := range wantBarrierPerms {
		if _, ok := barrierToolNames[name]; !ok {
			t.Errorf("wantBarrierPerms contains %q which is not in barrierToolNames; remove the stale expectation", name)
		}
	}

	for name, want := range wantBarrierPerms {
		t.Run(name, func(t *testing.T) {
			res, act, ok := getToolQRDBPermission(name)
			require.Truef(t, ok, "tool %q is unmapped — privilege-escalation gap: default-deny blocks authorised callers", name)
			assert.Equalf(t, want.resource, res, "tool %q: wrong resource", name)
			assert.Equalf(t, want.action, act, "tool %q: wrong action", name)
		})
	}
}

// TestBarrierToolsFailClosedOnUnknownTool asserts that a hypothetical future
// barrier tool that is registered in barrierToolNames but not yet added to
// qrdbToolExactMapping is default-denied rather than silently admitted via a
// catch-all. The "qrdb_barrier" prefix catch-all was removed precisely to
// enforce this posture.
func TestBarrierToolsFailClosedOnUnknownTool(t *testing.T) {
	_, _, ok := getToolQRDBPermission("qrdb_barrier_export_shares")
	assert.False(t, ok, "hypothetical future barrier tool must be default-denied, not admitted via a catch-all")
}
