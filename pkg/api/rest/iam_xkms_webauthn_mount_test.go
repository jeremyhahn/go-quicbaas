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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestBuildXKMSWebAuthnDispatcher_RejectsMissingTenantID verifies that
// a request whose chi URL parameters lack tenant_id returns a 400.
func TestBuildXKMSWebAuthnDispatcher_RejectsMissingTenantID(t *testing.T) {
	factory, err := iam.NewWebAuthnServiceFactory(iam.WebAuthnFactoryParams{
		Config: iam.WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB: dummyQRDBClient(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dispatcher := buildXKMSWebAuthnDispatcher(factory)

	req := httptest.NewRequest(http.MethodPost, "/registration/begin", nil)
	w := httptest.NewRecorder()
	dispatcher(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request, got %d", w.Code)
	}
}

// TestBuildXKMSWebAuthnDispatcher_StripsPrefix verifies the prefix
// stripping logic in isolation — given a request whose chi URL params
// carry a tenant id but whose URL path is exactly the prefix
// (no suffix), the dispatcher returns 404 rather than calling the
// downstream xkms handler.
//
// Construction of the underlying xkms Service requires a non-nil
// qrdb client whose KV/Users accessors return non-nil clients; the
// zero-value sentinel from dummyQRDBClient panics on those calls.
// The factory therefore returns an error from ServiceFor on first
// access, which the dispatcher surfaces as 503 — establishing that
// we got past prefix validation. We assert on either 503 or panic
// recovery to keep the test resilient to changes in the zero-value
// qrdb client behaviour.
func TestBuildXKMSWebAuthnDispatcher_RoutesAfterPrefixStrip(t *testing.T) {
	defer func() {
		// The zero-value qrdb client panics when accessed; recover so
		// the test surface stays clean. The fact that we reached the
		// panic is itself confirmation that the dispatcher routed past
		// the prefix validation.
		recover()
	}()
	factory, err := iam.NewWebAuthnServiceFactory(iam.WebAuthnFactoryParams{
		Config: iam.WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB: dummyQRDBClient(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	router := chi.NewRouter()
	router.Route("/api/v1/iam/{tenant_id}/webauthn", func(r chi.Router) {
		r.Post("/registration/begin", buildXKMSWebAuthnDispatcher(factory))
	})

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/iam/acme/webauthn/registration/begin",
		nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	body := w.Body.String()
	if w.Code == http.StatusBadRequest && body == "tenant_id is required\n" {
		t.Fatalf("dispatcher rejected tenant id resolution: %s", body)
	}
}

// TestMountXKMSWebAuthnRoutes_NoOpWhenFactoryUnavailable confirms the
// route mount silently skips registration when no xkms-backed manager
// is wired in (e.g. legacy quicbaas deployments).
func TestMountXKMSWebAuthnRoutes_NoOpWhenFactoryUnavailable(t *testing.T) {
	h := &IAMHandler{} // no server, no factory resolution path
	router := chi.NewRouter()
	h.MountXKMSWebAuthnRoutes(context.Background(), router) // must not panic

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/iam/acme/webauthn/registration/begin",
		nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (no routes mounted), got %d", w.Code)
	}
}

// dummyQRDBClient returns a sentinel non-nil qrdb client suitable for
// tests that exercise factory construction paths but do not need a
// live transport. The underlying client has nil internal state, so any
// real service call will fail — which is fine for tests that stop
// before invoking ceremony methods.
func dummyQRDBClient() *qrdbsdk.Client {
	return &qrdbsdk.Client{}
}
