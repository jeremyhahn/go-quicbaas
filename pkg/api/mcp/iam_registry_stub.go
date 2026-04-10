//go:build mcp && !iam

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

// Package mcp implements IAM integration with the ToolRegistry (stub when IAM disabled).
package mcp

import (
	"context"
)

// isIAMTool returns false when IAM is disabled since no IAM tools are registered.
func isIAMTool(name string) bool {
	return false
}

// executeIAMTool is a stub when IAM is disabled.
func (r *ToolRegistry) executeIAMTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	return nil, nil
}
