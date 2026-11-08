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
	"strconv"
	"strings"
	"testing"
)

// fixtureRules is a pattern file built from reference values only: an RFC 5737
// subnet, and host, path and account names nobody owns. It has the shapes the
// real rules have -- a numbered host series, a bare hostname with an optional
// domain, a home directory, a mount, a deployment directory, an account name --
// so every behaviour of the gate is exercised without the tests depending on
// the private rules or on the environment. The account rule comes first on
// purpose: ParsePatterns has to move it after the paths.
var fixtureRules = []string{
	"# reference values only",
	"owner-account/i\t" + `\bexample-owner\b`,
	"lab-address\t" + `\b203\.0\.113\.\d{1,3}\b`,
	"lab-host/i\t" + `\bfleet-node-\d{2}\b`,
	"lab-host/i\t" + `\bbuild-box-\d{2}\b`,
	"lab-host/i\t" + `\bfleethost(\.test)?\b`,
	"owner-path\t" + `/home/example-owner\b`,
	"owner-path\t" + `/mnt/example-share`,
	"owner-path\t" + `example-soak-dir`,
}

// fixturePatternText is fixtureRules as the environment variable would carry it.
var fixturePatternText = strings.Join(fixtureRules, "\n") + "\n"

// ownerPathFixture is the home directory the owner-path fixture rule finds.
const ownerPathFixture = "/home/example-owner"

// fixturePatterns parses fixturePatternText, failing the test if it does not.
func fixturePatterns(t testing.TB) []Pattern {
	t.Helper()
	patterns, err := ParsePatterns(fixturePatternText)
	if err != nil {
		t.Fatalf("the fixture pattern text does not parse: %v", err)
	}
	return patterns
}

// writeTree writes files into a temporary directory and returns the root plus
// the file list in the form Scan takes.
func writeTree(t *testing.T, files map[string]string) (string, []string) {
	t.Helper()
	root := t.TempDir()
	names := make([]string, 0, len(files))
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		names = append(names, name)
	}
	return root, names
}

func TestScan_ReportsEveryClassWithItsLineAndMatch(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		wantClass Class
		wantMatch string
		wantLine  int
	}{
		{
			name:      "fleet address in help text",
			content:   "package main\n\nconst help = \"dial 203.0.113.51:8443\"\n",
			wantClass: ClassLabAddress,
			wantMatch: "203.0.113.51",
			wantLine:  3,
		},
		{
			name:      "fleet hostname in a doc",
			content:   "# Runbook\n\nRun it on fleethost first.\n",
			wantClass: ClassLabHost,
			wantMatch: "fleethost",
			wantLine:  3,
		},
		{
			name:      "runner hostname in a captured log",
			content:   "2026-09-12T00:00:00Z build-box-02(version:v3.1.0)\n",
			wantClass: ClassLabHost,
			wantMatch: "build-box-02",
			wantLine:  1,
		},
		{
			name:      "agent hostname in a comment",
			content:   "// observed on FLEET-NODE-04\n",
			wantClass: ClassLabHost,
			wantMatch: "FLEET-NODE-04",
			wantLine:  1,
		},
		{
			name:      "owner home path in a doc",
			content:   "See " + ownerPathFixture + "/sources for the checkout.\n",
			wantClass: ClassOwnerPath,
			wantMatch: ownerPathFixture,
			wantLine:  1,
		},
		{
			name:      "owner account name in a comment",
			content:   "package x\n\n// TODO(example-owner): drop after the migration\n",
			wantClass: ClassOwnerAccount,
			wantMatch: "example-owner",
			wantLine:  3,
		},
		{
			name:      "lab NFS mount in a script default",
			content:   "QRDB=${QRDB:-/mnt/example-share/go-qrdb}\n",
			wantClass: ClassOwnerPath,
			wantMatch: "/mnt/example-share",
			wantLine:  1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, files := writeTree(t, map[string]string{"shipping/file.txt": tc.content})
			findings, err := ScanWith(root, files, nil, fixturePatterns(t))
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			var got *Finding
			for i := range findings {
				if findings[i].Class == tc.wantClass {
					got = &findings[i]
					break
				}
			}
			if got == nil {
				t.Fatalf("no %s finding; got %v", tc.wantClass, findings)
			}
			if got.Match != tc.wantMatch {
				t.Errorf("match = %q, want %q", got.Match, tc.wantMatch)
			}
			if got.Line != tc.wantLine {
				t.Errorf("line = %d, want %d", got.Line, tc.wantLine)
			}
			if got.Path != "shipping/file.txt" {
				t.Errorf("path = %q, want shipping/file.txt", got.Path)
			}
			if got.Guidance == "" {
				t.Error("finding carries no guidance; the report would not say what to do")
			}
			if !strings.Contains(got.String(), "shipping/file.txt:"+strconv.Itoa(tc.wantLine)) {
				t.Errorf("String() is not file:line clickable: %s", got.String())
			}
			if got.Guidance != classGuidance[tc.wantClass] {
				t.Errorf("guidance = %q, want the %s class advice", got.Guidance, tc.wantClass)
			}
		})
	}
}

