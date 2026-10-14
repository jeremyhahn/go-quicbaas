//go:build rest

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

package rest

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// Sentinel errors for the IAM REST API.
var (
	// ErrFederationManagerNotAvailable indicates the federation manager is not
	// available on the current service implementation.
	ErrFederationManagerNotAvailable = errors.New("federation manager not available")

	// ErrPolicyParse indicates a policy document could not be parsed.
	ErrPolicyParse = errors.New("policy parse failed")

	// ErrLoginRejected is the single sentinel every identity-resolution
	// failure in a login ceremony collapses onto.
	ErrLoginRejected = errors.New("authentication failed")
)

// LoginRejectedError is the ONLY failure an unauthenticated caller ever sees
// from a login ceremony once the request body has validated.
//
// Every identity-dependent outcome — unknown tenant, unknown user, user with no
// registered authenticator, rejected assertion — collapses onto this one value
// so the response cannot be used as an oracle. Distinguishable statuses or
// messages would let an anonymous caller enumerate tenant namespaces and user
// directories one request at a time, which is exactly the disclosure that the
// deletion of the public tenant-listing endpoint closed off. The real reason is
// recorded server-side in the audit log, never in the HTTP response.
type LoginRejectedError struct{}

// Error implements the error interface.
func (e *LoginRejectedError) Error() string {
	return "iam.login: authentication failed"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *LoginRejectedError) Unwrap() error {
	return ErrLoginRejected
}

// loginRejectedDescription is the fixed, identity-independent description sent
// to the caller. It is a constant so no code path can widen it.
const loginRejectedDescription = "authentication failed"

// RespondLoginRejected writes the uniform login failure. Callers MUST use this
// instead of handleServiceError for any error raised after a login ceremony
// starts resolving the caller's identity.
func RespondLoginRejected(w http.ResponseWriter, r *http.Request) {
	RespondIAMError(w, r, http.StatusUnauthorized,
		NewIAMAPIError(ErrUnauthorized, loginRejectedDescription))
}

// OAuthError represents an OAuth2/OIDC error response following RFC 6749.
type OAuthError struct {
	// Code is the error code
	Code string `json:"error"`

	// ErrorDescription provides additional human-readable information
	ErrorDescription string `json:"error_description,omitempty"`

	// ErrorURI is a URI to documentation about the error
	ErrorURI string `json:"error_uri,omitempty"`

	// State echoes the state parameter from the request
	State string `json:"state,omitempty"`
}

// OAuth2 error codes as defined in RFC 6749.
const (
	ErrInvalidRequest          = "invalid_request"
	ErrInvalidClient           = "invalid_client"
	ErrInvalidGrant            = "invalid_grant"
	ErrUnauthorizedClient      = "unauthorized_client"
	ErrUnsupportedGrantType    = "unsupported_grant_type"
	ErrInvalidScope            = "invalid_scope"
	ErrAccessDenied            = "access_denied"
	ErrUnsupportedResponseType = "unsupported_response_type"
	ErrServerError             = "server_error"
	ErrTemporarilyUnavailable  = "temporarily_unavailable"
)

// IAM API error codes.
const (
	ErrBadRequest            = "bad_request"
	ErrUnauthorized          = "unauthorized"
	ErrForbidden             = "forbidden"
	ErrNotFound              = "not_found"
	ErrConflict              = "conflict"
	ErrRateLimited           = "rate_limited"
	ErrInternalError         = "internal_error"
	ErrPIVLoginDisabled      = "piv_login_disabled"
	ErrClientCertRequired    = "client_cert_required"
	ErrPIVCertIssuerRejected = "piv_cert_issuer_rejected"
	ErrAAGUIDNotAllowed      = "aaguid_not_allowed"
)

// Client-facing messages for failures whose internal detail must not travel.
//
// An attestation verifier's errors describe the SERVER — the configured
// trust-bundle path, the parser's complaint about it, the subjects and issuers
// in the trust store — and the surfaces that run one are reachable by anyone
// who can open a connection. The detail belongs in the audit sink, where an
// operator can act on it; the response gets a stable sentence and the same
// error code, so a client's branching is unaffected while the free-text stops
// describing the deployment.
const (
	// ErrMsgPIVAttestationFailed is the response text for any PIV attestation
	// failure, whether the verifier could not be built or the presented chain
	// did not verify. The two are deliberately indistinguishable on the wire:
	// telling them apart is what lets a caller probe configuration.
	ErrMsgPIVAttestationFailed = "PIV attestation verification failed"

	// ErrMsgWebAuthnRegistrationRejected is the response text for a WebAuthn
	// registration this server would not process.
	ErrMsgWebAuthnRegistrationRejected = "the registration request could not be processed"
)

// Error implements the error interface.
func (e *OAuthError) Error() string {
	if e.ErrorDescription != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.ErrorDescription)
	}
	return e.Code
}

// RespondOAuthError writes an OAuth2 error response.
func RespondOAuthError(w http.ResponseWriter, r *http.Request, statusCode int, err *OAuthError) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	RespondJSON(w, r, statusCode, err)
}

