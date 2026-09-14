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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// installRemoteBackendResolver wires the BackendDispatcher's optional
// resolver that routes daemon-kind registrations to a RemoteACPBackend. When
// the resolver returns (backend, true), the orchestrator dispatches through
// it; otherwise the dispatcher falls back to local backends registered at
// NewAgentService. The resolver is the runtime glue that turns the registry
// + daemon-side gRPC server into a working remote execution path.
//
// Prerequisites:
//   - s.agentRegistry is non-nil (set during IAM init; see iam_init.go)
//   - s.orchestratorDaemonClient is non-nil (bootstrapped during IAM init)
//
// When either is missing we log and skip resolver installation — the central
// continues to run with only local backends.
func (s *Server) installRemoteBackendResolver(agentSvc orchestration.AgentService) {
	if s.agentRegistry == nil {
		s.logger.Warn("Remote backend resolver not installed: agent registry unavailable")
		return
	}
	if s.orchestratorDaemonClient == nil {
		s.logger.Warn("Remote backend resolver not installed: orchestrator daemon client not bootstrapped")
		return
	}
	wirer, ok := agentSvc.(interface {
		SetRemoteBackendResolver(orchestration.RemoteBackendResolver)
	})
	if !ok {
		s.logger.Warn("Remote backend resolver not installed: agent service lacks SetRemoteBackendResolver")
		return
	}

	adapter := logger.NewAdapter(s.logger)
	caCertPath := filepath.Join(s.config.Storage.DataDir, "certs", "ca-chain.crt")
	pool := newRemoteBackendPool(s.agentRegistry, s.orchestratorDaemonClient, s.centralTokenEndpoint(), caCertPath, s.config.Orchestration.HostAffinity, adapter)

	// Wire the embedded dialer registry so in-process agents bypass TCP.
	// Initialized here so the pool is ready before embedded agent startup;
	// the registry itself is populated later by installEmbeddedBackendDialer.
	if s.embeddedDialerRegistry == nil {
		s.embeddedDialerRegistry = NewEmbeddedDialerRegistry()
	}
	pool.embeddedDialers = s.embeddedDialerRegistry

	wirer.SetRemoteBackendResolver(pool.resolve)
	s.logger.Info("Remote backend resolver installed: daemon-kind registrations will be dispatched via RemoteACPBackend")
}

// centralTokenEndpoint returns the fully-qualified /oauth2/token URL for this
// server. The resolver uses it to mint JWTs for outbound daemon calls using
// the orchestrator's client_credentials app. Falls back to 127.0.0.1 when
// Server.Host is 0.0.0.0 since the central dials itself.
func (s *Server) centralTokenEndpoint() string {
	host := s.config.Server.Host
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	port := s.config.Server.REST.Port
	if port == 0 {
		// Fall back to the grpc port if REST is disabled — we only need to
		// reach the OIDC provider which is registered on both transports.
		port = s.config.Server.GRPC.Port
	}
	return fmt.Sprintf("https://%s:%d/oauth2/token", host, port)
}

// =============================================================================
// remoteBackendPool
// =============================================================================

// remoteBackendPool caches RemoteACPBackend instances so we don't re-dial per
// spawn request. The cache is two levels — daemon host, then the backend name
// the caller asked for — because both axes vary independently: one daemon
// serves several backends, and each backend name needs its own instance to
// stamp on the wire (go-quicbaas#937). Cache entries are evicted when the
// backing registration goes offline (via heartbeat expiry detected on next
// resolve call) or is replaced by a new registration on the same host.
type remoteBackendPool struct {
	registry    orchestration.AgentRegistry
	centralCred *OrchestratorDaemonClient
	tokenURL    string
	caCertPath  string
	logger      interfaces.Logger

	// hostAffinityCfg carries the operator-level enforcement default for
	// host_affinity hints. Per-request HostAffinityStrict overrides via
	// context still take precedence; this is the fall-through default.
	hostAffinityCfg config.SchedulerHostAffinityConfig

	// embeddedDialers maps AgentRegistration.Host to a bufconn dialer for
	// embedded agents. When non-nil and a dialer is found for a host, the
	// pool bypasses the TCP/TLS dial path entirely.
	embeddedDialers *EmbeddedDialerRegistry

	mu sync.Mutex
	// entries maps daemon host -> the backends constructed for it.
	entries map[string]*pooledDaemon
}