func TestScan_AcceptsWhatItShould(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "documentation addresses outside the rule's subnet",
			content: "dial 198.51.100.7:8443 or 192.0.2.10\n",
		},
		{
			name:    "addresses that share only part of the subnet",
			content: "peers: 203.0.1.4, 203.0.114.4, 10.0.0.1\n",
		},
		{
			name:    "the owner-path rules' neighbours",
			content: "/home/agent/sources and /home/example-owners-guide and /mnt/data\n",
		},
		{
			name:    "the repository's own example names",
			content: "--server https://quicbaas.example.com:8443 --host-affinity agent-01\n",
		},
		{
			name:    "a waiver on the offending line",
			content: "Measured on fleethost. <!-- lab-string-ok: measurement provenance -->\n",
		},
		{
			name:    "a waiver on the line above",
			content: "# lab-string-ok: measurement provenance\nMeasured on fleethost.\n",
		},
		{
			name:    "a waiver whose reason ends at a comment closer",
			content: "x := 1 /* lab-string-ok: provenance */ // fleethost\n",
		},
		{
			name:    "a hostname that merely contains a fleet name as a substring",
			content: "unfleethostness is not a host, and fleet-node-0 has no second digit\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, files := writeTree(t, map[string]string{"clean.txt": tc.content})
			findings, err := ScanWith(root, files, nil, fixturePatterns(t))
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if len(findings) != 0 {
				t.Fatalf("expected no findings, got %v", findings)
			}
		})
	}
}

