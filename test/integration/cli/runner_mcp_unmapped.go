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

// expectMCPUnmappedRefusal runs cmd and asserts the MCP tool authorizer
// refuses it when the runner's transport is MCP. The federated identity
// list/unlink tools and the MFA tools carry no MCP permission registration on
// purpose (pkg/api/mcp/authz_qrdb_unmapped.go), so MCP refuses them. It
// returns true when it asserted the refusal (the caller ends the test there)
// and false, without running cmd, on every other transport, which keeps its
// own path.
func expectMCPUnmappedRefusal(t *testing.T, runner *TestRunner, cmd CommandDef) bool {
	t.Helper()
	if runner.transport != TransportMCP {
		return false
	}
	stdout, stderr, err := runner.Run(cmd)
	combined := stdout + stderr
	require.Error(t, err, "[%s] %s: an unmapped MCP tool must be refused: stdout=%s stderr=%s",
		runner.transport, cmd.Name, stdout, stderr)
	require.Contains(t, combined, "FORBIDDEN",
		"[%s] %s should be refused with FORBIDDEN: %s", runner.transport, cmd.Name, combined)
	require.Contains(t, combined, "no permission registration",
		"[%s] %s should be refused for having no permission registration", runner.transport, cmd.Name)
	return true
}
