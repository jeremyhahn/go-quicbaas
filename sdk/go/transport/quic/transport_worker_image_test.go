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

	sdlcclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// TestRequest_WorkerImageVerbsOverHTTP3 proves QUIC carries the worker image
// refresh's two host verbs to the same routes the REST transport builds.
func TestRequest_WorkerImageVerbsOverHTTP3(t *testing.T) {
	type hit struct{ method, path string }
	hits := make(chan hit, 2)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- hit{r.Method, r.URL.Path}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
		}
		_, _ = w.Write([]byte(`{"serving":true,"queued":true,"last":{"outcome":"current","latest":{"claude_code":"2.1.280","codex":"0.156.0"}}}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	for method, want := range map[string]hit{
		"WorkerImageStatus":  {http.MethodGet, "/api/v1/sdlc/worker-image"},
		"RefreshWorkerImage": {http.MethodPost, "/api/v1/sdlc/worker-image/refresh"},
	} {
		resp, err := tr.Request(context.Background(), "quicbaas.SDLC", method, nil)
		require.NoError(t, err, method)
		status, ok := resp.(*sdlcclient.WorkerImageStatus)
		require.True(t, ok, "%s returned %T", method, resp)
		assert.True(t, status.Serving)
		require.NotNil(t, status.Last)
		assert.Equal(t, "2.1.280", status.Last.Latest.ClaudeCode)
		assert.Equal(t, want, <-hits, method)
	}
}

// TestRequest_WorkerImageRefreshOverHTTP3SurfacesRefusal proves a busy loop's
// 409 reaches the caller as an error.
func TestRequest_WorkerImageRefreshOverHTTP3SurfacesRefusal(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"worker image: a refresh is already in progress"}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = tr.Request(context.Background(), "quicbaas.SDLC", "RefreshWorkerImage", nil)
	require.Error(t, err)
}
