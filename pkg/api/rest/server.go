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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest/ui"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/routing"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/clientip"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
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

	// RateLimit is the per-source-IP request rate, in requests per second,
	// that the global limiter allows; the bucket also holds RateLimit
	// requests of burst. Zero means DefaultRateLimit, so a Config built
	// without the field, or from a config file that omits the key, is
	// limited rather than silently open. RateLimitDisabled (any negative
	// value) is the explicit opt-out. See EffectiveRateLimit.
	RateLimit int

	// TrustedProxies lists the CIDRs (or bare addresses) whose
	// X-Forwarded-For and X-Real-IP headers are believed.
	//
	// EMPTY MEANS TRUST NOTHING, which is the secure default and the right one
	// for a listener reachable directly: without it, any caller chooses its own
	// rate-limit bucket and its own audit trail by setting a header
	// (go-quicbaas#156). A deployment behind a load balancer opts in by naming
	// the balancer here; nothing is inferred from the request itself, because
	// everything in the request is attacker-controlled.
	TrustedProxies []string

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

	// PIVClientCert controls PIV certificate-based activation (default: disabled).
	PIVClientCert PIVClientCertConfig

	// IAMSelfServiceRegistration is the per-server self-service tenant
	// registration policy. When zero-valued the registration surface is
	// disabled (the safe default). Embedded callers that want to enable
	// self-service registration populate this field at server-construction
	// time from their own config struct.
	IAMSelfServiceRegistration config.IAMSelfServiceRegistrationConfig

	// IAMDynamicClientRegistration is the per-server RFC 7591 Dynamic Client
	// Registration policy. When zero-valued the registration endpoint is
	// disabled (the safe default) and anonymous registration is impossible:
	// a token-less caller can neither create a client nor pick its tenant.
	IAMDynamicClientRegistration config.IAMDynamicClientRegistrationConfig

	// MCPAuthorization is the per-server RFC 9728 protected-resource
	// metadata policy (/.well-known/oauth-protected-resource), the MCP
	// client-auth discovery counterpart to the OAuth AS metadata endpoint
	// (RFC 8414) the IAM OIDC provider already serves. When zero-valued
	// (Enabled false) the endpoint answers 404, the same disabled-by-
	// default posture every other MCP-auth surface takes.
	MCPAuthorization config.MCPAuthorizationConfig

	// SPIFFETrustDomain is the operator-configured SPIFFE trust domain used to
	// derive (and pin) the login tenant from a client certificate's
	// tenant-scoped SPIFFE URI SAN. When empty, SPIFFE-SAN tenant derivation
	// is disabled and login falls back to home-realm discovery.
	SPIFFETrustDomain string

	// HomeRealmResolver resolves a login's tenant from non-secret routing
	// hints (SNI/host, email domain) when no SPIFFE SAN is present. It reads
	// the shared, secret-free routing directory and is therefore usable while
	// target tenant barriers are sealed. When nil, HRD is disabled.
	HomeRealmResolver HomeRealmResolver
}

// HomeRealmResolver is the minimal home-realm discovery contract the REST
// login handlers depend on. It is satisfied structurally by
// iam.HomeRealmDirectory; declaring it here keeps the (rest-only) server.go
// free of an iam build-tag dependency.
type HomeRealmResolver interface {
	Resolve(ctx context.Context, hints RealmHints) (string, error)
}

// RealmHints mirrors iam.RealmHints for the resolver contract.
type RealmHints struct {
	TenantHint string
	SNIHost    string
	Email      string
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
	//
	// A DN comparison is not evidence of hardware — any certificate can assert
	// any issuer name. Hardware is established by AttestationCAFile.
	TrustedIssuers []string

	// AttestationCAFile is the PEM bundle of PIV attestation CA certificates
	// (e.g. the Yubico PIV Attestation root) a slot certificate must chain to
	// before a login is treated as hardware-backed.
	AttestationCAFile string

	// AttestationIntermediatesFile is an optional PEM bundle of vendor sub-CA
	// certificates used to complete an attestation chain.
	AttestationIntermediatesFile string

	// AllowUnattested permits PIV login for a certificate whose hardware
	// provenance was not proven. The issued token then reports the level that
	// was actually established and omits the hardware-attested ACR, so the
	// downgrade is explicit rather than silent. Default false: an unconfigured
	// deployment refuses.
	AllowUnattested bool
}

