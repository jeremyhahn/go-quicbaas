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
	"crypto/x509"
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
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/fips"
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
	pivActivator      *iam.PIVActivator

	// certLoginFactors enrolls the mtls auth factor that makes an admin
	// certificate binding resolvable by the unified login ceremony. Without it
	// a bound certificate authenticates the TLS handshake but fails
	// FinishLogin with "factor not found".
	certLoginFactors *iam.CertLoginFactorEnroller
}

// Compile-time check that iamComponents implements service.IAMComponents.
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

// GetInviteStore returns the invite token store.
func (c *iamComponents) GetInviteStore() iam.InviteStore {
	return c.inviteStore
}

// GetPIVActivator returns the PIV invite-activation service, or nil when PIV
// activation is not configured on this server (iam.piv_login disabled or no
// CA file).
func (c *iamComponents) GetPIVActivator() *iam.PIVActivator {
	return c.pivActivator
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

// newIAMBackendStorage builds the persistent IAM storage the server runs on.
//
// It exists as a named seam rather than an inline call so the wiring can be
// regression-tested: the logger argument is the only thing making a pre-KDF
// client secret announce itself, and dropping it changes no behaviour any
// functional test observes. ValidateClientSecret still refuses the record and
// the refusal is still collapsed into an opaque invalid_client before it
// reaches the caller — the ONLY difference is that the operator loses the one
// signal naming rotation as the remedy, exactly when they need it during an
// upgrade over legacy records (#118 F6, #166).
//
// Pinned by TestNewIAMBackendStorage_WarnsOnPreKDFSecret.
func newIAMBackendStorage(kv interfaces.KVStore, log iamStorageLogger) *iam.BackendStorage {
	return iam.NewBackendStorage(kv, iam.WithStorageLogger(log))
}

// iamStorageLogger is the logging surface IAM storage actually needs -- one
// leveled formatter, mirroring iam.WithStorageLogger's own parameter. Naming
// it here rather than taking *logger.Logger keeps the seam honest about its
// requirement and lets a test supply a capturing logger.
type iamStorageLogger interface {
	Warnf(format string, args ...interface{})
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
	iamKV := s.tenantRoutedKVStore()

	// Create the persistent IAM storage. The store satisfies TenantStorage,
	// UserStorage, GroupStorage, RoleStorage, PolicyStorage, AppStorage, and
	// TokenStorage so every IAM mutation survives daemon restarts.
	// The logger is wired so a pre-KDF client secret announces itself: the
	// refusal is collapsed into an opaque invalid_client before it reaches the
	// caller, so without this an upgrade over legacy records would look like a
	// broken token endpoint rather than a rotation the operator has to perform.
	backendStorage := newIAMBackendStorage(iamKV, s.logger)

	// Create tenant manager backed by the persistent IAM storage
	tenantManager := iam.NewDefaultTenantManager(backendStorage)

	// Wire the qrdb engine's TenantServicer so CreateTenant/DeleteTenant
	// initialise barrier, DEK, and quotas on the engine side BEFORE BaaS
	// metadata is written. This is the correctness gap identified in Phase D.5:
	// without this wiring, per-tenant barrier init and DEK lifecycle never fire.
	if node := s.getQRDBNode(); node != nil {
		if svcs := node.Services(); svcs != nil && svcs.Tenant != nil {
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
	if s.getQRDBNode() == nil {
		return nil, &QRDBUserStorageRequiredError{}
	}
	qrdbUserStorage := qrdbadapter.NewQRDBUserStorage(
		newPlatformUserServicerResolver(s),
		iamKV,
	)
	var userStorage iam.UserStorage = qrdbUserStorage

	// Wire the routed user store into the tenant manager (built above, before
	// userStorage existed) so DeleteTenant deprovisions the tenant's users
	// through the qrdb-backed store — purging the qrdb records and the
	// legacy-PIV index entries the legacy BackendStorage cascade misses.
	tenantManager.SetUserStorage(userStorage)

	// Create identity store with storage backends
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  userStorage,
		GroupStorage: backendStorage,
		RoleStorage:  roleStorage,
		Tenants:      tenantManager,
		Logger:       slog.Default(),
	})

	// Seed every new tenant's built-in roles through the SAME adapter
	// qrdbfirstboot.Initialize seeds the system tenant's roles through, so a
	// tenant created at runtime carries the identical role set — same names,
	// same deterministic IDs, same live permission projection. Without this a
	// fresh tenant has zero roles and cannot hold a user at all (#269).
	tenantManager.SetRoleSeeder(qrdbadapter.WrapRole(identityStore))

	// Create policy cache and engine
	policyCache := iam.NewPolicyCache(&iam.CacheConfig{
		TTL:               5 * time.Minute,
		MaxSize:           10000,
		BloomFilterSize:   1000000,
		BloomFilterHashes: 7,
	})
	// RBACAuthorizer wires PEPService/PolicyEngine.Evaluate onto the single
	// qrdb decision point (P6.1 end-state of the unified login-plane plan):
	// qrdb's own RBAC authorizer, decorated with a live-reloaded ABAC policy
	// set, is the WHOLE decision — the SAME store-resolved role grants and
	// ABAC overlay every other qrdb-fronted authz gate consults
	// (GetQRDBNodeClient().RBAC()). ABACSync/Simulator keep that live
	// decorator synchronized with the policy store on every CRUD mutation
	// and back SimulatePolicy's isolated draft-policy dry run, respectively.
	policyEngine := iam.NewPolicyEngine(iam.PolicyEngineOptions{
		Storage:        backendStorage,
		Cache:          policyCache,
		RBACAuthorizer: newQRDBPEPRBACAuthorizer(s),
		ABACSync:       newQRDBABACSync(s, tenantManager, backendStorage),
		Simulator:      newQRDBPolicySimulator(),
	})
	// Initial ABAC load: post-unseal, now that the IAM policy store is
	// readable and the qrdb node is already live (initializeIAMComponents
	// runs after node construction, see lifecycle.go), push every
	// previously stored policy into the node's live ABAC decorator before
	// the first request is evaluated.
	if syncErr := policyEngine.SyncABAC(context.Background()); syncErr != nil {
		s.logger.WithError(syncErr).Warn("Initial ABAC policy sync failed; live enforcement may not reflect previously stored policies")
	}

	// Create app manager
	appManager := iam.NewAppManager(backendStorage, tenantManager)

	// Create the audit logger backed by the embedded qrdb node's audit chain
	// (node_audit_logger.go) — the ONE audit plane every subsystem emits
	// onto (P5.2 of the login-plane plan): a durable, tamper-evident
	// HMAC-chained file log (or a queryable in-memory ring in embedded/test
	// contexts with no persistent audit path), never a per-tenant KV store
	// or an in-memory fallback that loses records on restart. The sink is
	// idempotently constructed here in case IAM init runs before the qrdb
	// node wiring populates it (unifiedModuleServerOptions,
	// qrdb_node.go) — both paths share the exact same sink instance.
	if s.qrdbAuditSink == nil {
		s.qrdbAuditSink = s.newQRDBAuditSink()
	}
	auditLogger := NewNodeAuditLogger(s.qrdbAuditSink)

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
			// Resolve operator config against those defaults and validate the
			// RESULT. Fail closed on the same terms as the derivation error
			// above: a relying party missing an RP ID or an origin accepts no
			// ceremony, and a node that boots with one presents a WebAuthn
			// surface that silently rejects every registration.
			// The relying party may create an account for an unknown email
			// only when the operator has switched self-service registration
			// on: that is the surface that registers one
			// (iam_register_self.go). go-xkms v0.9.2-alpha closed this by
			// default, so it has to be mapped rather than left to the
			// library's default.
			resolvedWebAuthn, resolveErr := webauthnFactoryConfigFromOperator(
				s.config.IAM.WebAuthn, issuerRPID, issuerRPOrigin,
				s.config.IAM.SelfServiceRegistration.Enabled)
			if resolveErr != nil {
				return nil, &IAMComponentInitError{Component: "WebAuthn relying-party configuration", Cause: resolveErr}
			}
			// Resolve the enrollment assurance policy and the AAGUID gate
			// BEFORE constructing the factory: both are applied inside the
			// per-tenant credential store, so a factory built without them
			// would serve ceremonies under the zero-value (strictest) policy
			// and refuse everything rather than enforce what the operator
			// configured. An unparseable assurance value is fatal — a typo
			// must not silently select a policy.
			assurance, assuranceErr := iam.ParseWebAuthnAssurance(s.config.IAM.WebAuthn.Assurance)
			if assuranceErr != nil {
				return nil, &IAMComponentInitError{Component: "WebAuthn enrollment assurance", Cause: assuranceErr}
			}
			// A failure to BUILD the allowlist is fatal too. It used to be
			// logged as "enforcement disabled", which turned an unreadable
			// allowlist file into a deployment that accepted every
			// authenticator — the operator's strictest setting failing open.
			aaguidAllowlist, aaguidErr := buildAAGUIDAllowlist(s.config.IAM.AAGUID)
			if aaguidErr != nil {
				return nil, &IAMComponentInitError{Component: "FIDO2 AAGUID allowlist", Cause: aaguidErr}
			}
			// The manufacturer attestation roots. An empty path is legal and
			// yields no trust store, which REFUSES hardware enrollment at
			// ceremony time with a message naming the setting — a configured
			// path that cannot be read or parsed is fatal, for the same reason
			// an unreadable allowlist is: an operator who configured trust
			// anchors and got a node that verifies nothing is the failure.
			attestationRoots, rootsErr := iam.NewWebAuthnAttestationRootsFromFile(
				s.config.IAM.WebAuthn.AttestationRootsFile)
			if rootsErr != nil {
				return nil, &IAMComponentInitError{Component: "WebAuthn attestation roots", Cause: rootsErr}
			}
			s.logger.Infof("iam.webauthn: enrollment assurance=%s, aaguid policy=%s (entries=%d), attestation roots=%d",
				assurance, aaguidAllowlist.Policy(), aaguidAllowlist.Len(), attestationRoots.Count())
			if !attestationRoots.Configured() && !assurance.AllowsSoftwareAdministrators() {
				s.logger.Warn("iam.webauthn: no attestation roots are configured " +
					"(iam.webauthn.attestation_roots_file), so no authenticator can prove it is hardware " +
					"and administrative enrollment will be refused. Configure the vendor attestation roots " +
					"this deployment trusts.")
			}
			if assurance.AllowsSoftwareAdministrators() {
				// Announced at WARN, unconditionally, on every boot. A
				// deployment running this policy has chosen to let a software
				// authenticator become an administrator credential; that choice
				// must be visible in the log of the node it applies to, not
				// only in the configuration file somebody wrote once.
				s.logger.Warn("iam.webauthn: SECURITY: assurance is insecure-allow-software-administrators — " +
					"a software or virtual authenticator can be enrolled as an ADMINISTRATOR credential. " +
					"This setting is for automated test environments only; set iam.webauthn.assurance to " +
					"\"hardware\" for any deployment holding real administrators.")
			}

			factoryParams := iam.WebAuthnFactoryParams{
				Config:              resolvedWebAuthn,
				QRDB:                platformQRDB,
				AttestationRecorder: s.qrdbAttestationRecorder,
				Settings:            iam.NewTenantManagerWebAuthnSettings(tenantManager),
				Assurance:           assurance,
				AAGUIDAllowlist:     aaguidAllowlist,
				Admins:              iam.NewWebAuthnAdminResolver(userStorage),
				AttestationRoots:    attestationRoots,
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
					// Cascade user deletion into the WebAuthn credential
					// store. The store is keyed by (tenant, email), so a
					// credential left behind by a deleted user is inherited
					// by the next account created under the same email and
					// blocks its registration ceremony permanently.
					identityStore.SetWebAuthnCredentialReaper(webauthnMgr)
					// Tenant teardown deprovisions users through the user
					// store directly, not through IdentityStore.DeleteUser,
					// so it needs the same revocation wired separately or
					// the tenant's credentials outlive the tenant.
					tenantManager.SetWebAuthnCredentialReaper(webauthnMgr)
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
		// The identity store verifies that an impersonation target really
		// is a user of the target tenant before a session is minted; the
		// manager fails closed without it (#357).
		TargetResolver: identityStore,
	})
	if err != nil {
		return nil, &IAMComponentInitError{Component: "admin manager", Cause: err}
	}

	// Create GDPR manager
	// A typed nil would satisfy the interface and then panic on first use, so
	// the reaper is promoted to the interface ONLY when a manager was actually
	// built. Erasure on a node without WebAuthn then finds a nil dependency and
	// skips the category, which is the honest answer for a node that has no
	// credentials to erase.
	var webauthnReaper iam.WebAuthnCredentialReaper
	if webauthnMgr != nil {
		webauthnReaper = webauthnMgr
	}

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
		// An erasure that leaves the subject's authenticators behind has not
		// erased them: a stored credential is personal data, and it would go on
		// to block the next account issued that address (go-quicbaas#273).
		WebAuthnCredentials: webauthnReaper,
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
	//
	// It binds to the same per-tenant KVStore as the IAM stores above, so an
	// agent registration is durable across restarts and a barrier-isolated
	// tenant's fleet lives inside that tenant's own barrier (own DEK +
	// namespace) rather than a shared table.
	if s.agentRegistry == nil {
		s.agentRegistry = orchestration.NewAgentRegistry(
			iamKV,
			orchestration.WithStoreAuditor(newOrchestrationStoreAuditor(func() iam.AuditLogger { return auditLogger })),
		)
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
	//
	// It is go-qrdb's per-tenant invitation store, not a quicbaas-local one.
	// Invitations live in each tenant's own store, so a token issued for one
	// tenant is simply ABSENT from another rather than found-then-rejected —
	// isolation is structural. Token generation, digesting and single-use
	// enforcement all belong to qrdb; this layer only routes and translates.
	inviteStore, inviteErr := s.buildTenantInviteStore()
	if inviteErr != nil {
		return nil, inviteErr
	}

	// PIV invite activation (POST /api/v1/iam/invites/{token}/activate/piv):
	// bind a chain-verified client certificate to a pending user and transition
	// them to active. Gated by the SAME iam.piv_login trust anchor as the PIV
	// login endpoint, so a certificate that activates is guaranteed to verify
	// at login time. A misconfigured CA bundle fails startup (fail closed),
	// mirroring the PIV login TLS pool behavior in lifecycle_rest.go.
	var pivActivator *iam.PIVActivator
	if s.config.IAM.PIVLogin.Enabled && s.config.IAM.PIVLogin.CAFile != "" {
		roots, rootsErr := loadPIVActivationRoots(s.config.IAM.PIVLogin.CAFile)
		if rootsErr != nil {
			return nil, &IAMComponentInitError{Component: "PIV activator", Cause: rootsErr}
		}
		pivActivator, err = iam.NewPIVActivator(iam.PIVActivatorDependencies{
			Roots:          roots,
			UserStorage:    userStorage,
			InviteStore:    inviteStore,
			AuditLogger:    auditLogger,
			TrustedIssuers: s.config.IAM.PIVLogin.TrustedIssuers,
		})
		if err != nil {
			return nil, &IAMComponentInitError{Component: "PIV activator", Cause: err}
		}
		s.logger.Infof("PIV invite activation enabled (roots: %s)", s.config.IAM.PIVLogin.CAFile)
	}

	// Certificate bindings (bootstrap admin PIV cert) must ALSO enroll
	// an mtls auth factor in qrdb's UserAuthFactor store — the store the unified
	// login ceremony resolves against. The servicer is resolved lazily per call
	// (the embedded node exposes it only after unseal), so the enroller can be
	// constructed here regardless of the node's current seal state.
	certLoginFactors, err := iam.NewCertLoginFactorEnroller(newQRDBUserAuthFactorServicerProxy(s))
	if err != nil {
		return nil, &IAMComponentInitError{Component: "certificate login factor enroller", Cause: err}
	}

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
		pivActivator:      pivActivator,
		certLoginFactors:  certLoginFactors,
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

	// Converge every tenant's built-in roles onto THIS build's definitions.
	// This is the last step of IAM initialization, and deliberately so: qrdb
	// resolves the built-in definitions through the permission registry at call
	// time, and both of this process's registration sites — pkg/permission's
	// Register() from NewServer, and the module's registerRoleGrants() from
	// apimodule.NewModule during embedded-node construction — have already run
	// by the time startService reaches here. Running earlier would converge
	// every tenant onto a definition missing the module's own verbs and report
	// success (go-quicbaas#573).
	//
	// Non-fatal, like the admin-role reconciliation above: a tenant left on a
	// stale grant is a degraded authorization surface an operator can repair
	// with the reconcile verb, not a reason to refuse the boot.
	if err := s.reconcileTenantBuiltInRoles(context.Background(), identityStore, tenantManager); err != nil {
		s.logger.WithError(err).Warn("Built-in tenant roles not reconciled; tenants seeded before a permission change keep their stale grants until POST /api/v1/tenants/{tenant_id}/roles/reconcile is run")
	}

	s.logger.Info("IAM components initialized successfully")
	return components, nil
}

// adminBundleDirName is the sub-directory of the data dir that receives the
// admin certificate (and, for the generate-and-distribute path, its private
// key) issued by first-boot. The operator copies it to their workstation and
// authenticates over mTLS with it.
const adminBundleDirName = "admin"

// adminCertFileName / adminKeyFileName name the files written into
// <data-dir>/admin by the generate-and-distribute enrollment path.
const (
	adminCertFileName = "admin.crt"
	adminKeyFileName  = "admin.key"
)

// writeFirstBootBannerFn is the function used to emit the enrollment banner.
// It is a package-level variable so unit tests can capture the output without
// touching os.Stderr.
var writeFirstBootBannerFn = func(w interface{ WriteString(string) (int, error) }, b adminEnrollmentBanner) {
	writeFirstBootBanner(w, b)
}

// firstBootBootstrap detects a fresh installation (zero admin assignments) and
// runs qrdb's canonical first-boot: system tenant, built-in roles, platform
// admin user, and — the point of this function — the admin's mTLS enrollment.
//
// Admin enrollment is CERT-BASED. There is no invite token and no activation
// URL; §08 of the bootstrap/auth architecture deleted that mechanism. Two
// enrollment paths, both implemented inside qrdb's firstboot and both ending
// with the certificate's canonical SPKI thumbprint bound to the admin user:
//
//	Path B (preferred, CSR-sign): the operator points
//	  auth.bootstrap_admin_csr_path at an admin-supplied CSR. The xkms CA signs
//	  it and the admin's private key never reaches this process.
//	Path A (generate-and-distribute): with no CSR configured, the xkms CA
//	  generates the keypair and certificate. Both are written to
//	  <data-dir>/admin/ for the operator to collect and remove.
//
// Path (d) — auth.bootstrap_admin_piv_cert_path — remains for deployments that
// pre-provision the admin certificate out of band; it binds that certificate
// instead of issuing one.
//
// On every subsequent boot the admin assignment exists so the function is a
// no-op.
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

	deps, users, err := s.firstBootDependencies(c)
	if err != nil {
		return err
	}

	// Delegate tenant + role + admin-user creation AND admin-mTLS enrollment to
	// qrdb's canonical bootstrap orchestrator. qrdbsdk.BootstrapSystem is the
	// SDK's declared daemon-init entry point ("Higher-level projects drive
	// bootstrap via these"): it brings up the SYSTEM barrier per the resolved
	// strategy (none / passphrase / shamir — idempotent when the barrier is
	// already initialized/unsealed) and then runs first-boot, which creates the
	// system tenant ("system"), registers the built-in roles, creates the
	// platform admin user with the Admin@system grant, issues the admin
	// certificate, and binds its SPKI thumbprint through deps.Binder. quicbaas
	// IAM-specific concerns (admin assignments, login-factor enrollment) layer
	// on top AFTER the orchestrator returns. Consumed via the qrdb SDK (never
	// pkg/bootstrap or pkg/firstboot directly).
	//
	// Mirroring qrdbd's runFirstBoot: the orchestrator is engaged only when
	// seal is enabled and a barrier registry is reachable; with seal disabled
	// the direct first-boot path is kept — BootstrapSystem requires a non-nil
	// barrier registry, and forcing a synthetic barrier would initialize seal
	// state the operator never asked for.
	result, err := s.runBootstrapSystem(ctx, deps)
	if err != nil {
		return err
	}

	// Look up the bootstrap admin user first-boot just created (or confirmed
	// already exists). The adapter synthesizes the email deterministically, and
	// on the create path it also remembers the record it wrote, so both lookups
	// resolve the same user.
	adminTenantID := qrdbsdk.SystemTenantID
	user := users.CreatedUser()
	if user == nil {
		adminEmail := "admin@" + adminTenantID + ".qrdb.local"
		user, err = c.identityStore.GetUserByEmail(ctx, adminTenantID, adminEmail)
		if err != nil {
			return &FirstBootBootstrapError{Op: "lookup bootstrap admin user", Cause: err}
		}
	}

	// Stash the bootstrap admin's UserID on the server so later admin binding
	// paths can find them without re-querying the identity store.
	s.bootstrapAdminUserID = user.ID
	s.bootstrapAdminTenantID = adminTenantID

	// Make the issued certificate usable for `quicbaas auth login --method
	// mtls`: enroll it as an mtls auth factor in qrdb's UserAuthFactor store.
	// This runs BEFORE the admin assignment so a failure leaves first-boot
	// incomplete and the next boot retries the whole (idempotent) sequence
	// rather than leaving a privileged admin who cannot log in.
	if err := s.enrollFirstBootAdminCert(ctx, c, adminTenantID, user.ID, result); err != nil {
		return err
	}

	// Grant the user a super_admin assignment so they can manage the system
	// after collecting their certificate. Nil TenantIDs scopes the
	// assignment to the SYSTEM tenant alone (iam.AdminAssignment.TenantIDs,
	// #357): it passes unscoped and system-tenant permission checks —
	// everything platform management needs — but never a check targeting a
	// customer tenant, so this admin cannot, for example, impersonate a
	// tenant's users. Cross-tenant administrative reach requires a separate
	// assignment naming the tenant explicitly.
	_, err = c.adminManager.CreateAssignment(ctx, iam.CreateAdminAssignmentOptions{
		UserID:    user.ID,
		AdminRole: iam.AdminRoleSuperAdmin,
		TenantIDs: nil, // system-tenant scope only (#357)
		GrantedBy: "system:first-boot",
	})
	if err != nil {
		return &FirstBootBootstrapError{Op: "create admin assignment", Cause: err}
	}

	// Non-interactive PIV binding (path d, pre-provisioned deployments and
	// integration clusters). When BootstrapAdminPIVCertPath is configured, read
	// the cert, bind both its canonical SPKI thumbprint and the legacy hex
	// digest, and transition the user to active.
	if path := s.config.Auth.BootstrapAdminPIVCertPath; path != "" {
		if err := bindAdminPIVCert(ctx, c, adminTenantID, user.ID, path); err != nil {
			return &FirstBootBootstrapError{Op: "bind bootstrap admin PIV cert", Cause: err}
		}
		s.logger.Infof("Bootstrap admin %s bound to PIV cert at %s", user.ID, path)
		return nil
	}

	// Enroll the operator's chosen HARDWARE login factors (--admin-factor).
	// This runs after the certificate exists because the mtls factor binds to
	// it, and before the bundle is written so a failed passkey ceremony does
	// not leave an operator believing enrollment completed.
	if err := s.enrollAdminLoginFactors(ctx, c, adminTenantID, user.ID, result); err != nil {
		return err
	}

	// Persist the issued bundle for the operator and tell them where it is.
	banner, err := s.writeAdminBundle(result)
	if err != nil {
		return err
	}

	// Print the enrollment banner to stderr. Use os.Stderr directly — NOT the
	// structured logger — so operators see it regardless of log level.
	writeFirstBootBannerFn(os.Stderr, banner)

	return nil
}

