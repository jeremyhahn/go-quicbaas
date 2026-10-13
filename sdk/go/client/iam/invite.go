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

package iam

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	inviteServiceName = "quicbaas.IAM.Invite"
)

// InviteResponse is returned when a user is created via the invite flow.
type InviteResponse struct {
	// UserID is the newly created user's ID.
	UserID string `json:"user_id"`

	// InviteToken is the one-time plaintext token (shown once; share out-of-band).
	InviteToken string `json:"invite_token"`

	// InviteURL is the fully-formed activation URL containing the token.
	// This URL is safe to share directly to the recipient.
	InviteURL string `json:"invite_url"`

	// ExpiresAt is when the invite expires.
	ExpiresAt time.Time `json:"expires_at"`
}

// InviteMetadata contains publicly visible information about a pending invite.
// This is returned without any authentication so the activation UI can pre-populate
// the recipient's details.
type InviteMetadata struct {
	// UserID is the user this invite activates.
	UserID string `json:"user_id"`

	// Email is the recipient's email address.
	Email string `json:"email,omitempty"`

	// Name is the recipient's display name.
	Name string `json:"name,omitempty"`

	// AuthMethods lists the methods the user may use to activate.
	AuthMethods []string `json:"auth_methods"`

	// ExpiresAt is when the invite expires.
	ExpiresAt time.Time `json:"expires_at"`
}

// ActivationResponse is returned on successful FIDO2 or PIV activation.
type ActivationResponse struct {
	// UserID is the activated user's ID.
	UserID string `json:"user_id"`

	// Message is a human-readable confirmation.
	Message string `json:"message"`

	// Warning records a non-fatal irregularity behind an otherwise successful
	// activation — currently, an activation write that reported an error on a
	// user the server then found active. The ceremony succeeded and the invite
	// was consumed, so this is not a failure, but an operator should see it
	// rather than have it swallowed by the client.
	Warning string `json:"warning,omitempty"`
}

// InviteClient provides invite-based user activation operations. An invite
// token is scoped to the tenant that issued it, so every verb names the
// tenant as well; a token presented to another tenant is not found.
type InviteClient interface {
	// GetInvite returns publicly visible metadata for a pending invite.
	// This endpoint is unauthenticated; use it to populate the activation UI.
	GetInvite(ctx context.Context, tenantID, token string) (*InviteMetadata, error)

	// ActivateFIDO2Begin starts a WebAuthn credential creation ceremony for the
	// invite. Returns CredentialCreationOptions to be passed to
	// navigator.credentials.create() in the browser.
	ActivateFIDO2Begin(ctx context.Context, tenantID, token string) (map[string]interface{}, error)

	// ActivateFIDO2Finish completes the WebAuthn registration. Pass the parsed
	// PublicKeyCredential response from the browser.
	ActivateFIDO2Finish(ctx context.Context, tenantID, token string, credential map[string]interface{}) (*ActivationResponse, error)

	// ActivatePIV activates the user with a PIV X.509 certificate PEM block.
	// The certificate chain must be trusted by the server's configured root pool.
	ActivatePIV(ctx context.Context, tenantID, token, certPEM string) (*ActivationResponse, error)
}

// inviteRequest addresses an invite: tokens are scoped to the tenant that
// issued them, so every verb names both.
type inviteRequest struct {
	TenantID string `json:"tenant_id"`
	Token    string `json:"token"`
}

// activateFIDO2FinishRequest is the request for ActivateFIDO2Finish.
type activateFIDO2FinishRequest struct {
	TenantID   string                 `json:"tenant_id"`
	Token      string                 `json:"token"`
	Credential map[string]interface{} `json:"credential"`
}

// activatePIVRequest is the request for ActivatePIV.
type activatePIVRequest struct {
	TenantID string `json:"tenant_id"`
	Token    string `json:"token"`
	CertPEM  string `json:"cert_pem"`
}

// inviteClient is the transport-backed InviteClient implementation.
type inviteClient struct {
	transport transport.Transport
}

// newInviteClient creates a new inviteClient.
func newInviteClient(t transport.Transport) InviteClient {
	return &inviteClient{transport: t}
}

