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

package quic

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
	sdlcclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// =============================================================================
// go-quicbaas#606 — ListRunners / ListForges over HTTP/3.
//
// QUIC is the REST surface on HTTP/3: this transport embeds *rest.Transport and
// swaps only the RoundTripper. Both verbs are driven against a real HTTP/3
// server here so that "QUIC carries it" is proven on the wire rather than
// inferred from the embedding.
// =============================================================================

// TestRequest_ListRunnersOverHTTP3 proves the runner registry verb reaches the
// same GET route the REST transport builds and decodes into the SDK envelope.
func TestRequest_ListRunnersOverHTTP3(t *testing.T) {
	gotMethod := make(chan string, 1)
	gotPath := make(chan string, 1)

	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod <- r.Method
		gotPath <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			`{"runners":[{"name":"sdlc","states":[{"state":"implementing","label":"Implementing","session_backed":true}]}],"count":1}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "ListRunners", nil)
	require.NoError(t, err)

	out, ok := resp.(*orchestratorclient.ListJobRunnersResponse)
	require.True(t, ok, "transport returned %T", resp)
	require.Len(t, out.Runners, 1)
	assert.Equal(t, 1, out.Count)
	assert.Equal(t, "sdlc", out.Runners[0].Name)
	require.Len(t, out.Runners[0].States, 1)
	assert.Equal(t, "implementing", out.Runners[0].States[0].State)
	assert.True(t, out.Runners[0].States[0].SessionBacked)

	assert.Equal(t, http.MethodGet, <-gotMethod)
	assert.Equal(t, "/api/v1/orchestrator/runners", <-gotPath)
}

func TestRequest_ListWorkflowsOverHTTP3(t *testing.T) {
	gotMethod := make(chan string, 1)
	gotPath := make(chan string, 1)
	gotLimit := make(chan string, 1)
	gotSort := make(chan string, 1)
	gotQuery := make(chan string, 1)

	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod <- r.Method
		gotPath <- r.URL.Path
		gotLimit <- r.URL.Query().Get("limit")
		gotSort <- r.URL.Query().Get("sort")
		gotQuery <- r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"workflows":[{"name":"delivery","mode":"canned","agents":[]}],"total":3,"limit":1,"offset":0,"next_offset":1}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "ListWorkflows", &orchestratorclient.ListWorkflowsRequest{
		TenantID: "tenant-a",
		Params:   page.Params{Limit: 1, Sort: "name:desc", Query: "del"},
	})
	require.NoError(t, err)

	out, ok := resp.(*orchestratorclient.ListWorkflowsResponse)
	require.True(t, ok, "transport returned %T", resp)
	require.Len(t, out.Workflows, 1)
	assert.Equal(t, "delivery", out.Workflows[0].Name)
	assert.Equal(t, 3, out.Total)
	assert.Equal(t, 1, out.Limit)
	assert.Equal(t, 1, out.NextOffset)

	assert.Equal(t, http.MethodGet, <-gotMethod)
	assert.Equal(t, "/api/v1/orchestrator/workflows", <-gotPath)
	assert.Equal(t, "1", <-gotLimit)
	assert.Equal(t, "name:desc", <-gotSort)
	assert.Equal(t, "del", <-gotQuery)
}

// TestRequest_ListForgesOverHTTP3 proves the forge-adapter verb reaches the same
// GET route the REST transport builds and decodes into the SDK envelope.
func TestRequest_ListForgesOverHTTP3(t *testing.T) {
	gotMethod := make(chan string, 1)
	gotPath := make(chan string, 1)

	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod <- r.Method
		gotPath <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"forges":["gitea","github"],"count":2}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.SDLC", "ListForges", &sdlcclient.ListForgesRequest{})
	require.NoError(t, err)

	out, ok := resp.(*sdlcclient.ListForgesResponse)
	require.True(t, ok, "transport returned %T", resp)
	assert.Equal(t, []string{"gitea", "github"}, out.Forges,
		"the server's sort order must survive the HTTP/3 round trip")
	assert.Equal(t, 2, out.Count)

	assert.Equal(t, http.MethodGet, <-gotMethod)
	assert.Equal(t, "/api/v1/sdlc/forges", <-gotPath)
}

// TestRequest_ListForgesOverHTTP3SurfacesRefusal proves a node that does not run
// the module refuses through to the caller rather than decoding the error body
// into an empty forge list.
func TestRequest_ListForgesOverHTTP3SurfacesRefusal(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"sdlc module is not running"}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = tr.Request(context.Background(), "quicbaas.SDLC", "ListForges", &sdlcclient.ListForgesRequest{})
	require.Error(t, err, "a refusing node produced no error")
}
