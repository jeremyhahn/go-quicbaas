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
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// DynamicClientRequest represents an RFC 7591 Dynamic Client Registration request.
type DynamicClientRequest struct {
	// RedirectURIs is the list of allowed redirect URIs (required for authorization_code grant)
	RedirectURIs []string `json:"redirect_uris,omitempty"`

	// TokenEndpointAuthMethod is the authentication method at the token endpoint
	// Supported: "client_secret_basic", "client_secret_post", "none"
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method,omitempty"`

	// GrantTypes is the list of OAuth 2.0 grant types the client may use
	// Defaults to ["authorization_code"]
	GrantTypes []string `json:"grant_types,omitempty"`

	// ResponseTypes is the list of OAuth 2.0 response types the client may use
	// Defaults to ["code"]
	ResponseTypes []string `json:"response_types,omitempty"`

	// ClientName is a human-readable name for the client
	ClientName string `json:"client_name,omitempty"`

	// ClientURI is a URL of the home page of the client
	ClientURI string `json:"client_uri,omitempty"`

	// LogoURI is a URL that references a logo for the client
	LogoURI string `json:"logo_uri,omitempty"`

	// Scope is a space-separated list of scope values the client may request
	Scope string `json:"scope,omitempty"`

	// Contacts is an array of email addresses for responsible parties
	Contacts []string `json:"contacts,omitempty"`

	// TOSURI is a URL of the terms of service document
	TOSURI string `json:"tos_uri,omitempty"`

	// PolicyURI is a URL of the privacy policy document
	PolicyURI string `json:"policy_uri,omitempty"`

	// JWKSURI is a URL for the client's JSON Web Key Set document
	JWKSURI string `json:"jwks_uri,omitempty"`

	// JWKS is the client's JSON Web Key Set (if not using jwks_uri)
	JWKS json.RawMessage `json:"jwks,omitempty"`

	// SoftwareID is a unique identifier for the client software
	SoftwareID string `json:"software_id,omitempty"`

	// SoftwareVersion is the version of the client software
	SoftwareVersion string `json:"software_version,omitempty"`

	// SoftwareStatement is a JWT containing client metadata assertions
	SoftwareStatement string `json:"software_statement,omitempty"`
}

// DynamicClientResponse represents an RFC 7591 Dynamic Client Registration response.
type DynamicClientResponse struct {
	// ClientID is the unique client identifier issued by the server
	ClientID string `json:"client_id"`

	// ClientSecret is the client secret (only for confidential clients)
	ClientSecret string `json:"client_secret,omitempty"`

	// ClientIDIssuedAt is the Unix timestamp when the client_id was issued
	ClientIDIssuedAt int64 `json:"client_id_issued_at,omitempty"`

	// ClientSecretExpiresAt is the Unix timestamp when client_secret expires (0 = never)
	ClientSecretExpiresAt int64 `json:"client_secret_expires_at,omitempty"`

	// RegistrationAccessToken is a bearer token for accessing client configuration endpoint
	RegistrationAccessToken string `json:"registration_access_token,omitempty"`

	// RegistrationClientURI is the URL of the client configuration endpoint
	RegistrationClientURI string `json:"registration_client_uri,omitempty"`

	// Echo back the request parameters
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	Contacts                []string `json:"contacts,omitempty"`
	TOSURI                  string   `json:"tos_uri,omitempty"`
	PolicyURI               string   `json:"policy_uri,omitempty"`
	JWKSURI                 string   `json:"jwks_uri,omitempty"`
	SoftwareID              string   `json:"software_id,omitempty"`
	SoftwareVersion         string   `json:"software_version,omitempty"`
}

// DCRError represents an RFC 7591 Dynamic Client Registration error.
type DCRError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// DCR error codes per RFC 7591 Section 3.2.2.
const (
	DCRErrorInvalidRedirectURI          = "invalid_redirect_uri"
	DCRErrorInvalidClientMetadata       = "invalid_client_metadata"
	DCRErrorInvalidSoftwareStatement    = "invalid_software_statement"
	DCRErrorUnapprovedSoftwareStatement = "unapproved_software_statement"
)

