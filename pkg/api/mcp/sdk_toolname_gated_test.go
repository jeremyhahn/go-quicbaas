//go:build mcp && rag

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

package mcp

// featureGatedSDKCalls is EMPTY when every optional feature the MCP server
// exposes is compiled in: with `rag` set, the RAG tools are registered and the
// contract holds without an exemption.
//
// The empty map rather than no map at all: the contract test reads this in
// every `mcp` build, so the variable has to exist in both. Its counterpart is
// sdk_toolname_gated_rag_test.go.
var featureGatedSDKCalls = map[sdkCall]string{}
