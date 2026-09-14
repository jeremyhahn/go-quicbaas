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

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// fido2ServicePrefix is the gRPC-style service identifier the SDK FIDO2
// ceremony surface routes through. It matches the const used by the REST /
// QUIC / MCP / embedded transports.
const fido2ServicePrefix = "quicbaas.FIDO2"

// isFIDO2Service reports whether svc names the QuicBaaS FIDO2 ceremony
// service. The SDK FIDO2 surface exchanges the go-xkms transport wire
// structs, which do not implement proto.Message, so they must be converted
// to the generated FIDO2Service proto types first.
func isFIDO2Service(svc string) bool {
	return strings.HasPrefix(svc, fido2ServicePrefix) || svc == "FIDO2"
}

// fido2Handler converts between the go-xkms transport wire structs and the
// generated FIDO2Service proto types, then calls the gRPC FIDO2Service
// registered by the server lifecycle. The tenant scope rides the proto
// tenant_id field, resolved from the request context
// (transport.ContextWithTenant); an empty tenant is rejected server-side by
// the tenant-scoped FIDO2 composition service.
type fido2Handler struct {
	conn *grpc.ClientConn
}

// newFIDO2Handler creates a new FIDO2 handler bound to conn.
func newFIDO2Handler(conn *grpc.ClientConn) *fido2Handler {
	return &fido2Handler{conn: conn}
}

// fido2MethodHandler is the per-method conversion function type used for
// O(1) dispatch.
type fido2MethodHandler func(*fido2Handler, context.Context, pb.FIDO2ServiceClient, string, interface{}) (interface{}, error)

// fido2Methods maps SDK method names onto their conversion handlers.
var fido2Methods = map[string]fido2MethodHandler{
	"BeginRegistration":    (*fido2Handler).handleBeginRegistration,
	"FinishRegistration":   (*fido2Handler).handleFinishRegistration,
	"BeginAuthentication":  (*fido2Handler).handleBeginAuthentication,
	"FinishAuthentication": (*fido2Handler).handleFinishAuthentication,
}

// handleRequest routes FIDO2 requests to the appropriate handler based on
// method name.
func (h *fido2Handler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	fn, ok := fido2Methods[method]
	if !ok {
		return nil, errors.NewUnimplemented("fido2Handler.handleRequest",
			"method not exposed by FIDO2Service gRPC proto: "+method)
	}
	return fn(h, ctx, pb.NewFIDO2ServiceClient(h.conn), transport.TenantFromContext(ctx), req)
}

func (h *fido2Handler) handleBeginRegistration(ctx context.Context, client pb.FIDO2ServiceClient, tenantID string, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*xkmstransport.BeginRegistrationRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("fido2Handler.handleBeginRegistration",
			"invalid request type, expected *xkmstransport.BeginRegistrationRequest")
	}

	pbResp, err := client.BeginRegistration(ctx, &pb.FIDO2BeginRegistrationRequest{
		TenantId: tenantID,
		// The PROTO field keeps the name; the SDK request's was renamed to
		// Email in xkms v0.12, where it is the canonical tenant-unique login
		// key the service resolves with GetByEmail.
		Username:    sdkReq.Email,
		DisplayName: sdkReq.DisplayName,
	})
	if err != nil {
		return nil, convertGRPCError(err, "fido2Handler.handleBeginRegistration")
	}

	return &xkmstransport.BeginRegistrationResponse{
		SessionID:   pbResp.SessionId,
		OptionsJSON: pbResp.OptionsJson,
	}, nil
}

func (h *fido2Handler) handleFinishRegistration(ctx context.Context, client pb.FIDO2ServiceClient, tenantID string, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*xkmstransport.FinishRegistrationRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("fido2Handler.handleFinishRegistration",
			"invalid request type, expected *xkmstransport.FinishRegistrationRequest")
	}

	pbResp, err := client.FinishRegistration(ctx, &pb.FIDO2FinishRegistrationRequest{
		TenantId:        tenantID,
		SessionId:       sdkReq.SessionID,
		AttestationJson: sdkReq.AttestationJSON,
	})
	if err != nil {
		return nil, convertGRPCError(err, "fido2Handler.handleFinishRegistration")
	}

	return &xkmstransport.FinishRegistrationResponse{
		Success:      pbResp.Success,
		CredentialID: pbResp.CredentialId,
		Token:        pbResp.Token,
		Message:      pbResp.Message,
	}, nil
}

func (h *fido2Handler) handleBeginAuthentication(ctx context.Context, client pb.FIDO2ServiceClient, tenantID string, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*xkmstransport.BeginAuthenticationRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("fido2Handler.handleBeginAuthentication",
			"invalid request type, expected *xkmstransport.BeginAuthenticationRequest")
	}

	pbResp, err := client.BeginAuthentication(ctx, &pb.FIDO2BeginAuthenticationRequest{
		TenantId: tenantID,
		Username: sdkReq.Email,
	})
	if err != nil {
		return nil, convertGRPCError(err, "fido2Handler.handleBeginAuthentication")
	}

	return &xkmstransport.BeginAuthenticationResponse{
		SessionID:   pbResp.SessionId,
		OptionsJSON: pbResp.OptionsJson,
	}, nil
}

func (h *fido2Handler) handleFinishAuthentication(ctx context.Context, client pb.FIDO2ServiceClient, tenantID string, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*xkmstransport.FinishAuthenticationRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("fido2Handler.handleFinishAuthentication",
			"invalid request type, expected *xkmstransport.FinishAuthenticationRequest")
	}

	pbResp, err := client.FinishAuthentication(ctx, &pb.FIDO2FinishAuthenticationRequest{
		TenantId:      tenantID,
		SessionId:     sdkReq.SessionID,
		Username:      sdkReq.Email,
		AssertionJson: sdkReq.AssertionJSON,
	})
	if err != nil {
		return nil, convertGRPCError(err, "fido2Handler.handleFinishAuthentication")
	}

	return &xkmstransport.FinishAuthenticationResponse{
		Success:  pbResp.Success,
		Token:    pbResp.Token,
		Username: pbResp.Username,
		Message:  pbResp.Message,
	}, nil
}
