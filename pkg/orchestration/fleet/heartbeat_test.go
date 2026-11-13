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

package fleet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// staticToken is a TokenSource that returns a canned token and lets tests
// trigger the 401-retry path by toggling what Invalidate produces next.
type staticToken struct {
	mu          sync.Mutex
	tokens      []string // consumed in order; if empty, last value re-used
	invalidated int32
}

func (s *staticToken) Token(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tokens) == 0 {
		return "", errors.New("staticToken: no tokens left")
	}
	t := s.tokens[0]
	if len(s.tokens) > 1 {
		s.tokens = s.tokens[1:]
	}
	return t, nil
}

func (s *staticToken) Invalidate() {
	atomic.AddInt32(&s.invalidated, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tokens) > 1 {
		s.tokens = s.tokens[1:]
	}
}

// captureIdentity provides a minimally-valid orchestration.AgentIdentity for
// tests without depending on enrollment.
func captureIdentity(nodeID, tenantID string) *orchestration.AgentIdentity {
	return &orchestration.AgentIdentity{
		AgentID:          nodeID,
		ServiceAccountID: "sa-" + nodeID,
		TenantID:         tenantID,
		Role:             "agent-runtime",
		Scopes:           []string{"agent:runtime:spawn", "agent:runtime:prompt", "agent:runtime:status"},
		ClientID:         "client-" + nodeID,
	}
}

// tlsClientFromServer returns an http.Client that trusts the httptest server's cert.
func tlsClientFromServer(s *httptest.Server) *http.Client {
	pool := x509.NewCertPool()
	pool.AddCert(s.Certificate())
	return &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}},
		Timeout:   5 * time.Second,
	}
}

func TestHTTPHeartbeatSender_SuccessfulPost(t *testing.T) {
	t.Parallel()
	var (
		reqURL  atomic.Value
		reqAuth atomic.Value
		body    atomic.Value
	)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqURL.Store(r.URL.String())
		reqAuth.Store(r.Header.Get("Authorization"))
		b, _ := io.ReadAll(r.Body)
		body.Store(string(b))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	sender, err := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: srv.URL,
		TenantID:   "tenant-a",
		HTTPClient: tlsClientFromServer(srv),
		Tokens:     &staticToken{tokens: []string{"tok-1"}},
		Logger:     &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewHTTPHeartbeatSender: %v", err)
	}

	err = sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{
		ActiveSessions: 3,
		SandboxPolicy:  orchestration.SandboxPolicyRequired,
		Backends:       []string{"ollama", "claude"},
		HasContainerRT: true,
		SequenceNumber: 42,
	})
	if err != nil {
		t.Fatalf("SendHeartbeat: %v", err)
	}

	gotURL, _ := reqURL.Load().(string)
	if !strings.HasPrefix(gotURL, "/api/v1/fleet/agent-1/heartbeat") {
		t.Errorf("path: got %q", gotURL)
	}
	if !strings.Contains(gotURL, "tenant_id=tenant-a") {
		t.Errorf("query: got %q", gotURL)
	}
	if got, _ := reqAuth.Load().(string); got != "Bearer tok-1" {
		t.Errorf("authorization: got %q", got)
	}
	b, _ := body.Load().(string)
	for _, want := range []string{`"active_sessions":3`, `"sandbox_policy":"required"`, `"ollama"`, `"has_container_runtime":true`, `"sequence":42`} {
		if !strings.Contains(b, want) {
			t.Errorf("body missing %q; got %s", want, b)
		}
	}
}

func TestHTTPHeartbeatSender_401RefreshesAndRetries(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Second call must carry the refreshed token.
		if got := r.Header.Get("Authorization"); got != "Bearer tok-fresh" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	tokens := &staticToken{tokens: []string{"tok-stale", "tok-fresh"}}
	sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: srv.URL,
		TenantID:   "tenant-a",
		HTTPClient: tlsClientFromServer(srv),
		Tokens:     tokens,
		Logger:     &testLogger{t: t},
	})

	if err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{}); err != nil {
		t.Fatalf("SendHeartbeat: %v", err)
	}
	if atomic.LoadInt32(&tokens.invalidated) != 1 {
		t.Errorf("expected exactly one invalidation, got %d", tokens.invalidated)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 server calls, got %d", calls.Load())
	}
}

func TestHTTPHeartbeatSender_Persistent401Surfaces(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	tokens := &staticToken{tokens: []string{"tok-1", "tok-2"}}
	sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: srv.URL,
		TenantID:   "tenant-a",
		HTTPClient: tlsClientFromServer(srv),
		Tokens:     tokens,
		Logger:     &testLogger{t: t},
	})

	err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{})
	if err == nil {
		t.Fatal("expected error on persistent 401")
	}
	var se *HeartbeatSendError
	if !errors.As(err, &se) || se.Status != http.StatusUnauthorized {
		t.Errorf("wrong error: %v", err)
	}
}

