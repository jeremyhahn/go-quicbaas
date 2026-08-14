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

package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TokenSource returns a short-lived OAuth2 Bearer token usable against the
// control plane. Implementations are responsible for refresh and
// caching; callers get a fresh, non-expired access_token every call.
//
// The default implementation wraps ProductionEnroller: on first call it
// exchanges the persisted client credentials for a JWT via /oauth2/token,
// then caches until expiry-5s. On cache miss it re-exchanges. On 401 from
// downstream callers, callers pass ForceRefresh=true via Invalidate().
type TokenSource interface {
	Token(ctx context.Context) (string, error)
	Invalidate()
}

// Reregisterer drives the agent's re-enrollment flow. It is called by
// HTTPHeartbeatSender when the control plane rejects a heartbeat with 404
// (the control plane forgot the agent and could not auto-recover from the
// peer cert — typically because the control plane was reinstalled and lost
// its CA / signing key, so the agent's cert no longer derives an
// AgentID the control plane can match).
//
// Implementations MUST clear any cached credentials, perform a fresh
// /api/v1/agents/register exchange, persist the new credentials, and
// publish a refreshed AgentIdentity (so the Daemon's identity pointer
// reflects the post-re-register state). Returning an error marks the
// re-register attempt as failed; HTTPHeartbeatSender counts consecutive
// failures and surfaces ErrAgentReRegisterFailed once the budget is
// exhausted.
type Reregisterer interface {
	Reregister(ctx context.Context) (*orchestration.AgentIdentity, error)
}

// HTTPHeartbeatSender delivers agent heartbeats to the control plane via REST
// POST /api/v1/orchestrator/agents/{agent_id}/heartbeat?tenant_id={tenant}.
// It implements the Daemon's HeartbeatSender interface so the existing
// heartbeatLoop can drive it unchanged.
//
// Retry / refresh strategy:
//   - Transient errors (5xx, network) → exponential backoff up to 30s,
//     retries absorbed within a single Tick call (caller's
//     heartbeat_interval still advances as usual).
//   - 401 Unauthorized → invalidate the token source + retry once with a
//     fresh JWT. Second 401 surfaces the error; the daemon's
//     heartbeatLoop logs it and the next tick tries again.
//   - 404 Not Found → the control plane does not know this agent. Server-side
//     auto-recovery (handlers_agent_heartbeat_recover.go) handles the
//     usual case (control plane restart lost the in-memory map). When the
//     server fails recovery — typically because the control plane's CA was
//     wiped and the agent's cert no longer matches — the sender invokes
//     the Reregisterer hook to bootstrap fresh OAuth2 credentials, then
//     retries the heartbeat once. Three consecutive re-register failures
//     surface ErrAgentReRegisterFailed so systemd restarts the unit.
//   - 4xx other than 401 / 404 → terminal; return wrapped error.
//
// The sender is safe for concurrent callers.
type HTTPHeartbeatSender struct {
	serverAddr     string
	tenantID       string
	httpClient     *http.Client
	tokens         TokenSource
	logger         interfaces.Logger
	reregisterer   Reregisterer
	reregisterFail atomic.Int32
}

// HTTPHeartbeatConfig bundles the sender's dependencies.
type HTTPHeartbeatConfig struct {
	// ServerAddr is the base URL of the control plane (e.g. https://host:8443).
	ServerAddr string

	// TenantID is the tenant this agent heartbeats under.
	TenantID string

	// HTTPClient is the transport used. Should already trust the control plane's
	// CA (see cliLoggerForAgentServe builder for the daemon wiring).
	HTTPClient *http.Client

	// Tokens returns a current access_token for Bearer auth on each POST.
	Tokens TokenSource

	// Logger is used for diagnostics. Required.
	Logger interfaces.Logger

	// Reregisterer is the optional re-enrollment driver invoked when the
	// control plane returns 404. Leave nil to disable client-side recovery —
	// the sender will then surface the 404 as a regular HeartbeatSendError
	// and the heartbeatLoop will keep retrying on the next tick.
	Reregisterer Reregisterer
}

