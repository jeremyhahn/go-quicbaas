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

package server

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	ggrpc "google.golang.org/grpc"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

type iamComponents struct {
	tenantManager     iam.TenantManager
	identityStore     *iam.IdentityStore
	policyEngine      *iam.PolicyEngine
	appManager        *iam.AppManager
	appStorage        iam.AppStorage
	oidcProvider      *iam.OIDCProvider
	federationManager iam.FederationManager
	mfaManager        *iam.DefaultMFAManager
	webauthnManager   *iam.XKMSWebAuthnManager
	auditLogger       iam.AuditLogger
	adminManager      *iam.AdminManager
	gdprManager       *iam.GDPRManager
	sessionStorage    iam.SessionStorage
	sessionManager    iam.SessionManager
	authManager       *inMemoryAuthManagerAdapter
	inviteStore       iam.InviteStore
}

// inMemoryAuthManagerAdapter wraps InMemoryAuthManager to implement service.AuthManagerInterface.
type inMemoryAuthManagerAdapter struct {
	manager *auth.InMemoryAuthManager
}

// Authenticate validates credentials and issues authentication tokens.
// Ownership of password transfers to the underlying manager, which
// destroys the buffer on every return path.
func (a *inMemoryAuthManagerAdapter) Authenticate(ctx context.Context, username string, password *secure.Buffer) (*auth.Token, error) {
	return a.manager.Authenticate(ctx, username, password)
}

// ValidateToken validates a token and returns validation information.
func (a *inMemoryAuthManagerAdapter) ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error) {
	return a.manager.ValidateToken(ctx, token)
}

// RefreshToken generates a new access token from a refresh token.
func (a *inMemoryAuthManagerAdapter) RefreshToken(ctx context.Context, refreshToken string) (*auth.Token, error) {
	return a.manager.RefreshToken(ctx, refreshToken)
}

// RevokeToken revokes an authentication token.
func (a *inMemoryAuthManagerAdapter) RevokeToken(ctx context.Context, token string) error {
	return a.manager.RevokeToken(ctx, token)
}

// CreateUser creates a new user with specified roles.
func (a *inMemoryAuthManagerAdapter) CreateUser(username, password string, roles []string) (*auth.User, error) {
	return a.manager.CreateUser(username, password, roles)
}

// IsPasswordAuthEnabled returns whether password authentication is enabled.
func (a *inMemoryAuthManagerAdapter) IsPasswordAuthEnabled() bool {
	return true // The manager already validates this internally in Authenticate
}

// interfacesAuthManagerAdapter wraps InMemoryAuthManager to implement interfaces.AuthManager.
// This adapter converts the concrete return types to interface{} as required by interfaces.AuthManager.
type interfacesAuthManagerAdapter struct {
	manager *auth.InMemoryAuthManager
}

// Compile-time check that interfacesAuthManagerAdapter implements interfaces.AuthManager.
var _ interfaces.AuthManager = (*interfacesAuthManagerAdapter)(nil)

// Authenticate validates credentials and issues authentication tokens.
// Ownership of password transfers to the underlying manager, which
// destroys the buffer on every return path.
func (a *interfacesAuthManagerAdapter) Authenticate(ctx context.Context, username string, password *secure.Buffer) (interface{}, error) {
	return a.manager.Authenticate(ctx, username, password)
}

// IssueTokenForUser issues JWT tokens for a user who has been authenticated.
func (a *interfacesAuthManagerAdapter) IssueTokenForUser(ctx context.Context, user interfaces.UserInfo) (interface{}, error) {
	return a.manager.IssueTokenForUser(ctx, user)
}

// Authorize checks if a token has permission to perform an action on a resource.
func (a *interfacesAuthManagerAdapter) Authorize(ctx context.Context, token, resource, action string) error {
	return a.manager.Authorize(ctx, token, resource, action)
}

// RefreshToken generates a new access token from a refresh token.
func (a *interfacesAuthManagerAdapter) RefreshToken(ctx context.Context, refreshToken string) (interface{}, error) {
	return a.manager.RefreshToken(ctx, refreshToken)
}

// RevokeToken revokes an authentication token.
func (a *interfacesAuthManagerAdapter) RevokeToken(ctx context.Context, token string) error {
	return a.manager.RevokeToken(ctx, token)
}

// ValidateToken validates a token and returns validation information.
func (a *interfacesAuthManagerAdapter) ValidateToken(ctx context.Context, token string) (interface{}, error) {
	return a.manager.ValidateToken(ctx, token)
}

// CreateUser creates a new user with specified roles.
func (a *interfacesAuthManagerAdapter) CreateUser(username, password string, roles []string) (interface{}, error) {
	return a.manager.CreateUser(username, password, roles)
}

// GetUserStore returns the user store for user management operations.
func (a *interfacesAuthManagerAdapter) GetUserStore() interfaces.UserStore {
	return a.manager.GetUserStore()
}

// GetRoleManager returns the role manager for RBAC operations.
func (a *interfacesAuthManagerAdapter) GetRoleManager() interfaces.RoleManager {
	return a.manager.GetRoleManager()
}

// Compile-time check that iamComponents implements service.IAMComponents
var _ service.IAMComponents = (*iamComponents)(nil)

// GetTenantManager returns the tenant manager.
func (c *iamComponents) GetTenantManager() iam.TenantManager {
	return c.tenantManager
}

// GetIdentityStore returns the identity store.
func (c *iamComponents) GetIdentityStore() *iam.IdentityStore {
	return c.identityStore
}

// GetPolicyEngine returns the policy engine.
func (c *iamComponents) GetPolicyEngine() *iam.PolicyEngine {
	return c.policyEngine
}

// GetAppManager returns the app manager.
func (c *iamComponents) GetAppManager() *iam.AppManager {
	return c.appManager
}

// GetAppStorage returns the underlying OAuth2 application storage backend.
// Used by mTLS authenticators (self_signed_tls_client_auth, tls_client_auth)
// that require direct storage access outside the AppManager abstraction.
func (c *iamComponents) GetAppStorage() iam.AppStorage {
	return c.appStorage
}

// GetOIDCProvider returns the OIDC provider.
func (c *iamComponents) GetOIDCProvider() *iam.OIDCProvider {
	return c.oidcProvider
}

// GetFederationManager returns the federation manager for external identity providers.
func (c *iamComponents) GetFederationManager() iam.FederationManager {
	return c.federationManager
}

// OperatorSSORegistrar returns the registrar used to configure operator (system
// tenant) external IdPs. Operator SSO is an ADDITIVE login path: operators MAY
// log into quicbaas via an OIDC provider registered for the system tenant, but
// the local-factor (mTLS/WebAuthn/PIV) admin login remains the always-available
// bootstrap + break-glass path and never routes through this registrar.
//
// The registrar enforces the bootstrap-ordering invariant: a system-tenant IdP
// can only be configured once the bootstrap admin exists (no admin -> can't
// configure Okta). It returns an error when federation is unavailable or the
// admin manager is absent.
func (c *iamComponents) OperatorSSORegistrar() (*iam.OperatorSSORegistrar, error) {
	if c.federationManager == nil {
		return nil, &IAMComponentInitError{Component: "operator SSO registrar (federation unavailable)"}
	}
	if c.adminManager == nil {
		return nil, &IAMComponentInitError{Component: "operator SSO registrar (admin manager unavailable)"}
	}
	return iam.NewOperatorSSORegistrar(c.federationManager, c.adminManager)
}

