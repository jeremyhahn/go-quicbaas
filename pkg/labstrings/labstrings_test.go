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

// privateFixture is a private pattern file as a deployment would write one. It
// names a network that does not exist: an RFC 5737 subnet, RFC 2606 hosts, a
// placeholder account and mount.
const privateFixture = `[
  {"class": "lab-address",   "expr": "\\b203\\.0\\.113\\.\\d{1,3}\\b"},
  {"class": "lab-host",      "expr": "(?i)\\bcore-\\d{2}(\\.example)?\\b"},
  {"class": "owner-path",    "expr": "/mnt/lab-share\\b"},
  {"class": "owner-account", "expr": "(?i)\\bjdoe\\b"}
]`

// writePatternFile writes content to a pattern file in a temporary directory
// and returns its path.
func writePatternFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "patterns.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write pattern file: %v", err)
	}
	return path
}

// configured returns the rule set a deployment carrying privateFixture runs
// with, loaded the way the gate loads it: through PatternsEnv.
func configured(t *testing.T) []Pattern {
	t.Helper()
	t.Setenv(PatternsEnv, writePatternFile(t, privateFixture))
	set, err := ConfiguredPatterns(t.TempDir())
	if err != nil {
		t.Fatalf("ConfiguredPatterns: %v", err)
	}
	return set
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
	set := configured(t)
	cases := []struct {
		name      string
		content   string
		wantClass Class
		wantMatch string
		wantLine  int
	}{
		{
			name:      "home-network address in help text",
			content:   "package main\n\nconst help = \"dial 192.168.7.51:8443\"\n",
			wantClass: ClassLabAddress,
			wantMatch: "192.168.7.51",
			wantLine:  3,
		},
		{
			name:      "private subnet address from the pattern file",
			content:   "  REGISTRY: 203.0.113.91:3001\n",
			wantClass: ClassLabAddress,
			wantMatch: "203.0.113.91",
			wantLine:  1,
		},
		{
			name:      "home-network hostname in a doc",
			content:   "# Runbook\n\nRun it on nas.lan first.\n",
			wantClass: ClassLabHost,
			wantMatch: "nas.lan",
			wantLine:  3,
		},
		{
			name:      "RFC 8375 home hostname in a config",
			content:   "server: printer.home.arpa\n",
			wantClass: ClassLabHost,
			wantMatch: "printer.home.arpa",
			wantLine:  1,
		},
		{
			name:      "private hostname from the pattern file, shouted in a log",
			content:   "2026-09-12T00:00:00Z CORE-02 reported online\n",
			wantClass: ClassLabHost,
			wantMatch: "CORE-02",
			wantLine:  1,
		},
		{
			name:      "personal home path in a doc",
			content:   "See /home/carol/src for the checkout.\n",
			wantClass: ClassOwnerPath,
			wantMatch: "/home/carol",
			wantLine:  1,
		},
		{
			name:      "macOS home path in a script",
			content:   "cd /Users/carol/src/go-quicbaas\n",
			wantClass: ClassOwnerPath,
			wantMatch: "/Users/carol/",
			wantLine:  1,
		},
		{
			name:      "private mount from the pattern file in a script default",
			content:   "QRDB=${QRDB:-/mnt/lab-share/go-qrdb}\n",
			wantClass: ClassOwnerPath,
			wantMatch: "/mnt/lab-share",
			wantLine:  1,
		},
		{
			name:      "account name from the pattern file in a comment",
			content:   "package x\n\n// TODO(jdoe): drop after the migration\n",
			wantClass: ClassOwnerAccount,
			wantMatch: "jdoe",
			wantLine:  3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, files := writeTree(t, map[string]string{"shipping/file.txt": tc.content})
			findings, err := Scan(root, files, set, nil)
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
	set := configured(t)
	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "documentation addresses outside the private subnet",
			content: "dial 198.51.100.7:8443 or 192.0.2.10\n",
		},
		{
			name:    "RFC 1918 literals outside the home-network range",
			content: "peers: 10.0.0.1, 172.16.0.4\n",
		},
		{
			name:    "the repository's own example names",
			content: "--server https://quicbaas.example.com:8443 --host-affinity agent-01\n",
		},
		{
			name:    "the home of a placeholder or service account",
			content: "HOME=/home/agent WORKDIR /home/softhsm cp x /home/u/.quicbaas/\n",
		},
		{
			name:    "a home segment inside a longer path",
			content: "http://www.example.org/iso/home/store/catalogue.htm\n",
		},
		{
			name:    "a Users segment that is not a macOS home",
			content: "getByRole('tab', { name: /Users/i }) // Roles/Users/Tenants/x\n",
		},
		{
			name:    "a word that merely contains a lan suffix",
			content: "the plan.language field, and cluster.local\n",
		},
		{
			name:    "a hostname that merely contains a private name as a substring",
			content: "hardcore-01 is not a host, and core-0 has no second digit\n",
		},
		{
			name:    "a waiver on the offending line",
			content: "Measured on core-01. <!-- lab-string-ok: measurement provenance -->\n",
		},
		{
			name:    "a waiver on the line above",
			content: "# lab-string-ok: measurement provenance\nMeasured on core-01.\n",
		},
		{
			name:    "a waiver whose reason ends at a comment closer",
			content: "x := 1 /* lab-string-ok: provenance */ // core-01\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, files := writeTree(t, map[string]string{"clean.txt": tc.content})
			findings, err := Scan(root, files, set, nil)
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if len(findings) != 0 {
				t.Fatalf("expected no findings, got %v", findings)
			}
		})
	}
}

