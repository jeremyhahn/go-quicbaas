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
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Ceremony gRPC handlers.
//
// CeremonyServer is the gRPC wrapper around the QuicBaaS CeremonyServicer
// facade. Like the barrier handler, the facade delegates to the embedded
// go-qrdb SDK CeremonyClient; QuicBaaS only re-publishes the surface so
// gRPC clients dialling the QuicBaaS endpoint can drive ceremony
// operations against the composed stack.
//
// Mirrors pkg/api/rest/handlers_ceremony.go one-for-one.
// =============================================================================

// CeremonyServer wraps the CeremonyServicer facade for the gRPC Ceremony
// service.
type CeremonyServer struct {
	pb.UnimplementedCeremonyServer
	ceremonySvc service.CeremonyServicer
	logger      *logger.Logger
}

// NewCeremonyServer constructs a CeremonyServer over the supplied facade.
// A nil facade is permitted — every RPC short-circuits with
// codes.Unavailable so the handler reports "not yet wired" cleanly.
func NewCeremonyServer(ceremonySvc service.CeremonyServicer, log *logger.Logger) *CeremonyServer {
	srv := &CeremonyServer{ceremonySvc: ceremonySvc}
	if log != nil {
		srv.logger = log.WithField("component", "ceremony-grpc-server")
	}
	return srv
}

