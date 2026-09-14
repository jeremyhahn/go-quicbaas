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

package sdlc

import (
	"go/build"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	modulePath          = "github.com/jeremyhahn/go-quicbaas"
	orchestrationImport = modulePath + "/pkg/orchestration"
	sdlcImport          = modulePath + "/pkg/sdlc"
)

// TestImportGraph_OrchestrationNeverImportsSDLC pins the direction of the
// seam: the engine registers nothing about this module, this module plugs
// into the engine. It walks every in-module package the orchestration
// package reaches, transitively, and refuses the module among them.
func TestImportGraph_OrchestrationNeverImportsSDLC(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	ctx := build.Default
	ctx.BuildTags = []string{"rest", "mcp", "quic", "raft", "rag", "vector", "timeseries"}

	seen := map[string]bool{}
	var walk func(importPath string, chain []string)
	walk = func(importPath string, chain []string) {
		if seen[importPath] {
			return
		}
		seen[importPath] = true
		if importPath == sdlcImport {
			t.Fatalf("pkg/orchestration reaches pkg/sdlc: %s", strings.Join(append(chain, importPath), " -> "))
		}
		pkg, err := ctx.Import(importPath, moduleRoot, 0)
		if err != nil {
			t.Fatalf("import %s: %v", importPath, err)
		}
		for _, dep := range pkg.Imports {
			if strings.HasPrefix(dep, modulePath+"/") {
				walk(dep, append(chain, importPath))
			}
		}
	}
	walk(orchestrationImport, nil)
	if !seen[orchestrationImport] {
		t.Fatal("the walk never visited pkg/orchestration")
	}
}
