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

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// writeTree writes files into a fresh directory and, unless bare is set, makes
// it a git work tree with every file tracked — rule 1 reads tracked files only,
// so an untracked fixture would prove nothing.
func writeTree(t *testing.T, bare bool, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	if bare {
		return dir
	}
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"add", "--force", "--", "."},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in the fixture: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestRunPassesACompliantTree(t *testing.T) {
	dir := writeTree(t, false, map[string]string{
		// The Software Factory is the area's name and the SDLC module is the
		// git module's; both are fine.
		"docs/factory.md": "The Software Factory drives the SDLC module.\n",
		// The English verb is a different word and is left alone.
		"pkg/mint/mint.go": "package mint\n\n// We " + allowedInflection + "e nothing here.\n",
		// Seal* on the documented alias surface.
		"pkg/seal/types.go":      "package seal\n\ntype SealMode int\n",
		"cmd/quicbaas/seal.go":   "package main\n\nvar _ SealClient\n",
		"pkg/barrier/barrier.go": "package barrier\n\ntype BarrierManager struct{}\n",
	})

	var stdout, stderr strings.Builder
	if status := run([]string{"-root", dir}, &stdout, &stderr); status != gate.ExitClean {
		t.Fatalf("run exited %d, want %d\nstdout: %s\nstderr: %s",
			status, gate.ExitClean, stdout.String(), stderr.String())
	}
	for _, want := range []string{
		"the retired product word appears in no tracked file",
		"every Seal* mention is on the documented one-release alias surface",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("the pass does not report %q:\n%s", want, stdout.String())
		}
	}
}

func TestRunFailsOnTheRetiredProductWord(t *testing.T) {
	// THE CASE THAT MATTERS. go-quicbaas#581 purged 77 occurrences across 34
	// files; a gate that cannot see one grow back is no gate.
	dir := writeTree(t, false, map[string]string{
		"docs/area.md": "The " + strings.ToUpper(retired[:1]) + retired[1:] + " drives the module.\n",
	})

	var stdout, stderr strings.Builder
	status := run([]string{"-root", dir}, &stdout, &stderr)
	if status != gate.ExitFindings {
		t.Fatalf("run exited %d, want %d\nstdout: %s", status, gate.ExitFindings, stdout.String())
	}
	report := stdout.String()
	if !strings.Contains(report, "the retired product word is back in 1 file(s)") {
		t.Fatalf("the report does not count the files: %s", report)
	}
	if !strings.Contains(report, "docs/area.md") {
		t.Fatalf("the report does not name the file: %s", report)
	}
	if !strings.Contains(report, "the gate has no allowlist") {
		t.Fatalf("the report does not say what to do: %s", report)
	}
}

func TestRunFailsOnSealOutsideTheAliasSurface(t *testing.T) {
	// THE OTHER CASE THAT MATTERS. The alias surface is a one-release
	// deprecation path; a new Seal* identifier anywhere else has to fail.
	dir := writeTree(t, false, map[string]string{
		"pkg/newthing/manager.go":   "package newthing\n\ntype SealManager interface{}\n",
		"sdk/typescript/src/new.ts": "export interface SealView {}\n",
	})

	var stdout, stderr strings.Builder
	status := run([]string{"-root", dir}, &stdout, &stderr)
	if status != gate.ExitFindings {
		t.Fatalf("run exited %d, want %d\nstdout: %s", status, gate.ExitFindings, stdout.String())
	}
	report := stdout.String()
	for _, want := range []string{
		"Seal* identifiers found outside the documented alias surface",
		"pkg/newthing/manager.go",
		"sdk/typescript/src/new.ts",
		"rename the symbols to Barrier*/Ceremony*/Bootstrap*",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("the report omits %q:\n%s", want, report)
		}
	}
}

func TestRunFailsClosedWhenItCannotLook(t *testing.T) {
	// A gate that passes because it could not look is worse than no gate, so a
	// tree git cannot answer for is a tool error and never a skip.
	dir := writeTree(t, true, map[string]string{
		"docs/area.md": "The " + retired + " is back.\n",
	})

	var stdout, stderr strings.Builder
	status := run([]string{"-root", dir}, &stdout, &stderr)
	if status != gate.ExitToolError {
		t.Fatalf("run on a non-work-tree exited %d, want %d", status, gate.ExitToolError)
	}
	if !strings.Contains(stderr.String(), "is not a git work tree") {
		t.Fatalf("the failure does not say why the scan could not run: %q", stderr.String())
	}
}

