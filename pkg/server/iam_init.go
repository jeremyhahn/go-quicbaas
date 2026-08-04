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
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
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
	inviteStore       iam.InviteStore
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
	// The in-memory auth manager is never wired into iamComponents; live token
	// operations flow through the qrdb/iam layer. Returns nil so callers fall
	// back to that path.
	return nil
}

// GetInviteStore returns the invite token store.
func (c *iamComponents) GetInviteStore() iam.InviteStore {
	return c.inviteStore
}

// GetInternalAuthManager returns the interfaces.AuthManager for internal service
// use. The in-memory auth manager is never wired into iamComponents (live token
// operations flow through the qrdb/iam layer), so this returns nil.
func (c *iamComponents) GetInternalAuthManager() interfaces.AuthManager {
	return nil
}

// initializeIAMComponents creates and initializes all IAM components.
// Returns nil if the qrdb client is not available (e.g., in standalone/embedded
// mode without storage initialization).
// serverPermissionResolver adapts the Server's late-bound qrdb node client to
// the iam.PermissionResolver seam consumed by the OIDC token service. The qrdb
// node client is constructed later in the server lifecycle than the OIDC
// provider, so the RBAC client is resolved per token-issue (a nil node client
// yields a nil RBAC client, and the token service then falls back to the OIDC
// scope). This mirrors the per-request RBAC resolver the REST/gRPC/QUIC/MCP
// authz gates use (GetQRDBNodeClient().RBAC()).
type serverPermissionResolver struct {
	server *Server
}

