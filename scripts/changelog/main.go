// Command changelog folds the changelog.d fragments into a new version
// section of CHANGELOG.md and empties changelog.d, keeping its .gitkeep.
//
// It is the pre-tag release step of handbook release-versioning §5: a pull
// request adds changelog.d/<issue>-<slug>.md holding only its entry under the
// Keep a Changelog subsection headings it lands under, and the release commit
// concatenates them. release.yml does not run it.
//
//	go run ./scripts/changelog -version v0.15.0-alpha
//
// Every check runs before anything is written: a missing or malformed
// version, a version CHANGELOG.md already has, a malformed fragment, or
// nothing to release refuses with the reason and leaves the tree as it was.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	exitReleased = 0
	exitRefused  = 1

	fragmentDir   = "changelog.d"
	changelogFile = "CHANGELOG.md"
	keepFile      = ".gitkeep"
	unreleased    = "## [Unreleased]"

	// breakingMarker joins a subsection type and its summary in a breaking
	// change heading: "### Changed — BREAKING: <summary>" (§5).
	breakingMarker = " — BREAKING: "
)

// sectionTypes are the Keep a Changelog 1.1.0 change types, in the order the
// assembled section lists them.
var sectionTypes = []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"}

// semver matches a SemVer 2.0.0 version with an optional leading "v".
var semver = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// leadingNumber is the issue number a fragment name starts with.
var leadingNumber = regexp.MustCompile(`^[0-9]+`)

// VersionError reports a -version that is empty or not a SemVer version.
type VersionError struct {
	Value string
}

func (e *VersionError) Error() string {
	if e.Value == "" {
		return "no version given: run make changelog VERSION=vX.Y.Z"
	}
	return fmt.Sprintf("version %q is not a SemVer version (vX.Y.Z[-pre])", e.Value)
}

// DateError reports a -date that is not YYYY-MM-DD.
type DateError struct {
	Value string
}

func (e *DateError) Error() string {
	return fmt.Sprintf("release date %q is not YYYY-MM-DD", e.Value)
}

// DuplicateVersionError reports a version CHANGELOG.md already has a section for.
type DuplicateVersionError struct {
	Version string
}

func (e *DuplicateVersionError) Error() string {
	return fmt.Sprintf("%s already has a [%s] section", changelogFile, e.Version)
}

// FragmentError reports a changelog.d entry the assembly cannot fold.
type FragmentError struct {
	Path   string
	Line   int
	Reason string
}

func (e *FragmentError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.Path, e.Line, e.Reason)
	}
	return fmt.Sprintf("%s: %s", e.Path, e.Reason)
}

// NothingToReleaseError reports a release with no fragments and an empty
// [Unreleased] section: every released version gets an entry (§5).
type NothingToReleaseError struct{}

func (e *NothingToReleaseError) Error() string {
	return fmt.Sprintf("nothing to release: %s holds no fragments and %s is empty", fragmentDir, unreleased)
}

// FileError reports a file the assembly could not read or write.
type FileError struct {
	Op   string
	Path string
	Err  error
}

func (e *FileError) Error() string { return fmt.Sprintf("cannot %s %s: %v", e.Op, e.Path, e.Err) }

func (e *FileError) Unwrap() error { return e.Err }

// fragment is one changelog.d file, split into its subsections.
type fragment struct {
	path     string
	sections []section
}

// section is one "### " heading and the entry text under it.
type section struct {
	heading string
	body    string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("changelog", flag.ContinueOnError)
	fs.SetOutput(stderr)
	version := fs.String("version", "", "version to release, vX.Y.Z[-pre]")
	root := fs.String("root", ".", "repository root holding CHANGELOG.md and changelog.d")
	date := fs.String("date", time.Now().UTC().Format(time.DateOnly), "release date, YYYY-MM-DD")
	if err := fs.Parse(args); err != nil {
		return exitRefused
	}

	folded, err := assemble(*root, *version, *date)
	if err != nil {
		// forbidigo forbids fmt.Print*; this tool is a CLI whose output IS its
		// product, so it writes to the stream directly.
		fmt.Fprintln(stderr, "changelog:", err)
		return exitRefused
	}
	fmt.Fprintf(stdout, "changelog: folded %d fragment(s) into [%s] - %s\n",
		folded, strings.TrimPrefix(*version, "v"), *date)
	return exitReleased
}

