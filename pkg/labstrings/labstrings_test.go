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

// ownerPathFixture is the owner home prefix this gate bans, assembled at runtime
// so the fixture does not itself have to be waived on every line that uses it.
var ownerPathFixture = "/home/" + "jhahn" // lab-string-ok: fixture for the owner-path rule

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
			content:   "package main\n\nconst help = \"dial 198.51.100.51:8443\"\n",
			wantClass: ClassLabAddress,
			wantMatch: "198.51.100.51",
			wantLine:  3,
		},
		{
			name:      "fleet hostname in a doc",
			content:   "# Runbook\n\nRun it on gpu-host first.\n",
			wantClass: ClassLabHost,
			wantMatch: "gpu-host",
			wantLine:  3,
		},
		{
			name:      "runner hostname in a captured log",
			content:   "2026-09-12T00:00:00Z ci-node-2(version:v3.1.0)\n",
			wantClass: ClassLabHost,
			wantMatch: "ci-node-2",
			wantLine:  1,
		},
		{
			name:      "agent hostname in a comment",
			content:   "// observed on vm-host-4\n",
			wantClass: ClassLabHost,
			wantMatch: "vm-host-4",
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
			name:      "lab NFS mount in a script default",
			content:   "QRDB=${QRDB:-/mnt/" + "vstation" + "-01/go-qrdb}\n", // lab-string-ok: fixture for the owner-path rule
			wantClass: ClassOwnerPath,
			wantMatch: "/mnt/" + "vstation",
			wantLine:  1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, files := writeTree(t, map[string]string{"shipping/file.txt": tc.content})
			findings, err := Scan(root, files, nil)
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
		})
	}
}

func TestScan_AcceptsWhatItShould(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "documentation addresses",
			content: "dial 198.51.100.7:8443 or 192.0.2.10 or 203.0.113.4\n",
		},
		{
			name:    "RFC 1918 literals that are not the fleet subnet",
			content: "peers: 192.168.1.10, 192.168.100.4, 10.0.0.1\n",
		},
		{
			name:    "the repository's own example names",
			content: "--server https://quicbaas.example.com:8443 --host-affinity agent-01\n",
		},
		{
			name:    "a waiver on the offending line",
			content: "Measured on gpu-host. <!-- lab-string-ok: measurement provenance -->\n",
		},
		{
			name:    "a waiver on the line above",
			content: "# lab-string-ok: measurement provenance\nMeasured on gpu-host.\n",
		},
		{
			name:    "a waiver whose reason ends at a comment closer",
			content: "x := 1 /* lab-string-ok: provenance */ // gpu-host\n",
		},
		{
			name:    "a hostname that merely contains a fleet name as a substring",
			content: "ungodlikeness is not a host, and vstation-0 has no second digit\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, files := writeTree(t, map[string]string{"clean.txt": tc.content})
			findings, err := Scan(root, files, nil)
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
		"bare.md":       "Measured on gpu-host. <!-- lab-string-ok: -->\n",
		"whitespace.md": "Measured on gpu-host. # lab-string-ok:    \n",
	})
	findings, err := Scan(root, files, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("want 2 findings (one per file), got %d: %v", len(findings), findings)
	}
}

