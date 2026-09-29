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

package embedded

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	secrets "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

// TestSecretsGetWithBytes_Embedded drives Client.GetWithBytes through the
// embedded transport: the service's payload buffer must reach the client
// undecoded and be wiped once the borrow returns. The lent value is compared
// in place inside fn, never copied out (testing-guidelines.md).
func TestSecretsGetWithBytes_Embedded(t *testing.T) {
	payload := []byte(`{"password":"hunter2"}`)
	var gotReq *service.GetSecretRequest
	secretsMock := &mockSecretsService{
		getFunc: func(_ context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
			gotReq = req
			return &service.GetSecretResponse{SecretID: req.SecretID, Version: 2, Data: payload}, nil
		},
	}
	tr, err := New(&mockQuicBaaS{secretsService: secretsMock})
	require.NoError(t, err)

	matched := false
	err = secrets.NewClient(tr).GetWithBytes(context.Background(), "bgp/peer1", "password",
		func(b []byte) error {
			matched = bytes.Equal(b, []byte("hunter2"))
			return nil
		}, secrets.WithVersion(2))
	require.NoError(t, err)
	assert.True(t, matched, "lent bytes differ from the stored field")
	require.NotNil(t, gotReq)
	assert.Equal(t, "bgp/peer1", gotReq.SecretID)
	assert.Equal(t, uint64(2), gotReq.Version)
	assert.Equal(t, make([]byte, len(payload)), payload, "service payload not wiped")
}

func TestHandleSecretsGetData_Errors(t *testing.T) {
	type getSecretRequest struct {
		Path    string
		Version int
	}
	ctx := context.Background()

	t.Run("service not configured", func(t *testing.T) {
		tr, err := New(&mockQuicBaaS{})
		require.NoError(t, err)
		_, err = tr.Request(ctx, "quicbaas.Secrets", "GetData", &getSecretRequest{Path: "p"})
		var svcErr *ServiceNotConfiguredError
		assert.ErrorAs(t, err, &svcErr)
	})

	t.Run("invalid request type", func(t *testing.T) {
		tr, err := New(&mockQuicBaaS{secretsService: &mockSecretsService{}})
		require.NoError(t, err)
		for _, req := range []interface{}{42, &struct{ Path string }{"p"}, &struct {
			Path    int
			Version int
		}{}} {
			_, err = tr.Request(ctx, "quicbaas.Secrets", "GetData", req)
			var typeErr *InvalidRequestTypeError
			assert.ErrorAs(t, err, &typeErr)
		}
	})

	t.Run("nil service response", func(t *testing.T) {
		tr, err := New(&mockQuicBaaS{secretsService: &mockSecretsService{
			getFunc: func(context.Context, *service.GetSecretRequest) (*service.GetSecretResponse, error) {
				return nil, nil
			},
		}})
		require.NoError(t, err)
		resp, err := tr.Request(ctx, "quicbaas.Secrets", "GetData", &getSecretRequest{Path: "p"})
		require.NoError(t, err)
		assert.Nil(t, resp, "a typed nil must not reach the client as a non-nil interface")
	})
}
