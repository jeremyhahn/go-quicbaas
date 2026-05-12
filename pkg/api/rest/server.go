//go:build rest

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

package rest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest/ui"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/routing"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// Config holds REST API server configuration.
type Config struct {
	// Address is the HTTP server bind address
	Address string

	// ReadTimeout is the maximum duration for reading the entire request
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out writes of the response
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the next request
	IdleTimeout time.Duration

	// MaxHeaderBytes is the maximum size of request headers
	MaxHeaderBytes int

	// MaxBodySize is the maximum request body size in bytes for standard API
	// endpoints. Requests exceeding this limit receive a 413 Payload Too Large
	// response. Set to 0 to disable body limiting (not recommended).
	// Default: 10 MB (DefaultMaxBodySize).
	MaxBodySize int64

	// MaxUploadBodySize is the maximum request body size in bytes for
	// upload-heavy endpoints (object storage, backups, object transfer).
	// Set to 0 to disable body limiting for uploads (not recommended).
	// Default: 100 MB (DefaultMaxUploadBodySize).
	MaxUploadBodySize int64

	// MaxConnections is the maximum number of concurrent connections.
	// When greater than zero, the listener rejects new connections once
	// the limit is reached (callers block in Accept until a slot opens).
	// Zero means unlimited connections.
	MaxConnections int

	// CORSOrigins is a list of allowed CORS origins
	CORSOrigins []string

	// RequestTimeout is the maximum duration for request processing
	RequestTimeout time.Duration

	// RateLimit is the maximum requests per second (0 = disabled)
	RateLimit int

	// ExposeShardHeader controls X-QuicBaaS-Shard response header tagging.
	ExposeShardHeader bool

	// NodeID is the Raft node ID of this daemon instance. Used as the
	// authoritative node_id in cluster status responses when the Raft
	// topology is unavailable (e.g., standalone mode or first boot).
	NodeID uint64

	// TLS configuration
	TLSEnabled    bool   // Enable TLS/HTTPS
	TLSCertFile   string // Path to TLS certificate file
	TLSKeyFile    string // Path to TLS private key file
	TLSCAFile     string // Path to CA certificate for client auth
	TLSClientAuth bool   // Require client certificate authentication (mTLS)

	// TLSConfig is a pre-built *tls.Config (e.g. backed by OpaqueKeys).
	// When set, the server uses this config directly instead of loading
	// certificates from TLSCertFile/TLSKeyFile.
	TLSConfig *tls.Config

	// Authorization configuration (optional)
	// Both must be provided to enable authorization middleware
	TokenValidator TokenValidator                  // JWT token validator
	AuthzService   interfaces.AuthorizationService // Authorization service

	// PIVClientCert controls PIV certificate-based activation (default: disabled).
	PIVClientCert PIVClientCertConfig

	// IAMSelfServiceRegistration is the per-server self-service tenant
	// registration policy. When zero-valued the registration surface is
	// disabled (the safe default). Embedded callers that want to enable
	// self-service registration populate this field at server-construction
	// time from their own config struct.
	IAMSelfServiceRegistration config.IAMSelfServiceRegistrationConfig
}

// PIVClientCertConfig holds PIV TLS client certificate configuration.
type PIVClientCertConfig struct {
	// Enabled controls whether the PIV activate endpoint accepts client certs
	// from the TLS handshake. When false, the PEM body parameter is used instead.
	// Default: false (cert provided in POST body as PEM).
	Enabled bool

	// TrustedRootsFile is the path to a PEM-encoded file containing trusted CA
	// certificates for PIV cert chain verification. When empty, the server-level
	// TLSCAFile is used as a fallback. Required when Enabled is true.
	TrustedRootsFile string

	// TrustedIssuers is an optional list of issuer Subject DN strings that the
	// certificate's issuing CA must match (case-insensitive per RFC 4514). When
	// empty, any cert that validates against the CA pool is accepted.
	TrustedIssuers []string
}

// DefaultConfig returns default REST API configuration.
func DefaultConfig() *Config {
	return &Config{
		Address:           ":8080",
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
		MaxBodySize:       DefaultMaxBodySize,
		MaxUploadBodySize: DefaultMaxUploadBodySize,
		RequestTimeout:    30 * time.Second,
		RateLimit:         0, // No rate limiting by default
		ExposeShardHeader: true,
	}
}

