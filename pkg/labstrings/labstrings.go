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

// Package labstrings is the release gate that keeps this repository publicly
// releasable: it reports every published file that names a host, an address,
// an account or a filesystem path belonging to a private development network.
//
// WHY THIS IS A GATE AND NOT A CONVENTION. go-quicbaas ships publicly — a
// module zip, a release tarball, two container images and an embedded web UI
// built from the tree. Anything committed here is published unless
// .gitattributes marks it export-ignore (see PublishedFiles). go-quicbaas#944
// audited the tree and found a symlink into a private network mount at the
// repo root, a private forge in the release workflow, in documentation links
// and in captured CI logs, a personal account baked into both shipped UI
// bundles, and private hosts in `--help` text and exported godoc. None of it
// was load-bearing and nothing prevented any of it. This is what prevents the
// repeat.
//
// WHAT IS REPORTED, and why each class is a leak rather than a style problem:
//
//   - ClassLabAddress — an address on a private network. Unreachable for
//     every public reader, so a copy-pasteable example containing one is
//     broken documentation, and it publishes the operator's network layout.
//   - ClassLabHost — a private hostname. Same, plus it names the machines an
//     attacker would go looking for.
//   - ClassOwnerPath — a path under a person's home directory or a private
//     network mount. Meaningless off that one machine, and it leaks the
//     account name.
//   - ClassOwnerAccount — a person's account name on its own. It identifies a
//     person, and it is how the paths above get rebuilt by hand.
//
// TWO PATTERN SETS. DefaultPatterns is public and holds for any fork: home
// directories, the home-network address range and the home-network host
// suffixes. The values that identify one deployment — its subnet, its
// hostnames, its owner's account, its mounts — must never be written into a
// published file, including this package, so they live in a private JSON file
// named by the PatternsEnv environment variable and are added to the default
// set (see LoadPatterns and ConfiguredPatterns). The Makefile exports the
// variable when a `.labstrings-patterns.json` sits at the repository root;
// that file is ignored by git and by every image build context.
//
// WHAT TO DO WITH A FINDING. In order of preference:
//
//   - Use a documentation value: RFC 5737 addresses (192.0.2.0/24,
//     198.51.100.0/24, 203.0.113.0/24), RFC 2606 names (example.com,
//     *.example, *.invalid), or the repository's own conventions —
//     `quicbaas.example.com:8443` for a server, `agent-01` for a daemon,
//     `*.internal` for an unrouted service, `/home/agent` for a home.
//   - Move the value out of the tree: a forge address belongs in a CI
//     repository variable, a fleet address in private infrastructure
//     configuration.
//   - If the value IS the point — provenance for a benchmark, the host a
//     captured log came from — waive the line in place (below).
//
// TWO WAYS TO ACCEPT A LINE, both explicit:
//
//   - An inline waiver: the text `lab-string-ok: <reason>` on the offending
//     line or the line immediately above it, in whatever comment syntax the
//     file uses. The justification then lives next to the value it explains.
//     This is the same mechanism scripts/localkeygen and scripts/codedsentinel
//     use, and it is preferred. An empty reason is not a waiver.
//   - An entry in DefaultAllowList, for whole trees and for files that cannot
//     carry a comment (minified bundles, captured logs, JSON). Each entry
//     carries the classes it accepts and a reason; see allowlist.go.
//
// WHERE THIS LIVES. The sibling gates are commands under scripts/. This one is
// a package under pkg/ because that is the tree `make test` walks
// (`./pkg/... ./cmd/... ./scripts/...`), so the gate runs on every unit-test
// pass with no Makefile target to remember and no target list to fall off. The
// scan is exported, so a command wrapper is a thin main whenever one is wanted.
//
// RESOLUTION is textual, deliberately: it needs no build tags, no module
// loading and no parser, so it runs on every tree state including one that does
// not compile, and it covers Markdown, YAML, Dockerfiles, shell, Python and
// minified JavaScript as readily as Go.
package labstrings

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Class groups the findings by what the matched text leaks. It is a string so a
// report, an allow-list entry and a test name all spell it the same way.
type Class string

const (
	// ClassLabAddress is an IP address on a private development network.
	ClassLabAddress Class = "lab-address"

	// ClassLabHost is a hostname belonging to a private development network.
	ClassLabHost Class = "lab-host"

	// ClassOwnerPath is a filesystem path that exists only on one person's
	// machine or on a private network mount.
	ClassOwnerPath Class = "owner-path"

	// ClassOwnerAccount is a person's account name outside a path.
	ClassOwnerAccount Class = "owner-account"
)

// waiverPrefix marks a line whose lab value is the point of the line. The text
// after it is the reason and must not be empty.
const waiverPrefix = "lab-string-ok:"

