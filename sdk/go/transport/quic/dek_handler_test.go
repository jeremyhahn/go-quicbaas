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

func TestIsDEKService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.DEK", true},
		{"DEK", true},
		{"quicbaas.KMS", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isDEKService(tt.svc), tt.svc)
	}
}

// TestDecodeDEKResponse_CreateDEK proves the canonical service.CreateDEKResponse
// round-trips over the wire and back into the same type the DEK client's
// `case *service.CreateDEKResponse` branch expects.
func TestDecodeDEKResponse_CreateDEK(t *testing.T) {
	want := &service.CreateDEKResponse{
		DEK: &service.DEKMetadata{
			Path:      "app/db",
			Version:   1,
			Status:    "active",
			IsDefault: true,
		},
	}

	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeDEKResponse("CreateDEK", payload)
	require.NoError(t, err)

	resp, ok := got.(*service.CreateDEKResponse)
	require.True(t, ok, "got %T", got)
	require.NotNil(t, resp.DEK)
	assert.Equal(t, "app/db", resp.DEK.Path)
	assert.Equal(t, uint64(1), resp.DEK.Version)
	assert.True(t, resp.DEK.IsDefault)
}

func TestDecodeDEKResponse_ListDEKs(t *testing.T) {
	want := &service.ListDEKsResponse{
		DEKs: []*service.DEKMetadata{
			{Path: "a", Version: 1},
			{Path: "b", Version: 2},
		},
		NextCursor: "n1",
		HasMore:    true,
	}

	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeDEKResponse("ListDEKs", payload)
	require.NoError(t, err)

	resp, ok := got.(*service.ListDEKsResponse)
	require.True(t, ok, "got %T", got)
	require.Len(t, resp.DEKs, 2)
	assert.Equal(t, "b", resp.DEKs[1].Path)
	assert.True(t, resp.HasMore)
}

func TestDecodeDEKResponse_EmptyPayload(t *testing.T) {
	got, err := decodeDEKResponse("CreateDEK", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeDEKResponse_UnknownMethod(t *testing.T) {
	got, err := decodeDEKResponse("Bogus", []byte(`{"k":"v"}`))
	require.NoError(t, err)
	m, ok := got.(map[string]interface{})
	require.True(t, ok, "got %T", got)
	assert.Equal(t, "v", m["k"])
}

func TestDecodeDEKResponse_InvalidJSON(t *testing.T) {
	_, err := decodeDEKResponse("CreateDEK", []byte(`{bad`))
	require.Error(t, err)
}
