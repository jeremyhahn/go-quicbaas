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

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	qrdbgrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	qrdbrest "github.com/jeremyhahn/go-qrdb/pkg/api/rest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
)

// alwaysSealedProbe reports the barrier sealed.
type alwaysSealedProbe struct{}

func (alwaysSealedProbe) IsSealed() bool { return true }

// TestNodeBarrierServerOptions_SealedAllowlistAdmitsComposedBarrierRoutes:
// with the seal disabled the sealed-window probe installs go-qrdb's outer
// REST gate with its default allowlist; after nodeBarrierServerOptions(false)
// that default admits the composed /api/v1/barrier/* routes and still
// refuses data routes while sealed (#2512).
func TestNodeBarrierServerOptions_SealedAllowlistAdmitsComposedBarrierRoutes(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	if got := s.nodeBarrierServerOptions(false); len(got) != 3 {
		t.Fatalf("seal disabled: expected 3 options, got %d", len(got))
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := qrdbrest.SealedWindowMiddleware(alwaysSealedProbe{}, nil)(ok)

	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/v1/barrier/status", http.StatusOK},
		{http.MethodPost, "/api/v1/barrier/unseal", http.StatusOK},
		{http.MethodGet, "/api/v1/kv/x", http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s while sealed: got %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
}

// TestInstallComposedRESTSealedAllowlist_ContainsComposedEntries checks every
// composed sealed-window entry is in go-qrdb's default allowlist afterwards,
// with no duplicates.
func TestInstallComposedRESTSealedAllowlist_ContainsComposedEntries(t *testing.T) {
	installComposedRESTSealedAllowlist()
	got := qrdbrest.SealedWindowAllowlist
	for _, entry := range sealedwindow.RESTAllowlist() {
		if !slices.Contains(got, entry) {
			t.Errorf("SealedWindowAllowlist missing composed entry %q", entry)
		}
	}
	seen := make(map[string]bool, len(got))
	for _, entry := range got {
		if seen[entry] {
			t.Errorf("SealedWindowAllowlist has duplicate entry %q", entry)
		}
		seen[entry] = true
	}
}

// TestInstallComposedGRPCSealedAllowlist_ContainsComposedEntries pins that
// nodeBarrierServerOptions(false) widens go-qrdb's native gRPC sealed-window
// allowlist with every composed quicbaas full method (the liveness RPCs
// included) while keeping every original qrdb entry, without duplicates.
func TestInstallComposedGRPCSealedAllowlist_ContainsComposedEntries(t *testing.T) {
	original := slices.Clone(qrdbgrpc.SealedWindowAllowlist)
	s := &Server{logger: buildTestLogger(t)}
	s.nodeBarrierServerOptions(false)
	got := qrdbgrpc.SealedWindowAllowlist

	want := append(sealedwindow.GRPCFullMethodAllowlist(), "/quicbaas.v1.QuicBaaS/Health")
	for _, entry := range want {
		if !slices.Contains(got, entry) {
			t.Errorf("qrdbgrpc.SealedWindowAllowlist missing composed entry %q", entry)
		}
	}
	for _, entry := range original {
		if !slices.Contains(got, entry) {
			t.Errorf("qrdbgrpc.SealedWindowAllowlist lost original entry %q", entry)
		}
	}
	seen := make(map[string]bool, len(got))
	for _, entry := range got {
		if seen[entry] {
			t.Errorf("qrdbgrpc.SealedWindowAllowlist has duplicate entry %q", entry)
		}
		seen[entry] = true
	}
}

// TestNodeBarrierServerOptions_GRPCSealedGateAdmitsQuicBaaSHealth drives a
// unary call through go-qrdb's native gRPC sealed-window interceptor (default
// allowlist) after nodeBarrierServerOptions(false): the quicbaas liveness RPC
// reaches its handler, a data RPC is refused with codes.Unavailable.
func TestNodeBarrierServerOptions_GRPCSealedGateAdmitsQuicBaaSHealth(t *testing.T) {
	s := &Server{logger: buildTestLogger(t)}
	s.nodeBarrierServerOptions(false)
	icpt := qrdbgrpc.SealedWindowUnaryInterceptor(alwaysSealedProbe{}, nil)

	handler := func(context.Context, any) (any, error) { return "ok", nil }
	ctx := context.Background()

	resp, err := icpt(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Health"}, handler)
	if err != nil || resp != "ok" {
		t.Errorf("Health while sealed: got (%v, %v), want handler reached", resp, err)
	}

	_, err = icpt(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"}, handler)
	if status.Code(err) != codes.Unavailable {
		t.Errorf("Put while sealed: got code %v (%v), want Unavailable", status.Code(err), err)
	}
}
