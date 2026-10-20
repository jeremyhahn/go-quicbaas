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
	"sort"
	"strings"
	"testing"
)

// repoRoot is the module root, relative to this package's directory, which is
// where `go test` runs it.
const repoRoot = "../.."

var (
	workflowsDir   = filepath.Join(repoRoot, ".github", "workflows")
	ciWorkflowPath = filepath.Join(workflowsDir, "ci.yml")
)

// gateJobName is the job branch protection requires, as
// `CI / CI Gate (pull_request)`.
const gateJobName = "ci-gate"

// fullTierIf is the condition every full-tier job carries, and the
// expression ci-gate evaluates into FULL_TIER (#1272).
const fullTierIf = `github.event_name == 'push' || github.event_name == 'workflow_dispatch' || contains(github.event.pull_request.labels.*.name, 'ci:full')`

var (
	jobHeaderRe = regexp.MustCompile(`^  ([a-z0-9][a-z0-9-]*):\s*$`)
	gateNeedsRe = regexp.MustCompile(`(?m)^    needs: \[([^\]]*)\]\s*$`)
	checkLineRe = regexp.MustCompile(`(?m)^\s+check\s+(\S+)\s+"[^"]*"\s*(\S*)\s*$`)
	jobIfRe     = regexp.MustCompile(`(?m)^    if:`)
)

// nonGatingJobs are the ci.yml jobs that run but deliberately do not gate
// CI Gate: they are in neither its needs nor a check line.
//
// Integration still runs on every PR but does not gate until row #1700
// closes (CA m787). Remove it from this set when the row closes, and restore
// it in ci-gate's needs and as a pr check line.
var nonGatingJobs = map[string]bool{
	"integration": true,
}

// gateNeeds returns the jobs listed in the gate job's `needs: [...]` line.
func gateNeeds(t *testing.T, gate string) []string {
	t.Helper()
	m := gateNeedsRe.FindStringSubmatch(gate)
	if m == nil {
		t.Fatalf("%s has no `needs: [...]` line. A gate that depends on nothing "+
			"reports success the moment the workflow starts.", gateJobName)
	}
	var needs []string
	for _, n := range strings.Split(m[1], ",") {
		if n = strings.TrimSpace(n); n != "" {
			needs = append(needs, n)
		}
	}
	return needs
}

// readFile returns a file's contents or fails the test.
func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// ciJobs returns every top-level job in ci.yml, in file order, mapped to its
// body: the lines after the `  <id>:` header up to the next line indented two
// spaces or less. Ending at the first shallow line, rather than matching
// indentation with a regexp, keeps a job body from running into the next job
// across a blank line.
func ciJobs(t *testing.T, body string) ([]string, map[string]string) {
	t.Helper()
	var order []string
	bodies := map[string]string{}
	inJobs := false
	current := ""
	var lines []string
	flush := func() {
		if current != "" {
			bodies[current] = strings.Join(lines, "\n") + "\n"
		}
		current, lines = "", nil
	}
	for _, line := range strings.Split(body, "\n") {
		if line == "jobs:" {
			inJobs = true
			continue
		}
		if !inJobs {
			continue
		}
		if strings.TrimSpace(line) == "" {
			lines = append(lines, line)
			continue
		}
		if !strings.HasPrefix(line, "   ") {
			flush()
			if m := jobHeaderRe.FindStringSubmatch(line); m != nil {
				current = m[1]
				order = append(order, current)
			}
			continue
		}
		if current != "" {
			lines = append(lines, line)
		}
	}
	flush()
	if len(order) < 2 {
		t.Fatalf("found %d jobs in %s; discovery has broken and every assertion "+
			"below would pass over an empty set", len(order), ciWorkflowPath)
	}
	return order, bodies
}

// gateChecks returns each job the gate's `check` lines name, mapped to its
// tier tag (empty when the line carries none).
func gateChecks(gate string) map[string]string {
	tiers := map[string]string{}
	for _, c := range checkLineRe.FindAllStringSubmatch(gate, -1) {
		tiers[c[1]] = c[2]
	}
	return tiers
}

