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
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// HRD resolves the tenant from the SNI host when no SPIFFE SAN is present.
func TestDeriveLoginTenant_HRDFromSNIHost(t *testing.T) {
	hrd := &fakeRealmResolver{byHost: map[string]string{"acme.app.example": "tenant-sni"}}
	srv := newDeriveTestServer(t, "example.com", hrd)
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.TLS = &tls.ConnectionState{ServerName: "acme.app.example"}

	got, err := srv.deriveLoginTenant(context.Background(), req, nil, "")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if got != "tenant-sni" {
		t.Fatalf("tenant = %q, want tenant-sni", got)
	}
}

// A broker cert with no SPIFFE SAN whose HRD also fails yields the
// reissue-required error (cert != nil branch of fail-closed).
func TestDeriveLoginTenant_NoSAN_ReissueRequired(t *testing.T) {
	hrd := &fakeRealmResolver{}
	srv := newDeriveTestServer(t, "example.com", hrd)
	cert := spiffeLeafCert(t, "broker", "", "")
	req := httptest.NewRequest(http.MethodPost, "/login", nil)

	_, err := srv.deriveLoginTenant(context.Background(), req, cert, "")
	if err != ErrLoginCertReissueRequired {
		t.Fatalf("err = %v, want ErrLoginCertReissueRequired", err)
	}
}

// A nil HomeRealmResolver with a non-mTLS login fails closed (unresolved).
func TestDeriveLoginTenant_NoResolver_FailsClosed(t *testing.T) {
	srv := newDeriveTestServer(t, "example.com", nil)
	req := httptest.NewRequest(http.MethodPost, "/login", nil)

	_, err := srv.deriveLoginTenant(context.Background(), req, nil, "")
	if err != ErrLoginTenantUnresolved {
		t.Fatalf("err = %v, want ErrLoginTenantUnresolved", err)
	}
}

// requestSNIHost prefers the TLS ServerName over the HTTP Host header.
func TestRequestSNIHost_PrefersServerName(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://host.example:8443/x", nil)
	req.TLS = &tls.ConnectionState{ServerName: "sni.example"}
	if got := requestSNIHost(req); got != "sni.example" {
		t.Fatalf("requestSNIHost = %q, want sni.example", got)
	}
}

// requestSNIHost strips the port from the Host header when there is no SNI.
func TestRequestSNIHost_StripsPortFromHost(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://host.example:8443/x", nil)
	if got := requestSNIHost(req); got != "host.example" {
		t.Fatalf("requestSNIHost = %q, want host.example", got)
	}
}

func TestTenantFromOAuthClientID(t *testing.T) {
	tests := []struct {
		name     string
		clientID string
		want     string
	}{
		{"valid", "app_tenant-acme_abc123", "tenant-acme"},
		{"no_prefix", "client_tenant_x", ""},
		{"no_suffix_separator", "app_tenantonly", ""},
		{"empty_tenant", "app__suffix", ""},
		{"multi_underscore_tenant", "app_a_b_suffix", "a_b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tenantFromOAuthClientID(tt.clientID); got != tt.want {
				t.Fatalf("tenantFromOAuthClientID(%q) = %q, want %q", tt.clientID, got, tt.want)
			}
		})
	}
}

func TestLoginContextWithTenant(t *testing.T) {
	ctx := loginContextWithTenant(context.Background(), "tenant-z")
	if got := boundTenant(ctx); got != "tenant-z" {
		t.Fatalf("TenantFromContext = %q, want tenant-z", got)
	}
}
