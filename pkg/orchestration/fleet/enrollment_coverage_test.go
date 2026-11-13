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
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/agentcert"
)

// reregisterStub builds a serverStub whose register + token endpoints
// mint a fresh credential set, suitable for driving Reregister.
func reregisterStub(t *testing.T) *serverStub {
	t.Helper()
	return &serverStub{
		t: t,
		registerBody: mustJSON(t, map[string]any{
			"client_id":          "rr-client",
			"client_secret":      "rr-secret",
			"agent_id":           "rr-agent",
			"service_account_id": "rr-sa",
			"role":               "agent-runtime",
			"scopes": []string{
				orchestration.AgentScopeRuntimeSpawn,
				orchestration.AgentScopeRuntimePrompt,
				orchestration.AgentScopeRuntimeStatus,
			},
		}),
		tokenBody: mustJSON(t, map[string]any{
			"access_token": "rr-jwt",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"scope": strings.Join([]string{
				orchestration.AgentScopeRuntimeSpawn,
				orchestration.AgentScopeRuntimePrompt,
				orchestration.AgentScopeRuntimeStatus,
			}, " "),
		}),
	}
}

// TestProductionEnroller_ReregisterWithConfig_Success drives the
// re-registration path end-to-end: it re-bootstraps via register,
// overwrites credentials.json, and exchanges a fresh token.
func TestProductionEnroller_ReregisterWithConfig_Success(t *testing.T) {
	t.Parallel()
	stub := reregisterStub(t)
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())

	ident, err := enroller.ReregisterWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ReregisterWithConfig: %v", err)
	}
	if ident.ClientID != "rr-client" {
		t.Fatalf("ClientID=%q want rr-client", ident.ClientID)
	}
	if stub.registerHits.Load() != 1 {
		t.Fatalf("register hits=%d want 1", stub.registerHits.Load())
	}
	// credentials.json must have been overwritten with fresh material.
	creds, ok, err := readCredentials(credentialsPath(cfg))
	if err != nil || !ok {
		t.Fatalf("readCredentials: ok=%v err=%v", ok, err)
	}
	if creds.ClientSecret != "rr-secret" {
		t.Fatalf("persisted secret=%q", creds.ClientSecret)
	}
}

// TestProductionEnroller_Reregister_UsesBoundConfig drives the thin
// Reregister wrapper, which forwards to ReregisterWithConfig using the
// config previously bound via SetConfig.
func TestProductionEnroller_Reregister_UsesBoundConfig(t *testing.T) {
	t.Parallel()
	stub := reregisterStub(t)
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	enroller.SetConfig(cfg)

	ident, err := enroller.Reregister(context.Background())
	if err != nil {
		t.Fatalf("Reregister: %v", err)
	}
	if ident.AgentID != "rr-agent" {
		t.Fatalf("AgentID=%q want rr-agent", ident.AgentID)
	}
}

// TestProductionEnroller_ReregisterWithConfig_NilConfig covers the
// guard branch.
func TestProductionEnroller_ReregisterWithConfig_NilConfig(t *testing.T) {
	t.Parallel()
	enroller := newProdEnroller(t, nil)
	_, err := enroller.ReregisterWithConfig(context.Background(), nil)
	if !errors.Is(err, ErrConfigNil) {
		t.Fatalf("want ErrConfigNil, got %v", err)
	}
}

// TestProductionEnroller_ReregisterWithConfig_RegisterRejected confirms
// a server rejection at the bootstrap stage surfaces an error without
// touching credentials.json.
func TestProductionEnroller_ReregisterWithConfig_RegisterRejected(t *testing.T) {
	t.Parallel()
	stub := &serverStub{
		t:              t,
		registerStatus: 401,
		registerBody:   []byte(`{"error":"invalid_client"}`),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	_, err := enroller.ReregisterWithConfig(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected register rejection error")
	}
	if _, statErr := os.Stat(credentialsPath(cfg)); !os.IsNotExist(statErr) {
		t.Fatal("credentials.json must not be written on rejection")
	}
}

// -----------------------------------------------------------------------------
// buildMTLSHTTPClient error branches
// -----------------------------------------------------------------------------

func TestBuildMTLSHTTPClient_Errors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cert, key, ca := agentcert.Write(t, dir)

	cases := []struct {
		name string
		cfg  *Config
		op   string
	}{
		{"missing cert", &Config{NodeKeyFile: key, ServerCAFile: ca}, "resolve node cert"},
		{"missing key", &Config{NodeCertFile: cert, ServerCAFile: ca}, "resolve node key"},
		{"bad key pair", &Config{NodeCertFile: filepath.Join(dir, "nope.crt"), NodeKeyFile: filepath.Join(dir, "nope.key")}, "load agent key pair"},
		{"missing server ca", &Config{NodeCertFile: cert, NodeKeyFile: key, ServerCAFile: filepath.Join(dir, "absent-ca.pem")}, "read server ca"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildMTLSHTTPClient(tc.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			var tlsErr *EnrollmentTLSError
			if !errors.As(err, &tlsErr) {
				t.Fatalf("want EnrollmentTLSError, got %T: %v", err, err)
			}
			if tlsErr.Op != tc.op {
				t.Fatalf("op=%q want %q", tlsErr.Op, tc.op)
			}
		})
	}
}

// TestBuildMTLSHTTPClient_BadServerCAPEM covers the "parse server ca"
// branch where the CA file exists but contains no PEM blocks.
func TestBuildMTLSHTTPClient_BadServerCAPEM(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cert, key, _ := agentcert.Write(t, dir)
	badCA := filepath.Join(dir, "bad-ca.pem")
	if err := os.WriteFile(badCA, []byte("not a pem block"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := buildMTLSHTTPClient(&Config{
		NodeCertFile: cert,
		NodeKeyFile:  key,
		ServerCAFile: badCA,
	})
	var tlsErr *EnrollmentTLSError
	if !errors.As(err, &tlsErr) {
		t.Fatalf("want EnrollmentTLSError, got %T: %v", err, err)
	}
	if tlsErr.Op != "parse server ca" {
		t.Fatalf("op=%q want parse server ca", tlsErr.Op)
	}
}

// TestBuildMTLSHTTPClient_Success confirms a fully-valid config yields a
// usable client.
func TestBuildMTLSHTTPClient_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cert, key, ca := agentcert.Write(t, dir)
	client, err := buildMTLSHTTPClient(&Config{
		NodeCertFile: cert,
		NodeKeyFile:  key,
		ServerCAFile: ca,
	})
	if err != nil {
		t.Fatalf("buildMTLSHTTPClient: %v", err)
	}
	if client == nil {
		t.Fatal("nil client")
	}
}

// -----------------------------------------------------------------------------
// Error type Error()/Unwrap() coverage
// -----------------------------------------------------------------------------

func TestEnrollmentTLSError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("root cause")
	e := &EnrollmentTLSError{Op: "load", Cause: cause}
	if !strings.Contains(e.Error(), "load") || !strings.Contains(e.Error(), "root cause") {
		t.Fatalf("unexpected message: %q", e.Error())
	}
	if !errors.Is(e, cause) {
		t.Fatal("Unwrap should expose cause")
	}
}
