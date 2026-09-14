//go:build rest

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

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// THE GOLDEN DESCRIBES A BUILD, NOT A PRODUCT.
//
// Routes() is not one surface. `rag` is a build tag: handlers_rag.go
// contributes six /api/v1/rag routes through ragRouteSpecs() when it is set,
// and rag_stub.go contributes none when it is not. A single golden file
// therefore cannot be right for both builds — it is wrong for one of them by
// exactly those six routes, and the failure it produces reads as "the trunk is
// red" rather than "this test was compiled with the other feature set".
//
// That is what #299 was: the goldens had been regenerated under `rest rag` and
// so froze the rag-enabled surface only. RAG is a build-time OPTION (Makefile
// WITH_RAG, default 1), and `make build-standard` — "cluster, no AI features",
// WITH_VECTOR=0 WITH_RAG=0 WITH_MCP=0 with REST still on — ships exactly the
// rest-without-rag binary these goldens then described wrongly. Both
// configurations are built and shipped; only one of them was pinned.
//
// So the goldens are split the way the production code already splits: a base
// file holding the surface every `rest` build serves, plus a per-feature file
// contributed by the same tag that contributes the routes. Adding a
// tag-gated surface now means adding its golden fragment beside it, and the
// base file keeps describing the shipped build.
//
// readGoldenLines returns the expected lines for the build it was compiled
// into: base, plus whatever the enabled feature tags contribute.
func readGoldenLines(t *testing.T, base string) []string {
	t.Helper()

	var lines []string
	// A line may appear in exactly one file. Splitting one golden into several
	// creates a way for a route to be listed twice, and both consumers compare
	// SETS — the parity test expands into a map, and the permission test's size
	// check would be thrown off in the opposite direction by the same amount as
	// a missing route. A duplicate would therefore either pass silently or fail
	// as something it is not, so it is caught here where it is unambiguous.
	from := make(map[string]string, 700)
	for _, path := range append([]string{"testdata/" + base + ".golden"}, ragGoldenExtras(base)...) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" {
			continue
		}
		for _, line := range strings.Split(trimmed, "\n") {
			if line = strings.TrimSpace(line); line == "" {
				continue
			}
			if prev, dup := from[line]; dup {
				t.Fatalf("golden line duplicated in %s and %s: %s", prev, path, line)
			}
			from[line] = path
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	return lines
}
