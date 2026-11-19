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

// Package capabilities reads the server's capabilities report: which
// subsystems the server serves (has the handlers for) and has configured (has
// the service those handlers use wired), with the setting or build tag that
// enables each one it does not, and the SDK families it serves.
//
// The report is served on every transport: GET /api/v1/capabilities on REST
// and QUIC, the quicbaas_capabilities tool on MCP, GetCapabilities on gRPC
// and Unix, and in process on Embedded. Any authenticated caller may read it.
package capabilities
