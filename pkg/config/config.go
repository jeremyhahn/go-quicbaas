// Package config provides configuration structures and management for QuicBaaS.
// It supports loading configuration from files, environment variables, and CLI flags
// with proper validation and defaults.
package config

import (
	"path/filepath"
	"strconv"
	"time"

	xkmsresolver "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/resolver"
)

// Server mode types determine how the server operates.
const (
	// ServerModeStandalone runs without Raft consensus - single node only.
	ServerModeStandalone = "standalone"

	// ServerModeCluster runs with Raft consensus for distributed HA.
	ServerModeCluster = "cluster"

	// ServerModeDevCluster runs multiple local nodes for development testing.
	ServerModeDevCluster = "devcluster"
)

// DefaultSealedBindHost is the loopback listen host used by the sealed-window
// (pre-unseal ceremony / unseal) listeners when Server.SealedBindHost is
// unset. Keeping the sealed surface on loopback by default prevents the
// pre-unseal API from being exposed network-wide by deployments that bind
// Server.Host to 0.0.0.0.
const DefaultSealedBindHost = "127.0.0.1"

// TenantIsolationMode constants define the supported tenant isolation strategies.
const (
	// TenantIsolationKeyspace uses key prefixes (__t:{tenantID}:) to scope all
	// data to a tenant. This is the default mode and is backward compatible.
	TenantIsolationKeyspace = "keyspace"

	// TenantIsolationBarrier uses go-qrdb's per-tenant cryptographic barrier to
	// provide full encryption isolation between tenants. Opt-in only.
	TenantIsolationBarrier = "barrier"

	// TenantIsolationVolume gives each tenant a dedicated storage volume per
	// database (Phase 2 dedicated-storage tier). Requires the volume_storage
	// entitlement.
	TenantIsolationVolume = "volume"

	// TenantIsolationRaft gives each tenant a dedicated Raft group per database
	// (Phase 4 dedicated-consensus tier). Requires the dedicated_raft entitlement.
	TenantIsolationRaft = "raft"
)

// Storage backend types determine the storage engine used.
// NOTE: The server.mode field determines whether Raft consensus is used:
//   - standalone: No Raft (single node)
//   - cluster/devcluster: Uses Raft for distributed consensus
//
// The storage.backend field only determines the storage engine (memory, file, pebble).
const (
	// StorageBackendMemory uses in-memory storage, no persistence.
	// Data is lost on restart. Best for testing and ephemeral workloads.
	StorageBackendMemory = "memory"

	// StorageBackendFile uses simple file-based storage.
	// Each key-value pair is stored as a separate file.
	// Best for development, testing, and low-volume deployments.
	StorageBackendFile = "file"

	// StorageBackendPebble uses PebbleDB for high-performance local storage.
	// Best for production single-node and cluster deployments.
	StorageBackendPebble = "pebble"
)

// MinBootstrapPasswordLength is the minimum password length required for
// bootstrap admin credentials when InsecureDevMode is false.
const MinBootstrapPasswordLength = 12

// QRDBTransport constants enumerate the supported qrdb SDK transport types.
const (
	// QRDBTransportEmbedded uses in-process calls via the embedded transport.
	// This is the default and has zero network overhead.
	QRDBTransportEmbedded = "embedded"

	// QRDBTransportGRPC uses gRPC to communicate with a remote qrdb server.
	QRDBTransportGRPC = "grpc"

	// QRDBTransportREST uses REST/HTTP to communicate with a remote qrdb server.
	QRDBTransportREST = "rest"

	// QRDBTransportQUIC uses QUIC to communicate with a remote qrdb server.
	QRDBTransportQUIC = "quic"

	// QRDBTransportMCP uses MCP (Model Context Protocol) to communicate with a remote qrdb server.
	QRDBTransportMCP = "mcp"

	// QRDBTransportUnix uses a Unix domain socket to communicate with a remote qrdb server.
	QRDBTransportUnix = "unix"
)

// IAMMTLSConfig holds operator-provided configuration for RFC 8705 mTLS client
// authentication pools that are loaded at daemon startup.
type IAMMTLSConfig struct {
	// TLSClientAuthCAFile is the path to a PEM bundle of trusted CA certificates
	// for tls_client_auth clients (RFC 8705 §2.1). When set, only certs that
	// chain to this pool are accepted for PKI-based client authentication.
	// If empty, tls_client_auth is unavailable; clients must use
	// self_signed_tls_client_auth instead.
	TLSClientAuthCAFile string `mapstructure:"tls_client_auth_ca_file"`

	// TPMEKRootsFile is the path to a PEM bundle of trusted TPM Endorsement Key
	// root CAs. When a client certificate chains to one of these roots the issued
	// access_token receives amr:["mtls","hwk"] and attest_method:"tpm".
	TPMEKRootsFile string `mapstructure:"tpm_ek_roots_file"`

	// XKeyRootsFile is the path to a PEM bundle of trusted xkey-issued CA roots.
	// When a client certificate chains to one of these roots the issued
	// access_token receives attest_method:"xkey".
	XKeyRootsFile string `mapstructure:"xkey_roots_file"`
}

// IAMPIVLoginConfig holds configuration for PIV smartcard TLS client-cert login.
type IAMPIVLoginConfig struct {
	// Enabled controls whether the POST /api/v1/auth/login/piv endpoint is active.
	// When true the TLS listener is configured with VerifyClientCertIfGiven so the
	// browser can present its PIV certificate during the TLS handshake.
	// Default: false.
	Enabled bool `mapstructure:"enabled"`

	// CAFile is the path to a PEM bundle of trusted CA roots for PIV cert chain
	// verification (e.g., government PIV-I root bundle, internal PKI). Required
	// when Enabled is true. The pool is appended to tlsConfig.ClientCAs so that
	// PIV-issuing CAs are trusted at the TLS layer in addition to any mTLS CA.
	CAFile string `mapstructure:"ca_file"`

	// TrustedIssuers is an optional list of Subject DN strings that the PIV cert
	// issuer must match. When empty, any cert that chains to the configured CA pool
	// is accepted; the deeper identity check occurs in the handler via thumbprint lookup.
	TrustedIssuers []string `mapstructure:"trusted_issuers"`
}

// IAMAAGUIDConfig holds operator configuration for FIDO2 authenticator AAGUID enforcement.
type IAMAAGUIDConfig struct {
	// Mode controls how unknown AAGUIDs are handled during WebAuthn registration.
	// "log_only" (default): unknown AAGUIDs are permitted; a warning is logged.
	// "enforce": unknown AAGUIDs cause registration to be rejected with aaguid_not_allowed.
	Mode string `mapstructure:"mode"`

	// AllowlistFile is the path to a JSON file containing allowed AAGUID entries.
	// Supports FIDO Alliance MDS3 format or a simple JSON array of
	// {"aaguid": "...", "name": "...", "attestation_level": "..."} objects.
	// When empty, the allowlist is loaded from built-in defaults (empty = permit all
	// in log_only mode, deny all in enforce mode).
	AllowlistFile string `mapstructure:"allowlist_file"`
}

// IAMSelfServiceRegistrationConfig holds configuration for self-service tenant
// registration. When enabled, unauthenticated users may create a new tenant
// and admin account via the /api/v1/auth/register/* endpoints.
type IAMSelfServiceRegistrationConfig struct {
	// Enabled controls whether the self-service registration endpoints are active.
	// Default: false (registration is disabled; invite-only).
	Enabled bool `mapstructure:"enabled"`

	// RequireAdminApproval places newly registered accounts in a "pending"
	// state until a platform admin approves them via
	// POST /api/v1/iam/users/{user_id}/approve.
	// Default: true (safe default — new tenants need admin sign-off).
	RequireAdminApproval bool `mapstructure:"require_admin_approval"`

	// RequireEmailVerification marks the account "unverified" after registration
	// and blocks login until email ownership is confirmed. No SMTP wiring is
	// performed in Phase 2; the flag gates the state transition only.
	// Default: false.
	RequireEmailVerification bool `mapstructure:"require_email_verification"`

	// DefaultRole is the built-in role granted to the registrant inside their
	// new tenant IN ADDITION to the always-granted Admin role on that tenant.
	// Must be one of: Admin, Operator, User, Auditor, Custodian, SO.
	// Default: "User".
	DefaultRole string `mapstructure:"default_role"`

	// AllowedIsolationModes lists the tenant isolation modes the operator
	// permits registrants to choose from. Accepted values: "keyspace", "barrier".
	// When empty, defaults to ["keyspace"].
	AllowedIsolationModes []string `mapstructure:"allowed_isolation_modes"`

	// DefaultIsolationMode is the isolation mode pre-selected for new tenants
	// when the registrant does not specify one. Must be a member of
	// AllowedIsolationModes.
	// Default: "keyspace".
	DefaultIsolationMode string `mapstructure:"default_isolation_mode"`

	// TenantBarrierVolumePool is the volume pool name used when a registrant
	// selects isolation_mode=barrier. Required (non-empty) when "barrier" is
	// present in AllowedIsolationModes.
	// Default: "default".
	TenantBarrierVolumePool string `mapstructure:"tenant_barrier_volume_pool"`
}

// SystemTenantID is the control-plane tenant. It mirrors qrdb's
// tenant.SystemTenantID; it is duplicated as a literal here so the config
// package (which every binary imports, including ones built without the qrdb
// runtime) stays dependency-free. A drift would be caught by
// TestDCRConfigSystemTenantMatchesQRDB in the rest package.
const SystemTenantID = "system"

// IAMDynamicClientRegistrationConfig gates the RFC 7591 Dynamic Client
// Registration endpoint (POST /oauth2/register).
//
// DCR mints an OAuth2 client — a first-class authentication principal — so it
// is DISABLED by default and, when enabled, requires an initial access token
// (a platform token; the caller's tenant governs where the client lands).
// Anonymous registration is an explicit, separately configured opt-in that must
// name a non-system landing tenant: an unauthenticated caller must never be
// able to create a client in the control plane.
type IAMDynamicClientRegistrationConfig struct {
	// Enabled controls whether POST /oauth2/register accepts registrations at
	// all. Default: false (the endpoint answers 403).
	Enabled bool `mapstructure:"enabled"`

	// AnonymousTenantID enables UNAUTHENTICATED dynamic client registration and
	// names the tenant such clients are created in. Empty (the default) means an
	// initial access token is REQUIRED and a token-less registration is refused
	// with 401. It must never name the system tenant.
	AnonymousTenantID string `mapstructure:"anonymous_tenant_id"`
}

// Validate returns an error if the IAMDynamicClientRegistrationConfig contains
// an invalid combination of field values. It is called during daemon startup.
func (c *IAMDynamicClientRegistrationConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.AnonymousTenantID == SystemTenantID {
		return &ConfigValidationError{
			Field:   "iam.dynamic_client_registration.anonymous_tenant_id",
			Message: "must not be the system tenant; anonymous registration may never create control-plane clients",
		}
	}
	return nil
}

