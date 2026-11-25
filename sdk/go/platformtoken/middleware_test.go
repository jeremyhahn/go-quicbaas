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
			opts := Options{ModeResolver: offline, Authorizer: &qrdbsdk.NoopAuthorizer{}, RBAC: &qrdbsdk.NoopAuthorizer{}}
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
		rbac       qrdbsdk.Authorizer
		wantStatus int
		wantBody   string
	}{
		{name: "ABAC denies a granted scope", scopes: []string{"keys:get"}, authz: abacDenyKeysGet(t),
			rbac: &qrdbsdk.NoopAuthorizer{}, wantStatus: http.StatusForbidden},
		{name: "the authorizer allows", scopes: []string{"keys:get"}, authz: &qrdbsdk.NoopAuthorizer{},
			rbac: &qrdbsdk.NoopAuthorizer{}, wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		{name: "no authorizer on a permission route", scopes: []string{"keys:get"},
			rbac: &qrdbsdk.NoopAuthorizer{}, wantStatus: http.StatusServiceUnavailable},
		{name: "a scope not granted never reaches the authorizer", scopes: []string{"keys:list"},
			authz: mustNotAuthorize{t}, rbac: &qrdbsdk.NoopAuthorizer{}, wantStatus: http.StatusForbidden},
		{name: "no RBAC on a permission route", scopes: []string{"keys:get"},
			authz: &qrdbsdk.NoopAuthorizer{}, wantStatus: http.StatusServiceUnavailable},
		{name: "a scope not granted never reaches RBAC", scopes: []string{"keys:list"},
			authz: mustNotAuthorize{t}, rbac: mustNotAuthorize{t}, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard := newAuthzValidator(t, key, now, Options{ModeResolver: offline, Authorizer: tc.authz, RBAC: tc.rbac}).
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
		rbac       qrdbsdk.Authorizer
		wantStatus int
		wantBody   string
	}{
		{name: "a role-selected deny refuses a granted scope", roles: []string{"role-auditor"},
			authz: abacRoleKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}), wantStatus: http.StatusForbidden},
		{name: "a role-selected deny does not touch another role", roles: []string{"role-operator"},
			authz:      abacRoleKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}),
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		{name: "a role-selected allow cannot override an inner deny", roles: []string{"role-auditor"},
			rbac:  innerDeny{},
			authz: abacRoleKeysGet(t, qrdbsdk.ABACEffectAllow, innerDeny{}), wantStatus: http.StatusForbidden},
		{name: "no roles in the token",
			authz:      abacRoleKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}),
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rbac := tc.rbac
			if rbac == nil {
				rbac = &qrdbsdk.NoopAuthorizer{}
			}
			guard := newAuthzValidator(t, key, now, Options{ModeResolver: offline, Authorizer: tc.authz, RBAC: rbac}).
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
// never overrides an RBAC refusal.
func TestMiddleware_AuthorizerSeesAuthAttributes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	offline := func(string) AuthzMode { return AuthzModeOffline }

	for _, tc := range []struct {
		name         string
		attestMethod string
		authz        qrdbsdk.Authorizer
		rbac         qrdbsdk.Authorizer
		wantStatus   int
		wantBody     string
	}{
		{name: "an attribute-selected deny refuses a granted scope", attestMethod: "manual",
			authz: abacAttestKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}), wantStatus: http.StatusForbidden},
		{name: "an attribute-selected deny does not touch another value", attestMethod: "tpm",
			authz:      abacAttestKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}),
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		{name: "an attribute-selected allow cannot override an inner deny", attestMethod: "manual",
			rbac:  innerDeny{},
			authz: abacAttestKeysGet(t, qrdbsdk.ABACEffectAllow, innerDeny{}), wantStatus: http.StatusForbidden},
		{name: "no authentication claims in the token",
			authz:      abacAttestKeysGet(t, qrdbsdk.ABACEffectDeny, &qrdbsdk.NoopAuthorizer{}),
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rbac := tc.rbac
			if rbac == nil {
				rbac = &qrdbsdk.NoopAuthorizer{}
			}
			guard := newAuthzValidator(t, key, now, Options{ModeResolver: offline, Authorizer: tc.authz, RBAC: rbac}).
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
			guard := newAuthzValidator(t, key, now, Options{ModeResolver: offline, Authorizer: rec, RBAC: &qrdbsdk.NoopAuthorizer{}}).
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

// TestAuthorizerIdentity_CompleteVerifiedIdentity pins the one identity the
// guard asks go-qrdb about: the token's subject, tenant, roles, scope and
// authentication claims together, each call a fresh copy.
func TestAuthorizerIdentity_CompleteVerifiedIdentity(t *testing.T) {
	t.Run("roles and claims", func(t *testing.T) {
		ac := &AuthorizedContext{
			Subject: "user-1", TenantID: "tenant-1", Scopes: []string{"keys:get"},
			Roles: []string{"role-auditor"}, AuthAttributes: map[string]string{"attest_method": "tpm"},
		}
		id := authorizerIdentity(ac)
		if id.Subject != "user-1" || id.TenantID != "tenant-1" ||
			!slices.Equal(id.Roles, []string{"role-auditor"}) ||
			!slices.Equal(id.ScopePermissions, []string{"keys:get"}) ||
			!maps.Equal(id.Metadata, map[string]string{"attest_method": "tpm", "auth_source": "platform_token"}) {
			t.Fatalf("identity %+v, want subject user-1, tenant tenant-1, roles [role-auditor], "+
				"scope [keys:get] and attest_method tpm", id)
		}
		if !maps.Equal(id.Attributes, map[string]string{"auth_source": "platform_token"}) {
			t.Fatalf("identity attributes %v, want only auth_source platform_token", id.Attributes)
		}
		id.Attributes["auth_source"] = "x"
		if got := authorizerIdentity(ac).Attributes["auth_source"]; got != "platform_token" {
			t.Fatalf("a changed attribute leaked into the next identity: auth_source %q", got)
		}

		id.Roles[0] = "role-admin"
		id.ScopePermissions[0] = "*:*"
		id.Metadata["attest_method"] = "manual"
		if ac.Roles[0] != "role-auditor" || ac.Scopes[0] != "keys:get" || ac.AuthAttributes["attest_method"] != "tpm" {
			t.Fatalf("changing the identity changed the authorised principal: %+v", ac)
		}
		again := authorizerIdentity(ac)
		if !slices.Equal(again.Roles, []string{"role-auditor"}) ||
			!slices.Equal(again.ScopePermissions, []string{"keys:get"}) ||
			!maps.Equal(again.Metadata, map[string]string{"attest_method": "tpm", "auth_source": "platform_token"}) {
			t.Fatalf("a changed identity leaked into the next one: %+v", again)
		}
	})
	t.Run("no roles and no claims", func(t *testing.T) {
		id := authorizerIdentity(&AuthorizedContext{Subject: "user-1", TenantID: "tenant-1", Scopes: []string{"keys:get"}})
		if id.Roles != nil {
			t.Fatalf("identity roles %v, want nil", id.Roles)
		}
		if !maps.Equal(id.Metadata, map[string]string{"auth_source": "platform_token"}) {
			t.Fatalf("identity metadata %v, want only auth_source platform_token", id.Metadata)
		}
		if !maps.Equal(id.Attributes, map[string]string{"auth_source": "platform_token"}) {
			t.Fatalf("identity attributes %v, want only auth_source platform_token", id.Attributes)
		}
	})
	t.Run("a claim never replaces the guard's mark", func(t *testing.T) {
		ac := &AuthorizedContext{
			Subject: "user-1", TenantID: "tenant-1", Scopes: []string{"keys:get"},
			AuthAttributes: map[string]string{"auth_source": "on_behalf"},
		}
		if got := authorizerIdentity(ac).Metadata["auth_source"]; got != "platform_token" {
			t.Fatalf("identity auth_source %q, want platform_token", got)
		}
		if ac.AuthAttributes["auth_source"] != "on_behalf" {
			t.Fatalf("building the identity changed the authorised context: %v", ac.AuthAttributes)
		}
	})
}

// TestAuthorizePermission_ScopeBoundsEveryGrant: the scope bound sits in the
// function that asks go-qrdb, not only in its caller's order, so an
// out-of-scope permission reaches neither RBAC nor the authorizer; an
// in-scope one asks each once with the token's roles, and a missing RBAC is
// a configuration error.
func TestAuthorizePermission_ScopeBoundsEveryGrant(t *testing.T) {
	rec := &identityRecorder{inner: &qrdbsdk.NoopAuthorizer{}}
	rbacRec := &identityRecorder{inner: &qrdbsdk.NoopAuthorizer{}}
	v := &Validator{authorizer: rec, rbac: rbacRec}
	ac := &AuthorizedContext{
		Subject: "user-1", TenantID: "tenant-1", Scopes: []string{"keys:get"}, Roles: []string{"role-auditor"},
	}

	err := v.authorizePermission(context.Background(), ac, "keys:delete")
	if !errors.As(err, new(*ScopeDeniedError)) {
		t.Fatalf("out-of-scope permission: err %v, want a ScopeDeniedError", err)
	}
	if len(rec.seen) != 0 || len(rbacRec.seen) != 0 {
		t.Fatalf("an out-of-scope permission was asked of the authorizer %d and RBAC %d times",
			len(rec.seen), len(rbacRec.seen))
	}

	if err := v.authorizePermission(context.Background(), ac, "keys:get"); err != nil {
		t.Fatalf("in-scope permission: %v", err)
	}
	if len(rbacRec.seen) != 1 || len(rec.seen) != 1 {
		t.Fatalf("RBAC was asked %d and the authorizer %d times, want once each", len(rbacRec.seen), len(rec.seen))
	}
	if !slices.Equal(rbacRec.seen[0].Roles, []string{"role-auditor"}) ||
		!slices.Equal(rec.seen[0].Roles, []string{"role-auditor"}) {
		t.Fatalf("RBAC saw roles %v and the authorizer %v, want [role-auditor] for both",
			rbacRec.seen[0].Roles, rec.seen[0].Roles)
	}

	ac.Scopes = []string{"keys:*"}
	if err := v.authorizePermission(context.Background(), ac, "keys:delete"); err != nil {
		t.Fatalf("a whole-segment wildcard scope must cover keys:delete: %v", err)
	}

	ac.Scopes = []string{"keys:get"}
	err = (&Validator{authorizer: rec}).authorizePermission(context.Background(), ac, "keys:get")
	if !errors.As(err, new(*ConfigError)) {
		t.Fatalf("no RBAC: err %v, want a ConfigError", err)
	}
}

// abacKeysGetDeny decorates inner with one tenant-1 policy that denies get on
// keys to every authenticated subject, under rule's When and Unless
// conditions.
func abacKeysGetDeny(t *testing.T, inner qrdbsdk.Authorizer, when, unless []qrdbsdk.ABACCondition) qrdbsdk.Authorizer {
	t.Helper()
	policy := &qrdbsdk.ABACPolicy{
		APIVersion: "abac.qrdb.io/v1",
		Kind:       "Policy",
		Metadata:   qrdbsdk.ABACPolicyMetadata{Name: "attested-key-reads", Tenant: "tenant-1"},
		Spec: qrdbsdk.ABACPolicySpec{
			Subjects: qrdbsdk.ABACSubjectSelector{Authenticated: true},
			Rules: []qrdbsdk.ABACRule{{
				Name: "attested-get", Effect: qrdbsdk.ABACEffectDeny,
				Actions: []string{"get"}, Resources: []string{"keys"},
				When: when, Unless: unless,
			}},
		},
	}
	a, err := qrdbsdk.DecorateAuthorizer(inner, qrdbsdk.ABACWithPolicies([]*qrdbsdk.ABACPolicy{policy}))
	if err != nil {
		t.Fatalf("DecorateAuthorizer: %v", err)
	}
	return a
}

// TestMiddleware_ABACSeesTheCompleteIdentity proves ABAC is judged once, on
// the token's complete verified identity: a deny conditioned on a claim's
// absence does not refuse a token that carries the claim, and a deny's unless
// exemption on a verified claim admits the token that carries it. A token
// with no authentication claims at all is judged the same way: an
// absence-based deny refuses it, and an exists-based exemption does not
// cover it. The guard carries no user-record attribute, so a condition on
// one is judged absent.
func TestMiddleware_ABACSeesTheCompleteIdentity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	offline := func(string) AuthzMode { return AuthzModeOffline }
	absence := abacKeysGetDeny(t, &qrdbsdk.NoopAuthorizer{},
		[]qrdbsdk.ABACCondition{{Expression: "subject.attrs.attest_method notexists"}}, nil)
	unless := abacKeysGetDeny(t, &qrdbsdk.NoopAuthorizer{},
		nil, []qrdbsdk.ABACCondition{{Expression: "subject.attrs.attest_method == \"tpm\""}})
	unlessExists := abacKeysGetDeny(t, &qrdbsdk.NoopAuthorizer{},
		nil, []qrdbsdk.ABACCondition{{Expression: "subject.attrs.attest_method exists"}})
	userAbsence := abacKeysGetDeny(t, &qrdbsdk.NoopAuthorizer{},
		[]qrdbsdk.ABACCondition{{Expression: "subject.user_attrs.clearance notexists"}}, nil)
	userUnlessExists := abacKeysGetDeny(t, &qrdbsdk.NoopAuthorizer{},
		nil, []qrdbsdk.ABACCondition{{Expression: "subject.user_attrs.clearance exists"}})

	for _, tc := range []struct {
		name         string
		authz        qrdbsdk.Authorizer
		attestMethod string
		amr          []string
		wantStatus   int
		wantBody     string
	}{
		{name: "a claim-bearing token passes an absence-based deny", authz: absence, attestMethod: "tpm",
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		// The token carries another authentication claim (amr) but not attest_method.
		{name: "a token without the claim is refused by an absence-based deny", authz: absence,
			amr: []string{"pwd"}, wantStatus: http.StatusForbidden},
		{name: "a token with no authentication claims is refused by an absence-based deny", authz: absence,
			wantStatus: http.StatusForbidden},
		{name: "an unless exemption on a verified claim admits", authz: unless, attestMethod: "tpm",
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		{name: "an unless exemption does not cover another value", authz: unless, attestMethod: "manual",
			wantStatus: http.StatusForbidden},
		{name: "an exists-based unless exemption does not cover a token with no authentication claims",
			authz: unlessExists, wantStatus: http.StatusForbidden},
		{name: "an exists-based unless exemption covers a claim-bearing token", authz: unlessExists,
			attestMethod: "tpm", wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
		{name: "a token is refused by a deny on a missing user attribute", authz: userAbsence,
			wantStatus: http.StatusForbidden},
		{name: "a claim-bearing token is refused by a deny on a missing user attribute", authz: userAbsence,
			attestMethod: "tpm", wantStatus: http.StatusForbidden},
		{name: "an exists-based unless exemption on a user attribute does not cover a token",
			authz: userUnlessExists, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{ModeResolver: offline, Authorizer: tc.authz, RBAC: &qrdbsdk.NoopAuthorizer{}}
			guard := newAuthzValidator(t, key, now, opts).Middleware("keys:get")(principalEcho(t))
			token := key.sign(t, tokenClaims{
				iss: testIssuer, aud: []string{testAudience}, sub: "user-1", tid: "tenant-1",
				scp: []string{"keys:get"}, attestMethod: tc.attestMethod, amr: tc.amr,
				exp: now.Add(10 * time.Minute), iat: now,
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

// soCryptoDenyRBAC stands in for go-qrdb RBAC's SO deny-list: it refuses a
// cryptographic permission to an identity holding the "so" role, and
// records every identity it is asked about.
type soCryptoDenyRBAC struct{ seen []qrdbsdk.Identity }

func (r *soCryptoDenyRBAC) Authorize(_ context.Context, id *qrdbsdk.Identity, resource, action string) error {
	r.seen = append(r.seen, *id)
	if slices.Contains(id.Roles, qrdbsdk.RoleNameSO) && qrdbsdk.IsPermissionCryptoUse(qrdbsdk.Permission(resource+":"+action)) {
		return errors.New("SO denied cryptographic use")
	}
	return nil
}

// TestMiddleware_RBACHoldsWhenABACAllows proves an ABAC allow does not lift
// RBAC's mandatory restrictions: go-qrdb's decorated authorizer admits on a
// matched allow without asking the inner RBAC, so the guard asks RBAC
// itself, with the token's roles, and its SO deny-list refuses.
func TestMiddleware_RBACHoldsWhenABACAllows(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	offline := func(string) AuthzMode { return AuthzModeOffline }

	for _, tc := range []struct {
		name       string
		roles      []string
		wantStatus int
		wantBody   string
	}{
		{name: "an ABAC allow does not lift the SO deny-list", roles: []string{"so"},
			wantStatus: http.StatusForbidden},
		{name: "the same allow admits a non-SO signer", roles: []string{"signer"},
			wantStatus: http.StatusOK, wantBody: "tenant-1/user-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rbac := &soCryptoDenyRBAC{}
			policy := &qrdbsdk.ABACPolicy{
				APIVersion: "abac.qrdb.io/v1",
				Kind:       "Policy",
				Metadata:   qrdbsdk.ABACPolicyMetadata{Name: "signers", Tenant: "tenant-1"},
				Spec: qrdbsdk.ABACPolicySpec{
					Subjects: qrdbsdk.ABACSubjectSelector{Roles: []string{"so", "signer"}},
					Rules: []qrdbsdk.ABACRule{{
						Name: "sign", Effect: qrdbsdk.ABACEffectAllow,
						Actions: []string{"sign"}, Resources: []string{"keys"},
					}},
				},
			}
			authz, err := qrdbsdk.DecorateAuthorizer(rbac, qrdbsdk.ABACWithPolicies([]*qrdbsdk.ABACPolicy{policy}))
			if err != nil {
				t.Fatalf("DecorateAuthorizer: %v", err)
			}
			if err := authz.Authorize(qrdbsdk.ContextWithTenant(context.Background(), "tenant-1"),
				&qrdbsdk.Identity{Subject: "user-1", TenantID: "tenant-1", Roles: []string{"so"},
					ScopePermissions: []string{"keys:sign"}}, "keys", "sign"); err != nil {
				t.Fatalf("the decorated authorizer alone must admit the SO on its allow: %v", err)
			}
			rbac.seen = nil

			opts := Options{ModeResolver: offline, Authorizer: authz, RBAC: rbac}
			guard := newAuthzValidator(t, key, now, opts).Middleware("keys:sign")(principalEcho(t))
			token := key.sign(t, tokenClaims{
				iss: testIssuer, aud: []string{testAudience}, sub: "user-1", tid: "tenant-1",
				scp: []string{"keys:sign"}, roles: tc.roles, exp: now.Add(10 * time.Minute), iat: now,
			})
			req := httptest.NewRequest(http.MethodPost, "https://daemon.example/v1/keys/k1/sign", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			guard.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantBody != "" && w.Body.String() != tc.wantBody {
				t.Fatalf("principal %q, want %q", w.Body.String(), tc.wantBody)
			}
			if len(rbac.seen) == 0 || !slices.Equal(rbac.seen[0].Roles, tc.roles) {
				t.Fatalf("RBAC saw %+v, want the token's roles %v", rbac.seen, tc.roles)
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

// TestMiddleware_LivenessRefusals proves the route guard itself runs the
// liveness check the validator's mode asks for, in the default hybrid mode
// and in online mode: an inactive token is refused with 401 invalid_token,
// a failed or missing liveness authority with 503, and in neither case does
// the request reach the handler.
func TestMiddleware_LivenessRefusals(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	key := newIssuerKey(t, testKID)
	online := func(string) AuthzMode { return AuthzModeOnline }

	for _, tc := range []struct {
		name       string
		mode       ModeResolver
		live       LivenessChecker
		wantStatus int
	}{
		{name: "hybrid default refuses an inactive token", live: &stubLiveness{results: []bool{false}},
			wantStatus: http.StatusUnauthorized},
		{name: "online refuses an inactive token", mode: online, live: &stubLiveness{results: []bool{false}},
			wantStatus: http.StatusUnauthorized},
		{name: "hybrid default cannot decide when the liveness authority fails",
			live: &stubLiveness{err: errors.New("introspection down")}, wantStatus: http.StatusServiceUnavailable},
		{name: "online cannot decide when the liveness authority fails", mode: online,
			live: &stubLiveness{err: errors.New("introspection down")}, wantStatus: http.StatusServiceUnavailable},
		{name: "hybrid default cannot decide without a liveness authority", wantStatus: http.StatusServiceUnavailable},
		{name: "online cannot decide without a liveness authority", mode: online,
			wantStatus: http.StatusServiceUnavailable},
		{name: "hybrid default admits an active token", live: &stubLiveness{results: []bool{true}},
			wantStatus: http.StatusOK},
		{name: "online admits an active token", mode: online, live: &stubLiveness{results: []bool{true}},
			wantStatus: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{ModeResolver: tc.mode, Authorizer: &qrdbsdk.NoopAuthorizer{}, RBAC: &qrdbsdk.NoopAuthorizer{}}
			if tc.live != nil {
				opts.LivenessChecker = tc.live
			}
			v := newAuthzValidator(t, key, now, opts)
			called := false
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "https://daemon.example/v1/keys", nil)
			req.Header.Set("Authorization", "Bearer "+bearerToken(t, key, now, []string{"keys:get"}, "tenant-1"))
			w := httptest.NewRecorder()
			v.Middleware("keys:get")(handler).ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			if called != (tc.wantStatus == http.StatusOK) {
				t.Fatalf("handler called %v, want %v", called, tc.wantStatus == http.StatusOK)
			}
			if tc.wantStatus == http.StatusUnauthorized &&
				!strings.Contains(w.Header().Get("WWW-Authenticate"), "invalid_token") {
				t.Fatalf("challenge %q, want invalid_token", w.Header().Get("WWW-Authenticate"))
			}
			if strings.Contains(w.Body.String(), "platformtoken:") {
				t.Fatalf("the validator's detail reached the caller: %q", w.Body.String())
			}
		})
	}

	t.Run("AuthorizeRequest refuses an inactive token in online mode", func(t *testing.T) {
		v := newAuthzValidator(t, key, now, Options{
			ModeResolver: online, LivenessChecker: &stubLiveness{results: []bool{false}},
			Authorizer: &qrdbsdk.NoopAuthorizer{}, RBAC: &qrdbsdk.NoopAuthorizer{},
		})
		req := httptest.NewRequest(http.MethodGet, "https://daemon.example/v1/keys", nil)
		req.Header.Set("Authorization", "Bearer "+bearerToken(t, key, now, []string{"keys:get"}, "tenant-1"))
		ac, err := v.AuthorizeRequest(req, "keys:get")
		if !errors.As(err, new(*TokenInactiveError)) {
			t.Fatalf("AuthorizeRequest error %v, want a TokenInactiveError", err)
		}
		if ac != nil {
			t.Fatalf("AuthorizeRequest returned a context for an inactive token: %+v", ac)
		}
	})
}
