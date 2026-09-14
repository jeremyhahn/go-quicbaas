// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// =============================================================================
// go-quicbaas#653 — the daemon does not name its own authority.
//
// A client_credentials token carries the grant on the application record. A
// daemon that names a scope list can only narrow that grant, never widen it,
// and a list persisted by an older enrollment narrows it to a set that cannot
// heartbeat — permanently, because the daemon re-enrolls on 401 and 404 but
// not on the 403 the narrowed token earns. Asking for nothing is the only
// request that self-heals.
// =============================================================================

// scopeCapturingServer records the form the daemon posts to each token
// endpoint and answers with a grant the control plane resolved.
type scopeCapturingServer struct {
	t            *testing.T
	grantedScope string

	tokenForm     url.Values
	tokenMTLSForm url.Values
}

func (s *scopeCapturingServer) handler() http.Handler {
	mux := http.NewServeMux()
	respond := func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"jwt","token_type":"Bearer","expires_in":3600,"scope":"` + s.grantedScope + `"}`))
	}
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			s.t.Errorf("parse form: %v", err)
		}
		s.tokenForm = r.PostForm
		respond(w)
	})
	mux.HandleFunc("/oauth2/token-mtls", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			s.t.Errorf("parse form: %v", err)
		}
		s.tokenMTLSForm = r.PostForm
		respond(w)
	})
	return mux
}

// staleScopeCreds is a credentials file written by a build that advertised
// only the service-surface scopes: the exact state every already-deployed
// daemon is in.
func staleScopeCreds() *persistedCredentials {
	return &persistedCredentials{
		ClientID:         "app_automatethethings_stale",
		ClientSecret:     "sk_stale",
		MTLSClientID:     "app_automatethethings_stale-mtls",
		AgentID:          "agent-automatethethings-vstation",
		ServiceAccountID: "agent-automatethethings-vstation",
		Role:             "agent-runtime",
		Scopes:           []string{"agent:runtime:spawn", "agent:runtime:prompt", "agent:runtime:status"},
	}
}

// TestExchangeToken_RequestsNoScope proves the secret-authenticated exchange
// leaves the scope to the control plane.
func TestExchangeToken_RequestsNoScope(t *testing.T) {
	t.Parallel()
	stub := &scopeCapturingServer{t: t, grantedScope: "agent:runtime:spawn orchestrator:agent_heartbeat"}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	enroller := newProdEnroller(t, srv.Client())

	if _, err := enroller.exchangeToken(context.Background(), cfg, staleScopeCreds(), srv.Client()); err != nil {
		t.Fatalf("exchangeToken: %v", err)
	}
	if got, ok := stub.tokenForm["scope"]; ok {
		t.Fatalf("the daemon named its own scope %q; the grant is the control plane's to resolve", strings.Join(got, " "))
	}
}

// TestExchangeTokenMTLS_RequestsNoScope is the same for the certificate-
// authenticated endpoint, which is the one a current daemon prefers.
func TestExchangeTokenMTLS_RequestsNoScope(t *testing.T) {
	t.Parallel()
	stub := &scopeCapturingServer{t: t, grantedScope: "agent:runtime:spawn orchestrator:agent_heartbeat"}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	enroller := newProdEnroller(t, srv.Client())

	if _, err := enroller.exchangeTokenMTLS(context.Background(), cfg, staleScopeCreds(), srv.Client()); err != nil {
		t.Fatalf("exchangeTokenMTLS: %v", err)
	}
	if got, ok := stub.tokenMTLSForm["scope"]; ok {
		t.Fatalf("the daemon named its own scope %q; the grant is the control plane's to resolve", strings.Join(got, " "))
	}
	if got := stub.tokenMTLSForm.Get("client_id"); got != staleScopeCreds().MTLSClientID {
		t.Fatalf("client_id=%q, want the mTLS client", got)
	}
}

// TestBuildIdentity_AdoptsTheResolvedGrant proves the daemon self-heals: the
// identity it serves from carries what the control plane granted, not the
// stale list its credentials file was written with.
func TestBuildIdentity_AdoptsTheResolvedGrant(t *testing.T) {
	t.Parallel()
	enroller := &ProductionEnroller{}
	cfg := &Config{TenantID: "automatethethings"}

	identity := enroller.buildIdentity(cfg, staleScopeCreds(), &tokenResponse{
		AccessToken: "jwt",
		Scope:       "agent:runtime:spawn agent:runtime:prompt agent:runtime:status orchestrator:agent_heartbeat orchestrator:hook",
	})
	if !identity.HasScope("orchestrator:agent_heartbeat") {
		t.Fatalf("identity scopes %v do not carry the heartbeat grant", identity.Scopes)
	}
	if !identity.HasScope("agent:runtime:spawn") {
		t.Fatalf("identity scopes %v dropped the service-surface grant", identity.Scopes)
	}

	// Error path: a control plane that resolves no grant leaves the daemon on
	// its registration-time set rather than on an empty one it cannot serve
	// its own spawn RPCs with.
	fallback := enroller.buildIdentity(cfg, staleScopeCreds(), &tokenResponse{AccessToken: "jwt"})
	if !fallback.HasScope("agent:runtime:spawn") {
		t.Fatalf("identity scopes %v lost the registration-time set", fallback.Scopes)
	}
}
