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

// Package agent — enrollment.go implements the production Enroller.
//
// Registration is authenticated by the daemon's mTLS client certificate.
// On first start the daemon calls:
//
//	POST <ServerAddr>/api/v1/agents/register
//	  (TLS handshake uses AgentCertFile + AgentKeyFile; server verified
//	   against ServerCAFile)
//	Content-Type: application/json
//	Body: {"tenant_id":"...","bind_addr":"...","sandbox_policy":"...",
//	       "hostname":"...","metadata":{...}}
//	201 → {"client_id","client_secret","agent_id",
//	       "service_account_id","role","scopes":[...]}
//	401 → ErrEnrollmentRejected
//
// On every start (first and subsequent) the daemon exchanges its cached
// client credentials for a short-lived JWT via the Client Credentials
// grant:
//
//	POST <ServerAddr>/oauth2/token
//	Authorization: Basic base64(client_id:client_secret)
//	Content-Type:  application/x-www-form-urlencoded
//	Body: grant_type=client_credentials&scope=<space-separated>
//	200 → {"access_token","token_type","expires_in","scope"}
//
// On success the persisted credentials file at <WorkDir>/credentials.json
// is written atomically with mode 0600. Subsequent daemon starts skip the
// registration POST entirely and go straight to token exchange.
package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// defaultRegisterPath is the relative URL the production enroller POSTs
// to on first start. The mTLS client certificate is presented on the TLS
// handshake; the body carries the daemon's self-declared capabilities.
const defaultRegisterPath = "/api/v1/agents/register"

// defaultTokenPath mirrors pkg/iam/oidc.go's TokenEndpoint so a daemon can
// reach the same OIDC provider that signed its registration.
const defaultTokenPath = "/oauth2/token" //nolint:gosec // G101: a URL path, not a credential

// defaultTokenMTLSPath is the mTLS token endpoint (RFC 8705). The agent posts
// here when it has an mtls_client_id and presents its agent.crt on the TLS
// handshake instead of sending a client_secret.
const defaultTokenMTLSPath = "/oauth2/token-mtls" //nolint:gosec // G101: a URL path, not a credential

// credentialsFileName is the on-disk filename for persisted OAuth2 client
// credentials inside cfg.WorkDir. 0600 permissions are enforced.
const credentialsFileName = "credentials.json"

// defaultEnrollmentTimeout bounds each individual HTTP call to the server
// so a misbehaving proxy cannot wedge daemon startup forever.
const defaultEnrollmentTimeout = 30 * time.Second

// persistedCredentials is the on-disk shape of credentials.json. It is
// also the JSON shape returned by the server's registration endpoint.
//
// EventIngestAddr is the gRPC URL the daemon dials for the PushEvents
// bidi-stream. The server sets it from its gRPC listener config so the
// daemon never has to guess; an empty value means the server has no
// gRPC listener exposed and event forwarding is disabled.
//
// MTLSClientID is the client_id of the OAuth2 application registered
// with self_signed_tls_client_auth on the server. When present the agent
// prefers /oauth2/token-mtls (no secret on disk). The legacy ClientID +
// ClientSecret fields remain for backward compatibility with older daemons
// that do not set MTLSClientID.
type persistedCredentials struct {
	ClientID         string   `json:"client_id"`
	ClientSecret     string   `json:"client_secret"`
	AgentID          string   `json:"agent_id"`
	ServiceAccountID string   `json:"service_account_id"`
	Role             string   `json:"role,omitempty"`
	Scopes           []string `json:"scopes"`
	EventIngestAddr  string   `json:"event_ingest_addr,omitempty"`
	MTLSClientID     string   `json:"mtls_client_id,omitempty"`
}

// tokenResponse mirrors the RFC 6749 token response shape emitted by
// pkg/iam/oidc.go's OIDCProvider.Token.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope"`
}

// ProductionEnrollerDeps injects the collaborators needed by
// ProductionEnroller. All fields except Logger are optional — defaults
// are applied in NewProductionEnroller.
type ProductionEnrollerDeps struct {
	// HTTPClient is the transport used to reach the server. When nil a
	// client is constructed from cfg at Enroll time so mTLS is wired
	// through the agent cert/key the caller configured.
	//
	// Tests that already have an httptest server can inject a pre-built
	// client here to bypass the on-disk cert plumbing.
	HTTPClient *http.Client

	// Logger is required; ProductionEnroller does not invent a logger.
	Logger interfaces.Logger

	// Now is the clock used to compute token expiry. Defaults to time.Now.
	// Primarily a test hook.
	Now func() time.Time

	// RegisterPath overrides defaultRegisterPath. Leave empty in production.
	RegisterPath string

	// TokenPath overrides defaultTokenPath. Leave empty in production.
	TokenPath string

	// TokenMTLSPath overrides defaultTokenMTLSPath. Leave empty in production.
	TokenMTLSPath string

	// ServedBackends reports the backend names the daemon actually serves,
	// so the register payload advertises what a task can route to rather
	// than the transport kinds the --backends flag named. Production wires
	// Runtime.Backends here; see the field on ProductionEnroller.
	ServedBackends func() []string
}

