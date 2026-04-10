//go:build iam

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

package iam

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// FederationManager orchestrates external IdP authentication.
type FederationManager interface {
	// Provider Management
	RegisterProvider(ctx context.Context, provider *ExternalProvider) error
	GetProvider(ctx context.Context, tenantID, providerID string) (*ExternalProvider, error)
	GetProviderByName(ctx context.Context, tenantID, providerName string) (*ExternalProvider, error)
	UpdateProvider(ctx context.Context, provider *ExternalProvider) error
	DeleteProvider(ctx context.Context, tenantID, providerID string) error
	ListProviders(ctx context.Context, tenantID string, opts ListOptions) ([]*ExternalProvider, error)

	// Authentication Flow
	BeginLogin(ctx context.Context, tenantID, providerName, redirectURI string) (*LoginRedirect, error)
	HandleCallback(ctx context.Context, stateToken, authCode string) (*FederationResult, error)

	// Account Linking
	LinkAccount(ctx context.Context, userID, tenantID, providerID, externalToken string) error
	UnlinkAccount(ctx context.Context, userID, tenantID, providerID string) error
	GetUserLinks(ctx context.Context, userID, tenantID string) ([]*FederatedIdentity, error)
}

// FederationResult contains the result of a successful federation authentication.
type FederationResult struct {
	// User is the authenticated or provisioned user.
	User *User `json:"user"`

	// FederatedIdentity is the identity link.
	FederatedIdentity *FederatedIdentity `json:"federated_identity"`

	// AccessToken is the QuicBaaS access token.
	AccessToken string `json:"access_token"`

	// RefreshToken is the QuicBaaS refresh token.
	RefreshToken string `json:"refresh_token,omitempty"`

	// ExpiresIn is the access token lifetime in seconds.
	ExpiresIn int64 `json:"expires_in"`

	// Provisioned indicates whether the user was newly created.
	Provisioned bool `json:"provisioned"`
}

// OIDCDiscoveryDocument represents the OIDC discovery document.
type OIDCDiscoveryDocument struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	JwksURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported"`
	ClaimsSupported       []string `json:"claims_supported"`
}

// oauthClientCacheEntry holds a cached OAuth2 config and OIDC discovery info.
type oauthClientCacheEntry struct {
	config    *oauth2.Config
	discovery *OIDCDiscoveryDocument
	expiresAt time.Time
}

// DefaultFederationManager is the default implementation of FederationManager.
type DefaultFederationManager struct {
	providerStorage ProviderStorage
	linkStorage     FederationStorage
	stateStorage    StateStorage
	identityStore   *IdentityStore
	oidcProvider    *OIDCProvider
	auditLogger     AuditLogger
	httpClient      *http.Client

	// OAuth client cache (keyed by providerID)
	clientCache   map[string]*oauthClientCacheEntry
	clientCacheMu sync.RWMutex
	cacheTTL      time.Duration

	// State nonce expiry
	stateExpiry time.Duration
}

// FederationManagerOption configures the federation manager.
type FederationManagerOption func(*DefaultFederationManager)

// WithProviderStorage sets the provider storage backend.
func WithProviderStorage(storage ProviderStorage) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		m.providerStorage = storage
	}
}

// WithLinkStorage sets the federation link storage backend.
func WithLinkStorage(storage FederationStorage) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		m.linkStorage = storage
	}
}

// WithStateStorage sets the state storage backend.
func WithStateStorage(storage StateStorage) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		m.stateStorage = storage
	}
}

// WithFederationIdentityStore sets the identity store.
func WithFederationIdentityStore(store *IdentityStore) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		m.identityStore = store
	}
}

// WithFederationOIDCProvider sets the OIDC provider for token generation.
func WithFederationOIDCProvider(provider *OIDCProvider) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		m.oidcProvider = provider
	}
}

// WithFederationAuditLogger sets the audit logger.
func WithFederationAuditLogger(logger AuditLogger) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		m.auditLogger = logger
	}
}

// WithFederationHTTPClient sets the HTTP client for OIDC discovery.
func WithFederationHTTPClient(client *http.Client) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		m.httpClient = client
	}
}

// WithClientCacheTTL sets the OAuth client cache TTL.
func WithClientCacheTTL(ttl time.Duration) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		m.cacheTTL = ttl
	}
}

