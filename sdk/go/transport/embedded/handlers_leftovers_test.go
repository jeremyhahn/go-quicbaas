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

package embedded

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

// =============================================================================
// Unary KV List handler (handleList)
// =============================================================================

func TestHandleList_Unary(t *testing.T) {
	ctx := context.Background()

	t.Run("single response", func(t *testing.T) {
		mock := &mockQuicBaaS{
			listFunc: func(_ context.Context, _ *service.ListRequest, stream chan<- *service.ListResponse) error {
				stream <- &service.ListResponse{Entities: []*service.Entity{{ID: 1}}, TotalCount: 1}
				close(stream)
				return nil
			},
		}
		tr, err := New(mock)
		require.NoError(t, err)
		resp, err := tr.Request(ctx, "quicbaas.KV", "List", &service.ListRequest{})
		require.NoError(t, err)
		listResp, ok := resp.(*service.ListResponse)
		require.True(t, ok)
		assert.Len(t, listResp.Entities, 1)
	})

	t.Run("multiple responses accumulate", func(t *testing.T) {
		mock := &mockQuicBaaS{
			listFunc: func(_ context.Context, _ *service.ListRequest, stream chan<- *service.ListResponse) error {
				stream <- &service.ListResponse{Entities: []*service.Entity{{ID: 1}}}
				stream <- &service.ListResponse{Entities: []*service.Entity{{ID: 2}}}
				close(stream)
				return nil
			},
		}
		tr, err := New(mock)
		require.NoError(t, err)
		resp, err := tr.Request(ctx, "quicbaas.KV", "List", &service.ListRequest{})
		require.NoError(t, err)
		listResp := resp.(*service.ListResponse)
		assert.Len(t, listResp.Entities, 2)
	})

	t.Run("empty stream yields zero-value response", func(t *testing.T) {
		mock := &mockQuicBaaS{
			listFunc: func(_ context.Context, _ *service.ListRequest, stream chan<- *service.ListResponse) error {
				close(stream)
				return nil
			},
		}
		tr, err := New(mock)
		require.NoError(t, err)
		resp, err := tr.Request(ctx, "quicbaas.KV", "List", &service.ListRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Empty(t, resp.(*service.ListResponse).Entities)
	})

	t.Run("service error", func(t *testing.T) {
		wantErr := errors.New("list failed")
		mock := &mockQuicBaaS{
			listFunc: func(_ context.Context, _ *service.ListRequest, stream chan<- *service.ListResponse) error {
				close(stream)
				return wantErr
			},
		}
		tr, err := New(mock)
		require.NoError(t, err)
		resp, err := tr.Request(ctx, "quicbaas.KV", "List", &service.ListRequest{})
		require.ErrorIs(t, err, wantErr)
		assert.Nil(t, resp)
	})

	t.Run("wrong request type", func(t *testing.T) {
		tr, err := New(&mockQuicBaaS{})
		require.NoError(t, err)
		resp, err := tr.Request(ctx, "quicbaas.KV", "List", &service.PutRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
	})
}

// =============================================================================
// Agent admin handlers (Disable/Enable)
// =============================================================================

func TestOrchestratorAgentAdmin(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		method string
		req    interface{}
	}{
		{"DisableFleetNode", &orchestratorSDK.DisableFleetNodeRequest{NodeID: "a1"}},
		{"EnableFleetNode", &orchestratorSDK.EnableFleetNodeRequest{NodeID: "a1"}},
	}

	t.Run("success", func(t *testing.T) {
		tr := newOrchTransport(t, false)
		for _, c := range cases {
			resp, err := tr.Request(ctx, "quicbaas.Orchestrator", c.method, c.req)
			require.NoError(t, err, c.method)
			assert.NotNil(t, resp, c.method)
		}
	})

	t.Run("service error", func(t *testing.T) {
		tr := newOrchTransport(t, true)
		for _, c := range cases {
			resp, err := tr.Request(ctx, "quicbaas.Orchestrator", c.method, c.req)
			require.ErrorIs(t, err, errOrch, c.method)
			assert.Nil(t, resp, c.method)
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		tr := newOrchTransport(t, false)
		for _, c := range cases {
			resp, err := tr.Request(ctx, "quicbaas.Orchestrator", c.method, &service.PutRequest{})
			require.Error(t, err, c.method)
			assert.Nil(t, resp, c.method)
		}
	})

	t.Run("admin not configured", func(t *testing.T) {
		// orchMock with nil admin service.
		tr, err := New(&orchMock{
			mockQuicBaaS: &mockQuicBaaS{},
			agent:        &fakeAgentService{},
			proj:         &fakeProjectService{},
			admin:        nil,
		})
		require.NoError(t, err)
		for _, c := range cases {
			resp, err := tr.Request(ctx, "quicbaas.Orchestrator", c.method, c.req)
			require.Error(t, err, c.method)
			assert.Nil(t, resp, c.method)
		}
	})
}

// =============================================================================
// Secrets Ensure handler
// =============================================================================

func TestHandleSecretsEnsure(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		tr, err := New(&mockQuicBaaS{secretsService: &mockSecretsService{}})
		require.NoError(t, err)
		resp, err := tr.Request(ctx, "quicbaas.Secrets", "Ensure", &secretsclient.EnsureSecretRequest{Path: "p", Length: 16})
		require.NoError(t, err)
		ensured, ok := resp.(*secretsclient.EnsureSecretResponse)
		require.True(t, ok)
		assert.Equal(t, "p", ensured.Path)
		assert.True(t, ensured.Generated)
	})

	t.Run("service error", func(t *testing.T) {
		wantErr := errors.New("ensure failed")
		mock := &mockSecretsService{
			ensureFunc: func(_ context.Context, _ *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
				return nil, wantErr
			},
		}
		tr, err := New(&mockQuicBaaS{secretsService: mock})
		require.NoError(t, err)
		resp, err := tr.Request(ctx, "quicbaas.Secrets", "Ensure", &secretsclient.EnsureSecretRequest{Path: "p"})
		require.ErrorIs(t, err, wantErr)
		assert.Nil(t, resp)
	})

	t.Run("wrong type", func(t *testing.T) {
		tr, err := New(&mockQuicBaaS{secretsService: &mockSecretsService{}})
		require.NoError(t, err)
		resp, err := tr.Request(ctx, "quicbaas.Secrets", "Ensure", &service.PutRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("not configured", func(t *testing.T) {
		tr, err := New(&mockQuicBaaS{})
		require.NoError(t, err)
		resp, err := tr.Request(ctx, "quicbaas.Secrets", "Ensure", &secretsclient.EnsureSecretRequest{Path: "p"})
		require.Error(t, err)
		assert.Nil(t, resp)
	})
}
