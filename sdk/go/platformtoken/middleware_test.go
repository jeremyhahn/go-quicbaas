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

package platformtoken

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"maps"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
)

func selfSignedCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Unix(1_600_000_000, 0),
		NotAfter:     time.Unix(1_900_000_000, 0),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, k.Public(), k)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return c
}

func certThumbprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// principalEcho is the daemon's handler behind the guard: it reports the
// principal the guard bound, and nothing from the request itself.
func principalEcho(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ok := FromContext(r.Context())
		if !ok {
			t.Error("the guard passed a request without binding its principal")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(ac.TenantID + "/" + ac.Subject))
	})
}

// The route guard a downstream daemon's REST plane mounts (go-trusted-platform
// #644): it authenticates with the go-quicbaas platform
// token, enforces its DPoP or mTLS binding, authorises the permission against
// the granted scopes, and binds the token's principal, never a request field.
func TestMiddleware_DaemonRoutesAuthenticateAndAuthoriseThroughTheSDK(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	offline := func(string) AuthzMode { return AuthzModeOffline }
	const url = "https://daemon.example/v1/keys"

	clientCert := selfSignedCert(t, "piv-user")
	otherCert := selfSignedCert(t, "someone-else")
	certBound := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "user-1", tid: "tenant-1",
		scp: []string{"keys:get"}, exp: now.Add(10 * time.Minute), iat: now,
		x5t: certThumbprint(clientCert),
	})

	dpopKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("dpop key: %v", err)
	}
	jkt, err := dpop.Thumbprint(dpopKey.Public())
	if err != nil {
		t.Fatalf("thumbprint: %v", err)
	}
	dpopBound := key.sign(t, tokenClaims{
		iss: testIssuer, aud: []string{testAudience}, sub: "user-1", tid: "tenant-1",
		scp: []string{"keys:get"}, exp: now.Add(10 * time.Minute), iat: now, jkt: jkt,
	})
	proof, err := dpop.NewProofForAccessTokenWithClock(dpopKey, http.MethodGet, url, dpopBound, &fixedDPoPClock{now})
	if err != nil {
		t.Fatalf("proof: %v", err)
	}
	noATH, err := dpop.NewProofWithClock(dpopKey, http.MethodGet, url, &fixedDPoPClock{now})
	if err != nil {
		t.Fatalf("proof: %v", err)
	}
	otherATH, err := dpop.NewProofForAccessTokenWithClock(dpopKey, http.MethodGet, url, "another-token", &fixedDPoPClock{now})
	if err != nil {
		t.Fatalf("proof: %v", err)
	}

	for _, tc := range []struct {
		name       string
		auth       string
		dpopProof  string
		peer       *x509.Certificate
		noReplay   bool
		wantStatus int
		wantBody   string
	}{
		{name: "no credential", wantStatus: http.StatusUnauthorized},
		{name: "not a bearer scheme", auth: "Basic dXNlcjpwYXNz", wantStatus: http.StatusUnauthorized},
		{name: "forged token", auth: "Bearer not.a.jwt", wantStatus: http.StatusUnauthorized},
		{name: "granted scope", auth: "Bearer " + bearerToken(t, key, now, []string{"keys:get"}, "tenant-1"),
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		{name: "scope not granted", auth: "Bearer " + bearerToken(t, key, now, []string{"keys:list"}, "tenant-1"),
			wantStatus: http.StatusForbidden},
		{name: "mTLS-bound without a client certificate", auth: "Bearer " + certBound, wantStatus: http.StatusUnauthorized},
		{name: "mTLS-bound with another certificate", auth: "Bearer " + certBound, peer: otherCert,
			wantStatus: http.StatusUnauthorized},
		{name: "mTLS-bound with its certificate", auth: "Bearer " + certBound, peer: clientCert,
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		{name: "DPoP-bound without a proof", auth: "DPoP " + dpopBound, wantStatus: http.StatusUnauthorized},
		{name: "DPoP-bound with its proof", auth: "DPoP " + dpopBound, dpopProof: proof,
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		{name: "DPoP-bound with a proof that carries no ath", auth: "DPoP " + dpopBound, dpopProof: noATH,
			wantStatus: http.StatusUnauthorized},
		{name: "DPoP-bound with another token's ath", auth: "DPoP " + dpopBound, dpopProof: otherATH,
			wantStatus: http.StatusUnauthorized},
		{name: "guard cannot verify a binding", auth: "DPoP " + dpopBound, dpopProof: proof, noReplay: true,
			wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{ModeResolver: offline, Authorizer: &qrdbsdk.NoopAuthorizer{}}
			if !tc.noReplay {
				opts.ReplayCache = dpop.NewMemoryReplayCache(&fixedDPoPClock{now})
			}
			guard := newAuthzValidator(t, key, now, opts).Middleware("keys:get")(principalEcho(t))

			// The request names another tenant and user; the guard binds the
			// token's principal regardless.
			req := httptest.NewRequest(http.MethodGet, url+"?tenant_id=tenant-2",
				strings.NewReader(`{"tenant_id":"tenant-2","user_id":"user-2"}`))
			req.Header.Set("X-Tenant-ID", "tenant-2")
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			if tc.dpopProof != "" {
				req.Header.Set("DPoP", tc.dpopProof)
			}
			if tc.peer != nil {
				req.TLS.PeerCertificates = []*x509.Certificate{tc.peer}
			}
			w := httptest.NewRecorder()
			guard.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantBody != "" && w.Body.String() != tc.wantBody {
				t.Fatalf("principal %q, want %q", w.Body.String(), tc.wantBody)
			}
			if tc.wantStatus == http.StatusUnauthorized && !strings.HasPrefix(w.Header().Get("WWW-Authenticate"), "Bearer") {
				t.Fatalf("a 401 carried no Bearer challenge: %q", w.Header().Get("WWW-Authenticate"))
			}
			if tc.wantStatus != http.StatusOK && strings.Contains(w.Body.String(), "platformtoken:") {
				t.Fatalf("the validator's detail reached the caller: %q", w.Body.String())
			}
		})
	}
}

// An empty permission authenticates only.
func TestMiddleware_EmptyPermissionAuthenticatesOnly(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	v := newAuthzValidator(t, key, now, Options{ModeResolver: func(string) AuthzMode { return AuthzModeOffline }})
	req := httptest.NewRequest(http.MethodGet, "https://daemon.example/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+bearerToken(t, key, now, nil, "tenant-1"))
	ac, err := v.AuthorizeRequest(req, "")
	if err != nil {
		t.Fatalf("AuthorizeRequest: %v", err)
	}
	if ac.TenantID != "tenant-1" || ac.Subject != "user-1" {
		t.Fatalf("principal %+v", ac)
	}
}

// abacDenyKeysGet is go-qrdb's ABAC decorator over an allow-all authorizer,
// with one tenant-1 policy that denies keys:get.
func abacDenyKeysGet(t *testing.T) qrdbsdk.Authorizer {
	t.Helper()
	policy := &qrdbsdk.ABACPolicy{
		APIVersion: "abac.qrdb.io/v1",
		Kind:       "Policy",
		Metadata:   qrdbsdk.ABACPolicyMetadata{Name: "no-key-reads", Tenant: "tenant-1"},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{Any: true},
			Rules: []qrdbsdk.ABACRule{{
				Name: "deny-get", Effect: qrdbsdk.ABACEffectDeny,
				Actions: []string{"get"}, Resources: []string{"keys"},
			}},
		},
	}
	a, err := qrdbsdk.DecorateAuthorizer(&qrdbsdk.NoopAuthorizer{}, qrdbsdk.ABACWithPolicies([]*qrdbsdk.ABACPolicy{policy}))
	if err != nil {
		t.Fatalf("DecorateAuthorizer: %v", err)
	}
	return a
}

// mustNotAuthorize fails the test if the guard asks it.
type mustNotAuthorize struct{ t *testing.T }

func (m mustNotAuthorize) Authorize(context.Context, *qrdbsdk.Identity, string, string) error {
	m.t.Error("the guard asked the authorizer for a permission the scopes do not grant")
	return nil
}

// A permission route asks the go-qrdb authorizer after the scope check, and fails closed without one.
func TestMiddleware_AuthorizerDecision(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	offline := func(string) AuthzMode { return AuthzModeOffline }

	for _, tc := range []struct {
		name       string
		scopes     []string
		authz      qrdbsdk.Authorizer
		wantStatus int
		wantBody   string
	}{
		{name: "ABAC denies a granted scope", scopes: []string{"keys:get"}, authz: abacDenyKeysGet(t),
			wantStatus: http.StatusForbidden},
		{name: "the authorizer allows", scopes: []string{"keys:get"}, authz: &qrdbsdk.NoopAuthorizer{},
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		{name: "no authorizer on a permission route", scopes: []string{"keys:get"},
			wantStatus: http.StatusServiceUnavailable},
		{name: "a scope not granted never reaches the authorizer", scopes: []string{"keys:list"},
			authz: mustNotAuthorize{t}, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard := newAuthzValidator(t, key, now, Options{ModeResolver: offline, Authorizer: tc.authz}).
				Middleware("keys:get")(principalEcho(t))
			req := httptest.NewRequest(http.MethodGet, "https://daemon.example/v1/keys", nil)
			req.Header.Set("X-Tenant-ID", "tenant-2")
			req.Header.Set("Authorization", "Bearer "+bearerToken(t, key, now, tc.scopes, "tenant-1"))
			w := httptest.NewRecorder()
			guard.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantBody != "" && w.Body.String() != tc.wantBody {
				t.Fatalf("principal %q, want %q", w.Body.String(), tc.wantBody)
			}
			if tc.wantStatus != http.StatusOK && strings.Contains(w.Body.String(), "platformtoken:") {
				t.Fatalf("the validator's detail reached the caller: %q", w.Body.String())
			}
		})
	}
}

// abacRoleKeysGet is an ABAC policy in tenant-1 that selects the role
// "role-auditor" with one rule of effect on get keys, decorating inner.
func abacRoleKeysGet(t *testing.T, effect qrdbsdk.ABACEffect, inner qrdbsdk.Authorizer) qrdbsdk.Authorizer {
	t.Helper()
	policy := &qrdbsdk.ABACPolicy{
		APIVersion: "abac.qrdb.io/v1",
		Kind:       "Policy",
		Metadata:   qrdbsdk.ABACPolicyMetadata{Name: "auditor-key-reads", Tenant: "tenant-1"},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{Roles: []string{"role-auditor"}},
			Rules: []qrdbsdk.ABACRule{{
				Name: "auditor-get", Effect: effect,
				Actions: []string{"get"}, Resources: []string{"keys"},
			}},
		},
	}
	a, err := qrdbsdk.DecorateAuthorizer(inner, qrdbsdk.ABACWithPolicies([]*qrdbsdk.ABACPolicy{policy}))
	if err != nil {
		t.Fatalf("DecorateAuthorizer: %v", err)
	}
	return a
}