func TestScan_AWaiverWithNoReasonIsNotAWaiver(t *testing.T) {
	root, files := writeTree(t, map[string]string{
		"bare.md":       "Measured on fleethost. <!-- lab-string-ok: -->\n",
		"whitespace.md": "Measured on fleethost. # lab-string-ok:    \n",
	})
	findings, err := ScanWith(root, files, nil, fixturePatterns(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("want 2 findings (one per file), got %d: %v", len(findings), findings)
	}
}

func TestScan_AnAllowRuleAcceptsOnlyTheClassesItNames(t *testing.T) {
	content := "host fleet-node-04 under " + ownerPathFixture + "/sources\n"
	root, files := writeTree(t, map[string]string{"pkg/thing_test.go": content})

	rules := []AllowRule{{
		Path:    "**/*_test.go",
		Classes: []Class{ClassLabHost},
		Reason:  "test fixtures may name a fleet host",
	}}
	findings, err := ScanWith(root, files, rules, fixturePatterns(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want exactly the owner-path finding, got %v", findings)
	}
	if findings[0].Class != ClassOwnerPath {
		t.Errorf("class = %s, want %s", findings[0].Class, ClassOwnerPath)
	}
}

func TestScan_RejectsAnUnusableAllowList(t *testing.T) {
	root, files := writeTree(t, map[string]string{"clean.txt": "nothing here\n"})
	rules := []AllowRule{{Path: "**/*.go", Classes: []Class{ClassLabHost}}}

	_, err := ScanWith(root, files, rules, fixturePatterns(t))
	var ruleErr *AllowRuleError
	if !errors.As(err, &ruleErr) {
		t.Fatalf("err = %v, want *AllowRuleError", err)
	}
	if !strings.Contains(ruleErr.Error(), "no reason given") {
		t.Errorf("error does not name the defect: %v", ruleErr)
	}
}

func TestScan_SkipsWhatCannotLeakText(t *testing.T) {
	root, files := writeTree(t, map[string]string{
		"binary.bin":  "fleethost\x00more bytes\n",
		"present.txt": "clean\n",
	})
	files = append(files, "deleted-from-the-working-tree.txt")

	findings, err := ScanWith(root, files, nil, fixturePatterns(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %v", findings)
	}
}

func TestScan_ReadsASymlinkTarget(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "go-quicbaas")
	if err := os.Symlink("/mnt/example-share/go-quicbaas/", link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	findings, err := ScanWith(root, []string{"go-quicbaas"}, nil, fixturePatterns(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var sawOwnerPath bool
	for _, f := range findings {
		if f.Class == ClassOwnerPath {
			sawOwnerPath = true
		}
		if f.Path != "go-quicbaas" {
			t.Errorf("finding path = %q, want the link itself", f.Path)
		}
	}
	if !sawOwnerPath {
		t.Fatalf("the link target was not scanned: %v", findings)
	}
}

func TestScan_AFileTooLargeToBeSourceIsSkipped(t *testing.T) {
	root := t.TempDir()
	name := "huge.txt"
	full := filepath.Join(root, name)
	body := make([]byte, maxFileSize+1)
	for i := range body {
		body[i] = 'a'
	}
	copy(body, []byte("fleethost"))
	if err := os.WriteFile(full, body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	findings, err := ScanWith(root, []string{name}, nil, fixturePatterns(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected the oversized file to be skipped, got %v", findings)
	}
}

func TestErrors_NameTheirSubjectAndUnwrap(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		contains []string
		wrapped  error
	}{
		{
			name:     "list failure names the root and what git said",
			err:      &ListError{Root: "/srv/tree", Stderr: "not a git repository", Err: os.ErrNotExist},
			contains: []string{"/srv/tree", "not a git repository"},
			wrapped:  os.ErrNotExist,
		},
		{
			name:     "read failure names the file",
			err:      &ReadError{Path: "pkg/x/y.go", Err: os.ErrPermission},
			contains: []string{"pkg/x/y.go"},
			wrapped:  os.ErrPermission,
		},
		{
			name:     "allow-rule failure names the entry and the defect",
			err:      &AllowRuleError{Index: 3, Path: "pkg/**", Problem: "no reason given"},
			contains: []string{"entry 3", "pkg/**", "no reason given"},
		},
		{
			name:     "pattern failure names the line and the defect",
			err:      &PatternError{Line: 12, Problem: PatternBadRegex, Code: "missing closing )"},
			contains: []string{"pattern line 12", string(PatternBadRegex), "missing closing )"},
		},
		{
			name:     "no patterns names the variable",
			err:      &NoPatternsError{Variable: PatternsEnv},
			contains: []string{PatternsEnv, "no patterns"},
		},
		{
			name:     "no patterns passed by the caller",
			err:      &NoPatternsError{},
			contains: []string{"no patterns"},
		},
		{
			name:     "report failure names the cause",
			err:      &ReportError{Err: os.ErrClosed},
			contains: []string{"cannot write report"},
			wrapped:  os.ErrClosed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, want := range tc.contains {
				if !strings.Contains(tc.err.Error(), want) {
					t.Errorf("%q omits %q", tc.err.Error(), want)
				}
			}
			if tc.wrapped != nil && !errors.Is(tc.err, tc.wrapped) {
				t.Errorf("%v does not unwrap to %v", tc.err, tc.wrapped)
			}
		})
	}
}

func TestReport_SaysWhatToDoAndOnlyWhenDirty(t *testing.T) {
	var clean strings.Builder
	dirty, err := Report(&clean, nil)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if dirty {
		t.Error("dirty = true for no findings")
	}
	if clean.String() != "" {
		t.Errorf("clean report wrote %q", clean.String())
	}

	var out strings.Builder
	dirty, err = Report(&out, []Finding{{
		Path:     "cmd/quicbaas/thing.go",
		Line:     7,
		Class:    ClassLabHost,
		Match:    "fleet-node-04",
		Excerpt:  "// observed on fleet-node-04",
		Guidance: "name the role",
	}})
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !dirty {
		t.Error("dirty = false with a finding")
	}
	for _, want := range []string{"cmd/quicbaas/thing.go:7", "fleet-node-04", "name the role", waiverPrefix} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report omits %q:\n%s", want, out.String())
		}
	}
}

// failingWriter fails every write, so a report that cannot reach the engineer is
// an error rather than a silently passing gate.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestReport_AWriteFailureIsATypedError(t *testing.T) {
	dirty, err := Report(failingWriter{}, []Finding{{Path: "x", Line: 1, Class: ClassLabHost}})
	if !dirty {
		t.Error("dirty = false although findings were passed")
	}
	var reportErr *ReportError
	if !errors.As(err, &reportErr) {
		t.Fatalf("err = %v, want *ReportError", err)
	}
	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("err does not unwrap to the write failure: %v", err)
	}
}

func TestTrackedFiles_ListsTheIndexAndRefusesADirectoryWithNoIndex(t *testing.T) {
	root := repoRoot(t)
	files, err := TrackedFiles(root)
	if err != nil {
		t.Fatalf("TrackedFiles: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no tracked files reported for this repository")
	}
	var sawSelf bool
	for _, f := range files {
		if f == "pkg/labstrings/labstrings.go" {
			sawSelf = true
		}
		if strings.HasPrefix(f, "/") {
			t.Errorf("path is not relative to the root: %q", f)
		}
	}
	if !sawSelf {
		t.Error("the gate's own source is not in the reported index")
	}

	// A temporary directory is not a work tree. Scanning it must fail loudly:
	// a gate that cannot enumerate must not report a clean tree.
	if _, err := TrackedFiles(t.TempDir()); err == nil {
		t.Fatal("TrackedFiles succeeded outside a git work tree")
	} else {
		var listErr *ListError
		if !errors.As(err, &listErr) {
			t.Fatalf("err = %v, want *ListError", err)
		}
		if listErr.Root == "" {
			t.Error("ListError does not name the root it was given")
		}
	}
}

func TestScan_AMinifiedLineIsQuotedBounded(t *testing.T) {
	long := "const x=\"" + strings.Repeat("padding;", 200) + "fleethost\";"
	root, files := writeTree(t, map[string]string{"dist/bundle.js": long + "\n"})

	findings, err := ScanWith(root, files, nil, fixturePatterns(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("want one finding, got %v", len(findings))
	}
	excerpt := findings[0].Excerpt
	if len(excerpt) > excerptLimit+len(" ...") {
		t.Errorf("excerpt is %d bytes; a report of these would be unreadable", len(excerpt))
	}
	if !strings.HasSuffix(excerpt, " ...") {
		t.Errorf("a truncated excerpt does not say so: %q", excerpt)
	}
}

// TestScan_AFileWhoseEveryClassIsAllowedIsNeverRead pins the shortcut for a
// file whose allow-list entries accept every class the patterns can report. The
// probe path runs through a regular file, so reaching the filesystem for it is a
// ReadError (not a directory) rather than a skipped absent file: a clean result
// proves the file was not read, and the control proves the probe would fail.
func TestScan_AFileWhoseEveryClassIsAllowedIsNeverRead(t *testing.T) {
	every := make([]Class, 0, len(classGuidance))
	for c := range classGuidance {
		every = append(every, c)
	}
	hostsOnly, err := ParsePatterns("lab-host\t" + `\bfleethost\b`)
	if err != nil {
		t.Fatalf("ParsePatterns: %v", err)
	}

	cases := []struct {
		name     string
		accepts  []Class
		patterns []Pattern
		wantRead bool
	}{
		{name: "every class accepted", accepts: every, patterns: fixturePatterns(t)},
		{name: "every class the patterns report accepted", accepts: []Class{ClassLabHost}, patterns: hostsOnly},
		{name: "control: one reportable class not accepted", accepts: []Class{ClassLabHost}, patterns: fixturePatterns(t), wantRead: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := writeTree(t, map[string]string{"vendored": "a regular file, not a directory\n"})
			rules := []AllowRule{{
				Path:    "vendored/**",
				Classes: tc.accepts,
				Reason:  "a tree this repository does not author and does not publish edits to",
			}}
			findings, err := ScanWith(root, []string{"vendored/thing.js"}, rules, tc.patterns)
			if tc.wantRead {
				var readErr *ReadError
				if !errors.As(err, &readErr) {
					t.Fatalf("err = %v, want the *ReadError that proves the probe reads", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ScanWith read a file it had no reason to: %v", err)
			}
			if len(findings) != 0 {
				t.Fatalf("expected no findings, got %v", findings)
			}
		})
	}
}

// TestScan_ReportsAValueOnceByItsMostSpecificRule pins the span dedupe: the
// account name inside an owner path is one leak, reported as the path. The
// fixture lists the account rule first, so this also pins ParsePatterns moving
// it last.
func TestScan_ReportsAValueOnceByItsMostSpecificRule(t *testing.T) {
	root, files := writeTree(t, map[string]string{"docs/a.md": "cd " + ownerPathFixture + "/sources, ask example-owner\n"})
	findings, err := ScanWith(root, files, nil, fixturePatterns(t))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	var classes []Class
	for _, f := range findings {
		classes = append(classes, f.Class)
	}
	want := []Class{ClassOwnerPath, ClassOwnerAccount}
	if len(classes) != len(want) || classes[0] != want[0] || classes[1] != want[1] {
		t.Fatalf("classes = %v, want %v (the path once, the bare name once)", classes, want)
	}
}
