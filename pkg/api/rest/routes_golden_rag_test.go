//go:build rest && rag

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

// ragGoldenExtras contributes the RAG fragment of a golden surface, mirroring
// the routes handlers_rag.go contributes through ragRouteSpecs(). The tag that
// adds the routes adds their golden lines, so the two cannot drift apart.
func ragGoldenExtras(base string) []string {
	return []string{"testdata/" + base + "_rag.golden"}
}
