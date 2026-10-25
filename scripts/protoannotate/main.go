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

// Command protoannotate rewrites the api/proto .proto files so every RPC that
// the gRPC PEP table (pkg/api/grpc/authz_mapping.go grpcMethodPermissions)
// maps to a qrdb permission carries the matching
// option (qrdb.v1.permission) = "resource:action" method option. The
// annotation is what qrdb's unified-server reflection interceptor enforces
// (Phase 6 of plans/unified-api-server-2026-08-14.md); the Go table stays the
// authoritative source until a later wave deletes it, so this tool is kept
// under scripts/ to re-run whenever the table changes.
//
// Methods absent from the table are left unannotated on purpose: public
// methods (publicGRPCMethods) mirror qrdb's own posture where Health-style
// RPCs carry no option, and unmapped methods stay DEFAULT-DENY on the unified
// server exactly as they are under the legacy interceptor. Table entries whose
// method does not exist in any .proto file (defensive aliases) are reported
// and skipped.
//
// Usage (from the repository root):
//
//	go run ./scripts/protoannotate
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	mappingFile = "pkg/api/grpc/authz_mapping.go"
	mappingVar  = "grpcMethodPermissions"
	protoDir    = "api/proto"
	authzImport = `import "qrdb/v1/permission.proto";`
)

// ParseError reports a failure to extract the permission table from the
// authz mapping source file.
type ParseError struct {
	Path   string
	Detail string
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	return "protoannotate: parse " + e.Path + ": " + e.Detail
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	perms, err := loadPermissionTable(mappingFile)
	if err != nil {
		return err
	}

	files, err := filepath.Glob(filepath.Join(protoDir, "*.proto"))
	if err != nil {
		return err
	}
	sort.Strings(files)

	annotated := make(map[string]bool, len(perms))
	total := 0
	for _, file := range files {
		n, annErr := annotateFile(file, perms, annotated)
		if annErr != nil {
			return annErr
		}
		if n > 0 {
			fmt.Fprintf(os.Stdout, "%s: %d RPCs annotated\n", file, n)
			total += n
		}
	}

	// Report table entries that matched no RPC on the wire (defensive
	// aliases and qrdb.v1 composition methods, which live in qrdb's protos).
	var unmatched []string
	for method := range perms {
		if !annotated[method] {
			unmatched = append(unmatched, method)
		}
	}
	sort.Strings(unmatched)
	fmt.Fprintf(os.Stdout, "annotated %d RPCs from %d table entries\n", total, len(perms))
	for _, method := range unmatched {
		fmt.Fprintf(os.Stdout, "no proto RPC for table entry: %s\n", method)
	}
	return nil
}

// loadPermissionTable parses the authz mapping source and returns the
// grpcMethodPermissions table as fullMethod -> "resource:action".
func loadPermissionTable(path string) (map[string]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	var lit *ast.CompositeLit
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != mappingVar {
			return true
		}
		if len(spec.Values) == 1 {
			if cl, clOK := spec.Values[0].(*ast.CompositeLit); clOK {
				lit = cl
			}
		}
		return false
	})
	if lit == nil {
		return nil, &ParseError{Path: path, Detail: mappingVar + " composite literal not found"}
	}

	perms := make(map[string]string, len(lit.Elts))
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, &ParseError{Path: path, Detail: "unexpected non key-value element"}
		}
		key, err := stringLit(kv.Key)
		if err != nil {
			return nil, &ParseError{Path: path, Detail: err.Error()}
		}
		val, ok := kv.Value.(*ast.CompositeLit)
		if !ok || len(val.Elts) != 2 {
			return nil, &ParseError{Path: path, Detail: "entry " + key + ": value is not a {resource, action} literal"}
		}
		resource, err := stringLit(val.Elts[0])
		if err != nil {
			return nil, &ParseError{Path: path, Detail: err.Error()}
		}
		action, err := stringLit(val.Elts[1])
		if err != nil {
			return nil, &ParseError{Path: path, Detail: err.Error()}
		}
		perms[key] = resource + ":" + action
	}
	return perms, nil
}

// stringLit evaluates a string basic literal expression.
func stringLit(e ast.Expr) (string, error) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", &ParseError{Path: mappingFile, Detail: "expected string literal"}
	}
	return strconv.Unquote(bl.Value)
}

