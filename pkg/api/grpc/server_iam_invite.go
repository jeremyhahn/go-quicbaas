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
	"encoding/json"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Invite activation
//
// The gRPC face of REST's public /api/v1/iam/{tenant_id}/invites/{token}
// routes. These methods are pre-auth (publicGRPCMethods): the one-time invite
// token is the credential. The redemption logic is not reimplemented here;
// every transport delegates to service.InviteActivationService.
// =============================================================================

// inviteActivationProvider is the optional capability an IAMService
// implementation advertises when it can redeem invites, mirroring
// webAuthnCredentialProvider.
type inviteActivationProvider interface {
	InviteActivation() service.InviteActivationService
}

// inviteActivation resolves the invite activation service from the wired IAM
// service, or a typed gRPC status when the deployment cannot serve the verbs.
func (s *IAMServer) inviteActivation() (service.InviteActivationService, error) {
	provider, ok := s.iamService.(inviteActivationProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "invite activation is not available on this node")
	}
	svc := provider.InviteActivation()
	if svc == nil {
		return nil, status.Error(codes.Unavailable, "invite activation is not configured")
	}
	return svc, nil
}

// GetInvite returns the public metadata of a usable invite.
func (s *IAMServer) GetInvite(ctx context.Context, req *iampb.GetInviteRequest) (*iampb.GetInviteResponse, error) {
	svc, err := s.inviteActivation()
	if err != nil {
		return nil, err
	}
	meta, err := svc.GetInvite(ctx, req.GetTenantId(), req.GetToken())
	if err != nil {
		return nil, convertInviteError(err)
	}
	return &iampb.GetInviteResponse{
		UserId:      meta.UserID,
		Email:       meta.Email,
		Name:        meta.Name,
		AuthMethods: meta.AuthMethods,
		ExpiresAt:   timestamppb.New(meta.ExpiresAt),
	}, nil
}

// ActivateFIDO2Begin starts the WebAuthn registration ceremony that enrols the
// invited account's first passkey.
func (s *IAMServer) ActivateFIDO2Begin(ctx context.Context, req *iampb.ActivateInviteRequest) (*iampb.ActivateFIDO2BeginResponse, error) {
	svc, err := s.inviteActivation()
	if err != nil {
		return nil, err
	}
	options, err := svc.ActivateFIDO2Begin(ctx, req.GetTenantId(), req.GetToken())
	if err != nil {
		return nil, convertInviteError(err)
	}
	optionsJSON, err := json.Marshal(options)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to encode credential creation options")
	}
	return &iampb.ActivateFIDO2BeginResponse{OptionsJson: string(optionsJSON)}, nil
}

// ActivateFIDO2Finish completes the ceremony and activates the account.
func (s *IAMServer) ActivateFIDO2Finish(ctx context.Context, req *iampb.ActivateFIDO2FinishRequest) (*iampb.ActivateInviteResponse, error) {
	svc, err := s.inviteActivation()
	if err != nil {
		return nil, err
	}
	// An absent credential stays nil so the service reports it as required.
	var credential map[string]interface{}
	if req.GetCredentialJson() != "" {
		if err := json.Unmarshal([]byte(req.GetCredentialJson()), &credential); err != nil {
			return nil, status.Error(codes.InvalidArgument, "credential_json is not a JSON object")
		}
	}
	activation, err := svc.ActivateFIDO2Finish(ctx, req.GetTenantId(), req.GetToken(), credential)
	if err != nil {
		return nil, convertInviteError(err)
	}
	return &iampb.ActivateInviteResponse{
		UserId:  activation.UserID,
		Message: "activation successful",
		Warning: activation.Warning,
	}, nil
}

// ActivatePIV activates the account with a PIV certificate.
func (s *IAMServer) ActivatePIV(ctx context.Context, req *iampb.ActivatePIVRequest) (*iampb.ActivateInviteResponse, error) {
	svc, err := s.inviteActivation()
	if err != nil {
		return nil, err
	}
	activation, err := svc.ActivatePIV(ctx, req.GetTenantId(), req.GetToken(), []byte(req.GetCertPem()))
	if err != nil {
		return nil, convertInviteError(err)
	}
	return &iampb.ActivateInviteResponse{
		UserId:  activation.UserID,
		Message: "PIV activation successful",
		Warning: activation.Warning,
	}, nil
}

// convertInviteError maps activation failures onto gRPC codes the way REST's
// respondInviteError maps them onto statuses. An absent, expired or spent
// invite collapses to one opaque NotFound so an unauthenticated caller cannot
// probe token state; the conditions are asked by name, not by QBError code,
// because the code of a spent invite is also the code of a rejected PIV chain
// or a sealed tenant (go-quicbaas#434).
func convertInviteError(err error) error {
	switch {
	case iam.IsInviteUnusable(err):
		return status.Error(codes.NotFound, "invite link is not valid")
	case iam.IsUserNotPending(err):
		return status.Error(codes.FailedPrecondition, "user is not in pending state")
	case iam.IsPIVCertRejected(err):
		return status.Error(codes.InvalidArgument, "PIV certificate chain verification failed")
	default:
		return convertIAMError(err)
	}
}
