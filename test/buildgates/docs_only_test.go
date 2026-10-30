package buildgates

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// TestChangesJobClassifiesTheDiffInline pins ci-template v1.24's `changes`
// job (CA m749, m751 (a); --no-renames from v1.24.1): it classifies the
// pull request's diff inline against DOCS_ONLY_PATTERN, and only a
// pull_request below the full tier can be docs-only. docs_only starts false,
// so a failed diff or an empty list runs the tests.
func TestChangesJobClassifiesTheDiffInline(t *testing.T) {
	_, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	changes, ok := jobs["changes"]
	if !ok {
		t.Fatalf("%s has no changes job. Every docs-only skip reads its output.", ciWorkflowPath)
	}
	for _, want := range []string{
		"docs_only: ${{ steps.diff.outputs.docs_only }}",
		"id: diff",
		"BASE: ${{ github.event.pull_request.base.sha }}",
		"HEAD: ${{ github.event.pull_request.head.sha }}",
		"EVENT: ${{ github.event_name }}",
		"docs_only=false\n",
		`if [ "$EVENT" = pull_request ] && [ "$CI_FULL_TIER" != true ]; then`,
		`if files=$(git diff --name-only --no-renames "$BASE...$HEAD") && [ -n "$files" ]; then`,
		`if ! printf '%s\n' "$files" | grep -qvE "$DOCS_ONLY_PATTERN"; then`,
		`echo "docs_only=$docs_only" >> "$GITHUB_OUTPUT"`,
	} {
		if !strings.Contains(changes, want) {
			t.Errorf("changes job lacks %q. The docs-only classification is the "+
				"ci-template's inline diff; a filter fed other inputs, or one that "+
				"defaults to docs-only, skips tests on a code change.", want)
		}
	}
}

// TestDocsOnlyPatternMatchesOnlyDocs pins go-quicbaas's DOCS_ONLY_PATTERN
// customization (DS-3161): docs/, changelog.d/, a top-level *.md file and any
// README.md are docs; a *.md file elsewhere can be a go:embed input and is
// code.
func TestDocsOnlyPatternMatchesOnlyDocs(t *testing.T) {
	m := regexp.MustCompile(`(?m)^  DOCS_ONLY_PATTERN: '([^']*)'$`).FindStringSubmatch(readFile(t, ciWorkflowPath))
	if m == nil {
		t.Fatalf("%s sets no single-quoted DOCS_ONLY_PATTERN in its env", ciWorkflowPath)
	}
	pattern, err := regexp.Compile(m[1])
	if err != nil {
		t.Fatalf("DOCS_ONLY_PATTERN %q does not compile: %v", m[1], err)
	}
	for path, docs := range map[string]bool{
		"README.md":                  true,
		"ROADMAP.md":                 true,
		"docs/reference/README.md":   true,
		"docs/sdk/go.md":             true,
		"changelog.d/2125.md":        true,
		"pkg/iam/README.md":          true,
		"pkg/sdlc/prompts/triage.md": false,
		"pkg/iam/invite.go":          false,
		"Makefile":                   false,
		".github/workflows/ci.yml":   false,
		"mydocs/notes.txt":           false,
		"docs.go":                    false,
		"scripts/docs-only.sh":       false,
	} {
		if got := pattern.MatchString(path); got != docs {
			t.Errorf("DOCS_ONLY_PATTERN matches %q = %v, want %v", path, got, docs)
		}
	}
}

