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

// auth_extra_test.go covers the MCP auth handler response transforms and the
// transport's credential-based authentication flow (authenticate /
// extractAccessToken / ensureAuthenticated) end-to-end.

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// Auth handler response transforms
// =============================================================================

func TestAuthTransformLoginResponseFields(t *testing.T) {
	h := newAuthHandler(nil)
	nano := float64(time.Now().Add(time.Hour).UnixNano())

	out, err := h.transformResponse("Login", map[string]interface{}{
		"access_token":  "AT",
		"refresh_token": "RT",
		"expires_at":    nano,
		"token_type":    "Bearer",
	})
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	resp := out.(*service.ExtendedLoginResponse)
	if resp.AccessToken != "AT" || resp.RefreshToken != "RT" || resp.TokenType != "Bearer" {
		t.Errorf("login response mismatch: %+v", resp)
	}
	if resp.ExpiresAt.IsZero() {
		t.Error("expires_at not decoded")
	}

	// A login response without token_type defaults to Bearer.
	out2, err := h.transformResponse("Login", map[string]interface{}{"access_token": "AT"})
	if err != nil {
		t.Fatalf("transformResponse default type: %v", err)
	}
	if out2.(*service.ExtendedLoginResponse).TokenType != "Bearer" {
		t.Error("token type should default to Bearer")
	}
}

func TestAuthTransformResponseDispatch(t *testing.T) {
	h := newAuthHandler(nil)
	expires := float64(time.Now().Add(time.Hour).UnixNano())

	refresh, err := h.transformResponse("RefreshToken", map[string]interface{}{
		"access_token": "AT2", "refresh_token": "RT2", "expires_at": expires})
	if err != nil {
		t.Fatalf("RefreshToken transform: %v", err)
	}
	rr := refresh.(*service.ExtendedRefreshTokenResponse)
	if rr.AccessToken != "AT2" || rr.TokenType != "Bearer" {
		t.Errorf("refresh mismatch: %+v", rr)
	}

	validate, err := h.transformResponse("ValidateToken", map[string]interface{}{
		"user_id": "u1", "username": "alice", "tenant_id": "tn",
		"roles": []interface{}{"admin", "user"}, "issuer": "iss", "expires_at": expires})
	if err != nil {
		t.Fatalf("ValidateToken transform: %v", err)
	}
	vc := validate.(*service.TokenClaims)
	if vc.UserID != "u1" || vc.Username != "alice" || len(vc.Roles) != 2 || vc.Issuer != "iss" {
		t.Errorf("validate mismatch: %+v", vc)
	}

	// PascalCase roles are also accepted.
	validate2, err := h.transformResponse("ValidateToken", map[string]interface{}{
		"UserID": "u2", "Roles": []interface{}{"r"}})
	if err != nil {
		t.Fatalf("ValidateToken pascal: %v", err)
	}
	if len(validate2.(*service.TokenClaims).Roles) != 1 {
		t.Error("pascal roles not decoded")
	}

	// Unknown method passes through unchanged.
	passthrough, err := h.transformResponse("Other", "raw")
	if err != nil {
		t.Fatalf("passthrough: %v", err)
	}
	if passthrough != "raw" {
		t.Errorf("passthrough = %v", passthrough)
	}
}

func TestAuthTransformResponseErrors(t *testing.T) {
	h := newAuthHandler(nil)

	if _, err := h.transformResponse("Login", "scalar"); err == nil {
		t.Error("Login on scalar must error")
	}
	if _, err := h.transformResponse("RefreshToken", 42); err == nil {
		t.Error("RefreshToken on scalar must error")
	}
	if _, err := h.transformResponse("ValidateToken", nil); err == nil {
		t.Error("ValidateToken on nil must error")
	}
	if _, err := h.transformResponse("Login", map[string]interface{}{"x": 1}); err == nil {
		t.Error("Login missing access_token must error")
	}
	if _, err := h.transformResponse("RefreshToken", map[string]interface{}{"x": 1}); err == nil {
		t.Error("RefreshToken missing access_token must error")
	}
}

func TestAuthExtractFieldHelpers(t *testing.T) {
	m := map[string]interface{}{"Pascal": "p", "snake_key": "s", "wrong": 5}

	if v, ok := extractStringField(m, "Pascal", "snake_key"); !ok || v != "p" {
		t.Errorf("pascal extract = %q,%v", v, ok)
	}
	if v, ok := extractStringField(m, "Missing", "snake_key"); !ok || v != "s" {
		t.Errorf("snake extract = %q,%v", v, ok)
	}
	if _, ok := extractStringField(m, "Missing", "absent"); ok {
		t.Error("absent keys must report not found")
	}
	if _, ok := extractStringField(m, "wrong", "absent"); ok {
		t.Error("non-string value must report not found")
	}

	nano := float64(time.Now().UnixNano())
	tm := map[string]interface{}{"PascalTime": nano, "snake_time": nano, "bad": "x"}
	if _, ok := extractTimeField(tm, "PascalTime", "snake_time"); !ok {
		t.Error("pascal time extract failed")
	}
	if _, ok := extractTimeField(tm, "Missing", "snake_time"); !ok {
		t.Error("snake time extract failed")
	}
	if _, ok := extractTimeField(tm, "bad", "absent"); ok {
		t.Error("non-float time must report not found")
	}
}