// WithStateExpiry sets the state token expiry duration.
func WithStateExpiry(expiry time.Duration) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		m.stateExpiry = expiry
	}
}

// NewFederationManager creates a new federation manager.
func NewFederationManager(opts ...FederationManagerOption) (*DefaultFederationManager, error) {
	m := &DefaultFederationManager{
		httpClient:  http.DefaultClient,
		clientCache: make(map[string]*oauthClientCacheEntry),
		cacheTTL:    1 * time.Hour,
		stateExpiry: 5 * time.Minute,
	}

	for _, opt := range opts {
		opt(m)
	}

	if m.providerStorage == nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.new", "provider storage is required", nil)
	}
	if m.linkStorage == nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.new", "link storage is required", nil)
	}
	if m.stateStorage == nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.new", "state storage is required", nil)
	}
	if m.identityStore == nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.new", "identity store is required", nil)
	}

	return m, nil
}

// RegisterProvider registers a new external identity provider.
func (m *DefaultFederationManager) RegisterProvider(ctx context.Context, provider *ExternalProvider) error {
	// Generate ID if not provided
	if provider.ID == "" {
		provider.ID = uuid.New().String()
	}

	// Apply defaults before validation
	provider.ApplyDefaults()

	if err := provider.Validate(); err != nil {
		return err
	}

	// Check for duplicate name
	existing, err := m.providerStorage.GetProviderByName(ctx, provider.TenantID, provider.Name)
	if err == nil && existing != nil {
		return NewIAMError(ErrProviderAlreadyExists, "iam.federation.register",
			fmt.Sprintf("provider with name '%s' already exists", provider.Name), nil)
	}

	now := time.Now()
	provider.Created = now
	provider.Updated = now

	if err := m.providerStorage.CreateProvider(ctx, provider); err != nil {
		return NewIAMError(ErrProviderInvalid, "iam.federation.register",
			"failed to create provider", err)
	}

	// Log audit event
	m.logProviderEvent(ctx, provider, "register", true, nil)

	return nil
}

// GetProvider retrieves a provider by ID.
func (m *DefaultFederationManager) GetProvider(ctx context.Context, tenantID, providerID string) (*ExternalProvider, error) {
	if tenantID == "" || providerID == "" {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.get",
			"tenant ID and provider ID are required", nil)
	}

	provider, err := m.providerStorage.GetProvider(ctx, tenantID, providerID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrProviderNotFound
		}
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.get",
			"failed to get provider", err)
	}

	return provider, nil
}

// GetProviderByName retrieves a provider by name.
func (m *DefaultFederationManager) GetProviderByName(ctx context.Context, tenantID, providerName string) (*ExternalProvider, error) {
	if tenantID == "" || providerName == "" {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.get_by_name",
			"tenant ID and provider name are required", nil)
	}

	provider, err := m.providerStorage.GetProviderByName(ctx, tenantID, providerName)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrProviderNotFound
		}
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.get_by_name",
			"failed to get provider", err)
	}

	return provider, nil
}

// UpdateProvider updates an existing provider.
func (m *DefaultFederationManager) UpdateProvider(ctx context.Context, provider *ExternalProvider) error {
	if err := provider.Validate(); err != nil {
		return err
	}

	// Verify provider exists
	existing, err := m.providerStorage.GetProvider(ctx, provider.TenantID, provider.ID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return ErrProviderNotFound
		}
		return NewIAMError(ErrProviderInvalid, "iam.federation.update",
			"failed to get provider", err)
	}

	// Check for name collision
	if provider.Name != existing.Name {
		byName, err := m.providerStorage.GetProviderByName(ctx, provider.TenantID, provider.Name)
		if err == nil && byName != nil && byName.ID != provider.ID {
			return NewIAMError(ErrProviderAlreadyExists, "iam.federation.update",
				fmt.Sprintf("provider with name '%s' already exists", provider.Name), nil)
		}
	}

	provider.Updated = time.Now()

	if err := m.providerStorage.UpdateProvider(ctx, provider); err != nil {
		return NewIAMError(ErrProviderInvalid, "iam.federation.update",
			"failed to update provider", err)
	}

	// Invalidate cache
	m.invalidateClientCache(provider.ID)

	// Log audit event
	m.logProviderEvent(ctx, provider, "update", true, nil)

	return nil
}