func TestHTTPHeartbeatSender_5xxReturnsSendError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: srv.URL,
		TenantID:   "tenant-a",
		HTTPClient: tlsClientFromServer(srv),
		Tokens:     &staticToken{tokens: []string{"tok-1"}},
		Logger:     &testLogger{t: t},
	})

	err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{})
	var se *HeartbeatSendError
	if !errors.As(err, &se) || se.Status != http.StatusInternalServerError {
		t.Fatalf("got %v", err)
	}
}

func TestHTTPHeartbeatSender_TransportError(t *testing.T) {
	t.Parallel()
	// Server closed immediately → dial fails.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()

	sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: srv.URL,
		TenantID:   "tenant-a",
		HTTPClient: tlsClientFromServer(srv),
		Tokens:     &staticToken{tokens: []string{"tok-1"}},
		Logger:     &testLogger{t: t},
	})
	err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{})
	var se *HeartbeatSendError
	if !errors.As(err, &se) {
		t.Fatalf("got %v", err)
	}
}

func TestHTTPHeartbeatSender_ConstructorValidation(t *testing.T) {
	t.Parallel()
	valid := HTTPHeartbeatConfig{
		ServerAddr: "https://x",
		TenantID:   "t",
		HTTPClient: &http.Client{},
		Tokens:     &staticToken{tokens: []string{"t"}},
		Logger:     &testLogger{t: t},
	}
	cases := []struct {
		name   string
		mutate func(*HTTPHeartbeatConfig)
		want   error
	}{
		{"no control plane", func(c *HTTPHeartbeatConfig) { c.ServerAddr = "" }, ErrHeartbeatNoServer},
		{"no tenant", func(c *HTTPHeartbeatConfig) { c.TenantID = "" }, ErrHeartbeatNoTenant},
		{"no client", func(c *HTTPHeartbeatConfig) { c.HTTPClient = nil }, ErrHeartbeatNoHTTPClient},
		{"no tokens", func(c *HTTPHeartbeatConfig) { c.Tokens = nil }, ErrHeartbeatNoTokens},
		{"no logger", func(c *HTTPHeartbeatConfig) { c.Logger = nil }, ErrHeartbeatNoLogger},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)
			if _, err := NewHTTPHeartbeatSender(cfg); !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestHTTPHeartbeatSender_EmptyIdentity(t *testing.T) {
	t.Parallel()
	sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: "https://x",
		TenantID:   "t",
		HTTPClient: &http.Client{},
		Tokens:     &staticToken{tokens: []string{"t"}},
		Logger:     &testLogger{t: t},
	})
	if err := sender.SendHeartbeat(context.Background(), nil, HeartbeatStatus{}); !errors.Is(err, ErrHeartbeatNoIdentity) {
		t.Errorf("got %v", err)
	}
	if err := sender.SendHeartbeat(context.Background(), &orchestration.AgentIdentity{}, HeartbeatStatus{}); !errors.Is(err, ErrHeartbeatNoIdentity) {
		t.Errorf("empty agent_id: got %v", err)
	}
}

func TestCachingTokenSource_RefreshAndInvalidate(t *testing.T) {
	t.Parallel()
	var refreshes atomic.Int32
	src := &CachingTokenSource{
		Refresh: func(_ context.Context) (string, time.Time, error) {
			n := refreshes.Add(1)
			return "t-" + strings.Repeat("x", int(n)), time.Now().Add(time.Hour), nil
		},
	}
	tok1, err := src.Token(context.Background())
	if err != nil || tok1 == "" {
		t.Fatalf("first token: %q, %v", tok1, err)
	}
	tok2, _ := src.Token(context.Background())
	if tok2 != tok1 {
		t.Errorf("expected cached token, got new: %q vs %q", tok1, tok2)
	}
	src.Invalidate()
	tok3, _ := src.Token(context.Background())
	if tok3 == tok1 {
		t.Error("expected fresh token after invalidate")
	}
	if got := refreshes.Load(); got != 2 {
		t.Errorf("expected 2 refreshes, got %d", got)
	}
}

