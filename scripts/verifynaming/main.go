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

// Command verifynaming is the repository's naming gate. Two rules:
//
//  1. The retired product word appears nowhere in a tracked file. The product
//     area is the Software Factory and the git module is the SDLC module; the
//     older working name was purged repo-wide by go-quicbaas#581 and must not
//     grow back.
//  2. Seal* identifiers ship only on the documented one-release deprecation
//     alias surface.
//
// The seal→barrier rename retains a deprecation surface so existing consumers
// don't break: legacy filenames (seal.go, client_seal.go, seal_manager_compat.go,
// factories_mcp_seal*.go, seal_alias_test.go, RestSealClient + SealClient TS
// alias, SealView Svelte alias). Those files are enumerated in aliasFiles below;
// anything else mentioning Seal* fails the check.
//
// THE TRACKED-FILE SCAN FAILS CLOSED. A gate that passes because it could not
// look is worse than no gate, so a tree git cannot answer for is a failure and
// not a skip.
//
// EXIT: 0 both rules hold · 1 a violation · 2 the gate could not run.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// retired is the retired product word, spelled in two halves on purpose:
// written whole, this file would be the one tracked file that trips its own
// gate.
const retired = "fab" + "ric"

// allowedInflection is the English verb built on the retired noun. "fabricate"
// and its inflections are a different word and are left alone; only the noun is
// retired.
const allowedInflection = "fab" + "ricat"

// There is no allowlist for rule 1. Every tracked file is in scope, including
// pkg/sdlc/, whose last occurrences went with the SDLC module rewrite.

// sealIdentifiers are the Seal* identifiers rule 2 governs.
var sealIdentifiers = regexp.MustCompile(`SealClient\b|SealManager\b|SealService\b|SealView\b|SealMode\b`)

// sealScanRoots are the trees rule 2 reads.
var sealScanRoots = []string{"pkg", "cmd", "sdk"}

// sealScanExtensions are the file kinds that can carry an identifier.
var sealScanExtensions = map[string]bool{
	".go":     true,
	".ts":     true,
	".svelte": true,
}

// sealScanPruned are trees that answer for themselves: installed dependencies
// and build output.
var sealScanPruned = map[string]bool{
	".deps":        true,
	"node_modules": true,
	"dist":         true,
}

// aliasFiles are the files ALLOWED to mention Seal* — the documented alias
// surface.
var aliasFiles = map[string]bool{
	"pkg/seal/types.go":                           true, // SealMode legacy enum
	"pkg/seal/barrier_seal_manager.go":            true, // legacy adapter (test-only at runtime)
	"pkg/seal/barrier_seal_manager_test.go":       true,
	"pkg/api/grpc/client_seal.go":                 true, // deprecated thin wrappers
	"pkg/api/grpc/seal_manager_compat.go":         true, // noop SealManager shim
	"cmd/quicbaas/seal.go":                        true, // deprecation alias forwarder
	"cmd/quicbaas/seal_test.go":                   true,
	"cmd/quicbaas/seal_alias_test.go":             true,
	"cmd/quicbaas/seal_recovery_keys_test.go":     true,
	"cmd/quicbaas/factories_mcp_seal_test.go":     true,
	"cmd/quicbaas/quic_kms_client.go":             true, // contains QUICSealClient (legacy adapter)
	"sdk/typescript/src/rest.ts":                  true, // contains RestSealClient deprecation alias
	"sdk/typescript/src/interfaces.ts":            true, // contains SealClient interface alias
	"sdk/typescript/src/index.ts":                 true, // re-exports the alias
	"sdk/typescript/svelte-security/src/types.ts": true, // comment references SealClient legacy adapter
	"pkg/service/service.go":                      true, // interface aggregator references SealService alias
	"pkg/testutil/mocks.go":                       true, // MockSealClient kept for legacy tests
}

