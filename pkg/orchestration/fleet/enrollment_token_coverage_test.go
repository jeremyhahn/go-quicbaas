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
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// mtlsCreds returns a persistedCredentials set that triggers the mTLS
// token-exchange path inside acquireToken.
func mtlsCreds() *persistedCredentials {
	return &persistedCredentials{
		ClientID:         "c",
		ClientSecret:     "s",
		MTLSClientID:     "c-mtls",
		AgentID:          "a",
		ServiceAccountID: "sa",
		Role:             "agent-runtime",
		Scopes: []string{
			orchestration.AgentScopeRuntimeSpawn,
			orchestration.AgentScopeRuntimePrompt,
			orchestration.AgentScopeRuntimeStatus,
		},
	}
}

// TestAcquireToken_MTLSPathSuccess drives acquireToken → exchangeTokenMTLS
// happily when credentials carry an MTLSClientID.
func TestAcquireToken_MTLSPathSuccess(t *testing.T) {
	t.Parallel()
	stub := &serverStub{
		t: t,
		tokenMTLSBody: mustJSON(t, map[string]any{
			"access_token": "mtls-jwt",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	enroller := newProdEnroller(t, srv.Client())

	tok, err := enroller.acquireToken(context.Background(), cfg, mtlsCreds(), srv.Client())
	if err != nil {
		t.Fatalf("acquireToken: %v", err)
	}
	if tok.AccessToken != "mtls-jwt" {
		t.Fatalf("token=%q want mtls-jwt", tok.AccessToken)
	}
	if stub.tokenMTLSHits.Load() != 1 {
		t.Fatalf("mtls hits=%d want 1", stub.tokenMTLSHits.Load())
	}
}

// TestAcquireToken_MTLSFallbackToBasic exercises the fallback branch:
// the mTLS endpoint fails (500), so acquireToken retries via the
// client_secret token endpoint.
func TestAcquireToken_MTLSFallbackToBasic(t *testing.T) {
	t.Parallel()
	stub := &serverStub{
		t:               t,
		tokenMTLSStatus: 500,
		tokenMTLSBody:   []byte(`{"error":"server_error"}`),
		tokenBody: mustJSON(t, map[string]any{
			"access_token": "basic-jwt",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	enroller := newProdEnroller(t, srv.Client())

	tok, err := enroller.acquireToken(context.Background(), cfg, mtlsCreds(), srv.Client())
	if err != nil {
		t.Fatalf("acquireToken fallback: %v", err)
	}
	if tok.AccessToken != "basic-jwt" {
		t.Fatalf("token=%q want basic-jwt", tok.AccessToken)
	}
	if stub.tokenMTLSHits.Load() != 1 || stub.tokenHits.Load() != 1 {
		t.Fatalf("mtls=%d basic=%d want 1/1", stub.tokenMTLSHits.Load(), stub.tokenHits.Load())
	}
}

// TestExchangeTokenMTLS_ErrorResponse covers the non-2xx and empty-token
// error branches directly.
func TestExchangeTokenMTLS_ErrorResponse(t *testing.T) {
	t.Parallel()
	stub := &serverStub{
		t:               t,
		tokenMTLSStatus: 403,
		tokenMTLSBody:   []byte(`{"error":"forbidden"}`),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	enroller := newProdEnroller(t, srv.Client())

	_, err := enroller.exchangeTokenMTLS(context.Background(), cfg, mtlsCreds(), srv.Client())
	if err == nil {
		t.Fatal("expected error from 403")
	}
	var xErr *EnrollmentTokenExchangeError
	if !errors.As(err, &xErr) {
		t.Fatalf("want EnrollmentTokenExchangeError, got %T", err)
	}
	if xErr.Status != 403 {
		t.Fatalf("status=%d want 403", xErr.Status)
	}
}

// TestExchangeTokenMTLS_EmptyAccessToken covers the validate branch when
// the server returns 200 with no access_token.
func TestExchangeTokenMTLS_EmptyAccessToken(t *testing.T) {
	t.Parallel()
	stub := &serverStub{
		t:             t,
		tokenMTLSBody: mustJSON(t, map[string]any{"token_type": "Bearer", "expires_in": 60}),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	enroller := newProdEnroller(t, srv.Client())

	_, err := enroller.exchangeTokenMTLS(context.Background(), cfg, mtlsCreds(), srv.Client())
	if err == nil {
		t.Fatal("expected empty access_token error")
	}
}

// -----------------------------------------------------------------------------
// RefreshTokenFn
// -----------------------------------------------------------------------------

// TestRefreshTokenFn_Success persists credentials, then exercises the
// refresh closure which reads them and exchanges a fresh JWT.
func TestRefreshTokenFn_Success(t *testing.T) {
	t.Parallel()
	stub := &serverStub{
		t: t,
		tokenBody: mustJSON(t, map[string]any{
			"access_token": "refreshed-jwt",
			"token_type":   "Bearer",
			"expires_in":   120,
		}),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	creds := &persistedCredentials{
		ClientID:         "c",
		ClientSecret:     "s",
		AgentID:          "a",
		ServiceAccountID: "sa",
		Role:             "agent-runtime",
		Scopes: []string{
			orchestration.AgentScopeRuntimeSpawn,
			orchestration.AgentScopeRuntimePrompt,
			orchestration.AgentScopeRuntimeStatus,
		},
	}
	if err := writeCredentials(credentialsPath(cfg), creds); err != nil {
		t.Fatalf("seed creds: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	refresh := enroller.RefreshTokenFn(cfg)
	tok, exp, err := refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if tok != "refreshed-jwt" {
		t.Fatalf("tok=%q", tok)
	}
	if exp.IsZero() {
		t.Fatal("expected non-zero expiry")
	}
}

// TestRefreshTokenFn_MissingCredentials covers the no-credentials branch.
func TestRefreshTokenFn_MissingCredentials(t *testing.T) {
	t.Parallel()
	cfg := newEnrollerConfig(t, "https://unused.test")
	enroller := newProdEnroller(t, nil)
	refresh := enroller.RefreshTokenFn(cfg)
	_, _, err := refresh(context.Background())
	if !errors.Is(err, ErrEnrollmentMissingCredentials) {
		t.Fatalf("want ErrEnrollmentMissingCredentials, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// Heartbeat error types — Error() string formatting
// -----------------------------------------------------------------------------

func TestReregisterFailedError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	e := &ReregisterFailedError{Attempts: 3, Cause: errors.New("dns down")}
	msg := e.Error()
	if !strings.Contains(msg, "3 attempts") || !strings.Contains(msg, "dns down") {
		t.Fatalf("unexpected message: %q", msg)
	}
	if !errors.Is(e, ErrNodeReRegisterFailed) {
		t.Fatal("Unwrap should expose ErrNodeReRegisterFailed")
	}
}

func TestHeartbeatSendError_ErrorVariants(t *testing.T) {
	t.Parallel()
	cause := errors.New("conn reset")
	cases := []struct {
		err  *HeartbeatSendError
		want []string
	}{
		{&HeartbeatSendError{Op: "post", Status: 500, Cause: cause}, []string{"post", "500", "conn reset"}},
		{&HeartbeatSendError{Op: "post", Status: 404}, []string{"post", "404"}},
		{&HeartbeatSendError{Op: "dial", Cause: cause}, []string{"dial", "conn reset"}},
		{&HeartbeatSendError{Op: "init"}, []string{"init"}},
	}
	for _, tc := range cases {
		msg := tc.err.Error()
		for _, w := range tc.want {
			if !strings.Contains(msg, w) {
				t.Fatalf("Error()=%q missing %q", msg, w)
			}
		}
	}
	// Unwrap exposes the cause when present.
	if !errors.Is(&HeartbeatSendError{Op: "x", Cause: cause}, cause) {
		t.Fatal("Unwrap should expose cause")
	}
}
