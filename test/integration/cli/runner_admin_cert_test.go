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

//go:build integration

package cli

import "testing"

// TestPresentsAdminClientCert pins which runners present the fixture's admin
// client certificate: the transports that speak gRPC to the node (gRPC, and
// Unix over the socket), whose qrdb sub-client carries no platform bearer.
func TestPresentsAdminClientCert(t *testing.T) {
	tests := []struct {
		transport Transport
		want      bool
	}{
		{TransportGRPC, true},
		{TransportUnix, true},
		{TransportREST, false},
		{TransportMCP, false},
		{TransportQUIC, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.transport), func(t *testing.T) {
			if got := presentsAdminClientCert(tt.transport); got != tt.want {
				t.Errorf("presentsAdminClientCert(%q) = %v, want %v", tt.transport, got, tt.want)
			}
		})
	}
}
