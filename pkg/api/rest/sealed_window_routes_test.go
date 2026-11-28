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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// reachedHandler records whether a request reached next, the real go-qrdb
// sealed-window handler.
type reachedHandler struct {
	next    http.Handler
	reached bool
}

func (h *reachedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.reached = true
	h.next.ServeHTTP(w, r)
}

// newRealSealedWindowRESTServer is a sealed REST server whose sealed-window
// handler is go-qrdb's real one, as the sealed listener installs it.
func newRealSealedWindowRESTServer(t *testing.T) (*Server, *reachedHandler) {
	t.Helper()
	probe := &restSealedProbe{sealed: true}
	s, mockSvc := newTestServer(t)
	mockSvc.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
		Status: service.HealthStatusHealthy,
		NodeID: 1,
	}, nil).Maybe()
	s.SetBarrierService(&fakeBarrierService{statusFn: func(context.Context) (*qrdbtransport.BarrierStatusResponse, error) {
		return &qrdbtransport.BarrierStatusResponse{Sealed: true, Strategy: "shamir"}, nil
	}})
	s.SetSealedWindowGate(probe, &restCeremonyStateProbe{}, sealedwindow.RESTAllowlist())
	h := &reachedHandler{next: qrdbsdk.NewSealedWindowHandler(nil, nil, nil, qrdbsdk.NewSealedWindowListenerProbe(probe, nil))}
	s.SetSealedWindowHandler(h)
	return s, h
}

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
	s, h := newRealSealedWindowRESTServer(t)

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
	// go-qrdb answers it without a barrier service here, so only the
	// routing is asserted.
	h.reached = false
	req := httptest.NewRequest(http.MethodPost, "/v1/barrier/officers", nil)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	assert.True(t, h.reached, "POST /v1/barrier/officers did not reach the sealed-window handler")

	// A natively served allowlisted route keeps its native handler.
	h.reached = false
	req = httptest.NewRequest(http.MethodGet, "/v1/barrier/status", nil)
	rec = httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	assert.False(t, h.reached, "GET /v1/barrier/status reached the sealed-window handler")
	assert.Equal(t, http.StatusOK, rec.Code, "body %q", rec.Body.String())
}

// TestRESTSealedWindow_HealthAndStatusAliasesAnswerWhileSealed: the sealed
// listener answers the allowlisted health and status aliases itself, because
// go-qrdb's sealed handler has no admin service. They serve only what
// /api/v1/health and /v1/barrier/status already serve while sealed.
func TestRESTSealedWindow_HealthAndStatusAliasesAnswerWhileSealed(t *testing.T) {
	s, h := newRealSealedWindowRESTServer(t)
	serve := func(method, path string) *httptest.ResponseRecorder {
		h.reached = false
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}

	for _, path := range []string{"/health", "/v1/health"} {
		t.Run("GET "+path, func(t *testing.T) {
			rec := serve(http.MethodGet, path)
			require.Equal(t, http.StatusOK, rec.Code, "body %q", rec.Body.String())
			assert.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json"),
				"Content-Type %q", rec.Header().Get("Content-Type"))
			assert.False(t, h.reached, "%s reached the sealed-window handler", path)
			var body HealthResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, "healthy", body.Status)
		})
	}

	t.Run("GET /status", func(t *testing.T) {
		rec := serve(http.MethodGet, "/status")
		require.Equal(t, http.StatusOK, rec.Code, "body %q", rec.Body.String())
		assert.False(t, h.reached, "/status reached the sealed-window handler")
		assert.JSONEq(t, `{"sealed":true,"strategy":"shamir","hardware_backed":false}`, rec.Body.String())

		barrier := serve(http.MethodGet, "/v1/barrier/status")
		require.Equal(t, http.StatusOK, barrier.Code, "body %q", barrier.Body.String())
		assert.JSONEq(t, barrier.Body.String(), rec.Body.String())
	})

	t.Run("POST /health", func(t *testing.T) {
		rec := serve(http.MethodPost, "/health")
		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "body %q", rec.Body.String())
		assert.False(t, h.reached, "POST /health reached the sealed-window handler")
	})
}