// Server represents the REST API server.
type Server struct {
	config  *Config
	router  *chi.Mux
	server  *http.Server
	service service.QuicBaaS
	logger  *logger.Logger
	// shardRouter selects a raft shard for (entityType, id)
	shardRouter interface{ Route(string, uint64) uint64 }

	// listener is the TCP listener backing the HTTP server.
	// It is created in Start and may be wrapped by a limitListener.
	listener net.Listener

	// Authorization middleware (optional)
	authzMiddleware *AuthzMiddleware

	// hookTaskStore is the task store used by Claude Code hook handlers (optional)
	hookTaskStore orchestration.TaskStore

	// agentRegistrar is the fleet-registry registrar used by the mTLS
	// register handler (handlers_agent_register.go) to publish newly
	// registered daemons. Stored as interface{} to avoid pulling iam
	// into the core rest package — the register handler, compiled under
	// the iam build tag, performs the type assertion itself.
	agentRegistrar interface{}

	// eventIngestURL is the gRPC URL the agent register handler hands
	// back to a daemon on enrollment so the daemon dials the gRPC
	// listener (h2 ALPN) for PushEvents instead of the REST listener
	// (http/1.1 ALPN). Format is a full URL: "https://host:port".
	// Empty string means the server has no gRPC listener exposed; the
	// agent then skips event forwarding rather than dialing a wrong
	// port. Wired by pkg/server lifecycle via SetEventIngestURL.
	eventIngestURL string

	// agentHeartbeatRecoverer, when set via SetAgentHeartbeatRecoverer,
	// performs idempotent registry recovery when a heartbeat arrives for
	// an agent that is no longer in the in-memory registry (e.g. after
	// the central daemon restarts). The recoverer uses the request's
	// mTLS peer certificate to re-authenticate the agent and re-publish
	// it into the fleet registry, so the agent never needs to re-enroll.
	// Stored as interface{} so the core rest package does not pull pkg/iam
	// — the iam-tagged install file performs the type assertion itself.
	// Nil means recovery is disabled and unknown-agent heartbeats return
	// 404 (the legacy behaviour).
	agentHeartbeatRecoverer interface{}

	// xkmsCAHandlers, when set via SetCertificateAuthority, is the go-xkms
	// REST CAHandlers set (pkg/api/rest.CAHandlers) wired around a
	// TrustedCA-backed servicer. The stub CA handlers in handlers_xkms.go
	// type-assert on caHandlerSet and delegate when non-nil; otherwise
	// they return 503 Service Unavailable. Typed as an interface so
	// server.go does not import the go-xkms rest package directly.
	xkmsCAHandlers caHandlerSet

	// iamHandler holds the *IAMHandler created during route registration under the
	// rest && iam build tags. Typed as interface{} so server.go does not need to
	// import pkg/iam — lifecycle_iam_mtls.go performs the type assertion.
	// Nil when IAM is not compiled in.
	iamHandler interface{}

	// barrierSvc serves /api/v1/barrier endpoints. Wired by lifecycle via
	// SetBarrierService once the qrdb SDK BarrierClient is available. Nil
	// before wiring; the handlers report 503 Service Unavailable.
	barrierSvc service.BarrierServicer

	// ceremonySvc serves /api/v1/init endpoints. Wired by lifecycle via
	// SetCeremonyService once the qrdb SDK CeremonyClient is available.
	// Nil before wiring; the handlers report 503 Service Unavailable.
	ceremonySvc service.CeremonyServicer

	// bootstrapSvc serves /api/v1/bootstrap endpoints. Wired by lifecycle
	// via SetBootstrapService once the trust bootstrap subsystem is online.
	// Nil before wiring; the handlers report 503 Service Unavailable.
	bootstrapSvc BootstrapServicer

	startTime time.Time
	mu        sync.RWMutex
	started   bool
	closed    bool
}

// NewServer creates a new REST API server.
func NewServer(svc service.QuicBaaS, config *Config, log *logger.Logger) (*Server, error) {
	if svc == nil {
		return nil, api.NewNilInputError("rest.NewServer", "service")
	}

	if log == nil {
		return nil, api.NewNilInputError("rest.NewServer", "logger")
	}

	if config == nil {
		config = DefaultConfig()
	}

	s := &Server{
		config:    config,
		service:   svc,
		logger:    log.WithField("component", "rest-api"),
		startTime: time.Now(),
	}

	// Default router: single-shard routing to shard 1
	s.shardRouter = routing.New()

	// Setup authorization middleware if both dependencies are provided
	if config.TokenValidator != nil && config.AuthzService != nil {
		s.authzMiddleware = NewAuthzMiddleware(&AuthzMiddlewareConfig{
			Logger:         s.logger,
			TokenValidator: config.TokenValidator,
			AuthzService:   config.AuthzService,
		})
		s.logger.Info("Authorization middleware enabled")
	} else if config.TokenValidator != nil || config.AuthzService != nil {
		// Only one is provided - log warning and skip authz
		s.logger.Warn("Authorization middleware not enabled: both TokenValidator and AuthzService are required")
	}

	// Setup router and routes
	s.setupRouter()

	s.logger.Info("REST API server created")
	return s, nil
}