// DefaultConfig returns default REST API configuration.
const (
	// DefaultRateLimit is the global per-source-IP limit, in requests per
	// second, applied when Config.RateLimit is zero (#158).
	//
	// Chosen by measuring the Go SDK against this router: NewClient sends no
	// request and every SDK call sends exactly one, so an interactive client
	// issues a handful of requests per user action. One sequential client
	// reached about 5,600 requests per second on loopback, where a request
	// costs about 0.18 ms. Across a network a sequential client is bound by
	// the round trip, so at 1 ms or more it cannot exceed 1,000 per second.
	// This limit therefore never throttles one sequential SDK client over a
	// real network, and still bounds a single source that floods with many
	// concurrent requests (eight goroutines on one client reached about
	// 31,000 per second).
	DefaultRateLimit = 1000

	// RateLimitDisabled is the documented opt-out value for
	// Config.RateLimit: it turns the global limiter off. Any negative value
	// has the same effect. The endpoint-scoped OAuth2 limiter stays on.
	RateLimitDisabled = -1
)

// EffectiveRateLimit returns the global limiter's requests-per-second rate,
// or 0 when the operator opted out with a negative RateLimit. A zero
// RateLimit resolves to DefaultRateLimit.
func (c *Config) EffectiveRateLimit() int {
	switch {
	case c.RateLimit < 0:
		return 0
	case c.RateLimit == 0:
		return DefaultRateLimit
	default:
		return c.RateLimit
	}
}

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
		RateLimit:         DefaultRateLimit,
		TrustedProxies:    nil, // Trust no proxy; attribute to the transport peer
		ExposeShardHeader: true,
	}
}