// ValidRoleNames enumerates the six built-in FIPS 140-3 role names.
var ValidRoleNames = map[string]struct{}{
	"Admin":     {},
	"Operator":  {},
	"User":      {},
	"Auditor":   {},
	"Custodian": {},
	"SO":        {},
}

// Validate returns an error if the IAMSelfServiceRegistrationConfig contains
// an invalid combination of field values. It is called during daemon startup.
func (c *IAMSelfServiceRegistrationConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if _, ok := ValidRoleNames[c.DefaultRole]; !ok {
		return &ConfigValidationError{
			Field:   "iam.self_service_registration.default_role",
			Message: "must be one of Admin, Operator, User, Auditor, Custodian, SO; got " + c.DefaultRole,
		}
	}
	modes := c.AllowedIsolationModes
	if len(modes) == 0 {
		modes = []string{TenantIsolationKeyspace}
	}
	modeSet := make(map[string]struct{}, len(modes))
	for _, m := range modes {
		switch m {
		case TenantIsolationKeyspace, TenantIsolationBarrier, TenantIsolationVolume, TenantIsolationRaft:
			// valid
		default:
			return &ConfigValidationError{
				Field:   "iam.self_service_registration.allowed_isolation_modes",
				Message: "unsupported isolation mode: " + m,
			}
		}
		modeSet[m] = struct{}{}
	}
	defMode := c.DefaultIsolationMode
	if defMode == "" {
		defMode = TenantIsolationKeyspace
	}
	if _, ok := modeSet[defMode]; !ok {
		return &ConfigValidationError{
			Field:   "iam.self_service_registration.default_isolation_mode",
			Message: "default_isolation_mode must be one of allowed_isolation_modes",
		}
	}
	_, barrierAllowed := modeSet[TenantIsolationBarrier]
	_, volumeAllowed := modeSet[TenantIsolationVolume]
	if (barrierAllowed || volumeAllowed) && c.TenantBarrierVolumePool == "" {
		return &ConfigValidationError{
			Field:   "iam.self_service_registration.tenant_barrier_volume_pool",
			Message: "must be non-empty when barrier or volume isolation mode is allowed",
		}
	}
	return nil
}

// ConfigValidationError is a typed error for configuration validation failures.
type ConfigValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ConfigValidationError) Error() string {
	return "config: " + e.Field + ": " + e.Message
}

// IAMWebAuthnConfig holds the relying-party configuration for the WebAuthn /
// FIDO2 ceremony layer mounted from go-xkms. Production deployments must
// supply an explicit RPID, RPDisplayName, and at least one origin; the
// embedded webauthn library refuses to operate against the dev defaults
// when the browser origin does not match.
//
// The fields mirror github.com/go-webauthn/webauthn/webauthn.Config so the
// xkms factory can construct one directly without remapping.
type IAMWebAuthnConfig struct {
	// RPID is the Relying Party ID — a valid effective domain (e.g.
	// "example.com"). Browsers require an exact match against the page
	// origin's eTLD+1. Required.
	RPID string `mapstructure:"rp_id"`

	// RPDisplayName is the human-readable name shown by the authenticator
	// during ceremonies. Required.
	RPDisplayName string `mapstructure:"rp_display_name"`

	// RPOrigins is the list of permitted origins. At least one entry
	// required. Each entry must be a full origin including scheme
	// (e.g. "https://app.example.com"). Required.
	RPOrigins []string `mapstructure:"rp_origins"`

	// Timeout is the ceremony timeout (e.g. "60s"). When empty the
	// underlying library default applies.
	Timeout string `mapstructure:"timeout"`

	// UserVerification selects the user-verification policy:
	// "preferred" (default), "required", or "discouraged".
	UserVerification string `mapstructure:"user_verification"`

	// AttestationPreference controls attestation conveyance:
	// "none" (default), "indirect", or "direct".
	AttestationPreference string `mapstructure:"attestation_preference"`

	// ResidentKeyRequirement selects the resident-key policy:
	// "discouraged", "preferred", or "required".
	ResidentKeyRequirement string `mapstructure:"resident_key_requirement"`

	// AuthenticatorAttachment optionally constrains the authenticator
	// type: "platform", "cross-platform", or empty (no constraint).
	AuthenticatorAttachment string `mapstructure:"authenticator_attachment"`

	// Debug enables verbose webauthn logging in non-production builds.
	Debug bool `mapstructure:"debug"`
}

// Validate ensures the WebAuthn relying-party configuration is fit for
// production use. The required fields (RPID, RPDisplayName, RPOrigins)
// are checked unconditionally — there is no "disabled" mode for
// WebAuthn config because the WebAuthn surface is always wired when
// IAM is compiled in. Operators that want to disable the surface
// gate it at the IAMSelfServiceRegistrationConfig level instead.
func (c *IAMWebAuthnConfig) Validate() error {
	if c.RPID == "" {
		return &ConfigValidationError{
			Field:   "iam.webauthn.rp_id",
			Message: "rp_id is required (the Relying Party effective domain)",
		}
	}
	if c.RPDisplayName == "" {
		return &ConfigValidationError{
			Field:   "iam.webauthn.rp_display_name",
			Message: "rp_display_name is required",
		}
	}
	if len(c.RPOrigins) == 0 {
		return &ConfigValidationError{
			Field:   "iam.webauthn.rp_origins",
			Message: "rp_origins must contain at least one origin",
		}
	}
	for i, o := range c.RPOrigins {
		if o == "" {
			return &ConfigValidationError{
				Field:   "iam.webauthn.rp_origins",
				Message: "rp_origins entries must be non-empty (entry " + strconv.Itoa(i) + ")",
			}
		}
	}
	switch c.UserVerification {
	case "", "preferred", "required", "discouraged":
	default:
		return &ConfigValidationError{
			Field:   "iam.webauthn.user_verification",
			Message: "must be one of preferred, required, discouraged",
		}
	}
	switch c.AttestationPreference {
	case "", "none", "indirect", "direct":
	default:
		return &ConfigValidationError{
			Field:   "iam.webauthn.attestation_preference",
			Message: "must be one of none, indirect, direct",
		}
	}
	switch c.ResidentKeyRequirement {
	case "", "discouraged", "preferred", "required":
	default:
		return &ConfigValidationError{
			Field:   "iam.webauthn.resident_key_requirement",
			Message: "must be one of discouraged, preferred, required",
		}
	}
	switch c.AuthenticatorAttachment {
	case "", "platform", "cross-platform":
	default:
		return &ConfigValidationError{
			Field:   "iam.webauthn.authenticator_attachment",
			Message: "must be one of platform, cross-platform, or empty",
		}
	}
	return nil
}

// IAMConfig holds top-level IAM configuration including mTLS settings.
type IAMConfig struct {
	// MTLS holds operator-provided mTLS client authentication configuration.
	MTLS IAMMTLSConfig `mapstructure:"mtls"`

	// PIVLogin controls PIV smartcard TLS client-cert login.
	PIVLogin IAMPIVLoginConfig `mapstructure:"piv_login"`

	// AAGUID controls FIDO2 authenticator AAGUID allowlist enforcement.
	AAGUID IAMAAGUIDConfig `mapstructure:"aaguid"`

	// WebAuthn holds the relying-party configuration for the WebAuthn
	// ceremony layer (RPID, origins, timeout, ceremony policy).
	WebAuthn IAMWebAuthnConfig `mapstructure:"webauthn"`

	// SelfServiceRegistration controls the self-service tenant registration surface.
	SelfServiceRegistration IAMSelfServiceRegistrationConfig `mapstructure:"self_service_registration"`

	// DynamicClientRegistration controls the RFC 7591 Dynamic Client
	// Registration surface (POST /oauth2/register). Disabled by default.
	DynamicClientRegistration IAMDynamicClientRegistrationConfig `mapstructure:"dynamic_client_registration"`

	// SPIFFETrustDomain is the operator-configured SPIFFE trust domain. When
	// set, broker-issued client certificates carry a tenant-scoped SPIFFE URI
	// SAN (spiffe://<trust-domain>/tenant/<tenantID>/<kind>/<name>) and login
	// derives (and pins) the tenant from it. When empty, login relies solely
	// on home-realm discovery. This value is also propagated to the embedded
	// qrdb mTLS authenticator and the xkms CA at startup.
	SPIFFETrustDomain string `mapstructure:"spiffe_trust_domain"`
}

// Config represents the complete QuicBaaS configuration.
type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	DevCluster    DevClusterConfig    `mapstructure:"devcluster"`
	Raft          RaftConfig          `mapstructure:"raft"`
	Storage       StorageConfig       `mapstructure:"storage"`
	Security      SecurityConfig      `mapstructure:"security"`
	Auth          AuthConfig          `mapstructure:"auth"`
	IAM           IAMConfig           `mapstructure:"iam"`
	Backup        BackupConfig        `mapstructure:"backup"`
	ObjectStorage ObjectStorageConfig `mapstructure:"object_storage"`
	Logging       LoggingConfig       `mapstructure:"logging"`
	Discovery     DiscoveryConfig     `mapstructure:"discovery"`
	Cache         EnhancedCacheConfig `mapstructure:"cache"`
	Metrics       MetricsConfig       `mapstructure:"metrics"`
	Codec         CodecConfig         `mapstructure:"codec"`
	KMS           KMSConfig           `mapstructure:"kms"`
	Secrets       SecretsConfig       `mapstructure:"secrets"`
	Vector        VectorConfig        `mapstructure:"vector"`
	TimeSeries    TimeSeriesConfig    `mapstructure:"timeseries" json:"timeseries" yaml:"timeseries"`
	Ollama        OllamaConfig        `mapstructure:"ollama"`
	RAG           RAGConfig           `mapstructure:"rag"`
	Scheduler     SchedulerConfig     `mapstructure:"scheduler"`
	Messaging     MessagingConfig     `mapstructure:"messaging"`
	Orchestration OrchestrationConfig `mapstructure:"orchestration"`
	Seal          SealConfig          `mapstructure:"seal"`
	CA            CAConfig            `mapstructure:"ca"`
	QRDB          QRDBConfig          `mapstructure:"qrdb"`
	Databases     DatabasesConfig     `mapstructure:"databases"`
	XKMS          XKMSConfig          `mapstructure:"xkms"`
	Crypto        CryptoConfig        `mapstructure:"crypto"`
	// Trust configures quicbaasd's outbound trust resolver used when dialing
	// upstream TLS endpoints during bootstrap and inter-node communication.
	// When non-nil, the config is validated at startup via Trust.Validate().
	// If nil, no trust resolver is configured and outbound connections use the
	// default OS trust store.
	Trust *xkmsresolver.Config `mapstructure:"trust" json:"trust,omitempty" yaml:"trust,omitempty"`
}