// Pattern is one rule the scanner applies to every line. Build one with
// NewPattern, which derives Needle and Guidance.
type Pattern struct {
	// Class says what a match leaks.
	Class Class

	// Expr matches the offending text. It matches the value, not the whole
	// line, so the report can quote exactly what has to change.
	Expr *regexp.Regexp

	// Needle is a lowercase literal that every string Expr can match must
	// contain. It is the cheap precondition the sieve uses: a substring
	// search runs at memory bandwidth where this regexp runs at a few
	// megabytes a second, and the whole tree has to be read on every unit
	// test pass. A Needle that is not implied by Expr would make the gate
	// silently blind, which is why NewPattern derives it from Expr.
	Needle string

	// Except lists matches that are not findings: values the pattern's
	// shape covers but that are deliberately neutral.
	Except []string

	// Guidance is what the engineer who tripped this rule should do instead.
	Guidance string
}

// ruleSet is a pattern set prepared for one scan.
type ruleSet struct {
	patterns []Pattern

	// needles is every pattern's Needle, in the form the sieve wants them.
	needles [][]byte

	// classes is every class some pattern reports.
	classes map[Class]bool
}

func newRuleSet(patterns []Pattern) ruleSet {
	rs := ruleSet{
		patterns: patterns,
		needles:  make([][]byte, 0, len(patterns)),
		classes:  make(map[Class]bool, len(classGuidance)),
	}
	for _, p := range patterns {
		rs.needles = append(rs.needles, []byte(p.Needle))
		rs.classes[p.Class] = true
	}
	return rs
}

// mayContainLabValue reports whether any pattern could possibly match inside
// content. Almost no file in this tree contains a lab value, and 30 MB of
// tracked content is read on every unit-test pass: settling "there is nothing
// here" has to be cheap, and a substring search is two orders of magnitude
// cheaper than the patterns themselves. Case is folded because a hostname is
// as likely to appear shouted in a log line as lowercased in a config file.
func (rs ruleSet) mayContainLabValue(content []byte) bool {
	folded := bytes.ToLower(content)
	for _, needle := range rs.needles {
		if bytes.Contains(folded, needle) {
			return true
		}
	}
	return false
}

// allAccepted reports whether accepted covers every class the set reports, in
// which case reading the file cannot produce a finding.
func (rs ruleSet) allAccepted(accepted map[Class]bool) bool {
	for class := range rs.classes {
		if !accepted[class] {
			return false
		}
	}
	return true
}

// Finding is one line that must change before this tree can be published.
type Finding struct {
	// Path is slash-separated and relative to the scan root.
	Path string

	// Line is 1-based, so the report is clickable.
	Line int

	// Class says what the matched text leaks.
	Class Class

	// Match is the offending text exactly as it appears.
	Match string

	// Excerpt is the whole line, trimmed and truncated: a finding with no
	// context around it costs the reader a second trip to the file.
	Excerpt string

	// Guidance is the pattern's remediation advice.
	Guidance string
}

// String renders a finding in the file:line:message form every editor and every
// CI log viewer already knows how to jump to.
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: [%s] %q -- %s\n\t%s", f.Path, f.Line, f.Class, f.Match, f.Guidance, f.Excerpt)
}

// excerptLimit bounds the quoted line. Minified bundles are single lines of
// more than a megabyte; a report is useless if one finding fills the screen.
const excerptLimit = 160

// maxFileSize bounds what the scanner will read into memory. Nothing shipped in
// this tree is larger, and a scan must not be the thing that runs a CI runner
// out of memory.
const maxFileSize = 32 << 20

// binarySniff is how much of a file is examined for a NUL byte before deciding
// it is binary. It matches what git itself uses.
const binarySniff = 8000

// ScanRepo is the gate: it enumerates the published files under root (see
// PublishedFiles) and reports every one that matches ConfiguredPatterns and is
// neither waived in place nor covered by DefaultAllowList.
func ScanRepo(root string) ([]Finding, error) {
	patterns, err := ConfiguredPatterns(root)
	if err != nil {
		return nil, err
	}
	files, err := PublishedFiles(root)
	if err != nil {
		return nil, err
	}
	return Scan(root, files, patterns, DefaultAllowList)
}

// TrackedFiles lists the files git has in the index at root, slash-separated and
// relative to it. The index is where the published manifest starts: untracked
// working-tree litter is nobody's release problem, and PublishedFiles removes
// what .gitattributes keeps out of every archive.
func TrackedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z", "--cached")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, &ListError{Root: root, Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}
	var files []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}

// Scan reports the values patterns match in the named files, which are
// interpreted relative to root. Rules are validated first: an allow-list that
// does not say why it allows something is a defect in the gate, not a pass.
//
// A file listed in the index but absent from the working tree is skipped — a
// deletion in progress has no content to scan — as is a file that is binary or
// larger than maxFileSize.
func Scan(root string, files []string, patterns []Pattern, rules []AllowRule) ([]Finding, error) {
	if err := ValidateAllowList(rules); err != nil {
		return nil, err
	}
	rs := newRuleSet(patterns)
	var findings []Finding
	for _, name := range files {
		accepted := acceptedClasses(rules, name)
		if rs.allAccepted(accepted) {
			continue
		}
		content, ok, err := readCandidate(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		findings = append(findings, rs.scanContent(name, content, accepted)...)
	}
	return findings, nil
}

// readCandidate reads a file the scanner may have to look at. The bool reports
// whether the content is worth scanning; a missing, oversized or binary file is
// not an error.
func readCandidate(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, &ReadError{Path: path, Err: err}
	}
	if !info.Mode().IsRegular() {
		// A symlink's own target is scanned as the link's content
		// below; anything else (a submodule gitlink, a device) has no
		// content in this tree.
		if info.Mode()&os.ModeSymlink == 0 {
			return nil, false, nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return nil, false, &ReadError{Path: path, Err: err}
		}
		return []byte(target), true, nil
	}
	if info.Size() > maxFileSize {
		return nil, false, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false, &ReadError{Path: path, Err: err}
	}
	if isBinary(content) {
		return nil, false, nil
	}
	return content, true, nil
}