// validateInviteScope checks the tenant and token every verb requires.
func validateInviteScope(op, tenantID, token string) error {
	if tenantID == "" {
		return errors.NewInvalidInput(inviteServiceName+"."+op, "tenant ID is required")
	}
	if token == "" {
		return errors.NewInvalidInput(inviteServiceName+"."+op, "token is required")
	}
	return nil
}

// GetInvite returns metadata for the pending invite identified by token.
func (c *inviteClient) GetInvite(ctx context.Context, tenantID, token string) (*InviteMetadata, error) {
	if err := validateInviteScope("GetInvite", tenantID, token); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, inviteServiceName, "GetInvite", &inviteRequest{
		TenantID: tenantID,
		Token:    token,
	})
	if err != nil {
		return nil, err
	}

	return convertToInviteMetadata(resp)
}

// ActivateFIDO2Begin initiates the WebAuthn registration ceremony.
func (c *inviteClient) ActivateFIDO2Begin(ctx context.Context, tenantID, token string) (map[string]interface{}, error) {
	if err := validateInviteScope("ActivateFIDO2Begin", tenantID, token); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, inviteServiceName, "ActivateFIDO2Begin", &inviteRequest{
		TenantID: tenantID,
		Token:    token,
	})
	if err != nil {
		return nil, err
	}

	if options, ok := resp.(map[string]interface{}); ok {
		return options, nil
	}
	return nil, errors.NewInternal(inviteServiceName+".ActivateFIDO2Begin", "unexpected response type")
}

// ActivateFIDO2Finish completes the WebAuthn registration.
func (c *inviteClient) ActivateFIDO2Finish(ctx context.Context, tenantID, token string, credential map[string]interface{}) (*ActivationResponse, error) {
	if err := validateInviteScope("ActivateFIDO2Finish", tenantID, token); err != nil {
		return nil, err
	}
	if credential == nil {
		return nil, errors.NewInvalidInput(inviteServiceName+".ActivateFIDO2Finish", "credential is required")
	}

	resp, err := c.transport.Request(ctx, inviteServiceName, "ActivateFIDO2Finish", &activateFIDO2FinishRequest{
		TenantID:   tenantID,
		Token:      token,
		Credential: credential,
	})
	if err != nil {
		return nil, err
	}

	return convertToActivationResponse(resp)
}

// ActivatePIV activates the user via PIV certificate.
func (c *inviteClient) ActivatePIV(ctx context.Context, tenantID, token, certPEM string) (*ActivationResponse, error) {
	if err := validateInviteScope("ActivatePIV", tenantID, token); err != nil {
		return nil, err
	}
	if certPEM == "" {
		return nil, errors.NewInvalidInput(inviteServiceName+".ActivatePIV", "certPEM is required")
	}

	resp, err := c.transport.Request(ctx, inviteServiceName, "ActivatePIV", &activatePIVRequest{
		TenantID: tenantID,
		Token:    token,
		CertPEM:  certPEM,
	})
	if err != nil {
		return nil, err
	}

	return convertToActivationResponse(resp)
}

// convertToInviteMetadata converts a transport response to *InviteMetadata.
func convertToInviteMetadata(resp interface{}) (*InviteMetadata, error) {
	if resp == nil {
		return nil, errors.NewInternal(inviteServiceName+".GetInvite", "nil response")
	}

	if meta, ok := resp.(*InviteMetadata); ok {
		return meta, nil
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, errors.NewInternal(inviteServiceName+".GetInvite", "failed to marshal response: "+err.Error())
	}

	var meta InviteMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, errors.NewInternal(inviteServiceName+".GetInvite", "failed to decode response: "+err.Error())
	}

	return &meta, nil
}

// convertToActivationResponse converts a transport response to *ActivationResponse.
func convertToActivationResponse(resp interface{}) (*ActivationResponse, error) {
	if resp == nil {
		return nil, errors.NewInternal(inviteServiceName+".Activate", "nil response")
	}

	if ar, ok := resp.(*ActivationResponse); ok {
		return ar, nil
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return nil, errors.NewInternal(inviteServiceName+".Activate", "failed to marshal response: "+err.Error())
	}

	var ar ActivationResponse
	if err := json.Unmarshal(data, &ar); err != nil {
		return nil, errors.NewInternal(inviteServiceName+".Activate", "failed to decode response: "+err.Error())
	}

	return &ar, nil
}