func TestRetiredWordHitsIgnoresTheEnglishVerb(t *testing.T) {
	// Only the noun is retired. "fabricate" and its inflections are a different
	// word; retiring them too would make the gate unusable in prose.
	dir := writeTree(t, false, map[string]string{
		"a.md": allowedInflection + "e\n" + allowedInflection + "ed\n" +
			allowedInflection + "ion\n" + strings.ToUpper(allowedInflection) + "ING\n",
	})
	hits, err := retiredWordHits(dir)
	if err != nil {
		t.Fatalf("retiredWordHits: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("the English verb was reported as the retired noun: %+v", hits)
	}

	// The noun itself, in any case, is a hit.
	dir = writeTree(t, false, map[string]string{
		"a.md": retired + "\n" + strings.ToUpper(retired) + "\n",
	})
	hits, err = retiredWordHits(dir)
	if err != nil {
		t.Fatalf("retiredWordHits: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected both spellings of the noun to be hits, got %+v", hits)
	}
	for _, h := range hits {
		if h.path != "a.md" {
			t.Fatalf("a hit names the wrong file: %+v", h)
		}
	}
}

func TestOnAliasSurfaceAcceptsOnlyTheDocumentedPaths(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: "pkg/seal/types.go", want: true},         // enumerated file
		{path: "pkg/seal/anything_else.go", want: true}, // enumerated tree
		{path: "cmd/quicbaas/factories_mcp_seal.go", want: true},
		{path: "sdk/go/client/seal/client.go", want: true},
		{path: "pkg/testutil/mocks.go", want: true},
		{path: "pkg/newthing/manager.go", want: false},
		{path: "sdk/typescript/src/new.ts", want: false},
		// A path that merely starts with an alias tree's name is not inside it.
		{path: "pkg/sealant/thing.go", want: false},
		{path: "cmd/quicbaasd/main.go", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := onAliasSurface(tc.path); got != tc.want {
				t.Fatalf("onAliasSurface(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestSealOffAliasSkipsInstalledAndGeneratedTrees(t *testing.T) {
	// A dependency's own Seal* identifiers are not this repository's naming.
	dir := writeTree(t, false, map[string]string{
		"pkg/.deps/other/x.go":         "package other\n\ntype SealClient struct{}\n",
		"sdk/node_modules/pkg/x.ts":    "export interface SealClient {}\n",
		"sdk/typescript/dist/index.ts": "export interface SealClient {}\n",
		"pkg/quiet/readme.md":          "SealManager is fine in prose here.\n",
		"pkg/quiet/quiet.go":           "package quiet\n",
	})
	offAlias, err := sealOffAlias(dir)
	if err != nil {
		t.Fatalf("sealOffAlias: %v", err)
	}
	if len(offAlias) != 0 {
		t.Fatalf("installed, generated or non-code files were reported: %q", offAlias)
	}
}

func TestTypedErrorsCarryTheirCause(t *testing.T) {
	cause := errors.New("fatal: not a git repository")
	notTree := &NotAWorkTreeError{Root: "/tmp/x", Err: cause}
	if !errors.Is(notTree, cause) {
		t.Fatal("NotAWorkTreeError must unwrap to the underlying failure")
	}
	if !strings.Contains(notTree.Error(), "/tmp/x") {
		t.Fatalf("NotAWorkTreeError must name the tree: %s", notTree.Error())
	}
	grep := &GitGrepError{Err: cause}
	if !errors.Is(grep, cause) {
		t.Fatal("GitGrepError must unwrap to the underlying failure")
	}
}

func TestRunFailsWhenTheSealScanCannotReadAFile(t *testing.T) {
	// Rule 2 reads every candidate file. One it cannot read is a file whose
	// Seal* content is unknown, which fails the gate rather than passing it.
	dir := writeTree(t, false, map[string]string{"pkg/keep/keep.go": "package keep\n"})
	// A dangling symlink named like a Go file: the walk sees a candidate, and
	// reading it fails. Root-independent, unlike revoking the read bit.
	if err := os.Symlink("no-such-target.go", filepath.Join(dir, "pkg", "keep", "dangling.go")); err != nil {
		t.Fatalf("preparing the fixture: %v", err)
	}

	if _, err := sealOffAlias(dir); err == nil {
		t.Fatal("sealOffAlias must fail on a candidate it cannot read")
	}

	var stdout, stderr strings.Builder
	if status := run([]string{"-root", dir}, &stdout, &stderr); status != gate.ExitToolError {
		t.Fatalf("run exited %d, want %d", status, gate.ExitToolError)
	}
	if !strings.Contains(stderr.String(), "verify-naming FAIL") {
		t.Fatalf("the failure was not reported: %q", stderr.String())
	}
}

func TestGitGrepErrorSaysWhatFailed(t *testing.T) {
	err := &GitGrepError{Err: errors.New("fatal: bad revision")}
	if !strings.Contains(err.Error(), "git grep failed") {
		t.Fatalf("GitGrepError.Error() = %s", err.Error())
	}
	if !strings.Contains(err.Error(), "fatal: bad revision") {
		t.Fatalf("GitGrepError.Error() omits the cause: %s", err.Error())
	}
}
