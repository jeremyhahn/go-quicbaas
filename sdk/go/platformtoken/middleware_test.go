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
	"math/big"
	"net/http"
	"net/http/httptest"
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
