//go:build mcp

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

package mcp

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPublicTool(t *testing.T) {
	testCases := []struct {
		name     string
		toolName string
		expected bool
	}{
		{"health tool is public", "qrdb_health", true},
		{"liveness tool is public", "quicbaas_liveness", true},
		{"readiness tool is public", "quicbaas_readiness", true},
		{"login begin is not public until abandoned attempts are bounded", "quicbaas_login_begin", false},
		{"login finish is not public until go-qrdb#1181", "quicbaas_login_finish", false},
		{"get tool is not public", "qrdb_get", false},
		{"put tool is not public", "qrdb_put", false},
		{"delete tool is not public", "qrdb_delete", false},
		{"cluster_status is not public", "quicbaas_cluster_status", false},
		{"unregistered version tool is not public", "quicbaas_version", false},
		{"unknown tool is not public", "unknown_tool", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsPublicTool(tc.toolName)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestPublicTools_MatchDocumentedSet pins the unauthenticated MCP surface to
// the set docs/mcp-server/user.md documents. Widening it publishes an
// unauthenticated capability, so a change must update the docs and this test
// together, as one reviewed access-control decision.
func TestPublicTools_MatchDocumentedSet(t *testing.T) {
	documented := []string{
		"qrdb_health",
		"quicbaas_liveness",
		"quicbaas_readiness",
	}

	got := make([]string, 0, len(publicTools))
	for name, public := range publicTools {
		assert.Truef(t, public, "publicTools entry %q is false; delete it instead", name)
		got = append(got, name)
	}
	sort.Strings(got)
	assert.Equal(t, documented, got)
}

// TestLoginBegin_StaysDeclaredDead pins the answer to the abandoned-attempt
// finding: quicbaas_login_begin allocates a login attempt that only
// quicbaas_login_finish removes, the pinned go-qrdb SDK neither expires nor
// caps abandoned attempts, and login_finish is refused on MCP. So the tool is
// not public and maps to no permission: it is declared dead, with that reason.
func TestLoginBegin_StaysDeclaredDead(t *testing.T) {
	const tool = "quicbaas_login_begin"

	assert.False(t, IsPublicTool(tool), "%s must not be public", tool)

	_, _, mapped := getToolQRDBPermission(tool)
	assert.False(t, mapped, "%s must map to no permission", tool)

	group, dead := unmappedTools[tool]
	if assert.True(t, dead, "%s must be declared dead in unmappedToolGroups", tool) {
		assert.Contains(t, group.reason, "abandoned attempts")
		assert.Contains(t, group.resolution, "expires and caps")
	}

	finish, dead := unmappedTools["quicbaas_login_finish"]
	if assert.True(t, dead, "quicbaas_login_finish must stay declared dead") {
		assert.NotEqual(t, group.reason, finish.reason, "the two login tools are dead for different reasons")
	}
}
