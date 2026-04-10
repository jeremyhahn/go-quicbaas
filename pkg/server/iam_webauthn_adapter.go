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

//go:build iam

package server

import (
	"context"
	"encoding/json"

	"github.com/go-webauthn/webauthn/protocol"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// webauthnServiceAdapter adapts *iam.WebAuthnManager to service.WebAuthnManager.
// It converts between the typed protocol structures used by the IAM layer and
// the map[string]interface{} used by the REST API for JSON serialization.
type webauthnServiceAdapter struct {
	manager *iam.WebAuthnManager
}

// Compile-time check that webauthnServiceAdapter implements service.WebAuthnManager.
var _ service.WebAuthnManager = (*webauthnServiceAdapter)(nil)

// newWebAuthnServiceAdapter creates a new WebAuthn service adapter.
func newWebAuthnServiceAdapter(manager *iam.WebAuthnManager) *webauthnServiceAdapter {
	return &webauthnServiceAdapter{manager: manager}
}

// BeginRegistration initiates the WebAuthn credential registration process.
// It returns the credential creation options as a map for JSON serialization.
func (a *webauthnServiceAdapter) BeginRegistration(ctx context.Context, tenantID, userID string) (map[string]interface{}, error) {
	options, err := a.manager.BeginRegistration(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	// Convert the protocol.CredentialCreation to map[string]interface{}
	// by marshaling to JSON and unmarshaling to map
	data, err := json.Marshal(options)
	if err != nil {
		return nil, iam.NewIAMError(iam.ErrWebAuthnRegistrationFailed, "webauthn.adapter.begin_registration",
			"failed to marshal registration options", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, iam.NewIAMError(iam.ErrWebAuthnRegistrationFailed, "webauthn.adapter.begin_registration",
			"failed to unmarshal registration options", err)
	}

	return result, nil
}

// FinishRegistration completes the WebAuthn credential registration process.
// It parses the credential data from map[string]interface{} to the protocol structure.
func (a *webauthnServiceAdapter) FinishRegistration(ctx context.Context, tenantID, userID string, credential map[string]interface{}) error {
	// Parse the credential from map to protocol.ParsedCredentialCreationData
	parsedCredential, err := parseCredentialCreationResponse(credential)
	if err != nil {
		return iam.NewIAMError(iam.ErrWebAuthnRegistrationFailed, "webauthn.adapter.finish_registration",
			"failed to parse credential", err)
	}

	// Call the underlying manager with the parsed credential
	_, err = a.manager.FinishRegistration(ctx, tenantID, userID, parsedCredential)
	return err
}

// BeginLogin initiates the WebAuthn authentication process.
// It returns the assertion options as a map for JSON serialization.
func (a *webauthnServiceAdapter) BeginLogin(ctx context.Context, tenantID, email string) (map[string]interface{}, error) {
	options, err := a.manager.BeginLogin(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}

	// Convert the protocol.CredentialAssertion to map[string]interface{}
	data, err := json.Marshal(options)
	if err != nil {
		return nil, iam.NewIAMError(iam.ErrWebAuthnAuthenticationFailed, "webauthn.adapter.begin_login",
			"failed to marshal login options", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, iam.NewIAMError(iam.ErrWebAuthnAuthenticationFailed, "webauthn.adapter.begin_login",
			"failed to unmarshal login options", err)
	}

	return result, nil
}

// FinishLogin completes the WebAuthn authentication process.
// It parses the credential data from map[string]interface{} to the protocol structure.
func (a *webauthnServiceAdapter) FinishLogin(ctx context.Context, tenantID string, credential map[string]interface{}) (*iam.User, error) {
	// Parse the credential from map to protocol.ParsedCredentialAssertionData
	parsedCredential, err := parseCredentialAssertionResponse(credential)
	if err != nil {
		return nil, iam.NewIAMError(iam.ErrWebAuthnAuthenticationFailed, "webauthn.adapter.finish_login",
			"failed to parse credential", err)
	}

	// Call the underlying manager with the parsed credential
	return a.manager.FinishLogin(ctx, tenantID, parsedCredential)
}

// parseCredentialCreationResponse parses credential data from map[string]interface{}
// to *protocol.ParsedCredentialCreationData using the go-webauthn library.
//
// The credential data from the browser's WebAuthn API contains:
// - id: base64url-encoded credential ID
// - rawId: base64url-encoded raw credential ID
// - type: credential type (should be "public-key")
// - response: authenticator attestation response containing:
//   - clientDataJSON: base64url-encoded client data
//   - attestationObject: base64url-encoded attestation object
//   - transports: optional array of transport hints
//   - publicKey: optional base64url-encoded public key
//   - publicKeyAlgorithm: optional algorithm identifier
//   - authenticatorData: optional base64url-encoded authenticator data
func parseCredentialCreationResponse(credential map[string]interface{}) (*protocol.ParsedCredentialCreationData, error) {
	// Marshal the map back to JSON
	jsonData, err := json.Marshal(credential)
	if err != nil {
		return nil, err
	}

	// Use the go-webauthn library's built-in parsing function
	// ParseCredentialCreationResponseBytes handles all the base64url decoding
	// and structure validation
	return protocol.ParseCredentialCreationResponseBytes(jsonData)
}

// parseCredentialAssertionResponse parses credential data from map[string]interface{}
// to *protocol.ParsedCredentialAssertionData using the go-webauthn library.
//
// The credential data from the browser's WebAuthn API contains:
// - id: base64url-encoded credential ID
// - rawId: base64url-encoded raw credential ID
// - type: credential type (should be "public-key")
// - response: authenticator assertion response containing:
//   - clientDataJSON: base64url-encoded client data
//   - authenticatorData: base64url-encoded authenticator data
//   - signature: base64url-encoded signature
//   - userHandle: optional base64url-encoded user handle
func parseCredentialAssertionResponse(credential map[string]interface{}) (*protocol.ParsedCredentialAssertionData, error) {
	// Marshal the map back to JSON
	jsonData, err := json.Marshal(credential)
	if err != nil {
		return nil, err
	}

	// Use the go-webauthn library's built-in parsing function
	// ParseCredentialRequestResponseBytes handles all the base64url decoding
	// and structure validation
	return protocol.ParseCredentialRequestResponseBytes(jsonData)
}