// innerDeny is an inner authorizer that refuses every request.
type innerDeny struct{}

func (innerDeny) Authorize(context.Context, *qrdbsdk.Identity, string, string) error {
	return errors.New("inner deny")
}

func TestMiddleware_AuthorizerSeesTokenRoles(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	offline := func(string) AuthzMode { return AuthzModeOffline }

	for _, tc := range []struct {
		name       string
		roles      []string
		authz      qrdbsdk.Authorizer
		wantStatus int
		wantBody   string
	}{
		{name: "a role-selected deny refuses a granted scope", roles: []string{"role-auditor"},
			authz: abacRoleKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}), wantStatus: http.StatusForbidden},
		{name: "a role-selected deny does not touch another role", roles: []string{"role-operator"},
			authz:      abacRoleKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}),
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		{name: "a role-selected allow cannot override an inner deny", roles: []string{"role-auditor"},
			authz: abacRoleKeysGet(t, qrdbsdk.ABACEffectAllow, innerDeny{}), wantStatus: http.StatusForbidden},
		{name: "no roles in the token",
			authz:      abacRoleKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}),
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard := newAuthzValidator(t, key, now, Options{ModeResolver: offline, Authorizer: tc.authz}).
				Middleware("keys:get")(principalEcho(t))
			token := key.sign(t, tokenClaims{
				iss: testIssuer, aud: []string{testAudience}, sub: "user-1", tid: "tenant-1",
				scp: []string{"keys:get"}, roles: tc.roles, exp: now.Add(10 * time.Minute), iat: now,
			})
			req := httptest.NewRequest(http.MethodGet, "https://daemon.example/v1/keys", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			guard.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantBody != "" && w.Body.String() != tc.wantBody {
				t.Fatalf("principal %q, want %q", w.Body.String(), tc.wantBody)
			}
			if tc.wantStatus != http.StatusOK {
				if !strings.Contains(w.Body.String(), "access_denied") {
					t.Fatalf("refusal body %q, want access_denied", w.Body.String())
				}
				if strings.Contains(w.Body.String(), "platformtoken:") {
					t.Fatalf("the validator's detail reached the caller: %q", w.Body.String())
				}
			}
		})
	}
}