// RegisterDynamicClient handles POST /oauth2/register for Dynamic Client Registration (RFC 7591).
func (h *IAMHandler) RegisterDynamicClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req DynamicClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondDCRError(w, http.StatusBadRequest, DCRErrorInvalidClientMetadata, "invalid JSON request body")
		return
	}

	// Validate required fields based on grant types
	if err := validateDCRRequest(&req); err != nil {
		respondDCRError(w, http.StatusBadRequest, err.code, err.description)
		return
	}

	// Get app manager from service
	appProvider, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, "dynamic registration not available")
		return
	}

	appManager, err := appProvider.GetAppManager(ctx)
	if err != nil {
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, "failed to get app manager")
		return
	}

	// Determine tenant ID (use default for DCR or extract from initial access token)
	tenantID := extractTenantFromRequest(r)
	if tenantID == "" {
		tenantID = "default"
	}

	// Build RegisterAppOptions from DCR request
	opts := iam.RegisterAppOptions{
		Name:          req.ClientName,
		RedirectURIs:  req.RedirectURIs,
		GrantTypes:    req.GrantTypes,
		ResponseTypes: req.ResponseTypes,
	}

	// Set defaults
	if opts.Name == "" {
		opts.Name = "Dynamic Client"
	}

	// Register the application
	app, err := appManager.RegisterApp(ctx, tenantID, opts)
	if err != nil {
		// Map IAM errors to DCR errors
		respondDCRError(w, http.StatusBadRequest, DCRErrorInvalidClientMetadata, err.Error())
		return
	}

	// Build response
	resp := &DynamicClientResponse{
		ClientID:              app.ClientID,
		ClientSecret:          app.ClientSecret, // Only time it's visible
		ClientIDIssuedAt:      app.Created.Unix(),
		ClientSecretExpiresAt: 0, // Never expires

		// Echo back accepted values
		RedirectURIs:            app.RedirectURIs,
		TokenEndpointAuthMethod: "client_secret_basic", // Default
		GrantTypes:              app.GrantTypes,
		ResponseTypes:           app.ResponseTypes,
		ClientName:              app.Name,
		ClientURI:               req.ClientURI,
		LogoURI:                 req.LogoURI,
		Scope:                   req.Scope,
		Contacts:                req.Contacts,
		TOSURI:                  req.TOSURI,
		PolicyURI:               req.PolicyURI,
		JWKSURI:                 req.JWKSURI,
		SoftwareID:              req.SoftwareID,
		SoftwareVersion:         req.SoftwareVersion,
	}

	// Log audit event
	h.logAuditEvent(ctx, tenantID, iam.AuditEventAppEvent, iam.AuditCategoryAdministration,
		"dynamic_client_registration", iam.AuditOutcomeSuccess,
		&iam.AuditResource{
			Type: "application",
			ID:   app.ClientID,
		},
		map[string]interface{}{
			"client_name": app.Name,
			"grant_types": app.GrantTypes,
		})

	// Return 201 Created with response
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// dcrValidationError represents a DCR validation error.
type dcrValidationError struct {
	code        string
	description string
}

// validateDCRRequest validates the DCR request according to RFC 7591.
func validateDCRRequest(req *DynamicClientRequest) *dcrValidationError {
	// Determine if redirect_uris are required
	requiresRedirectURIs := false
	for _, gt := range req.GrantTypes {
		if gt == "authorization_code" || gt == "implicit" {
			requiresRedirectURIs = true
			break
		}
	}

	// Default grant type is authorization_code, which requires redirect_uris
	if len(req.GrantTypes) == 0 {
		requiresRedirectURIs = true
	}

	// Validate redirect URIs
	if requiresRedirectURIs {
		if len(req.RedirectURIs) == 0 {
			return &dcrValidationError{
				code:        DCRErrorInvalidRedirectURI,
				description: "redirect_uris required for authorization_code or implicit grants",
			}
		}

		for _, uri := range req.RedirectURIs {
			if !isValidRedirectURI(uri) {
				return &dcrValidationError{
					code:        DCRErrorInvalidRedirectURI,
					description: "invalid redirect_uri: must be HTTPS or localhost HTTP",
				}
			}
		}
	}

	// Validate grant types
	for _, gt := range req.GrantTypes {
		if !isValidDCRGrantType(gt) {
			return &dcrValidationError{
				code:        DCRErrorInvalidClientMetadata,
				description: "unsupported grant_type: " + gt,
			}
		}
	}

	// Validate response types
	for _, rt := range req.ResponseTypes {
		if !isValidDCRResponseType(rt) {
			return &dcrValidationError{
				code:        DCRErrorInvalidClientMetadata,
				description: "unsupported response_type: " + rt,
			}
		}
	}

	// Validate token endpoint auth method
	if req.TokenEndpointAuthMethod != "" {
		validMethods := map[string]bool{
			"client_secret_basic": true,
			"client_secret_post":  true,
			"none":                true,
			"private_key_jwt":     true,
		}
		if !validMethods[req.TokenEndpointAuthMethod] {
			return &dcrValidationError{
				code:        DCRErrorInvalidClientMetadata,
				description: "unsupported token_endpoint_auth_method",
			}
		}
	}

	return nil
}

