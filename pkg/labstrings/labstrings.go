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
// releasable: it reports every tracked file that names a host, an address or a
// filesystem path belonging to the private development fleet.
//
// WHY THIS IS A GATE AND NOT A CONVENTION. go-quicbaas ships publicly — a
// module zip, a release tarball, two container images and an embedded web UI
// built from the tree. Anything committed here is published unless
// .gitattributes marks it export-ignore (see PublishedFiles). The first audit
// of the tree found a symlink into the lab's NFS mount at the repo root, the
// private forge in the release workflow's REGISTRY, the private forge in seven
// documentation links, the private registry in captured CI logs, the owner's
// forge principal baked into both shipped UI bundles, and lab hosts in `--help`
// text and exported godoc. None of it was load-bearing and nothing prevented
// any of it. This is what prevents the repeat.
//
// WHERE THE RULES LIVE. Not here. A public repository cannot carry the values it
// refuses to publish, not even as the patterns that refuse them, so this
// package holds the machinery and no rule. The rules are a private pattern file
// handed to the gate at run time in PatternsEnv, one `class<TAB>regex` line per
// rule (see ParsePatterns). The CI step "Releasability (private patterns)"
// reads the same text and greps the whole tracked tree with it; this package
// is the same check with line-level reporting, waivers and an allow list, for
// running locally against a working tree. Without the variable there is
// nothing to look for, and the gate says so (NoPatternsError) rather than
// reporting a clean tree. The package's own tests use reference values only:
// RFC 5737 addresses and names nobody owns.
//
// WHAT IS REPORTED, and why each class is a leak rather than a style problem:
//
//   - ClassLabAddress — an address on the fleet's subnet. Unreachable for every
//     public reader, so a copy-pasteable example containing one is broken
//     documentation, and it publishes the operator's network layout.
//   - ClassLabHost — a fleet hostname. Same, plus it names the machines an
//     attacker would go looking for.
//   - ClassOwnerPath — a path under the owner's home directory or the lab's NFS
//     mount. Meaningless off that one machine, and it leaks the account name.
//   - ClassOwnerAccount — the owner's account name on its own. It identifies a
//     person, and it is how the paths above get rebuilt by hand.
//   - ClassCoordHost — a name from the fleet's coordination services: the
//     forge and registry endpoints, the agent accounts that use them, and the
//     variables that carry those accounts' credentials. It maps the build
//     infrastructure and names the accounts worth phishing.
//   - ClassInternalEmail — a mail address on an internal domain. It names an
//     account and a domain nobody outside the fleet can reach.
//
// WHAT TO DO WITH A FINDING. In order of preference:
//
//   - Use a documentation value: RFC 5737 addresses (192.0.2.0/24,
//     198.51.100.0/24, 203.0.113.0/24), RFC 2606 names (example.com,
//     *.example, *.invalid), or the repository's own conventions —
//     `quicbaas.example.com:8443` for a server, `agent-01` for a daemon,
//     `*.internal` for an unrouted service.
//   - Move the value out of the tree: a forge address belongs in a CI
//     repository variable, a fleet address in the private infra repository.
//   - If the value IS the point — provenance for a benchmark, the host a
//     captured log came from — waive the line in place (below). Note that the
//     CI step greps the whole tracked tree and honours neither waivers nor the
//     allow list, so a value one of its patterns covers fails there whatever
//     this package accepts.
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
// (`./pkg/... ./cmd/... ./scripts/...`), so with PatternsEnv set the gate runs
// on every unit-test pass with no Makefile target to remember. The scan is
// exported, so a command wrapper is a thin main whenever one is wanted.
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
	"sort"
	"strings"
)

// Class groups the findings by what the matched text leaks. It is a string so a
// report, an allow-list entry, a pattern file and a test name all spell it the
// same way.
type Class string

const (
	// ClassLabAddress is an IP address on the development fleet's subnet.
	ClassLabAddress Class = "lab-address"

	// ClassLabHost is a hostname belonging to the development fleet.
	ClassLabHost Class = "lab-host"

	// ClassOwnerPath is a filesystem path that exists only on the owner's
	// machine or on the lab's network mount.
	ClassOwnerPath Class = "owner-path"

	// ClassOwnerAccount is the owner's account name outside a path.
	ClassOwnerAccount Class = "owner-account"

	// ClassCoordHost is a name from the fleet's coordination services: a forge
	// or registry endpoint, an agent account, or a credential variable.
	ClassCoordHost Class = "coord-host"

	// ClassInternalEmail is a mail address on an internal domain.
	ClassInternalEmail Class = "internal-email"
)

