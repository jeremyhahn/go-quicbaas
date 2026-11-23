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

package codec

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestCodec_NoImporters pins SPEC row codec (#1647): the codec go-quicbaas
// runs on is go-qrdb's, and this package is a parallel implementation that
// nothing imports. It stays only until the owner rules it removed; no
// production file may start depending on it meanwhile.
func TestCodec_NoImporters(t *testing.T) {
	const self = "github.com/jeremyhahn/go-quicbaas/pkg/codec"
	root := filepath.Join("..", "..")
	selfDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata":
				return filepath.SkipDir
			}
			if abs, _ := filepath.Abs(path); abs == selfDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil // not this guard's concern
		}
		for _, imp := range file.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == self || strings.HasPrefix(p, self+"/") {
				t.Errorf("%s imports %s; the codec is go-qrdb's and pkg/codec has no importers", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
