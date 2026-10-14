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
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// WebAuthn credential administration
//
// These are the gRPC face of the same verbs REST serves at
// /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials. They exist
// on this transport because the CLI's default transport is not REST: an admin
// recovery verb that only works over one transport is not a recovery verb.
//
// The credential-store access itself is NOT reimplemented here. Every transport
// resolves through service.WebAuthnCredentialService, which owns the one part
// that must not vary — translating the IAM user id the caller was authorized
// against into the (tenant, email) key the credential store uses.
// =============================================================================

// webAuthnCredentialProvider is the optional capability an IAMService
// implementation advertises when it can administer WebAuthn credentials. It is
// a structural assertion rather than a method on the IAMService interface so a
// deployment (or a test double) that predates these verbs keeps compiling and
// simply reports Unimplemented.
type webAuthnCredentialProvider interface {
	WebAuthnCredentials() service.WebAuthnCredentialService
}

// webAuthnCredentials resolves the credential administration service from the
// wired IAM service, or a typed gRPC status when the deployment cannot serve
// these verbs.
func (s *IAMServer) webAuthnCredentials() (service.WebAuthnCredentialService, error) {
	provider, ok := s.iamService.(webAuthnCredentialProvider)
	if !ok {
		return nil, status.Error(codes.Unimplemented,
			"WebAuthn credential administration is not available on this node")
	}
	svc := provider.WebAuthnCredentials()
	if svc == nil {
		return nil, status.Error(codes.Unavailable,
			"WebAuthn credential administration is not configured")
	}
	return svc, nil
}

// ListWebAuthnCredentials returns the WebAuthn authenticators registered to a
// user.
func (s *IAMServer) ListWebAuthnCredentials(ctx context.Context, req *iampb.ListWebAuthnCredentialsRequest) (*iampb.ListWebAuthnCredentialsResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.IAM", req.TenantId); err != nil {
		return nil, err
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	svc, err := s.webAuthnCredentials()
	if err != nil {
		return nil, err
	}
	userID, err := parseUserID("user_id", req.UserId)
	if err != nil {
		return nil, err
	}
	credentials, err := svc.ListCredentials(ctx, req.TenantId, userID)
	if err != nil {
		return nil, convertWebAuthnCredentialError(err)
	}

	out := make([]*iampb.WebAuthnCredential, 0, len(credentials))
	for _, credential := range credentials {
		out = append(out, webAuthnCredentialToProto(credential))
	}
	return &iampb.ListWebAuthnCredentialsResponse{
		Credentials: out,
		Total:       int32(len(out)),
	}, nil
}

// DeleteWebAuthnCredential removes one of a user's registered authenticators.
func (s *IAMServer) DeleteWebAuthnCredential(ctx context.Context, req *iampb.DeleteWebAuthnCredentialRequest) (*iampb.DeleteWebAuthnCredentialResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.IAM", req.TenantId); err != nil {
		return nil, err
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.CredentialId == "" {
		return nil, status.Error(codes.InvalidArgument, "credential_id is required")
	}

	svc, err := s.webAuthnCredentials()
	if err != nil {
		return nil, err
	}
	userID, err := parseUserID("user_id", req.UserId)
	if err != nil {
		return nil, err
	}
	if dErr := svc.DeleteCredential(ctx, req.TenantId, userID, req.CredentialId); dErr != nil {
		return nil, convertWebAuthnCredentialError(dErr)
	}
	return &iampb.DeleteWebAuthnCredentialResponse{Success: true}, nil
}

// DeleteWebAuthnCredentials removes every authenticator registered to a user.
func (s *IAMServer) DeleteWebAuthnCredentials(ctx context.Context, req *iampb.DeleteWebAuthnCredentialsRequest) (*iampb.DeleteWebAuthnCredentialsResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.IAM", req.TenantId); err != nil {
		return nil, err
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	svc, err := s.webAuthnCredentials()
	if err != nil {
		return nil, err
	}
	userID, err := parseUserID("user_id", req.UserId)
	if err != nil {
		return nil, err
	}
	deleted, err := svc.DeleteCredentials(ctx, req.TenantId, userID)
	if err != nil {
		return nil, convertWebAuthnCredentialError(err)
	}
	return &iampb.DeleteWebAuthnCredentialsResponse{Deleted: int32(deleted)}, nil
}

// webAuthnCredentialToProto projects the domain summary onto the wire message.
// Both carry the same non-secret authenticator context and no key material.
func webAuthnCredentialToProto(c iam.WebAuthnCredentialSummary) *iampb.WebAuthnCredential {
	return &iampb.WebAuthnCredential{
		Id:              c.ID,
		Aaguid:          c.AAGUID,
		AttestationType: c.AttestationType,
		Transports:      c.Transports,
		SignCount:       c.SignCount,
		BackupEligible:  c.BackupEligible,
		BackupState:     c.BackupState,
		CloneWarning:    c.CloneWarning,
	}
}

// convertWebAuthnCredentialError maps the credential surface's typed errors
// onto gRPC codes, so a bad credential id and an unknown one are distinguished
// on the wire the same way they are over REST. A truncated id — a fragment of
// an id the account holds — is InvalidArgument for the same reason REST answers
// 400: the input is malformed, not the resource missing. Everything else falls
// through to the shared IAM error conversion.
func convertWebAuthnCredentialError(err error) error {
	var truncated *iam.WebAuthnCredentialTruncatedIDError
	if errors.As(err, &truncated) {
		return status.Error(codes.InvalidArgument, truncated.Error())
	}
	var notFound *iam.WebAuthnCredentialNotFoundError
	if errors.As(err, &notFound) {
		return status.Error(codes.NotFound, notFound.Error())
	}
	var badID *iam.WebAuthnCredentialIDError
	if errors.As(err, &badID) {
		return status.Error(codes.InvalidArgument, badID.Error())
	}
	return convertIAMError(err)
}