// isValidRedirectURI validates a redirect URI for DCR.
func isValidRedirectURI(uri string) bool {
	// Parse the URI
	if uri == "" {
		return false
	}

	// Check for HTTPS or localhost HTTP
	if len(uri) >= 8 && uri[:8] == "https://" {
		return true
	}
	if len(uri) >= 17 && uri[:17] == "http://localhost" {
		return true
	}
	if len(uri) >= 17 && uri[:17] == "http://127.0.0.1" {
		return true
	}
	if len(uri) >= 12 && uri[:12] == "http://[::1]" {
		return true
	}

	// Allow custom schemes for native apps (RFC 8252)
	// Format: com.example.app:/callback
	for i, c := range uri {
		if c == ':' {
			// Found scheme separator
			if i > 0 && len(uri) > i+1 {
				return true
			}
			break
		}
		// Scheme must be alphanumeric with dots/hyphens
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+') {
			break
		}
	}

	return false
}

// isValidDCRGrantType checks if a grant type is valid for DCR.
func isValidDCRGrantType(grantType string) bool {
	validGrantTypes := map[string]bool{
		"authorization_code": true,
		"client_credentials": true,
		"refresh_token":      true,
		"urn:ietf:params:oauth:grant-type:device_code": true,
	}
	return validGrantTypes[grantType]
}

// isValidDCRResponseType checks if a response type is valid for DCR.
func isValidDCRResponseType(responseType string) bool {
	validResponseTypes := map[string]bool{
		"code":     true,
		"token":    true,
		"id_token": true,
	}
	return validResponseTypes[responseType]
}

// extractTenantFromRequest extracts tenant ID from initial access token or header.
func extractTenantFromRequest(r *http.Request) string {
	// Check for X-Tenant-ID header
	if tenantID := r.Header.Get("X-Tenant-ID"); tenantID != "" {
		return tenantID
	}

	// Could also extract from initial access token claims if provided
	// For now, return empty to use default

	return ""
}

// respondDCRError writes a DCR error response.
func respondDCRError(w http.ResponseWriter, statusCode int, errorCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(&DCRError{
		Error:            errorCode,
		ErrorDescription: description,
	})
}

// validateRegistrationAccessToken validates the registration_access_token from the Authorization header.
// Per RFC 7592, the registration_access_token is required for GET/PUT/DELETE operations on the
// client configuration endpoint. This token must match the token issued during initial registration.
// Returns the client ID associated with the token, or an empty string and error if validation fails.
//
// Note: Full implementation requires AppManager to store and validate registration access tokens
// alongside client credentials. Currently, clients can be accessed by client_id only.
func (h *IAMHandler) validateRegistrationAccessToken(r *http.Request, clientID string) (bool, string) {
	// Extract Bearer token from Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false, "missing Authorization header"
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false, "invalid Authorization header format"
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return false, "empty registration_access_token"
	}

	// Registration access token validation requires:
	// 1. AppManager to store registration_access_token with each app
	// 2. Token to be cryptographically verified or looked up
	// 3. Token to be associated with the specific client_id
	//
	// For now, we accept any valid bearer token format.
	// Full implementation will be added when AppManager supports registration access tokens.
	// See RFC 7592 Section 2.1 for requirements.
	return true, ""
}

// GetDynamicClient handles GET /oauth2/register/{client_id} for reading client configuration.
// This is RFC 7592 (OAuth 2.0 Dynamic Client Registration Management Protocol).
func (h *IAMHandler) GetDynamicClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract client_id from path
	clientID := chi.URLParam(r, "client_id")
	if clientID == "" {
		respondDCRError(w, http.StatusBadRequest, DCRErrorInvalidClientMetadata, "client_id required")
		return
	}

	// Validate registration_access_token from Authorization header (RFC 7592)
	// The token must be associated with this specific client_id
	if valid, errMsg := h.validateRegistrationAccessToken(r, clientID); !valid {
		respondDCRError(w, http.StatusUnauthorized, "invalid_token", errMsg)
		return
	}

	// Get app manager from service
	appProvider, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, "client management not available")
		return
	}

	appManager, err := appProvider.GetAppManager(ctx)
	if err != nil {
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, "failed to get app manager")
		return
	}

	// Get the application
	app, err := appManager.GetApp(ctx, clientID)
	if err != nil {
		respondDCRError(w, http.StatusNotFound, DCRErrorInvalidClientMetadata, "client not found")
		return
	}

	// Build response (without secret)
	resp := &DynamicClientResponse{
		ClientID:                app.ClientID,
		ClientIDIssuedAt:        app.Created.Unix(),
		ClientSecretExpiresAt:   0,
		RedirectURIs:            app.RedirectURIs,
		TokenEndpointAuthMethod: "client_secret_basic",
		GrantTypes:              app.GrantTypes,
		ResponseTypes:           app.ResponseTypes,
		ClientName:              app.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	_ = json.NewEncoder(w).Encode(resp)
}

