//go:build mcp && !rag

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

// featureGatedSDKCalls lists SDK verbs whose MCP tool is not registered in
// THIS BUILD because the feature behind it was compiled out.
//
// RAG is a build-time option (Makefile WITH_RAG). With it off, rag_mcp_stub.go
// and rag_index_tools_stub.go register nothing, so quicbaas.RAG.Query and
// quicbaas.RAG.BuildContext have no tool — correctly, and not as debt. The
// unconditional ledger in sdk_toolname_contract_test.go must NOT carry them:
// it records verbs that are unreachable in EVERY build, and putting a
// configuration-dependent gap there would make the contract stop failing when
// the rag build genuinely loses those tools.
//
// This gap was invisible until go-quicbaas#305, because `mcp && !rag` was a
// constraint no test pass satisfied: the maximal pass sets both tags and the
// minimal pass sets neither, so the contract test simply never ran in the one
// configuration where it fails. The lean pass added with that gate compiles it,
// and it failed on the first run.
var featureGatedSDKCalls = map[sdkCall]string{
	{"quicbaas.RAG", "Query"}:        "RAG is compiled out (WITH_RAG=0): rag_mcp_stub.go registers no tools",
	{"quicbaas.RAG", "BuildContext"}: "RAG is compiled out (WITH_RAG=0): rag_mcp_stub.go registers no tools",
}
