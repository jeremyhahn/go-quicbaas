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
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// RFC 7591 error codes for dynamic client registration.
const (
	// ErrorCodeInvalidRedirectURI indicates a redirect URI is invalid.
	ErrorCodeInvalidRedirectURI = "invalid_redirect_uri"

	// ErrorCodeInvalidClientMetadata indicates the request body is invalid.
	ErrorCodeInvalidClientMetadata = "invalid_client_metadata"

	// ErrorCodeInvalidSoftwareStatement indicates a software statement is invalid.
	ErrorCodeInvalidSoftwareStatement = "invalid_software_statement"
)

// ClientRegistrationRequest represents RFC 7591 client registration request.
type ClientRegistrationRequest struct {
	// RedirectURIs is the list of redirect URIs for the client (REQUIRED).
	RedirectURIs []string `json:"redirect_uris"`

	// TokenEndpointAuthMethod is the client authentication method for the token endpoint.
	// Default is "client_secret_basic".
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method,omitempty"`

	// GrantTypes is the list of grant types the client will use.
	// Default is ["authorization_code"].
	GrantTypes []string `json:"grant_types,omitempty"`

	// ResponseTypes is the list of response types the client will use.
	// Default is ["code"].
	ResponseTypes []string `json:"response_types,omitempty"`

	// ClientName is the human-readable name of the client.
	ClientName string `json:"client_name,omitempty"`

	// ClientURI is the URL of the client's home page.
	ClientURI string `json:"client_uri,omitempty"`

	// Scope is a space-separated string of the requested scopes.
	Scope string `json:"scope,omitempty"`
}