// TestCIGateDependsOnEveryJobAndChecksEveryDependency is the gate's own gate.
//
// Branch protection requires exactly ONE status context, so `CI Gate` speaks
// for the whole pipeline. Two ways that claim rots, both silent:
//
//   - a NEW JOB is added and not wired into `needs`. The gate goes green while
//     that job is red.
//   - a job IS in `needs` but has no `check` line. `needs` only orders
//     execution; with `if: always()` the gate runs regardless of the
//     dependency's result, so an unchecked dependency is decorative.
func TestCIGateDependsOnEveryJobAndChecksEveryDependency(t *testing.T) {
	order, bodies := ciJobs(t, readFile(t, ciWorkflowPath))
	gate, ok := bodies[gateJobName]
	if !ok {
		t.Fatalf("ci.yml has no %q job. Branch protection requires the single "+
			"context it publishes (CI / CI Gate).", gateJobName)
	}

	var substantive []string
	for _, j := range order {
		if j != gateJobName && !nonGatingJobs[j] {
			substantive = append(substantive, j)
		}
	}

	needs := gateNeeds(t, gate)

	var checked []string
	for j := range gateChecks(gate) {
		checked = append(checked, j)
	}
	if len(checked) == 0 {
		t.Fatal("the CI Gate job runs no `check` lines at all; it passes unconditionally")
	}

	sort.Strings(substantive)
	sort.Strings(needs)
	sort.Strings(checked)

	if diff := missing(substantive, needs); len(diff) > 0 {
		t.Errorf("jobs exist in ci.yml but are NOT in %s's needs: %v\n\n"+
			"The gate would report success while these are red. Add each to `needs` "+
			"AND to a `check` line.", gateJobName, diff)
	}
	if diff := missing(needs, checked); len(diff) > 0 {
		t.Errorf("jobs are in %s's needs but have NO `check` line: %v\n\n"+
			"`needs` only orders execution. With `if: always()` the gate runs whatever "+
			"the dependency did, so an unchecked dependency gates nothing.", gateJobName, diff)
	}
	if diff := missing(checked, needs); len(diff) > 0 {
		t.Errorf("jobs have a `check` line but are NOT in %s's needs: %v\n\n"+
			"`needs.<job>.result` is empty for a job that is not a dependency, so the "+
			"check never matches `success` and fails the gate permanently.", gateJobName, diff)
	}
}

// TestNonGatingJobsCannotHideAJob proves nonGatingJobs exempts only real,
// deliberately ungated jobs: a stale or misspelled entry would silently exempt
// nothing, and an entry still in the gate's needs or checks contradicts itself.
func TestNonGatingJobsCannotHideAJob(t *testing.T) {
	_, bodies := ciJobs(t, readFile(t, ciWorkflowPath))
	gate, ok := bodies[gateJobName]
	if !ok {
		t.Fatalf("ci.yml has no %q job", gateJobName)
	}
	needs := gateNeeds(t, gate)
	checks := gateChecks(gate)

	for j := range nonGatingJobs {
		if j == gateJobName {
			t.Errorf("%s itself cannot be non-gating", gateJobName)
		}
		if _, ok := bodies[j]; !ok {
			t.Errorf("non-gating job %q does not exist in ci.yml; remove the stale entry", j)
		}
		if slices.Contains(needs, j) {
			t.Errorf("job %q is non-gating but is in %s's needs; remove it from "+
				"nonGatingJobs now that it gates", j, gateJobName)
		}
		if _, ok := checks[j]; ok {
			t.Errorf("job %q is non-gating but has a check line in %s", j, gateJobName)
		}
	}
}

