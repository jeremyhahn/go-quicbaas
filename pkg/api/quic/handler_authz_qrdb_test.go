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

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetQRDBPermission_Mapped(t *testing.T) {
	cases := []struct {
		opcode   byte
		resource string
		action   string
	}{
		{OpGet, "kv", "get"},
		{OpPut, "kv", "put"},
		{OpDelete, "kv", "delete"},
		{OpTransaction, "transaction", "execute"},
		{OpKMSGenerateKey, "keys", "create"},
		{OpCryptoSign, "keys", "sign"},
		{OpSecretsStore, "credentials", "store"},
		{OpCertIssue, "certs", "generate"},
		{OpBarrier, "tenant_barrier", "status"},
		{OpCeremony, "ceremony", "status"},
		{OpVectorSearchSimilar, "vector", "search_similar"},
		{OpCreateUser, "users", "create"},
		// Stage 0.5 broker data-plane surfaces.
		{OpOSPutObject, "objstore", "put"},
		{OpOSGetObject, "objstore", "get"},
		{OpOSBatchPutObjects, "objstore", "batch"},
		{OpOSAdmin, "objstore", "admin"},
		{OpOllamaEmbed, "ollama", "embed"},
		{OpOllamaGetModelInfo, "ollama", "model_info"},
		{OpDynamicSecretsGenerate, "dynamic_secrets", "generate"},
		{OpDynamicSecretsCreateRole, "dynamic_secrets", "role_create"},
		{OpParameterSet, "parameters", "write"},
		{OpParameterSearch, "parameters", "search"},
		{OpScheduler, "scheduler", "status"},
		{OpOrchestrator, "orchestrator", "status"},
	}
	for _, c := range cases {
		res, act, ok := getQRDBPermission(c.opcode)
		require.True(t, ok, "opcode 0x%02X", c.opcode)
		assert.Equal(t, c.resource, res, "opcode 0x%02X", c.opcode)
		assert.Equal(t, c.action, act, "opcode 0x%02X", c.opcode)
	}
}

func TestGetQRDBPermission_Unmapped(t *testing.T) {
	// Surfaces with no qrdb permission family must be unmapped (default-deny).
	// Embedding has no qrdb resource family after Stage 0.5.
	for _, op := range []byte{
		OpEmbeddingGenerate,
		OpEmbeddingBatchGenerate,
		OpEmbeddingListProviders,
	} {
		_, _, ok := getQRDBPermission(op)
		assert.False(t, ok, "opcode 0x%02X should be unmapped", op)
	}
}

func TestAuthorizeAndInject_RequireAuthzOff_NoEnforcement(t *testing.T) {
	// With NO validator installed — the RequireAuthz=false posture — every
	// non-public op is allowed (no enforcement).
	h := NewHandler(nil, nil, nil, nil, nil)
	_, resp := h.authorizeAndInject(context.Background(), &Request{Opcode: OpPut, Token: ""})
	assert.Nil(t, resp, "RequireAuthz=false must perform no enforcement (no token, no denial)")

	// A public op is always allowed.
	_, respPub := h.authorizeAndInject(context.Background(), &Request{Opcode: OpHealth})
	assert.Nil(t, respPub)
}