// DeleteProvider deletes a provider.
func (m *DefaultFederationManager) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	if tenantID == "" || providerID == "" {
		return NewIAMError(ErrProviderInvalid, "iam.federation.delete",
			"tenant ID and provider ID are required", nil)
	}

	provider, err := m.providerStorage.GetProvider(ctx, tenantID, providerID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return ErrProviderNotFound
		}
		return NewIAMError(ErrProviderInvalid, "iam.federation.delete",
			"failed to get provider", err)
	}

	if err := m.providerStorage.DeleteProvider(ctx, tenantID, providerID); err != nil {
		return NewIAMError(ErrProviderInvalid, "iam.federation.delete",
			"failed to delete provider", err)
	}

	// Invalidate cache
	m.invalidateClientCache(providerID)

	// Log audit event
	m.logProviderEvent(ctx, provider, "delete", true, nil)

	return nil
}

// ListProviders returns a list of providers for a tenant.
func (m *DefaultFederationManager) ListProviders(ctx context.Context, tenantID string, opts ListOptions) ([]*ExternalProvider, error) {
	if tenantID == "" {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.list",
			"tenant ID is required", nil)
	}

	providers, err := m.providerStorage.ListProviders(ctx, tenantID, opts)
	if err != nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.list",
			"failed to list providers", err)
	}

	return providers, nil
}

// BeginLogin initiates the OAuth2 authorization flow.
func (m *DefaultFederationManager) BeginLogin(ctx context.Context, tenantID, providerName, redirectURI string) (*LoginRedirect, error) {
	if tenantID == "" || providerName == "" {
		return nil, NewIAMError(ErrFederationStateInvalid, "iam.federation.begin_login",
			"tenant ID and provider name are required", nil)
	}

	// Get provider
	provider, err := m.providerStorage.GetProviderByName(ctx, tenantID, providerName)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrProviderNotFound
		}
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.begin_login",
			"failed to get provider", err)
	}

	if !provider.Enabled {
		return nil, ErrProviderDisabled
	}

	// Get or create OAuth2 config
	oauthConfig, err := m.getOAuthConfig(ctx, provider)
	if err != nil {
		return nil, err
	}

	// Generate cryptographically secure state nonce (32 bytes, base64 encoded)
	stateNonce, err := generateSecureNonce(32)
	if err != nil {
		return nil, NewIAMError(ErrFederationStateInvalid, "iam.federation.begin_login",
			"failed to generate state token", err)
	}

	// Store state
	now := time.Now()
	state := &FederationCallbackState{
		Nonce:       stateNonce,
		ProviderID:  provider.ID,
		TenantID:    tenantID,
		RedirectURI: redirectURI,
		ExpiresAt:   now.Add(m.stateExpiry),
	}

	if err := m.stateStorage.SaveState(ctx, state); err != nil {
		return nil, NewIAMError(ErrFederationStateInvalid, "iam.federation.begin_login",
			"failed to store state", err)
	}

	// Generate authorization URL
	authURL := oauthConfig.AuthCodeURL(stateNonce, oauth2.AccessTypeOffline)

	return &LoginRedirect{
		AuthURL:   authURL,
		State:     stateNonce,
		ExpiresAt: state.ExpiresAt,
	}, nil
}

