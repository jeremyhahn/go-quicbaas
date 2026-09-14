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

// Command codedsentinel reports every errors.Is call in this tree whose TARGET
// is a qrdb-coded sentinel, because such a call does not mean what it reads.
//
// THE MECHANISM. pkg/errors.QBError is a type ALIAS for go-qrdb's
// errors.QRDBError, and QRDBError.Is compares the error CODE:
//
//	func (e *QRDBError) Is(target error) bool {
//		var de *QRDBError
//		if errors.As(target, &de) {
//			return e.Code == de.Code
//		}
//		...
//	}
//
// So errors.Is(err, iam.ErrInviteExpired) is true for EVERY error carrying the
// Forbidden code -- a spent invite, a rejected PIV certificate, a suspended
// tenant, a forbidden bubbling up from the store. The call site reads like
// idiomatic sentinel matching and is not. It over-matches, so it takes a branch
// written for a narrower case, and the failure is silent: go-quicbaas#434 found
// a 422 branch that could never run, an authentication route that described why
// a proof failed, and a token-liveness rule that admitted a token because the
// store answered not-found.
//
// WHAT TO DO WITH A FINDING. Two answers, and the point of this tool is to
// force the choice to be made rather than assumed:
//
//   - The site means the CODE -- a REST bridge answering 404 for any not-found,
//     a loop skipping any absent record. Say so with a `coded-sentinel-ok:`
//     comment carrying the reason. A bare code check (dberrors.IsNotFound, or
//     errors.As on *QBError plus a Code comparison) is clearer still.
//   - The site means the CONDITION. Give the sentinel a dedicated typed inner
//     error and match it with errors.As, as pkg/iam/errors_conditions.go and
//     pkg/service/fido2_service.go do. Write the test POSITIVELY, so a failure
//     mode added later lands in the caller's default bucket instead of silently
//     joining the matched set.
//
// SCOPE. Non-test files only. A test asserting ErrorIs against a coded sentinel
// is a weaker assertion than it looks, but it is an assertion, not a branch: it
// cannot mis-route a request or admit a token. Branches are what this guards.
// -include-tests reports them anyway for an audit.
//
// RESOLUTION is syntactic, deliberately: it needs no build tags, no module
// loading and no dependency, so it runs on every tree state including one that
// does not compile. A sentinel is a package-level var whose initializer is a
// composite literal of a type NAMED QBError or QRDBError, or a var assigned
// from another such var. Targets are matched as `Name` (same package) or
// `pkg.Name`, keyed by the package CLAUSE name -- so two packages with the same
// clause name share a namespace here. That errs toward reporting, which for a
// guard with an inline waiver is the safe direction.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// waiverPrefix marks a call site that means the error code on purpose. The text
// after it is the reason and must not be empty.
const waiverPrefix = "coded-sentinel-ok:"

// sentinelKey identifies a package-level sentinel var by its package clause
// name and its own name.
type sentinelKey struct {
	pkg  string
	name string
}

// finding is one errors.Is call against a coded sentinel.
type finding struct {
	pos    token.Position
	target string
}

func main() {
	var dirs stringList
	flag.Var(&dirs, "dir", "directory to scan (repeatable)")
	includeTests := flag.Bool("include-tests", false, "report _test.go files too")
	listSentinels := flag.Bool("list-sentinels", false, "print the resolved sentinel set and exit")
	flag.Parse()

	if len(dirs) == 0 {
		dirs = stringList{"pkg", "cmd", "sdk"}
	}

	files, err := collect(dirs, *includeTests)
	if err != nil {
		fmt.Fprintln(os.Stderr, "codedsentinel:", err)
		os.Exit(2)
	}

	fset := token.NewFileSet()
	parsed := make(map[string]*ast.File, len(files))
	for _, path := range files {
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			// A file this tool cannot parse is not a finding; the compiler
			// reports it better than this would.
			continue
		}
		parsed[path] = f
	}

	sentinels := resolveSentinels(parsed)
	if *listSentinels {
		keys := make([]string, 0, len(sentinels))
		for k := range sentinels {
			keys = append(keys, k.pkg+"."+k.name)
		}
		sort.Strings(keys)
		for _, k := range keys {
			// forbidigo forbids fmt.Print*; this tool is a CLI whose output IS its
			// product, so it writes to the stream directly rather than suppressing
			// the rule.
			fmt.Fprintln(os.Stdout, k)
		}
		return
	}

	var findings []finding
	for path, f := range parsed {
		findings = append(findings, scanFile(fset, path, f, sentinels)...)
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].pos.Filename != findings[j].pos.Filename {
			return findings[i].pos.Filename < findings[j].pos.Filename
		}
		return findings[i].pos.Offset < findings[j].pos.Offset
	})

	if len(findings) == 0 {
		return
	}
	for _, fd := range findings {
		fmt.Fprintf(os.Stdout, "%s:%d:%d: errors.Is against the qrdb-coded sentinel %s matches its CODE, not the sentinel\n",
			fd.pos.Filename, fd.pos.Line, fd.pos.Column, fd.target)
	}
	fmt.Fprintf(os.Stderr,
		"\ncodedsentinel: %d call site(s). Either narrow the site with a typed error and errors.As\n"+
			"(see pkg/iam/errors_conditions.go), or, if the code really is what the site means,\n"+
			"say so on the line above it:\n\n\t// %s <why the whole code class is the intent>\n\n",
		len(findings), waiverPrefix)
	os.Exit(1)
}