func TestCachingTokenSource_ExpiredCacheTriggersRefresh(t *testing.T) {
	t.Parallel()
	var refreshes atomic.Int32
	src := &CachingTokenSource{
		Refresh: func(_ context.Context) (string, time.Time, error) {
			refreshes.Add(1)
			// Short TTL so second call sees the cache as "within 5s of expiry".
			return "t", time.Now().Add(2 * time.Second), nil
		},
	}
	_, _ = src.Token(context.Background())
	_, _ = src.Token(context.Background())
	if refreshes.Load() != 2 {
		t.Errorf("near-expiry cache should refresh; got %d refreshes", refreshes.Load())
	}
}

func TestCachingTokenSource_MissingRefreshErrors(t *testing.T) {
	t.Parallel()
	src := &CachingTokenSource{}
	if _, err := src.Token(context.Background()); err == nil {
		t.Error("expected error with no refresh fn")
	}
}

// staticReregisterer is a TokenSource-style fake that records every
// Reregister call and returns the configured error sequence. Used to
// drive the 404 → re-register decision in HTTPHeartbeatSender.
type staticReregisterer struct {
	mu    sync.Mutex
	calls atomic.Int32
	errs  []error // consumed in order; if len < calls, last err re-used
}

func (r *staticReregisterer) Reregister(_ context.Context) (*orchestration.AgentIdentity, error) {
	n := r.calls.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := int(n) - 1
	if idx >= len(r.errs) {
		idx = len(r.errs) - 1
	}
	if idx < 0 {
		return &orchestration.AgentIdentity{AgentID: "agent-1", TenantID: "tenant-a"}, nil
	}
	if r.errs[idx] != nil {
		return nil, r.errs[idx]
	}
	return &orchestration.AgentIdentity{AgentID: "agent-1", TenantID: "tenant-a"}, nil
}

// TestHTTPHeartbeatSender_404TriggersReRegister asserts that when the
// control plane returns 404, the sender invokes the Reregisterer hook and
// retries the heartbeat. The first POST returns 404; after re-register
// the second POST returns 200, so SendHeartbeat reports success
// (recovery is transparent to the daemon's heartbeatLoop).
func TestHTTPHeartbeatSender_404TriggersReRegister(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rereg := &staticReregisterer{errs: []error{nil}}
	tokens := &staticToken{tokens: []string{"tok-1", "tok-2"}}
	sender, err := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr:   srv.URL,
		TenantID:     "tenant-a",
		HTTPClient:   tlsClientFromServer(srv),
		Tokens:       tokens,
		Logger:       &testLogger{t: t},
		Reregisterer: rereg,
	})
	if err != nil {
		t.Fatalf("NewHTTPHeartbeatSender: %v", err)
	}

	if err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{}); err != nil {
		t.Fatalf("SendHeartbeat after re-register should succeed: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 server hits (404, then 200), got %d", calls.Load())
	}
	if rereg.calls.Load() != 1 {
		t.Errorf("expected exactly 1 re-register, got %d", rereg.calls.Load())
	}
	if atomic.LoadInt32(&tokens.invalidated) != 1 {
		t.Errorf("expected token invalidate after re-register, got %d", tokens.invalidated)
	}
}

// TestHTTPHeartbeatSender_404WithoutReregistererSurfaces asserts that
// when no Reregisterer is wired, a 404 surfaces as a normal
// HeartbeatSendError (the daemon's loop will keep retrying on the next
// tick — server-side auto-recovery is the primary mechanism, the
// re-register hook is just the fallback for permanent CA loss).
func TestHTTPHeartbeatSender_404WithoutReregistererSurfaces(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: srv.URL,
		TenantID:   "tenant-a",
		HTTPClient: tlsClientFromServer(srv),
		Tokens:     &staticToken{tokens: []string{"tok-1"}},
		Logger:     &testLogger{t: t},
	})

	err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{})
	var se *HeartbeatSendError
	if !errors.As(err, &se) || se.Status != http.StatusNotFound {
		t.Fatalf("expected HeartbeatSendError with status=404, got %v", err)
	}
}

// TestHTTPHeartbeatSender_404ReregisterBudgetExhausted asserts that
// after maxConsecutiveReregisterFailures consecutive re-register
// failures, the sender surfaces a typed ReregisterFailedError that
// unwraps to ErrNodeReRegisterFailed. The daemon turns this into a
// clean exit so systemd restarts the unit.
func TestHTTPHeartbeatSender_404ReregisterBudgetExhausted(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	regErr := errors.New("control plane refused enrollment")
	rereg := &staticReregisterer{errs: []error{regErr, regErr, regErr}}
	sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr:   srv.URL,
		TenantID:     "tenant-a",
		HTTPClient:   tlsClientFromServer(srv),
		Tokens:       &staticToken{tokens: []string{"tok-1"}},
		Logger:       &testLogger{t: t},
		Reregisterer: rereg,
	})

	var lastErr error
	for i := 0; i < 3; i++ {
		lastErr = sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{})
	}
	if !errors.Is(lastErr, ErrNodeReRegisterFailed) {
		t.Fatalf("after 3 re-register failures expected ErrNodeReRegisterFailed, got %v", lastErr)
	}
	var failed *ReregisterFailedError
	if !errors.As(lastErr, &failed) {
		t.Fatalf("expected *ReregisterFailedError, got %T", lastErr)
	}
	if failed.Attempts != 3 {
		t.Errorf("expected Attempts=3, got %d", failed.Attempts)
	}
	if rereg.calls.Load() != 3 {
		t.Errorf("expected 3 re-register attempts, got %d", rereg.calls.Load())
	}
}

