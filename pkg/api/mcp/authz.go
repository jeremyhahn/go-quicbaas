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

// publicTools lists tools that do not require authentication. On the
// unified server these are the tools the module (pkg/apimodule
// MCPToolPermissions) declares with the identity-gated sentinel rather than
// a qrdb-canonical resource:action permission — authenticated at qrdb's
// gate, the tool itself is harmless discovery/health.
var publicTools = map[string]bool{
	"quicbaas_health":    true,
	"quicbaas_version":   true,
	"quicbaas_liveness":  true,
	"quicbaas_readiness": true,
	// qrdb-mirror health tool (Health.Check is exposed as qrdb_health — see
	// pkg/api/mcp/CLAUDE.md). Liveness/readiness stay quicbaas_*.
	"qrdb_health": true,
	// Login is pre-authentication on every transport: gRPC lists
	// Login/BeginLogin and FinishLogin among its public methods and REST
	// declares the login routes in authz_public_routes.go (owner item 134,
	// ruled permit). The mtls factor still needs the TLS leaf qrdb verified.
	"quicbaas_login_begin":  true,
	"quicbaas_login_finish": true,
}

// IsPublicTool returns true if the tool does not require authentication.
func IsPublicTool(toolName string) bool {
	return publicTools[toolName]
}