// assemble writes the new section into CHANGELOG.md and removes the folded
// fragments. It returns the number of fragments folded.
func assemble(root, version, date string) (int, error) {
	if !semver.MatchString(version) {
		return 0, &VersionError{Value: version}
	}
	version = strings.TrimPrefix(version, "v")
	if _, err := time.Parse(time.DateOnly, date); err != nil {
		return 0, &DateError{Value: date}
	}

	changelogPath := filepath.Join(root, changelogFile)
	text, err := os.ReadFile(changelogPath)
	if err != nil {
		return 0, &FileError{Op: "read", Path: changelogPath, Err: err}
	}
	lines := strings.Split(string(text), "\n")
	if hasVersion(lines, version) {
		return 0, &DuplicateVersionError{Version: version}
	}

	dir := filepath.Join(root, fragmentDir)
	fragments, err := readFragments(dir)
	if err != nil {
		return 0, err
	}

	start, end := unreleasedSpan(lines)
	carried := ""
	if start >= 0 {
		carried = trimBlank(lines[start+1 : end])
	}
	if len(fragments) == 0 && carried == "" {
		return 0, &NothingToReleaseError{}
	}

	release := renderSection(version, date, carried, fragments)
	updated := insertSection(lines, start, end, release)
	info, err := os.Stat(changelogPath)
	if err != nil {
		return 0, &FileError{Op: "stat", Path: changelogPath, Err: err}
	}
	if err := os.WriteFile(changelogPath, []byte(updated), info.Mode().Perm()); err != nil {
		return 0, &FileError{Op: "write", Path: changelogPath, Err: err}
	}
	for _, f := range fragments {
		if err := os.Remove(f.path); err != nil {
			return 0, &FileError{Op: "remove", Path: f.path, Err: err}
		}
	}
	return len(fragments), nil
}

// hasVersion reports whether a "## [version]" heading already exists.
func hasVersion(lines []string, version string) bool {
	heading := "## [" + version + "]"
	for _, line := range lines {
		if line == heading || strings.HasPrefix(line, heading+" ") {
			return true
		}
	}
	return false
}

// readFragments parses every changelog.d/*.md file, ordered by the issue
// number its name starts with, then by name. Anything else in the directory
// except .gitkeep is refused rather than silently left behind.
func readFragments(dir string) ([]fragment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, &FileError{Op: "read", Path: dir, Err: err}
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(dir, name)
		switch {
		case name == keepFile:
			continue
		case entry.IsDir():
			return nil, &FragmentError{Path: path, Reason: "is a directory; fragments are flat .md files"}
		case filepath.Ext(name) != ".md":
			return nil, &FragmentError{Path: path, Reason: "is not a .md fragment"}
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return fragmentLess(names[i], names[j]) })

	fragments := make([]fragment, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		text, err := os.ReadFile(path)
		if err != nil {
			return nil, &FileError{Op: "read", Path: path, Err: err}
		}
		sections, err := parseFragment(path, string(text))
		if err != nil {
			return nil, err
		}
		fragments = append(fragments, fragment{path: path, sections: sections})
	}
	return fragments, nil
}

// fragmentLess orders fragment names by leading issue number, then by name.
func fragmentLess(a, b string) bool {
	na, aok := issueNumber(a)
	nb, bok := issueNumber(b)
	if aok && bok && na != nb {
		return na < nb
	}
	if aok != bok {
		return aok
	}
	return a < b
}

