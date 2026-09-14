//go:build mcp

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

package mcp

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// WebAuthn credential administration tools
//
// The MCP face of the verbs REST serves under
// /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials. The
// credential-store access is not reimplemented here: like every other
// transport, these tools resolve through service.WebAuthnCredentialService so
// the IAM-user-id-to-email translation happens in exactly one place.
// =============================================================================

// webAuthnCredentialAccessor provides access to the WebAuthn credential
// administration service from the QuicBaaS service. It is satisfied when the
// node is built with the "iam" build tag.
type webAuthnCredentialAccessor interface {
	WebAuthnCredentials() service.WebAuthnCredentialService
}

// WebAuthnCredentialUnavailableError indicates the WebAuthn credential
// administration surface is not available on this node.
type WebAuthnCredentialUnavailableError struct {
	Reason string
}

// Error implements error.
func (e *WebAuthnCredentialUnavailableError) Error() string {
	if e == nil || e.Reason == "" {
		return "webauthn credential administration unavailable"
	}
	return "webauthn credential administration unavailable: " + e.Reason
}

// WebAuthnCredentialInvalidInputError indicates a missing or invalid tool
// argument.
type WebAuthnCredentialInvalidInputError struct {
	Field   string
	Message string
}

// Error implements error.
func (e *WebAuthnCredentialInvalidInputError) Error() string {
	if e == nil {
		return "invalid webauthn credential input"
	}
	if e.Message != "" {
		return e.Message
	}
	return "invalid webauthn credential input: " + e.Field
}

// getWebAuthnCredentials retrieves the credential administration service.
func (r *ToolRegistry) getWebAuthnCredentials() (service.WebAuthnCredentialService, error) {
	accessor, ok := r.service.(webAuthnCredentialAccessor)
	if !ok {
		return nil, &WebAuthnCredentialUnavailableError{Reason: "service does not support WebAuthn"}
	}
	svc := accessor.WebAuthnCredentials()
	if svc == nil {
		return nil, &WebAuthnCredentialUnavailableError{Reason: "WebAuthn is not configured"}
	}
	return svc, nil
}

// webAuthnCredentialScope pulls and validates the tenant/user scope every
// credential tool takes.
func webAuthnCredentialScope(args map[string]interface{}) (string, string, error) {
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return "", "", &WebAuthnCredentialInvalidInputError{Field: "tenant_id", Message: "tenant_id is required"}
	}
	userID := getString(args, "user_id")
	if userID == "" {
		return "", "", &WebAuthnCredentialInvalidInputError{Field: "user_id", Message: "user_id is required"}
	}
	return tenantID, userID, nil
}

// executeIAMWebAuthnCredentialsList lists a user's registered authenticators.
func (r *ToolRegistry) executeIAMWebAuthnCredentialsList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getWebAuthnCredentials()
	if err != nil {
		return nil, err
	}
	tenantID, userID, err := webAuthnCredentialScope(args)
	if err != nil {
		return nil, err
	}
	credentials, err := svc.ListCredentials(ctx, tenantID, userID)
	if err != nil {
		return nil, api.NewOperationFailedError("list webauthn credentials", err)
	}
	return marshalResult(map[string]interface{}{
		"credentials": credentials,
		"total":       len(credentials),
	})
}

// executeIAMWebAuthnCredentialDelete removes one authenticator by credential id.
func (r *ToolRegistry) executeIAMWebAuthnCredentialDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getWebAuthnCredentials()
	if err != nil {
		return nil, err
	}
	tenantID, userID, err := webAuthnCredentialScope(args)
	if err != nil {
		return nil, err
	}
	credentialID := getString(args, "credential_id")
	if credentialID == "" {
		return nil, &WebAuthnCredentialInvalidInputError{Field: "credential_id", Message: "credential_id is required"}
	}
	if dErr := svc.DeleteCredential(ctx, tenantID, userID, credentialID); dErr != nil {
		return nil, api.NewOperationFailedError("delete webauthn credential", dErr)
	}
	return marshalResult(map[string]interface{}{"success": true})
}

// executeIAMWebAuthnCredentialsDeleteAll removes every authenticator for a user.
func (r *ToolRegistry) executeIAMWebAuthnCredentialsDeleteAll(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getWebAuthnCredentials()
	if err != nil {
		return nil, err
	}
	tenantID, userID, err := webAuthnCredentialScope(args)
	if err != nil {
		return nil, err
	}
	deleted, err := svc.DeleteCredentials(ctx, tenantID, userID)
	if err != nil {
		return nil, api.NewOperationFailedError("delete webauthn credentials", err)
	}
	return marshalResult(map[string]interface{}{"deleted": deleted})
}

// registerIAMWebAuthnTools registers the credential administration tools.
func (r *ToolRegistry) registerIAMWebAuthnTools() {
	scope := map[string]interface{}{
		"tenant_id": map[string]interface{}{
			"type":        "string",
			"description": "Owning tenant ID",
		},
		"user_id": map[string]interface{}{
			"type":        "string",
			"description": "User identifier",
		},
	}

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_webauthn_credentials_list",
		Description: "List the WebAuthn authenticators (passkeys, security keys) registered to a user",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": scope,
			"required":   []string{"tenant_id", "user_id"},
		},
	})

	deleteProps := map[string]interface{}{
		"tenant_id": scope["tenant_id"],
		"user_id":   scope["user_id"],
		"credential_id": map[string]interface{}{
			"type":        "string",
			"description": "Base64url WebAuthn credential ID, as reported by the list tool",
		},
	}
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_webauthn_credential_delete",
		Description: "Delete one of a user's registered WebAuthn authenticators by credential ID",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": deleteProps,
			"required":   []string{"tenant_id", "user_id", "credential_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_webauthn_credentials_delete_all",
		Description: "Delete every WebAuthn authenticator registered to a user, so the account can register a passkey again",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": scope,
			"required":   []string{"tenant_id", "user_id"},
		},
	})
}
