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

package grpc

import (
	"context"
	"strings"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	trustclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// trustServicePrefix is the gRPC-style service identifier the SDK trust
// surface routes through. It matches the const used by the REST / QUIC /
// MCP / embedded transports.
const trustServicePrefix = "quicbaas.Trust"

// isTrustService reports whether the service name targets the
// quicbaas.Trust surface so doRequest can branch into the typed trust
// handler. The SDK trust surface uses plain Go structs that do not
// implement proto.Message, so they must be converted to the generated
// TrustService proto types first.
func isTrustService(svc string) bool {
	return strings.HasPrefix(svc, trustServicePrefix)
}

// trustHandler converts between the leaf client/trust structs and the
// generated TrustService proto types, then calls the gRPC TrustService
// registered by the server lifecycle.
type trustHandler struct {
	conn *grpc.ClientConn
}

// newTrustHandler creates a new trust handler bound to conn.
func newTrustHandler(conn *grpc.ClientConn) *trustHandler {
	return &trustHandler{conn: conn}
}

// handleRequest routes Trust requests to the appropriate handler based on
// method name. PublishDANE has no proto analogue and returns a clear
// unimplemented error rather than falling through to the untyped path.
func (h *trustHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	client := pb.NewTrustServiceClient(h.conn)

	switch method {
	case "CrossSignedRootPool":
		return h.handleCrossSignedRootPool(ctx, client, req)
	case "AutoImportAIA":
		return h.handleAutoImportAIA(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("trustHandler.handleRequest",
			"method not exposed by TrustService gRPC proto: "+method)
	}
}

func (h *trustHandler) handleCrossSignedRootPool(ctx context.Context, client pb.TrustServiceClient, req interface{}) (interface{}, error) {
	if req != nil {
		if _, ok := req.(*trustclient.CrossSignedRootPoolRequest); !ok {
			return nil, errors.NewInvalidInput("trustHandler.handleCrossSignedRootPool",
				"invalid request type, expected *trust.CrossSignedRootPoolRequest")
		}
	}

	pbResp, err := client.CrossSignedRootPool(ctx, &pb.CrossSignedRootPoolRequest{})
	if err != nil {
		return nil, convertGRPCError(err, "trustHandler.handleCrossSignedRootPool")
	}

	return &trustclient.CrossSignedRootPoolResponse{RootPEMs: pbResp.RootPems}, nil
}

func (h *trustHandler) handleAutoImportAIA(ctx context.Context, client pb.TrustServiceClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*trustclient.AutoImportAIARequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("trustHandler.handleAutoImportAIA",
			"invalid request type, expected *trust.AutoImportAIARequest")
	}
	if len(sdkReq.ChainDER) == 0 {
		return nil, errors.NewInvalidInput("trustHandler.handleAutoImportAIA",
			"chain is required")
	}

	pbResp, err := client.AutoImportAIA(ctx, &pb.AutoImportAIARequest{
		ChainDer: sdkReq.ChainDER,
	})
	if err != nil {
		return nil, convertGRPCError(err, "trustHandler.handleAutoImportAIA")
	}

	return &trustclient.AutoImportAIAResponse{Imported: int(pbResp.Imported)}, nil
}
