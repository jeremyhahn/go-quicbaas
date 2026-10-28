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

//go:build integration

package helpers

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// shardProbeNode stands in for one cluster node: its PIV login answers
// loginStatus for the first refusals logins and then issues token; every
// other request answers probeStatus and records its Authorization header.
type shardProbeNode struct {
	server  *httptest.Server
	headers chan string
	logins  atomic.Int32
}

func newShardProbeNode(t *testing.T, token string, refusals int32, loginStatus, probeStatus int) *shardProbeNode {
	t.Helper()
	node := &shardProbeNode{headers: make(chan string, 1)}
	node.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != pivLoginPath {
			select {
			case node.headers <- r.Header.Get("Authorization"):
			default:
			}
			w.WriteHeader(probeStatus)
			if _, err := w.Write([]byte(`{"error":"probe"}`)); err != nil {
				t.Errorf("write probe response: %v", err)
			}
			return
		}
		body := `{"access_token":"` + token + `","token_type":"Bearer","expires_in":900}`
		if node.logins.Add(1) <= refusals {
			w.WriteHeader(loginStatus)
			body = `{"error":"refused"}`
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write login response: %v", err)
		}
	}))
	node.server.TLS = &tls.Config{ClientAuth: tls.RequestClientCert}
	node.server.StartTLS()
	t.Cleanup(node.server.Close)
	return node
}

// builderFor returns a builder whose one node is node, trusting its TLS
// certificate and, when admin is true, logging in as the system admin.
func builderFor(t *testing.T, node *shardProbeNode, admin bool) *ClusterBuilder {
	t.Helper()
	transport, ok := node.server.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatal("httptest client transport is not *http.Transport")
	}
	cb := NewClusterBuilder().WithEndpoints([]string{node.server.URL}).WithRetries(0)
	cb.restEndpoints = []string{node.server.URL}
	cb.tlsConfig = func() (*tls.Config, error) { return transport.TLSClientConfig, nil }
	if admin {
		certPath, keyPath, caPath := pivLoginFiles(t, node.server)
		cb.login = pivLogin(certPath, keyPath, caPath)
	}
	return cb
}

// probeAuthorization builds cb's cluster, sends one Status probe and returns
// the Authorization header the node saw.
func probeAuthorization(t *testing.T, cb *ClusterBuilder, node *shardProbeNode) string {
	t.Helper()
	tc, err := cb.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() {
		if err := tc.Close(); err != nil {
			t.Errorf("close cluster: %v", err)
		}
	})
	if _, err := tc.Clients[0].Cluster().Status(context.Background()); err == nil {
		t.Fatal("Status against the refusing probe node succeeded")
	}
	return <-node.headers
}

// require_authz makes Cluster().Status need admin:status, so the readiness
// probe must carry the bearer its own node issued to the system admin.
func TestClusterBuilder_WithSystemAdmin_ProbeCarriesNodeBearer(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusForbidden)
	if got := probeAuthorization(t, builderFor(t, node, true), node); got != "Bearer admin-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer admin-token")
	}
}

func TestClusterBuilder_DefaultProbeCarriesNoBearer(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusForbidden)
	if got := probeAuthorization(t, builderFor(t, node, false), node); got != "" {
		t.Errorf("Authorization = %q, want none from the default builder", got)
	}
}

func TestClusterBuilder_WithSystemAdmin_SetsLogin(t *testing.T) {
	if NewClusterBuilder().login != nil {
		t.Error("default builder logs in, want unauthenticated clients")
	}
	if NewClusterBuilder().WithSystemAdmin().login == nil {
		t.Error("WithSystemAdmin left the builder without a login")
	}
}

// A node still electing refuses the login; the builder retries it within its
// retries instead of failing on the first refusal.
func TestClusterBuilder_RetriesRefusedLogin(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 1, http.StatusServiceUnavailable, http.StatusForbidden)
	cb := builderFor(t, node, true).WithRetries(1)
	if got := probeAuthorization(t, cb, node); got != "Bearer admin-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer admin-token")
	}
	if got := node.logins.Load(); got != 2 {
		t.Errorf("logins = %d, want 2", got)
	}
}

func TestClusterBuilder_RefusedLoginFails(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 1, http.StatusUnauthorized, http.StatusOK)
	_, err := builderFor(t, node, true).Build(context.Background())
	var authErr *PrincipalAuthError
	if !errors.As(err, &authErr) || authErr.Principal != systemAdminPrincipal || authErr.Step != "login" {
		t.Fatalf("error = %v, want system admin login *PrincipalAuthError", err)
	}
	var badStatus *AuthBadStatusError
	if !errors.As(err, &badStatus) || badStatus.Status != http.StatusUnauthorized {
		t.Errorf("cause = %v, want *AuthBadStatusError with status 401", err)
	}
}

func TestClusterBuilder_SystemAdminWithoutRESTEndpointFails(t *testing.T) {
	cb := NewClusterBuilder().WithEndpoints([]string{"node1:50051"}).WithSystemAdmin()
	cb.restEndpoints = nil
	_, err := cb.Build(context.Background())
	var restErr *ClusterRESTEndpointError
	if !errors.As(err, &restErr) || restErr.Node != 1 || restErr.Endpoint != "node1:50051" {
		t.Fatalf("error = %v, want *ClusterRESTEndpointError for node 1", err)
	}
	if restErr.Error() == "" {
		t.Error("ClusterRESTEndpointError has an empty message")
	}
}

func TestWaitForClusterShardLeaders_AuthenticatedProbeSucceeds(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusForbidden)
	if err := waitForClusterShardLeaders(context.Background(), builderFor(t, node, true), nil, time.Second); err != nil {
		t.Fatalf("waitForClusterShardLeaders: %v", err)
	}
}

// A refused login is named in the timeout, not reported only as a missing
// leader.
func TestWaitForClusterShardLeaders_RefusedLoginSurfaces(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 1<<30, http.StatusUnauthorized, http.StatusOK)
	err := waitForClusterShardLeaders(context.Background(), builderFor(t, node, true), []uint64{1}, 300*time.Millisecond)
	var leadersErr *ShardLeadersError
	if !errors.As(err, &leadersErr) {
		t.Fatalf("error = %v, want *ShardLeadersError", err)
	}
	var badStatus *AuthBadStatusError
	if !errors.As(leadersErr.Err, &badStatus) || badStatus.Status != http.StatusUnauthorized {
		t.Errorf("ShardLeadersError.Err = %v, want the login's *AuthBadStatusError 401", leadersErr.Err)
	}
}

// A refused Status probe is kept as the poll's last error.
func TestWaitForClusterShardLeaders_RefusedStatusSurfaces(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusForbidden)
	err := waitForClusterShardLeaders(context.Background(), builderFor(t, node, true), []uint64{1}, 300*time.Millisecond)
	var leadersErr *ShardLeadersError
	if !errors.As(err, &leadersErr) {
		t.Fatalf("error = %v, want *ShardLeadersError", err)
	}
	var timeoutErr *PollTimeoutError
	if !errors.As(leadersErr.Err, &timeoutErr) || timeoutErr.LastErr == nil {
		t.Errorf("ShardLeadersError.Err = %v, want a *PollTimeoutError carrying the refused probe", leadersErr.Err)
	}
}