// QRDBConfig configures the qrdb SDK client transport.
type QRDBConfig struct {
	// Transport specifies which transport the SDK client uses to communicate
	// with the qrdb service layer. Options:
	//   - "embedded" (default): In-process calls via embedded transport (zero overhead)
	//   - "grpc": gRPC transport connecting to a remote qrdb server
	//   - "rest": REST/HTTP transport
	//   - "quic": QUIC transport
	//   - "mcp": MCP (Model Context Protocol) transport
	//   - "unix": Unix domain socket transport
	Transport string `mapstructure:"transport"`

	// Address is the target address for network transports (gRPC, REST, QUIC, MCP).
	// Ignored when transport is "embedded".
	// Examples: "localhost:50051" (gRPC), "https://localhost:8443" (REST)
	Address string `mapstructure:"address"`

	// UnixSocketPath is the Unix socket path for the "unix" transport.
	UnixSocketPath string `mapstructure:"unix_socket_path"`

	// TLSEnabled enables TLS for network transports.
	TLSEnabled bool `mapstructure:"tls_enabled"`

	// TLSCertFile is the path to the TLS certificate file.
	TLSCertFile string `mapstructure:"tls_cert_file"`

	// TLSKeyFile is the path to the TLS key file.
	TLSKeyFile string `mapstructure:"tls_key_file"`

	// TLSCAFile is the path to the CA certificate file for verification.
	TLSCAFile string `mapstructure:"tls_ca_file"`
}

// IsEmbedded returns true if the transport is embedded or unset (default).
func (q *QRDBConfig) IsEmbedded() bool {
	return q.Transport == "" || q.Transport == QRDBTransportEmbedded
}

