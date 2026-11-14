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
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/emptypb"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
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
		"sealed":           resp.GetSealed(),
		"strategy":         resp.GetStrategy(),
		"hardware_backed":  resp.GetHardwareBacked(),
		"initialized_at":   resp.GetInitializedAt(),
		"shamir_threshold": resp.GetShamirThreshold(),
		"shamir_total":     resp.GetShamirTotal(),
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
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("MCP qrdb_barrier_status result %T: %v", result, err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("MCP qrdb_barrier_status body: %v (%s)", err, raw)
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
		"sealed":           info.Sealed,
		"strategy":         info.Strategy,
		"hardware_backed":  info.HardwareBacked,
		"initialized_at":   info.InitializedAt.UTC().Format(qrdbBarrierParityTimeFormat),
		"shamir_threshold": info.ShamirThreshold,
		"shamir_total":     info.ShamirTotal,
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

// TestBarrierStatus_CarriesGoQrdbShamirQuorum pins the re-export (row #1650):
// every transport carries the Shamir quorum go-qrdb reports to the caller.
// Before, REST and MCP's own status mapper, gRPC's BarrierStatusResponse and
// the QUIC/embedded servicer adapter all dropped shamir_threshold and
// shamir_total.
func TestBarrierStatus_CarriesGoQrdbShamirQuorum(t *testing.T) {
	client := &stubBarrierClient{statusResp: &qrdbtransport.BarrierStatusResponse{
		Sealed:          true,
		Strategy:        "shamir",
		InitializedAt:   "2026-08-25T04:40:05Z",
		ShamirThreshold: 2,
		ShamirTotal:     3,
	}}
	svc := service.NewBarrierService(client)

	answers := map[string]map[string]any{
		"rest":          barrierStatusOverREST(t, svc),
		"grpc+unix":     barrierStatusOverGRPC(t, svc),
		"mcp":           barrierStatusOverMCP(t, svc),
		"quic+embedded": barrierStatusOverQRDBServicer(t, client),
	}
	for transport, got := range answers {
		raw, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("%s: re-marshal status: %v", transport, err)
		}
		var decoded qrdbtransport.BarrierStatusResponse
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s: decode as go-qrdb status: %v", transport, err)
		}
		if decoded != *client.statusResp {
			t.Errorf("%s: barrier status = %+v, want go-qrdb's %+v", transport, decoded, *client.statusResp)
		}
	}
}

