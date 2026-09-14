// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package iam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

// oidcMockIdP is a minimal upstream IdP: a token endpoint that returns a signed
// (or forged) ID token plus a JWKS endpoint. It is used to drive HandleCallback
// end-to-end and prove the parseIDToken signature hole is closed.
type oidcMockIdP struct {
	srv      *httptest.Server
	key      *rsa.PrivateKey
	kid      string
	clientID string
	now      time.Time

	// idTokenFn produces the id_token returned from the token endpoint for a
	// given exchange. It receives the issuer so claims can be set correctly.
	idTokenFn func(issuer string) string
}

func newOIDCMockIdP(t *testing.T, clientID string, now time.Time) *oidcMockIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	idp := &oidcMockIdP{key: key, kid: "idp-kid", clientID: clientID, now: now}

	jwk := map[string]interface{}{
		"kty": "RSA", "kid": idp.kid, "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes()),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []interface{}{jwk}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		idToken := ""
		if idp.idTokenFn != nil {
			idToken = idp.idTokenFn(idp.issuer())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "upstream-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     idToken,
		})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func (idp *oidcMockIdP) issuer() string { return idp.srv.URL }

// signedIDToken produces a correctly-signed ID token; mutate may override claims.
func (idp *oidcMockIdP) signedIDToken(issuer, nonce string, mutate func(jwt.MapClaims)) string {
	claims := jwt.MapClaims{
		"iss":            issuer,
		"aud":            idp.clientID,
		"sub":            "idp|user-1",
		"email":          "alice@acme.com",
		"email_verified": true,
		"name":           "Alice",
		"nonce":          nonce,
		"groups":         []string{"Acme Administrators"},
		"iat":            idp.now.Unix(),
		"exp":            idp.now.Add(time.Hour).Unix(),
	}
	if mutate != nil {
		mutate(claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = idp.kid
	s, _ := tok.SignedString(idp.key)
	return s
}

// forgedIDToken produces an ID token signed by an attacker key (wrong signature).
func (idp *oidcMockIdP) forgedIDToken(issuer, nonce string) string {
	attacker, _ := rsa.GenerateKey(rand.Reader, 2048)
	claims := jwt.MapClaims{
		"iss": issuer, "aud": idp.clientID, "sub": "attacker",
		"email": "attacker@evil.com", "nonce": nonce,
		"iat": idp.now.Unix(), "exp": idp.now.Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = idp.kid
	s, _ := tok.SignedString(attacker)
	return s
}

// callbackHarness wires a federation manager against the mock IdP and drives a
// full BeginLogin -> HandleCallback round trip. It returns the manager, the
// provider, and a helper that runs the callback.
type callbackHarness struct {
	manager       *DefaultFederationManager
	identityStore *IdentityStore
	provider      *ExternalProvider
	idp           *oidcMockIdP
	ctx           context.Context
}

func newCallbackHarness(t *testing.T, mappings []GroupRoleMapping, defaults []string) *callbackHarness {
	t.Helper()
	now := time.Now()
	clientID := "acme-client"
	idp := newOIDCMockIdP(t, clientID, now)

	manager, providerStorage, _, _, identityStore, _ := setupTestFederationManager(t)
	manager.httpClient = idp.srv.Client()
	manager.now = func() time.Time { return now }

	provider := &ExternalProvider{
		ID:            "acme-okta",
		TenantID:      "tenant-acme",
		Name:          "okta",
		Type:          ProviderTypeOIDC,
		Enabled:       true,
		ClientID:      clientID,
		ClientSecret:  "secret",
		IssuerURL:     idp.issuer(),
		AuthURL:       idp.issuer() + "/authorize",
		TokenURL:      idp.issuer() + "/token",
		JWKSURL:       idp.issuer() + "/jwks",
		Scopes:        []string{"openid", "email", "profile", "groups"},
		AutoProvision: true,
		AllowLinking:  true,
		DefaultRoles:  defaults,
		ClaimMappings: &ClaimMappings{
			Subject:           "sub",
			Email:             "email",
			Name:              "name",
			Groups:            "groups",
			GroupRoleMappings: mappings,
		},
	}
	provider.ApplyDefaults()
	provider.ClaimMappings.GroupRoleMappings = mappings
	if err := providerStorage.CreateProvider(context.Background(), provider); err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// AssignRole requires the target roles to exist; create the ones the test
	// mappings and defaults reference.
	roleIDs := map[string]struct{}{}
	for _, m := range mappings {
		roleIDs[m.RoleID] = struct{}{}
	}
	for _, r := range defaults {
		roleIDs[r] = struct{}{}
	}
	for id := range roleIDs {
		if _, err := identityStore.CreateRole(context.Background(), provider.TenantID, CreateRoleOptions{
			ID:   id,
			Name: id,
		}); err != nil {
			t.Fatalf("create role %s: %v", id, err)
		}
	}

	// oauth2 exchange uses the http client stashed on the context.
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, idp.srv.Client())

	return &callbackHarness{
		manager:       manager,
		identityStore: identityStore,
		provider:      provider,
		idp:           idp,
		ctx:           ctx,
	}
}

// begin runs BeginLogin and returns the state token and the OIDC nonce that the
// IdP must echo in the ID token.
func (h *callbackHarness) begin(t *testing.T) (stateToken, nonce string) {
	t.Helper()
	redirect, err := h.manager.BeginLogin(h.ctx, h.provider.TenantID, h.provider.Name, "https://app.acme.com/cb")
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	stateToken = redirect.State
	// Recover the OIDC nonce the manager generated from the auth URL.
	u, err := url.Parse(redirect.AuthURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	nonce = u.Query().Get("nonce")
	if nonce == "" {
		t.Fatal("expected nonce in auth URL")
	}
	return stateToken, nonce
}

func TestHandleCallback_ForgedIDTokenRejected(t *testing.T) {
	h := newCallbackHarness(t, []GroupRoleMapping{{Group: "Acme Administrators", RoleID: "role-admin"}}, nil)
	stateToken, nonce := h.begin(t)

	// The IdP returns a token signed by an attacker key.
	h.idp.idTokenFn = func(issuer string) string {
		return h.idp.forgedIDToken(issuer, nonce)
	}

	_, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
	if err == nil {
		t.Fatal("expected forged ID token to be rejected (hole closed)")
	}

	// No user must have been provisioned.
	if _, uerr := h.identityStore.GetUserByEmail(h.ctx, h.provider.TenantID, "attacker@evil.com"); uerr == nil {
		t.Fatal("forged login must not provision a user")
	}
}

func TestHandleCallback_NonceMismatchRejected(t *testing.T) {
	h := newCallbackHarness(t, nil, []string{"role-default"})
	stateToken, _ := h.begin(t)

	// Correctly-signed token but with the WRONG nonce.
	h.idp.idTokenFn = func(issuer string) string {
		return h.idp.signedIDToken(issuer, "attacker-nonce", nil)
	}

	_, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
	if err == nil {
		t.Fatal("expected nonce-mismatch rejection")
	}
}

func TestHandleCallback_ValidTokenJITWithMappedRoles(t *testing.T) {
	h := newCallbackHarness(t, []GroupRoleMapping{
		{Group: "Acme Administrators", RoleID: "role-admin"},
		{Group: "Acme Auditors", RoleID: "role-auditor"},
	}, nil)
	stateToken, nonce := h.begin(t)

	h.idp.idTokenFn = func(issuer string) string {
		return h.idp.signedIDToken(issuer, nonce, nil) // groups: ["Acme Administrators"]
	}

	res, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !res.Provisioned {
		t.Error("expected user to be provisioned")
	}

	roles, err := h.identityStore.GetUserRoles(h.ctx, h.provider.TenantID, res.User.ID)
	if err != nil {
		t.Fatalf("get roles: %v", err)
	}
	if len(roles) != 1 || roles[0].ID != "role-admin" {
		t.Errorf("roles = %v, want [role-admin]", roles)
	}
}

func TestHandleCallback_FailClosedNoGroupMatch(t *testing.T) {
	h := newCallbackHarness(t, []GroupRoleMapping{
		{Group: "Acme Administrators", RoleID: "role-admin"},
	}, []string{"role-default"})
	stateToken, nonce := h.begin(t)

	// Valid token but groups match no mapping.
	h.idp.idTokenFn = func(issuer string) string {
		return h.idp.signedIDToken(issuer, nonce, func(c jwt.MapClaims) {
			c["groups"] = []string{"Unrelated"}
		})
	}

	_, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
	if err == nil {
		t.Fatal("expected fail-closed denial when no group matches")
	}
	if _, uerr := h.identityStore.GetUserByEmail(h.ctx, h.provider.TenantID, "alice@acme.com"); uerr == nil {
		t.Fatal("denied login must not provision a user")
	}
}

func TestHandleCallback_DefaultRolesFallback(t *testing.T) {
	h := newCallbackHarness(t, nil, []string{"role-default"})
	stateToken, nonce := h.begin(t)

	h.idp.idTokenFn = func(issuer string) string {
		return h.idp.signedIDToken(issuer, nonce, nil)
	}

	res, err := h.manager.HandleCallback(h.ctx, stateToken, "auth-code")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	roles, err := h.identityStore.GetUserRoles(h.ctx, h.provider.TenantID, res.User.ID)
	if err != nil {
		t.Fatalf("get roles: %v", err)
	}
	if len(roles) != 1 || roles[0].ID != "role-default" {
		t.Errorf("roles = %v, want [role-default]", roles)
	}
}

func TestHandleCallback_ReSyncRolesOnRepeatLogin(t *testing.T) {
	mappings := []GroupRoleMapping{
		{Group: "Acme Administrators", RoleID: "role-admin"},
		{Group: "Acme Auditors", RoleID: "role-auditor"},
	}
	h := newCallbackHarness(t, mappings, nil)

	// First login: admin group.
	state1, nonce1 := h.begin(t)
	h.idp.idTokenFn = func(issuer string) string {
		return h.idp.signedIDToken(issuer, nonce1, func(c jwt.MapClaims) {
			c["groups"] = []string{"Acme Administrators"}
		})
	}
	res1, err := h.manager.HandleCallback(h.ctx, state1, "code-1")
	if err != nil {
		t.Fatalf("first login: %v", err)
	}

	// Second login: the IdP now reports the user as an auditor only.
	state2, nonce2 := h.begin(t)
	h.idp.idTokenFn = func(issuer string) string {
		return h.idp.signedIDToken(issuer, nonce2, func(c jwt.MapClaims) {
			c["groups"] = []string{"Acme Auditors"}
		})
	}
	if _, err := h.manager.HandleCallback(h.ctx, state2, "code-2"); err != nil {
		t.Fatalf("second login: %v", err)
	}

	roles, err := h.identityStore.GetUserRoles(h.ctx, h.provider.TenantID, res1.User.ID)
	if err != nil {
		t.Fatalf("get roles: %v", err)
	}
	if len(roles) != 1 || roles[0].ID != "role-auditor" {
		t.Errorf("after re-sync roles = %v, want [role-auditor]", roles)
	}
}
