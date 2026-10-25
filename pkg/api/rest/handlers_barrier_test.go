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

// TestBarrierRoutesAreRegistered guards against the regression where the
// seal→barrier rename landed handler functions but never wired them into the
// REST router. A 404 here means a route is missing from setupRouter; any
// other response code (including 503 from the unwired barrier facade) means
// the route is mounted, which is all this test cares about.
//
// This is intentionally a routing test, not a behavior test. Request bodies
// are deliberately empty and validation failures are expected — handler
// behavior is covered by the per-method tests in this package.
func TestBarrierRoutesAreRegistered(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"status", http.MethodGet, "/api/v1/barrier/status"},
		{"initialize", http.MethodPost, "/api/v1/barrier/initialize"},
		{"unseal", http.MethodPost, "/api/v1/barrier/unseal"},
		{"seal", http.MethodPost, "/api/v1/barrier/seal"},
		{"shamir/initialize", http.MethodPost, "/api/v1/barrier/shamir/initialize"},
		{"POST cluster-unseal", http.MethodPost, "/api/v1/barrier/cluster-unseal"},
		{"GET officers", http.MethodGet, "/api/v1/barrier/officers"},
		{"POST officers", http.MethodPost, "/api/v1/barrier/officers"},
		{"POST officers/revoke", http.MethodPost, "/api/v1/barrier/officers/revoke"},
		{"POST officers/{username}/claim/begin", http.MethodPost, "/api/v1/barrier/officers/alice/claim/begin"},
		{"POST officers/{username}/claim/complete", http.MethodPost, "/api/v1/barrier/officers/alice/claim/complete"},
		{"POST quorum/begin", http.MethodPost, "/api/v1/barrier/quorum/begin"},
		{"POST quorum/submit", http.MethodPost, "/api/v1/barrier/quorum/submit"},
		{"POST recovery/officers", http.MethodPost, "/api/v1/barrier/recovery/officers"},
		{"POST recovery/officers/{username}/claim/begin", http.MethodPost, "/api/v1/barrier/recovery/officers/alice/claim/begin"},
		{"POST recovery/officers/{username}/claim/complete", http.MethodPost, "/api/v1/barrier/recovery/officers/alice/claim/complete"},
		{"POST rekey/start", http.MethodPost, "/api/v1/barrier/rekey/start"},
		{"recovery/generate", http.MethodPost, "/api/v1/barrier/recovery/generate"},
		{"recovery (delete)", http.MethodDelete, "/api/v1/barrier/recovery"},
		{"recovery/status", http.MethodGet, "/api/v1/barrier/recovery/status"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(""))
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

// TestBarrierStatusReportsServiceUnavailableWhenUnwired pins the contract
// that the barrier handlers report a service-unavailable error (not a
// generic 500 or 404) when the barrier facade is nil.
//
// The TimeoutMiddleware no longer rewrites status codes, so handler-emitted
// 503 reaches the wire intact. The test asserts on both the wire status and
// the typed-error message in the JSON body, which is what callers branch on.
func TestBarrierStatusReportsServiceUnavailableWhenUnwired(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// barrierSvc is nil by default in setupMockServer.
	if server.barrierSvc != nil {
		t.Fatal("test precondition: barrierSvc must be nil to exercise the unavailable branch")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/barrier/status", nil)
	w := httptest.NewRecorder()
	server.GetRouter().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), ErrBarrierServiceUnavailable.Error()) {
		t.Fatalf("response body should reference the typed error %q, got %s",
			ErrBarrierServiceUnavailable.Error(), w.Body.String())
	}
}
