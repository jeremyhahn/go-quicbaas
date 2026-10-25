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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestCeremonyRoutesAreRegistered guards against the regression where the
// ceremony lifecycle handlers landed in the package but were not wired into
// the REST router. A 404 here means /api/v1/init is unmounted; any other
// status (including 503 for the unwired ceremony facade) is fine.
//
// The ceremony surface mounts under /api/v1/init/* — not /api/v1/ceremony/* —
// to match the qrdb sealed-window allowlist.
func TestCeremonyRoutesAreRegistered(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"status", http.MethodGet, "/api/v1/init/status"},
		{"officers", http.MethodGet, "/api/v1/init/officers"},
		{"initialize", http.MethodPost, "/api/v1/init/initialize"},
		{"sign-csr", http.MethodPost, "/api/v1/init/sign-csr"},
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

// TestCeremonyStatusReportsServiceUnavailableWhenUnwired pins the contract
// that ceremony handlers report a service-unavailable error when ceremonySvc
// is nil. The TimeoutMiddleware no longer rewrites status codes, so
// handler-emitted 503 reaches the wire intact.
func TestCeremonyStatusReportsServiceUnavailableWhenUnwired(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	if server.ceremonySvc != nil {
		t.Fatal("test precondition: ceremonySvc must be nil to exercise the unavailable branch")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/init/status", nil)
	w := httptest.NewRecorder()
	server.GetRouter().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), ErrCeremonyServiceUnavailable.Error()) {
		t.Fatalf("response body should reference the typed error %q, got %s",
			ErrCeremonyServiceUnavailable.Error(), w.Body.String())
	}
}

// TestCeremonyListOfficersReturns503WhenUnwired pins the contract that the
// /officers handler reports the same canonical 503 envelope as every other
// /api/v1/init/* endpoint when the ceremony facade is missing. This is the
// unwired branch — the typed-error envelope must be reachable here so
// callers can distinguish "ceremony subsystem offline" from "no officers
// enrolled yet".
func TestCeremonyListOfficersReturns503WhenUnwired(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	if server.ceremonySvc != nil {
		t.Fatal("test precondition: ceremonySvc must be nil to exercise the unavailable branch")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/init/officers", nil)
	w := httptest.NewRecorder()
	server.GetRouter().ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), ErrCeremonyServiceUnavailable.Error()) {
		t.Fatalf("response body should reference the typed error %q, got %s",
			ErrCeremonyServiceUnavailable.Error(), w.Body.String())
	}
}

// TestCeremonyListOfficersReturnsEmptyRosterWhenWired guards the regression
// the front-end manual chrome run surfaced: GET /api/v1/init/officers
// returning 404 rendered a red error banner on the Ceremony view. The
// canonical empty-state shape is `{"officers": []}` — matching go-qrdb's
// persistent ceremony registry response. The handler MUST emit this
// envelope (200 OK + empty array) when the ceremony service is wired but
// no officers are yet enrolled, so the UI lists the empty roster instead
// of treating "no officers yet" as a transport error.
func TestCeremonyListOfficersReturnsEmptyRosterWhenWired(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Wire a non-nil ceremony service so requireCeremonyService passes
	// through to the empty-roster branch. The CeremonyService struct's
	// methods short-circuit when client is nil, but ListOfficers does
	// not delegate to the SDK transport — the empty roster is returned
	// directly. Any non-nil value is sufficient here.
	server.SetCeremonyService(&service.CeremonyService{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/init/officers", nil)
	w := httptest.NewRecorder()
	server.GetRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response body must be valid JSON, got %s (err=%v)", w.Body.String(), err)
	}

	officersRaw, ok := resp["officers"]
	if !ok {
		t.Fatalf("response must contain an \"officers\" key, got %s", w.Body.String())
	}
	officers, ok := officersRaw.([]interface{})
	if !ok {
		t.Fatalf("\"officers\" must be a JSON array, got %T", officersRaw)
	}
	if len(officers) != 0 {
		t.Fatalf("expected empty roster, got %d officers", len(officers))
	}
}
