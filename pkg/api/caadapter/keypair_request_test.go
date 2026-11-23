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

package caadapter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKeypairCertificateRequest pins the keypair-issuance request every
// transport builds: subject, parsed SANs, validity and tenant always, and the
// tenant SPIFFE SAN fields only when both a trust domain and a tenant are set.
func TestKeypairCertificateRequest(t *testing.T) {
	cases := []struct {
		name, cn, profile, tenant, domain string
		wantKind, wantName                string
	}{
		{name: "no-domain", cn: "alice", profile: "client", tenant: "acme"},
		{name: "no-tenant", cn: "alice", profile: "client", domain: "example.com"},
		{name: "client-is-user", cn: "alice", profile: "client", tenant: "acme", domain: "example.com", wantKind: "user", wantName: "alice"},
		{name: "server-is-svc", cn: "acme-svc", profile: "server", tenant: "acme", domain: "example.com", wantKind: "svc", wantName: "acme-svc"},
		{name: "name-sanitized", cn: "admin@example.com", profile: "client", tenant: "acme", domain: "example.com", wantKind: "user", wantName: "admin_40example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := KeypairCertificateRequest(tc.cn, "Acme", []string{"DNS:a.example.com", "IP:10.0.0.1", "Email:a@example.com", "URI:https://a.example.com", "bare.example.com"}, 30, tc.profile, tc.tenant, tc.domain)
			require.Equal(t, tc.cn, req.Subject.CommonName, "the subject CN is kept as requested")
			require.Equal(t, "Acme", req.Subject.Organization)
			require.Equal(t, 30, req.Valid)
			require.Equal(t, tc.tenant, req.TenantID)
			require.NotNil(t, req.SANS)
			require.Equal(t, []string{"a.example.com", "bare.example.com"}, req.SANS.DNS)
			require.Equal(t, []string{"10.0.0.1"}, req.SANS.IPs)
			require.Equal(t, []string{"a@example.com"}, req.SANS.Email)
			require.Equal(t, []string{"https://a.example.com"}, req.SANS.URIs)
			if tc.wantKind == "" {
				require.Empty(t, req.SPIFFETrustDomain)
				require.Empty(t, req.SPIFFEKind)
				require.Empty(t, req.SPIFFEName)
				return
			}
			require.Equal(t, tc.domain, req.SPIFFETrustDomain)
			require.Equal(t, tc.wantKind, req.SPIFFEKind)
			require.Equal(t, tc.wantName, req.SPIFFEName)
		})
	}
	require.Nil(t, KeypairCertificateRequest("x", "", nil, 0, "", "", "").SANS, "no SANs gives none")
}
