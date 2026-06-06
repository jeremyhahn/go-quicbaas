//go:build rest && iam

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
)

// Sentinel errors for the IAM REST API.
var (
	// ErrFederationManagerNotAvailable indicates the federation manager is not
	// available on the current service implementation.
	ErrFederationManagerNotAvailable = errors.New("federation manager not available")

	// ErrPolicyParse indicates a policy document could not be parsed.
	ErrPolicyParse = errors.New("policy parse failed")
)

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
