//go:build rest

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

package rest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestREST_NoCodecSurface pins SPEC row codec (#1647): the codec is
// go-qrdb's and has no API surface of its own, so the REST server carries
// no codec handler (HandleGetOptimized) and no codec negotiation helper.
// REST bodies are JSON; the messaging channel codec is a messaging field.
func TestREST_NoCodecSurface(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Name.Name == "HandleGetOptimized" || fn.Name.Name == "setContentType" || strings.Contains(fn.Name.Name, "Codec") {
				t.Errorf("%s declares %s; the codec is go-qrdb's and has no REST surface", name, fn.Name.Name)
			}
		}
	}
}
