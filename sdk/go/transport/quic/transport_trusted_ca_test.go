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

package quic

import (
	"context"
	"net/http"
	"sync"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTrustedCAOverHTTP3 pins #1330 on QUIC: the SDK QUIC transport is the
// REST client over HTTP/3, so every verb of the re-exported go-xkms
// CAService (quicbaas.TrustedCA) reaches the node's /api/v1/xkms/ca/* routes,
// which the unified REST/HTTP3 listener serves.
func TestTrustedCAOverHTTP3(t *testing.T) {
	var mu sync.Mutex
	var hit string
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hit = r.Method + " " + r.URL.Path
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"certificate_pem":"TEVBRg==","serial_number":"0a","revoked":true,"crl_pem":"Q1JM","success":true}`))
	}))
	t.Cleanup(shutdown)
	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	t.Cleanup(func() { tr.Close() })

	cases := []struct {
		method string
		req    interface{}
		want   string
	}{
		{"GetCABundle", &xkmstransport.GetCABundleRequest{TenantID: "t1"}, "GET /api/v1/xkms/ca/bundle"},
		{"GetCACertificate", &xkmstransport.GetCACertificateRequest{Identity: "root"}, "GET /api/v1/xkms/ca/cert"},
		{"SignCSR", &xkmstransport.SignCSRRequest{CSRPEM: []byte("CSR")}, "POST /api/v1/xkms/ca/sign"},
		{"IssueCertificate", &xkmstransport.IssueCertificateRequest{CommonName: "node-1"}, "POST /api/v1/xkms/ca/issue"},
		{"RevokeCertificate", &xkmstransport.RevokeCertificateRequest{SerialNumber: "0a"}, "POST /api/v1/xkms/ca/revoke"},
		{"GenerateCRL", &xkmstransport.GenerateCRLRequest{TenantID: "t1"}, "POST /api/v1/xkms/ca/crl"},
		{"IsRevoked", &xkmstransport.IsRevokedRequest{SerialNumber: "0a"}, "GET /api/v1/xkms/ca/revoked/0a"},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			resp, err := tr.Request(context.Background(), "quicbaas.TrustedCA", tc.method, tc.req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, tc.want, hit)
		})
	}
}
