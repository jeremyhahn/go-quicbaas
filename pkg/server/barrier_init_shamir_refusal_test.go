//go:build rest && mcp

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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// ceremonyGuidance is the bootstrap path every refusal must name.
const ceremonyGuidance = "quicbaas init initialize"

// shamirReadyBarrierClient is a qrdb barrier client that would create the
// Shamir barrier if asked: the refusal must hold even when the backend is
// able to distribute shares.
type shamirReadyBarrierClient struct {
	qrdbtransport.BarrierClient
}

func (c *shamirReadyBarrierClient) BarrierInitializeShamir(_ context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
	return &qrdbtransport.BarrierInitializeShamirResponse{Threshold: req.Threshold, TotalShares: req.TotalShares, ShareCount: req.TotalShares}, nil
}

// refusalFacades are the two server states the refusal covers: a backend that
// could distribute shares, and no share distributor at all.
func refusalFacades() map[string]service.BarrierServicer {
	return map[string]service.BarrierServicer{
		"share distributor present": service.NewBarrierService(&shamirReadyBarrierClient{}),
		"no share distributor":      service.NewBarrierService(nil),
	}
}

func TestBarrierInitShamir_RefusedOverREST(t *testing.T) {
	for name, svc := range refusalFacades() {
		t.Run(name, func(t *testing.T) {
			srv, err := restapi.NewServer(testutil.NewMockQuicBaaS(), restapi.DefaultConfig(), parityLogger(t))
			if err != nil {
				t.Fatalf("restapi.NewServer: %v", err)
			}
			srv.SetBarrierService(svc)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/barrier/shamir/initialize",
				strings.NewReader(`{"threshold":2,"total_shares":3}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.HandleBarrierShamirInitialize(rec, req)

			if rec.Code != http.StatusPreconditionFailed {
				t.Fatalf("REST init-shamir: got %d %s, want 412", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("REST init-shamir body: %v (%s)", err, rec.Body.String())
			}
			if body["code"] != "precondition_failed" {
				t.Errorf("REST init-shamir code = %v, want precondition_failed", body["code"])
			}
			if msg, _ := body["error"].(string); !strings.Contains(msg, ceremonyGuidance) {
				t.Errorf("REST init-shamir error %q does not name %q", msg, ceremonyGuidance)
			}
		})
	}
}

func TestBarrierInitShamir_RefusedOverGRPC(t *testing.T) {
	for name, svc := range refusalFacades() {
		t.Run(name, func(t *testing.T) {
			_, err := grpcapi.NewBarrierServer(svc, parityLogger(t)).
				BarrierShamirInitialize(context.Background(), &pb.BarrierShamirInitializeRequest{Threshold: 2, TotalShares: 3})
			st, ok := status.FromError(err)
			if !ok || st.Code() != codes.FailedPrecondition {
				t.Fatalf("gRPC init-shamir: got %v, want FailedPrecondition", err)
			}
			if !strings.Contains(st.Message(), ceremonyGuidance) {
				t.Errorf("gRPC init-shamir message %q does not name %q", st.Message(), ceremonyGuidance)
			}
		})
	}
}

func TestBarrierInitShamir_RefusedOverMCP(t *testing.T) {
	const tool = "qrdb_barrier_shamir_initialize"
	for name, svc := range refusalFacades() {
		t.Run(name, func(t *testing.T) {
			registry := mcpapi.NewToolRegistry(testutil.NewMockQuicBaaS(), parityLogger(t))
			registry.SetBarrierService(svc)

			_, err := registry.ExecuteTool(context.Background(), tool,
				map[string]any{"threshold": float64(2), "total_shares": float64(3)})
			if err == nil {
				t.Fatal("MCP init-shamir must be refused, got a result")
			}
			result := mcpapi.ToolErrorResult(parityLogger(t), tool, err)
			toolErr, ok := result.StructuredContent.(mcptransport.ToolError)
			if !ok || toolErr.Code != mcptransport.MCPPreconditionFailed {
				t.Fatalf("MCP init-shamir code = %#v, want MCPPreconditionFailed", result.StructuredContent)
			}
			text, ok := result.Content[0].(mcpgo.TextContent)
			if !ok || !strings.Contains(text.Text, ceremonyGuidance) {
				t.Errorf("MCP init-shamir message %#v does not name %q", result.Content[0], ceremonyGuidance)
			}
		})
	}
}
