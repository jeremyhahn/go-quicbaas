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

// handleKMSExportKey / handleKMSUnwrapKey share the nil-service, decode-error,
// service-error, and happy-path branches. Exercise each.

func TestHandleKMSExportKey_AllBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("happy", func(t *testing.T) {
		handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createTestLogger())
		body, err := EncodePayload(&service.ExportKeyRequest{KeyID: "k1"})
		require.NoError(t, err)
		resp := handler.HandleRequest(ctx, &Request{Opcode: OpKMSExportKey, Payload: body})
		assert.Equal(t, StatusOK, resp.Status)
	})

	t.Run("nil-service", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		body, _ := EncodePayload(&service.ExportKeyRequest{})
		resp := handler.HandleRequest(ctx, &Request{Opcode: OpKMSExportKey, Payload: body})
		assert.NotEqual(t, StatusOK, resp.Status)
	})

	t.Run("decode-error", func(t *testing.T) {
		handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createTestLogger())
		resp := handler.HandleRequest(ctx, &Request{Opcode: OpKMSExportKey, Payload: []byte("{bad")})
		assert.NotEqual(t, StatusOK, resp.Status)
	})

	t.Run("service-error", func(t *testing.T) {
		handler := NewHandler(&mockService{}, &errorKMSService{}, nil, nil, createTestLogger())
		body, _ := EncodePayload(&service.ExportKeyRequest{KeyID: "k1"})
		resp := handler.HandleRequest(ctx, &Request{Opcode: OpKMSExportKey, Payload: body})
		assert.NotEqual(t, StatusOK, resp.Status)
	})
}

func TestHandleKMSUnwrapKey_AllBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("happy", func(t *testing.T) {
		handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createTestLogger())
		body, err := EncodePayload(&service.UnwrapKeyRequest{})
		require.NoError(t, err)
		resp := handler.HandleRequest(ctx, &Request{Opcode: OpKMSUnwrapKey, Payload: body})
		assert.Equal(t, StatusOK, resp.Status)
	})

	t.Run("nil-service", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		body, _ := EncodePayload(&service.UnwrapKeyRequest{})
		resp := handler.HandleRequest(ctx, &Request{Opcode: OpKMSUnwrapKey, Payload: body})
		assert.NotEqual(t, StatusOK, resp.Status)
	})

	t.Run("decode-error", func(t *testing.T) {
		handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createTestLogger())
		resp := handler.HandleRequest(ctx, &Request{Opcode: OpKMSUnwrapKey, Payload: []byte("{bad")})
		assert.NotEqual(t, StatusOK, resp.Status)
	})

	t.Run("service-error", func(t *testing.T) {
		handler := NewHandler(&mockService{}, &errorKMSService{}, nil, nil, createTestLogger())
		body, _ := EncodePayload(&service.UnwrapKeyRequest{})
		resp := handler.HandleRequest(ctx, &Request{Opcode: OpKMSUnwrapKey, Payload: body})
		assert.NotEqual(t, StatusOK, resp.Status)
	})
}
