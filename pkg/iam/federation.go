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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
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

// ProviderResolver performs home-realm provider discovery: it resolves the
// tenant and the home provider from non-secret routing hints. It is satisfied by
// *HomeRealmDirectory and injected into the federation manager so BeginLogin can
// select a provider automatically when the caller does not name one.
type ProviderResolver interface {
	// ResolveProvider resolves both the tenant and the home provider from hints.
	ResolveProvider(ctx context.Context, hints RealmHints, lister ProviderLister) (RealmProvider, error)
	// ResolveProviderForTenant selects the home provider within an already-known
	// tenant (used when BeginLogin is given an explicit tenant ID).
	ResolveProviderForTenant(ctx context.Context, tenantID string, hints RealmHints, lister ProviderLister) (RealmProvider, error)
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

	// ScopesGranted are the platform token scopes (the `scp` catalog) granted to
	// this login, resolved from the IdP claim->scope mappings (or DefaultScopes)
	// and carried on the issued access token.
	ScopesGranted []string `json:"scopes_granted,omitempty"`
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

	// ID-token validator cache (keyed by providerID). Each entry validates the
	// UPSTREAM IdP's ID token (JWKS/iss/aud), distinct from the quicbaas
	// platformtoken validator.
	idTokenValidators   map[string]*IDTokenValidator
	idTokenValidatorsMu sync.RWMutex

	// now is the clock used for ID-token freshness and JWKS-cache expiry. It is
	// injectable for deterministic tests.
	now func() time.Time

	// providerResolver performs home-realm provider discovery for BeginLoginHRD
	// and for BeginLogin when no provider name is supplied. It is optional: when
	// nil, HRD-driven selection is unavailable and a missing provider name is an
	// error (preserving the legacy explicit-name behaviour).
	providerResolver ProviderResolver

	// bootstrapGate reports whether a bootstrap admin already exists. It guards
	// registration of SYSTEM-tenant (operator) providers so the bootstrap
	// ordering invariant holds on EVERY path that reaches RegisterProvider --
	// not just the OperatorSSORegistrar. When nil, system-tenant registration
	// fails closed (no gate => cannot verify the admin => deny). It is never
	// consulted for non-system tenants, which federate independently.
	bootstrapGate AdminPresenceChecker
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

// WithFederationClock sets the clock used for ID-token freshness and JWKS-cache
// expiry. It is primarily used by tests for deterministic behaviour.
func WithFederationClock(now func() time.Time) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		if now != nil {
			m.now = now
		}
	}
}

// WithProviderResolver sets the home-realm provider resolver used for
// HRD-driven provider selection (BeginLoginHRD and BeginLogin without a provider
// name).
func WithProviderResolver(resolver ProviderResolver) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		m.providerResolver = resolver
	}
}

// WithFederationBootstrapGate sets the admin-presence checker that guards
// SYSTEM-tenant (operator) provider registration. It enforces the bootstrap
// ordering invariant at the manager level so no transport (REST/gRPC/QUIC/MCP)
// can register a system-tenant IdP before a bootstrap admin exists. It is
// optional for non-system tenants; when it is not set, system-tenant
// registration fails closed.
func WithFederationBootstrapGate(gate AdminPresenceChecker) FederationManagerOption {
	return func(m *DefaultFederationManager) {
		m.bootstrapGate = gate
	}
}