// abacAttestKeysGet decorates inner with one tenant-1 policy whose rule has
// effect for get on keys, selecting callers whose attest_method is manual.
func abacAttestKeysGet(t *testing.T, effect qrdbsdk.ABACEffect, inner qrdbsdk.Authorizer) qrdbsdk.Authorizer {
	t.Helper()
	policy := &qrdbsdk.ABACPolicy{
		APIVersion: "abac.qrdb.io/v1",
		Kind:       "Policy",
		Metadata:   qrdbsdk.ABACPolicyMetadata{Name: "manual-attest-key-reads", Tenant: "tenant-1"},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{Match: map[string]interface{}{"attest_method": "manual"}},
			Rules: []qrdbsdk.ABACRule{{
				Name: "manual-attest-get", Effect: effect,
				Actions: []string{"get"}, Resources: []string{"keys"},
			}},
		},
	}
	a, err := qrdbsdk.DecorateAuthorizer(inner, qrdbsdk.ABACWithPolicies([]*qrdbsdk.ABACPolicy{policy}))
	if err != nil {
		t.Fatalf("DecorateAuthorizer: %v", err)
	}
	return a
}

// TestMiddleware_AuthorizerSeesAuthAttributes proves the authorizer sees the
// token's authentication claims as Identity.Metadata: an ABAC deny that
// selects attest_method refuses the request, and an attribute-selected allow
// never admits a request the decisions without the attributes refuse.
func TestMiddleware_AuthorizerSeesAuthAttributes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	offline := func(string) AuthzMode { return AuthzModeOffline }

	for _, tc := range []struct {
		name         string
		attestMethod string
		authz        qrdbsdk.Authorizer
		wantStatus   int
		wantBody     string
	}{
		{name: "an attribute-selected deny refuses a granted scope", attestMethod: "manual",
			authz: abacAttestKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}), wantStatus: http.StatusForbidden},
		{name: "an attribute-selected deny does not touch another value", attestMethod: "tpm",
			authz:      abacAttestKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}),
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		{name: "an attribute-selected allow cannot override an inner deny", attestMethod: "manual",
			authz: abacAttestKeysGet(t, qrdbsdk.ABACEffectAllow, innerDeny{}), wantStatus: http.StatusForbidden},
		{name: "no authentication claims in the token",
			authz:      abacAttestKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}),
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard := newAuthzValidator(t, key, now, Options{ModeResolver: offline, Authorizer: tc.authz}).
				Middleware("keys:get")(principalEcho(t))
			token := key.sign(t, tokenClaims{
				iss: testIssuer, aud: []string{testAudience}, sub: "user-1", tid: "tenant-1",
				scp: []string{"keys:get"}, attestMethod: tc.attestMethod, exp: now.Add(10 * time.Minute), iat: now,
			})
			req := httptest.NewRequest(http.MethodGet, "https://daemon.example/v1/keys", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			guard.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantBody != "" && w.Body.String() != tc.wantBody {
				t.Fatalf("principal %q, want %q", w.Body.String(), tc.wantBody)
			}
			if tc.wantStatus != http.StatusOK {
				if !strings.Contains(w.Body.String(), "access_denied") {
					t.Fatalf("refusal body %q, want access_denied", w.Body.String())
				}
				if strings.Contains(w.Body.String(), "platformtoken:") {
					t.Fatalf("the validator's detail reached the caller: %q", w.Body.String())
				}
			}
		})
	}
}

