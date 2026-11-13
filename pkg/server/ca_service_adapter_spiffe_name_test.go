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

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// emailCommonName is a valid X.509 subject common name that is not a SPIFFE
// path segment ('@' is outside the SPIFFE-ID §2.2 character set).
const emailCommonName = "admin@example.com"

// requireEmailCNWithSPIFFESAN parses certPEM and proves the subject common
// name is kept as requested while the SPIFFE SAN carries the separately
// derived workload name for the tenant.
func requireEmailCNWithSPIFFESAN(t *testing.T, certPEM []byte, tenantID string) {
	t.Helper()
	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	assert.Equal(t, emailCommonName, cert.Subject.CommonName, "the subject CN is kept as requested")
	require.Len(t, cert.URIs, 1, "a SPIFFE SAN is synthesised")
	uri := cert.URIs[0]
	assert.Equal(t, "spiffe", uri.Scheme)
	assert.Contains(t, uri.Path, "/"+tenantID+"/", "the SAN is scoped to the tenant")
	assert.True(t, strings.HasSuffix(uri.Path, "/admin_40example.com"),
		"the SAN's workload name is the escaped CN, got %q", uri.String())
}

// TestCAServiceAdapter_EmailCommonNameWithTrustDomain is the regression test
// for Codex 7420 P1 5 (#1894): with a SPIFFE trust domain set, a client
// certificate or CSR whose CN is an e-mail address is issued, for a tenant
// and for the system tenant, instead of being refused because the CN is not
// a SPIFFE path segment.
func TestCAServiceAdapter_EmailCommonNameWithTrustDomain(t *testing.T) {
	for _, tenantID := range []string{"acme-corp", qrdbsdk.SystemTenantID} {
		t.Run(tenantID, func(t *testing.T) {
			adapter := newRealAdapter(t)
			adapter.SetSPIFFETrustDomain("cluster.test")
			ctx := context.Background()

			t.Run("keypair", func(t *testing.T) {
				resp, err := adapter.IssueCertificate(ctx, &xkmstransport.IssueCertificateRequest{
					CommonName:   emailCommonName,
					ValidityDays: 30,
					Algorithm:    "ecdsa-p256",
					Profile:      "client",
					TenantID:     tenantID,
				})
				require.NoError(t, err)
				requireEmailCNWithSPIFFESAN(t, resp.CertificatePEM, tenantID)
			})

			t.Run("csr", func(t *testing.T) {
				key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				require.NoError(t, err)
				csrDER, err := x509.CreateCertificateRequest(rand.Reader,
					&x509.CertificateRequest{Subject: pkix.Name{CommonName: emailCommonName}}, key)
				require.NoError(t, err)

				resp, err := adapter.SignCSR(ctx, &xkmstransport.SignCSRRequest{
					CSRPEM:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}),
					ValidityDays: 30,
					Profile:      "client",
					TenantID:     tenantID,
				})
				require.NoError(t, err)
				requireEmailCNWithSPIFFESAN(t, resp.CertificatePEM, tenantID)
			})
		})
	}
}