// ProductionEnroller implements Enroller using the existing OAuth2 Client
// Credentials flow in pkg/iam/oidc.go. It is safe for concurrent use only
// across disjoint Enroll calls: each call is stateless and serializes all
// on-disk writes through an atomic rename.
type ProductionEnroller struct {
	httpClient    *http.Client
	logger        interfaces.Logger
	now           func() time.Time
	registerPath  string
	tokenPath     string
	tokenMTLSPath string

	// servedBackends reports the backend names the daemon actually serves.
	//
	// A DAEMON MUST ADVERTISE WHAT IT SERVES, NOT HOW IT SERVES IT. The
	// --backends flag names transport kinds ("acp", "ollama"); one "acp"
	// kind constructs the claude, codex and opencode backends, and those
	// three names are what a task routes on. Registering "acp" left every
	// dispatcher that filters by backend name unable to see the daemon, so
	// an sdlc task routed on "claude" fell through to an in-process spawn
	// the runner then refused for having no container (go-quicbaas#681).
	//
	// Nil when the caller has no runtime to ask, in which case the payload
	// advertises the configured kinds, which is all that is known. A source
	// that IS wired is authoritative even when it reports nothing: see
	// advertisedBackends.
	servedBackends func() []string

	// cfg is the validated daemon Config the enroller binds at SetConfig
	// time. Used by Reregister so the heartbeat sender can drive
	// re-enrollment without re-threading the config through every call
	// site. Nil when the enroller has not been bound; Reregister will
	// then return ErrConfigNil.
	cfg *Config
}

// NewProductionEnroller constructs a ProductionEnroller. Logger is
// required. When HTTPClient is nil the enroller builds one per-call from
// the config so every call honours the latest cert on disk (e.g. after
// a rotation).
func NewProductionEnroller(deps ProductionEnrollerDeps) *ProductionEnroller {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	registerPath := deps.RegisterPath
	if registerPath == "" {
		registerPath = defaultRegisterPath
	}
	tokenPath := deps.TokenPath
	if tokenPath == "" {
		tokenPath = defaultTokenPath
	}
	tokenMTLSPath := deps.TokenMTLSPath
	if tokenMTLSPath == "" {
		tokenMTLSPath = defaultTokenMTLSPath
	}
	return &ProductionEnroller{
		httpClient:     deps.HTTPClient, // may be nil — built lazily per-call
		logger:         deps.Logger,
		servedBackends: deps.ServedBackends,
		now:            now,
		registerPath:   registerPath,
		tokenPath:      tokenPath,
		tokenMTLSPath:  tokenMTLSPath,
	}
}

// Enroll implements the Enroller interface. The flow is:
//  1. Attempt to load persisted credentials from <WorkDir>/credentials.json.
//  2. If absent, POST the registration payload to the server over mTLS
//     and persist the returned OAuth2 credentials atomically.
//  3. Exchange credentials for a short-lived JWT. When mtls_client_id is set,
//     the mTLS token endpoint is preferred over the legacy client_secret path.
//  4. Assemble an AgentIdentity from the persisted creds + token response.
func (e *ProductionEnroller) Enroll(ctx context.Context, cfg *Config) (*orchestration.AgentIdentity, error) {
	if cfg == nil {
		return nil, ErrConfigNil
	}
	client, err := e.resolveHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	creds, fromCache, err := e.loadOrBootstrapWithSource(ctx, cfg, client)
	if err != nil {
		return nil, err
	}
	tok, err := e.acquireToken(ctx, cfg, creds, client)
	if err == nil {
		return e.buildIdentity(cfg, creds, tok), nil
	}
	// Boot-time recovery: when the cached credentials no longer
	// authenticate (control plane wiped its IAM store), fall through to a
	// fresh /api/v1/agents/register exchange. Symmetrical to the
	// runtime invalid_client path on CachingTokenSource.
	if !fromCache || !isInvalidClientTokenExchangeError(err) {
		return nil, err
	}
	if e.logger != nil {
		e.logger.Infof("agent enroller: cached credentials repudiated by control plane (invalid_client); re-enrolling from scratch")
	}
	return e.ReregisterWithConfig(ctx, cfg)
}

