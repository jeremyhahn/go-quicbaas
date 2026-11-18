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
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	// hostedRunner is where every ci.yml job runs (ci-template v1.43,
	// rulings 833 and 834): the standard GitHub-hosted runner.
	hostedRunner = "ubuntu-latest"
	// selfHostedWaiver opens the line comment a job that leaves
	// hostedRunner carries on its runs-on line.
	selfHostedWaiver = "# self-hosted: "
	// overflowReason is the waiver of the split unit matrix (ruling 834,
	// phase 2 option C, ruling 841).
	overflowReason = "overflow split, ruling 834"
	// samePoolEvents is true for a push to main and a same-repository pull
	// request: a fork's pull request never reaches the pool.
	samePoolEvents = `((github.event_name == 'push' && github.ref == 'refs/heads/main') || (github.event_name == 'pull_request' && github.event.pull_request.head.repo.full_name == github.repository))`
	// splitHead and splitTail bracket the split unit matrix's runs-on: the
	// shards the JSON list between them names run on the bigdisk pool, where
	// the job's runner.environment == 'self-hosted' steps run make natively
	// under setup-go; every other shard runs on hostedRunner.
	splitHead = `${{ contains(fromJSON('`
	splitTail = `'), matrix.shard) && ` + samePoolEvents + ` && fromJSON('["self-hosted","linux","x64","bigdisk"]') || '` + hostedRunner + `' }}`
	// splitJob is the matrix job that overflows to the pool.
	splitJob = "unit"
)

// releaseJobs keep the runner their own workflow names: publish-images runs
// on Gitea's release label and on hostedRunner on github.com.
var releaseJobs = map[string]string{
	"publish-images": `${{ github.server_url == 'https://github.com' && '` + hostedRunner + `' || 'release' }}`,
}

// runnerProblems lists every ci.yml job that is not on hostedRunner the way
// rulings 833, 834 and 841 allow, given the workflow's text.
func runnerProblems(body []byte) []string {
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil || len(doc.Content) == 0 {
		return []string{"ci.yml does not parse: " + errString(err)}
	}
	jobs := mapValue(doc.Content[0], "jobs")
	if jobs == nil || len(jobs.Content) == 0 {
		return []string{"ci.yml has no jobs"}
	}
	var problems []string
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		name, job := jobs.Content[i].Value, jobs.Content[i+1]
		runsOn := mapValue(job, "runs-on")
		if runsOn == nil {
			problems = append(problems, "job "+name+" has no runs-on")
			continue
		}
		waiver := strings.TrimSpace(runsOn.LineComment)
		switch {
		case name == splitJob:
			problems = append(problems, splitProblems(job, runsOn.Value, waiver)...)
		case releaseJobs[name] != "":
			if runsOn.Value != releaseJobs[name] {
				problems = append(problems, "job "+name+" runs on "+strconv.Quote(runsOn.Value)+"; want "+strconv.Quote(releaseJobs[name]))
			}
		case runsOn.Value != hostedRunner:
			problems = append(problems, "job "+name+" runs on "+strconv.Quote(runsOn.Value)+"; every job but the split "+splitJob+" matrix runs on "+hostedRunner+" (rulings 833, 834)")
		}
	}
	return problems
}

// splitProblems checks the split matrix job: its runs-on sends a proper,
// non-empty subset of its shards to the pool, states the overflow waiver,
// and the job keeps the native steps a pool shard runs.
func splitProblems(job *yaml.Node, runsOn, waiver string) []string {
	where := "job " + splitJob
	if !strings.HasPrefix(runsOn, splitHead) || !strings.HasSuffix(runsOn, splitTail) {
		return []string{where + " does not split its shards between " + hostedRunner + " and the bigdisk pool for same-repository events (ruling 841): runs-on is " + strconv.Quote(runsOn)}
	}
	var problems []string
	if waiver != selfHostedWaiver+overflowReason {
		problems = append(problems, where+" splits onto the pool without `"+selfHostedWaiver+overflowReason+"` on its runs-on line")
	}
	var pool []int
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(runsOn, splitHead), splitTail)), &pool); err != nil {
		return append(problems, where+" names its pool shards in a list that is not JSON integers: "+err.Error())
	}
	var shards []int
	if m := mapValue(mapValue(mapValue(job, "strategy"), "matrix"), "shard"); m != nil {
		if err := m.Decode(&shards); err != nil {
			return append(problems, where+" has a shard matrix that is not integers: "+err.Error())
		}
	}
	for _, s := range pool {
		if !slices.Contains(shards, s) {
			problems = append(problems, where+" sends shard "+strconv.Itoa(s)+" to the pool, which its matrix "+intsString(shards)+" does not have")
		}
	}
	if len(pool) == 0 || len(pool) >= len(shards) {
		problems = append(problems, where+" sends "+intsString(pool)+" of "+intsString(shards)+" to the pool; a split keeps shards on both sides")
	}
	var native bool
	for _, step := range mapValue(job, "steps").Content {
		run, cond := mapValue(step, "run"), mapValue(step, "if")
		if run != nil && cond != nil && strings.Contains(run.Value, "make coverage-shard") &&
			strings.Contains(cond.Value, "runner.environment == 'self-hosted'") {
			native = true
		}
	}
	if !native {
		problems = append(problems, where+" has no `make coverage-shard` step for runner.environment == 'self-hosted': a pool shard would run nothing")
	}
	// A pool shard's t.TempDir socket paths overflow sun_path under the
	// runner's _work/_temp, so the job takes a short per-job directory. Go
	// 1.27's t.TempDir uses GOTMPDIR whenever it is set, and the pool sets
	// it to _work/_temp, so the step must point both at the short directory.
	var short bool
	for _, step := range mapValue(job, "steps").Content {
		run := mapValue(step, "run")
		if run != nil && strings.Contains(run.Value, "mktemp -d /tmp/") &&
			strings.Contains(run.Value, `echo "TMPDIR=$d"`) && strings.Contains(run.Value, `echo "GOTMPDIR=$d"`) {
			short = true
		}
	}
	if !short {
		problems = append(problems, where+" has no short per-job temp step that sets both TMPDIR and GOTMPDIR: on the pool, GOTMPDIR (_work/_temp) wins for t.TempDir and Unix socket paths overflow")
	}
	return problems
}

