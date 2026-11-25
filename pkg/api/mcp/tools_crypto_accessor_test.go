//go:build mcp

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

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// cryptoAccessorService is shaped like the production service: the crypto
// verbs are reachable only through Crypto(), never at the top level.
type cryptoAccessorService struct {
	service.QuicBaaS
	crypto service.CryptoService
}

func (s cryptoAccessorService) Crypto() service.CryptoService { return s.crypto }

// TestCryptoTools_UseCryptoAccessor checks that the MCP crypto tools reach
// the crypto service through QuicBaaS.Crypto() and not by asserting crypto
// verbs on the top-level service.
func TestCryptoTools_UseCryptoAccessor(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	want := []byte{0xde, 0xad, 0xbe, 0xef}
	wantB64 := base64.StdEncoding.EncodeToString(want)
	msgB64 := base64.StdEncoding.EncodeToString([]byte("hello"))

	tests := []struct {
		name string
		mock *mocks.MockCryptoService
		call func(r *ToolRegistry) (interface{}, error)
	}{
		{
			name: "random",
			mock: &mocks.MockCryptoService{
				GenerateRandomFunc: func(context.Context, *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
					return &service.GenerateRandomResponse{RandomBytes: want}, nil
				},
			},
			call: func(r *ToolRegistry) (interface{}, error) {
				return r.executeCryptoRandom(context.Background(), map[string]interface{}{"num_bytes": float64(4)})
			},
		},
		{
			name: "hash",
			mock: &mocks.MockCryptoService{
				HashFunc: func(context.Context, *service.HashRequest) (*service.HashResponse, error) {
					return &service.HashResponse{Algorithm: "SHA256", Hash: want}, nil
				},
			},
			call: func(r *ToolRegistry) (interface{}, error) {
				return r.executeCryptoHash(context.Background(), map[string]interface{}{"data": msgB64, "algorithm": "SHA256"})
			},
		},
		{
			name: "sign",
			mock: &mocks.MockCryptoService{
				SignFunc: func(context.Context, *service.SignRequest) (*service.SignResponse, error) {
					return &service.SignResponse{KeyID: "k", Signature: want}, nil
				},
			},
			call: func(r *ToolRegistry) (interface{}, error) {
				return r.executeCryptoSign(context.Background(), map[string]interface{}{"key_id": "k", "message": msgB64})
			},
		},
		{
			name: "data key",
			mock: &mocks.MockCryptoService{
				GenerateDataKeyFunc: func(context.Context, *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
					return &service.GenerateDataKeyResponse{KeyID: "k", Plaintext: want, CiphertextBlob: want}, nil
				},
			},
			call: func(r *ToolRegistry) (interface{}, error) {
				return r.executeCryptoDataKey(context.Background(), map[string]interface{}{"key_id": "k"})
			},
		},
		{
			name: "rewrap",
			mock: &mocks.MockCryptoService{
				RewrapFunc: func(context.Context, *service.RewrapRequest) (*service.RewrapResponse, error) {
					return &service.RewrapResponse{KeyID: "k", Ciphertext: want}, nil
				},
			},
			call: func(r *ToolRegistry) (interface{}, error) {
				return r.executeCryptoRewrap(context.Background(), map[string]interface{}{"key_id": "k", "ciphertext": msgB64})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewToolRegistry(cryptoAccessorService{QuicBaaS: &mockService{}, crypto: tt.mock}, log)
			result, err := tt.call(registry)
			require.NoError(t, err)
			out, err := json.Marshal(result)
			require.NoError(t, err)
			assert.Contains(t, string(out), wantB64)
		})
	}
}
