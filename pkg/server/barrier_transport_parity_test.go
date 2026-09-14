//go:build rest && mcp

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

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/protobuf/types/known/emptypb"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// =============================================================================
// Cross-transport barrier-status parity (go-quicbaas#405)
//
// The issue was found by asking two transports the same question and getting
// two different answers about whether a barrier exists at all. The structural
// cause was route ownership, fixed elsewhere; this test pins the CONTRACT the
// fix exists to satisfy — every transport renders one barrier's state, from
// one facade, identically.
//
// Every transport quicbaas serves barrier verbs on funnels through the ONE
// service.BarrierServicer facade over the go-qrdb SDK BarrierClient
// (pkg/server/grpc_barrier_ceremony.go buildBarrierCeremonyServices), so this
// drives all of them from a single facade over a stub client and compares the
// rendered fields byte for byte:
//
//	REST          pkg/api/rest handlers_barrier.go   (also the qrdb-wire
//	                                                  /v1/barrier/* mount)
//	gRPC + unix   pkg/api/grpc handler_barrier.go    (unix serves the same
//	                                                  gRPC wire over a UDS)
//	MCP           pkg/api/mcp  tools_barrier.go
//	QUIC + embedded + the qrdb.v1 gRPC/unix surface
//	              pkg/server grpc_qrdb_barrier_adapter.go, which reaches the
//	              same platform-shard BarrierClient
// =============================================================================

// parityStatusResponse is the barrier state the stub client reports. The
// nanosecond timestamp is the one from the go-quicbaas#405 report, chosen
// precisely because a second-precision rendering would silently drop it.
var parityStatusResponse = &qrdbtransport.BarrierStatusResponse{
	Sealed:         false,
	Strategy:       "passphrase",
	HardwareBacked: false,
	InitializedAt:  "2026-08-25T04:40:05.727835519Z",
}

// parityLogger builds a quiet logger for the transport constructors.
func parityLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	return log
}

// barrierStatusOverREST drives GET /api/v1/barrier/status through the real
// REST server and returns the decoded JSON body.
func barrierStatusOverREST(t *testing.T, svc service.BarrierServicer) map[string]any {
	t.Helper()

	srv, err := restapi.NewServer(testutil.NewMockQuicBaaS(), restapi.DefaultConfig(), parityLogger(t))
	if err != nil {
		t.Fatalf("restapi.NewServer: %v", err)
	}
	srv.SetBarrierService(svc)

	rec := httptest.NewRecorder()
	srv.HandleBarrierStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/barrier/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("REST barrier status: got %d %s, want 200", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("REST barrier status body: %v (%s)", err, rec.Body.String())
	}
	return body
}

// barrierStatusOverGRPC drives the quicbaas.v1 Barrier gRPC handler, which is
// the same wire the unix transport serves over a UDS.
func barrierStatusOverGRPC(t *testing.T, svc service.BarrierServicer) map[string]any {
	t.Helper()

	resp, err := grpcapi.NewBarrierServer(svc, parityLogger(t)).
		BarrierStatus(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("gRPC BarrierStatus: %v", err)
	}
	return map[string]any{
		"sealed":          resp.GetSealed(),
		"strategy":        resp.GetStrategy(),
		"hardware_backed": resp.GetHardwareBacked(),
		"initialized_at":  resp.GetInitializedAt(),
	}
}

// barrierStatusOverMCP drives the qrdb_barrier_status tool.
func barrierStatusOverMCP(t *testing.T, svc service.BarrierServicer) map[string]any {
	t.Helper()

	registry := mcpapi.NewToolRegistry(testutil.NewMockQuicBaaS(), parityLogger(t))
	registry.SetBarrierService(svc)

	result, err := registry.ExecuteTool(context.Background(), "qrdb_barrier_status", map[string]any{})
	if err != nil {
		t.Fatalf("MCP qrdb_barrier_status: %v", err)
	}
	body, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("MCP qrdb_barrier_status returned %T, want map[string]any", result)
	}
	return body
}

// barrierStatusOverQRDBServicer drives the qrdb.v1 BarrierServicer adapter —
// the surface the QUIC, embedded, and qrdb-wire gRPC/unix transports reach.
// The adapter yields a time.Time; it is rendered with the same format the
// other transports use so the comparison is like for like.
func barrierStatusOverQRDBServicer(t *testing.T, client qrdbtransport.BarrierClient) map[string]any {
	t.Helper()

	info, err := newQRDBBarrierClientServicer(client).BarrierStatus(context.Background())
	if err != nil {
		t.Fatalf("qrdb BarrierServicer adapter BarrierStatus: %v", err)
	}
	return map[string]any{
		"sealed":          info.Sealed,
		"strategy":        info.Strategy,
		"hardware_backed": info.HardwareBacked,
		"initialized_at":  info.InitializedAt.UTC().Format(qrdbBarrierParityTimeFormat),
	}
}

