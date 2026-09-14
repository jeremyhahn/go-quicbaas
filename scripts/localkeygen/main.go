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

// Command localkeygen reports every site in this tree that creates an
// asymmetric key pair or signs an X.509 certificate itself, instead of asking
// go-xkms to do it.
//
// WHY THIS IS A GATE AND NOT A CONVENTION. go-xkms owns keys and certificates
// for the whole stack; go-quicbaas composes it (repo-root CLAUDE.md, Layer
// Ownership). A key generated in this process cannot be protected by the TPM or
// the HSM that is the entire point of go-xkms, and a certificate signed here
// bypasses the CA's profiles, serial policy, SPIFFE synthesis and certificate
// store. go-quicbaas#910 found six local ecdsa/rsa/ed25519.GenerateKey calls
// across three packages, one of them in an adapter whose own sibling file
// documents go-xkms as the canonical issuer. Nothing prevented them, and the
// repeat is what this command exists to prevent.
//
// WHAT IS REPORTED. Calls to ecdsa.GenerateKey, rsa.GenerateKey,
// ed25519.GenerateKey, x509.CreateCertificate and x509.CreateCertificateRequest.
// Resolution is on the call's selector text, so `rsa.GenerateKey` is a finding
// whatever the import alias resolves to; a local helper that happens to be
// named GenerateKey on some other package is not, because the receiver must be
// one of the named crypto packages.
//
// WHAT TO DO WITH A FINDING. In order of preference:
//
//   - Platform key material: ask go-xkms. Its keystore generates inside the
//     backend (TPM 2.0, PKCS#11, PKCS#8) and hands back an opaque
//     crypto.Signer. Nothing needs to be created here.
//   - A certificate: hand a CSR to the go-xkms CA (TrustedCA.SignCSR). That is
//     the stack's single x509.CreateCertificate call.
//   - A credential the REQUESTER will hold, which go-xkms deliberately cannot
//     export: call pkg/xkms.GenerateRequesterKey, the one audited generator,
//     which draws from the go-xkms FIPS RNG resolver. Read pkg/xkms/keygen.go
//     before deciding a new site belongs in this category.
//
// A site that truly has no alternative waives itself in place with a
// `// local-keygen-ok: <reason>` comment, so the justification lives next to
// the code rather than in a list somewhere else. An empty reason is not a
// waiver. This is the same mechanism check-coded-sentinels uses.
//
// SCOPE. pkg, cmd, sdk AND test. The test tree used to be excluded outright,
// on the stated grounds that a fixture minting a throwaway self-signed
// certificate is not a production key-custody decision. That is true of a
// three-line inline fixture and was not true of what actually grew in the blind
// spot: go-quicbaas#943 found a 444-line parallel certificate authority under
// test/integration that the ENTIRE integration suite depended on, so the suite
// proved the system worked against certificates no part of the product issues
// and every divergence from the real issuance path was invisible to every test.
// The fixtures now come from the product's own CA
// (test/integration/certfixture) and the tree is in scope.
//
// Non-test files only, uniformly across every tree: _test.go is excluded because
// a unit test's self-signed throwaway chains to nothing and decides no key
// custody. -include-tests reports them anyway for an audit. A tree or file that
// genuinely must be exempt is named explicitly with -skip, so the exemption is a
// listed decision rather than a whole tree nobody looks at.
//
// RESOLUTION is syntactic, deliberately: it needs no build tags, no module
// loading and no dependency, so it runs on every tree state including one that
// does not compile.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// waiverPrefix marks a call site that must create key or certificate material
// locally. The text after it is the reason and must not be empty.
const waiverPrefix = "local-keygen-ok:"

// forbidden maps a crypto package name to the functions in it that create key
// or certificate material. A map keyed by package then function so a lookup is
// two constant-time steps and the forbidden set is the data, not the code.
var forbidden = map[string]map[string]string{
	"ecdsa": {
		"GenerateKey": "creates an ECDSA key pair locally",
	},
	"rsa": {
		"GenerateKey": "creates an RSA key pair locally",
	},
	"ed25519": {
		"GenerateKey": "creates an Ed25519 key pair locally",
	},
	"x509": {
		"CreateCertificate":        "signs a certificate locally, bypassing the go-xkms CA",
		"CreateCertificateRequest": "builds a CSR locally instead of using go-xkms CreateCSRWithKey",
	},
}

// finding is one forbidden call site.
type finding struct {
	pos    token.Position
	call   string
	reason string
}

