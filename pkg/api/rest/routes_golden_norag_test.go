//go:build rest && !rag

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

package rest

// ragGoldenExtras contributes no golden lines when RAG is compiled out,
// matching rag_stub.go's empty ragRouteSpecs(). This is the `make
// build-standard` configuration (WITH_RAG=0, REST on), so the base golden
// alone is the surface that binary serves.
func ragGoldenExtras(string) []string {
	return nil
}
