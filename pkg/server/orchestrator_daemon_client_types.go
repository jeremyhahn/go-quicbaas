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

// OrchestratorDaemonClient and related types are consumed by server.go's
// Server struct, so they live in this dedicated file within pkg/server.

package server

// OrchestratorDaemonClient holds the credentials + scopes the central uses
// to authenticate to remote agent daemons. Mirrors the credentials the
// daemons themselves hold — the central is just another client of the
// AgentRuntime service.
//
// The concrete provisioning logic lives in pkg/server/iam_init.go; until it
// runs, callers see a nil *OrchestratorDaemonClient on the Server struct and
// the resolver gracefully degrades to local-only dispatch.
type OrchestratorDaemonClient struct {
	ClientID     string
	ClientSecret string
	Scopes       []string
	TenantID     string
}
