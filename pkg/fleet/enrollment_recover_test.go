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
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestProductionEnroller_Enroll_InvalidClientReenrollsFromCache asserts
// the boot-time recovery path: when cached credentials are present on
// disk but the control plane rejects /oauth2/token with 401 invalid_client
// (because the control plane's IAM store was wiped), Enroll() must
// transparently re-bootstrap via /api/v1/fleet/enroll and complete
// successfully with fresh credentials. This is the symmetrical
// counterpart of CachingTokenSource.Token() runtime recovery.
func TestProductionEnroller_Enroll_InvalidClientReenrollsFromCache(t *testing.T) {
	t.Parallel()

	var (
		registerHits atomic.Int32
		tokenHits    atomic.Int32
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/fleet/enroll", func(w http.ResponseWriter, _ *http.Request) {
		registerHits.Add(1)
		// The only register hit in this test is Enroll's recovery
		// re-bootstrap (the stale creds were seeded directly into the
		// credentials file on disk, bypassing the register endpoint).
		// Mint fresh creds the token endpoint will accept.
		clientID := "fresh-client"
		clientSecret := "fresh-secret"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"client_id":"` + clientID + `",
			"client_secret":"` + clientSecret + `",
			"agent_id":"agent-recover",
			"service_account_id":"sa-recover",
			"role":"agent-runtime",
			"scopes":["` + orchestration.AgentScopeRuntimeSpawn + `","` +
			orchestration.AgentScopeRuntimePrompt + `","` +
			orchestration.AgentScopeRuntimeStatus + `"]
		}`))
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		tokenHits.Add(1)
		_ = r.ParseForm()
		// The control plane recognises only the freshly-minted credentials.
		// The stale client_secret on disk is rejected with the exact
		// invalid_client body the production control plane emits.
		_, secret, _ := r.BasicAuth()
		if secret != "fresh-secret" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"client authentication failed"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"jwt-recovered","token_type":"Bearer","expires_in":3600,"scope":"` +
			strings.Join([]string{
				orchestration.AgentScopeRuntimeSpawn,
				orchestration.AgentScopeRuntimePrompt,
				orchestration.AgentScopeRuntimeStatus,
			}, " ") + `"}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	// Seed the cached credentials with the stale secret directly so
	// Enroll's first action is "load cache, exchange, fail with 401".
	staleCreds := &persistedCredentials{
		ClientID:         "stale-client",
		ClientSecret:     "stale-secret",
		AgentID:          "agent-recover",
		ServiceAccountID: "sa-recover",
		Role:             "agent-runtime",
		Scopes: []string{
			orchestration.AgentScopeRuntimeSpawn,
			orchestration.AgentScopeRuntimePrompt,
			orchestration.AgentScopeRuntimeStatus,
		},
	}
	if err := writeCredentials(credentialsPath(cfg), staleCreds); err != nil {
		t.Fatalf("seed stale creds: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	enroller.SetConfig(cfg)

	ident, err := enroller.Enroll(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Enroll should recover via re-enroll, got: %v", err)
	}
	if ident.ClientID != "fresh-client" {
		t.Errorf("ClientID after recovery: got %q, want %q", ident.ClientID, "fresh-client")
	}
	if got := registerHits.Load(); got != 1 {
		t.Errorf("register hits: got %d, want 1 (one re-bootstrap on the invalid_client path)", got)
	}
	// Token endpoint hit twice: first with stale secret (401), then
	// after re-bootstrap with fresh secret (200).
	if got := tokenHits.Load(); got != 2 {
		t.Errorf("token hits: got %d, want 2 (stale 401, then fresh 200)", got)
	}
}

// TestProductionEnroller_Enroll_InvalidClientNoRecoveryWhenBootstrapped
// asserts the boot-time recovery is gated on "credentials came from
// cache". When the very first bootstrap succeeded but the immediately
// following token exchange returns invalid_client (a server bug, since
// the secret was just minted), retrying bootstrap is meaningless and
// would mask the real cause — so the error must propagate.
func TestProductionEnroller_Enroll_InvalidClientNoRecoveryWhenBootstrapped(t *testing.T) {
	t.Parallel()

	var (
		registerHits atomic.Int32
		tokenHits    atomic.Int32
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/fleet/enroll", func(w http.ResponseWriter, _ *http.Request) {
		registerHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"client_id":"c",
			"client_secret":"s",
			"agent_id":"a",
			"service_account_id":"sa",
			"role":"agent-runtime",
			"scopes":["` + orchestration.AgentScopeRuntimeSpawn + `","` +
			orchestration.AgentScopeRuntimePrompt + `","` +
			orchestration.AgentScopeRuntimeStatus + `"]
		}`))
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		tokenHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"client authentication failed"}`))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	enroller.SetConfig(cfg)

	_, err := enroller.Enroll(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected token-exchange error to propagate, got nil")
	}
	if registerHits.Load() != 1 {
		t.Errorf("register hits: got %d, want 1 (no retry — credentials were freshly bootstrapped)",
			registerHits.Load())
	}
}