// setupRouter initializes the router and registers all routes.
func (s *Server) setupRouter() {
	r := chi.NewRouter()

	// Apply global middleware
	// Tracing (env-gated)
	r.Use(TracingMiddleware())
	r.Use(RequestIDMiddleware())
	r.Use(SecurityHeadersMiddleware())
	r.Use(RecoveryMiddleware(s.logger))
	r.Use(LoggingMiddleware(s.logger))
	r.Use(MetricsMiddleware())

	// CORS middleware (if origins configured)
	if len(s.config.CORSOrigins) > 0 {
		r.Use(CORSMiddleware(s.config.CORSOrigins))
	}

	// Timeout middleware
	if s.config.RequestTimeout > 0 {
		r.Use(TimeoutMiddleware(s.config.RequestTimeout))
	}

	// Rate limiting middleware (if enabled)
	if s.config.RateLimit > 0 {
		r.Use(RateLimitMiddleware(s.config.RateLimit))
	}

	// API v1 routes
	r.Route("/api/v1", func(r chi.Router) {
		// Apply authorization middleware if configured
		if s.authzMiddleware != nil {
			r.Use(s.authzMiddleware.Authorize)
		}

		// Standard API routes with default body size limit (10 MB).
		// This group protects against memory exhaustion DoS by capping
		// request body sizes via http.MaxBytesReader.
		r.Group(func(r chi.Router) {
			r.Use(BodyLimitMiddleware(s.config.MaxBodySize))

			// Transaction routes
			r.Post("/transaction", s.HandleTransaction) // Transaction with compare-and-swap

			// Batch operation routes
			r.Route("/batch", func(r chi.Router) {
				r.Post("/put", s.HandleBatchPut)       // Atomic batch put
				r.Post("/get", s.HandleBatchGet)       // Batch get multiple entities
				r.Post("/delete", s.HandleBatchDelete) // Atomic batch delete
			})

			// Entity type discovery
			r.Get("/entity-types", s.HandleListEntityTypes) // List unique entity types

			// Entity CRUD routes (numeric IDs)
			r.Route("/entities/{type}", func(r chi.Router) {
				r.Post("/", s.HandlePut)          // Create entity
				r.Get("/", s.HandleList)          // List entities
				r.Get("/{id}", s.HandleGet)       // Get entity by ID
				r.Put("/{id}", s.HandlePut)       // Update entity by ID
				r.Delete("/{id}", s.HandleDelete) // Delete entity by ID
			})

			// Key-Value routes (string keys)
			// These endpoints support string-based keys for key-value store operations,
			// as opposed to the /entities/ endpoints which use numeric IDs.
			r.Route("/kv/{type}", func(r chi.Router) {
				r.Get("/", s.HandleKVList)           // List all key-value pairs for type
				r.Get("/{key}", s.HandleKVGet)       // Get value by string key
				r.Put("/{key}", s.HandleKVPut)       // Put/update value by string key
				r.Post("/{key}", s.HandleKVPut)      // Put/update value by string key (alternative)
				r.Delete("/{key}", s.HandleKVDelete) // Delete by string key
			})

			// Query routes
			r.Route("/query", func(r chi.Router) {
				r.Get("/{type}/{index}", s.HandleQuery) // Query by index
				r.Post("/range", s.HandleRangeQuery)    // Range query
			})

			// KMS routes
			r.Route("/kms", func(r chi.Router) {
				// Key management routes
				r.Post("/keys", s.HandleGenerateKey)                    // Generate new key
				r.Post("/keys/import", s.HandleImportKey)               // Import key
				r.Post("/keys/unwrap", s.HandleUnwrapKey)               // Unwrap/import wrapped key
				r.Get("/keys", s.HandleListKeys)                        // List keys
				r.Get("/keys/{name}", s.HandleGetKey)                   // Get key metadata
				r.Post("/keys/{name}/rotate", s.HandleRotateKey)        // Rotate key
				r.Post("/keys/{name}/export", s.HandleExportKey)        // Export key wrapped
				r.Put("/keys/{name}/status", s.HandleUpdateKeyStatus)   // Update key status
				r.Put("/keys/{name}/default", s.HandleSetDefaultKey)    // Set key as system default
				r.Put("/keys/{name}/safety", s.HandleSetKeySafety)      // Set key safety limits
				r.Delete("/keys/{name}", s.HandleDeleteKey)             // Delete key
				r.Delete("/keys/{name}/safety", s.HandleClearKeySafety) // Clear key safety limits
				r.Get("/keys/{name}/versions", s.HandleListKeyVersions) // List key versions
				r.Put("/keys/{name}/policy", s.HandleSetKeyPolicy)      // Set key access policy
				r.Get("/keys/{name}/policy", s.HandleGetKeyPolicy)      // Get key access policy
				r.Post("/encrypt/{name}", s.HandleKMSEncrypt)           // Encrypt data
				r.Post("/decrypt/{name}", s.HandleKMSDecrypt)           // Decrypt data
				r.Get("/backend/status", s.HandleBackendStatus)         // Backend status
				r.Post("/backend/test", s.HandleBackendTest)            // Test backend

				// Safety monitoring routes
				r.Get("/safety/check", s.HandleSafetyCheck)   // Check keys approaching limits
				r.Get("/safety/report", s.HandleSafetyReport) // Comprehensive safety report

				// DEK (Data Encryption Key) routes
				// Uses wildcard routing to support path-based DEK identifiers like "tenant/app/key"
				r.Route("/deks", func(r chi.Router) {
					r.Post("/", s.HandleCreateDEK)                   // Create DEK
					r.Get("/", s.HandleListDEKs)                     // List DEKs
					r.Put("/default", s.HandleSetDefaultDEKFromBody) // Set default DEK (path in body)
					r.Get("/*", s.handleDEKGetAction)                // Get DEK or status
					r.Post("/*", s.handleDEKPostAction)              // Rotate or set-default
					r.Put("/*", s.handleDEKPutAction)                // Set safety
					r.Delete("/*", s.handleDEKDeleteAction)          // Delete DEK or clear safety
				})
			})

			// Secrets routes
			// Note: Uses ?action= query param for rollback/rotate/destroy, ?versions=true for listing versions
			// Wildcard routes handle path-based secret IDs like "app/database/password"
			r.Route("/secrets", func(r chi.Router) {
				r.Get("/", s.HandleListSecrets)          // List secrets
				r.Post("/*", s.handleSecretsPostAction)  // Store/rollback/rotate/enable/disable/destroy secret
				r.Get("/*", s.HandleGetSecretOrVersions) // Get secret or list versions (uses ?versions=true)
				r.Delete("/*", s.HandleDeleteSecret)     // Delete secret (use ?destroy=true for permanent)
			})

			// Secrets prefix, watch, and versions routes (separate to avoid wildcard conflicts)
			r.Route("/secrets-prefix", func(r chi.Router) {
				r.Get("/*", s.HandleGetSecretsByPrefix) // Get all secrets by prefix
			})
			r.Route("/secrets-watch", func(r chi.Router) {
				r.Get("/*", s.HandleWatchSecrets) // Watch secrets via SSE
			})
			r.Route("/secrets-versions", func(r chi.Router) {
				r.Get("/*", s.HandleListSecretVersions) // List versions for a secret path
			})

			// Parameters routes
			// Uses wildcard routing to support hierarchical paths like "/app/database/host"
			r.Route("/parameters", func(r chi.Router) {
				r.Get("/", s.HandleListParameters)                     // List parameters by prefix
				r.Get("/*", s.HandleGetParameter)                      // Get single parameter
				r.Put("/*", s.HandleSetParameter)                      // Create/update parameter
				r.Delete("/*", s.HandleDeleteParameter)                // Delete parameter
				r.Post("/batch/get", s.HandleGetParametersBatch)       // Batch get
				r.Post("/batch/set", s.HandleSetParametersBatch)       // Batch set
				r.Post("/batch/delete", s.HandleDeleteParametersBatch) // Batch delete
				r.Post("/search", s.HandleSearchParametersByTags)      // Search by tags
			})

			// SSH routes — proxy to xkms SSH SDK via service.QuicBaaS.SSHClient().
			// Mounted at both /api/v1/ssh (canonical) and /api/v1/xkms/v1/ssh
			// (the path prefix the go-xkms REST SSH SDK client expects — this
			// alias lets CLIs set XKMSD_URL to the quicbaasd base and reuse
			// the go-xkms client unchanged).
			r.Route("/ssh", func(r chi.Router) {
				r.Post("/keys", s.HandleGenerateSSHKey)
				r.Post("/keys/import", s.HandleImportSSHKey)
				r.Get("/keys", s.HandleListSSHKeys)
				r.Get("/keys/{scope}/{owner}/{name}", s.HandleGetSSHKey)
				r.Delete("/keys/{scope}/{owner}/{name}", s.HandleDeleteSSHKey)
				r.Get("/keys/{scope}/{owner}/{name}/public", s.HandleExportSSHKeyPublic)
				r.Post("/keys/{scope}/{owner}/{name}/sign", s.HandleSignSSHDigest)

				r.Post("/known-hosts", s.HandleAddKnownHost)
				r.Get("/known-hosts", s.HandleListKnownHosts)
				r.Delete("/known-hosts/{host}", s.HandleRemoveKnownHost)
				r.Post("/known-hosts/{host}/verify", s.HandleVerifyKnownHost)

				r.Post("/authorized", s.HandleEmitAuthorized)
				r.Post("/authorized/verify", s.HandleVerifyAuthorized)
			})

			// Alias: /api/v1/xkms/v1/ssh/* → same handlers as /api/v1/ssh/*.
			// Matches the xkms SDK REST client's fixed path prefix.
			r.Route("/xkms/v1/ssh", func(r chi.Router) {
				r.Post("/keys", s.HandleGenerateSSHKey)
				r.Post("/keys/import", s.HandleImportSSHKey)
				r.Get("/keys", s.HandleListSSHKeys)
				r.Get("/keys/{scope}/{owner}/{name}", s.HandleGetSSHKey)
				r.Delete("/keys/{scope}/{owner}/{name}", s.HandleDeleteSSHKey)
				r.Get("/keys/{scope}/{owner}/{name}/public", s.HandleExportSSHKeyPublic)
				r.Post("/keys/{scope}/{owner}/{name}/sign", s.HandleSignSSHDigest)

				r.Post("/known-hosts", s.HandleAddKnownHost)
				r.Get("/known-hosts", s.HandleListKnownHosts)
				r.Delete("/known-hosts/{host}", s.HandleRemoveKnownHost)
				r.Post("/known-hosts/{host}/verify", s.HandleVerifyKnownHost)

				r.Post("/authorized", s.HandleEmitAuthorized)
				r.Post("/authorized/verify", s.HandleVerifyAuthorized)
			})


			// Seal routes (LEGACY — superseded by /api/v1/barrier/*).
			// Each handler stamps Deprecation/Sunset/Link headers per
			// RFC 8594 + draft-ietf-httpapi-deprecation-header. See
			// markSealDeprecated in handlers_seal.go.
			r.Route("/seal", func(r chi.Router) {
				r.Get("/status", s.HandleSealStatus)                      // Get seal status
				r.Post("/init", s.HandleSealInit)                         // Initialize
				r.Post("/unseal", s.HandleUnseal)                         // Unseal with key share
				r.Post("/seal", s.HandleSeal)                             // Seal database
				r.Post("/rekey", s.HandleRekey)                           // Rekey master key
				r.Post("/recovery/generate", s.HandleGenerateRecoveryKey) // Generate recovery key
				r.Delete("/recovery", s.HandleDeleteRecoveryKey)          // Delete recovery key
				r.Post("/root/generate", s.HandleGenerateRootToken)       // Generate root token
				r.Post("/rotate-root", s.HandleRotateRootKey)             // Rotate root encryption key
			})

			// Barrier routes — canonical seal-lifecycle surface that
			// supersedes /api/v1/seal. Mirrors the qrdb /v1/barrier
			// contract so a single SDK works against both products.
			r.Route("/barrier", func(r chi.Router) {
				r.Get("/status", s.HandleBarrierStatus)
				r.Post("/initialize", s.HandleBarrierInitialize)
				r.Post("/unseal", s.HandleBarrierUnseal)
				r.Post("/seal", s.HandleBarrierSeal)

				r.Post("/shamir/initialize", s.HandleBarrierShamirInitialize)
				r.Post("/shamir/unseal-share", s.HandleBarrierShamirUnsealShare)
				r.Post("/shamir/unseal-shares", s.HandleBarrierShamirUnsealShares)
				r.Get("/shamir/shares", s.HandleBarrierShamirListShares)
				r.Delete("/shamir/shares", s.HandleBarrierShamirDeleteAllShares)
				r.Delete("/shamir/shares/{index}", s.HandleBarrierShamirDeleteShare)
				r.Post("/shamir/verify", s.HandleBarrierShamirVerify)

				r.Post("/rekey", s.HandleBarrierRekey)

				r.Post("/recovery/generate", s.HandleBarrierRecoveryGenerate)
				r.Post("/recovery/recover", s.HandleBarrierRecoveryRecover)
				r.Delete("/recovery", s.HandleBarrierRecoveryDelete)
				r.Get("/recovery/status", s.HandleBarrierRecoveryStatus)

				r.Post("/root-token", s.HandleBarrierRootToken)
			})

			// Ceremony routes — canonical /api/v1/init surface. The
			// ceremony lifecycle (M-of-N officer enrollment, certificate
			// claims, share distribution, CSR signing) lives under /init
			// to align with the qrdb sealed-window allowlist.
			r.Route("/init", func(r chi.Router) {
				r.Get("/status", s.HandleCeremonyStatus)
				r.Get("/officers", s.HandleCeremonyListOfficers)
				r.Post("/initialize", s.HandleCeremonyInitialize)
				r.Post("/claim-cert/begin", s.HandleCeremonyClaimCertBegin)
				r.Post("/claim-cert/complete", s.HandleCeremonyClaimCertComplete)
				r.Post("/claim-share", s.HandleCeremonyClaimShare)
				r.Post("/sign-csr", s.HandleCeremonySignCSR)
				r.Post("/enroll-external-officer", s.HandleCeremonyEnrollExternalOfficer)
			})

			// Bootstrap routes — trust-bootstrap surface for first-touch
			// CA discovery (DANE, Noise PSK, SPKI pin). Handlers return
			// 503 Service Unavailable until the truststrap subsystem is
			// online, then 501 Not Implemented for methods that haven't
			// been wired to a concrete servicer yet.
			r.Route("/bootstrap", func(r chi.Router) {
				r.Post("/auto", s.HandleBootstrapAuto)
				r.Post("/dane", s.HandleBootstrapDane)
				r.Post("/noise", s.HandleBootstrapNoise)
				r.Post("/spki", s.HandleBootstrapSpki)
			})

			// Crypto routes
			r.Route("/crypto", func(r chi.Router) {
				r.Post("/encrypt/{name}", s.HandleCryptoEncrypt)            // Encrypt
				r.Post("/decrypt/{name}", s.HandleCryptoDecrypt)            // Decrypt
				r.Post("/rewrap/{name}", s.HandleCryptoRewrap)              // Re-encrypt with latest key
				r.Post("/sign/{name}", s.HandleCryptoSign)                  // Sign data
				r.Post("/verify/{name}", s.HandleCryptoVerify)              // Verify signature
				r.Post("/hash", s.HandleCryptoHash)                         // Hash data
				r.Post("/random", s.HandleCryptoRandom)                     // Generate random bytes
				r.Post("/datakey/{name}", s.HandleCryptoDataKey)            // Generate data encryption key
				r.Post("/encrypt/{name}/batch", s.HandleCryptoBatchEncrypt) // Batch encrypt
				r.Post("/decrypt/{name}/batch", s.HandleCryptoBatchDecrypt) // Batch decrypt
			})

			// Cluster management routes
			r.Route("/cluster", func(r chi.Router) {
				// Status and information
				r.Get("/status", s.HandleClusterStatus)       // Cluster status
				r.Get("/leader/{shardID}", s.HandleGetLeader) // Get shard leader
				r.Get("/leader", s.HandleGetClusterLeader)    // Get cluster leader
				r.Get("/shards", s.HandleClusterShards)       // List shard details

				// Node management
				r.Route("/nodes", func(r chi.Router) {
					r.Post("/", s.HandleAddNode)                     // Add node
					r.Get("/", s.HandleListNodes)                    // List nodes
					r.Get("/non-leader", s.HandleListNonLeaderNodes) // List non-leader nodes for load balancer routing
					r.Delete("/{nodeId}", s.HandleRemoveNode)        // Remove node
					r.Put("/{nodeId}/replace", s.HandleReplaceNode)  // Replace node
				})

				// Rebalance operations
				r.Route("/rebalance", func(r chi.Router) {
					r.Post("/", s.HandleStartRebalance)          // Start rebalancing
					r.Get("/status", s.HandleGetRebalanceStatus) // Get rebalance status
					r.Get("/plan", s.HandleGetRebalancePlan)     // Get rebalance plan
				})

				// Migration operations
				r.Route("/migrate", func(r chi.Router) {
					r.Post("/", s.HandleStartMigration)          // Start migration
					r.Get("/status", s.HandleGetMigrationStatus) // Get migration status
					r.Delete("/", s.HandleCancelMigration)       // Cancel migration
				})

				// Upgrade operations
				r.Route("/upgrade", func(r chi.Router) {
					r.Post("/", s.HandleStartUpgrade)          // Start upgrade
					r.Get("/status", s.HandleGetUpgradeStatus) // Get upgrade status
				})
			})

			// Timeseries routes
			r.Route("/timeseries", func(r chi.Router) {
				r.Post("/write", s.HandleWritePoint)
				r.Post("/write-batch", s.HandleWriteBatch)
				r.Post("/query", s.HandleTSQuery)
				r.Delete("/series", s.HandleDeleteSeries)
				r.Get("/retention/{metric}", s.HandleGetRetentionPolicy)
				r.Put("/retention/{metric}", s.HandleSetRetentionPolicy)
				r.Get("/metrics", s.HandleListMetrics)
			})

			// Health check routes
			r.Get("/health", s.HandleHealth)
			r.Get("/health/live", s.HandleLiveness)
			r.Get("/health/ready", s.HandleReadiness)

			// Version (open — needed by the GUI dashboard).
			r.Get("/version", s.HandleVersion)

			// Metrics routes
			r.Route("/metrics", func(r chi.Router) {
				// Prometheus metrics
				r.Handle("/", promhttp.HandlerFor(
					metrics.Registry(),
					promhttp.HandlerOpts{
						EnableOpenMetrics: true,
					},
				))
				// Performance metrics (JSON)
				r.Get("/performance", s.HandlePerformanceMetrics)
			})

			// Vector database routes
			registerVectorRoutes(s, r)

			// Ollama integration routes
			registerOllamaRoutes(s, r)

			// RAG context routes
			registerRAGRoutes(s, r)

			// Embedding service routes
			registerEmbeddingRoutes(s, r)

			// Certificate service routes
			registerCertificateRoutes(s, r)

			// Dynamic secrets service routes
			registerDynamicSecretsRoutes(s, r)

			// Cluster DEK service routes
			registerClusterDEKRoutes(s, r)

			// Scheduler routes
			registerSchedulerRoutes(s, r)

			// Push notification routes
			registerMessagingRoutes(s, r)

			// Claude Code hook routes
			registerHookRoutes(s, r)

			// Cache management routes
			registerCacheRoutes(s, r)

			// XKMS-compatible routes (go-xkms SDK REST transport compatibility)
			registerXKMSRoutes(s, r)

			// Project routes (tenant → project → workflow hierarchy)
			registerProjectRoutes(s, r)

			// Agent admin routes (disable/enable agents)
			registerAgentAdminRoutes(s, r)

			// Orchestrator routes (agent sessions, tasks, backends)
			registerOrchestratorRoutes(s, r)

		})

		// Upload-heavy routes with larger body size limit (100 MB).
		// Object storage, backup, and object transfer endpoints accept
		// larger payloads and use a separate, higher body size limit.
		r.Group(func(r chi.Router) {
			r.Use(BodyLimitMiddleware(s.config.MaxUploadBodySize))

			// Object storage routes
			registerObjectStorageRoutes(s, r)

			// Object storage admin routes
			registerObjectStorageAdminRoutes(s, r)

			// Object transfer routes (peer-to-peer object data transfer)
			registerObjectTransferRoutes(s, r)

			// Backup management routes
			registerBackupRoutes(s, r)
		})
	})

	// IAM routes (no-op when iam build tag is not set)
	// Note: This is at root level because OIDC endpoints must be at /.well-known/*
	// The IAM handler internally registers /api/v1/* routes for management APIs
	s.registerIAMRoutes(r)

	// Integration-only debug routes (no-op in non-integration builds)
	registerDebug(s, r)

	// API Documentation routes
	r.Get("/docs", s.HandleDocsRedirect)
	r.Get("/docs/", s.HandleSwaggerUI)
	r.Get("/docs/openapi.yaml", s.HandleOpenAPISpec)

	// Embedded Svelte UI — registered after all /api/v1 routes so that API
	// paths are never shadowed. The UI is available at /ui/*.
	ui.RegisterUI(r)

	// Root redirect - now points to API documentation
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
	})

	s.router = r
}

