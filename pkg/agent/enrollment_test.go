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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// serverStub is a minimal in-memory fake of the QuicBaaS server's
// mTLS register endpoint plus OAuth2 token endpoint. Tests control
// status codes and bodies per endpoint.
type serverStub struct {
	t *testing.T

	registerStatus int
	registerBody   []byte

	tokenStatus int
	tokenBody   []byte

	tokenMTLSStatus int
	tokenMTLSBody   []byte

	registerHits  atomic.Int32
	tokenHits     atomic.Int32
	tokenMTLSHits atomic.Int32
}

func (s *serverStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agents/register", func(w http.ResponseWriter, r *http.Request) {
		s.registerHits.Add(1)
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			s.t.Errorf("register Content-Type: got %q, want application/json", ct)
		}
		status := s.registerStatus
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(s.registerBody)
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		s.tokenHits.Add(1)
		if err := r.ParseForm(); err != nil {
			s.t.Errorf("token ParseForm: %v", err)
		}
		if got := r.PostForm.Get("grant_type"); got != "client_credentials" {
			s.t.Errorf("token grant_type: got %q, want client_credentials", got)
		}
		if _, _, ok := r.BasicAuth(); !ok {
			s.t.Error("token endpoint missing Basic auth")
		}
		status := s.tokenStatus
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(s.tokenBody)
	})
	mux.HandleFunc("/oauth2/token-mtls", func(w http.ResponseWriter, r *http.Request) {
		s.tokenMTLSHits.Add(1)
		if err := r.ParseForm(); err != nil {
			s.t.Errorf("token-mtls ParseForm: %v", err)
		}
		if got := r.PostForm.Get("grant_type"); got != "client_credentials" {
			s.t.Errorf("token-mtls grant_type: got %q, want client_credentials", got)
		}
		// mTLS endpoint uses client_id in form body, not Basic auth.
		if r.PostForm.Get("client_id") == "" {
			s.t.Error("token-mtls missing client_id form field")
		}
		status := s.tokenMTLSStatus
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(s.tokenMTLSBody)
	})
	return mux
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// newEnrollerConfig returns a Config whose WorkDir is scoped to the
// test. The production-style cert/key/ca fields are always populated
// because Validate() now requires them even when tests inject their
// own HTTPClient.
func newEnrollerConfig(t *testing.T, serverAddr string) *Config {
	t.Helper()
	tmp := t.TempDir()
	cert, key, ca := writeTestAgentIdentity(t, tmp)
	return &Config{
		ServerAddr:    serverAddr,
		TenantID:      "tenant-a",
		BindAddr:      "127.0.0.1:0",
		WorkDir:       filepath.Join(tmp, "agent"),
		AgentCertFile: cert,
		AgentKeyFile:  key,
		ServerCAFile:  ca,
	}
}

func newProdEnroller(t *testing.T, client *http.Client) *ProductionEnroller {
	t.Helper()
	return NewProductionEnroller(ProductionEnrollerDeps{
		HTTPClient: client,
		Logger:     &testLogger{t: t},
	})
}

// TestProductionEnroller_FirstStart_PersistsAndReuses verifies that the
// first Enroll call issues both HTTP requests, persists credentials.json
// with mode 0600, and that a second Enroll skips the register POST
// entirely — reading the cached credentials instead.
func TestProductionEnroller_FirstStart_PersistsAndReuses(t *testing.T) {
	t.Parallel()

	stub := &serverStub{
		t: t,
		registerBody: mustJSON(t, map[string]any{
			"client_id":          "client-abc",
			"client_secret":      "secret-xyz",
			"agent_id":           "agent-001",
			"service_account_id": "sa-001",
			"role":               "agent-runtime",
			"scopes": []string{
				orchestration.AgentScopeRuntimeSpawn,
				orchestration.AgentScopeRuntimePrompt,
				orchestration.AgentScopeRuntimeStatus,
			},
		}),
		tokenBody: mustJSON(t, map[string]any{
			"access_token": "jwt-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"scope": strings.Join([]string{
				orchestration.AgentScopeRuntimeSpawn,
				orchestration.AgentScopeRuntimePrompt,
				orchestration.AgentScopeRuntimeStatus,
			}, " "),
		}),
	}

	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())

	ident, err := enroller.Enroll(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first Enroll: %v", err)
	}
	if ident.AgentID != "agent-001" {
		t.Errorf("AgentID: got %q, want %q", ident.AgentID, "agent-001")
	}
	if ident.TenantID != cfg.TenantID {
		t.Errorf("TenantID: got %q, want %q", ident.TenantID, cfg.TenantID)
	}
	if !ident.HasScope(orchestration.AgentScopeRuntimeSpawn) {
		t.Errorf("missing runtime:spawn scope, got %v", ident.Scopes)
	}
	if err := ident.Validate(); err != nil {
		t.Errorf("identity validate: %v", err)
	}
	if got := stub.registerHits.Load(); got != 1 {
		t.Errorf("register hits (first): got %d, want 1", got)
	}
	if got := stub.tokenHits.Load(); got != 1 {
		t.Errorf("token hits (first): got %d, want 1", got)
	}

	info, err := os.Stat(credentialsPath(cfg))
	if err != nil {
		t.Fatalf("stat credentials.json: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("credentials.json mode: got %o, want 0600", mode)
	}

	// Second start — must NOT hit register (credentials cached).
	ident2, err := enroller.Enroll(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second Enroll: %v", err)
	}
	if ident2.AgentID != "agent-001" {
		t.Errorf("second AgentID: got %q, want %q", ident2.AgentID, "agent-001")
	}
	if got := stub.registerHits.Load(); got != 1 {
		t.Errorf("register hits (second): got %d, want 1 (cached)", got)
	}
	if got := stub.tokenHits.Load(); got != 2 {
		t.Errorf("token hits (second): got %d, want 2", got)
	}
}