// TestCIGateRunsEvenWhenADependencyFails pins `if: always()` and the
// fail-closed comparison.
//
// Without `if: always()` the gate is SKIPPED when any dependency fails. A
// skipped job publishes no status, so the required context never arrives and
// the merge blocks on silence rather than failing on a verdict.
func TestCIGateRunsEvenWhenADependencyFails(t *testing.T) {
	_, bodies := ciJobs(t, readFile(t, ciWorkflowPath))
	gate := bodies[gateJobName]
	if gate == "" {
		t.Fatalf("could not isolate the %s job body", gateJobName)
	}
	if !regexp.MustCompile(`(?m)^    if: always\(\)\s*$`).MatchString(gate) {
		t.Errorf("%s is missing `if: always()`:\n%s", gateJobName, gate)
	}
	// The ci-template's check(): only success passes, a skip passes only for a
	// full-tier job below the full tier, and every other result fails.
	for _, line := range []string{
		"            case \"$2\" in\n              success) echo \"OK   $1\";;\n              skipped)\n",
		"                if [ \"$3\" = full ] && [ \"${FULL_TIER}\" != true ]; then\n",
		"                  echo \"FAIL $1 (skipped)\"; BAD=1\n",
		"              *) echo \"FAIL $1 ($2)\"; BAD=1;;\n",
		"          exit $BAD\n",
	} {
		if !strings.Contains(gate, line) {
			t.Errorf("%s's check differs from the ci-template; it lacks:\n%s", gateJobName, line)
		}
	}
}

// TestCIGateTiersMatchTheJobs pins the two tiers to the gate.
//
// ci-gate passes a skipped job only when its check line tags it `full` and the
// run did not select the full tier. Two drifts would break that: a job tagged
// `full` whose `if:` differs from FULL_TIER, so the gate excuses a skip in a
// run that should have executed it; and a PR-tier job that gains an `if:`, so
// it skips on some event and fails the gate there.
func TestCIGateTiersMatchTheJobs(t *testing.T) {
	order, bodies := ciJobs(t, readFile(t, ciWorkflowPath))
	gate := bodies[gateJobName]

	if !strings.Contains(gate, "FULL_TIER: ${{ "+fullTierIf+" }}") {
		t.Errorf("%s's FULL_TIER is not the full-tier condition %q", gateJobName, fullTierIf)
	}

	tiers := gateChecks(gate)
	for _, j := range order {
		if j == gateJobName || nonGatingJobs[j] {
			continue
		}
		job := bodies[j]
		switch tiers[j] {
		case "full":
			if !strings.Contains(job, "\n    if: "+fullTierIf+"\n") {
				t.Errorf("job %q is tagged full in %s but lacks `if: %s`", j, gateJobName, fullTierIf)
			}
		case "pr":
			if jobIfRe.MatchString(job) {
				t.Errorf("job %q is PR tier but carries a job-level `if:`; a PR-tier job "+
					"must run on every event, and %s fails it when skipped", j, gateJobName)
			}
		default:
			t.Errorf("job %q's check line in %s has tier %q; want `pr` or `full`",
				j, gateJobName, tiers[j])
		}
	}
}

// TestCIIgnoresLabelChanges pins ci.yml's pull_request types without
// `labeled` (#1445). Gitea maps every label add and remove to `labeled`, so
// with it each code-review or task:* change starts a run that cancels the
// PR's run in flight.
func TestCIIgnoresLabelChanges(t *testing.T) {
	body := readFile(t, ciWorkflowPath)
	// The ci-template's pull_request trigger carries no types list, so it runs
	// on the default opened, synchronize and reopened, never on a label change.
	const trigger = "\n  pull_request:\n    branches: [main]\n  workflow_dispatch:"
	if !strings.Contains(body, trigger) {
		t.Errorf("ci.yml's pull_request trigger is not the ci-template's:%s", trigger)
	}
	if strings.Contains(body, "labeled") {
		t.Errorf("ci.yml's triggers carry `labeled`; a label change must not start CI")
	}
}

// TestPullRequestWorkflowsCancelSupersededRuns asserts that every workflow
// triggered on `pull_request` cancels its superseded runs through a ref-keyed
// group (devops.md §9b). The ref keeps one pull request from cancelling
// another. `pull_request_target` is out of scope: its github.ref is the base
// branch, so a ref-keyed group there would span every pull request.
// cancelsEveryRef matches a concurrency key, not a comment, that cancels
// in-progress runs on every ref, main included.
var cancelsEveryRef = regexp.MustCompile(`(?m)^\s*cancel-in-progress:\s*true\s*$`)

