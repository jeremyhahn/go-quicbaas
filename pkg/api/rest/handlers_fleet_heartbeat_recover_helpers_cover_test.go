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
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"
)

// TestRewriteWildcardHost_Branches exercises every decision branch of
// rewriteWildcardHost: unparsable bind addr, non-wildcard host, wildcard with a
// CN-bearing peer cert, wildcard with only DNS SANs, and wildcard with no usable
// peer identity.
func TestRewriteWildcardHost_Branches(t *testing.T) {
	cnPeer := &x509.Certificate{Subject: pkix.Name{CommonName: "agent-node-7"}}
	dnsPeer := &x509.Certificate{DNSNames: []string{"node-9.cluster.local"}}
	emptyPeer := &x509.Certificate{}

	cases := []struct {
		name     string
		bindAddr string
		peer     *x509.Certificate
		want     string
	}{
		{
			name:     "unparsable bind addr returns input unchanged",
			bindAddr: "this-has-no-port",
			peer:     cnPeer,
			want:     "this-has-no-port",
		},
		{
			name:     "non-wildcard host returns input unchanged",
			bindAddr: "10.0.0.4:9091",
			peer:     cnPeer,
			want:     "10.0.0.4:9091",
		},
		{
			name:     "wildcard host rewrites from peer CN with agent- prefix stripped",
			bindAddr: "0.0.0.0:9091",
			peer:     cnPeer,
			want:     "node-7:9091",
		},
		{
			name:     "wildcard host falls back to first DNS SAN",
			bindAddr: "0.0.0.0:9091",
			peer:     dnsPeer,
			want:     "node-9.cluster.local:9091",
		},
		{
			name:     "wildcard host with empty peer identity returns input unchanged",
			bindAddr: "0.0.0.0:9091",
			peer:     emptyPeer,
			want:     "0.0.0.0:9091",
		},
		{
			name:     "wildcard host with nil peer returns input unchanged",
			bindAddr: "0.0.0.0:9091",
			peer:     nil,
			want:     "0.0.0.0:9091",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rewriteWildcardHost(tc.bindAddr, tc.peer)
			if got != tc.want {
				t.Errorf("rewriteWildcardHost(%q) = %q, want %q", tc.bindAddr, got, tc.want)
			}
		})
	}
}

// TestRecoveredHostFromCert_Branches covers the nil-peer, CN-derived,
// DNS-SAN-derived, and empty-identity branches of recoveredHostFromCert.
func TestRecoveredHostFromCert_Branches(t *testing.T) {
	cases := []struct {
		name string
		peer *x509.Certificate
		want string
	}{
		{
			name: "nil peer returns empty string",
			peer: nil,
			want: "",
		},
		{
			name: "CN with agent- prefix stripped and default port appended",
			peer: &x509.Certificate{Subject: pkix.Name{CommonName: "agent-10.0.0.5"}},
			want: "10.0.0.5:" + defaultRecoveryNodePort,
		},
		{
			name: "falls back to first DNS SAN with default port",
			peer: &x509.Certificate{DNSNames: []string{"legacy.node"}},
			want: "legacy.node:" + defaultRecoveryNodePort,
		},
		{
			name: "no CN and no SAN returns empty string",
			peer: &x509.Certificate{},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := recoveredHostFromCert(tc.peer)
			if got != tc.want {
				t.Errorf("recoveredHostFromCert() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSplitHostPortSafe_Branches covers the success and error branches of the
// splitHostPortSafe wrapper.
func TestSplitHostPortSafe_Branches(t *testing.T) {
	host, port, err := splitHostPortSafe("192.168.1.10:8443")
	if err != nil {
		t.Fatalf("unexpected error for valid host:port: %v", err)
	}
	if host != "192.168.1.10" || port != "8443" {
		t.Errorf("splitHostPortSafe = (%q, %q), want (192.168.1.10, 8443)", host, port)
	}

	h, p, err := splitHostPortSafe("missing-port")
	if err == nil {
		t.Fatal("expected error for input without a port, got nil")
	}
	if h != "" || p != "" {
		t.Errorf("splitHostPortSafe error path returned (%q, %q), want empty strings", h, p)
	}
}
