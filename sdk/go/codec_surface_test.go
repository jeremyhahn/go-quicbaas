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

package quicbaas

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestSDK_NoCodecSurface pins SPEC row codec (#1647): the codec is
// go-qrdb's and has no API surface of its own, so the SDK exports no
// Codec* identifier and ships no client/codec package. The messaging
// channel codec (MessagingCodec*) is a messaging field, not a codec API.
func TestSDK_NoCodecSurface(t *testing.T) {
	if _, err := os.Stat("client/codec"); err == nil {
		t.Error("sdk/go/client/codec exists; the codec has no SDK client")
	}

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
			for _, ident := range declNames(decl) {
				if strings.HasPrefix(ident, "Codec") {
					t.Errorf("%s exports %s; the codec is go-qrdb's and has no SDK surface", name, ident)
				}
			}
		}
	}
}

// declNames returns the top-level names a declaration introduces.
func declNames(decl ast.Decl) []string {
	var names []string
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv == nil {
			names = append(names, d.Name.Name)
		}
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				names = append(names, s.Name.Name)
			case *ast.ValueSpec:
				for _, n := range s.Names {
					names = append(names, n.Name)
				}
			}
		}
	}
	return names
}
