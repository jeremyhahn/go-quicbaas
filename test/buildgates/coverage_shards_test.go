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
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The coverage shards (#1912) split `make coverage` across the ci.yml
// unit-shard-<n> jobs. A package that no shard lists would silently drop out
// of the coverage gate, and one that two shards list would be counted twice,
// so these tests hold the Makefile's shard lists to COVER_PKGS and COVER_STAGES
// and ci.yml's shard jobs to the Makefile's COVER_SHARDS.

const (
	shardJobPrefix = "unit-shard-"
	mergeJobName   = "coverage-merge"
)

var jobNeedsRe = regexp.MustCompile(`(?m)^    needs: \[([^\]]*)\]`)

// coverShard is one coverage shard: its number and the members (packages or
// stages) it holds.
type coverShard struct {
	name    string
	members []string
}

// makeValue returns the expanded value of the Makefile variable name.
func makeValue(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.Command("make", "-s", "--no-print-directory", "-C", repoRoot, "print-"+name).Output()
	if err != nil {
		t.Fatalf("make print-%s: %v", name, err)
	}
	return strings.TrimSpace(string(out))
}

// goListCovered returns the packages the coverage pass profiles for patterns:
// `go list` under TEST_TAGS, proto packages dropped as go_packages drops them.
func goListCovered(t *testing.T, tags string, patterns []string) []string {
	t.Helper()
	if len(patterns) == 0 {
		return nil
	}
	cmd := exec.Command("go", append([]string{"list", "-tags", tags}, patterns...)...)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %v: %v", patterns, err)
	}
	var pkgs []string
	for _, pkg := range strings.Fields(string(out)) {
		if !strings.HasSuffix(pkg, "/proto") {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

// shardPartitionFindings reports every member of all that no shard holds,
// every member that two or more shards hold, every member a shard holds that
// all lacks, and every shard that holds nothing.
func shardPartitionFindings(all []string, shards []coverShard) []string {
	holders := map[string][]string{}
	var findings []string
	for _, shard := range shards {
		if len(shard.members) == 0 {
			findings = append(findings, "shard "+shard.name+" holds nothing")
		}
		for _, member := range shard.members {
			holders[member] = append(holders[member], shard.name)
		}
	}
	for _, member := range all {
		switch n := len(holders[member]); {
		case n == 0:
			findings = append(findings, member+" is in no shard")
		case n > 1:
			findings = append(findings, member+" is in shards "+strings.Join(holders[member], " and "))
		}
	}
	extra := []string{}
	for member := range holders {
		if !slices.Contains(all, member) {
			extra = append(extra, member)
		}
	}
	sort.Strings(extra)
	for _, member := range extra {
		findings = append(findings, member+" is in shard "+strings.Join(holders[member], " and ")+" but not in the coverage set")
	}
	return findings
}

// coverShards returns the Makefile's shards, each holding the expansion of
// the variable COVER_SHARD_<n><suffix> through expand.
func coverShards(t *testing.T, suffix string, expand func([]string) []string) []coverShard {
	t.Helper()
	names := strings.Fields(makeValue(t, "COVER_SHARDS"))
	if len(names) == 0 {
		t.Fatal("COVER_SHARDS is empty; every package would drop out of the sharded coverage gate")
	}
	shards := make([]coverShard, 0, len(names))
	for _, name := range names {
		shards = append(shards, coverShard{name: name, members: expand(strings.Fields(makeValue(t, "COVER_SHARD_"+name+suffix)))})
	}
	return shards
}

func TestCoverageShardsHoldEveryPackageOnce(t *testing.T) {
	tags := makeValue(t, "TEST_TAGS")
	all := goListCovered(t, tags, strings.Fields(makeValue(t, "COVER_PKGS")))
	if len(all) < 2 {
		t.Fatalf("COVER_PKGS lists %d packages; discovery has broken", len(all))
	}
	shards := coverShards(t, "", func(patterns []string) []string { return goListCovered(t, tags, patterns) })
	for _, finding := range shardPartitionFindings(all, shards) {
		t.Error(finding)
	}
}

func TestCoverageShardsHoldEveryStageOnce(t *testing.T) {
	all := strings.Fields(makeValue(t, "COVER_STAGES"))
	if len(all) == 0 {
		t.Fatal("COVER_STAGES is empty")
	}
	shards := coverShards(t, "_STAGES", func(stages []string) []string { return stages })
	// A shard may run its packages alone, so a shard with no stage is fine.
	for _, finding := range shardPartitionFindings(all, shards) {
		if !strings.HasSuffix(finding, " holds nothing") {
			t.Error(finding)
		}
	}
}

func TestShardPartitionFindingsReportsAGapAnOverlapAndAStray(t *testing.T) {
	all := []string{"a", "b", "c"}
	shards := []coverShard{
		{name: "1", members: []string{"a", "b"}},
		{name: "2", members: []string{"b", "z"}},
		{name: "3"},
	}
	want := []string{
		"shard 3 holds nothing",
		"b is in shards 1 and 2",
		"c is in no shard",
		"z is in shard 2 but not in the coverage set",
	}
	if got := shardPartitionFindings(all, shards); !slices.Equal(got, want) {
		t.Errorf("findings = %q, want %q", got, want)
	}
	clean := []coverShard{{name: "1", members: []string{"a"}}, {name: "2", members: []string{"b", "c"}}}
	if got := shardPartitionFindings(all, clean); len(got) != 0 {
		t.Errorf("a clean partition reported %q", got)
	}
}

// ciShardFindings reports where ci.yml's jobs differ from the Makefile's
// shards: a shard with no unit-shard job or a job with no shard, a shard job
// that does not run its own make target, and a merge job that does not run
// coverage-merge after every shard job.
func ciShardFindings(order []string, bodies map[string]string, shards []string) []string {
	var findings []string
	var jobs []string
	for _, job := range order {
		if strings.HasPrefix(job, shardJobPrefix) {
			jobs = append(jobs, strings.TrimPrefix(job, shardJobPrefix))
		}
	}
	for _, shard := range shards {
		if !slices.Contains(jobs, shard) {
			findings = append(findings, "shard "+shard+" has no "+shardJobPrefix+shard+" job")
		}
	}
	for _, job := range jobs {
		if !slices.Contains(shards, job) {
			findings = append(findings, shardJobPrefix+job+" runs a shard COVER_SHARDS does not list")
			continue
		}
		if !strings.Contains(bodies[shardJobPrefix+job], "make coverage-shard-"+job+" ") {
			findings = append(findings, shardJobPrefix+job+" does not run make coverage-shard-"+job)
		}
	}
	merge, ok := bodies[mergeJobName]
	if !ok {
		return append(findings, "no "+mergeJobName+" job gates the shard profiles")
	}
	if !strings.Contains(merge, "make coverage-merge ") {
		findings = append(findings, mergeJobName+" does not run make coverage-merge")
	}
	var needs []string
	if m := jobNeedsRe.FindStringSubmatch(merge); m != nil {
		for _, n := range strings.Split(m[1], ",") {
			needs = append(needs, strings.TrimSpace(n))
		}
	}
	for _, shard := range shards {
		if !slices.Contains(needs, shardJobPrefix+shard) {
			findings = append(findings, mergeJobName+" does not need "+shardJobPrefix+shard)
		}
	}
	return findings
}

func TestCIShardJobsMatchTheMakefileShards(t *testing.T) {
	order, bodies := ciJobs(t, readFile(t, ciWorkflowPath))
	shards := strings.Fields(makeValue(t, "COVER_SHARDS"))
	for _, finding := range ciShardFindings(order, bodies, shards) {
		t.Error(finding)
	}
	for _, job := range order {
		if strings.Contains(bodies[job], "make coverage ") {
			t.Errorf("job %s still runs the unsharded make coverage; the shards and %s run it", job, mergeJobName)
		}
	}
}

func TestCIShardFindingsReportsADriftedWorkflow(t *testing.T) {
	order := []string{"unit-shard-1", "unit-shard-4", mergeJobName}
	bodies := map[string]string{
		"unit-shard-1": "      run: make coverage-shard-2 X\n",
		"unit-shard-4": "      run: make coverage-shard-4 X\n",
		mergeJobName:   "    needs: [unit-shard-1]\n      run: make coverage-merge X\n",
	}
	want := []string{
		"shard 2 has no unit-shard-2 job",
		"unit-shard-1 does not run make coverage-shard-1",
		"unit-shard-4 runs a shard COVER_SHARDS does not list",
		"coverage-merge does not need unit-shard-2",
	}
	if got := ciShardFindings(order, bodies, []string{"1", "2"}); !slices.Equal(got, want) {
		t.Errorf("findings = %q, want %q", got, want)
	}
	if got := ciShardFindings([]string{"unit-shard-1"}, map[string]string{"unit-shard-1": "make coverage-shard-1 X"}, []string{"1"}); !slices.Equal(got, []string{"no coverage-merge job gates the shard profiles"}) {
		t.Errorf("a workflow with no merge job reported %q", got)
	}
}

// TestCoverageShardsHonourGoBuildTags holds each shard to the tag rule
// TestMakeStagesHonourGoBuildTags holds `coverage` to (#1580): its package
// discovery, its coverage pass and its tag-pass and tooling go tests all
// carry GO_BUILD_TAGS.
func TestCoverageShardsHonourGoBuildTags(t *testing.T) {
	probes := []string{"gobuildtagsprobea", "gobuildtagsprobeb"}
	stageTests := regexp.MustCompile(`^go test -p 2 `)
	for _, shard := range strings.Fields(makeValue(t, "COVER_SHARDS")) {
		taggedStages := 0
		for _, stage := range strings.Fields(makeValue(t, "COVER_SHARD_"+shard+"_STAGES")) {
			if stage != "test-sdk-go" {
				taggedStages++
			}
		}
		target := "coverage-shard-" + shard
		invocations := []stageInvocation{
			{role: "package discovery go list", count: 1, cmd: regexp.MustCompile(`^pkgs=\$\(go list (-tags '[^']*' )?\./(pkg|cmd)/`), tags: singleQuotedTags},
			{role: "coverage go test", count: 1, cmd: regexp.MustCompile(`\bgo test -race .*-coverprofile=coverage/shard-` + shard + `\.out `), tags: singleQuotedTags},
			{role: "stage go test", count: taggedStages, cmd: stageTests, tags: singleQuotedTags},
		}
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			out := makeDryRun(t, makefilePath, target, "GO_BUILD_TAGS="+strings.Join(probes, " "))
			for _, finding := range stageTagFindings(target, invocations, out, probes, true) {
				t.Error(finding)
			}
		})
	}
}
