package buildgates

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// docsOnlyScript is the filter ci.yml's changes job runs. A docs-only pull
// request skips build, unit tests, SDK tests, security scans and the
// integration job, so a wrong "true" ships untested code (Codex 6488).
var docsOnlyScript = filepath.Join(repoRoot, "scripts", "docs-only.sh")

// gitEnv isolates git from the host: no global or system config, a HOME of
// the test's own, and a fixed author so commits succeed anywhere.
func gitEnv(home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=docs-only test",
		"GIT_AUTHOR_EMAIL=docs-only@example.invalid",
		"GIT_COMMITTER_NAME=docs-only test",
		"GIT_COMMITTER_EMAIL=docs-only@example.invalid",
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeRepoFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// docsOnlyChange is one pull request: the files of its base commit, the files
// its head commit writes (new or modified) and the files it renames.
type docsOnlyChange struct {
	base  []string
	write []string
	moves [][2]string
}

// commitChange builds a throwaway repository holding the change as two
// commits and returns its directory and the base commit.
func commitChange(t *testing.T, change docsOnlyChange) (dir, baseSHA string) {
	t.Helper()
	dir = t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	writeRepoFile(t, dir, "keep.txt", "base\n")
	for _, path := range change.base {
		writeRepoFile(t, dir, path, "base\n")
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "base")
	baseSHA = runGit(t, dir, "rev-parse", "HEAD")

	for _, path := range change.write {
		writeRepoFile(t, dir, path, "head\n")
	}
	for _, move := range change.moves {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(move[1])), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", move[1], err)
		}
		runGit(t, dir, "mv", move[0], move[1])
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "--allow-empty", "-m", "head")
	return dir, baseSHA
}

// runDocsOnly runs the filter in dir with the given event and base and
// returns its stdout, stderr and whether it exited zero.
func runDocsOnly(t *testing.T, dir, event, baseSHA string) (stdout, stderr string, ok bool) {
	t.Helper()
	script, err := filepath.Abs(docsOnlyScript)
	if err != nil {
		t.Fatalf("resolve %s: %v", docsOnlyScript, err)
	}
	cmd := exec.Command("sh", script)
	cmd.Dir = dir
	// ci.yml always sets BASE_SHA; outside a pull request it is empty.
	cmd.Env = append(gitEnv(dir), "EVENT="+event, "BASE_SHA="+baseSHA)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	runErr := cmd.Run()
	if runErr != nil {
		if _, exited := runErr.(*exec.ExitError); !exited {
			t.Fatalf("run %s: %v", script, runErr)
		}
	}
	return out.String(), errOut.String(), runErr == nil
}

func TestDocsOnlyFilterClassifiesTheChangeSet(t *testing.T) {
	cases := []struct {
		name   string
		event  string
		change docsOnlyChange
		want   string
	}{
		{name: "docs file changed", event: "pull_request",
			change: docsOnlyChange{base: []string{"docs/x.md"}, write: []string{"docs/x.md"}},
			want:   "docs_only=true"},
		{name: "changelog entry added", event: "pull_request",
			change: docsOnlyChange{write: []string{"changelog.d/x.md"}},
			want:   "docs_only=true"},
		{name: "top-level markdown changed", event: "pull_request",
			change: docsOnlyChange{base: []string{"ROADMAP.md"}, write: []string{"ROADMAP.md"}},
			want:   "docs_only=true"},
		{name: "nested README changed", event: "pull_request",
			change: docsOnlyChange{base: []string{"pkg/x/README.md"}, write: []string{"pkg/x/README.md"}},
			want:   "docs_only=true"},
		{name: "markdown embedded in a package", event: "pull_request",
			change: docsOnlyChange{base: []string{"pkg/x/notes.md"}, write: []string{"pkg/x/notes.md"}},
			want:   "docs_only=false"},
		{name: "docs and code together", event: "pull_request",
			change: docsOnlyChange{base: []string{"docs/x.md", "pkg/x/x.go"}, write: []string{"docs/x.md", "pkg/x/x.go"}},
			want:   "docs_only=false"},
		{name: "code renamed into docs", event: "pull_request",
			change: docsOnlyChange{base: []string{"pkg/x/x.go"}, moves: [][2]string{{"pkg/x/x.go", "docs/x.go"}}},
			want:   "docs_only=false"},
		{name: "docs renamed into code", event: "pull_request",
			change: docsOnlyChange{base: []string{"docs/x.md"}, moves: [][2]string{{"docs/x.md", "pkg/x/x.md"}}},
			want:   "docs_only=false"},
		{name: "no files changed", event: "pull_request",
			change: docsOnlyChange{},
			want:   "docs_only=false"},
		{name: "push of a docs change", event: "push",
			change: docsOnlyChange{base: []string{"docs/x.md"}, write: []string{"docs/x.md"}},
			want:   "docs_only=false"},
		{name: "dispatch of a docs change", event: "workflow_dispatch",
			change: docsOnlyChange{base: []string{"docs/x.md"}, write: []string{"docs/x.md"}},
			want:   "docs_only=false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, baseSHA := commitChange(t, tc.change)
			stdout, stderr, ok := runDocsOnly(t, dir, tc.event, baseSHA)
			if !ok {
				t.Fatalf("docs-only.sh failed:\n%s", stderr)
			}
			if stdout != tc.want+"\n" {
				t.Errorf("stdout = %q, want %q. Only a pull request whose every "+
					"changed file is documentation may skip the build, test, "+
					"security and integration stages; stdout is appended to "+
					"$GITHUB_OUTPUT, so it must be that one line.\nstderr:\n%s",
					stdout, tc.want+"\n", stderr)
			}
		})
	}
}

