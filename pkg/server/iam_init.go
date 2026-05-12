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
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	ggrpc "google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/security"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

type iamComponents struct {
	tenantManager          iam.TenantManager
	identityStore          *iam.IdentityStore
	policyEngine           *iam.PolicyEngine
	appManager             *iam.AppManager
	appStorage             iam.AppStorage
	oidcProvider           *iam.OIDCProvider
	federationManager      iam.FederationManager
	mfaManager             *iam.DefaultMFAManager
	webauthnManager        *iam.XKMSWebAuthnManager
	auditLogger            iam.AuditLogger
	adminManager           *iam.AdminManager
	gdprManager            *iam.GDPRManager
	sessionStorage         iam.SessionStorage
	sessionManager         iam.SessionManager
	authManager            *inMemoryAuthManagerAdapter
	inviteStore            iam.InviteStore
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

	// Create the base in-memory storage that satisfies TenantStorage,
	// UserStorage, GroupStorage, RoleStorage, PolicyStorage, AppStorage,
	// and TokenStorage interfaces.
	memStorage := iam.NewMemoryStorage()

	// Create tenant manager backed by the memory storage
	tenantManager := iam.NewDefaultTenantManager(memStorage)

	// Wire the hardware-backed TenantDEKManager when KMS is available.
	// The DEK provisioner ensures each tenant receives a dedicated DEK on
	// creation and that the DEK is destroyed on deletion.
	if s.kmsManager != nil {
		// Ensure the KEK used to wrap tenant DEKs exists in KMS. On a fresh
		// server this is the first write into the key store, so we create it
		// lazily rather than requiring operators to run a pre-start bootstrap
		// command. Idempotent: GetKey is only called once and any "already
		// exists" error is swallowed.
		if err := s.ensureTenantDEKMasterKey(context.Background()); err != nil {
			s.logger.WithError(err).Warn("Failed to ensure tenant DEK master key — tenant provisioning will fail until this key exists")
		}
		dekStore := s.GetKVStore(qbshards.PlatformShardID)
		dekMgr := security.NewTenantDEKManager(s.kmsManager, dekStore, security.TenantDEKMasterKeyID, logger.NewAdapter(s.logger))
		tenantManager.SetDEKProvisioner(security.NewTenantDEKProvisioner(dekMgr))
		s.tenantDEKManager = dekMgr
		s.logger.Info("TenantDEKManager wired into tenant lifecycle")
	}

	// Create identity store with storage backends
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  memStorage,
		GroupStorage: memStorage,
		RoleStorage:  memStorage,
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
		Storage: memStorage,
		Cache:   policyCache,
	})

	// Create app manager
	appManager := iam.NewAppManager(memStorage, tenantManager)

	// Create audit logger with default in-memory storage
	auditLogger, err := iam.NewAuditLogger()
	if err != nil {
		return nil, &IAMComponentInitError{Component: "audit logger", Cause: err}
	}

	// Create session storage (WebAuthn sessions) and extended session manager
	sessionStorage := iam.NewMemorySessionStorage()
	sessionExtStorage := iam.NewMemorySessionExtStorage()
	sessionManager, err := iam.NewSessionManager(
		iam.WithSessionStorage(sessionExtStorage),
	)
	if err != nil {
		return nil, &IAMComponentInitError{Component: "session manager", Cause: err}
	}

	// Create OIDC provider with an ephemeral RSA signing key.
	// In production, the signing key should come from KMS.
	var oidcProvider *iam.OIDCProvider
	signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to generate OIDC signing key, OIDC provider will be unavailable")
	} else {
		issuerURL := s.getIssuerURL()
		oidcProvider, err = iam.NewOIDCProvider(
			&iam.OIDCConfig{
				Issuer:           issuerURL,
				SigningAlgorithm: "RS256",
				SigningKey:       signingKey,
				AccessTokenTTL:   1 * time.Hour,
				RefreshTokenTTL:  7 * 24 * time.Hour,
				IDTokenTTL:       1 * time.Hour,
				AuthCodeTTL:      10 * time.Minute,
				PKCERequired:     true,
				AllowedMethods:   []string{"S256"},
			},
			iam.OIDCDependencies{
				IdentityStore: identityStore,
				AppStorage:    memStorage,
				TokenStorage:  memStorage,
			},
		)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to create OIDC provider, OIDC features will be unavailable")
			oidcProvider = nil
		}
	}

	// Create MFA manager with in-memory storage
	mfaManager, err := iam.NewMFAManager(
		iam.WithMFAStorage(iam.NewMemoryMFAStorage()),
		iam.WithUserStorage(memStorage),
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
				Config: webauthnFactoryConfigFromOperator(s.config.IAM.WebAuthn),
				QRDB:   platformQRDB,
			}
			factory, fErr := iam.NewWebAuthnServiceFactory(factoryParams)
			if fErr != nil {
				s.logger.WithError(fErr).Warn("iam.webauthn: factory construction failed; WebAuthn surface disabled")
			} else {
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

	// Create admin manager
	adminManager, err := iam.NewAdminManager(iam.AdminManagerOptions{
		AdminStorage:         iam.NewMemoryAdminStorage(),
		ImpersonationStorage: iam.NewMemoryImpersonationStorage(),
		AuditLogger:          auditLogger,
	})
	if err != nil {
		return nil, &IAMComponentInitError{Component: "admin manager", Cause: err}
	}

	// Create GDPR manager
	gdprManager, err := iam.NewGDPRManager(iam.GDPRManagerDependencies{
		ConsentStorage:    iam.NewMemoryConsentStorage(),
		DataExportStorage: iam.NewMemoryDataExportStorage(),
		DeletionStorage:   iam.NewMemoryDeletionStorage(),
		UserStorage:       memStorage,
		SessionStorage:    sessionStorage,
		GroupStorage:      memStorage,
		RoleStorage:       memStorage,
	})
	if err != nil {
		return nil, &IAMComponentInitError{Component: "GDPR manager", Cause: err}
	}

	// Create federation manager
	var fedMgr iam.FederationManager
	fedMgr, err = iam.NewFederationManager(
		iam.WithProviderStorage(iam.NewMemoryProviderStorage()),
		iam.WithLinkStorage(iam.NewMemoryFederationStorage()),
		iam.WithStateStorage(iam.NewMemoryStateStorage()),
		iam.WithFederationIdentityStore(identityStore),
		iam.WithFederationOIDCProvider(oidcProvider),
		iam.WithFederationAuditLogger(auditLogger),
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

	inviteStore := iam.NewMemoryInviteStore()

	components := &iamComponents{
		tenantManager:          tenantManager,
		identityStore:          identityStore,
		policyEngine:           policyEngine,
		appManager:             appManager,
		appStorage:             memStorage,
		oidcProvider:           oidcProvider,
		federationManager:      fedMgr,
		mfaManager:             mfaManager,
		webauthnManager:        webauthnMgr,
		auditLogger:            auditLogger,
		adminManager:           adminManager,
		gdprManager:            gdprManager,
		sessionStorage:         sessionStorage,
		sessionManager:         sessionManager,
		inviteStore:            inviteStore,
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

	// First-boot bootstrap: when no admin assignments exist, the seal
	// barrier is enabled, AND password / dev auth are not enabled, create a
	// pending admin user and print an activation invite so the operator can
	// register their FIDO2 / PIV credential.
	//
	// The barrier-enabled gate matches the design intent: the SO ceremony
	// exists to gate access to an *encrypted barrier* (and the unseal
	// quorum that protects its master key). When the barrier is disabled
	// — the default — there is no sealed material to gate, so the
	// first-boot ceremony is conceptually a no-op and forcing operators
	// through it just to bring the daemon up is friction with no
	// security benefit.
	//
	// Operators who later enable the encrypted barrier flip the gate
	// open by setting seal.enabled=true; the next boot will print the
	// banner and run the ceremony exactly as before.
	skipBootstrap := s.config.Auth.AllowPasswordAuth ||
		s.config.Auth.InsecureDevMode ||
		!s.config.Seal.Enabled
	if !skipBootstrap {
		if bootstrapErr := s.firstBootBootstrap(context.Background(), components); bootstrapErr != nil {
			// Non-fatal: log the error but do not abort the server. The operator
			// can fix the cause (e.g., corrupt storage) and restart.
			s.logger.WithError(bootstrapErr).Warn("First-boot bootstrap failed — no admin credential will be created this boot")
		}
	} else if !s.config.Seal.Enabled {
		s.logger.Info("Seal barrier is disabled — skipping first-boot SO ceremony bootstrap (no encrypted barrier to gate)")
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
//  (a) Local: `quicbaasd init --barrier-strategy ... --so alice:csr:/path`
//      runs the init ceremony in-process. On completion the SO certificate's
//      SPKI thumbprint is bound to the bootstrap admin via
//      BindAdminToSOCert; an mTLS handshake presenting the SO cert is then
//      recognized as the admin user.
//
//  (b) Remote: `quicbaas init claim-cert/claim-share` walks the operator
//      through claiming a server-issued certificate from a separate
//      machine. The CLI is owned by the Phase 12 split-CLI agent.
//
//  (c) WebAuthn invite URL: kept as a tertiary path for browser-only
//      operators. The activation URL is generated and printed here; the
//      invite remains valid for firstBootBootstrapTTL.
//
// On every subsequent boot the admin assignment exists so the function is
// a no-op.
func (s *Server) firstBootBootstrap(ctx context.Context, c *iamComponents) error {
	// Detect first boot: any active admin assignment means a prior bootstrap ran.
	assignments, err := c.adminManager.ListAssignments(ctx, iam.ListAdminAssignmentsOptions{
		ActiveOnly: true,
		Limit:      1,
	})
	if err != nil {
		return &FirstBootBootstrapError{Op: "list admin assignments", Cause: err}
	}
	if len(assignments) > 0 {
		// Admin already exists — nothing to do.
		return nil
	}

	// Resolve admin email from config; default to "admin@quicbaas.local".
	// The username (URL-safe identifier) is always "admin" since the identity
	// store username field must be alphanumeric/dash/underscore only.
	adminEmail := s.config.Auth.BootstrapAdminUsername
	if adminEmail == "" {
		adminEmail = "admin@quicbaas.local"
	}

	// Create the admin user in pending state inside the default tenant.
	// The user cannot authenticate until one of the three ceremony paths
	// completes. AuthMethods includes "mtls" so that path (a) — SO-cert
	// SPKI binding — is reachable without a follow-up update.
	user, err := c.identityStore.CreateUser(ctx, "default", iam.CreateUserOptions{
		Email:        adminEmail,
		Username:     "admin",
		Name:         "Bootstrap Admin",
		Roles:        []string{"admin"},
		AuthMethods:  []string{"mtls", "webauthn", "piv"},
		InviteExpiry: firstBootBootstrapTTL,
	})
	if err != nil {
		return &FirstBootBootstrapError{Op: "create bootstrap admin user", Cause: err}
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
		TenantID:  "default",
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
	s.bootstrapAdminTenantID = "default"

	// Print the three-option banner to stderr. Use os.Stderr directly —
	// NOT the structured logger — so operators see it regardless of log level.
	printFirstBootBannerFn(os.Stderr, activationURL)

	return nil
}

// BindAdminToSOCert pins the bootstrap admin user to an SO certificate's
// SubjectPublicKeyInfo (SPKI) SHA-256 thumbprint. Once bound, an mTLS
// handshake presenting the SO certificate (or any cert with the same public
// key) is recognized as the admin user, mirroring the agent enrollment
// pattern in pkg/api/rest/handlers_agent_register.go.
//
// The thumbprint is computed by SOCertSPKIThumbprint over the SubjectPublicKeyInfo
// bytes (RFC 5280 §4.1.2.7), NOT the full certificate. This binding survives
// SO certificate renewal as long as the public key remains the same.
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

	thumbprint, err := SOCertSPKIThumbprint(certPEM)
	if err != nil {
		return &FirstBootBootstrapError{Op: "compute so cert spki thumbprint", Cause: err}
	}

	userID := s.bootstrapAdminUserID
	tenantID := s.bootstrapAdminTenantID
	if userID == "" || tenantID == "" {
		// Fall back to the default-tenant "admin" user when the bootstrap
		// path stored no UserID (e.g., test fixtures or restart paths
		// where firstBootBootstrap has not run in this process). ListUsers
		// is bounded to the default tenant — a fresh server has at most
		// the bootstrap admin and the orchestrator-daemon-client app
		// principal, so the scan is O(1) in practice.
		users, lookupErr := c.identityStore.ListUsers(ctx, "default", iam.ListOptions{Limit: 100})
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
		tenantID = "default"
	}

	// Persist the thumbprint via the canonical PIVCertThumbprint update
	// path. The field name predates the SO-cert ceremony but is the
	// existing thumbprint-to-user index in the identity store; reusing it
	// keeps a single mTLS-recognition path through the codebase. Also
	// transition the user from pending to active so subsequent mTLS
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

// ensureTenantDEKMasterKey lazily creates the KEK used by TenantDEKManager
// to wrap per-tenant DEKs. On a fresh server the key store is empty, so the
// first tenant creation fails with "key not found" unless we provision this
// key up front. Idempotent: if the key already exists (server restart against
// an existing data dir), GenerateKey's "already exists" error is swallowed.
func (s *Server) ensureTenantDEKMasterKey(ctx context.Context) error {
	if s.kmsManager == nil {
		return fmt.Errorf("ensureTenantDEKMasterKey: kms manager unavailable")
	}

	// Fast path: key already present. The software backend returns an error
	// when the key is missing; we rely on that rather than a sentinel.
	if _, err := s.kmsManager.GetKey(ctx, security.TenantDEKMasterKeyID); err == nil {
		return nil
	}

	attrs := &xkmssdk.KeyAttributes{
		CN:                 security.TenantDEKMasterKeyID,
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
		StoreType:          xkmssdk.StoreSoftware,
	}
	if _, err := s.kmsManager.GenerateKey(ctx, attrs); err != nil {
		// Swallow duplicate-creation races so two servers starting against
		// the same datastore don't both fail.
		if strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return fmt.Errorf("ensureTenantDEKMasterKey: generate %s: %w", security.TenantDEKMasterKeyID, err)
	}
	s.logger.Infof("Bootstrapped tenant DEK master key %q (symmetric AES-256-GCM)", security.TenantDEKMasterKeyID)
	return nil
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