// mapValue returns the value of key in the mapping n, or nil.
func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func intsString(v []int) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func errString(err error) string {
	if err == nil {
		return "empty document"
	}
	return err.Error()
}

func readCI(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	return b
}

// TestCIRunsHostedFirst: rulings 833, 834 and 841. Every ci.yml job runs on
// the GitHub-hosted runner but the release publish and the unit matrix,
// which overflows a proper subset of its shards to the bigdisk pool.
func TestCIRunsHostedFirst(t *testing.T) {
	for _, p := range runnerProblems(readCI(t)) {
		t.Error(p)
	}
}

// TestRunnerProblems_Mutations: each regression of ci.yml's routing is
// reported.
func TestRunnerProblems_Mutations(t *testing.T) {
	body := string(readCI(t))
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("parse ci.yml: %v", err)
	}
	current := mapValue(mapValue(mapValue(doc.Content[0], "jobs"), splitJob), "runs-on").Value
	waived := func(runsOn string) string { return "    runs-on: " + runsOn + "  " + selfHostedWaiver + overflowReason }
	currentLine := waived(current)
	if !strings.Contains(body, currentLine) {
		t.Fatalf("ci.yml lacks the split runs-on line %q", currentLine)
	}
	splitLine := func(runsOn string) func(string) string {
		return func(b string) string { return strings.Replace(b, currentLine, runsOn, 1) }
	}
	for _, c := range []struct {
		name   string
		mutate func(string) string
		want   string
	}{
		{"waiver dropped", splitLine("    runs-on: " + current), "without `" + selfHostedWaiver + overflowReason + "`"},
		{"every shard on the pool", splitLine(waived(splitHead + "[0, 1, 2, 3]" + splitTail)), "keeps shards on both sides"},
		{"no shard on the pool", splitLine(waived(splitHead + "[]" + splitTail)), "keeps shards on both sides"},
		{"a shard the matrix lacks", splitLine(waived(splitHead + "[0, 7]" + splitTail)), "sends shard 7 to the pool"},
		{"fork pull requests reach the pool", splitLine(waived(strings.Replace(current, samePoolEvents+" && ", "", 1))), "does not split its shards"},
		{"split reverted to hosted", splitLine("    runs-on: " + hostedRunner), "does not split its shards"},
		{"another job leaves the hosted runner", func(b string) string {
			return replaceJobRunner(t, b, "coverage", "[self-hosted, linux, x64, bigdisk]")
		}, "job coverage runs on"},
		{"native shard step removed", func(b string) string { return dropNativeShardStep(t, b) }, "a pool shard would run nothing"},
		{"short temp step leaves GOTMPDIR", func(b string) string {
			return strings.Replace(b, `echo "GOTMPDIR=$d" >> "$GITHUB_ENV"`+"\n", "", 1)
		}, "sets both TMPDIR and GOTMPDIR"},
	} {
		t.Run(c.name, func(t *testing.T) {
			mutated := c.mutate(body)
			if mutated == body {
				t.Fatal("the mutation changed nothing")
			}
			p := runnerProblems([]byte(mutated))
			if !slices.ContainsFunc(p, func(s string) bool { return strings.Contains(s, c.want) }) {
				t.Fatalf("want a problem containing %q, got %v", c.want, p)
			}
		})
	}
}

// TestRunnerProblems_Unparsable: a workflow that is not YAML, or has no
// jobs, is reported rather than passed.
func TestRunnerProblems_Unparsable(t *testing.T) {
	for in, want := range map[string]string{"jobs: [": "does not parse", "on: push\n": "has no jobs"} {
		if p := runnerProblems([]byte(in)); len(p) != 1 || !strings.Contains(p[0], want) {
			t.Errorf("runnerProblems(%q) = %v; want one problem containing %q", in, p, want)
		}
	}
}

// replaceJobRunner sets job's first runs-on line to runsOn.
func replaceJobRunner(t *testing.T, body, job, runsOn string) string {
	t.Helper()
	start := strings.Index(body, "\n  "+job+":\n")
	if start < 0 {
		t.Fatalf("ci.yml has no job %s", job)
	}
	i := strings.Index(body[start:], "    runs-on: ") + start
	end := strings.Index(body[i:], "\n") + i
	return body[:i] + "    runs-on: " + runsOn + body[end:]
}

// dropNativeShardStep removes the unit job's native make coverage-shard
// step, from its `- name:` line to the next step.
func dropNativeShardStep(t *testing.T, body string) string {
	t.Helper()
	const name = "- name: make coverage-shard (bigdisk pool, native)\n"
	i := strings.Index(body, name)
	if i < 0 {
		t.Fatalf("ci.yml lacks %q", name)
	}
	next := strings.Index(body[i+len(name):], "- name: ")
	if next < 0 {
		t.Fatal("the native shard step is the last step; nothing marks its end")
	}
	return body[:i] + body[i+len(name)+next:]
}
