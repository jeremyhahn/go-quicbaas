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

	// Convert service response to proto response
	return &pb.RefreshTokenResponse{
		AccessToken: serviceResp.AccessToken,
		ExpiresAt:   serviceResp.ExpiresAt,
		TokenType:   "Bearer",
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
		Roles:     serviceResp.Roles,
		ExpiresAt: serviceResp.ExpiresAt,
	}, nil
}