// AuthConfig contains authentication configuration.
type AuthConfig struct {
	// Enabled enables JWT-based authentication.
	// When enabled, most endpoints require a valid JWT token.
	// Default: false
	Enabled bool `mapstructure:"enabled"`

	// JWT signing keys are never configured here. Token signing is asymmetric
	// (ES256) with keys generated on first boot and stored in the xkms backend;
	// dedicated tenants may opt in to their own signing key via tenant OIDC
	// settings. See pkg/iam SigningKeyManager and TenantSigningKeyResolver.

	// TokenTTL is the duration that access tokens are valid.
	// Default: 1h
	TokenTTL time.Duration `mapstructure:"token_ttl"`

	// RefreshTokenTTL is the duration that refresh tokens are valid.
	// Shared by both the legacy password-auth manager and the OIDC provider.
	// Default: 7d (168h)
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`

	// AccessTokenTTL is the lifetime of OIDC/OAuth2 access tokens minted by the
	// broker. The platform access token is intentionally short-lived (5-15 min)
	// to bound replay exposure. Must be within [DefaultOIDCAccessTokenTTLMin,
	// DefaultOIDCAccessTokenTTLMax] when set; out-of-range values are rejected
	// by the validator.
	// Default: 15m (DefaultOIDCAccessTokenTTL) when zero.
	AccessTokenTTL time.Duration `mapstructure:"access_token_ttl"`

	// IDTokenTTL is the lifetime of OIDC ID tokens minted by the broker.
	// Default: 1h (DefaultOIDCIDTokenTTL) when zero.
	IDTokenTTL time.Duration `mapstructure:"id_token_ttl"`

	// AuthCodeTTL is the lifetime of OAuth2 authorization codes minted by the
	// broker (the short window between authorize and token exchange).
	// Default: 10m (DefaultOIDCAuthCodeTTL) when zero.
	AuthCodeTTL time.Duration `mapstructure:"auth_code_ttl"`

	// AllowPasswordAuth enables username/password authentication.
	// Default: false (use IAM-based authentication: WebAuthn, OIDC, mTLS)
	// WARNING: Password-based authentication is NOT recommended for production.
	// QuicBaaS is designed as a passwordless database - use WebAuthn, OIDC, or mTLS.
	// This option exists only for development/testing convenience.
	AllowPasswordAuth bool `mapstructure:"allow_password_auth"`

	// BootstrapAdmin enables automatic creation of an admin user on startup.
	// Only applies when AllowPasswordAuth is true.
	// Default: false
	BootstrapAdmin bool `mapstructure:"bootstrap_admin"`

	// BootstrapAdminUsername is the username for the bootstrap admin user.
	// Only used when BootstrapAdmin is true.
	// Default: "admin"
	BootstrapAdminUsername string `mapstructure:"bootstrap_admin_username"`

	// BootstrapAdminPassword is the password for the bootstrap admin user.
	// Only used when BootstrapAdmin is true.
	// In production (InsecureDevMode=false), the password must be at least
	// 12 characters and must not be set to the default value "admin".
	// Default: "admin"
	BootstrapAdminPassword string `mapstructure:"bootstrap_admin_password"`

	// BootstrapAdminPIVCertPath is the path to a PEM-encoded X.509 client
	// certificate that, when present at startup, is bound to the bootstrap
	// admin user as its PIV factor. The SHA-256 thumbprint of the cert's
	// DER bytes is computed and stored as a qrdb-canonical PIV-attested cert
	// binding on the user (user.CertBindings), and the user is transitioned to
	// active. After binding, a TLS client presenting
	// the matching key can authenticate via POST /api/v1/auth/login/piv.
	//
	// Primary use case: integration test clusters that need a deterministic
	// admin identity without going through the interactive activation flow.
	// In production the activation banner path (paths a/b/c in
	// firstBootBootstrap) should be used instead.
	BootstrapAdminPIVCertPath string `mapstructure:"bootstrap_admin_piv_cert_path"`

	// BootstrapAdminCSRPath is the path to a PEM-encoded PKCS#10 certificate
	// signing request supplied by the administrator. When set, first-boot
	// selects the CSR-sign enrollment path: the platform CA signs this CSR and
	// binds the resulting certificate's canonical SPKI thumbprint to the admin
	// user, so the admin's private key never reaches this host.
	//
	// When unset, first-boot falls back to generate-and-distribute: the CA
	// creates the keypair and writes both the certificate and the key under
	// <data-dir>/admin for the operator to collect and remove.
	BootstrapAdminCSRPath string `mapstructure:"bootstrap_admin_csr_path"`

	// AdminFactors is the ordered set of LOGIN factors enrolled for the
	// bootstrap administrator at first boot: any of "mtls", "fido2", "piv".
	// Empty means the mTLS baseline alone.
	//
	// This is a distinct concern from the barrier's seal strategy. A seal
	// strategy answers "how is the barrier key protected at rest"; a login
	// factor answers "how does a human prove who they are". Conflating them
	// produces an init that can unseal a database nobody can log into.
	//
	// go-qrdb owns the vocabulary and the ordering (mTLS is always enrolled
	// first, as the baseline that needs no hardware attached); go-xkms
	// implements the CTAP2 / PIV ceremonies. Both reach this daemon through
	// the go-xkms SDK, which is why hardware factors are available here and
	// not in a bare go-qrdb deployment.
	AdminFactors []string `mapstructure:"admin_factors"`

	// PasskeyDevice selects one FIDO2 authenticator when several are attached
	// to the host running first boot. Empty auto-selects the only one present.
	PasskeyDevice string `mapstructure:"passkey_device"`

	// InsecureDevMode allows insecure default credentials (e.g., admin/admin)
	// for development and testing. When false (default), the server rejects
	// weak bootstrap passwords and requires a minimum of 12 characters.
	// WARNING: NEVER enable this in production deployments.
	// Default: false
	InsecureDevMode bool `mapstructure:"insecure_dev_mode"`

	// MaxLoginAttempts is the maximum number of consecutive failed login
	// attempts before an account is temporarily locked.
	// Set to 0 to disable login attempt throttling entirely (useful for
	// integration tests where failed auth attempts should not cascade).
	// Set to -1 (or omit) to use the default of 5.
	// Default: -1 (use auth package default of 5)
	MaxLoginAttempts int `mapstructure:"max_login_attempts"`

	// LoginLockoutDuration is the duration for which an account remains
	// locked after exceeding the maximum failed login attempts.
	// Only applies when MaxLoginAttempts > 0.
	// Default: 15m
	LoginLockoutDuration time.Duration `mapstructure:"login_lockout_duration"`
}

// OIDC token TTL defaults and bounds. These preserve the historical
// hardcoded broker lifetimes (15m access / 7d refresh / 1h id / 10m auth-code)
// when the corresponding AuthConfig field is unset, and bound the access-token
// lifetime to the platform's short-lived 5-15 minute window.
const (
	// DefaultOIDCAccessTokenTTL is the default access-token lifetime.
	DefaultOIDCAccessTokenTTL = 15 * time.Minute
	// DefaultOIDCRefreshTokenTTL is the default refresh-token lifetime.
	DefaultOIDCRefreshTokenTTL = 7 * 24 * time.Hour
	// DefaultOIDCIDTokenTTL is the default ID-token lifetime.
	DefaultOIDCIDTokenTTL = 1 * time.Hour
	// DefaultOIDCAuthCodeTTL is the default authorization-code lifetime.
	DefaultOIDCAuthCodeTTL = 10 * time.Minute

	// DefaultOIDCAccessTokenTTLMin is the lower bound for the access-token
	// lifetime (replay window must stay short).
	DefaultOIDCAccessTokenTTLMin = 1 * time.Minute
	// DefaultOIDCAccessTokenTTLMax is the upper bound for the access-token
	// lifetime (the platform's 5-15 minute intent caps at 60m).
	DefaultOIDCAccessTokenTTLMax = 60 * time.Minute
)

// OIDCAccessTokenTTL returns the configured access-token lifetime, defaulting
// to DefaultOIDCAccessTokenTTL when unset (zero).
func (a *AuthConfig) OIDCAccessTokenTTL() time.Duration {
	if a.AccessTokenTTL <= 0 {
		return DefaultOIDCAccessTokenTTL
	}
	return a.AccessTokenTTL
}

// OIDCRefreshTokenTTL returns the configured refresh-token lifetime, defaulting
// to DefaultOIDCRefreshTokenTTL when unset (zero).
func (a *AuthConfig) OIDCRefreshTokenTTL() time.Duration {
	if a.RefreshTokenTTL <= 0 {
		return DefaultOIDCRefreshTokenTTL
	}
	return a.RefreshTokenTTL
}

// OIDCIDTokenTTL returns the configured ID-token lifetime, defaulting to
// DefaultOIDCIDTokenTTL when unset (zero).
func (a *AuthConfig) OIDCIDTokenTTL() time.Duration {
	if a.IDTokenTTL <= 0 {
		return DefaultOIDCIDTokenTTL
	}
	return a.IDTokenTTL
}

// OIDCAuthCodeTTL returns the configured authorization-code lifetime, defaulting
// to DefaultOIDCAuthCodeTTL when unset (zero).
func (a *AuthConfig) OIDCAuthCodeTTL() time.Duration {
	if a.AuthCodeTTL <= 0 {
		return DefaultOIDCAuthCodeTTL
	}
	return a.AuthCodeTTL
}

// SchedulerConfig contains job scheduler configuration.
type SchedulerConfig struct {
	// Enabled enables the job scheduler.
	// Default: false
	Enabled bool `mapstructure:"enabled"`

	// TickInterval is how often the scheduler checks for jobs.
	// Default: 1s
	TickInterval time.Duration `mapstructure:"tick_interval"`

	// MaxConcurrentJobs limits parallel job execution.
	// Default: 10
	MaxConcurrentJobs int `mapstructure:"max_concurrent_jobs"`
}

// MessagingConfig contains messaging and watch bridge configuration.
type MessagingConfig struct {
	// Enabled enables the messaging/push notification subsystem.
	// Default: false
	Enabled bool `mapstructure:"enabled"`

	// BufferSize is the notification hub buffer size.
	// Default: 4096
	BufferSize int `mapstructure:"buffer_size"`

	// DedupEnabled enables message deduplication.
	// Default: true
	DedupEnabled bool `mapstructure:"dedup_enabled"`

	// DedupWindow is the deduplication time window.
	// Default: 1m
	DedupWindow time.Duration `mapstructure:"dedup_window"`

	// WatchBridge contains watch-to-messaging bridge configuration.
	WatchBridge WatchBridgeConfig `mapstructure:"watch_bridge"`
}

// LeadAgentConfig configures the optional LLM lead agent for intelligent orchestration.
// This mirrors orchestration.LeadAgentConfig to avoid import cycles.
type LeadAgentConfig struct {
	// Backend is the backend name to use for the lead agent (e.g. "claude", "ollama").
	Backend string `mapstructure:"backend"`

	// Model is the model identifier to use for the lead agent.
	Model string `mapstructure:"model"`

	// SystemPrompt is the system prompt for orchestration behavior.
	SystemPrompt string `mapstructure:"system_prompt"`
}

// EmbeddedAgentConfig configures the optional in-process agent that
// quicbaasd can boot alongside the orchestrator. When Enabled is true,
// quicbaasd starts a full agent runtime (identical to `quicbaas agent serve`)
// and registers it through the same fleet registry that remote agents use.
// The runtime communicates with the orchestrator via a bufconn loopback — no
// network round-trip — while retaining a proper mTLS identity issued by the
// local CA.
type EmbeddedAgentConfig struct {
	// Enabled starts an in-process agent runtime at quicbaasd boot.
	// Default: false. When false, quicbaasd is a pure orchestrator.
	Enabled bool `mapstructure:"enabled"`

	// Backends lists the backend kinds the embedded agent exposes.
	// Default: ["ollama"]
	Backends []string `mapstructure:"backends"`

	// OllamaURL is the Ollama API endpoint the embedded Ollama backend dials.
	// Default: "http://127.0.0.1:11434"
	OllamaURL string `mapstructure:"ollama_url"`

	// HostAffinity is the self-tag this node advertises so callers can pin
	// work to this specific machine with host_affinity hints.
	// Default: os.Hostname()
	HostAffinity string `mapstructure:"host_affinity"`

	// GPUAvailable advertises GPU capability in the embedded agent's
	// registration so GPU-aware routing includes this node.
	// Default: false
	GPUAvailable bool `mapstructure:"gpu_available"`

	// DataDir is the working directory for the embedded agent's session
	// scratch space. Defaults to <storage.data_dir>/embedded-agent.
	DataDir string `mapstructure:"data_dir"`

	// SandboxPolicy governs how the embedded agent resolves SandboxModeDefault
	// and which explicit modes it accepts: "opt-in" (default, raw exec unless
	// the client explicitly requests Required), "preferred", or "required".
	// Sandboxed (container) sessions also require a container runtime
	// (docker/podman) on PATH and a SandboxImage / ImageMap entry for the
	// backend.
	SandboxPolicy string `mapstructure:"sandbox_policy"`

	// SandboxNetwork is the container network sandboxed sessions run in:
	// "bridge" (default — the container gets its own network namespace with
	// egress), "none" (no egress at all), "host", or the name of a
	// user-defined runtime network. "host" shares this machine's network
	// namespace with the agent container, which lets it reach every
	// loopback-bound service on the host; it is honored, but only because it
	// was configured here deliberately.
	SandboxNetwork string `mapstructure:"sandbox_network"`

	// SandboxImage is a convenience setting that supplies the OCI image used
	// for sandboxed sessions across every configured backend when ImageMap
	// does not already name one. The image must have the backend agent binary
	// (e.g. opencode) installed and reachable on PATH.
	SandboxImage string `mapstructure:"sandbox_image"`

	// ImageMap maps a backend name ("opencode", "claude", "codex") to the
	// fully qualified OCI image the embedded agent spawns for sandboxed
	// sessions with that backend. Takes precedence over SandboxImage.
	ImageMap map[string]string `mapstructure:"image_map"`

	// ImageRegistry is an optional registry prefix applied to ImageMap /
	// SandboxImage values that lack one (e.g. "ghcr.io/jeremyhahn").
	ImageRegistry string `mapstructure:"image_registry"`

	// AutoPull instructs the embedded agent to `docker pull` each sandbox
	// image at startup. Leave false for images that exist only in the local
	// daemon image store (e.g. CI-built e2e images that were never pushed).
	AutoPull bool `mapstructure:"auto_pull"`
}

// OrchestrationConfig configures the agent orchestration service.
type OrchestrationConfig struct {
	// Enabled enables the agent orchestration service.
	// Default: true
	Enabled bool `mapstructure:"enabled"`

	// LeadAgent configures the optional LLM lead agent for intelligent orchestration.
	LeadAgent LeadAgentConfig `mapstructure:"lead_agent"`

	// HeartbeatTimeout is the duration after which an agent is considered offline
	// if no heartbeat is received.
	// Default: 90s
	HeartbeatTimeout time.Duration `mapstructure:"heartbeat_timeout"`

	// ClaudeTeamMode enables Claude Code team mode for multi-agent collaboration.
	// When enabled, the --team flag is appended to Claude Code backend commands.
	// Default: false
	ClaudeTeamMode bool `mapstructure:"claude_team_mode"`

	// OllamaChatModel is the default chat model for Ollama orchestration operations.
	// Default: "llama3"
	OllamaChatModel string `mapstructure:"ollama_chat_model"`

	// ClaudeNativeSessionResume selects the Claude CLI's own on-disk session
	// store as the mechanism for resuming a session after a daemon restart.
	//
	// Default (false): the CLI runs with --no-session-persistence and keeps no
	// transcript on the host. A session interrupted by a restart is
	// reconstituted by replaying the durable, barrier-encrypted transcript from
	// the session event log into a fresh process.
	//
	// True: the CLI keeps its own transcript under the daemon's home directory
	// and a resume is performed with "--resume <session-id>", restoring tool
	// calls and results as well as messages, with nothing re-sent by QuicBaaS.
	// The trade is a plaintext copy of every conversation on the host
	// filesystem, outside the storage barrier — enable it only where that
	// filesystem is already inside the trust boundary.
	ClaudeNativeSessionResume bool `mapstructure:"claude_native_session_resume"`

	// TranscriptReplayBudget bounds, in characters, the transcript replayed
	// into a resumed agent when ClaudeNativeSessionResume is false. Oldest
	// turns are dropped first and the agent is told how many were omitted.
	// Zero selects orchestration.DefaultReplayBudget (48 KiB).
	TranscriptReplayBudget int `mapstructure:"transcript_replay_budget"`

	// HostAffinity configures how strictly host_affinity hints on
	// SpawnSession / Inference requests are enforced when the named host is
	// offline. See SchedulerHostAffinityConfig.
	HostAffinity SchedulerHostAffinityConfig `mapstructure:"host_affinity"`

	// ProjectsRoot is the root directory for server-managed project
	// workspaces. Each managed workspace is provisioned at
	// <projects_root>/<tenant_id>/<project_id>/workspace with 0700
	// permissions. When set it must be an absolute path. Empty derives
	// "<storage.data_dir>/projects" — see EffectiveProjectsRoot.
	ProjectsRoot string `mapstructure:"projects_root"`

	// Sandbox configures the sandbox mount policy applied to the host bind
	// mounts requested on orchestrated agent sessions.
	Sandbox OrchestrationSandboxConfig `mapstructure:"sandbox"`

	// TrustPolicy is the integrity floor every spawned agent session must
	// meet. It is the trust axis, orthogonal to the sandbox axis: the sandbox
	// policy answers "is the agent isolated from the host", this one answers
	// "is the agent the code we approved, and can the host prove it".
	//
	// Accepted values, in ascending strictness:
	//
	//	"opt-in"   (default) no integrity requirement. A session gets a tier
	//	           only when it asks for one.
	//	"pinned"   Tier 0: every agent container must be launched from an
	//	           immutable image content digest, verified after launch.
	//	"attested" Tier 1: Tier 0 plus a host TPM quote verified against the
	//	           session's expected PCR set. Requires a TPM.
	//	"measured" Tier 2: Tier 1 plus measured launch — the image and launch
	//	           configuration are extended into a PCR before the container
	//	           starts and included in the quote.
	//
	// The floor is what a request naming no level resolves to, and a request
	// naming a WEAKER level is refused rather than honored. A tier the target
	// host cannot prove is also refused, never silently downgraded: raising
	// this beyond what the deployment's container runtime supports fails
	// spawns closed. See orchestration.TrustPolicy / ResolveTrustLevel.
	TrustPolicy string `mapstructure:"trust_policy"`

	// AccountProviders defines, or overrides, the OAuth defaults applied to
	// pooled agent accounts per provider (token endpoint, client id, scopes,
	// access-token environment variable). Built-in providers are merged
	// first and an entry here fills or replaces individual fields; a pooled
	// account's own credential always wins over both. See
	// AccountProviderConfig and docs/orchestration/accounts.md.
	AccountProviders []AccountProviderConfig `mapstructure:"account_providers"`
}

// OrchestrationSandboxConfig configures the sandbox mount policy for
// orchestrated agent sessions.
type OrchestrationSandboxConfig struct {
	// AllowedMountRoots is an operator-configured allowlist of absolute
	// host directories under which a sandbox bind mount may be sourced, in
	// addition to the requesting tenant's own subtree of the managed
	// projects root. Every mount source outside both — the session's work
	// directory included — is denied.
	//
	// Unlike the projects root these roots are NOT tenant-scoped: the
	// operator named them explicitly and every tenant may mount from them.
	// Do not list a directory holding one tenant's data.
	AllowedMountRoots []string `mapstructure:"allowed_mount_roots"`
}

// OrchestrationTrustPolicies lists the accepted orchestration.trust_policy
// values. It mirrors orchestration.ParseTrustPolicy, which is the authority;
// pkg/config cannot import pkg/orchestration (that package's tests import this
// one), so the vocabulary is restated here for early config validation and the
// two are kept in step by TestValidateOrchestration_TrustPolicy.
var OrchestrationTrustPolicies = []string{"opt-in", "pinned", "attested", "measured"}

// EffectiveProjectsRoot returns ProjectsRoot when set, defaulting to
// "<dataDir>/projects" when empty. dataDir is the top-level storage data
// directory (Storage.DataDir).
func (c OrchestrationConfig) EffectiveProjectsRoot(dataDir string) string {
	if c.ProjectsRoot != "" {
		return c.ProjectsRoot
	}
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "projects")
}

// SchedulerHostAffinityConfig controls operator policy for the host_affinity
// hint applied to orchestrator and scheduler requests.
type SchedulerHostAffinityConfig struct {
	// Mode selects the enforcement mode when a request supplies host_affinity
	// but the named host is offline:
	//   "soft" (default): warn and fall back to any available agent.
	//   "hard": return a NoMatchingAgentError so callers know the request was
	//   not satisfied.
	// Empty defaults to "soft".
	Mode string `mapstructure:"mode"`
}

// HostAffinityModeSoft is the default soft enforcement mode (warn + fallback).
const HostAffinityModeSoft = "soft"

// HostAffinityModeHard is the strict enforcement mode (typed-error + no fallback).
const HostAffinityModeHard = "hard"

// EffectiveMode returns Mode normalised to lowercase, defaulting to
// HostAffinityModeSoft when empty.
func (c SchedulerHostAffinityConfig) EffectiveMode() string {
	switch c.Mode {
	case "":
		return HostAffinityModeSoft
	default:
		return c.Mode
	}
}

// IsHard reports whether the operator-level default is "hard".
func (c SchedulerHostAffinityConfig) IsHard() bool {
	return c.EffectiveMode() == HostAffinityModeHard
}

// WatchBridgeConfig contains watch bridge specific configuration.
type WatchBridgeConfig struct {
	// Enabled enables the watch bridge.
	// Default: false
	Enabled bool `mapstructure:"enabled"`

	// LeaderOnly publishes events only when this node is leader.
	// Default: true
	LeaderOnly bool `mapstructure:"leader_only"`

	// BufferSize is the internal event buffer size.
	// Default: 1024
	BufferSize int `mapstructure:"buffer_size"`

	// DefaultChannelPrefix is the default prefix for generated channel names.
	// Default: "watch:"
	DefaultChannelPrefix string `mapstructure:"default_channel_prefix"`

	// AutoCreateChannels automatically creates channels for watched prefixes.
	// Default: true
	AutoCreateChannels bool `mapstructure:"auto_create_channels"`

	// Mappings defines key prefix to channel mappings.
	Mappings []WatchBridgeMappingConfig `mapstructure:"mappings"`
}

// WatchBridgeMappingConfig defines a single key prefix to channel mapping.
type WatchBridgeMappingConfig struct {
	// KeyPrefix is the key prefix to watch (e.g., "/users/").
	KeyPrefix string `mapstructure:"key_prefix"`

	// ChannelPrefix is the channel to publish to. If empty, uses "watch:" + KeyPrefix.
	ChannelPrefix string `mapstructure:"channel_prefix"`

	// FilterPut disables bridging of PUT events.
	FilterPut bool `mapstructure:"filter_put"`

	// FilterDelete disables bridging of DELETE events.
	FilterDelete bool `mapstructure:"filter_delete"`
}

// ObjectStorageConfig defines named storage backends that can be referenced by components.
type ObjectStorageConfig struct {
	// Enabled enables the object storage service.
	// When enabled via environment variable without backends configured,
	// a default in-memory backend named "default" is created automatically.
	// Default: false
	Enabled bool `mapstructure:"enabled"`

	// Backends is a map of backend name to backend configuration.
	// Example keys: "primary", "keyring", "backup", "archive"
	Backends map[string]*StorageBackendConfig `mapstructure:"backends"`
}

// StorageBackendConfig defines configuration for a single storage backend.
type StorageBackendConfig struct {
	// Type specifies the backend type.
	// Supported: "local", "s3", "minio", "gcs", "azure", "memory"
	Type string `mapstructure:"type"`

	// Config contains type-specific configuration as key-value pairs.
	// This maps directly to go-objstore factory configuration.
	Config map[string]string `mapstructure:"config"`

	// Description is an optional human-readable description of this backend.
	Description string `mapstructure:"description"`
}

// BackupConfig contains backup and restore configuration.
type BackupConfig struct {
	// Enabled determines if backup functionality is available
	Enabled bool `mapstructure:"enabled"`

	// StorageBackend references a named backend from ObjectStorage.Backends
	// Required if Enabled is true
	StorageBackend string `mapstructure:"storage_backend"`

	// DefaultCompression specifies default compression algorithm
	// Supported: "none", "gzip", "zstd"
	DefaultCompression string `mapstructure:"default_compression"`

	// DefaultEncryption specifies default encryption algorithm
	// Supported: "none", "aes-256-gcm"
	DefaultEncryption string `mapstructure:"default_encryption"`

	// EncryptionKey is the backup encryption key (32 bytes for AES-256)
	// Can be base64 encoded string or path to key file
	EncryptionKey string `mapstructure:"encryption_key"`

	// WorkDir is the temporary directory for backup operations
	WorkDir string `mapstructure:"work_dir"`

	// Timeout for backup operations
	Timeout time.Duration `mapstructure:"timeout"`
}

// ServerGRPCConfig contains gRPC server configuration.
type ServerGRPCConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Port    int  `mapstructure:"port"`

	// BindAddress optionally overrides Server.Host for the gRPC listener,
	// allowing this protocol to bind to a private-ingress interface (SC-7)
	// independently of the other listeners. Empty = use Server.Host
	// (current behavior).
	BindAddress string `mapstructure:"bind_address"`
}

// ServerRESTConfig contains REST/HTTP server configuration.
type ServerRESTConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Port    int  `mapstructure:"port"`

	// BindAddress optionally overrides Server.Host for the REST listener,
	// allowing this protocol to bind to a private-ingress interface (SC-7)
	// independently of the other listeners. Empty = use Server.Host
	// (current behavior).
	BindAddress string `mapstructure:"bind_address"`
}

// ServerQUICConfig contains QUIC server configuration.
type ServerQUICConfig struct {
	Enabled                bool          `mapstructure:"enabled"`
	Port                   int           `mapstructure:"port"`
	MaxStreams             int           `mapstructure:"max_streams"`
	MaxIdleTimeout         time.Duration `mapstructure:"max_idle_timeout"`
	HandshakeIdleTimeout   time.Duration `mapstructure:"handshake_idle_timeout"`
	MaxStreamReceiveWindow uint64        `mapstructure:"max_stream_receive_window"`

	// BindAddress optionally overrides Server.Host for the QUIC listener,
	// allowing this protocol to bind to a private-ingress interface (SC-7)
	// independently of the other listeners. Empty = use Server.Host
	// (current behavior).
	BindAddress string `mapstructure:"bind_address"`
}

// ServerMCPConfig contains MCP (Model Context Protocol) server configuration.
type ServerMCPConfig struct {
	Enabled             bool                         `mapstructure:"enabled"`
	Port                int                          `mapstructure:"port"`
	Authorization       MCPAuthorizationConfig       `mapstructure:"authorization"`
	DynamicRegistration MCPDynamicRegistrationConfig `mapstructure:"dynamic_registration"`

	// BindAddress optionally overrides Server.Host for the MCP listener,
	// allowing this protocol to bind to a private-ingress interface (SC-7)
	// independently of the other listeners. Empty = use Server.Host
	// (current behavior).
	BindAddress string `mapstructure:"bind_address"`
}

// ServerUnixConfig contains Unix domain socket server configuration.
type ServerUnixConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Socket  string `mapstructure:"socket"`

	// BindAddress optionally overrides Server.Host for any network-facing
	// component of the Unix transport. The Unix domain socket itself is
	// addressed by Socket; this field exists for parity and is honored only
	// when the transport binds a network address. Empty = use Server.Host
	// (current behavior).
	BindAddress string `mapstructure:"bind_address"`
}

// ServerConfig contains HTTP/gRPC/QUIC server configuration.
type ServerConfig struct {
	// Mode specifies the server operation mode.
	// Options: "standalone" (no Raft), "cluster" (distributed Raft), "devcluster" (local multi-node)
	// Default: "standalone"
	// REQUIRED for cluster operation - the mode field determines whether Raft consensus is used.
	Mode string `mapstructure:"mode"`

	Host              string `mapstructure:"host"`
	ExposeShardHeader bool   `mapstructure:"expose_shard_header"`

	// SealedBindHost is the listen host used by the sealed-window (pre-unseal
	// ceremony / unseal) listeners on REST, gRPC, QUIC, and MCP. It is
	// deliberately independent of Host and the per-protocol BindAddress
	// overrides so a deployment that binds the full post-unseal API on
	// 0.0.0.0 does not also expose the pre-unseal surface network-wide.
	// Empty = "127.0.0.1" (loopback). Set to a routable address (for
	// example "0.0.0.0") only when remote Shamir officers must reach the
	// ceremony/unseal surface during the sealed window.
	SealedBindHost string `mapstructure:"sealed_bind_host"`

	// AdditionalSANDNS lists extra DNS names to include in the server's
	// auto-generated TLS certificate SANs. Required when operators reach
	// this server via a hostname that differs from Host (for example, a
	// load-balancer FQDN, an internal DNS alias, or a remote GPU host
	// that dials the central by hostname).
	AdditionalSANDNS []string `mapstructure:"additional_san_dns"`

	// AdditionalSANIPs lists extra IP addresses to include in the server's
	// auto-generated TLS certificate SANs. Required when operators reach
	// this server by a routable IP other than 127.0.0.1/::1 — which is
	// always the case for remote GPU agents connecting across the LAN.
	AdditionalSANIPs []string `mapstructure:"additional_san_ips"`

	// Embedded enables embedded mode where QuicBaaS runs in-process without
	// any network listeners (Unix socket, TCP, REST, QUIC). Use this when
	// embedding QuicBaaS as a library and accessing the service directly via
	// Server.Service(). Raft communication still uses TCP for inter-node consensus.
	// Default: false.
	Embedded bool `mapstructure:"embedded"`

	// EmbeddedAgent configures the optional co-located agent runtime.
	// See EmbeddedAgentConfig for field semantics.
	EmbeddedAgent EmbeddedAgentConfig `mapstructure:"embedded_agent"`

	// Protocol-specific configurations
	GRPC ServerGRPCConfig `mapstructure:"grpc"`
	REST ServerRESTConfig `mapstructure:"rest"`
	QUIC ServerQUICConfig `mapstructure:"quic"`
	MCP  ServerMCPConfig  `mapstructure:"mcp"`
	Unix ServerUnixConfig `mapstructure:"unix"`

	// Timeouts and limits
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	IdleTimeout     time.Duration `mapstructure:"idle_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	MaxConnections  int           `mapstructure:"max_connections"`

	// Sharding & routing
	// Shards lists available shard IDs for routing fallback/modulo.
	Shards []uint64 `mapstructure:"shards"`
	// EntityShardPins maps entity type to a fixed shard ID.
	EntityShardPins map[string]uint64 `mapstructure:"entity_shard_pins"`
}