// isBinary reports whether content looks like something no reader will ever read
// a lab hostname out of. A NUL byte in the first binarySniff bytes is git's own
// test and it is good enough here.
func isBinary(content []byte) bool {
	head := content
	if len(head) > binarySniff {
		head = head[:binarySniff]
	}
	return bytes.IndexByte(head, 0) >= 0
}

// scanContent applies every pattern whose class is not accepted for this file to
// every line of it.
func (rs ruleSet) scanContent(name string, content []byte, accepted map[Class]bool) []Finding {
	if !rs.mayContainLabValue(content) {
		return nil
	}
	lines := strings.Split(string(content), "\n")
	var findings []Finding
	for i, line := range lines {
		if waived(line) || (i > 0 && waived(lines[i-1])) {
			continue
		}
		matches := rs.lineMatches(line, accepted)
		for j, m := range matches {
			if covered(j, matches) {
				continue
			}
			findings = append(findings, Finding{
				Path:     name,
				Line:     i + 1,
				Class:    m.pattern.Class,
				Match:    line[m.start:m.end],
				Excerpt:  excerpt(line),
				Guidance: m.pattern.Guidance,
			})
		}
	}
	return findings
}

// lineMatch is one pattern's match on one line.
type lineMatch struct {
	start, end int
	pattern    *Pattern
}

// lineMatches collects every match on line of every pattern whose class is not
// accepted, in pattern order, leaving out the pattern's own exceptions.
func (rs ruleSet) lineMatches(line string, accepted map[Class]bool) []lineMatch {
	var matches []lineMatch
	for i := range rs.patterns {
		p := &rs.patterns[i]
		if accepted[p.Class] {
			continue
		}
		for _, span := range p.Expr.FindAllStringIndex(line, -1) {
			if slices.Contains(p.Except, line[span[0]:span[1]]) {
				continue
			}
			matches = append(matches, lineMatch{start: span[0], end: span[1], pattern: p})
		}
	}
	return matches
}

// covered reports whether matches[i] lies inside a longer match on its line, or
// repeats the span of an earlier one, so one value is reported once, by the
// rule that matched the most of it: the account name inside a home path is
// reported as the path. Pattern order cannot change the answer, so a private
// pattern file may list its entries in any order.
func covered(i int, matches []lineMatch) bool {
	m := matches[i]
	for j, other := range matches {
		if j == i || other.start > m.start || other.end < m.end {
			continue
		}
		sameSpan := other.start == m.start && other.end == m.end
		if !sameSpan || j < i {
			return true
		}
	}
	return false
}

// waived reports whether a line carries an inline waiver with a reason. The
// reason is what makes it a waiver: `lab-string-ok:` on its own says only that
// somebody wanted the gate to stop talking.
func waived(line string) bool {
	idx := strings.Index(line, waiverPrefix)
	if idx < 0 {
		return false
	}
	reason := strings.TrimSpace(line[idx+len(waiverPrefix):])
	// Trim the comment closers the host syntaxes need, so a reason that ends
	// at `-->` or `*/` is still a reason.
	for _, closer := range []string{"-->", "*/"} {
		reason = strings.TrimSpace(strings.TrimSuffix(reason, closer))
	}
	return reason != ""
}

// excerpt trims and bounds a line for the report.
func excerpt(line string) string {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) <= excerptLimit {
		return trimmed
	}
	return trimmed[:excerptLimit] + " ..."
}

// Report writes the findings in a form an engineer can act on without opening
// this file, and returns whether anything was written. Findings are the gate's
// product: a gate whose output does not say what to do next gets waived
// wholesale by the next person who trips it.
func Report(w io.Writer, findings []Finding) (bool, error) {
	if len(findings) == 0 {
		return false, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d lab value(s) in tracked files -- this tree ships publicly:\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "%s\n\n", f)
	}
	b.WriteString("Replace the value, move it to CI/infra configuration, or -- if the value\n")
	b.WriteString("is the point (benchmark provenance, a captured log) -- waive the line with\n")
	b.WriteString("a `" + waiverPrefix + " <reason>` comment. See pkg/labstrings for the full list.\n")
	if _, err := io.WriteString(w, b.String()); err != nil {
		return true, &ReportError{Err: err}
	}
	return true, nil
}