// UpdateDynamicClient handles PUT /oauth2/register/{client_id} for updating client configuration.
func (h *IAMHandler) UpdateDynamicClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract client_id from path
	clientID := chi.URLParam(r, "client_id")
	if clientID == "" {
		respondDCRError(w, http.StatusBadRequest, DCRErrorInvalidClientMetadata, "client_id required")
		return
	}

	// Parse request body
	var req DynamicClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondDCRError(w, http.StatusBadRequest, DCRErrorInvalidClientMetadata, "invalid JSON request body")
		return
	}

	// Validate request
	if err := validateDCRRequest(&req); err != nil {
		respondDCRError(w, http.StatusBadRequest, err.code, err.description)
		return
	}

	// Validate registration_access_token from Authorization header (RFC 7592)
	// The token must be associated with this specific client_id
	if valid, errMsg := h.validateRegistrationAccessToken(r, clientID); !valid {
		respondDCRError(w, http.StatusUnauthorized, "invalid_token", errMsg)
		return
	}

	// Get app manager from service
	appProvider, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, "client management not available")
		return
	}

	appManager, err := appProvider.GetAppManager(ctx)
	if err != nil {
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, "failed to get app manager")
		return
	}

	// Build update options
	opts := iam.UpdateAppOptions{}
	if req.ClientName != "" {
		opts.Name = &req.ClientName
	}
	if len(req.RedirectURIs) > 0 {
		opts.RedirectURIs = &req.RedirectURIs
	}
	if len(req.GrantTypes) > 0 {
		opts.GrantTypes = &req.GrantTypes
	}
	if len(req.ResponseTypes) > 0 {
		opts.ResponseTypes = &req.ResponseTypes
	}

	// Update the application
	app, err := appManager.UpdateApp(ctx, clientID, opts)
	if err != nil {
		respondDCRError(w, http.StatusBadRequest, DCRErrorInvalidClientMetadata, err.Error())
		return
	}

	// Build response (without secret)
	resp := &DynamicClientResponse{
		ClientID:                app.ClientID,
		ClientIDIssuedAt:        app.Created.Unix(),
		ClientSecretExpiresAt:   0,
		RedirectURIs:            app.RedirectURIs,
		TokenEndpointAuthMethod: "client_secret_basic",
		GrantTypes:              app.GrantTypes,
		ResponseTypes:           app.ResponseTypes,
		ClientName:              app.Name,
		ClientURI:               req.ClientURI,
		LogoURI:                 req.LogoURI,
		Scope:                   req.Scope,
		Contacts:                req.Contacts,
		TOSURI:                  req.TOSURI,
		PolicyURI:               req.PolicyURI,
		JWKSURI:                 req.JWKSURI,
		SoftwareID:              req.SoftwareID,
		SoftwareVersion:         req.SoftwareVersion,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	_ = json.NewEncoder(w).Encode(resp)
}

// DeleteDynamicClient handles DELETE /oauth2/register/{client_id} for deleting a client.
func (h *IAMHandler) DeleteDynamicClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract client_id from path
	clientID := chi.URLParam(r, "client_id")
	if clientID == "" {
		respondDCRError(w, http.StatusBadRequest, DCRErrorInvalidClientMetadata, "client_id required")
		return
	}

	// Validate registration_access_token from Authorization header (RFC 7592)
	// The token must be associated with this specific client_id
	if valid, errMsg := h.validateRegistrationAccessToken(r, clientID); !valid {
		respondDCRError(w, http.StatusUnauthorized, "invalid_token", errMsg)
		return
	}

	// Get app manager from service
	appProvider, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, "client management not available")
		return
	}

	appManager, err := appProvider.GetAppManager(ctx)
	if err != nil {
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, "failed to get app manager")
		return
	}

	// Delete the application
	if err := appManager.DeleteApp(ctx, clientID); err != nil {
		respondDCRError(w, http.StatusNotFound, DCRErrorInvalidClientMetadata, "client not found")
		return
	}

	// Return 204 No Content
	w.WriteHeader(http.StatusNoContent)
}