// ResolveBindHost returns the listen host for a protocol listener: the
// per-protocol override when non-empty, otherwise the shared Server.Host.
// This keeps the default (empty override) behavior identical to binding all
// listeners on Server.Host while allowing per-protocol private-ingress
// isolation (SC-7) when an operator sets a bind_address.
func (c *ServerConfig) ResolveBindHost(override string) string {
	if override != "" {
		return override
	}
	return c.Host
}

// ResolveSealedBindHost returns the listen host for the sealed-window
// (pre-unseal) listeners: SealedBindHost when non-empty, otherwise the
// loopback default DefaultSealedBindHost. Server.Host and the per-protocol
// BindAddress overrides never apply while sealed — the ceremony/unseal
// surface stays on loopback unless an operator explicitly opts in.
func (c *ServerConfig) ResolveSealedBindHost() string {
	if c.SealedBindHost != "" {
		return c.SealedBindHost
	}
	return DefaultSealedBindHost
}

// MCPAuthorizationConfig contains MCP authorization settings.
type MCPAuthorizationConfig struct {
	// Enabled enables OAuth 2.1 authorization for MCP endpoints.
	// Default: false (auth disabled, all tools accessible).
	Enabled bool `mapstructure:"enabled"`

	// ResourceURI is this MCP server's resource identifier.
	// Used for token audience validation per RFC 8707.
	// Example: "https://quicbaas.example.com/mcp"
	ResourceURI string `mapstructure:"resource_uri"`

	// AuthorizationServers is a list of OAuth 2.0/OIDC authorization server URIs.
	// These are advertised in the protected resource metadata (RFC 9728).
	// If empty, uses the built-in IAM OIDC provider.
	AuthorizationServers []string `mapstructure:"authorization_servers"`

	// ResourceDocumentation is a URL to the MCP API documentation.
	// Included in the protected resource metadata response.
	ResourceDocumentation string `mapstructure:"resource_documentation"`

	// RequireHTTPSRedirects requires HTTPS for all OAuth redirect URIs.
	// Set to false to allow localhost HTTP for development.
	// Default: true in production.
	RequireHTTPSRedirects bool `mapstructure:"require_https_redirects"`
}

