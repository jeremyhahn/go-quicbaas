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
	"strconv"
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

	t.Run("pages follow NextPageToken", func(t *testing.T) {
		// The mock keeps the service contract: one page of 5 over IDs 1..20
		// per call, resuming after the ID named in PageToken.
		mock := &mockQuicBaaS{
			listFunc: func(_ context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error {
				defer close(stream)
				start := uint64(1)
				if req.PageToken != "" {
					last, err := strconv.ParseUint(req.PageToken, 10, 64)
					if err != nil {
						return &service.InvalidPageTokenError{Token: req.PageToken}
					}
					start = last + 1
				}
				page := &service.ListResponse{TotalCount: 20}
				for id := start; id < start+req.PageSize && id <= 20; id++ {
					page.Entities = append(page.Entities, &service.Entity{ID: id})
				}
				if last := start + req.PageSize - 1; last < 20 {
					page.NextPageToken = strconv.FormatUint(last, 10)
				}
				stream <- page
				return nil
			},
		}
		tr, err := New(mock)
		require.NoError(t, err)

		var ids []uint64
		token := ""
		for page := 1; page <= 4; page++ {
			resp, err := tr.Request(ctx, "quicbaas.KV", "List", &service.ListRequest{PageSize: 5, PageToken: token})
			require.NoError(t, err)
			listResp := resp.(*service.ListResponse)
			require.Len(t, listResp.Entities, 5, "page %d", page)
			assert.Equal(t, uint64(20), listResp.TotalCount)
			for _, e := range listResp.Entities {
				ids = append(ids, e.ID)
			}
			token = listResp.NextPageToken
			if page < 4 {
				require.NotEmpty(t, token, "page %d", page)
			}
		}
		assert.Empty(t, token, "last page carries no token")
		require.Len(t, ids, 20)
		assert.Equal(t, uint64(1), ids[0])
		assert.Equal(t, uint64(20), ids[19])

		resp, err := tr.Request(ctx, "quicbaas.KV", "List", &service.ListRequest{PageSize: 5, PageToken: "bogus"})
		var tokenErr *service.InvalidPageTokenError
		require.ErrorAs(t, err, &tokenErr)
		assert.Nil(t, resp)
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