// acquireToken prefers the mTLS token endpoint when mtls_client_id is present
// in the persisted credentials, falling back to the legacy client_secret path.
func (e *ProductionEnroller) acquireToken(ctx context.Context, cfg *Config, creds *persistedCredentials, client *http.Client) (*tokenResponse, error) {
	if creds.MTLSClientID != "" {
		tok, err := e.exchangeTokenMTLS(ctx, cfg, creds, client)
		if err == nil {
			return tok, nil
		}
		// If the mTLS path fails (e.g., server not upgraded yet), fall back
		// to client_secret. Log the degradation so operators notice.
		if e.logger != nil {
			e.logger.Warnf("agent enroller: mTLS token exchange failed (%v); falling back to client_secret", err)
		}
	}
	return e.exchangeToken(ctx, cfg, creds, client)
}

// loadOrBootstrapWithSource is loadOrBootstrap that also reports whether
// the returned credentials came from disk (true) or were minted by a
// fresh bootstrap (false). The Enroll boot-time recovery path uses the
// flag to decide whether re-bootstrap on invalid_client is meaningful:
// repeating bootstrap on a brand-new bootstrap that already failed is
// pointless and would mask the real cause.
func (e *ProductionEnroller) loadOrBootstrapWithSource(ctx context.Context, cfg *Config, client *http.Client) (*persistedCredentials, bool, error) {
	path := credentialsPath(cfg)
	creds, ok, err := readCredentials(path)
	if err != nil {
		return nil, false, err
	}
	if ok {
		if e.logger != nil {
			e.logger.Infof("agent enroller: using cached credentials at %s (agent_id=%s)",
				path, creds.AgentID)
		}
		return creds, true, nil
	}
	creds, err = e.bootstrap(ctx, cfg, client)
	if err != nil {
		return nil, false, err
	}
	if err := writeCredentials(path, creds); err != nil {
		return nil, false, err
	}
	if e.logger != nil {
		e.logger.Infof("agent enroller: persisted new credentials at %s (agent_id=%s)",
			path, creds.AgentID)
	}
	return creds, false, nil
}

// oauthErrInvalidClient is the RFC 6749 §5.2 error code the control plane
// emits when the presented client_secret no longer matches the stored
// hash for the client_id (e.g., after the IAM store was wiped). Both
// the boot-time Enroll() flow and the runtime CachingTokenSource treat
// this code as the canonical signal that the cached credentials are
// dead and a fresh /api/v1/agents/register exchange is required.
const oauthErrInvalidClient = "invalid_client"

// isInvalidClientTokenExchangeError reports whether err is a 401
// /oauth2/token response carrying the OAuth2 invalid_client error
// code. Shared by the boot-time Enroll() recovery path and
// CachingTokenSource.Token() runtime recovery so both flows agree on
// which failure mode warrants client-side re-enrollment.
func isInvalidClientTokenExchangeError(err error) bool {
	var exErr *EnrollmentTokenExchangeError
	if !errors.As(err, &exErr) {
		return false
	}
	if exErr.Status != 401 {
		return false
	}
	return strings.Contains(exErr.Detail, `"error":"`+oauthErrInvalidClient+`"`) ||
		strings.Contains(exErr.Detail, `"error": "`+oauthErrInvalidClient+`"`)
}

// resolveHTTPClient returns the enroller's injected HTTPClient when the
// caller pre-supplied one, or lazily builds a new mTLS-configured client
// from the config on disk.
func (e *ProductionEnroller) resolveHTTPClient(cfg *Config) (*http.Client, error) {
	if e.httpClient != nil {
		return e.httpClient, nil
	}
	return buildMTLSHTTPClient(cfg)
}

// advertisedBackends returns the backend names the register payload declares
// this daemon can run.
//
// A WIRED RUNTIME IS THE ANSWER, INCLUDING WHEN THE ANSWER IS NONE. When the
// caller supplied a served-backend source, its reply is advertised verbatim —
// an empty reply advertises nothing. A daemon whose backend construction
// produced nothing serves nothing, and substituting the configured transport
// kinds there would mint exactly the phantom "acp" record this derivation
// exists to remove (go-quicbaas#681), on the one daemon least able to honour
// it. The register handler and the operator see the fault instead.
//
// The configured kinds are reachable only when no source was wired at all —
// an enroller built without a runtime, which has nothing better to say.
func (e *ProductionEnroller) advertisedBackends(cfg *Config) []string {
	if e.servedBackends != nil {
		return append([]string(nil), e.servedBackends()...)
	}
	kinds := cfg.EffectiveBackends()
	names := make([]string, 0, len(kinds))
	for _, b := range kinds {
		names = append(names, string(b))
	}
	return names
}