// handleDEKGetAction routes GET requests for DEKs based on path suffix.
// Routes to status handler if path ends with /status, otherwise to get DEK.
func (s *Server) handleDEKGetAction(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")

	// Check for /status suffix
	if len(path) > 7 && path[len(path)-7:] == "/status" {
		s.HandleGetDEKStatus(w, r)
		return
	}

	// Check for /reencrypt suffix (get re-encryption progress)
	if len(path) > 10 && path[len(path)-10:] == "/reencrypt" {
		s.HandleGetReencryptionProgress(w, r)
		return
	}

	// Default to get DEK
	s.HandleGetDEK(w, r)
}

// handleDEKPostAction routes POST requests for DEKs based on path suffix.
// Routes to rotate, set-default, or reencrypt handlers based on path suffix.
func (s *Server) handleDEKPostAction(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")

	// Check for /rotate suffix
	if len(path) > 7 && path[len(path)-7:] == "/rotate" {
		s.HandleRotateDEK(w, r)
		return
	}

	// Check for /set-default suffix
	if len(path) > 12 && path[len(path)-12:] == "/set-default" {
		s.HandleSetDefaultDEK(w, r)
		return
	}

	// Check for /reencrypt suffix
	if len(path) > 10 && path[len(path)-10:] == "/reencrypt" {
		s.HandleStartReencryption(w, r)
		return
	}

	// Unknown action
	RespondBadRequest(w, r, "unknown DEK action: expected /rotate, /set-default, or /reencrypt suffix")
}

