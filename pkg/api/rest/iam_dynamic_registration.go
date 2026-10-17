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
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// OAuth 2.0 Dynamic Client Registration (RFC 7591) and its management protocol
// (RFC 7592).
//
// AUTHENTICATION MODEL — the two layers and how they compose:
//
//	POST /oauth2/register (create) is a PRIVILEGED operation: it mints an
//	OAuth2 client, which is an authentication principal. It is therefore
//	gated by the standard REST authz chain (useAPIAuthzMiddleware, installed
//	on the route group in iam.go) and PEP-mapped to tenants:create. The
//	presented platform token IS the RFC 7591 "initial access token", and the
//	tenant it authenticated into is the tenant the new client lands in — a
//	caller can never choose someone else's tenant. Registration is also
//	config-gated off by default (config.IAMDynamicClientRegistrationConfig).
//
//	GET/PUT/DELETE /oauth2/register/{client_id} (the RFC 7592 client
//	configuration endpoint) authenticate SOLELY with the registration access
//	token issued for that exact client, per RFC 7592 §2. A registrant holds no
//	platform token — it holds the per-client credential — so these three paths
//	are a deliberate pre-auth carve-out declared in preAuthRouteTemplates
//	(authz_public_routes.go), which every enforcement posture honors. The
//	carve-out is safe ONLY because the registration access token is a real,
//	fail-closed gate: see iam.AppManager.AuthorizeRegistrationAccess.
//
// The carve-out is whole-path and segment-exact, so it exempts exactly the
// three management verbs and never the sibling create route.
// =============================================================================

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

	// RegistrationAccessToken is the bearer token for the client configuration
	// endpoint (RFC 7592 §2). It is returned ONLY in the registration response —
	// the server stores a hash and can never reproduce it.
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

// DCR error codes per RFC 7591 Section 3.2.2, plus the RFC 6750 bearer-token
// codes the RFC 7592 management endpoints answer with.
const (
	DCRErrorInvalidRedirectURI          = "invalid_redirect_uri"
	DCRErrorInvalidClientMetadata       = "invalid_client_metadata"
	DCRErrorInvalidSoftwareStatement    = "invalid_software_statement"
	DCRErrorUnapprovedSoftwareStatement = "unapproved_software_statement"
	// DCRErrorInvalidToken is returned whenever a registration access token is
	// absent, malformed, unknown or does not belong to the addressed client.
	DCRErrorInvalidToken = "invalid_token"
	// DCRErrorAccessDenied is returned when the registration surface itself is
	// switched off by operator configuration.
	DCRErrorAccessDenied = "access_denied"
)

// dcrInvalidTokenDescription is the single, deliberately opaque description
// returned for every RFC 7592 authorization failure. Distinguishing "unknown
// client" from "wrong token" would turn the endpoint into a client-id oracle.
const dcrInvalidTokenDescription = "invalid registration access token"

