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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Compile-time assertion that *Server satisfies the generated
// TrustService server interface. The trust handlers live on the same
// *Server that backs the QuicBaaS facade and the certificate service, so
// registration requires no separate backing type.
var _ pb.TrustServiceServer = (*Server)(nil)

// RegisterTrustServiceServer registers the TrustService handlers with a
// gRPC server. The handlers delegate to the QuicBaaS service's Trust()
// sub-service, which composes the xkms TrustedCA cross-sign / AIA verbs.
// This keeps the gRPC trust surface symmetric with the REST
// /api/v1/trust routes and the QUIC/MCP/Unix/embedded transports.
func RegisterTrustServiceServer(server *grpc.Server, srv *Server) {
	pb.RegisterTrustServiceServer(server, srv)
}

// CrossSignedRootPool returns the active CA's cross-sign-aware root set.
func (s *Server) CrossSignedRootPool(ctx context.Context, req *pb.CrossSignedRootPoolRequest) (*pb.CrossSignedRootPoolResponse, error) {
	trustService := s.service.Trust()
	if trustService == nil {
		return nil, status.Error(codes.Unimplemented, "trust service not available")
	}

	resp, err := trustService.CrossSignedRootPool(ctx, &service.CrossSignedRootPoolRequest{})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.CrossSignedRootPoolResponse{
		RootPems:  resp.RootPEMs,
		Timestamp: timestamppb.New(time.Now()),
	}, nil
}

// AutoImportAIA walks the AIA chain of the supplied DER-encoded
// certificates and imports the referenced issuing CAs.
func (s *Server) AutoImportAIA(ctx context.Context, req *pb.AutoImportAIARequest) (*pb.AutoImportAIAResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if len(req.ChainDer) == 0 {
		return nil, status.Error(codes.InvalidArgument, "chain_der is required")
	}

	trustService := s.service.Trust()
	if trustService == nil {
		return nil, status.Error(codes.Unimplemented, "trust service not available")
	}

	resp, err := trustService.AutoImportAIA(ctx, &service.AutoImportAIARequest{
		ChainDER: req.ChainDer,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.AutoImportAIAResponse{
		Imported:  int32(resp.Imported),
		Timestamp: timestamppb.New(time.Now()),
	}, nil
}

// PublishDANE derives the DANE TLSA records for the supplied certificate
// and publishes them via the registered DANE publisher. Returns a
// not-configured error when no publisher is registered on the server.
func (s *Server) PublishDANE(ctx context.Context, req *pb.PublishDANERequest) (*pb.PublishDANEResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	trustService := s.service.Trust()
	if trustService == nil {
		return nil, status.Error(codes.Unimplemented, "trust service not available")
	}

	resp, err := trustService.PublishDANE(ctx, &service.PublishDANERequest{
		Name:    req.Name,
		Port:    int(req.Port),
		CertDER: req.CertDer,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.PublishDANEResponse{
		Published: int32(resp.Published),
		Timestamp: timestamppb.New(time.Now()),
	}, nil
}
