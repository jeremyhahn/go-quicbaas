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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// fakeBootstrapServicer is a configurable test double for the
// service.BootstrapServicer the REST handlers delegate to. Each mode records
// the request it received and replays a fixed response or error so the routing
// + serialisation contract can be exercised without any real network I/O.
type fakeBootstrapServicer struct {
	resp    *service.BootstrapBundleResponse
	err     error
	lastReq *service.BootstrapRequest
}

func (f *fakeBootstrapServicer) Auto(_ context.Context, req *service.BootstrapRequest) (*service.BootstrapBundleResponse, error) {
	f.lastReq = req
	return f.resp, f.err
}

func (f *fakeBootstrapServicer) Dane(_ context.Context, req *service.BootstrapRequest) (*service.BootstrapBundleResponse, error) {
	f.lastReq = req
	return f.resp, f.err
}

func (f *fakeBootstrapServicer) Noise(_ context.Context, req *service.BootstrapRequest) (*service.BootstrapBundleResponse, error) {
	f.lastReq = req
	return f.resp, f.err
}

func (f *fakeBootstrapServicer) Spki(_ context.Context, req *service.BootstrapRequest) (*service.BootstrapBundleResponse, error) {
	f.lastReq = req
	return f.resp, f.err
}

// TestBootstrapRoutesAreRegistered guards against the regression where the
// /api/v1/bootstrap surface had no routes mounted at all (handler files
// did not even exist). A 404 here means a route is missing; any other
// status is fine.
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

// TestBootstrapReportsServiceUnavailableWhenUnwired pins the staged-readiness
// contract: when bootstrapSvc is nil the handlers report a service-unavailable
// error so callers can distinguish "still booting" from "missing route".
func TestBootstrapReportsServiceUnavailableWhenUnwired(t *testing.T) {
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

// TestBootstrapRoutesDelegateToServicer pins the wired contract: each route
// decodes the body, dispatches the matching servicer mode, and serialises the
// returned CA bundle with a 200.
func TestBootstrapRoutesDelegateToServicer(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	fake := &fakeBootstrapServicer{
		resp: &service.BootstrapBundleResponse{
			Method:      "auto",
			BundlePEM:   "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
			ContentType: "application/pem-certificate-chain",
		},
	}
	server.SetBootstrapService(fake)

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
			body := `{"server_url":"https://kms.example.com:8443"}`
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 from %s, got %d (body=%s)", tc.path, w.Code, w.Body.String())
			}
			var got service.BootstrapBundleResponse
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.ContentType != "application/pem-certificate-chain" {
				t.Errorf("content_type = %q", got.ContentType)
			}
			if fake.lastReq == nil || fake.lastReq.ServerURL != "https://kms.example.com:8443" {
				t.Errorf("servicer did not receive decoded request: %+v", fake.lastReq)
			}
		})
	}
}

// TestBootstrapServicerErrorMapsToStatus pins that a typed invalid-input error
// from the servicer (e.g. xkms reporting a missing server URL) surfaces as a
// 400 through the shared service-error mapping rather than a generic 500.
func TestBootstrapServicerErrorMapsToStatus(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	server.SetBootstrapService(&fakeBootstrapServicer{
		err: &qberrors.QBError{Code: qberrors.ErrInvalidInput, Op: "BootstrapService.Dane", Err: service.ErrBootstrapNilRequest},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/dane", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.GetRouter().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestBootstrapRejectsInvalidJSON pins that a malformed body is a 400 before
// the servicer is ever consulted.
func TestBootstrapRejectsInvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	fake := &fakeBootstrapServicer{resp: &service.BootstrapBundleResponse{}}
	server.SetBootstrapService(fake)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bootstrap/spki", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.GetRouter().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	if fake.lastReq != nil {
		t.Error("servicer must not be called for a malformed body")
	}
}