// RegisterDynamicClient handles POST /oauth2/register for Dynamic Client
// Registration (RFC 7591).
//
// The endpoint is disabled unless the operator enables it, and the tenant the
// new client is created in is derived from the caller's verified identity — it
// is NEVER taken from a request header and NEVER defaults to the control-plane
// system tenant.
func (h *IAMHandler) RegisterDynamicClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Operator gate + tenant resolution BEFORE any state is touched.
	tenantID, dcrErr := h.dcrRegistrationTenant(r)
	if dcrErr != nil {
		respondDCRError(w, dcrErr.status, dcrErr.code, dcrErr.description)
		return
	}

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

	appManager, ok := h.dcrAppManager(w, ctx, "dynamic registration not available")
	if !ok {
		return
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

	// Register the application together with its RFC 7592 registration access
	// token. The token is the sole credential for the management endpoints.
	app, registrationToken, err := appManager.RegisterDCRApp(ctx, tenantID, opts)
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

		// RFC 7592 management credential + endpoint. Also only visible here.
		RegistrationAccessToken: registrationToken,
		RegistrationClientURI:   requestAbsoluteURI(r) + "/" + app.ClientID,

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

// dcrGateError is a refusal produced before any client state is touched: the
// registration surface is disabled, or the caller has not proven which tenant
// it may register in.
type dcrGateError struct {
	status      int
	code        string
	description string
}

// dcrConfig returns the effective Dynamic Client Registration policy. A nil
// server or config yields the zero value, which is "disabled" — the surface
// fails closed for every partially-constructed handler.
func (h *IAMHandler) dcrConfig() config.IAMDynamicClientRegistrationConfig {
	if h.server == nil || h.server.config == nil {
		return config.IAMDynamicClientRegistrationConfig{}
	}
	return h.server.config.IAMDynamicClientRegistration
}

// dcrRegistrationTenant resolves the tenant a dynamic client registration
// creates its client in, or the refusal that stops the request.
//
// Precedence, fail-closed at every step:
//
//  1. The surface must be enabled by the operator (403 otherwise).
//  2. The verified principal's tenant (the RFC 7591 initial access token) wins.
//     The authz middleware on the route group put it there; a caller cannot
//     forge or override it, and cannot name another tenant.
//  3. Only if no verified tenant exists may an explicitly configured ANONYMOUS
//     landing tenant be used — never the control-plane system tenant, which is
//     re-checked here because the rest.Config may be populated programmatically
//     by an embedded caller that bypassed config validation.
//  4. Otherwise the registration is refused with 401: an initial access token
//     is required.
func (h *IAMHandler) dcrRegistrationTenant(r *http.Request) (string, *dcrGateError) {
	cfg := h.dcrConfig()
	if !cfg.Enabled {
		return "", &dcrGateError{
			status:      http.StatusForbidden,
			code:        DCRErrorAccessDenied,
			description: "dynamic client registration is disabled",
		}
	}

	if ac := AuthorizedContextFromRequest(r.Context()); ac != nil && ac.TenantID != "" {
		return ac.TenantID, nil
	}

	if cfg.AnonymousTenantID != "" {
		if cfg.AnonymousTenantID == qrdbsdk.SystemTenantID {
			return "", &dcrGateError{
				status:      http.StatusForbidden,
				code:        DCRErrorAccessDenied,
				description: "anonymous dynamic client registration into the system tenant is not permitted",
			}
		}
		return cfg.AnonymousTenantID, nil
	}

	return "", &dcrGateError{
		status:      http.StatusUnauthorized,
		code:        DCRErrorInvalidToken,
		description: "an initial access token is required for dynamic client registration",
	}
}

// dcrAppManager resolves the AppManager, writing the RFC 7591 error response
// and reporting false when the capability is not wired.
func (h *IAMHandler) dcrAppManager(w http.ResponseWriter, ctx context.Context, unavailable string) (*iam.AppManager, bool) {
	appProvider, ok := h.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	})
	if !ok {
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, unavailable)
		return nil, false
	}

	appManager, err := appProvider.GetAppManager(ctx)
	if err != nil {
		respondDCRError(w, http.StatusInternalServerError, DCRErrorInvalidClientMetadata, "failed to get app manager")
		return nil, false
	}
	return appManager, true
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

	// A redirect URI must not contain a fragment component
	// (draft-ietf-oauth-v2-1 §2.3), not even an empty one.
	if strings.Contains(uri, "#") {
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
		if !isURISchemeChar(c) {
			break
		}
	}

	return false
}

// isURISchemeChar reports whether r may appear in a URI scheme (RFC 3986 3.1).
func isURISchemeChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '.' || r == '-' || r == '+'
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