// classGuidance is every class the gate knows, with what to do instead of the
// value. It is the one list of classes: the pattern parser and the allow-list
// validator both look a class up here, so a class cannot be valid in one and
// unknown in the other. The advice is per class because the rules are private
// and a rule's own advice could not be written down next to it.
var classGuidance = map[Class]string{
	ClassLabAddress:    "use an RFC 5737 documentation address (198.51.100.0/24) or a name under .example.com",
	ClassLabHost:       "name the role, not the machine: agent-01, ci-runner-01, gpu-host.internal, quicbaas.example.com",
	ClassOwnerPath:     "make the path relative to the repository root, or use /var/lib/quicbaas, /srv, /home/agent",
	ClassOwnerAccount:  "name a role, not a person: operator, agent, or the project's own module path",
	ClassCoordHost:     "use a reference endpoint (forge.example.com, registry.example:5000) and a role account (company-agent); name a credential variable for what it holds (FORGE_READ_TOKEN)",
	ClassInternalEmail: "use an RFC 2606 address: agent@example.com",
}

// waiverPrefix marks a line whose lab value is the point of the line. The text
// after it is the reason and must not be empty.
const waiverPrefix = "lab-string-ok:"

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

// ScanRepo is the gate: it reads the rules from PatternsEnv, enumerates the
// published files under root (see PublishedFiles) and reports every one that
// names a lab value and is neither waived in place nor covered by
// DefaultAllowList. The rules are read first, so a gate with nothing to look
// for fails before it lists anything.
func ScanRepo(root string) ([]Finding, error) {
	patterns, err := PatternsFromEnv()
	if err != nil {
		return nil, err
	}
	files, err := PublishedFiles(root)
	if err != nil {
		return nil, err
	}
	return ScanWith(root, files, DefaultAllowList, patterns)
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

// Scan is ScanWith using the rules in PatternsEnv.
func Scan(root string, files []string, rules []AllowRule) ([]Finding, error) {
	patterns, err := PatternsFromEnv()
	if err != nil {
		return nil, err
	}
	return ScanWith(root, files, rules, patterns)
}

// ScanWith reports the matches of patterns in the named files, which are
// interpreted relative to root. The allow list is validated first: an
// allow-list that does not say why it allows something is a defect in the
// gate, not a pass. An empty pattern set is a *NoPatternsError, because a scan
// that looks for nothing finds nothing and that is not a clean tree.
//
// A file listed in the index but absent from the working tree is skipped — a
// deletion in progress has no content to scan — as is a file that is binary or
// larger than maxFileSize. A file whose allow-list entries accept every class
// the patterns can report is not read at all.
func ScanWith(root string, files []string, rules []AllowRule, patterns []Pattern) ([]Finding, error) {
	if err := ValidateAllowList(rules); err != nil {
		return nil, err
	}
	if len(patterns) == 0 {
		return nil, &NoPatternsError{}
	}
	reportable := make(map[Class]bool, len(classGuidance))
	for _, p := range patterns {
		reportable[p.Class] = true
	}
	var findings []Finding
	for _, name := range files {
		accepted := acceptedClasses(rules, name)
		if acceptsAll(accepted, reportable) {
			continue
		}
		content, ok, err := readCandidate(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		findings = append(findings, scanContent(name, content, accepted, patterns)...)
	}
	return findings, nil
}

// acceptsAll reports whether every class in reportable is accepted.
func acceptsAll(accepted, reportable map[Class]bool) bool {
	for c := range reportable {
		if !accepted[c] {
			return false
		}
	}
	return true
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

// scanContent applies every pattern whose class is not accepted for this file,
// and whose sieve the content passes, to every line of it. Patterns are tried
// in order and a span inside one already reported on the line is skipped, which
// is what makes the order of the rules matter (see ParsePatterns). A zero-width
// match quotes nothing and is not a finding.
func scanContent(name string, content []byte, accepted map[Class]bool, patterns []Pattern) []Finding {
	live := candidates(content, patterns, accepted)
	if len(live) == 0 {
		return nil
	}
	lines := strings.Split(string(content), "\n")
	var findings []Finding
	for i, line := range lines {
		if waived(line) || (i > 0 && waived(lines[i-1])) {
			continue
		}
		var reported [][]int
		for _, p := range live {
			for _, span := range p.Expr.FindAllStringIndex(line, -1) {
				if span[0] == span[1] || within(span, reported) {
					continue
				}
				reported = append(reported, span)
				findings = append(findings, Finding{
					Path:     name,
					Line:     i + 1,
					Class:    p.Class,
					Match:    line[span[0]:span[1]],
					Excerpt:  excerpt(line),
					Guidance: p.Guidance,
				})
			}
		}
	}
	return findings
}

// within reports whether span lies inside one of the spans already reported on
// its line, so one value is reported once, by the most specific rule.
func within(span []int, reported [][]int) bool {
	for _, r := range reported {
		if span[0] >= r[0] && span[1] <= r[1] {
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
	b.WriteString("a `" + waiverPrefix + " <reason>` comment. See pkg/labstrings for what each class means.\n")
	if _, err := io.WriteString(w, b.String()); err != nil {
		return true, &ReportError{Err: err}
	}
	return true, nil
}
