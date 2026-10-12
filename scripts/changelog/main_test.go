package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const fixtureChangelog = `# Changelog

All notable changes to QuicBaaS will be documented in this file.

## [Unreleased]

## [0.14.0-alpha] - 2026-10-02

### Fixed

- An older fix.
`

const fixtureDate = "2026-10-12"

// newTree lays out a repository root holding CHANGELOG.md and changelog.d
// with its .gitkeep and the given fragments.
func newTree(t *testing.T, changelog string, fragments map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, fragmentDir)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		filepath.Join(root, changelogFile): changelog,
		filepath.Join(dir, keepFile):       "",
	}
	for name, text := range fragments {
		files[filepath.Join(dir, name)] = text
	}
	for path, text := range files {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// snapshot reads every file under root, keyed by its relative path.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[rel] = string(text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestAssemble_FoldsFragmentsIntoNewSection(t *testing.T) {
	root := newTree(t, fixtureChangelog, map[string]string{
		"10-later.md": "### Fixed\n\n- Ten fixed.\n",
		"9-multi.md":  "\n### Fixed\n\n- Nine fixed,\n  wrapped.\n\n### Added\n\n- Nine added.\n",
		"7-breaking.md": "### Changed — BREAKING: the wire format\n\n- Seven broke it.\n\n" +
			"### Changed\n\n- Seven changed.\n\n#### Detail\n\nKept as entry text.\n",
		"ops-notes.md": "### Security\n\n- Unnumbered.\n",
	})

	folded, err := assemble(root, "v0.15.0-alpha", fixtureDate)
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	if folded != 4 {
		t.Errorf("folded = %d, want 4", folded)
	}

	want := `# Changelog

All notable changes to QuicBaaS will be documented in this file.

## [Unreleased]

## [0.15.0-alpha] - 2026-10-12

### Added

- Nine added.

### Changed

- Seven changed.

#### Detail

Kept as entry text.

### Changed — BREAKING: the wire format

- Seven broke it.

### Fixed

- Nine fixed,
  wrapped.

- Ten fixed.

### Security

- Unnumbered.

## [0.14.0-alpha] - 2026-10-02

### Fixed

- An older fix.
`
	got := snapshot(t, root)
	wantFiles := map[string]string{
		changelogFile:                        want,
		filepath.Join(fragmentDir, keepFile): "",
	}
	if !reflect.DeepEqual(got, wantFiles) {
		t.Errorf("tree after assembly:\n%q\nwant:\n%q", got, wantFiles)
	}
}

func TestAssemble_CarriesUnreleasedText(t *testing.T) {
	changelog := strings.Replace(fixtureChangelog, "## [Unreleased]\n",
		"## [Unreleased]\n\n### Added\n\n- Written before fragments.\n", 1)
	root := newTree(t, changelog, nil)

	if _, err := assemble(root, "0.15.0", fixtureDate); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	got := snapshot(t, root)[changelogFile]
	want := "## [Unreleased]\n\n## [0.15.0] - 2026-10-12\n\n### Added\n\n- Written before fragments.\n\n## [0.14.0-alpha]"
	if !strings.Contains(got, want) {
		t.Errorf("CHANGELOG.md =\n%s\nwant it to contain\n%s", got, want)
	}
}

func TestAssemble_WithoutUnreleasedHeadingInsertsBeforeFirstVersion(t *testing.T) {
	changelog := strings.Replace(fixtureChangelog, "## [Unreleased]\n\n", "", 1)
	root := newTree(t, changelog, map[string]string{"1-a.md": "### Added\n\n- One.\n"})

	if _, err := assemble(root, "v0.15.0", fixtureDate); err != nil {
		t.Fatalf("assemble: %v", err)
	}
	got := snapshot(t, root)[changelogFile]
	want := "this file.\n\n## [0.15.0] - 2026-10-12\n\n### Added\n\n- One.\n\n## [0.14.0-alpha]"
	if !strings.Contains(got, want) {
		t.Errorf("CHANGELOG.md =\n%s\nwant it to contain\n%s", got, want)
	}
}

