//go:build mcp

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

package apimodule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	quicbaaspermission "github.com/jeremyhahn/go-quicbaas/pkg/permission"
)

// TestSpendReportTool_DeclaresSpendReportRead pins go-quicbaas#885 on MCP.
// go-qrdb's MCP tool registry authorizes every call against the permission
// this module declares, so the declaration is the whole of quicbaas's part:
// the cost tool must declare spend_report:read, never orchestrator:status.
func TestSpendReportTool_DeclaresSpendReportRead(t *testing.T) {
	backend := newMCPBackend(t)
	m := newMCPModule(t, func() *mcpapi.Server { return backend })
	perms := m.MCPToolPermissions()

	var tool string
	for _, spec := range mcpapi.ModuleTools() {
		if spec.Name == "quicbaas_orchestrator_cost" {
			tool = spec.Tool
		}
	}
	require.NotEmpty(t, tool, "the module does not expose quicbaas_orchestrator_cost")

	perm, declared := perms[tool]
	require.True(t, declared, "the cost tool must be declared, not omitted")
	assert.Equal(t, quicbaaspermission.SpendReportRead, perm)
	assert.NotEqual(t, qrdbsdk.Permission("orchestrator:status"), perm,
		"a principal holding session status alone must not reach the spend report")
}