// bootstrap performs the POST /api/v1/agents/register exchange. Any
// non-2xx response becomes ErrEnrollmentRejected; transport errors are
// wrapped with the same sentinel so callers can distinguish via errors.Is.
func (e *ProductionEnroller) bootstrap(ctx context.Context, cfg *Config, client *http.Client) (*persistedCredentials, error) {
	endpoint, err := joinURL(cfg.ServerAddr, e.registerPath)
	if err != nil {
		return nil, &EnrollmentRequestError{Op: "build register url", Cause: err}
	}

	hostname, _ := os.Hostname()
	backendNames := e.advertisedBackends(cfg)
	// Advertise GPU capability so the control plane scheduler can filter to
	// GPU-capable daemons when a task or inference request sets GpuRequired.
	if DetectGPU() {
		backendNames = append(backendNames, "gpu")
	}
	// And the signing sockets this host can mount, so a delivery worker can be
	// placed here from the first spawn rather than from the first heartbeat.
	for _, id := range cfg.SigningAgents() {
		backendNames = append(backendNames, orchestration.SigningAgentTrait(id))
	}

	payload := map[string]interface{}{
		"tenant_id":      cfg.TenantID,
		"bind_addr":      cfg.BindAddr,
		"sandbox_policy": cfg.SandboxPolicy.String(),
		"hostname":       hostname,
		"metadata": map[string]string{
			"backends":           strings.Join(backendNames, ","),
			"container_runtime":  string(cfg.EffectiveContainerRuntime()),
			"heartbeat_interval": cfg.EffectiveHeartbeat().String(),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &EnrollmentRequestError{Op: "encode register request", Cause: err}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, &EnrollmentRequestError{Op: "build register request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, &EnrollmentRejectedError{Status: 0, Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &EnrollmentRejectedError{
			Status: resp.StatusCode,
			Detail: strings.TrimSpace(string(detail)),
		}
	}

	var creds persistedCredentials
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&creds); err != nil {
		return nil, &EnrollmentRejectedError{Status: resp.StatusCode, Cause: err}
	}
	if err := validateBootstrapResponse(&creds); err != nil {
		return nil, err
	}
	if creds.Role == "" {
		creds.Role = "agent-runtime"
	}
	return &creds, nil
}

// Reregister forces a fresh /api/v1/agents/register exchange, replacing
// any persisted credentials with the freshly minted ones. Used by the
// HTTPHeartbeatSender when the control plane rejects a heartbeat with 404 and
// server-side auto-recovery has already failed (typically because the
// control plane was reinstalled and lost its CA / signing keys, so the
// agent's mTLS cert no longer derives an AgentID the control plane can match).
//
// Implements the Reregisterer interface so the heartbeat sender can
// drive recovery without coupling to the concrete enroller type.
//
// Side-effects on success:
//   - <WorkDir>/credentials.json is overwritten atomically with the new
//     OAuth2 client_id + client_secret.
//   - A fresh JWT exchange runs and the rebuilt AgentIdentity reflects
//     the new ClientID / ServiceAccountID / Role / Scopes.
//
// On failure no on-disk state changes; the heartbeat sender's failure
// counter increments and the budget guards against infinite retry.
func (e *ProductionEnroller) Reregister(ctx context.Context) (*orchestration.AgentIdentity, error) {
	return e.ReregisterWithConfig(ctx, e.cfg)
}

// ReregisterWithConfig is the variant used when the enroller does not
// hold the daemon config itself. The cmd/quicbaas wiring captures cfg
// at construction time and binds it via SetConfig, but tests may pass
// the cfg explicitly through this method.
func (e *ProductionEnroller) ReregisterWithConfig(ctx context.Context, cfg *Config) (*orchestration.AgentIdentity, error) {
	if cfg == nil {
		return nil, ErrConfigNil
	}
	client, err := e.resolveHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	creds, err := e.bootstrap(ctx, cfg, client)
	if err != nil {
		return nil, err
	}
	if err := writeCredentials(credentialsPath(cfg), creds); err != nil {
		return nil, err
	}
	if e.logger != nil {
		e.logger.Infof("agent enroller: re-registered, persisted fresh credentials at %s (agent_id=%s)",
			credentialsPath(cfg), creds.AgentID)
	}
	tok, err := e.acquireToken(ctx, cfg, creds, client)
	if err != nil {
		return nil, err
	}
	return e.buildIdentity(cfg, creds, tok), nil
}