// firstBootDependencies assembles the seam set handed to qrdb's first-boot,
// including the admin-mTLS seams the enrollment depends on. It returns the
// dependency set and the user adapter (which doubles as the CertBinder and
// remembers the admin record it creates).
//
// Which enrollment path qrdb takes is decided by what this function puts in
// deps, and quicbaas generates nothing on either one:
//
//	Path B (auth.bootstrap_admin_csr_path set): deps.AdminCSRPEM carries the
//	  admin's own CSR, qrdb signs it through deps.Signer, and the admin's
//	  private key never reaches this host.
//	Path A (no CSR configured): deps.AdminCSRPEM stays empty, so qrdb calls
//	  deps.CA.GenerateAdminCert — go-xkms's own generate-and-distribute
//	  implementation (pkg/init/firstboot_adapter.go). go-xkms creates the admin
//	  keypair, signs the CSR with the same CA, and returns both, which qrdb
//	  surfaces as FirstBootResult.AdminKeyPEM for the caller to write out.
//
// Path A used to be forked here: quicbaas generated the admin keypair and CSR
// itself and fed them in as Path B. That was a Layer Ownership violation with
// no upside — the code it duplicated is go-xkms's, reached through a seam this
// function already populates (go-quicbaas#910).
//
// The CA seams are populated from the server's xkms CA, which initializeCA
// brings up before IAM init. When no CA is available (TLS disabled and no
// external CA supplied) the cert seams stay nil and qrdb's first-boot creates
// the tenant/roles/admin without issuing a certificate — the admin then
// enrolls through the PIV path or a later ceremony.
func (s *Server) firstBootDependencies(c *iamComponents) (*qrdbsdk.FirstBootDependencies, *qrdbadapter.UserAdapter, error) {
	users := qrdbadapter.WrapUser(c.identityStore)
	roles := qrdbadapter.WrapRole(c.identityStore)
	deps := &qrdbsdk.FirstBootDependencies{
		Tenants: qrdbadapter.WrapTenant(c.tenantManager),
		Roles:   roles,
		// The same adapter answers "which role is named N in tenant T".
		//
		// First-boot needs it on the RE-RUN path: it is idempotent, so a second
		// run over an already-seeded system tenant finds the built-in roles
		// present rather than creating them, and it must then bind the admin's
		// grant to the identifier those roles are ACTUALLY persisted under. A
		// cluster seeded before system role IDs moved off UUIDv5 keeps the
		// legacy identifiers forever, so a derived identifier would name no
		// role and the grant would silently confer nothing. With nothing able
		// to resolve them, first-boot refuses to guess and fails with
		// UnresolvableSystemRoleError instead.
		//
		// Wired explicitly rather than left to qrdb's fallback (which would
		// pick the Roles surface up on its own now that it implements the
		// interface): the dependency this bootstrap has on a name lookup
		// should be visible here, not inferred from a type assertion.
		RoleResolver: roles,
		Users:        users,
		Binder:       users,
		Logger:       slog.Default(),
		// Strict (least-privilege) RBAC under FIPS 140-3: the bootstrap admin
		// keeps only the canonical Admin@system grant and the all-permission
		// Owner role is never seeded.
		SeparationOfDuties: fips.Enabled(),
	}

	ca := s.ceremonyCAAdapter()
	if ca == nil {
		s.logger.Warn("First-boot: no certificate authority available — the platform admin will be created WITHOUT an mTLS certificate")
		return deps, users, nil
	}
	deps.CA = ca
	deps.Signer = ca

	// FAIL CLOSED without a SPIFFE trust domain.
	//
	// The xkms CAAdapter stamps the tenant-scoped SPIFFE URI SAN
	// (spiffe://<trust-domain>/tenant/system/user/admin) onto the admin
	// certificate ONLY when a trust domain is configured, and REST's
	// deriveLoginTenant resolves and PINS the login tenant from exactly that
	// SAN. With no trust domain the certificate carries no SPIFFE SAN, login
	// falls back to home-realm discovery, and a freshly initialized node has no
	// HRD routing entries — so the credential would be unusable.
	//
	// Issuing it anyway (with a warning) is the appears-to-work-but-doesn't
	// failure mode: the operator receives a real certificate and only discovers
	// at first login that it cannot authenticate. Refuse instead.
	//
	// The two rejected alternatives, recorded so they are not re-litigated:
	//   - Synthesizing a trust domain (e.g. from --hostname) would bake a
	//     PINNED security identifier into every issued certificate; configuring
	//     a real trust domain later would silently invalidate all of them.
	//   - Seeding a system-tenant home-realm entry would make "system"
	//     resolvable for ANY unrouted login attempt, permanently widening the
	//     authentication surface to buy first-boot convenience.
	// Requiring the value invents no identifier and widens nothing; the
	// operator must declare a trust domain eventually regardless.
	//
	// This is the authoritative enforcement point because it is where the
	// certificate is issued: it covers `quicbaasd start` against a data dir
	// with no init marker (a supported path — see resolveBarrierStrategy case
	// 3) and embedded/library consumers that never invoke the CLI. The CLI adds
	// an earlier fail-fast so the operator learns before any side effect.
	if s.config.IAM.SPIFFETrustDomain == "" {
		return nil, nil, &FirstBootBootstrapError{
			Op:    "issue admin certificate",
			Cause: ErrNoSPIFFETrustDomain,
		}
	}

	if path := s.config.Auth.BootstrapAdminCSRPath; path != "" {
		csrPEM, err := os.ReadFile(path) //nolint:gosec // path comes from operator config
		if err != nil {
			return nil, nil, &FirstBootBootstrapError{Op: "read admin CSR " + path, Cause: err}
		}
		if len(csrPEM) == 0 {
			return nil, nil, &FirstBootBootstrapError{Op: "read admin CSR " + path, Cause: ErrEmptyAdminCSR}
		}
		deps.AdminCSRPEM = csrPEM
		return deps, users, nil
	}

	// No operator CSR: leave deps.AdminCSRPEM empty so qrdb selects Path A and
	// go-xkms's CAAdapter.GenerateAdminCert creates the keypair. The key comes
	// back on FirstBootResult.AdminKeyPEM.
	return deps, users, nil
}

