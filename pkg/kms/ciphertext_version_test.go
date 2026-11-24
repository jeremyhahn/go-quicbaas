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

package kms

import (
	"encoding/json"
	"testing"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"
)

func encodeCiphertext(t *testing.T, metadata map[string][]byte) []byte {
	t.Helper()
	data, err := json.Marshal(kctypes.EncryptedData{Ciphertext: []byte("ct"), Metadata: metadata})
	require.NoError(t, err)
	return data
}

func TestCiphertextKeyVersion(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string][]byte
		want     uint64
		wantErr  bool
	}{
		{name: "no metadata", metadata: nil, want: 1},
		{name: "metadata without kv", metadata: map[string][]byte{"other": []byte("x")}, want: 1},
		{name: "kv 1", metadata: map[string][]byte{keyVersionMetadataKey: []byte("1")}, want: 1},
		{name: "kv 7", metadata: map[string][]byte{keyVersionMetadataKey: []byte("7")}, want: 7},
		{name: "kv unparsable", metadata: map[string][]byte{keyVersionMetadataKey: []byte("abc")}, wantErr: true},
		{name: "kv negative", metadata: map[string][]byte{keyVersionMetadataKey: []byte("-1")}, wantErr: true},
		{name: "kv zero", metadata: map[string][]byte{keyVersionMetadataKey: []byte("0")}, wantErr: true},
		{name: "kv signed", metadata: map[string][]byte{keyVersionMetadataKey: []byte("+2")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CiphertextKeyVersion(encodeCiphertext(t, tt.metadata))
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidCiphertextKeyVersion)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCiphertextKeyVersion_NotJSON(t *testing.T) {
	_, err := CiphertextKeyVersion([]byte("not json"))
	require.Error(t, err)
}
