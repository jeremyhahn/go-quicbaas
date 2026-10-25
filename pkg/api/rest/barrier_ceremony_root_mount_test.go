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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The quicbaas SDK's go-qrdb sub-client requests barrier and ceremony verbs
// at ROOT-level /v1/* paths (the qrdb REST transport hardcodes those
// prefixes — see go-qrdb transport_barrier.go / transport_ceremony.go),
// whereas the primary quicbaasd surface lives under /api/v1/*. Without the
// root mounts these requests 404. These tests assert the root mounts route
// to the same Handle* methods as /api/v1, exactly mirroring the
// /v1/credentials root-mount contract.

// TestBarrierRootMount_StatusRoutesToHandler proves GET /v1/barrier/status
// reaches HandleBarrierStatus (returning 200 with the barrier status body)
// rather than the chi 404 "page not found" that the SDK previously hit.
func TestBarrierRootMount_StatusRoutesToHandler(t *testing.T) {
	called := false
	svc := &fakeBarrierService{
		statusFn: func(ctx context.Context) (*service.BarrierStatusInfo, error) {
			called = true
			return &service.BarrierStatusInfo{Sealed: true, Strategy: "shamir"}, nil
		},
	}
	s := barrierServer(t, svc)

	w := doBarrierReq(t, s, http.MethodGet, "/v1/barrier/status", "")
	if w.Code == http.StatusNotFound {
		t.Fatalf("/v1/barrier/status 404'd — root mount missing (got body %q)", w.Body.String())
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from /v1/barrier/status, got %d (body %q)", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("HandleBarrierStatus was not invoked for the root /v1/barrier/status path")
	}
}

// TestBarrierRootMount_AllVerbsRegistered asserts every path the qrdb
// BarrierClient requests resolves to a handler (i.e. is not 404). It does not
// assert success semantics — only that the route exists at the root prefix.
func TestBarrierRootMount_AllVerbsRegistered(t *testing.T) {
	s := barrierServer(t, &fakeBarrierService{})

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/v1/barrier/status"},
		{http.MethodPost, "/v1/barrier/initialize"},
		{http.MethodPost, "/v1/barrier/unseal"},
		{http.MethodPost, "/v1/barrier/seal"},
		{http.MethodPost, "/v1/barrier/shamir/initialize"},
		{http.MethodPost, "/v1/barrier/cluster-unseal"},
		{http.MethodGet, "/v1/barrier/officers"},
		{http.MethodPost, "/v1/barrier/officers"},
		{http.MethodPost, "/v1/barrier/officers/revoke"},
		{http.MethodPost, "/v1/barrier/officers/alice/claim/begin"},
		{http.MethodPost, "/v1/barrier/officers/alice/claim/complete"},
		{http.MethodPost, "/v1/barrier/quorum/begin"},
		{http.MethodPost, "/v1/barrier/quorum/submit"},
		{http.MethodPost, "/v1/barrier/recovery/officers"},
		{http.MethodPost, "/v1/barrier/recovery/officers/alice/claim/begin"},
		{http.MethodPost, "/v1/barrier/recovery/officers/alice/claim/complete"},
		{http.MethodPost, "/v1/barrier/rekey/start"},
		{http.MethodPost, "/v1/barrier/recovery/generate"},
		{http.MethodDelete, "/v1/barrier/recovery"},
		{http.MethodGet, "/v1/barrier/recovery/status"},
	}
	for _, c := range cases {
		w := doBarrierReq(t, s, c.method, c.path, "{}")
		if w.Code == http.StatusNotFound {
			t.Fatalf("%s %s 404'd — root barrier route not registered", c.method, c.path)
		}
	}
}

// TestCeremonyRootMount_AllVerbsRegistered asserts every path the qrdb
// CeremonyClient requests resolves to a handler. The qrdb client splits its
// verbs across /v1/ceremony/*.
func TestCeremonyRootMount_AllVerbsRegistered(t *testing.T) {
	s, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/v1/ceremony/status"},
		{http.MethodPost, "/v1/ceremony/initialize"},
		{http.MethodPost, "/v1/ceremony/sign-csr"},
	}
	for _, c := range cases {
		w := doBarrierReq(t, s, c.method, c.path, "{}")
		if w.Code == http.StatusNotFound {
			t.Fatalf("%s %s 404'd — root ceremony route not registered", c.method, c.path)
		}
	}
}