func TestAssemble_RefusesAndChangesNothing(t *testing.T) {
	good := map[string]string{"1-good.md": "### Added\n\n- Fine.\n"}
	withBad := func(name, text string) map[string]string {
		return map[string]string{"1-good.md": good["1-good.md"], name: text}
	}

	tests := []struct {
		name      string
		changelog string
		fragments map[string]string
		version   string
		date      string
		want      any
	}{
		{name: "empty version", fragments: good, version: "", want: new(*VersionError)},
		{name: "not semver", fragments: good, version: "v1.2", want: new(*VersionError)},
		{name: "duplicate version", fragments: good, version: "v0.14.0-alpha", want: new(*DuplicateVersionError)},
		{name: "bad date", fragments: good, version: "v0.15.0", date: "12/10/2026", want: new(*DateError)},
		{name: "text before heading", version: "v0.15.0", fragments: withBad("2-bad.md", "- An entry.\n\n### Added\n\n- x\n"), want: new(*FragmentError)},
		{name: "unknown type", version: "v0.15.0", fragments: withBad("2-bad.md", "### Improved\n\n- x\n"), want: new(*FragmentError)},
		{name: "breaking without summary", version: "v0.15.0", fragments: withBad("2-bad.md", "### Changed — BREAKING: \n\n- x\n"), want: new(*FragmentError)},
		{name: "version heading", version: "v0.15.0", fragments: withBad("2-bad.md", "### Added\n\n- x\n\n## [Unreleased]\n"), want: new(*FragmentError)},
		{name: "empty subsection", version: "v0.15.0", fragments: withBad("2-bad.md", "### Added\n\n### Fixed\n\n- x\n"), want: new(*FragmentError)},
		{name: "no heading", version: "v0.15.0", fragments: withBad("2-bad.md", "\n\n"), want: new(*FragmentError)},
		{name: "not markdown", version: "v0.15.0", fragments: withBad("2-bad.txt", "### Added\n\n- x\n"), want: new(*FragmentError)},
		{name: "nothing to release", version: "v0.15.0", want: new(*NothingToReleaseError)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changelog := tt.changelog
			if changelog == "" {
				changelog = fixtureChangelog
			}
			date := tt.date
			if date == "" {
				date = fixtureDate
			}
			root := newTree(t, changelog, tt.fragments)
			before := snapshot(t, root)

			_, err := assemble(root, tt.version, date)
			if err == nil {
				t.Fatal("assemble succeeded, want a refusal")
			}
			if !errors.As(err, tt.want) {
				t.Errorf("error = %T %v, want %T", err, err, tt.want)
			}
			if after := snapshot(t, root); !reflect.DeepEqual(after, before) {
				t.Errorf("refusal changed the tree:\n%q\nwas:\n%q", after, before)
			}
		})
	}
}

func TestAssemble_MissingChangelog(t *testing.T) {
	root := newTree(t, fixtureChangelog, nil)
	if err := os.Remove(filepath.Join(root, changelogFile)); err != nil {
		t.Fatal(err)
	}
	var fileErr *FileError
	if _, err := assemble(root, "v0.15.0", fixtureDate); !errors.As(err, &fileErr) {
		t.Errorf("error = %v, want *FileError", err)
	}
}

func TestFragmentError_NamesFileAndLine(t *testing.T) {
	_, err := parseFragment("changelog.d/2-bad.md", "### Added\n\n- x\n\n### Improved\n")
	want := `changelog.d/2-bad.md:5: heading "Improved" is not one of`
	if err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error = %v, want prefix %q", err, want)
	}
}

func TestFragmentLess(t *testing.T) {
	names := []string{"ops.md", "1266-b.md", "259-a.md", "1266-a.md", "abc.md"}
	sorted := append([]string(nil), names...)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if fragmentLess(sorted[j], sorted[i]) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	want := []string{"259-a.md", "1266-a.md", "1266-b.md", "abc.md", "ops.md"}
	if !reflect.DeepEqual(sorted, want) {
		t.Errorf("order = %v, want %v", sorted, want)
	}
}

func TestRun_ReportsReleaseAndRefusal(t *testing.T) {
	root := newTree(t, fixtureChangelog, map[string]string{"1-a.md": "### Added\n\n- One.\n"})
	args := []string{"-root", root, "-version", "v0.15.0", "-date", fixtureDate}

	var stdout, stderr bytes.Buffer
	if code := run(args, &stdout, &stderr); code != exitReleased {
		t.Fatalf("run = %d, stderr %q", code, stderr.String())
	}
	if want := "folded 1 fragment(s) into [0.15.0] - 2026-10-12"; !strings.Contains(stdout.String(), want) {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run(args, &stdout, &stderr); code != exitRefused {
		t.Fatalf("second run = %d, want %d", code, exitRefused)
	}
	if want := "already has a [0.15.0] section"; !strings.Contains(stderr.String(), want) {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRun_RejectsUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-bogus"}, &stdout, &stderr); code != exitRefused {
		t.Errorf("run = %d, want %d", code, exitRefused)
	}
}
