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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestTrustedCA_RoutesToXKMSCAMount pins the #1330 REST re-export of the
// go-xkms CAService: every method reaches the route the server mounts go-xkms's
// CA handlers on (/api/v1/xkms/ca/*), with go-xkms's own request fields, and
// decodes go-xkms's response type. Before, the default dispatch sent
// quicbaas.TrustedCA verbs to /api/v1/TrustedCA/{method}, which no server route
// serves.
func TestTrustedCA_RoutesToXKMSCAMount(t *testing.T) {
	type seen struct {
		method, path, query string
		body                map[string]interface{}
	}
	var got seen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = seen{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&got.body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"certificate_pem":"LEAF","serial_number":"0a","revoked":true,"crl_pem":"Q1JM","success":true}`))
	}))
	defer server.Close()
	tr := newSecurityTestTransport(t, server, "")

	cases := []struct {
		method    string
		req       interface{}
		wantVerb  string
		wantPath  string
		wantQuery string
		wantBody  map[string]interface{}
	}{
		{"GetCABundle", &xkmstransport.GetCABundleRequest{TenantID: "t1"}, http.MethodGet, "/api/v1/xkms/ca/bundle", "tenant_id=t1", nil},
		{"GetCACertificate", &xkmstransport.GetCACertificateRequest{Identity: "root"}, http.MethodGet, "/api/v1/xkms/ca/cert", "identity=root", nil},
		{"SignCSR", &xkmstransport.SignCSRRequest{CSRPEM: []byte("CSR"), Profile: "server", TenantID: "t1"}, http.MethodPost, "/api/v1/xkms/ca/sign", "",
			map[string]interface{}{"csr_pem": []byte("CSR"), "profile": "server", "tenant_id": "t1"}},
		{"IssueCertificate", &xkmstransport.IssueCertificateRequest{CommonName: "node-1", Profile: "server", SANs: []string{"node-1"}}, http.MethodPost, "/api/v1/xkms/ca/issue", "",
			map[string]interface{}{"common_name": "node-1", "profile": "server", "sans": []interface{}{"node-1"}}},
		{"RevokeCertificate", &xkmstransport.RevokeCertificateRequest{SerialNumber: "0a", Reason: 1}, http.MethodPost, "/api/v1/xkms/ca/revoke", "",
			map[string]interface{}{"serial_number": "0a", "reason": 1}},
		{"GenerateCRL", &xkmstransport.GenerateCRLRequest{TenantID: "t1"}, http.MethodPost, "/api/v1/xkms/ca/crl", "",
			map[string]interface{}{"tenant_id": "t1"}},
		{"IsRevoked", &xkmstransport.IsRevokedRequest{SerialNumber: "0a"}, http.MethodGet, "/api/v1/xkms/ca/revoked/0a", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			got = seen{}
			resp, err := tr.Request(context.Background(), securityServiceTrustedCA, tc.method, tc.req)
			if err != nil {
				t.Fatalf("Request: %v", err)
			}
			if resp == nil {
				t.Fatal("nil response")
			}
			if got.method != tc.wantVerb || got.path != tc.wantPath || got.query != tc.wantQuery {
				t.Fatalf("hit %s %s?%s, want %s %s?%s", got.method, got.path, got.query, tc.wantVerb, tc.wantPath, tc.wantQuery)
			}
			for k, v := range tc.wantBody {
				gb, _ := json.Marshal(got.body[k])
				wb, _ := json.Marshal(v)
				if string(gb) != string(wb) {
					t.Errorf("body[%q] = %s, want %s", k, gb, wb)
				}
			}
		})
	}

	resp, err := tr.Request(context.Background(), securityServiceTrustedCA, "IsRevoked", &xkmstransport.IsRevokedRequest{SerialNumber: "0a"})
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := resp.(*xkmstransport.IsRevokedResponse); !ok || !r.Revoked {
		t.Fatalf("IsRevoked decoded %#v, want *IsRevokedResponse{Revoked: true}", resp)
	}
}

// TestTrustedCA_RejectsWrongRequestType keeps the typed contract: a request of
// another type is refused before any round-trip.
func TestTrustedCA_RejectsWrongRequestType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected round-trip to %s", r.URL.Path)
	}))
	defer server.Close()
	tr := newSecurityTestTransport(t, server, "")
	for _, m := range []string{"GetCABundle", "GetCACertificate", "SignCSR", "IssueCertificate", "RevokeCertificate", "GenerateCRL", "IsRevoked"} {
		if _, err := tr.Request(context.Background(), securityServiceTrustedCA, m, struct{}{}); err == nil {
			t.Errorf("%s accepted a request of the wrong type", m)
		}
	}
	if _, err := tr.Request(context.Background(), securityServiceTrustedCA, "IsRevoked", &xkmstransport.IsRevokedRequest{}); err == nil {
		t.Error("IsRevoked accepted an empty serial")
	}
}
