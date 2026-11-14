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
	"net/http"
	"net/http/httptest"
	"testing"
)

// The end-to-end behaviour of the adapter is exercised in the live
// integration smoke-test against the daemon (see docs/xkms-ca-delegation.md).
// The unit tests below cover the pure translation logic and the
// "CA not configured" fast-path that must return 503 without any CA
// dependency.

// TestXKMSCA_NotConfiguredReturns503 verifies every CA endpoint returns
// 503 Service Unavailable when the CA has not been installed via
// SetCertificateAuthority. Exercises the fast-fail path before any CA
// interaction.
func TestXKMSCA_NotConfiguredReturns503(t *testing.T) {
	routes := []struct {
		name    string
		method  string
		path    string
		handler func(*Server, http.ResponseWriter, *http.Request)
	}{
		{"GetCABundle", http.MethodGet, "/api/v1/xkms/ca/bundle", (*Server).HandleXKMSGetCABundle},
		{"GetCACertificate", http.MethodGet, "/api/v1/xkms/ca/cert", (*Server).HandleXKMSGetCACertificate},
		{"SignCSR", http.MethodPost, "/api/v1/xkms/ca/sign", (*Server).HandleXKMSSignCSR},
		{"IssueCertificate", http.MethodPost, "/api/v1/xkms/ca/issue", (*Server).HandleXKMSIssueCertificate},
		{"RevokeCertificate", http.MethodPost, "/api/v1/xkms/ca/revoke", (*Server).HandleXKMSRevokeCertificate},
		{"GenerateCRL", http.MethodPost, "/api/v1/xkms/ca/crl", (*Server).HandleXKMSGenerateCRL},
		{"IsRevoked", http.MethodGet, "/api/v1/xkms/ca/revoked/abcd", (*Server).HandleXKMSIsRevoked},
	}

	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			s := &Server{} // xkmsCAHandlers intentionally nil
			w := httptest.NewRecorder()
			r := httptest.NewRequest(route.method, route.path, nil)
			route.handler(s, w, r)
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("%s: want 503, got %d; body=%s", route.name, w.Code, w.Body.String())
			}
		})
	}
}

// TestXKMSCA_NotConfiguredError_Message ensures the typed error string
// is stable so CLI/log consumers can pattern-match against it.
func TestXKMSCA_NotConfiguredError_Message(t *testing.T) {
	err := &XKMSCANotConfiguredError{}
	if err.Error() != "xkms ca: certificate authority not configured" {
		t.Fatalf("unexpected error message: %q", err.Error())
	}
}
