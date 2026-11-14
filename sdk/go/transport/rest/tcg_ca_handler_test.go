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

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestTCGCA_RoutesToXKMSTCGMount pins #1330's TCGCAService on REST: each verb
// posts go-xkms's own JSON request to /api/v1/xkms/ca/tcg/* and decodes the
// go-xkms response.
func TestTCGCA_RoutesToXKMSTCGMount(t *testing.T) {
	var gotVerb, gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVerb, gotPath, gotBody = r.Method, r.URL.Path, nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"certificate_der":"REVS","serial_number":"0a","iak_cert_der":"SUFL","credential_blob":"QkxPQg=="}`))
	}))
	defer server.Close()
	tr := newSecurityTestTransport(t, server, "")

	cases := []struct {
		method   string
		req      interface{}
		wantPath string
		wantKey  string
	}{
		{"IssueEKCertificate", &xkmstransport.IssueEKCertificateRequest{CommonName: "dev-1", EKPublicKey: []byte("EK")}, "/api/v1/xkms/ca/tcg/ek", "ek_public_key"},
		{"IssueAKCertificate", &xkmstransport.IssueAKCertificateRequest{CommonName: "ak-1", PublicKey: []byte("AK")}, "/api/v1/xkms/ca/tcg/ak", "public_key"},
		{"SignTCGCSR", &xkmstransport.SignTCGCSRRequest{CommonName: "dev-1", TCGCSR: []byte("CSR")}, "/api/v1/xkms/ca/tcg/sign-csr", "tcg_csr"},
		{"EnrollDevice", &xkmstransport.EnrollDeviceRequest{CommonName: "dev-1", PackedCSR: []byte("CSR")}, "/api/v1/xkms/ca/tcg/enroll", "packed_csr"},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(context.Background(), securityServiceTCGCA, tc.method, tc.req)
			if err != nil {
				t.Fatalf("Request: %v", err)
			}
			if resp == nil {
				t.Fatal("nil response")
			}
			if gotVerb != http.MethodPost || gotPath != tc.wantPath {
				t.Fatalf("hit %s %s, want POST %s", gotVerb, gotPath, tc.wantPath)
			}
			if gotBody["common_name"] == nil || gotBody[tc.wantKey] == nil {
				t.Fatalf("body %v lacks common_name or %s", gotBody, tc.wantKey)
			}
		})
	}

	resp, err := tr.Request(context.Background(), securityServiceTCGCA, "EnrollDevice", &xkmstransport.EnrollDeviceRequest{CommonName: "d", PackedCSR: []byte{1}})
	if err != nil {
		t.Fatal(err)
	}
	r, ok := resp.(*xkmstransport.EnrollDeviceResponse)
	if !ok || !bytes.Equal(r.IAKCertDER, []byte("IAK")) || !bytes.Equal(r.CredentialBlob, []byte("BLOB")) {
		t.Fatalf("EnrollDevice decoded %#v, want the IAK certificate and credential blob", resp)
	}
}

// TestTCGCA_RejectsWrongRequestType covers the request-type checks and an
// unknown verb.
func TestTCGCA_RejectsWrongRequestType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server reached for a rejected request")
	}))
	defer server.Close()
	tr := newSecurityTestTransport(t, server, "")
	for _, m := range []string{"IssueEKCertificate", "IssueAKCertificate", "SignTCGCSR", "EnrollDevice", "Unknown"} {
		if _, err := tr.Request(context.Background(), securityServiceTCGCA, m, struct{}{}); err == nil {
			t.Errorf("%s accepted a request of the wrong type", m)
		}
	}
}
