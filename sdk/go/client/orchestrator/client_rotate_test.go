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

package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// RotateSessionAccount
// =============================================================================

func TestClient_RotateSessionAccount_Success(t *testing.T) {
	var gotService, gotMethod string
	var gotReq *RotateSessionAccountRequest

	c := NewClient(&mockTransport{
		requestFn: func(_ context.Context, service, method string, req interface{}) (interface{}, error) {
			gotService, gotMethod = service, method
			gotReq = req.(*RotateSessionAccountRequest)
			return &RotateSessionAccountResponse{
				SessionID:         "s1",
				PreviousAccountID: "acct-a",
				AccountID:         "acct-b",
				Strategy:          "least_usage",
				Mode:              "resume",
			}, nil
		},
	})

	resp, err := c.RotateSessionAccount(context.Background(), &RotateSessionAccountRequest{
		SessionID:    "s1",
		Reason:       "seat retired",
		HoldOutgoing: true,
	})
	require.NoError(t, err)

	assert.Equal(t, "quicbaas.Orchestrator", gotService)
	assert.Equal(t, "RotateSessionAccount", gotMethod,
		"the method name is the key every transport dispatches on")
	require.NotNil(t, gotReq)
	assert.Equal(t, "seat retired", gotReq.Reason)
	assert.True(t, gotReq.HoldOutgoing)

	assert.Equal(t, "acct-a", resp.PreviousAccountID)
	assert.Equal(t, "acct-b", resp.AccountID)
	assert.Equal(t, "resume", resp.Mode)
}

func TestClient_RotateSessionAccount_InvalidInput(t *testing.T) {
	reached := false
	c := NewClient(&mockTransport{
		requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			reached = true
			return nil, nil
		},
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := c.RotateSessionAccount(context.Background(), nil)
		require.Error(t, err)
	})

	t.Run("empty session id", func(t *testing.T) {
		_, err := c.RotateSessionAccount(context.Background(), &RotateSessionAccountRequest{})
		require.Error(t, err)
	})

	t.Run("nil context", func(t *testing.T) {
		var nilCtx context.Context
		_, err := c.RotateSessionAccount(nilCtx, &RotateSessionAccountRequest{SessionID: "s1"})
		require.Error(t, err)
	})

	assert.False(t, reached, "an invalid request must never reach the transport")
}

func TestClient_RotateSessionAccount_TransportError(t *testing.T) {
	c := NewClient(&mockTransport{
		requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errors.NewInternal("transport", "boom")
		},
	})

	_, err := c.RotateSessionAccount(context.Background(), &RotateSessionAccountRequest{SessionID: "s1"})
	require.Error(t, err)
}

func TestClient_RotateSessionAccount_BadResponse(t *testing.T) {
	t.Run("nil response", func(t *testing.T) {
		c := NewClient(&mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				return nil, nil
			},
		})
		_, err := c.RotateSessionAccount(context.Background(), &RotateSessionAccountRequest{SessionID: "s1"})
		require.Error(t, err)
	})

	t.Run("wrong response type", func(t *testing.T) {
		c := NewClient(&mockTransport{
			requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
				return &CloseSessionResponse{SessionID: "s1"}, nil
			},
		})
		_, err := c.RotateSessionAccount(context.Background(), &RotateSessionAccountRequest{SessionID: "s1"})
		require.Error(t, err)
	})
}
