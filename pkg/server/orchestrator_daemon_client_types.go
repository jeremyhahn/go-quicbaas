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

// This file is intentionally NOT gated by //go:build iam. The types
// declared here are consumed by non-iam-tagged files (notably server.go's
// Server struct field), so they must be compilable in every build of
// pkg/server regardless of the iam tag.

package server

// OrchestratorDaemonClient holds the credentials + scopes the central uses
// to authenticate to remote agent daemons. Mirrors the credentials the
// daemons themselves hold — the central is just another client of the
// AgentRuntime service.
//
// The concrete provisioning logic lives in pkg/server/iam_init.go under
// the iam build tag; callers that run without the iam tag see a nil
// *OrchestratorDaemonClient on the Server struct and the resolver
// gracefully degrades to local-only dispatch.
type OrchestratorDaemonClient struct {
	ClientID     string
	ClientSecret string
	Scopes       []string
	TenantID     string
}
