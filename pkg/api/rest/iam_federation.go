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
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// FederationManager defines the interface for managing federated identity providers.
// Note: This is kept for backward compatibility with existing REST types. The actual
// service uses iam.FederationManager which has different method signatures.
type FederationManager interface {
	// Provider management
	RegisterProvider(ctx context.Context, tenantID string, opts RegisterProviderOptions) (*FederatedProvider, error)
	ListProviders(ctx context.Context, tenantID string, opts ListProvidersOptions) ([]*FederatedProvider, error)
	GetProvider(ctx context.Context, tenantID, providerID string) (*FederatedProvider, error)
	UpdateProvider(ctx context.Context, tenantID, providerID string, opts UpdateProviderOptions) (*FederatedProvider, error)
	DeleteProvider(ctx context.Context, tenantID, providerID string) error

	// Authentication flow
	BeginLogin(ctx context.Context, tenantID, providerName string, opts BeginLoginOptions) (*LoginRedirect, error)
	HandleCallback(ctx context.Context, tenantID, providerName string, opts CallbackOptions) (*FederatedAuthResult, error)

	// Account linking
	LinkAccount(ctx context.Context, tenantID, userID string, opts LinkAccountOptions) (*AccountLink, error)
	UnlinkAccount(ctx context.Context, tenantID, userID, providerID string) error
	GetUserLinks(ctx context.Context, tenantID, userID string, opts ListLinksOptions) ([]*AccountLink, error)
}