// SetConfig binds the daemon config the enroller will use for follow-up
// operations like Reregister. Called from the daemon-serve wiring once
// the validated config is available; tests can use ReregisterWithConfig
// directly to avoid the binding step.
func (e *ProductionEnroller) SetConfig(cfg *Config) {
	e.cfg = cfg
}

// RefreshTokenFn returns a closure suitable for use as CachingTokenSource.Refresh.
// It reads persisted credentials and exchanges them for a fresh JWT on
// each call. The returned closure is safe for concurrent callers;
// CachingTokenSource serializes access itself via its internal mutex.
//
// Typical wiring:
//
//	enroller := agent.NewProductionEnroller(deps)
//	src := &agent.CachingTokenSource{Refresh: enroller.RefreshTokenFn(cfg)}
//	sender, _ := agent.NewHTTPHeartbeatSender(agent.HTTPHeartbeatConfig{
//	    ServerAddr: cfg.ServerAddr, TenantID: cfg.TenantID,
//	    HTTPClient: client, Tokens: src, Logger: logger,
//	})
func (e *ProductionEnroller) RefreshTokenFn(cfg *Config) func(ctx context.Context) (string, time.Time, error) {
	return func(ctx context.Context) (string, time.Time, error) {
		path := credentialsPath(cfg)
		creds, ok, err := readCredentials(path)
		if err != nil {
			return "", time.Time{}, err
		}
		if !ok {
			return "", time.Time{}, ErrEnrollmentMissingCredentials
		}
		client, err := e.resolveHTTPClient(cfg)
		if err != nil {
			return "", time.Time{}, err
		}
		tok, err := e.acquireToken(ctx, cfg, creds, client)
		if err != nil {
			return "", time.Time{}, err
		}
		ttl := time.Duration(tok.ExpiresIn) * time.Second
		if ttl <= 0 {
			ttl = 5 * time.Minute
		}
		return tok.AccessToken, e.now().Add(ttl), nil
	}
}

// exchangeTokenMTLS performs the Client Credentials grant at /oauth2/token-mtls.
// The mTLS client certificate on the HTTP transport authenticates the client;
// the client_id is sent in the form body (no client_secret required).
func (e *ProductionEnroller) exchangeTokenMTLS(ctx context.Context, cfg *Config, creds *persistedCredentials, client *http.Client) (*tokenResponse, error) {
	endpoint, err := joinURL(cfg.ServerAddr, e.tokenMTLSPath)
	if err != nil {
		return nil, &EnrollmentTokenExchangeError{Op: "build token-mtls url", Cause: err}
	}

	// No scope parameter: the daemon does not name its own authority. A
	// client_credentials token carries the grant on the application record,
	// so a scope list can only narrow it -- and the list a daemon has on disk
	// is whatever the build that enrolled it advertised. A daemon enrolled
	// before the control-plane permissions were granted would narrow every
	// token to a set that cannot heartbeat, and stay there: it re-enrolls on
	// 401 and 404, never on the resulting 403 (go-quicbaas#653). Asking for
	// nothing takes the whole current grant on the next refresh.
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", creds.MTLSClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, &EnrollmentTokenExchangeError{Op: "build token-mtls request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, &EnrollmentTokenExchangeError{Op: "post token-mtls request", Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &EnrollmentTokenExchangeError{
			Op:     "token-mtls response",
			Status: resp.StatusCode,
			Detail: strings.TrimSpace(string(detail)),
		}
	}

	var tok tokenResponse
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := dec.Decode(&tok); err != nil {
		return nil, &EnrollmentTokenExchangeError{Op: "decode token-mtls response", Cause: err}
	}
	if tok.AccessToken == "" {
		return nil, &EnrollmentTokenExchangeError{Op: "validate token-mtls response", Cause: errors.New("empty access_token")}
	}
	return &tok, nil
}

