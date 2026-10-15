// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package containerruntime

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostBinaries are the programs the package's tests must never launch or
// look up on the host PATH: the shells the moved tests once used as a
// stand-in runtime, the real OCI runtimes, and the image tools runtime.go
// shells out to. Tests use the fake OCI runtime (fakeOCIRuntime) instead.
var hostBinaries = map[string]bool{
	"sh":     true,
	"bash":   true,
	"crun":   true,
	"runc":   true,
	"podman": true,
	"skopeo": true,
}

// shellRuntimes never name a real OCI runtime: an OCIRuntime set to one is a
// stand-in chosen only so exec.LookPath finds it.
var shellRuntimes = map[string]bool{
	"sh":   true,
	"bash": true,
}

// launchCalls maps each function that starts or looks up a program to the
// index of its program argument.
var launchCalls = map[string]int{
	"exec.Command":        0,
	"exec.CommandContext": 1,
	"exec.LookPath":       0,
	"runOCIExec":          1,
	"runOCIExecTTY":       1,
	"defaultRunOCIExec":   1,
	"startPTYExec":        1,
	"defaultStartPTYExec": 1,
}

func TestTestsNeverNameHostBinaries(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	fset := token.NewFileSet()
	for _, name := range files {
		file, err := parser.ParseFile(fset, name, nil, 0)
		require.NoError(t, err)
		for _, use := range hostBinaryUses(fset, file) {
			t.Errorf("%s: use the fake OCI runtime (fakeOCIRuntime) instead", use)
		}
	}
}

func TestHostBinaryUses(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{"exec.Command shell", `func f() { exec.Command("sh", "-c", "exit 5") }`, 1},
		{"exec.CommandContext runtime", `func f() { exec.CommandContext(ctx, "crun", "state") }`, 1},
		{"exec.LookPath tool", `func f() { exec.LookPath("skopeo") }`, 1},
		{"exec seam runtime", `func f() { runOCIExecTTY(ctx, "bash", nil) }`, 1},
		{"shell as OCIRuntime", `func f() { _ = RuntimeConfig{OCIRuntime: "sh"} }`, 1},
		{"runtime looked up by NewRuntime", `func f() { NewRuntime(ctx, &RuntimeConfig{OCIRuntime: "runc"}) }`, 1},
		{"runtime as config value", `func f() { _ = PodmanConfig{OCIRuntime: "crun"} }`, 0},
		{"container command", `func f() { _ = Container{Command: []string{"sh"}} }`, 0},
		{"fake runtime", `func f() { exec.Command(fakeOCIRuntime(t, fakeOCIResult{}), "state") }`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "src.go", "package p\n"+tt.src, 0)
			require.NoError(t, err)
			assert.Len(t, hostBinaryUses(fset, file), tt.want)
		})
	}
}

// hostBinaryUses reports every place in file that launches or looks up a host
// binary, or configures one as the OCI runtime NewRuntime looks up.
func hostBinaryUses(fset *token.FileSet, file *ast.File) []string {
	var uses []string
	report := func(node ast.Node, name string) {
		uses = append(uses, fmt.Sprintf("%s names host binary %q", fset.Position(node.Pos()), name))
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		looksUpRuntime := callsFunction(fn.Body, "NewRuntime")

		ast.Inspect(fn.Body, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.CallExpr:
				index, ok := launchCalls[calleeName(n.Fun)]
				if !ok || index >= len(n.Args) {
					return true
				}
				if name, ok := stringLiteral(n.Args[index]); ok && hostBinaries[name] {
					report(n, name)
				}
			case *ast.KeyValueExpr:
				key, ok := n.Key.(*ast.Ident)
				if !ok || key.Name != "OCIRuntime" {
					return true
				}
				name, ok := stringLiteral(n.Value)
				if ok && (shellRuntimes[name] || (looksUpRuntime && hostBinaries[name])) {
					report(n, name)
				}
			}
			return true
		})
	}
	return uses
}

// callsFunction reports whether body calls the function named name.
func callsFunction(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && calleeName(call.Fun) == name {
			found = true
		}
		return !found
	})
	return found
}

// calleeName returns a call's function as written: "f" or "pkg.f".
func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if pkg, ok := f.X.(*ast.Ident); ok {
			return pkg.Name + "." + f.Sel.Name
		}
	}
	return ""
}

// stringLiteral returns the value of expr when it is a string literal.
func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}
