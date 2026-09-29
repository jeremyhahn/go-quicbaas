//go:build rest

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

package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/workerimage"
)

// workerImageLoop is a refresh loop that answers from fields.
type workerImageLoop struct {
	status workerimage.Status
	err    error
}

func (l *workerImageLoop) RequestRefresh() error      { return l.err }
func (l *workerImageLoop) Status() workerimage.Status { return l.status }

func workerImageCall(t *testing.T, server *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Get("/api/v1/sdlc/worker-image", server.HandleGetSDLCWorkerImage)
	r.Post("/api/v1/sdlc/worker-image/refresh", server.HandleRefreshSDLCWorkerImage)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestSDLCWorkerImage_StatusReportsTheLastPass(t *testing.T) {
	server, svc := setupSDLCServer(t)
	svc.SetWorkerImageRefresher(&workerImageLoop{status: workerimage.Status{
		Serving: true,
		Last:    &workerimage.Report{Outcome: workerimage.OutcomePromoted, Reference: "reg/o/w:1@sha256:abc"},
	}})

	rec := workerImageCall(t, server, http.MethodGet, "/api/v1/sdlc/worker-image")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got workerimage.Status
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.True(t, got.Serving)
	require.NotNil(t, got.Last)
	assert.Equal(t, workerimage.OutcomePromoted, got.Last.Outcome)
	assert.Equal(t, "reg/o/w:1@sha256:abc", got.Last.Reference)
}

func TestSDLCWorkerImage_StatusWithNoLoopIsNotServing(t *testing.T) {
	server, _ := setupSDLCServer(t)
	rec := workerImageCall(t, server, http.MethodGet, "/api/v1/sdlc/worker-image")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got workerimage.Status
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.False(t, got.Serving)
}

func TestSDLCWorkerImage_RefreshIsAcceptedOrRefused(t *testing.T) {
	cases := map[string]struct {
		loop *workerImageLoop
		want int
	}{
		"queued":       {loop: &workerImageLoop{status: workerimage.Status{Serving: true, Queued: true}}, want: http.StatusAccepted},
		"busy":         {loop: &workerImageLoop{err: &workerimage.RefreshInProgressError{}}, want: http.StatusConflict},
		"no loop here": {loop: nil, want: http.StatusServiceUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			server, svc := setupSDLCServer(t)
			if tc.loop != nil {
				svc.SetWorkerImageRefresher(tc.loop)
			}
			rec := workerImageCall(t, server, http.MethodPost, "/api/v1/sdlc/worker-image/refresh")
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
}

func TestSDLCWorkerImage_ModuleNotRunningIs503(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	mock.SetSDLCService(nil)
	for _, route := range [][2]string{{http.MethodGet, "/api/v1/sdlc/worker-image"}, {http.MethodPost, "/api/v1/sdlc/worker-image/refresh"}} {
		rec := workerImageCall(t, server, route[0], route[1])
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, route[1])
	}
}