func TestScan_AnAllowRuleAcceptsOnlyTheClassesItNames(t *testing.T) {
	content := "host vm-host-4 under " + ownerPathFixture + "/sources\n"
	root, files := writeTree(t, map[string]string{"pkg/thing_test.go": content})

	rules := []AllowRule{{
		Path:    "**/*_test.go",
		Classes: []Class{ClassLabHost},
		Reason:  "test fixtures may name a fleet host",
	}}
	findings, err := Scan(root, files, rules)
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

	_, err := Scan(root, files, rules)
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
		"binary.bin":  "gpu-host\x00more bytes\n",
		"present.txt": "clean\n",
	})
	files = append(files, "deleted-from-the-working-tree.txt")

	findings, err := Scan(root, files, nil)
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
	if err := os.Symlink("/mnt/"+"vstation"+"-01/go-quicbaas/", link); err != nil { // lab-string-ok: fixture for the owner-path rule
		t.Fatalf("symlink: %v", err)
	}

	findings, err := Scan(root, []string{"go-quicbaas"}, nil)
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
	copy(body, []byte("gpu-host"))
	if err := os.WriteFile(full, body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	findings, err := Scan(root, []string{name}, nil)
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
		Match:    "vm-host-4",
		Excerpt:  "// observed on vm-host-4",
		Guidance: "name the role",
	}})
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !dirty {
		t.Error("dirty = false with a finding")
	}
	for _, want := range []string{"cmd/quicbaas/thing.go:7", "vm-host-4", "name the role", waiverPrefix} {
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

// TestPatterns_EveryNeedleIsImpliedByItsExpr pins the sieve's invariant: a
// pattern whose Needle its own matches do not contain would be skipped for every
// file in the tree, and the gate would pass while seeing nothing.
func TestPatterns_EveryNeedleIsImpliedByItsExpr(t *testing.T) {
	// One string per pattern that the pattern is known to match. They are the
	// values the audit actually found, so this doubles as the record of what
	// each rule was written for.
	samples := map[string][]string{
		`\b192\.168\.101\.\d{1,3}\b`:   {"198.51.100.91:3001", "at 198.51.100.51:8443"},
		`(?i)\bvstation-\d{2}\b`:       {"agent-vm-host-4", "vm-host-1 rebooted"},
		`(?i)\bdellemc-\d{2}\b`:        {"runner ci-node-2", "ci-node-1"},
		`(?i)\bgodlike(\.internal)?\b`: {"on gpu-host", "http://gpu-host.internal:8077"},
		`/home/jhahn\b`:                {ownerPathFixture + "/sources"},        // lab-string-ok: sample for the owner-path rule
		`/mnt/vstation`:                {"/mnt/" + "vstation" + "-01/go-qrdb"}, // lab-string-ok: sample for the owner-path rule
	}

	for _, p := range patterns {
		t.Run(p.Expr.String(), func(t *testing.T) {
			if p.Needle == "" {
				t.Fatal("pattern has no sieve needle")
			}
			if p.Needle != strings.ToLower(p.Needle) {
				t.Errorf("needle %q is not lowercase; the sieve folds case before searching", p.Needle)
			}
			got, ok := samples[p.Expr.String()]
			if !ok {
				t.Fatalf("no sample for this pattern: add one so its needle stays honest")
			}
			for _, sample := range got {
				if !p.Expr.MatchString(sample) {
					t.Errorf("%q does not match its own sample %q", p.Expr, sample)
				}
				if !strings.Contains(strings.ToLower(sample), p.Needle) {
					t.Errorf("sample %q matches but does not contain needle %q: the sieve would skip it", sample, p.Needle)
				}
			}
		})
	}
}

func TestScan_AMinifiedLineIsQuotedBounded(t *testing.T) {
	long := "const x=\"" + strings.Repeat("padding;", 200) + "gpu-host\";"
	root, files := writeTree(t, map[string]string{"dist/bundle.js": long + "\n"})

	findings, err := Scan(root, files, nil)
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

func TestScan_AFileWhoseEveryClassIsAllowedIsNeverRead(t *testing.T) {
	every := make([]Class, 0, len(patterns))
	seen := map[Class]bool{}
	for _, p := range patterns {
		if !seen[p.Class] {
			seen[p.Class] = true
			every = append(every, p.Class)
		}
	}
	rules := []AllowRule{{
		Path:    "vendored/**",
		Classes: every,
		Reason:  "a tree this repository does not author and does not publish edits to",
	}}

	// The file does not exist: reaching the filesystem at all would be a
	// ReadError, so a clean result proves the shortcut skipped it.
	findings, err := Scan(t.TempDir(), []string{"vendored/thing.js"}, rules)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %v", findings)
	}
}
