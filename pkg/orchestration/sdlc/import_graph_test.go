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
	"errors"
	"go/build"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	modulePath          = "github.com/jeremyhahn/go-quicbaas"
	orchestrationImport = modulePath + "/pkg/orchestration"
	sdlcImport          = modulePath + "/pkg/orchestration/sdlc"
)

// TestImportGraph_OrchestrationNeverImportsSDLC pins the direction of the
// seam: the engine registers nothing about this module, this module plugs
// into the engine. It walks every in-module package the orchestration
// package reaches, transitively, and refuses the module among them.
// testModuleRoot is the repository root: this file sits in
// pkg/orchestration/sdlc, three directories below it.
func testModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

// TestPackageTree_SoftwareFactoryUnderOrchestration pins the one package tree
// of the Software Factory (#805): the SDLC module, the fleet daemon and the
// prompt sets live under pkg/orchestration, and the old sibling directories
// pkg/orchestration/sdlc and pkg/orchestration/fleet are gone.
func TestPackageTree_SoftwareFactoryUnderOrchestration(t *testing.T) {
	root := testModuleRoot(t)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root %s has no go.mod: %v", root, err)
	}
	// The old paths are spelled from parts so a path rewrite cannot move them.
	for _, gone := range []string{"pkg/" + "sdlc", "pkg/" + "fleet", "pkg/" + "agent"} {
		if _, err := os.Stat(filepath.Join(root, gone)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s still exists (err=%v): the Software Factory's packages live under pkg/orchestration", gone, err)
		}
	}
	ctx := build.Default
	for _, sub := range []string{"sdlc", "sdlc/gitsign", "sdlc/workerimage", "fleet", "fleet/deploy", "prompts"} {
		importPath := orchestrationImport + "/" + sub
		if _, err := ctx.Import(importPath, root, build.FindOnly); err != nil {
			t.Errorf("import %s: %v", importPath, err)
		}
	}
}

func TestImportGraph_OrchestrationNeverImportsSDLC(t *testing.T) {
	moduleRoot := testModuleRoot(t)
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
			t.Fatalf("pkg/orchestration reaches pkg/orchestration/sdlc: %s", strings.Join(append(chain, importPath), " -> "))
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
