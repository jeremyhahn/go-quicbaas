// Package config provides configuration structures and management for QuicBaaS.
// It supports loading configuration from files, environment variables, and CLI flags
// with proper validation and defaults.
package config

import (
	"strconv"
	"time"
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

// TenantIsolationMode constants define the supported tenant isolation strategies.
const (
	// TenantIsolationKeyspace uses key prefixes (__t:{tenantID}:) to scope all
	// data to a tenant. This is the default mode and is backward compatible.
	TenantIsolationKeyspace = "keyspace"

	// TenantIsolationBarrier uses go-qrdb's per-tenant cryptographic barrier to
	// provide full encryption isolation between tenants. Opt-in only.
	TenantIsolationBarrier = "barrier"
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
		if m != TenantIsolationKeyspace && m != TenantIsolationBarrier {
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
	if _, ok := modeSet[TenantIsolationBarrier]; ok {
		pool := c.TenantBarrierVolumePool
		if pool == "" {
			return &ConfigValidationError{
				Field:   "iam.self_service_registration.tenant_barrier_volume_pool",
				Message: "must be non-empty when barrier isolation mode is allowed",
			}
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

	// JWTSecret is the secret key for signing JWT tokens with HS256.
	// REQUIRED when Enabled is true and JWTSigningKeyID is not set.
	// Should be a secure random string of at least 32 characters.
	// Deprecated: Use JWTSigningKeyID with asymmetric signing for production.
	JWTSecret string `mapstructure:"jwt_secret"`

	// JWTSigningKeyID is the KMS key identifier for asymmetric JWT signing.
	// When set, tokens are signed using the specified KMS key (e.g., ES256)
	// instead of the shared JWTSecret (HS256). For production deployments,
	// this is strongly recommended over symmetric HS256 signing.
	// Example: "quicbaas-jwt-signing"
	JWTSigningKeyID string `mapstructure:"jwt_signing_key_id"`

	// JWTSigningAlgorithm specifies the asymmetric signing algorithm to use
	// when JWTSigningKeyID is configured. Supported values:
	//   "ES256" (default), "ES384", "ES512" (ECDSA)
	//   "RS256", "RS384", "RS512" (RSA)
	// Ignored when JWTSigningKeyID is empty (HS256 fallback).
	JWTSigningAlgorithm string `mapstructure:"jwt_signing_algorithm"`

	// TokenTTL is the duration that access tokens are valid.
	// Default: 1h
	TokenTTL time.Duration `mapstructure:"token_ttl"`

	// RefreshTokenTTL is the duration that refresh tokens are valid.
	// Default: 7d (168h)
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`

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

	// HostAffinity configures how strictly host_affinity hints on
	// SpawnSession / Inference requests are enforced when the named host is
	// offline. See SchedulerHostAffinityConfig.
	HostAffinity SchedulerHostAffinityConfig `mapstructure:"host_affinity"`
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
}

// ServerRESTConfig contains REST/HTTP server configuration.
type ServerRESTConfig struct {
	Enabled bool `mapstructure:"enabled"`
	Port    int  `mapstructure:"port"`
}

// ServerQUICConfig contains QUIC server configuration.
type ServerQUICConfig struct {
	Enabled                bool          `mapstructure:"enabled"`
	Port                   int           `mapstructure:"port"`
	MaxStreams             int           `mapstructure:"max_streams"`
	MaxIdleTimeout         time.Duration `mapstructure:"max_idle_timeout"`
	HandshakeIdleTimeout   time.Duration `mapstructure:"handshake_idle_timeout"`
	MaxStreamReceiveWindow uint64        `mapstructure:"max_stream_receive_window"`
}

// ServerMCPConfig contains MCP (Model Context Protocol) server configuration.
type ServerMCPConfig struct {
	Enabled             bool                         `mapstructure:"enabled"`
	Port                int                          `mapstructure:"port"`
	Authorization       MCPAuthorizationConfig       `mapstructure:"authorization"`
	DynamicRegistration MCPDynamicRegistrationConfig `mapstructure:"dynamic_registration"`
}

// ServerUnixConfig contains Unix domain socket server configuration.
type ServerUnixConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Socket  string `mapstructure:"socket"`
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
	NodeID              uint64        `mapstructure:"node_id"`
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

	// Raft-specific TLS certificate paths for inter-node mTLS.
	// When set, these override the server Security TLS paths for Raft transport.
	// This allows Raft mTLS to use a shared CA and per-node certs that are
	// independent of the server API TLS certificates.
	TLSCertFile string `mapstructure:"tls_cert_file"`
	TLSKeyFile  string `mapstructure:"tls_key_file"`
	TLSCAFile   string `mapstructure:"tls_ca_file"`
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
	// TLSEnabled indicates TLS is enabled. Always true in production; retained for config compatibility.
	TLSEnabled          bool   `mapstructure:"tls_enabled"`
	TLSCertFile         string `mapstructure:"tls_cert_file"`
	TLSKeyFile          string `mapstructure:"tls_key_file"`
	TLSCAFile           string `mapstructure:"tls_ca_file"`
	TLSClientAuth       bool   `mapstructure:"tls_client_auth"`
	FIPSMode            bool   `mapstructure:"fips_mode"`
	EncryptionEnabled   bool   `mapstructure:"encryption_enabled"`
	EncryptionAlgorithm string `mapstructure:"encryption_algorithm"`

	// SealEnabled enables seal/unseal functionality for protecting
	// encryption keys at rest. When enabled, the server starts in a
	// sealed state and requires manual unsealing before operations.
	SealEnabled bool `mapstructure:"seal_enabled"`

	// KeyStorageBackend references a named backend from ObjectStorage.Backends
	// for storing encryption keys. If empty, defaults to in-memory storage.
	KeyStorageBackend string `mapstructure:"key_storage_backend"`

	// EncryptionKeyFile is DEPRECATED but maintained for backward compatibility.
	// If KeyStorageBackend is empty and EncryptionKeyFile is set,
	// a local file backend will be created automatically.
	EncryptionKeyFile string `mapstructure:"encryption_key_file"`

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
	// - rbac: Role-based access control using RoleManager
	// - abac: Attribute-based access control using PolicyEngine (requires iam build tag)
	// - hybrid: ABAC-first with RBAC fallback for gradual migration
	Provider string `mapstructure:"provider"`

	// RBAC contains role-based access control configuration.
	RBAC RBACAuthzConfig `mapstructure:"rbac"`

	// ABAC contains attribute-based access control configuration.
	// Only used when Provider is "abac" or "hybrid" and iam build tag is set.
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
	// Requires iam build tag.
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
	// Options: "software", "tpm2", "pkcs11", "awskms", "gcpkms", "azurekv"
	// Default: ["software"]
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
