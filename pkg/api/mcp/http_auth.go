//go:build mcp

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

package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
)

const (
	// DefaultRealm is the default realm for WWW-Authenticate headers.
	DefaultRealm = "quicbaas-mcp"
)

// WWWAuthenticateError represents an OAuth error for WWW-Authenticate header.
type WWWAuthenticateError string

const (
	// WWWAuthenticateErrorInvalidToken indicates the token is invalid or expired.
	WWWAuthenticateErrorInvalidToken WWWAuthenticateError = "invalid_token"

	// WWWAuthenticateErrorInvalidRequest indicates the request is malformed.
	WWWAuthenticateErrorInvalidRequest WWWAuthenticateError = "invalid_request"

	// WWWAuthenticateErrorInsufficientScope indicates the token lacks required scope.
	WWWAuthenticateErrorInsufficientScope WWWAuthenticateError = "insufficient_scope"
)

// ExtractBearerToken extracts the Bearer token from the Authorization header.
// Returns empty string if no Bearer token is found.
func ExtractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}

	return parts[1]
}

// WriteWWWAuthenticate writes the WWW-Authenticate header per RFC 6750.
// This should be called on 401 and 403 responses.
// Per RFC 9728, includes resource_metadata parameter when a resource metadata URL is provided.
func WriteWWWAuthenticate(w http.ResponseWriter, realm string, authError WWWAuthenticateError, description string, scope string) {
	if realm == "" {
		realm = DefaultRealm
	}

	parts := []string{
		"realm=\"" + realm + "\"",
	}

	if authError != "" {
		parts = append(parts, "error=\""+string(authError)+"\"")
	}

	if description != "" {
		parts = append(parts, "error_description=\""+description+"\"")
	}

	if scope != "" {
		parts = append(parts, "scope=\""+scope+"\"")
	}

	w.Header().Set("WWW-Authenticate", "Bearer "+strings.Join(parts, ", "))
}

// WriteWWWAuthenticateWithResourceMetadata writes the WWW-Authenticate header per RFC 6750
// with the resource_metadata parameter per RFC 9728.
// The resourceMetadataURL is the URL of the /.well-known/oauth-protected-resource endpoint.
func WriteWWWAuthenticateWithResourceMetadata(w http.ResponseWriter, realm string, authError WWWAuthenticateError, description string, scope string, resourceMetadataURL string) {
	if realm == "" {
		realm = DefaultRealm
	}

	parts := []string{
		"realm=\"" + realm + "\"",
	}

	if resourceMetadataURL != "" {
		parts = append(parts, "resource_metadata=\""+resourceMetadataURL+"\"")
	}

	if authError != "" {
		parts = append(parts, "error=\""+string(authError)+"\"")
	}

	if description != "" {
		parts = append(parts, "error_description=\""+description+"\"")
	}

	if scope != "" {
		parts = append(parts, "scope=\""+scope+"\"")
	}

	w.Header().Set("WWW-Authenticate", "Bearer "+strings.Join(parts, ", "))
}

// HTTPAuthError represents an HTTP authentication/authorization error response.
type HTTPAuthError struct {
	StatusCode int                  `json:"-"`
	Error      WWWAuthenticateError `json:"error"`
	Message    string               `json:"error_description,omitempty"`
	Scope      string               `json:"scope,omitempty"`
}

// RespondAuthError writes an authentication error response with proper headers.
// For 401: missing or invalid token
// For 403: valid token but insufficient permissions.
func RespondAuthError(w http.ResponseWriter, r *http.Request, realm string, authErr *HTTPAuthError) {
	// Write WWW-Authenticate header
	WriteWWWAuthenticate(w, realm, authErr.Error, authErr.Message, authErr.Scope)

	// Write JSON-RPC error response
	var jsonRPCCode int
	switch authErr.StatusCode {
	case http.StatusUnauthorized:
		jsonRPCCode = AuthenticationRequired
		if authErr.Error == WWWAuthenticateErrorInvalidToken {
			jsonRPCCode = InvalidToken
		}
	case http.StatusForbidden:
		jsonRPCCode = PermissionDenied
	default:
		jsonRPCCode = InternalError
	}

	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		Error: &JSONRPCError{
			Code:    jsonRPCCode,
			Message: authErr.Message,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(authErr.StatusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

// RespondAuthErrorWithResourceMetadata writes an authentication error response with proper headers,
// including the resource_metadata parameter per RFC 9728.
func RespondAuthErrorWithResourceMetadata(w http.ResponseWriter, r *http.Request, realm string, authErr *HTTPAuthError, resourceMetadataURL string) {
	// Write WWW-Authenticate header with resource_metadata
	WriteWWWAuthenticateWithResourceMetadata(w, realm, authErr.Error, authErr.Message, authErr.Scope, resourceMetadataURL)

	// Write JSON-RPC error response
	var jsonRPCCode int
	switch authErr.StatusCode {
	case http.StatusUnauthorized:
		jsonRPCCode = AuthenticationRequired
		if authErr.Error == WWWAuthenticateErrorInvalidToken {
			jsonRPCCode = InvalidToken
		}
	case http.StatusForbidden:
		jsonRPCCode = PermissionDenied
	default:
		jsonRPCCode = InternalError
	}

	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		Error: &JSONRPCError{
			Code:    jsonRPCCode,
			Message: authErr.Message,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(authErr.StatusCode)
	_ = json.NewEncoder(w).Encode(resp)
}

// ErrorMissingToken creates an error for missing authentication.
func ErrorMissingToken() *HTTPAuthError {
	return &HTTPAuthError{
		StatusCode: http.StatusUnauthorized,
		Error:      WWWAuthenticateErrorInvalidRequest,
		Message:    "authentication required",
	}
}

// ErrorInvalidToken creates an error for invalid or expired tokens.
func ErrorInvalidToken(description string) *HTTPAuthError {
	if description == "" {
		description = "token is invalid or expired"
	}
	return &HTTPAuthError{
		StatusCode: http.StatusUnauthorized,
		Error:      WWWAuthenticateErrorInvalidToken,
		Message:    description,
	}
}

// ErrorInsufficientScope creates an error for insufficient permissions.
func ErrorInsufficientScope(requiredScope string) *HTTPAuthError {
	return &HTTPAuthError{
		StatusCode: http.StatusForbidden,
		Error:      WWWAuthenticateErrorInsufficientScope,
		Message:    "insufficient scope",
		Scope:      requiredScope,
	}
}

// ErrorPermissionDenied creates a generic permission denied error.
func ErrorPermissionDenied(message string) *HTTPAuthError {
	if message == "" {
		message = "permission denied"
	}
	return &HTTPAuthError{
		StatusCode: http.StatusForbidden,
		Error:      WWWAuthenticateErrorInsufficientScope,
		Message:    message,
	}
}