// HandleCallback processes the OAuth2 callback.
func (m *DefaultFederationManager) HandleCallback(ctx context.Context, stateToken, authCode string) (*FederationResult, error) {
	if stateToken == "" || authCode == "" {
		return nil, NewIAMError(ErrFederationStateInvalid, "iam.federation.callback",
			"state token and auth code are required", nil)
	}

	// Retrieve and validate state
	state, err := m.stateStorage.GetState(ctx, stateToken)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrFederationStateNotFound
		}
		return nil, NewIAMError(ErrFederationStateInvalid, "iam.federation.callback",
			"failed to get state", err)
	}

	// Delete state (one-time use)
	defer func() {
		_ = m.stateStorage.DeleteState(ctx, stateToken)
	}()

	// Check expiry (5-minute window)
	if state.IsExpired() {
		return nil, ErrFederationStateExpired
	}

	// Get provider
	provider, err := m.providerStorage.GetProvider(ctx, state.TenantID, state.ProviderID)
	if err != nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.callback",
			"failed to get provider", err)
	}

	if !provider.Enabled {
		return nil, ErrProviderDisabled
	}

	// Get OAuth2 config
	oauthConfig, err := m.getOAuthConfig(ctx, provider)
	if err != nil {
		return nil, err
	}

	// Exchange code for tokens
	token, err := oauthConfig.Exchange(ctx, authCode)
	if err != nil {
		m.logAuthEvent(ctx, state.TenantID, nil, provider, "callback", false,
			map[string]interface{}{"error": err.Error()})
		return nil, NewIAMError(ErrOIDCTokenExchangeFailed, "iam.federation.callback",
			"token exchange failed", err)
	}

	// Extract claims from ID token or userinfo
	claims, err := m.extractClaims(ctx, provider, token)
	if err != nil {
		return nil, err
	}

	// Process the authentication
	result, err := m.processAuthentication(ctx, provider, claims, state)
	if err != nil {
		m.logAuthEvent(ctx, state.TenantID, nil, provider, "callback", false,
			map[string]interface{}{"error": err.Error()})
		return nil, err
	}

	// Log successful authentication
	m.logAuthEvent(ctx, state.TenantID, result.User, provider, "callback", true,
		map[string]interface{}{"provisioned": result.Provisioned})

	return result, nil
}

// LinkAccount links an existing user account to an external provider.
func (m *DefaultFederationManager) LinkAccount(ctx context.Context, userID, tenantID, providerID, externalToken string) error {
	if userID == "" || tenantID == "" || providerID == "" || externalToken == "" {
		return NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.link",
			"user ID, tenant ID, provider ID, and external token are required", nil)
	}

	// Verify user exists
	user, err := m.identityStore.GetUser(ctx, tenantID, userID)
	if err != nil {
		return NewIAMError(ErrUserNotFound, "iam.federation.link",
			"user not found", err)
	}

	// Get provider
	provider, err := m.providerStorage.GetProvider(ctx, tenantID, providerID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return ErrProviderNotFound
		}
		return NewIAMError(ErrProviderInvalid, "iam.federation.link",
			"failed to get provider", err)
	}

	if !provider.AllowLinking {
		return ErrAccountLinkingDisabled
	}

	// Create OAuth2 token from the external token string
	oauthToken := &oauth2.Token{AccessToken: externalToken}

	// Extract claims
	claims, err := m.extractClaims(ctx, provider, oauthToken)
	if err != nil {
		return err
	}

	externalID := m.extractClaim(claims, provider.ClaimMappings.Subject, DefaultSubjectClaim)
	if externalID == "" {
		return NewIAMError(ErrOIDCClaimMappingFailed, "iam.federation.link",
			"external ID not found in claims", nil)
	}

	// Check if external ID is already linked
	existingByExternal, err := m.linkStorage.GetLinkByExternal(ctx, providerID, externalID)
	if err == nil && existingByExternal != nil {
		if existingByExternal.UserID == userID {
			return ErrFederatedIdentityAlreadyLinked
		}
		return NewIAMError(ErrFederatedIdentityAlreadyLinked, "iam.federation.link",
			"external identity already linked to another user", nil)
	}

	// Check if user already has a link to this provider
	existingLinks, err := m.linkStorage.GetLinksByUser(ctx, userID)
	if err == nil {
		for _, link := range existingLinks {
			if link.ProviderID == providerID {
				return ErrFederatedIdentityAlreadyLinked
			}
		}
	}

	// Create identity link
	now := time.Now()
	identity := &FederatedIdentity{
		ID:            uuid.New().String(),
		UserID:        userID,
		TenantID:      tenantID,
		ProviderID:    providerID,
		ExternalID:    externalID,
		ExternalEmail: m.extractClaim(claims, provider.ClaimMappings.Email, DefaultEmailClaim),
		ExternalName:  m.extractClaim(claims, provider.ClaimMappings.Name, DefaultNameClaim),
		Claims:        claims,
		LastLogin:     now,
		Created:       now,
		Updated:       now,
	}

	if err := m.linkStorage.CreateLink(ctx, identity); err != nil {
		return NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.link",
			"failed to create identity link", err)
	}

	// Log audit event
	m.logLinkEvent(ctx, tenantID, user, provider, "link", true, nil)

	return nil
}

