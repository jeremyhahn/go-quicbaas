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
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdlcclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// TestRequest_ARebindOverHTTP3KeepsTheTrainAndClustering is the QUIC half of
// the rebind regression for the throughput policies.
func TestRequest_ARebindOverHTTP3KeepsTheTrainAndClustering(t *testing.T) {
	var mu sync.Mutex
	var held []byte
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			held = body
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(held)
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()
	ctx := context.Background()
	want := sdlcclient.ProjectBinding{
		ProjectID:  "p1",
		MergeTrain: sdlcclient.MergeTrainPolicy{Disabled: true, MaxSize: 7},
		Clustering: sdlcclient.ClusteringPolicy{MaxSize: 3},
	}
	_, err = tr.Request(ctx, "quicbaas.SDLC", "Bind", &sdlcclient.BindRequest{ProjectID: "p1", Binding: want})
	require.NoError(t, err)
	read, err := tr.Request(ctx, "quicbaas.SDLC", "Binding", &sdlcclient.GetBindingRequest{ProjectID: "p1"})
	require.NoError(t, err)
	rebound, err := tr.Request(ctx, "quicbaas.SDLC", "Bind", &sdlcclient.BindRequest{ProjectID: "p1", Binding: *read.(*sdlcclient.ProjectBinding)})
	require.NoError(t, err)
	got := rebound.(*sdlcclient.ProjectBinding)
	assert.Equal(t, want.MergeTrain, got.MergeTrain)
	assert.Equal(t, want.Clustering, got.Clustering)
}

// TestRequest_APausedBindOverHTTP3SendsPaused: the paused flag reaches the
// server as the query parameter the REST handler reads.
func TestRequest_APausedBindOverHTTP3SendsPaused(t *testing.T) {
	var mu sync.Mutex
	var query string
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		query = r.URL.RawQuery
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"p1"}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()
	_, err = tr.Request(context.Background(), "quicbaas.SDLC", "Bind",
		&sdlcclient.BindRequest{ProjectID: "p1", Binding: sdlcclient.ProjectBinding{ProjectID: "p1"}, Paused: true})
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "paused=true", query)
}
