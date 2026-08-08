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
	"crypto/x509"
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Unified Login Ceremony gRPC handlers.
//
// LoginServer wraps the QuicBaaS LoginCeremony() facade. The facade forwards
// to the embedded go-qrdb node's in-process LoginClient and owns the mTLS
// anti-spoofing model. This handler's sole security responsibility is to
// extract the VERIFIED peer certificate from the gRPC TLS handshake and place
// it on the context so the facade can derive the mtls lookup key from it (and
// never from the client-supplied request body).
// =============================================================================

// LoginServer wraps the LoginServicer facade for the gRPC Login service.
type LoginServer struct {
	pb.UnimplementedLoginServer
	loginSvc service.LoginServicer
	logger   *logger.Logger
}

// NewLoginServer constructs a LoginServer over the supplied facade. A nil
// facade is permitted — every RPC short-circuits with codes.Unavailable.
func NewLoginServer(loginSvc service.LoginServicer, log *logger.Logger) *LoginServer {
	srv := &LoginServer{loginSvc: loginSvc}
	if log != nil {
		srv.logger = log.WithField("component", "login-grpc-server")
	}
	return srv
}

// RegisterLoginServer registers the Login service implementation on the
// supplied grpc.Server.
func RegisterLoginServer(server *grpc.Server, svc service.LoginServicer, log *logger.Logger) {
	pb.RegisterLoginServer(server, NewLoginServer(svc, log))
}

// verifiedPeerCertFromGRPC extracts the leaf certificate from the gRPC TLS
// handshake's VERIFIED chain. Returns nil when the connection is not mTLS or
// no chain was verified. Using VerifiedChains (not the raw PeerCertificates)
// guarantees the certificate passed chain validation against the configured
// client CA — the facade keys an mtls login off it.
func verifiedPeerCertFromGRPC(ctx context.Context) *x509.Certificate {
	p, ok := peer.FromContext(ctx)
	if !ok || p == nil {
		return nil
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return nil
	}
	state := tlsInfo.State
	if len(state.VerifiedChains) > 0 && len(state.VerifiedChains[0]) > 0 {
		return state.VerifiedChains[0][0]
	}
	return nil
}

// mapLoginError translates service-layer login errors into gRPC status errors.
func mapLoginError(err error) error {
	if err == nil {
		return nil
	}
	var unavailable *service.LoginCeremonyUnavailableError
	if errors.As(err, &unavailable) {
		return status.Error(codes.Unavailable, err.Error())
	}
	var peerRequired *service.MTLSPeerCertRequiredError
	if errors.As(err, &peerRequired) {
		return status.Error(codes.Unauthenticated, err.Error())
	}
	var nilReq *service.LoginRequestNilError
	if errors.As(err, &nilReq) {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return status.Error(codes.Internal, err.Error())
}

// BeginLogin handles the gRPC BeginLogin RPC.
func (s *LoginServer) BeginLogin(ctx context.Context, in *pb.BeginLoginRequest) (*pb.BeginLoginResponse, error) {
	if s.loginSvc == nil {
		return nil, status.Error(codes.Unavailable, "login service not available")
	}
	req := &qrdbtransport.BeginLoginRequest{
		Type:     qrdbtransport.FactorType(in.GetType()),
		Hint:     in.GetHint(),
		TenantID: in.GetTenantId(),
	}
	resp, err := s.loginSvc.BeginLogin(ctx, req)
	if err != nil {
		return nil, mapLoginError(err)
	}
	return &pb.BeginLoginResponse{
		SessionId:         resp.SessionID,
		Nonce:             resp.Nonce,
		ChallengeMetadata: resp.ChallengeMetadata,
		ExpiresAt:         resp.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

// FinishLogin handles the gRPC FinishLogin RPC. The verified peer certificate
// is placed on the context so the facade derives the mtls lookup key from it.
func (s *LoginServer) FinishLogin(ctx context.Context, in *pb.FinishLoginRequest) (*pb.FinishLoginResponse, error) {
	if s.loginSvc == nil {
		return nil, status.Error(codes.Unavailable, "login service not available")
	}

	// Attach the verified peer certificate (network transport). The facade
	// fails closed for an mtls request when this is absent.
	if cert := verifiedPeerCertFromGRPC(ctx); cert != nil {
		ctx = service.WithVerifiedPeerCert(ctx, cert)
	}

	req := &qrdbtransport.FinishLoginRequest{
		SessionID:      in.GetSessionId(),
		Type:           qrdbtransport.FactorType(in.GetType()),
		LookupKey:      in.GetLookupKey(),
		AltLookupKeys:  in.GetAltLookupKeys(),
		Signature:      in.GetSignature(),
		MethodMetadata: in.GetMethodMetadata(),
	}
	resp, err := s.loginSvc.FinishLogin(ctx, req)
	if err != nil {
		return nil, mapLoginError(err)
	}

	out := &pb.FinishLoginResponse{
		Bearer:       resp.Bearer,
		ExpiresAt:    resp.ExpiresAt.UTC().Format(time.RFC3339Nano),
		FactorId:     resp.FactorID,
		RefreshToken: resp.RefreshToken,
	}
	if !resp.RefreshExpiresAt.IsZero() {
		out.RefreshExpiresAt = resp.RefreshExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if resp.Identity != nil {
		out.Identity = &pb.LoginIdentity{
			Subject:  resp.Identity.Subject,
			TenantId: resp.Identity.TenantID,
			Roles:    resp.Identity.Roles,
		}
	}
	return out, nil
}
