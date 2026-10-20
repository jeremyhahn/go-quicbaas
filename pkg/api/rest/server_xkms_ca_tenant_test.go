//go:build rest

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

package rest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

func newXKMSCATenantTestCSR(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "alice"},
	}, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

// TestXKMSCA_SignAndIssueTenantBinding drives the REST sign and issue routes
// with a caller bound to tenant-a: its own tenant_id is served, a foreign one
// is refused with go-xkms's not-found status for a foreign tenant.
func TestXKMSCA_SignAndIssueTenantBinding(t *testing.T) {
	routes := []struct {
		name    string
		path    string
		handler func(*Server, http.ResponseWriter, *http.Request)
		body    func(tenantID string) any
		okCode  int
	}{
		{
			name:    "SignCSR",
			path:    "/api/v1/xkms/ca/sign",
			handler: (*Server).HandleXKMSSignCSR,
			body: func(tenantID string) any {
				return &xkmstransport.SignCSRRequest{CSRPEM: newXKMSCATenantTestCSR(t), Profile: "client", TenantID: tenantID}
			},
			okCode: http.StatusOK,
		},
		{
			name:    "IssueCertificate",
			path:    "/api/v1/xkms/ca/issue",
			handler: (*Server).HandleXKMSIssueCertificate,
			body: func(tenantID string) any {
				return &xkmstransport.IssueCertificateRequest{CommonName: "bob", Profile: "client", ValidityDays: 1, TenantID: tenantID}
			},
			okCode: http.StatusCreated,
		},
	}
	cases := []struct {
		tenantID string
		want     func(okCode int) int
	}{
		{"tenant-a", func(okCode int) int { return okCode }},
		{"tenant-b", func(int) int { return http.StatusNotFound }},
	}

	s := &Server{config: &Config{SPIFFETrustDomain: "example.test"}}
	s.SetCertificateAuthority(testca.New(t, "rest-tenant-ca"))

	for _, route := range routes {
		for _, tc := range cases {
			t.Run(route.name+"/tenant_id="+tc.tenantID, func(t *testing.T) {
				payload, err := json.Marshal(route.body(tc.tenantID))
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				r := httptest.NewRequest(http.MethodPost, route.path, bytes.NewReader(payload))
				r = r.WithContext(tenantctx.WithTenant(r.Context(), "tenant-a"))
				w := httptest.NewRecorder()

				route.handler(s, w, r)

				if want := tc.want(route.okCode); w.Code != want {
					t.Fatalf("status = %d, want %d; body=%s", w.Code, want, w.Body.String())
				}
			})
		}
	}
}