// RegisterCeremonyServer registers the Ceremony service implementation on
// the supplied grpc.Server.
func RegisterCeremonyServer(server *grpc.Server, svc service.CeremonyServicer, log *logger.Logger) {
	pb.RegisterCeremonyServer(server, NewCeremonyServer(svc, log))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ceremonyServiceUnavailableError is the canonical "service not wired"
// status returned by every Ceremony RPC when the facade is nil.
func ceremonyServiceUnavailableError() error {
	return status.Error(codes.Unavailable, "ceremony service not available")
}

// requireCeremonyService returns the canonical unavailable status when the
// facade has not yet been wired. Returns nil when the service is present.
func (s *CeremonyServer) requireCeremonyService() error {
	if s.ceremonySvc == nil {
		return ceremonyServiceUnavailableError()
	}
	return nil
}

// mapCeremonyError translates service-layer errors into gRPC status
// errors. The CeremonyServiceMissingError is surfaced as codes.Unavailable
// so it stays distinguishable from genuine failures.
func mapCeremonyError(err error) error {
	if err == nil {
		return nil
	}
	var missing *service.CeremonyServiceMissingError
	if errors.As(err, &missing) {
		return ceremonyServiceUnavailableError()
	}
	return ToGRPCError(err)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// CeremonyInitialize starts the initialization ceremony.
func (s *CeremonyServer) CeremonyInitialize(ctx context.Context, req *pb.CeremonyInitializeRequest) (*pb.CeremonyInitializeResponse, error) {
	if err := s.requireCeremonyService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	officers := make([]service.CeremonyOfficer, 0, len(req.GetOfficers()))
	for _, o := range req.GetOfficers() {
		officers = append(officers, service.CeremonyOfficer{
			Username: o.GetUsername(),
			CSRPEM:   o.GetCsrPem(),
		})
	}

	info, err := s.ceremonySvc.CeremonyInitialize(ctx, &service.CeremonyInitRequest{
		SOPin:                  req.GetSoPin(),
		UserPin:                req.GetUserPin(),
		CredentialSealStrategy: req.GetCredentialSealStrategy(),
		Threshold:              int(req.GetThreshold()),
		Officers:               officers,
		Hostname:               req.GetHostname(),
		SANs:                   req.GetSans(),
	})
	if err != nil {
		return nil, mapCeremonyError(err)
	}

	return &pb.CeremonyInitializeResponse{
		SpkiPin:    info.SPKIPin,
		CaCertPem:  string(info.CACertPEM),
		TlsCertPem: string(info.TLSCertPEM),
		State:      info.State,
		ShareCount: int32(info.ShareCount),
	}, nil
}

// CeremonyStatus returns the current ceremony state.
func (s *CeremonyServer) CeremonyStatus(ctx context.Context, _ *emptypb.Empty) (*pb.CeremonyStatusResponse, error) {
	if err := s.requireCeremonyService(); err != nil {
		return nil, err
	}
	info, err := s.ceremonySvc.CeremonyStatus(ctx)
	if err != nil {
		return nil, mapCeremonyError(err)
	}
	return &pb.CeremonyStatusResponse{
		State:   info.State,
		SpkiPin: info.SPKIPin,
	}, nil
}

// CeremonyClaimCertBegin starts a certificate claim challenge for the
// supplied officer.
func (s *CeremonyServer) CeremonyClaimCertBegin(ctx context.Context, req *pb.CeremonyClaimCertBeginRequest) (*pb.CeremonyClaimCertBeginResponse, error) {
	if err := s.requireCeremonyService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetUsername() == "" {
		return nil, status.Error(codes.InvalidArgument, "username is required")
	}
	challenge, err := s.ceremonySvc.CeremonyBeginClaimCert(ctx, req.GetUsername())
	if err != nil {
		return nil, mapCeremonyError(err)
	}
	return &pb.CeremonyClaimCertBeginResponse{
		NonceHex:  challenge.NonceHex,
		ExpiresAt: formatTime(challenge.ExpiresAt),
	}, nil
}

// CeremonyClaimCertComplete completes a certificate claim by submitting
// the signed nonce. The signature is base64-encoded in transit to mirror
// the REST contract and the service facade signature.
func (s *CeremonyServer) CeremonyClaimCertComplete(ctx context.Context, req *pb.CeremonyClaimCertCompleteRequest) (*pb.CeremonyClaimCertCompleteResponse, error) {
	if err := s.requireCeremonyService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetUsername() == "" {
		return nil, status.Error(codes.InvalidArgument, "username is required")
	}
	if req.GetNonceHex() == "" {
		return nil, status.Error(codes.InvalidArgument, "nonce_hex is required")
	}
	if req.GetSignature() == "" {
		return nil, status.Error(codes.InvalidArgument, "signature is required")
	}
	result, err := s.ceremonySvc.CeremonyCompleteClaimCert(ctx, req.GetUsername(), req.GetNonceHex(), req.GetSignature())
	if err != nil {
		return nil, mapCeremonyError(err)
	}
	return &pb.CeremonyClaimCertCompleteResponse{
		CertPem:   string(result.CertPEM),
		CaCertPem: string(result.CACertPEM),
	}, nil
}

// CeremonyBeginClaimShare starts the Shamir share claim handshake for the
// supplied officer, returning the challenge nonce to sign. The caller's
// certificate thumbprint is derived by the qrdb node from the transport
// connection, never from the request.
func (s *CeremonyServer) CeremonyBeginClaimShare(ctx context.Context, req *pb.CeremonyBeginClaimShareRequest) (*pb.CeremonyBeginClaimShareResponse, error) {
	if err := s.requireCeremonyService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetUsername() == "" {
		return nil, status.Error(codes.InvalidArgument, "username is required")
	}
	challenge, err := s.ceremonySvc.CeremonyBeginClaimShare(ctx, req.GetUsername())
	if err != nil {
		return nil, mapCeremonyError(err)
	}
	return &pb.CeremonyBeginClaimShareResponse{
		NonceHex:  challenge.NonceHex,
		ExpiresAt: formatTime(challenge.ExpiresAt),
	}, nil
}

// CeremonyCompleteClaimShare completes the Shamir share claim handshake by
// submitting the signed challenge nonce. The share is released exactly once.
func (s *CeremonyServer) CeremonyCompleteClaimShare(ctx context.Context, req *pb.CeremonyCompleteClaimShareRequest) (*pb.CeremonyCompleteClaimShareResponse, error) {
	if err := s.requireCeremonyService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetUsername() == "" {
		return nil, status.Error(codes.InvalidArgument, "username is required")
	}
	if req.GetNonceHex() == "" {
		return nil, status.Error(codes.InvalidArgument, "nonce_hex is required")
	}
	if len(req.GetSignature()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "signature is required")
	}
	result, err := s.ceremonySvc.CeremonyCompleteClaimShare(ctx, req.GetUsername(), req.GetNonceHex(), req.GetSignature())
	if err != nil {
		return nil, mapCeremonyError(err)
	}
	return &pb.CeremonyCompleteClaimShareResponse{
		ShareIndex: int32(result.ShareIndex), //nolint:gosec // small index
		Share:      result.ShareJSON,
	}, nil
}

// CeremonySignCSR signs a CSR during the ceremony for an officer or
// service.
func (s *CeremonyServer) CeremonySignCSR(ctx context.Context, req *pb.CeremonySignCSRRequest) (*pb.CeremonySignCSRResponse, error) {
	if err := s.requireCeremonyService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetUsername() == "" {
		return nil, status.Error(codes.InvalidArgument, "username is required")
	}
	if req.GetCsrPem() == "" {
		return nil, status.Error(codes.InvalidArgument, "csr_pem is required")
	}
	result, err := s.ceremonySvc.CeremonySignCSR(ctx, req.GetUsername(), req.GetSoPin(), req.GetCsrPem(), req.GetRole())
	if err != nil {
		return nil, mapCeremonyError(err)
	}
	return &pb.CeremonySignCSRResponse{CertPem: string(result.CertPEM)}, nil
}

// CeremonyListOfficers returns the persisted officer roster.
//
// The CeremonyServicer facade does not (yet) expose a typed ListOfficers
// method — the persistent ceremony-registry surface is still in flight.
// To keep the front-end placeholder logic identical with REST, we return
// an empty roster when the facade is wired but the registry has not been
// surfaced. When the facade itself is unwired, the canonical
// codes.Unavailable status is returned via requireCeremonyService.
func (s *CeremonyServer) CeremonyListOfficers(ctx context.Context, _ *emptypb.Empty) (*pb.CeremonyListOfficersResponse, error) {
	if err := s.requireCeremonyService(); err != nil {
		return nil, err
	}
	return &pb.CeremonyListOfficersResponse{Officers: nil}, nil
}

// CeremonyEnrollExternalOfficer enrols an officer whose key material
// lives outside the daemon. Exactly one of cert_pem or pubkey_pem must
// be supplied, matching the REST contract.
func (s *CeremonyServer) CeremonyEnrollExternalOfficer(ctx context.Context, req *pb.CeremonyEnrollExternalOfficerRequest) (*pb.CeremonyEnrollExternalOfficerResponse, error) {
	if err := s.requireCeremonyService(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetUsername() == "" {
		return nil, status.Error(codes.InvalidArgument, "username is required")
	}
	if req.GetCertPem() == "" && req.GetPubkeyPem() == "" {
		return nil, status.Error(codes.InvalidArgument, "exactly one of cert_pem or pubkey_pem is required")
	}
	if req.GetCertPem() != "" && req.GetPubkeyPem() != "" {
		return nil, status.Error(codes.InvalidArgument, "cert_pem and pubkey_pem are mutually exclusive")
	}
	if req.GetAuthFactor() == "" {
		return nil, status.Error(codes.InvalidArgument, "auth_factor is required")
	}

	resp, err := s.ceremonySvc.CeremonyEnrollExternalOfficer(ctx, &service.CeremonyEnrollExternalOfficerRequest{
		Username:   req.GetUsername(),
		CertPEM:    req.GetCertPem(),
		PubKeyPEM:  req.GetPubkeyPem(),
		ShareIndex: int(req.GetShareIndex()),
		AuthFactor: req.GetAuthFactor(),
		ShareBytes: req.GetShareBytes(),
	})
	if err != nil {
		return nil, mapCeremonyError(err)
	}
	return &pb.CeremonyEnrollExternalOfficerResponse{SealedShare: resp.SealedShare}, nil
}
