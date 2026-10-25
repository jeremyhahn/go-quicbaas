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

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

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

	officers, err := ceremonyOfficersFromPB(req.GetOfficers())
	if err != nil {
		return nil, err
	}

	secret, err := service.NewCeremonySecret(ctx, req.GetSecret())
	if err != nil {
		return nil, mapCeremonyError(err)
	}
	if secret != nil {
		defer secret.Destroy()
	}

	info, err := s.ceremonySvc.CeremonyInitialize(ctx, &service.CeremonyInitRequest{
		SOPin:                  req.GetSoPin(),
		UserPin:                req.GetUserPin(),
		CredentialSealStrategy: req.GetCredentialSealStrategy(),
		Threshold:              int(req.GetThreshold()),
		Policy:                 req.GetPolicy(),
		Officers:               officers,
		Hostname:               req.GetHostname(),
		SANs:                   req.GetSans(),
		AdminCSRPEM:            req.GetAdminCsrPem(),
		AdminCertPEM:           req.GetAdminCertPem(),
		AdminCertPoolPEM:       req.GetAdminCertPoolPem(),
		AdminGenerate:          req.GetAdminGenerate(),
		Secret:                 secret,
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

		Holders:               ceremonyHoldersToPB(info.Holders),
		AdminCertPem:          string(info.AdminCertPEM),
		AdminKeyPem:           info.AdminKeyPEM,
		AdminCredentialSource: info.AdminCredentialSource,
	}, nil
}

// ceremonyOfficersFromPB decodes the ceremony roster into go-qrdb's own
// officer type, so every field go-qrdb defines reaches it.
func ceremonyOfficersFromPB(in []*pb.CeremonyOfficer) ([]service.CeremonyOfficer, error) {
	officers := make([]service.CeremonyOfficer, 0, len(in))
	for i, o := range in {
		factors, err := holderFactorsFromPB(i, o.GetFactors())
		if err != nil {
			return nil, err
		}
		officers = append(officers, service.CeremonyOfficer{
			Username:      o.GetUsername(),
			Role:          o.GetRole(),
			CSRPEM:        o.GetCsrPem(),
			EncryptionKEM: o.GetEncryptionKem(),
			EncryptionKey: o.GetEncryptionKey(),
			Factors:       factors,
			Designated:    o.GetDesignated(),
		})
	}
	return officers, nil
}

// ceremonyHoldersToPB encodes the certificates go-qrdb issued to the initial
// holders.
func ceremonyHoldersToPB(in []qrdbtransport.CeremonyHolderCert) []*pb.CeremonyHolderCert {
	if len(in) == 0 {
		return nil
	}
	out := make([]*pb.CeremonyHolderCert, len(in))
	for i, h := range in {
		out[i] = &pb.CeremonyHolderCert{Username: h.Username, Role: h.Role, CertPem: h.CertPEM}
	}
	return out
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
