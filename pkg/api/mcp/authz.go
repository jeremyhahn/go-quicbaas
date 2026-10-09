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

// publicTools lists tools that do not require authentication: the login
// ceremony (there is no way in without it) and the health probes (a probe
// must not need a token during exactly the states it reports). It is the set
// docs/mcp-server/user.md documents, and every entry must name a registered
// tool (TestMCPPermissions_PublicToolsExist). On the unified server these are
// the tools the module (pkg/apimodule MCPToolPermissions) declares with the
// identity-gated sentinel rather than a qrdb-canonical resource:action
// permission.
//
// Each addition publishes an unauthenticated capability: it is an
// access-control decision, not a convenience.
var publicTools = map[string]bool{
	// Siblings treat login as pre-authentication too: gRPC lists
	// BeginLogin/FinishLogin among its public methods and REST declares the
	// login routes in authz_public_routes.go.
	"quicbaas_login_begin":  true,
	"quicbaas_login_finish": true,
	// qrdb-mirror health tool (Health.Check is exposed as qrdb_health — see
	// pkg/api/mcp/CLAUDE.md). Liveness/readiness stay quicbaas_*.
	"qrdb_health":        true,
	"quicbaas_liveness":  true,
	"quicbaas_readiness": true,
}

// IsPublicTool returns true if the tool does not require authentication.
func IsPublicTool(toolName string) bool {
	return publicTools[toolName]
}
