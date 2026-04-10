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

// Package service provides the unified service facade for QuicBaaS.
package service

import "context"

// =============================================================================
// FIDO2 Service - WebAuthn Registration and Authentication
// =============================================================================

// FIDO2Service handles FIDO2/WebAuthn registration and authentication flows.
// This service implements the server-side components of the WebAuthn protocol
// for passwordless authentication using hardware security keys, biometrics,
// and platform authenticators.
//
// The FIDO2 flow consists of two phases:
//
// Registration (credential creation):
//  1. BeginRegistration: Server generates challenge and options
//  2. Client creates credential via navigator.credentials.create()
//  3. FinishRegistration: Server validates and stores the credential
//
// Authentication (assertion):
//  1. BeginAuthentication: Server generates challenge with allowed credentials
//  2. Client signs challenge via navigator.credentials.get()
//  3. FinishAuthentication: Server verifies signature and issues session token
//
// All operations delegate to the underlying go-xkms FIDO2Service, with
// QuicBaaS-specific tenant context injection and access control enforcement.
type FIDO2Service interface {
	// BeginRegistration initiates a WebAuthn registration ceremony.
	// Returns a challenge and credential creation options that the client
	// passes to navigator.credentials.create().
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing username, display name, and relying party info
	// Returns:
	//   - FIDO2BeginRegistrationResponse: Contains challenge, user ID, RP info, and allowed algorithms
	//   - error: ErrInvalidInput, ErrUnavailable, ErrUnauthorized, ErrTimeout
	BeginRegistration(ctx context.Context, req *FIDO2BeginRegistrationRequest) (*FIDO2BeginRegistrationResponse, error)

	// FinishRegistration completes a WebAuthn registration ceremony.
	// Validates the authenticator response and stores the new credential.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing authenticator response data (credential ID, public key, attestation)
	// Returns:
	//   - FIDO2FinishRegistrationResponse: Contains success status and credential ID
	//   - error: ErrInvalidInput, ErrUnavailable, ErrUnauthorized, ErrTimeout
	FinishRegistration(ctx context.Context, req *FIDO2FinishRegistrationRequest) (*FIDO2FinishRegistrationResponse, error)

	// BeginAuthentication initiates a WebAuthn authentication ceremony.
	// Returns a challenge and assertion options that the client passes
	// to navigator.credentials.get().
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing username and relying party ID
	// Returns:
	//   - FIDO2BeginAuthenticationResponse: Contains challenge, RP ID, and allowed credential IDs
	//   - error: ErrInvalidInput, ErrNotFound, ErrUnavailable, ErrTimeout
	BeginAuthentication(ctx context.Context, req *FIDO2BeginAuthenticationRequest) (*FIDO2BeginAuthenticationResponse, error)

	// FinishAuthentication completes a WebAuthn authentication ceremony.
	// Verifies the authenticator signature and issues a session token.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing authenticator assertion data (credential ID, signature, authenticator data)
	// Returns:
	//   - FIDO2FinishAuthenticationResponse: Contains success status and JWT session token
	//   - error: ErrInvalidInput, ErrNotFound, ErrUnauthorized, ErrUnavailable, ErrTimeout
	FinishAuthentication(ctx context.Context, req *FIDO2FinishAuthenticationRequest) (*FIDO2FinishAuthenticationResponse, error)
}

// =============================================================================
// FIDO2 Service Request/Response Types
// =============================================================================

// FIDO2BeginRegistrationRequest contains parameters for starting a WebAuthn registration.
type FIDO2BeginRegistrationRequest struct {
	// Username is the account username to register the credential for.
	Username string

	// DisplayName is the human-readable name shown during registration.
	DisplayName string

	// RPID is the Relying Party identifier (typically the domain name).
	RPID string

	// RPName is the human-readable Relying Party name.
	RPName string
}

// FIDO2CredentialParam represents a WebAuthn credential type and algorithm pair.
type FIDO2CredentialParam struct {
	// Type is the credential type (e.g., "public-key").
	Type string

	// Alg is the COSE algorithm identifier (e.g., -7 for ES256, -257 for RS256).
	Alg int
}

// FIDO2BeginRegistrationResponse contains the challenge and options for registration.
type FIDO2BeginRegistrationResponse struct {
	// Challenge is the base64-encoded random challenge for the authenticator.
	Challenge string

	// UserID is the base64-encoded unique user identifier.
	UserID string

	// RPID is the Relying Party identifier.
	RPID string

	// RPName is the Relying Party display name.
	RPName string

	// CredentialParams lists the allowed credential type and algorithm pairs.
	CredentialParams []FIDO2CredentialParam
}