// handleDEKPutAction routes PUT requests for DEKs based on path suffix.
// Routes to set safety handler if path ends with /safety.
func (s *Server) handleDEKPutAction(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")

	// Check for /safety suffix
	if len(path) > 7 && path[len(path)-7:] == "/safety" {
		s.HandleSetDEKSafety(w, r)
		return
	}

	// Unknown action
	RespondBadRequest(w, r, "unknown DEK action: expected /safety suffix")
}

// handleDEKDeleteAction routes DELETE requests for DEKs based on path suffix.
// Routes to clear safety handler if path ends with /safety, otherwise to delete DEK.
func (s *Server) handleDEKDeleteAction(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")

	// Check for /safety suffix
	if len(path) > 7 && path[len(path)-7:] == "/safety" {
		s.HandleClearDEKSafety(w, r)
		return
	}

	// Default to delete DEK
	s.HandleDeleteDEK(w, r)
}

// handleSecretsPostAction dispatches wildcard POST requests under /api/v1/secrets/*
// to the appropriate handler based on the path suffix or query parameter.
//
// Routing priority:
// 1. Path suffix /enable or /disable for status changes
// 2. Fall through to HandleSecretsAction for ?action= dispatch (store/rollback/rotate/destroy)
//
// Note: Destroy is handled via ?action=destroy or DELETE ?destroy=true, NOT path suffix,
// to avoid conflicts with secret paths that legitimately end in "/destroy".
func (s *Server) handleSecretsPostAction(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")

	// Check for enable/disable suffixes
	if len(path) > 7 && path[len(path)-7:] == "/enable" {
		s.HandleEnableSecret(w, r)
		return
	}
	if len(path) > 8 && path[len(path)-8:] == "/disable" {
		s.HandleDisableSecret(w, r)
		return
	}
	if len(path) > 7 && path[len(path)-7:] == "/ensure" {
		s.HandleEnsureSecret(w, r)
		return
	}

	// Fall through to action-based dispatch (store/rollback/rotate/destroy)
	s.HandleSecretsAction(w, r)
}

