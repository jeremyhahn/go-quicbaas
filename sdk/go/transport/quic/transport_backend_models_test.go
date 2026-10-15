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
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestListBackendModels_RoundTripsOverHTTP3 drives the SDK client over QUIC,
// which is the REST surface over HTTP/3: the node, backend and tenant land on
// GET /api/v1/fleet/{nodeID}/backends/{backendName}/models, every response
// field comes back, and the daemon's 404 and 400 arrive coded.
func TestListBackendModels_RoundTripsOverHTTP3(t *testing.T) {
	gotURL := make(chan string, 1)

	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL <- r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/fleet/any/backends/missing/models":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"acp: backend not found: missing"}`))
		case "/api/v1/fleet/any/backends/bad/models":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"backend name is malformed"}`))
		default:
			_, _ = w.Write([]byte(`{"backend":"ollama","models":["llama3","qwen2"],"count":2}`))
		}
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()
	client := orchestratorclient.NewClient(tr)
	ctx := context.Background()

	resp, err := client.ListBackendModels(ctx, &orchestratorclient.ListBackendModelsRequest{
		NodeID: "node-7", BackendName: "ollama", TenantID: "t1",
	})
	require.NoError(t, err)
	assert.Equal(t, &orchestratorclient.ListBackendModelsResponse{
		Backend: "ollama", Models: []string{"llama3", "qwen2"}, Count: 2,
	}, resp)
	assert.Equal(t, "/api/v1/fleet/node-7/backends/ollama/models?tenant_id=t1", <-gotURL)

	_, err = client.ListBackendModels(ctx, &orchestratorclient.ListBackendModelsRequest{BackendName: "missing"})
	<-gotURL
	assert.True(t, errors.IsNotFound(err), "unknown backend: got %v", err)

	_, err = client.ListBackendModels(ctx, &orchestratorclient.ListBackendModelsRequest{BackendName: "bad"})
	<-gotURL
	assert.True(t, errors.IsInvalidInput(err), "malformed backend: got %v", err)
}