func TestDocsOnlyFilterFailsWhenTheChangeSetIsUnknown(t *testing.T) {
	cases := []struct {
		name    string
		baseSHA string
		wantErr string
	}{
		{name: "base commit not in the repository", baseSHA: "0123456789abcdef0123456789abcdef01234567",
			wantErr: "::error::cannot list the files this pull request changes"},
		{name: "no base commit", baseSHA: "",
			wantErr: "::error::BASE_SHA is not set for a pull_request event"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, _ := commitChange(t, docsOnlyChange{base: []string{"docs/x.md"}, write: []string{"docs/x.md"}})
			stdout, stderr, ok := runDocsOnly(t, dir, "pull_request", tc.baseSHA)
			if ok {
				t.Errorf("docs-only.sh exited zero (stdout %q). A pull request "+
					"whose changed files cannot be listed must fail the job, "+
					"never pass it on with stages skipped.", stdout)
			}
			if strings.Contains(stdout, "docs_only=true") {
				t.Errorf("stdout = %q: an unknown change set was marked docs-only, "+
					"which would skip every expensive stage", stdout)
			}
			if !strings.Contains(stderr, tc.wantErr) {
				t.Errorf("stderr = %q, want %q: the job log must name the cause", stderr, tc.wantErr)
			}
		})
	}
}

// ciSteps splits a job body into its steps, one string per `- ` item.
func ciSteps(job string) []string {
	var steps []string
	var current []string
	for _, line := range strings.Split(job, "\n") {
		if strings.HasPrefix(line, "      - ") {
			if current != nil {
				steps = append(steps, strings.Join(current, "\n"))
			}
			current = []string{line}
			continue
		}
		if current != nil {
			current = append(current, line)
		}
	}
	if current != nil {
		steps = append(steps, strings.Join(current, "\n"))
	}
	return steps
}

func stepName(step string) string {
	for _, line := range strings.Split(step, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if name, ok := strings.CutPrefix(line, "name: "); ok {
			return name
		}
	}
	return ""
}

func stepIf(step string) string {
	for _, line := range strings.Split(step, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- "))
		if cond, ok := strings.CutPrefix(line, "if: "); ok {
			return cond
		}
	}
	return ""
}

func TestChangesJobRunsTheDocsOnlyScript(t *testing.T) {
	_, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	changes, ok := jobs["changes"]
	if !ok {
		t.Fatalf("%s has no changes job. Every docs-only skip reads its output.", ciWorkflowPath)
	}
	for _, want := range []string{
		"docs_only: ${{ steps.diff.outputs.docs_only }}",
		"id: diff",
		"EVENT: ${{ github.event_name }}",
		"BASE_SHA: ${{ github.event.pull_request.base.sha }}",
		// ci-template v1.23: the full tier (ci:full) is never docs-only.
		"if [ \"$CI_FULL_TIER\" = true ]; then\n            echo \"docs_only=false\" >> \"$GITHUB_OUTPUT\"\n",
		`scripts/docs-only.sh >> "$GITHUB_OUTPUT"`,
	} {
		if !strings.Contains(changes, want) {
			t.Errorf("changes job lacks %q. The classification is tested in "+
				"scripts/docs-only.sh; a filter written inline in the job, or one "+
				"fed other inputs, is not the one the tests prove.", want)
		}
	}
}

