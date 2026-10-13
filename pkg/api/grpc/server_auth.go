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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Logout invalidates the current session and tokens.
func (s *Server) Logout(ctx context.Context, req *pb.LogoutRequest) (*pb.LogoutResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Convert proto request to service request
	serviceReq := &service.LogoutRequest{
		AccessToken: req.GetAccessToken(),
	}

	// Call service layer
	serviceResp, err := s.service.Logout(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Convert service response to proto response
	return &pb.LogoutResponse{
		Success: serviceResp.Success,
	}, nil
}

// RefreshToken refreshes an access token using a refresh token.
func (s *Server) RefreshToken(ctx context.Context, req *pb.RefreshTokenRequest) (*pb.RefreshTokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Convert proto request to service request
	serviceReq := &service.RefreshTokenRequest{
		RefreshToken: req.GetRefreshToken(),
	}

	// Call service layer
	serviceResp, err := s.service.RefreshToken(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	tokenType := serviceResp.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}

	// Convert service response to proto response. The rotated refresh
	// credential is carried back; the presented one is now revoked.
	return &pb.RefreshTokenResponse{
		AccessToken:      serviceResp.AccessToken,
		ExpiresAt:        serviceResp.ExpiresAt,
		TokenType:        tokenType,
		RefreshToken:     serviceResp.RefreshToken,
		RefreshExpiresAt: serviceResp.RefreshExpiresAt,
	}, nil
}

// ValidateToken validates an access token and returns user info.
func (s *Server) ValidateToken(ctx context.Context, req *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Convert proto request to service request
	serviceReq := &service.ValidateTokenRequest{
		AccessToken: req.GetAccessToken(),
	}

	// Call service layer
	serviceResp, err := s.service.ValidateToken(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	// Convert service response to proto response
	return &pb.ValidateTokenResponse{
		Valid:     serviceResp.Valid,
		UserId:    serviceResp.UserID,
		Username:  serviceResp.Username,
		TenantId:  serviceResp.TenantID,
		Roles:     serviceResp.Roles,
		ExpiresAt: serviceResp.ExpiresAt,
	}, nil
}

// IntrospectToken implements the IntrospectToken RPC: the RFC 7662
// introspection REST serves at /oauth2/introspect, answered by the same
// service-layer method. An inactive token is a normal reply carrying only
// active=false.
func (s *Server) IntrospectToken(ctx context.Context, req *pb.IntrospectTokenRequest) (*pb.IntrospectTokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	introspection, err := s.service.IntrospectToken(ctx, &service.IntrospectTokenRequest{
		Token: req.GetToken(),
	})
	if err != nil {
		return nil, toProtoError(err)
	}
	if !introspection.Active {
		return &pb.IntrospectTokenResponse{Active: false}, nil
	}

	return &pb.IntrospectTokenResponse{
		Active:       true,
		Scope:        introspection.Scope,
		ClientId:     introspection.ClientID,
		Username:     introspection.Username,
		TokenType:    introspection.TokenType,
		ExpiresAt:    unixSecondsOrZero(introspection.ExpiresAt),
		IssuedAt:     unixSecondsOrZero(introspection.IssuedAt),
		NotBefore:    unixSecondsOrZero(introspection.NotBefore),
		Subject:      introspection.Subject,
		Audience:     introspection.Audience,
		Issuer:       introspection.Issuer,
		Jti:          introspection.JTI,
		AttestLevel:  introspection.AttestLevel,
		AttestAaguid: introspection.AttestAAGUID,
	}, nil
}

// unixSecondsOrZero converts a time to Unix seconds, keeping the zero time as 0.
func unixSecondsOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
