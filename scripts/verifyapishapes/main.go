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

// Command verifyapishapes verifies that the QuicBaaS TypeScript
// Barrier*/Ceremony*/Bootstrap* type names match the go-qrdb canonical Go
// counterparts.
//
// This is a name-set check, not a structural one: structural drift on
// field-by-field types is caught by the SDK's TypeScript compiler when the
// frontend imports the SDK. The name-set check catches "renamed or added
// upstream but downstream still uses the old name" drift.
//
// GO-QRDB IS RESOLVED FROM THE MODULE GRAPH, NOT FROM A PATH. The shell gate
// this replaces defaulted its two Go inputs to an absolute NFS path on one
// developer's machine and exited 0 when that path was absent, so on every host
// that is not that one
// lab machine — which is every host, including CI — it had never compared
// anything and reported a pass (go-quicbaas#942). go-qrdb's SDK is a module this
// repository requires, so `go list -m` knows exactly where it is wherever the
// build works, and there is nothing left to default. A module that cannot be
// resolved is a typed error naming what is missing; it is never a skip.
//
// EXIT: 0 every Go type is mirrored · 1 one is not · 2 the gate could not run.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// qrdbSDKModule is the module that owns the canonical barrier, ceremony and
// bootstrap transport types.
const qrdbSDKModule = "github.com/jeremyhahn/go-qrdb/sdk/go"

// qrdbTypeFiles are the files within that module the canonical names live in,
// relative to the module root.
var qrdbTypeFiles = []string{
	"transport/barrier_types.go",
	"transport/ceremony_types.go",
}

// defaultTSTypes is the TypeScript surface under test, relative to the
// repository root.
const defaultTSTypes = "sdk/typescript/src/types.ts"

var (
	goTypeDecl = regexp.MustCompile(`(?m)^type (Barrier[A-Za-z]+|Ceremony[A-Za-z]+|Bootstrap[A-Za-z]+|EnrollExternalOfficer[A-Za-z]+) `)
	tsTypeDecl = regexp.MustCompile(`(?m)^export (?:interface|type) (Barrier[A-Za-z]+|Ceremony[A-Za-z]+|Bootstrap[A-Za-z]+)`)
)

// namingDrift normalizes the naming-convention differences between Go and
// TypeScript that are conventions rather than drift. The Go upstream uses the
// verbose `BarrierInitializeShamir*` while the TS SDK prefers `BarrierShamir*`,
// and the ceremony officer is a bare `CeremonyOfficer` downstream. Both sides are
// collapsed to the same comparable form.
var namingDrift = []struct {
	from string
	to   string
}{
	{from: "BarrierInitializeShamir", to: "BarrierShamir"},
	{from: "CeremonyOfficerRequest", to: "CeremonyOfficer"},
}

// goPrefixRewrites make a Go name comparable before normalization. go-qrdb
// declares the external-officer enrollment types without the Ceremony prefix the
// TS SDK gives them.
var goPrefixRewrites = []struct {
	from string
	to   string
}{
	{from: "EnrollExternalOfficer", to: "CeremonyEnrollExternalOfficer"},
}

