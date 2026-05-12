//go:build rest

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

package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubBootstrapServicer is a zero-method placeholder used only by the
// bootstrap routing tests. The BootstrapServicer interface is currently
// empty, so any value satisfies it; this type exists to make the intent
// explicit at the call site.
type stubBootstrapServicer struct{}

// TestBootstrapRoutesAreRegistered guards against the regression where the
// /api/v1/bootstrap surface had no routes mounted at all (handler files
// did not even exist). A 404 here means a route is missing; any other
// status (503 unwired, 501 wired-but-unimplemented) is fine.
func TestBootstrapRoutesAreRegistered(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"auto", http.MethodPost, "/api/v1/bootstrap/auto"},
		{"dane", http.MethodPost, "/api/v1/bootstrap/dane"},
		{"noise", http.MethodPost, "/api/v1/bootstrap/noise"},
		{"spki", http.MethodPost, "/api/v1/bootstrap/spki"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.GetRouter().ServeHTTP(w, req)

			if w.Code == http.StatusNotFound {
				t.Fatalf("%s %s returned 404 — route is not mounted", tc.method, tc.path)
			}
			if w.Code == http.StatusMethodNotAllowed {
				t.Fatalf("%s %s returned 405 — route exists but method is not registered", tc.method, tc.path)
			}
		})
	}
}

// TestBootstrapAutoReportsServiceUnavailableWhenUnwired pins the
// staged-readiness contract: when bootstrapSvc is nil the handlers report a
// service-unavailable error so callers can distinguish "still booting" from
// "missing route" or "internal error". The TimeoutMiddleware no longer
// rewrites status codes, so handler-emitted 503 reaches the wire intact.
func TestBootstrapAutoReportsServiceUnavailableWhenUnwired(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	if server.bootstrapSvc != nil {
		t.Fatal("test precondition: bootstrapSvc must be nil to exercise the unavailable branch")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/auto", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.GetRouter().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), ErrBootstrapServiceUnavailable.Error()) {
		t.Fatalf("response body should reference the typed error %q, got %s",
			ErrBootstrapServiceUnavailable.Error(), w.Body.String())
	}
}

// TestBootstrapRoutesReturn501WhenServiceWired pins the second half of the
// staged-readiness contract: once a servicer is wired, handlers return 501
// (with a per-mode message) until concrete implementations land. This keeps
// the route available to clients during incremental rollout.
func TestBootstrapRoutesReturn501WhenServiceWired(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	server.SetBootstrapService(stubBootstrapServicer{})

	cases := []struct {
		name string
		path string
	}{
		{"auto", "/api/v1/bootstrap/auto"},
		{"dane", "/api/v1/bootstrap/dane"},
		{"noise", "/api/v1/bootstrap/noise"},
		{"spki", "/api/v1/bootstrap/spki"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			if w.Code != http.StatusNotImplemented {
				t.Fatalf("expected 501 from %s, got %d (body=%s)", tc.path, w.Code, w.Body.String())
			}
		})
	}
}
