package buildgates

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// docsOnlyScript is the filter ci.yml's preflight job runs. A docs-only pull
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

func TestPreflightRunsTheDocsOnlyScript(t *testing.T) {
	_, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	preflight, ok := jobs["preflight"]
	if !ok {
		t.Fatalf("%s has no preflight job. Every docs-only skip reads its output.", ciWorkflowPath)
	}
	for _, want := range []string{
		"docs_only: ${{ steps.diff.outputs.docs_only }}",
		"id: diff",
		"EVENT: ${{ github.event_name }}",
		"BASE_SHA: ${{ github.event.pull_request.base.sha }}",
		`scripts/docs-only.sh >> "$GITHUB_OUTPUT"`,
	} {
		if !strings.Contains(preflight, want) {
			t.Errorf("preflight job lacks %q. The classification is tested in "+
				"scripts/docs-only.sh; a filter written inline in the job, or one "+
				"fed other inputs, is not the one the tests prove.", want)
		}
	}
}

// docsOnlySkip is the job-level condition term every code job carries, so a
// docs-only pull request skips it and CI Gate reads the skip as `code`.
const docsOnlySkip = "needs.preflight.outputs.docs_only != 'true'"

// jobIf returns a job's job-level `if:` condition, or "" when it has none.
func jobIf(job string) string {
	for _, line := range strings.Split(job, "\n") {
		if cond, ok := strings.CutPrefix(line, "    if: "); ok {
			return cond
		}
	}
	return ""
}

func TestOnlyLintRunsForADocsOnlyPullRequest(t *testing.T) {
	_, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	if cond := jobIf(jobs["lint"]); strings.Contains(cond, "docs_only") {
		t.Errorf("the lint job is skipped on docs_only (if: %s). Lint is the one "+
			"stage a docs-only pull request still runs.", cond)
	}
	tiers := gateChecks(jobs[gateJobName])
	for _, name := range []string{"image", "unit", "coverage", "unit-passes", "integration"} {
		job, ok := jobs[name]
		if !ok {
			t.Errorf("ci.yml has no %s job", name)
			continue
		}
		if cond := jobIf(job); !strings.Contains(cond, docsOnlySkip) {
			t.Errorf("job %q runs on a docs-only pull request (if: %s); it must "+
				"carry %q", name, cond, docsOnlySkip)
		}
		if nonGatingJobs[name] {
			continue
		}
		if tiers[name] != "code" {
			t.Errorf("job %q skips on a docs-only pull request, so its check line "+
				"in %s must be `code`, not %q", name, gateJobName, tiers[name])
		}
	}
}

func TestLintBuildsTheToolchainImageAlone(t *testing.T) {
	_, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	step := func(job, name string) string {
		for _, s := range ciSteps(jobs[job]) {
			if stepName(s) == name {
				return s
			}
		}
		t.Fatalf("%s job has no %q step", job, name)
		return ""
	}
	if lint := step("lint", "Build the toolchain image"); !strings.Contains(lint, "make docker-build-toolchain MODULE_ENV_FILE=") {
		t.Errorf("the lint job must build the toolchain image alone (make docker-build-toolchain):\n%s", lint)
	}
	if image := step("image", "Build the devcontainer images"); !strings.Contains(image, "make docker-build MODULE_ENV_FILE=") {
		t.Errorf("the image job must build both images (make docker-build): its stages scan the integration image:\n%s", image)
	}
}

func TestIntegrationSkipsOnlyForADocsOnlyPullRequest(t *testing.T) {
	_, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	const cond = "if: ${{ !github.event.pull_request.draft && needs.preflight.outputs.verdict == 'run' && " + docsOnlySkip + " }}"
	if !strings.Contains(jobs["integration"], "\n    "+cond+"\n") {
		t.Errorf("integration job lacks %q. It must skip for a docs-only pull "+
			"request and run for every other change.", cond)
	}
	needs := gateNeeds(t, jobs[gateJobName])
	if missing([]string{"preflight"}, needs) != nil {
		t.Errorf("%s needs %v, want preflight among them: a failed filter must "+
			"fail the gate, not leave every stage skipped and green", gateJobName, needs)
	}
}
