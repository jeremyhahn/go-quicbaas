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

// Package mcp provides the Model Context Protocol (MCP) transport implementation
// for the QuicBaaS SDK. MCP is a JSON-RPC 2.0 based protocol designed for AI/LLM
// systems to interact with database operations through a standardized interface.
//
// The MCP transport enables:
//   - JSON-RPC 2.0 protocol over HTTP POST requests
//   - Tool-based database operations via the tools/call method
//   - MCP protocol version negotiation (supports "2024-11-05")
//   - Bearer token authentication via Authorization header
//   - Automatic service/method to tool name mapping
//
// Tool Name Mapping:
//
// The transport maps SDK service/method names to MCP tool names. qrdb-mirror
// services (KV, Vector, Timeseries, Health.Check, RBAC user/role lifecycle,
// Barrier, Ceremony) are routed to canonical qrdb_* tools; quicbaas-specific
// surfaces (KMS, Crypto, Orchestrator, Scheduler, Agent, etc.) keep the
// quicbaas_ prefix.
//
// Examples:
//   - "quicbaas.KV/Put" -> "qrdb_put"
//   - "quicbaas.KV/Get" -> "qrdb_get"
//   - "quicbaas.KV/Delete" -> "qrdb_delete"
//   - "quicbaas.Cluster/Status" -> "quicbaas_cluster_status"
//   - "quicbaas.Health/Check" -> "qrdb_health"
//   - "quicbaas.KMS/GenerateKey" -> "quicbaas_kms_generate_key"
//
// Usage:
//
//	cfg := &transport.Config{
//	    Address: "http://localhost:8888",
//	    Timeout: 30 * time.Second,
//	}
//
//	t, err := mcp.New(cfg,
//	    mcp.WithAuthToken("your-access-token"),
//	)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer t.Close()
//
//	// Use the transport with the SDK client
//	resp, err := t.Request(ctx, "quicbaas.KV", "Put", &PutRequest{...})
//
// MCP Protocol Flow:
//
//  1. On first request, the transport initializes the MCP session by calling
//     the "initialize" method to negotiate protocol version and capabilities.
//
//  2. Subsequent requests are sent as tool calls via the "tools/call" method,
//     with the tool name derived from the service/method combination.
//
//  3. Responses are unwrapped from the MCP content format (array of content items
//     with type "text" containing JSON-encoded results).
//
// Reference: https://modelcontextprotocol.io/
package mcp
