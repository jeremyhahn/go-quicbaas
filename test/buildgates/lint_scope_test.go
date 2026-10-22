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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// parentPath matches a `..` path component: `../x`, `x/..`, or a bare `..`
// argument such as `cd ..`. The go tool's `./...` pattern does not match,
// because its dots follow a dot or run on past two.
var parentPath = regexp.MustCompile(`(^|[^A-Za-z0-9_.])\.\.(/|$|[\s'";)|&])`)

// treeWalk matches a command that lists files by walking the filesystem:
// find, or a recursive grep. A walk reads whatever lies under its start
// directory, including another checkout nested in the tree, so lint must take
// its file list from git (git ls-files, git grep) or the go tool instead.
var treeWalk = regexp.MustCompile(`(^|[|;&(]\s*|\$\(\s*)(find\s|grep\s+(-[A-Za-z]*[rR]|--recursive|--dereference-recursive))`)

// lintScopeProblems returns each line of a dry-run recipe that could make lint
// read code from outside the repository it runs in: a path through `..`, or a
// filesystem walk instead of a list git or the go tool resolves inside the
// root (#302). Shell comments are ignored.
func lintScopeProblems(recipe string) []string {
	var problems []string
	for _, line := range strings.Split(recipe, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		switch {
		case parentPath.MatchString(trimmed):
			problems = append(problems, "names a path outside the root: "+trimmed)
		case treeWalk.MatchString(trimmed):
			problems = append(problems, "walks the filesystem instead of the repository: "+trimmed)
		}
	}
	return problems
}

// repoMakeDryRun returns `make -n target` for the repository Makefile with
// every variable at its default, TEST_CHECKS included, unlike makeDryRun.
// GOPROXY=off keeps the go commands the Makefile evaluates while parsing off
// the network.
func repoMakeDryRun(t *testing.T, target string) string {
	t.Helper()
	cmd := exec.Command("make", "-n", "--no-print-directory", "-C", repoRoot, target)
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n %s: %v\n%s", target, err, out)
	}
	return string(out)
}

func TestLintScansOnlyTheRepositoryItRunsIn(t *testing.T) {
	recipe := repoMakeDryRun(t, "lint")
	if !strings.Contains(recipe, "golangci-lint run") {
		t.Fatalf("make -n lint did not expand to the golangci-lint pass:\n%s", recipe)
	}
	for _, problem := range lintScopeProblems(recipe) {
		t.Errorf("make lint %s", problem)
	}
}

// TestChecksScanOnlyTheRepositoryItRunsIn holds the TEST_CHECKS targets that
// scan source files to the same rule as make lint: a recursive walk from the
// root reads a nested checkout or a lane clone, so each one takes its file
// list from REPO_GO_FILES instead.
func TestChecksScanOnlyTheRepositoryItRunsIn(t *testing.T) {
	for _, target := range []string{"check-insecure-tls"} {
		t.Run(target, func(t *testing.T) {
			recipe := repoMakeDryRun(t, target)
			if !strings.Contains(recipe, "git ls-files") {
				t.Errorf("make -n %s does not take its files from REPO_GO_FILES:\n%s", target, recipe)
			}
			for _, problem := range lintScopeProblems(recipe) {
				t.Errorf("make %s %s", target, problem)
			}
		})
	}
}

func TestLintScopeProblemsFindsARecipeLeavingTheRoot(t *testing.T) {
	cases := map[string]struct {
		recipe  string
		problem bool
	}{
		"parent directory lint":  {recipe: "golangci-lint run --timeout=20m ../...", problem: true},
		"cd to the parent":       {recipe: "cd .. && go vet ./...", problem: true},
		"sibling checkout":       {recipe: "go vet ./... ../sibling/...", problem: true},
		"find from the parent":   {recipe: "UNFORMATTED=$(find .. -name '*.go' | xargs gofmt -l)", problem: true},
		"find from the root":     {recipe: "UNFORMATTED=$(find . -name '*.go' | xargs gofmt -l)", problem: true},
		"recursive grep":         {recipe: "found=$(grep -rlE 'nolint' --include='*.go' .)", problem: true},
		"recursive grep in pipe": {recipe: "true | grep --recursive nolint .", problem: true},
		"module list via parent": {recipe: "modules=\"go.mod sdk/../../other/go.mod\"", problem: true},
		"git-listed files":       {recipe: "UNFORMATTED=$(git ls-files -z -- '*.go' | xargs -0r gofmt -l)"},
		"in-root packages":       {recipe: "golangci-lint run --timeout=20m ./...\ncd sdk/go && go vet ./test/..."},
		"git grep scan":          {recipe: "pkgs=$(git grep -lE '^//go:build integration' -- '*.go' | xargs -n1 dirname)"},
		"comment mentions find":  {recipe: "# find .. would leave the tree"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := lintScopeProblems(tc.recipe); (len(got) > 0) != tc.problem {
				t.Errorf("lintScopeProblems = %q, want a problem: %v", got, tc.problem)
			}
		})
	}
}

