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
	"fmt"
	"strings"
	"time"

	ggrpc "google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

type iamComponents struct {
	tenantManager     iam.TenantManager
	identityStore     *iam.IdentityStore
	policyEngine      *iam.PolicyEngine
	appManager        *iam.AppManager
	oidcProvider      *iam.OIDCProvider
	federationManager iam.FederationManager
	mfaManager        *iam.DefaultMFAManager
	webauthnManager   *iam.WebAuthnManager
	webauthnAdapter   *webauthnServiceAdapter
	auditLogger       iam.AuditLogger
	adminManager      *iam.AdminManager
	gdprManager       *iam.GDPRManager
	sessionStorage    iam.SessionStorage
	sessionManager    iam.SessionManager
	authManager       *inMemoryAuthManagerAdapter
}

// inMemoryAuthManagerAdapter wraps InMemoryAuthManager to implement service.AuthManagerInterface.
type inMemoryAuthManagerAdapter struct {
	manager *auth.InMemoryAuthManager
}

// Authenticate validates credentials and issues authentication tokens.
func (a *inMemoryAuthManagerAdapter) Authenticate(ctx context.Context, username, password string) (*auth.Token, error) {
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
func (a *interfacesAuthManagerAdapter) Authenticate(ctx context.Context, username, password string) (interface{}, error) {
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

// GetWebAuthnManager returns the WebAuthn manager.
// Returns the adapter that converts between typed protocol structures
// and map[string]interface{} for REST API serialization.
func (c *iamComponents) GetWebAuthnManager() service.WebAuthnManager {
	return c.webauthnAdapter
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

	// Create WebAuthn manager
	var webauthnMgr *iam.WebAuthnManager
	var webauthnAdapter *webauthnServiceAdapter
	credentialStorage := iam.NewMemoryCredentialStorage()
	webauthnSessionStorage := iam.NewMemorySessionStorage()
	webauthnMgr, err = iam.NewWebAuthnManager(
		&iam.WebAuthnConfig{
			RPID:      "localhost",
			RPName:    "QuicBaaS",
			RPOrigins: []string{"https://localhost"},
		},
		iam.WebAuthnDependencies{
			CredentialStorage: credentialStorage,
			IdentityStore:     memStorage,
			SessionStorage:    webauthnSessionStorage,
		},
	)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to create WebAuthn manager, WebAuthn features will be unavailable")
		webauthnMgr = nil
	} else {
		webauthnAdapter = newWebAuthnServiceAdapter(webauthnMgr)
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
		CredentialStorage: credentialStorage,
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

	components := &iamComponents{
		tenantManager:     tenantManager,
		identityStore:     identityStore,
		policyEngine:      policyEngine,
		appManager:        appManager,
		oidcProvider:      oidcProvider,
		federationManager: fedMgr,
		mfaManager:        mfaManager,
		webauthnManager:   webauthnMgr,
		webauthnAdapter:   webauthnAdapter,
		auditLogger:       auditLogger,
		adminManager:      adminManager,
		gdprManager:       gdprManager,
		sessionStorage:    sessionStorage,
		sessionManager:    sessionManager,
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

	// Create bootstrap "default" tenant if it doesn't already exist.
	// Many integration tests and SDK operations expect a default tenant.
	ctx := context.Background()
	if _, err := tenantManager.GetTenant(ctx, "default"); err != nil {
		_, createErr := tenantManager.CreateTenant(ctx, iam.CreateTenantOptions{
			ID:   "default",
			Name: "Default Tenant",
		})
		if createErr != nil {
			s.logger.WithError(createErr).Warn("Failed to create default tenant, some features may be unavailable")
		} else {
			s.logger.Info("Bootstrap default tenant created")
		}
	}

	s.logger.Info("IAM components initialized successfully")
	return components, nil
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