// ErrNoSPIFFETrustDomain reports that first-boot was asked to issue the admin
// certificate with no iam.spiffe_trust_domain configured. Without a trust
// domain the certificate carries no tenant SPIFFE SAN and login cannot resolve
// a tenant from it, so the credential would be unusable.
var ErrNoSPIFFETrustDomain = errors.New(
	"first-boot: iam.spiffe_trust_domain is required to issue the admin certificate " +
		"(example: iam.spiffe_trust_domain: quicbaas.example.test)")

// ErrNoDataDirForAdminBundle reports that first-boot issued an admin credential
// but storage.data_dir is unset, so there is no safe place to write it.
var ErrNoDataDirForAdminBundle = errors.New("first-boot: storage.data_dir is required to write the admin bundle")

// ErrEmptyAdminCSR reports that auth.bootstrap_admin_csr_path pointed at an
// empty file. Falling through to the generate-and-distribute path would
// silently produce a private key the operator did not ask this process to
// hold, so the bootstrap fails closed instead.
var ErrEmptyAdminCSR = errors.New("first-boot: configured admin CSR file is empty")

// enrollFirstBootAdminCert enrolls the certificate first-boot issued as an mtls
// login factor. It is a no-op when no certificate was issued (no CA wired, or
// an idempotent re-run where the admin already existed).
func (s *Server) enrollFirstBootAdminCert(
	ctx context.Context,
	c *iamComponents,
	tenantID, userID string,
	result *qrdbsdk.FirstBootResult,
) error {
	if result == nil || len(result.AdminCertPEM) == 0 {
		return nil
	}
	cert, err := parseCertLoginPEM(result.AdminCertPEM)
	if err != nil {
		return &FirstBootBootstrapError{Op: "parse issued admin certificate", Cause: err}
	}
	if err := enrollAdminCertLoginFactor(ctx, c, tenantID, userID, cert, adminCertBindingSourceFirstBoot); err != nil {
		return &FirstBootBootstrapError{Op: "enroll admin mtls login factor", Cause: err}
	}
	s.logger.Infof("Bootstrap admin %s enrolled with mTLS certificate (SPKI thumbprint %s)",
		userID, result.AdminCertThumbprint)
	return nil
}

