// Ruling 283(9): a draft pull request runs lint and unit tests only. The
// Integration job takes 84-111 minutes on the bigdisk runner, and a draft's
// run of it is thrown away by the next push, so its `if:` must skip drafts.
// The draft's Integration runs on the first push after it is marked ready.

package buildgates

import (
	"slices"
	"strings"
	"testing"
)

const (
	integrationJobName = "integration"
	docsOnlyCondition  = "needs.changes.outputs.docs_only != 'true'"
	notDraftCondition  = "!github.event.pull_request.draft"
)

// integrationIfFindings returns what is wrong with the integration job's `if:`
// in a ci.yml body, and an empty slice when it skips both docs-only changes
// and draft pull requests.
func integrationIfFindings(t *testing.T, body string) []string {
	t.Helper()
	_, bodies := ciJobs(t, body)
	job, ok := bodies[integrationJobName]
	if !ok {
		return []string{"ci.yml has no " + integrationJobName + " job"}
	}
	var cond string
	for _, line := range strings.Split(job, "\n") {
		if jobIfRe.MatchString(line) {
			cond = line
			break
		}
	}
	if cond == "" {
		return []string{"the " + integrationJobName + " job has no `if:`"}
	}
	var findings []string
	for _, want := range []string{docsOnlyCondition, notDraftCondition} {
		if !strings.Contains(cond, want) {
			findings = append(findings, "the "+integrationJobName+" job's `if:` lacks "+want)
		}
	}
	return findings
}

func TestIntegrationSkipsDraftPullRequests(t *testing.T) {
	body := readFile(t, ciWorkflowPath)
	for _, f := range integrationIfFindings(t, body) {
		t.Error(f)
	}

	_, bodies := ciJobs(t, body)
	gate, ok := bodies[gateJobName]
	if !ok {
		t.Fatalf("ci.yml has no %q job", gateJobName)
	}
	if slices.Contains(gateNeeds(t, gate), integrationJobName) {
		t.Errorf("%s needs %s. A draft skips Integration, so the gate would "+
			"read a skipped result; Integration must stay non-gating (row #1700).",
			gateJobName, integrationJobName)
	}
}

func TestIntegrationDraftCheckCatchesAMissingDraftCondition(t *testing.T) {
	const body = `jobs:
  changes:
    runs-on: ubuntu-latest
  integration:
    needs: [changes]
    if: ${{ needs.changes.outputs.docs_only != 'true' }}
    runs-on: ubuntu-latest
`
	findings := integrationIfFindings(t, body)
	want := "the " + integrationJobName + " job's `if:` lacks " + notDraftCondition
	if !slices.Equal(findings, []string{want}) {
		t.Errorf("findings = %q, want only %q", findings, want)
	}
}