// UnlinkAccount removes the link between a user and an external provider.
func (m *DefaultFederationManager) UnlinkAccount(ctx context.Context, userID, tenantID, providerID string) error {
	if userID == "" || tenantID == "" || providerID == "" {
		return NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.unlink",
			"user ID, tenant ID, and provider ID are required", nil)
	}

	// Verify user exists
	user, err := m.identityStore.GetUser(ctx, tenantID, userID)
	if err != nil {
		return NewIAMError(ErrUserNotFound, "iam.federation.unlink",
			"user not found", err)
	}

	// Find the identity link
	links, err := m.linkStorage.GetLinksByUser(ctx, userID)
	if err != nil {
		return NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.unlink",
			"failed to get identity links", err)
	}

	var targetLink *FederatedIdentity
	for _, link := range links {
		if link.ProviderID == providerID {
			targetLink = link
			break
		}
	}

	if targetLink == nil {
		return ErrFederatedIdentityNotFound
	}

	// Get provider for audit
	provider, _ := m.providerStorage.GetProvider(ctx, tenantID, providerID)

	if err := m.linkStorage.DeleteLink(ctx, targetLink.ID); err != nil {
		return NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.unlink",
			"failed to delete identity link", err)
	}

	// Log audit event
	m.logLinkEvent(ctx, tenantID, user, provider, "unlink", true, nil)

	return nil
}

// GetUserLinks returns all federated identities for a user.
func (m *DefaultFederationManager) GetUserLinks(ctx context.Context, userID, tenantID string) ([]*FederatedIdentity, error) {
	if userID == "" || tenantID == "" {
		return nil, NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.get_links",
			"user ID and tenant ID are required", nil)
	}

	links, err := m.linkStorage.GetLinksByUser(ctx, userID)
	if err != nil {
		return nil, NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.get_links",
			"failed to list identity links", err)
	}

	// Filter by tenant
	var result []*FederatedIdentity
	for _, link := range links {
		if link.TenantID == tenantID {
			result = append(result, link)
		}
	}

	return result, nil
}

// getOAuthConfig returns or creates an OAuth2 config for the provider.
func (m *DefaultFederationManager) getOAuthConfig(ctx context.Context, provider *ExternalProvider) (*oauth2.Config, error) {
	// Check cache
	m.clientCacheMu.RLock()
	entry, ok := m.clientCache[provider.ID]
	m.clientCacheMu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
		return entry.config, nil
	}

	// Build OAuth2 config
	var authURL, tokenURL string

	if provider.Type == ProviderTypeOIDC {
		// Check if explicit endpoints are provided
		if provider.AuthURL != "" && provider.TokenURL != "" {
			authURL = provider.AuthURL
			tokenURL = provider.TokenURL
		} else {
			// Perform OIDC discovery
			discovery, err := m.discoverOIDC(ctx, provider.IssuerURL)
			if err != nil {
				return nil, err
			}
			authURL = discovery.AuthorizationEndpoint
			tokenURL = discovery.TokenEndpoint

			// Cache with discovery info
			m.clientCacheMu.Lock()
			m.clientCache[provider.ID] = &oauthClientCacheEntry{
				config: &oauth2.Config{
					ClientID:     provider.ClientID,
					ClientSecret: provider.ClientSecret,
					Endpoint: oauth2.Endpoint{
						AuthURL:  authURL,
						TokenURL: tokenURL,
					},
					Scopes: provider.Scopes,
				},
				discovery: discovery,
				expiresAt: time.Now().Add(m.cacheTTL),
			}
			m.clientCacheMu.Unlock()

			return m.clientCache[provider.ID].config, nil
		}
	} else {
		// Non-OIDC provider - use explicit endpoints
		authURL = provider.AuthURL
		tokenURL = provider.TokenURL
	}

	config := &oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  authURL,
			TokenURL: tokenURL,
		},
		Scopes: provider.Scopes,
	}

	// Cache
	m.clientCacheMu.Lock()
	m.clientCache[provider.ID] = &oauthClientCacheEntry{
		config:    config,
		expiresAt: time.Now().Add(m.cacheTTL),
	}
	m.clientCacheMu.Unlock()

	return config, nil
}