// collect returns every .go file under the given directories.
func collect(dirs []string, includeTests bool) ([]string, error) {
	var out []string
	for _, dir := range dirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "vendor" || d.Name() == "testdata" || d.Name() == "node_modules" {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if !includeTests && strings.HasSuffix(path, "_test.go") {
				return nil
			}
			out = append(out, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

// resolveSentinels finds every package-level var that holds a QBError/QRDBError,
// including vars assigned from another such var.
func resolveSentinels(files map[string]*ast.File) map[sentinelKey]bool {
	direct := map[sentinelKey]bool{}
	alias := map[sentinelKey]sentinelKey{}

	for _, f := range files {
		pkg := f.Name.Name
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					key := sentinelKey{pkg: pkg, name: name.Name}
					if isCodedErrorLiteral(vs.Values[i]) {
						direct[key] = true
						continue
					}
					if ref, ok := varReference(pkg, vs.Values[i]); ok {
						alias[key] = ref
					}
				}
			}
		}
	}

	for changed := true; changed; {
		changed = false
		for from, to := range alias {
			if !direct[from] && direct[to] {
				direct[from] = true
				changed = true
			}
		}
	}
	return direct
}

// isCodedErrorLiteral reports whether e is `&QBError{...}` / `&QRDBError{...}`,
// qualified or not.
func isCodedErrorLiteral(e ast.Expr) bool {
	unary, ok := e.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return false
	}
	lit, ok := unary.X.(*ast.CompositeLit)
	if !ok {
		return false
	}
	switch t := lit.Type.(type) {
	case *ast.Ident:
		return isCodedErrorTypeName(t.Name)
	case *ast.SelectorExpr:
		return isCodedErrorTypeName(t.Sel.Name)
	}
	return false
}

func isCodedErrorTypeName(name string) bool {
	return name == "QBError" || name == "QRDBError"
}

// varReference reads `X` or `pkg.X` as a sentinelKey.
func varReference(localPkg string, e ast.Expr) (sentinelKey, bool) {
	switch v := e.(type) {
	case *ast.Ident:
		if v.Obj == nil && !isExportedOrLocal(v.Name) {
			return sentinelKey{}, false
		}
		return sentinelKey{pkg: localPkg, name: v.Name}, true
	case *ast.SelectorExpr:
		if x, ok := v.X.(*ast.Ident); ok {
			return sentinelKey{pkg: x.Name, name: v.Sel.Name}, true
		}
	}
	return sentinelKey{}, false
}

// isExportedOrLocal keeps varReference from treating a keyword-ish identifier
// (nil, true) as a var name.
func isExportedOrLocal(name string) bool {
	switch name {
	case "nil", "true", "false", "iota":
		return false
	}
	return true
}

// scanFile reports the errors.Is calls in f whose target is a coded sentinel
// and that carry no waiver.
func scanFile(fset *token.FileSet, path string, f *ast.File, sentinels map[sentinelKey]bool) []finding {
	waived := waivedLines(fset, f)
	pkg := f.Name.Name

	var out []finding
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		var argIdx int
		switch sel.Sel.Name {
		case "Is":
			argIdx = 1
		case "ErrorIs", "NotErrorIs":
			// testify: (t, err, target)
			argIdx = 2
		default:
			return true
		}
		if len(call.Args) <= argIdx {
			return true
		}
		target := call.Args[argIdx]
		key, ok := varReference(pkg, target)
		if !ok || !sentinels[key] {
			return true
		}
		pos := fset.Position(call.Pos())
		if waived[pos.Line] {
			return true
		}
		out = append(out, finding{pos: pos, target: key.pkg + "." + key.name})
		return true
	})
	return out
}

// waivedLines returns the source lines a waiver comment applies to: the line
// the comment is on, and the lines of the statement it precedes. A waiver
// covers the comment's own line and the six lines after it, which is enough for
// a multi-line switch case without letting one waiver silence a whole function.
func waivedLines(fset *token.FileSet, f *ast.File) map[int]bool {
	const reach = 6
	waived := map[int]bool{}
	for _, group := range f.Comments {
		for _, c := range group.List {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*"))
			idx := strings.Index(text, waiverPrefix)
			if idx < 0 {
				continue
			}
			if strings.TrimSpace(text[idx+len(waiverPrefix):]) == "" {
				// A waiver with no reason is not a waiver.
				continue
			}
			line := fset.Position(c.Pos()).Line
			for i := 0; i <= reach; i++ {
				waived[line+i] = true
			}
		}
	}
	return waived
}

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}