// Server represents the REST API server.
type Server struct {
	config *Config

	// clientIP resolves which address a request is attributed to. Built once
	// from Config.TrustedProxies so the trusted-proxy decision is made in one
	// place; nil is impossible after NewServer, but a nil resolver is safe and
	// trusts nothing.
	clientIP *clientip.Resolver

	// rateLimit is the global per-client-IP limiter, built once in NewServer
	// so the core router (setupRouter) and every module route (BindRoute)
	// draw on one budget per client. Nil when the operator opted out.
	rateLimit func(http.Handler) http.Handler

	router  *chi.Mux
	server  *http.Server
	service service.QuicBaaS
	logger  *logger.Logger
	// shardRouter selects a raft shard for (entityType, id)
	shardRouter interface{ Route(string, uint64) uint64 }

	// clusterSecretHandler serves the cluster CA secret-delivery endpoint
	// (tpm-kek). Nil unless a cluster CA key strategy delivers enrollment
	// material; set via SetClusterSecretHandler before setupRouter.
	clusterSecretHandler http.HandlerFunc

	// sdlcWebhookHandler serves the forge webhook ingress. Nil until the SDLC
	// module starts and injects it; the route is registered either way so a
	// node with the module disabled answers 503 rather than 404. See
	// HandleSDLCWebhook for why that distinction matters to a forge.
	sdlcWebhookHandler http.HandlerFunc

	// tenantWebAuthnWarned is the one-per-episode flag behind the tenant
	// WebAuthn ceremony's unavailability warning. It lives on the server
	// rather than in the handler closure because the module route table binds
	// moduleWebAuthnHandler five times, once per ceremony path, and five
	// closures would each warn about the same outage naming the same five
	// routes. See warnTenantWebAuthnUnavailable.
	tenantWebAuthnWarned atomic.Bool

	// listener is the TCP listener backing the HTTP server.
	// It is created in Start and may be wrapped by a limitListener.
	listener net.Listener

	// pivAttestation verifies that a PIV login certificate chains to an
	// operator-configured smartcard attestation CA. It is built once from
	// config on the first PIV login — the trust anchors cannot change without
	// a restart, so re-reading the bundle per request would only add I/O.
	// pivAttestationErr holds a construction failure so the refusal is
	// reported identically on every subsequent request rather than being
	// retried into a different answer.
	pivAttestationOnce sync.Once
	pivAttestation     *iam.PIVAttestationVerifier
	pivAttestationErr  error

	// moduleAuthorizer is the platform-token validator AuthenticateHTTP
	// (auth_support.go) authenticates against: when go-qrdb's unified-server
	// middleware reaches a quicbaas module route WITHOUT a qrdb-authenticated
	// identity (a platform-JWT caller, which qrdb's own mTLS chain does not
	// understand), it consults the module's ModuleAuthFallback
	// (pkg/apimodule module_http.go), which delegates to
	// Server.AuthenticateHTTP, which validates the token through this seam.
	// Wired by SetModuleTokenAuthorizer (pkg/server configureRESTAuthz) only
	// when SecurityConfig.RequireAuthz is enabled; nil means
	// AuthenticateHTTP fails closed (ModuleAuthorizerRequiredError) and a
	// platform-token caller with no qrdb identity is denied — matching the
	// "no authz gate configured" posture with mTLS-only enforcement.
	moduleAuthorizer ModuleTokenAuthorizer

	// sealedProbe + sealedAllowlist implement the sealed-window gate (see
	// SetSealedWindowGate in sealed_window.go). While the probe reports
	// sealed, setupRouter installs qrdb's canonical REST sealed-window
	// middleware ahead of every authz gate so only allowlisted paths are
	// reachable.
	sealedProbe     qrdbsdk.SealedWindowProbe
	sealedAllowlist []string

	// sealedBypass is the shared sealed-window authentication-bypass
	// predicate (probe + canonical-table lookup), derived by
	// SetSealedWindowGate from the same probe that drives the sealed gate.
	// setupRouter propagates it into the installed authz middleware so that
	// while the barrier is sealed the allowlisted unseal-flow paths are
	// admitted without authentication (audited); unsealed it is inert. Nil
	// until SetSealedWindowGate installs a probe — nil never bypasses.
	sealedBypass *sealedwindow.AuthBypass

	// hookTaskStore is the task store used by Claude Code hook handlers (optional)
	hookTaskStore orchestration.TaskStore

	// fleetRegistrar is the fleet-registry registrar used by the mTLS
	// register handler (handlers_fleet_enroll.go) to publish newly
	// registered daemons. Stored as interface{} to avoid pulling pkg/iam
	// into the core rest server type — the register handler performs the
	// type assertion itself.
	fleetRegistrar interface{}

	// fleetAddressProber refuses, at enrollment, a fleet node address the
	// control plane cannot resolve and dial. Nil registers it unprobed.
	fleetAddressProber FleetAddressProber

	// fleetClustered is true when this control plane is one node of a
	// cluster: enrollment then refuses a loopback fleet node address, which
	// the other nodes cannot dial.
	fleetClustered bool

	// eventIngestURL is the gRPC URL the agent register handler hands
	// back to a daemon on enrollment so the daemon dials the gRPC
	// listener (h2 ALPN) for PushEvents instead of the REST listener
	// (http/1.1 ALPN). Format is a full URL: "https://host:port".
	// Empty string means the server has no gRPC listener exposed; the
	// agent then skips event forwarding rather than dialing a wrong
	// port. Wired by pkg/server lifecycle via SetEventIngestURL.
	eventIngestURL string

	// fleetHeartbeatRecoverer, when set via SetFleetHeartbeatRecoverer,
	// performs idempotent registry recovery when a heartbeat arrives for
	// an agent that is no longer in the in-memory registry (e.g. after
	// the central daemon restarts). The recoverer uses the request's
	// mTLS peer certificate to re-authenticate the agent and re-publish
	// it into the fleet registry, so the agent never needs to re-enroll.
	// Stored as interface{} so the core rest server type does not pull
	// pkg/iam — the install file performs the type assertion itself.
	// Nil means recovery is disabled and unknown-agent heartbeats return
	// 404 (the legacy behaviour).
	fleetHeartbeatRecoverer interface{}

	// xkmsCAHandlers, when set via SetCertificateAuthority, is the go-xkms
	// REST CAHandlers set (pkg/api/rest.CAHandlers) wired around a
	// TrustedCA-backed servicer. The stub CA handlers in handlers_xkms.go
	// type-assert on caHandlerSet and delegate when non-nil; otherwise
	// they return 503 Service Unavailable. Typed as an interface so
	// server.go does not import the go-xkms rest package directly.
	xkmsCAHandlers caHandlerSet

	// iamHandler holds the *IAMHandler created during route registration under
	// the rest build tag. Typed as interface{} so server.go does not need to
	// import pkg/iam — lifecycle_iam_mtls.go performs the type assertion.
	// Nil until route registration wires it.
	iamHandler interface{}

	// registrationSweeper discards the self-registrations a restart left
	// unfinished, and holds the timers armed for those not yet expired.
	// Shutdown stops it. Nil until route registration wires it.
	registrationSweeper *registrationSweeper

	// barrierSvc serves /api/v1/barrier endpoints. Wired by lifecycle via
	// SetBarrierService once the qrdb SDK BarrierClient is available. Nil
	// before wiring; the handlers report 503 Service Unavailable.
	barrierSvc service.BarrierServicer

	// tenantBarrierSvc serves /api/v1/tenants/{id}/barrier endpoints. Wired by
	// lifecycle via SetTenantBarrierService once the qrdb SDK TenantClient is
	// available. Nil before wiring; the handlers report 503.
	tenantBarrierSvc service.TenantBarrierServicer

	// barrierScopeAuthz decides POST /v1/barrier/enable-encryption, the one
	// barrier route whose scope comes from its body rather than its path, so
	// no single registration-site permission can gate it (go-quicbaas#990).
	// Wired by lifecycle via SetBarrierScopeAuthorizer to go-qrdb's RBAC
	// authorizer. Nil before wiring; the handler fails closed with 503.
	barrierScopeAuthz qrdbsdk.Authorizer

	// ceremonySvc serves /api/v1/init endpoints. Wired by lifecycle via
	// SetCeremonyService once the qrdb SDK CeremonyClient is available.
	// Nil before wiring; the handlers report 503 Service Unavailable.
	ceremonySvc service.CeremonyServicer

	// bootstrapSvc serves /api/v1/bootstrap endpoints. Wired by lifecycle
	// via SetBootstrapService once the trust bootstrap subsystem is online.
	// Nil before wiring; the handlers report 503 Service Unavailable.
	bootstrapSvc service.BootstrapServicer

	// sessionPresence records operator_joined / operator_left for live
	// session-event attachments (handlers_session_events.go). It is owned by
	// the server because presence is per-process state shared by every
	// concurrent attach, and it must be flushed on shutdown so a restart
	// never leaves phantom operators in a transcript.
	sessionPresence *orchestration.SessionPresence

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

	// Built before the router, and its error is fatal: a misspelled proxy entry
	// silently dropped would leave the deployment trusting fewer proxies than
	// the operator wrote, and the symptom -- every client attributed to the
	// load balancer -- looks like a different bug entirely.
	resolver, err := clientip.NewResolver(config.TrustedProxies)
	if err != nil {
		return nil, err
	}

	s := &Server{
		config:    config,
		clientIP:  resolver,
		service:   svc,
		logger:    log.WithField("component", "rest-api"),
		startTime: time.Now(),
	}
	if limit := config.EffectiveRateLimit(); limit > 0 {
		s.rateLimit = RateLimitMiddleware(limit)
	}
	s.sessionPresence = orchestration.NewSessionPresence(orchestration.DefaultPresenceLinger, s.warnSessionStream)

	// Default router: single-shard routing to shard 1
	s.shardRouter = routing.New()

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
	// FIRST, because everything below attributes a request to an address:
	// TracingMiddleware records http.client_ip, LoggingMiddleware and the shard
	// log record ip_address, RateLimitMiddleware keys its buckets on it, and
	// the OIDC handlers write it into audit records. Resolving here means they
	// all read one decision instead of each re-reading the headers.
	r.Use(ClientIPMiddleware(s.clientIP))
	r.Use(TracingMiddleware())
	r.Use(RequestIDMiddleware())
	r.Use(SecurityHeadersMiddleware())
	r.Use(RecoveryMiddleware(s.logger))
	r.Use(LoggingMiddleware(s.logger))
	r.Use(MetricsMiddleware())

	// Sealed-window gate: while the storage barrier is sealed, every request
	// not on the canonical allowlist is rejected with 503 + qrdb's sealed
	// envelope. Installed BEFORE the authz gates: while sealed there is no
	// OIDC provider to validate tokens against, and the allowlisted verbs
	// mirror qrdb's pre-auth sealed surface (handlers enforce their own
	// credentials, e.g. officer cert + nonce checks). Pass-through when the
	// probe reports unsealed.
	if s.sealedProbe != nil {
		r.Use(qrdbsdk.SealedWindowMiddleware(s.sealedProbe, s.sealedAllowlist))
	}

	// CORS middleware (if origins configured)
	if len(s.config.CORSOrigins) > 0 {
		r.Use(CORSMiddleware(s.config.CORSOrigins))
	}

	// Timeout middleware
	if s.config.RequestTimeout > 0 {
		r.Use(TimeoutMiddleware(s.config.RequestTimeout))
	}

	// Rate limiting middleware: on unless the operator opted out. The
	// endpoint-scoped OAuth2 limiter (RateLimitOAuth2ClientAuth) applies on
	// top of this one, as defense in depth. The limiter instance is shared
	// with BindRoute, so module routes spend the same per-client budget.
	if s.rateLimit != nil {
		r.Use(s.rateLimit)
	}

	// Route registration is table-driven: Routes() (routes.go) is the single
	// source of truth for the REST surface owned by this server's
	// registration, shared with the unified-API-server ServiceModule
	// (pkg/apimodule RegisterHTTP). Routes are grouped by middleware profile;
	// a route whose Enabled guard reports false is not served. The IAM/OIDC
	// plane, the raw ACME directory, docs, debug, and UI mounts below keep
	// their dedicated registration owners.
	s.registerRoutes(r)

	// ACME RFC 8555 directory endpoints. Mounted at the router ROOT (NOT
	// under /api/v1) so the directory URL the server advertises
	// (https://host:port/acme/directory) follows the RFC 8555 convention.
	// These delegate to the composed go-xkms RestServicer; they respond 503
	// when ACME is not configured.
	registerACMEDirectoryRoutes(s, r)

	// IAM routes
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

// registerRoutes registers the full table-driven route surface onto r: the
// /api/v1 group with the shared protected-API authorization chain and its
// two body-limit sub-groups, plus the three qrdb-wire root mounts that
// remain load-bearing for the standalone sealed-window REST listener
// (GroupBarrierWire, GroupCeremonyWire, GroupInitWire — see the RouteGroup
// doc comment in routes.go). The /v1/credentials, /users, and
// /v1/users/factors wire mounts were removed once go-qrdb's unified REST
// server started serving those paths natively on the same listener —
// quicbaas re-serving them would only shadow or race qrdb's own handlers,
// and none of the three is reachable pre-unseal. An unmatched path under
// any of these mounts still traverses the mount's middleware (401 before
// 404), exactly as before the table-driven conversion. The per-request PEP
// mapping inside the chain remains the authorization decision point for
// this legacy listener; the table's derived Auth / Resource / Action
// columns feed the unified server's per-route registration (pkg/apimodule).
func (s *Server) registerRoutes(r chi.Router) {
	byGroup := map[RouteGroup][]RouteSpec{}
	for _, spec := range Routes() {
		if spec.Enabled != nil && !spec.Enabled(s) {
			continue
		}
		byGroup[spec.Group] = append(byGroup[spec.Group], spec)
	}

	present := map[string]struct{}{}
	for _, spec := range Routes() {
		present[spec.Method+" "+spec.Pattern] = struct{}{}
	}

	// register binds one spec, preserving the historical chi mount matching:
	// a collection route registered as Get("/") inside a Route("/parent")
	// mount also matched the bare /parent path (the mount dispatches it as
	// "/"), so every flattened trailing-slash pattern registers a bare
	// alias unless the table already carries a distinct bare route. A bare
	// mount-root pattern ("/" relative to a wire mount) needs no alias: the
	// enclosing r.Route mount dispatches the bare prefix itself.
	register := func(g chi.Router, spec RouteSpec, prefix string) {
		h := spec.Handler(s)
		rel := strings.TrimPrefix(spec.Pattern, prefix)
		g.MethodFunc(spec.Method, rel, h)
		if bare := strings.TrimSuffix(rel, "/"); bare != rel && bare != "" {
			if _, taken := present[spec.Method+" "+strings.TrimSuffix(spec.Pattern, "/")]; !taken {
				g.MethodFunc(spec.Method, bare, h)
			}
		}
	}

	// /api/v1: the shared authorization chain wraps both body-limit groups.
	r.Route("/api/v1", func(api chi.Router) {
		// Standard API routes with the default body size limit.
		api.Group(func(g chi.Router) {
			g.Use(BodyLimitMiddleware(s.config.MaxBodySize))
			for _, spec := range byGroup[GroupAPI] {
				register(g, spec, "/api/v1")
			}
			// Prometheus scrape endpoint: a multi-method http.Handler
			// mount, registered directly because the table is
			// http.HandlerFunc-shaped (GET /metrics/performance rides the
			// table like every other route).
			g.Route("/metrics", func(m chi.Router) {
				m.Handle("/", promhttp.HandlerFor(
					metrics.Registry(),
					promhttp.HandlerOpts{
						EnableOpenMetrics: true,
					},
				))
			})
		})

		// Upload-heavy routes with the larger body size limit.
		api.Group(func(g chi.Router) {
			g.Use(BodyLimitMiddleware(s.config.MaxUploadBodySize))
			for _, spec := range byGroup[GroupAPIUpload] {
				register(g, spec, "/api/v1")
			}
		})
	})

	// qrdb-wire root mounts (outside the /api/v1 chi group): each installs
	// the shared protected-API authorization chain itself. These three
	// groups (unlike the credential/users/user-factors wire groups that
	// were removed once go-qrdb's unified server took over serving them)
	// stay because the standalone sealed-window REST listener
	// (pkg/server/sealed_window_rest.go) binds THIS router directly, before
	// the unified server exists, and pkg/api/sealedwindow's REST allowlist
	// requires both the /v1/barrier|ceremony/* wire paths and their
	// /api/v1/barrier|init/* mirrors to be routable.
	for _, group := range []RouteGroup{
		GroupBarrierWire, GroupCeremonyWire, GroupInitWire,
	} {
		specs := byGroup[group]
		if len(specs) == 0 {
			continue
		}
		prefix := routeGroupMounts[group]
		r.Route(prefix, func(g chi.Router) {
			for _, spec := range specs {
				register(g, spec, prefix)
			}
		})
	}
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

// handleSecretsPutAction dispatches PUT /api/v1/secrets/* by suffix, mirroring
// handleSecretsPostAction. A secret id is a hierarchical path, so the route has
// to be a wildcard and the sub-resource has to be read off the end of it.
//
// Only /status is defined today; anything else is refused rather than falling
// through to a default, so a caller mistyping the sub-resource is told so
// instead of having some other secret operation performed on their behalf.
func (s *Server) handleSecretsPutAction(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	if strings.HasSuffix(path, "/status") {
		s.HandleUpdateSecretStatus(w, r)
		return
	}
	RespondNotFound(w, r, "unknown secret sub-resource; PUT supports /status")
}

// SetShardRouter overrides the default shard router (for tests or custom routing).
func (s *Server) SetShardRouter(r interface{ Route(string, uint64) uint64 }) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shardRouter = r
}

// SetClusterSecretHandler registers the cluster CA secret-delivery handler
// (tpm-kek). It must be called before setupRouter mounts routes. Nil leaves the
// endpoint unmounted (barrier/hsm).
func (s *Server) SetClusterSecretHandler(h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clusterSecretHandler = h
}

// SetSDLCWebhookHandler registers the forge webhook ingress handler.
//
// Injected by the SDLC module at boot rather than constructed here, because the
// handler needs the module's stores and secret resolution — none of
// which the REST layer knows about, and all of which would tie this package to
// pkg/sdlc if it built them.
func (s *Server) SetSDLCWebhookHandler(h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sdlcWebhookHandler = h
}

// HandleSDLCWebhook serves the forge webhook ingress route.
//
// It answers 503 when the module is not running, NOT 404. The route exists in
// every build, so 404 would claim the endpoint is absent when it is merely
// disabled — and the difference is behavioural rather than cosmetic: a forge
// RETRIES a 503 and gives up on a 404. Retrying is what we want while an
// operator turns the module on, and giving up is what we want never.
func (s *Server) HandleSDLCWebhook(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	handler := s.sdlcWebhookHandler
	s.mu.RUnlock()

	if handler == nil {
		http.Error(w, "the sdlc module is not running on this node", http.StatusServiceUnavailable)
		return
	}
	handler(w, r)
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

// SetTenantBarrierService installs the per-tenant barrier facade used by the
// /api/v1/tenants/{id}/barrier handlers. Lifecycle wiring calls this once the
// qrdb SDK TenantClient is available. Passing nil disables the surface
// (handlers report 503).
func (s *Server) SetTenantBarrierService(svc service.TenantBarrierServicer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tenantBarrierSvc = svc
}

// SetBarrierScopeAuthorizer installs the authorizer
// HandleBarrierWireEnableEncryption consults for the scope a request selects:
// seal:enable_encryption against the system tenant for the system barrier,
// tenant_barrier:enable_encryption against the named tenant for a tenant's.
// Lifecycle wiring passes go-qrdb's RBAC authorizer — the same decision point
// every registration-site permission is rendered by. Passing nil makes the
// route fail closed (503). The same authorizer renders the route permission
// in the URL's tenant when a system caller names another tenant on the REST
// IAM routes (urlTenant).
func (s *Server) SetBarrierScopeAuthorizer(authz qrdbsdk.Authorizer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.barrierScopeAuthz = authz
}

// barrierScopeAuthorizer returns the wired scope authorizer under the read
// lock, or nil when not yet wired.
func (s *Server) barrierScopeAuthorizer() qrdbsdk.Authorizer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.barrierScopeAuthz
}

// barrierService returns the wired system barrier facade under the read lock,
// or nil when not yet wired. Use this instead of reading s.barrierSvc directly.
func (s *Server) barrierService() service.BarrierServicer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.barrierSvc
}