// discoverOIDC performs OIDC discovery.
func (m *DefaultFederationManager) discoverOIDC(ctx context.Context, issuerURL string) (*OIDCDiscoveryDocument, error) {
	discoveryURL := strings.TrimSuffix(issuerURL, "/") + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.discover",
			"failed to create discovery request", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.discover",
			"failed to fetch discovery document", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.discover",
			(&DiscoveryStatusError{StatusCode: resp.StatusCode}).Error(), nil)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.discover",
			"failed to read discovery document", err)
	}

	var discovery OIDCDiscoveryDocument
	if err := json.Unmarshal(body, &discovery); err != nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.discover",
			"failed to parse discovery document", err)
	}

	return &discovery, nil
}

// extractClaims extracts user claims from the OAuth2 token.
func (m *DefaultFederationManager) extractClaims(ctx context.Context, provider *ExternalProvider, token *oauth2.Token) (map[string]interface{}, error) {
	claims := make(map[string]interface{})

	// Try to extract from ID token if present (OIDC)
	if idToken, ok := token.Extra("id_token").(string); ok && idToken != "" {
		idClaims, err := m.parseIDToken(idToken)
		if err == nil {
			for k, v := range idClaims {
				claims[k] = v
			}
			return claims, nil
		}
		// Fall through to userinfo if ID token parsing fails
	}

	// Fetch userinfo
	userInfoURL := provider.UserInfoURL
	if userInfoURL == "" {
		// Try to get from cached discovery
		m.clientCacheMu.RLock()
		entry, ok := m.clientCache[provider.ID]
		m.clientCacheMu.RUnlock()
		if ok && entry.discovery != nil {
			userInfoURL = entry.discovery.UserinfoEndpoint
		}
	}

	if userInfoURL == "" {
		return nil, NewIAMError(ErrOIDCUserInfoFailed, "iam.federation.claims",
			"no userinfo endpoint available", nil)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return nil, NewIAMError(ErrOIDCUserInfoFailed, "iam.federation.claims",
			"failed to create userinfo request", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, NewIAMError(ErrOIDCUserInfoFailed, "iam.federation.claims",
			"failed to fetch userinfo", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, NewIAMError(ErrOIDCUserInfoFailed, "iam.federation.claims",
			(&UserInfoStatusError{StatusCode: resp.StatusCode}).Error(), nil)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewIAMError(ErrOIDCUserInfoFailed, "iam.federation.claims",
			"failed to read userinfo response", err)
	}

	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, NewIAMError(ErrOIDCUserInfoFailed, "iam.federation.claims",
			"failed to parse userinfo response", err)
	}

	return claims, nil
}

// parseIDToken parses a JWT ID token without full verification.
// In production, this should verify the token signature.
func (m *DefaultFederationManager) parseIDToken(idToken string) (map[string]interface{}, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, &IDTokenFormatError{}
	}

	// Decode payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, &IDTokenDecodeError{Cause: err}
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, &IDTokenClaimsError{Cause: err}
	}

	return claims, nil
}