// GetMFAManager returns the MFA manager.
func (c *iamComponents) GetMFAManager() service.MFAManager {
	return c.mfaManager
}

// GetWebAuthnManager returns the WebAuthn manager. The XKMSWebAuthnManager
// type implements service.WebAuthnManager directly; no separate adapter is
// needed since the multi-tenant xkms factory keeps the protocol-vs-map
// conversion contained inside the manager itself.
func (c *iamComponents) GetWebAuthnManager() service.WebAuthnManager {
	if c.webauthnManager == nil {
		return nil
	}
	return c.webauthnManager
}

// GetAuditLogger returns the audit logger.
func (c *iamComponents) GetAuditLogger() iam.AuditLogger {
	return c.auditLogger
}

// GetAdminManager returns the admin manager for delegated administration.
func (c *iamComponents) GetAdminManager() *iam.AdminManager {
	return c.adminManager
}

// GetGDPRManager returns the GDPR compliance manager.
func (c *iamComponents) GetGDPRManager() *iam.GDPRManager {
	return c.gdprManager
}

// GetSessionStorage returns the session storage.
func (c *iamComponents) GetSessionStorage() iam.SessionStorage {
	return c.sessionStorage
}

// GetSessionManager returns the session manager.
func (c *iamComponents) GetSessionManager() iam.SessionManager {
	return c.sessionManager
}

// GetAuthManager returns the authentication manager for password-based login.
// Returns nil if password authentication is not configured.
func (c *iamComponents) GetAuthManager() service.AuthManagerInterface {
	if c.authManager == nil {
		return nil
	}
	return c.authManager
}

// GetInviteStore returns the invite token store.
func (c *iamComponents) GetInviteStore() iam.InviteStore {
	return c.inviteStore
}

// GetInternalAuthManager returns the interfaces.AuthManager for internal service use.
// This is used to wire up the auth manager in the QuicBaaS service for MCP transport
// and other internal operations that require the full interfaces.AuthManager interface.
// Returns nil if password authentication is not configured.
func (c *iamComponents) GetInternalAuthManager() interfaces.AuthManager {
	if c.authManager == nil {
		return nil
	}
	// Return an adapter that wraps the underlying manager and implements interfaces.AuthManager.
	// This adapter converts concrete return types to interface{} as required by the interface.
	return &interfacesAuthManagerAdapter{manager: c.authManager.manager}
}

