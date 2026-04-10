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
	default:
		return nil, errors.NewUnimplemented("authHandler.handleRequest",
			"unsupported method: "+method)
	}
}

// handleLogin handles Login requests.
func (h *authHandler) handleLogin(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	// Handle the ExtendedLoginRequest type from the service package.
	var username, password string

	switch r := req.(type) {
	case *service.ExtendedLoginRequest:
		username = r.Username
		password = r.Password
	default:
		return nil, errors.NewInvalidInput("authHandler.handleLogin",
			"invalid request type, expected *service.ExtendedLoginRequest")
	}

	// Convert SDK request to protobuf request.
	pbReq := &pb.LoginRequest{
		Username: username,
		Password: password,
	}

	// Call gRPC.
	pbResp, err := client.Login(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "authHandler.handleLogin")
	}

	// Convert protobuf response to SDK response.
	return &service.ExtendedLoginResponse{
		AccessToken:  pbResp.AccessToken,
		RefreshToken: pbResp.RefreshToken,
		ExpiresAt:    time.Unix(0, pbResp.ExpiresAt),
		TokenType:    pbResp.TokenType,
		MFARequired:  false,
		MFAChallenge: nil,
	}, nil
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