// identityRecorder records a copy of every identity the guard asks inner
// about, and returns inner's decision.
type identityRecorder struct {
	inner qrdbsdk.Authorizer
	seen  []qrdbsdk.Identity
}

func (r *identityRecorder) Authorize(ctx context.Context, id *qrdbsdk.Identity, resource, action string) error {
	r.seen = append(r.seen, *id)
	return r.inner.Authorize(ctx, id, resource, action)
}

// abacRoleKeys decorates inner with one tenant-1 policy that selects the
// role "role-auditor" with one rule of effect on actions over keys.
func abacRoleKeys(t *testing.T, effect qrdbsdk.ABACEffect, actions []string, inner qrdbsdk.Authorizer) qrdbsdk.Authorizer {
	t.Helper()
	policy := &qrdbsdk.ABACPolicy{
		APIVersion: "abac.qrdb.io/v1",
		Kind:       "Policy",
		Metadata:   qrdbsdk.ABACPolicyMetadata{Name: "auditor-keys", Tenant: "tenant-1"},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{Roles: []string{"role-auditor"}},
			Rules: []qrdbsdk.ABACRule{{
				Name: "auditor-keys", Effect: effect,
				Actions: actions, Resources: []string{"keys"},
			}},
		},
	}
	a, err := qrdbsdk.DecorateAuthorizer(inner, qrdbsdk.ABACWithPolicies([]*qrdbsdk.ABACPolicy{policy}))
	if err != nil {
		t.Fatalf("DecorateAuthorizer: %v", err)
	}
	return a
}

