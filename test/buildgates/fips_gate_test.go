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
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fipsJobName is ci.yml's scoped FIPS gate (#772; owner ruling #114 and SPEC
// security-hardening: "The scoped FIPS gate runs on push").
const fipsJobName = "fips"

// TestFIPSGateRunsOnPush pins the scoped fips140=only gate on the push path. It
// must be a ci.yml job that CI Gate requires, in the code tier (it skips only
// on a docs-only pull request), never schedule- or dispatch-only, and it must
// run `make test-fips-gate`. nightly.yml's full run doesn't count, because no
// merge waits on it.
func TestFIPSGateRunsOnPush(t *testing.T) {
	order, bodies := ciJobs(t, readFile(t, ciWorkflowPath))
	if !slices.Contains(order, fipsJobName) {
		t.Fatalf("ci.yml has no %q job: the scoped FIPS gate is off the push path (#772)", fipsJobName)
	}
	job := bodies[fipsJobName]
	if nonGatingJobs[fipsJobName] {
		t.Errorf("%q is listed in nonGatingJobs; the FIPS gate is required", fipsJobName)
	}
	if !regexp.MustCompile(`(?m)^\s+run: make test-fips-gate\b`).MatchString(job) {
		t.Errorf("the %q job does not run `make test-fips-gate`", fipsJobName)
	}
	cond := ""
	if m := regexp.MustCompile(`(?m)^    if: (.*)$`).FindStringSubmatch(job); m != nil {
		cond = m[1]
	}
	for _, event := range []string{"schedule", "workflow_dispatch"} {
		if strings.Contains(cond, event) {
			t.Errorf("the %q job's `if:` (%s) names %s; the gate runs on every push", fipsJobName, cond, event)
		}
	}

	gate := bodies[gateJobName]
	if !slices.Contains(gateNeeds(t, gate), fipsJobName) {
		t.Errorf("%s does not need %q", gateJobName, fipsJobName)
	}
	if tier := gateChecks(gate)[fipsJobName]; tier != "code" {
		t.Errorf("%s checks %q as %q; want code (skipped only on a docs-only pull request)", gateJobName, fipsJobName, tier)
	}
}

// TestFIPSGateExcludesOnlyWithAnIssue pins the Makefile's FIPS_EXCLUDE rules
// that test-fips-gate enforces at run time: each entry names one package and
// a remediation issue, with no duplicates.
func TestFIPSGateExcludesOnlyWithAnIssue(t *testing.T) {
	makefile := readFile(t, filepath.Join(repoRoot, "Makefile"))
	if !strings.Contains(makefile, "\ntest-fips-gate:") {
		t.Fatal("the Makefile has no test-fips-gate target")
	}
	if !strings.Contains(makefile, "\nFIPS_EXCLUDE :=\n") {
		t.Fatal("the Makefile does not start FIPS_EXCLUDE empty (`FIPS_EXCLUDE :=`)")
	}
	entry := regexp.MustCompile(`^FIPS_EXCLUDE \+= (\./[A-Za-z0-9/_.-]+) +# .*#\d+`)
	seen := map[string]bool{}
	for _, line := range strings.Split(makefile, "\n") {
		if !strings.HasPrefix(line, "FIPS_EXCLUDE +=") {
			continue
		}
		m := entry.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("FIPS_EXCLUDE line %q is not one package path with a `# ... #<issue>` comment", line)
			continue
		}
		if seen[m[1]] {
			t.Errorf("FIPS_EXCLUDE lists %s twice", m[1])
		}
		seen[m[1]] = true
	}
}
