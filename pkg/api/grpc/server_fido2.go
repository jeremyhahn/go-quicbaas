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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// Compile-time assertion that *Server satisfies the generated FIDO2Service
// server interface. The FIDO2 handlers live on the same *Server that backs
// the QuicBaaS facade; every verb delegates to the tenant-scoped
// service.FIDO2() composition service, so registration requires no separate
// backing type.
var _ pb.FIDO2ServiceServer = (*Server)(nil)

// RegisterFIDO2ServiceServer registers the FIDO2Service handlers with a
// gRPC server. The handlers delegate to the QuicBaaS service's FIDO2()
// sub-service (the go-xkms FIDO2 ceremony contract, tenant-scoped), keeping
// the gRPC ceremony surface symmetric with the REST
// /api/v1/iam/{tenant}/webauthn routes and the QUIC/MCP/Unix/embedded
// transports.
func RegisterFIDO2ServiceServer(server *grpc.Server, srv *Server) {
	pb.RegisterFIDO2ServiceServer(server, srv)
}

// BeginRegistration starts a WebAuthn registration ceremony for the
// tenant's relying party.
func (s *Server) BeginRegistration(ctx context.Context, req *pb.FIDO2BeginRegistrationRequest) (*pb.FIDO2BeginRegistrationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	fido2Service := s.service.FIDO2()
	if fido2Service == nil {
		return nil, status.Error(codes.Unimplemented, "fido2 service not available")
	}

	resp, err := fido2Service.BeginRegistration(ctx, req.TenantId, &xkmstransport.BeginRegistrationRequest{
		// xkms v0.12 renamed this field to Email and made it the canonical
		// tenant-unique login key: the service resolves the account with
		// GetByEmail, and a username is a different identifier it does not
		// accept. The proto field keeps its name for wire compatibility, so
		// this is the one place the two spellings meet.
		Email:       req.Username,
		DisplayName: req.DisplayName,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.FIDO2BeginRegistrationResponse{
		SessionId:   resp.SessionID,
		OptionsJson: resp.OptionsJSON,
	}, nil
}

// FinishRegistration completes a WebAuthn registration ceremony with the
// authenticator attestation.
func (s *Server) FinishRegistration(ctx context.Context, req *pb.FIDO2FinishRegistrationRequest) (*pb.FIDO2FinishRegistrationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	fido2Service := s.service.FIDO2()
	if fido2Service == nil {
		return nil, status.Error(codes.Unimplemented, "fido2 service not available")
	}

	resp, err := fido2Service.FinishRegistration(ctx, req.TenantId, &xkmstransport.FinishRegistrationRequest{
		SessionID:       req.SessionId,
		AttestationJSON: req.AttestationJson,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.FIDO2FinishRegistrationResponse{
		Success:      resp.Success,
		CredentialId: resp.CredentialID,
		Token:        resp.Token,
		Message:      resp.Message,
	}, nil
}

// BeginAddCredential starts a ceremony that adds a passkey to the
// authenticated caller's own account. The request names neither account nor
// tenant: both are the caller's, and the tenant is the one the authorization
// interceptor bound to the context. Finish with FinishRegistration.
func (s *Server) BeginAddCredential(ctx context.Context, req *pb.FIDO2BeginAddCredentialRequest) (*pb.FIDO2BeginRegistrationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	fido2Service := s.service.FIDO2()
	if fido2Service == nil {
		return nil, status.Error(codes.Unimplemented, "fido2 service not available")
	}

	tenantID, _ := tenantctx.Lookup(ctx)
	resp, err := fido2Service.BeginAddCredential(ctx, tenantID)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.FIDO2BeginRegistrationResponse{
		SessionId:   resp.SessionID,
		OptionsJson: resp.OptionsJSON,
	}, nil
}

// BeginAuthentication starts a WebAuthn authentication ceremony for the
// tenant's relying party. This verb is pre-auth: WebAuthn IS the
// authentication mechanism.
func (s *Server) BeginAuthentication(ctx context.Context, req *pb.FIDO2BeginAuthenticationRequest) (*pb.FIDO2BeginAuthenticationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	fido2Service := s.service.FIDO2()
	if fido2Service == nil {
		return nil, status.Error(codes.Unimplemented, "fido2 service not available")
	}

	resp, err := fido2Service.BeginAuthentication(ctx, req.TenantId, &xkmstransport.BeginAuthenticationRequest{
		Email: req.Username,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.FIDO2BeginAuthenticationResponse{
		SessionId:   resp.SessionID,
		OptionsJson: resp.OptionsJSON,
	}, nil
}

// FinishAuthentication completes a WebAuthn authentication ceremony with
// the authenticator assertion. This verb is pre-auth: a successful ceremony
// is what mints the caller's token.
func (s *Server) FinishAuthentication(ctx context.Context, req *pb.FIDO2FinishAuthenticationRequest) (*pb.FIDO2FinishAuthenticationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	fido2Service := s.service.FIDO2()
	if fido2Service == nil {
		return nil, status.Error(codes.Unimplemented, "fido2 service not available")
	}

	resp, err := fido2Service.FinishAuthentication(ctx, req.TenantId, &xkmstransport.FinishAuthenticationRequest{
		SessionID:     req.SessionId,
		Email:         req.Username,
		AssertionJSON: req.AssertionJson,
	})
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.FIDO2FinishAuthenticationResponse{
		Success:  resp.Success,
		Token:    resp.Token,
		Username: resp.Username,
		Message:  resp.Message,
	}, nil
}
