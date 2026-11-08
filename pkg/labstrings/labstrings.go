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
// .gitattributes marks it export-ignore (see PublishedFiles). go-quicbaas#944
// audited the tree and found a symlink into the lab's NFS mount at the repo
// root, the private forge in the release workflow's REGISTRY, the private forge
// in seven documentation links, the private registry in captured CI logs, the
// owner's Gitea principal baked into both shipped UI bundles, and lab hosts in
// `--help` text and exported godoc. None of it was load-bearing and nothing
// prevented any of it. This is what prevents the repeat.
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
	"sort"
	"strings"
)

// Class groups the findings by what the matched text leaks. It is a string so a
// report, an allow-list entry and a test name all spell it the same way.
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
)

// waiverPrefix marks a line whose lab value is the point of the line. The text
// after it is the reason and must not be empty.
const waiverPrefix = "lab-string-ok:"

// Pattern is one rule the scanner applies to every line.
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
	// silently blind, so TestPatterns_EveryNeedleIsImpliedByItsExpr pins it.
	Needle string

	// Guidance is what the engineer who tripped this rule should do instead.
	Guidance string
}

// patterns is the rule set. It is a package-level var rather than a constant
// list so a new fleet subnet or hostname is one line, and so the lab's own
// values are written down in exactly one place in this repository.
//
// Note what is NOT here: 192.168.0.0/16 at large. Tests across this tree use
// RFC 1918 literals as opaque addresses that are never dialed, and banning the
// whole space would bury the real findings under hundreds of them. The fleet
// subnet is named precisely instead.
var patterns = []Pattern{
	{
		Class:    ClassLabAddress,
		Expr:     regexp.MustCompile(`\b192\.168\.101\.\d{1,3}\b`),
		Needle:   "192.168.101.",
		Guidance: "use an RFC 5737 documentation address (198.51.100.0/24) or a name under .example.com",
	},
	{
		Class:    ClassLabHost,
		Expr:     regexp.MustCompile(`(?i)\bvstation-\d{2}\b`),
		Needle:   "vstation-",
		Guidance: "name the role, not the machine: agent-01, gpu-host.internal, quicbaas.example.com",
	},
	{
		Class:    ClassLabHost,
		Expr:     regexp.MustCompile(`(?i)\bdellemc-\d{2}\b`),
		Needle:   "dellemc-",
		Guidance: "a CI runner's own hostname is never load-bearing: ci-runner-01 reads the same",
	},
	{
		Class:    ClassLabHost,
		Expr:     regexp.MustCompile(`(?i)\bgodlike(\.internal)?\b`), // lab-string-ok: the hostname this rule exists to find
		Needle:   "gpu-host",
		Guidance: "name the role, not the machine: gpu-host.internal, npu-host.internal",
	},
	{
		Class:    ClassLabHost,
		Expr:     regexp.MustCompile(`(?i)\bomega\b`), // lab-string-ok: the hostname this rule exists to find
		Needle:   "omega",
		Guidance: "name the role, not the machine: agent-01, remote-agent.internal",
	},
	{
		Class:    ClassOwnerPath,
		Expr:     regexp.MustCompile(`/home/jhahn\b`), // lab-string-ok: the path prefix this rule exists to find
		Needle:   "/home/jhahn",
		Guidance: "make the path relative to the repository root, or use /var/lib, /srv, /home/agent",
	},
	{
		Class:    ClassOwnerPath,
		Expr:     regexp.MustCompile(`/mnt/vstation`), // lab-string-ok: the mount prefix this rule exists to find
		Needle:   "/mnt/vstation",
		Guidance: "the lab's NFS mount does not exist anywhere else; use a repository-relative path",
	},
	{
		Class:    ClassOwnerPath,
		Expr:     regexp.MustCompile(`quicbaas-burnin`), // lab-string-ok: the deployment directory this rule exists to find
		Needle:   "quicbaas-burnin",
		Guidance: "the burn-in node's deployment directory is private; use /var/lib/quicbaas or a repository-relative path",
	},
	{
		// Last on purpose: a match inside a span an earlier rule already
		// reported (the account inside an owner path) is not reported twice.
		Class:    ClassOwnerAccount,
		Expr:     regexp.MustCompile(`(?i)\bjhahn\b`), // lab-string-ok: the account name this rule exists to find
		Needle:   "jhahn",
		Guidance: "name a role, not a person: operator, agent, or the project's own module path",
	},
}