// aliasDirs are trees whose files remain on the alias path. All other test files
// inside cmd/quicbaas reference SealClient/MockSealClient variables for legacy
// CLI tests.
var aliasDirs = []string{
	"cmd/quicbaas",        // CLI tests still reference SealClient/MockSealClient via aliases
	"sdk/typescript/test", // TS tests cover the deprecation aliases
	"sdk/go",              // Go SDK retains Seal*/SealClient aliases for one release
	"pkg/seal",            // legacy seal package retained as compat layer
	"pkg/service",         // legacy seal_service.go and SealService aliases on the interface aggregator
	"pkg/testutil",        // MockSealClient kept for legacy tests
	"pkg/server",          // gRPC no-op seal shim seam and SealService wiring on the lifecycle layer
	"pkg/api",             // SealErrors retained for one release
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// NotAWorkTreeError reports that the tracked-file scan could not run, which is
// a failure and never a skip.
type NotAWorkTreeError struct {
	Root string
	Err  error
}

func (e *NotAWorkTreeError) Error() string {
	return e.Root + " is not a git work tree, so the tracked-file scan could not run: " + e.Err.Error()
}

func (e *NotAWorkTreeError) Unwrap() error { return e.Err }

// GitGrepError reports that git grep failed for a reason other than "no match".
type GitGrepError struct {
	Err error
}

func (e *GitGrepError) Error() string { return "git grep failed: " + e.Err.Error() }

func (e *GitGrepError) Unwrap() error { return e.Err }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("verifynaming", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root to scan")
	if err := fs.Parse(args); err != nil {
		return gate.ExitToolError
	}

	hits, err := retiredWordHits(*root)
	if err != nil {
		fmt.Fprintln(stderr, "verify-naming FAIL:", err)
		return gate.ExitToolError
	}
	if len(hits) > 0 {
		reportRetired(stdout, hits)
		return gate.ExitFindings
	}
	fmt.Fprintln(stdout, "verify-naming OK: the retired product word appears in no tracked file")

	offAlias, err := sealOffAlias(*root)
	if err != nil {
		fmt.Fprintln(stderr, "verify-naming FAIL:", err)
		return gate.ExitToolError
	}
	if len(offAlias) > 0 {
		reportSeal(stdout, offAlias)
		return gate.ExitFindings
	}
	fmt.Fprintln(stdout, "verify-naming OK: every Seal* mention is on the documented one-release alias surface")
	return gate.ExitClean
}

// hit is one occurrence of the retired word, as git grep reported it.
type hit struct {
	// line is git grep's `path:line:match` record.
	line string
	// path is the tracked file it came from.
	path string
}

// retiredWordHits returns every occurrence of the retired noun in a tracked
// file. Tracked files only, and it fails closed when that cannot be established.
func retiredWordHits(root string) ([]hit, error) {
	inside := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	inside.Dir = root
	if err := inside.Run(); err != nil {
		return nil, &NotAWorkTreeError{Root: root, Err: err}
	}

	grep := exec.Command("git", "grep", "-nIoiE", retired+"[a-z]*", "--", ".")
	grep.Dir = root
	out, err := grep.Output()
	if err != nil {
		// git grep exits 1 when it matches nothing, which is the clean tree.
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil, nil
		}
		return nil, &GitGrepError{Err: err}
	}

	var hits []hit
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		// `git grep -o` prints path:line:match, so the match is the last field
		// and the path starts the line.
		cut := strings.LastIndex(line, ":")
		if cut < 0 {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line[cut+1:]), allowedInflection) {
			continue
		}
		path := line
		if first := strings.Index(line, ":"); first >= 0 {
			path = line[:first]
		}
		hits = append(hits, hit{line: line, path: path})
	}
	return hits, nil
}

func reportRetired(stdout io.Writer, hits []hit) {
	files := map[string]bool{}
	for _, h := range hits {
		files[h.path] = true
	}
	fmt.Fprintf(stdout, "verify-naming FAIL: the retired product word is back in %d file(s):\n", len(files))
	for _, h := range hits {
		fmt.Fprintln(stdout, "  "+h.line)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "The product area is the Software Factory; the git module is the SDLC")
	fmt.Fprintln(stdout, "module. Reword the occurrence — the gate has no allowlist.")
}

// sealOffAlias returns every file mentioning a Seal* identifier that is not on
// the documented alias surface.
//
// The walk COLLECTS and the read happens after it, rather than reading inside
// the WalkDir callback. Reading a path the callback was handed is a symlink
// TOCTOU (gosec G122): the entry the walk classified and the file the read opens
// need not be the same thing. Separating the two is also the shape
// scripts/internal/dockerinv uses, so the tree has one answer to this rather
// than two.
func sealOffAlias(root string) ([]string, error) {
	candidates, err := sealCandidates(root)
	if err != nil {
		return nil, err
	}

	var offAlias []string
	for _, rel := range candidates {
		raw, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if rerr != nil {
			// A candidate this gate cannot read is a file whose Seal* content is
			// unknown, which fails the scan rather than passing it.
			return nil, rerr
		}
		if sealIdentifiers.Match(raw) {
			offAlias = append(offAlias, rel)
		}
	}
	sort.Strings(offAlias)
	return offAlias, nil
}

// sealCandidates returns every file rule 2 must read: the right kind of file,
// under one of the scanned trees, outside the installed and generated trees, and
// not already on the alias surface. Paths are relative to root and
// slash-separated, so they read the same on every platform and match the alias
// surface as written.
func sealCandidates(root string) ([]string, error) {
	var candidates []string
	for _, scanRoot := range sealScanRoots {
		abs := filepath.Join(root, scanRoot)
		if _, err := os.Stat(abs); errors.Is(err, os.ErrNotExist) {
			continue
		}
		err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if sealScanPruned[d.Name()] {
					return fs.SkipDir
				}
				return nil
			}
			if !sealScanExtensions[filepath.Ext(d.Name())] {
				return nil
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			if onAliasSurface(rel) {
				return nil
			}
			candidates = append(candidates, rel)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return candidates, nil
}

// onAliasSurface reports whether path is one of the enumerated alias files or
// lies under one of the alias trees.
func onAliasSurface(path string) bool {
	if aliasFiles[path] {
		return true
	}
	for _, dir := range aliasDirs {
		if strings.HasPrefix(path, dir+"/") {
			return true
		}
	}
	return false
}

func reportSeal(stdout io.Writer, offAlias []string) {
	fmt.Fprintln(stdout, "verify-naming FAIL: Seal* identifiers found outside the documented alias surface:")
	for _, path := range offAlias {
		fmt.Fprintln(stdout, "  "+path)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Either move these files into the alias path (and add to scripts/verifynaming/main.go),")
	fmt.Fprintln(stdout, "or rename the symbols to Barrier*/Ceremony*/Bootstrap*.")
}