// FIDO2FinishRegistrationRequest contains the authenticator response to complete registration.
type FIDO2FinishRegistrationRequest struct {
	// Username is the account username the credential belongs to.
	Username string

	// CredentialID is the base64-encoded credential identifier from the authenticator.
	CredentialID string

	// PublicKey is the base64-encoded COSE public key from the authenticator.
	PublicKey string

	// AttestationData is the base64-encoded attestation object from the authenticator.
	AttestationData string

	// ClientDataJSON is the base64-encoded client data JSON from the browser.
	ClientDataJSON string
}

// FIDO2FinishRegistrationResponse contains the result of credential registration.
type FIDO2FinishRegistrationResponse struct {
	// Success indicates whether the registration was completed successfully.
	Success bool

	// CredentialID is the identifier of the newly registered credential.
	CredentialID string

	// Message provides additional context about the registration result.
	Message string
}

// FIDO2BeginAuthenticationRequest contains parameters for starting a WebAuthn authentication.
type FIDO2BeginAuthenticationRequest struct {
	// Username is the account username to authenticate.
	Username string

	// RPID is the Relying Party identifier (typically the domain name).
	RPID string
}

// FIDO2BeginAuthenticationResponse contains the challenge and options for authentication.
type FIDO2BeginAuthenticationResponse struct {
	// Challenge is the base64-encoded random challenge for the authenticator.
	Challenge string

	// RPID is the Relying Party identifier.
	RPID string

	// CredentialIDs is a list of base64-encoded credential IDs allowed for authentication.
	CredentialIDs []string
}

// FIDO2FinishAuthenticationRequest contains the authenticator assertion to complete authentication.
type FIDO2FinishAuthenticationRequest struct {
	// Username is the account username being authenticated.
	Username string

	// CredentialID is the base64-encoded credential identifier used for signing.
	CredentialID string

	// Signature is the base64-encoded authenticator signature over the challenge.
	Signature string

	// AuthenticatorData is the base64-encoded authenticator data from the assertion.
	AuthenticatorData string

	// ClientDataJSON is the base64-encoded client data JSON from the browser.
	ClientDataJSON string
}

// FIDO2FinishAuthenticationResponse contains the result of authentication.
type FIDO2FinishAuthenticationResponse struct {
	// Success indicates whether authentication was completed successfully.
	Success bool

	// Token is the JWT session token issued upon successful authentication.
	Token string

	// Message provides additional context about the authentication result.
	Message string
}

// =============================================================================
// FIDO2 Service Error Types
// =============================================================================

// FIDO2NotAvailableError indicates that the FIDO2 service is not configured
// or the underlying backend does not support WebAuthn operations.
type FIDO2NotAvailableError struct{}

// Error implements the error interface.
func (e *FIDO2NotAvailableError) Error() string {
	return "FIDO2 service not available"
}

// Is supports errors.Is matching by type.
func (e *FIDO2NotAvailableError) Is(target error) bool {
	_, ok := target.(*FIDO2NotAvailableError)
	return ok
}

// FIDO2RegistrationError indicates that a WebAuthn registration ceremony failed.
type FIDO2RegistrationError struct {
	Username string
	Cause    error
}

// Error implements the error interface.
func (e *FIDO2RegistrationError) Error() string {
	return "FIDO2 registration failed for user " + e.Username + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *FIDO2RegistrationError) Unwrap() error {
	return e.Cause
}

// FIDO2AuthenticationError indicates that a WebAuthn authentication ceremony failed.
type FIDO2AuthenticationError struct {
	Username string
	Cause    error
}

// Error implements the error interface.
func (e *FIDO2AuthenticationError) Error() string {
	return "FIDO2 authentication failed for user " + e.Username + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *FIDO2AuthenticationError) Unwrap() error {
	return e.Cause
}

// FIDO2CredentialNotFoundError indicates that no credentials were found for the user.
type FIDO2CredentialNotFoundError struct {
	Username string
}

// Error implements the error interface.
func (e *FIDO2CredentialNotFoundError) Error() string {
	return "no FIDO2 credentials found for user: " + e.Username
}

// Is supports errors.Is matching by type.
func (e *FIDO2CredentialNotFoundError) Is(target error) bool {
	_, ok := target.(*FIDO2CredentialNotFoundError)
	return ok
}

// FIDO2InvalidChallengeError indicates that the challenge verification failed
// during a registration or authentication ceremony.
type FIDO2InvalidChallengeError struct{}

// Error implements the error interface.
func (e *FIDO2InvalidChallengeError) Error() string {
	return "FIDO2 challenge verification failed"
}

// Is supports errors.Is matching by type.
func (e *FIDO2InvalidChallengeError) Is(target error) bool {
	_, ok := target.(*FIDO2InvalidChallengeError)
	return ok
}