// NewFederationManager creates a new federation manager.
func NewFederationManager(opts ...FederationManagerOption) (*DefaultFederationManager, error) {
	m := &DefaultFederationManager{
		httpClient:        http.DefaultClient,
		clientCache:       make(map[string]*oauthClientCacheEntry),
		idTokenValidators: make(map[string]*IDTokenValidator),
		cacheTTL:          1 * time.Hour,
		stateExpiry:       5 * time.Minute,
		now:               time.Now,
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

	// Bootstrap-ordering gate: a SYSTEM-tenant (operator) IdP may only be
	// registered once the local-factor bootstrap admin exists. This is enforced
	// here -- the single chokepoint every transport routes through -- so no path
	// can bypass it. Non-system tenants are unaffected. When no gate is wired we
	// cannot verify the admin, so we fail closed for the system tenant rather
	// than risk locking out the control plane behind a misconfigured IdP.
	if qrdbsdk.IsSystemTenant(provider.TenantID) {
		if err := m.ensureBootstrapReady(ctx); err != nil {
			m.logProviderEvent(ctx, provider, "register", false,
				map[string]interface{}{"error": err.Error()})
			return err
		}
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

// ensureBootstrapReady enforces the bootstrap-ordering invariant for the
// system tenant. It fails closed with ErrBootstrapNotReady when no gate is
// configured or when the gate cannot confirm an admin is present, never opening
// the configuration path on uncertainty.
func (m *DefaultFederationManager) ensureBootstrapReady(ctx context.Context) error {
	if m.bootstrapGate == nil {
		return ErrBootstrapNotReady
	}
	hasAdmin, err := m.bootstrapGate.HasAdmin(ctx)
	if err != nil {
		return NewIAMError(ErrBootstrapNotReady, "iam.federation.register",
			"failed to verify bootstrap admin presence", err)
	}
	if !hasAdmin {
		return ErrBootstrapNotReady
	}
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

// BeginLogin initiates the OAuth2 authorization flow. When providerName is
// supplied the named provider is used (the explicit-name path). When providerName
// is empty, home-realm provider discovery selects the provider WITHIN the given
// tenant (per-tenant default or single-provider shortcut); this requires a
// provider resolver to have been configured.
func (m *DefaultFederationManager) BeginLogin(ctx context.Context, tenantID, providerName, redirectURI string) (*LoginRedirect, error) {
	if tenantID == "" {
		return nil, NewIAMError(ErrFederationStateInvalid, "iam.federation.begin_login",
			"tenant ID is required", nil)
	}

	if providerName == "" {
		// HRD-driven selection within an already-known tenant: select the home
		// provider by per-domain mapping / tenant default / single-provider
		// shortcut, fail-closed when ambiguous.
		if m.providerResolver == nil {
			return nil, NewIAMError(ErrFederationStateInvalid, "iam.federation.begin_login",
				"provider name is required (home-realm discovery is not configured)", nil)
		}
		realm, err := m.providerResolver.ResolveProviderForTenant(ctx, tenantID, RealmHints{}, m)
		if err != nil {
			return nil, NewIAMError(ErrProviderNotFound, "iam.federation.begin_login",
				"home-realm provider discovery failed", err)
		}
		return m.beginLoginFromRealm(ctx, realm, redirectURI)
	}

	// Explicit-name path.
	provider, err := m.providerStorage.GetProviderByName(ctx, tenantID, providerName)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrProviderNotFound
		}
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.begin_login",
			"failed to get provider", err)
	}

	return m.beginLoginForProvider(ctx, provider, redirectURI)
}

// BeginLoginHRD resolves the tenant AND home provider from home-realm discovery
// hints, then begins the login. A resolution to the local realm short-circuits
// to a LoginRedirect carrying LocalRealmProviderID and no AuthURL.
func (m *DefaultFederationManager) BeginLoginHRD(ctx context.Context, hints RealmHints, redirectURI string) (*LoginRedirect, error) {
	if m.providerResolver == nil {
		return nil, NewIAMError(ErrFederationStateInvalid, "iam.federation.begin_login",
			"home-realm provider discovery is not configured", nil)
	}

	realm, err := m.providerResolver.ResolveProvider(ctx, hints, m)
	if err != nil {
		return nil, NewIAMError(ErrProviderNotFound, "iam.federation.begin_login",
			"home-realm provider discovery failed", err)
	}
	return m.beginLoginFromRealm(ctx, realm, redirectURI)
}

// beginLoginFromRealm turns a resolved RealmProvider into a LoginRedirect. A
// local-realm resolution short-circuits to the local-factor sentinel (no OAuth2
// redirect); otherwise the resolved external provider drives the flow.
func (m *DefaultFederationManager) beginLoginFromRealm(ctx context.Context, realm RealmProvider, redirectURI string) (*LoginRedirect, error) {
	// Local realm: route to the tenant's local-factor path. No OAuth2 redirect is
	// produced; the caller drives the local factor stack.
	if realm.IsLocalRealm() {
		return &LoginRedirect{ProviderID: LocalRealmProviderID}, nil
	}

	provider, err := m.providerStorage.GetProvider(ctx, realm.TenantID, realm.ProviderID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrProviderNotFound
		}
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.begin_login",
			"failed to get resolved provider", err)
	}

	return m.beginLoginForProvider(ctx, provider, redirectURI)
}