// initializeIAMComponents creates and initializes all IAM components.
// Returns nil if the qrdb client is not available (e.g., in standalone/embedded
// mode without storage initialization).
func (s *Server) initializeIAMComponents() (service.IAMComponents, error) {
	// Check if qrdb client is available before attempting IAM init.
	// In standalone/embedded mode, the qrdb client may not be initialized
	// if storage was skipped (BaaS client mode).
	if s.qrdbClient == nil {
		s.logger.Info("Skipping IAM initialization: qrdb client not available")
		return nil, nil
	}

	s.logger.Info("Initializing IAM components...")

	// Build the IAM KVStore. When the qrdb node exposes a per-tenant
	// StoreResolver (seal enabled with a barrier registry), every IAM
	// operation is routed to the store backing the context tenant: a
	// barrier-isolated tenant's IAM data (policies, apps, users, groups,
	// roles, sessions, tokens, invites, admin assignments) lands in that
	// tenant's own TenantBarrier-backed store (own DEK + namespace), while a
	// prefix / system / legacy tenant uses the shared store. A sealed barrier
	// tenant fails closed (tenant.ErrTenantSealed) with no shared fallback.
	// When no resolver is available (seal disabled) the store is the shared
	// platform shard, preserving prior single-store behaviour.
	//
	// Routing is done at the KVStore seam so the IAM BackendStorage (and the
	// Admin / Impersonation / Invite Backend stores) need no interface change:
	// they each see one interfaces.KVStore. The platform shard is the
	// canonical IAM shard per the architecture docs (qbshards.PlatformShardID).
	iamKV := s.iamTenantKVStore()

	// Create the persistent IAM storage. The store satisfies TenantStorage,
	// UserStorage, GroupStorage, RoleStorage, PolicyStorage, AppStorage, and
	// TokenStorage so every IAM mutation survives daemon restarts.
	backendStorage := iam.NewBackendStorage(iamKV)

	// Create tenant manager backed by the persistent IAM storage
	tenantManager := iam.NewDefaultTenantManager(backendStorage)

	// Wire the qrdb engine's TenantServicer so CreateTenant/DeleteTenant
	// initialise barrier, DEK, and quotas on the engine side BEFORE BaaS
	// metadata is written. This is the correctness gap identified in Phase D.5:
	// without this wiring, per-tenant barrier init and DEK lifecycle never fire.
	if s.qrdbNode != nil {
		if svcs := s.qrdbNode.Services(); svcs != nil && svcs.Tenant != nil {
			tenantManager.SetQRDBTenantProvisioner(newQRDBTenantProvisionerAdapter(svcs.Tenant))
			s.logger.Info("qrdb TenantServicer wired into IAM tenant lifecycle (barrier+DEK+quota)")
		}
	}

	// Create identity store with storage backends
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  backendStorage,
		GroupStorage: backendStorage,
		RoleStorage:  backendStorage,
		Tenants:      tenantManager,
	})

	// Create policy cache and engine
	policyCache := iam.NewPolicyCache(&iam.CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           10000,
		BloomFilterSize:   1000000,
		BloomFilterHashes: 7,
	})
	policyEngine := iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage: backendStorage,
		Cache:   policyCache,
	})

	// Create app manager
	appManager := iam.NewAppManager(backendStorage, tenantManager)

	// Create audit logger backed by the persistent, per-tenant KVStore so audit
	// events survive daemon restarts and are encrypted-at-rest in the owning
	// tenant's barrier (a barrier-isolated tenant's events live in its own
	// store, never a global table). This mirrors the session/MFA backend stores
	// above; the in-memory store remains the NewAuditLogger default for
	// non-backend contexts (e.g. tests, embedded callers without a KVStore).
	auditLogger, err := iam.NewAuditLogger(
		iam.WithAuditStorage(iam.NewBackendAuditStorage(iamKV)),
	)
	if err != nil {
		return nil, &IAMComponentInitError{Component: "audit logger", Cause: err}
	}

	// Create session storage (WebAuthn sessions) and extended session manager.
	// Both route through the per-tenant KVStore so a barrier-isolated tenant's
	// sessions are stored in its own barrier; the tenant manager enables
	// per-tenant expiry sweeps without a global cross-tenant scan.
	sessionStorage := iam.NewBackendSessionStorage(iamKV, tenantManager)
	sessionExtStorage := iam.NewBackendSessionExtStorage(iamKV, tenantManager)
	sessionManager, err := iam.NewSessionManager(
		iam.WithSessionStorage(sessionExtStorage),
	)
	if err != nil {
		return nil, &IAMComponentInitError{Component: "session manager", Cause: err}
	}

	// Create the OIDC provider with an xkms/KMS/HSM-backed ES256 signing key.
	// The key manager performs get-or-create against xkms (the key persists in
	// the configured keystore across restarts), derives a stable kid from the
	// key's public SPKI, and supports rotation with a JWKS overlap window.
	// If xkms is unavailable or the key cannot be built we log a warning and
	// leave the OIDC provider nil (graceful degradation) rather than panic.
	var oidcProvider *iam.OIDCProvider
	signingKeyManager, err := iam.NewSigningKeyManagerFromXKMS(iam.OIDCSigningKeyCN, "")
	if err != nil {
		s.logger.WithError(err).Warn("Failed to initialize xkms-backed OIDC signing key, OIDC provider will be unavailable")
	} else {
		issuerURL := s.getIssuerURL()
		// Per-tenant signing-key resolver: dedicated tenants are signed with
		// their own ES256 key (own kid) under the same single broker issuer;
		// every other tenant uses the global key. The aggregated JWKS publishes
		// every active kid. Cache rebuilds when a tenant's settings change.
		tenantSigningKeys, tkErr := iam.NewTenantSigningKeyResolver(
			signingKeyManager, iam.NewTenantManagerOIDCSettings(tenantManager))
		if tkErr != nil {
			s.logger.WithError(tkErr).Warn("iam.oidc: per-tenant signing-key resolver unavailable; using global key only")
		} else {
			tenantManager.AddSettingsObserver(tenantSigningKeys)
		}
		oidcProvider, err = iam.NewOIDCProvider(
			&iam.OIDCConfig{
				Issuer:            issuerURL,
				SigningAlgorithm:  iam.SigningAlgorithmES256,
				SigningKeyManager: signingKeyManager,
				TenantSigningKeys: tenantSigningKeys,
				// Phase 6: the platform access token is short-lived (5-15 min
				// window). TTLs are config-wired via AuthConfig and default to
				// 15m access / 7d refresh / 1h id / 10m auth-code when unset;
				// the access TTL is bound-checked by the config validator.
				AccessTokenTTL:  s.config.Auth.OIDCAccessTokenTTL(),
				RefreshTokenTTL: s.config.Auth.OIDCRefreshTokenTTL(),
				IDTokenTTL:      s.config.Auth.OIDCIDTokenTTL(),
				AuthCodeTTL:     s.config.Auth.OIDCAuthCodeTTL(),
				PKCERequired:    true,
				AllowedMethods:  []string{"S256"},
			},
			iam.OIDCDependencies{
				IdentityStore: identityStore,
				AppStorage:    backendStorage,
				TokenStorage:  backendStorage,
			},
		)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to create OIDC provider, OIDC features will be unavailable")
			oidcProvider = nil
		}
	}

	// Create MFA manager with in-memory storage
	mfaManager, err := iam.NewMFAManager(
		iam.WithMFAStorage(iam.NewBackendMFAStorage(iamKV, tenantManager)),
		iam.WithUserStorage(backendStorage),
	)
	if err != nil {
		return nil, &IAMComponentInitError{Component: "MFA manager", Cause: err}
	}

	// Build the multi-tenant xkms-backed WebAuthn manager. The qrdb client
	// is required — when unavailable the manager stays nil and the WebAuthn
	// surface returns 503-equivalents (handled at the REST layer).
	var webauthnMgr *iam.XKMSWebAuthnManager
	if s.qrdbClient != nil {
		platformQRDB, qrdbErr := s.qrdbClient.ForShard(qbshards.PlatformShardID)
		if qrdbErr != nil {
			s.logger.WithError(qrdbErr).Warn("iam.webauthn: qrdb client unavailable; WebAuthn surface disabled")
		} else {
			factoryParams := iam.WebAuthnFactoryParams{
				Config:   webauthnFactoryConfigFromOperator(s.config.IAM.WebAuthn),
				QRDB:     platformQRDB,
				Settings: iam.NewTenantManagerWebAuthnSettings(tenantManager),
			}
			factory, fErr := iam.NewWebAuthnServiceFactory(factoryParams)
			if fErr != nil {
				s.logger.WithError(fErr).Warn("iam.webauthn: factory construction failed; WebAuthn surface disabled")
			} else {
				// Rebuild a tenant's cached WebAuthn RP service when its
				// settings change (e.g. RPID/origin update).
				tenantManager.AddSettingsObserver(factory)
				mgr, mErr := iam.NewXKMSWebAuthnManager(factory)
				if mErr != nil {
					s.logger.WithError(mErr).Warn("iam.webauthn: manager construction failed; WebAuthn surface disabled")
				} else {
					webauthnMgr = mgr
					if aaguidAllowlist, aaguidErr := buildAAGUIDAllowlist(s.config.IAM.AAGUID); aaguidErr != nil {
						s.logger.WithError(aaguidErr).Warn("iam.aaguid: failed to build AAGUID allowlist; enforcement disabled")
					} else if aaguidAllowlist != nil {
						webauthnMgr.SetAAGUIDAllowlist(aaguidAllowlist)
						s.logger.Infof("iam.aaguid: allowlist loaded (mode=%s, entries=%d)",
							s.config.IAM.AAGUID.Mode, aaguidAllowlist.Len())
					}
				}
			}
		}
	}

	// Create admin manager. AdminStorage is persistent because firstBootBootstrap
	// uses ListAssignments to detect whether init has already run — switching
	// back to an in-memory store would break the idempotency contract across
	// restarts and cause duplicate bootstrap admin users.
	adminManager, err := iam.NewAdminManager(iam.AdminManagerOptions{
		AdminStorage:         iam.NewBackendAdminStorage(iamKV),
		ImpersonationStorage: iam.NewBackendImpersonationStorage(iamKV),
		AuditLogger:          auditLogger,
	})
	if err != nil {
		return nil, &IAMComponentInitError{Component: "admin manager", Cause: err}
	}

	// Create GDPR manager
	gdprManager, err := iam.NewGDPRManager(iam.GDPRManagerDependencies{
		ConsentStorage:    iam.NewBackendConsentStorage(iamKV, tenantManager),
		DataExportStorage: iam.NewBackendDataExportStorage(iamKV, tenantManager),
		DeletionStorage:   iam.NewBackendDeletionStorage(iamKV, tenantManager),
		UserStorage:       backendStorage,
		SessionStorage:    sessionStorage,
		GroupStorage:      backendStorage,
		RoleStorage:       backendStorage,
	})
	if err != nil {
		return nil, &IAMComponentInitError{Component: "GDPR manager", Cause: err}
	}

	// Create federation manager
	var fedMgr iam.FederationManager
	fedMgr, err = iam.NewFederationManager(
		iam.WithProviderStorage(iam.NewBackendProviderStorage(iamKV)),
		iam.WithLinkStorage(iam.NewBackendFederationStorage(iamKV)),
		iam.WithStateStorage(iam.NewBackendStateStorage(iamKV, tenantManager)),
		iam.WithFederationIdentityStore(identityStore),
		iam.WithFederationOIDCProvider(oidcProvider),
		iam.WithFederationAuditLogger(auditLogger),
		// Push the bootstrap-ordering gate DOWN into RegisterProvider so no
		// transport can register a system-tenant operator IdP before the
		// bootstrap admin exists. The AdminManager (built above) satisfies the
		// AdminPresenceChecker via HasAdmin.
		iam.WithFederationBootstrapGate(adminManager),
	)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to create federation manager, federation features will be unavailable")
		fedMgr = nil
	}

	// Create the shared fleet registry so orchestration has a fleet view
	// from day one. Registration now flows through the mTLS /api/v1/agents/
	// register endpoint (pkg/api/rest/handlers_agent_register.go) which
	// publishes into this same registry.
	if s.agentRegistry == nil {
		s.agentRegistry = orchestration.NewInMemoryAgentRegistry()
	}

	// Ensure the default tenant exists BEFORE any subsystem that registers
	// apps/agents inside it (e.g. the orchestrator daemon client below).
	// Ordering is load-bearing: the daemon-client bootstrap registers a
	// per-tenant OAuth2 app and needs the tenant row in place first.
	tenantCtx := context.Background()
	if _, err := tenantManager.GetTenant(tenantCtx, "default"); err != nil {
		if _, createErr := tenantManager.CreateTenant(tenantCtx, iam.CreateTenantOptions{
			ID:   "default",
			Name: "Default Tenant",
		}); createErr != nil {
			s.logger.WithError(createErr).Warn("Failed to create default tenant, some features may be unavailable")
		} else {
			s.logger.Info("Bootstrap default tenant created")
		}
	}

	// Seed the well-known "admin" role in the default tenant. The bootstrap
	// admin user is created with role label "admin" but the IAM Role store
	// is a separate index — without a corresponding Role row, the Roles tab
	// in the UI shows "No roles defined" while the Users tab shows the
	// admin role. Creating the role here keeps the two surfaces consistent
	// and gives operators a target object to extend with permissions.
	// Idempotent: a duplicate role creation is swallowed.
	if err := ensureAdminRole(tenantCtx, identityStore, "default"); err != nil {
		s.logger.WithError(err).Warn("Failed to seed default admin role; Roles tab may show empty until a role is created")
	}

	// Bootstrap the orchestrator's own OAuth2 client for outbound daemon
	// RPCs. The orchestrator presents a JWT minted from these credentials
	// to every daemon it dispatches work to. Stored on the Server so the
	// orchestration lifecycle can wire it into the RemoteBackendResolver.
	orchClient, orchErr := s.ensureOrchestratorDaemonClient(context.Background(), appManager)
	if orchErr != nil {
		s.logger.WithError(orchErr).Warn("Failed to bootstrap orchestrator daemon client — remote daemon dispatch will be disabled until this succeeds")
	} else {
		s.orchestratorDaemonClient = orchClient
	}

	// InviteStore must be persistent: an operator may redeem the first-boot
	// activation URL minutes or hours after the daemon starts, possibly across
	// process restarts.
	inviteStore := iam.NewBackendInviteStore(iamKV)

	components := &iamComponents{
		tenantManager:     tenantManager,
		identityStore:     identityStore,
		policyEngine:      policyEngine,
		appManager:        appManager,
		appStorage:        backendStorage,
		oidcProvider:      oidcProvider,
		federationManager: fedMgr,
		mfaManager:        mfaManager,
		webauthnManager:   webauthnMgr,
		auditLogger:       auditLogger,
		adminManager:      adminManager,
		gdprManager:       gdprManager,
		sessionStorage:    sessionStorage,
		sessionManager:    sessionManager,
		inviteStore:       inviteStore,
	}

	// Configure password-based authentication if enabled
	if s.config.Auth.AllowPasswordAuth || s.config.Auth.InsecureDevMode {
		username, password := s.resolveBootstrapCredentials()
		if err := validateBootstrapCredentials(username, password, s.config.Auth.InsecureDevMode); err != nil {
			return nil, err
		}

		authCfg := &auth.AuthConfig{
			Enabled:              true,
			JWTSecret:            s.config.Auth.JWTSecret,
			JWTSigningKeyID:      s.config.Auth.JWTSigningKeyID,
			JWTSigningAlgorithm:  s.config.Auth.JWTSigningAlgorithm,
			TokenTTL:             s.config.Auth.TokenTTL,
			RefreshTokenTTL:      s.config.Auth.RefreshTokenTTL,
			AllowPasswordAuth:    s.config.Auth.AllowPasswordAuth,
			InsecureDevMode:      s.config.Auth.InsecureDevMode,
			MaxLoginAttempts:     s.config.Auth.MaxLoginAttempts,
			LoginLockoutDuration: s.config.Auth.LoginLockoutDuration,
		}

		authMgr, err := auth.NewInMemoryAuthManager(authCfg, s.logger)
		if err != nil {
			return nil, &IAMComponentInitError{Component: "auth manager", Cause: err}
		}

		// Create bootstrap admin user
		if _, err := authMgr.CreateUser(username, password, []string{"admin"}); err != nil {
			return nil, &IAMComponentInitError{Component: "bootstrap admin user", Cause: err}
		}

		components.authManager = &inMemoryAuthManagerAdapter{manager: authMgr}
		s.logger.Info("Password authentication enabled with bootstrap admin user")
	}

	// (Default tenant was created earlier, before the orchestrator
	// daemon-client bootstrap that depends on it.)

	// First-boot bootstrap: when no admin assignments exist, create a
	// pending admin user and print an activation invite so the operator
	// can register their FIDO2 / PIV credential. Every QuicBaaS server
	// needs an authenticated admin regardless of whether the encrypted
	// seal barrier is in use — access control and storage encryption
	// are independent concerns.
	//
	// The bootstrap is skipped only when another path already provisions
	// the admin: AllowPasswordAuth (legacy password admin), InsecureDevMode
	// (dev-mode password admin), or BootstrapAdminPIVCertPath (non-interactive
	// PIV pre-provisioning). The PIV path is independent of the others and
	// always runs when configured.
	pivBootstrap := s.config.Auth.BootstrapAdminPIVCertPath != ""
	skipBootstrap := (s.config.Auth.AllowPasswordAuth ||
		s.config.Auth.InsecureDevMode) && !pivBootstrap
	if !skipBootstrap {
		if bootstrapErr := s.firstBootBootstrap(context.Background(), components); bootstrapErr != nil {
			// Non-fatal: log the error but do not abort the server. The operator
			// can fix the cause (e.g., corrupt storage) and restart.
			s.logger.WithError(bootstrapErr).Warn("First-boot bootstrap failed — no admin credential will be created this boot")
		}
	}

	s.logger.Info("IAM components initialized successfully")
	return components, nil
}

