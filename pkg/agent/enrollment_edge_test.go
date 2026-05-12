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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// validateBootstrapResponse — each required-field-missing case
// =============================================================================

func TestValidateBootstrapResponse_MissingFieldsReturnRejectedError(t *testing.T) {
	t.Parallel()

	complete := &persistedCredentials{
		ClientID:         "cid",
		ClientSecret:     "sec",
		AgentID:          "aid",
		ServiceAccountID: "said",
		Scopes:           []string{orchestration.AgentScopeRuntimeSpawn},
		Role:             "agent-runtime",
	}

	// Sanity: complete creds pass.
	if err := validateBootstrapResponse(complete); err != nil {
		t.Fatalf("complete creds failed: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*persistedCredentials)
		detail string
	}{
		{"missing client_id", func(c *persistedCredentials) { c.ClientID = "" }, "client_id"},
		{"missing client_secret", func(c *persistedCredentials) { c.ClientSecret = "" }, "client_secret"},
		{"missing agent_id", func(c *persistedCredentials) { c.AgentID = "" }, "agent_id"},
		{"missing service_account_id", func(c *persistedCredentials) { c.ServiceAccountID = "" }, "service_account_id"},
		{"missing scopes", func(c *persistedCredentials) { c.Scopes = nil }, "scopes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Deep-copy the complete struct.
			creds := *complete
			tc.mutate(&creds)
			err := validateBootstrapResponse(&creds)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			var rejErr *EnrollmentRejectedError
			if !errors.As(err, &rejErr) {
				t.Fatalf("expected *EnrollmentRejectedError, got %T: %v", err, err)
			}
			if !strings.Contains(rejErr.Detail, tc.detail) {
				t.Errorf("detail %q does not contain %q", rejErr.Detail, tc.detail)
			}
		})
	}
}

// =============================================================================
// writeCredentials — atomic rename failure when dir is read-only
// =============================================================================

// TestWriteCredentials_FailsOnReadOnlyDir verifies that writeCredentials
// returns an EnrollmentCredentialsSaveError when it cannot create temp files
// due to a read-only directory.
func TestWriteCredentials_FailsOnReadOnlyDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Make the directory read-only so CreateTemp inside writeCredentials fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	path := filepath.Join(dir, "credentials.json")
	creds := &persistedCredentials{
		ClientID:         "c",
		ClientSecret:     "s",
		AgentID:          "a",
		ServiceAccountID: "sa",
		Scopes:           []string{orchestration.AgentScopeRuntimeSpawn},
		Role:             "agent-runtime",
	}
	err := writeCredentials(path, creds)
	if err == nil {
		t.Fatal("expected error writing credentials to read-only dir")
	}
	var saveErr *EnrollmentCredentialsSaveError
	if !errors.As(err, &saveErr) {
		t.Fatalf("expected *EnrollmentCredentialsSaveError, got %T: %v", err, err)
	}
}

// =============================================================================
// bootstrap — non-2xx token exchange after successful enroll
// =============================================================================

// TestProductionEnroller_Bootstrap_Non2xxAfterEnroll simulates a server that
// returns 201 (not 200) for enrollment to verify the 2xx range check is
// inclusive, then tests a non-2xx (503) that must become ErrEnrollmentRejected.
func TestProductionEnroller_Bootstrap_Non2xxStatus(t *testing.T) {
	t.Parallel()

	stub := &serverStub{
		t:            t,
		registerStatus: http.StatusServiceUnavailable,
		registerBody:   []byte(`{"error":"overloaded"}`),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	_, err := enroller.Enroll(context.Background(), cfg)
	if !errors.Is(err, ErrEnrollmentRejected) {
		t.Fatalf("expected ErrEnrollmentRejected for 503, got %v", err)
	}
	var rejErr *EnrollmentRejectedError
	if !errors.As(err, &rejErr) {
		t.Fatalf("expected *EnrollmentRejectedError, got %T", err)
	}
	if rejErr.Status != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", rejErr.Status)
	}
}

// TestProductionEnroller_TokenExchange_AfterCachedCredentials exercises the
// path where credentials are cached on disk (from a prior run) but the token
// exchange step fails with a non-2xx. Enrollment POST is skipped; only the
// token endpoint is hit.
func TestProductionEnroller_TokenExchange_AfterCachedCredentials(t *testing.T) {
	t.Parallel()

	stub := &serverStub{
		t: t,
		registerBody: mustJSON(t, map[string]any{
			"client_id":          "client-cached",
			"client_secret":      "secret-cached",
			"agent_id":           "agent-cached",
			"service_account_id": "sa-cached",
			"role":               "agent-runtime",
			"scopes":             []string{orchestration.AgentScopeRuntimeSpawn},
		}),
		tokenStatus: http.StatusBadGateway,
		tokenBody:   []byte(`{"error":"upstream_unavailable"}`),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Pre-seed the credentials file so the enroller skips bootstrap.
	creds := &persistedCredentials{
		ClientID:         "client-cached",
		ClientSecret:     "secret-cached",
		AgentID:          "agent-cached",
		ServiceAccountID: "sa-cached",
		Role:             "agent-runtime",
		Scopes:           []string{orchestration.AgentScopeRuntimeSpawn},
	}
	path := credentialsPath(cfg)
	data, _ := json.MarshalIndent(creds, "", "  ")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("seed credentials: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	_, err := enroller.Enroll(context.Background(), cfg)
	if !errors.Is(err, ErrEnrollmentTokenExchange) {
		t.Fatalf("expected ErrEnrollmentTokenExchange, got %v", err)
	}
	// Register endpoint must NOT have been hit — we had cached credentials.
	if got := stub.registerHits.Load(); got != 0 {
		t.Errorf("enroll endpoint should not be hit with cached creds, got %d", got)
	}
	// Token endpoint must have been hit.
	if got := stub.tokenHits.Load(); got != 1 {
		t.Errorf("token hits: got %d, want 1", got)
	}
}

// =============================================================================
// Config effective methods — non-default values
// =============================================================================

func TestConfig_EffectiveMethods_NonDefaultValues(t *testing.T) {
	t.Parallel()

	c := newTestConfig(t)
	c.Backends = []BackendKind{BackendACP}
	c.HeartbeatInterval = 5 * time.Second
	c.ShutdownGrace = 10 * time.Second
	c.ContainerRuntime = ContainerRuntimeDocker

	if got := c.EffectiveBackends(); len(got) != 1 || got[0] != BackendACP {
		t.Errorf("EffectiveBackends: got %v, want [acp]", got)
	}
	if got := c.EffectiveHeartbeat(); got != 5*time.Second {
		t.Errorf("EffectiveHeartbeat: got %v, want 5s", got)
	}
	if got := c.EffectiveShutdownGrace(); got != 10*time.Second {
		t.Errorf("EffectiveShutdownGrace: got %v, want 10s", got)
	}
	if got := c.EffectiveContainerRuntime(); got != ContainerRuntimeDocker {
		t.Errorf("EffectiveContainerRuntime: got %v, want docker", got)
	}
}