// TestProductionEnroller_RegistrationRejected_401 verifies that a 401
// from the server surfaces ErrEnrollmentRejected and does NOT persist
// any credentials file.
func TestProductionEnroller_RegistrationRejected_401(t *testing.T) {
	t.Parallel()

	stub := &serverStub{
		t:              t,
		registerStatus: http.StatusUnauthorized,
		registerBody:   []byte(`{"error":"invalid_client"}`),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())

	_, err := enroller.Enroll(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if !errors.Is(err, ErrEnrollmentRejected) {
		t.Fatalf("want ErrEnrollmentRejected, got %v", err)
	}
	if _, statErr := os.Stat(credentialsPath(cfg)); !os.IsNotExist(statErr) {
		t.Fatalf("credentials file should not exist after rejection, stat err=%v", statErr)
	}
}

// TestProductionEnroller_TokenExchangeBadURL surfaces the URL parse
// failure path inside exchangeToken when ServerAddr is malformed.
func TestProductionEnroller_TokenExchangeBadURL(t *testing.T) {
	t.Parallel()

	stub := &serverStub{
		t: t,
		registerBody: mustJSON(t, map[string]any{
			"client_id":          "c",
			"client_secret":      "s",
			"agent_id":           "a",
			"service_account_id": "sa",
			"scopes":             []string{orchestration.AgentScopeRuntimeSpawn},
		}),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	enroller.tokenPath = "://bad-url-no-scheme"

	_, err := enroller.Enroll(context.Background(), cfg)
	if !errors.Is(err, ErrEnrollmentTokenExchange) {
		t.Fatalf("err: got %v, want wraps ErrEnrollmentTokenExchange", err)
	}
	var texErr *EnrollmentTokenExchangeError
	if !errors.As(err, &texErr) {
		t.Fatalf("err type: got %T, want *EnrollmentTokenExchangeError", err)
	}
}

// TestProductionEnroller_EmptyAccessToken tests the token-validation
// path when the server returns a 200 but with an empty access_token.
func TestProductionEnroller_EmptyAccessToken(t *testing.T) {
	t.Parallel()

	stub := &serverStub{
		t: t,
		registerBody: mustJSON(t, map[string]any{
			"client_id":          "c",
			"client_secret":      "s",
			"agent_id":           "a",
			"service_account_id": "sa",
			"scopes":             []string{orchestration.AgentScopeRuntimeSpawn},
		}),
		tokenBody: mustJSON(t, map[string]any{
			"access_token": "",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	_, err := enroller.Enroll(context.Background(), cfg)
	if !errors.Is(err, ErrEnrollmentTokenExchange) {
		t.Fatalf("err: got %v, want wraps ErrEnrollmentTokenExchange", err)
	}
}

// TestProductionEnroller_NilConfig guards the nil-check path.
func TestProductionEnroller_NilConfig(t *testing.T) {
	t.Parallel()
	enroller := NewProductionEnroller(ProductionEnrollerDeps{
		Logger: &testLogger{t: t},
	})
	if _, err := enroller.Enroll(context.Background(), nil); !errors.Is(err, ErrConfigNil) {
		t.Fatalf("want ErrConfigNil, got %v", err)
	}
}

// TestProductionEnroller_RefreshTokenFn exercises the CachingTokenSource
// integration path: after one successful Enroll, RefreshTokenFn must
// return a token drawn from the same cached credentials on every call.
func TestProductionEnroller_RefreshTokenFn(t *testing.T) {
	t.Parallel()

	stub := &serverStub{
		t: t,
		registerBody: mustJSON(t, map[string]any{
			"client_id":          "c",
			"client_secret":      "s",
			"agent_id":           "a",
			"service_account_id": "sa",
			"scopes":             []string{orchestration.AgentScopeRuntimeSpawn},
		}),
		tokenBody: mustJSON(t, map[string]any{
			"access_token": "tok",
			"token_type":   "Bearer",
			"expires_in":   60,
		}),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	if _, err := enroller.Enroll(context.Background(), cfg); err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	refresh := enroller.RefreshTokenFn(cfg)
	token, _, err := refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if token != "tok" {
		t.Fatalf("token: got %q, want tok", token)
	}
}

// TestProductionEnroller_RefreshTokenFn_MissingCredentials surfaces
// ErrEnrollmentMissingCredentials when a caller tries to refresh
// without ever having completed registration.
func TestProductionEnroller_RefreshTokenFn_MissingCredentials(t *testing.T) {
	t.Parallel()
	cfg := newEnrollerConfig(t, "https://nowhere")
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	enroller := NewProductionEnroller(ProductionEnrollerDeps{
		Logger: &testLogger{t: t},
	})
	_, _, err := enroller.RefreshTokenFn(cfg)(context.Background())
	if !errors.Is(err, ErrEnrollmentMissingCredentials) {
		t.Fatalf("want ErrEnrollmentMissingCredentials, got %v", err)
	}
}

// TestReadWriteCredentials round-trips the on-disk credentials file.
func TestReadWriteCredentials(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials.json")
	want := &persistedCredentials{
		ClientID: "c", ClientSecret: "s",
		AgentID: "a", ServiceAccountID: "sa",
		Scopes: []string{"agent:runtime:spawn"},
	}
	if err := writeCredentials(path, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode=%o want 0600", mode)
	}
	got, ok, err := readCredentials(path)
	if err != nil || !ok {
		t.Fatalf("read: ok=%v err=%v", ok, err)
	}
	if got.ClientID != want.ClientID || got.ClientSecret != want.ClientSecret {
		t.Fatalf("round-trip mismatch: %+v vs %+v", got, want)
	}
}

// TestReadCredentials_Missing returns ok=false without an error when
// the credentials file does not exist.
func TestReadCredentials_Missing(t *testing.T) {
	t.Parallel()
	_, ok, err := readCredentials(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("ok=true for missing file")
	}
}

// TestValidateBootstrapResponse ensures that the mandatory fields are
// each surfaced by name when missing.
func TestValidateBootstrapResponse(t *testing.T) {
	base := &persistedCredentials{
		ClientID:         "c",
		ClientSecret:     "s",
		AgentID:          "a",
		ServiceAccountID: "sa",
		Scopes:           []string{"agent:runtime:spawn"},
	}
	if err := validateBootstrapResponse(base); err != nil {
		t.Fatalf("baseline: unexpected error %v", err)
	}
	cases := []struct {
		name    string
		mutate  func(c *persistedCredentials)
		wantSub string
	}{
		{"no client_id", func(c *persistedCredentials) { c.ClientID = "" }, "client_id"},
		// client_secret can be absent when mtls_client_id is present; when both
		// are absent the validator surfaces a clear error.
		{"no client_secret and no mtls_client_id", func(c *persistedCredentials) { c.ClientSecret = "" }, "client_secret"},
		{"no agent_id", func(c *persistedCredentials) { c.AgentID = "" }, "agent_id"},
		{"no service_account_id", func(c *persistedCredentials) { c.ServiceAccountID = "" }, "service_account_id"},
		{"no scopes", func(c *persistedCredentials) { c.Scopes = nil }, "scopes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := *base
			c.Scopes = append([]string(nil), base.Scopes...)
			tc.mutate(&c)
			err := validateBootstrapResponse(&c)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err=%q; want contains %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// drainBody is a convenience for tests that want to assert a response
// body was consumed.
func drainBody(t *testing.T, r io.Reader) {
	t.Helper()
	if _, err := io.ReadAll(r); err != nil {
		t.Fatalf("drain: %v", err)
	}
}

// TestAgentTokenExchange_PrefersMTLS_WhenAvailable verifies that when persisted
// credentials carry an mtls_client_id, the enroller posts to /oauth2/token-mtls
// and does NOT post to /oauth2/token.
func TestAgentTokenExchange_PrefersMTLS_WhenAvailable(t *testing.T) {
	t.Parallel()

	tokenBody := mustJSON(t, map[string]any{
		"access_token": "mtls-jwt",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"scope":        "agent:runtime:spawn",
	})

	stub := &serverStub{
		t:             t,
		tokenMTLSBody: tokenBody,
	}

	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	// Persist credentials with mtls_client_id already set (simulates a new deploy).
	creds := &persistedCredentials{
		ClientID:         "legacy-id",
		ClientSecret:     "legacy-secret",
		AgentID:          "agent-mtls-001",
		ServiceAccountID: "sa-mtls-001",
		Scopes:           []string{"agent:runtime:spawn"},
		MTLSClientID:     "agent-mtls-001-mtls",
	}
	if err := writeCredentials(credentialsPath(cfg), creds); err != nil {
		t.Fatalf("write creds: %v", err)
	}

	enroller := NewProductionEnroller(ProductionEnrollerDeps{
		HTTPClient: srv.Client(),
		Logger:     &testLogger{t: t},
	})

	ident, err := enroller.Enroll(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if ident.AgentID != "agent-mtls-001" {
		t.Errorf("AgentID: got %q, want agent-mtls-001", ident.AgentID)
	}

	// mTLS path must have been hit; legacy path must NOT have been hit.
	if stub.tokenMTLSHits.Load() != 1 {
		t.Errorf("token-mtls endpoint hit count: got %d, want 1", stub.tokenMTLSHits.Load())
	}
	if stub.tokenHits.Load() != 0 {
		t.Errorf("legacy token endpoint hit count: got %d, want 0", stub.tokenHits.Load())
	}
}

// TestAgentTokenExchange_FallsBackToClientSecret_WhenNoMTLS verifies that when
// persisted credentials have no mtls_client_id, the enroller falls back to the
// legacy /oauth2/token endpoint with client_secret Basic auth.
func TestAgentTokenExchange_FallsBackToClientSecret_WhenNoMTLS(t *testing.T) {
	t.Parallel()

	tokenBody := mustJSON(t, map[string]any{
		"access_token": "legacy-jwt",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"scope":        "agent:runtime:spawn",
	})

	stub := &serverStub{
		t:         t,
		tokenBody: tokenBody,
	}

	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	// Persist legacy credentials only (no mtls_client_id).
	creds := &persistedCredentials{
		ClientID:         "legacy-id",
		ClientSecret:     "legacy-secret",
		AgentID:          "agent-legacy-001",
		ServiceAccountID: "sa-legacy-001",
		Scopes:           []string{"agent:runtime:spawn"},
	}
	if err := writeCredentials(credentialsPath(cfg), creds); err != nil {
		t.Fatalf("write creds: %v", err)
	}

	enroller := NewProductionEnroller(ProductionEnrollerDeps{
		HTTPClient: srv.Client(),
		Logger:     &testLogger{t: t},
	})

	ident, err := enroller.Enroll(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if ident.AgentID != "agent-legacy-001" {
		t.Errorf("AgentID: got %q, want agent-legacy-001", ident.AgentID)
	}

	// Legacy token path must have been hit; mTLS path must NOT.
	if stub.tokenHits.Load() != 1 {
		t.Errorf("legacy token endpoint hit count: got %d, want 1", stub.tokenHits.Load())
	}
	if stub.tokenMTLSHits.Load() != 0 {
		t.Errorf("token-mtls endpoint hit count: got %d, want 0", stub.tokenMTLSHits.Load())
	}
}

// TestValidateBootstrapResponse_MTLSClientIDNoSecret verifies that credentials
// with an mtls_client_id but no client_secret are accepted (mTLS-only agents
// need no secret on disk).
func TestValidateBootstrapResponse_MTLSClientIDNoSecret(t *testing.T) {
	t.Parallel()
	creds := &persistedCredentials{
		ClientID:         "c",
		MTLSClientID:     "c-mtls",
		AgentID:          "a",
		ServiceAccountID: "sa",
		Scopes:           []string{"agent:runtime:spawn"},
		// ClientSecret intentionally empty
	}
	if err := validateBootstrapResponse(creds); err != nil {
		t.Fatalf("expected nil error for mtls-only creds, got %v", err)
	}
}