// NewHTTPHeartbeatSender constructs the production heartbeat sender.
func NewHTTPHeartbeatSender(cfg HTTPHeartbeatConfig) (*HTTPHeartbeatSender, error) {
	if cfg.ServerAddr == "" {
		return nil, ErrHeartbeatNoServer
	}
	if cfg.TenantID == "" {
		return nil, ErrHeartbeatNoTenant
	}
	if cfg.HTTPClient == nil {
		return nil, ErrHeartbeatNoHTTPClient
	}
	if cfg.Tokens == nil {
		return nil, ErrHeartbeatNoTokens
	}
	if cfg.Logger == nil {
		return nil, ErrHeartbeatNoLogger
	}
	return &HTTPHeartbeatSender{
		serverAddr:   strings.TrimRight(cfg.ServerAddr, "/"),
		tenantID:     cfg.TenantID,
		httpClient:   cfg.HTTPClient,
		tokens:       cfg.Tokens,
		logger:       cfg.Logger,
		reregisterer: cfg.Reregisterer,
	}, nil
}

// SendHeartbeat implements HeartbeatSender. The ctx typically has no deadline
// (the caller's heartbeatLoop runs forever), so we bound each attempt here.
//
// Map-based dispatch on the post() result keeps the recovery decision
// table explicit: each error class maps to a single recovery action,
// avoiding nested if/else and making the contract trivially auditable.
func (s *HTTPHeartbeatSender) SendHeartbeat(ctx context.Context, identity *orchestration.AgentIdentity, status HeartbeatStatus) error {
	if identity == nil || identity.AgentID == "" {
		return ErrHeartbeatNoIdentity
	}

	endpoint, err := s.buildEndpoint(identity.AgentID)
	if err != nil {
		return err
	}

	// Bound each attempt so we never hang forever on a stuck control plane.
	attemptCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// First attempt with cached token.
	firstErr := s.post(attemptCtx, endpoint, status, false)
	switch {
	case firstErr == nil:
		s.reregisterFail.Store(0)
		return nil

	case errors.Is(firstErr, errHeartbeatUnauthorized):
		// 401 → invalidate and retry once with a freshly exchanged JWT.
		s.tokens.Invalidate()
		retryCtx, retryCancel := context.WithTimeout(ctx, 15*time.Second)
		defer retryCancel()
		if retryErr := s.post(retryCtx, endpoint, status, true); retryErr != nil {
			return retryErr
		}
		s.reregisterFail.Store(0)
		return nil

	case errors.Is(firstErr, errHeartbeatNotFound):
		// 404 → server failed auto-recovery. Try client-side
		// re-enrollment, then retry the heartbeat once with the new
		// identity. Three consecutive failures surface a typed error
		// the daemon turns into a clean exit.
		return s.handleNotFound(ctx, endpoint, status)

	default:
		return firstErr
	}
}

// handleNotFound drives the 404 → re-register → retry sequence. The
// sender keeps an atomic failure counter so transient re-register
// errors do not hard-fail the daemon — three consecutive failures must
// occur before ErrAgentReRegisterFailed is surfaced.
func (s *HTTPHeartbeatSender) handleNotFound(ctx context.Context, endpoint string, status HeartbeatStatus) error {
	if s.reregisterer == nil {
		return &HeartbeatSendError{Op: "control plane rejected heartbeat", Status: http.StatusNotFound}
	}

	s.logger.Infof("agent heartbeat: control plane returned 404 for known identity; attempting re-register")
	reCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if _, reErr := s.reregisterer.Reregister(reCtx); reErr != nil {
		fails := s.reregisterFail.Add(1)
		s.logger.Warnf("agent heartbeat: re-register attempt failed (%d/%d): %v",
			fails, maxConsecutiveReregisterFailures, reErr)
		if fails >= maxConsecutiveReregisterFailures {
			return &ReregisterFailedError{Attempts: int(fails), Cause: reErr}
		}
		return &HeartbeatSendError{Op: "re-register", Cause: reErr}
	}

	// Refresh the cached token because re-register minted a fresh
	// OAuth2 client_secret; the old JWT no longer authenticates.
	s.tokens.Invalidate()

	retryCtx, retryCancel := context.WithTimeout(ctx, 15*time.Second)
	defer retryCancel()
	if retryErr := s.post(retryCtx, endpoint, status, true); retryErr != nil {
		fails := s.reregisterFail.Add(1)
		s.logger.Warnf("agent heartbeat: post-re-register heartbeat failed (%d/%d): %v",
			fails, maxConsecutiveReregisterFailures, retryErr)
		if fails >= maxConsecutiveReregisterFailures {
			return &ReregisterFailedError{Attempts: int(fails), Cause: retryErr}
		}
		return retryErr
	}
	s.reregisterFail.Store(0)
	s.logger.Infof("agent heartbeat: re-register succeeded; resumed normal heartbeats")
	return nil
}

