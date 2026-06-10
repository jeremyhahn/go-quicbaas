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
	"context"
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
		{"qrdb_barrier_status", "tenant_barrier", "status"},
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

func TestCheckToolAuthorization_RequireAuthzOff_NoNewDenial(t *testing.T) {
	// RequireAuthz=false posture: no authService, no oidc, no authManager.
	// CheckToolAuthorization allows every tool (auth disabled) — today's
	// behaviour preserved.
	ac := NewAuthorizationChecker(nil, nil)
	assert.Nil(t, ac.CheckToolAuthorization(context.Background(), "quicbaas_put", ""))
	assert.Nil(t, ac.CheckToolAuthorization(context.Background(), "quicbaas_kms_generate", ""))
}