// adminEnrollmentBanner carries the operator-facing result of first-boot admin
// enrollment: where the issued material landed and how the admin authenticates.
type adminEnrollmentBanner struct {
	// CertPath is the file the issued admin certificate was written to. Empty
	// when no certificate was issued.
	CertPath string

	// KeyPath is the file the admin private key was written to. Empty for the
	// CSR-sign path (the admin already holds the key) and when no certificate
	// was issued.
	KeyPath string

	// Thumbprint is the canonical SPKI thumbprint bound to the admin user.
	Thumbprint string
}

// writeAdminBundle persists the certificate (and, for the
// generate-and-distribute path, the private key) first-boot issued into
// <data-dir>/admin so the operator can collect it. It returns the banner
// describing what was written; when no certificate was issued the banner is
// empty and nothing is written.
func (s *Server) writeAdminBundle(result *qrdbsdk.FirstBootResult) (adminEnrollmentBanner, error) {
	var banner adminEnrollmentBanner
	if result == nil || len(result.AdminCertPEM) == 0 {
		return banner, nil
	}
	banner.Thumbprint = result.AdminCertThumbprint

	// Refuse to write anywhere but the configured data dir. With an empty
	// DataDir, filepath.Join would yield the RELATIVE path "admin", dropping the
	// admin private key into whatever the process CWD happens to be — the source
	// tree in development. Fail closed instead; the certificate is already bound
	// to the admin, so the operator can re-issue rather than hunt for a key in an
	// unexpected directory.
	if s.config.Storage.DataDir == "" {
		return banner, &FirstBootBootstrapError{
			Op:    "write admin bundle",
			Cause: ErrNoDataDirForAdminBundle,
		}
	}

	dir := filepath.Join(s.config.Storage.DataDir, adminBundleDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return banner, &FirstBootBootstrapError{Op: "create admin bundle dir " + dir, Cause: err}
	}

	certPath := filepath.Join(dir, adminCertFileName)
	if err := os.WriteFile(certPath, result.AdminCertPEM, 0o600); err != nil {
		return banner, &FirstBootBootstrapError{Op: "write " + certPath, Cause: err}
	}
	banner.CertPath = certPath

	// AdminKeyPEM is nil for the CSR-sign path (Path B) and for an adopted
	// certificate (Path C) — the admin kept their key and there is nothing here
	// to distribute. On Path A it holds the keypair go-xkms generated.
	if len(result.AdminKeyPEM) > 0 {
		keyPath := filepath.Join(dir, adminKeyFileName)
		if err := os.WriteFile(keyPath, result.AdminKeyPEM, 0o600); err != nil {
			return banner, &FirstBootBootstrapError{Op: "write " + keyPath, Cause: err}
		}
		banner.KeyPath = keyPath
	}
	return banner, nil
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

// ErrCertPEMEmpty indicates the certificate PEM input is empty.
var ErrCertPEMEmpty = errors.New("cert pem: empty input")

// ErrCertPEMNoBlock indicates the certificate PEM input has no PEM block.
var ErrCertPEMNoBlock = errors.New("cert pem: no PEM block")

// CertPEMParseError indicates the certificate PEM body could not be
// parsed into an X.509 certificate.
type CertPEMParseError struct {
	Reason string
}

// Error implements the error interface.
func (e *CertPEMParseError) Error() string {
	return "cert pem: parse failed: " + e.Reason
}

// parseCertLoginPEM decodes the single PEM-encoded certificate an admin binding
// path was handed. The binding needs the parsed certificate itself — not just a
// digest — because the mtls login factor is enrolled from both the certificate
// DER and its SubjectPublicKeyInfo.
func parseCertLoginPEM(certPEM []byte) (*x509.Certificate, error) {
	if len(certPEM) == 0 {
		return nil, ErrCertPEMEmpty
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, ErrCertPEMNoBlock
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, &CertPEMParseError{Reason: err.Error()}
	}
	return cert, nil
}

// bindAdminPIVCert reads a PEM-encoded client certificate from path, enrolls it
// as an mtls login factor, binds its thumbprints to the bootstrap admin user,
// and transitions the user to active. Used by the non-interactive
// pre-provisioned bootstrap (auth.bootstrap_admin_piv_cert_path).
//
// The factor enrollment comes FIRST — see enrollAdminCertLoginFactor for the
// ordering rationale. Without it the certificate authenticates the TLS handshake
// but `quicbaas auth login --method mtls` fails FinishLogin with
// "factor not found", because the login ceremony resolves qrdb's UserAuthFactor
// store rather than the user-side thumbprint index written below.
func bindAdminPIVCert(ctx context.Context, c *iamComponents, tenantID, userID, path string) error {
	certPEM, err := os.ReadFile(path) //nolint:gosec // path comes from operator config
	if err != nil {
		return &PIVBootstrapError{Op: "read PIV cert", Path: path, Cause: err}
	}
	cert, err := parseCertLoginPEM(certPEM)
	if err != nil {
		// Distinguish "this file is not PEM at all" from "the PEM body is not a
		// certificate" so the operator knows which half of the input is wrong.
		if errors.Is(err, ErrCertPEMEmpty) || errors.Is(err, ErrCertPEMNoBlock) {
			return &PIVBootstrapError{Op: "decode PIV cert PEM", Path: path, Cause: err}
		}
		return &PIVBootstrapError{Op: "parse PIV cert", Path: path, Cause: err}
	}

	if err := enrollAdminCertLoginFactor(ctx, c, tenantID, userID, cert, adminCertBindingSourceBootstrap); err != nil {
		return &PIVBootstrapError{Op: "enroll bootstrap admin mtls login factor", Path: path, Cause: err}
	}

	// Bind BOTH thumbprints, exactly as the interactive PIV activation path
	// does (see pkg/iam/piv.go): the canonical SPKI thumbprint that qrdb's
	// UserByCertThumbprint index resolves natively and survives renewal with
	// the same key, plus the legacy hex digest of the certificate DER that the
	// broker-side fallback index resolves.
	thumbprints := []string{
		qrdbsdk.ComputeSPKIThumbprint(cert),
		xkmssdk.PIVThumbprintSHA256(cert),
	}
	active := iam.LifecycleActive
	if _, err := c.identityStore.UpdateUser(ctx, tenantID, userID, iam.UpdateUserOptions{
		Lifecycle:          &active,
		PIVCertThumbprints: &thumbprints,
	}); err != nil {
		return &PIVBootstrapError{Op: "update bootstrap admin with PIV thumbprint", Path: path, Cause: err}
	}
	return nil
}

// Admin certificate binding provenance recorded on the enrolled login factor.
const (
	// adminCertBindingSourceBootstrap marks the non-interactive
	// auth.bootstrap_admin_piv_cert_path binding.
	adminCertBindingSourceBootstrap = "bootstrap_admin_cert"

	// adminCertBindingSourceFirstBoot marks the certificate qrdb's first-boot
	// issued (generate-and-distribute) or CSR-signed for the platform admin.
	adminCertBindingSourceFirstBoot = "first_boot_admin_cert"
)

// AdminCertFactorUnavailableError reports that an admin certificate binding was
// attempted before the components that make the certificate usable for login
// were wired. It fails the binding closed: silently skipping the enrollment is
// exactly the split-store defect that made a bound certificate unable to log in.
type AdminCertFactorUnavailableError struct {
	// Component names the missing dependency.
	Component string
}

// Error implements the error interface.
func (e *AdminCertFactorUnavailableError) Error() string {
	if e == nil || e.Component == "" {
		return "admin cert binding: login factor enrollment is unavailable"
	}
	return "admin cert binding: login factor enrollment is unavailable: " + e.Component + " is not wired"
}

// enrollAdminCertLoginFactor enrolls the admin certificate as an mtls auth
// factor in qrdb's UserAuthFactor store — the store go-qrdb's LoginResolver
// reads through LookupByCredentialKey. This is what makes a bound certificate
// resolvable by `quicbaas auth login --method mtls`; the user-side PIV
// thumbprint index the callers write afterwards serves a DIFFERENT lookup (the
// REST PIV login handler's user resolution).
//
// ORDERING (deliberate): callers enroll the factor BEFORE persisting the
// binding, so a failure leaves nothing written and the operator sees a clean
// no-op. Binding first and compensating on failure was rejected because undoing
// a factor requires a FORCE removal, which qrdb escalates into disabling the
// user when it is their last active factor — a destructive action to take on a
// bootstrap error path. See pkg/iam/cert_login_factor.go.
func enrollAdminCertLoginFactor(
	ctx context.Context,
	c *iamComponents,
	tenantID, userID string,
	cert *x509.Certificate,
	source string,
) error {
	if c == nil || c.certLoginFactors == nil {
		return &AdminCertFactorUnavailableError{Component: "certificate login factor enroller"}
	}
	qrdbUserID, err := iam.ParseUserID(userID)
	if err != nil {
		return err
	}
	return c.certLoginFactors.Enroll(ctx, iam.CertLoginFactorEnrollment{
		UserID:      qrdbUserID,
		Certificate: cert,
		Label:       adminCertFactorLabel(cert),
		Metadata:    adminCertFactorMetadata(tenantID, userID, source, cert),
	})
}

// adminCertFactorLabel derives the operator-visible factor label from the bound
// certificate's subject, falling back to a generic label for a certificate with
// no common name.
func adminCertFactorLabel(cert *x509.Certificate) string {
	if cn := cert.Subject.CommonName; cn != "" {
		return "admin certificate (" + cn + ")"
	}
	return "admin certificate"
}

// adminCertFactorMetadata projects the non-secret binding provenance onto the
// factor record: which binding path created it, the owning tenant and broker
// user, and the certificate's subject / serial / expiry. It never carries key
// material.
func adminCertFactorMetadata(tenantID, userID, source string, cert *x509.Certificate) map[string]string {
	return map[string]string{
		"binding_source": source,
		"tenant_id":      tenantID,
		"broker_user_id": userID,
		"cert_subject":   cert.Subject.String(),
		"cert_serial":    cert.SerialNumber.String(),
		"cert_not_after": cert.NotAfter.UTC().Format(time.RFC3339),
	}
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

// writeFirstBootBanner writes the admin enrollment banner to w. Extracted for
// testability.
//
// Admin enrollment is cert-based: first-boot already issued (or CSR-signed) the
// platform admin certificate and bound its SPKI thumbprint to the admin user.
// The banner tells the operator where that material is and how to use it. There
// is no invite token and no activation URL — §08 deleted that mechanism.
func writeFirstBootBanner(w interface{ WriteString(string) (int, error) }, b adminEnrollmentBanner) {
	const sep = "============================================================"
	lines := []string{"", sep, "  FIRST-BOOT COMPLETE: platform admin created."}

	if b.CertPath == "" {
		lines = append(lines,
			"",
			"  No certificate authority was available, so NO admin certificate",
			"  was issued. Enable TLS (or supply an external CA) and restart, or",
			"  pre-provision one via auth.bootstrap_admin_piv_cert_path.",
		)
	} else {
		lines = append(lines,
			"",
			"  Admin mTLS certificate:",
			"        "+b.CertPath,
		)
		if b.KeyPath != "" {
			lines = append(lines,
				"  Admin private key (generate-and-distribute path):",
				"        "+b.KeyPath,
				"",
				"  Copy BOTH files to the administrator's workstation over a secure",
				"  channel and DELETE them from this host. To keep the private key",
				"  off this host entirely, use the CSR-sign path instead: set",
				"  auth.bootstrap_admin_csr_path to an admin-supplied CSR.",
			)
		} else {
			lines = append(lines,
				"",
				"  The private key never reached this host (CSR-sign path). Give the",
				"  certificate above to the administrator who holds the key.",
			)
		}
		lines = append(lines,
			"",
			"  Bound SPKI thumbprint: "+b.Thumbprint,
			"",
			"  Authenticate with:",
			"        quicbaas auth login --method mtls",
		)
	}

	lines = append(lines,
		"",
		"  This banner appears only on first boot.",
		sep,
		"",
	)
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
	GetWebAuthnManager(ctx context.Context) (service.WebAuthnManager, error)
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
	// WebAuthn credential administration over gRPC (and the Unix socket that
	// delegates to it). The service is built from the components already
	// resolved here so gRPC runs the SAME user-id-to-email translation REST,
	// the embedded transport and MCP do; a nil WebAuthn manager makes the
	// verbs report unavailable rather than misresolve.
	webauthnMgr, _ := getter.GetWebAuthnManager(ctx)
	iamAdapter.SetWebAuthnCredentials(
		service.NewWebAuthnCredentialServiceFor(identityStore, webauthnMgr))
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

	// Register Audit service. The audit logger is ALWAYS the node-backed
	// NodeAuditLogger (node_audit_logger.go) once IAM composes — there is
	// no in-memory fallback (P5.2: a fallback that loses records on
	// restart is the defect the unified audit plane exists to eliminate).
	// A nil return here means IAM itself failed to compose, in which case
	// skipping Audit gRPC registration is correct: nothing else registered
	// either.
	auditLogger, _ := getter.GetAuditLogger(ctx)
	if auditLogger == nil {
		s.logger.Error("Audit logger unavailable, skipping Audit gRPC registration")
		return
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

	// The dispatch client's grant: the three service-surface scopes a remote
	// daemon's gRPC spawn server checks on every inbound RPC. It is a grant,
	// not a label — the token the central presents carries exactly what this
	// application record holds (go-quicbaas#653) — and it is the whole of the
	// central's authority AS A CLIENT of its own fleet. It confers nothing on
	// the control plane, where the central acts under its own identity.
	scopes := iam.DefaultAgentRuntimeScopes()

	// Fast path: credentials already cached on disk. Trust the cache — on a
	// corrupted file we re-bootstrap which is cheap. The cache short-circuits
	// creation, so converge the grant here too: an application registered by a
	// build that had no grants would otherwise keep its cached secret forever
	// and never acquire one, leaving the fleet undispatchable after upgrade.
	if existing, err := readOrchestratorDaemonClient(path); err == nil && existing != nil {
		if appManager != nil {
			if _, grantErr := appManager.GrantScopes(ctx, existing.TenantID, existing.ClientID, scopes); grantErr != nil {
				return nil, &ServerCauseError{Message: "ensureOrchestratorDaemonClient: converge grant", Cause: grantErr}
			}
		}
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
	app, err := appManager.RegisterApp(ctx, systemTenant, iam.RegisterAppOptions{
		Name:           OrchestratorDaemonClientAppName,
		GrantTypes:     []string{iam.GrantTypeClientCredentials},
		ResponseTypes:  []string{},
		ResourcePrefix: "orchestrator",
		Scopes:         scopes,
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
	// IsRoleNotFound, not errors.Is: errors.Is compares the QBError CODE, so
	// every not-found the role lookup could raise -- an absent tenant, a store
	// answering 404 -- was reported as the canonical admin role being missing,
	// which is the mistake the comment below this one forbids
	// (go-quicbaas#434).
	if iam.IsRoleNotFound(err) {
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

// PIVActivationRootsError indicates the iam.piv_login CA bundle configured for
// PIV invite activation could not be loaded or parsed.
type PIVActivationRootsError struct {
	Path   string
	Reason string
	Cause  error
}

// Error implements the error interface.
func (e *PIVActivationRootsError) Error() string {
	msg := "piv activation roots: " + e.Reason + " (" + e.Path + ")"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause.
func (e *PIVActivationRootsError) Unwrap() error {
	return e.Cause
}

// loadPIVActivationRoots reads the PEM CA bundle at path into a certificate
// pool for PIV invite-activation chain verification. The path is the
// operator's iam.piv_login.ca_file -- the same trust anchor the PIV login
// endpoint verifies against.
func loadPIVActivationRoots(path string) (*x509.CertPool, error) {
	pemData, err := os.ReadFile(path) //nolint:gosec // operator-configured CA path
	if err != nil {
		return nil, &PIVActivationRootsError{Path: path, Reason: "read failed", Cause: err}
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemData) {
		return nil, &PIVActivationRootsError{Path: path, Reason: "no certificates parsed"}
	}
	return pool, nil
}
