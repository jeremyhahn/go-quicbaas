//go:build !integration

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

// lifecycle_remote_resolver_unit_test.go covers the unit-level helpers in
// lifecycle_remote_resolver.go that do not require a live HTTP / OIDC
// stack. centralTokenEndpoint resolves the server's own outbound token URL
// from configuration; the resolver layer above uses it to mint JWTs for
// remote daemon calls. The helper has three branches (empty host, 0.0.0.0
// host, REST port zero) that this test covers explicitly.

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_CentralTokenEndpoint_EmptyHostUsesLoopback(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Host = ""
	srv.config.Server.REST.Port = 7100
	got := srv.centralTokenEndpoint()
	assert.True(t, strings.Contains(got, "127.0.0.1"),
		"empty host must resolve to loopback so the central can dial itself")
	assert.True(t, strings.HasSuffix(got, "/oauth2/token"))
}

func TestServer_CentralTokenEndpoint_AllZerosHostUsesLoopback(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Host = "0.0.0.0"
	srv.config.Server.REST.Port = 7101
	got := srv.centralTokenEndpoint()
	assert.True(t, strings.Contains(got, "127.0.0.1"))
}

func TestServer_CentralTokenEndpoint_SpecificHostStays(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Host = "central.example.com"
	srv.config.Server.REST.Port = 7102
	got := srv.centralTokenEndpoint()
	assert.True(t, strings.Contains(got, "central.example.com"))
}

func TestServer_CentralTokenEndpoint_RESTPortZeroFallsBackToGRPC(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Server.Host = "host1"
	srv.config.Server.REST.Port = 0
	srv.config.Server.GRPC.Port = 9001
	got := srv.centralTokenEndpoint()
	assert.True(t, strings.Contains(got, ":9001"),
		"zero REST port must fall through to GRPC port so the OIDC endpoint is still reachable")
}

// ---------------------------------------------------------------------------
// decodeJSON / isLoopback / loopbackTrustedClient
// ---------------------------------------------------------------------------

func TestDecodeJSON_RoundTrip(t *testing.T) {
	type payload struct {
		Access string `json:"access_token"`
	}
	body := `{"access_token":"abc"}`
	var got payload
	require.NoError(t, decodeJSON(strings.NewReader(body), &got))
	assert.Equal(t, "abc", got.Access)
}

func TestDecodeJSON_MalformedReturnsError(t *testing.T) {
	type payload struct{ X int }
	var got payload
	err := decodeJSON(strings.NewReader("not-json"), &got)
	require.Error(t, err)
}

func TestIsLoopback(t *testing.T) {
	t.Run("loopback IPv4", func(t *testing.T) {
		assert.True(t, isLoopback("https://127.0.0.1:8443/oauth2/token"))
	})

	t.Run("localhost name", func(t *testing.T) {
		assert.True(t, isLoopback("https://localhost:8443/oauth2/token"))
	})

	t.Run("IPv6 loopback", func(t *testing.T) {
		assert.True(t, isLoopback("https://[::1]:8443/oauth2/token"))
	})

	t.Run("remote host", func(t *testing.T) {
		assert.False(t, isLoopback("https://central.example.com/oauth2/token"))
	})

	t.Run("malformed URL", func(t *testing.T) {
		assert.False(t, isLoopback("://invalid"))
	})
}

func TestLoopbackTrustedClient_EmptyPathFails(t *testing.T) {
	_, err := loopbackTrustedClient("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "caCertPath is required")
}

func TestLoopbackTrustedClient_NonexistentPathFails(t *testing.T) {
	_, err := loopbackTrustedClient("/no/such/path/ca-chain.crt")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// centralOAuth2TokenSource.Token / refresh
// ---------------------------------------------------------------------------

func TestCentralOAuth2TokenSource_TokenRoundTrip(t *testing.T) {
	// Stub the central's /oauth2/token endpoint with an httptest server.
	// We use httptest.NewServer (plain HTTP) because the source's http.Client
	// is unconstrained for non-loopback URLs; this exercises the full
	// refresh -> decodeJSON -> cache path without TLS plumbing.
	var observed struct {
		grantType string
		basicAuth string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		observed.grantType = r.PostForm.Get("grant_type")
		user, _, _ := r.BasicAuth()
		observed.basicAuth = user
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"abc.def","token_type":"Bearer","expires_in":3600,"scope":"a b"}`))
	}))
	t.Cleanup(srv.Close)

	cred := &OrchestratorDaemonClient{
		ClientID:     "client-id-1",
		ClientSecret: "client-secret-1",
		Scopes:       []string{"a", "b"},
	}
	ts := newCentralOAuth2TokenSource(cred, srv.URL, "")
	// httptest server is non-loopback per isLoopback (it dials a 127.0.0.1
	// port but uses a stable hostname URL — verify and override if needed).
	// We bypass loopback entirely here by forcing it false: the test URL
	// IS on 127.0.0.1, so loopback paths would require a CA path that we
	// don't have. Reset the flag for plain-HTTP delivery.
	ts.loopback = false

	tok, err := ts.Token(context.Background())
	require.NoError(t, err)
	require.NotNil(t, tok)
	assert.Equal(t, "abc.def", tok.AccessToken)
	assert.Equal(t, "Bearer", tok.TokenType)
	assert.WithinDuration(t, time.Now().Add(time.Hour), tok.Expiry, 30*time.Second)
	assert.Equal(t, "client_credentials", observed.grantType)
	assert.Equal(t, "client-id-1", observed.basicAuth)

	// Second call should use the cached token (still well within the
	// 5-second expiry buffer).
	tok2, err := ts.Token(context.Background())
	require.NoError(t, err)
	assert.Same(t, tok, tok2, "Token must return the cached value when not yet expired")
}

func TestCentralOAuth2TokenSource_RefreshHTTPErrorIsForwarded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	t.Cleanup(srv.Close)

	ts := newCentralOAuth2TokenSource(&OrchestratorDaemonClient{
		ClientID: "x", ClientSecret: "y",
	}, srv.URL, "")
	ts.loopback = false
	_, err := ts.Token(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status=401")
}

func TestCentralOAuth2TokenSource_EmptyAccessTokenIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"","token_type":"Bearer","expires_in":60}`))
	}))
	t.Cleanup(srv.Close)

	ts := newCentralOAuth2TokenSource(&OrchestratorDaemonClient{
		ClientID: "x", ClientSecret: "y",
	}, srv.URL, "")
	ts.loopback = false
	_, err := ts.Token(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty access_token")
}

// TestCentralOAuth2TokenSource_ZeroExpiresInDefaultsTTL covers the
// non-positive-TTL default branch of refresh: a token response with
// expires_in=0 must be assigned the default 5-minute TTL rather than expiring
// immediately.
func TestCentralOAuth2TokenSource_ZeroExpiresInDefaultsTTL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":0}`))
	}))
	t.Cleanup(srv.Close)

	ts := newCentralOAuth2TokenSource(&OrchestratorDaemonClient{
		ClientID: "x", ClientSecret: "y",
	}, srv.URL, "")
	ts.loopback = false

	tok, err := ts.Token(context.Background())
	require.NoError(t, err)
	require.NotNil(t, tok)
	// expires_in=0 must default to a 5-minute TTL, not an already-expired token.
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), tok.Expiry, 30*time.Second)
}
