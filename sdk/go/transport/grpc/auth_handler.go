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

package grpc

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// Auth service prefix for routing.
const (
	authServicePrefix = "quicbaas.Auth"
)

// isAuthService checks if the service name is an Auth service.
func isAuthService(svc string) bool {
	return strings.HasPrefix(svc, authServicePrefix)
}

// authHandler handles Auth service requests by converting between SDK types and protobuf types.
type authHandler struct {
	conn *grpc.ClientConn
}

// newAuthHandler creates a new Auth handler.
func newAuthHandler(conn *grpc.ClientConn) *authHandler {
	return &authHandler{conn: conn}
}

// handleRequest routes Auth requests to the appropriate handler based on method name.
func (h *authHandler) handleRequest(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
	client := pb.NewQuicBaaSClient(h.conn)

	switch method {
	case "Login":
		return h.handleLogin(ctx, client, req)
	case "Logout":
		return h.handleLogout(ctx, client, req)
	case "RefreshToken":
		return h.handleRefreshToken(ctx, client, req)
	case "ValidateToken":
		return h.handleValidateToken(ctx, client, req)
	case "BeginLogin":
		return h.handleBeginLogin(ctx, req)
	case "FinishLogin":
		return h.handleFinishLogin(ctx, req)
	default:
		return nil, errors.NewUnimplemented("authHandler.handleRequest",
			"unsupported method: "+method)
	}
}

// handleLogin is the retired legacy username/password login verb. The locked
// architecture has no passwords; authentication is cert/factor-based. This
// path previously submitted an empty password to the server, which is unsafe.
// It now fails closed with a typed PasswordLoginUnsupportedError directing the
// caller to the unified login ceremony (BeginLogin/FinishLogin) with an mtls,
// fido2, or piv factor. The method is retained so the router and downstream
// callers keep compiling; it never reaches the network.
func (h *authHandler) handleLogin(_ context.Context, _ pb.QuicBaaSClient, _ interface{}) (interface{}, error) {
	return nil, errors.NewPasswordLoginUnsupported("authHandler.handleLogin")
}

// handleLogout handles Logout requests.
func (h *authHandler) handleLogout(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	var accessToken string

	switch r := req.(type) {
	case *service.LogoutRequest:
		accessToken = r.AccessToken
	default:
		return nil, errors.NewInvalidInput("authHandler.handleLogout",
			"invalid request type, expected *service.LogoutRequest")
	}

	// Convert to protobuf request.
	pbReq := &pb.LogoutRequest{
		AccessToken: accessToken,
	}

	// Call gRPC.
	_, err := client.Logout(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "authHandler.handleLogout")
	}

	return nil, nil
}

// handleRefreshToken handles RefreshToken requests.
func (h *authHandler) handleRefreshToken(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	var refreshToken string

	switch r := req.(type) {
	case *service.RefreshTokenRequest:
		refreshToken = r.RefreshToken
	default:
		return nil, errors.NewInvalidInput("authHandler.handleRefreshToken",
			"invalid request type, expected *service.RefreshTokenRequest")
	}

	// Convert to protobuf request.
	pbReq := &pb.RefreshTokenRequest{
		RefreshToken: refreshToken,
	}

	// Call gRPC.
	pbResp, err := client.RefreshToken(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "authHandler.handleRefreshToken")
	}

	// Convert protobuf response to SDK response.
	return &service.ExtendedRefreshTokenResponse{
		AccessToken: pbResp.AccessToken,
		ExpiresAt:   time.Unix(0, pbResp.ExpiresAt),
		TokenType:   pbResp.TokenType,
	}, nil
}

// handleValidateToken handles ValidateToken requests.
func (h *authHandler) handleValidateToken(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	var accessToken string

	switch r := req.(type) {
	case *service.ValidateTokenRequest:
		accessToken = r.AccessToken
	default:
		return nil, errors.NewInvalidInput("authHandler.handleValidateToken",
			"invalid request type, expected *service.ValidateTokenRequest")
	}

	// Convert to protobuf request.
	pbReq := &pb.ValidateTokenRequest{
		AccessToken: accessToken,
	}

	// Call gRPC.
	pbResp, err := client.ValidateToken(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "authHandler.handleValidateToken")
	}

	// Convert protobuf response to SDK response.
	return &service.TokenClaims{
		Valid:     pbResp.Valid,
		UserID:    pbResp.UserId,
		Username:  pbResp.Username,
		Roles:     pbResp.Roles,
		ExpiresAt: time.Unix(0, pbResp.ExpiresAt),
	}, nil
}

// handleBeginLogin routes the unified login ceremony BeginLogin step to the
// gRPC Login service. The request/response are go-qrdb transport types.
func (h *authHandler) handleBeginLogin(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*qrdbtransport.BeginLoginRequest)
	if !ok {
		return nil, errors.NewInvalidInput("authHandler.handleBeginLogin",
			"invalid request type, expected *transport.BeginLoginRequest")
	}
	client := pb.NewLoginClient(h.conn)
	pbResp, err := client.BeginLogin(ctx, &pb.BeginLoginRequest{
		Type:     string(r.Type),
		Hint:     r.Hint,
		TenantId: r.TenantID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "authHandler.handleBeginLogin")
	}
	out := &qrdbtransport.BeginLoginResponse{
		SessionID:         pbResp.GetSessionId(),
		Nonce:             pbResp.GetNonce(),
		ChallengeMetadata: pbResp.GetChallengeMetadata(),
	}
	if ts := pbResp.GetExpiresAt(); ts != "" {
		if parsed, perr := time.Parse(time.RFC3339Nano, ts); perr == nil {
			out.ExpiresAt = parsed
		}
	}
	return out, nil
}

// handleFinishLogin routes the unified login ceremony FinishLogin step to the
// gRPC Login service. For an mtls factor the server derives the lookup key
// from the verified peer certificate; the lookup_key here is ignored server
// side for mtls over the network.
func (h *authHandler) handleFinishLogin(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*qrdbtransport.FinishLoginRequest)
	if !ok {
		return nil, errors.NewInvalidInput("authHandler.handleFinishLogin",
			"invalid request type, expected *transport.FinishLoginRequest")
	}
	client := pb.NewLoginClient(h.conn)
	pbResp, err := client.FinishLogin(ctx, &pb.FinishLoginRequest{
		SessionId:      r.SessionID,
		Type:           string(r.Type),
		LookupKey:      r.LookupKey,
		AltLookupKeys:  r.AltLookupKeys,
		Signature:      r.Signature,
		MethodMetadata: r.MethodMetadata,
	})
	if err != nil {
		return nil, convertGRPCError(err, "authHandler.handleFinishLogin")
	}
	out := &qrdbtransport.FinishLoginResponse{
		Bearer:   pbResp.GetBearer(),
		FactorID: pbResp.GetFactorId(),
	}
	if ts := pbResp.GetExpiresAt(); ts != "" {
		if parsed, perr := time.Parse(time.RFC3339Nano, ts); perr == nil {
			out.ExpiresAt = parsed
		}
	}
	if id := pbResp.GetIdentity(); id != nil {
		out.Identity = &qrdbtransport.Identity{
			Subject:  id.GetSubject(),
			TenantID: id.GetTenantId(),
			Roles:    id.GetRoles(),
		}
	}
	return out, nil
}