// TestHTTPHeartbeatSender_404ReregisterFailureCounterResets asserts
// the failure counter resets after a successful heartbeat, so a
// transient re-register hiccup does not steal the budget from a later
// genuine failure.
func TestHTTPHeartbeatSender_404ReregisterFailureCounterResets(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		// Pattern: 404, then 200 forever.
		if n == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	regErr := errors.New("transient")
	// First re-register fails, second succeeds.
	rereg := &staticReregisterer{errs: []error{regErr, nil}}
	sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr:   srv.URL,
		TenantID:     "tenant-a",
		HTTPClient:   tlsClientFromServer(srv),
		Tokens:       &staticToken{tokens: []string{"tok-1", "tok-2", "tok-3"}},
		Logger:       &testLogger{t: t},
		Reregisterer: rereg,
	})

	// First call: 404 → re-register fails (1/3).
	first := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{})
	if errors.Is(first, ErrNodeReRegisterFailed) {
		t.Fatalf("first failure must not exhaust budget: %v", first)
	}

	// Subsequent calls: server now returns 200 directly. Counter
	// resets on first success — even though the prior re-register
	// attempt logged a failure.
	for i := 0; i < 3; i++ {
		if err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{}); err != nil {
			t.Fatalf("post-recovery heartbeat %d: %v", i, err)
		}
	}
}

// TestHTTPHeartbeatSender_BodyIncludesBindAddr asserts the heartbeat
// POST body carries the BindAddr field from HeartbeatStatus verbatim
// so the control plane's recovery path can repair the registry's Host
// column. The agent sends raw bind_addr (e.g. "0.0.0.0:9091") and the
// server applies the dialableFleetNodeHost rewrite — keeping the wildcard
// fix-up logic in one place (the existing helper in
// pkg/api/rest/handlers_fleet_enroll.go) instead of duplicating it
// into the agent.
func TestHTTPHeartbeatSender_BodyIncludesBindAddr(t *testing.T) {
	t.Parallel()
	var bodyStore atomic.Value
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodyStore.Store(string(b))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	sender, err := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: srv.URL,
		TenantID:   "tenant-a",
		HTTPClient: tlsClientFromServer(srv),
		Tokens:     &staticToken{tokens: []string{"tok-1"}},
		Logger:     &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewHTTPHeartbeatSender: %v", err)
	}

	if err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{
		BindAddr: "0.0.0.0:9091",
	}); err != nil {
		t.Fatalf("SendHeartbeat: %v", err)
	}

	got, _ := bodyStore.Load().(string)
	if !strings.Contains(got, `"bind_addr":"0.0.0.0:9091"`) {
		t.Errorf("body missing bind_addr; got %s", got)
	}
}

// TestHTTPHeartbeatSender_BodyOmitsEmptyBindAddrValue asserts the
// heartbeat still POSTs successfully when BindAddr is empty (a
// pre-Start daemon, or a HeartbeatStatus the caller built without
// observing the spawn server's listener). The body still contains the
// "bind_addr" key with an empty value — the server's recovery path
// treats empty-string as "fall back to cert-derived host", so we get
// graceful degradation instead of a parse error.
func TestHTTPHeartbeatSender_BodyOmitsEmptyBindAddrValue(t *testing.T) {
	t.Parallel()
	var bodyStore atomic.Value
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodyStore.Store(string(b))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	sender, _ := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: srv.URL,
		TenantID:   "tenant-a",
		HTTPClient: tlsClientFromServer(srv),
		Tokens:     &staticToken{tokens: []string{"tok-1"}},
		Logger:     &testLogger{t: t},
	})

	if err := sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{}); err != nil {
		t.Fatalf("SendHeartbeat: %v", err)
	}
	got, _ := bodyStore.Load().(string)
	if !strings.Contains(got, `"bind_addr":""`) {
		t.Errorf("body should encode an empty bind_addr; got %s", got)
	}
}
