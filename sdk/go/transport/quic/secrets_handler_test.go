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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSecretsService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.Secrets", true},
		{"Secrets", true},
		{"quicbaas.KMS", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isSecretsService(tt.svc), tt.svc)
	}
}

// TestDecodeSecretsResponse_Rotate proves the canonical service.RotateSecretResponse
// round-trips over the wire into the same type the Secrets client's
// `case *service.RotateSecretResponse` branch expects.
func TestDecodeSecretsResponse_Rotate(t *testing.T) {
	want := &service.RotateSecretResponse{
		SecretID:   "db/password",
		OldVersion: 3,
		NewVersion: 4,
	}

	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeSecretsResponse("Rotate", payload)
	require.NoError(t, err)

	resp, ok := got.(*service.RotateSecretResponse)
	require.True(t, ok, "got %T", got)
	assert.Equal(t, "db/password", resp.SecretID)
	assert.Equal(t, uint64(3), resp.OldVersion)
	assert.Equal(t, uint64(4), resp.NewVersion)
}

func TestDecodeSecretsResponse_GetVersion(t *testing.T) {
	want := &service.GetSecretVersionResponse{
		Version: &service.SecretVersion{Version: 7},
	}

	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeSecretsResponse("GetVersion", payload)
	require.NoError(t, err)

	resp, ok := got.(*service.GetSecretVersionResponse)
	require.True(t, ok, "got %T", got)
	require.NotNil(t, resp.Version)
	assert.Equal(t, uint64(7), resp.Version.Version)
}

func TestDecodeSecretsResponse_EmptyPayload(t *testing.T) {
	got, err := decodeSecretsResponse("Rotate", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeSecretsResponse_UnknownMethod(t *testing.T) {
	got, err := decodeSecretsResponse("Store", []byte(`{"x":true}`))
	require.NoError(t, err)
	m, ok := got.(map[string]interface{})
	require.True(t, ok, "got %T", got)
	assert.Equal(t, true, m["x"])
}

func TestDecodeSecretsResponse_InvalidJSON(t *testing.T) {
	_, err := decodeSecretsResponse("Rotate", []byte(`{bad`))
	require.Error(t, err)
}
