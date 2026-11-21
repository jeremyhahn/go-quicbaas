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

package quicbaas

import "testing"

func TestClient_Services_NonNil_AndCached(t *testing.T) {
	c := &client{transport: &mockTransport{}, opts: defaultOptions()}
	first := c.Services()
	if first == nil {
		t.Fatal("Services() returned nil")
	}
	if second := c.Services(); first != second {
		t.Error("Services() should return the same instance across calls")
	}
	if got := (&clusterManagementService{c: c}).Services(); got != first {
		t.Error("the cluster management group should delegate Services() to the client")
	}
}
