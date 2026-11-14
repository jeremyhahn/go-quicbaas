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

package iam

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

func daemonGuardClientCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
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

// The go-trusted-platform daemon's REST plane (go-trusted-platform#644, owner
// ruling 133) mounts the sdk/go route guard over the tokens this node issues:
// a WebAuthn login, a PIV smartcard login and an mTLS service login each reach
// the daemon's handler with the token's tenant and subject, the role's RBAC
// permissions decide the route, and an mTLS-bound token needs its certificate.
func TestDaemonRESTGuard_AdmitsTokensThisNodeIssues(t *testing.T) {
	const issuer = "https://broker.example.com"
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("issuer key: %v", err)
	}
	rbac := &fakeRBACClient{permsByRole: map[string][]string{"keyholder": {"keys:get"}}}
	svc := newKIDTokenService(t, priv, &OIDCConfig{
		Issuer:           issuer,
		SigningAlgorithm: "ES256",
		AccessTokenTTL:   time.Hour,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}, &fakePermissionResolver{client: rbac})

	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:       issuer,
		Audience:     issuer,
		JWKS:         svc.keyManager.JWKS(),
		ModeResolver: func(string) platformtoken.AuthzMode { return platformtoken.AuthzModeOffline },
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ok := platformtoken.FromContext(r.Context())
		if !ok {
			t.Error("the guard passed a request without binding its principal")
			return
		}
		_, _ = w.Write([]byte(ac.TenantID + "/" + ac.Subject))
	})

	hardware := func(amr []string) string {
		t.Helper()
		set, err := svc.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
			User:        &User{ID: 42, TenantID: "system", Email: "k@example.com", Name: "K", Roles: []string{"keyholder"}},
			AMR:         amr,
			Attestation: testHardwareAttestation("cb69481e"),
		})
		if err != nil {
			t.Fatalf("IssueHardwareTokens(%v): %v", amr, err)
		}
		return set.AccessToken
	}
	webauthn := hardware([]string{"webauthn", "hwk"})
	piv := hardware([]string{"smartcard", "mfa"})

	clientCert := daemonGuardClientCert(t, "svc-daemon")
	sum := sha256.Sum256(clientCert.Raw)
	svcSet, err := svc.IssueServiceAccountTokens(context.Background(), ServiceAccountTokenOptions{
		ClientID:           "svc-daemon",
		TenantID:           "system",
		SCP:                []string{"keys:get"},
		CertThumbprintS256: base64.RawURLEncoding.EncodeToString(sum[:]),
		AMR:                []string{"mtls"},
	})
	if err != nil {
		t.Fatalf("IssueServiceAccountTokens: %v", err)
	}
	mtls := svcSet.AccessToken

	for _, tc := range []struct {
		name       string
		token      string
		permission string
		peer       *x509.Certificate
		wantStatus int
		wantBody   string
	}{
		{"webauthn login, granted", webauthn, "keys:get", nil, http.StatusOK, "system/42"},
		{"webauthn login, not granted", webauthn, "keys:delete", nil, http.StatusForbidden, ""},
		{"piv login, granted", piv, "keys:get", nil, http.StatusOK, "system/42"},
		{"piv login, not granted", piv, "keys:delete", nil, http.StatusForbidden, ""},
		{"mtls login with its certificate", mtls, "keys:get", clientCert, http.StatusOK, "system/svc-daemon"},
		{"mtls login without a certificate", mtls, "keys:get", nil, http.StatusUnauthorized, ""},
		{"mtls login with another certificate", mtls, "keys:get", daemonGuardClientCert(t, "other"), http.StatusUnauthorized, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "https://daemon.example/v1/keys?tenant_id=acme",
				strings.NewReader(`{"tenant_id":"acme","user_id":"9001"}`))
			req.Header.Set("Authorization", "Bearer "+tc.token)
			req.Header.Set("X-Tenant-ID", "acme")
			if tc.peer != nil {
				req.TLS.PeerCertificates = []*x509.Certificate{tc.peer}
			}
			w := httptest.NewRecorder()
			validator.Middleware(tc.permission)(echo).ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantBody != "" && w.Body.String() != tc.wantBody {
				t.Fatalf("principal %q, want %q", w.Body.String(), tc.wantBody)
			}
		})
	}
}