// tenantBarrierService returns the wired per-tenant barrier facade under the
// read lock, or nil when not yet wired.
func (s *Server) tenantBarrierService() service.TenantBarrierServicer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tenantBarrierSvc
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
func (s *Server) SetBootstrapService(svc service.BootstrapServicer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bootstrapSvc = svc
}

// SetModuleTokenAuthorizer installs the platform-token Validator
// AuthenticateHTTP (auth_support.go) authenticates against — the seam
// backing the module's qrdb ModuleAuthFallback (pkg/apimodule
// module_http.go) for a caller that reaches a module route WITHOUT a
// qrdb-authenticated identity. authorizer is typically the central
// platform-token Validator (pkg/server buildPlatformValidator) — the SAME
// seam the gRPC/QUIC/MCP transports already share — so a platform-JWT
// caller is authenticated here and then authorized by qrdb's ONE RBAC
// authorizer (store-resolved roles unioned with the token's
// ScopePermissions, go-qrdb v0.5.6-alpha+), exactly like an mTLS identity.
//
// Passing nil makes AuthenticateHTTP fail closed
// (ModuleAuthorizerRequiredError): a platform-token caller with no qrdb
// identity is denied rather than silently admitted.
func (s *Server) SetModuleTokenAuthorizer(authorizer ModuleTokenAuthorizer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.moduleAuthorizer = authorizer
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
	//     is the mode quicbaasd uses so the /api/v1/fleet/enroll
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

	// Cancel the timers armed for unfinished self-registrations, so none fires
	// against a store this server no longer serves.
	if s.registrationSweeper != nil {
		s.registrationSweeper.stop()
	}

	// Commit any deferred operator departures before the listener goes away,
	// so an operator who was attached at shutdown is recorded as having left
	// rather than lingering in the transcript forever.
	s.sessionPresence.Close()

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

// Addr returns the actual network address the server's listener is bound
// to, or nil if the server has not been started. This is the canonical way
// to discover the OS-assigned port when the server was configured to listen
// on port 0 (ephemeral), since the configured Address still reads ":0".
func (s *Server) Addr() net.Addr {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
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