// barrierVerbOverREST POSTs body to one REST barrier handler and decodes the
// 200 reply into out.
func barrierVerbOverREST(t *testing.T, svc service.BarrierServicer, verb, path, body string, out any) {
	t.Helper()

	srv, err := restapi.NewServer(testutil.NewMockQuicBaaS(), restapi.DefaultConfig(), parityLogger(t))
	if err != nil {
		t.Fatalf("restapi.NewServer: %v", err)
	}
	srv.SetBarrierService(svc)
	handlers := map[string]http.HandlerFunc{
		"rekey":             srv.HandleBarrierRekey,
		"recovery-generate": srv.HandleBarrierRecoveryGenerate,
		"unseal-share":      srv.HandleBarrierShamirUnsealShare,
		"root-token":        srv.HandleBarrierRootToken,
	}

	rec := httptest.NewRecorder()
	handlers[verb](rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("REST %s: got %d %s, want 200", verb, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatalf("REST %s body: %v (%s)", verb, err, rec.Body.String())
	}
}

// barrierVerbOverMCP calls one MCP barrier tool and decodes its result into out.
func barrierVerbOverMCP(t *testing.T, svc service.BarrierServicer, tool string, args map[string]any, out any) {
	t.Helper()

	registry := mcpapi.NewToolRegistry(testutil.NewMockQuicBaaS(), parityLogger(t))
	registry.SetBarrierService(svc)
	result, err := registry.ExecuteTool(context.Background(), tool, args)
	if err != nil {
		t.Fatalf("MCP %s: %v", tool, err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("MCP %s result %T: %v", tool, result, err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("MCP %s body: %v (%s)", tool, err, raw)
	}
}

// TestBarrierRekeyAndRecovery_CarryGoQrdbShareCount pins rekey and recovery
// key generation to go-qrdb's share-free responses on every transport: the
// quorum and the count of shares go-qrdb sealed to officers, never the shares.
func TestBarrierRekeyAndRecovery_CarryGoQrdbShareCount(t *testing.T) {
	client := &stubBarrierClient{
		rekeyResp:    &qrdbtransport.BarrierRekeyResponse{Threshold: 2, TotalShares: 3, ShareCount: 3},
		recoveryResp: &qrdbtransport.BarrierRecoveryKeysResponse{Threshold: 3, Total: 5, ShareCount: 5},
	}
	svc := service.NewBarrierService(client)
	ctx := context.Background()
	grpcSrv := grpcapi.NewBarrierServer(svc, parityLogger(t))
	qrdbSvc := newQRDBBarrierClientServicer(client)

	t.Run("rekey", func(t *testing.T) {
		answers := map[string]qrdbtransport.BarrierRekeyResponse{}

		var rest qrdbtransport.BarrierRekeyResponse
		barrierVerbOverREST(t, svc, "rekey", "/api/v1/barrier/rekey", `{"threshold":2,"total":3}`, &rest)
		answers["rest"] = rest

		g, err := grpcSrv.BarrierRekey(ctx, &pb.BarrierRekeyRequest{Threshold: 2, Total: 3})
		if err != nil {
			t.Fatalf("gRPC BarrierRekey: %v", err)
		}
		answers["grpc+unix"] = qrdbtransport.BarrierRekeyResponse{
			Threshold: int(g.GetThreshold()), TotalShares: int(g.GetTotalShares()), ShareCount: int(g.GetShareCount()),
		}

		var mcp qrdbtransport.BarrierRekeyResponse
		barrierVerbOverMCP(t, svc, "qrdb_barrier_rekey", map[string]any{"threshold": 2, "total": 3}, &mcp)
		answers["mcp"] = mcp

		q, err := qrdbSvc.BarrierRekey(ctx, 2, 3)
		if err != nil {
			t.Fatalf("qrdb BarrierServicer adapter BarrierRekey: %v", err)
		}
		answers["quic+embedded"] = qrdbtransport.BarrierRekeyResponse{
			Threshold: q.Threshold, TotalShares: q.TotalShares, ShareCount: q.ShareCount,
		}

		for transport, got := range answers {
			if got != *client.rekeyResp {
				t.Errorf("%s: rekey = %+v, want go-qrdb's %+v", transport, got, *client.rekeyResp)
			}
		}
	})

	t.Run("recovery-generate", func(t *testing.T) {
		answers := map[string]qrdbtransport.BarrierRecoveryKeysResponse{}

		var rest qrdbtransport.BarrierRecoveryKeysResponse
		barrierVerbOverREST(t, svc, "recovery-generate", "/api/v1/barrier/recovery/generate", `{"threshold":3,"total":5}`, &rest)
		answers["rest"] = rest

		g, err := grpcSrv.BarrierRecoveryGenerate(ctx, &pb.BarrierRecoveryGenerateRequest{Threshold: 3, Total: 5})
		if err != nil {
			t.Fatalf("gRPC BarrierRecoveryGenerate: %v", err)
		}
		answers["grpc+unix"] = qrdbtransport.BarrierRecoveryKeysResponse{
			Threshold: int(g.GetThreshold()), Total: int(g.GetTotal()), ShareCount: int(g.GetShareCount()),
		}

		var mcp qrdbtransport.BarrierRecoveryKeysResponse
		barrierVerbOverMCP(t, svc, "qrdb_barrier_recovery_generate", map[string]any{"threshold": 3, "total": 5}, &mcp)
		answers["mcp"] = mcp

		q, err := qrdbSvc.BarrierGenerateRecoveryKeys(ctx, 3, 5)
		if err != nil {
			t.Fatalf("qrdb BarrierServicer adapter BarrierGenerateRecoveryKeys: %v", err)
		}
		answers["quic+embedded"] = qrdbtransport.BarrierRecoveryKeysResponse{
			Threshold: q.Threshold, Total: q.Total, ShareCount: q.ShareCount,
		}

		for transport, got := range answers {
			if got != *client.recoveryResp {
				t.Errorf("%s: recovery generate = %+v, want go-qrdb's %+v", transport, got, *client.recoveryResp)
			}
		}
	})
}

// TestBarrierUnsealShareAndRootToken_CarryGoQrdbResponses pins the two
// remaining answer-bearing share verbs to go-qrdb's responses on every
// transport: unseal-share reports go-qrdb's quorum progress, root-token
// go-qrdb's token and creation time.
func TestBarrierUnsealShareAndRootToken_CarryGoQrdbResponses(t *testing.T) {
	client := &stubBarrierClient{
		shareResp:     &qrdbtransport.BarrierUnsealShareResponse{Required: 3, Submitted: 2, Complete: false},
		rootTokenResp: &qrdbtransport.BarrierRootTokenResponse{Token: "hvs.root-1650", CreatedAt: "2026-08-25T04:40:05Z"},
	}
	svc := service.NewBarrierService(client)
	ctx := context.Background()
	grpcSrv := grpcapi.NewBarrierServer(svc, parityLogger(t))
	qrdbSvc := newQRDBBarrierClientServicer(client)

	t.Run("unseal-share", func(t *testing.T) {
		answers := map[string]qrdbtransport.BarrierUnsealShareResponse{}

		var rest qrdbtransport.BarrierUnsealShareResponse
		barrierVerbOverREST(t, svc, "unseal-share", "/api/v1/barrier/shamir/unseal-share", `{"share":"s1"}`, &rest)
		answers["rest"] = rest

		g, err := grpcSrv.BarrierShamirUnsealShare(ctx, &pb.BarrierShamirUnsealShareRequest{Share: "s1"})
		if err != nil {
			t.Fatalf("gRPC BarrierShamirUnsealShare: %v", err)
		}
		answers["grpc+unix"] = qrdbtransport.BarrierUnsealShareResponse{
			Required: int(g.GetRequired()), Submitted: int(g.GetSubmitted()), Complete: g.GetComplete(),
		}

		var mcp qrdbtransport.BarrierUnsealShareResponse
		barrierVerbOverMCP(t, svc, "qrdb_barrier_shamir_unseal_share", map[string]any{"share": "s1"}, &mcp)
		answers["mcp"] = mcp

		q, err := qrdbSvc.BarrierUnsealWithShare(ctx, "s1")
		if err != nil {
			t.Fatalf("qrdb BarrierServicer adapter BarrierUnsealWithShare: %v", err)
		}
		answers["quic+embedded"] = qrdbtransport.BarrierUnsealShareResponse{
			Required: q.Required, Submitted: q.Submitted, Complete: q.Complete,
		}

		for transport, got := range answers {
			if got != *client.shareResp {
				t.Errorf("%s: unseal-share = %+v, want go-qrdb's %+v", transport, got, *client.shareResp)
			}
		}
	})

	t.Run("root-token", func(t *testing.T) {
		answers := map[string]qrdbtransport.BarrierRootTokenResponse{}

		var rest qrdbtransport.BarrierRootTokenResponse
		barrierVerbOverREST(t, svc, "root-token", "/api/v1/barrier/root-token", `{"shares":["s1","s2"]}`, &rest)
		answers["rest"] = rest

		g, err := grpcSrv.BarrierRootToken(ctx, &pb.BarrierRootTokenRequest{Shares: []string{"s1", "s2"}})
		if err != nil {
			t.Fatalf("gRPC BarrierRootToken: %v", err)
		}
		answers["grpc+unix"] = qrdbtransport.BarrierRootTokenResponse{Token: g.GetToken(), CreatedAt: g.GetCreatedAt()}

		var mcp qrdbtransport.BarrierRootTokenResponse
		barrierVerbOverMCP(t, svc, "qrdb_barrier_root_token", map[string]any{"shares": []any{"s1", "s2"}}, &mcp)
		answers["mcp"] = mcp

		q, err := qrdbSvc.BarrierGenerateRootToken(ctx, []string{"s1", "s2"})
		if err != nil {
			t.Fatalf("qrdb BarrierServicer adapter BarrierGenerateRootToken: %v", err)
		}
		answers["quic+embedded"] = qrdbtransport.BarrierRootTokenResponse{
			Token: q.Token, CreatedAt: q.CreatedAt.UTC().Format(time.RFC3339),
		}

		for transport, got := range answers {
			if got != *client.rootTokenResp {
				t.Errorf("%s: root-token = %+v, want go-qrdb's %+v", transport, got, *client.rootTokenResp)
			}
		}
	})
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