func TestPullRequestWorkflowsCancelSupersededRuns(t *testing.T) {
	// The ci-template v1.22.1 block: one group per ref, a newer run cancels the
	// older one off main, and a main run is never cancelled.
	const want = "concurrency:\n  group: ${{ github.workflow }}-${{ github.ref }}\n" +
		"  cancel-in-progress: ${{ github.ref != 'refs/heads/main' }}\n"
	for _, path := range workflowFiles(t) {
		body := readFile(t, path)
		if strings.Contains(body, "\n  push:\n") && cancelsEveryRef.MatchString(body) {
			t.Errorf("%s runs on push and sets `cancel-in-progress: true`, which cancels main runs", path)
		}
		if !strings.Contains(body, "\n  pull_request:\n") {
			continue
		}
		if !strings.Contains(body, "\n"+want) {
			t.Errorf("%s runs on pull_request but lacks the ci-template's concurrency block:\n%s", path, want)
		}
	}
}

// TestMakeBuildHonoursGoBuildTags proves that the ci-template's GO_BUILD_TAGS,
// which the workflow passes to make build, reaches every go build line on top
// of the repository's own tags, and that an empty value adds nothing (#1580).
func TestMakeBuildHonoursGoBuildTags(t *testing.T) {
	probes := []string{"gobuildtagsprobea", "gobuildtagsprobeb"}
	cases := []struct {
		name string
		arg  string
		want bool
	}{
		{name: "space separated list", arg: "GO_BUILD_TAGS=" + strings.Join(probes, " "), want: true},
		{name: "empty", arg: "GO_BUILD_TAGS=", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, tags := range makeBuildTagSets(t, tc.arg) {
				if !tags["gms_pure_go"] {
					t.Errorf("go build line lost the repository tag gms_pure_go: %v", tags)
				}
				for _, probe := range probes {
					if tags[probe] != tc.want {
						t.Errorf("%s: tag %s present=%v, want %v", tc.arg, probe, tags[probe], tc.want)
					}
				}
			}
		})
	}
}

// makeBuildTagSets dry-runs make build with arg and returns the -tags set of
// every go build line it prints. It fails the test when there is none.
func makeBuildTagSets(t *testing.T, arg string) []map[string]bool {
	t.Helper()
	out, err := exec.Command("make", "-n", "--no-print-directory", "-C", repoRoot, "build", arg).CombinedOutput()
	if err != nil {
		t.Fatalf("make -n build %s: %v\n%s", arg, err, out)
	}
	var sets []map[string]bool
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "go build ") {
			continue
		}
		_, rest, found := strings.Cut(line, "-tags '")
		if !found {
			t.Errorf("go build line carries no -tags: %s", line)
			continue
		}
		list, _, _ := strings.Cut(rest, "'")
		set := map[string]bool{}
		for _, tag := range strings.FieldsFunc(list, func(r rune) bool { return r == ' ' || r == ',' }) {
			set[tag] = true
		}
		sets = append(sets, set)
	}
	if len(sets) == 0 {
		t.Fatalf("make -n build %s printed no go build line:\n%s", arg, out)
	}
	return sets
}

// TestNoWorkflowRunsOnASchedule pins the owner ruling of 2026-10-11: the
// coordinator dispatches the nightly full tier, no workflow schedules itself.
// automerge.yml is under owner hold and keeps its sweep.
func TestNoWorkflowRunsOnASchedule(t *testing.T) {
	scheduleRe := regexp.MustCompile(`(?m)^  schedule:`)
	for _, path := range workflowFiles(t) {
		if filepath.Base(path) == "automerge.yml" {
			continue
		}
		if scheduleRe.MatchString(readFile(t, path)) {
			t.Errorf("%s carries a `schedule:` trigger; dispatch it instead", path)
		}
	}
}

// workflowFiles returns every workflow file, failing when there are none so
// the loops above cannot pass over an empty set.
func workflowFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(workflowsDir, "*.yml"))
	if err != nil {
		t.Fatalf("glob %s: %v", workflowsDir, err)
	}
	if len(paths) < 2 {
		t.Fatalf("found %d workflows in %s; discovery has broken", len(paths), workflowsDir)
	}
	return paths
}

// missing returns the elements of want that are absent from got.
func missing(want, got []string) []string {
	have := make(map[string]bool, len(got))
	for _, g := range got {
		have[g] = true
	}
	var out []string
	for _, w := range want {
		if !have[w] {
			out = append(out, w)
		}
	}
	return out
}