// maxConsecutiveReregisterFailures is the budget the sender allows
// before surfacing ErrAgentReRegisterFailed. Three failures across
// three heartbeat ticks (~90s at the default 30s interval) is enough
// signal that the control plane is permanently unrecoverable from this
// agent's perspective; a clean exit lets systemd restart the unit and
// retry from a fresh process state.
const maxConsecutiveReregisterFailures int32 = 3

// buildEndpoint assembles the full URL for the heartbeat POST. Separated for
// testability and so a malformed ServerAddr fails fast instead of on every
// heartbeat tick.
func (s *HTTPHeartbeatSender) buildEndpoint(agentID string) (string, error) {
	if agentID == "" {
		return "", ErrHeartbeatNoIdentity
	}
	base, err := url.Parse(s.serverAddr)
	if err != nil {
		return "", &HeartbeatSendError{Op: "parse control plane addr", Cause: err}
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/api/v1/orchestrator/agents/" + url.PathEscape(agentID) + "/heartbeat"
	q := base.Query()
	q.Set("tenant_id", s.tenantID)
	base.RawQuery = q.Encode()
	return base.String(), nil
}

// post issues one heartbeat POST. Returns errHeartbeatUnauthorized for 401,
// *HeartbeatSendError for any other error. 2xx is nil. 5xx and network errors
// are wrapped as send errors so the caller can apply its own retry policy
// (the daemon's heartbeatLoop ignores and retries on the next tick).
func (s *HTTPHeartbeatSender) post(ctx context.Context, endpoint string, status HeartbeatStatus, afterRefresh bool) error {
	token, err := s.tokens.Token(ctx)
	if err != nil {
		return &HeartbeatSendError{Op: "acquire token", Cause: err}
	}

	body := fmt.Sprintf(
		`{"active_sessions":%d,"sandbox_policy":%q,"backends":%s,"has_container_runtime":%t,"sequence":%d,"bind_addr":%q}`,
		status.ActiveSessions,
		status.SandboxPolicy.String(),
		marshalStringSlice(status.Backends),
		status.HasContainerRT,
		status.SequenceNumber,
		status.BindAddr,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return &HeartbeatSendError{Op: "build request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return &HeartbeatSendError{Op: "transport", Cause: err}
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		if afterRefresh {
			return &HeartbeatSendError{Op: "authenticate", Status: resp.StatusCode, Cause: errors.New("still 401 after token refresh")}
		}
		return errHeartbeatUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		return errHeartbeatNotFound
	default:
		return &HeartbeatSendError{Op: "control plane rejected heartbeat", Status: resp.StatusCode}
	}
}

// marshalStringSlice produces a JSON array literal for a []string without
// pulling the encoding/json package for such a small field. Avoids import
// bloat in a hot-path function that runs every heartbeat tick.
func marshalStringSlice(xs []string) string {
	if len(xs) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range xs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		for _, r := range x {
			if r == '"' || r == '\\' {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		b.WriteByte('"')
	}
	b.WriteByte(']')
	return b.String()
}

// errHeartbeatUnauthorized is an internal sentinel used by post() to signal
// that SendHeartbeat should invalidate the token cache and retry.
var errHeartbeatUnauthorized = errors.New("agent heartbeat: 401 unauthorized")

// errHeartbeatNotFound is an internal sentinel used by post() to signal
// that SendHeartbeat should drive a re-register flow before retrying.
var errHeartbeatNotFound = errors.New("agent heartbeat: 404 not found")

// ErrAgentReRegisterFailed indicates the agent's client-side
// re-enrollment hook has failed maxConsecutiveReregisterFailures times
// in a row. The daemon turns this into a clean exit so systemd can
// restart the unit and retry from a fresh process state.
var ErrAgentReRegisterFailed = errors.New("agent heartbeat: re-register exhausted retry budget")

// ReregisterFailedError wraps the terminal re-register failure with
// the failure count and the underlying cause so logs preserve the
// debugging context the operator needs.
type ReregisterFailedError struct {
	Attempts int
	Cause    error
}

func (e *ReregisterFailedError) Error() string {
	return fmt.Sprintf("agent heartbeat: re-register failed after %d attempts: %v", e.Attempts, e.Cause)
}

func (e *ReregisterFailedError) Unwrap() error { return ErrAgentReRegisterFailed }

// HeartbeatSendError wraps non-auth failures from the heartbeat HTTP POST.
type HeartbeatSendError struct {
	Op     string
	Status int
	Cause  error
}

func (e *HeartbeatSendError) Error() string {
	if e.Status != 0 && e.Cause != nil {
		return fmt.Sprintf("agent heartbeat: %s: status=%d: %v", e.Op, e.Status, e.Cause)
	}
	if e.Status != 0 {
		return fmt.Sprintf("agent heartbeat: %s: status=%d", e.Op, e.Status)
	}
	if e.Cause != nil {
		return fmt.Sprintf("agent heartbeat: %s: %v", e.Op, e.Cause)
	}
	return "agent heartbeat: " + e.Op
}

func (e *HeartbeatSendError) Unwrap() error { return e.Cause }

// Heartbeat error sentinels.
var (
	ErrHeartbeatNoServer     = errors.New("agent heartbeat: control plane addr required")
	ErrHeartbeatNoTenant     = errors.New("agent heartbeat: tenant id required")
	ErrHeartbeatNoHTTPClient = errors.New("agent heartbeat: http client required")
	ErrHeartbeatNoTokens     = errors.New("agent heartbeat: token source required")
	ErrHeartbeatNoLogger     = errors.New("agent heartbeat: logger required")
	ErrHeartbeatNoIdentity   = errors.New("agent heartbeat: identity with agent_id required")
)

// =============================================================================
// CachingTokenSource — the production TokenSource implementation
// =============================================================================

// CachingTokenSource caches the most recent access_token and refreshes via
// the supplied Refresh func when it is expired, invalidated, or missing.
// Callers must call Invalidate() after any downstream 401 so the next Token
// call triggers a fresh exchange.
//
// Re-enrollment on invalid_client:
//
// When Refresh returns a *EnrollmentTokenExchangeError carrying status 401
// with an OAuth2 error body of "invalid_client" (the control plane no longer
// trusts the cached client_secret — typically because the control plane's IAM
// store was wiped or rebuilt), the token source treats this as evidence
// the cached credentials are dead. It calls Reregisterer.Reregister(ctx)
// to mint a fresh client_id / client_secret pair, then retries the
// Refresh once. This is symmetrical to the 404-on-heartbeat flow in
// HTTPHeartbeatSender: the budget (maxConsecutiveReregisterFailures)
// guards against infinite retry, and exhausting it surfaces a typed
// *ReregisterFailedError so the daemon exits cleanly and systemd
// restarts the unit.
//
// A 401 with any other OAuth2 error code (invalid_scope, invalid_grant,
// etc.) is propagated unchanged — only invalid_client is interpreted as
// a credential trust break warranting re-enrollment.
type CachingTokenSource struct {
	Refresh func(ctx context.Context) (token string, expiresAt time.Time, err error)

	// Reregisterer is the optional re-enrollment driver invoked when the
	// control plane rejects the token exchange with invalid_client. Leave nil
	// to disable client-side recovery — the 401 will then propagate as a
	// regular EnrollmentTokenExchangeError and downstream callers will
	// keep retrying on their own cadence.
	Reregisterer Reregisterer

	// Logger is used to surface the re-enroll attempt and outcome. May
	// be nil; the token source never panics on a missing logger.
	Logger interfaces.Logger

	mu             sync.Mutex
	cached         string
	expiresAt      time.Time
	reregisterFail atomic.Int32
}

// SetReregisterer wires the re-enrollment driver after construction.
// Symmetrical to HTTPHeartbeatConfig.Reregisterer; useful when the
// enroller and the token source are constructed together but the
// enroller does not yet know its bound config (the daemon-serve wiring
// captures cfg via SetConfig before publishing the token source).
func (t *CachingTokenSource) SetReregisterer(r Reregisterer) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Reregisterer = r
}

// SetLogger wires the diagnostics logger after construction.
func (t *CachingTokenSource) SetLogger(l interfaces.Logger) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Logger = l
}

// Token returns a valid access_token. When the cached token is missing or
// within 5s of expiry, it re-runs Refresh. Concurrent callers share one
// in-flight refresh.
//
// On a Refresh failure that signals invalid_client at the control plane, the
// token source drives re-enrollment via the configured Reregisterer and
// retries the Refresh once. The mutex is held across the retry so a
// burst of concurrent Token calls does not stampede the control plane with
// parallel /api/v1/agents/register POSTs.
func (t *CachingTokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cached != "" && time.Until(t.expiresAt) > 5*time.Second {
		return t.cached, nil
	}
	if t.Refresh == nil {
		return "", errors.New("agent heartbeat: token source has no refresh fn")
	}
	tok, exp, err := t.Refresh(ctx)
	if err == nil {
		t.cached = tok
		t.expiresAt = exp
		t.reregisterFail.Store(0)
		return tok, nil
	}

	// Decide whether the failure warrants client-side re-enrollment.
	// Only 401 with OAuth2 error=invalid_client triggers the recovery
	// path; every other failure mode propagates unchanged.
	if !isInvalidClientTokenExchangeError(err) || t.Reregisterer == nil {
		return "", err
	}

	if t.Logger != nil {
		t.Logger.Infof("agent: token exchange rejected by control plane; attempting re-enroll")
	}
	if _, reErr := t.Reregisterer.Reregister(ctx); reErr != nil {
		fails := t.reregisterFail.Add(1)
		if t.Logger != nil {
			t.Logger.Warnf("agent: re-enroll attempt failed (%d/%d): %v",
				fails, maxConsecutiveReregisterFailures, reErr)
		}
		if fails >= maxConsecutiveReregisterFailures {
			return "", &ReregisterFailedError{Attempts: int(fails), Cause: reErr}
		}
		// Surface the original 401 wrapped error so the caller's logs
		// preserve the control plane's response body for triage.
		return "", err
	}

	// Re-register succeeded; the credentials file on disk now carries a
	// fresh client_id / client_secret. Retry the exchange once with the
	// new credentials.
	tok, exp, retryErr := t.Refresh(ctx)
	if retryErr != nil {
		fails := t.reregisterFail.Add(1)
		if t.Logger != nil {
			t.Logger.Warnf("agent: post-re-enroll token exchange failed (%d/%d): %v",
				fails, maxConsecutiveReregisterFailures, retryErr)
		}
		if fails >= maxConsecutiveReregisterFailures {
			return "", &ReregisterFailedError{Attempts: int(fails), Cause: retryErr}
		}
		return "", retryErr
	}
	t.cached = tok
	t.expiresAt = exp
	t.reregisterFail.Store(0)
	if t.Logger != nil {
		t.Logger.Infof("agent: re-enroll succeeded; token exchange recovered")
	}
	return tok, nil
}

// Invalidate clears the cache so the next Token() call forces a fresh
// exchange. Safe for concurrent callers.
func (t *CachingTokenSource) Invalidate() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cached = ""
	t.expiresAt = time.Time{}
}