// beginLoginForProvider performs the OAuth2 authorization-flow setup for an
// already-resolved provider. It is the shared body of BeginLogin and
// BeginLoginHRD.
func (m *DefaultFederationManager) beginLoginForProvider(ctx context.Context, provider *ExternalProvider, redirectURI string) (*LoginRedirect, error) {
	tenantID := provider.TenantID

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

	// Generate a distinct OIDC nonce for ID-token replay protection. The state
	// token above is the CSRF state; the OIDC nonce is echoed back in the ID
	// token's `nonce` claim and checked at callback (OIDC core 3.1.2.7).
	oidcNonce, err := generateSecureNonce(32)
	if err != nil {
		return nil, NewIAMError(ErrFederationStateInvalid, "iam.federation.begin_login",
			"failed to generate OIDC nonce", err)
	}

	// Store state
	now := m.now()
	state := &FederationCallbackState{
		Nonce:       stateNonce,
		OIDCNonce:   oidcNonce,
		ProviderID:  provider.ID,
		TenantID:    tenantID,
		RedirectURI: redirectURI,
		ExpiresAt:   now.Add(m.stateExpiry),
	}

	if err := m.stateStorage.SaveState(ctx, state); err != nil {
		return nil, NewIAMError(ErrFederationStateInvalid, "iam.federation.begin_login",
			"failed to store state", err)
	}

	// Generate authorization URL. Include the OIDC nonce so the IdP echoes it in
	// the ID token.
	authURL := oauthConfig.AuthCodeURL(stateNonce, oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("nonce", oidcNonce))

	return &LoginRedirect{
		AuthURL:    authURL,
		State:      stateNonce,
		ExpiresAt:  state.ExpiresAt,
		ProviderID: provider.ID,
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

	// Check expiry (5-minute window) against the manager clock so freshness is
	// deterministic in tests and consistent with state creation in BeginLogin.
	if m.now().After(state.ExpiresAt) {
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

	// Extract claims from the verified ID token (fail-closed). The state carries
	// the OIDC nonce the ID token must echo.
	claims, err := m.extractClaims(ctx, provider, token, state)
	if err != nil {
		m.logAuthEvent(ctx, state.TenantID, nil, provider, "callback", false,
			map[string]interface{}{"error": err.Error()})
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

	// Extract claims. Account linking presents an access token (not an OIDC
	// authorize result), so there is no ID-token nonce to verify; claims are
	// sourced from the provider userinfo endpoint.
	claims, err := m.extractClaims(ctx, provider, oauthToken, nil)
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

// extractClaims extracts user claims from the OAuth2 token. When an OIDC ID
// token is present it is fully verified (signature via the provider JWKS, exact
// iss, aud containing the client ID, exp/iat freshness, and nonce match against
// the login state) before its claims are trusted. Verification failure is
// fatal and fails closed — there is no fall-through to the unverified payload.
//
// state may be nil only on the account-linking path, which presents an access
// token rather than an OIDC authorize result; in that case claims are sourced
// from the userinfo endpoint.
func (m *DefaultFederationManager) extractClaims(ctx context.Context, provider *ExternalProvider, token *oauth2.Token, state *FederationCallbackState) (map[string]interface{}, error) {
	claims := make(map[string]interface{})

	// Verify and use the ID token when present (OIDC authorize result).
	if idToken, ok := token.Extra("id_token").(string); ok && idToken != "" {
		validator, err := m.idTokenValidator(ctx, provider)
		if err != nil {
			return nil, err
		}

		validated, err := validator.Validate(ctx, idToken)
		if err != nil {
			// Fail closed: a forged/unsigned/expired/wrong-iss/wrong-aud token
			// is rejected outright. No fall-through to an unverified payload.
			return nil, NewIAMError(ErrOIDCTokenExchangeFailed, "iam.federation.claims",
				"ID token verification failed", err)
		}

		// Replay protection: the ID token nonce MUST equal the OIDC nonce we
		// stashed at BeginLogin. Skipped only on the linking path (state == nil),
		// which never carries an ID token via the authorize flow.
		if state != nil && state.OIDCNonce != "" {
			if validated.Nonce != state.OIDCNonce {
				return nil, NewIAMError(ErrOIDCTokenExchangeFailed, "iam.federation.claims",
					"ID token nonce mismatch", &IDTokenNonceError{})
			}
		}

		for k, v := range validated.Raw {
			claims[k] = v
		}
		return claims, nil
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

// idTokenValidator returns a cached per-provider ID-token validator, building it
// (and resolving the JWKS URL from the provider config or OIDC discovery) on
// first use. Each provider has its own issuer/JWKS/clientID.
func (m *DefaultFederationManager) idTokenValidator(ctx context.Context, provider *ExternalProvider) (*IDTokenValidator, error) {
	m.idTokenValidatorsMu.RLock()
	v, ok := m.idTokenValidators[provider.ID]
	m.idTokenValidatorsMu.RUnlock()
	if ok {
		return v, nil
	}

	// Resolve the JWKS URL: explicit override, cached discovery, or fresh
	// discovery from the issuer.
	jwksURL := provider.JWKSURL
	if jwksURL == "" {
		m.clientCacheMu.RLock()
		entry, cached := m.clientCache[provider.ID]
		m.clientCacheMu.RUnlock()
		if cached && entry.discovery != nil {
			jwksURL = entry.discovery.JwksURI
		}
	}
	// If still unknown, the validator discovers it lazily from the issuer.

	validator, err := NewIDTokenValidator(IDTokenValidatorConfig{
		Issuer:     provider.IssuerURL,
		Audience:   provider.ClientID,
		JWKSURL:    jwksURL,
		CacheTTL:   m.cacheTTL,
		HTTPClient: m.httpClient,
		Now:        m.now,
	})
	if err != nil {
		return nil, NewIAMError(ErrProviderInvalid, "iam.federation.idtoken",
			"failed to build ID token validator", err)
	}

	m.idTokenValidatorsMu.Lock()
	if existing, ok := m.idTokenValidators[provider.ID]; ok {
		validator = existing
	} else {
		m.idTokenValidators[provider.ID] = validator
	}
	m.idTokenValidatorsMu.Unlock()

	return validator, nil
}

// processAuthentication handles user provisioning, account linking, and
// per-login role synchronization from the IdP claims.
func (m *DefaultFederationManager) processAuthentication(ctx context.Context, provider *ExternalProvider, claims map[string]interface{}, state *FederationCallbackState) (*FederationResult, error) {
	// Extract claims using mappings
	externalID := m.extractClaim(claims, provider.ClaimMappings.Subject, DefaultSubjectClaim)
	email := m.extractClaim(claims, provider.ClaimMappings.Email, DefaultEmailClaim)
	name := m.extractClaim(claims, provider.ClaimMappings.Name, DefaultNameClaim)

	if externalID == "" {
		return nil, NewIAMError(ErrOIDCClaimMappingFailed, "iam.federation.process",
			"external ID not found in claims", nil)
	}

	// Email-based linking and email-based JIT both key off the IdP-asserted
	// email. When either is in play we enforce the email security controls on
	// the verified IdP claims BEFORE touching any user record (fail-closed):
	//
	//   1. The email claim MUST be present (no silent substitution of the
	//      external subject ID).
	//   2. The IdP MUST report email_verified == true (linking/JIT on an
	//      unverified address is an account-takeover vector).
	//   3. When AllowedEmailDomains is configured, the email's domain MUST be in
	//      the allowlist.
	emailBased := provider.AllowLinking || provider.AutoProvision
	if emailBased {
		if email == "" {
			return nil, NewIAMError(ErrMissingEmailClaim, "iam.federation.process",
				"email claim missing from IdP token", &MissingEmailClaimError{})
		}
		if !claimEmailVerified(claims) {
			return nil, NewIAMError(ErrEmailNotVerified, "iam.federation.process",
				"IdP email is not verified", &EmailNotVerifiedError{Email: email})
		}
		if !emailDomainAllowed(provider.AllowedEmailDomains, email) {
			return nil, NewIAMError(ErrEmailDomainNotAllowed, "iam.federation.process",
				"IdP email domain is not in the provider allowlist",
				&EmailDomainNotAllowedError{Domain: emailDomain(email)})
		}
	}

	// Resolve the tenant roles from the IdP groups/roles claims BEFORE touching
	// any user record. This fails closed: if claim->role mappings are configured
	// but the IdP values match none, the login is denied here (no user is
	// provisioned, no roles are synced).
	resolution, err := resolveRoles(provider, claims)
	if err != nil {
		return nil, NewIAMError(ErrOIDCClaimMappingFailed, "iam.federation.process",
			"federated login denied by claim mapping", err)
	}

	// Fail-closed role posture: when the provider requires an explicit role
	// mapping, a login that resolved to zero roles (no GroupRoleMapping matched
	// AND no DefaultRoles configured) is denied. No zero-role user is ever
	// provisioned or linked.
	if provider.RequireExplicitRoleMapping && len(resolution.RoleIDs) == 0 {
		return nil, NewIAMError(ErrNoRolesResolvable, "iam.federation.process",
			"federated login denied: no roles resolvable", &NoRolesResolvableError{ProviderID: provider.ID})
	}

	// Resolve the platform token scopes from the IdP groups/roles claims, on the
	// same axis as roles. This also fails closed: if claim->scope mappings are
	// configured but the IdP values match none, the login is denied here before
	// any user record is touched.
	scopeResolution, err := resolveScopes(provider, claims)
	if err != nil {
		return nil, NewIAMError(ErrOIDCClaimMappingFailed, "iam.federation.process",
			"federated login denied by scope mapping", err)
	}

	now := m.now()

	// Try to find existing identity link
	identity, err := m.linkStorage.GetLinkByExternal(ctx, provider.ID, externalID)
	if err == nil && identity != nil {
		// User is already linked
		user, err := m.identityStore.GetUser(ctx, state.TenantID, identity.UserID)
		if err != nil {
			return nil, NewIAMError(ErrUserNotFound, "iam.federation.process",
				"linked user not found", err)
		}

		// Re-sync roles from the current IdP groups on every login.
		if err := m.syncUserRoles(ctx, state.TenantID, user.ID, resolution); err != nil {
			return nil, err
		}

		// Update last login and claims
		identity.LastLogin = now
		identity.Updated = now
		identity.Claims = claims
		_ = m.linkStorage.UpdateLink(ctx, identity)

		// Generate tokens
		return m.generateTokenResult(ctx, user, identity, false, scopeResolution)
	}

	// No existing link - try to link by email if allowed
	if provider.AllowLinking && email != "" {
		user, err := m.identityStore.GetUserByEmail(ctx, email)
		if err == nil && user != nil && user.TenantID == state.TenantID {
			// Re-sync roles from the current IdP groups.
			if err := m.syncUserRoles(ctx, state.TenantID, user.ID, resolution); err != nil {
				return nil, err
			}

			// Link to existing user
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

			return m.generateTokenResult(ctx, user, identity, false, scopeResolution)
		}
	}

	// No existing user - provision if allowed
	if !provider.AutoProvision {
		return nil, ErrJITProvisioningDisabled
	}

	// JIT user provisioning with the resolved roles.
	user, err := m.provisionUser(ctx, provider, state.TenantID, externalID, email, name, resolution)
	if err != nil {
		return nil, err
	}

	// Create identity link
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

	return m.generateTokenResult(ctx, user, identity, true, scopeResolution)
}

// syncUserRoles reconciles a user's role assignments to exactly the set the IdP
// claims resolved to. Stale federation-assigned roles are revoked and newly
// resolved roles are assigned. When the resolution used static DefaultRoles
// (no claim->role mappings configured), existing roles are left intact and the
// defaults are additively ensured, preserving locally-granted roles.
func (m *DefaultFederationManager) syncUserRoles(ctx context.Context, tenantID, userID string, resolution *RoleResolution) error {
	if resolution == nil {
		return nil
	}

	if resolution.UsedDefaults {
		// Additive: ensure default roles are present without revoking others.
		for _, roleID := range resolution.RoleIDs {
			if err := m.identityStore.AssignRole(ctx, tenantID, userID, roleID); err != nil {
				return NewIAMError(ErrUserInvalid, "iam.federation.sync_roles",
					"failed to assign default role", err)
			}
		}
		return nil
	}

	// Claim-mapped: the IdP is authoritative. Reconcile to exactly RoleIDs.
	want := make(map[string]struct{}, len(resolution.RoleIDs))
	for _, r := range resolution.RoleIDs {
		want[r] = struct{}{}
	}

	current, err := m.identityStore.GetUserRoles(ctx, tenantID, userID)
	if err != nil {
		return NewIAMError(ErrUserInvalid, "iam.federation.sync_roles",
			"failed to read current roles", err)
	}
	have := make(map[string]struct{}, len(current))
	for _, role := range current {
		have[role.ID] = struct{}{}
		if _, keep := want[role.ID]; !keep {
			if err := m.identityStore.RevokeRole(ctx, tenantID, userID, role.ID); err != nil {
				return NewIAMError(ErrUserInvalid, "iam.federation.sync_roles",
					"failed to revoke stale role", err)
			}
		}
	}
	for roleID := range want {
		if _, ok := have[roleID]; ok {
			continue
		}
		if err := m.identityStore.AssignRole(ctx, tenantID, userID, roleID); err != nil {
			return NewIAMError(ErrUserInvalid, "iam.federation.sync_roles",
				"failed to assign mapped role", err)
		}
	}
	return nil
}

// provisionUser creates a new user from external identity claims and assigns the
// roles resolved from the IdP claim mappings (or DefaultRoles fallback).
func (m *DefaultFederationManager) provisionUser(ctx context.Context, provider *ExternalProvider, tenantID, externalID, email, name string, resolution *RoleResolution) (*User, error) {
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

	// Assign the resolved roles. Failure here is fatal: a provisioned user with
	// the wrong roles is a privilege-boundary violation, so we fail closed.
	if err := m.syncUserRoles(ctx, tenantID, user.ID, resolution); err != nil {
		return nil, err
	}

	// Assign default groups
	for _, groupID := range provider.DefaultGroups {
		if err := m.identityStore.AddUserToGroup(ctx, tenantID, groupID, user.ID); err != nil {
			return nil, NewIAMError(ErrUserInvalid, "iam.federation.provision",
				"failed to assign default group", err)
		}
	}

	return user, nil
}

// generateTokenResult generates a FederationResult with QuicBaaS tokens. The
// resolved platform scopes (from the IdP claim->scope mappings or DefaultScopes)
// are unioned with the base OIDC scopes and carried onto the issued access token
// so the `scp` catalog reflects the tenant's claim mappings.
func (m *DefaultFederationManager) generateTokenResult(ctx context.Context, user *User, identity *FederatedIdentity, provisioned bool, scopeResolution *ScopeResolution) (*FederationResult, error) {
	scopes := federationTokenScopes(scopeResolution)

	// Generate QuicBaaS tokens using OIDC provider if available
	if m.oidcProvider != nil {
		tokenResp, err := m.oidcProvider.generateTokenResponse(ctx, user, "", scopes, "", "", nil)
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
			ScopesGranted:     scopes,
		}, nil
	}

	// Return result without tokens if no OIDC provider
	return &FederationResult{
		User:              user,
		FederatedIdentity: identity,
		Provisioned:       provisioned,
		ScopesGranted:     scopes,
	}, nil
}

// federationBaseScopes are the OIDC scopes always present on a federated login's
// issued token. Resolved platform scopes are unioned on top of these.
var federationBaseScopes = []string{"openid", "profile", "email"}

// federationTokenScopes unions the base OIDC scopes with the resolved platform
// scopes, de-duplicated and order-stable (base scopes first). A nil resolution
// yields just the base scopes.
func federationTokenScopes(resolution *ScopeResolution) []string {
	seen := make(map[string]struct{}, len(federationBaseScopes))
	out := make([]string, 0, len(federationBaseScopes))
	add := func(scope string) {
		if scope == "" {
			return
		}
		if _, ok := seen[scope]; ok {
			return
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	for _, s := range federationBaseScopes {
		add(s)
	}
	if resolution != nil {
		for _, s := range resolution.Scopes {
			add(s)
		}
	}
	return out
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

// invalidateClientCache removes a provider from the OAuth client cache and the
// ID-token validator cache so config changes (issuer/JWKS/clientID) take effect.
func (m *DefaultFederationManager) invalidateClientCache(providerID string) {
	m.clientCacheMu.Lock()
	delete(m.clientCache, providerID)
	m.clientCacheMu.Unlock()

	m.idTokenValidatorsMu.Lock()
	delete(m.idTokenValidators, providerID)
	m.idTokenValidatorsMu.Unlock()
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
