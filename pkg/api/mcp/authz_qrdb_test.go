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
		{"quicbaas_get", "kv", "get"},
		{"quicbaas_put", "kv", "put"},
		{"qrdb_delete", "kv", "delete"},
		{"quicbaas_user_create", "users", "create"},
		{"quicbaas_role_assign", "roles", "assign"},
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
		{"project_grant_agent", "project", "grant"},
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

func TestGetToolQRDBPermission_Unmapped(t *testing.T) {
	// Surfaces with no qrdb permission family must be unmapped (default-deny).
	for _, tool := range []string{
		"quicbaas_kms_generate",
		"quicbaas_crypto_sign",
		"quicbaas_secrets_store",
		"agent_enable",
		"quicbaas_embedding_generate",
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
	// wantBarrierPerms is the exact gRPC-canonical table for the 17 barrier
	// tools. Values are copied from pkg/api/grpc/authz_mapping.go.
	wantBarrierPerms := map[string]qrdbToolResource{
		"qrdb_barrier_status":                   {"tenant_barrier", "status"},
		"qrdb_barrier_initialize":               {"tenant_barrier", "init"},
		"qrdb_barrier_unseal":                   {"tenant_barrier", "unseal"},
		"qrdb_barrier_seal":                     {"tenant_barrier", "seal"},
		"qrdb_barrier_shamir_initialize":        {"seal", "initialize_shamir"},
		"qrdb_barrier_shamir_unseal_share":      {"barrier", "unseal-share"},
		"qrdb_barrier_shamir_unseal_shares":     {"barrier", "unseal-share"},
		"qrdb_barrier_rekey":                    {"seal", "rekey"},
		"qrdb_barrier_recovery_generate":        {"seal", "generate_recovery"},
		"qrdb_barrier_recovery_recover":         {"seal", "recover_with_keys"},
		"qrdb_barrier_recovery_delete":          {"seal", "generate_recovery"},
		"qrdb_barrier_recovery_status":          {"seal", "status"},
		"qrdb_barrier_root_token":               {"seal", "bootstrap"},
		"qrdb_barrier_enable_encryption":        {"seal", "enable_encryption"},
		"qrdb_tenant_barrier_enable_encryption": {"tenant_barrier", "enable_encryption"},
		"qrdb_tenant_update_quotas":             {"tenants", "set_quotas"},
		"qrdb_tenant_update_entitlements":       {"tenants", "set_entitlements"},
	}

	// Assert the expectation table covers every tool in the dispatch map so
	// this test fails loudly when a new tool is added to barrierToolNames
	// without a corresponding entry here and in qrdbToolExactMapping.
	for name := range barrierToolNames {
		if _, ok := wantBarrierPerms[name]; !ok {
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