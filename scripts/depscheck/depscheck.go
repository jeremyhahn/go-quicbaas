package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"golang.org/x/mod/modfile"
)

// RecentReleaseWindow is how long after its release a newer version is only
// reported, not failed: a requirement behind a release younger than this has
// not yet had time to be adopted (#1704). A release exactly this old fails.
const RecentReleaseWindow = 7 * 24 * time.Hour

// listedModule is the subset of `go list -m -u -json` output the check reads.
type listedModule struct {
	Path    string
	Version string
	Main    bool
	Update  *struct {
		Version string
		Time    time.Time
	}
	Replace *struct{ Path string }
	Error   *struct{ Err string }
}

// UnresolvedError reports direct requirements whose latest release the go
// command could not look up, such as a private module with no credentials. The
// check cannot vouch for them, so it fails rather than passing them.
type UnresolvedError struct {
	ModFile string
	Modules []string
}

func (e *UnresolvedError) Error() string {
	return fmt.Sprintf("depscheck: %s: cannot look up the latest release of:\n  %s",
		e.ModFile, strings.Join(e.Modules, "\n  "))
}

// Behind is a direct requirement held below its latest release with no
// reason written beside it in go.mod. Released is the latest release's time,
// zero when the go command did not give one.
type Behind struct {
	Path     string
	Current  string
	Latest   string
	Released time.Time
}

func (b Behind) String() string {
	return b.Path + " " + b.Current + " -> " + b.Latest
}

// ReportedLine is the line `make deps-check` prints for a requirement behind
// a recent release: its versions, the release time and its age at now.
func (b Behind) ReportedLine(now time.Time) string {
	age := now.Sub(b.Released).Round(time.Minute)
	return "depscheck: " + b.String() + " released " + b.Released.UTC().Format(time.RFC3339) +
		" (" + age.String() + " ago), reported: under 7 days old"
}

// ModFileError reports a go.mod that cannot be parsed.
type ModFileError struct {
	Path string
	Err  error
}

func (e *ModFileError) Error() string {
	return "depscheck: parse " + e.Path + ": " + e.Err.Error()
}

func (e *ModFileError) Unwrap() error { return e.Err }

// ModuleListError reports `go list -m -u -json` output that cannot be decoded.
type ModuleListError struct {
	Err error
}

func (e *ModuleListError) Error() string {
	return "depscheck: decode go list output: " + e.Err.Error()
}

func (e *ModuleListError) Unwrap() error { return e.Err }

// BehindError reports the undocumented requirements the check found.
type BehindError struct {
	ModFile string
	Behind  []Behind
}

func (e *BehindError) Error() string {
	lines := make([]string, 0, len(e.Behind))
	for _, b := range e.Behind {
		lines = append(lines, "  "+b.String())
	}
	return fmt.Sprintf("depscheck: %s: %d requirement(s) behind the latest release without a reason in go.mod "+
		"(devops.md §9: upgrade, or write the reason as a comment on the require or replace line):\n%s",
		e.ModFile, len(e.Behind), strings.Join(lines, "\n"))
}

// Check compares the direct requirements of the go.mod at modPath (its
// content is modData) against the `go list -m -u -json all` stream in
// listing. A requirement with a newer release fails the check unless its
// require or replace line carries a comment, which is where devops.md §9
// puts the reason for a held version. Modules replaced by a local directory
// are the repository's own and are skipped.
//
// A requirement whose latest release is younger than RecentReleaseWindow at
// now does not fail: it is returned in the reported slice, sorted by path. A
// latest release with no time is treated as old, so missing data fails.
func Check(modPath string, modData []byte, listing io.Reader, now time.Time) ([]Behind, error) {
	mf, err := modfile.Parse(modPath, modData, nil)
	if err != nil {
		return nil, &ModFileError{Path: modPath, Err: err}
	}
	listed, err := decodeListing(listing)
	if err != nil {
		return nil, err
	}
	documented := replaceComments(mf)
	var behind, reported []Behind
	var unresolved []string
	for _, req := range mf.Require {
		if req.Indirect {
			continue
		}
		m, ok := listed[req.Mod.Path]
		if !ok || m.Main || isLocalReplace(m) {
			continue
		}
		if m.Error != nil {
			unresolved = append(unresolved, m.Path+": "+m.Error.Err)
			continue
		}
		if m.Update == nil {
			continue
		}
		if hasComment(req.Syntax) || documented[req.Mod.Path] {
			continue
		}
		b := Behind{Path: req.Mod.Path, Current: m.Version, Latest: m.Update.Version, Released: m.Update.Time}
		if isRecent(b.Released, now) {
			reported = append(reported, b)
			continue
		}
		behind = append(behind, b)
	}
	sortByPath(reported)
	var errs []error
	if len(behind) > 0 {
		sortByPath(behind)
		errs = append(errs, &BehindError{ModFile: modPath, Behind: behind})
	}
	if len(unresolved) > 0 {
		sort.Strings(unresolved)
		errs = append(errs, &UnresolvedError{ModFile: modPath, Modules: unresolved})
	}
	return reported, errors.Join(errs...)
}

// isRecent reports whether a release at released is younger than
// RecentReleaseWindow at now. A zero time is never recent, so a release the
// go command gave no time for fails.
func isRecent(released, now time.Time) bool {
	return !released.IsZero() && now.Sub(released) < RecentReleaseWindow
}

func sortByPath(behind []Behind) {
	sort.Slice(behind, func(i, j int) bool { return behind[i].Path < behind[j].Path })
}

func decodeListing(r io.Reader) (map[string]listedModule, error) {
	listed := make(map[string]listedModule)
	dec := json.NewDecoder(r)
	for {
		var m listedModule
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			return listed, nil
		}
		if err != nil {
			return nil, &ModuleListError{Err: err}
		}
		listed[m.Path] = m
	}
}

func replaceComments(mf *modfile.File) map[string]bool {
	documented := make(map[string]bool, len(mf.Replace))
	for _, rep := range mf.Replace {
		if hasComment(rep.Syntax) {
			documented[rep.Old.Path] = true
		}
	}
	return documented
}

func isLocalReplace(m listedModule) bool {
	return m.Replace != nil && (strings.HasPrefix(m.Replace.Path, ".") || strings.HasPrefix(m.Replace.Path, "/"))
}

// hasComment reports whether a go.mod line carries a comment of its own: one
// on the lines directly above it or after it on the same line. The `//
// indirect` marker the go command writes is not a reason.
func hasComment(line *modfile.Line) bool {
	if line == nil {
		return false
	}
	if len(line.Before) > 0 {
		return true
	}
	for _, c := range line.Suffix {
		if strings.TrimSpace(strings.TrimPrefix(c.Token, "//")) != "indirect" {
			return true
		}
	}
	return false
}