// exchangeToken performs the Client Credentials grant.
func (e *ProductionEnroller) exchangeToken(ctx context.Context, cfg *Config, creds *persistedCredentials, client *http.Client) (*tokenResponse, error) {
	endpoint, err := joinURL(cfg.ServerAddr, e.tokenPath)
	if err != nil {
		return nil, &EnrollmentTokenExchangeError{Op: "build token url", Cause: err}
	}

	// No scope parameter, for the reason exchangeTokenMTLS documents.
	form := url.Values{}
	form.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, &EnrollmentTokenExchangeError{Op: "build token request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Basic "+basicAuth(creds.ClientID, creds.ClientSecret))

	resp, err := client.Do(req)
	if err != nil {
		return nil, &EnrollmentTokenExchangeError{Op: "post token request", Cause: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &EnrollmentTokenExchangeError{
			Op:     "token response",
			Status: resp.StatusCode,
			Detail: strings.TrimSpace(string(detail)),
		}
	}

	var tok tokenResponse
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := dec.Decode(&tok); err != nil {
		return nil, &EnrollmentTokenExchangeError{Op: "decode token response", Cause: err}
	}
	if tok.AccessToken == "" {
		return nil, &EnrollmentTokenExchangeError{Op: "validate token response", Cause: errors.New("empty access_token")}
	}
	return &tok, nil
}

// buildIdentity assembles the AgentIdentity returned to the Daemon.
//
// Scopes come from the token response, which is authoritative: the daemon
// requests none, so what comes back is the grant the control plane resolved
// from the application record. That is how a daemon whose credentials file
// predates a grant change converges on the current one without re-enrolling.
// A provider that omits the scope leaves the identity on its
// registration-time set, so the daemon's own spawn server can still admit the
// central's dispatch rather than refusing every RPC for want of a scope.
func (e *ProductionEnroller) buildIdentity(cfg *Config, creds *persistedCredentials, tok *tokenResponse) *orchestration.AgentIdentity {
	scopes := creds.Scopes
	if trimmed := strings.TrimSpace(tok.Scope); trimmed != "" {
		scopes = strings.Fields(trimmed)
	}
	return &orchestration.AgentIdentity{
		AgentID:          creds.AgentID,
		ServiceAccountID: creds.ServiceAccountID,
		TenantID:         cfg.TenantID,
		Role:             creds.Role,
		Scopes:           scopes,
		ClientID:         creds.ClientID,
		ClientSecret:     creds.ClientSecret,
	}
}

// buildMTLSHTTPClient constructs an HTTP client that presents the
// daemon's mTLS client certificate on the TLS handshake and trusts the
// server CA bundle referenced by cfg.
func buildMTLSHTTPClient(cfg *Config) (*http.Client, error) {
	if strings.TrimSpace(cfg.AgentCertFile) == "" {
		return nil, &EnrollmentTLSError{Op: "resolve agent cert", Cause: errors.New("agent_cert_file is required")}
	}
	if strings.TrimSpace(cfg.AgentKeyFile) == "" {
		return nil, &EnrollmentTLSError{Op: "resolve agent key", Cause: errors.New("agent_key_file is required")}
	}
	cert, err := tls.LoadX509KeyPair(cfg.AgentCertFile, cfg.AgentKeyFile)
	if err != nil {
		return nil, &EnrollmentTLSError{Op: "load agent key pair", Cause: err}
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if cfg.ServerCAFile != "" {
		pemBytes, err := os.ReadFile(cfg.ServerCAFile)
		if err != nil {
			return nil, &EnrollmentTLSError{Op: "read server ca", Cause: err}
		}
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, &EnrollmentTLSError{Op: "parse server ca", Cause: errors.New("no PEM certificates parsed from server ca file")}
		}
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      pool,
			MinVersion:   tls.VersionTLS12,
		},
	}
	return &http.Client{
		Transport: transport,
		Timeout:   defaultEnrollmentTimeout,
	}, nil
}

// credentialsPath returns the absolute path to credentials.json inside
// the daemon's work directory.
func credentialsPath(cfg *Config) string {
	return filepath.Join(cfg.WorkDir, credentialsFileName)
}

// readCredentials loads persisted credentials. ok=false with no error
// means the file does not exist; any other failure is wrapped in
// ErrEnrollmentCredentialsRead.
func readCredentials(path string) (*persistedCredentials, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, &EnrollmentCredentialsReadError{Path: path, Cause: err}
	}
	var creds persistedCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, false, &EnrollmentCredentialsReadError{Path: path, Cause: err}
	}
	if err := validateBootstrapResponse(&creds); err != nil {
		return nil, false, &EnrollmentCredentialsReadError{Path: path, Cause: err}
	}
	return &creds, true, nil
}

