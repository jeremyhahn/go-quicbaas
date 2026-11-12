// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

package cli

import "testing"

// TestAllocBarrierPorts_QUICIsTheRESTPort pins that the barrier matrix
// probes QUIC on the REST port: the daemon serves HTTP/3 on the REST
// listener's own address and binds no separate QUIC port.
func TestAllocBarrierPorts_QUICIsTheRESTPort(t *testing.T) {
	ports := allocBarrierPorts(t, t.TempDir())
	if ports.REST == 0 {
		t.Fatalf("REST port = 0, want a reserved port")
	}
	if ports.QUIC != ports.REST {
		t.Fatalf("QUIC port = %d, want the REST port %d", ports.QUIC, ports.REST)
	}
}
