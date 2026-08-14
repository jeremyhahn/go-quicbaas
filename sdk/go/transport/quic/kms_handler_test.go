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

package quic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestIsKMSService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.KMS", true},
		{"KMS", true},
		{"quicbaas.DEK", false},
		{"quicbaas.Crypto", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isKMSService(tt.svc), tt.svc)
	}
}

// TestDecodeKMSResponse_GenerateKey proves the wire shape produced by the server
// (EncodePayload of the canonical service.GenerateKeyResponse) round-trips back
// into the same canonical type — the exact decode the KMS client's
// `case *service.GenerateKeyResponse` branch relies on. This is the regression
// guard for the "invalid response type" bug where the QUIC transport fell
// through to a raw map[string]interface{}.
func TestDecodeKMSResponse_GenerateKey(t *testing.T) {
	want := &service.GenerateKeyResponse{
		Key: &service.KeyHandle{
			KeyID:   "key-123",
			Version: 2,
		},
		Metadata: &service.KeyMetadata{KeyID: "key-123"},
	}

	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeKMSResponse("GenerateKey", payload)
	require.NoError(t, err)

	resp, ok := got.(*service.GenerateKeyResponse)
	require.True(t, ok, "expected *service.GenerateKeyResponse, got %T", got)
	require.NotNil(t, resp.Key)
	assert.Equal(t, "key-123", resp.Key.KeyID)
	assert.Equal(t, uint64(2), resp.Key.Version)
	require.NotNil(t, resp.Metadata)
	assert.Equal(t, "key-123", resp.Metadata.KeyID)
}

func TestDecodeKMSResponse_ListKeys(t *testing.T) {
	want := &service.ListKeysResponse{
		Keys: []*service.KeyHandle{
			{KeyID: "k1", Version: 1},
			{KeyID: "k2", Version: 3},
		},
		NextCursor: "cursor-1",
		HasMore:    true,
	}

	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeKMSResponse("ListKeys", payload)
	require.NoError(t, err)

	resp, ok := got.(*service.ListKeysResponse)
	require.True(t, ok, "got %T", got)
	require.Len(t, resp.Keys, 2)
	assert.Equal(t, "k2", resp.Keys[1].KeyID)
	assert.Equal(t, "cursor-1", resp.NextCursor)
	assert.True(t, resp.HasMore)
}

func TestDecodeKMSResponse_EmptyPayload(t *testing.T) {
	got, err := decodeKMSResponse("GenerateKey", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeKMSResponse_UnknownMethod(t *testing.T) {
	got, err := decodeKMSResponse("Bogus", []byte(`{"foo":"bar"}`))
	require.NoError(t, err)
	m, ok := got.(map[string]interface{})
	require.True(t, ok, "expected fallback map, got %T", got)
	assert.Equal(t, "bar", m["foo"])
}

func TestDecodeKMSResponse_InvalidJSON(t *testing.T) {
	_, err := decodeKMSResponse("GenerateKey", []byte(`{not json`))
	require.Error(t, err)
}