// MCPDynamicRegistrationConfig contains DCR settings.
type MCPDynamicRegistrationConfig struct {
	// Enabled enables RFC 7591 Dynamic Client Registration.
	// Default: false.
	Enabled bool `mapstructure:"enabled"`

	// RequireInitialAccessToken requires an initial access token for DCR.
	// When enabled, clients must present a valid token to register.
	// Default: false (open registration).
	RequireInitialAccessToken bool `mapstructure:"require_initial_access_token"`
}

// RaftConfig contains QuicRaft cluster configuration.
type RaftConfig struct {
	NodeID uint64 `mapstructure:"node_id"`
	// DeploymentID uniquely identifies the quicbaas cluster. All nodes in a
	// cluster MUST share the same DeploymentID so quicraft can isolate them
	// from other clusters on the same network. If unset (0), buildQRDBConfig
	// derives it from NodeID (or falls back to 1) for single-node dev/test;
	// in production, set an explicit non-zero value that is the same across
	// all nodes. Supported format: any uint64 > 0 (e.g. 1, 42, or a
	// hash of the cluster name).
	DeploymentID        uint64        `mapstructure:"deployment_id"`
	ElectionRTT         uint64        `mapstructure:"election_rtt"`
	HeartbeatRTT        uint64        `mapstructure:"heartbeat_rtt"`
	SnapshotEntries     uint64        `mapstructure:"snapshot_entries"`
	CompactionOverhead  uint64        `mapstructure:"compaction_overhead"`
	RequestTimeout      time.Duration `mapstructure:"request_timeout"`
	RTTMillisecond      uint64        `mapstructure:"rtt_millisecond"`
	RaftAddress         string        `mapstructure:"raft_address"`
	JoinAddresses       []string      `mapstructure:"join_addresses"`
	ListenAddress       string        `mapstructure:"listen_address"`
	MutualTLS           bool          `mapstructure:"mutual_tls"`
	MaxInMemLogSize     uint64        `mapstructure:"max_in_mem_log_size"`
	MaxSendQueueSize    uint64        `mapstructure:"max_send_queue_size"`
	MaxReceiveQueueSize uint64        `mapstructure:"max_receive_queue_size"`
	MaxSnapshotSendBPS  uint64        `mapstructure:"max_snapshot_send_bps"`
	MaxSnapshotRecvBPS  uint64        `mapstructure:"max_snapshot_recv_bps"`
	NotifyCommit        bool          `mapstructure:"notify_commit"`
	EnableMetrics       bool          `mapstructure:"enable_metrics"`

	// Write mode configuration
	WriteMode          string        `mapstructure:"write_mode"`           // sync, wal, or batched
	BatchMaxSize       int           `mapstructure:"batch_max_size"`       // Max operations per batch
	BatchFlushInterval time.Duration `mapstructure:"batch_flush_interval"` // Max batching delay

	// Bootstrap configuration
	EnableBootstrap   bool          `mapstructure:"enable_bootstrap"`    // Whether to enable automatic bootstrap
	BootstrapTimeout  time.Duration `mapstructure:"bootstrap_timeout"`   // Max time to wait for bootstrap
	LeaderWaitTimeout time.Duration `mapstructure:"leader_wait_timeout"` // How long to wait for leader election
	JoinTimeout       time.Duration `mapstructure:"join_timeout"`        // How long to wait when joining existing cluster
	MinNodes          int           `mapstructure:"min_nodes"`           // Minimum nodes required for quorum

	// Join indicates that this node is joining an existing Raft cluster rather
	// than bootstrapping a new one. When true, StartShard is called with
	// join=true so quicraft does not require the node to be present in the
	// initial members map. Set QUICBAAS_RAFT_JOIN=true for dynamic membership
	// (adding a node to a running cluster after initial bootstrap).
	// Default: false (bootstrap mode — all members in JoinAddresses form the
	// initial cluster together).
	Join bool `mapstructure:"join"`

	// Raft-specific TLS certificate paths for inter-node mTLS.
	// When set, these override the server Security TLS paths for Raft transport.
	// This allows Raft mTLS to use a shared CA and per-node certs that are
	// independent of the server API TLS certificates.
	TLSCertFile string `mapstructure:"tls_cert_file"`
	TLSKeyFile  string `mapstructure:"tls_key_file"`
	TLSCAFile   string `mapstructure:"tls_ca_file"`

	// RaftEnrollment configures automatic provisioning of the inter-node Raft
	// mTLS material when the TLS file paths above are not supplied. A
	// multi-node Raft transport always requires mTLS, so without either the
	// TLS files or RaftEnrollment a multi-node cluster fails to start.
	// Enrollment is delegated to go-qrdb's shared cluster-TLS toolkit: the
	// first node generates a self-signed cluster CA and advertises it over a
	// Noise NK channel; joining nodes fetch it.
	RaftEnrollment RaftEnrollmentConfig `mapstructure:"raft_enrollment"`
}

// RaftEnrollmentConfig configures automatic provisioning of the inter-node
// Raft mTLS material when operator-supplied TLS file paths are not set.
// All fields are optional and receive working defaults at boot.
type RaftEnrollmentConfig struct {
	// TrustDomain is the SPIFFE trust domain stamped into node certificates.
	// Defaults to "quicbaas.cluster".
	TrustDomain string `mapstructure:"trust_domain"`

	// KeyBackend selects the crypto backend that generates and holds the Raft
	// TLS signing key. When set to a hardware backend ("tpm2" / "pkcs11"),
	// the private key never leaves the device and the Raft transport uses a
	// signer-backed certificate. Empty means software key management (default).
	KeyBackend string `mapstructure:"key_backend"`

	// AdvertiseNoisePort is the TCP port the first (bootstrapping) node serves
	// the Noise NK bootstrap endpoint on for peer enrollment. Defaults to the
	// qrdb clusterenroll default port (63006).
	AdvertiseNoisePort int `mapstructure:"advertise_noise_port"`

	// Bootstrap configures how a JOINING node fetches the cluster CA bundle
	// from an existing node. Leave empty on the first (bootstrapping) node.
	Bootstrap RaftBootstrapConfig `mapstructure:"bootstrap"`

	// ACME switches node-certificate issuance to TPM-attested ACME enroll-01
	// with the runtime TrustedCA as the single cluster trust anchor: the first
	// node pre-stages the CA hierarchy and issues its own leaf; joining nodes
	// prove their identity via TPM attestation to an existing node's ACME
	// directory instead of receiving a shared CA key.
	ACME RaftACMEEnrollmentConfig `mapstructure:"acme"`

	// ClusterCACN names the dedicated cluster intermediate that issues Raft
	// transport certificates in ACME-attested mode. It must match a
	// Subject.CommonName in the CA hierarchy. When empty a default derived from
	// the CA root name is used. Keeping transport issuance on a dedicated
	// intermediate (root signs intermediates only) is the recommended
	// FedRAMP/PCI posture.
	ClusterCACN string `mapstructure:"cluster_ca_cn"`

	// CAKeyProtection selects how the node-local cluster CA signing keystore is
	// protected at rest, using the canonical seal vocabulary:
	//   "" (auto) — TPM/HSM-sealed when available, else passphrase, else refuse
	//   "tpm2" / "pkcs11" — random key sealed to the TPM/HSM (unattended)
	//   "passphrase" — PBES2 with an operator passphrase
	//   "none" — plaintext PKCS#8 (development/testing only)
	// The cluster CA keys must exist before the replicated datastore, so they
	// live node-local and are protected by the standard seal strategies.
	CAKeyProtection string `mapstructure:"ca_key_protection"`

	// CAKeyPassphrase supplies the operator passphrase for "passphrase"
	// protection (and the passphrase fallback in auto mode). Supports
	// credential:// references resolved at boot.
	CAKeyPassphrase string `mapstructure:"ca_key_passphrase"`

	// CAKeyStrategy selects how the cluster CA signing key is made available
	// across the cluster so the appropriate node(s) can issue node certificates:
	//   "" / "barrier" — key in the replicated barrier-encrypted datastore; any
	//                    node loads it and can issue (default, software HA).
	//   "hsm"          — key kept inside a shared PKCS#11 HSM; never leaves it.
	//   "tpm-kek"      — replicated key wrapped under a KEK derived from a cluster
	//                    seed sealed to each node's local TPM.
	CAKeyStrategy string `mapstructure:"ca_key_strategy"`
}

// RaftACMEEnrollmentConfig configures TPM-attested ACME enroll-01 provisioning
// of Raft node certificates with the runtime TrustedCA as the single cluster
// trust anchor.
type RaftACMEEnrollmentConfig struct {
	// Enabled switches Raft node certificate provisioning to ACME-attested
	// mode.
	Enabled bool `mapstructure:"enabled"`

	// DirectoryURL is the ACME directory endpoint of an existing cluster node
	// (e.g. "https://node1:8443/acme/directory"). Required on JOINING nodes;
	// ignored on the first node, which issues in-process from the local
	// TrustedCA.
	DirectoryURL string `mapstructure:"directory_url"`

	// ChallengePort is the local HTTP port this node serves the enroll-01
	// credential-activation exchange on while enrolling. Defaults to "8080".
	ChallengePort string `mapstructure:"challenge_port"`
}