// ClientRegistrationResponse represents RFC 7591 client registration response.
type ClientRegistrationResponse struct {
	// ClientID is the unique identifier for the client (REQUIRED).
	ClientID string `json:"client_id"`

	// ClientSecret is the client secret (REQUIRED for confidential clients).
	ClientSecret string `json:"client_secret,omitempty"`

	// ClientIDIssuedAt is the time when the client ID was issued (OPTIONAL).
	ClientIDIssuedAt int64 `json:"client_id_issued_at"`

	// ClientSecretExpiresAt is the time when the client secret expires (REQUIRED if client_secret issued).
	// 0 means the secret does not expire.
	ClientSecretExpiresAt int64 `json:"client_secret_expires_at"`

	// RedirectURIs is the list of redirect URIs (echoed from request).
	RedirectURIs []string `json:"redirect_uris"`

	// TokenEndpointAuthMethod is the authentication method (echoed from request).
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`

	// GrantTypes is the list of grant types (echoed from request).
	GrantTypes []string `json:"grant_types"`

	// ResponseTypes is the list of response types (echoed from request).
	ResponseTypes []string `json:"response_types"`

	// ClientName is the client name (echoed from request).
	ClientName string `json:"client_name,omitempty"`

	// ClientURI is the client URI (echoed from request).
	ClientURI string `json:"client_uri,omitempty"`

	// Scope is the scope (echoed from request).
	Scope string `json:"scope,omitempty"`
}

// ClientRegistrationErrorResponse represents RFC 7591 error response for JSON serialization.
type ClientRegistrationErrorResponse struct {
	// Error is the error code (REQUIRED).
	Error string `json:"error"`

	// ErrorDescription is a human-readable description of the error (OPTIONAL).
	ErrorDescription string `json:"error_description,omitempty"`
}

// ClientRegistrationError represents typed client registration errors.
type ClientRegistrationError struct {
	Code        string
	Description string
}

// Error implements the error interface.
func (e *ClientRegistrationError) Error() string {
	if e.Description != "" {
		return e.Code + ": " + e.Description
	}
	return e.Code
}

// NewInvalidRedirectURIError creates a new invalid redirect URI error.
func NewInvalidRedirectURIError(description string) *ClientRegistrationError {
	return &ClientRegistrationError{
		Code:        ErrorCodeInvalidRedirectURI,
		Description: description,
	}
}

// NewInvalidClientMetadataError creates a new invalid client metadata error.
func NewInvalidClientMetadataError(description string) *ClientRegistrationError {
	return &ClientRegistrationError{
		Code:        ErrorCodeInvalidClientMetadata,
		Description: description,
	}
}

// AppRegistrar defines the interface for registering OAuth2 applications.
// This interface allows integration with the IAM AppManager.
type AppRegistrar interface {
	// RegisterClient registers a new OAuth2 client and returns the client credentials.
	RegisterClient(ctx context.Context, req *ClientRegistrationRequest) (*ClientRegistrationResponse, error)
}

// InMemoryClientStore provides an in-memory implementation of client storage.
// This is used when no external AppManager is configured.
type InMemoryClientStore struct {
	mu      sync.RWMutex
	clients map[string]*storedClient
}

// storedClient represents a client stored in memory.
type storedClient struct {
	ClientID     string
	SecretHash   string
	RegisteredAt time.Time
	Request      *ClientRegistrationRequest
}

// NewInMemoryClientStore creates a new in-memory client store.
func NewInMemoryClientStore() *InMemoryClientStore {
	return &InMemoryClientStore{
		clients: make(map[string]*storedClient),
	}
}

// RegisterClient registers a new client in the in-memory store.
func (s *InMemoryClientStore) RegisterClient(ctx context.Context, req *ClientRegistrationRequest) (*ClientRegistrationResponse, error) {
	// Validate redirect URIs
	if len(req.RedirectURIs) == 0 {
		return nil, NewInvalidClientMetadataError("redirect_uris is required")
	}

	for _, uri := range req.RedirectURIs {
		if err := ValidateRedirectURI(uri); err != nil {
			return nil, err
		}
	}

	// Generate client ID and secret
	clientID, err := generateSecureID("client_")
	if err != nil {
		return nil, NewInvalidClientMetadataError("failed to generate client ID")
	}

	clientSecret, secretHash, err := generateClientSecretWithHash()
	if err != nil {
		return nil, NewInvalidClientMetadataError("failed to generate client secret")
	}

	// Apply defaults
	tokenEndpointAuthMethod := req.TokenEndpointAuthMethod
	if tokenEndpointAuthMethod == "" {
		tokenEndpointAuthMethod = "client_secret_basic"
	}

	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code"}
	}

	responseTypes := req.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}

	now := time.Now()

	// Store client
	s.mu.Lock()
	s.clients[clientID] = &storedClient{
		ClientID:     clientID,
		SecretHash:   secretHash,
		RegisteredAt: now,
		Request:      req,
	}
	s.mu.Unlock()

	// Build response
	response := &ClientRegistrationResponse{
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		ClientIDIssuedAt:        now.Unix(),
		ClientSecretExpiresAt:   0, // Does not expire
		RedirectURIs:            req.RedirectURIs,
		TokenEndpointAuthMethod: tokenEndpointAuthMethod,
		GrantTypes:              grantTypes,
		ResponseTypes:           responseTypes,
		ClientName:              req.ClientName,
		ClientURI:               req.ClientURI,
		Scope:                   req.Scope,
	}

	return response, nil
}

// GetClient retrieves a client by ID.
func (s *InMemoryClientStore) GetClient(clientID string) (*storedClient, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	client, ok := s.clients[clientID]
	return client, ok
}

// ValidateRedirectURI validates a redirect URI according to RFC 7591/OAuth2 requirements.
// HTTPS is required except for localhost/127.0.0.1/::1.
// No wildcards are allowed (exact match only).
func ValidateRedirectURI(uri string) error {
	if uri == "" {
		return NewInvalidRedirectURIError("redirect URI cannot be empty")
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return NewInvalidRedirectURIError("invalid URI format: " + err.Error())
	}

	// Must have a scheme
	if parsed.Scheme == "" {
		return NewInvalidRedirectURIError("redirect URI must have a scheme")
	}

	// Check for wildcards (not allowed)
	if containsWildcard(uri) {
		return NewInvalidRedirectURIError("wildcards are not allowed in redirect URIs")
	}

	// HTTP is only allowed for localhost (development)
	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		if !isLocalhostAddress(host) {
			return NewInvalidRedirectURIError("HTTP scheme is only allowed for localhost, 127.0.0.1, or ::1")
		}
	} else if parsed.Scheme != "https" {
		// Only https and http (for localhost) are valid for web redirects
		// Allow custom schemes for native apps (e.g., myapp://)
		if !isValidCustomScheme(parsed.Scheme) {
			return NewInvalidRedirectURIError("redirect URI scheme must be HTTPS, HTTP (for localhost), or a valid custom scheme")
		}
	}

	return nil
}

// isLocalhostAddress checks if the host is a localhost address.
func isLocalhostAddress(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// containsWildcard checks if a URI contains wildcard characters.
func containsWildcard(uri string) bool {
	for _, char := range uri {
		if char == '*' {
			return true
		}
	}
	return false
}

// isValidCustomScheme checks if a scheme is a valid custom scheme for native apps.
// Custom schemes must start with a letter and contain only alphanumeric, '+', '-', or '.'.
func isValidCustomScheme(scheme string) bool {
	if len(scheme) == 0 {
		return false
	}

	// Must start with a letter (De Morgan's law applied)
	first := scheme[0]
	if (first < 'a' || first > 'z') && (first < 'A' || first > 'Z') {
		return false
	}

	// Rest must be alphanumeric, '+', '-', or '.'
	for i := 1; i < len(scheme); i++ {
		c := scheme[i]
		isLowerAlpha := c >= 'a' && c <= 'z'
		isUpperAlpha := c >= 'A' && c <= 'Z'
		isDigit := c >= '0' && c <= '9'
		isSpecial := c == '+' || c == '-' || c == '.'
		if !isLowerAlpha && !isUpperAlpha && !isDigit && !isSpecial {
			return false
		}
	}

	return true
}

// generateSecureID generates a secure random ID with the given prefix.
func generateSecureID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// generateClientSecretWithHash generates a client secret and its bcrypt hash.
func generateClientSecretWithHash() (plaintext, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}

	plaintext = "sk_" + base64.RawURLEncoding.EncodeToString(b)

	hashed, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}

	return plaintext, string(hashed), nil
}

// handleClientRegistration handles POST /oauth2/register - RFC 7591 Dynamic Client Registration.
func (s *Server) handleClientRegistration(w http.ResponseWriter, r *http.Request) {
	// Only POST is allowed
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Content-Type must be application/json
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" && contentType != "" {
		s.writeRegistrationError(w, http.StatusBadRequest, ErrorCodeInvalidClientMetadata, "Content-Type must be application/json")
		return
	}

	// Parse request body
	var req ClientRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeRegistrationError(w, http.StatusBadRequest, ErrorCodeInvalidClientMetadata, "invalid JSON: "+err.Error())
		return
	}

	// Validate redirect URIs are present
	if len(req.RedirectURIs) == 0 {
		s.writeRegistrationError(w, http.StatusBadRequest, ErrorCodeInvalidClientMetadata, "redirect_uris is required")
		return
	}

	// Validate each redirect URI
	for _, uri := range req.RedirectURIs {
		if err := ValidateRedirectURI(uri); err != nil {
			var regErr *ClientRegistrationError
			if errors.As(err, &regErr) {
				s.writeRegistrationError(w, http.StatusBadRequest, regErr.Code, regErr.Description)
			} else {
				s.writeRegistrationError(w, http.StatusBadRequest, ErrorCodeInvalidRedirectURI, err.Error())
			}
			return
		}
	}

	// Use AppManager if available, otherwise use in-memory store
	var response *ClientRegistrationResponse
	var err error

	if s.appRegistrar != nil {
		response, err = s.appRegistrar.RegisterClient(r.Context(), &req)
	} else if s.inMemoryClientStore != nil {
		response, err = s.inMemoryClientStore.RegisterClient(r.Context(), &req)
	} else {
		s.writeRegistrationError(w, http.StatusInternalServerError, ErrorCodeInvalidClientMetadata, "client registration not configured")
		return
	}

	if err != nil {
		var regErr *ClientRegistrationError
		if errors.As(err, &regErr) {
			s.writeRegistrationError(w, http.StatusBadRequest, regErr.Code, regErr.Description)
		} else {
			s.writeRegistrationError(w, http.StatusBadRequest, ErrorCodeInvalidClientMetadata, err.Error())
		}
		return
	}

	// Write success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.WithError(err).Error("Failed to write client registration response")
	}
}

// writeRegistrationError writes an RFC 7591 error response.
func (s *Server) writeRegistrationError(w http.ResponseWriter, statusCode int, errorCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	errResp := &ClientRegistrationErrorResponse{
		Error:            errorCode,
		ErrorDescription: description,
	}
	if err := json.NewEncoder(w).Encode(errResp); err != nil {
		s.logger.WithError(err).Error("Failed to write client registration error response")
	}
}