// processAuthentication handles user provisioning and linking.
func (m *DefaultFederationManager) processAuthentication(ctx context.Context, provider *ExternalProvider, claims map[string]interface{}, state *FederationCallbackState) (*FederationResult, error) {
	// Extract claims using mappings
	externalID := m.extractClaim(claims, provider.ClaimMappings.Subject, DefaultSubjectClaim)
	email := m.extractClaim(claims, provider.ClaimMappings.Email, DefaultEmailClaim)
	name := m.extractClaim(claims, provider.ClaimMappings.Name, DefaultNameClaim)

	if externalID == "" {
		return nil, NewIAMError(ErrOIDCClaimMappingFailed, "iam.federation.process",
			"external ID not found in claims", nil)
	}

	// Try to find existing identity link
	identity, err := m.linkStorage.GetLinkByExternal(ctx, provider.ID, externalID)
	if err == nil && identity != nil {
		// User is already linked
		user, err := m.identityStore.GetUser(ctx, state.TenantID, identity.UserID)
		if err != nil {
			return nil, NewIAMError(ErrUserNotFound, "iam.federation.process",
				"linked user not found", err)
		}

		// Update last login and claims
		identity.LastLogin = time.Now()
		identity.Updated = time.Now()
		identity.Claims = claims
		_ = m.linkStorage.UpdateLink(ctx, identity)

		// Generate tokens
		return m.generateTokenResult(ctx, user, identity, false)
	}

	// No existing link - try to link by email if allowed
	if provider.AllowLinking && email != "" {
		user, err := m.identityStore.GetUserByEmail(ctx, email)
		if err == nil && user != nil && user.TenantID == state.TenantID {
			// Link to existing user
			now := time.Now()
			identity = &FederatedIdentity{
				ID:            uuid.New().String(),
				UserID:        user.ID,
				TenantID:      state.TenantID,
				ProviderID:    provider.ID,
				ExternalID:    externalID,
				ExternalEmail: email,
				ExternalName:  name,
				Claims:        claims,
				LastLogin:     now,
				Created:       now,
				Updated:       now,
			}

			if err := m.linkStorage.CreateLink(ctx, identity); err != nil {
				return nil, NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.process",
					"failed to create identity link", err)
			}

			return m.generateTokenResult(ctx, user, identity, false)
		}
	}

	// No existing user - provision if allowed
	if !provider.AutoProvision {
		return nil, ErrJITProvisioningDisabled
	}

	// JIT user provisioning
	user, err := m.provisionUser(ctx, provider, state.TenantID, externalID, email, name, claims)
	if err != nil {
		return nil, err
	}

	// Create identity link
	now := time.Now()
	identity = &FederatedIdentity{
		ID:            uuid.New().String(),
		UserID:        user.ID,
		TenantID:      state.TenantID,
		ProviderID:    provider.ID,
		ExternalID:    externalID,
		ExternalEmail: email,
		ExternalName:  name,
		Claims:        claims,
		LastLogin:     now,
		Created:       now,
		Updated:       now,
	}

	if err := m.linkStorage.CreateLink(ctx, identity); err != nil {
		return nil, NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.process",
			"failed to create identity link", err)
	}

	return m.generateTokenResult(ctx, user, identity, true)
}

// provisionUser creates a new user from external identity claims.
func (m *DefaultFederationManager) provisionUser(ctx context.Context, provider *ExternalProvider, tenantID, externalID, email, name string, claims map[string]interface{}) (*User, error) {
	// Generate username from email or external ID
	username := email
	if username == "" {
		username = externalID
	}
	// Sanitize username
	username = strings.ReplaceAll(username, "@", "_")
	username = strings.ReplaceAll(username, ".", "_")
	if len(username) > 64 {
		username = username[:64]
	}

	// Prepare create options
	opts := CreateUserOptions{
		Email:    email,
		Username: username,
		Name:     name,
		Enabled:  true,
	}

	// Create user
	user, err := m.identityStore.CreateUser(ctx, tenantID, opts)
	if err != nil {
		return nil, NewIAMError(ErrUserInvalid, "iam.federation.provision",
			"failed to create user", err)
	}

	// Assign default roles
	for _, roleID := range provider.DefaultRoles {
		if err := m.identityStore.AssignRole(ctx, tenantID, user.ID, roleID); err != nil {
			// Log but don't fail on role assignment errors
			continue
		}
	}

	// Assign default groups
	for _, groupID := range provider.DefaultGroups {
		if err := m.identityStore.AddUserToGroup(ctx, tenantID, groupID, user.ID); err != nil {
			// Log but don't fail on group assignment errors
			continue
		}
	}

	return user, nil
}

// generateTokenResult generates a FederationResult with QuicBaaS tokens.
func (m *DefaultFederationManager) generateTokenResult(ctx context.Context, user *User, identity *FederatedIdentity, provisioned bool) (*FederationResult, error) {
	// Generate QuicBaaS tokens using OIDC provider if available
	if m.oidcProvider != nil {
		tokenResp, err := m.oidcProvider.generateTokenResponse(ctx, user, "", []string{"openid", "profile", "email"}, "", "")
		if err != nil {
			return nil, NewIAMError(ErrOIDCTokenExchangeFailed, "iam.federation.tokens",
				"failed to generate tokens", err)
		}

		return &FederationResult{
			User:              user,
			FederatedIdentity: identity,
			AccessToken:       tokenResp.AccessToken,
			RefreshToken:      tokenResp.RefreshToken,
			ExpiresIn:         tokenResp.ExpiresIn,
			Provisioned:       provisioned,
		}, nil
	}

	// Return result without tokens if no OIDC provider
	return &FederationResult{
		User:              user,
		FederatedIdentity: identity,
		Provisioned:       provisioned,
	}, nil
}