func TestOnlyLintRunsForADocsOnlyPullRequest(t *testing.T) {
	_, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	image := jobs["image"]
	if !strings.Contains(image, "DOCS_ONLY: ${{ needs.changes.outputs.docs_only }}") {
		t.Errorf("image job does not set DOCS_ONLY from the changes job; every " +
			"stage guard below would compare an empty value and never skip")
	}
	// ci-template v1.23 guards a PR-tier stage on the changes job's output. A
	// full-tier stage needs no guard: the changes job never reports a
	// full-tier run docs-only. The releasability test is the one stage that
	// runs only for a docs-only pull request; `make coverage` runs it otherwise.
	const (
		guard         = "needs.changes.outputs.docs_only != 'true'"
		fullTier      = "env.CI_FULL_TIER == 'true'"
		docsOnlyGuard = "needs.changes.outputs.docs_only == 'true'"
	)
	stages := 0
	for _, step := range ciSteps(image) {
		if !strings.Contains(step, "scripts/in-toolchain-image.sh") {
			continue
		}
		stages++
		name, cond := stepName(step), stepIf(step)
		if name == "make lint" {
			if strings.Contains(cond, "docs_only") || strings.Contains(cond, "DOCS_ONLY") {
				t.Errorf("make lint is guarded by docs_only (if: %s). Lint is the "+
					"one stage a docs-only pull request still runs.", cond)
			}
			continue
		}
		if strings.Contains(cond, docsOnlyGuard) && strings.Contains(step, "RELEASABILITY_TEST") {
			continue
		}
		if !strings.Contains(cond, guard) && !strings.Contains(cond, fullTier) {
			t.Errorf("step %q runs in the toolchain image without %q or %q in its if: "+
				"(if: %s). A docs-only pull request builds only the toolchain "+
				"image, so the stage would fail, or run for nothing.", name, guard, fullTier, cond)
		}
	}
	if stages < 2 {
		t.Fatalf("found %d toolchain-image steps in the image job; step discovery "+
			"has broken and the guard check passed over nothing", stages)
	}
}

func TestDocsOnlyBuildsTheToolchainImageAlone(t *testing.T) {
	_, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	var build string
	for _, step := range ciSteps(jobs["image"]) {
		if stepName(step) == "Build the devcontainer images" {
			build = step
		}
	}
	if build == "" {
		t.Fatalf("image job has no \"Build the devcontainer images\" step")
	}
	const choice = `if [ "$DOCS_ONLY" = true ]; then target=docker-build-toolchain-image; fi`
	if !strings.Contains(build, "target=docker-build\n") || !strings.Contains(build, choice) {
		t.Errorf("the build step must default to docker-build and choose "+
			"docker-build-toolchain-image only through %q: any other pull request "+
			"needs the integration image its stages scan and test", choice)
	}
	if n := strings.Count(build, "docker-build-toolchain-image"); n != 1 {
		t.Errorf("docker-build-toolchain-image appears %d times in the build step, "+
			"want 1 (the DOCS_ONLY branch). A second use could build the "+
			"toolchain image alone for a code change.", n)
	}
}

// TestIntegrationNeverRunsForADocsOnlyPullRequest: since ci-template v1.23
// integration is full tier, and the changes job never reports a full-tier run
// docs-only, so a docs-only pull request never runs it.
func TestIntegrationNeverRunsForADocsOnlyPullRequest(t *testing.T) {
	_, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	jobIf := jobIfRe.FindStringSubmatch(jobs["integration"])
	if jobIf == nil || !strings.Contains(jobIf[1], "("+fullTierIf+")") {
		t.Errorf("integration job's `if:` lacks the full-tier condition (%s); "+
			"a docs-only pull request would run it", fullTierIf)
	}
	needs := gateNeeds(t, jobs[gateJobName])
	if missing([]string{"changes"}, needs) != nil {
		t.Errorf("%s needs %v, want changes among them: a failed filter must "+
			"fail the gate, not leave every stage skipped and green", gateJobName, needs)
	}
}