// SetShardRouter overrides the default shard router (for tests or custom routing).
func (s *Server) SetShardRouter(r interface{ Route(string, uint64) uint64 }) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shardRouter = r
}

// SetHookTaskStore sets the task store used by Claude Code hook handlers.
func (s *Server) SetHookTaskStore(store orchestration.TaskStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hookTaskStore = store
}

// SetBarrierService installs the barrier facade used by /api/v1/barrier
// handlers. Lifecycle wiring calls this once the qrdb SDK BarrierClient
// is available. Passing nil disables the surface (handlers report 503).
func (s *Server) SetBarrierService(svc service.BarrierServicer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.barrierSvc = svc
}

// SetCeremonyService installs the ceremony facade used by /api/v1/init
// handlers. Lifecycle wiring calls this once the qrdb SDK CeremonyClient
// is available. Passing nil disables the surface (handlers report 503).
func (s *Server) SetCeremonyService(svc service.CeremonyServicer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ceremonySvc = svc
}

// SetBootstrapService installs the trust-bootstrap facade used by
// /api/v1/bootstrap handlers. Lifecycle wiring calls this once the
// truststrap subsystem is online. Passing nil disables the surface
// (handlers report 503).
func (s *Server) SetBootstrapService(svc BootstrapServicer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bootstrapSvc = svc
}

