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

package iam

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// webauthnServiceName is the service the credential administration verbs
// dispatch under. They are deliberately NOT part of the User service: they
// operate on the WebAuthn relying party's credential store, which is keyed by
// (tenant, email) and outlives the IAM user record — the reason an orphaned
// credential could block an email in the first place.
const webauthnServiceName = "quicbaas.IAM.WebAuthn"

// listWebAuthnCredentialsRequest is the request for ListWebAuthnCredentials.
type listWebAuthnCredentialsRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

// deleteWebAuthnCredentialRequest is the request for DeleteWebAuthnCredential.
type deleteWebAuthnCredentialRequest struct {
	TenantID     string `json:"tenant_id"`
	UserID       string `json:"user_id"`
	CredentialID string `json:"credential_id"`
}

// deleteWebAuthnCredentialsRequest is the request for
// DeleteWebAuthnCredentials.
type deleteWebAuthnCredentialsRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

// webAuthnCredentialsPayload is the list response envelope.
type webAuthnCredentialsPayload struct {
	Credentials []*cli.WebAuthnCredential `json:"credentials"`
	Total       int                       `json:"total"`
}

// ListWebAuthnCredentials returns the WebAuthn authenticators registered to a
// user. A user who has never registered a passkey has none, which is an empty
// list rather than an error.
func (c *client) ListWebAuthnCredentials(ctx context.Context, tenantID, userID string) ([]*cli.WebAuthnCredential, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.webauthn.ListWebAuthnCredentials", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.webauthn.ListWebAuthnCredentials", "user ID is required")
	}

	resp, err := c.transport.Request(ctx, webauthnServiceName, "ListWebAuthnCredentials", &listWebAuthnCredentialsRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return nil, err
	}

	if payload, ok := resp.(*webAuthnCredentialsPayload); ok {
		return payload.Credentials, nil
	}
	converted, err := convertMapResponse[webAuthnCredentialsPayload](resp, "iam.webauthn.ListWebAuthnCredentials")
	if err != nil {
		return nil, err
	}
	if converted.Credentials == nil {
		return []*cli.WebAuthnCredential{}, nil
	}
	return converted.Credentials, nil
}

// DeleteWebAuthnCredential removes one of a user's registered authenticators.
// credentialID is the base64url id ListWebAuthnCredentials reports.
func (c *client) DeleteWebAuthnCredential(ctx context.Context, tenantID, userID, credentialID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.webauthn.DeleteWebAuthnCredential", "tenant ID is required")
	}
	if userID == "" {
		return errors.NewInvalidInput("iam.webauthn.DeleteWebAuthnCredential", "user ID is required")
	}
	if credentialID == "" {
		return errors.NewInvalidInput("iam.webauthn.DeleteWebAuthnCredential", "credential ID is required")
	}

	_, err := c.transport.Request(ctx, webauthnServiceName, "DeleteWebAuthnCredential", &deleteWebAuthnCredentialRequest{
		TenantID:     tenantID,
		UserID:       userID,
		CredentialID: credentialID,
	})
	return err
}

// DeleteWebAuthnCredentials removes every authenticator registered to a user
// and reports how many were removed. It is the recovery verb for an account
// whose registration ceremony an authenticator refuses because it already holds
// a credential for that email.
func (c *client) DeleteWebAuthnCredentials(ctx context.Context, tenantID, userID string) (int, error) {
	if tenantID == "" {
		return 0, errors.NewInvalidInput("iam.webauthn.DeleteWebAuthnCredentials", "tenant ID is required")
	}
	if userID == "" {
		return 0, errors.NewInvalidInput("iam.webauthn.DeleteWebAuthnCredentials", "user ID is required")
	}

	resp, err := c.transport.Request(ctx, webauthnServiceName, "DeleteWebAuthnCredentials", &deleteWebAuthnCredentialsRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return 0, err
	}

	converted, cErr := convertMapResponse[struct {
		Deleted int `json:"deleted"`
	}](resp, "iam.webauthn.DeleteWebAuthnCredentials")
	if cErr != nil {
		// Reporting 0 here would be indistinguishable from "the account had
		// no credentials", which is the one answer an operator clearing a
		// stuck account must not be told wrongly. Surface the decode failure.
		return 0, cErr
	}
	return converted.Deleted, nil
}
