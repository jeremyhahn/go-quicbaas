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

func TestIsCryptoService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.Crypto", true},
		{"Crypto", true},
		{"quicbaas.KMS", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isCryptoService(tt.svc), tt.svc)
	}
}

// TestDecodeCryptoResponse_Encrypt proves the canonical service.EncryptResponse
// round-trips over the wire into the same type the Crypto client's
// `case *service.EncryptResponse` branch expects.
func TestDecodeCryptoResponse_Encrypt(t *testing.T) {
	want := &service.EncryptResponse{
		KeyID:      "key-9",
		KeyVersion: 4,
		Ciphertext: []byte("ciphertext-bytes"),
	}

	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeCryptoResponse("Encrypt", payload)
	require.NoError(t, err)

	resp, ok := got.(*service.EncryptResponse)
	require.True(t, ok, "got %T", got)
	assert.Equal(t, "key-9", resp.KeyID)
	assert.Equal(t, uint64(4), resp.KeyVersion)
	assert.Equal(t, []byte("ciphertext-bytes"), resp.Ciphertext)
}

func TestDecodeCryptoResponse_GenerateRandom(t *testing.T) {
	want := &service.GenerateRandomResponse{
		RandomBytes: []byte{0x01, 0x02, 0x03, 0x04},
	}

	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeCryptoResponse("GenerateRandom", payload)
	require.NoError(t, err)

	resp, ok := got.(*service.GenerateRandomResponse)
	require.True(t, ok, "got %T", got)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, resp.RandomBytes)
}

func TestDecodeCryptoResponse_EmptyPayload(t *testing.T) {
	got, err := decodeCryptoResponse("Encrypt", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeCryptoResponse_UnknownMethod(t *testing.T) {
	got, err := decodeCryptoResponse("Bogus", []byte(`{"a":1}`))
	require.NoError(t, err)
	m, ok := got.(map[string]interface{})
	require.True(t, ok, "got %T", got)
	assert.Equal(t, float64(1), m["a"])
}

func TestDecodeCryptoResponse_InvalidJSON(t *testing.T) {
	_, err := decodeCryptoResponse("Encrypt", []byte(`{bad`))
	require.Error(t, err)
}