// RaftBootstrapConfig holds the join-side bootstrap source a node uses to
// fetch the cluster CA bundle from an existing cluster member. Configure
// whichever method the bootstrapping node advertises (Noise NK is the default).
type RaftBootstrapConfig struct {
	// NoiseKey is the hex Curve25519 public key of an existing node's Noise NK
	// bootstrap server (the first node logs it at startup).
	NoiseKey string `mapstructure:"noise_key"`

	// NoiseAddr is the host:port of that Noise NK bootstrap server.
	NoiseAddr string `mapstructure:"noise_addr"`

	// SPKIPin is a hex SHA-256 SPKI pin used to fetch the bundle over HTTPS.
	SPKIPin string `mapstructure:"spki_pin"`

	// ServerAddr is the host:port of the bootstrap HTTPS server (SPKI / DANE).
	ServerAddr string `mapstructure:"server_addr"`

	// DANEDNS is the DNS server used for DANE TLSA validation of the bootstrap
	// server certificate.
	DANEDNS string `mapstructure:"dane_dns"`

	// DANEDoT enables DNS-over-TLS for the DANE lookup.
	DANEDoT bool `mapstructure:"dane_dot"`

	// DirectURL fetches the bundle over plain HTTPS with the system trust
	// store. Last-resort fallback for trusted networks.
	DirectURL string `mapstructure:"direct_url"`
}

// StorageConfig contains storage engine configuration.
type StorageConfig struct {
	// Backend specifies the storage backend type.
	// Options: "memory", "file", "pebble"
	// Default: "pebble"
	// NOTE: Use server.mode to control Raft consensus (standalone vs cluster).
	Backend string `mapstructure:"backend"`

	// Partitions defines named storage partitions with per-partition data directories.
	// If empty, a single "default" partition is used with DataDir.
	Partitions PartitionsConfig `mapstructure:"partitions"`

	DataDir               string `mapstructure:"data_dir"`
	WALDir                string `mapstructure:"wal_dir"`
	CacheSize             int64  `mapstructure:"cache_size"`
	MaxOpenFiles          int    `mapstructure:"max_open_files"`
	MaxConcurrentReads    int    `mapstructure:"max_concurrent_reads"`
	MaxConcurrentWrites   int    `mapstructure:"max_concurrent_writes"`
	DisableWAL            bool   `mapstructure:"disable_wal"`
	SyncWrites            bool   `mapstructure:"sync_writes"`
	BytesPerSync          int    `mapstructure:"bytes_per_sync"`
	MemTableSize          int64  `mapstructure:"mem_table_size"`
	MemTableStopWrites    int    `mapstructure:"mem_table_stop_writes"`
	L0CompactionThreshold int    `mapstructure:"l0_compaction_threshold"`
	L0StopWritesThreshold int    `mapstructure:"l0_stop_writes_threshold"`
	BlockSize             int    `mapstructure:"block_size"`
	TargetFileSize        int64  `mapstructure:"target_file_size"`
	CompressionType       string `mapstructure:"compression_type"`
}

// SecurityConfig contains security and encryption settings.
type SecurityConfig struct {
	// TLSEnabled indicates TLS is enabled. TLS is REQUIRED for every network
	// transport: there is no operator-facing knob to disable it (the field is
	// excluded from config-file/env binding) and the loader forces it true.
	// The field exists only as internal lifecycle state so in-process test
	// harnesses can compose listener-less servers.
	TLSEnabled    bool   `mapstructure:"-"`
	TLSCertFile   string `mapstructure:"tls_cert_file"`
	TLSKeyFile    string `mapstructure:"tls_key_file"`
	TLSCAFile     string `mapstructure:"tls_ca_file"`
	TLSClientAuth bool   `mapstructure:"tls_client_auth"`

	// RequireMTLS enforces a strict mTLS posture (SC-7): when true the server
	// TLS uses RequireAndVerifyClientCert instead of the default
	// VerifyClientCertIfGiven, rejecting any connection that does not present
	// a client certificate chaining to a trusted CA. Default false preserves
	// the current opportunistic-mTLS behavior.
	RequireMTLS bool `mapstructure:"require_mtls"`

	FIPSMode            bool   `mapstructure:"fips_mode"`
	EncryptionAlgorithm string `mapstructure:"encryption_algorithm"`

	// SealEnabled enables seal/unseal functionality for protecting
	// encryption keys at rest. When enabled, the server starts in a
	// sealed state and requires manual unsealing before operations.
	SealEnabled bool `mapstructure:"seal_enabled"`

	// RequireAuthz enables the default-deny scope -> RBAC authorization gate on
	// the network transports (REST, gRPC, QUIC, MCP, Unix). When true,
	// non-public, non-cluster-mTLS requests must present a valid platform token
	// carrying the required scope; token-less requests are denied. In embedded
	// mode all network transports are disabled so this flag has no effect on
	// data-plane calls — embedded callers rely on NewSystemContext or an injected
	// Subject to satisfy service-layer authorization checks. Production configs
	// must leave this true (the default); only InsecureDevMode configs set it
	// false.
	RequireAuthz bool `mapstructure:"require_authz"`

	// InsecureDevMode explicitly acknowledges running with authorization
	// disabled on network transports. It is the operator's signed consent to a
	// fail-open posture: when RequireAuthz is false AND at least one network
	// transport (REST, gRPC, QUIC, MCP) is enabled, configuration validation
	// REFUSES to start the server unless this flag is true. This guards against
	// a production config silently reaching fail-open, a violation of
	// FedRAMP/HIPAA/PCI/IL4-5 controls. Intended ONLY for local development;
	// it must NEVER be set in production. The Unix domain socket is not a
	// network transport and does not trigger the gate.
	InsecureDevMode bool `mapstructure:"insecure_dev_mode"`

	// EmbeddedSystemPrincipal is the named system identity used for in-process
	// calls when the server runs in embedded mode. It must be non-empty when
	// RequireAuthz is true and the server is embedded. The value is bound to the
	// composition context via auth.NewSystemContext so all daemon-internal
	// operations (schedulers, KMS rotation, CA issuance) carry an explicit
	// identity rather than relying on anonymous access. The default value
	// "system" matches the reserved system-tenant convention. Changing this in
	// production has no security effect — it is a label only; actual
	// authorization bypass is governed by auth.IsSystemContext.
	// This field is ignored when InsecureDevMode is true.
	EmbeddedSystemPrincipal string `mapstructure:"embedded_system_principal"`

	// AuthzMode is the deployment-level default platform-token authorization mode.
	// It governs whether an online liveness/revocation check is enforced in
	// addition to the always-offline scope decision. Valid values: "offline"
	// (trust signed scopes + expiry only), "online" (uncached liveness check every
	// request), "hybrid" (cached liveness check; the default). A per-tenant override
	// in TenantSettings.AuthzMode takes precedence; an empty per-tenant value falls
	// back to this deployment default. Default: "hybrid".
	AuthzMode string `mapstructure:"authz_mode"`

	// LivenessCacheTTL bounds how long a positive hybrid-mode liveness result is
	// cached before the next request live-checks again. Ignored in offline and
	// online modes. When zero, DefaultLivenessCacheTTL (15s) is used.
	LivenessCacheTTL time.Duration `mapstructure:"liveness_cache_ttl"`

	// Authorization configures the unified authorization system.
	Authorization AuthorizationConfig `mapstructure:"authorization"`

	// TenantIsolation configures the tenant isolation strategy.
	TenantIsolation TenantIsolationConfig `mapstructure:"tenant_isolation"`
}

// TenantIsolationConfig selects the per-tenant isolation strategy.
type TenantIsolationConfig struct {
	// Mode is the isolation strategy. Accepted values: "keyspace" (default), "barrier".
	// keyspace — data is isolated via key prefixes (__t:{tenantID}:). Zero overhead,
	//            fully backward compatible.
	// barrier  — each tenant gets its own cryptographic barrier backed by go-qrdb's
	//            BarrierRegistry. Provides encryption isolation at the cost of an
	//            initialize/unseal step per tenant.
	Mode string `mapstructure:"mode" json:"mode" yaml:"mode"`
}

// AuthorizationConfig contains authorization provider configuration.
type AuthorizationConfig struct {
	// Provider specifies the authorization provider to use.
	// Options: "rbac" (default), "abac", "hybrid"
	// - rbac: Role-based access control (qrdb-owned role model)
	// - abac: Attribute-based access control using PolicyEngine
	// - hybrid: ABAC-first with RBAC fallback for gradual migration
	Provider string `mapstructure:"provider"`

	// RBAC contains role-based access control configuration.
	RBAC RBACAuthzConfig `mapstructure:"rbac"`

	// ABAC contains attribute-based access control configuration.
	// Only used when Provider is "abac" or "hybrid".
	ABAC ABACAuthzConfig `mapstructure:"abac"`

	// Audit contains authorization audit logging configuration.
	Audit AuthzAuditConfig `mapstructure:"audit"`
}

// RBACAuthzConfig contains RBAC-specific authorization settings.
type RBACAuthzConfig struct {
	// Enabled enables RBAC authorization.
	// Default: true
	Enabled bool `mapstructure:"enabled"`

	// DefaultRole is the role assigned to unauthenticated requests.
	// If empty, unauthenticated requests are denied.
	// Options: "readonly", "readwrite", "admin", or a custom role name
	DefaultRole string `mapstructure:"default_role"`

	// AllowAnonymous allows anonymous access to public endpoints.
	// Default: true (health, metrics, etc. are accessible without auth)
	AllowAnonymous bool `mapstructure:"allow_anonymous"`
}

// ABACAuthzConfig contains ABAC-specific authorization settings.
type ABACAuthzConfig struct {
	// Enabled enables ABAC authorization.
	// Default: false
	Enabled bool `mapstructure:"enabled"`

	// CacheTTL is the duration to cache policy evaluation results.
	// Set to 0 to disable caching.
	// Default: 5m
	CacheTTL time.Duration `mapstructure:"cache_ttl"`

	// PolicyDir is the directory containing ABAC policy files.
	// If empty, uses the default policy configuration.
	PolicyDir string `mapstructure:"policy_dir"`
}

// AuthzAuditConfig contains authorization audit logging settings.
type AuthzAuditConfig struct {
	// Enabled enables audit logging of authorization decisions.
	// Default: true
	Enabled bool `mapstructure:"enabled"`

	// LogDenied logs denied authorization requests.
	// Default: true
	LogDenied bool `mapstructure:"log_denied"`

	// LogAllowed logs allowed authorization requests.
	// Default: false (set to true for compliance requirements)
	LogAllowed bool `mapstructure:"log_allowed"`
}

// LoggingConfig contains logging configuration.
type LoggingConfig struct {
	Level     string `mapstructure:"level"`
	Format    string `mapstructure:"format"`
	Output    string `mapstructure:"output"`
	File      string `mapstructure:"file"`
	Component string `mapstructure:"component"`
}

// DiscoveryConfig contains service discovery configuration.
type DiscoveryConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	Type           string        `mapstructure:"type"` // "kubernetes", "consul", "multicast", "static", "dns"
	Namespace      string        `mapstructure:"namespace"`
	LabelSelector  string        `mapstructure:"label_selector"`
	UpdateInterval time.Duration `mapstructure:"update_interval"`

	// Kubernetes specific
	KubeConfig string `mapstructure:"kube_config"`

	// Consul specific
	ConsulAddress string `mapstructure:"consul_address"`
	ConsulToken   string `mapstructure:"consul_token"`

	// Multicast specific - for zero-config LAN discovery
	Multicast MulticastDiscoveryConfig `mapstructure:"multicast"`

	// Static specific - for fixed peer lists
	StaticPeers []string `mapstructure:"static_peers"`

	// DNS specific - for DNS-based discovery
	DNSName     string        `mapstructure:"dns_name"`
	DNSInterval time.Duration `mapstructure:"dns_interval"`
}

