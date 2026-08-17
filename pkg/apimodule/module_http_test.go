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

package apimodule

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// captured records one registrar registration.
type captured struct {
	method, pattern string
	perm            qrdbsdk.Permission
	public          bool
	handler         http.HandlerFunc
}

// newCaptureRegistrar builds a real qrdb HTTPRegistrar whose route/public
// sinks record into the returned slice.
func newCaptureRegistrar() (*qrdbsdk.ModuleHTTPRegistrar, *[]captured) {
	var recs []captured
	r := qrdbsdk.NewModuleHTTPRegistrar(chi.NewRouter(),
		func(method, pattern string, perm qrdbsdk.Permission, h http.HandlerFunc) {
			recs = append(recs, captured{method: method, pattern: pattern, perm: perm, handler: h})
		},
		func(method, pattern string, h http.HandlerFunc) {
			recs = append(recs, captured{method: method, pattern: pattern, public: true, handler: h})
		})
	return r, &recs
}

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return log
}

func newModuleRESTServer(t *testing.T) *restapi.Server {
	t.Helper()
	mockSvc := testutil.NewMockQuicBaaS()
	mockSvc.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
		Status: service.HealthStatusHealthy,
		NodeID: 1,
	}, nil).Maybe()
	srv, err := restapi.NewServer(mockSvc, restapi.DefaultConfig(), newTestLogger(t))
	require.NoError(t, err)
	return srv
}

func newHTTPModule(t *testing.T, resolve RESTResolver) *Module {
	t.Helper()
	m, err := NewModule(func() service.QuicBaaS { return nil }, newTestLogger(t),
		WithRESTServer(resolve))
	require.NoError(t, err)
	return m
}

// expectedHTTPRegistrations mirrors RegisterHTTP's dedup + bare-alias rules
// over the module route table (restapi.ModuleRoutes, NOT restapi.Routes --
// P3.3 excludes /api/v1/barrier/* and /api/v1/init/* from the module surface;
// the legacy listener alone keeps serving them, see routes.go
// moduleExcludedPathPrefixes), returning the expected
// (method, pattern) -> lead spec map.
func expectedHTTPRegistrations() map[string]restapi.RouteSpec {
	type key struct{ m, p string }
	table := map[key]restapi.RouteSpec{}
	for _, spec := range restapi.ModuleRoutes() {
		k := key{spec.Method, spec.Pattern}
		if _, dup := table[k]; !dup {
			table[k] = spec
		}
	}
	want := map[string]restapi.RouteSpec{}
	for k, spec := range table {
		want[k.m+" "+k.p] = spec
		if bare := strings.TrimSuffix(k.p, "/"); bare != k.p && bare != "" {
			if _, taken := table[key{k.m, bare}]; !taken {
				want[k.m+" "+bare] = spec
			}
		}
	}
	return want
}

// TestRegisterHTTP_FullSurface proves the module registers the complete
// route table (deduplicated, with the mount-implied bare aliases) with the
// correct permission class per route.
func TestRegisterHTTP_FullSurface(t *testing.T) {
	srv := newModuleRESTServer(t)
	m := newHTTPModule(t, func() *restapi.Server { return srv })

	reg, recs := newCaptureRegistrar()
	m.RegisterHTTP(reg)

	want := expectedHTTPRegistrations()
	require.Len(t, *recs, len(want), "one registration per distinct route (plus bare aliases)")

	got := map[string]captured{}
	for _, rec := range *recs {
		got[rec.method+" "+rec.pattern] = rec
	}
	for key, spec := range want {
		rec, ok := got[key]
		require.True(t, ok, "route %s not registered", key)
		switch spec.Auth {
		case restapi.RouteAuthPublic:
			assert.True(t, rec.public, "%s must register public", key)
		case restapi.RouteAuthSession:
			assert.Equal(t, qrdbsdk.PermissionIdentityGated, rec.perm,
				"%s must register identity-gated", key)
		default:
			assert.False(t, rec.public, "%s must not register public", key)
			assert.Equal(t, qrdbsdk.Permission(spec.Resource+":"+spec.Action), rec.perm,
				"%s permission", key)
		}
	}
}

// TestRegisterHTTP_NoResolver proves a module constructed without a REST
// resolver registers no HTTP routes (BaseModule semantics preserved).
func TestRegisterHTTP_NoResolver(t *testing.T) {
	m, err := NewModule(func() service.QuicBaaS { return nil }, newTestLogger(t))
	require.NoError(t, err)

	reg, recs := newCaptureRegistrar()
	m.RegisterHTTP(reg)
	assert.Empty(t, *recs)
}

// TestRegisterHTTP_LazyBinding proves module routes answer 503 while the
// REST surface is unresolved and serve normally once it resolves — without
// re-registration.
func TestRegisterHTTP_LazyBinding(t *testing.T) {
	var srv atomic.Pointer[restapi.Server]
	m := newHTTPModule(t, func() *restapi.Server { return srv.Load() })

	reg, recs := newCaptureRegistrar()
	m.RegisterHTTP(reg)

	var health http.HandlerFunc
	for _, rec := range *recs {
		if rec.method == "GET" && rec.pattern == "/api/v1/health" {
			health = rec.handler
		}
	}
	require.NotNil(t, health, "GET /api/v1/health must be registered")

	// Unresolved: 503.
	w := httptest.NewRecorder()
	health(w, httptest.NewRequest("GET", "/api/v1/health", nil))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	// Resolved: served by the real handler.
	srv.Store(newModuleRESTServer(t))
	w = httptest.NewRecorder()
	health(w, httptest.NewRequest("GET", "/api/v1/health", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestRegisterHTTP_DisabledRouteAnswers501 proves a registered route whose
// backing subsystem is absent on the resolved server answers 501 rather
// than binding a dead handler. The cluster CA secret-delivery route is the
// table's only availability-guarded entry.
func TestRegisterHTTP_DisabledRouteAnswers501(t *testing.T) {
	srv := newModuleRESTServer(t)
	m := newHTTPModule(t, func() *restapi.Server { return srv })

	reg, recs := newCaptureRegistrar()
	m.RegisterHTTP(reg)

	var secret http.HandlerFunc
	for _, rec := range *recs {
		if rec.method == "POST" && rec.pattern == "/api/v1/cluster/secret" {
			secret = rec.handler
		}
	}
	require.NotNil(t, secret, "POST /api/v1/cluster/secret must be registered")

	w := httptest.NewRecorder()
	secret(w, httptest.NewRequest("POST", "/api/v1/cluster/secret", nil))
	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

// TestRegisterHTTP_BareAliasesRegistered proves the mount-implied bare
// aliases of trailing-slash collection routes are registered with the same
// permission class as their canonical form.
func TestRegisterHTTP_BareAliasesRegistered(t *testing.T) {
	srv := newModuleRESTServer(t)
	m := newHTTPModule(t, func() *restapi.Server { return srv })

	reg, recs := newCaptureRegistrar()
	m.RegisterHTTP(reg)

	got := map[string]captured{}
	for _, rec := range *recs {
		got[rec.method+" "+rec.pattern] = rec
	}
	canonical, okCanonical := got["GET /api/v1/parameters/"]
	bare, okBare := got["GET /api/v1/parameters"]
	require.True(t, okCanonical, "canonical collection route must be registered")
	require.True(t, okBare, "bare alias must be registered")
	assert.Equal(t, canonical.perm, bare.perm, "alias must carry the canonical permission")
	assert.Equal(t, canonical.public, bare.public)
}
