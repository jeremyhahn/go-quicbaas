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
// AttestationService server interface. The attestation handlers live on the
// same *Server that backs the QuicBaaS facade, so registration requires no
// separate backing type.
var _ pb.AttestationServiceServer = (*Server)(nil)

// RegisterAttestationServiceServer registers the AttestationService handlers
// with a gRPC server. The handlers delegate to the QuicBaaS service's
// Attestation() sub-service, which composes the go-xkms attestation verifier
// and the container attestation orchestration seam. This keeps the gRPC
// attestation surface symmetric with the REST /api/v1/attestation routes and
// the QUIC/MCP/Unix/embedded transports.
func RegisterAttestationServiceServer(server *grpc.Server, srv *Server) {
	pb.RegisterAttestationServiceServer(server, srv)
}

// Verify validates a complete attestation statement.
func (s *Server) Verify(ctx context.Context, req *pb.VerifyAttestationRequest) (*pb.VerifyAttestationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	attestationService := s.service.Attestation()
	if attestationService == nil {
		return nil, status.Error(codes.Unimplemented, "attestation service not available")
	}

	resp, err := attestationService.Verify(ctx, &service.VerifyAttestationRequest{
		Format:             req.Format,
		Backend:            req.Backend,
		ChainPEM:           req.ChainPem,
		AttestedKeyDER:     req.AttestedKeyDer,
		AttestationData:    req.AttestationData,
		Signature:          req.Signature,
		SignatureAlgorithm: req.SignatureAlgorithm,
		Nonce:              req.Nonce,
		CreatedAt:          req.CreatedAt,
		PCRs:               req.Pcrs,
		Options:            attestationOptionsFromProto(req.Options),
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.VerifyAttestationResponse{
		Valid:          resp.Valid,
		Format:         resp.Format,
		Backend:        resp.Backend,
		ChainValid:     resp.ChainValid,
		SignatureValid: resp.SignatureValid,
		FreshnessValid: resp.FreshnessValid,
		PcrsValid:      resp.PCRsValid,
		Reason:         resp.Reason,
		Timestamp:      timestamppb.New(time.Now()),
	}, nil
}

// VerifyChain validates only the certificate chain of an attestation.
func (s *Server) VerifyChain(ctx context.Context, req *pb.VerifyAttestationChainRequest) (*pb.VerifyAttestationChainResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if len(req.ChainPem) == 0 {
		return nil, status.Error(codes.InvalidArgument, "chain_pem is required")
	}

	attestationService := s.service.Attestation()
	if attestationService == nil {
		return nil, status.Error(codes.Unimplemented, "attestation service not available")
	}

	resp, err := attestationService.VerifyChain(ctx, &service.VerifyAttestationChainRequest{
		Format:   req.Format,
		ChainPEM: req.ChainPem,
		Options:  attestationOptionsFromProto(req.Options),
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.VerifyAttestationChainResponse{
		Valid:     resp.Valid,
		Reason:    resp.Reason,
		Timestamp: timestamppb.New(time.Now()),
	}, nil
}

// AttestContainer crosses the orchestration seam to the registered container
// attestor. Returns a not-configured error until one is registered.
func (s *Server) AttestContainer(ctx context.Context, req *pb.AttestContainerRequest) (*pb.AttestContainerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if req.Image == "" {
		return nil, status.Error(codes.InvalidArgument, "image is required")
	}

	attestationService := s.service.Attestation()
	if attestationService == nil {
		return nil, status.Error(codes.Unimplemented, "attestation service not available")
	}

	resp, err := attestationService.AttestContainer(ctx, &service.AttestContainerRequest{
		DeviceID:        req.DeviceId,
		ContainerID:     req.ContainerId,
		Image:           req.Image,
		ImageDigest:     req.ImageDigest,
		TrustLabel:      req.TrustLabel,
		TrustLevel:      req.TrustLevel,
		ExpectedPCRs:    req.ExpectedPcrs,
		MeasurementPCR:  int(req.MeasurementPcr),
		MeasurementBank: req.MeasurementBank,
		Nonce:           req.Nonce,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	out := &pb.AttestContainerResponse{
		Attested:          resp.Attested,
		DeviceId:          resp.DeviceID,
		ContainerId:       resp.ContainerID,
		Image:             resp.Image,
		ImageDigest:       resp.ImageDigest,
		ConfigDigest:      resp.ConfigDigest,
		TrustLabel:        resp.TrustLabel,
		TrustLevel:        resp.TrustLevel,
		Nonce:             resp.Nonce,
		Quoted:            resp.Quoted,
		Signature:         resp.Signature,
		Pcrs:              resp.PCRs,
		EventLog:          resp.EventLog,
		Measured:          resp.Measured,
		MeasurementPcr:    int32(resp.MeasurementPCR),
		MeasurementBank:   resp.MeasurementBank,
		LaunchMeasurement: resp.LaunchMeasurement,
		Timestamp:         timestamppb.New(time.Now()),
	}
	if !resp.AttestedAt.IsZero() {
		out.AttestedAt = timestamppb.New(resp.AttestedAt)
	}
	return out, nil
}

// attestationOptionsFromProto converts proto verify options into the
// service-layer form. A nil input stays nil so the service applies the xkms
// secure defaults rather than an all-false struct.
func attestationOptionsFromProto(opts *pb.AttestationVerifyOptions) *service.VerifyAttestationOptions {
	if opts == nil {
		return nil
	}
	return &service.VerifyAttestationOptions{
		TrustedRootsPEM:        opts.TrustedRootsPem,
		CheckFreshness:         opts.CheckFreshness,
		ExpectedNonce:          opts.ExpectedNonce,
		FreshnessWindowSeconds: opts.FreshnessWindowSeconds,
		VerifyPCRs:             opts.VerifyPcrs,
		ExpectedPCRs:           opts.ExpectedPcrs,
		AllowSelfSigned:        opts.AllowSelfSigned,
	}
}
