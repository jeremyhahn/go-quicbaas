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
	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
)

// init registers this build's MCP-tool permission contribution
// (module_grants.go's mcpToolContributor) so registerRoleGrants derives from
// the full MCP tool table whenever the mcp tag is compiled in. Public tools
// carry qrdbsdk.PermissionIdentityGated, which contributeGRPCPermissions'
// sibling logic already excludes; ToolSpec.Public is skipped directly here
// for the same reason.
func init() {
	mcpToolContributor = func(set map[string]struct{}) {
		for _, spec := range mcpapi.ModuleTools() {
			if spec.Public {
				continue
			}
			if spec.Resource != "" && spec.Action != "" {
				set[spec.Resource+":"+spec.Action] = struct{}{}
			}
		}
	}
}