// TestScan_ThePublicDefaultsAloneDoNotKnowThePrivateValues pins the split: a
// fork without the private file is not told what the private values are, and
// does not find them either.
func TestScan_ThePublicDefaultsAloneDoNotKnowThePrivateValues(t *testing.T) {
	root, files := writeTree(t, map[string]string{"notes.md": "core-01 at 203.0.113.9, ask jdoe\n"})
	findings, err := Scan(root, files, DefaultPatterns, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("the public defaults reported private values: %v", findings)
	}
}

func TestScan_AWaiverWithNoReasonIsNotAWaiver(t *testing.T) {
	root, files := writeTree(t, map[string]string{
		"bare.md":       "Measured on nas.lan. <!-- lab-string-ok: -->\n",
		"whitespace.md": "Measured on nas.lan. # lab-string-ok:    \n",
	})
	findings, err := Scan(root, files, DefaultPatterns, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("want 2 findings (one per file), got %d: %v", len(findings), findings)
	}
}

func TestScan_AnAllowRuleAcceptsOnlyTheClassesItNames(t *testing.T) {
	content := "host nas.lan under /home/carol/sources\n"
	root, files := writeTree(t, map[string]string{"pkg/thing_test.go": content})

	rules := []AllowRule{{
		Path:    "**/*_test.go",
		Classes: []Class{ClassLabHost},
		Reason:  "test fixtures may name a lab host",
	}}
	findings, err := Scan(root, files, DefaultPatterns, rules)
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

	_, err := Scan(root, files, DefaultPatterns, rules)
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
		"binary.bin":  "nas.lan\x00more bytes\n",
		"present.txt": "clean\n",
	})
	files = append(files, "deleted-from-the-working-tree.txt")

	findings, err := Scan(root, files, DefaultPatterns, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %v", findings)
	}
}

func TestScan_AnUnreadableFileIsATypedError(t *testing.T) {
	root, _ := writeTree(t, map[string]string{"blocker": "a regular file\n"})

	// "blocker" is a file, so a path beneath it fails with ENOTDIR: a real
	// I/O failure, not the absent file Scan skips.
	_, err := Scan(root, []string{"blocker/inner.txt"}, DefaultPatterns, nil)
	var readErr *ReadError
	if !errors.As(err, &readErr) {
		t.Fatalf("err = %v, want *ReadError", err)
	}
}