var (
	packageRe = regexp.MustCompile(`^package\s+([\w.]+)\s*;`)
	serviceRe = regexp.MustCompile(`^service\s+(\w+)\s*\{`)
	rpcRe     = regexp.MustCompile(`^(\s*)(rpc\s+(\w+)\s*\(.*\)\s*returns\s*\(.*\))\s*;\s*$`)
	importRe  = regexp.MustCompile(`^import\s+"`)
	optionRe  = regexp.MustCompile(`^option\s+`)
	declRe    = regexp.MustCompile(`^(service|message|enum)\s`)
)

// annotateFile rewrites one .proto file in place, expanding every mapped
// single-line rpc into a block carrying the permission option, and adds the
// qrdb permission.proto import when at least one RPC was annotated. Already
// annotated files (import present) are left untouched so the tool is
// idempotent. It returns the number of RPCs annotated.
func annotateFile(path string, perms map[string]string, annotated map[string]bool) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	content := string(data)
	if strings.Contains(content, "qrdb/v1/permission.proto") {
		// Record which of this file's RPCs the table covers so the unmatched
		// report stays accurate on re-runs, then leave the file untouched.
		markAnnotated(content, perms, annotated)
		return 0, nil
	}

	lines := strings.Split(content, "\n")
	var out []string
	var pkg, service string
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := packageRe.FindStringSubmatch(trimmed); m != nil {
			pkg = m[1]
		}
		if m := serviceRe.FindStringSubmatch(trimmed); m != nil {
			service = m[1]
		}
		if trimmed == "}" {
			service = ""
		}
		if service != "" && pkg != "" {
			if m := rpcRe.FindStringSubmatch(line); m != nil {
				indent, decl, name := m[1], m[2], m[3]
				full := "/" + pkg + "." + service + "/" + name
				if perm, ok := perms[full]; ok {
					out = append(out,
						indent+decl+" {",
						indent+"    option (qrdb.v1.permission) = "+strconv.Quote(perm)+";",
						indent+"}")
					annotated[full] = true
					count++
					continue
				}
			}
		}
		out = append(out, line)
	}

	if count == 0 {
		return 0, nil
	}

	result := insertImport(out)
	// Rewriting a .proto source file in place with annotations -- world-
	// readable source code meant to be committed, not sensitive data.
	return count, os.WriteFile(path, []byte(strings.Join(result, "\n")), 0o644) // #nosec G306 -- generated/annotated source file, not sensitive data
}

// markAnnotated records the table entries an already-annotated file carries so
// re-runs report accurate unmatched counts.
func markAnnotated(content string, perms map[string]string, annotated map[string]bool) {
	var pkg, service string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := packageRe.FindStringSubmatch(trimmed); m != nil {
			pkg = m[1]
		}
		if m := serviceRe.FindStringSubmatch(trimmed); m != nil {
			service = m[1]
		}
		if trimmed == "}" && !strings.Contains(trimmed, "rpc") {
			// Only reset on a column-zero closer; rpc blocks close indented.
			if strings.HasPrefix(line, "}") {
				service = ""
			}
		}
		if service == "" || pkg == "" {
			continue
		}
		if m := rpcRe.FindStringSubmatch(strings.TrimSuffix(line, " {")); m != nil {
			full := "/" + pkg + "." + service + "/" + m[3]
			if _, ok := perms[full]; ok {
				annotated[full] = true
			}
		}
	}
}

// insertImport places the qrdb authz import after the last top-level import
// in the file header, or after the top-level option block (go_package etc.)
// when the file imports nothing. Only column-zero lines before the first
// declaration are considered so indented method options never attract the
// import into a service body.
func insertImport(lines []string) []string {
	boundary := len(lines)
	for i, line := range lines {
		if declRe.MatchString(line) {
			boundary = i
			break
		}
	}
	lastImport := -1
	lastOption := -1
	for i := 0; i < boundary; i++ {
		if importRe.MatchString(lines[i]) {
			lastImport = i
		}
		if optionRe.MatchString(lines[i]) && lastImport == -1 {
			lastOption = i
		}
	}
	insertAt := lastImport
	if insertAt == -1 {
		insertAt = lastOption
	}
	if insertAt == -1 {
		// Fall back to after the package declaration.
		for i := 0; i < boundary; i++ {
			if packageRe.MatchString(strings.TrimSpace(lines[i])) {
				insertAt = i
				break
			}
		}
	}
	result := make([]string, 0, len(lines)+2)
	result = append(result, lines[:insertAt+1]...)
	if lastImport == -1 {
		result = append(result, "", authzImport)
	} else {
		result = append(result, authzImport)
	}
	result = append(result, lines[insertAt+1:]...)
	return result
}
