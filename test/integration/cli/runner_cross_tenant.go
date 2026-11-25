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

//go:build integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// crossTenantRefusers lists the transports that refuse a system admin who acts
// for another tenant: gRPC, the Unix socket and MCP.
var crossTenantRefusers = []Transport{TransportGRPC, TransportUnix, TransportMCP}

// expectCrossTenantRefusal runs cmd, a system-admin command for another tenant,
// only on a transport in refusers (crossTenantRefusers when none is given). It
// requires the command to fail with FORBIDDEN and returns true, so the caller
// returns. Tenants are isolated: a system admin does not act for another
// tenant. On any other transport it runs nothing and returns false.
func expectCrossTenantRefusal(t *testing.T, runner *TestRunner, cmd CommandDef, refusers ...Transport) bool {
	t.Helper()
	if len(refusers) == 0 {
		refusers = crossTenantRefusers
	}
	refuses := false
	for _, tr := range refusers {
		if runner.transport == tr {
			refuses = true
			break
		}
	}
	if !refuses {
		return false
	}
	stdout, stderr, err := runner.Run(cmd)
	require.Error(t, err, "[%s] %s: a system admin acting for another tenant must be refused: stdout=%s stderr=%s",
		runner.transport, cmd.Name, stdout, stderr)
	require.Contains(t, stdout+stderr, "FORBIDDEN", "[%s] %s should be refused with FORBIDDEN", runner.transport, cmd.Name)
	return true
}