// sieveNeedles is every pattern's Needle, in the form the sieve wants them.
// Almost no file in this tree contains a lab value, and 30 MB of tracked
// content is read on every unit-test pass: settling "there is nothing here" has
// to be cheap, and a substring search is two orders of magnitude cheaper than
// the patterns themselves.
var sieveNeedles = func() [][]byte {
	needles := make([][]byte, 0, len(patterns))
	for _, p := range patterns {
		needles = append(needles, []byte(p.Needle))
	}
	return needles
}()

// mayContainLabValue reports whether any pattern could possibly match inside
// content. Case is folded because a hostname is as likely to appear shouted in a
// log line as lowercased in a config file.
func mayContainLabValue(content []byte) bool {
	folded := bytes.ToLower(content)
	for _, needle := range sieveNeedles {
		if bytes.Contains(folded, needle) {
			return true
		}
	}
	return false
}

// Finding is one line that must change before this tree can be published.
type Finding struct {
	// Path is slash-separated and relative to the scan root.
	Path string

	// Line is 1-based, so the report is clickable.
	Line int

	// Class says what the matched text leaks.
	Class Class

	// Match is the offending text exactly as it appears. It is for callers
	// and tests; String never prints it, because the value a private pattern
	// matched is as private as the pattern.
	Match string

	// Excerpt is the whole line, trimmed and truncated. String never prints
	// it, for the same reason as Match.
	Excerpt string

	// Guidance is the pattern's remediation advice.
	Guidance string
}

// String renders a finding in the file:line:message form every editor and every
// CI log viewer already knows how to jump to. It names the place, the class and
// the remedy, never the matched text: a CI log is public, and printing the
// value would publish the thing the gate exists to keep out.
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: [%s] -- %s", f.Path, f.Line, f.Class, f.Guidance)
}

// excerptLimit bounds Finding.Excerpt. Minified bundles are single lines of
// more than a megabyte; a finding must not carry one.
const excerptLimit = 160

// maxFileSize bounds what the scanner will read into memory. Nothing shipped in
// this tree is larger, and a scan must not be the thing that runs a CI runner
// out of memory.
const maxFileSize = 32 << 20

// binarySniff is how much of a file is examined for a NUL byte before deciding
// it is binary. It matches what git itself uses.
const binarySniff = 8000

// ScanRepo is the gate: it enumerates the published files under root (see
// PublishedFiles) and reports every one that names a lab value and is neither
// waived in place nor covered by DefaultAllowList.
func ScanRepo(root string) ([]Finding, error) {
	files, err := PublishedFiles(root)
	if err != nil {
		return nil, err
	}
	return Scan(root, files, DefaultAllowList)
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

// Scan reports the lab values in the named files, which are interpreted relative
// to root. Rules are validated first: an allow-list that does not say why it
// allows something is a defect in the gate, not a pass.
//
// A file listed in the index but absent from the working tree is skipped — a
// deletion in progress has no content to scan — as is a file that is binary or
// larger than maxFileSize.
func Scan(root string, files []string, rules []AllowRule) ([]Finding, error) {
	if err := ValidateAllowList(rules); err != nil {
		return nil, err
	}
	var findings []Finding
	for _, name := range files {
		accepted := acceptedClasses(rules, name)
		if len(accepted) == len(patterns) {
			// Every class this gate knows about is allowed here;
			// reading the file cannot produce a finding.
			continue
		}
		content, ok, err := readCandidate(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		findings = append(findings, scanContent(name, content, accepted)...)
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
func scanContent(name string, content []byte, accepted map[Class]bool) []Finding {
	if !mayContainLabValue(content) {
		return nil
	}
	lines := strings.Split(string(content), "\n")
	var findings []Finding
	for i, line := range lines {
		if waived(line) || (i > 0 && waived(lines[i-1])) {
			continue
		}
		var reported [][]int
		for _, p := range patterns {
			if accepted[p.Class] {
				continue
			}
			for _, span := range p.Expr.FindAllStringIndex(line, -1) {
				if within(span, reported) {
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
	b.WriteString("a `" + waiverPrefix + " <reason>` comment. See pkg/labstrings for the full list.\n")
	if _, err := io.WriteString(w, b.String()); err != nil {
		return true, &ReportError{Err: err}
	}
	return true, nil
}
