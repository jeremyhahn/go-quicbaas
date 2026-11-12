// Package config provides configuration structures and management for QuicBaaS.
// It supports loading configuration from files, environment variables, and CLI flags
// with proper validation and defaults.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/spf13/viper"

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

// IsClusterLikeMode reports whether mode runs more than one node.
//
// This is the SINGLE source of truth for that question, and it exists because
// the answer was previously spelled out at each call site. A mode added to the
// set in one place and missed in another matches for config validation while
// falling through everywhere else — which is exactly how devcluster came to be
// validated as a Raft mode while the scheduler treated it as standalone and
// gave every node its own unguarded dispatch. Callers ask here; nobody
// re-derives it.
func IsClusterLikeMode(mode string) bool {
	return mode == ServerModeCluster || mode == ServerModeDevCluster
}

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
	//
	// It is a NAME comparison and therefore not evidence of anything: a
	// self-signed certificate can carry whatever issuer DN it likes. Hardware
	// is established by AttestationCAFile below, not here.
	TrustedIssuers []string `mapstructure:"trusted_issuers"`

	// AttestationCAFile is the path to a PEM bundle of PIV attestation CA
	// certificates — the VENDOR roots that sign a card's slot attestation
	// certificates (e.g. the Yubico PIV Attestation root).
	//
	// Point it at the vendor root, never at the CA that issues login
	// certificates. A login is hardware-backed only when the client presents a
	// SEPARATE slot attestation certificate (`ykman piv keys attest <slot>`)
	// that chains here, carries a PIV attestation profile extension, and
	// covers the authenticating key. The login certificate is never its own
	// evidence: if it were, pointing this setting at the login CA would make
	// every software-held certificate report hardware.
	//
	// Required for hardware-attested PIV login. When it is empty the endpoint
	// refuses rather than assuming: see AllowUnattested.
	AttestationCAFile string `mapstructure:"attestation_ca_file"`

	// AttestationIntermediatesFile is an optional PEM bundle of vendor
	// sub-CA certificates used to complete an attestation chain. Card-specific
	// intermediates (a YubiKey's slot-F9 certificate) travel with the
	// credential and do not belong here.
	AttestationIntermediatesFile string `mapstructure:"attestation_intermediates_file"`

	// AllowUnattested permits PIV login for a certificate whose hardware
	// provenance was NOT proven — because no attestation CA is configured, or
	// because the certificate does not chain to one.
	//
	// It is an explicit, deliberately unattractive opt-in for development and
	// for test environments driving a software smartcard. It does not make an
	// unattested login look attested: the issued token carries the level that
	// was actually proven and does NOT carry the hardware-attested ACR, so an
	// authorization policy that requires hardware still refuses it.
	//
	// Default false: a deployment that configures nothing refuses PIV login
	// rather than minting hardware claims it never verified.
	AllowUnattested bool `mapstructure:"allow_unattested"`
}

