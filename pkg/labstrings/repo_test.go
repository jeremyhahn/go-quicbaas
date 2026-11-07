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

package labstrings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory to the module root. The
// gate scans the whole repository, so it has to find it the same way from any
// package directory `go test` happens to start in.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// TestTheTrackedTreeIsPubliclyReleasable is the gate itself. It runs on every
// `make test` pass because pkg/... is in that target, so a lab value cannot
// reach main without somebody reading this failure first.
func TestTheTrackedTreeIsPubliclyReleasable(t *testing.T) {
	root := repoRoot(t)
	findings, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("scanning %s: %v", root, err)
	}
	var report strings.Builder
	dirty, reportErr := Report(&report, findings)
	if reportErr != nil {
		t.Fatalf("writing the report: %v", reportErr)
	}
	if dirty {
		t.Fatalf("this tree cannot be published as it stands:\n\n%s", report.String())
	}
}

// TestThePlantedViolationIsCaught proves the gate above can fail. A gate nobody
// has seen fail is indistinguishable from a gate that does not run: this writes
// one of each class into a copy of the tree's own layout, half of them public
// leaks and half values only the private pattern file knows, and asserts every
// one comes back, then asserts the same values pass once waived.
func TestThePlantedViolationIsCaught(t *testing.T) {
	planted := map[string]string{
		"cmd/quicbaas/help.go": "const help = \"--server https://203.0.113.51:8443\"\n",
		"docs/guide.md":        "Deploy it on core-01, then check /home/carol/sources.\n",
		"pkg/api/rest/ui/dist/assets/index-DEADBEEF.js": "const p=\"agent on nas.lan\";\n",
		// The Makefile once carried a whole-file allowance, which let a
		// private forge default land unreported (go-quicbaas#1130). It is
		// scanned like any other file now; this keeps it that way.
		"Makefile":  "GITEA_HTTP_ORIGIN ?= http://192.168.7.91:3000\n",
		"README.md": "Questions go to jdoe.\n",
	}
	root, files := writeTree(t, planted)
	t.Setenv(PatternsEnv, writePatternFile(t, privateFixture))
	set, err := ConfiguredPatterns(root)
	if err != nil {
		t.Fatalf("ConfiguredPatterns: %v", err)
	}

	findings, err := Scan(root, files, set, DefaultAllowList)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byClass := map[Class]int{}
	for _, f := range findings {
		byClass[f.Class]++
	}
	for _, want := range []Class{ClassLabAddress, ClassLabHost, ClassOwnerPath, ClassOwnerAccount} {
		if byClass[want] == 0 {
			t.Errorf("no %s finding in %v", want, findings)
		}
	}

	var report strings.Builder
	dirty, err := Report(&report, findings)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !dirty {
		t.Fatal("Report says a tree with planted violations is clean")
	}
	for name := range planted {
		if !strings.Contains(report.String(), name) {
			t.Errorf("report does not name %s:\n%s", name, report.String())
		}
	}

	// The same values, waived in place, are accepted -- which is what makes
	// the gate usable for the cases where the value is the point.
	waived := map[string]string{
		"docs/guide.md": "Deploy it on core-01 <!-- lab-string-ok: measurement provenance -->\n",
	}
	waivedRoot, waivedFiles := writeTree(t, waived)
	rest, err := Scan(waivedRoot, waivedFiles, set, DefaultAllowList)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(rest) != 0 {
		t.Fatalf("a waived line was still reported: %v", rest)
	}
}

// TestScanRepo_AnUnusablePatternFileFailsTheGate pins that a configured but
// broken private file is an error, never a quietly narrower scan.
func TestScanRepo_AnUnusablePatternFileFailsTheGate(t *testing.T) {
	t.Setenv(PatternsEnv, filepath.Join(t.TempDir(), "absent.json"))
	_, err := ScanRepo(repoRoot(t))
	var fileErr *PatternFileError
	if !errors.As(err, &fileErr) {
		t.Fatalf("ScanRepo with a missing pattern file = %v, want *PatternFileError", err)
	}
}

// TestTheGateCatchesWhatTheAuditFound pins the kinds of leak go-quicbaas#944
// found, each in the kind of file it was found in, with the private values
// stood in for by the fixture network. If a change to the scanner stops
// matching any of these, the gate has stopped doing the job it was written for.
func TestTheGateCatchesWhatTheAuditFound(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content string
		want    Class
	}{
		{
			name:    "a repo-root symlink target into a private mount",
			file:    "note.txt",
			content: "/mnt/lab-share/go-quicbaas/\n",
			want:    ClassOwnerPath,
		},
		{
			name:    "a private registry in a release workflow",
			file:    ".github/workflows/release.yml",
			content: "  REGISTRY: 203.0.113.91:3001\n",
			want:    ClassLabAddress,
		},
		{
			name:    "a forge issue link in a shipped doc",
			file:    "docs/barrier/cli.md",
			content: "([#491](http://203.0.113.91:3001/example/go-quicbaas/issues/491))\n",
			want:    ClassLabAddress,
		},
		{
			name:    "a private host in --help text",
			file:    "cmd/quicbaas/orchestrator.go",
			content: "\t\t\"Pin the session by Name or Host (e.g. 'core-01')\")\n",
			want:    ClassLabHost,
		},
		{
			name:    "a home-network address in exported godoc",
			file:    "pkg/fleet/config.go",
			content: "\t// (e.g., \"https://192.168.7.51:8443\"). Required.\n",
			want:    ClassLabAddress,
		},
		{
			name:    "a copy-pasteable endpoint in a doc",
			file:    "docs/embeddings/npu.md",
			content: "cfg.BaseURL = \"http://core-02.example:8077\"\n",
			want:    ClassLabHost,
		},
		{
			name:    "a private registry in a captured runner log",
			file:    "pkg/cifault/testdata/registry-unreachable.log",
			content: "image: 203.0.113.91:3001/example/ci-runner:latest\n",
			want:    ClassLabAddress,
		},
		{
			name:    "a personal account baked into a shipped bundle",
			file:    "pkg/api/rest/ui/dist/assets/index-DEADBEEF.js",
			content: "const owner=\"jdoe\";\n",
			want:    ClassOwnerAccount,
		},
		{
			name:    "a session note full of home paths",
			file:    "SESSION_RESUME.md",
			content: "- `/home/carol/sources/...`\n",
			want:    ClassOwnerPath,
		},
	}

	set := configured(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, files := writeTree(t, map[string]string{tc.file: tc.content})
			findings, err := Scan(root, files, set, DefaultAllowList)
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			for _, f := range findings {
				if f.Class == tc.want {
					return
				}
			}
			t.Fatalf("no %s finding for %s: %v", tc.want, tc.file, findings)
		})
	}
}