// writeCredentials atomically writes the credentials file. The temp file
// is created in the same directory (so rename is same-filesystem) with
// mode 0600 and is renamed into place on success.
func writeCredentials(path string, creds *persistedCredentials) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return &EnrollmentCredentialsSaveError{Path: path, Cause: err}
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return &EnrollmentCredentialsSaveError{Path: path, Cause: err}
	}
	tmp, err := os.CreateTemp(dir, credentialsFileName+".tmp-*")
	if err != nil {
		return &EnrollmentCredentialsSaveError{Path: path, Cause: err}
	}
	tmpPath := tmp.Name()
	defer func() {
		// Best-effort cleanup if rename never happens.
		if _, statErr := os.Stat(tmpPath); statErr == nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return &EnrollmentCredentialsSaveError{Path: path, Cause: err}
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return &EnrollmentCredentialsSaveError{Path: path, Cause: err}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return &EnrollmentCredentialsSaveError{Path: path, Cause: err}
	}
	if err := tmp.Close(); err != nil {
		return &EnrollmentCredentialsSaveError{Path: path, Cause: err}
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return &EnrollmentCredentialsSaveError{Path: path, Cause: err}
	}
	return nil
}

// validateBootstrapResponse ensures the registration response (or
// on-disk credentials) carries the fields the daemon cannot function
// without.
//
// ClientSecret is optional when MTLSClientID is set — in that case the daemon
// authenticates exclusively via the mTLS token endpoint and requires no secret.
func validateBootstrapResponse(creds *persistedCredentials) error {
	switch {
	case creds.ClientID == "":
		return &EnrollmentRejectedError{Status: 0, Detail: "missing client_id"}
	case creds.ClientSecret == "" && creds.MTLSClientID == "":
		return &EnrollmentRejectedError{Status: 0, Detail: "missing client_secret (and no mtls_client_id as fallback)"}
	case creds.AgentID == "":
		return &EnrollmentRejectedError{Status: 0, Detail: "missing agent_id"}
	case creds.ServiceAccountID == "":
		return &EnrollmentRejectedError{Status: 0, Detail: "missing service_account_id"}
	case len(creds.Scopes) == 0:
		return &EnrollmentRejectedError{Status: 0, Detail: "missing scopes"}
	}
	return nil
}

// basicAuth returns the base64-encoded "user:pass" HTTP Basic header value.
// RFC 6749 §2.3.1 requires Basic for Client Credentials when
// client_secret is present.
func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

// joinURL concatenates a base URL and a path fragment in a way that is
// robust to trailing slashes and embedded query strings.
func joinURL(base, ref string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	r, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return b.ResolveReference(r).String(), nil
}

// EnrollmentRejectedError is returned when the server refuses to mint
// client credentials for the submitted registration.
type EnrollmentRejectedError struct {
	Status int
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *EnrollmentRejectedError) Error() string {
	var sb strings.Builder
	sb.WriteString(ErrEnrollmentRejected.Error())
	if e.Status != 0 {
		fmt.Fprintf(&sb, ": status=%d", e.Status)
	}
	if e.Detail != "" {
		fmt.Fprintf(&sb, ": %s", e.Detail)
	}
	if e.Cause != nil {
		fmt.Fprintf(&sb, ": %v", e.Cause)
	}
	return sb.String()
}

// Unwrap returns the sentinel error plus the underlying cause. Exposing
// the cause is what lets the daemon's enrollment classifier see the
// transport failure (connection refused, DNS, TLS) underneath a rejection
// that never got an HTTP status.
func (e *EnrollmentRejectedError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrEnrollmentRejected}
	}
	return []error{ErrEnrollmentRejected, e.Cause}
}

// EnrollmentRequestError wraps a client-side failure while constructing
// the registration HTTP request (bad URL, JSON encoding bug). It unwraps
// to ErrEnrollmentRejected because, from the daemon's standpoint,
// registration did not succeed — but keeps the original
// transport/encoding cause.
type EnrollmentRequestError struct {
	Op    string
	Cause error
}

// Error implements the error interface.
func (e *EnrollmentRequestError) Error() string {
	return fmt.Sprintf("%s: %s: %v", ErrEnrollmentRejected.Error(), e.Op, e.Cause)
}

// Unwrap returns the sentinel error plus the underlying cause so the
// enrollment classifier can reach the transport failure beneath it.
func (e *EnrollmentRequestError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrEnrollmentRejected}
	}
	return []error{ErrEnrollmentRejected, e.Cause}
}