// IAMAAGUIDConfig holds operator configuration for FIDO2 authenticator AAGUID enforcement.
type IAMAAGUIDConfig struct {
	// Mode controls how unknown AAGUIDs are handled during WebAuthn registration.
	// "enforce" (DEFAULT, and what an EMPTY value selects): unknown AAGUIDs
	// cause registration to be rejected with aaguid_not_allowed.
	// "log_only": unknown AAGUIDs are permitted; a warning is logged. It is an
	// explicit opt-out — only this exact value selects it.
	//
	// Any other non-empty value is REJECTED at startup with AAGUIDModeError.
	// A typo does not quietly select the strict policy: an operator who wrote
	// "log-only" meant to relax the gate, and a deployment that silently
	// enforced instead would refuse every enrollment for a reason nothing
	// stated. Both directions of misconfiguration are told to the operator
	// rather than guessed at.
	Mode string `mapstructure:"mode"`

	// AllowlistFile is the path to a JSON file naming the authenticator models
	// this deployment admits. Two shapes are accepted:
	//
	//   - a JSON ARRAY an operator curates by hand:
	//     [{"aaguid": "...", "name": "...", "attestation_level": "..."}]
	//   - a decoded FIDO Metadata Service v3 document: a JSON OBJECT with an
	//     "entries" array. Only entries[].aaguid, the metadata statement's
	//     description and the certification history in entries[].statusReports
	//     are read; entries the Alliance has REVOKED or reported compromised
	//     are SKIPPED, so importing a blob wholesale cannot admit a withdrawn
	//     authenticator. Entries with no AAGUID (the U2F and UAF models every
	//     blob carries) are skipped too.
	//
	// When empty the allowlist is empty, which under the default enforce mode
	// denies every authenticator with a message naming this setting. That is
	// deliberate: an unconfigured deployment must refuse rather than accept any
	// authenticator as an administrator credential. A file that cannot be read
	// or parsed is a fatal startup error, not a silent fallback to permissive.
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
// an invalid combination of field values. The boot gate (Validate, through
// Validator.validateIAM) calls it, so quicbaasd refuses to start when anonymous
// registration would land clients in the system tenant.
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
// an invalid combination of field values. The boot gate (Validate, through
// Validator.validateIAM) calls it, so quicbaasd refuses to start on an
// invalid enabled section instead of failing at the first registration.
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
	if _, ok := modeSet[c.DefaultIsolationMode]; !ok {
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

// IAMSelfServiceRegistrationKeys are the self-service registration keys,
// bound to the environment by BindIAMSelfServiceRegistrationEnv.
//
// EXPLICIT, BECAUSE AutomaticEnv IS NOT ENOUGH: a key no config file names is
// not decoded from the environment, so without these bindings an
// environment-only deployment could not turn registration on or off.
//
// `allowed_isolation_modes` IS DELIBERATELY ABSENT: viper does not decode a
// list from the environment reliably through Unmarshal, so it is file-only.
var IAMSelfServiceRegistrationKeys = []string{
	"iam.self_service_registration.enabled",
	"iam.self_service_registration.require_admin_approval",
	"iam.self_service_registration.require_email_verification",
	"iam.self_service_registration.default_role",
	"iam.self_service_registration.default_isolation_mode",
	"iam.self_service_registration.tenant_barrier_volume_pool",
}

// BindIAMSelfServiceRegistrationEnv binds the self-service registration keys
// to their environment variables (QUICBAAS_IAM_SELF_SERVICE_REGISTRATION_*).
func BindIAMSelfServiceRegistrationEnv(v *viper.Viper) {
	for _, key := range IAMSelfServiceRegistrationKeys {
		_ = v.BindEnv(key)
	}
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
	SDLC          SDLCConfig          `mapstructure:"sdlc"`
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

// AgentRefConfig names a tenant Agent or pool from configuration: exactly one
// of AgentID and PoolID, or neither for "not configured". It mirrors
// orchestration.AgentRef to avoid an import cycle.
type AgentRefConfig struct {
	AgentID string `mapstructure:"agent_id"`
	PoolID  string `mapstructure:"pool_id"`
}

// LeadAgentConfig configures the optional LLM lead agent for intelligent
// orchestration: the system tenant's Agent or pool it runs as. The Agent
// supplies the backend, the model and the system prompt.
type LeadAgentConfig struct {
	AgentRefConfig `mapstructure:",squash"`
}

// EmbeddedFleetNodeConfig configures the optional in-process agent that
// quicbaasd can boot alongside the orchestrator. When Enabled is true,
// quicbaasd starts a full agent runtime (identical to `quicbaas fleet serve`)
// and registers it through the same fleet registry that remote fleet nodes use.
// The runtime communicates with the orchestrator via a bufconn loopback — no
// network round-trip — while retaining a proper mTLS identity issued by the
// local CA.
type EmbeddedFleetNodeConfig struct {
	// Enabled starts an in-process agent runtime at quicbaasd boot.
	// Default: false. When false, quicbaasd is a pure orchestrator.
	Enabled bool `mapstructure:"enabled"`

	// Backends lists the backend kinds the embedded agent exposes.
	// Default: ["ollama"]
	Backends []string `mapstructure:"backends"`

	// OllamaURL is the Ollama API endpoint the embedded Ollama backend dials.
	// Default: DefaultOllamaBaseURL. An empty value is refused at startup
	// when the "ollama" backend is enabled.
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

	// SandboxCapAdd is the capability set a sandboxed container is given back
	// after every capability is dropped (--cap-drop ALL). Empty keeps the
	// default CHOWN, DAC_OVERRIDE, FOWNER, SETGID, SETUID — what the worker's
	// sudo and apk need. A non-empty list REPLACES that set; "ALL" is refused.
	SandboxCapAdd []string `mapstructure:"sandbox_cap_add"`

	// SandboxPidsLimit bounds processes plus threads in a sandboxed container
	// (--pids-limit). Zero takes orchestration.DefaultSandboxPidsLimit;
	// negative is refused.
	SandboxPidsLimit int64 `mapstructure:"sandbox_pids_limit"`

	// SandboxUlimits overrides or adds a sandboxed container's resource
	// limits, each in the runtime's own "name=soft[:hard]" form. The default
	// is a memlock of orchestration.DefaultSandboxMemlockBytes (4 GiB, what
	// the CI runners give); an entry naming memlock replaces it. An unknown
	// name, a negative value or a resource named twice is refused at startup.
	SandboxUlimits []string `mapstructure:"sandbox_ulimits"`

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
	// image at startup, and to pull an image a spawn names (a project
	// binding's worker image) that this host does not hold before starting
	// it. Leave false for images that exist only in the local daemon image
	// store (e.g. CI-built e2e images that were never pushed).
	AutoPull bool `mapstructure:"auto_pull"`
}

// SDLCConfig configures the SDLC module of the Software Factory: which forge
// hosts a bound project may live on, what a worker runs to verify its work,
// and how a parked task's reason reaches its issue.
//
// The daemon does no clone, copy or worktree of its own: the worker's
// container clones the project repository and the project's context
// repositories itself, side by side, from the spec it is spawned with.
//
// The generic knobs of a job — the attempt budget, the lease TTL, the stage
// timeouts, the idle thresholds, the sweep interval — belong to the engine
// and live under orchestration:. They are refused here BY NAME
// (RemovedSDLCKeys) rather than silently ignored, so a configuration that
// still carries them fails to load with the key and its new home.
type SDLCConfig struct {
	// Enabled runs the SDLC module on this node.
	//
	// Default: false. The module registers an unauthenticated webhook route
	// and holds forge credentials, so it is opt-in.
	Enabled bool `mapstructure:"enabled"`

	// IssuePageLimit bounds one project sweep's issue listing.
	//
	// Default: 50. A sweep that fills its page has probably not seen every
	// triggered issue; the next sweep sees the rest once the first page has
	// been adopted and labelled in progress.
	IssuePageLimit int `mapstructure:"issue_page_limit"`

	// VerificationCommands is DECLARED AND UNREAD — see go-quicbaas#625.
	//
	// It described the validating-phase criteria of a canonical sdlc
	// workflow that no longer exists (deleted by #596, replaced by the sdlc
	// runner in #607). What the runner runs is the BINDING's own
	// verification list, and what admits a command is the operator
	// allowlist orchestration.verification_commands. Nothing reads this
	// field; setting it has no effect.
	//
	// Default: make build, make test, make lint.
	VerificationCommands []string `mapstructure:"verification_commands"`

	// CheckoutRoot is where the worker's container finds its checkouts: the
	// project at <root>/<project name> and each context repository beside
	// it. It is the path INSIDE the container — the daemon clones nothing —
	// and it is what the runner's clone commands name there.
	//
	// Default: /workspace (orchestration.WorkspaceCheckoutRoot). An operator
	// whose worker image lays its filesystem out differently names it here.
	CheckoutRoot string `mapstructure:"checkout_root"`

	// TriageAgent is the NODE's default triage agent: the Agent or pool every
	// bound project triages with unless its own binding names one.
	//
	// TRIAGE IS A READING TASK AND IT IS THE HIGHEST-VOLUME ONE. Every open
	// issue on every bound project is re-read on every sweep, and nothing
	// triage does writes code. Without this default the fallback is the
	// project's WORKER, so a fleet that wrote its code with a frontier model
	// also re-read its entire backlog with one, every sweep, forever.
	//
	// IT NAMES AN AGENT OF THE PROJECT'S TENANT, and is taken only where that
	// tenant defines it for the triage role; a tenant that does not triages
	// with its worker. Default: empty, which falls back to the worker.
	TriageAgent AgentRefConfig `mapstructure:"triage_agent"`

	// RepoHosts is the allowlist of forge hosts a bound project's repository
	// may live on, matched by hostname or host:port. A project whose
	// repository is elsewhere is refused at bind time.
	//
	// Default: empty — naming the forge host is the operator's explicit act.
	RepoHosts []string `mapstructure:"repo_hosts"`

	// AllowLocalRepos additionally admits file:// and bare-path repositories.
	// For test rigs; never on a node that serves tenants.
	//
	// Default: false.
	AllowLocalRepos bool `mapstructure:"allow_local_repos"`

	// MergeAttempts bounds the retries when the forge reports a pull request
	// not ready to merge yet.
	//
	// Default: 6. Must be positive.
	MergeAttempts int `mapstructure:"merge_attempts"`

	// Rulers are the forge logins, beyond a repository's owner and its
	// binding's own rulers, whose comment on a parked issue is an owner
	// ruling that readmits the task. See SDLCRulers.
	Rulers SDLCRulers `mapstructure:"rulers"`

	// Surfacing bounds the Blocked hook's delivery of a parked task's reason
	// to its forge issue. Only the retry is configured here.
	Surfacing SDLCSurfacing `mapstructure:"surfacing"`

	// LabelReconcile paces the sweep that repairs forge labels an ordinary
	// write never managed to make.
	LabelReconcile SDLCLabelReconcile `mapstructure:"label_reconcile"`

	// Webhook bounds the forge webhook ingress.
	Webhook SDLCWebhook `mapstructure:"webhook"`

	// Forge bounds every call the module makes to a forge: how long one call
	// may take, and how hard an unanswered one is retried.
	Forge SDLCForge `mapstructure:"forge"`

	// TriageSweepInterval is how often the triage backstop pass visits every
	// bound project. Triage is ordinarily woken by the forge webhook within
	// a second of an issue being filed; this pass catches the deliveries
	// that never arrived — an unprovisioned hook, a forge outage, a daemon
	// restarting through the delivery.
	//
	// Default: sdlc.DefaultTriageSweepInterval (15m). Zero takes the
	// default.
	TriageSweepInterval time.Duration `mapstructure:"triage_sweep_interval"`

	// ReviewSweepInterval is how often the review floor visits every bound
	// project: the pass that finds an open pull request whose pipeline is
	// green and which carries no review verdict, and reviews it. A review is
	// ordinarily dispatched from the forge webhook within a second of the
	// pipeline finishing; this pass is what makes the review reachable at all
	// on a node whose delivery path is broken or was never provisioned — and
	// the only thing that can recover a backlog, since a pipeline event for an
	// already-green head is never redelivered (go-quicbaas#933).
	//
	// SHORTER THAN TriageSweepInterval BY DEFAULT because it is cheaper: this
	// pass makes forge reads and starts work only for a pull request that is
	// genuinely unjudged, where a triage pass starts a job per bound project.
	//
	// Default: sdlc.DefaultReviewSweepInterval (5m). Zero takes the default.
	ReviewSweepInterval time.Duration `mapstructure:"review_sweep_interval"`

	// UnblockSweepInterval is how often blocked tasks are re-examined and the
	// ones whose condition has cleared returned to the queue.
	//
	// Default: sdlc.DefaultUnblockSweepInterval (10m). Zero takes the default.
	UnblockSweepInterval time.Duration `mapstructure:"unblock_sweep_interval"`

	// TrainSweepInterval is how often the merge-train backstop visits every
	// trained project and starts a train where a change waits in the queue
	// with no train running. A queued change ordinarily wakes its train at
	// once; this pass closes the window in which that wake is refused because
	// a finishing train still holds the project.
	//
	// Default: sdlc.DefaultTrainSweepInterval (5m). Zero takes the default.
	TrainSweepInterval time.Duration `mapstructure:"train_sweep_interval"`

	// WorkerImage configures the scheduled, gated refresh of the Software
	// Factory worker image (pkg/sdlc/workerimage).
	WorkerImage SDLCWorkerImage `mapstructure:"worker_image"`
}

// SDLCWorkerImage configures the worker image refresh: how often it looks for
// newer agent CLIs, where it publishes the image it builds, and which model
// its gate must prove works. See docs/agent-fleet/worker-image.md.
//
// IT RUNS ONLY WHERE IT CAN. The refresh needs the SDLC module (sdlc.enabled)
// and a container engine on this node; without either it is not started, and
// the boot log says which was missing.
type SDLCWorkerImage struct {
	// Enabled runs the refresh loop.
	//
	// Default: true. A module that binds projects to a worker image and never
	// refreshes it strands every project the day a model it names needs a
	// newer CLI; opting OUT is the deliberate act.
	Enabled bool `mapstructure:"enabled"`

	// Repository is the local repository candidates are built into, and one
	// of the two repositories whose bound images the refresh keeps current.
	//
	// Default: quicbaas-software-factory-worker (what `make
	// build-worker-image` builds).
	Repository string `mapstructure:"repository"`

	// Registry is the repository promoted images are pushed to, as
	// host[:port]/owner/name — e.g. a Gitea container registry. Bindings are
	// then rebound to the pushed, digest-pinned reference, which every node
	// can pull.
	//
	// Default: empty — LOCAL MODE. Bindings are rebound to the local tag,
	// which only this host's engine holds; right for a single-host install,
	// wrong for a fleet.
	Registry string `mapstructure:"registry"`

	// RegistryUsername, RegistryTokenSecret and RegistryTokenTenant are the
	// push credential: the registry account, and the QuicBaaS secret holding
	// its token — a short, tenant-relative secret name (e.g.
	// sdlc/registry/worker-image/token) in the secrets of
	// RegistryTokenTenant, the tenant that owns the factory. The secret is
	// read only when a pass pushes, so rotating the token is a new version of
	// the secret and needs no restart. The token reaches the engine's login
	// on stdin under a private, temporary client configuration, never the
	// node-wide one.
	//
	// All three, or none: none pushes with the engine's own configuration (a
	// prior `docker login`).
	RegistryUsername    string `mapstructure:"registry_username"`
	RegistryTokenSecret string `mapstructure:"registry_token_secret"`
	RegistryTokenTenant string `mapstructure:"registry_token_tenant"`

	// NPMRegistryURL is where the latest agent CLI versions are read.
	//
	// Default: https://registry.npmjs.org. An operator behind a mirror names
	// the mirror.
	NPMRegistryURL string `mapstructure:"npm_registry_url"`

	// RefreshInterval is how often a pass runs by itself. A pass is also the
	// only thing that reads the npm registry, so this is the registry check's
	// cadence too; an operator who knows a CLI shipped requests a pass (CLI,
	// API or the web UI's "Check for updates now").
	//
	// Default: workerimage.DefaultRefreshInterval (24h). Zero takes the
	// default.
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`

	// PassTimeout bounds one pass, build and gate included.
	//
	// Default: workerimage.DefaultPassTimeout (1h). Zero takes the default.
	PassTimeout time.Duration `mapstructure:"pass_timeout"`

	// SmokeModel is the Claude model the gate always proves the new image can
	// run, whatever the bindings name.
	//
	// Default: claude-opus-5-5.
	SmokeModel string `mapstructure:"smoke_model"`

	// SmokeHost pins the gate's smoke sessions to the fleet node whose
	// engine built the candidate (matched against an agent's name or host).
	//
	// Default: empty, which resolves, per smoke, the agent registered from
	// THIS node — the embedded agent, or a daemon whose registered host is
	// this node's hostname. Name one explicitly when the local agent
	// registers under another name (e.g. it runs in a container).
	SmokeHost string `mapstructure:"smoke_host"`
}

// SDLCRulers is the node's ruler policy, per tenant. A project's binding can
// add rulers of its own on top; it cannot remove these.
//
// FILE-ONLY, like every list setting here: viper does not decode a list or a
// map from the environment reliably. A malformed login is refused when the
// SDLC module starts (sdlc.RulerPolicy.Validate), so the daemon never runs
// with a ruler set that silently matches nobody.
type SDLCRulers struct {
	// Default applies to every tenant without an entry in Tenants.
	//
	// Default: unset, which takes sdlc.DefaultRulers (the company's
	// administrative agent, company-agent). An explicit empty list leaves
	// each repository's owner and its binding's rulers as the only ones.
	Default []string `mapstructure:"default"`

	// Tenants replaces Default for the tenants it names, keyed by tenant ID.
	// An empty list gives that tenant no node-configured rulers.
	Tenants map[string][]string `mapstructure:"tenants"`
}

// SDLCWebhook bounds the forge webhook ingress.
type SDLCWebhook struct {
	// MaxBody caps an inbound delivery in bytes. Read before anything is
	// parsed or verified, because the signature check cannot run until the
	// body is in memory.
	//
	// Default: 1 MiB. Must be positive.
	MaxBody int64 `mapstructure:"max_body"`

	// PublicURL is THIS NODE's externally reachable base URL, the origin a
	// forge delivers to: scheme and host, no path — e.g.
	// "https://factory.example.com".
	//
	// REQUIRED WHEN THE MODULE IS ENABLED, and deliberately not defaulted.
	// Onboarding a project provisions a webhook on the forge pointing at
	// <public_url>/api/v1/sdlc/webhook/<projectID>, and "the webhook points
	// at this node" is a property of the NODE rather than of any project.
	// A guessed origin — the listen address, a loopback — would provision
	// every managed repository to deliver into the void and report every one
	// of them provisioned, which is the silent failure the whole onboarding
	// surface exists to end.
	PublicURL string `mapstructure:"public_url"`
}

// SDLCLabelReconcile paces the sweep that converges a bound project's forge
// labels onto the engine's own record of its tasks. Zero fields take the
// module's defaults.
//
// IT IS A REPAIR PASS, NOT A CONTROL LOOP. Nothing depends on it running: every
// ordinary ending already writes to the forge as it happens. What it fixes is
// the write that did not land — a forge outage, a node that died between
// concluding a task and posting its outcome — which is why its cadence is
// measured in minutes and its forge calls are deliberately paced.
type SDLCLabelReconcile struct {
	// Interval is how often the sweep runs. Zero takes the module's default
	// (sdlc.DefaultLabelReconcileInterval).
	Interval time.Duration `mapstructure:"interval"`

	// ForgeInterval is the minimum wall time between the sweep's forge
	// calls. Zero takes the module's default
	// (sdlc.DefaultForgeCallInterval).
	ForgeInterval time.Duration `mapstructure:"forge_interval"`
}

// SDLCForge bounds the module's forge calls. Zero fields take the module's
// defaults (sdlc.DefaultForgeTransport); the shipped values match them and a
// drift test pins the two.
//
// SPLIT BY WHAT A CALL COSTS THE FORGE. One deadline for every call was too
// long for a listing and too short for a squash merge of a large repository,
// and a merge that outlasted it landed while the job was recorded failed
// (go-quicbaas#1034).
type SDLCForge struct {
	// ReadTimeout bounds one attempt at a read. Reads are retried.
	ReadTimeout time.Duration `mapstructure:"read_timeout"`

	// WriteTimeout bounds a light write: a label, a comment, a status.
	WriteTimeout time.Duration `mapstructure:"write_timeout"`

	// HeavyWriteTimeout bounds a write the forge does real work for before it
	// answers: merging a pull request, opening one.
	HeavyWriteTimeout time.Duration `mapstructure:"heavy_write_timeout"`

	// ReadAttempts bounds the attempts at an unanswered read, or at a write
	// the adapter has proven safe to repeat. Writes that are not safe to
	// repeat are never repeated; they are settled by reading the forge.
	ReadAttempts int `mapstructure:"read_attempts"`

	// RetryBackoff is the first wait between attempts; each retry doubles it.
	RetryBackoff time.Duration `mapstructure:"retry_backoff"`

	// TokenCommands maps a forge type ("github", "gitea") to the command the
	// node runs for that forge's token. A binding of a type listed here takes
	// its token from the command, never from its TokenSecretRef. This is
	// node configuration only: no tenant, project or binding can name a
	// command.
	TokenCommands map[string]SDLCForgeTokenCommand `mapstructure:"token_commands"`
}

// SDLCForgeTokenCommand is one forge type's token command. It prints
// {"token": "...", "expires_at": "<RFC 3339>"} on stdout.
type SDLCForgeTokenCommand struct {
	// Argv is the command and its words; Argv[0] is an absolute path. No
	// shell runs it.
	Argv []string `mapstructure:"argv"`

	// AllowedBaseURLs are the forge API roots this command's token may be
	// sent to. Required: a binding whose base_url is not listed is refused
	// before the command runs.
	AllowedBaseURLs []string `mapstructure:"allowed_base_urls"`

	// RefreshBefore is how long before expires_at the command runs again.
	// Zero takes 5m.
	RefreshBefore time.Duration `mapstructure:"refresh_before"`

	// Timeout bounds one run. Zero takes 30s.
	Timeout time.Duration `mapstructure:"timeout"`
}

// SDLCSurfacing is the retry behind a parked task's comment on its issue.
// Zero fields take the module's defaults (sdlc.DefaultSurfacePolicy).
type SDLCSurfacing struct {
	// RetryAttempts is how many posts are tried before the hook fails.
	RetryAttempts int `mapstructure:"retry_attempts"`

	// RetryInterval paces the retries.
	RetryInterval time.Duration `mapstructure:"retry_interval"`

	// PostTimeout bounds one post.
	PostTimeout time.Duration `mapstructure:"post_timeout"`
}

// SDLCSurfacingMinPostTimeout and SDLCSurfacingMaxPostTimeout are the
// accepted range of an explicit post_timeout. They mirror the module's own
// bounds (sdlc.MinPostTimeout, sdlc.MaxPostTimeout); pkg/config cannot
// import pkg/sdlc, and a drift test pins the pairs.
const (
	SDLCSurfacingMinPostTimeout = time.Millisecond
	SDLCSurfacingMaxPostTimeout = 24 * time.Hour
)

// SDLCSurfacingKeys are the configuration keys the surfacing retry lives
// under, bound to the environment by BindSDLCSurfacingEnv.
var SDLCSurfacingKeys = []string{
	"sdlc.surfacing.retry_attempts",
	"sdlc.surfacing.retry_interval",
	"sdlc.surfacing.post_timeout",
}

// BindSDLCSurfacingEnv binds the surfacing keys to their environment
// variables (QUICBAAS_SDLC_SURFACING_*).
func BindSDLCSurfacingEnv(v *viper.Viper) {
	for _, key := range SDLCSurfacingKeys {
		_ = v.BindEnv(key)
	}
}

// SDLCKeys are the SDLC module's own configuration keys, bound to the
// environment by BindSDLCEnv.
//
// `sdlc.enabled` IS THE ONE THAT MATTERS MOST. The module is opt-in, so an
// unbound `enabled` means an environment-only deployment can set every other
// SDLC variable, have each of them decoded, and still run with the module off —
// the sweep this section paces would never start. That is a worse failure than a
// dropped knob, because every other variable appears to work.
//
// THE SLICES ARE DELIBERATELY ABSENT. `verification_commands` and `repo_hosts`
// are list configuration whose environment representation viper does not decode
// into a []string reliably through Unmarshal, so binding them would advertise a
// reachability that does not hold. They are file-only, said here rather than
// discovered, and the drift gate skips them by type for the same reason.
var SDLCKeys = []string{
	"sdlc.enabled",
	"sdlc.issue_page_limit",
	"sdlc.checkout_root",
	"sdlc.triage_agent.agent_id",
	"sdlc.triage_agent.pool_id",
	"sdlc.allow_local_repos",
	"sdlc.merge_attempts",
	"sdlc.triage_sweep_interval",
	"sdlc.review_sweep_interval",
	"sdlc.unblock_sweep_interval",
	"sdlc.train_sweep_interval",
	"sdlc.webhook.max_body",
	"sdlc.webhook.public_url",
	"sdlc.forge.read_timeout",
	"sdlc.forge.write_timeout",
	"sdlc.forge.heavy_write_timeout",
	"sdlc.forge.read_attempts",
	"sdlc.forge.retry_backoff",
}

// BindSDLCEnv binds the module's own keys to their environment variables
// (QUICBAAS_SDLC_*).
func BindSDLCEnv(v *viper.Viper) {
	for _, key := range SDLCKeys {
		_ = v.BindEnv(key)
	}
}

// SDLCLabelReconcileKeys are the configuration keys the label reconciliation
// sweep lives under, bound to the environment by BindSDLCLabelReconcileEnv.
//
// EXPLICIT, BECAUSE AutomaticEnv IS NOT ENOUGH. Viper's Unmarshal decodes the
// keys it knows about, and a key that appears in no config file is not one of
// them however the environment is set -- so a knob left unbound is read from
// the file and never from the environment. That is the shape of a setting an
// operator sets and never gets, and it would bite hardest in exactly the forge
// outage this sweep's pacing exists for.
var SDLCLabelReconcileKeys = []string{
	"sdlc.label_reconcile.interval",
	"sdlc.label_reconcile.forge_interval",
}

// BindSDLCLabelReconcileEnv binds the label reconciliation keys to their
// environment variables (QUICBAAS_SDLC_LABEL_RECONCILE_*).
func BindSDLCLabelReconcileEnv(v *viper.Viper) {
	for _, key := range SDLCLabelReconcileKeys {
		_ = v.BindEnv(key)
	}
}

// SDLCWorkerImageKeys are the worker image refresh's configuration keys,
// bound to the environment by BindSDLCWorkerImageEnv for the reason
// SDLCLabelReconcileKeys gives: a key no file names is otherwise read from
// the file and never from the environment.
var SDLCWorkerImageKeys = []string{
	"sdlc.worker_image.enabled",
	"sdlc.worker_image.repository",
	"sdlc.worker_image.registry",
	"sdlc.worker_image.registry_username",
	"sdlc.worker_image.registry_token_secret",
	"sdlc.worker_image.registry_token_tenant",
	"sdlc.worker_image.npm_registry_url",
	"sdlc.worker_image.refresh_interval",
	"sdlc.worker_image.pass_timeout",
	"sdlc.worker_image.smoke_model",
	"sdlc.worker_image.smoke_host",
}

// BindSDLCWorkerImageEnv binds the worker image refresh keys to their
// environment variables (QUICBAAS_SDLC_WORKER_IMAGE_*).
func BindSDLCWorkerImageEnv(v *viper.Viper) {
	for _, key := range SDLCWorkerImageKeys {
		_ = v.BindEnv(key)
	}
}

// RemovedKeys are the configuration keys nothing reads any more, each mapped
// to where the setting went. A configuration that sets one is refused at load
// (RefuseRemovedKeys) — pre-v1, no migration: the key is renamed by hand.
var RemovedKeys = map[string]string{
	"sdlc.triage_backend":                    agentReplacesTuple("sdlc.triage_agent"),
	"sdlc.triage_model":                      agentReplacesTuple("sdlc.triage_agent"),
	"orchestration.lead_agent.backend":       agentReplacesTuple("orchestration.lead_agent"),
	"orchestration.lead_agent.model":         agentReplacesTuple("orchestration.lead_agent"),
	"orchestration.lead_agent.system_prompt": "the lead Agent's prompt (orchestration.lead_agent names the Agent)",
	"sdlc.attempt_budget":                    "the engine's per-task budget (Task.AttemptBudget; default orchestration.DefaultAttemptBudget)",
	"sdlc.reap_lease_ttl":                    "orchestration.reap_lease_ttl",
	"sdlc.stage_timeouts":                    "orchestration.stage_timeouts",
	"sdlc.idle_thresholds":                   "orchestration.idle_thresholds",
	"sdlc.dispatch_cap":                      "the project's quotas.max_concurrency (live job leases per project)",
	"sdlc.run_timeout":                       "orchestration.idle_thresholds",
	"sdlc.sweep_interval":                    "orchestration.sweep_interval",
	"sdlc.mirror_lease_ttl":                  "nothing; adoption is decided by the task store's guarded source index",
	"sdlc.queue_capacity":                    "nothing; a verified webhook delivery nudges the task-source poller",
	"sdlc.delivery_memory":                   "nothing; re-polling is idempotent by construction",
	"sdlc.max_webhook_body":                  "sdlc.webhook.max_body",
	"sdlc.stage_event_flush_timeout":         "nothing; job events are the engine's own stream",
	"sdlc.agent_config_dir":                  "orchestration.agent_config_dir",
	"sdlc.companions":                        workerClonesItsOwnWorkspace,
	"sdlc.worker_image.registry_token_file": "sdlc.worker_image.registry_token_secret and registry_token_tenant: " +
		"the push token is a QuicBaaS tenant secret, never a plaintext file",
	"sdlc.worktree_root": workerClonesItsOwnWorkspace,
}

// agentReplacesTuple is where a backend/model tuple went: a reference to a
// tenant Agent or pool, which carries the backend, the model and the image.
func agentReplacesTuple(key string) string {
	return key + ".agent_id or " + key + ".pool_id: a tenant Agent or pool, which carries the backend and the model"
}

// workerClonesItsOwnWorkspace is where the daemon-side workspace settings
// went: nowhere. The worker's container clones the project repository and
// the project's context_repos itself at start, side by side, so there is no
// daemon-side worktree root and no companion copy to configure.
const workerClonesItsOwnWorkspace = "nothing; the worker's container clones the project and the project's context_repos itself, so there is no daemon-side worktree or companion copy"

// ErrRemovedKey indicates a configuration sets a key nothing reads any more.
var ErrRemovedKey = errors.New("config: key no longer exists")

// RemovedKeyError names the removed key and where its setting went.
type RemovedKeyError struct {
	Key         string
	Replacement string
}

// Error implements the error interface.
func (e *RemovedKeyError) Error() string {
	return ErrRemovedKey.Error() + ": " + e.Key + " is no longer read; it moved to " + e.Replacement
}

// Unwrap returns the sentinel for errors.Is support.
func (e *RemovedKeyError) Unwrap() error { return ErrRemovedKey }

// RefuseRemovedKeys returns a typed error for the first removed key the
// loaded configuration (file or environment) sets, in key order so the
// refusal is deterministic.
func RefuseRemovedKeys(v *viper.Viper) error {
	keys := make([]string, 0, len(RemovedKeys))
	for key := range RemovedKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if v.IsSet(key) {
			return &RemovedKeyError{Key: key, Replacement: RemovedKeys[key]}
		}
	}
	return nil
}

// SessionHistoryConfig bounds the per-session conversation transcript the
// orchestrator keeps.
//
// A transcript is append-only and a long-lived session has no other ceiling,
// so without a bound one runaway agent loop grows a keyspace no operator ever
// reads and no process ever reclaims. Retention is expressed in TURNS — a
// prompt and everything the agent produced answering it — because that is the
// unit an operator reads, and because pruning by event count would cut a turn
// in half and show an answer with no question above it.
type SessionHistoryConfig struct {
	// MaxTurnsPerSession is the deployment-wide number of turns kept per
	// session. Zero selects orchestration.DefaultSessionRetentionTurns (200).
	MaxTurnsPerSession int `mapstructure:"max_turns_per_session"`

	// PerTenantMaxTurns overrides MaxTurnsPerSession for named tenants. A
	// value of zero or less for a tenant keeps its transcripts whole — the
	// explicit setting for a tenant under a legal hold.
	PerTenantMaxTurns map[string]int `mapstructure:"per_tenant_max_turns"`
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

	// AccountDeliveryTTL is how long ONE hand-over of a pooled provider
	// credential stays authorised without renewal.
	//
	// It bounds DELIVERY, not the credential: a subscription login's own
	// lifetime is its OAuth expiry, which the refresher on the control plane
	// owns, and this never shortens it. What it bounds is how long a host that
	// was handed a copy may keep refreshing that copy without the grant being
	// renewed — the backstop for a session whose conclusion never ran because
	// its node was destroyed rather than shut down.
	//
	// Zero selects orchestration.DefaultAccountDeliveryTTL (1h). The ordinary
	// bound is the session's conclusion, which revokes the grant outright.
	AccountDeliveryTTL time.Duration `mapstructure:"account_delivery_ttl"`

	// SessionHistory bounds how much of each session's conversation is kept.
	// See SessionHistoryConfig.
	SessionHistory SessionHistoryConfig `mapstructure:"session_history"`

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

	// SocketsRoot is the root the daemon binds its per-session agent sockets
	// under — today the ephemeral, KMS-backed SSH signing agent one sdlc
	// attempt's container signs against.
	//
	// IT IS SEPARATE FROM ProjectsRoot BECAUSE A UNIX SOCKET ADDRESS IS A
	// FIXED-SIZE KERNEL FIELD. sun_path holds 108 bytes, and a managed
	// workspace is <projects_root>/<tenant_id>/<project_id>/workspace — 88
	// bytes of tenant and project before a socket name is appended — so a
	// socket placed there exceeds the kernel's limit on every realistic
	// projects root. This root is short, dedicated and deployment-wide; a
	// session's own directory under it is created 0700 by the agent and
	// removed with it.
	//
	// When set it must be an absolute path. Empty derives
	// "$XDG_RUNTIME_DIR/quicbaas" when XDG_RUNTIME_DIR is set, and
	// "<storage.data_dir>/s" when it is not — see EffectiveSocketsRoot.
	SocketsRoot string `mapstructure:"sockets_root"`

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

	// ReapLeaseTTL is the heartbeat age at which a job's lease is stale: a
	// node that has not proved a job alive for this long has lost it, and
	// the reaper concludes the job on the next sweep. Running loops
	// heartbeat at a third of this, and the reaper and stall detector sweep
	// on the same cadence. Zero takes the default.
	// Default: 90s
	ReapLeaseTTL time.Duration `mapstructure:"reap_lease_ttl"`

	// IdleThresholds bounds how long a job may sit in each state before the
	// stall detector reports the wait, keyed by job state name (see
	// OrchestrationWatchedJobStates). A state past its bound in a
	// session-backed state with no addressable session is ended. Zero takes
	// the engine default for that state; a NEGATIVE value stops watching
	// the state. An unknown state name is refused.
	IdleThresholds map[string]time.Duration `mapstructure:"idle_thresholds"`

	// BackendConcurrency bounds how many jobs may run at once on each agent
	// backend, by backend name.
	//
	// FOR A MODEL SERVER SEVERAL PROJECTS SHARE. A project quota bounds one
	// project's work and the fleet quota bounds worker slots; neither can
	// express "this GPU can serve two things at once", which is the only
	// limit a locally hosted model has. It matters most for REVIEWS, which
	// are off the project budget by construction so that a pull request never
	// queues behind the work it unblocks — with the consequence that a
	// project's every green, unjudged pull request is reviewed in the same
	// pass. Measured on the burn-in fleet 2026-09-14: five such reviews
	// landed on one 27B model at once, the GPU pinned at 99%, a two-second
	// prompt took 115 seconds, and no review finished in 37 minutes.
	//
	// A backend that is absent, or mapped to zero or less, is unlimited — so
	// a deployment that configures nothing behaves exactly as it did before.
	//
	// Example: qwen: 2
	BackendConcurrency map[string]int `mapstructure:"backend_concurrency"`
	// SweepInterval paces the task-source poller: how often every registered
	// TaskSource is asked, per project with a DefaultWorkflow, whether it
	// has new work. Re-polling is idempotent (the task store's guarded
	// source index refuses a second adoption of one origin), so a shorter
	// interval costs only source round-trips.
	//
	// Default: 30s. Zero means unset and takes the default
	// (EffectiveSweepInterval); negative is refused.
	SweepInterval time.Duration `mapstructure:"sweep_interval"`

	// VerificationCommands is the operator allowlist of commands a
	// workflow's command review criteria may run on the daemon host. A
	// criterion's command is TENANT data run through the daemon's shell, so
	// only a command spelled out HERE — the system plane, never
	// tenant-writable — is accepted, matched exactly, and re-checked where
	// it would execute.
	//
	// Default: the standard verification shapes (make test / make build /
	// make lint / make security). An empty list configured explicitly
	// refuses every command.
	VerificationCommands []string `mapstructure:"verification_commands"`

	// AgentShellAllow is the shell command prefixes a Claude Code worker
	// that runs on the daemon host may run (for example "go", "gofmt",
	// "make", "git status"). The daemon renders them, with every
	// VerificationCommands entry as an exact rule, into the settings.json
	// of AgentConfigDir at boot; a host worker spawned in acceptEdits mode
	// has no shell otherwise. Container workers are unaffected: they run
	// with full control inside their sandbox.
	//
	// Default: empty, which allows only the verification commands. Ignored
	// when AgentConfigDir is unset.
	AgentShellAllow []string `mapstructure:"agent_shell_allow"`

	// AgentConfigDir is the configuration directory every Claude Code
	// session the engine spawns reads (CLAUDE_CONFIG_DIR). A directory the
	// engine owns is a directory with no operator credential in it — the
	// session's credential comes from the pooled account pool instead — so
	// a worker's settings, and therefore its behaviour, are the engine's.
	// When set it must be an absolute path. Empty leaves the backend's
	// scrubbed environment as it is.
	AgentConfigDir string `mapstructure:"agent_config_dir"`

	// StageTimeouts bounds each lifecycle moment of a job individually. A
	// job's context has no deadline of its own, so an unbounded hook that
	// hangs would hold the job forever with nothing naming which moment was
	// holding it. A zero field takes orchestration.DefaultStageTimeouts.
	StageTimeouts OrchestrationStageTimeouts `mapstructure:"stage_timeouts"`

	// Fleet bounds the whole deployment, across every tenant and project.
	// See OrchestrationFleetConfig.
	Fleet OrchestrationFleetConfig `mapstructure:"fleet"`

	// Pricing is the model price table every recorded turn is priced from.
	// See PricingConfig; an absent table leaves every turn unpriced.
	Pricing PricingConfig `mapstructure:"pricing"`
}

// OrchestrationFleetConfig is the deployment-wide bound on orchestrated work.
//
// IT IS THE INPUT PER-PROJECT QUOTAS CANNOT SUPPLY. Project.Quotas.MaxConcurrency
// bounds one project; ten projects each permitted three concurrent jobs will run
// thirty of them on a fleet with eight worker slots, and nothing below this
// setting knows the fleet has eight. See pkg/orchestration/admission.go.
type OrchestrationFleetConfig struct {
	// MaxConcurrentJobs is how many orchestration jobs the whole deployment
	// may run at once, counted against live job leases across every tenant.
	//
	// ZERO LEAVES THE FLEET AXIS UNDECLARED, and that is the shipped default
	// rather than a guess. This tree cannot measure how many worker slots a
	// deployment has — the number is a property of the CI hosts, the container
	// runtime and the operator's intent — and a default that guessed low would
	// refuse work the fleet would have served while looking like a safety
	// bound. An operator who has counted their slots sets it here; see
	// docs/software-factory/admission-and-readiness.md.
	//
	// Free slots are divided among the projects asking for them by max-min
	// fairness, so raising this number never reduces any project's share and
	// lowering it never starves one.
	MaxConcurrentJobs int `mapstructure:"max_concurrent_jobs"`
}

// DefaultOrchestrationReapLeaseTTL is the lease TTL applied when
// reap_lease_ttl is unset.
const DefaultOrchestrationReapLeaseTTL = 90 * time.Second

// OrchestrationWatchedJobStates lists the job state names an idle threshold
// may be set on, for the early refusal in ValidateOrchestration.
//
// orchestration.WatchedJobStates IS THE AUTHORITY and derives this set from
// the registered runners' JobStateSpecs (#621). This restatement survives
// because pkg/config cannot read it: pkg/orchestration imports pkg/interfaces
// which imports pkg/config, so the reverse edge is a production import cycle,
// not merely a test one. Deriving here needs a seam that does not exist yet —
// either the state vocabulary moves to a leaf package both import, or this
// check moves to orchestration.JobIdleThresholdsFrom, which already refuses
// an unknown name at wiring time.
//
// Until then the two are kept in step by
// TestJobIdleThresholdsFrom_RefusesAnUnknownState in pkg/orchestration, which
// compares this list against the derived one and fails the build on drift.
var OrchestrationWatchedJobStates = []string{
	"assigning", "cleanup", "completing", "decomposing", "escalating", "executing",
	"initializing", "re-delegating", "reviewing", "scheduling", "validating",
}

// EffectiveReapLeaseTTL returns ReapLeaseTTL, or the default when unset.
func (c OrchestrationConfig) EffectiveReapLeaseTTL() time.Duration {
	if c.ReapLeaseTTL > 0 {
		return c.ReapLeaseTTL
	}
	return DefaultOrchestrationReapLeaseTTL
}

// DefaultOrchestrationSweepInterval is the task-source poller cadence an
// unset orchestration.sweep_interval takes.
const DefaultOrchestrationSweepInterval = 30 * time.Second

// EffectiveSweepInterval returns the configured sweep interval, or the
// default when it is unset.
func (c *OrchestrationConfig) EffectiveSweepInterval() time.Duration {
	if c.SweepInterval <= 0 {
		return DefaultOrchestrationSweepInterval
	}
	return c.SweepInterval
}

// OrchestrationStageTimeouts is the operator's per-moment bound on a job's
// lifecycle hooks (see orchestration.JobLifecycleHooks).
//
// It mirrors orchestration.StageTimeouts field for field rather than reusing
// that type, for the reason every other config struct in this file is its own
// type: the configuration surface is a contract with the operator's YAML, and
// binding it to an engine's internal struct would make a rename in the engine
// a silent break in every deployed configuration file.
//
// A zero field means UNSET and takes the engine's defaults layer. Zero is not
// read as "no bound": a bound of zero expires before its moment begins.
type OrchestrationStageTimeouts struct {
	// Prepare bounds the moment before any session spawns.
	Prepare time.Duration `mapstructure:"prepare"`

	// Reviewable bounds the moment the work is published for judgement.
	Reviewable time.Duration `mapstructure:"reviewable"`

	// Complete bounds the moment the accepted work is finalized.
	Complete time.Duration `mapstructure:"complete"`

	// Cleanup bounds the discard moment, which runs on a context detached
	// from the job's own however the job ended.
	Cleanup time.Duration `mapstructure:"cleanup"`

	// Blocked bounds the moment the engine parks the task for a person.
	Blocked time.Duration `mapstructure:"blocked"`
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

	// GoCacheDir is the host directory holding the Go module and build
	// caches every sandbox container this daemon launches shares, one pair
	// per tenant: <go_cache_dir>/<tenant_id>/{mod,build}. Empty takes
	// <data_dir>/cache/go (EffectiveGoCacheDir). It must be absolute and lie
	// outside the projects root and every allowed mount root, so no spawn can
	// name a cache as a bind mount of its own; the daemon refuses to start
	// otherwise.
	GoCacheDir string `mapstructure:"go_cache_dir"`
}

// EffectiveGoCacheDir returns GoCacheDir when set, defaulting to
// "<dataDir>/cache/go". Empty when neither is known.
func (c OrchestrationSandboxConfig) EffectiveGoCacheDir(dataDir string) string {
	if c.GoCacheDir != "" {
		return c.GoCacheDir
	}
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "cache", "go")
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

// SocketsRootRuntimeDirName is the directory the derived sockets root takes
// inside $XDG_RUNTIME_DIR. The runtime directory is per-user, tmpfs-backed
// and 0700 already, which is exactly what an agent socket's parent must be.
const SocketsRootRuntimeDirName = "quicbaas"

// SocketsRootDataDirName is the directory the derived sockets root takes
// under the data directory when there is no runtime directory. It is ONE
// character because every byte of it is spent out of the socket's 107-byte
// budget.
const SocketsRootDataDirName = "s"

// SocketsRootInstanceLen is the width of the per-daemon segment inside the
// runtime directory: 12 lowercase hexadecimal characters.
//
// It is fixed so the worst-case socket path under a derived root is a
// computation rather than a guess — the same reason the session directory a
// signing agent mints is fixed-width — and it is truncated from a SHA-256
// digest rather than taken whole because all 64 characters would spend the
// socket budget the derivation exists to protect. 48 bits is collision
// resistance against the handful of daemons one user runs, not against an
// adversary: the segment names a directory in the user's own private,
// 0700 runtime directory and authorises nothing.
const SocketsRootInstanceLen = 12

// SocketsRootInstanceSegment is the per-daemon directory name inside
// $XDG_RUNTIME_DIR/quicbaas, derived from the absolute data directory.
//
// THE DATA DIRECTORY IS WHAT DISTINGUISHES TWO DAEMONS OF ONE USER. There is
// no node identity to borrow — raft.node_id defaults to 1 for every
// standalone daemon — and the data directory is the one setting two instances
// on one host cannot share. Hashing rather than embedding it keeps the
// segment fixed-width and free of separators; the digest is truncated, never
// reversed, and the path it names is recomputed from the configuration on
// every start rather than recorded anywhere.
func SocketsRootInstanceSegment(dataDir string) string {
	sum := sha256.Sum256([]byte(dataDir))
	return hex.EncodeToString(sum[:])[:SocketsRootInstanceLen]
}

// EffectiveSocketsRoot returns SocketsRoot when set, deriving it otherwise:
// "$XDG_RUNTIME_DIR/quicbaas/<instance>" when the runtime directory is named
// in the environment, and "<dataDir>/s" when it is not. dataDir is the
// top-level storage data directory (Storage.DataDir).
//
// The runtime directory is preferred because it is short, per-user and
// already private, and because sockets are runtime state that must not
// survive a reboot inside a data directory.
//
// IT IS PER-DAEMON, NOT PER-USER. $XDG_RUNTIME_DIR is a per-user directory,
// so a root derived from it alone is reached by every quicbaasd the user
// runs: two daemons with different data directories would bind their session
// sockets under one root, and the startup sweep that removes the sessions a
// killed daemon left would be walking a sibling's live ones
// (go-quicbaas#687). The instance segment makes the roots disjoint, so each
// daemon sweeps and binds only under its own. The data-directory derivation
// needs no such segment: it is already inside the directory that
// distinguishes the daemons, and adding one would spend budget for nothing.
//
// The identity is the ABSOLUTE data directory, so two daemons started from
// different working directories with the same relative data_dir are still
// distinguished. Resolving it is the only failure this has: a working
// directory that cannot be read leaves the daemon with no identity to derive
// from, and that is reported rather than collapsed onto a shared root.
//
// An empty data directory answers the empty root it always has — a daemon
// that stores nothing on disk has nothing to be identified by, and the
// caller's own refusal (sdlc.EnsureSigningSocketRoot) names the setting.
func (c OrchestrationConfig) EffectiveSocketsRoot(dataDir string) (string, error) {
	if c.SocketsRoot != "" {
		return c.SocketsRoot, nil
	}
	if dataDir == "" {
		return "", nil
	}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); filepath.IsAbs(runtimeDir) {
		absolute, err := filepath.Abs(dataDir)
		if err != nil {
			return "", &SocketsRootIdentityError{DataDir: dataDir, Cause: err}
		}
		return filepath.Join(runtimeDir, SocketsRootRuntimeDirName,
			SocketsRootInstanceSegment(filepath.Clean(absolute))), nil
	}
	return filepath.Join(dataDir, SocketsRootDataDirName), nil
}

// SocketsRootIdentityError reports that the daemon's own identity could not
// be resolved, so no per-daemon sockets root could be derived for it.
type SocketsRootIdentityError struct {
	// DataDir is the configured storage.data_dir that could not be resolved.
	DataDir string

	// Cause is the resolution failure, which is a working directory that
	// could not be read.
	Cause error
}

func (e *SocketsRootIdentityError) Error() string {
	return "config: orchestration.sockets_root: storage.data_dir " + strconv.Quote(e.DataDir) +
		" could not be resolved to an absolute path, so no per-daemon sockets root could be derived: " +
		e.Cause.Error()
}

func (e *SocketsRootIdentityError) Unwrap() error { return e.Cause }

// SchedulerHostAffinityConfig controls operator policy for the host_affinity
// hint applied to orchestrator and scheduler requests.
type SchedulerHostAffinityConfig struct {
	// Mode selects the enforcement mode when a request supplies host_affinity
	// but the named host is offline:
	//   "soft" (default): warn and fall back to any available agent.
	//   "hard": return a NoMatchingFleetNodeError so callers know the request was
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

	// PrimaryBackend names the backend that receives objects stored without
	// an explicit target backend. It must be one of Backends. When unset and
	// exactly one backend is configured, that backend is primary; with
	// several backends it is required. See ResolvePrimaryBackend.
	PrimaryBackend string `mapstructure:"primary_backend"`
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

	// RateLimit is the global per-source-IP request limit, in requests per
	// second (the burst is the same size). Zero, which is also what an
	// omitted key decodes to, means the REST default of 1000. -1 disables
	// the global limiter; the endpoint-scoped OAuth2 limiter stays on.
	RateLimit int `mapstructure:"rate_limit"`
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

	// TrustedProxies lists CIDRs (or bare addresses) whose X-Forwarded-For and
	// X-Real-IP headers are believed when attributing a request to a client.
	//
	// EMPTY MEANS TRUST NOTHING, and that is the default. Without it, any
	// caller reaching the listener directly chooses its own rate-limit bucket
	// and its own audit-record IP by setting a header (go-quicbaas#156). A
	// deployment behind a load balancer names the balancer here; nothing is
	// inferred from the request, because everything in the request is
	// attacker-controlled.
	//
	// Example: ["10.0.0.0/8", "192.0.2.7"]
	TrustedProxies []string `mapstructure:"trusted_proxies"`

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

	// EmbeddedFleetNode configures the optional co-located agent runtime.
	// See EmbeddedFleetNodeConfig for field semantics.
	EmbeddedFleetNode EmbeddedFleetNodeConfig `mapstructure:"embedded_fleet_node"`

	// Protocol-specific configurations. Network serving runs exclusively on
	// the embedded qrdb node's unified multi-protocol server (see
	// (*Config).NetworkServing and pkg/server/qrdb_node.go's
	// unifiedModuleServerOptions/buildQRDBConfig): when REST.Enabled,
	// GRPC.Enabled, or MCP.Enabled is true, the node's own listener for that
	// protocol comes up on the corresponding Port and serves the broker
	// ServiceModule (pkg/apimodule), gated per-RPC by qrdb's reflection
	// interceptor over option (qrdb.v1.permission). There is no separate
	// legacy listener — these ARE the unified server's protocol gates.
	GRPC ServerGRPCConfig `mapstructure:"grpc"`
	REST ServerRESTConfig `mapstructure:"rest"`
	MCP  ServerMCPConfig  `mapstructure:"mcp"`
	// Unix serves the gRPC surface over a local Unix domain socket for CLI
	// compatibility. This is local IPC, not network serving: it is served by
	// quicbaas itself (a live, fully-registered *grpc.Server), independent of
	// whether REST/GRPC/MCP network serving is enabled — mirroring go-xkms's
	// local-IPC split (qrdb's unified server transports are TLS-only network
	// listeners; they carry no Unix-domain-socket transport of their own).
	Unix ServerUnixConfig `mapstructure:"unix"`

	// ACME configures the Security().ACME() RFC 8555 server's challenge
	// validation.
	ACME ServerACMEConfig `mapstructure:"acme"`

	// SecureMemoryBudgetMiB caps, in MiB, the locked memory the daemon's
	// protected buffers and opened secrets may hold at once. It is installed
	// as the process-wide allocator at startup and must fit within the
	// host's RLIMIT_MEMLOCK, or the daemon refuses to start. Zero (the
	// default) derives it from the host: 64 MiB, or RLIMIT_MEMLOCK less
	// 2 MiB of headroom when that is smaller. Negative is invalid.
	SecureMemoryBudgetMiB int64 `mapstructure:"secure_memory_budget_mib"`

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

// ServerACMEConfig configures the ACME server behind Security().ACME().
type ServerACMEConfig struct {
	// AllowPrivateChallengeTargets lets challenge validation (http-01,
	// enroll-01) connect to reserved addresses: loopback, private,
	// link-local, metadata and the other special-purpose ranges. go-xkms
	// validates every challenge through one SSRF-safe client that refuses
	// those addresses by default (go-xkms acme.challenges.allow_private_targets);
	// a deployment whose devices enroll from a private network sets this.
	// Production on a public network leaves it false.
	AllowPrivateChallengeTargets bool `mapstructure:"allow_private_challenge_targets"`
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
	DeploymentID       uint64        `mapstructure:"deployment_id"`
	ElectionRTT        uint64        `mapstructure:"election_rtt"`
	HeartbeatRTT       uint64        `mapstructure:"heartbeat_rtt"`
	SnapshotEntries    uint64        `mapstructure:"snapshot_entries"`
	CompactionOverhead uint64        `mapstructure:"compaction_overhead"`
	RequestTimeout     time.Duration `mapstructure:"request_timeout"`
	RTTMillisecond     uint64        `mapstructure:"rtt_millisecond"`
	RaftAddress        string        `mapstructure:"raft_address"`
	JoinAddresses      []string      `mapstructure:"join_addresses"`
	// MemberIdentities lists the Raft member identities of the other replicas
	// as "replicaID=spki:<sha256-hex>" entries (`qrdbd node-identity --cert`
	// prints the value for a certificate). go-qrdb requires it on every
	// multi-node replica: a member is admitted by its identity or not at all.
	// The local replica's identity is derived from its own certificate.
	MemberIdentities []string `mapstructure:"member_identities"`
	// ListenAddress is the host:port the Raft transport binds. ListenAddr
	// resolves the bind address in this order:
	//
	//  1. ListenAddress, used as given (the NAT opt-in: it may differ from
	//     RaftAddress);
	//  2. ListenHost joined with the port of RaftAddress (the opt-in for
	//     one interface, or 0.0.0.0 / :: for every interface);
	//  3. RaftAddress itself: by default a node listens on the host it
	//     advertises. An empty or wildcard advertised host is then a
	//     validation error, so no default ever binds every interface.
	ListenAddress string `mapstructure:"listen_address"`
	// ListenHost is the interface the Raft transport binds when ListenAddress
	// is empty; empty (the default) means the host of RaftAddress.
	ListenHost          string `mapstructure:"listen_host"`
	MutualTLS           bool   `mapstructure:"mutual_tls"`
	MaxInMemLogSize     uint64 `mapstructure:"max_in_mem_log_size"`
	MaxSendQueueSize    uint64 `mapstructure:"max_send_queue_size"`
	MaxReceiveQueueSize uint64 `mapstructure:"max_receive_queue_size"`
	MaxSnapshotSendBPS  uint64 `mapstructure:"max_snapshot_send_bps"`
	MaxSnapshotRecvBPS  uint64 `mapstructure:"max_snapshot_recv_bps"`
	NotifyCommit        bool   `mapstructure:"notify_commit"`
	EnableMetrics       bool   `mapstructure:"enable_metrics"`

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

// NetworkServing reports whether any network protocol is enabled. Network
// serving runs exclusively on the embedded qrdb node's unified multi-protocol
// server: the node's own REST/gRPC/MCP listeners take Server.REST.Port /
// Server.GRPC.Port / Server.MCP.Port and serve the QuicBaaS capability plane
// through the registered ServiceModule (pkg/apimodule), gated per-RPC by
// qrdb's reflection interceptor over option (qrdb.v1.permission). It
// therefore requires explicit TLS material (qrdb's transports are TLS-only) —
// validateNetworkServingTLS enforces this. The Unix domain socket listener
// (Server.Unix) is local IPC, served by quicbaas itself, and is deliberately
// excluded here — mirroring go-xkms's (*Config).NetworkServing.
func (c *Config) NetworkServing() bool {
	return c.Server.REST.Enabled || c.Server.GRPC.Enabled || c.Server.MCP.Enabled
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