func TestIsMCPAuthService(t *testing.T) {
	if !isMCPAuthService("quicbaas.Auth") {
		t.Error("Auth detection failed")
	}
	if isMCPAuthService("quicbaas.KV") {
		t.Error("KV must not be Auth")
	}
}

// =============================================================================
// Transport authentication flow
// =============================================================================

// newAuthMCPServer answers initialize, quicbaas_login, and a follow-up
// authenticated tool call. loginText is the JSON the login tool returns; when
// loginError is true the login tool reports a tool-level failure.
func newAuthMCPServer(t *testing.T, loginText string, loginError bool) (*httptest.Server, *[]string) {
	t.Helper()
	var authHeaders []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		var result interface{}
		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": MCPVersion,
				"serverInfo":      map[string]interface{}{"name": "S", "version": "1"},
				"capabilities":    map[string]interface{}{},
			}
		case "tools/call":
			params, _ := req.Params.(map[string]interface{})
			name, _ := params["name"].(string)
			if name == "quicbaas_login" {
				result = map[string]interface{}{
					"content": []interface{}{map[string]interface{}{"type": "text", "text": loginText}},
					"isError": loginError,
				}
			} else {
				result = map[string]interface{}{
					"content": []interface{}{map[string]interface{}{"type": "text", "text": `{"ok":true}`}},
					"isError": false,
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: mustMarshalJSON(result)})
	}))
	return srv, &authHeaders
}

func TestTransportAuthenticateSuccess(t *testing.T) {
	srv, headers := newAuthMCPServer(t, `{"access_token":"TOKEN123"}`, false)
	defer srv.Close()

	tr, err := New(&transport.Config{Address: srv.URL, Timeout: 5 * time.Second},
		WithCredentials("alice", "pw"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	// A non-public tool triggers authentication, then the call itself.
	if _, err := tr.Request(context.Background(), "quicbaas.KV", "Get",
		map[string]interface{}{"key": "k"}); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !tr.authenticated.Load() {
		t.Error("transport should be authenticated")
	}
	var sawBearer bool
	for _, h := range *headers {
		if h == "Bearer TOKEN123" {
			sawBearer = true
		}
	}
	if !sawBearer {
		t.Errorf("expected Bearer TOKEN123 header, saw %v", *headers)
	}
}

func TestTransportAuthenticatePascalCaseToken(t *testing.T) {
	srv, _ := newAuthMCPServer(t, `{"AccessToken":"PCTOKEN"}`, false)
	defer srv.Close()

	tr, err := New(&transport.Config{Address: srv.URL, Timeout: 5 * time.Second},
		WithCredentials("alice", "pw"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()
	if err := tr.ensureAuthenticated(context.Background()); err != nil {
		t.Fatalf("ensureAuthenticated: %v", err)
	}
	if tr.accessToken != "PCTOKEN" {
		t.Errorf("accessToken = %q", tr.accessToken)
	}
}

func TestTransportAuthenticateToolError(t *testing.T) {
	srv, _ := newAuthMCPServer(t, "invalid credentials", true)
	defer srv.Close()

	tr, err := New(&transport.Config{Address: srv.URL, Timeout: 5 * time.Second},
		WithCredentials("alice", "bad"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()
	if err := tr.ensureAuthenticated(context.Background()); err == nil {
		t.Fatal("expected authentication error on tool failure")
	}
}

func TestTransportAuthenticateMissingToken(t *testing.T) {
	srv, _ := newAuthMCPServer(t, `{"other":"value"}`, false)
	defer srv.Close()

	tr, err := New(&transport.Config{Address: srv.URL, Timeout: 5 * time.Second},
		WithCredentials("alice", "pw"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()
	if err := tr.ensureAuthenticated(context.Background()); err == nil {
		t.Fatal("expected error when login response lacks a token")
	}
}

func TestEnsureAuthenticatedNoCredentials(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://localhost:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()
	if err := tr.ensureAuthenticated(context.Background()); err != nil {
		t.Errorf("ensureAuthenticated with no creds: %v", err)
	}
	if tr.authenticated.Load() {
		t.Error("transport must not be marked authenticated without credentials")
	}
}

func TestExtractAccessTokenEdgeCases(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://localhost:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	if _, err := tr.extractAccessToken(nil); err == nil {
		t.Error("empty content must error")
	}
	if _, err := tr.extractAccessToken([]contentItem{{Text: "not json"}}); err == nil {
		t.Error("malformed JSON must error")
	}
	if _, err := tr.extractAccessToken([]contentItem{{Text: `{"x":1}`}}); err == nil {
		t.Error("missing token must error")
	}
	tok, err := tr.extractAccessToken([]contentItem{{Text: `{"access_token":"S"}`}})
	if err != nil || tok != "S" {
		t.Errorf("snake token = %q, %v", tok, err)
	}
}