// pooledDaemon holds every backend instance constructed for one daemon host.
//
// THE HOST IS THE INVALIDATION UNIT, THE BACKEND NAME IS THE CACHE AXIS. A
// daemon replaced at the same address (new registration id) invalidates all of
// its backends at once, which a flat map keyed by host+backend could only do by
// scanning; a lookup for a second backend on a live daemon adds an instance
// beside the first rather than replacing it.
type pooledDaemon struct {
	registrationID string
	// backends maps the REQUESTED backend name -> the instance that serves it.
	backends map[string]*orchestration.RemoteACPBackend
}

func newRemoteBackendPool(
	registry orchestration.AgentRegistry,
	cred *OrchestratorDaemonClient,
	tokenURL string,
	caCertPath string,
	hostAffinityCfg config.SchedulerHostAffinityConfig,
	logger interfaces.Logger,
) *remoteBackendPool {
	return &remoteBackendPool{
		registry:        registry,
		centralCred:     cred,
		tokenURL:        tokenURL,
		caCertPath:      caCertPath,
		hostAffinityCfg: hostAffinityCfg,
		logger:          logger,
		entries:         map[string]*pooledDaemon{},
	}
}

// resolve implements orchestration.RemoteBackendResolver. Returns (backend,
// true) when a registered daemon satisfies all of: serving the requested
// backend, host-affinity, and GPU constraints; returns (nil, false) to let
// the dispatcher fall through to local backends.
//
// Filtering rules applied in order:
//  1. Backend filter: agent must serve the named backend, primary or capability
//     (orchestration.AgentServesBackend — the one predicate every backend
//     filter in the stack shares).
//  2. Host-affinity filter (when non-empty): Name, Host exact match, or Host prefix match.
//  3. GPU filter (when GpuRequired): agent Capabilities must contain "gpu".
//
// When all online agents exist but none survives filtering, a
// NoMatchingAgentError is logged; (nil, false) is still returned so the
// dispatcher can surface BackendNotFoundError to the caller.
func (p *remoteBackendPool) resolve(ctx context.Context, backend, projectID string) (orchestration.AgentBackend, bool) {
	tenantID := orchestration.TenantFromContext(ctx)
	if tenantID == "" {
		return nil, false
	}
	hostAffinity := orchestration.HostAffinityFromContext(ctx)
	gpuRequired := orchestration.GpuRequiredFromContext(ctx)

	// Pull all tenant+backend matches across multiple status values; a
	// just-enrolled daemon is in AgentStatusRegistered until its first
	// heartbeat, which would otherwise hide it from dispatch.
	var allRegs []*orchestration.AgentRegistration
	for _, status := range []orchestration.AgentStatus{
		orchestration.AgentStatusRunning,
		orchestration.AgentStatusIdle,
		orchestration.AgentStatusRegistered,
	} {
		regs, err := p.registry.ListAgents(ctx, tenantID, &orchestration.AgentFilter{
			Backend: backend,
			Status:  status,
		})
		if err != nil {
			p.logger.Warnf("remote resolver: list agents failed: tenant=%s backend=%s status=%s err=%v",
				tenantID, backend, status, err)
			continue
		}
		allRegs = append(allRegs, regs...)
	}
	if len(allRegs) == 0 {
		return nil, false
	}

	reg, constraint := p.pickDaemon(allRegs, backend, hostAffinity, gpuRequired)
	if reg == nil {
		noMatchErr := buildNoMatchError(backend, hostAffinity, gpuRequired, constraint)
		// In hard mode (operator default OR per-request override) and when
		// the failure was specifically the host-affinity filter, stash the
		// typed error on the context so the public-API handler can surface
		// 400 no_matching_agent rather than the soft 404 fall-through.
		strict := orchestration.HostAffinityStrictFromContext(ctx) || p.hostAffinityCfg.IsHard()
		if strict && constraint == "host_affinity" {
			noMatchErr.Detail = "host " + hostAffinity + " is offline"
			orchestration.StashResolveError(ctx, noMatchErr)
		}
		p.logger.Warnf("remote resolver: %s tenant=%s strict=%v", noMatchErr.Error(), tenantID, strict)
		return nil, false
	}
	return p.backendFor(ctx, reg, backend)
}

