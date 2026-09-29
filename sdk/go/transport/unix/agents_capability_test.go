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

package unix

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
)

// TestTransport_CarriesTheAgentsService: the Agents client refuses a transport
// that does not declare the service, so the delegation must declare it.
func TestTransport_CarriesTheAgentsService(t *testing.T) {
	var capable agents.CapableTransport = &Transport{}
	if !capable.SupportsAgents() {
		t.Fatal("the unix transport must carry the Agents service")
	}
}