// qrdbBarrierParityTimeFormat mirrors pkg/api/{rest,grpc,mcp}'s
// barrierTimeFormat. Those constants are unexported in their own packages, so
// the parity comparison restates the format here — and any divergence from it
// shows up as a mismatched field below.
const qrdbBarrierParityTimeFormat = "2006-01-02T15:04:05.999999999Z07:00"

// TestBarrierStatus_IsIdenticalOnEveryTransport is the go-quicbaas#405
// acceptance: an unsealed, configured barrier reports the same state,
// strategy and initialized-at on every transport, byte for byte.
func TestBarrierStatus_IsIdenticalOnEveryTransport(t *testing.T) {
	client := &stubBarrierClient{statusResp: parityStatusResponse}
	svc := service.NewBarrierService(client)

	answers := map[string]map[string]any{
		"rest":          barrierStatusOverREST(t, svc),
		"grpc+unix":     barrierStatusOverGRPC(t, svc),
		"mcp":           barrierStatusOverMCP(t, svc),
		"quic+embedded": barrierStatusOverQRDBServicer(t, client),
	}

	// The fields go-quicbaas#405 names, with the value the stub reported.
	want := map[string]any{
		"sealed":          false,
		"strategy":        "passphrase",
		"initialized_at":  "2026-08-25T04:40:05.727835519Z",
		"hardware_backed": false,
	}

	for transport, got := range answers {
		for field, wantValue := range want {
			gotValue, present := got[field]
			if !present {
				t.Errorf("%s: barrier status omits %q (got %v)", transport, field, got)
				continue
			}
			if gotValue != wantValue {
				t.Errorf("%s: barrier status %q = %v, want %v", transport, field, gotValue, wantValue)
			}
		}
	}
}

// TestBarrierStatus_SealedIsIdenticalOnEveryTransport is the same acceptance
// for a sealed node: "sealed" must be reported as sealed everywhere, never as
// an absent or unconfigured barrier.
func TestBarrierStatus_SealedIsIdenticalOnEveryTransport(t *testing.T) {
	client := &stubBarrierClient{statusResp: &qrdbtransport.BarrierStatusResponse{
		Sealed:         true,
		Strategy:       "shamir",
		HardwareBacked: true,
		InitializedAt:  "2026-08-25T04:40:05.727835519Z",
	}}
	svc := service.NewBarrierService(client)

	answers := map[string]map[string]any{
		"rest":          barrierStatusOverREST(t, svc),
		"grpc+unix":     barrierStatusOverGRPC(t, svc),
		"mcp":           barrierStatusOverMCP(t, svc),
		"quic+embedded": barrierStatusOverQRDBServicer(t, client),
	}

	want := map[string]any{
		"sealed":          true,
		"strategy":        "shamir",
		"hardware_backed": true,
		"initialized_at":  "2026-08-25T04:40:05.727835519Z",
	}

	for transport, got := range answers {
		for field, wantValue := range want {
			gotValue, present := got[field]
			if !present {
				t.Errorf("%s: sealed barrier status omits %q (got %v)", transport, field, got)
				continue
			}
			if gotValue != wantValue {
				t.Errorf("%s: sealed barrier status %q = %v, want %v", transport, field, gotValue, wantValue)
			}
		}
	}
}

// TestBarrierStatus_UnwiredFacadeFailsHonestly proves the OTHER half of the
// contract: a transport whose barrier facade is genuinely absent says so
// distinctly, and never reports a configured barrier as missing.
//
// "Not wired" is a different claim from "no barrier": the first is a
// composition state the operator can act on, the second is a fact about the
// node. go-quicbaas#405 was exactly this confusion answered in the wrong
// direction, so each transport's unwired answer is asserted to be its own
// service-unavailable signal rather than a barrier state.
func TestBarrierStatus_UnwiredFacadeFailsHonestly(t *testing.T) {
	t.Run("rest", func(t *testing.T) {
		srv, err := restapi.NewServer(testutil.NewMockQuicBaaS(), restapi.DefaultConfig(), parityLogger(t))
		if err != nil {
			t.Fatalf("restapi.NewServer: %v", err)
		}
		// No SetBarrierService: the facade is absent.
		rec := httptest.NewRecorder()
		srv.HandleBarrierStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/barrier/status", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("unwired REST barrier status: got %d, want 503", rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unwired REST body: %v (%s)", err, rec.Body.String())
		}
		if _, leaked := body["sealed"]; leaked {
			t.Errorf("unwired REST barrier status must not report a barrier state: %v", body)
		}
	})

	t.Run("grpc", func(t *testing.T) {
		_, err := grpcapi.NewBarrierServer(nil, parityLogger(t)).
			BarrierStatus(context.Background(), &emptypb.Empty{})
		if err == nil {
			t.Fatal("unwired gRPC barrier status must fail, got a status response")
		}
	})

	t.Run("mcp", func(t *testing.T) {
		registry := mcpapi.NewToolRegistry(testutil.NewMockQuicBaaS(), parityLogger(t))
		// No SetBarrierService.
		if _, err := registry.ExecuteTool(context.Background(), "qrdb_barrier_status", map[string]any{}); err == nil {
			t.Fatal("unwired MCP barrier status must fail, got a status response")
		}
	})
}