// pickDaemon filters a registration slice to the first daemon-kind agent
// that satisfies all three constraints: backend, host affinity, and GPU.
// It returns the matching registration and an empty constraint name on
// success. On failure it returns (nil, <constraint>) where constraint names
// the first filter that eliminated all candidates, helping callers build
// actionable error messages.
//
// Constraint evaluation order mirrors the priority operators expect:
//  1. backend — the agent must serve the named backend
//  2. host_affinity — Name, Host exact match, or Host prefix match
//  3. gpu — Capabilities must contain "gpu"
func (p *remoteBackendPool) pickDaemon(
	regs []*orchestration.AgentRegistration,
	backend, hostAffinity string,
	gpuRequired bool,
) (reg *orchestration.AgentRegistration, failedConstraint string) {
	passedBackend := false
	passedAffinity := false

	for _, r := range regs {
		if r == nil {
			continue
		}
		kind := r.EffectiveKind()
		if kind != orchestration.AgentKindDaemon && kind != orchestration.AgentKindEmbedded {
			continue
		}
		if !orchestration.AgentServesBackend(r, backend) {
			continue
		}
		passedBackend = true

		if hostAffinity != "" && r.Name != hostAffinity && r.Host != hostAffinity && !strings.HasPrefix(r.Host, hostAffinity+":") {
			continue
		}
		passedAffinity = true

		if gpuRequired && !capabilityContains(r.Capabilities, "gpu") {
			continue
		}

		return r, ""
	}

	// Identify the earliest constraint that blocked all candidates.
	if !passedBackend {
		return nil, "backend"
	}
	if hostAffinity != "" && !passedAffinity {
		return nil, "host_affinity"
	}
	return nil, "gpu"
}

// buildNoMatchError constructs a NoMatchingAgentError that names the
// unsatisfied constraint and all active filters for clear operator messaging.
func buildNoMatchError(backend, hostAffinity string, gpuRequired bool, constraint string) *orchestration.NoMatchingAgentError {
	detail := "no online agents serve backend=" + backend
	if hostAffinity != "" {
		detail += " and host_affinity=" + hostAffinity
	}
	if gpuRequired {
		detail += " and have capability=gpu"
	}
	return &orchestration.NoMatchingAgentError{
		Constraint: constraint,
		Detail:     detail,
	}
}

// capabilityContains reports whether the capabilities slice advertises the
// named trait. The backend axis is answered by
// orchestration.AgentServesBackend; this is the trait axis — "gpu" — which a
// registration carries beside its backends rather than as one of them.
func capabilityContains(caps []string, backend string) bool {
	for _, c := range caps {
		if c == backend {
			return true
		}
	}
	return false
}