// TestMiddleware_TokenRolesDenyMatchesAndScopeBoundsGrants: the verified
// token's roles reach the authorizer, so a deny on one of them refuses, and
// the token's scope bounds every grant, so a role that would grant an action
// outside the scope is refused before the authorizer is asked.
func TestMiddleware_TokenRolesDenyMatchesAndScopeBoundsGrants(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	offline := func(string) AuthzMode { return AuthzModeOffline }
	allowGetDelete := func() qrdbsdk.Authorizer {
		return abacRoleKeys(t, qrdbsdk.ABACEffectAllow, []string{"get", "delete"}, &qrdbsdk.NoopAuthorizer{})
	}

	for _, tc := range []struct {
		name       string
		permission string
		inner      qrdbsdk.Authorizer
		wantStatus int
		wantBody   string
		wantError  string
		wantCalls  bool
	}{
		{name: "a deny on a token role refuses", permission: "keys:get",
			inner:      abacRoleKeys(t, qrdbsdk.ABACEffectDeny, []string{"get"}, &qrdbsdk.NoopAuthorizer{}),
			wantStatus: http.StatusForbidden, wantError: "access_denied", wantCalls: true},
		{name: "a role grant outside the token scope is refused", permission: "keys:delete",
			inner: allowGetDelete(), wantStatus: http.StatusForbidden, wantError: "insufficient_scope"},
		{name: "a role grant inside the token scope is admitted", permission: "keys:get",
			inner: allowGetDelete(), wantStatus: http.StatusOK, wantBody: "tenant-1/user-1", wantCalls: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &identityRecorder{inner: tc.inner}
			guard := newAuthzValidator(t, key, now, Options{ModeResolver: offline, Authorizer: rec}).
				Middleware(tc.permission)(principalEcho(t))
			token := key.sign(t, tokenClaims{
				iss: testIssuer, aud: []string{testAudience}, sub: "user-1", tid: "tenant-1",
				scp: []string{"keys:get"}, roles: []string{"role-auditor"}, exp: now.Add(10 * time.Minute), iat: now,
			})
			req := httptest.NewRequest(http.MethodGet, "https://daemon.example/v1/keys", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			guard.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantBody != "" && w.Body.String() != tc.wantBody {
				t.Fatalf("principal %q, want %q", w.Body.String(), tc.wantBody)
			}
			if tc.wantStatus != http.StatusOK {
				if !strings.Contains(w.Body.String(), tc.wantError) {
					t.Fatalf("refusal body %q, want %s", w.Body.String(), tc.wantError)
				}
				if strings.Contains(w.Body.String(), "platformtoken:") {
					t.Fatalf("the validator's detail reached the caller: %q", w.Body.String())
				}
			}
			if !tc.wantCalls {
				if len(rec.seen) != 0 {
					t.Fatalf("the authorizer was asked %d times for an out-of-scope permission", len(rec.seen))
				}
				return
			}
			found := false
			for _, id := range rec.seen {
				if slices.Equal(id.Roles, []string{"role-auditor"}) {
					found = true
					if tc.wantStatus == http.StatusOK {
						if !slices.Equal(id.ScopePermissions, []string{"keys:get"}) || id.Subject != "user-1" || id.TenantID != "tenant-1" {
							t.Fatalf("role identity %+v, want subject user-1, tenant tenant-1, scope [keys:get]", id)
						}
					}
				}
			}
			if !found {
				t.Fatalf("no identity the authorizer saw carried the token's roles: %+v", rec.seen)
			}
		})
	}
}