// extractClaim extracts a claim value using the mapping or default key.
func (m *DefaultFederationManager) extractClaim(claims map[string]interface{}, mappedKey, defaultKey string) string {
	key := mappedKey
	if key == "" {
		key = defaultKey
	}

	if val, ok := claims[key]; ok {
		switch v := val.(type) {
		case string:
			return v
		case float64:
			return fmt.Sprintf("%.0f", v)
		default:
			return fmt.Sprintf("%v", v)
		}
	}

	return ""
}

// invalidateClientCache removes a provider from the OAuth client cache.
func (m *DefaultFederationManager) invalidateClientCache(providerID string) {
	m.clientCacheMu.Lock()
	delete(m.clientCache, providerID)
	m.clientCacheMu.Unlock()
}

// generateSecureNonce generates a cryptographically secure nonce.
func generateSecureNonce(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// logProviderEvent logs an audit event for provider operations.
func (m *DefaultFederationManager) logProviderEvent(ctx context.Context, provider *ExternalProvider, action string, success bool, details map[string]interface{}) {
	if m.auditLogger == nil {
		return
	}

	outcome := AuditOutcomeSuccess
	if !success {
		outcome = AuditOutcomeFailure
	}

	event := &AuditEvent{
		TenantID:  provider.TenantID,
		EventType: AuditEventAppEvent,
		Category:  AuditCategoryAdministration,
		Resource: &AuditResource{
			Type: "federation_provider",
			ID:   provider.ID,
			Name: provider.Name,
		},
		Action:  action,
		Outcome: outcome,
		Details: details,
	}

	_ = m.auditLogger.Log(ctx, event)
}

// logAuthEvent logs an audit event for federation authentication.
func (m *DefaultFederationManager) logAuthEvent(ctx context.Context, tenantID string, user *User, provider *ExternalProvider, action string, success bool, details map[string]interface{}) {
	if m.auditLogger == nil {
		return
	}

	outcome := AuditOutcomeSuccess
	if !success {
		outcome = AuditOutcomeFailure
	}

	var actor *AuditActor
	if user != nil {
		actor = &AuditActor{
			UserID:   user.ID,
			Username: user.Name,
			Email:    user.Email,
		}
	}

	if details == nil {
		details = make(map[string]interface{})
	}
	if provider != nil {
		details["provider_id"] = provider.ID
		details["provider_name"] = provider.Name
		details["provider_type"] = provider.Type
	}

	event := &AuditEvent{
		TenantID:  tenantID,
		EventType: AuditEventAuthentication,
		Category:  AuditCategoryAuthentication,
		Actor:     actor,
		Action:    "federation_" + action,
		Outcome:   outcome,
		Details:   details,
	}

	_ = m.auditLogger.Log(ctx, event)
}

// logLinkEvent logs an audit event for account linking operations.
func (m *DefaultFederationManager) logLinkEvent(ctx context.Context, tenantID string, user *User, provider *ExternalProvider, action string, success bool, details map[string]interface{}) {
	if m.auditLogger == nil {
		return
	}

	outcome := AuditOutcomeSuccess
	if !success {
		outcome = AuditOutcomeFailure
	}

	var actor *AuditActor
	if user != nil {
		actor = &AuditActor{
			UserID:   user.ID,
			Username: user.Name,
			Email:    user.Email,
		}
	}

	if details == nil {
		details = make(map[string]interface{})
	}
	if provider != nil {
		details["provider_id"] = provider.ID
		details["provider_name"] = provider.Name
	}

	event := &AuditEvent{
		TenantID:  tenantID,
		EventType: AuditEventUserManagement,
		Category:  AuditCategorySecurity,
		Actor:     actor,
		Action:    "federation_" + action,
		Outcome:   outcome,
		Details:   details,
	}

	_ = m.auditLogger.Log(ctx, event)
}

// Ensure DefaultFederationManager implements FederationManager.
var _ FederationManager = (*DefaultFederationManager)(nil)
