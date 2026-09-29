// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestIsDevelopmentTooling(t *testing.T) {
	cases := map[string]bool{
		"CLAUDE.md":                      true,
		"AGENTS.md":                      true,
		"cmd/quicbaas/CLAUDE.md":         true,
		"test/integration/rag/AGENTS.md": true,
		".claude/agents/golang-pro.md":   true,
		".claude/skills/x/SKILL.md":      true,
		".serena/project.yml":            true,
		"sub/.serena/memories/notes.md":  true,
		"docs/CLAUDE.md.txt":             false,
		"docs/claude.md":                 false,
		"pkg/claude/client.go":           false,
		"README.md":                      false,
		".claude":                        false,
		"go.mod":                         false,
	}
	for name, want := range cases {
		if got := IsDevelopmentTooling(name); got != want {
			t.Errorf("IsDevelopmentTooling(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestThePublishedTreeCarriesNoDevelopmentTooling is the exclusion gate on this
// repository: every agent file stays tracked here for development, and none of
// them is in what gets published.
func TestThePublishedTreeCarriesNoDevelopmentTooling(t *testing.T) {
	published, err := PublishedFiles(repoRoot(t))
	if err != nil {
		t.Fatalf("PublishedFiles: %v", err)
	}
	for _, name := range published {
		if IsDevelopmentTooling(name) {
			t.Errorf("%s is published; add its path to .gitattributes as export-ignore", name)
		}
	}
	if !slices.Contains(published, "go.mod") {
		t.Fatal("the published manifest lost go.mod; the exclusion is too wide")
	}
}

// TestTheDockerBuildContextExcludesDevelopmentTooling covers the images, whose
// build context is the working tree rather than an archive.
func TestTheDockerBuildContextExcludesDevelopmentTooling(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".dockerignore"))
	if err != nil {
		t.Fatalf("reading .dockerignore: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	for _, want := range []string{"**/CLAUDE.md", "**/AGENTS.md", "**/.claude", "**/.serena"} {
		if !slices.Contains(lines, want) {
			t.Errorf(".dockerignore does not exclude %s", want)
		}
	}
}

// TestGitArchiveHonoursTheExclusionList proves the list does what it claims:
// a repository carrying this tree's own .gitattributes, archived the way a
// forge builds a release tarball, contains none of the development tooling,
// and PublishedFiles agrees with the archive.
func TestGitArchiveHonoursTheExclusionList(t *testing.T) {
	attrs, err := os.ReadFile(filepath.Join(repoRoot(t), ".gitattributes"))
	if err != nil {
		t.Fatalf("reading .gitattributes: %v", err)
	}
	root, _ := writeTree(t, map[string]string{
		".gitattributes":                  string(attrs),
		"go.mod":                          "module example.com/m\n",
		"pkg/a/a.go":                      "package a\n",
		"CLAUDE.md":                       "private\n",
		"AGENTS.md":                       "private\n",
		"pkg/a/CLAUDE.md":                 "private\n",
		".claude/agents/golang-pro.md":    "private\n",
		".serena/memories/notes.md":       "private\n",
		"test/rag/.serena/project.yml":    "private\n",
		"docs/claude-code-integration.md": "public\n",
	})
	git(t, root, "init", "-q")
	git(t, root, "add", "-A")
	git(t, root, "-c", "user.name=gate", "-c", "user.email=gate@example.com", "-c", "commit.gpgsign=false",
		"commit", "-q", "-m", "fixture")

	archived := archiveEntries(t, root)
	want := []string{".gitattributes", "docs/claude-code-integration.md", "go.mod", "pkg/a/a.go"}
	if !slices.Equal(archived, want) {
		t.Errorf("git archive contains %v, want %v", archived, want)
	}

	published, err := PublishedFiles(root)
	if err != nil {
		t.Fatalf("PublishedFiles: %v", err)
	}
	if !slices.Equal(published, want) {
		t.Errorf("PublishedFiles = %v, want the archive's %v", published, want)
	}
}

func TestPublishedFilesOutsideAWorkTreeIsAListError(t *testing.T) {
	_, err := PublishedFiles(t.TempDir())
	var listErr *ListError
	if !errors.As(err, &listErr) {
		t.Fatalf("PublishedFiles outside a work tree = %v, want *ListError", err)
	}
}

func TestExportIgnoredReportsAGitFailure(t *testing.T) {
	_, err := exportIgnored(filepath.Join(t.TempDir(), "absent"), []string{"go.mod"})
	var listErr *ListError
	if !errors.As(err, &listErr) {
		t.Fatalf("exportIgnored on a missing root = %v, want *ListError", err)
	}
}

// git runs one git command in dir, failing the test on error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// archiveEntries lists the regular files `git archive HEAD` produces, sorted.
func archiveEntries(t *testing.T, dir string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "archive", "--format=tar", "HEAD").Output()
	if err != nil {
		t.Fatalf("git archive: %v", err)
	}
	var names []string
	reader := tar.NewReader(bytes.NewReader(out))
	for {
		hdr, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("reading the archive: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			names = append(names, hdr.Name)
		}
	}
	slices.Sort(names)
	return names
}
