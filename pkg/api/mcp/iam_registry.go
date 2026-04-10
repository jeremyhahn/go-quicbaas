//go:build mcp && iam

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

// Package mcp implements IAM integration with the ToolRegistry.
package mcp

import (
	"strings"
)

// isIAMTool returns true if the tool name is an IAM or MFA/PEP tool.
func isIAMTool(name string) bool {
	return strings.HasPrefix(name, "quicbaas_iam_") ||
		strings.HasPrefix(name, "quicbaas_mfa_") ||
		strings.HasPrefix(name, "quicbaas_pep_")
}