// knownUnmirrored are canonical Go names with no TypeScript counterpart that are
// NOT drift, plus the ones that are drift and are tracked rather than silenced.
//
// EVERY ENTRY IS A NAME NOBODY IS CHECKING, so every entry carries a reason.
// The first group are upstream concepts the TS SDK represents inline through
// BarrierStatusResponse and CeremonyStatusResponse, or forms left over from
// normalization. The second group is real drift: go-quicbaas#946 owns the
// product decision for each, and deleting these three entries is part of closing
// it. They are listed rather than removed from the comparison because the
// alternative was to keep shipping a gate that compared nothing at all.
var knownUnmirrored = map[string]string{
	"CeremonyOfficer":                 "represented inline in CeremonyStatusResponse",
	"EnrollExternalOfficer":           "prefixed form is what the TS SDK exports",
	"BarrierShamir":                   "normalization residue of BarrierInitializeShamir",
	"BarrierShamirRequest":            "normalization residue of BarrierInitializeShamirRequest",
	"BarrierShamirResponse":           "normalization residue of BarrierInitializeShamirResponse",
	"BarrierInitialize":               "TS SDK spells the verb BarrierInit",
	"BarrierInitializeRequest":        "TS SDK exports it as BarrierInitRequest",
	"BarrierSealRequest":              "go-quicbaas#946 — TS BarrierClient.seal() takes no argument",
	"BarrierEnableEncryptionRequest":  "go-quicbaas#946 — the TS SDK has no enable-encryption verb",
	"BarrierEnableEncryptionResponse": "go-quicbaas#946 — the TS SDK has no enable-encryption verb",
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// ModuleResolveError reports that the go-qrdb SDK module could not be located.
type ModuleResolveError struct {
	Module string
	Output string
	Err    error
}

func (e *ModuleResolveError) Error() string {
	msg := "cannot locate module " + e.Module + " (is it in go.mod, and has it been downloaded?): " + e.Err.Error()
	if trimmed := strings.TrimSpace(e.Output); trimmed != "" {
		msg += " (" + trimmed + ")"
	}
	return msg
}

func (e *ModuleResolveError) Unwrap() error { return e.Err }

// EmptyTypeSetError reports that a side of the comparison yielded no names. An
// empty set makes every comparison vacuously true, which is the shape of a gate
// that has stopped reading its input.
type EmptyTypeSetError struct {
	Side   string
	Source string
}

func (e *EmptyTypeSetError) Error() string {
	return "extracted no " + e.Side + " types from " + e.Source + "; the extraction has stopped matching the source"
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verifyapishapes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root holding the TypeScript SDK")
	qrdbDir := fs.String("qrdb-dir", "", "go-qrdb sdk/go module root (default: resolved from the module graph)")
	tsTypes := fs.String("ts-types", "", "TypeScript types file (default: "+defaultTSTypes+" under -root)")
	if err := fs.Parse(args); err != nil {
		return gate.ExitToolError
	}

	dir := *qrdbDir
	if dir == "" {
		resolved, err := resolveModuleDir(*root, qrdbSDKModule)
		if err != nil {
			fmt.Fprintln(stderr, "verify-api-shapes FAIL:", err)
			return gate.ExitToolError
		}
		dir = resolved
	}
	tsPath := *tsTypes
	if tsPath == "" {
		tsPath = *root + string(os.PathSeparator) + defaultTSTypes
	}

	goNames, err := goTypes(dir)
	if err != nil {
		fmt.Fprintln(stderr, "verify-api-shapes FAIL:", err)
		return gate.ExitToolError
	}
	tsNames, err := tsTypesFrom(tsPath)
	if err != nil {
		fmt.Fprintln(stderr, "verify-api-shapes FAIL:", err)
		return gate.ExitToolError
	}

	// The TS surface may be a strict superset of the Go surface — the QuicBaaS
	// SDK adds local convenience aliases — but any Go type with no TS
	// counterpart after normalization is drift.
	mirrored := map[string]bool{}
	for _, name := range tsNames {
		mirrored[normalize(name)] = true
	}
	var missing []string
	for _, name := range goNames {
		canonical := normalize(name)
		if mirrored[canonical] {
			continue
		}
		if _, known := knownUnmirrored[canonical]; known {
			continue
		}
		missing = append(missing, canonical)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		fmt.Fprintln(stdout, "verify-api-shapes FAIL: Go types missing from TypeScript SDK:")
		for _, name := range missing {
			fmt.Fprintln(stdout, "  "+name)
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Add the type to sdk/typescript/src/types.ts, or — if the concept is genuinely")
		fmt.Fprintln(stdout, "unrepresentable downstream — record it in knownUnmirrored in")
		fmt.Fprintln(stdout, "scripts/verifyapishapes/main.go with the reason and the issue that owns it.")
		return gate.ExitFindings
	}

	fmt.Fprintln(stdout, "verify-api-shapes OK: every Go barrier/ceremony/bootstrap type is mirrored in the TypeScript SDK")
	return gate.ExitClean
}

// resolveModuleDir asks the go tool where a required module's source is. This is
// what replaces the lab mount point: the answer is correct on a developer's
// machine, in a container and on a runner, and it is an error rather than an
// empty string when the module is not there.
func resolveModuleDir(root, module string) (string, error) {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", module)
	cmd.Dir = root
	// The go tool's own complaint belongs in the error, not on the terminal
	// alongside it.
	var complaint strings.Builder
	cmd.Stderr = &complaint
	out, err := cmd.Output()
	if err != nil {
		return "", &ModuleResolveError{Module: module, Output: complaint.String(), Err: err}
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", &gate.MissingInputError{
			Input: "module " + module,
			Why:   "the go tool resolved it to no directory, so the canonical Go types cannot be read",
		}
	}
	return dir, nil
}

// goTypes returns the canonical Go type names, with the prefix rewrites applied
// that make them comparable with the TypeScript surface.
func goTypes(moduleDir string) ([]string, error) {
	var names []string
	for _, rel := range qrdbTypeFiles {
		path := moduleDir + string(os.PathSeparator) + rel
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, &gate.MissingInputError{
				Input: path,
				Why:   "it holds the canonical barrier/ceremony type names this gate compares against",
			}
		}
		for _, match := range goTypeDecl.FindAllStringSubmatch(string(raw), -1) {
			names = append(names, rewriteGoPrefix(match[1]))
		}
	}
	names = sortUnique(names)
	if len(names) == 0 {
		return nil, &EmptyTypeSetError{Side: "Go", Source: moduleDir}
	}
	return names, nil
}

// tsTypesFrom returns the exported TypeScript type names under test.
func tsTypesFrom(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &gate.MissingInputError{
			Input: path,
			Why:   "it is the TypeScript surface this gate holds against the canonical Go types",
		}
	}
	var names []string
	for _, match := range tsTypeDecl.FindAllStringSubmatch(string(raw), -1) {
		names = append(names, match[1])
	}
	names = sortUnique(names)
	if len(names) == 0 {
		return nil, &EmptyTypeSetError{Side: "TypeScript", Source: path}
	}
	return names, nil
}

// rewriteGoPrefix gives a Go name the prefix its TypeScript counterpart carries.
func rewriteGoPrefix(name string) string {
	for _, rewrite := range goPrefixRewrites {
		if strings.HasPrefix(name, rewrite.from) {
			return rewrite.to + strings.TrimPrefix(name, rewrite.from)
		}
	}
	return name
}

// normalize collapses the naming-convention differences so both sides are
// comparable.
func normalize(name string) string {
	for _, rule := range namingDrift {
		name = strings.ReplaceAll(name, rule.from, rule.to)
	}
	return name
}

func sortUnique(in []string) []string {
	sort.Strings(in)
	out := in[:0]
	for i, v := range in {
		if i == 0 || in[i-1] != v {
			out = append(out, v)
		}
	}
	return out
}
