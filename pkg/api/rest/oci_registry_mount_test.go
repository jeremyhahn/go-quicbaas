//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
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
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubStatus is a status the router cannot produce itself, so a response
// with it came from the stub.
const stubStatus = 299

// ociStub records the method and path of each request it sees.
type ociStub struct {
	mu   sync.Mutex
	seen []string
}

func (s *ociStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.seen = append(s.seen, r.Method+" "+r.URL.Path)
	s.mu.Unlock()
	w.WriteHeader(stubStatus)
}

func (s *ociStub) last() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) == 0 {
		return ""
	}
	return s.seen[len(s.seen)-1]
}

func serveRoot(s *Server, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func requireOCIUnavailable(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var body struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Errors, 1)
	require.Equal(t, "UNAVAILABLE", body.Errors[0].Code)
}

func TestOCIRegistryMount_UnsetIs503(t *testing.T) {
	s, _ := newTestServer(t)
	requireOCIUnavailable(t, serveRoot(s, http.MethodGet, "/v2/"))
	requireOCIUnavailable(t, serveRoot(s, http.MethodPut, "/v2/repo/manifests/latest"))
}

func TestOCIRegistryMount_ForwardsFullPath(t *testing.T) {
	s, _ := newTestServer(t)
	stub := &ociStub{}
	s.SetOCIRegistryHandler(stub)

	cases := []struct{ method, path string }{
		{http.MethodGet, "/v2"},
		{http.MethodGet, "/v2/"},
		{http.MethodHead, "/v2/repo/blobs/sha256:abc"},
		{http.MethodPost, "/v2/repo/blobs/uploads/"},
		{http.MethodPatch, "/v2/repo/blobs/uploads/u1"},
		{http.MethodPut, "/v2/repo/blobs/uploads/u1"},
		{http.MethodDelete, "/v2/repo/manifests/latest"},
		{http.MethodPost, "/v2/_attestation/challenge"},
	}
	for _, tc := range cases {
		rec := serveRoot(s, tc.method, tc.path)
		require.Equal(t, stubStatus, rec.Code, "%s %s: %s", tc.method, tc.path, rec.Body.String())
		require.Equal(t, tc.method+" "+tc.path, stub.last())
	}
}

func TestOCIRegistryMount_NoPlatformAuthChain(t *testing.T) {
	s, _ := newTestServer(t)
	stub := &ociStub{}
	s.SetOCIRegistryHandler(stub)

	req := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	require.Empty(t, req.Header.Get("Authorization"))
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	require.Equal(t, stubStatus, rec.Code, rec.Body.String())
	require.Equal(t, "GET /v2/", stub.last())
}

func TestOCIRegistryMount_NilClearsHandler(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetOCIRegistryHandler(&ociStub{})
	require.Equal(t, stubStatus, serveRoot(s, http.MethodGet, "/v2/").Code)

	s.SetOCIRegistryHandler(nil)
	requireOCIUnavailable(t, serveRoot(s, http.MethodGet, "/v2/"))
}
