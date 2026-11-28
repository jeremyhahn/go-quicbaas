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

package buildgates

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	docsDir = filepath.Join(repoRoot, "docs")

	// docsPlansDir holds one working plan per row issue. Plans are working
	// records, not documentation, so the banner and link rules skip them.
	docsPlansDir = filepath.Join(docsDir, "plans")

	bannerStampRe  = regexp.MustCompile(`Last (verified|audited): \d{4}-\d{2}-\d{2}`)
	indexLinkRowRe = regexp.MustCompile("^\\| \\[([^/\\]]+)/\\]\\(([^)/]+)/\\)")
	indexCodeRowRe = regexp.MustCompile("^\\| `([^/`]+)/`")
	mdLinkRe       = regexp.MustCompile(`\]\(\s*<?([^)\s>]*)>?`)
	urlSchemeRe    = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

// docFiles returns every Markdown file under docs/ outside docs/plans/,
// failing when there are none so the checks cannot pass over an empty set.
func docFiles(t *testing.T) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(docsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == docsPlansDir {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", docsDir, err)
	}
	if len(paths) == 0 {
		t.Fatalf("found no Markdown files in %s; discovery has broken", docsDir)
	}
	return paths
}

// docBanner returns the blockquote lines that follow a doc's first `# `
// title, and whether the doc has a title at all.
func docBanner(body string) ([]string, bool) {
	lines := strings.Split(body, "\n")
	i := 0
	for i < len(lines) && !strings.HasPrefix(lines[i], "# ") {
		i++
	}
	if i == len(lines) {
		return nil, false
	}
	i++
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	var banner []string
	for i < len(lines) && strings.HasPrefix(lines[i], ">") {
		banner = append(banner, lines[i])
		i++
	}
	return banner, true
}

// TestEveryDocCarriesAStatusBanner enforces the handbook's documentation
// rules: every doc opens, after its title, with a status banner carrying a
// last-verified date stamp.
func TestEveryDocCarriesAStatusBanner(t *testing.T) {
	for _, path := range docFiles(t) {
		banner, titled := docBanner(readFile(t, path))
		if !titled {
			t.Errorf("%s: no `# ` title line", path)
			continue
		}
		if len(banner) == 0 || !strings.HasPrefix(banner[0], "> **Status: ") {
			t.Errorf("%s: no status banner after the title", path)
			continue
		}
		if !bannerStampRe.MatchString(strings.Join(banner, "\n")) {
			t.Errorf("%s: banner has no Last verified: YYYY-MM-DD stamp", path)
		}
	}
}

// TestEveryDocsFolderIsIndexed requires docs/README.md to carry a row for
// every directory directly under docs/, and every folder row to name a
// directory that exists.
func TestEveryDocsFolderIsIndexed(t *testing.T) {
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		t.Fatalf("read %s: %v", docsDir, err)
	}
	folders := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			folders[e.Name()] = true
		}
	}
	if len(folders) == 0 {
		t.Fatalf("found no folders in %s; discovery has broken", docsDir)
	}

	readme := filepath.Join(docsDir, "README.md")
	indexed := map[string]bool{}
	for _, line := range strings.Split(readFile(t, readme), "\n") {
		if m := indexLinkRowRe.FindStringSubmatch(line); m != nil && m[1] == m[2] {
			indexed[m[1]] = true
		} else if m := indexCodeRowRe.FindStringSubmatch(line); m != nil {
			indexed[m[1]] = true
		}
	}

	var names []string
	for name := range folders {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !indexed[name] {
			t.Errorf("%s: folder %s/ is not indexed", readme, name)
		}
	}
	names = names[:0]
	for name := range indexed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !folders[name] {
			t.Errorf("%s: row %s/ names no folder under docs/", readme, name)
		}
	}
}

// stripFences drops fenced code blocks, whose bracketed text is not a link.
func stripFences(body string) string {
	var kept []string
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if !inFence {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// TestDocsInRepoLinksResolve requires every relative link in a doc that stays
// inside the repository to name a path that exists.
func TestDocsInRepoLinksResolve(t *testing.T) {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("resolve %s: %v", repoRoot, err)
	}
	for _, path := range docFiles(t) {
		dir := filepath.Dir(path)
		for _, m := range mdLinkRe.FindAllStringSubmatch(stripFences(readFile(t, path)), -1) {
			target, _, _ := strings.Cut(m[1], "#")
			if target == "" || strings.HasPrefix(m[1], "#") || strings.HasPrefix(target, "/") || urlSchemeRe.MatchString(target) {
				continue
			}
			resolved, err := filepath.Abs(filepath.Join(dir, target))
			if err != nil {
				t.Errorf("%s: link %s: %v", path, target, err)
				continue
			}
			if rel, err := filepath.Rel(root, resolved); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf("%s: link %s does not resolve", path, target)
			}
		}
	}
}