// EnrollmentCredentialsReadError wraps a failure to load the persisted
// credentials file (corrupt JSON, permission denied, partial write, etc.).
type EnrollmentCredentialsReadError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *EnrollmentCredentialsReadError) Error() string {
	return fmt.Sprintf("%s: %s: %v", ErrEnrollmentCredentialsRead.Error(), e.Path, e.Cause)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *EnrollmentCredentialsReadError) Unwrap() error { return ErrEnrollmentCredentialsRead }

// EnrollmentCredentialsSaveError wraps a failure to persist credentials.json.
type EnrollmentCredentialsSaveError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *EnrollmentCredentialsSaveError) Error() string {
	return fmt.Sprintf("%s: %s: %v", ErrEnrollmentCredentialsSave.Error(), e.Path, e.Cause)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *EnrollmentCredentialsSaveError) Unwrap() error { return ErrEnrollmentCredentialsSave }

// EnrollmentTokenExchangeError wraps a failure during the OAuth2 Client
// Credentials exchange.
type EnrollmentTokenExchangeError struct {
	Op     string
	Status int
	Detail string
	Cause  error
}

// Error implements the error interface.
func (e *EnrollmentTokenExchangeError) Error() string {
	var sb strings.Builder
	sb.WriteString(ErrEnrollmentTokenExchange.Error())
	if e.Op != "" {
		fmt.Fprintf(&sb, ": %s", e.Op)
	}
	if e.Status != 0 {
		fmt.Fprintf(&sb, ": status=%d", e.Status)
	}
	if e.Detail != "" {
		fmt.Fprintf(&sb, ": %s", e.Detail)
	}
	if e.Cause != nil {
		fmt.Fprintf(&sb, ": %v", e.Cause)
	}
	return sb.String()
}

// Unwrap returns the sentinel error plus the underlying cause so the
// enrollment classifier can reach the transport failure beneath it.
func (e *EnrollmentTokenExchangeError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrEnrollmentTokenExchange}
	}
	return []error{ErrEnrollmentTokenExchange, e.Cause}
}

// EnrollmentTLSError wraps a failure to construct the mTLS HTTP client
// (missing/corrupt cert, bad server CA, etc.).
type EnrollmentTLSError struct {
	Op    string
	Cause error
}

// Error implements the error interface.
func (e *EnrollmentTLSError) Error() string {
	return fmt.Sprintf("agent enroller tls: %s: %v", e.Op, e.Cause)
}

// Unwrap returns the wrapped cause.
func (e *EnrollmentTLSError) Unwrap() error { return e.Cause }

// Enrollment error sentinels.
var (
	// ErrEnrollmentMissingCredentials indicates the daemon has no cached
	// credentials on disk — typically because the caller attempted a
	// token refresh without ever having completed registration.
	ErrEnrollmentMissingCredentials = errors.New("agent: no cached credentials; registration required")

	// ErrEnrollmentRejected indicates the server refused registration.
	ErrEnrollmentRejected = errors.New("agent: server rejected registration")

	// ErrEnrollmentCredentialsRead indicates a failure loading the
	// persisted credentials file.
	ErrEnrollmentCredentialsRead = errors.New("agent: failed to read persisted credentials")

	// ErrEnrollmentCredentialsSave indicates a failure persisting the
	// credentials file.
	ErrEnrollmentCredentialsSave = errors.New("agent: failed to persist credentials")

	// ErrEnrollmentTokenExchange indicates the OAuth2 client credentials
	// grant exchange failed.
	ErrEnrollmentTokenExchange = errors.New("agent: token exchange failed")
)

// EventIngestAddrFromDisk returns the gRPC URL the daemon should dial
// for the PushEvents bidi-stream, as advertised by the server in the
// register response and persisted to credentials.json. Returns
// ("", ErrEnrollmentMissingCredentials) when the credentials file is
// absent, and ("", *EnrollmentCredentialsReadError) for any other
// load failure. An empty string with no error indicates the server
// did not advertise an ingest address (older daemons or REST-only
// deployments) — callers should disable event forwarding in that
// case rather than dialing a fallback port.
func (e *ProductionEnroller) EventIngestAddrFromDisk(cfg *Config) (string, error) {
	if cfg == nil {
		return "", ErrConfigNil
	}
	creds, ok, err := readCredentials(credentialsPath(cfg))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrEnrollmentMissingCredentials
	}
	return creds.EventIngestAddr, nil
}

// Compile-time interface check.
var _ Enroller = (*ProductionEnroller)(nil)

// Compile-time check that ProductionEnroller satisfies Reregisterer.
var _ Reregisterer = (*ProductionEnroller)(nil)