// authorizeRegistrationAccess is the single gate on the RFC 7592 client
// configuration endpoints. It requires a Bearer registration access token that
// was issued for THIS client_id, verified by constant-time comparison against
// the hash stored on the client record (iam.AppManager.AuthorizeRegistrationAccess).
//
// It fails closed: a missing/malformed header, an unknown client, a client that
// owns no registration token, a token belonging to a different client, and a
// client outside the caller's authenticated tenant all produce the same opaque
// 401 so the endpoint cannot be used to enumerate clients.
//
// On success it returns the AppManager and the resolved (redacted) application,
// whose TenantID is authoritative for the subsequent tenant-scoped mutation.
func (h *IAMHandler) authorizeRegistrationAccess(w http.ResponseWriter, r *http.Request, clientID string) (*iam.AppManager, *iam.Application, bool) {
	token, err := extractBearerToken(r)
	if err != nil {
		respondDCRError(w, http.StatusUnauthorized, DCRErrorInvalidToken, dcrInvalidTokenDescription)
		return nil, nil, false
	}

	appManager, ok := h.dcrAppManager(w, r.Context(), "client management not available")
	if !ok {
		return nil, nil, false
	}

	// When the request carries a verified principal (the authz middleware ran,
	// or a management call is proxied by an authenticated operator), the client
	// must additionally live in that principal's tenant. A registration token is
	// already client-specific — this only narrows, never widens.
	tenantID := ""
	if ac := AuthorizedContextFromRequest(r.Context()); ac != nil {
		tenantID = ac.TenantID
	}

	app, err := appManager.AuthorizeRegistrationAccess(r.Context(), tenantID, clientID, token)
	if err != nil {
		h.logAuditEvent(r.Context(), tenantID, iam.AuditEventAppEvent, iam.AuditCategoryAdministration,
			"dynamic_client_management_denied", iam.AuditOutcomeFailure,
			&iam.AuditResource{Type: "application", ID: clientID},
			map[string]interface{}{"reason": err.Error()})
		respondDCRError(w, http.StatusUnauthorized, DCRErrorInvalidToken, dcrInvalidTokenDescription)
		return nil, nil, false
	}

	return appManager, app, true
}

// GetDynamicClient handles GET /oauth2/register/{client_id} for reading client configuration.
// This is RFC 7592 (OAuth 2.0 Dynamic Client Registration Management Protocol).
func (h *IAMHandler) GetDynamicClient(w http.ResponseWriter, r *http.Request) {
	// Extract client_id from path
	clientID := chi.URLParam(r, "client_id")
	if clientID == "" {
		respondDCRError(w, http.StatusBadRequest, DCRErrorInvalidClientMetadata, "client_id required")
		return
	}

	// The registration access token is the credential (RFC 7592 §2).
	_, app, ok := h.authorizeRegistrationAccess(w, r, clientID)
	if !ok {
		return
	}

	// Build response (without secret)
	resp := &DynamicClientResponse{
		ClientID:                app.ClientID,
		ClientIDIssuedAt:        app.Created.Unix(),
		ClientSecretExpiresAt:   0,
		RegistrationClientURI:   requestAbsoluteURI(r),
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

	// Authorize FIRST: an unauthorized caller must learn nothing about the
	// client, not even whether its proposed metadata would have validated.
	appManager, current, ok := h.authorizeRegistrationAccess(w, r, clientID)
	if !ok {
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

	// Update the application, scoped to the tenant that owns it.
	app, err := appManager.UpdateAppInTenant(ctx, current.TenantID, clientID, opts)
	if err != nil {
		respondDCRError(w, http.StatusBadRequest, DCRErrorInvalidClientMetadata, err.Error())
		return
	}

	h.logAuditEvent(ctx, app.TenantID, iam.AuditEventAppEvent, iam.AuditCategoryAdministration,
		"dynamic_client_update", iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "application", ID: app.ClientID},
		map[string]interface{}{
			"redirect_uris": app.RedirectURIs,
			"grant_types":   app.GrantTypes,
		})

	// Build response (without secret)
	resp := &DynamicClientResponse{
		ClientID:                app.ClientID,
		ClientIDIssuedAt:        app.Created.Unix(),
		ClientSecretExpiresAt:   0,
		RegistrationClientURI:   requestAbsoluteURI(r),
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

	appManager, app, ok := h.authorizeRegistrationAccess(w, r, clientID)
	if !ok {
		return
	}

	// Delete the application, scoped to the tenant that owns it.
	if err := appManager.DeleteAppInTenant(ctx, app.TenantID, clientID); err != nil {
		respondDCRError(w, http.StatusNotFound, DCRErrorInvalidClientMetadata, "client not found")
		return
	}

	h.logAuditEvent(ctx, app.TenantID, iam.AuditEventAppEvent, iam.AuditCategoryAdministration,
		"dynamic_client_delete", iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "application", ID: clientID}, nil)

	// Return 204 No Content
	w.WriteHeader(http.StatusNoContent)
}
