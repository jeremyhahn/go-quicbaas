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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// sealedWindowStubHeader marks a response served by the stub sealed-window
// handler, standing in for qrdb's rest.NewSealedWindowHandler.
const sealedWindowStubHeader = "X-Test-Sealed-Window"

// sealedWindowRouteProbe turns one REST allowlist entry ("METHOD /path",
// "/path" or the "/prefix/" form) into the methods and a concrete path to
// route-match it with.
func sealedWindowRouteProbe(entry string) ([]string, string) {
	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}
	path := entry
	if method, rest, ok := strings.Cut(entry, " "); ok {
		methods = []string{method}
		path = rest
	}
	if strings.HasSuffix(path, "/") {
		path += "probe"
	}
	return methods, path
}

// TestRESTSealedWindow_EveryAllowlistedRouteIsMounted pins that the sealed
// REST listener serves every route its own sealed allowlist names: an
// allowlisted path the native router does not carry is mounted on the
// sealed-window handler instead of falling through to 404.
func TestRESTSealedWindow_EveryAllowlistedRouteIsMounted(t *testing.T) {
	s := newSealedGateRESTServer(t, &restSealedProbe{sealed: true})
	s.SetSealedWindowHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(sealedWindowStubHeader, "1")
		w.WriteHeader(http.StatusOK)
	}))

	for _, entry := range sealedwindow.RESTAllowlist() {
		methods, path := sealedWindowRouteProbe(entry)
		mounted := false
		for _, method := range methods {
			if s.router.Match(chi.NewRouteContext(), method, path) {
				mounted = true
				break
			}
		}
		assert.True(t, mounted, "allowlisted sealed REST route %q is not mounted", entry)
	}

	// Officer registration has no native quicbaas handler: it must reach
	// the sealed-window handler, the one the unix and gRPC windows serve.
	req := httptest.NewRequest(http.MethodPost, "/v1/barrier/officers", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body %q", rec.Body.String())
	assert.Equal(t, "1", rec.Header().Get(sealedWindowStubHeader))

	// A natively served allowlisted route keeps its native handler.
	req = httptest.NewRequest(http.MethodGet, "/v1/barrier/status", nil)
	rec = httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	assert.Empty(t, rec.Header().Get(sealedWindowStubHeader))
}
