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

package iam

import "strings"

// =============================================================================
// Agent Application Name Derivation
// =============================================================================
//
// AgentAppName produces the canonical OAuth2 application name an agent
// daemon receives when it presents its mTLS client certificate at the
// /api/v1/agents/register endpoint. The function is centralised here so
// that:
//
//   - The REST register handler (server side) and the agent deploy
//     orchestrator (client side) compute the SAME value.
//   - The deploy command can pre-compute the AgentID before the daemon
//     even enrolls, enabling deterministic wait-online polling that
//     does not depend on host-string heuristics.
//
// The construction is intentionally trivial — string concatenation plus
// a fixed sanitiser — so both sides can produce the same value with no
// shared runtime state.

// SanitizeAppName maps a certificate Common Name into a stable IAM
// application-name segment. Whitespace, slashes, colons, and backslashes
// are replaced with hyphens because the OAuth2 AppManager treats the
// name as an opaque identifier and the underlying storage rejects
// reserved characters in some backends.
//
// An empty or whitespace-only input yields "unknown" so callers can
// always emit a non-empty appName.
func SanitizeAppName(cn string) string {
	cn = strings.TrimSpace(cn)
	if cn == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer(
		" ", "-",
		"/", "-",
		":", "-",
		"\\", "-",
	)
	return replacer.Replace(cn)
}

// AgentAppName returns the canonical OAuth2 application name (and
// orchestrator AgentID) for a daemon presenting a certificate with the
// supplied Common Name in the supplied tenant.
//
// The shape ("agent-<tenant>-<sanitised-cn>") is part of the deploy
// contract: changing it requires coordinated rollouts of both the
// register handler and any client-side deploy orchestrator that
// pre-computes an expected ID for wait-online polling.
func AgentAppName(tenantID, commonName string) string {
	return "agent-" + tenantID + "-" + SanitizeAppName(commonName)
}
