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

// GetToolResourceActionForTest is a test helper that exposes the internal
// getToolResourceAction function for testing purposes.
// This allows integration tests to verify tool-to-resource/action mappings.
func GetToolResourceActionForTest(toolName string) (resource, action string) {
	return getToolResourceAction(toolName)
}