// MulticastDiscoveryConfig configures UDP multicast peer discovery.
type MulticastDiscoveryConfig struct {
	// Group is the multicast IP address to use (default: 239.255.77.77)
	Group string `mapstructure:"group"`

	// Port is the UDP port to use (default: 63777)
	Port int `mapstructure:"port"`

	// Interface is the network interface name to use (optional)
	Interface string `mapstructure:"interface"`

	// BroadcastInterval is how often to send announcements (default: 2s)
	BroadcastInterval time.Duration `mapstructure:"broadcast_interval"`

	// PeerTTL is how long to remember a peer without hearing from it (default: 10s)
	PeerTTL time.Duration `mapstructure:"peer_ttl"`

	// ClusterSecret is a shared secret for authenticating discovery messages (optional)
	// If set, all discovery messages include an HMAC-SHA256 signature
	// If empty, discovery is unauthenticated (open cluster)
	ClusterSecret string `mapstructure:"cluster_secret"`
}

// MetricsConfig contains metrics and observability configuration.
type MetricsConfig struct {
	// Enabled enables metrics collection and export.
	// Default: true
	Enabled bool `mapstructure:"enabled"`

	// AdvancedEnabled enables advanced metrics with higher cardinality
	// and more detailed breakdowns. May increase memory usage.
	// Default: false
	AdvancedEnabled bool `mapstructure:"advanced_enabled"`
}

// CodecConfig contains codec-related configuration.
type CodecConfig struct {
	DefaultFormat string `mapstructure:"default_format"` // "json", "msgpack", "binary"
	EnableBinary  bool   `mapstructure:"enable_binary"`
	EnableJSON    bool   `mapstructure:"enable_json"`
	EnableMsgpack bool   `mapstructure:"enable_msgpack"`
}

// SealConfig contains seal/unseal configuration for encryption at rest.
type SealConfig struct {
	// Enabled enables seal/unseal functionality.
	// When enabled, the server starts sealed and requires unsealing.
	// Default: false
	Enabled bool `mapstructure:"enabled"`

	// Strategy is the canonical sealing strategy name.
	// Matches the StrategyID constants defined in go-quicraft/pkg/seal and
	// re-exported by go-qrdb/pkg/seal: "none", "passphrase", "shamir", "tpm2",
	// "pkcs11", "awskms", "gcpkms", "azurekv", "vault".
	//
	// "none"       — identity passthrough (no encryption); dev/test only.
	// "passphrase" — Argon2id or PBKDF2-SHA256 (FIPS) key derivation (default).
	// "shamir"     — Shamir M-of-N secret sharing over the passphrase strategy.
	// Hardware strategies (tpm2, pkcs11, cloud KMS) require the corresponding
	// go-xkms strategy to be registered via XKMSCryptoOptions.
	//
	// Default: "passphrase"
	Strategy string `mapstructure:"strategy"`

	// StrategyExplicit is a runtime-only flag (never serialized) set by the
	// daemon entry point when the operator explicitly supplied a seal strategy
	// via config file or environment. When true the configured Strategy wins
	// over any barrier strategy persisted by `quicbaasd init` in the data dir.
	// When false (the default) the persisted init strategy is authoritative for
	// an already-initialised data dir. See pkg/server/init_marker.go
	// (resolveBarrierStrategy) for the precedence rules.
	StrategyExplicit bool `mapstructure:"-"`

	// Mode specifies manual (Shamir shares) or auto (TPM/Cloud KMS) unseal.
	// Options: "manual", "auto"
	// Default: "manual"
	Mode string `mapstructure:"mode"`

	// Threshold is the minimum number of Shamir shares needed to unseal.
	// Default: 2
	Threshold int `mapstructure:"threshold"`

	// TotalShares is the total number of Shamir shares to generate.
	// Default: 3
	TotalShares int `mapstructure:"total_shares"`

	// BarrierDir is the directory for the sealed root key blob.
	// Default: "$DataDir/barrier"
	BarrierDir string `mapstructure:"barrier_dir"`

	// PreferenceOrder is the strategy preference order for sealing.
	// Options: "passphrase", "tpm2", "pkcs11", "awskms", "gcpkms", "azurekv"
	// Default: ["passphrase"]
	PreferenceOrder []string `mapstructure:"preference_order"`

	// RecoveryEnabled enables generation of recovery keys.
	// Default: true
	RecoveryEnabled bool `mapstructure:"recovery_enabled"`

	// RecoveryThreshold is the minimum recovery keys needed.
	// Default: 3
	RecoveryThreshold int `mapstructure:"recovery_threshold"`

	// RecoveryTotal is the total recovery keys to generate.
	// Default: 5
	RecoveryTotal int `mapstructure:"recovery_total"`

	// AWSKMS configures the AWS KMS sealing strategy (strategy "awskms").
	// Consulted only when Strategy is "awskms". See AWSSealConfig.
	AWSKMS AWSSealConfig `mapstructure:"awskms"`

	// GCPKMS configures the Google Cloud KMS sealing strategy (strategy "gcpkms").
	// Consulted only when Strategy is "gcpkms". See GCPSealConfig.
	GCPKMS GCPSealConfig `mapstructure:"gcpkms"`

	// AzureKV configures the Azure Key Vault sealing strategy (strategy "azurekv").
	// Consulted only when Strategy is "azurekv". See AzureSealConfig.
	AzureKV AzureSealConfig `mapstructure:"azurekv"`

	// Vault configures the HashiCorp Vault Transit sealing strategy (strategy "vault").
	// Consulted only when Strategy is "vault". See VaultSealConfig.
	Vault VaultSealConfig `mapstructure:"vault"`

	// TPM2 configures the TPM 2.0 sealing strategy (strategy "tpm2").
	// Consulted only when Strategy is "tpm2". See TPM2SealConfig.
	TPM2 TPM2SealConfig `mapstructure:"tpm2"`

	// PKCS11 configures the PKCS#11 HSM sealing strategy (strategy "pkcs11").
	// Consulted only when Strategy is "pkcs11". See PKCS11SealConfig.
	PKCS11 PKCS11SealConfig `mapstructure:"pkcs11"`
}

// CAConfig contains Certificate Authority configuration for TLS bootstrap.
type CAConfig struct {
	// RootCommonName is the root CA certificate common name.
	// Default: "QuicBaaS Root CA"
	RootCommonName string `mapstructure:"root_common_name"`

	// RootOrganization is the root CA certificate organization.
	// Default: "QuicBaaS"
	RootOrganization string `mapstructure:"root_organization"`

	// RootValidYears is how many years the root CA certificate is valid.
	// Default: 10
	RootValidYears int `mapstructure:"root_valid_years"`

	// RootKeyAlgorithm is the key algorithm for the root CA.
	// Options: "ECDSA", "RSA"
	// Default: "ECDSA"
	RootKeyAlgorithm string `mapstructure:"root_key_algorithm"`

	// RootKeyCurve is the ECDSA curve for the root CA key.
	// Options: "P-256", "P-384", "P-521"
	// Default: "P-256"
	RootKeyCurve string `mapstructure:"root_key_curve"`

	// IntermediateEnabled enables intermediate CA generation.
	// Default: true
	IntermediateEnabled bool `mapstructure:"intermediate_enabled"`

	// IntermediateCommonName is the intermediate CA certificate common name.
	// Default: "QuicBaaS Intermediate CA"
	IntermediateCommonName string `mapstructure:"intermediate_common_name"`

	// IntermediateValidYears is how many years the intermediate CA is valid.
	// Default: 5
	IntermediateValidYears int `mapstructure:"intermediate_valid_years"`

	// IntermediateKeyAlgorithm is the key algorithm for the intermediate CA.
	// Default: "ECDSA"
	IntermediateKeyAlgorithm string `mapstructure:"intermediate_key_algorithm"`

	// IntermediateKeyCurve is the ECDSA curve for the intermediate CA key.
	// Default: "P-256"
	IntermediateKeyCurve string `mapstructure:"intermediate_key_curve"`

	// DefaultValidityDays is the default certificate validity in days.
	// Default: 365
	DefaultValidityDays int `mapstructure:"default_validity_days"`

	// IncludeLocalhostSANS includes localhost in certificate SANs.
	// Default: true
	IncludeLocalhostSANS bool `mapstructure:"include_localhost_sans"`

	// KeysDir is the directory for CA private keys (filesystem-based).
	// Default: "$DataDir/keys"
	KeysDir string `mapstructure:"keys_dir"`

	// KeystoreType selects the key store for the CA signing key only.
	// Values: "" (inherit from crypto.subsystems.ca.backend), "software",
	// "tpm2", "pkcs11". The TLS server key always uses software regardless
	// of this setting. Default: "" (inherits; backward compatible = software).
	KeystoreType string `mapstructure:"keystore_type"`

	// RootKeyPassword is the password used to protect the CA signing key in
	// PKCS#8 format. When empty the key is stored unencrypted (default for
	// backward compatibility). The value is a secret reference URI
	// (e.g. "credential://ca-key-password", "kms://keys/ca-password") or a
	// literal string that is used verbatim. Resolution occurs via the unified
	// secret registry after the credential store is online — so any reference
	// scheme supported by the registry is valid here.
	RootKeyPassword string `mapstructure:"root_key_password"`
}

// Clone creates a deep copy of the Config.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}

	clone := &Config{
		Server:     c.Server,
		Raft:       c.Raft,
		Storage:    c.Storage,
		Security:   c.Security,
		Auth:       c.Auth,
		Logging:    c.Logging,
		RAG:        c.RAG,
		Scheduler:  c.Scheduler,
		TimeSeries: c.TimeSeries,
		Messaging:  c.Messaging,
		Seal:       c.Seal,
		CA:         c.CA,
		Crypto:     c.Crypto,
		QRDB:       c.QRDB,
		Databases:  c.Databases,
	}

	// Deep copy DatabasesConfig slices
	if c.Databases.AllowedEngines != nil {
		clone.Databases.AllowedEngines = make([]string, len(c.Databases.AllowedEngines))
		copy(clone.Databases.AllowedEngines, c.Databases.AllowedEngines)
	}
	if c.Databases.AllowedBackends != nil {
		clone.Databases.AllowedBackends = make([]string, len(c.Databases.AllowedBackends))
		copy(clone.Databases.AllowedBackends, c.Databases.AllowedBackends)
	}

	// Deep copy SealConfig.PreferenceOrder slice
	if c.Seal.PreferenceOrder != nil {
		clone.Seal.PreferenceOrder = make([]string, len(c.Seal.PreferenceOrder))
		copy(clone.Seal.PreferenceOrder, c.Seal.PreferenceOrder)
	}

	return clone
}