// SetAuthorizationService sets the authorization service and token validator after construction.
// This allows setting authz dependencies after the server is created.
// Both tokenValidator and authz must be non-nil to enable authorization.
// This method recreates the router with the new middleware applied.
func (s *Server) SetAuthorizationService(authz interfaces.AuthorizationService, tokenValidator TokenValidator) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if authz == nil || tokenValidator == nil {
		s.logger.Warn("SetAuthorizationService called with nil dependency, authorization not enabled")
		return
	}

	s.authzMiddleware = NewAuthzMiddleware(&AuthzMiddlewareConfig{
		Logger:         s.logger,
		TokenValidator: tokenValidator,
		AuthzService:   authz,
	})

	// Rebuild the router with the new middleware
	s.setupRouter()

	s.logger.Info("Authorization middleware enabled via SetAuthorizationService")
}

// HasAuthorizationMiddleware returns true if authorization middleware is configured.
func (s *Server) HasAuthorizationMiddleware() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authzMiddleware != nil
}

// Start starts the HTTP server. TLS is always enabled: the server prefers the
// pre-built TLSConfig (backed by OpaqueKeys) and falls back to loading
// certificates from TLSCertFile/TLSKeyFile when TLSConfig is nil.
//
// When Config.MaxConnections is greater than zero, the TCP listener is wrapped
// with a connection limiter that blocks Accept once the limit is reached.
func (s *Server) Start(address string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return api.NewClientClosedError("rest.Server.Start")
	}

	if s.started {
		return api.NewInvalidInputError("server already started")
	}

	// Use provided address or default from config
	if address == "" {
		address = s.config.Address
	}

	s.server = &http.Server{
		Addr:           address,
		Handler:        s.router,
		ReadTimeout:    s.config.ReadTimeout,
		WriteTimeout:   s.config.WriteTimeout,
		IdleTimeout:    s.config.IdleTimeout,
		MaxHeaderBytes: s.config.MaxHeaderBytes,
		// Disable HTTP/2 auto-enablement. Go's ServeTLS() calls http2.ConfigureServer()
		// unless TLSNextProto is a non-nil map. Without this, clients using HTTP/1.1
		// (ForceAttemptHTTP2: false) get "tls: bad record MAC" errors.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	// Build TLS configuration: prefer pre-built config, fall back to file-based
	if s.config.TLSConfig != nil {
		// Use pre-built TLS config (OpaqueKey-backed, in-memory)
		s.server.TLSConfig = s.config.TLSConfig
	} else if s.config.TLSEnabled {
		// Fall back to file-based TLS config
		tlsConfig, err := s.buildTLSConfig()
		if err != nil {
			return api.NewConfigurationError("rest.Server.Start", "configure TLS", err)
		}
		s.server.TLSConfig = tlsConfig
	}

	// Create the TCP listener
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return api.NewOperationFailedError("listen on "+address, err)
	}

	// Wrap with connection limiter when MaxConnections is configured
	if s.config.MaxConnections > 0 {
		ln = newLimitListener(ln, s.config.MaxConnections)
		s.logger.WithField("max_connections", s.config.MaxConnections).
			Info("Connection limit enforced on REST listener")
	}

	s.listener = ln
	s.started = true

	// Start server with TLS when a TLS config is available
	if s.server.TLSConfig != nil {
		s.logger.WithField("address", address).Info("Starting REST API server with TLS (HTTPS)")
		go func() {
			// Pass empty strings: the TLS config already contains the certificate
			if err := s.server.ServeTLS(ln, "", ""); err != nil && err != http.ErrServerClosed {
				s.logger.WithError(err).Error("REST API server error")
			}
		}()
	} else {
		s.logger.WithField("address", address).Warn("Starting REST API server without TLS (not recommended for production)")
		go func() {
			if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
				s.logger.WithError(err).Error("REST API server error")
			}
		}()
	}

	return nil
}