// backendFor returns a cached *RemoteACPBackend for the (daemon, backend)
// pair, or constructs one on first use.
//
// BOTH HALVES OF THE KEY ARE LOAD-BEARING. The host is the dialable target;
// the backend is the name the instance stamps on every RPC. Keying on the host
// alone handed a request for "codex" the instance built for "claude", which
// then asked the daemon for claude — a working session running the wrong model
// family, with the session record still saying codex (go-quicbaas#937).
//
// A registration that does not serve the requested backend is refused rather
// than served by its primary: NewRemoteACPBackend returns a typed
// NoMatchingAgentError, which is stashed on the context so the public API
// surfaces the refusal instead of silently falling through to a local backend.
func (p *remoteBackendPool) backendFor(
	ctx context.Context,
	reg *orchestration.AgentRegistration,
	backend string,
) (orchestration.AgentBackend, bool) {
	if cached, ok := p.cachedBackend(reg, backend); ok {
		return cached, true
	}

	// For embedded agents, use the bufconn dialer so RPCs stay in-process.
	// The dialer is keyed by Host, which is the logical agent name set at
	// registration time (e.g. the hostname or host-affinity tag) — the backend
	// axis of the cache key above does not reach this lookup, so an embedded
	// daemon serving several backends resolves the same dialer for each.
	var connDialer func(ctx context.Context, target string) (*grpc.ClientConn, error)
	isEmbedded := false
	if p.embeddedDialers != nil {
		if d, ok := p.embeddedDialers.Lookup(reg.Host); ok {
			connDialer = d
			isEmbedded = true
		}
	}

	// Embedded agents run in-process with a synthetic identity set on their
	// GRPCSpawnServer (see embedded_agent.go buildEmbeddedAgentIdentity).
	// Their bufconn gRPC server has no bearer-token interceptor, so the token
	// fetch is unnecessary and causes TLS handshake noise when the loopback
	// client tries to hit /oauth2/token. Skip the token source for embedded
	// agents; only remote daemon backends need OAuth2 bearer tokens.
	var tokenSource oauth2.TokenSource
	if !isEmbedded {
		tokenSource = newCentralOAuth2TokenSource(p.centralCred, p.tokenURL, p.caCertPath)
	}

	built, err := orchestration.NewRemoteACPBackend(orchestration.RemoteACPBackendDeps{
		Registration: reg,
		Backend:      backend,
		TokenSource:  tokenSource,
		ConnDialer:   connDialer,
		Logger:       p.logger,
	})
	if err != nil {
		p.logger.Warnf("remote resolver: build RemoteACPBackend failed: host=%s backend=%s err=%v",
			reg.Host, backend, err)
		// A refusal the caller can act on travels; a construction fault does
		// not become a silent fall-through to some other backend.
		var noMatch *orchestration.NoMatchingAgentError
		if errors.As(err, &noMatch) {
			orchestration.StashResolveError(ctx, noMatch)
		}
		return nil, false
	}

	stored := p.storeBackend(reg, backend, built)
	p.logger.Infof("remote resolver: routed backend=%q tenant=%s → daemon=%s host=%s primary=%q",
		backend, reg.TenantID, reg.Name, reg.Host, reg.Backend)
	return stored, true
}

// cachedBackend returns the pooled instance for the (daemon, backend) pair.
// A registration id that no longer matches means the daemon at this host was
// replaced, so EVERY backend cached for the host is dropped — invalidation is
// per daemon even though lookup is per backend.
//
// The dropped instances are not closed here: a session spawned before the
// replacement still holds its instance and its conn, and closing the pool
// underneath it would fail a live turn. They are released when the holder
// calls CloseAll.
func (p *remoteBackendPool) cachedBackend(
	reg *orchestration.AgentRegistration,
	backend string,
) (*orchestration.RemoteACPBackend, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[reg.Host]
	if !ok {
		return nil, false
	}
	if entry.registrationID != reg.ID {
		delete(p.entries, reg.Host)
		return nil, false
	}
	cached, ok := entry.backends[backend]
	return cached, ok
}

// storeBackend records a freshly constructed instance and returns the one the
// pool now holds. A concurrent caller that constructed the same pair first
// wins, so every caller for one (daemon, backend) shares a single conn pool.
func (p *remoteBackendPool) storeBackend(
	reg *orchestration.AgentRegistration,
	backend string,
	built *orchestration.RemoteACPBackend,
) *orchestration.RemoteACPBackend {
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[reg.Host]
	if !ok || entry.registrationID != reg.ID {
		entry = &pooledDaemon{
			registrationID: reg.ID,
			backends:       map[string]*orchestration.RemoteACPBackend{},
		}
		p.entries[reg.Host] = entry
	}
	if existing, ok := entry.backends[backend]; ok {
		return existing
	}
	entry.backends[backend] = built
	return built
}

// =============================================================================
// centralOAuth2TokenSource — oauth2.TokenSource implementation
// =============================================================================

// centralOAuth2TokenSource implements oauth2.TokenSource by calling the
// central's own /oauth2/token endpoint with client_credentials.
//
// Using the OIDC network round-trip (rather than minting the JWT in-process)
// keeps this code honest: the central authenticates to daemons exactly the
// way any third-party client would, which means daemons never need special
// trust of the central beyond the standard JWT validation.
type centralOAuth2TokenSource struct {
	client     *http.Client
	cred       *OrchestratorDaemonClient
	tokenURL   string
	loopback   bool
	caCertPath string

	mu        sync.Mutex
	cached    *oauth2.Token
	expiresAt time.Time
}

