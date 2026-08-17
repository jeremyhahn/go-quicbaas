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
		{"version tool is public", "quicbaas_version", true},
		{"liveness tool is public", "quicbaas_liveness", true},
		{"get tool is not public", "qrdb_get", false},
		{"put tool is not public", "qrdb_put", false},
		{"delete tool is not public", "qrdb_delete", false},
		{"cluster_status is not public", "quicbaas_cluster_status", false},
		{"unknown tool is not public", "unknown_tool", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsPublicTool(tc.toolName)
			assert.Equal(t, tc.expected, result)
		})
	}
}
