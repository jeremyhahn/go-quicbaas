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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTrustService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.Trust", true},
		{"Trust", true},
		{"quicbaas.Certificate", false},
		{"quicbaas.KMS", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isTrustService(tt.svc), tt.svc)
	}
}

func TestTrustMethodToOpcode(t *testing.T) {
	op, ok := MethodToOpcode("quicbaas.Trust", "CrossSignedRootPool")
	require.True(t, ok)
	assert.Equal(t, OpTrustCrossSignedRootPool, op)

	op, ok = MethodToOpcode("quicbaas.Trust", "AutoImportAIA")
	require.True(t, ok)
	assert.Equal(t, OpTrustAutoImportAIA, op)

	// Opcodes must match the server (0x7A-0x7B).
	assert.Equal(t, byte(0x7A), OpTrustCrossSignedRootPool)
	assert.Equal(t, byte(0x7B), OpTrustAutoImportAIA)

	// PublishDANE has no QUIC opcode and must surface as unknown.
	_, ok = MethodToOpcode("quicbaas.Trust", "PublishDANE")
	assert.False(t, ok)
}

func TestTrustOpcodeToMethod(t *testing.T) {
	m, ok := OpcodeToMethod(OpTrustCrossSignedRootPool)
	require.True(t, ok)
	assert.Equal(t, "quicbaas.Trust/CrossSignedRootPool", m)

	m, ok = OpcodeToMethod(OpTrustAutoImportAIA)
	require.True(t, ok)
	assert.Equal(t, "quicbaas.Trust/AutoImportAIA", m)
}

// TestDecodeTrustResponse_CrossSignedRootPool proves the server's response
// (emitted via EncodePayload, JSON snake_case) round-trips into the leaf
// trust response type the SDK asserts on.
func TestDecodeTrustResponse_CrossSignedRootPool(t *testing.T) {
	want := &trust.CrossSignedRootPoolResponse{
		RootPEMs: [][]byte{[]byte("root-a"), []byte("root-b")},
	}
	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeTrustResponse("CrossSignedRootPool", payload)
	require.NoError(t, err)
	resp, ok := got.(*trust.CrossSignedRootPoolResponse)
	require.True(t, ok, "got %T", got)
	require.Len(t, resp.RootPEMs, 2)
	assert.Equal(t, "root-a", string(resp.RootPEMs[0]))
}

func TestDecodeTrustResponse_AutoImportAIA(t *testing.T) {
	want := &trust.AutoImportAIAResponse{Imported: 3}
	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeTrustResponse("AutoImportAIA", payload)
	require.NoError(t, err)
	resp, ok := got.(*trust.AutoImportAIAResponse)
	require.True(t, ok, "got %T", got)
	assert.Equal(t, 3, resp.Imported)
}

func TestDecodeTrustResponse_EmptyPayload(t *testing.T) {
	got, err := decodeTrustResponse("CrossSignedRootPool", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeTrustResponse_UnknownMethod(t *testing.T) {
	_, err := decodeTrustResponse("PublishDANE", []byte(`{}`))
	require.Error(t, err)
}

func TestDecodeTrustResponse_InvalidJSON(t *testing.T) {
	_, err := decodeTrustResponse("CrossSignedRootPool", []byte("{not json"))
	require.Error(t, err)
	_, err = decodeTrustResponse("AutoImportAIA", []byte("{not json"))
	require.Error(t, err)
}