func issueNumber(name string) (int, bool) {
	digits := leadingNumber.FindString(name)
	if digits == "" {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	return n, err == nil
}

// parseFragment splits a fragment into its "### <Type>" subsections. The
// fragment must open with such a heading, every subsection must hold an
// entry, and a "#" or "##" heading, which would break the section structure
// of CHANGELOG.md, is refused.
func parseFragment(path, text string) ([]section, error) {
	var sections []section
	var body []string
	headingLine := 0
	flush := func() error {
		if len(sections) == 0 {
			return nil
		}
		entry := trimBlank(body)
		if entry == "" {
			return &FragmentError{Path: path, Line: headingLine, Reason: "subsection has no entry"}
		}
		sections[len(sections)-1].body = entry
		return nil
	}

	for i, line := range strings.Split(text, "\n") {
		lineNo := i + 1
		if strings.HasPrefix(line, "### ") {
			if err := flush(); err != nil {
				return nil, err
			}
			heading := strings.TrimSpace(strings.TrimPrefix(line, "### "))
			if !validHeading(heading) {
				return nil, &FragmentError{Path: path, Line: lineNo, Reason: fmt.Sprintf(
					"heading %q is not one of %s, optionally %q<summary>",
					heading, strings.Join(sectionTypes, ", "), "Changed"+breakingMarker)}
			}
			sections = append(sections, section{heading: heading})
			headingLine = lineNo
			body = nil
			continue
		}
		if isTopHeading(line) {
			return nil, &FragmentError{Path: path, Line: lineNo, Reason: "a fragment may hold only ### subsection headings"}
		}
		if len(sections) == 0 {
			if strings.TrimSpace(line) != "" {
				return nil, &FragmentError{Path: path, Line: lineNo, Reason: "text before the first ### subsection heading"}
			}
			continue
		}
		body = append(body, line)
	}
	if len(sections) == 0 {
		return nil, &FragmentError{Path: path, Reason: "no ### subsection heading (Added, Changed, Fixed, ...)"}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return sections, nil
}

// isTopHeading reports a "#" or "##" Markdown heading.
func isTopHeading(line string) bool {
	return line == "#" || line == "##" || strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ")
}

// validHeading accepts a change type, or a change type followed by the
// breaking-change marker and a summary.
func validHeading(heading string) bool {
	kind, summary, breaking := strings.Cut(heading, breakingMarker)
	if breaking && strings.TrimSpace(summary) == "" {
		return false
	}
	return typeRank(kind) >= 0
}

// typeRank is the position of a change type in sectionTypes, or -1.
func typeRank(kind string) int {
	for i, t := range sectionTypes {
		if t == kind {
			return i
		}
	}
	return -1
}

// headingLess orders headings by change type; within one type the plain
// heading comes first and breaking headings follow in name order.
func headingLess(a, b string) bool {
	ka, _, aBreaking := strings.Cut(a, breakingMarker)
	kb, _, bBreaking := strings.Cut(b, breakingMarker)
	if ra, rb := typeRank(ka), typeRank(kb); ra != rb {
		return ra < rb
	}
	if aBreaking != bBreaking {
		return !aBreaking
	}
	return a < b
}

// renderSection builds "## [version] - date", the carried [Unreleased] text,
// then one subsection per heading holding every fragment's entry under it in
// fragment order.
func renderSection(version, date, carried string, fragments []fragment) string {
	entries := map[string][]string{}
	var headings []string
	for _, f := range fragments {
		for _, s := range f.sections {
			if _, seen := entries[s.heading]; !seen {
				headings = append(headings, s.heading)
			}
			entries[s.heading] = append(entries[s.heading], s.body)
		}
	}
	sort.SliceStable(headings, func(i, j int) bool { return headingLess(headings[i], headings[j]) })

	var b strings.Builder
	fmt.Fprintf(&b, "## [%s] - %s\n", version, date)
	if carried != "" {
		b.WriteString("\n" + carried + "\n")
	}
	for _, heading := range headings {
		b.WriteString("\n### " + heading + "\n")
		for _, entry := range entries[heading] {
			b.WriteString("\n" + entry + "\n")
		}
	}
	return b.String()
}

// unreleasedSpan returns the line index of the [Unreleased] heading and the
// index of the next "## " heading (or len(lines)); start is -1 without one.
func unreleasedSpan(lines []string) (start, end int) {
	start = -1
	for i, line := range lines {
		if strings.TrimSpace(line) == unreleased {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, -1
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return start, end
}

// insertSection places the release section under an emptied [Unreleased]
// heading, or before the first version heading when there is none.
func insertSection(lines []string, start, end int, release string) string {
	var head, tail []string
	if start >= 0 {
		head = append(head, lines[:start+1]...)
		head = append(head, "")
		tail = lines[end:]
	} else {
		at := len(lines)
		for i, line := range lines {
			if strings.HasPrefix(line, "## [") {
				at = i
				break
			}
		}
		head = lines[:at]
		tail = lines[at:]
	}

	var b strings.Builder
	for _, line := range head {
		b.WriteString(line + "\n")
	}
	b.WriteString(release)
	rest := strings.Join(tail, "\n")
	if strings.TrimSpace(rest) == "" {
		return b.String()
	}
	b.WriteString("\n" + rest)
	return b.String()
}

// trimBlank joins lines with leading and trailing blank lines removed.
func trimBlank(lines []string) string {
	first, last := 0, len(lines)
	for first < last && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	for last > first && strings.TrimSpace(lines[last-1]) == "" {
		last--
	}
	return strings.Join(lines[first:last], "\n")
}
