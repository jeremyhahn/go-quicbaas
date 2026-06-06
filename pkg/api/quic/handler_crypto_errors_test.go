//go:build quic

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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The symmetric/asymmetric crypto handlers share a common shape: nil-service,
// decode-error, and service-error branches. The HMAC handlers already cover
// these; this file fills the gap for Encrypt/Decrypt/Rewrap/Sign/Verify/
// Hash/Random.

func cryptoPayload(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := EncodePayload(v)
	require.NoError(t, err)
	return b
}

func TestCryptoHandlers_ServiceErrors(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, &errorCryptoService{}, createTestLogger())
	ctx := context.Background()

	cases := []struct {
		name    string
		opcode  byte
		payload []byte
	}{
		{"encrypt", OpCryptoEncrypt, cryptoPayload(t, &service.EncryptRequest{KeyID: "k"})},
		{"decrypt", OpCryptoDecrypt, cryptoPayload(t, &service.DecryptRequest{KeyID: "k"})},
		{"rewrap", OpCryptoRewrap, cryptoPayload(t, &service.RewrapRequest{})},
		{"sign", OpCryptoSign, cryptoPayload(t, &service.SignRequest{KeyID: "k"})},
		{"verify", OpCryptoVerify, cryptoPayload(t, &service.VerifyRequest{KeyID: "k"})},
		{"hash", OpCryptoHash, cryptoPayload(t, &service.HashRequest{})},
		{"random", OpCryptoRandom, cryptoPayload(t, &service.GenerateRandomRequest{NumBytes: 16})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, &Request{Opcode: tc.opcode, Payload: tc.payload})
			assert.NotEqual(t, StatusOK, resp.Status, "expected error from crypto service")
		})
	}
}

func TestCryptoHandlers_DecodeErrors(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, &securityTestCryptoService{}, createTestLogger())
	ctx := context.Background()
	bad := []byte("{not-json")

	opcodes := []byte{
		OpCryptoEncrypt, OpCryptoDecrypt, OpCryptoRewrap,
		OpCryptoSign, OpCryptoVerify, OpCryptoHash, OpCryptoRandom,
	}
	for _, op := range opcodes {
		resp := handler.HandleRequest(ctx, &Request{Opcode: op, Payload: bad})
		assert.NotEqual(t, StatusOK, resp.Status, "opcode 0x%02X should reject malformed body", op)
	}
}

func TestCryptoHandlers_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
	ctx := context.Background()

	opcodes := []byte{
		OpCryptoEncrypt, OpCryptoDecrypt, OpCryptoRewrap,
		OpCryptoSign, OpCryptoVerify, OpCryptoHash, OpCryptoRandom,
	}
	for _, op := range opcodes {
		resp := handler.HandleRequest(ctx, &Request{Opcode: op, Payload: []byte("{}")})
		assert.NotEqual(t, StatusOK, resp.Status, "opcode 0x%02X should fail with nil crypto service", op)
	}
}