// FederatedProvider represents an external identity provider configuration.
type FederatedProvider struct {
	ID           string                 `json:"id"`
	TenantID     string                 `json:"tenant_id"`
	Name         string                 `json:"name"`
	Type         ProviderType           `json:"type"`
	Enabled      bool                   `json:"enabled"`
	ClientID     string                 `json:"client_id"`
	ClientSecret string                 `json:"client_secret,omitempty"`
	Issuer       string                 `json:"issuer,omitempty"`
	AuthURL      string                 `json:"auth_url,omitempty"`
	TokenURL     string                 `json:"token_url,omitempty"`
	UserInfoURL  string                 `json:"userinfo_url,omitempty"`
	JWKSURL      string                 `json:"jwks_url,omitempty"`
	Scopes       []string               `json:"scopes,omitempty"`
	ClaimMapping map[string]string      `json:"claim_mapping,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// ProviderType represents the type of identity provider.
type ProviderType string

const (
	ProviderTypeOIDC   ProviderType = "oidc"
	ProviderTypeSAML   ProviderType = "saml"
	ProviderTypeOAuth2 ProviderType = "oauth2"
	ProviderTypeLDAP   ProviderType = "ldap"
)

// RegisterProviderOptions contains options for registering a provider.
type RegisterProviderOptions struct {
	ID           string                 `json:"id,omitempty"`
	Name         string                 `json:"name"`
	Type         ProviderType           `json:"type"`
	Enabled      bool                   `json:"enabled"`
	ClientID     string                 `json:"client_id"`
	ClientSecret string                 `json:"client_secret"`
	Issuer       string                 `json:"issuer,omitempty"`
	AuthURL      string                 `json:"auth_url,omitempty"`
	TokenURL     string                 `json:"token_url,omitempty"`
	UserInfoURL  string                 `json:"userinfo_url,omitempty"`
	JWKSURL      string                 `json:"jwks_url,omitempty"`
	Scopes       []string               `json:"scopes,omitempty"`
	ClaimMapping map[string]string      `json:"claim_mapping,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// UpdateProviderOptions contains options for updating a provider.
type UpdateProviderOptions struct {
	Name         *string                 `json:"name,omitempty"`
	Enabled      *bool                   `json:"enabled,omitempty"`
	ClientID     *string                 `json:"client_id,omitempty"`
	ClientSecret *string                 `json:"client_secret,omitempty"`
	Issuer       *string                 `json:"issuer,omitempty"`
	AuthURL      *string                 `json:"auth_url,omitempty"`
	TokenURL     *string                 `json:"token_url,omitempty"`
	UserInfoURL  *string                 `json:"userinfo_url,omitempty"`
	JWKSURL      *string                 `json:"jwks_url,omitempty"`
	Scopes       *[]string               `json:"scopes,omitempty"`
	ClaimMapping *map[string]string      `json:"claim_mapping,omitempty"`
	Metadata     *map[string]interface{} `json:"metadata,omitempty"`
}

// ListProvidersOptions contains options for listing providers.
type ListProvidersOptions struct {
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Type   string `json:"type,omitempty"`
}

// BeginLoginOptions contains options for beginning a federated login.
type BeginLoginOptions struct {
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state,omitempty"`
	Nonce       string `json:"nonce,omitempty"`
	Scopes      string `json:"scopes,omitempty"`
}

// LoginRedirect contains the redirect information for federated login.
type LoginRedirect struct {
	RedirectURL string `json:"redirect_url"`
	State       string `json:"state"`
	Nonce       string `json:"nonce,omitempty"`
}

// CallbackOptions contains options for handling the callback.
type CallbackOptions struct {
	Code  string `json:"code"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// FederatedAuthResult contains the result of federated authentication.
type FederatedAuthResult struct {
	User         *iam.User `json:"user"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
	ExpiresIn    int       `json:"expires_in"`
	TokenType    string    `json:"token_type"`
	IsNewUser    bool      `json:"is_new_user"`
}

// LinkAccountOptions contains options for linking an account.
type LinkAccountOptions struct {
	ProviderID  string `json:"provider_id"`
	ExternalID  string `json:"external_id"`
	AccessToken string `json:"access_token,omitempty"`
}

// AccountLink represents a link between a user and an external identity.
type AccountLink struct {
	ID           string                 `json:"id"`
	UserID       string                 `json:"user_id"`
	ProviderID   string                 `json:"provider_id"`
	ProviderName string                 `json:"provider_name"`
	ExternalID   string                 `json:"external_id"`
	Email        string                 `json:"email,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// ListLinksOptions contains options for listing account links.
type ListLinksOptions struct {
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// RegisterFederationRoutes registers federation routes on the IAM handler.
func RegisterFederationRoutes(router chi.Router, h *IAMHandler) {
	// Provider management (under tenant routes)
	router.Route("/providers", func(r chi.Router) {
		r.Post("/", h.RegisterProvider)
		r.Get("/", h.ListProviders)
		r.Get("/{provider_id}", h.GetProvider)
		r.Put("/{provider_id}", h.UpdateProvider)
		r.Delete("/{provider_id}", h.DeleteProvider)
	})

	// Account linking (under user routes)
	router.Route("/links", func(r chi.Router) {
		r.Post("/", h.LinkAccount)
		r.Get("/", h.GetUserLinks)
		r.Delete("/{provider_id}", h.UnlinkAccount)
	})
}

// RegisterFederationAuthRoutes registers public authentication flow routes.
func RegisterFederationAuthRoutes(router chi.Router, h *IAMHandler) {
	router.Route("/api/v1/auth/providers/{tenant_id}/{provider_name}", func(r chi.Router) {
		r.Get("/login", h.BeginFederatedLogin)
		r.Get("/callback", h.HandleFederatedCallback)
	})
}

// getIAMFederationManager extracts the federation manager from the service,
// using the iam.FederationManager interface that the service actually implements.
func (h *IAMHandler) getIAMFederationManager(ctx context.Context) (iam.FederationManager, error) {
	fedMgr, ok := h.server.service.(interface {
		GetFederationManager(ctx context.Context) (iam.FederationManager, error)
	})
	if !ok {
		return nil, &FederationManagerNotAvailableError{}
	}
	return fedMgr.GetFederationManager(ctx)
}

// RegisterProvider handles POST /api/v1/tenants/{tenant_id}/providers - Register a new identity provider.
func (h *IAMHandler) RegisterProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse request
	var req RegisterProviderOptions
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Validate required fields
	if req.Name == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "provider name is required"))
		return
	}
	if req.Type == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "provider type is required"))
		return
	}
	if !isValidProviderType(req.Type) {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid provider type: must be one of oidc, saml, oauth2, ldap"))
		return
	}
	if req.ClientID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "client_id is required"))
		return
	}

	// Get federation manager using iam.FederationManager interface
	mgr, err := h.getIAMFederationManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Convert REST options to iam.ExternalProvider
	provider := &iam.ExternalProvider{
		ID:           req.ID,
		TenantID:     tenantID,
		Name:         req.Name,
		Type:         iam.ProviderType(req.Type),
		Enabled:      req.Enabled,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
		IssuerURL:    req.Issuer,
		AuthURL:      req.AuthURL,
		TokenURL:     req.TokenURL,
		UserInfoURL:  req.UserInfoURL,
		Scopes:       req.Scopes,
		Created:      time.Now(),
		Updated:      time.Now(),
	}

	// Register provider
	if err := mgr.RegisterProvider(ctx, provider); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, tenantID,
		iam.AuditEventAppEvent,
		iam.AuditCategoryAdministration,
		"register_provider",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "federation_provider", ID: provider.ID, Name: provider.Name},
		map[string]interface{}{"provider_type": string(provider.Type), "name": provider.Name},
	)

	RespondJSON(w, r, http.StatusCreated, provider)
}

// ListProviders handles GET /api/v1/tenants/{tenant_id}/providers - List identity providers.
func (h *IAMHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	// Parse query parameters
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	providerType := r.URL.Query().Get("type")

	// Get federation manager using iam.FederationManager interface
	mgr, err := h.getIAMFederationManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Convert to iam.ListOptions
	opts := iam.ListOptions{
		Limit:  limit,
		Offset: offset,
	}
	if providerType != "" {
		opts.Filter = map[string]interface{}{"type": providerType}
	}

	// List providers
	providers, err := mgr.ListProviders(ctx, tenantID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"providers": providers,
		"limit":     limit,
		"offset":    offset,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// GetProvider handles GET /api/v1/tenants/{tenant_id}/providers/{provider_id} - Get a provider.
func (h *IAMHandler) GetProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	providerID := chi.URLParam(r, "provider_id")
	if providerID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "provider ID is required"))
		return
	}

	// Get federation manager using iam.FederationManager interface
	mgr, err := h.getIAMFederationManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Get provider
	provider, err := mgr.GetProvider(ctx, tenantID, providerID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, provider)
}

// UpdateProvider handles PUT /api/v1/tenants/{tenant_id}/providers/{provider_id} - Update a provider.
func (h *IAMHandler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	providerID := chi.URLParam(r, "provider_id")
	if providerID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "provider ID is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse request
	var req UpdateProviderOptions
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Get federation manager using iam.FederationManager interface
	mgr, err := h.getIAMFederationManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Get existing provider to apply partial updates
	existing, err := mgr.GetProvider(ctx, tenantID, providerID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Apply updates to existing provider
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.ClientID != nil {
		existing.ClientID = *req.ClientID
	}
	if req.ClientSecret != nil {
		existing.ClientSecret = *req.ClientSecret
	}
	if req.Issuer != nil {
		existing.IssuerURL = *req.Issuer
	}
	if req.AuthURL != nil {
		existing.AuthURL = *req.AuthURL
	}
	if req.TokenURL != nil {
		existing.TokenURL = *req.TokenURL
	}
	if req.UserInfoURL != nil {
		existing.UserInfoURL = *req.UserInfoURL
	}
	if req.Scopes != nil {
		existing.Scopes = *req.Scopes
	}
	existing.Updated = time.Now()

	// Update provider
	if err := mgr.UpdateProvider(ctx, existing); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, tenantID,
		iam.AuditEventAppEvent,
		iam.AuditCategoryAdministration,
		"update_provider",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "federation_provider", ID: existing.ID, Name: existing.Name},
		map[string]interface{}{"provider_id": providerID},
	)

	RespondJSON(w, r, http.StatusOK, existing)
}

// DeleteProvider handles DELETE /api/v1/tenants/{tenant_id}/providers/{provider_id} - Delete a provider.
func (h *IAMHandler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	providerID := chi.URLParam(r, "provider_id")
	if providerID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "provider ID is required"))
		return
	}

	// Get federation manager using iam.FederationManager interface
	mgr, err := h.getIAMFederationManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Delete provider
	if err := mgr.DeleteProvider(ctx, tenantID, providerID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, tenantID,
		iam.AuditEventAppEvent,
		iam.AuditCategoryAdministration,
		"delete_provider",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "federation_provider", ID: providerID},
		map[string]interface{}{"provider_id": providerID},
	)

	RespondNoContent(w, r)
}

// BeginFederatedLogin handles GET /api/v1/auth/providers/{tenant_id}/{provider_name}/login - Begin federated login.
func (h *IAMHandler) BeginFederatedLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	providerName := chi.URLParam(r, "provider_name")

	// Parse query parameters
	redirectURI := r.URL.Query().Get("redirect_uri")
	if redirectURI == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "redirect_uri is required"))
		return
	}

	// Get federation manager using iam.FederationManager interface
	mgr, err := h.getIAMFederationManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Home-realm discovery: when no explicit tenant is supplied (or the
	// placeholder "_"/"auto" is used), resolve the tenant from non-secret
	// routing hints (X-Tenant header, TLS SNI/Host, or the login_hint email
	// domain). Fail closed if HRD cannot resolve a tenant.
	if isAutoSelector(tenantID) {
		resolved, rerr := h.resolveFederationTenant(ctx, r)
		if rerr != nil {
			// The caller supplied no tenant and home-realm discovery could not
			// route the request. This is a client error: supply a tenant or
			// register a home-realm domain/host.
			RespondIAMError(w, r, http.StatusBadRequest,
				NewIAMAPIError(ErrBadRequest, "tenant ID is required (home-realm discovery could not resolve a tenant)"))
			return
		}
		tenantID = resolved
	}

	// Multi-provider selection: a tenant may have several providers (Okta +
	// Cognito + ...). When no explicit provider is named, auto-select the
	// tenant's single enabled provider; if more than one exists the caller must
	// name one.
	if isAutoSelector(providerName) {
		selected, serr := h.selectTenantProvider(ctx, mgr, tenantID)
		if serr != nil {
			RespondIAMError(w, r, http.StatusBadRequest,
				NewIAMAPIError(ErrBadRequest, "provider name is required ("+serr.Error()+")"))
			return
		}
		providerName = selected
	}

	// Begin login flow - iam.FederationManager.BeginLogin takes (ctx, tenantID, providerName, redirectURI)
	redirect, err := mgr.BeginLogin(ctx, tenantID, providerName, redirectURI)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Redirect to identity provider
	http.Redirect(w, r, redirect.AuthURL, http.StatusFound)
}

// HandleFederatedCallback handles GET /api/v1/auth/providers/{tenant_id}/{provider_name}/callback - Handle IdP callback.
func (h *IAMHandler) HandleFederatedCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	providerName := chi.URLParam(r, "provider_name")
	if providerName == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "provider name is required"))
		return
	}

	// Parse callback parameters
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	// Check for error from IdP
	if errorParam != "" {
		errorDesc := r.URL.Query().Get("error_description")
		RespondOAuthError(w, r, http.StatusBadRequest,
			NewOAuthError(errorParam, errorDesc))
		return
	}

	if code == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "authorization code is required"))
		return
	}

	// Suppress unused variable warnings - state is part of the callback flow
	_ = providerName
	_ = tenantID

	// Get federation manager using iam.FederationManager interface
	mgr, err := h.getIAMFederationManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Handle callback - iam.FederationManager.HandleCallback takes (ctx, stateToken, authCode)
	result, err := mgr.HandleCallback(ctx, state, code)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, tenantID,
		iam.AuditEventAuthentication,
		iam.AuditCategoryAuthentication,
		"federated_login",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "user", ID: result.User.ID, Name: result.User.Email},
		map[string]interface{}{
			"provider":    providerName,
			"is_new_user": result.Provisioned,
		},
	)

	// Convert to REST response format
	resp := &FederatedAuthResult{
		User:         result.User,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    int(result.ExpiresIn),
		TokenType:    "Bearer",
		IsNewUser:    result.Provisioned,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// LinkAccount handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/links - Link an external account.
func (h *IAMHandler) LinkAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse request
	var req LinkAccountOptions
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Validate required fields
	if req.ProviderID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "provider_id is required"))
		return
	}
	if req.ExternalID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "external_id is required"))
		return
	}

	// Get federation manager using iam.FederationManager interface
	mgr, err := h.getIAMFederationManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Link account - iam.FederationManager.LinkAccount takes (ctx, userID, tenantID, providerID, externalToken)
	if err := mgr.LinkAccount(ctx, userID, tenantID, req.ProviderID, req.AccessToken); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, tenantID,
		iam.AuditEventUserManagement,
		iam.AuditCategoryAdministration,
		"link_account",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "user", ID: userID},
		map[string]interface{}{
			"provider_id": req.ProviderID,
			"external_id": req.ExternalID,
		},
	)

	// Build response
	link := &AccountLink{
		UserID:     userID,
		ProviderID: req.ProviderID,
		ExternalID: req.ExternalID,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	RespondJSON(w, r, http.StatusCreated, link)
}

// UnlinkAccount handles DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/links/{provider_id} - Unlink an account.
func (h *IAMHandler) UnlinkAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	providerID := chi.URLParam(r, "provider_id")
	if providerID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "provider ID is required"))
		return
	}

	// Get federation manager using iam.FederationManager interface
	mgr, err := h.getIAMFederationManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Unlink account - iam.FederationManager.UnlinkAccount takes (ctx, userID, tenantID, providerID)
	if err := mgr.UnlinkAccount(ctx, userID, tenantID, providerID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Log audit event
	h.logAuditEvent(ctx, tenantID,
		iam.AuditEventUserManagement,
		iam.AuditCategoryAdministration,
		"unlink_account",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "user", ID: userID},
		map[string]interface{}{"provider_id": providerID},
	)

	RespondNoContent(w, r)
}

// GetUserLinks handles GET /api/v1/tenants/{tenant_id}/users/{user_id}/links - Get user's linked accounts.
func (h *IAMHandler) GetUserLinks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	// Parse pagination query parameters
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Get federation manager using iam.FederationManager interface
	mgr, err := h.getIAMFederationManager(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Get user links - iam.FederationManager.GetUserLinks takes (ctx, userID, tenantID)
	links, err := mgr.GetUserLinks(ctx, userID, tenantID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"links":  links,
		"limit":  limit,
		"offset": offset,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// isValidProviderType validates the provider type.
func isValidProviderType(t ProviderType) bool {
	switch t {
	case ProviderTypeOIDC, ProviderTypeSAML, ProviderTypeOAuth2, ProviderTypeLDAP:
		return true
	default:
		return false
	}
}