func TestScan_ReadsASymlinkTarget(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "go-quicbaas")
	if err := os.Symlink("/home/carol/src/go-quicbaas/", link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	findings, err := Scan(root, []string{"go-quicbaas"}, DefaultPatterns, nil)
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
	copy(body, []byte(" nas.lan"))
	if err := os.WriteFile(full, body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	findings, err := Scan(root, []string{name}, DefaultPatterns, nil)
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
		{
			name:     "pattern failure names the expression, the class and the defect",
			err:      &PatternError{Class: ClassLabHost, Expr: `core-(`, Problem: "invalid regexp", Err: os.ErrInvalid},
			contains: []string{`"core-("`, "lab-host", "invalid regexp"},
			wrapped:  os.ErrInvalid,
		},
		{
			name:     "pattern-file failure names the file",
			err:      &PatternFileError{Path: "/etc/labstrings.json", Err: os.ErrNotExist},
			contains: []string{"/etc/labstrings.json"},
			wrapped:  os.ErrNotExist,
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
		Match:    "core-04",
		Excerpt:  "// observed on core-04",
		Guidance: "name the role",
	}})
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !dirty {
		t.Error("dirty = false with a finding")
	}
	for _, want := range []string{"cmd/quicbaas/thing.go:7", "core-04", "name the role", waiverPrefix} {
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

// TestDefaultPatterns_EveryNeedleIsImpliedByItsExpr pins the sieve's invariant
// for the public set: a pattern whose Needle its own matches do not contain
// would be skipped for every file in the tree, and the gate would pass while
// seeing nothing.
func TestDefaultPatterns_EveryNeedleIsImpliedByItsExpr(t *testing.T) {
	// One or more strings per pattern that the pattern is known to match.
	samples := map[string][]string{
		`\B/home/[a-z_][a-z0-9_-]*`:      {"/home/carol/src", "HOME=/home/carol"},
		`\B/Users/[A-Za-z0-9_.-]+/`:      {"/Users/Carol/src", "'/Users/carol/'"},
		`\b192\.168\.\d{1,3}\.\d{1,3}\b`: {"at 192.168.7.51:8443", "192.168.0.1"},
		`(?i)\b[a-z0-9-]+\.lan\b`:        {"nas.lan", "http://NAS.LAN:8080"},
		`(?i)\b[a-z0-9-]+\.home\.arpa\b`: {"printer.home.arpa", "NAS.HOME.ARPA"},
	}

	for _, p := range DefaultPatterns {
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
	long := "const x=\"" + strings.Repeat("padding;", 200) + " nas.lan\";"
	root, files := writeTree(t, map[string]string{"dist/bundle.js": long + "\n"})

	findings, err := Scan(root, files, DefaultPatterns, nil)
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
	set := configured(t)
	rules := []AllowRule{{
		Path:    "vendored/**",
		Classes: []Class{ClassLabAddress, ClassLabHost, ClassOwnerPath, ClassOwnerAccount},
		Reason:  "a tree this repository does not author and does not publish edits to",
	}}

	// "vendored" is a regular file, so reading anything beneath it is a
	// ReadError: a clean result proves the shortcut never reached the disk.
	root, _ := writeTree(t, map[string]string{"vendored": "a regular file\n"})
	findings, err := Scan(root, []string{"vendored/thing.js"}, set, rules)
	if err != nil {
		t.Fatalf("Scan read a file every class of which is allowed: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %v", findings)
	}

	// One class short of every class the set reports, the file is read.
	rules[0].Classes = rules[0].Classes[:3]
	if _, err := Scan(root, []string{"vendored/thing.js"}, set, rules); err == nil {
		t.Fatal("Scan skipped a file whose owner-account class is not allowed")
	}
}

// TestScan_ReportsAValueOnceByItsLongestMatch pins the span dedupe: the account
// name inside a home path is one leak, reported as the path, whichever order
// the patterns come in.
func TestScan_ReportsAValueOnceByItsLongestMatch(t *testing.T) {
	set := configured(t)
	reversed := make([]Pattern, len(set))
	for i, p := range set {
		reversed[len(set)-1-i] = p
	}
	root, files := writeTree(t, map[string]string{"docs/a.md": "cd /home/jdoe/sources, ask jdoe\n"})

	for name, patterns := range map[string][]Pattern{"defaults first": set, "private first": reversed} {
		t.Run(name, func(t *testing.T) {
			findings, err := Scan(root, files, patterns, nil)
			if err != nil {
				t.Fatalf("Scan: %v", err)
			}
			got := map[Class]string{}
			for _, f := range findings {
				got[f.Class] = f.Match
			}
			if len(findings) != 2 || got[ClassOwnerPath] != "/home/jdoe" || got[ClassOwnerAccount] != "jdoe" {
				t.Fatalf("findings = %v, want the path once and the bare name once", findings)
			}
		})
	}
}