// newCentralOAuth2TokenSource constructs the source. The http.Client is the
// default client — we rely on the central's cert being self-trusted via the
// system trust store. For loopback-local token endpoint calls (central
// talking to itself on 127.0.0.1) we build a dedicated http.Client that
// trusts the daemon's on-disk ca-chain.crt, since the cert was issued by
// the daemon's own CA and is not in the system trust store.
func newCentralOAuth2TokenSource(cred *OrchestratorDaemonClient, tokenURL, caCertPath string) *centralOAuth2TokenSource {
	return &centralOAuth2TokenSource{
		client:     &http.Client{Timeout: 15 * time.Second},
		cred:       cred,
		tokenURL:   tokenURL,
		loopback:   isLoopback(tokenURL),
		caCertPath: caCertPath,
	}
}

// Token implements oauth2.TokenSource.
func (t *centralOAuth2TokenSource) Token() (*oauth2.Token, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cached != nil && time.Until(t.expiresAt) > 5*time.Second {
		return t.cached, nil
	}
	tok, err := t.refresh()
	if err != nil {
		return nil, err
	}
	t.cached = tok
	t.expiresAt = tok.Expiry
	return tok, nil
}

// refresh performs the client_credentials exchange. Uses HTTP Basic auth per
// RFC 6749 §2.3.1 since that is the form pkg/api/rest/iam_oidc.go's Token
// handler prefers.
func (t *centralOAuth2TokenSource) refresh() (*oauth2.Token, error) {
	body := url.Values{}
	body.Set("grant_type", "client_credentials")
	body.Set("scope", strings.Join(t.cred.Scopes, " "))

	req, err := http.NewRequest(http.MethodPost, t.tokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(t.cred.ClientID, t.cred.ClientSecret)

	client := t.client
	if t.loopback {
		lbClient, lbErr := loopbackTrustedClient(t.caCertPath)
		if lbErr != nil {
			return nil, &ServerCauseError{Message: "loopback token client", Cause: lbErr}
		}
		client = lbClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &ServerMessageError{Message: fmt.Sprintf("central token exchange: status=%d", resp.StatusCode)}
	}

	// Minimal decode — we only need access_token + expires_in.
	var out struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	if err := decodeJSON(resp.Body, &out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, errors.New("central token exchange: empty access_token")
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &oauth2.Token{
		AccessToken: out.AccessToken,
		TokenType:   out.TokenType,
		Expiry:      time.Now().Add(ttl),
	}, nil
}

// decodeJSON wraps json.Decode with a 1 MiB body cap so a compromised or
// pathological token endpoint can't exhaust memory.
func decodeJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(v)
}

// isLoopback reports whether the URL host resolves to 127.0.0.1 / ::1. Used
// to decide whether to accept the self-issued cert on the loopback token
// endpoint. Non-loopback token URLs go through the normal system roots.
func isLoopback(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// loopbackTrustedClient returns an http.Client that trusts the daemon's own
// CA chain from disk. Used when the central is talking to itself on 127.0.0.1
// (the cert is issued by the daemon's CA). QuicBaaS bans insecure TLS, so we
// load the on-disk ca-chain.crt explicitly rather than skip verification.
//
// The caCertPath is set on the remoteBackendPool at construction time from
// the server config's data-dir/certs/ca-chain.crt. Empty path returns an
// error — there is no fallback to insecure mode.
func loopbackTrustedClient(caCertPath string) (*http.Client, error) {
	if caCertPath == "" {
		return nil, &ServerMessageError{Message: "loopbackTrustedClient: caCertPath is required (QuicBaaS bans insecure TLS)"}
	}
	caPEM, err := os.ReadFile(caCertPath) //nolint:gosec // caCertPath comes from server config
	if err != nil {
		return nil, &ServerCauseError{Message: fmt.Sprintf("read ca-chain %s", caCertPath), Cause: err}
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, &ServerMessageError{Message: fmt.Sprintf("no PEM certs in ca-chain %s", caCertPath)}
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS13,
			},
		},
	}, nil
}
