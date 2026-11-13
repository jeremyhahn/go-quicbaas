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

package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestConfig_NoCodecBlock pins SPEC row codec (#1647): the codec is
// go-qrdb's, so go-quicbaas carries no `codec:` configuration block and no
// CodecConfig type of its own.
func TestConfig_NoCodecBlock(t *testing.T) {
	if _, ok := reflect.TypeOf(Config{}).FieldByName("Codec"); ok {
		t.Error("config.Config has a Codec field; the codec is go-qrdb's and has no quicbaas configuration")
	}
	for i, f := 0, reflect.TypeOf(Config{}); i < f.NumField(); i++ {
		if tag := f.Field(i).Tag.Get("mapstructure"); tag == "codec" {
			t.Errorf("config.Config field %s reads a `codec:` block", f.Field(i).Name)
		}
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
		ast.Inspect(file, func(n ast.Node) bool {
			if ts, ok := n.(*ast.TypeSpec); ok && strings.HasPrefix(ts.Name.Name, "Codec") {
				t.Errorf("%s declares type %s; the codec is go-qrdb's", name, ts.Name.Name)
			}
			return true
		})
	}
}