// RBACClient returns the qrdb RBAC client, or nil when the node client is not
// yet available.
func (r *serverPermissionResolver) RBACClient() qrdbtransport.RBACClient {
	node := r.server.GetQRDBNodeClient()
	if node == nil {
		return nil
	}
	return node.RBAC()
}

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

	// Route role persistence to qrdb's canonical RoleServicer (via the SDK
	// RoleClient on the platform shard) so the broker and the qrdb policy
	// decision point share ONE role ID space: a role the broker creates is the
	// same object the PDP resolves to permissions, and a token's role names are
	// recognized by the PDP. The broker-supplied string role ID is passed
	// straight through to qrdb (the deterministic-ID seam), so a later
	// Get/Update/Delete by that SAME id resolves the same record. Group
	// persistence is NOT routed here — qrdb has no Group type — so it stays on
	// the BackendStorage. This mirrors the KVStore-seam tenant routing: the
	// IdentityStore needs no interface change, it just sees a different
	// RoleStorage.
	roleStorage := iam.NewQRDBRoleStorage(newPlatformRoleClientResolver(s.qrdbClient))

	// Route user persistence to qrdb's canonical UserServicer (via the embedded
	// node) so the broker and the qrdb policy decision point share ONE user
	// record: the user the broker creates is the same object login and the PDP
	// resolve against. qrdb owns the user CORE (Username, RoleIDs, Attributes,
	// CertBindings) while a broker side-record holds the broker-only fields
	// (Email/State/MFA/Invite) keyed by the qrdb uint64 id; the string-UUID <->
	// uint64 correlation is the IDMap. This mirrors the role-storage swap: the
	// IdentityStore needs no interface change, it just sees a different
	// UserStorage.
	//
	// There is NO local-store fallback. BackendStorage no longer persists users
	// (phase C1-F), and both standalone and clustered modes always set
	// s.qrdbNode. A nil node here means a qrdb client was wired without a node —
	// an inconsistent state in which there is no place to store users — so IAM
	// fails closed rather than silently routing to an empty store. (The
	// client-less path returned (nil, nil) earlier, before this point.)
	if s.qrdbNode == nil {
		return nil, &QRDBUserStorageRequiredError{}
	}
	var userStorage iam.UserStorage = qrdbadapter.NewQRDBUserStorage(
		newPlatformUserServicerResolver(s),
		iam.NewBackendBrokerProfileStore(iamKV),
		qrdbadapter.NewBackendIDMap(iamKV),
		iamKV,
	)

	// Wire the routed user store into the tenant manager (built above, before
	// userStorage existed) so DeleteTenant deprovisions the tenant's users
	// through the qrdb-backed store — purging broker profile + IDMap +
	// email/legacy-PIV index records the legacy BackendStorage cascade misses.
	tenantManager.SetUserStorage(userStorage)

	// Create identity store with storage backends
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  userStorage,
		GroupStorage: backendStorage,
		RoleStorage:  roleStorage,
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
				// Late-bind the qrdb RBAC client so the scp claim carries the
				// subject's qrdb-resolved effective permissions (colon
				// "resource:action") rather than the OAuth-requested scope. The
				// node client is constructed later in lifecycle, so the resolver
				// is consulted per token-issue via GetQRDBNodeClient().RBAC().
				PermissionResolver: &serverPermissionResolver{server: s},
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
		// Route to the qrdb-backed user store (not backendStorage): users live in
		// qrdb, so MFA enrollment must consult the same store login resolves
		// against. Wiring backendStorage here would consult an empty store.
		iam.WithUserStorage(userStorage),
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
			// Derive the WebAuthn relying-party defaults from the canonical
			// issuer URL so the RP ID, RP origin (with port), OIDC issuer, and
			// activation banner all agree. Fail closed at startup when the
			// issuer host is an IP literal (no valid RP ID) and the operator
			// has not supplied an explicit rp_id.
			issuerRPID, issuerRPOrigin, derErr := webauthnDefaultsFromIssuer(s.getIssuerURL())
			if derErr != nil && s.config.IAM.WebAuthn.RPID == "" && len(s.config.IAM.WebAuthn.RPOrigins) == 0 {
				return nil, &IAMComponentInitError{Component: "WebAuthn relying-party defaults", Cause: derErr}
			}
			factoryParams := iam.WebAuthnFactoryParams{
				Config:   webauthnFactoryConfigFromOperator(s.config.IAM.WebAuthn, issuerRPID, issuerRPOrigin),
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
					// Wire the canonical user store so FinishLogin re-resolves the
					// authenticated user (external string ID + qrdb-resolved roles)
					// instead of mapping xkms's webauthn handle straight to a
					// half-populated iam.User with a raw-bytes subject and no roles.
					webauthnMgr.SetUserResolver(userStorage)
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
		// Route user/role storage to the qrdb-backed stores so a GDPR export or
		// erasure operates on the same user record and roles the rest of the
		// stack uses; backendStorage would expose an empty user/role view.
		// Groups stay on backendStorage (qrdb has no Group type).
		UserStorage:    userStorage,
		SessionStorage: sessionStorage,
		GroupStorage:   backendStorage,
		RoleStorage:    roleStorage,
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

	// The system tenant (qrdbsdk.SystemTenantID) is provisioned by qrdb's
	// first-boot routine via WithInternalCall — it is the ONLY path allowed
	// to create a reserved tenant, and it treats already-exists as idempotent
	// success. By the time we reach this point it is guaranteed to exist, so
	// quicbaas must NOT attempt to create it (and must never re-create the
	// retired "default" tenant, which qrdb permanently reserves and rejects).
	// The orchestrator daemon-client bootstrap below registers its OAuth2 app
	// under qrdbsdk.SystemTenantID, which already has its tenant row in place.

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

	// (Default tenant was created earlier, before the orchestrator
	// daemon-client bootstrap that depends on it.)

	// First-boot bootstrap: when no admin assignments exist, create a
	// pending admin user and print an activation invite so the operator
	// can register their FIDO2 / PIV credential. Every QuicBaaS server
	// needs an authenticated admin regardless of whether the encrypted
	// seal barrier is in use — access control and storage encryption
	// are independent concerns.
	//
	// Password-based admin provisioning has been removed; admin enrollment
	// is cert-based (mTLS / PIV / WebAuthn). First-boot therefore always
	// runs, including when BootstrapAdminPIVCertPath is configured for
	// non-interactive PIV pre-provisioning.
	if bootstrapErr := s.firstBootBootstrap(context.Background(), components); bootstrapErr != nil {
		// Non-fatal: log the error but do not abort the server. The operator
		// can fix the cause (e.g., corrupt storage) and restart.
		s.logger.WithError(bootstrapErr).Warn("First-boot bootstrap failed — no admin credential will be created this boot")
	}

	// Reconcile against qrdb's canonical admin role (created by first-boot in
	// the system tenant) rather than seeding a parallel broker admin. This is a
	// no-op on the happy path; a missing role is logged so operators know a
	// fresh first-boot is required before admin authorization resolves.
	if err := reconcileAdminRole(context.Background(), identityStore); err != nil {
		s.logger.WithError(err).Warn("Canonical admin role not reconciled; admin authorization may be unavailable until first-boot completes")
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
	// The SPA router (cmd/quicbaas/frontend MainApp.svelte, route
	// 'invite-activate') maps the activation view at the path-parameter route
	// #/invite/:token — there is exactly ONE canonical entry point (pre-v1
	// stack, no compat aliases).
	activationURL := s.getIssuerURL() + "/ui/#/invite/" + token

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
// stored as a PIV-attested cert binding (indexed for GetUserByPIVThumbprint). It is
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
// login lookup key every login path computes and the lookup thumbprint of the
// PIV-attested cert binding. It is the single digest shared by bindAdminPIVCert
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

// getIssuerURL constructs the canonical external URL for this server — the
// single source of truth shared by the OIDC issuer claim, the first-boot
// activation banner, and the WebAuthn relying-party defaults.
//
// Loopback bind hosts (the empty / wildcard bind address and the IPv4/IPv6
// loopback literals) are canonicalised to "localhost" rather than an IP
// literal. This is mandated by the WebAuthn specification: the RP ID derived
// from this URL must be a registrable domain, and browsers reject IP literals
// as invalid domains. Binding still happens on the configured Server.Host;
// only the externally advertised URL is normalised.
func (s *Server) getIssuerURL() string {
	return issuerURLFromConfig(s.config)
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

	// The orchestrator client is provisioned under the control-plane system
	// tenant (qrdbsdk.SystemTenantID), which qrdb's first-boot always
	// provisions. Dispatching to daemons in other tenants requires a
	// per-tenant client app; see ensureOrchestratorDaemonClientForTenant for
	// that path. The system tenant hosts the orchestrator so the first-start
	// flow works without explicit tenant management; other tenants are
	// created on first dispatch.
	systemTenant := qrdbsdk.SystemTenantID
	scopes := iam.DefaultAgentRuntimeScopes()
	app, err := appManager.RegisterApp(ctx, systemTenant, iam.RegisterAppOptions{
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
			return nil, &ServerCauseError{Message: fmt.Sprintf("ensureOrchestratorDaemonClient: app %q exists but cache %q is missing — delete the app via `quicbaas iam app delete` and restart to re-bootstrap", OrchestratorDaemonClientAppName, path), Cause: err}
		}
		return nil, &ServerCauseError{Message: "ensureOrchestratorDaemonClient: register app", Cause: err}
	}

	client := &OrchestratorDaemonClient{
		ClientID:     app.ClientID,
		ClientSecret: app.ClientSecret, // plaintext — only visible here
		Scopes:       append([]string(nil), scopes...),
		TenantID:     systemTenant,
	}
	if err := writeOrchestratorDaemonClient(path, client); err != nil {
		return nil, &ServerCauseError{Message: "ensureOrchestratorDaemonClient: persist cache", Cause: err}
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

// AdminTenantID is the tenant that owns the canonical admin role and the
// bootstrap admin user. qrdb's first-boot seeds the built-in roles and the
// admin user in the system tenant; the broker reconciles against that single
// source of truth rather than minting a parallel admin in another tenant.
var AdminTenantID = qrdbsdk.SystemTenantID

// AdminRoleID is the deterministic, cluster-stable ID of qrdb's canonical
// "admin" role in the system tenant. It is NOT a broker-invented identifier:
// it resolves to the exact role object qrdb's first-boot creates and that the
// qrdb policy decision point resolves to admin permissions. Using qrdb's
// deterministic ID means a token carrying the admin role binds to the same
// role the PDP authorizes against — there is one admin role across the stack,
// never two.
var AdminRoleID = qrdbsdk.SystemRoleID(qrdbsdk.SystemTenantID, qrdbsdk.RoleNameAdmin)

// reconcileAdminRole verifies that qrdb's canonical "admin" role is present and
// addressable in the system tenant after first-boot has run. qrdb's first-boot
// (qrdbsdk.FirstBootInitialize) is the SOLE creator of the built-in admin role;
// this function does NOT mint a second one. It exists so a restart against an
// already-bootstrapped data dir, or a deployment whose first-boot was skipped,
// still surfaces a clear error when the canonical admin role is missing rather
// than silently rendering an empty Roles tab.
//
// Idempotent and read-only on the happy path: when the admin role already
// exists (the normal case) the function is a no-op. A missing role is returned
// as a typed not-found error so the caller can log and re-bootstrap. The role is
// resolved by qrdb's deterministic admin ID.
func reconcileAdminRole(ctx context.Context, identityStore *iam.IdentityStore) error {
	if identityStore == nil {
		return nil
	}
	_, err := identityStore.GetRole(ctx, AdminTenantID, AdminRoleID)
	if err == nil {
		return nil
	}
	if errors.Is(err, iam.ErrRoleNotFound) {
		// qrdb's first-boot is the sole creator of the admin role under its
		// deterministic id; a not-found here means the canonical admin role is
		// genuinely absent (first-boot skipped or storage wiped).
		return ErrAdminRoleMissing
	}
	// A transport/storage error (e.g. sealed barrier) is not a missing role —
	// surface it verbatim so the caller does not mistake it for a
	// reconciliation gap.
	return err
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