// TestOnlyLintRunsForADocsOnlyPullRequest pins which image-job stages a
// docs-only pull request runs under ci-template v1.29.5: make lint, make
// deps-check (v1.28, ruling 219(3)) and the releasability test (v1.26.1,
// m1128 (9)), which runs only there because make coverage skips. Every other
// stage carries `needs.changes.outputs.docs_only != 'true'` or is full tier,
// and a full-tier run is never docs-only.
func TestOnlyLintRunsForADocsOnlyPullRequest(t *testing.T) {
	_, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	image := jobs["image"]
	if !strings.Contains(image, "\n    needs: [changes, schedule-guard]") {
		t.Errorf("image job does not need the changes job; every docs-only guard " +
			"below would compare an empty value and never skip")
	}
	const (
		skip     = "needs.changes.outputs.docs_only != 'true'"
		only     = "needs.changes.outputs.docs_only == 'true'"
		fullTier = "env.CI_FULL_TIER == 'true'"
	)
	everyRun := map[string]bool{"make lint": true, "make deps-check": true}
	docsOnlyRun := map[string]bool{"Releasability test (docs-only PR)": true}
	stages := 0
	for _, step := range ciSteps(image) {
		if !strings.Contains(step, "scripts/in-toolchain-image.sh") {
			continue
		}
		stages++
		name, cond := stepName(step), stepIf(step)
		switch {
		case everyRun[name]:
			if strings.Contains(cond, "docs_only") || strings.Contains(cond, "CI_FULL_TIER") {
				t.Errorf("%s is guarded (if: %s). It runs on every event, a docs-only "+
					"pull request included.", name, cond)
			}
		case docsOnlyRun[name]:
			if !strings.Contains(cond, only) {
				t.Errorf("step %q lacks %q in its if: (if: %s); it stands in for make "+
					"coverage on a docs-only pull request only", name, only, cond)
			}
		case !strings.Contains(cond, skip) && !strings.Contains(cond, fullTier):
			t.Errorf("step %q runs in the toolchain image without %q or %q in its if: "+
				"(if: %s). A docs-only pull request runs lint alone.", name, skip, fullTier, cond)
		}
	}
	if stages < 2 {
		t.Fatalf("found %d toolchain-image steps in the image job; step discovery "+
			"has broken and the guard check passed over nothing", stages)
	}
}

// TestImageJobBuildsBothImagesOnEveryRun pins ci-template v1.29.5's build
// step, which supersedes the local toolchain-image-only build for a
// docs-only pull request: the image job runs `make docker-build` on every
// run and takes the toolchain image from `make -s toolchain-image`, so every
// stage, the releasability test and deps-check included, has the images it
// runs in.
func TestImageJobBuildsBothImagesOnEveryRun(t *testing.T) {
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
	if cond := stepIf(build); cond != "" {
		t.Errorf("the build step carries `if: %s`; it must run on every run", cond)
	}
	for _, want := range []string{
		"\n          make docker-build ",
		"\n          echo \"TOOLCHAIN_IMAGE=$(make -s toolchain-image)\" >> \"$GITHUB_ENV\"",
	} {
		if !strings.Contains(build, want) {
			t.Errorf("the build step lacks %q", want)
		}
	}
	if strings.Contains(build, "DOCS_ONLY") || strings.Contains(build, "docker-build-toolchain-image") {
		t.Errorf("the build step chooses its target by docs-only; the ci-template " +
			"builds both images on every run")
	}
}

// TestIntegrationRunsOnlyOnTheFullTier pins ci-template v1.23's integration
// job, which supersedes the local rule that it ran on every pull request but
// a docs-only one: Integration runs on the full tier only (every event but a
// pull request, and a ci:full pull request), never on a draft, and on a
// schedule run only on the schedule guard's `run` (v1.29.3). A docs-only
// pull request is below the full tier, so it never runs Integration.
func TestIntegrationRunsOnlyOnTheFullTier(t *testing.T) {
	_, jobs := ciJobs(t, readFile(t, ciWorkflowPath))
	const cond = "if: ${{ (" + fullTierExpr + ") && !github.event.pull_request.draft && " +
		"(github.event_name != 'schedule' || needs.schedule-guard.outputs.verdict == 'run') }}"
	if !strings.Contains(jobs["integration"], "\n    "+cond+"\n") {
		t.Errorf("integration job lacks %q. It must run on the full tier alone.", cond)
	}
	needs := gateNeeds(t, jobs[gateJobName])
	if missing([]string{"changes"}, needs) != nil {
		t.Errorf("%s needs %v, want changes among them: a failed filter must "+
			"fail the gate, not leave every stage skipped and green", gateJobName, needs)
	}
}