// buildTLSConfig creates a TLS configuration for the REST server.
func (s *Server) buildTLSConfig() (*tls.Config, error) {
	// Load server certificate
	cert, err := tls.LoadX509KeyPair(s.config.TLSCertFile, s.config.TLSKeyFile)
	if err != nil {
		return nil, api.NewOperationFailedError("load server certificate", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	// Configure client certificate authentication.
	//
	// Three modes are supported:
	//   TLSClientAuth=true + TLSCAFile → RequireAndVerifyClientCert
	//     (every TLS connection must present a CA-signed client cert)
	//
	//   TLSClientAuth=false + TLSCAFile → VerifyClientCertIfGiven
	//     (browsers connect without a cert; agents can optionally
	//     present one which per-route handlers then validate. This
	//     is the mode quicbaasd uses so the /api/v1/agents/register
	//     mTLS endpoint coexists with the unauthenticated UI.)
	//
	//   TLSClientAuth=false + no TLSCAFile → no client-auth plumbing
	if s.config.TLSClientAuth {
		if s.config.TLSCAFile == "" {
			return nil, api.NewRequiredFieldError("TLSCAFile")
		}
		caCert, err := os.ReadFile(s.config.TLSCAFile)
		if err != nil {
			return nil, api.NewOperationFailedError("read CA certificate", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, api.NewInvalidFormatError("CA certificate", nil)
		}
		tlsConfig.ClientCAs = caCertPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		s.logger.Info("REST server configured with mTLS (client certificate required)")
	} else if s.config.TLSCAFile != "" {
		caCert, err := os.ReadFile(s.config.TLSCAFile)
		if err != nil {
			return nil, api.NewOperationFailedError("read CA certificate", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, api.NewInvalidFormatError("CA certificate", nil)
		}
		tlsConfig.ClientCAs = caCertPool
		tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
		s.logger.Info("REST server configured with optional mTLS (per-route client cert validation)")
	}

	return tlsConfig, nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.logger.Info("Shutting down REST API server")

	s.closed = true

	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			return api.NewOperationFailedError("shutdown server", err)
		}
	}

	s.logger.Info("REST API server shutdown complete")
	return nil
}

// Close closes the server (alias for Shutdown with background context).
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.Shutdown(ctx)
}

// GetRouter returns the underlying chi router (for testing).
func (s *Server) GetRouter() *chi.Mux {
	return s.router
}

// IsStarted returns true if the server is started.
func (s *Server) IsStarted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started
}

// IsClosed returns true if the server is closed.
func (s *Server) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// Uptime returns the server uptime.
func (s *Server) Uptime() time.Duration {
	return time.Since(s.startTime)
}