// NewOAuthError creates a new OAuth error with the specified code and description.
func NewOAuthError(code, description string) *OAuthError {
	return &OAuthError{
		Code:             code,
		ErrorDescription: description,
		ErrorURI:         fmt.Sprintf("https://docs.quicbaas.io/iam/errors#%s", code),
	}
}

// NewInvalidRequestError creates an invalid_request OAuth error.
func NewInvalidRequestError(description string) *OAuthError {
	return NewOAuthError(ErrInvalidRequest, description)
}

// NewInvalidClientError creates an invalid_client OAuth error.
func NewInvalidClientError(description string) *OAuthError {
	return NewOAuthError(ErrInvalidClient, description)
}

// NewInvalidGrantError creates an invalid_grant OAuth error.
func NewInvalidGrantError(description string) *OAuthError {
	return NewOAuthError(ErrInvalidGrant, description)
}

// NewUnauthorizedClientError creates an unauthorized_client OAuth error.
func NewUnauthorizedClientError(description string) *OAuthError {
	return NewOAuthError(ErrUnauthorizedClient, description)
}

// NewUnsupportedGrantTypeError creates an unsupported_grant_type OAuth error.
func NewUnsupportedGrantTypeError(description string) *OAuthError {
	return NewOAuthError(ErrUnsupportedGrantType, description)
}

// NewInvalidScopeError creates an invalid_scope OAuth error.
func NewInvalidScopeError(description string) *OAuthError {
	return NewOAuthError(ErrInvalidScope, description)
}

// NewAccessDeniedError creates an access_denied OAuth error.
func NewAccessDeniedError(description string) *OAuthError {
	return NewOAuthError(ErrAccessDenied, description)
}

// NewUnsupportedResponseTypeError creates an unsupported_response_type OAuth error.
func NewUnsupportedResponseTypeError(description string) *OAuthError {
	return NewOAuthError(ErrUnsupportedResponseType, description)
}

// IAMAPIError represents a standard IAM API error response.
type IAMAPIError struct {
	// Error is the error code
	Error string `json:"error"`

	// ErrorDescription provides additional human-readable information
	ErrorDescription string `json:"error_description"`

	// ErrorURI is a URI to documentation about the error
	ErrorURI string `json:"error_uri,omitempty"`
}

// NewIAMAPIError creates a new IAM API error.
func NewIAMAPIError(code, description string) *IAMAPIError {
	return &IAMAPIError{
		Error:            code,
		ErrorDescription: description,
		ErrorURI:         fmt.Sprintf("https://docs.quicbaas.io/iam/errors#%s", code),
	}
}

// RespondIAMError writes an IAM API error response.
func RespondIAMError(w http.ResponseWriter, r *http.Request, statusCode int, err *IAMAPIError) {
	RespondJSON(w, r, statusCode, err)
}

// FederationManagerNotAvailableError indicates the federation manager is not
// available on the current service implementation.
type FederationManagerNotAvailableError struct{}

// Error implements the error interface.
func (e *FederationManagerNotAvailableError) Error() string {
	return "iam.federation: federation manager not available"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *FederationManagerNotAvailableError) Unwrap() error {
	return ErrFederationManagerNotAvailable
}

// PolicyParseError indicates a policy document could not be parsed from the
// request body. It wraps the underlying parsing error with a descriptive reason.
type PolicyParseError struct {
	Reason string
	Cause  error
}

// Error implements the error interface.
func (e *PolicyParseError) Error() string {
	if e.Cause != nil {
		return "iam.policy.parse: " + e.Reason + ": " + e.Cause.Error()
	}
	return "iam.policy.parse: " + e.Reason
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *PolicyParseError) Unwrap() error {
	return ErrPolicyParse
}

// UserIDParamError reports a user_id path or body value that is not a
// non-zero decimal uint64 user id.
type UserIDParamError struct {
	Value string
	Err   error
}

// Error implements the error interface.
func (e *UserIDParamError) Error() string {
	if e.Err != nil {
		return "iam.user_id: invalid user id " + strconv.Quote(e.Value) + ": " + e.Err.Error()
	}
	return "iam.user_id: invalid user id " + strconv.Quote(e.Value)
}

// Unwrap returns the underlying parse error, if any.
func (e *UserIDParamError) Unwrap() error {
	return e.Err
}

// parseUserIDParam parses a user id from its decimal wire form. Zero is
// refused because it means "not created yet".
func parseUserIDParam(value string) (uint64, error) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, &UserIDParamError{Value: value, Err: err}
	}
	if id == 0 {
		return 0, &UserIDParamError{Value: value}
	}
	return id, nil
}

// requireUserIDParam reads the user_id URL parameter and parses it. On a
// missing or invalid value it writes a 400 response and returns false.
func requireUserIDParam(w http.ResponseWriter, r *http.Request) (uint64, bool) {
	value := chi.URLParam(r, "user_id")
	if value == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return 0, false
	}
	id, err := parseUserIDParam(value)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, err.Error()))
		return 0, false
	}
	return id, true
}