// TestAuthorizerIdentities_TokenRolesPopulateTheIdentity pins the identities
// the guard asks the authorizer about: the role-less floor, then the token's
// roles, then the roles with the authentication claims; each with only the
// token's scope, and each a copy.
func TestAuthorizerIdentities_TokenRolesPopulateTheIdentity(t *testing.T) {
	t.Run("roles and claims", func(t *testing.T) {
		ac := &AuthorizedContext{
			Subject: "user-1", TenantID: "tenant-1", Scopes: []string{"keys:get"},
			Roles: []string{"role-auditor"}, AuthAttributes: map[string]string{"attest_method": "tpm"},
		}
		ids := authorizerIdentities(ac)
		if len(ids) != 3 {
			t.Fatalf("%d identities, want 3", len(ids))
		}
		if ids[0].Roles != nil || ids[0].Metadata != nil {
			t.Fatalf("floor identity %+v, want no roles and no metadata", ids[0])
		}
		if !slices.Equal(ids[1].Roles, []string{"role-auditor"}) || ids[1].Metadata != nil {
			t.Fatalf("role identity %+v, want roles [role-auditor] and no metadata", ids[1])
		}
		if !slices.Equal(ids[2].Roles, []string{"role-auditor"}) ||
			!maps.Equal(ids[2].Metadata, map[string]string{"attest_method": "tpm"}) {
			t.Fatalf("claims identity %+v, want roles [role-auditor] and attest_method tpm", ids[2])
		}
		for i, id := range ids {
			if !slices.Equal(id.ScopePermissions, []string{"keys:get"}) || id.Subject != "user-1" || id.TenantID != "tenant-1" {
				t.Fatalf("identity %d %+v, want subject user-1, tenant tenant-1, scope [keys:get]", i, id)
			}
		}

		ids[1].Roles[0] = "role-admin"
		ids[0].ScopePermissions[0] = "*:*"
		ids[2].Metadata["attest_method"] = "manual"
		if ac.Roles[0] != "role-auditor" || ac.Scopes[0] != "keys:get" || ac.AuthAttributes["attest_method"] != "tpm" {
			t.Fatalf("changing an identity changed the authorised principal: %+v", ac)
		}
	})
	t.Run("no roles and no claims", func(t *testing.T) {
		ids := authorizerIdentities(&AuthorizedContext{Subject: "user-1", TenantID: "tenant-1", Scopes: []string{"keys:get"}})
		if len(ids) != 1 {
			t.Fatalf("%d identities, want 1", len(ids))
		}
		if ids[0].Roles != nil {
			t.Fatalf("identity roles %v, want nil", ids[0].Roles)
		}
	})
}

// TestAuthorizePermission_ScopeBoundsEveryGrant: the scope bound sits in the
// function that asks the authorizer, not only in its caller's order, so an
// out-of-scope permission never reaches the authorizer.
func TestAuthorizePermission_ScopeBoundsEveryGrant(t *testing.T) {
	rec := &identityRecorder{inner: &qrdbsdk.NoopAuthorizer{}}
	v := &Validator{authorizer: rec}
	ac := &AuthorizedContext{
		Subject: "user-1", TenantID: "tenant-1", Scopes: []string{"keys:get"}, Roles: []string{"role-auditor"},
	}

	err := v.authorizePermission(context.Background(), ac, "keys:delete")
	if !errors.As(err, new(*ScopeDeniedError)) {
		t.Fatalf("out-of-scope permission: err %v, want a ScopeDeniedError", err)
	}
	if len(rec.seen) != 0 {
		t.Fatalf("the authorizer was asked %d times for an out-of-scope permission", len(rec.seen))
	}

	if err := v.authorizePermission(context.Background(), ac, "keys:get"); err != nil {
		t.Fatalf("in-scope permission: %v", err)
	}
	if len(rec.seen) != 2 {
		t.Fatalf("the authorizer was asked %d times, want 2 (role-less, then with roles)", len(rec.seen))
	}

	ac.Scopes = []string{"keys:*"}
	if err := v.authorizePermission(context.Background(), ac, "keys:delete"); err != nil {
		t.Fatalf("a whole-segment wildcard scope must cover keys:delete: %v", err)
	}
}