// Exit codes. A finding and a usage failure are distinguished so a caller can
// tell "the tree has a violation" from "this tool could not run".
const (
	exitClean     = 0
	exitFindings  = 1
	exitToolError = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run scans the requested directories and reports findings to stdout and the
// remediation guidance to stderr. It returns the process exit code, so the whole
// command is exercisable from a test without spawning a process.
//
// Streams are parameters rather than os.Stdout/os.Stderr directly because the
// report IS this command's product: a test that cannot read it cannot assert the
// gate says anything useful to the engineer who trips it.
func run(args []string, stdout, stderr io.Writer) int {
	var dirs stringList
	var skips stringList
	fs := flag.NewFlagSet("localkeygen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Var(&dirs, "dir", "directory to scan (repeatable)")
	fs.Var(&skips, "skip", "path prefix to exclude (repeatable)")
	includeTests := fs.Bool("include-tests", false, "report _test.go files too")
	if err := fs.Parse(args); err != nil {
		return exitToolError
	}

	if len(dirs) == 0 {
		dirs = defaultDirs()
	}

	files, err := collect(dirs, skips, *includeTests)
	if err != nil {
		// forbidigo forbids fmt.Print*; this tool is a CLI whose output IS its
		// product, so it writes to the stream directly rather than suppressing
		// the rule.
		fmt.Fprintln(stderr, "localkeygen:", err)
		return exitToolError
	}

	findings := scanFiles(files)
	if len(findings) == 0 {
		return exitClean
	}
	for _, fd := range findings {
		fmt.Fprintf(stdout, "%s:%d:%d: %s %s\n",
			fd.pos.Filename, fd.pos.Line, fd.pos.Column, fd.call, fd.reason)
	}
	fmt.Fprintf(stderr,
		"\nlocalkeygen: %d site(s). go-xkms owns keys and certificates; go-quicbaas composes it.\n"+
			"Platform key material: ask the go-xkms keystore. A certificate: hand a CSR to\n"+
			"TrustedCA.SignCSR. A credential the requester holds and go-xkms cannot export:\n"+
			"pkg/xkms.GenerateRequesterKey, the one audited generator. If a site genuinely has\n"+
			"no alternative, say why on the line above it:\n\n\t// %s <why nothing else can work here>\n\n",
		len(findings), waiverPrefix)
	return exitFindings
}

// defaultDirs is the scanned set when a caller names none. test is in it
// deliberately -- see SCOPE in the package comment; excluding it is what let a
// parallel certificate authority grow there unnoticed.
func defaultDirs() stringList {
	return stringList{"pkg", "cmd", "sdk", "test"}
}

// scanFiles parses each path and returns every finding across all of them, in a
// stable file-then-offset order so the report does not reshuffle between runs.
func scanFiles(files []string) []finding {
	fset := token.NewFileSet()
	var findings []finding
	for _, path := range files {
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if perr != nil {
			// A file this tool cannot parse is not a finding; the compiler
			// reports it better than this would.
			continue
		}
		findings = append(findings, scanFile(fset, f)...)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].pos.Filename != findings[j].pos.Filename {
			return findings[i].pos.Filename < findings[j].pos.Filename
		}
		return findings[i].pos.Offset < findings[j].pos.Offset
	})
	return findings
}

// collect returns every .go file under the given directories, minus the skipped
// prefixes.
func collect(dirs, skips []string, includeTests bool) ([]string, error) {
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
				if skipped(path, skips) {
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
			if skipped(path, skips) {
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

// skipped reports whether path lies under one of the excluded prefixes.
// Comparison is on slash-separated paths so a -skip value reads the same on
// every platform.
func skipped(path string, skips []string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	for _, s := range skips {
		prefix := filepath.ToSlash(filepath.Clean(s))
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return true
		}
	}
	return false
}

// scanFile reports the forbidden calls in f that carry no waiver.
func scanFile(fset *token.FileSet, f *ast.File) []finding {
	waived := waivedLines(fset, f)

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
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		reason, ok := forbidden[pkg.Name][sel.Sel.Name]
		if !ok {
			return true
		}
		pos := fset.Position(call.Pos())
		if waived[pos.Line] {
			return true
		}
		out = append(out, finding{
			pos:    pos,
			call:   pkg.Name + "." + sel.Sel.Name,
			reason: reason,
		})
		return true
	})
	return out
}

// waivedLines returns the source lines a waiver comment applies to: the line
// the comment is on, and the six lines after it — enough for a multi-line call
// without letting one waiver silence a whole function.
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