// TestRepoGoFilesListsOnlyTheRepositoryTree runs the Makefile's REPO_GO_FILES
// command in a throwaway repository that has a checkout nested inside it, an
// ignored directory, and a sibling directory beside it. Only the repository's
// own tracked and new Go files may come back.
func TestRepoGoFilesListsOnlyTheRepositoryTree(t *testing.T) {
	out, err := exec.Command("make", "-s", "--no-print-directory", "-C", repoRoot, "print-REPO_GO_FILES").Output()
	if err != nil {
		t.Fatalf("make print-REPO_GO_FILES: %v", err)
	}
	command := strings.TrimSpace(string(out))
	if command == "" {
		t.Fatal("the Makefile defines no REPO_GO_FILES")
	}

	base := t.TempDir()
	root := filepath.Join(base, "repo")
	files := map[string]string{
		filepath.Join(base, "sibling", "sibling.go"):     "package sibling\n",
		filepath.Join(root, ".gitignore"):                "ignored/\n",
		filepath.Join(root, "tracked.go"):                "package repo\n",
		filepath.Join(root, "pkg", "added.go"):           "package pkg\n",
		filepath.Join(root, "pkg", "untracked.go"):       "package pkg\n",
		filepath.Join(root, "ignored", "ignored.go"):     "package ignored\n",
		filepath.Join(root, "nested", "lane", "lane.go"): "package lane\n",
	}
	for path, body := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git(root, "init", "-q")
	git(filepath.Join(root, "nested", "lane"), "init", "-q")
	git(filepath.Join(root, "nested", "lane"), "add", "lane.go")
	git(root, "add", ".gitignore", "tracked.go", "pkg/added.go")

	list := exec.Command("sh", "-c", command)
	list.Dir = root
	listed, err := list.Output()
	if err != nil {
		t.Fatalf("REPO_GO_FILES (%s): %v", command, err)
	}
	got := strings.Split(strings.TrimRight(string(listed), "\x00"), "\x00")
	slices.Sort(got)
	want := []string{"pkg/added.go", "pkg/untracked.go", "tracked.go"}
	if !slices.Equal(got, want) {
		t.Errorf("REPO_GO_FILES listed %q, want %q", got, want)
	}
}

// runInsecureTLSGate runs the check-insecure-tls recipe, as make prints it,
// in dir and returns its combined output and whether it passed. The recipe
// runs outside the repository, so git there sees only what the test made.
func runInsecureTLSGate(t *testing.T, dir string) (string, bool) {
	t.Helper()
	recipe := repoMakeDryRun(t, "check-insecure-tls")
	cmd := exec.Command("sh", "-c", recipe)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CEILING_DIRECTORIES="+filepath.Dir(dir),
	)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

// insecureTLSTree writes files into a fresh directory and, when withGit is
// set, makes it a git repository with every file tracked.
func insecureTLSTree(t *testing.T, withGit bool, files map[string]string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	for path, body := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !withGit {
		return root
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root
}

// skipVerifyTrue is the forbidden assignment, split (like the nolint marker
// below) so this file's own source matches neither check-insecure-tls nor
// check-nolint.
const skipVerifyTrue = "InsecureSkipVerify: " + "true"

const forbiddenTLSFile = "package bad\n\nimport \"crypto/tls\"\n\nvar Config = &tls.Config{" + skipVerifyTrue + "}\n"

// A tree git cannot list (a source archive, a build context without .git)
// must fail the gate, not pass it with nothing scanned (Codex review 6378).
func TestInsecureTLSGateFailsWhenGitCannotListFiles(t *testing.T) {
	dir := insecureTLSTree(t, false, map[string]string{"bad.go": forbiddenTLSFile})
	out, passed := runInsecureTLSGate(t, dir)
	if passed {
		t.Fatalf("check-insecure-tls passed in a tree with no .git:\n%s", out)
	}
	if !strings.Contains(out, "cannot list") {
		t.Errorf("check-insecure-tls failed without naming the listing failure:\n%s", out)
	}
}

func TestInsecureTLSGateFailsOnAForbiddenAssignment(t *testing.T) {
	dir := insecureTLSTree(t, true, map[string]string{
		"ok.go":      "package bad\n",
		"pkg/bad.go": forbiddenTLSFile,
	})
	out, passed := runInsecureTLSGate(t, dir)
	if passed {
		t.Fatalf("check-insecure-tls passed a bare %s:\n%s", skipVerifyTrue, out)
	}
	if !strings.Contains(out, "pkg/bad.go:5") {
		t.Errorf("check-insecure-tls did not name pkg/bad.go:5:\n%s", out)
	}
}

func TestInsecureTLSGatePassesACleanTree(t *testing.T) {
	dir := insecureTLSTree(t, true, map[string]string{
		"ok.go": "package ok\n",
		"pkg/allowed.go": "package pkg\n\nimport \"crypto/tls\"\n\n" +
			"var Config = &tls.Config{" + skipVerifyTrue + "} //" + "nolint:gosec // loopback test cert\n",
		"pkg/commented.go": "package pkg\n\n// " + skipVerifyTrue + " is forbidden here.\n",
	})
	out, passed := runInsecureTLSGate(t, dir)
	if !passed {
		t.Fatalf("check-insecure-tls failed a clean tree:\n%s", out)
	}
}