// firstBootBootstrapTTL is the lifetime of the one-time activation invite
// that is printed to stderr on first boot.
const firstBootBootstrapTTL = 24 * time.Hour

// printFirstBootBannerFn is the function used to emit the activation banner.
// It is a package-level variable so unit tests can capture the output without
// touching os.Stderr.
var printFirstBootBannerFn = func(w interface{ WriteString(string) (int, error) }, url string) {
	printFirstBootBanner(w, url)
}

// firstBootBootstrap detects a fresh installation (zero admin assignments)
// and prints the three-option ceremony banner used by Phase 11 of the
// unification plan. No invite is stored: the operator chooses one of the
// three documented bootstrap paths and the resulting admin record is created
// when the chosen path completes (CLI ceremony, remote ceremony, or WebAuthn
// invite redemption).
//
// The three paths printed by the banner:
//
//	(a) Local: `quicbaasd init --barrier-strategy ... --so alice:csr:/path`
//	    runs the init ceremony in-process. On completion the SO certificate's
//	    SPKI thumbprint is bound to the bootstrap admin via
//	    BindAdminToSOCert; an mTLS handshake presenting the SO cert is then
//	    recognized as the admin user.
//
//	(b) Remote: `quicbaas init claim-cert/claim-share` walks the operator
//	    through claiming a server-issued certificate from a separate
//	    machine. The CLI is owned by the Phase 12 split-CLI agent.
//
//	(c) WebAuthn invite URL: kept as a tertiary path for browser-only
//	    operators. The activation URL is generated and printed here; the
//	    invite remains valid for firstBootBootstrapTTL.
//
// On every subsequent boot the admin assignment exists so the function is
// a no-op.
func (s *Server) firstBootBootstrap(ctx context.Context, c *iamComponents) error {
	// Detect first boot: any active admin assignment means a prior bootstrap ran.
	//
	// On a fresh cluster, IAM init runs before raft consensus is established, so
	// this read — and the bootstrap writes that follow — initially fail with
	// "not leader, leader unknown". Wait for a leader to serve the read (bounded
	// by firstBootLeaderWait) so the one-time admin bootstrap is not silently
	// skipped on an otherwise-healthy cluster. Once the read succeeds a leader
	// exists, so the subsequent FirstBootInitialize / PIV-bind writes also
	// commit through raft.
	assignments, err := s.waitForAdminAssignmentsCheck(ctx, c)
	if err != nil {
		return &FirstBootBootstrapError{Op: "list admin assignments", Cause: err}
	}
	if len(assignments) > 0 {
		// Admin already exists — nothing to do.
		return nil
	}

	// Delegate tenant + role + admin-user creation to qrdb's canonical
	// first-boot. This is the SOLE entry point that creates the system
	// tenant ("system"), registers the six built-in roles, and creates
	// the platform admin user with Admin@system grant (qrdb's RBAC
	// system-tenant rule promotes that to cross-tenant authority).
	// quicbaas IAM-specific concerns (admin assignments, PIV/SO cert
	// binding) layer on top after first-boot returns. Consumed via the qrdb
	// SDK (never pkg/firstboot directly).
	if _, err := qrdbsdk.FirstBootInitialize(ctx, qrdbsdk.FirstBootDependencies{
		Tenants: qrdbadapter.WrapTenant(c.tenantManager),
		Roles:   qrdbadapter.WrapRole(c.identityStore),
		Users:   qrdbadapter.WrapUser(c.identityStore),
		Logger:  slog.Default(),
	}); err != nil {
		return &FirstBootBootstrapError{Op: "firstboot.Initialize", Cause: err}
	}

	// Look up the bootstrap admin user first-boot just created (or
	// confirmed already exists). The adapter synthesizes the email
	// deterministically; the lookup is reliable.
	adminTenantID := qrdbsdk.SystemTenantID
	adminEmail := "admin@" + adminTenantID + ".qrdb.local"
	user, err := c.identityStore.GetUserByEmail(ctx, adminEmail)
	if err != nil {
		return &FirstBootBootstrapError{Op: "lookup bootstrap admin user", Cause: err}
	}

	// Grant the user a super_admin assignment so they can issue further invites
	// and manage the system after completing activation.
	_, err = c.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    user.ID,
		AdminRole: iam.AdminRoleSuperAdmin,
		TenantIDs: nil, // global / system-level
		GrantedBy: "system:first-boot",
	})
	if err != nil {
		return &FirstBootBootstrapError{Op: "create admin assignment", Cause: err}
	}

	// Generate and store the one-time invite token used by path (c).
	// Paths (a) and (b) do not need the invite — they bind via the SO
	// cert SPKI thumbprint instead.
	token, err := iam.GenerateInviteToken()
	if err != nil {
		return &FirstBootBootstrapError{Op: "generate invite token", Cause: err}
	}

	now := time.Now()
	expiresAt := now.Add(firstBootBootstrapTTL)
	if err := c.inviteStore.Create(ctx, &iam.InviteRecord{
		TokenHash: iam.TokenHashExported(token),
		UserID:    user.ID,
		TenantID:  adminTenantID,
		ExpiresAt: expiresAt,
	}); err != nil {
		return &FirstBootBootstrapError{Op: "store invite token", Cause: err}
	}

	// Build the activation URL from the server's issuer URL for path (c).
	activationURL := s.getIssuerURL() + "/ui/#/activate?token=" + token

	// Stash the bootstrap admin's UserID on the server so a later
	// BindAdminToSOCert call (path a) can find them without re-querying
	// the identity store.
	s.bootstrapAdminUserID = user.ID
	s.bootstrapAdminTenantID = adminTenantID

	// Non-interactive PIV binding (path d, integration test convenience).
	// When BootstrapAdminPIVCertPath is configured, read the cert, compute
	// its SHA-256 thumbprint, store it on the user, and transition to active.
	// This skips the invite-token activation flow entirely — only suitable
	// for test clusters or pre-provisioned deployments where the cert is
	// generated alongside the cluster.
	if path := s.config.Auth.BootstrapAdminPIVCertPath; path != "" {
		if err := bindAdminPIVCert(ctx, c.identityStore, adminTenantID, user.ID, path); err != nil {
			return &FirstBootBootstrapError{Op: "bind bootstrap admin PIV cert", Cause: err}
		}
		s.logger.Infof("Bootstrap admin %s bound to PIV cert at %s", user.ID, path)
		return nil
	}

	// Print the three-option banner to stderr. Use os.Stderr directly —
	// NOT the structured logger — so operators see it regardless of log level.
	printFirstBootBannerFn(os.Stderr, activationURL)

	return nil
}

