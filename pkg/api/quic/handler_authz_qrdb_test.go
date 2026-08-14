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
		payload  []byte
		resource string
		action   string
	}{
		{OpGet, nil, "kv", "get"},
		{OpPut, nil, "kv", "put"},
		{OpDelete, nil, "kv", "delete"},
		{OpTransaction, nil, "transaction", "execute"},
		{OpKMSGenerateKey, nil, "keys", "create"},
		{OpCryptoSign, nil, "keys", "sign"},
		{OpSecretsStore, nil, "credentials", "store"},
		{OpCertIssue, nil, "certs", "generate"},
		// OpBarrier is sub-op-aware; payload[0] must select the sub-operation.
		{OpBarrier, []byte{SubOpBarrierStatus}, "tenant_barrier", "status"},
		// OpCeremony is sub-op-aware too; the read and the trust-root writes must
		// resolve to different permissions (see
		// handler_ceremony_authz_guard_test.go for the full spec).
		{OpCeremony, []byte{SubOpCeremonyStatus}, "ceremony", "status"},
		{OpCeremony, []byte{SubOpCeremonyInitialize}, "ceremony", "initialize"},
		{OpVectorSearchSimilar, nil, "vector", "search_similar"},
		{OpCreateUser, nil, "users", "create"},
		// Stage 0.5 broker data-plane surfaces.
		{OpOSPutObject, nil, "objstore", "put"},
		{OpOSGetObject, nil, "objstore", "get"},
		{OpOSBatchPutObjects, nil, "objstore", "batch"},
		{OpOSAdmin, nil, "objstore", "admin"},
		{OpOllamaEmbed, nil, "ollama", "embed"},
		{OpOllamaGetModelInfo, nil, "ollama", "model_info"},
		{OpDynamicSecretsGenerate, nil, "dynamic_secrets", "generate"},
		{OpDynamicSecretsCreateRole, nil, "dynamic_secrets", "role_create"},
		{OpParameterSet, nil, "parameters", "write"},
		{OpParameterSearch, nil, "parameters", "search"},
		{OpScheduler, nil, "scheduler", "status"},
		{OpOrchestrator, nil, "orchestrator", "status"},
	}
	for _, c := range cases {
		res, act, ok := getQRDBPermission(c.opcode, c.payload)
		require.True(t, ok, "opcode 0x%02X payload %v", c.opcode, c.payload)
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
		_, _, ok := getQRDBPermission(op, nil)
		assert.False(t, ok, "opcode 0x%02X should be unmapped", op)
	}

	// The sub-op dispatch opcodes carry no surface-level permission: without a
	// sub-op byte there is nothing to authorize, so they fail closed.
	for _, op := range []byte{OpBarrier, OpCeremony} {
		_, _, ok := getQRDBPermission(op, nil)
		assert.False(t, ok, "dispatch opcode 0x%02X must be unmapped without a sub-op", op)
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