// firstBootLeaderWait bounds how long firstBootBootstrap waits for the raft
// shard to elect a leader before giving up. In a fresh cluster IAM init runs
// before consensus, so the bootstrap must wait for leadership; a healthy
// cluster elects within seconds, and an unhealthy one falls through to the
// existing non-fatal warning after the deadline.
const firstBootLeaderWait = 90 * time.Second

// waitForAdminAssignmentsCheck lists admin assignments, retrying while the read
// fails because the raft shard has no leader yet (the canonical
// "not leader, leader unknown" startup condition). It returns the assignments
// once a leader serves the read, the last error after firstBootLeaderWait
// elapses, or ctx.Err() if the context is cancelled. Using the read itself as
// the readiness probe means no separate leadership API is required, and once it
// succeeds the follow-on bootstrap writes commit against the same leader.
func (s *Server) waitForAdminAssignmentsCheck(ctx context.Context, c *iamComponents) ([]*iam.AdminAssignment, error) {
	deadline := time.Now().Add(firstBootLeaderWait)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		assignments, err := c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{
			ActiveOnly: true,
			Limit:      1,
		})
		if err == nil {
			return assignments, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// ErrSOCertLoginEmpty indicates the certificate PEM input is empty.
var ErrSOCertLoginEmpty = errors.New("so cert login thumbprint: empty PEM input")

// ErrSOCertLoginNoPEMBlock indicates the certificate PEM input has no PEM block.
var ErrSOCertLoginNoPEMBlock = errors.New("so cert login thumbprint: no PEM block")

// SOCertLoginThumbprintError indicates the certificate PEM body could not be
// parsed into an X.509 certificate.
type SOCertLoginThumbprintError struct {
	Reason string
}

// Error implements the error interface.
func (e *SOCertLoginThumbprintError) Error() string {
	return "so cert login thumbprint: " + e.Reason
}

// soCertLoginThumbprint computes the canonical login lookup thumbprint for a
// PEM-encoded certificate: hex(SHA-256(cert.Raw)). This is the SINGLE digest
// every login path computes (pkg/service/login_service.go mtlsLookupKey,
// pkg/api/rest/iam_login_piv.go PIVLogin, pkg/iam/oauth2_mtls.go) and the value
// stored in the PIVCertThumbprint index read by GetUserByPIVThumbprint. It is
// deliberately NOT the SPKI thumbprint (SOCertSPKIThumbprint): there is no
// SPKI-keyed user index, so an SPKI digest in that column is unreachable by any
// login lookup. Keeping one digest function here keeps bindAdminPIVCert and
// BindAdminToSOCert from drifting apart.
func soCertLoginThumbprint(certPEM []byte) (string, error) {
	if len(certPEM) == 0 {
		return "", ErrSOCertLoginEmpty
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", ErrSOCertLoginNoPEMBlock
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", &SOCertLoginThumbprintError{Reason: err.Error()}
	}
	return certRawSHA256Hex(cert), nil
}

// certRawSHA256Hex returns hex(SHA-256(cert.Raw)) — the canonical mTLS/PIV
// login lookup key every login path computes and the value stored in the
// PIVCertThumbprint index. It is the single digest shared by bindAdminPIVCert
// and soCertLoginThumbprint so the two binding paths cannot drift apart.
func certRawSHA256Hex(cert *x509.Certificate) string {
	digest := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(digest[:])
}

// bindAdminPIVCert reads a PEM-encoded client certificate from path, computes
// its SHA-256 thumbprint, stores it on the bootstrap admin user, and transitions
// the user to active. Used by the non-interactive integration-test bootstrap.
func bindAdminPIVCert(ctx context.Context, identityStore *iam.IdentityStore, tenantID, userID, path string) error {
	certPEM, err := os.ReadFile(path) //nolint:gosec // path comes from operator config
	if err != nil {
		return &PIVBootstrapError{Op: "read PIV cert", Path: path, Cause: err}
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return &PIVBootstrapError{Op: "decode PIV cert PEM", Path: path}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return &PIVBootstrapError{Op: "parse PIV cert", Path: path, Cause: err}
	}
	thumbprint := certRawSHA256Hex(cert)

	active := iam.UserStateActive
	emptyInvite := ""
	_, err = identityStore.UpdateUser(ctx, tenantID, userID, iam.UpdateUserOptions{
		State:             &active,
		PIVCertThumbprint: &thumbprint,
		InviteToken:       &emptyInvite,
	})
	if err != nil {
		return &PIVBootstrapError{Op: "update bootstrap admin with PIV thumbprint", Path: path, Cause: err}
	}
	return nil
}

// PIVBootstrapError reports failures binding a PIV certificate to the
// bootstrap admin during non-interactive cluster initialization.
type PIVBootstrapError struct {
	Op    string
	Path  string
	Cause error
}

func (e *PIVBootstrapError) Error() string {
	if e.Cause != nil {
		return e.Op + " " + e.Path + ": " + e.Cause.Error()
	}
	return e.Op + " " + e.Path
}

func (e *PIVBootstrapError) Unwrap() error { return e.Cause }

// BindAdminToSOCert pins the bootstrap admin user to the SHA-256 thumbprint
// of an SO certificate so that an mTLS / PIV handshake presenting that exact
// certificate is recognized as the admin user.
//
// The thumbprint is hex(SHA-256(cert.Raw)) — the SAME canonical login lookup
// key every login path computes: pkg/service/login_service.go (mtlsLookupKey),
// pkg/api/rest/iam_login_piv.go (PIVLogin), pkg/iam/oauth2_mtls.go, and the
// bindAdminPIVCert bootstrap convenience path above. It is written to the PIV
// thumbprint index, which is the SINGLE thumbprint-to-user index read by
// GetUserByPIVThumbprint and the unified login ceremony's mtls resolver.
//
// This DELIBERATELY does NOT use SOCertSPKIThumbprint (SHA-256 over the
// SubjectPublicKeyInfo only). The iam.User model exposes exactly one
// thumbprint index (PIVCertThumbprint); writing an SPKI digest there produced
// a digest that NO login path could ever match — a user bound via the SO-cert
// path was unfindable, and the PIV (cert.Raw) and SPKI digests collided on one
// column. The SPKI-survives-renewal property is not reachable here because no
// SPKI-keyed user index exists; the login lookup is over the full cert DER.
// See soCertLoginThumbprint.
//
// Returns FirstBootBootstrapError on any storage failure. Returns
// ErrBootstrapAdminMissing when no bootstrap admin exists yet (typical when
// the operator runs path (a) on a server that has already been initialized).
func (s *Server) BindAdminToSOCert(ctx context.Context, certPEM []byte) error {
	if s.iamComponents == nil {
		return &FirstBootBootstrapError{Op: "bind admin to so cert", Cause: ErrBootstrapAdminMissing}
	}
	c, ok := s.iamComponents.(*iamComponents)
	if !ok || c == nil || c.identityStore == nil {
		return &FirstBootBootstrapError{Op: "bind admin to so cert", Cause: ErrBootstrapAdminMissing}
	}

	thumbprint, err := soCertLoginThumbprint(certPEM)
	if err != nil {
		return &FirstBootBootstrapError{Op: "compute so cert login thumbprint", Cause: err}
	}

	userID := s.bootstrapAdminUserID
	tenantID := s.bootstrapAdminTenantID
	if userID == "" || tenantID == "" {
		// Fall back to the system-tenant "admin" user when the bootstrap
		// path stored no UserID (e.g., test fixtures or restart paths
		// where firstBootBootstrap has not run in this process). The
		// bootstrap admin is created in qrdbsdk.SystemTenantID by
		// firstBootBootstrap (see adminTenantID above), NOT the "default"
		// tenant — scanning "default" here would miss it. ListUsers is
		// bounded to the system tenant, which holds only control-plane
		// identities, so the scan is O(1) in practice.
		users, lookupErr := c.identityStore.ListUsers(ctx, qrdbsdk.SystemTenantID, iam.ListOptions{Limit: 100})
		if lookupErr != nil {
			return &FirstBootBootstrapError{Op: "find bootstrap admin", Cause: lookupErr}
		}
		var found *iam.User
		for _, u := range users {
			// The IAM User type was refactored from Username → Name +
			// Email. The bootstrap admin is identified by display name
			// "admin" (matches the seed in EnsureBootstrapAdmin); fall
			// through on the empty-Name case so legacy fixtures that
			// only set ID still match by ID.
			if u != nil && (u.Name == "admin" || u.ID == "admin") {
				found = u
				break
			}
		}
		if found == nil {
			return &FirstBootBootstrapError{Op: "find bootstrap admin", Cause: ErrBootstrapAdminMissing}
		}
		userID = found.ID
		tenantID = qrdbsdk.SystemTenantID
	}

	// Persist the cert.Raw SHA-256 thumbprint to the PIVCertThumbprint index
	// — the single thumbprint-to-user index every login path reads. Also
	// transition the user from pending to active so subsequent mTLS / PIV
	// handshakes can bind to the admin record.
	thumbprintCopy := thumbprint
	activeState := iam.UserStateActive
	if _, err := c.identityStore.UpdateUser(ctx, tenantID, userID, iam.UpdateUserOptions{
		PIVCertThumbprint: &thumbprintCopy,
		State:             &activeState,
	}); err != nil {
		return &FirstBootBootstrapError{Op: "persist so cert thumbprint", Cause: err}
	}
	return nil
}

// printFirstBootBanner writes the three-option ceremony banner to w.
// Extracted for testability. Lists the canonical bootstrap paths in the
// order documented by Phase 11 of the unification plan.
//
// The commands shown are platformctl invocations — the user-facing
// CLI for go-trusted-platform deployments. platformctl drives the same
// ceremony APIs internally via the quicbaas SDK, so embedded and remote
// transports are both supported by a single binary. Direct quicbaasd /
// quicbaas invocations are no longer surfaced to operators; they are
// SDK-internal and documented separately for library consumers.
func printFirstBootBanner(w interface{ WriteString(string) (int, error) }, activationURL string) {
	const sep = "============================================================"
	lines := []string{
		"",
		sep,
		"  FIRST-BOOT BOOTSTRAP: this server has no admin user yet.",
		"  This banner only appears when seal.enabled=true. To bring up",
		"  the daemon without an encrypted barrier, set seal.enabled=false",
		"  (the default) and restart — no ceremony is required.",
		"",
		"  When the barrier IS enabled, choose ONE of the three ceremony",
		"  paths below to bind a Security Officer credential:",
		"",
		"  (a) LOCAL CEREMONY (recommended for single-host installs):",
		"        platformctl init \\",
		"          --barrier-strategy software \\",
		"          --so-pin <SO_PIN> --user-pin <USER_PIN> \\",
		"          --so admin:csr:/path/to/admin.csr",
		"",
		"      The SO certificate's SubjectPublicKeyInfo SHA-256 will be",
		"      pinned to the admin user; future mTLS handshakes presenting",
		"      that cert authenticate as admin.",
		"",
		"  (b) REMOTE CEREMONY (split admin / server hosts):",
		"        platformctl ceremony claim-cert --username admin --so-pin <SO_PIN>",
		"        platformctl ceremony claim-share --username admin",
		"",
		"      Same SO-cert thumbprint binding as (a); useful when the",
		"      admin keypair lives on a hardware token on a separate host.",
		"",
		"  (c) WEBAUTHN INVITE (browser-only operators):",
		"",
		"        " + activationURL,
		"",
		"      One-time URL — opens the activation page in a browser to",
		"      register a FIDO2 / PIV credential. Shown ONCE.",
		"",
		"  After ANY of these paths completes, this banner will not",
		"  appear on subsequent boots.",
		sep,
		"",
	}
	for _, line := range lines {
		_, _ = w.WriteString(line + "\n")
	}
}

// resolveBootstrapCredentials resolves the bootstrap admin credentials
// from configuration, applying defaults when values are not set.
func (s *Server) resolveBootstrapCredentials() (string, string) {
	username := s.config.Auth.BootstrapAdminUsername
	if username == "" {
		username = "admin"
	}

	password := s.config.Auth.BootstrapAdminPassword
	if password == "" && s.config.Auth.InsecureDevMode {
		password = "quicbaas-dev-admin"
	}

	return username, password
}

// validateBootstrapCredentials validates the bootstrap admin credentials
// for security. In insecure dev mode, all checks are skipped.
func validateBootstrapCredentials(username, password string, insecureDevMode bool) error {
	if insecureDevMode {
		return nil
	}

	if password == "" {
		return &BootstrapPasswordRequiredError{}
	}

	// Check for common insecure passwords
	insecurePasswords := []string{"admin", "password", "changeme"}
	for _, insecure := range insecurePasswords {
		if strings.EqualFold(password, insecure) {
			return &BootstrapPasswordNotAllowedError{}
		}
	}

	// Password must not match username
	if strings.EqualFold(password, username) {
		return &BootstrapPasswordMatchesUsernameError{}
	}

	// Minimum length requirement
	if len(password) < config.MinBootstrapPasswordLength {
		return &BootstrapPasswordTooShortError{MinLength: config.MinBootstrapPasswordLength}
	}

	return nil
}

// getIssuerURL constructs the OIDC issuer URL from server configuration.
func (s *Server) getIssuerURL() string {
	scheme := "http"
	if s.config.Security.TLSEnabled {
		scheme = "https"
	}

	host := s.config.Server.Host
	if host == "" || host == "0.0.0.0" {
		host = "localhost"
	}

	port := s.config.Server.REST.Port
	if port == 0 {
		port = 8443
	}

	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

// iamComponentsGetter is an internal interface for accessing IAM components
// from the QuicBaaS service. Implementations that support IAM should implement
// this interface to provide component access for gRPC service registration.
type iamComponentsGetter interface {
	GetTenantManager(ctx context.Context) (iam.TenantManager, error)
	GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	GetAppManager(ctx context.Context) (*iam.AppManager, error)
	GetAdminManager(ctx context.Context) (*iam.AdminManager, error)
	GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	GetFederationManager(ctx context.Context) (iam.FederationManager, error)
	GetMFAManager(ctx context.Context) (service.MFAManager, error)
	GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	GetSessionStorage(ctx context.Context) (iam.SessionStorage, error)
}

// registerIAMService registers IAM-related gRPC services with the gRPC server.
// It retrieves IAM components from the QuicBaaS service via the iamComponentsGetter
// interface and creates gRPC server adapters for IAM, MFA, PEP, Session, and Audit.
func (s *Server) registerIAMService(grpcServer *ggrpc.Server) {
	if s.quicBaaSService == nil {
		return
	}

	getter, ok := s.quicBaaSService.(iamComponentsGetter)
	if !ok {
		s.logger.Debug("QuicBaaS service does not implement iamComponentsGetter, skipping IAM gRPC registration")
		return
	}

	ctx := context.Background()

	// Retrieve core IAM components
	tenantMgr, err := getter.GetTenantManager(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to get tenant manager, skipping IAM gRPC registration")
		return
	}

	identityStore, err := getter.GetIdentityStore(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to get identity store, skipping IAM gRPC registration")
		return
	}

	policyEngine, err := getter.GetPolicyEngine(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to get policy engine, skipping IAM gRPC registration")
		return
	}

	appManager, err := getter.GetAppManager(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to get app manager, skipping IAM gRPC registration")
		return
	}

	// Retrieve optional components
	adminManager, _ := getter.GetAdminManager(ctx)
	gdprManager, _ := getter.GetGDPRManager(ctx)

	// Create IAM gRPC service adapter and register
	iamAdapter := grpcapi.NewIAMServiceAdapter(tenantMgr, identityStore, policyEngine, appManager, adminManager, gdprManager)
	iamServer := grpcapi.NewIAMServer(iamAdapter, s.logger)
	iampb.RegisterIAMServiceServer(grpcServer, iamServer)
	s.logger.Info("Registered IAM gRPC service")

	// Register Federation service if available
	fedMgr, _ := getter.GetFederationManager(ctx)
	if fedMgr != nil {
		fedServer := grpcapi.NewFederationServer(fedMgr, s.logger)
		iampb.RegisterFederationServiceServer(grpcServer, fedServer)
		s.logger.Info("Registered Federation gRPC service")
	}

	// Register MFA service if available
	mfaMgr, _ := getter.GetMFAManager(ctx)
	if mfaMgr != nil {
		mfaAdapter := grpcapi.NewMFAServiceAdapter(mfaMgr)
		if mfaAdapter != nil {
			mfaServer := grpcapi.NewMFAServer(mfaAdapter, s.logger)
			iampb.RegisterMFAServiceServer(grpcServer, mfaServer)
			s.logger.Info("Registered MFA gRPC service")
		}
	}

	// Register PEP (Policy Enforcement Point) service
	pepAdapter := grpcapi.NewPEPServiceAdapter(policyEngine)
	pepServer := grpcapi.NewPEPServer(pepAdapter, s.logger)
	iampb.RegisterPEPServiceServer(grpcServer, pepServer)
	s.logger.Info("Registered PEP gRPC service")

	// Register Session service with fallback to in-memory storage
	sessionStorage, _ := getter.GetSessionStorage(ctx)
	if sessionStorage == nil {
		sessionStorage = iam.NewMemorySessionStorage()
		s.logger.Info("Using in-memory session storage fallback for gRPC")
	}
	sessionAdapter := grpcapi.NewSessionServiceAdapter(sessionStorage)
	sessionServer := grpcapi.NewSessionServer(sessionAdapter, s.logger)
	iampb.RegisterSessionServiceServer(grpcServer, sessionServer)
	s.logger.Info("Registered Session gRPC service")

	// Register Audit service with fallback to in-memory logger
	auditLogger, _ := getter.GetAuditLogger(ctx)
	if auditLogger == nil {
		auditLogger, err = iam.NewAuditLogger()
		if err != nil {
			s.logger.WithError(err).Error("Failed to create fallback audit logger, skipping Audit gRPC registration")
			return
		}
		s.logger.Info("Using in-memory audit logger fallback for gRPC")
	}
	auditAdapter := grpcapi.NewAuditServiceAdapter(auditLogger)
	auditServer := grpcapi.NewAuditServer(auditAdapter, s.logger)
	iampb.RegisterAuditServiceServer(grpcServer, auditServer)
	s.logger.Info("Registered Audit gRPC service")
}

// OrchestratorDaemonClientAppName is the fixed OAuth2 application name the
// central uses for its own outbound calls to remote agent daemons. The name
// is stable across restarts so the app is created once per data directory.
const OrchestratorDaemonClientAppName = "quicbaas-orchestrator-daemon-client"

// ensureOrchestratorDaemonClient ensures the central's well-known OAuth2
// application for daemon dispatch exists. Idempotent across restarts: on
// the first start it creates the app via AppManager.RegisterApp and caches
// the plaintext client_secret to disk under the data directory; on
// subsequent starts the cached credentials are returned without creating a
// duplicate app.
//
// Storing the client_secret on the local filesystem is acceptable here
// because (a) it lives alongside the CA root key which is at least as
// sensitive, (b) the permissions are 0600, and (c) it never transits the
// network — the central uses it to sign requests to daemons, which the
// daemons validate against their own knowledge of the central's OIDC issuer.
func (s *Server) ensureOrchestratorDaemonClient(ctx context.Context, appManager *iam.AppManager) (*OrchestratorDaemonClient, error) {
	path := filepath.Join(s.config.Storage.DataDir, "orchestrator_daemon_client.json")

	// Fast path: credentials already cached on disk. Trust the cache — on a
	// corrupted file we re-bootstrap which is cheap.
	if existing, err := readOrchestratorDaemonClient(path); err == nil && existing != nil {
		return existing, nil
	}

	// The orchestrator client is provisioned under the bootstrap "default"
	// tenant. Dispatching to daemons in other tenants requires a per-tenant
	// client app; see ensureOrchestratorDaemonClientForTenant for that path.
	// Today we rely on the default tenant so the first-start demo works
	// without tenant management; other tenants are created on first dispatch.
	defaultTenant := "default"
	scopes := iam.DefaultAgentRuntimeScopes()
	app, err := appManager.RegisterApp(ctx, defaultTenant, iam.RegisterAppOptions{
		Name:           OrchestratorDaemonClientAppName,
		GrantTypes:     []string{iam.GrantTypeClientCredentials},
		ResponseTypes:  []string{},
		ResourcePrefix: "orchestrator",
	})
	if err != nil {
		// If the app already exists from a prior run but the cache file is
		// gone, we can't recover the plaintext secret (AppManager never
		// stores it). Rotate the secret to get a fresh plaintext.
		if strings.Contains(err.Error(), "already") {
			return nil, fmt.Errorf("ensureOrchestratorDaemonClient: app %q exists but cache %q is missing — delete the app via `quicbaas iam app delete` and restart to re-bootstrap: %w", OrchestratorDaemonClientAppName, path, err)
		}
		return nil, fmt.Errorf("ensureOrchestratorDaemonClient: register app: %w", err)
	}

	client := &OrchestratorDaemonClient{
		ClientID:     app.ClientID,
		ClientSecret: app.ClientSecret, // plaintext — only visible here
		Scopes:       append([]string(nil), scopes...),
		TenantID:     defaultTenant,
	}
	if err := writeOrchestratorDaemonClient(path, client); err != nil {
		return nil, fmt.Errorf("ensureOrchestratorDaemonClient: persist cache: %w", err)
	}
	s.logger.Infof("Bootstrapped orchestrator daemon client (app=%q, client_id=%s) — credentials cached at %s",
		OrchestratorDaemonClientAppName, client.ClientID, path)
	return client, nil
}

// readOrchestratorDaemonClient loads cached credentials from disk. Returns
// (nil, nil) when the file is missing; any other read/unmarshal error is
// returned so the caller can log and re-bootstrap.
func readOrchestratorDaemonClient(path string) (*OrchestratorDaemonClient, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var c OrchestratorDaemonClient
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, errors.New("orchestrator daemon client cache missing required fields")
	}
	return &c, nil
}

// AdminRoleID is the well-known ID assigned to the seed "admin" role in
// every tenant. Using a stable ID lets ensureAdminRole detect the existing
// row on restart with a single GetRole call instead of a full ListRoles
// scan, and makes the role addressable from CLI / API without a lookup.
const AdminRoleID = "admin"

// ensureAdminRole seeds the canonical "admin" role inside the given tenant
// when one does not already exist. The first-boot bootstrap creates the
// admin user with the role label "admin" — without a matching Role row in
// the IdentityStore the IAM Roles tab renders an empty list while the
// Users tab continues to show the admin role string. Seeding the row keeps
// both surfaces in sync and gives operators a target object to extend
// with permissions.
//
// Idempotent: the function is a no-op when the role already exists. Only
// genuine creation failures (e.g. the underlying storage rejecting the
// row) bubble up to the caller.
func ensureAdminRole(ctx context.Context, identityStore *iam.IdentityStore, tenantID string) error {
	if identityStore == nil {
		return nil
	}
	if _, err := identityStore.GetRole(ctx, tenantID, AdminRoleID); err == nil {
		return nil
	}
	_, err := identityStore.CreateRole(ctx, tenantID, iam.CreateRoleOptions{
		ID:          AdminRoleID,
		Name:        "admin",
		Description: "Built-in administrator role with full access. Granted to the bootstrap admin user on first boot.",
		Permissions: []string{"*"},
	})
	if err != nil {
		// Swallow the duplicate-creation race so a concurrent restart
		// against the same data dir does not surface a spurious warning.
		if errors.Is(err, iam.ErrRoleAlreadyExists) {
			return nil
		}
		if strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return err
	}
	return nil
}

// writeOrchestratorDaemonClient persists the central's daemon-dispatch
// credentials with 0600 permissions via atomic rename.
func writeOrchestratorDaemonClient(path string, c *OrchestratorDaemonClient) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
